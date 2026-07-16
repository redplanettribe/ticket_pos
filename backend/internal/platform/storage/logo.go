package storage

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// BuildLogoObjectKey returns a unique object key for an organization logo.
// Reuses the same content-type allowlist as event covers.
func BuildLogoObjectKey(organizationID, contentType, fileName string) (string, error) {
	ext, ok := CoverExtensionForContentType(contentType)
	if !ok {
		return "", fmt.Errorf("unsupported content type %q", contentType)
	}

	if trimmed := strings.TrimSpace(fileName); trimmed != "" {
		if guessed := strings.ToLower(filepath.Ext(trimmed)); guessed == ext {
			ext = guessed
		}
	}

	return fmt.Sprintf("logos/%s/%s%s", organizationID, uuid.NewString(), ext), nil
}

// LogoKeyBelongsToOrg reports whether an object key is scoped to the organization.
func LogoKeyBelongsToOrg(key, organizationID string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	prefix := fmt.Sprintf("logos/%s/", organizationID)
	return strings.HasPrefix(key, prefix) && !strings.Contains(key, "..")
}
