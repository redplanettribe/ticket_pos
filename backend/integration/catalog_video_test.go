package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// mintCoverImageKey issues a cover upload URL and returns the minted object key.
func mintCoverImageKey(t *testing.T, env *testEnv, sessionID, eventID string) string {
	t.Helper()
	_, body := env.post(t, "/api/v1/staff/events/"+eventID+"/cover-upload-url", map[string]string{
		"content_type": "image/png",
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("cover upload url error=%+v", body.Error)
	}
	var upload struct {
		ObjectKey string `json:"object_key"`
	}
	if err := json.Unmarshal(body.Data, &upload); err != nil {
		t.Fatalf("decode cover upload: %v", err)
	}
	return upload.ObjectKey
}

// mintCoverVideoKey issues a video upload URL and returns the minted object key.
func mintCoverVideoKey(t *testing.T, env *testEnv, sessionID, eventID string) string {
	t.Helper()
	_, body := env.post(t, "/api/v1/staff/events/"+eventID+"/video-upload-url", map[string]string{
		"content_type": "video/mp4",
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("video upload url error=%+v", body.Error)
	}
	var upload struct {
		ObjectKey string `json:"object_key"`
	}
	if err := json.Unmarshal(body.Data, &upload); err != nil {
		t.Fatalf("decode video upload: %v", err)
	}
	return upload.ObjectKey
}

func TestCatalogVideoUploadURLAndAttach(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Video Fest", "video-fest")
	imageKey := mintCoverImageKey(t, env, sessionID, eventID)

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/video-upload-url", map[string]string{
		"content_type": "video/mp4",
		"file_name":    "teaser.mp4",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("video upload url status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var result struct {
		UploadURL string `json:"upload_url"`
		ObjectKey string `json:"object_key"`
		PublicURL string `json:"public_url"`
	}
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("decode video upload: %v", err)
	}
	if result.UploadURL == "" {
		t.Fatal("expected upload_url")
	}
	if !strings.HasPrefix(result.ObjectKey, "videos/") {
		t.Fatalf("object_key=%q", result.ObjectKey)
	}
	if !strings.HasSuffix(result.ObjectKey, ".mp4") {
		t.Fatalf("object_key should end in .mp4: %q", result.ObjectKey)
	}
	if !strings.Contains(result.ObjectKey, eventID) {
		t.Fatalf("object_key should include event id: %q", result.ObjectKey)
	}
	if result.PublicURL == "" || !strings.Contains(result.PublicURL, result.ObjectKey) {
		t.Fatalf("public_url=%q object_key=%q", result.PublicURL, result.ObjectKey)
	}

	// Attach via the event PATCH. The Cover Image is the video's poster, so it
	// goes on in the same request (see the poster invariant tests below).
	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Video Fest",
		"slug":            "video-fest",
		"cover_image_key": imageKey,
		"cover_video_key": result.ObjectKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch video key status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var updated struct {
		CoverVideoKey *string `json:"cover_video_key"`
		CoverVideoURL *string `json:"cover_video_url"`
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode updated event: %v", err)
	}
	if updated.CoverVideoKey == nil || *updated.CoverVideoKey != result.ObjectKey {
		t.Fatalf("cover_video_key=%v", updated.CoverVideoKey)
	}
	if updated.CoverVideoURL == nil || *updated.CoverVideoURL != result.PublicURL {
		t.Fatalf("cover_video_url=%v want %q", updated.CoverVideoURL, result.PublicURL)
	}

	// Omitting the field preserves the attached video.
	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name": "Video Fest",
		"slug": "video-fest",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch without video key status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode preserved event: %v", err)
	}
	if updated.CoverVideoKey == nil || *updated.CoverVideoKey != result.ObjectKey {
		t.Fatalf("expected preserved cover_video_key, got %v", updated.CoverVideoKey)
	}

	// Reading the event back exposes key and URL.
	resp, body = env.get(t, "/api/v1/staff/events/"+eventID, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode read event: %v", err)
	}
	if updated.CoverVideoKey == nil || *updated.CoverVideoKey != result.ObjectKey {
		t.Fatalf("read cover_video_key=%v", updated.CoverVideoKey)
	}
	if updated.CoverVideoURL == nil || *updated.CoverVideoURL != result.PublicURL {
		t.Fatalf("read cover_video_url=%v", updated.CoverVideoURL)
	}

	// Empty string clears it.
	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Video Fest",
		"slug":            "video-fest",
		"cover_video_key": "",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear video status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode cleared event: %v", err)
	}
	if updated.CoverVideoKey != nil {
		t.Fatalf("expected cleared cover_video_key, got %v", updated.CoverVideoKey)
	}
	if updated.CoverVideoURL != nil {
		t.Fatalf("expected cleared cover_video_url, got %v", updated.CoverVideoURL)
	}
}

