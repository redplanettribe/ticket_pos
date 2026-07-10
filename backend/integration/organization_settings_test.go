package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestOrganizationProfileReadAndUpdate(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := env.get(t, "/api/v1/staff/organization", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get organization status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var org struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(body.Data, &org); err != nil {
		t.Fatalf("decode organization: %v", err)
	}
	if org.Name != "Test Org" || org.Slug != "test-org" {
		t.Fatalf("organization=%+v", org)
	}

	resp, body = env.patch(t, "/api/v1/staff/organization", map[string]string{
		"name": "Renamed Org",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch organization status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if err := json.Unmarshal(body.Data, &org); err != nil {
		t.Fatalf("decode organization: %v", err)
	}
	if org.Name != "Renamed Org" || org.Slug != "test-org" {
		t.Fatalf("updated organization=%+v", org)
	}
}

func TestOrganizationSettingsForbiddenForNonOrgAdmin(t *testing.T) {
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
	resp, body = env.get(t, "/api/v1/staff/organization", authHeader(staffSessionID))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN, got %+v", body.Error)
	}
}

func TestMemberRosterManagement(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "staff@example.com",
		"role":  "event_staff",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "staff@example.com",
		"role":  "event_staff",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "MEMBER_ALREADY_EXISTS" {
		t.Fatalf("expected MEMBER_ALREADY_EXISTS, got %+v", body.Error)
	}

	resp, body = env.get(t, "/api/v1/staff/members", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list members status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var members []struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.Unmarshal(body.Data, &members); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("members=%+v", members)
	}
}

func TestMemberGuardrails(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := env.get(t, "/api/v1/staff/members", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list members status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var members []struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	if err := json.Unmarshal(body.Data, &members); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("members=%+v", members)
	}
	adminID := members[0].ID

	resp, body = env.deleteJSON(t, "/api/v1/staff/members/"+adminID, nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "CANNOT_REMOVE_SELF" {
		t.Fatalf("expected CANNOT_REMOVE_SELF, got %+v", body.Error)
	}
}

func TestEventAssignments(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "staff@example.com",
		"role":  "event_staff",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var member struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &member); err != nil {
		t.Fatalf("decode member: %v", err)
	}

	resp, body = env.post(t, "/api/v1/staff/events", map[string]string{
		"name": "Summer Fest",
		"slug": "summer-fest",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create event status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var event struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}

	resp, body = env.put(t, "/api/v1/staff/events/"+event.ID+"/assignments/"+member.ID, map[string]string{
		"role": "event_staff",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upsert assignment status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.get(t, "/api/v1/staff/events/"+event.ID+"/assignments", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list assignments status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var assignments []struct {
		MemberID string `json:"member_id"`
		Role     string `json:"role"`
	}
	if err := json.Unmarshal(body.Data, &assignments); err != nil {
		t.Fatalf("decode assignments: %v", err)
	}
	if len(assignments) != 1 || assignments[0].MemberID != member.ID {
		t.Fatalf("assignments=%+v", assignments)
	}
}

func TestDeleteOrganization(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := env.deleteJSON(t, "/api/v1/staff/organization", map[string]string{
		"confirmation_name": "Wrong Name",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "ORGANIZATION_DELETE_CONFIRMATION_MISMATCH" {
		t.Fatalf("expected ORGANIZATION_DELETE_CONFIRMATION_MISMATCH, got %+v", body.Error)
	}

	resp, body = env.deleteJSON(t, "/api/v1/staff/organization", map[string]string{
		"confirmation_name": "Test Org",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete organization status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.get(t, "/api/v1/staff/organization", authHeader(sessionID))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 after delete, got %d", resp.StatusCode)
	}

	var activeMemberID *string
	if err := env.db.QueryRowContext(context.Background(), `
		SELECT active_member_id::text FROM sessions WHERE id = $1
	`, sessionID).Scan(&activeMemberID); err != nil {
		t.Fatalf("load session: %v", err)
	}
	if activeMemberID != nil {
		t.Fatalf("expected active_member_id cleared, got %q", *activeMemberID)
	}
}

func TestPreProvisionedMemberCanSignIn(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	_, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "joiner@example.com",
		"role":  "event_staff",
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("add member error=%+v", body.Error)
	}

	staffSessionID := verifyOTP(t, env, "joiner@example.com")
	resp, body := env.get(t, "/api/v1/auth/session", authHeader(staffSessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get session status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var session struct {
		ActiveMember *struct {
			OrganizationSlug string `json:"organization_slug"`
		} `json:"active_member"`
		Memberships []struct {
			OrganizationSlug string `json:"organization_slug"`
		} `json:"memberships"`
	}
	if err := json.Unmarshal(body.Data, &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if len(session.Memberships) != 1 || session.Memberships[0].OrganizationSlug != "test-org" {
		t.Fatalf("session=%+v", session)
	}
	if session.ActiveMember == nil || session.ActiveMember.OrganizationSlug != "test-org" {
		t.Fatalf("expected auto-selected org, session=%+v", session)
	}
}
