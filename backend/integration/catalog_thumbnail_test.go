package integration

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The Thumbnail (spec #853): an Event's optional square image, framed by the
// organizer in the browser and stored as one JPEG under its own `thumbnails/`
// prefix. Attached and cleared through the Event PATCH exactly like the Cover
// Image, and independent of it.

type thumbnailUpload struct {
	UploadURL string `json:"upload_url"`
	ObjectKey string `json:"object_key"`
	PublicURL string `json:"public_url"`
}

// mintThumbnail issues a Thumbnail upload URL and returns what was minted.
func mintThumbnail(t *testing.T, env *testEnv, sessionID, eventID string) thumbnailUpload {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/thumbnail-upload-url", map[string]string{
		"content_type": "image/jpeg",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("thumbnail upload url status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var upload thumbnailUpload
	if err := json.Unmarshal(body.Data, &upload); err != nil {
		t.Fatalf("decode thumbnail upload: %v", err)
	}
	return upload
}

func mintThumbnailKey(t *testing.T, env *testEnv, sessionID, eventID string) string {
	t.Helper()
	return mintThumbnail(t, env, sessionID, eventID).ObjectKey
}

// eventMedia is the staff Event detail's media, read off a PATCH or a GET.
type eventMedia struct {
	CoverImageKey *string `json:"cover_image_key"`
	CoverVideoKey *string `json:"cover_video_key"`
	ThumbnailKey  *string `json:"thumbnail_key"`
	ThumbnailURL  *string `json:"thumbnail_url"`
}

func patchEventMediaRead(t *testing.T, env *testEnv, sessionID, eventID, name, slug string, fields map[string]any) eventMedia {
	t.Helper()
	payload := map[string]any{"name": name, "slug": slug}
	for k, v := range fields {
		payload[k] = v
	}
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, payload, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch %v status=%d error=%+v", fields, resp.StatusCode, body.Error)
	}
	var media eventMedia
	if err := json.Unmarshal(body.Data, &media); err != nil {
		t.Fatalf("decode patched event: %v", err)
	}
	return media
}

func readEventMedia(t *testing.T, env *testEnv, sessionID, eventID string) eventMedia {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var media eventMedia
	if err := json.Unmarshal(body.Data, &media); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	return media
}

func assertKey(t *testing.T, field string, got *string, want string) {
	t.Helper()
	if want == "" {
		if got != nil {
			t.Fatalf("%s = %q, want null", field, *got)
		}
		return
	}
	if got == nil || *got != want {
		t.Fatalf("%s = %v, want %q", field, got, want)
	}
}

// expectPatchRefused sends a PATCH and asserts it is refused with code.
func expectPatchRefused(t *testing.T, env *testEnv, sessionID, eventID, name, slug string, fields map[string]any, code string) {
	t.Helper()
	payload := map[string]any{"name": name, "slug": slug}
	for k, v := range fields {
		payload[k] = v
	}
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, payload, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("patch %v: expected 400, got %d error=%+v", fields, resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != code {
		t.Fatalf("patch %v: expected %s, got %+v", fields, code, body.Error)
	}
}

func TestThumbnailUploadURLIsIssuedForJPEG(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Square Fest", "square-fest")

	upload := mintThumbnail(t, env, sessionID, eventID)
	if upload.UploadURL == "" {
		t.Fatal("expected upload_url")
	}
	if !strings.HasPrefix(upload.ObjectKey, "thumbnails/") {
		t.Fatalf("object_key=%q, want the thumbnails/ prefix", upload.ObjectKey)
	}
	if !strings.Contains(upload.ObjectKey, eventID) {
		t.Fatalf("object_key should include event id: %q", upload.ObjectKey)
	}
	if !strings.HasSuffix(upload.ObjectKey, ".jpg") {
		t.Fatalf("object_key should end in .jpg: %q", upload.ObjectKey)
	}
	if upload.PublicURL == "" || !strings.Contains(upload.PublicURL, upload.ObjectKey) {
		t.Fatalf("public_url=%q object_key=%q", upload.PublicURL, upload.ObjectKey)
	}
}

func TestThumbnailUploadURLRefusesEveryTypeButJPEG(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Not JPEG", "not-jpeg")

	// PNG and WebP are fine for a Cover Image, but a Thumbnail is always the
	// browser's own JPEG export of the crop, never a raw upload.
	for _, contentType := range []string{"image/png", "image/webp", "image/gif", ""} {
		resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/thumbnail-upload-url", map[string]string{
			"content_type": contentType,
		}, authHeader(sessionID))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%q: expected 400, got %d", contentType, resp.StatusCode)
		}
		if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
			t.Fatalf("%q: expected VALIDATION_FAILED, got %+v", contentType, body.Error)
		}
	}
}

