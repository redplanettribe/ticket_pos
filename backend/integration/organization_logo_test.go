package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestOrganizationLogoUploadAttachAndRemove(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := env.post(t, "/api/v1/staff/organization/logo-upload-url", map[string]string{
		"content_type": "image/png",
		"file_name":    "logo.png",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logo upload url status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var result struct {
		UploadURL string `json:"upload_url"`
		ObjectKey string `json:"object_key"`
		PublicURL string `json:"public_url"`
	}
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("decode logo upload: %v", err)
	}
	if result.UploadURL == "" {
		t.Fatal("expected upload_url")
	}
	if !strings.HasPrefix(result.ObjectKey, "logos/") {
		t.Fatalf("object_key=%q", result.ObjectKey)
	}
	if result.PublicURL == "" || !strings.Contains(result.PublicURL, result.ObjectKey) {
		t.Fatalf("public_url=%q object_key=%q", result.PublicURL, result.ObjectKey)
	}

	resp, body = env.patch(t, "/api/v1/staff/organization", map[string]any{
		"name":           "Test Org",
		"logo_image_key": result.ObjectKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch logo key status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var updated struct {
		LogoURL *string `json:"logo_url"`
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode updated organization: %v", err)
	}
	if updated.LogoURL == nil || *updated.LogoURL != result.PublicURL {
		t.Fatalf("logo_url=%v want %q", updated.LogoURL, result.PublicURL)
	}

	resp, body = env.patch(t, "/api/v1/staff/organization", map[string]any{
		"name":           "Test Org",
		"logo_image_key": "",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear logo status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode cleared organization: %v", err)
	}
	if updated.LogoURL != nil {
		t.Fatalf("expected cleared logo_url, got %v", *updated.LogoURL)
	}
}

func TestOrganizationLogoUploadURLValidation(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := env.post(t, "/api/v1/staff/organization/logo-upload-url", map[string]string{
		"content_type": "image/gif",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("expected VALIDATION_FAILED, got %+v", body.Error)
	}
}

func TestOrganizationLogoUploadForbiddenForNonOrgAdmin(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "staff@example.com",
		"role":  "event_staff",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}

	staffSessionID := verifyOTP(t, env, "staff@example.com")
	resp, body = env.post(t, "/api/v1/staff/organization/logo-upload-url", map[string]string{
		"content_type": "image/png",
	}, authHeader(staffSessionID))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d error=%+v", resp.StatusCode, body.Error)
	}
}
