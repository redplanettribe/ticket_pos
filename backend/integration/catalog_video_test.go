package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCatalogVideoUploadURLAndAttach(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Video Fest", "video-fest")

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/video-upload-url", map[string]string{
		"content_type": "video/mp4",
		"file_name":    "teaser.mp4",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("video upload url status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var result struct {
		UploadURL string `json:"upload_url"`
		ObjectKey string `json:"object_key"`
		PublicURL string `json:"public_url"`
	}
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("decode video upload: %v", err)
	}
	if result.UploadURL == "" {
		t.Fatal("expected upload_url")
	}
	if !strings.HasPrefix(result.ObjectKey, "videos/") {
		t.Fatalf("object_key=%q", result.ObjectKey)
	}
	if !strings.HasSuffix(result.ObjectKey, ".mp4") {
		t.Fatalf("object_key should end in .mp4: %q", result.ObjectKey)
	}
	if !strings.Contains(result.ObjectKey, eventID) {
		t.Fatalf("object_key should include event id: %q", result.ObjectKey)
	}
	if result.PublicURL == "" || !strings.Contains(result.PublicURL, result.ObjectKey) {
		t.Fatalf("public_url=%q object_key=%q", result.PublicURL, result.ObjectKey)
	}

	// Attach via the event PATCH.
	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Video Fest",
		"slug":            "video-fest",
		"cover_video_key": result.ObjectKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch video key status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var updated struct {
		CoverVideoKey *string `json:"cover_video_key"`
		CoverVideoURL *string `json:"cover_video_url"`
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode updated event: %v", err)
	}
	if updated.CoverVideoKey == nil || *updated.CoverVideoKey != result.ObjectKey {
		t.Fatalf("cover_video_key=%v", updated.CoverVideoKey)
	}
	if updated.CoverVideoURL == nil || *updated.CoverVideoURL != result.PublicURL {
		t.Fatalf("cover_video_url=%v want %q", updated.CoverVideoURL, result.PublicURL)
	}

	// Omitting the field preserves the attached video.
	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name": "Video Fest",
		"slug": "video-fest",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch without video key status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode preserved event: %v", err)
	}
	if updated.CoverVideoKey == nil || *updated.CoverVideoKey != result.ObjectKey {
		t.Fatalf("expected preserved cover_video_key, got %v", updated.CoverVideoKey)
	}

	// Reading the event back exposes key and URL.
	resp, body = env.get(t, "/api/v1/staff/events/"+eventID, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode read event: %v", err)
	}
	if updated.CoverVideoKey == nil || *updated.CoverVideoKey != result.ObjectKey {
		t.Fatalf("read cover_video_key=%v", updated.CoverVideoKey)
	}
	if updated.CoverVideoURL == nil || *updated.CoverVideoURL != result.PublicURL {
		t.Fatalf("read cover_video_url=%v", updated.CoverVideoURL)
	}

	// Empty string clears it.
	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Video Fest",
		"slug":            "video-fest",
		"cover_video_key": "",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear video status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode cleared event: %v", err)
	}
	if updated.CoverVideoKey != nil {
		t.Fatalf("expected cleared cover_video_key, got %v", updated.CoverVideoKey)
	}
	if updated.CoverVideoURL != nil {
		t.Fatalf("expected cleared cover_video_url, got %v", updated.CoverVideoURL)
	}
}

func TestCatalogVideoUploadURLRejectsNonMP4(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Bad Video", "bad-video")

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/video-upload-url", map[string]string{
		"content_type": "video/webm",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("expected VALIDATION_FAILED, got %+v", body.Error)
	}
}

func TestCatalogVideoUploadURLForbiddenForNonOrgAdmin(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Guarded Video", "guarded-video")

	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "staff@example.com",
		"role":  "event_staff",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}

	staffSessionID := verifyOTP(t, env, "staff@example.com")
	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/video-upload-url", map[string]string{
		"content_type": "video/mp4",
	}, authHeader(staffSessionID))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d error=%+v", resp.StatusCode, body.Error)
	}
}

