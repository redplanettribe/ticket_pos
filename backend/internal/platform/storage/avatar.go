package storage

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// BuildAvatarObjectKey returns a unique object key for a Customer Avatar.
// Reuses the same content-type allowlist as event covers and logos.
func BuildAvatarObjectKey(customerID, contentType, fileName string) (string, error) {
	ext, ok := CoverExtensionForContentType(contentType)
	if !ok {
		return "", fmt.Errorf("unsupported content type %q", contentType)
	}

	if trimmed := strings.TrimSpace(fileName); trimmed != "" {
		if guessed := strings.ToLower(filepath.Ext(trimmed)); guessed == ext {
			ext = guessed
		}
	}

	return fmt.Sprintf("avatars/%s/%s%s", customerID, uuid.NewString(), ext), nil
}

// AvatarKeyBelongsToCustomer reports whether an object key is scoped to the
// Customer. It is the write-side guard: a Customer may attach only a key minted
// under their own prefix, so no request can point their Avatar at another
// Customer's image or at an arbitrary object.
func AvatarKeyBelongsToCustomer(key, customerID string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	prefix := fmt.Sprintf("avatars/%s/", customerID)
	return strings.HasPrefix(key, prefix) && !strings.Contains(key, "..")
}
