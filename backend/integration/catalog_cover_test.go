package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type mockObjectStorage struct{}

func (m *mockObjectStorage) Put(ctx context.Context, key, contentType string, body io.Reader) error {
	return nil
}

func (m *mockObjectStorage) PresignPut(ctx context.Context, key, contentType string, expires time.Duration) (string, error) {
	return "https://storage.example/upload?key=" + key, nil
}

func (m *mockObjectStorage) PublicURL(key string) string {
	return "https://storage.example/" + key
}

func TestCatalogCoverUploadURL(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	_, body := env.post(t, "/api/v1/staff/events", map[string]string{
		"name": "Cover Fest",
		"slug": "cover-fest",
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("create event error=%+v", body.Error)
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &created); err != nil {
		t.Fatalf("decode event: %v", err)
	}

	resp, body := env.post(t, "/api/v1/staff/events/"+created.ID+"/cover-upload-url", map[string]string{
		"content_type": "image/png",
		"file_name":    "poster.png",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cover upload url status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var result struct {
		UploadURL string `json:"upload_url"`
		ObjectKey string `json:"object_key"`
		PublicURL string `json:"public_url"`
	}
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("decode cover upload: %v", err)
	}
	if result.UploadURL == "" {
		t.Fatal("expected upload_url")
	}
	if !strings.Contains(result.UploadURL, created.ID) && !strings.Contains(result.UploadURL, "cover-fest") {
		if !strings.Contains(result.UploadURL, "storage.example") {
			t.Fatalf("unexpected upload_url=%q", result.UploadURL)
		}
	}
	if !strings.HasPrefix(result.ObjectKey, "covers/") {
		t.Fatalf("object_key=%q", result.ObjectKey)
	}
	if !strings.Contains(result.ObjectKey, created.ID) {
		t.Fatalf("object_key should include event id: %q", result.ObjectKey)
	}
	if result.PublicURL == "" || !strings.Contains(result.PublicURL, result.ObjectKey) {
		t.Fatalf("public_url=%q object_key=%q", result.PublicURL, result.ObjectKey)
	}

	resp, body = env.patch(t, "/api/v1/staff/events/"+created.ID, map[string]any{
		"name":            "Cover Fest",
		"slug":            "cover-fest",
		"cover_image_key": result.ObjectKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch cover key status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var updated struct {
		CoverImageKey *string `json:"cover_image_key"`
		CoverImageURL *string `json:"cover_image_url"`
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode updated event: %v", err)
	}
	if updated.CoverImageKey == nil || *updated.CoverImageKey != result.ObjectKey {
		t.Fatalf("cover_image_key=%v", updated.CoverImageKey)
	}
	if updated.CoverImageURL == nil || *updated.CoverImageURL != result.PublicURL {
		t.Fatalf("cover_image_url=%v want %q", updated.CoverImageURL, result.PublicURL)
	}

	resp, body = env.patch(t, "/api/v1/staff/events/"+created.ID, map[string]any{
		"name":            "Cover Fest",
		"slug":            "cover-fest",
		"cover_image_key": "",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear cover status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode cleared event: %v", err)
	}
	if updated.CoverImageKey != nil {
		t.Fatalf("expected cleared cover_image_key, got %v", updated.CoverImageKey)
	}
}

func TestCatalogCoverUploadURLValidation(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	_, body := env.post(t, "/api/v1/staff/events", map[string]string{
		"name": "Bad Cover",
		"slug": "bad-cover",
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("create event error=%+v", body.Error)
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &created); err != nil {
		t.Fatalf("decode event: %v", err)
	}

	resp, body := env.post(t, "/api/v1/staff/events/"+created.ID+"/cover-upload-url", map[string]string{
		"content_type": "image/gif",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("expected VALIDATION_FAILED, got %+v", body.Error)
	}
}