func TestThumbnailUploadURLForbiddenForNonOrgAdmin(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Guarded Square", "guarded-square")

	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "staff@example.com",
		"role":  "event_staff",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}

	staffSessionID := verifyOTP(t, env, "staff@example.com")
	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/thumbnail-upload-url", map[string]string{
		"content_type": "image/jpeg",
	}, authHeader(staffSessionID))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d error=%+v", resp.StatusCode, body.Error)
	}
}

func TestThumbnailAttachPreserveAndClear(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Attach Square", "attach-square")
	upload := mintThumbnail(t, env, sessionID, eventID)

	updated := patchEventMediaRead(t, env, sessionID, eventID, "Attach Square", "attach-square", map[string]any{
		"thumbnail_key": upload.ObjectKey,
	})
	assertKey(t, "thumbnail_key", updated.ThumbnailKey, upload.ObjectKey)
	assertKey(t, "thumbnail_url", updated.ThumbnailURL, upload.PublicURL)

	// Omitting the field leaves the Thumbnail alone.
	patchEventMediaRead(t, env, sessionID, eventID, "Attach Square", "attach-square", nil)
	read := readEventMedia(t, env, sessionID, eventID)
	assertKey(t, "read thumbnail_key", read.ThumbnailKey, upload.ObjectKey)
	assertKey(t, "read thumbnail_url", read.ThumbnailURL, upload.PublicURL)

	// An empty string clears it.
	cleared := patchEventMediaRead(t, env, sessionID, eventID, "Attach Square", "attach-square", map[string]any{
		"thumbnail_key": "",
	})
	assertKey(t, "cleared thumbnail_key", cleared.ThumbnailKey, "")
	assertKey(t, "cleared thumbnail_url", cleared.ThumbnailURL, "")
}

func TestThumbnailAttachRefusesKeysOutsideTheEventsThumbnailPrefix(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Own Square", "own-square")
	otherEventID := createDraftEvent(t, env, sessionID, "Other Square", "other-square")

	otherSessionID := otherOrganizationSession(t, env)
	otherOrgEventID := createDraftEvent(t, env, otherSessionID, "Foreign Square", "foreign-square")

	refused := map[string]string{
		"another Event":        mintThumbnailKey(t, env, sessionID, otherEventID),
		"another Organization": mintThumbnailKey(t, env, otherSessionID, otherOrgEventID),
		"the covers prefix":    mintCoverImageKey(t, env, sessionID, eventID),
		"the videos prefix":    mintCoverVideoKey(t, env, sessionID, eventID),
		"a path escape":        "thumbnails/../covers/x.jpg",
	}
	for name, key := range refused {
		t.Run(name, func(t *testing.T) {
			expectPatchRefused(t, env, sessionID, eventID, "Own Square", "own-square", map[string]any{
				"thumbnail_key": key,
			}, "INVALID_THUMBNAIL_KEY")
		})
	}

	// And the mirror: a Thumbnail key for this very Event is not a Cover Image.
	expectPatchRefused(t, env, sessionID, eventID, "Own Square", "own-square", map[string]any{
		"cover_image_key": mintThumbnailKey(t, env, sessionID, eventID),
	}, "INVALID_COVER_IMAGE_KEY")

	// Nothing refused was stored.
	assertKey(t, "thumbnail_key", readEventMedia(t, env, sessionID, eventID).ThumbnailKey, "")
}

