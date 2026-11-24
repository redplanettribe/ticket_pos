package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"strings"
	"testing"
)

// Email renditions (#876, spec #870): every Event gets a wide rendition (the
// Cover Image as a JPEG 1200px wide) and a square one (a 128px JPEG from the
// Thumbnail, or from the Cover Image cropped to its centre), produced on the
// server after the save commits. Best-effort: a failure leaves the rendition
// null and never fails the save. The rendition fill renders whatever is null.
//
// An original reaches the bucket as a browser's presigned upload would put it,
// through storeObject on the shared mock, and the renditions are read back out
// of the same mock. The rendition keys are read from the row: they are not part
// of any API response, since only mail uses them.

var (
	bandRed    = color.RGBA{R: 220, G: 30, B: 30, A: 255}
	bandGreen  = color.RGBA{R: 30, G: 200, B: 30, A: 255}
	bandBlue   = color.RGBA{R: 30, G: 30, B: 220, A: 255}
	thumbGold  = color.RGBA{R: 240, G: 200, B: 20, A: 255}
	coverOlive = color.RGBA{R: 120, G: 120, B: 20, A: 255}
)

// threeBands paints a w x h image red, green and blue from left to right, so
// a square crop shows which part it kept: the centre one is all green.
func threeBands(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		c := bandRed
		switch {
		case x >= 2*w/3:
			c = bandBlue
		case x >= w/3:
			c = bandGreen
		}
		for y := 0; y < h; y++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func solid(w, h int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func pngBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func jpegBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// mintCoverImageKeyAs issues a Cover Image upload URL for contentType.
func mintCoverImageKeyAs(t *testing.T, env *testEnv, sessionID, eventID, contentType string) string {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/cover-upload-url", map[string]string{
		"content_type": contentType,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cover upload url status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var upload struct {
		ObjectKey string `json:"object_key"`
	}
	if err := json.Unmarshal(body.Data, &upload); err != nil {
		t.Fatalf("decode cover upload: %v", err)
	}
	return upload.ObjectKey
}

// uploadCover mints a Cover Image key and places img under it as PNG.
func uploadCover(t *testing.T, env *testEnv, sessionID, eventID string, img image.Image) string {
	t.Helper()
	key := mintCoverImageKeyAs(t, env, sessionID, eventID, "image/png")
	sharedStorage.storeObject(key, "image/png", pngBytes(t, img))
	return key
}

// uploadThumbnail mints a Thumbnail key and places img under it as JPEG.
func uploadThumbnail(t *testing.T, env *testEnv, sessionID, eventID string, img image.Image) string {
	t.Helper()
	key := mintThumbnailKey(t, env, sessionID, eventID)
	sharedStorage.storeObject(key, "image/jpeg", jpegBytes(t, img))
	return key
}

type renditionKeys struct {
	wide, square string
}

func readRenditionKeys(t *testing.T, eventID string) renditionKeys {
	t.Helper()
	var wide, square sql.NullString
	if err := sharedDB.QueryRowContext(context.Background(),
		`SELECT email_wide_key, email_square_key FROM events WHERE id = $1`, eventID,
	).Scan(&wide, &square); err != nil {
		t.Fatalf("read rendition keys: %v", err)
	}
	return renditionKeys{wide: wide.String, square: square.String}
}

// renditionImage reads a rendition out of the bucket, asserting it is a JPEG
// stored as one.
func renditionImage(t *testing.T, key string) image.Image {
	t.Helper()
	obj, ok := sharedStorage.object(key)
	if !ok {
		t.Fatalf("rendition %q is not in the bucket", key)
	}
	if obj.contentType != "image/jpeg" {
		t.Fatalf("rendition %q stored as %q, want image/jpeg", key, obj.contentType)
	}
	img, format, err := image.Decode(bytes.NewReader(obj.body))
	if err != nil {
		t.Fatalf("decode rendition %q: %v", key, err)
	}
	if format != "jpeg" {
		t.Fatalf("rendition %q is %s, want jpeg", key, format)
	}
	return img
}

// assertColourNear asserts the pixel at (x, y) is within a JPEG's tolerance of
// want.
func assertColourNear(t *testing.T, img image.Image, x, y int, want color.RGBA) {
	t.Helper()
	r, g, b, _ := img.At(x, y).RGBA()
	got := [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
	exp := [3]int{int(want.R), int(want.G), int(want.B)}
	for i := range got {
		if d := got[i] - exp[i]; d > 24 || d < -24 {
			t.Fatalf("pixel (%d,%d) = %v, want about %v", x, y, got, exp)
		}
	}
}

func assertSize(t *testing.T, img image.Image, w, h int) {
	t.Helper()
	if got := img.Bounds().Size(); got.X != w || got.Y != h {
		t.Fatalf("rendition size = %v, want %dx%d", got, w, h)
	}
}

// assertSquareIs asserts a square rendition is 128px and filled with want.
func assertSquareIs(t *testing.T, key string, want color.RGBA) {
	t.Helper()
	img := renditionImage(t, key)
	assertSize(t, img, 128, 128)
	for _, p := range []image.Point{{8, 8}, {64, 64}, {120, 120}, {8, 120}, {120, 8}} {
		assertColourNear(t, img, p.X, p.Y, want)
	}
}

func assertRenditionPrefix(t *testing.T, slot, key, prefix, eventID string) {
	t.Helper()
	if !strings.HasPrefix(key, prefix) || !strings.Contains(key, eventID) || !strings.HasSuffix(key, ".jpg") {
		t.Fatalf("%s rendition key = %q, want a .jpg under %s for the Event", slot, key, prefix)
	}
}

func TestSettingACoverImageProducesBothRenditions(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Mail Fest", "mail-fest")
	cover := uploadCover(t, env, sessionID, eventID, threeBands(2400, 800))

	patchEventMedia(t, env, sessionID, eventID, "Mail Fest", "mail-fest", map[string]any{
		"cover_image_key": cover,
	})

	keys := readRenditionKeys(t, eventID)
	assertRenditionPrefix(t, "wide", keys.wide, "email-wide/", eventID)
	assertRenditionPrefix(t, "square", keys.square, "email-square/", eventID)

	wide := renditionImage(t, keys.wide)
	assertSize(t, wide, 1200, 400)
	assertColourNear(t, wide, 100, 200, bandRed)
	assertColourNear(t, wide, 1100, 200, bandBlue)

	// No Thumbnail: the square is the Cover Image cropped to its centre.
	assertSquareIs(t, keys.square, bandGreen)

	// A save that leaves the Cover Image alone leaves its renditions alone.
	patchEventMedia(t, env, sessionID, eventID, "Mail Fest", "mail-fest", map[string]any{
		"cover_image_key": cover,
	})
	if again := readRenditionKeys(t, eventID); again != keys {
		t.Fatalf("an unchanged Cover Image re-rendered: %+v, then %+v", keys, again)
	}
	assertDeleted(t, nil)
}

func TestReplacingTheCoverImageReplacesItsRenditionsAndDeletesTheOldOnes(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Swap Fest", "swap-fest")
	first := uploadCover(t, env, sessionID, eventID, threeBands(900, 300))
	patchEventMedia(t, env, sessionID, eventID, "Swap Fest", "swap-fest", map[string]any{
		"cover_image_key": first,
	})
	before := readRenditionKeys(t, eventID)

	second := uploadCover(t, env, sessionID, eventID, solid(600, 300, coverOlive))
	patchEventMedia(t, env, sessionID, eventID, "Swap Fest", "swap-fest", map[string]any{
		"cover_image_key": second,
	})

	after := readRenditionKeys(t, eventID)
	if after.wide == "" || after.wide == before.wide {
		t.Fatalf("wide rendition = %q after replacing, was %q", after.wide, before.wide)
	}
	if after.square == "" || after.square == before.square {
		t.Fatalf("square rendition = %q after replacing, was %q", after.square, before.square)
	}
	wide := renditionImage(t, after.wide)
	assertSize(t, wide, 600, 300)
	assertColourNear(t, wide, 300, 150, coverOlive)
	assertSquareIs(t, after.square, coverOlive)

	// The replaced original, then the renditions made from it.
	assertDeleted(t, []string{first, before.wide, before.square})
}

func TestClearingTheCoverImageClearsItsRenditions(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Bare Fest", "bare-fest")
	cover := uploadCover(t, env, sessionID, eventID, threeBands(900, 300))
	patchEventMedia(t, env, sessionID, eventID, "Bare Fest", "bare-fest", map[string]any{
		"cover_image_key": cover,
	})
	before := readRenditionKeys(t, eventID)

	patchEventMedia(t, env, sessionID, eventID, "Bare Fest", "bare-fest", map[string]any{
		"cover_image_key": "",
	})

	if after := readRenditionKeys(t, eventID); after != (renditionKeys{}) {
		t.Fatalf("renditions after clearing the Cover Image = %+v, want none", after)
	}
	assertDeleted(t, []string{cover, before.wide, before.square})
}

func TestAddingAThumbnailReplacesTheSquareCroppedFromTheCover(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Square Mail", "square-mail")
	cover := uploadCover(t, env, sessionID, eventID, threeBands(900, 300))
	patchEventMedia(t, env, sessionID, eventID, "Square Mail", "square-mail", map[string]any{
		"cover_image_key": cover,
	})
	fromCover := readRenditionKeys(t, eventID)
	assertSquareIs(t, fromCover.square, bandGreen)

	thumbnail := uploadThumbnail(t, env, sessionID, eventID, solid(400, 400, thumbGold))
	patchEventMedia(t, env, sessionID, eventID, "Square Mail", "square-mail", map[string]any{
		"thumbnail_key": thumbnail,
	})

	fromThumbnail := readRenditionKeys(t, eventID)
	if fromThumbnail.wide != fromCover.wide {
		t.Fatalf("the wide rendition changed with the Thumbnail: %q, was %q", fromThumbnail.wide, fromCover.wide)
	}
	if fromThumbnail.square == fromCover.square {
		t.Fatal("the square rendition was not replaced when the Thumbnail was added")
	}
	assertSquareIs(t, fromThumbnail.square, thumbGold)
	assertDeleted(t, []string{fromCover.square})

	// Clearing the Thumbnail falls back to the Cover Image, as the Storefront
	// does, so the square is made from its centre again.
	patchEventMedia(t, env, sessionID, eventID, "Square Mail", "square-mail", map[string]any{
		"thumbnail_key": "",
	})
	fallback := readRenditionKeys(t, eventID)
	if fallback.square == "" || fallback.square == fromThumbnail.square {
		t.Fatalf("square rendition = %q after clearing the Thumbnail", fallback.square)
	}
	assertSquareIs(t, fallback.square, bandGreen)
	assertDeleted(t, []string{fromCover.square, thumbnail, fromThumbnail.square})
}

func TestAThumbnailAloneMakesOnlyTheSquareAndClearingItClearsThat(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Only Square", "only-square")
	thumbnail := uploadThumbnail(t, env, sessionID, eventID, solid(300, 300, thumbGold))

	patchEventMedia(t, env, sessionID, eventID, "Only Square", "only-square", map[string]any{
		"thumbnail_key": thumbnail,
	})
	keys := readRenditionKeys(t, eventID)
	if keys.wide != "" {
		t.Fatalf("wide rendition = %q with no Cover Image", keys.wide)
	}
	assertSquareIs(t, keys.square, thumbGold)

	// Replacing the Thumbnail replaces the square and deletes the old one.
	replacement := uploadThumbnail(t, env, sessionID, eventID, solid(300, 300, bandBlue))
	patchEventMedia(t, env, sessionID, eventID, "Only Square", "only-square", map[string]any{
		"thumbnail_key": replacement,
	})
	replaced := readRenditionKeys(t, eventID)
	if replaced.square == "" || replaced.square == keys.square {
		t.Fatalf("square rendition = %q after replacing the Thumbnail, was %q", replaced.square, keys.square)
	}
	assertSquareIs(t, replaced.square, bandBlue)
	assertDeleted(t, []string{thumbnail, keys.square})

	patchEventMedia(t, env, sessionID, eventID, "Only Square", "only-square", map[string]any{
		"thumbnail_key": "",
	})
	if after := readRenditionKeys(t, eventID); after != (renditionKeys{}) {
		t.Fatalf("renditions after clearing the only image = %+v, want none", after)
	}
	assertDeleted(t, []string{thumbnail, keys.square, replacement, replaced.square})
}

func TestAWebPCoverImageRendersToJPEG(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "WebP Fest", "webp-fest")
	webp, err := os.ReadFile("../internal/platform/storage/testdata/three-bands.webp")
	if err != nil {
		t.Fatalf("read webp fixture: %v", err)
	}
	cover := mintCoverImageKeyAs(t, env, sessionID, eventID, "image/webp")
	if !strings.HasSuffix(cover, ".webp") {
		t.Fatalf("cover key = %q, want a .webp", cover)
	}
	sharedStorage.storeObject(cover, "image/webp", webp)

	patchEventMedia(t, env, sessionID, eventID, "WebP Fest", "webp-fest", map[string]any{
		"cover_image_key": cover,
	})

	keys := readRenditionKeys(t, eventID)
	wide := renditionImage(t, keys.wide)
	assertSize(t, wide, 300, 100)
	assertColourNear(t, wide, 50, 50, bandRed)
	assertSquareIs(t, keys.square, bandGreen)
}

func TestARenditionFailureLeavesTheSaveSuccessfulAndTheRenditionNull(t *testing.T) {
	cases := map[string]func(t *testing.T, key string){
		// The upload never reached the bucket.
		"the original is missing": func(t *testing.T, key string) {},
		// The bytes are no image at all.
		"the original is no image": func(t *testing.T, key string) {
			sharedStorage.storeObject(key, "image/png", []byte("not an image"))
		},
	}
	for name, place := range cases {
		t.Run(name, func(t *testing.T) {
			env := setupTest(t)
			sessionID := orgAdminSession(t, env)
			eventID := createDraftEvent(t, env, sessionID, "Broken Mail", "broken-mail")
			cover := mintCoverImageKeyAs(t, env, sessionID, eventID, "image/png")
			place(t, cover)

			// patchEventMediaRead asserts the 200.
			media := patchEventMediaRead(t, env, sessionID, eventID, "Broken Mail", "broken-mail", map[string]any{
				"cover_image_key": cover,
			})
			assertKey(t, "cover_image_key", media.CoverImageKey, cover)
			assertKey(t, "read cover_image_key", readEventMedia(t, env, sessionID, eventID).CoverImageKey, cover)

			if keys := readRenditionKeys(t, eventID); keys != (renditionKeys{}) {
				t.Fatalf("renditions after a failed render = %+v, want none", keys)
			}
		})
	}
}

type renditionFill struct {
	Rendered  int  `json:"rendered"`
	Failed    int  `json:"failed"`
	Remaining int  `json:"remaining"`
	Complete  bool `json:"complete"`
}

func runRenditionFill(t *testing.T, env *testEnv) renditionFill {
	t.Helper()
	resp, body := env.post(t, "/api/v1/internal/email-renditions/fill", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fill status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var result renditionFill
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("decode fill: %v", err)
	}
	return result
}

// setOriginalsUnrendered writes an Event's originals straight to the row, as
// every Event saved before renditions existed has them: set, and never
// rendered.
func setOriginalsUnrendered(t *testing.T, eventID string, cover, thumbnail sql.NullString) {
	t.Helper()
	if _, err := sharedDB.ExecContext(context.Background(), `
		UPDATE events SET cover_image_key = $2, thumbnail_key = $3,
			email_wide_key = NULL, email_square_key = NULL
		WHERE id = $1
	`, eventID, cover, thumbnail); err != nil {
		t.Fatalf("set originals: %v", err)
	}
}

func nullKey(key string) sql.NullString {
	return sql.NullString{String: key, Valid: key != ""}
}

func TestTheRenditionFillRendersOnlyWhatIsNullAndASecondRunRendersNothing(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// Saved before renditions existed: a Cover Image and no Thumbnail.
	coverOnly := createDraftEvent(t, env, sessionID, "Old Cover", "old-cover")
	setOriginalsUnrendered(t, coverOnly, nullKey(uploadCover(t, env, sessionID, coverOnly, threeBands(2400, 800))), sql.NullString{})

	// Saved before renditions existed: both, so the square is the Thumbnail's.
	both := createDraftEvent(t, env, sessionID, "Old Both", "old-both")
	setOriginalsUnrendered(t, both,
		nullKey(uploadCover(t, env, sessionID, both, solid(600, 300, coverOlive))),
		nullKey(uploadThumbnail(t, env, sessionID, both, solid(200, 200, thumbGold))))

	// Saved since: already rendered, and must be left exactly as it is.
	current := createDraftEvent(t, env, sessionID, "New Cover", "new-cover")
	patchEventMedia(t, env, sessionID, current, "New Cover", "new-cover", map[string]any{
		"cover_image_key": uploadCover(t, env, sessionID, current, threeBands(900, 300)),
	})
	currentKeys := readRenditionKeys(t, current)

	// No images at all: nothing to render.
	bare := createDraftEvent(t, env, sessionID, "No Images", "no-images")

	// Its original never reached the bucket, so it fails every run.
	missing := createDraftEvent(t, env, sessionID, "Lost Cover", "lost-cover")
	lostCover := mintCoverImageKeyAs(t, env, sessionID, missing, "image/png")
	setOriginalsUnrendered(t, missing, nullKey(lostCover), sql.NullString{})

	first := runRenditionFill(t, env)
	if first != (renditionFill{Rendered: 4, Failed: 2, Remaining: 1, Complete: true}) {
		t.Fatalf("first fill = %+v, want 4 rendered, 2 failed, 1 remaining, complete", first)
	}

	coverOnlyKeys := readRenditionKeys(t, coverOnly)
	assertSize(t, renditionImage(t, coverOnlyKeys.wide), 1200, 400)
	assertSquareIs(t, coverOnlyKeys.square, bandGreen)
	bothKeys := readRenditionKeys(t, both)
	assertColourNear(t, renditionImage(t, bothKeys.wide), 300, 150, coverOlive)
	assertSquareIs(t, bothKeys.square, thumbGold)
	if got := readRenditionKeys(t, current); got != currentKeys {
		t.Fatalf("the fill touched an Event already rendered: %+v, was %+v", got, currentKeys)
	}
	if got := readRenditionKeys(t, bare); got != (renditionKeys{}) {
		t.Fatalf("an Event with no images got renditions: %+v", got)
	}
	if got := readRenditionKeys(t, missing); got != (renditionKeys{}) {
		t.Fatalf("a missing original got renditions: %+v", got)
	}
	assertDeleted(t, nil)

	// Rerun: nothing new is rendered, and the failure is simply tried again.
	second := runRenditionFill(t, env)
	if second != (renditionFill{Rendered: 0, Failed: 2, Remaining: 1, Complete: true}) {
		t.Fatalf("second fill = %+v, want nothing rendered and the one failure again", second)
	}
	if got := readRenditionKeys(t, coverOnly); got != coverOnlyKeys {
		t.Fatalf("a second fill re-rendered: %+v, was %+v", got, coverOnlyKeys)
	}

	// Once the original arrives, the next run fills it, and the one after has
	// nothing to do.
	sharedStorage.storeObject(lostCover, "image/png", pngBytes(t, threeBands(900, 300)))
	if third := runRenditionFill(t, env); third != (renditionFill{Rendered: 2, Remaining: 0, Complete: true}) {
		t.Fatalf("third fill = %+v, want the late original's two renditions", third)
	}
	if fourth := runRenditionFill(t, env); fourth != (renditionFill{Remaining: 0, Complete: true}) {
		t.Fatalf("fourth fill = %+v, want nothing to do", fourth)
	}
}

func TestTheRenditionFillRendersOnlyTheMissingSlotOfAnEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Half Rendered", "half-rendered")
	patchEventMedia(t, env, sessionID, eventID, "Half Rendered", "half-rendered", map[string]any{
		"cover_image_key": uploadCover(t, env, sessionID, eventID, threeBands(900, 300)),
	})
	rendered := readRenditionKeys(t, eventID)
	// The square failed when the Cover Image was saved; the wide did not.
	if _, err := sharedDB.ExecContext(context.Background(),
		`UPDATE events SET email_square_key = NULL WHERE id = $1`, eventID); err != nil {
		t.Fatalf("null the square: %v", err)
	}

	if got := runRenditionFill(t, env); got != (renditionFill{Rendered: 1, Remaining: 0, Complete: true}) {
		t.Fatalf("fill = %+v, want the one missing square", got)
	}
	after := readRenditionKeys(t, eventID)
	if after.wide != rendered.wide {
		t.Fatalf("the fill re-rendered the wide rendition: %q, was %q", after.wide, rendered.wide)
	}
	if after.square == "" || after.square == rendered.square {
		t.Fatalf("square rendition = %q after the fill", after.square)
	}
	assertSquareIs(t, after.square, bandGreen)
}