func TestCatalogVideoUploadURLRejectsNonMP4(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Bad Video", "bad-video")

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/video-upload-url", map[string]string{
		"content_type": "video/webm",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("expected VALIDATION_FAILED, got %+v", body.Error)
	}
}

func TestCatalogVideoUploadURLForbiddenForNonOrgAdmin(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Guarded Video", "guarded-video")

	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "staff@example.com",
		"role":  "event_staff",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}

	staffSessionID := verifyOTP(t, env, "staff@example.com")
	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/video-upload-url", map[string]string{
		"content_type": "video/mp4",
	}, authHeader(staffSessionID))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d error=%+v", resp.StatusCode, body.Error)
	}
}

func TestCatalogVideoAttachRejectsForeignAndImageKeys(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Own Video", "own-video")
	otherEventID := createDraftEvent(t, env, sessionID, "Other Video", "other-video")

	// A video key minted for another Event must not attach here.
	_, body := env.post(t, "/api/v1/staff/events/"+otherEventID+"/video-upload-url", map[string]string{
		"content_type": "video/mp4",
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("other video upload url error=%+v", body.Error)
	}
	var other struct {
		ObjectKey string `json:"object_key"`
	}
	if err := json.Unmarshal(body.Data, &other); err != nil {
		t.Fatalf("decode other upload: %v", err)
	}

	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Own Video",
		"slug":            "own-video",
		"cover_video_key": other.ObjectKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for foreign video key, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "INVALID_COVER_VIDEO_KEY" {
		t.Fatalf("expected INVALID_COVER_VIDEO_KEY, got %+v", body.Error)
	}

	// A covers/-prefixed key for this very Event must not validate as a video key.
	_, body = env.post(t, "/api/v1/staff/events/"+eventID+"/cover-upload-url", map[string]string{
		"content_type": "image/png",
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("cover upload url error=%+v", body.Error)
	}
	var cover struct {
		ObjectKey string `json:"object_key"`
	}
	if err := json.Unmarshal(body.Data, &cover); err != nil {
		t.Fatalf("decode cover upload: %v", err)
	}

	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Own Video",
		"slug":            "own-video",
		"cover_video_key": cover.ObjectKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for covers-prefixed video key, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "INVALID_COVER_VIDEO_KEY" {
		t.Fatalf("expected INVALID_COVER_VIDEO_KEY, got %+v", body.Error)
	}

	// And the mirror: a videos/-prefixed key must not validate as an image key.
	_, body = env.post(t, "/api/v1/staff/events/"+eventID+"/video-upload-url", map[string]string{
		"content_type": "video/mp4",
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("video upload url error=%+v", body.Error)
	}
	var video struct {
		ObjectKey string `json:"object_key"`
	}
	if err := json.Unmarshal(body.Data, &video); err != nil {
		t.Fatalf("decode video upload: %v", err)
	}

	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Own Video",
		"slug":            "own-video",
		"cover_image_key": video.ObjectKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for videos-prefixed image key, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "INVALID_COVER_IMAGE_KEY" {
		t.Fatalf("expected INVALID_COVER_IMAGE_KEY, got %+v", body.Error)
	}
}

func TestPublicEventExposesCoverVideoURL(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	soon := env.fixedClock.Add(10 * 24 * time.Hour)
	eventID := publishEvent(t, env, sessionID, "Public Video", "public-video", soon, true, 2500, 50)

	_, body := env.post(t, "/api/v1/staff/events/"+eventID+"/video-upload-url", map[string]string{
		"content_type": "video/mp4",
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("video upload url error=%+v", body.Error)
	}
	var upload struct {
		ObjectKey string `json:"object_key"`
		PublicURL string `json:"public_url"`
	}
	if err := json.Unmarshal(body.Data, &upload); err != nil {
		t.Fatalf("decode upload: %v", err)
	}

	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Public Video",
		"slug":            "public-video",
		"starts_at":       soon.Format(time.RFC3339),
		"timezone":        "America/New_York",
		"venue_name":      "The Hall",
		"cover_image_key": mintCoverImageKey(t, env, sessionID, eventID),
		"cover_video_key": upload.ObjectKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("attach video status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.get(t, "/api/v1/public/organizations/test-org/events/public-video", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var public struct {
		CoverVideoURL *string `json:"cover_video_url"`
		CoverVideoKey *string `json:"cover_video_key"`
	}
	if err := json.Unmarshal(body.Data, &public); err != nil {
		t.Fatalf("decode public event: %v", err)
	}
	if public.CoverVideoURL == nil || *public.CoverVideoURL != upload.PublicURL {
		t.Fatalf("public cover_video_url=%v want %q", public.CoverVideoURL, upload.PublicURL)
	}
	if public.CoverVideoKey != nil {
		t.Fatalf("public view must not expose cover_video_key, got %v", public.CoverVideoKey)
	}
}

// The poster invariant (#132): a Cover Video requires a Cover Image, evaluated
// against the final state of the PATCH.

func TestCatalogVideoAttachRequiresCoverImage(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Poster Less", "poster-less")
	videoKey := mintCoverVideoKey(t, env, sessionID, eventID)

	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Poster Less",
		"slug":            "poster-less",
		"cover_video_key": videoKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 attaching video without image, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "COVER_VIDEO_REQUIRES_COVER_IMAGE" {
		t.Fatalf("expected COVER_VIDEO_REQUIRES_COVER_IMAGE, got %+v", body.Error)
	}
}

func TestCatalogVideoAcceptsImageAndVideoInOnePatch(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Both At Once", "both-at-once")
	imageKey := mintCoverImageKey(t, env, sessionID, eventID)
	videoKey := mintCoverVideoKey(t, env, sessionID, eventID)

	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Both At Once",
		"slug":            "both-at-once",
		"cover_image_key": imageKey,
		"cover_video_key": videoKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 setting both, got %d error=%+v", resp.StatusCode, body.Error)
	}
	var updated struct {
		CoverImageKey *string `json:"cover_image_key"`
		CoverVideoKey *string `json:"cover_video_key"`
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode updated event: %v", err)
	}
	if updated.CoverImageKey == nil || *updated.CoverImageKey != imageKey {
		t.Fatalf("cover_image_key=%v", updated.CoverImageKey)
	}
	if updated.CoverVideoKey == nil || *updated.CoverVideoKey != videoKey {
		t.Fatalf("cover_video_key=%v", updated.CoverVideoKey)
	}
}

func TestCatalogVideoBlocksClearingImageWhileVideoAttached(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Poster Locked", "poster-locked")
	imageKey := mintCoverImageKey(t, env, sessionID, eventID)
	videoKey := mintCoverVideoKey(t, env, sessionID, eventID)

	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Poster Locked",
		"slug":            "poster-locked",
		"cover_image_key": imageKey,
		"cover_video_key": videoKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup attach status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// Clearing the image while the already-attached video is preserved.
	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Poster Locked",
		"slug":            "poster-locked",
		"cover_image_key": "",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 clearing image under a video, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "COVER_VIDEO_REQUIRES_COVER_IMAGE" {
		t.Fatalf("expected COVER_VIDEO_REQUIRES_COVER_IMAGE, got %+v", body.Error)
	}

	// The rejected request changed nothing.
	resp, body = env.get(t, "/api/v1/staff/events/"+eventID, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var current struct {
		CoverImageKey *string `json:"cover_image_key"`
		CoverVideoKey *string `json:"cover_video_key"`
	}
	if err := json.Unmarshal(body.Data, &current); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if current.CoverImageKey == nil || *current.CoverImageKey != imageKey {
		t.Fatalf("image should be untouched, got %v", current.CoverImageKey)
	}
	if current.CoverVideoKey == nil || *current.CoverVideoKey != videoKey {
		t.Fatalf("video should be untouched, got %v", current.CoverVideoKey)
	}
}

func TestCatalogVideoBlocksClearingImageWhileSettingVideoInSamePatch(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Same Patch", "same-patch")
	imageKey := mintCoverImageKey(t, env, sessionID, eventID)
	videoKey := mintCoverVideoKey(t, env, sessionID, eventID)

	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Same Patch",
		"slug":            "same-patch",
		"cover_image_key": imageKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup image status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Same Patch",
		"slug":            "same-patch",
		"cover_image_key": "",
		"cover_video_key": videoKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 clearing image while setting video, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "COVER_VIDEO_REQUIRES_COVER_IMAGE" {
		t.Fatalf("expected COVER_VIDEO_REQUIRES_COVER_IMAGE, got %+v", body.Error)
	}
}

func TestCatalogVideoAllowsClearingVideoThenImage(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Unwind", "unwind")
	imageKey := mintCoverImageKey(t, env, sessionID, eventID)
	videoKey := mintCoverVideoKey(t, env, sessionID, eventID)

	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Unwind",
		"slug":            "unwind",
		"cover_image_key": imageKey,
		"cover_video_key": videoKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup attach status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Unwind",
		"slug":            "unwind",
		"cover_video_key": "",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear video status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Unwind",
		"slug":            "unwind",
		"cover_image_key": "",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear image status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var updated struct {
		CoverImageKey *string `json:"cover_image_key"`
		CoverVideoKey *string `json:"cover_video_key"`
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode updated event: %v", err)
	}
	if updated.CoverImageKey != nil || updated.CoverVideoKey != nil {
		t.Fatalf("expected both cleared, got image=%v video=%v", updated.CoverImageKey, updated.CoverVideoKey)
	}
}

func TestCatalogVideoAllowsClearingBothInOnePatch(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Clear Both", "clear-both")
	imageKey := mintCoverImageKey(t, env, sessionID, eventID)
	videoKey := mintCoverVideoKey(t, env, sessionID, eventID)

	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Clear Both",
		"slug":            "clear-both",
		"cover_image_key": imageKey,
		"cover_video_key": videoKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup attach status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Clear Both",
		"slug":            "clear-both",
		"cover_image_key": "",
		"cover_video_key": "",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 clearing both, got %d error=%+v", resp.StatusCode, body.Error)
	}
	var updated struct {
		CoverImageKey *string `json:"cover_image_key"`
		CoverVideoKey *string `json:"cover_video_key"`
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode updated event: %v", err)
	}
	if updated.CoverImageKey != nil || updated.CoverVideoKey != nil {
		t.Fatalf("expected both cleared, got image=%v video=%v", updated.CoverImageKey, updated.CoverVideoKey)
	}
}

func TestCatalogVideoAllowsReplacingImageWhileVideoExists(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "New Poster", "new-poster")
	imageKey := mintCoverImageKey(t, env, sessionID, eventID)
	videoKey := mintCoverVideoKey(t, env, sessionID, eventID)

	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "New Poster",
		"slug":            "new-poster",
		"cover_image_key": imageKey,
		"cover_video_key": videoKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup attach status=%d error=%+v", resp.StatusCode, body.Error)
	}

	replacementKey := mintCoverImageKey(t, env, sessionID, eventID)
	if replacementKey == imageKey {
		t.Fatalf("expected a distinct replacement key, got %q", replacementKey)
	}

	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "New Poster",
		"slug":            "new-poster",
		"cover_image_key": replacementKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 replacing the image, got %d error=%+v", resp.StatusCode, body.Error)
	}
	var updated struct {
		CoverImageKey *string `json:"cover_image_key"`
		CoverVideoKey *string `json:"cover_video_key"`
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode updated event: %v", err)
	}
	if updated.CoverImageKey == nil || *updated.CoverImageKey != replacementKey {
		t.Fatalf("cover_image_key=%v want %q", updated.CoverImageKey, replacementKey)
	}
	if updated.CoverVideoKey == nil || *updated.CoverVideoKey != videoKey {
		t.Fatalf("video should survive an image replacement, got %v", updated.CoverVideoKey)
	}
}

func TestCatalogImagelessEventWithoutVideoStaysValid(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "No Media", "no-media")

	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "No Media",
		"slug":            "no-media",
		"cover_image_key": "",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for an imageless event, got %d error=%+v", resp.StatusCode, body.Error)
	}
}
