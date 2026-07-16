package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestPublicOrganizationBySlug(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := env.post(t, "/api/v1/staff/organization/logo-upload-url", map[string]string{
		"content_type": "image/png",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logo upload url status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var presign struct {
		ObjectKey string `json:"object_key"`
		PublicURL string `json:"public_url"`
	}
	if err := json.Unmarshal(body.Data, &presign); err != nil {
		t.Fatalf("decode presign: %v", err)
	}
	resp, body = env.patch(t, "/api/v1/staff/organization", map[string]any{
		"name":           "Test Org",
		"logo_image_key": presign.ObjectKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch logo status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.get(t, "/api/v1/public/organizations/test-org", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public org status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var view struct {
		Name    string  `json:"name"`
		Slug    string  `json:"slug"`
		LogoURL *string `json:"logo_url"`
	}
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode public organization: %v", err)
	}
	if view.Name != "Test Org" || view.Slug != "test-org" {
		t.Fatalf("view=%+v", view)
	}
	if view.LogoURL == nil || *view.LogoURL != presign.PublicURL {
		t.Fatalf("logo_url=%v want %q", view.LogoURL, presign.PublicURL)
	}
}

func TestPublicOrganizationNotFound(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)

	resp, body := env.get(t, "/api/v1/public/organizations/does-not-exist", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "ORGANIZATION_NOT_FOUND" {
		t.Fatalf("expected ORGANIZATION_NOT_FOUND, got %+v", body.Error)
	}
}
