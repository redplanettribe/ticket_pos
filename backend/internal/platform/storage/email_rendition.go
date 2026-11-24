package storage

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"

	// The formats an Event original may be: a Cover Image is JPEG, PNG or
	// WebP, and a Thumbnail is always JPEG. Registered for image.Decode.
	_ "image/png"

	"github.com/google/uuid"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Email renditions (spec #870): mail never ships an original, because an
// original may be a WebP desktop Outlook cannot show or a multi-megabyte
// photo nobody should download on a phone. Each Event has two, produced on
// the server from its originals:
//
//   - the wide rendition, the Cover Image as a JPEG 1200px wide (2x the 600px
//     card), and
//   - the square rendition, a 128px square JPEG (2x the 64px Event row cell)
//     from the Thumbnail, or from the Cover Image cropped to its centre when
//     there is no Thumbnail, which is the Storefront's own fallback.
//
// Each lives under its own prefix, disjoint from `covers/`, `thumbnails/` and
// `videos/` (ADR 0020), and is never minted for a browser: no presigned URL is
// ever issued for these prefixes and no client-supplied key is checked
// against them.
const (
	// EmailRenditionContentType is the one type every rendition is stored as.
	EmailRenditionContentType = "image/jpeg"
	// EmailWideWidth is the wide rendition's width. A narrower Cover Image is
	// kept at its own width rather than upscaled: the mail sets the display
	// width, so upscaling would only add bytes.
	EmailWideWidth = 1200
	// EmailSquareSide is the square rendition's side.
	EmailSquareSide = 128

	emailRenditionQuality = 82
	// maxOriginalBytes and maxOriginalPixels bound what a rendition will read
	// and decode, so a hostile or accidental original cannot exhaust memory.
	// Both are far above any Cover Image the staff form produces.
	maxOriginalBytes  = 40 << 20
	maxOriginalPixels = 50_000_000
)

// BuildEmailWideObjectKey returns a unique object key for an Event's wide
// email rendition.
func BuildEmailWideObjectKey(organizationID, eventID string) string {
	return fmt.Sprintf("email-wide/%s/%s/%s.jpg", organizationID, eventID, uuid.NewString())
}

// BuildEmailSquareObjectKey returns a unique object key for an Event's square
// email rendition.
func BuildEmailSquareObjectKey(organizationID, eventID string) string {
	return fmt.Sprintf("email-square/%s/%s/%s.jpg", organizationID, eventID, uuid.NewString())
}

// RenderEmailWide reads an original image and returns its wide rendition: a
// JPEG at most EmailWideWidth wide, keeping the aspect ratio.
func RenderEmailWide(original io.Reader) ([]byte, error) {
	src, err := decodeOriginal(original)
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > EmailWideWidth {
		h = max(1, (h*EmailWideWidth+w/2)/w)
		w = EmailWideWidth
	}
	return encodeRendition(src, b, w, h)
}

// RenderEmailSquare reads an original image and returns its square rendition:
// the largest centred square of it, as an EmailSquareSide JPEG.
func RenderEmailSquare(original io.Reader) ([]byte, error) {
	src, err := decodeOriginal(original)
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	x0 := b.Min.X + (b.Dx()-side)/2
	y0 := b.Min.Y + (b.Dy()-side)/2
	crop := image.Rect(x0, y0, x0+side, y0+side)
	return encodeRendition(src, crop, EmailSquareSide, EmailSquareSide)
}

func decodeOriginal(original io.Reader) (image.Image, error) {
	data, err := io.ReadAll(io.LimitReader(original, maxOriginalBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read original: %w", err)
	}
	if len(data) > maxOriginalBytes {
		return nil, fmt.Errorf("original is over %d bytes", maxOriginalBytes)
	}
	if len(data) == 0 {
		return nil, errors.New("original is empty")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("read original header: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxOriginalPixels {
		return nil, fmt.Errorf("original %s is %dx%d, outside the %d pixel limit", format, cfg.Width, cfg.Height, maxOriginalPixels)
	}
	img, _, err := image.Decode(bufio.NewReader(bytes.NewReader(data)))
	if err != nil {
		return nil, fmt.Errorf("decode original %s: %w", format, err)
	}
	return img, nil
}

// encodeRendition scales the part of src inside from to w x h over a white
// canvas, so a transparent PNG or WebP does not turn black in a JPEG, and
// encodes it.
func encodeRendition(src image.Image, from image.Rectangle, w, h int) ([]byte, error) {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, from, draw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: emailRenditionQuality}); err != nil {
		return nil, fmt.Errorf("encode rendition: %w", err)
	}
	return out.Bytes(), nil
}
