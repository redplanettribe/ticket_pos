package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

type tagView struct {
	Name    string `json:"name"`
	Curated bool   `json:"curated"`
}

func decodeTags(t *testing.T, raw json.RawMessage) []tagView {
	t.Helper()
	var tags []tagView
	if err := json.Unmarshal(raw, &tags); err != nil {
		t.Fatalf("decode tags: %v", err)
	}
	return tags
}

func setEventTags(t *testing.T, env *testEnv, sessionID, eventID string, names []string) (*http.Response, envelope) {
	t.Helper()
	return env.put(t, "/api/v1/staff/events/"+eventID+"/tags", map[string]any{"tags": names}, authHeader(sessionID))
}

func TestPopularCustomTagsRankedByUsageExcludingPresetsAndOrphans(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	eventA := createDraftEvent(t, env, sessionID, "Rave A", "rave-a")
	eventB := createDraftEvent(t, env, sessionID, "Rave B", "rave-b")
	eventC := createDraftEvent(t, env, sessionID, "Rave C", "rave-c")
	orphanEvent := createDraftEvent(t, env, sessionID, "Orphan Fest", "orphan-fest")

	// Techno used twice, House and Ambient once each; Music is a preset (must be
	// excluded from popular even though it has usage).
	setEventTags(t, env, sessionID, eventA, []string{"Techno", "House", "Music"})
	setEventTags(t, env, sessionID, eventB, []string{"Techno"})
	setEventTags(t, env, sessionID, eventC, []string{"Ambient"})

	// Coin a custom tag then remove it: the pool row survives with zero usage and
	// must not appear in the popular browse list.
	setEventTags(t, env, sessionID, orphanEvent, []string{"Orphan"})
	setEventTags(t, env, sessionID, orphanEvent, []string{})

	resp, body := env.get(t, "/api/v1/staff/tags/popular", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("popular tags status=%d error=%+v", resp.StatusCode, body.Error)
	}
	tags := decodeTags(t, body.Data)

	names := make([]string, len(tags))
	for i, tag := range tags {
		if tag.Curated {
			t.Fatalf("popular list must be custom-only, got curated tag %q", tag.Name)
		}
		names[i] = tag.Name
	}

	// Most-used first: Techno (2) leads; House and Ambient (1 each) follow.
	if len(names) != 3 || names[0] != "Techno" {
		t.Fatalf("expected Techno first among 3 custom tags, got %v", names)
	}
	for _, banned := range []string{"Music", "Orphan"} {
		for _, got := range names {
			if got == banned {
				t.Fatalf("popular list should exclude %q, got %v", banned, names)
			}
		}
	}
}

func TestSearchTagsReturnsPresetsFirst(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := env.get(t, "/api/v1/staff/tags?q=mus", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search tags status=%d error=%+v", resp.StatusCode, body.Error)
	}
	tags := decodeTags(t, body.Data)
	if len(tags) == 0 || tags[0].Name != "Music" || !tags[0].Curated {
		t.Fatalf("expected Music preset first, got %+v", tags)
	}
}

func TestSetEventTagsCoinsCustomTagAndReuses(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventA := createDraftEvent(t, env, sessionID, "Rave A", "rave-a")
	eventB := createDraftEvent(t, env, sessionID, "Rave B", "rave-b")

	resp, body := setEventTags(t, env, sessionID, eventA, []string{"Music", "Techno"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set tags status=%d error=%+v", resp.StatusCode, body.Error)
	}
	tags := decodeTags(t, body.Data)
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %+v", tags)
	}
	// Preset first, custom coined as non-curated.
	if tags[0].Name != "Music" || !tags[0].Curated {
		t.Errorf("expected Music curated first, got %+v", tags[0])
	}
	if tags[1].Name != "Techno" || tags[1].Curated {
		t.Errorf("expected Techno custom, got %+v", tags[1])
	}

	// A different event coining the same name with different casing reuses the
	// one pool row and keeps the first coiner's display casing.
	if _, b := setEventTags(t, env, sessionID, eventB, []string{"techno"}); b.Error != nil {
		t.Fatalf("set tags B error=%+v", b.Error)
	}
	var count int
	var display string
	if err := env.db.QueryRow(`SELECT COUNT(*), MAX(display_name) FROM tags WHERE canonical_key = 'techno'`).Scan(&count, &display); err != nil {
		t.Fatalf("query techno tag: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 pooled techno tag, got %d", count)
	}
	if display != "Techno" {
		t.Fatalf("expected first coiner casing 'Techno', got %q", display)
	}
}

func TestSetEventTagsDedupesWithinRequest(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Dupe Fest", "dupe-fest")

	_, body := setEventTags(t, env, sessionID, eventID, []string{"Techno", "techno", "  TECHNO "})
	tags := decodeTags(t, body.Data)
	if len(tags) != 1 || tags[0].Name != "Techno" {
		t.Fatalf("expected single Techno tag, got %+v", tags)
	}
}

func TestSetEventTagsReplacesExistingSet(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Replace Fest", "replace-fest")

	setEventTags(t, env, sessionID, eventID, []string{"Music", "Comedy"})
	_, body := setEventTags(t, env, sessionID, eventID, []string{"Film"})
	tags := decodeTags(t, body.Data)
	if len(tags) != 1 || tags[0].Name != "Film" {
		t.Fatalf("expected only Film after replace, got %+v", tags)
	}

	// Clearing with an empty set removes all tags.
	_, body = setEventTags(t, env, sessionID, eventID, []string{})
	if tags := decodeTags(t, body.Data); len(tags) != 0 {
		t.Fatalf("expected no tags after clear, got %+v", tags)
	}
}

func TestSetEventTagsRejectsInvalidNames(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Bad Tag Fest", "bad-tag-fest")

	resp, body := setEventTags(t, env, sessionID, eventID, []string{"rock&roll"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "INVALID_TAG" {
		t.Fatalf("expected INVALID_TAG, got %+v", body.Error)
	}
}

func TestSetEventTagsForbiddenForNonAdmin(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, adminSessionID, "Members Fest", "members-fest")

	staffSessionID := addEventStaffMember(t, env, adminSessionID, "staff@example.com")

	resp, _ := setEventTags(t, env, staffSessionID, eventID, []string{"Music"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", resp.StatusCode)
	}
}
