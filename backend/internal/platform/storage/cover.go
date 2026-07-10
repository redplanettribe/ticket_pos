package storage

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

var allowedCoverContentTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// CoverContentTypeAllowed reports whether a MIME type is accepted for event covers.
func CoverContentTypeAllowed(contentType string) bool {
	_, ok := allowedCoverContentTypes[strings.ToLower(strings.TrimSpace(contentType))]
	return ok
}

// CoverExtensionForContentType returns the file extension for an allowed content type.
func CoverExtensionForContentType(contentType string) (string, bool) {
	ext, ok := allowedCoverContentTypes[strings.ToLower(strings.TrimSpace(contentType))]
	return ext, ok
}

// BuildCoverObjectKey returns a unique object key for an event cover image.
func BuildCoverObjectKey(organizationID, eventID, contentType, fileName string) (string, error) {
	ext, ok := CoverExtensionForContentType(contentType)
	if !ok {
		return "", fmt.Errorf("unsupported content type %q", contentType)
	}

	if trimmed := strings.TrimSpace(fileName); trimmed != "" {
		if guessed := strings.ToLower(filepath.Ext(trimmed)); guessed == ext {
			ext = guessed
		}
	}

	return fmt.Sprintf("covers/%s/%s/%s%s", organizationID, eventID, uuid.NewString(), ext), nil
}

// CoverKeyBelongsToEvent reports whether an object key is scoped to the org and event.
func CoverKeyBelongsToEvent(key, organizationID, eventID string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	prefix := fmt.Sprintf("covers/%s/%s/", organizationID, eventID)
	return strings.HasPrefix(key, prefix) && !strings.Contains(key, "..")
}
