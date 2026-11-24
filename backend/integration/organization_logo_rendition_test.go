package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"net/http"
	"os"
	"strings"
	"testing"
)

// The Logo email rendition (#877, spec #870): every Organization's Logo gets a
// PNG fitted within 96px square with its transparency kept, produced on the
// server after the save, on the Event renditions' terms (#876). Best-effort: a
// failure leaves the rendition null and never fails the save. The rendition
// fill renders whatever Logo rendition is null.
//
// A Logo reaches the bucket as a browser's presigned upload would put it,
// through storeObject on the shared mock; the rendition key is read from the
// row, since no API response carries it.

var logoInk = color.RGBA{R: 30, G: 30, B: 220, A: 255}

// mintLogoKeyAs issues a Logo upload URL for contentType.
func mintLogoKeyAs(t *testing.T, env *testEnv, sessionID, contentType string) string {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/organization/logo-upload-url", map[string]string{
		"content_type": contentType,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logo upload url status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var upload struct {
		ObjectKey string `json:"object_key"`
	}
	if err := json.Unmarshal(body.Data, &upload); err != nil {
		t.Fatalf("decode logo upload: %v", err)
	}
	return upload.ObjectKey
}

// uploadLogo mints a Logo key and places img under it as PNG.
func uploadLogo(t *testing.T, env *testEnv, sessionID string, img image.Image) string {
	t.Helper()
	key := mintLogoKeyAs(t, env, sessionID, "image/png")
	sharedStorage.storeObject(key, "image/png", pngBytes(t, img))
	return key
}

