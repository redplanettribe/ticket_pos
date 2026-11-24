package storage

import (
	"bytes"
	"image"
	"os"
	"strings"
	"testing"
)

// decodeRenditionPNG asserts the rendition is a PNG and returns it decoded.
func decodeRenditionPNG(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode rendition: %v", err)
	}
	if format != "png" {
		t.Fatalf("rendition format = %q, want png", format)
	}
	return img
}

func TestTheLogoRenditionIsAPNGFittedWithin96PxKeepingTheAspect(t *testing.T) {
	cases := map[string]struct {
		w, h int
		want image.Point
	}{
		"a wide Logo is 96px wide":          {w: 900, h: 300, want: image.Point{X: 96, Y: 32}},
		"a tall Logo is 96px tall":          {w: 200, h: 800, want: image.Point{X: 24, Y: 96}},
		"a square Logo is 96px square":      {w: 512, h: 512, want: image.Point{X: 96, Y: 96}},
		"a smaller Logo keeps its own size": {w: 60, h: 30, want: image.Point{X: 60, Y: 30}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := RenderEmailLogo(bytes.NewReader(encodePNG(t, bands(c.w, c.h))))
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if got := decodeRenditionPNG(t, out).Bounds().Size(); got != c.want {
				t.Fatalf("size = %v, want %v", got, c.want)
			}
		})
	}
}

func TestTheLogoRenditionKeepsTheWholeLogo(t *testing.T) {
	out, err := RenderEmailLogo(bytes.NewReader(encodeJPEG(t, bands(900, 300))))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	img := decodeRenditionPNG(t, out)
	// Nothing is cropped: all three bands survive.
	assertNear(t, img, 8, 16, red)
	assertNear(t, img, 48, 16, green)
	assertNear(t, img, 88, 16, blue)
}

func TestTheLogoRenditionKeepsTransparency(t *testing.T) {
	// A Logo drawn on nothing: a transparent field with an opaque blue square
	// in the middle.
	logo := image.NewNRGBA(image.Rect(0, 0, 192, 192))
	for x := 64; x < 128; x++ {
		for y := 64; y < 128; y++ {
			logo.Set(x, y, blue)
		}
	}
	out, err := RenderEmailLogo(bytes.NewReader(encodePNG(t, logo)))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	img := decodeRenditionPNG(t, out)
	if _, _, _, a := img.At(4, 4).RGBA(); a != 0 {
		t.Fatalf("corner alpha = %d, want fully transparent", a)
	}
	if _, _, _, a := img.At(48, 48).RGBA(); a != 0xffff {
		t.Fatalf("centre alpha = %d, want opaque", a)
	}
	assertNear(t, img, 48, 48, blue)
}

func TestAWebPLogoRendersToPNG(t *testing.T) {
	webp, err := os.ReadFile("testdata/three-bands.webp")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	out, err := RenderEmailLogo(bytes.NewReader(webp))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	img := decodeRenditionPNG(t, out)
	if got := img.Bounds().Size(); got != (image.Point{X: 96, Y: 32}) {
		t.Fatalf("size = %v, want 96x32", got)
	}
	assertNear(t, img, 8, 16, red)
	assertNear(t, img, 48, 16, green)
}

func TestALogoThatIsNoImageIsRefused(t *testing.T) {
	if _, err := RenderEmailLogo(strings.NewReader("not an image")); err == nil {
		t.Fatal("expected an error for bytes that are no image")
	}
	if _, err := RenderEmailLogo(bytes.NewReader(pngHeaderOnly(20000, 20000))); err == nil {
		t.Fatal("expected an error for a Logo over the pixel limit")
	}
}

func TestTheLogoRenditionKeyLivesUnderItsOwnPrefix(t *testing.T) {
	org := "11111111-1111-1111-1111-111111111111"
	key := BuildEmailLogoObjectKey(org)
	if !strings.HasPrefix(key, "email-logo/"+org+"/") || !strings.HasSuffix(key, ".png") {
		t.Fatalf("logo rendition key = %q", key)
	}
	if BuildEmailLogoObjectKey(org) == key {
		t.Fatal("each rendition key must be unique")
	}
	// ADR 0020: a rendition key never validates as an original's.
	if LogoKeyBelongsToOrg(key, org) {
		t.Fatalf("rendition key %q validates as a Logo", key)
	}
}
