package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// publishPlainEvent publishes a non-discoverable event and returns its ID.
func publishPlainEvent(t *testing.T, env *testEnv, sessionID, name, slug string) string {
	t.Helper()
	startsAt := env.fixedClock.Add(24 * time.Hour)
	return publishEvent(t, env, sessionID, name, slug, startsAt, false, 1000, 50)
}

// addEventStaffMember adds a non-admin member and returns their session ID.
func addEventStaffMember(t *testing.T, env *testEnv, adminSessionID, email string) string {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": email,
		"role":  "event_staff",
	}, authHeader(adminSessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return verifyOTP(t, env, email)
}

func eventDiscoverable(t *testing.T, env *testEnv, sessionID, eventID string) bool {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var event struct {
		Discoverable bool `json:"discoverable"`
	}
	if err := json.Unmarshal(body.Data, &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	return event.Discoverable
}

func TestSetDiscoverableByNonAdminMemberOnPublishedEvent(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	eventID := publishPlainEvent(t, env, adminSessionID, "Summer Show", "summer-show")

	staffSessionID := addEventStaffMember(t, env, adminSessionID, "staff@example.com")

	// A non-admin member turns discoverability on.
	resp, body := env.put(t, "/api/v1/staff/events/"+eventID+"/discoverable", map[string]any{
		"discoverable": true,
	}, authHeader(staffSessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set discoverable status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var updated struct {
		Discoverable bool `json:"discoverable"`
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode updated event: %v", err)
	}
	if !updated.Discoverable {
		t.Fatalf("expected discoverable=true, got %+v", updated)
	}
	if !eventDiscoverable(t, env, adminSessionID, eventID) {
		t.Fatal("expected event to be discoverable after enabling")
	}

	// And turns it back off.
	resp, body = env.put(t, "/api/v1/staff/events/"+eventID+"/discoverable", map[string]any{
		"discoverable": false,
	}, authHeader(staffSessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unset discoverable status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if eventDiscoverable(t, env, adminSessionID, eventID) {
		t.Fatal("expected event to be non-discoverable after disabling")
	}
}

func TestSetDiscoverableRejectedOnDraftEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Draft Show", "draft-show")

	resp, body := env.put(t, "/api/v1/staff/events/"+eventID+"/discoverable", map[string]any{
		"discoverable": true,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 on draft, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_NOT_PUBLISHED" {
		t.Fatalf("expected EVENT_NOT_PUBLISHED, got %+v", body.Error)
	}
}

func TestSetDiscoverableRejectedOnCancelledEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := publishPlainEvent(t, env, sessionID, "Cancelled Show", "cancelled-show")

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/cancel", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel event status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.put(t, "/api/v1/staff/events/"+eventID+"/discoverable", map[string]any{
		"discoverable": true,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 on cancelled, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_NOT_PUBLISHED" {
		t.Fatalf("expected EVENT_NOT_PUBLISHED, got %+v", body.Error)
	}
}

func TestPatchDoesNotSetDiscoverable(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := publishPlainEvent(t, env, sessionID, "Patch Show", "patch-show")

	// PATCH no longer owns the discoverable flag; sending it must be ignored.
	// A published event's slug cannot change, so reuse the published slug.
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":         "Patch Show",
		"slug":         "patch-show",
		"discoverable": true,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if eventDiscoverable(t, env, sessionID, eventID) {
		t.Fatal("expected PATCH to leave discoverable unchanged (false)")
	}
}