func TestCatalogVideoAttachRejectsForeignAndImageKeys(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Own Video", "own-video")
	otherEventID := createDraftEvent(t, env, sessionID, "Other Video", "other-video")

	// A video key minted for another Event must not attach here.
	_, body := env.post(t, "/api/v1/staff/events/"+otherEventID+"/video-upload-url", map[string]string{
		"content_type": "video/mp4",
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("other video upload url error=%+v", body.Error)
	}
	var other struct {
		ObjectKey string `json:"object_key"`
	}
	if err := json.Unmarshal(body.Data, &other); err != nil {
		t.Fatalf("decode other upload: %v", err)
	}

	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Own Video",
		"slug":            "own-video",
		"cover_video_key": other.ObjectKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for foreign video key, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "INVALID_COVER_VIDEO_KEY" {
		t.Fatalf("expected INVALID_COVER_VIDEO_KEY, got %+v", body.Error)
	}

	// A covers/-prefixed key for this very Event must not validate as a video key.
	_, body = env.post(t, "/api/v1/staff/events/"+eventID+"/cover-upload-url", map[string]string{
		"content_type": "image/png",
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("cover upload url error=%+v", body.Error)
	}
	var cover struct {
		ObjectKey string `json:"object_key"`
	}
	if err := json.Unmarshal(body.Data, &cover); err != nil {
		t.Fatalf("decode cover upload: %v", err)
	}

	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Own Video",
		"slug":            "own-video",
		"cover_video_key": cover.ObjectKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for covers-prefixed video key, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "INVALID_COVER_VIDEO_KEY" {
		t.Fatalf("expected INVALID_COVER_VIDEO_KEY, got %+v", body.Error)
	}

	// And the mirror: a videos/-prefixed key must not validate as an image key.
	_, body = env.post(t, "/api/v1/staff/events/"+eventID+"/video-upload-url", map[string]string{
		"content_type": "video/mp4",
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("video upload url error=%+v", body.Error)
	}
	var video struct {
		ObjectKey string `json:"object_key"`
	}
	if err := json.Unmarshal(body.Data, &video); err != nil {
		t.Fatalf("decode video upload: %v", err)
	}

	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Own Video",
		"slug":            "own-video",
		"cover_image_key": video.ObjectKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for videos-prefixed image key, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "INVALID_COVER_IMAGE_KEY" {
		t.Fatalf("expected INVALID_COVER_IMAGE_KEY, got %+v", body.Error)
	}
}

func TestPublicEventExposesCoverVideoURL(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	soon := env.fixedClock.Add(10 * 24 * time.Hour)
	eventID := publishEvent(t, env, sessionID, "Public Video", "public-video", soon, true, 2500, 50)

	_, body := env.post(t, "/api/v1/staff/events/"+eventID+"/video-upload-url", map[string]string{
		"content_type": "video/mp4",
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("video upload url error=%+v", body.Error)
	}
	var upload struct {
		ObjectKey string `json:"object_key"`
		PublicURL string `json:"public_url"`
	}
	if err := json.Unmarshal(body.Data, &upload); err != nil {
		t.Fatalf("decode upload: %v", err)
	}

	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Public Video",
		"slug":            "public-video",
		"starts_at":       soon.Format(time.RFC3339),
		"timezone":        "America/New_York",
		"venue_name":      "The Hall",
		"cover_video_key": upload.ObjectKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("attach video status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.get(t, "/api/v1/public/organizations/test-org/events/public-video", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var public struct {
		CoverVideoURL *string `json:"cover_video_url"`
		CoverVideoKey *string `json:"cover_video_key"`
	}
	if err := json.Unmarshal(body.Data, &public); err != nil {
		t.Fatalf("decode public event: %v", err)
	}
	if public.CoverVideoURL == nil || *public.CoverVideoURL != upload.PublicURL {
		t.Fatalf("public cover_video_url=%v want %q", public.CoverVideoURL, upload.PublicURL)
	}
	if public.CoverVideoKey != nil {
		t.Fatalf("public view must not expose cover_video_key, got %v", public.CoverVideoKey)
	}
}