func TestThumbnailAndCoverImageAreIndependent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	const name, slug = "Two Images", "two-images"
	eventID := createDraftEvent(t, env, sessionID, name, slug)

	// A Thumbnail needs no Cover Image.
	thumbnail := mintThumbnailKey(t, env, sessionID, eventID)
	media := patchEventMediaRead(t, env, sessionID, eventID, name, slug, map[string]any{
		"thumbnail_key": thumbnail,
	})
	assertKey(t, "thumbnail_key", media.ThumbnailKey, thumbnail)
	assertKey(t, "cover_image_key", media.CoverImageKey, "")

	// Adding a Cover Image leaves the Thumbnail.
	cover := mintCoverImageKey(t, env, sessionID, eventID)
	media = patchEventMediaRead(t, env, sessionID, eventID, name, slug, map[string]any{
		"cover_image_key": cover,
	})
	assertKey(t, "thumbnail_key", media.ThumbnailKey, thumbnail)
	assertKey(t, "cover_image_key", media.CoverImageKey, cover)

	// Replacing the Cover Image leaves the Thumbnail.
	cover = mintCoverImageKey(t, env, sessionID, eventID)
	media = patchEventMediaRead(t, env, sessionID, eventID, name, slug, map[string]any{
		"cover_image_key": cover,
	})
	assertKey(t, "thumbnail_key", media.ThumbnailKey, thumbnail)

	// Replacing the Thumbnail leaves the Cover Image.
	thumbnail = mintThumbnailKey(t, env, sessionID, eventID)
	media = patchEventMediaRead(t, env, sessionID, eventID, name, slug, map[string]any{
		"thumbnail_key": thumbnail,
	})
	assertKey(t, "thumbnail_key", media.ThumbnailKey, thumbnail)
	assertKey(t, "cover_image_key", media.CoverImageKey, cover)

	// Clearing the Cover Image leaves the Thumbnail.
	media = patchEventMediaRead(t, env, sessionID, eventID, name, slug, map[string]any{
		"cover_image_key": "",
	})
	assertKey(t, "thumbnail_key", media.ThumbnailKey, thumbnail)
	assertKey(t, "cover_image_key", media.CoverImageKey, "")

	// Clearing the Thumbnail leaves the Cover Image.
	cover = mintCoverImageKey(t, env, sessionID, eventID)
	patchEventMediaRead(t, env, sessionID, eventID, name, slug, map[string]any{
		"cover_image_key": cover,
	})
	media = patchEventMediaRead(t, env, sessionID, eventID, name, slug, map[string]any{
		"thumbnail_key": "",
	})
	assertKey(t, "thumbnail_key", media.ThumbnailKey, "")
	assertKey(t, "cover_image_key", media.CoverImageKey, cover)
}

