package storage

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Thumbnails live under their own `thumbnails/` prefix, apart from `covers/`
// and `videos/` (ADR 0020), so a key minted for one of an Event's images never
// validates as another's.
//
// A Thumbnail is always the browser's own JPEG export of the organizer's square
// crop, never a raw upload, so JPEG is the only type stored (spec #853).
const thumbnailContentType = "image/jpeg"

// ThumbnailContentTypeAllowed reports whether a MIME type is accepted for an
// Event Thumbnail.
func ThumbnailContentTypeAllowed(contentType string) bool {
	return strings.ToLower(strings.TrimSpace(contentType)) == thumbnailContentType
}

// BuildThumbnailObjectKey returns a unique object key for an Event Thumbnail.
func BuildThumbnailObjectKey(organizationID, eventID, contentType string) (string, error) {
	if !ThumbnailContentTypeAllowed(contentType) {
		return "", fmt.Errorf("unsupported content type %q", contentType)
	}
	return fmt.Sprintf("thumbnails/%s/%s/%s.jpg", organizationID, eventID, uuid.NewString()), nil
}

// ThumbnailKeyBelongsToEvent reports whether an object key is scoped to the org
// and event.
func ThumbnailKeyBelongsToEvent(key, organizationID, eventID string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	prefix := fmt.Sprintf("thumbnails/%s/%s/", organizationID, eventID)
	return strings.HasPrefix(key, prefix) && !strings.Contains(key, "..")
}
