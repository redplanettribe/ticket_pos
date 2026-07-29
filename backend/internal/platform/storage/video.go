package storage

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Cover Videos live under their own `videos/` prefix, deliberately apart from
// `covers/` (ADR 0020). The two prefix guards below and in cover.go are the only
// thing standing between an MP4 key and the image slot: keeping the namespaces
// disjoint means neither key can ever validate as the other's.
const videoContentType = "video/mp4"

// VideoContentTypeAllowed reports whether a MIME type is accepted for event
// Cover Videos. Only H.264/AAC MP4 is stored, verbatim and untranscoded.
func VideoContentTypeAllowed(contentType string) bool {
	return strings.ToLower(strings.TrimSpace(contentType)) == videoContentType
}

// BuildVideoObjectKey returns a unique object key for an event Cover Video.
func BuildVideoObjectKey(organizationID, eventID, contentType string) (string, error) {
	if !VideoContentTypeAllowed(contentType) {
		return "", fmt.Errorf("unsupported content type %q", contentType)
	}
	return fmt.Sprintf("videos/%s/%s/%s.mp4", organizationID, eventID, uuid.NewString()), nil
}

// VideoKeyBelongsToEvent reports whether an object key is scoped to the org and event.
func VideoKeyBelongsToEvent(key, organizationID, eventID string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	prefix := fmt.Sprintf("videos/%s/%s/", organizationID, eventID)
	return strings.HasPrefix(key, prefix) && !strings.Contains(key, "..")
}