func TestCoverVideoPosterRuleIgnoresTheThumbnail(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	const name, slug = "Square Poster", "square-poster"
	eventID := createDraftEvent(t, env, sessionID, name, slug)
	thumbnail := mintThumbnailKey(t, env, sessionID, eventID)

	// A Thumbnail is not a poster: a Cover Video still needs a Cover Image.
	expectPatchRefused(t, env, sessionID, eventID, name, slug, map[string]any{
		"thumbnail_key":   thumbnail,
		"cover_video_key": mintCoverVideoKey(t, env, sessionID, eventID),
	}, "COVER_VIDEO_REQUIRES_COVER_IMAGE")

	// With a Cover Image the video attaches, and the Thumbnail comes and goes
	// without touching either.
	cover := mintCoverImageKey(t, env, sessionID, eventID)
	video := mintCoverVideoKey(t, env, sessionID, eventID)
	media := patchEventMediaRead(t, env, sessionID, eventID, name, slug, map[string]any{
		"cover_image_key": cover,
		"cover_video_key": video,
		"thumbnail_key":   thumbnail,
	})
	assertKey(t, "thumbnail_key", media.ThumbnailKey, thumbnail)

	media = patchEventMediaRead(t, env, sessionID, eventID, name, slug, map[string]any{
		"thumbnail_key": "",
	})
	assertKey(t, "cover_image_key", media.CoverImageKey, cover)
	assertKey(t, "cover_video_key", media.CoverVideoKey, video)

	// And the Cover Image still cannot be cleared from under the video, even
	// with a Thumbnail on the Event.
	patchEventMediaRead(t, env, sessionID, eventID, name, slug, map[string]any{
		"thumbnail_key": mintThumbnailKey(t, env, sessionID, eventID),
	})
	expectPatchRefused(t, env, sessionID, eventID, name, slug, map[string]any{
		"cover_image_key": "",
	}, "COVER_VIDEO_REQUIRES_COVER_IMAGE")
}

// thumbnailURLField decodes a JSON object and returns its thumbnail_url, failing
// when the field is missing altogether: null and absent are different answers.
func thumbnailURLField(t *testing.T, where string, raw json.RawMessage) *string {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("%s: decode: %v", where, err)
	}
	value, ok := fields["thumbnail_url"]
	if !ok {
		t.Fatalf("%s: thumbnail_url is missing", where)
	}
	var url *string
	if err := json.Unmarshal(value, &url); err != nil {
		t.Fatalf("%s: decode thumbnail_url: %v", where, err)
	}
	return url
}

// rawCardBySlug picks the card with slug out of a list of raw cards.
func rawCardBySlug(t *testing.T, where string, cards []json.RawMessage, slug string) json.RawMessage {
	t.Helper()
	for _, card := range cards {
		var head struct {
			Slug string `json:"slug"`
		}
		if err := json.Unmarshal(card, &head); err != nil {
			t.Fatalf("%s: decode card: %v", where, err)
		}
		if head.Slug == slug {
			return card
		}
	}
	t.Fatalf("%s: no card %s", where, slug)
	return nil
}

