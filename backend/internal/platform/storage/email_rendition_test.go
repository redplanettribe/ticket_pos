package storage

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"testing"
)

// bands paints a w x h image in three vertical bands, red, green and blue, so
// a crop shows which part of the original it kept.
func bands(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		c := color.RGBA{R: 220, G: 30, B: 30, A: 255}
		switch {
		case x >= 2*w/3:
			c = color.RGBA{R: 30, G: 30, B: 220, A: 255}
		case x >= w/3:
			c = color.RGBA{R: 30, G: 200, B: 30, A: 255}
		}
		for y := range h {
			img.Set(x, y, c)
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// pngHeaderOnly is a PNG signature and IHDR chunk claiming w x h, with no
// pixels behind it: enough for a decoder to read the size and nothing more.
func pngHeaderOnly(w, h uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 2 // truecolour
	chunk := append([]byte("IHDR"), ihdr...)
	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&out, binary.BigEndian, uint32(len(ihdr)))
	out.Write(chunk)
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return out.Bytes()
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// decodeRenditionJPEG asserts the rendition is a JPEG and returns it decoded.
func decodeRenditionJPEG(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode rendition: %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("rendition format = %q, want jpeg", format)
	}
	return img
}

// assertNear asserts the pixel at (x, y) is within a JPEG's tolerance of want.
func assertNear(t *testing.T, img image.Image, x, y int, want color.RGBA) {
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

var (
	red   = color.RGBA{R: 220, G: 30, B: 30, A: 255}
	green = color.RGBA{R: 30, G: 200, B: 30, A: 255}
	blue  = color.RGBA{R: 30, G: 30, B: 220, A: 255}
	white = color.RGBA{R: 255, G: 255, B: 255, A: 255}
)

func TestTheWideRenditionIsAJPEG1200PxWideKeepingTheAspect(t *testing.T) {
	out, err := RenderEmailWide(bytes.NewReader(encodePNG(t, bands(2400, 800))))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	img := decodeRenditionJPEG(t, out)
	if got := img.Bounds().Size(); got != (image.Point{X: 1200, Y: 400}) {
		t.Fatalf("size = %v, want 1200x400", got)
	}
	assertNear(t, img, 100, 200, red)
	assertNear(t, img, 600, 200, green)
	assertNear(t, img, 1100, 200, blue)
}

func TestTheWideRenditionNeverUpscalesASmallerCover(t *testing.T) {
	out, err := RenderEmailWide(bytes.NewReader(encodeJPEG(t, bands(600, 300))))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got := decodeRenditionJPEG(t, out).Bounds().Size(); got != (image.Point{X: 600, Y: 300}) {
		t.Fatalf("size = %v, want the original 600x300", got)
	}
}

func TestTheSquareRenditionIsTheCentreOfTheImageAt128Px(t *testing.T) {
	out, err := RenderEmailSquare(bytes.NewReader(encodePNG(t, bands(900, 300))))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	img := decodeRenditionJPEG(t, out)
	if got := img.Bounds().Size(); got != (image.Point{X: 128, Y: 128}) {
		t.Fatalf("size = %v, want 128x128", got)
	}
	// The centre third of a 3:1 image is the centre square: all green.
	for _, p := range []image.Point{{8, 8}, {64, 64}, {120, 120}, {8, 120}, {120, 8}} {
		assertNear(t, img, p.X, p.Y, green)
	}
}

func TestTheSquareRenditionCropsATallImageToItsCentreToo(t *testing.T) {
	// A portrait original: the bands rotated, so rows rather than columns.
	tall := image.NewRGBA(image.Rect(0, 0, 200, 600))
	for y := range 600 {
		c := red
		switch {
		case y >= 400:
			c = blue
		case y >= 200:
			c = green
		}
		for x := range 200 {
			tall.Set(x, y, c)
		}
	}
	out, err := RenderEmailSquare(bytes.NewReader(encodePNG(t, tall)))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	img := decodeRenditionJPEG(t, out)
	assertNear(t, img, 64, 8, green)
	assertNear(t, img, 64, 120, green)
}

func TestAWebPOriginalRendersToJPEG(t *testing.T) {
	webp, err := os.ReadFile("testdata/three-bands.webp")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	wide, err := RenderEmailWide(bytes.NewReader(webp))
	if err != nil {
		t.Fatalf("render wide: %v", err)
	}
	img := decodeRenditionJPEG(t, wide)
	if got := img.Bounds().Size(); got != (image.Point{X: 300, Y: 100}) {
		t.Fatalf("wide size = %v, want 300x100", got)
	}
	assertNear(t, img, 50, 50, red)

	square, err := RenderEmailSquare(bytes.NewReader(webp))
	if err != nil {
		t.Fatalf("render square: %v", err)
	}
	assertNear(t, decodeRenditionJPEG(t, square), 64, 64, green)
}

func TestATransparentOriginalIsFlattenedOnWhite(t *testing.T) {
	// A JPEG has no alpha; the transparent pixels must not turn black.
	img := image.NewNRGBA(image.Rect(0, 0, 200, 200))
	out, err := RenderEmailSquare(bytes.NewReader(encodePNG(t, img)))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	assertNear(t, decodeRenditionJPEG(t, out), 64, 64, white)
}

func TestAnOriginalThatIsNoImageIsRefused(t *testing.T) {
	if _, err := RenderEmailWide(strings.NewReader("not an image")); err == nil {
		t.Fatal("expected an error for bytes that are no image")
	}
	if _, err := RenderEmailSquare(strings.NewReader("")); err == nil {
		t.Fatal("expected an error for an empty object")
	}
}

func TestAnOriginalWithTooManyPixelsIsRefusedBeforeDecoding(t *testing.T) {
	// A PNG header claiming 20000x20000 is refused from its header alone, so a
	// small file cannot make the server allocate gigabytes.
	huge := pngHeaderOnly(20000, 20000)
	if _, err := RenderEmailWide(bytes.NewReader(huge)); err == nil {
		t.Fatal("expected an error for an image over the pixel limit")
	}
	// 30 megapixels would decode to about 120 MB as RGBA, too much headroom to
	// spend on mail imagery inside a 512Mi instance.
	if _, err := RenderEmailSquare(bytes.NewReader(pngHeaderOnly(6000, 5000))); err == nil {
		t.Fatal("expected an error for a 30 megapixel image")
	}
}

func TestRenditionKeysLiveUnderTheirOwnPrefixes(t *testing.T) {
	org, event := "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"
	wide := BuildEmailWideObjectKey(org, event)
	square := BuildEmailSquareObjectKey(org, event)
	if !strings.HasPrefix(wide, "email-wide/"+org+"/"+event+"/") || !strings.HasSuffix(wide, ".jpg") {
		t.Fatalf("wide key = %q", wide)
	}
	if !strings.HasPrefix(square, "email-square/"+org+"/"+event+"/") || !strings.HasSuffix(square, ".jpg") {
		t.Fatalf("square key = %q", square)
	}
	if BuildEmailWideObjectKey(org, event) == wide {
		t.Fatal("each rendition key must be unique")
	}
	// ADR 0020: no rendition key validates as an original's.
	for _, key := range []string{wide, square} {
		if CoverKeyBelongsToEvent(key, org, event) || ThumbnailKeyBelongsToEvent(key, org, event) || VideoKeyBelongsToEvent(key, org, event) {
			t.Fatalf("rendition key %q validates as an original", key)
		}
	}
}
