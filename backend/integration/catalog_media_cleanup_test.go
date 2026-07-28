package integration

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

// Delete-on-replace (#134, ADR 0020): replacing or clearing a Cover Video or a
// Cover Image deletes the object the Event used to point at. The database
// commits first; the delete is best-effort and its failure never reaches the
// organizer.

func videoUploadKey(t *testing.T, env *testEnv, sessionID, eventID string) string {
	t.Helper()
	_, body := env.post(t, "/api/v1/staff/events/"+eventID+"/video-upload-url", map[string]string{
		"content_type": "video/mp4",
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("video upload url error=%+v", body.Error)
	}
	var upload struct {
		ObjectKey string `json:"object_key"`
	}
	if err := json.Unmarshal(body.Data, &upload); err != nil {
		t.Fatalf("decode video upload: %v", err)
	}
	return upload.ObjectKey
}

func coverUploadKey(t *testing.T, env *testEnv, sessionID, eventID string) string {
	t.Helper()
	_, body := env.post(t, "/api/v1/staff/events/"+eventID+"/cover-upload-url", map[string]string{
		"content_type": "image/png",
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("cover upload url error=%+v", body.Error)
	}
	var upload struct {
		ObjectKey string `json:"object_key"`
	}
	if err := json.Unmarshal(body.Data, &upload); err != nil {
		t.Fatalf("decode cover upload: %v", err)
	}
	return upload.ObjectKey
}

// patchEventMedia sends the minimum PATCH body plus whatever media fields the
// case is about, and asserts the request succeeded.
func patchEventMedia(t *testing.T, env *testEnv, sessionID, eventID, name, slug string, fields map[string]any) {
	t.Helper()
	payload := map[string]any{"name": name, "slug": slug}
	for k, v := range fields {
		payload[k] = v
	}
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, payload, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch %v status=%d error=%+v", fields, resp.StatusCode, body.Error)
	}
}

func assertDeleted(t *testing.T, want []string) {
	t.Helper()
	got := sharedStorage.deletedKeys()
	if len(got) != len(want) {
		t.Fatalf("deleted keys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("deleted keys = %v, want %v", got, want)
		}
	}
}

func TestCoverVideoReplaceDeletesTheOldObject(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Clean Video", "clean-video")

	first := videoUploadKey(t, env, sessionID, eventID)
	patchEventMedia(t, env, sessionID, eventID, "Clean Video", "clean-video", map[string]any{
		"cover_video_key": first,
	})
	// Attaching for the first time has no predecessor to clean up.
	assertDeleted(t, nil)

	second := videoUploadKey(t, env, sessionID, eventID)
	if second == first {
		t.Fatalf("expected a distinct second key, got %q twice", first)
	}
	patchEventMedia(t, env, sessionID, eventID, "Clean Video", "clean-video", map[string]any{
		"cover_video_key": second,
	})
	assertDeleted(t, []string{first})

	// Clearing deletes what is there now.
	patchEventMedia(t, env, sessionID, eventID, "Clean Video", "clean-video", map[string]any{
		"cover_video_key": "",
	})
	assertDeleted(t, []string{first, second})
}

func TestCoverImageReplaceDeletesTheOldObject(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Clean Cover", "clean-cover")

	first := coverUploadKey(t, env, sessionID, eventID)
	patchEventMedia(t, env, sessionID, eventID, "Clean Cover", "clean-cover", map[string]any{
		"cover_image_key": first,
	})
	assertDeleted(t, nil)

	second := coverUploadKey(t, env, sessionID, eventID)
	if second == first {
		t.Fatalf("expected a distinct second key, got %q twice", first)
	}
	patchEventMedia(t, env, sessionID, eventID, "Clean Cover", "clean-cover", map[string]any{
		"cover_image_key": second,
	})
	assertDeleted(t, []string{first})

	patchEventMedia(t, env, sessionID, eventID, "Clean Cover", "clean-cover", map[string]any{
		"cover_image_key": "",
	})
	assertDeleted(t, []string{first, second})
}

func TestMediaCleanupSkipsOmittedAndUnchangedKeys(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Kept Media", "kept-media")

	video := videoUploadKey(t, env, sessionID, eventID)
	cover := coverUploadKey(t, env, sessionID, eventID)
	patchEventMedia(t, env, sessionID, eventID, "Kept Media", "kept-media", map[string]any{
		"cover_video_key": video,
		"cover_image_key": cover,
	})
	assertDeleted(t, nil)

	// Omitted: the fields are preserved, nothing is replaced.
	patchEventMedia(t, env, sessionID, eventID, "Kept Media", "kept-media", nil)
	assertDeleted(t, nil)

	// Resent unchanged: same object, still in use.
	patchEventMedia(t, env, sessionID, eventID, "Kept Media", "kept-media", map[string]any{
		"cover_video_key": video,
		"cover_image_key": cover,
	})
	assertDeleted(t, nil)

	// Clearing a field that was already empty deletes nothing.
	patchEventMedia(t, env, sessionID, eventID, "Kept Media", "kept-media", map[string]any{
		"cover_video_key": "",
	})
	assertDeleted(t, []string{video})
	patchEventMedia(t, env, sessionID, eventID, "Kept Media", "kept-media", map[string]any{
		"cover_video_key": "",
	})
	assertDeleted(t, []string{video})
}

func TestMediaCleanupFailureDoesNotFailThePatch(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Doomed Delete", "doomed-delete")

	first := videoUploadKey(t, env, sessionID, eventID)
	patchEventMedia(t, env, sessionID, eventID, "Doomed Delete", "doomed-delete", map[string]any{
		"cover_video_key": first,
	})

	sharedStorage.failDeletes(errors.New("bucket is on fire"))

	second := videoUploadKey(t, env, sessionID, eventID)
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":            "Doomed Delete",
		"slug":            "doomed-delete",
		"cover_video_key": second,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch with failing delete status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var updated struct {
		CoverVideoKey *string `json:"cover_video_key"`
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode updated event: %v", err)
	}
	if updated.CoverVideoKey == nil || *updated.CoverVideoKey != second {
		t.Fatalf("cover_video_key=%v want %q", updated.CoverVideoKey, second)
	}
	// The attempt happened; only its failure was swallowed.
	assertDeleted(t, []string{first})
}