func TestThumbnailURLOnEverySurfaceThatShowsAnEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	soon := env.fixedClock.Add(10 * 24 * time.Hour)

	squareID := publishEvent(t, env, sessionID, "Has Square", "has-square", soon, true, 2500, 50)
	upload := mintThumbnail(t, env, sessionID, squareID)
	patchEventMediaRead(t, env, sessionID, squareID, "Has Square", "has-square", map[string]any{
		"starts_at":       soon.Format(time.RFC3339),
		"timezone":        "America/New_York",
		"venue_name":      "The Hall",
		"cover_image_key": mintCoverImageKey(t, env, sessionID, squareID),
		"thumbnail_key":   upload.ObjectKey,
	})

	// Cover Image only: the API never substitutes it into thumbnail_url.
	coverOnlyID := publishEvent(t, env, sessionID, "Cover Only", "cover-only", soon, true, 2500, 50)
	patchEventMediaRead(t, env, sessionID, coverOnlyID, "Cover Only", "cover-only", map[string]any{
		"starts_at":       soon.Format(time.RFC3339),
		"timezone":        "America/New_York",
		"venue_name":      "The Hall",
		"cover_image_key": mintCoverImageKey(t, env, sessionID, coverOnlyID),
	})

	check := func(where string, raw json.RawMessage, want string) {
		t.Helper()
		got := thumbnailURLField(t, where, raw)
		if want == "" {
			if got != nil {
				t.Fatalf("%s: thumbnail_url = %q, want null", where, *got)
			}
			return
		}
		if got == nil || *got != want {
			t.Fatalf("%s: thumbnail_url = %v, want %q", where, got, want)
		}
	}

	// The staff Event detail.
	for id, want := range map[string]string{squareID: upload.PublicURL, coverOnlyID: ""} {
		resp, body := env.get(t, "/api/v1/staff/events/"+id, authHeader(sessionID))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("staff event status=%d error=%+v", resp.StatusCode, body.Error)
		}
		check("staff detail "+id, body.Data, want)
	}

	// The public explorer.
	resp, body := env.get(t, "/api/v1/public/events", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public events status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var explorer struct {
		Events []json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(body.Data, &explorer); err != nil {
		t.Fatalf("decode explorer: %v", err)
	}
	check("explorer has-square", rawCardBySlug(t, "explorer", explorer.Events, "has-square"), upload.PublicURL)
	check("explorer cover-only", rawCardBySlug(t, "explorer", explorer.Events, "cover-only"), "")

	// The Organization listing.
	resp, body = env.get(t, "/api/v1/public/organizations/test-org/events", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("organization listing status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var listing struct {
		Upcoming []json.RawMessage `json:"upcoming"`
	}
	if err := json.Unmarshal(body.Data, &listing); err != nil {
		t.Fatalf("decode listing: %v", err)
	}
	check("listing has-square", rawCardBySlug(t, "listing", listing.Upcoming, "has-square"), upload.PublicURL)
	check("listing cover-only", rawCardBySlug(t, "listing", listing.Upcoming, "cover-only"), "")

	// The public Event detail.
	for slug, want := range map[string]string{"has-square": upload.PublicURL, "cover-only": ""} {
		resp, body = env.get(t, "/api/v1/public/organizations/test-org/events/"+slug, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("public event status=%d error=%+v", resp.StatusCode, body.Error)
		}
		check("public detail "+slug, body.Data, want)
		var public struct {
			ThumbnailKey *string `json:"thumbnail_key"`
		}
		if err := json.Unmarshal(body.Data, &public); err != nil {
			t.Fatalf("decode public event: %v", err)
		}
		if public.ThumbnailKey != nil {
			t.Fatalf("public view must not expose thumbnail_key, got %v", *public.ThumbnailKey)
		}
	}
}

func TestThumbnailReplaceAndClearDeleteTheOldObject(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	const name, slug = "Clean Square", "clean-square"
	eventID := createDraftEvent(t, env, sessionID, name, slug)

	first := mintThumbnailKey(t, env, sessionID, eventID)
	patchEventMedia(t, env, sessionID, eventID, name, slug, map[string]any{"thumbnail_key": first})
	assertDeleted(t, nil)

	// Omitted and resent unchanged: still in use, nothing deleted.
	patchEventMedia(t, env, sessionID, eventID, name, slug, nil)
	patchEventMedia(t, env, sessionID, eventID, name, slug, map[string]any{"thumbnail_key": first})
	assertDeleted(t, nil)

	second := mintThumbnailKey(t, env, sessionID, eventID)
	patchEventMedia(t, env, sessionID, eventID, name, slug, map[string]any{"thumbnail_key": second})
	assertDeleted(t, []string{first})

	patchEventMedia(t, env, sessionID, eventID, name, slug, map[string]any{"thumbnail_key": ""})
	assertDeleted(t, []string{first, second})
}

func TestThumbnailCleanupFailureDoesNotFailThePatch(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	const name, slug = "Doomed Square", "doomed-square"
	eventID := createDraftEvent(t, env, sessionID, name, slug)

	first := mintThumbnailKey(t, env, sessionID, eventID)
	patchEventMedia(t, env, sessionID, eventID, name, slug, map[string]any{"thumbnail_key": first})

	sharedStorage.failDeletes(errors.New("bucket is on fire"))

	second := mintThumbnailKey(t, env, sessionID, eventID)
	media := patchEventMediaRead(t, env, sessionID, eventID, name, slug, map[string]any{"thumbnail_key": second})
	assertKey(t, "thumbnail_key", media.ThumbnailKey, second)
	// The attempt happened; only its failure was swallowed.
	assertDeleted(t, []string{first})
}