// saveLogo saves the Organization with logoKey ("" clears it), asserting the
// save answered 200.
func saveLogo(t *testing.T, env *testEnv, sessionID, logoKey string) {
	t.Helper()
	resp, body := env.patch(t, "/api/v1/staff/organization", map[string]any{
		"name":           "Test Org",
		"logo_image_key": logoKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save logo status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

func organizationIDBySlug(t *testing.T, slug string) string {
	t.Helper()
	var id string
	if err := sharedDB.QueryRowContext(context.Background(),
		`SELECT id FROM organizations WHERE slug = $1`, slug).Scan(&id); err != nil {
		t.Fatalf("read organization id: %v", err)
	}
	return id
}

func readLogoRenditionKey(t *testing.T, organizationID string) string {
	t.Helper()
	var key sql.NullString
	if err := sharedDB.QueryRowContext(context.Background(),
		`SELECT email_logo_key FROM organizations WHERE id = $1`, organizationID).Scan(&key); err != nil {
		t.Fatalf("read logo rendition key: %v", err)
	}
	return key.String
}

// logoRenditionImage reads the Logo rendition out of the bucket, asserting it
// is a PNG stored as one under its own prefix for the Organization.
func logoRenditionImage(t *testing.T, organizationID, key string) image.Image {
	t.Helper()
	if !strings.HasPrefix(key, "email-logo/"+organizationID+"/") || !strings.HasSuffix(key, ".png") {
		t.Fatalf("logo rendition key = %q, want a .png under email-logo/ for the Organization", key)
	}
	obj, ok := sharedStorage.object(key)
	if !ok {
		t.Fatalf("logo rendition %q is not in the bucket", key)
	}
	if obj.contentType != "image/png" {
		t.Fatalf("logo rendition %q stored as %q, want image/png", key, obj.contentType)
	}
	img, format, err := image.Decode(bytes.NewReader(obj.body))
	if err != nil {
		t.Fatalf("decode logo rendition %q: %v", key, err)
	}
	if format != "png" {
		t.Fatalf("logo rendition %q is %s, want png", key, format)
	}
	return img
}

// transparentLogo is a Logo drawn on nothing: a transparent field with an
// opaque square of logoInk in its middle third.
func transparentLogo(side int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	for x := side / 3; x < 2*side/3; x++ {
		for y := side / 3; y < 2*side/3; y++ {
			img.Set(x, y, logoInk)
		}
	}
	return img
}

func assertAlpha(t *testing.T, img image.Image, x, y int, want uint32) {
	t.Helper()
	if _, _, _, a := img.At(x, y).RGBA(); a != want {
		t.Fatalf("alpha at (%d,%d) = %d, want %d", x, y, a, want)
	}
}

func TestSettingALogoProducesItsRenditionKeepingTransparency(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	orgID := organizationIDBySlug(t, "test-org")

	saveLogo(t, env, sessionID, uploadLogo(t, env, sessionID, transparentLogo(480)))

	img := logoRenditionImage(t, orgID, readLogoRenditionKey(t, orgID))
	assertSize(t, img, 96, 96)
	assertAlpha(t, img, 4, 4, 0)
	assertAlpha(t, img, 48, 48, 0xffff)
	assertColourNear(t, img, 48, 48, logoInk)
	assertDeleted(t, nil)
}

func TestReplacingTheLogoReplacesItsRenditionAndDeletesTheOldOne(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	orgID := organizationIDBySlug(t, "test-org")
	first := uploadLogo(t, env, sessionID, solid(300, 300, bandRed))
	saveLogo(t, env, sessionID, first)
	replaced := readLogoRenditionKey(t, orgID)

	saveLogo(t, env, sessionID, uploadLogo(t, env, sessionID, threeBands(900, 300)))

	key := readLogoRenditionKey(t, orgID)
	if key == "" || key == replaced {
		t.Fatalf("logo rendition = %q after replacing the Logo, was %q", key, replaced)
	}
	img := logoRenditionImage(t, orgID, key)
	assertSize(t, img, 96, 32)
	assertColourNear(t, img, 8, 16, bandRed)
	assertColourNear(t, img, 48, 16, bandGreen)
	assertColourNear(t, img, 88, 16, bandBlue)
	assertDeleted(t, []string{replaced})
}

func TestSavingTheSameLogoAgainLeavesItsRenditionAlone(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	orgID := organizationIDBySlug(t, "test-org")
	logo := uploadLogo(t, env, sessionID, solid(300, 300, bandRed))
	saveLogo(t, env, sessionID, logo)
	rendered := readLogoRenditionKey(t, orgID)

	saveLogo(t, env, sessionID, logo)

	if got := readLogoRenditionKey(t, orgID); got != rendered {
		t.Fatalf("logo rendition = %q after saving the same Logo, was %q", got, rendered)
	}
	assertDeleted(t, nil)
}

func TestClearingTheLogoClearsItsRendition(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	orgID := organizationIDBySlug(t, "test-org")
	saveLogo(t, env, sessionID, uploadLogo(t, env, sessionID, solid(300, 300, bandRed)))
	rendered := readLogoRenditionKey(t, orgID)
	if rendered == "" {
		t.Fatal("no logo rendition after setting the Logo")
	}

	saveLogo(t, env, sessionID, "")

	if got := readLogoRenditionKey(t, orgID); got != "" {
		t.Fatalf("logo rendition = %q after clearing the Logo, want none", got)
	}
	assertDeleted(t, []string{rendered})
}

func TestAWebPLogoRendersToPNG(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	orgID := organizationIDBySlug(t, "test-org")
	webp, err := os.ReadFile("../internal/platform/storage/testdata/three-bands.webp")
	if err != nil {
		t.Fatalf("read webp fixture: %v", err)
	}
	logo := mintLogoKeyAs(t, env, sessionID, "image/webp")
	if !strings.HasSuffix(logo, ".webp") {
		t.Fatalf("logo key = %q, want a .webp", logo)
	}
	sharedStorage.storeObject(logo, "image/webp", webp)

	saveLogo(t, env, sessionID, logo)

	img := logoRenditionImage(t, orgID, readLogoRenditionKey(t, orgID))
	assertSize(t, img, 96, 32)
	assertColourNear(t, img, 8, 16, bandRed)
	assertColourNear(t, img, 48, 16, bandGreen)
}

func TestALogoRenditionFailureLeavesTheSaveSuccessfulAndTheRenditionNull(t *testing.T) {
	cases := map[string]func(t *testing.T, key string){
		// The upload never reached the bucket.
		"the Logo is missing": func(t *testing.T, key string) {},
		// The bytes are no image at all.
		"the Logo is no image": func(t *testing.T, key string) {
			sharedStorage.storeObject(key, "image/png", []byte("not an image"))
		},
	}
	for name, place := range cases {
		t.Run(name, func(t *testing.T) {
			env := setupTest(t)
			sessionID := orgAdminSession(t, env)
			orgID := organizationIDBySlug(t, "test-org")
			logo := mintLogoKeyAs(t, env, sessionID, "image/png")
			place(t, logo)

			resp, body := env.patch(t, "/api/v1/staff/organization", map[string]any{
				"name":           "Test Org",
				"logo_image_key": logo,
			}, authHeader(sessionID))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("save status=%d error=%+v, want the save to succeed", resp.StatusCode, body.Error)
			}
			var saved struct {
				LogoURL *string `json:"logo_url"`
			}
			if err := json.Unmarshal(body.Data, &saved); err != nil {
				t.Fatalf("decode organization: %v", err)
			}
			if saved.LogoURL == nil || !strings.Contains(*saved.LogoURL, logo) {
				t.Fatalf("logo_url = %v, want the saved Logo %q", saved.LogoURL, logo)
			}

			if got := readLogoRenditionKey(t, orgID); got != "" {
				t.Fatalf("logo rendition = %q after a failed render, want none", got)
			}
		})
	}
}

// setLogoUnrendered writes a Logo straight to the row, as every Organization
// saved before Logo renditions existed has it: set, and never rendered.
func setLogoUnrendered(t *testing.T, organizationID, logoKey string) {
	t.Helper()
	if _, err := sharedDB.ExecContext(context.Background(), `
		UPDATE organizations SET logo_image_key = $2, email_logo_key = NULL WHERE id = $1
	`, organizationID, nullKey(logoKey)); err != nil {
		t.Fatalf("set logo: %v", err)
	}
}

func TestTheRenditionFillRendersMissingLogosAndASecondRunRendersNothing(t *testing.T) {
	env := setupTest(t)

	// Saved before Logo renditions existed.
	oldSession := orgAdminSession(t, env)
	old := organizationIDBySlug(t, "test-org")
	setLogoUnrendered(t, old, uploadLogo(t, env, oldSession, transparentLogo(300)))

	// Saved since: already rendered, and must be left exactly as it is.
	currentSession := verifyOTP(t, env, "current@example.com")
	createOrganization(t, env, currentSession, "Current Org", "current-org")
	current := organizationIDBySlug(t, "current-org")
	resp, body := env.patch(t, "/api/v1/staff/organization", map[string]any{
		"name":           "Current Org",
		"logo_image_key": uploadLogo(t, env, currentSession, solid(200, 200, bandGreen)),
	}, authHeader(currentSession))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save current logo status=%d error=%+v", resp.StatusCode, body.Error)
	}
	currentKey := readLogoRenditionKey(t, current)
	if currentKey == "" {
		t.Fatal("the saved Logo was not rendered")
	}

	// No Logo at all: nothing to render.
	bareSession := verifyOTP(t, env, "bare@example.com")
	createOrganization(t, env, bareSession, "Bare Org", "bare-org")
	bare := organizationIDBySlug(t, "bare-org")

	// Its Logo never reached the bucket, so it fails every run.
	lostSession := verifyOTP(t, env, "lost@example.com")
	createOrganization(t, env, lostSession, "Lost Org", "lost-org")
	lost := organizationIDBySlug(t, "lost-org")
	lostLogo := mintLogoKeyAs(t, env, lostSession, "image/png")
	setLogoUnrendered(t, lost, lostLogo)

	first := runRenditionFill(t, env)
	if first != (renditionFill{Rendered: 1, Failed: 1, Remaining: 1, Complete: true}) {
		t.Fatalf("first fill = %+v, want 1 rendered, 1 failed, 1 remaining, complete", first)
	}
	oldKey := readLogoRenditionKey(t, old)
	img := logoRenditionImage(t, old, oldKey)
	assertSize(t, img, 96, 96)
	assertAlpha(t, img, 4, 4, 0)
	assertColourNear(t, img, 48, 48, logoInk)
	if got := readLogoRenditionKey(t, current); got != currentKey {
		t.Fatalf("the fill touched a Logo already rendered: %q, was %q", got, currentKey)
	}
	if got := readLogoRenditionKey(t, bare); got != "" {
		t.Fatalf("an Organization with no Logo got a rendition: %q", got)
	}
	if got := readLogoRenditionKey(t, lost); got != "" {
		t.Fatalf("a missing Logo got a rendition: %q", got)
	}
	assertDeleted(t, nil)

	// Rerun: nothing new is rendered, and the failure is simply tried again.
	if second := runRenditionFill(t, env); second != (renditionFill{Failed: 1, Remaining: 1, Complete: true}) {
		t.Fatalf("second fill = %+v, want nothing rendered and the one failure again", second)
	}
	if got := readLogoRenditionKey(t, old); got != oldKey {
		t.Fatalf("a second fill re-rendered: %q, was %q", got, oldKey)
	}

	// Once the Logo arrives, the next run fills it, and the one after has
	// nothing to do.
	sharedStorage.storeObject(lostLogo, "image/png", pngBytes(t, solid(120, 60, bandRed)))
	if third := runRenditionFill(t, env); third != (renditionFill{Rendered: 1, Complete: true}) {
		t.Fatalf("third fill = %+v, want the late Logo's rendition", third)
	}
	assertSize(t, logoRenditionImage(t, lost, readLogoRenditionKey(t, lost)), 96, 48)
	if fourth := runRenditionFill(t, env); fourth != (renditionFill{Complete: true}) {
		t.Fatalf("fourth fill = %+v, want nothing to do", fourth)
	}
}

func TestTheRenditionFillCoversEventsAndLogosInOneRun(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	orgID := organizationIDBySlug(t, "test-org")
	setLogoUnrendered(t, orgID, uploadLogo(t, env, sessionID, solid(200, 200, bandRed)))
	eventID := createDraftEvent(t, env, sessionID, "Both Kinds", "both-kinds")
	setOriginalsUnrendered(t, eventID, nullKey(uploadCover(t, env, sessionID, eventID, threeBands(900, 300))), sql.NullString{})

	if got := runRenditionFill(t, env); got != (renditionFill{Rendered: 3, Complete: true}) {
		t.Fatalf("fill = %+v, want the Event's two renditions and the Logo's", got)
	}
	if readLogoRenditionKey(t, orgID) == "" {
		t.Fatal("the fill left the Logo unrendered")
	}
	if keys := readRenditionKeys(t, eventID); keys.wide == "" || keys.square == "" {
		t.Fatalf("the fill left the Event unrendered: %+v", keys)
	}
}
