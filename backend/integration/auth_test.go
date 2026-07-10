package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestAuthOTPHappyPath(t *testing.T) {
	env := setupTest(t)

	resp, body := env.post(t, "/api/v1/auth/otp/request", map[string]string{
		"email": "staff@example.com",
	}, map[string]string{"X-Forwarded-For": "203.0.113.1"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request otp status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("expected no error, got %+v", body.Error)
	}
	if env.email.LastCode == "" {
		t.Fatal("expected captured otp code")
	}

	resp, body = env.post(t, "/api/v1/auth/otp/verify", map[string]string{
		"email": "staff@example.com",
		"code":  env.email.LastCode,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify otp status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var verifyData struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(body.Data, &verifyData); err != nil {
		t.Fatalf("decode verify data: %v", err)
	}
	if verifyData.SessionID == "" {
		t.Fatal("expected session_id")
	}

	resp, body = env.get(t, "/api/v1/auth/session", authHeader(verifyData.SessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get session status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var session struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body.Data, &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if session.Email != "staff@example.com" {
		t.Fatalf("session email=%q", session.Email)
	}
}

func TestAuthInvalidOTP(t *testing.T) {
	env := setupTest(t)

	_, _ = env.post(t, "/api/v1/auth/otp/request", map[string]string{
		"email": "staff@example.com",
	}, nil)

	resp, body := env.post(t, "/api/v1/auth/otp/verify", map[string]string{
		"email": "staff@example.com",
		"code":  "000000",
	}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "OTP_INVALID" {
		t.Fatalf("expected OTP_INVALID, got %+v", body.Error)
	}
}

func TestAuthExpiredOTP(t *testing.T) {
	env := setupTest(t)

	_, _ = env.post(t, "/api/v1/auth/otp/request", map[string]string{
		"email": "staff@example.com",
	}, nil)
	code := env.email.LastCode

	env.service.WithClock(func() time.Time {
		return env.fixedClock.Add(11 * time.Minute)
	})

	resp, body := env.post(t, "/api/v1/auth/otp/verify", map[string]string{
		"email": "staff@example.com",
		"code":  code,
	}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "OTP_EXPIRED" {
		t.Fatalf("expected OTP_EXPIRED, got %+v", body.Error)
	}
}

func TestAuthRateLimitByEmail(t *testing.T) {
	env := setupTest(t)

	for i := 0; i < 3; i++ {
		resp, body := env.post(t, "/api/v1/auth/otp/request", map[string]string{
			"email": "staff@example.com",
		}, map[string]string{"X-Forwarded-For": fmt.Sprintf("203.0.113.%d", i)})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d status=%d error=%+v", i, resp.StatusCode, body.Error)
		}
	}

	resp, body := env.post(t, "/api/v1/auth/otp/request", map[string]string{
		"email": "staff@example.com",
	}, map[string]string{"X-Forwarded-For": "203.0.113.99"})
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "OTP_RATE_LIMITED" {
		t.Fatalf("expected OTP_RATE_LIMITED, got %+v", body.Error)
	}
}

func TestAuthRateLimitByIP(t *testing.T) {
	env := setupTest(t)

	for i := 0; i < 10; i++ {
		resp, body := env.post(t, "/api/v1/auth/otp/request", map[string]string{
			"email": fmt.Sprintf("user%d@example.com", i),
		}, map[string]string{"X-Forwarded-For": "198.51.100.10"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d status=%d error=%+v", i, resp.StatusCode, body.Error)
		}
	}

	resp, body := env.post(t, "/api/v1/auth/otp/request", map[string]string{
		"email": "another@example.com",
	}, map[string]string{"X-Forwarded-For": "198.51.100.10"})
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "OTP_RATE_LIMITED" {
		t.Fatalf("expected OTP_RATE_LIMITED, got %+v", body.Error)
	}
}

func TestAuthAttemptCap(t *testing.T) {
	env := setupTest(t)

	_, _ = env.post(t, "/api/v1/auth/otp/request", map[string]string{
		"email": "staff@example.com",
	}, nil)

	for i := 0; i < 5; i++ {
		resp, body := env.post(t, "/api/v1/auth/otp/verify", map[string]string{
			"email": "staff@example.com",
			"code":  "000000",
		}, nil)
		if i < 4 {
			if resp.StatusCode != http.StatusUnauthorized || body.Error.Code != "OTP_INVALID" {
				t.Fatalf("attempt %d: expected OTP_INVALID, got status=%d error=%+v", i, resp.StatusCode, body.Error)
			}
			continue
		}
		if resp.StatusCode != http.StatusTooManyRequests || body.Error.Code != "OTP_ATTEMPTS_EXCEEDED" {
			t.Fatalf("attempt %d: expected OTP_ATTEMPTS_EXCEEDED, got status=%d error=%+v", i, resp.StatusCode, body.Error)
		}
	}
}

func TestAuthLogoutDestroysSession(t *testing.T) {
	env := setupTest(t)

	_, _ = env.post(t, "/api/v1/auth/otp/request", map[string]string{
		"email": "staff@example.com",
	}, nil)
	resp, body := env.post(t, "/api/v1/auth/otp/verify", map[string]string{
		"email": "staff@example.com",
		"code":  env.email.LastCode,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify status=%d", resp.StatusCode)
	}
	var verifyData struct {
		SessionID string `json:"session_id"`
	}
	_ = json.Unmarshal(body.Data, &verifyData)

	resp, body = env.post(t, "/api/v1/auth/logout", nil, authHeader(verifyData.SessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.get(t, "/api/v1/auth/session", authHeader(verifyData.SessionID))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout, got %d", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "SESSION_NOT_FOUND" {
		t.Fatalf("expected SESSION_NOT_FOUND, got %+v", body.Error)
	}
}

func TestCreateOrganizationFlow(t *testing.T) {
	env := setupTest(t)
	sessionID := verifyOTP(t, env, "neworg@example.com")

	resp, body := env.post(t, "/api/v1/staff/organizations", map[string]string{
		"name": "Acme Events",
		"slug": "acme-events",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create org status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var session struct {
		Email        string `json:"email"`
		ActiveMember *struct {
			MemberID         string `json:"member_id"`
			OrganizationName string `json:"organization_name"`
			OrganizationSlug string `json:"organization_slug"`
			Role             string `json:"role"`
		} `json:"active_member"`
		Memberships []struct {
			OrganizationSlug string `json:"organization_slug"`
		} `json:"memberships"`
	}
	if err := json.Unmarshal(body.Data, &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if session.ActiveMember == nil {
		t.Fatal("expected active_member after org creation")
	}
	if session.ActiveMember.OrganizationSlug != "acme-events" {
		t.Fatalf("active org slug=%q", session.ActiveMember.OrganizationSlug)
	}
	if session.ActiveMember.Role != "org_admin" {
		t.Fatalf("active role=%q", session.ActiveMember.Role)
	}
	if len(session.Memberships) != 1 || session.Memberships[0].OrganizationSlug != "acme-events" {
		t.Fatalf("memberships=%+v", session.Memberships)
	}

	resp, body = env.get(t, "/api/v1/auth/session", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get session status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if err := json.Unmarshal(body.Data, &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if session.ActiveMember == nil || session.ActiveMember.OrganizationSlug != "acme-events" {
		t.Fatalf("session after create: %+v", session.ActiveMember)
	}
}

func TestDuplicateOrganizationSlug(t *testing.T) {
	env := setupTest(t)
	sessionID := verifyOTP(t, env, "slug-taken@example.com")

	resp, body := env.post(t, "/api/v1/staff/organizations", map[string]string{
		"name": "Duplicate Slug",
		"slug": "demo-venue",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "ORGANIZATION_SLUG_TAKEN" {
		t.Fatalf("expected ORGANIZATION_SLUG_TAKEN, got %+v", body.Error)
	}
}

func TestPreSeededSingleMemberAutoSelect(t *testing.T) {
	env := setupTest(t)

	_, _ = env.post(t, "/api/v1/auth/otp/request", map[string]string{
		"email": "preseeded@example.com",
	}, nil)

	resp, body := env.post(t, "/api/v1/auth/otp/verify", map[string]string{
		"email": "preseeded@example.com",
		"code":  env.email.LastCode,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify otp status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var verifyData struct {
		SessionID string `json:"session_id"`
		Session   struct {
			ActiveMember *struct {
				OrganizationSlug string `json:"organization_slug"`
				Role             string `json:"role"`
			} `json:"active_member"`
			Memberships []struct {
				OrganizationSlug string `json:"organization_slug"`
			} `json:"memberships"`
		} `json:"session"`
	}
	if err := json.Unmarshal(body.Data, &verifyData); err != nil {
		t.Fatalf("decode verify data: %v", err)
	}
	if verifyData.SessionID == "" {
		t.Fatal("expected session_id")
	}
	if len(verifyData.Session.Memberships) != 1 {
		t.Fatalf("expected 1 membership, got %d", len(verifyData.Session.Memberships))
	}
	if verifyData.Session.Memberships[0].OrganizationSlug != "demo-venue" {
		t.Fatalf("membership slug=%q", verifyData.Session.Memberships[0].OrganizationSlug)
	}
	if verifyData.Session.ActiveMember == nil {
		t.Fatal("expected active_member auto-selected")
	}
	if verifyData.Session.ActiveMember.OrganizationSlug != "demo-venue" {
		t.Fatalf("active org slug=%q", verifyData.Session.ActiveMember.OrganizationSlug)
	}
	if verifyData.Session.ActiveMember.Role != "org_admin" {
		t.Fatalf("active role=%q", verifyData.Session.ActiveMember.Role)
	}

	resp, body = env.get(t, "/api/v1/staff/memberships", authHeader(verifyData.SessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list memberships status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var memberships []struct {
		OrganizationSlug string `json:"organization_slug"`
	}
	if err := json.Unmarshal(body.Data, &memberships); err != nil {
		t.Fatalf("decode memberships: %v", err)
	}
	if len(memberships) != 1 || memberships[0].OrganizationSlug != "demo-venue" {
		t.Fatalf("memberships=%+v", memberships)
	}
}

func verifyOTP(t *testing.T, env *testEnv, email string) string {
	t.Helper()

	_, _ = env.post(t, "/api/v1/auth/otp/request", map[string]string{
		"email": email,
	}, nil)

	resp, body := env.post(t, "/api/v1/auth/otp/verify", map[string]string{
		"email": email,
		"code":  env.email.LastCode,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify otp status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var verifyData struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(body.Data, &verifyData); err != nil {
		t.Fatalf("decode verify data: %v", err)
	}
	if verifyData.SessionID == "" {
		t.Fatal("expected session_id")
	}
	return verifyData.SessionID
}

func seedMultiMembership(t *testing.T, env *testEnv, email string) (memberID1, memberID2 string) {
	t.Helper()
	// No API exists yet to provision a second Organization membership for the same email.
	ctx := context.Background()

	var orgNorthID, orgSouthID string
	if err := env.db.QueryRowContext(ctx, `
		INSERT INTO organizations (name, slug, created_at)
		VALUES ('North Hall', 'north-hall', $1)
		RETURNING id
	`, env.fixedClock).Scan(&orgNorthID); err != nil {
		t.Fatalf("insert org north hall: %v", err)
	}

	if err := env.db.QueryRowContext(ctx, `
		INSERT INTO organizations (name, slug, created_at)
		VALUES ('South Hall', 'south-hall', $1)
		RETURNING id
	`, env.fixedClock).Scan(&orgSouthID); err != nil {
		t.Fatalf("insert org south hall: %v", err)
	}

	if err := env.db.QueryRowContext(ctx, `
		INSERT INTO members (organization_id, email, role, created_at)
		VALUES ($1, $2, 'org_admin', $3)
		RETURNING id
	`, orgNorthID, email, env.fixedClock).Scan(&memberID1); err != nil {
		t.Fatalf("insert member north: %v", err)
	}

	if err := env.db.QueryRowContext(ctx, `
		INSERT INTO members (organization_id, email, role, created_at)
		VALUES ($1, $2, 'org_admin', $3)
		RETURNING id
	`, orgSouthID, email, env.fixedClock).Scan(&memberID2); err != nil {
		t.Fatalf("insert member south: %v", err)
	}

	return memberID1, memberID2
}

func TestTwoMembershipPickerFlow(t *testing.T) {
	env := setupTest(t)
	email := "multi@example.com"
	memberNorth, memberSouth := seedMultiMembership(t, env, email)

	_, _ = env.post(t, "/api/v1/auth/otp/request", map[string]string{"email": email}, nil)
	resp, body := env.post(t, "/api/v1/auth/otp/verify", map[string]string{
		"email": email,
		"code":  env.email.LastCode,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify otp status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var verifyData struct {
		SessionID string `json:"session_id"`
		Session   struct {
			ActiveMember *struct {
				MemberID string `json:"member_id"`
			} `json:"active_member"`
			Memberships []struct {
				MemberID string `json:"member_id"`
			} `json:"memberships"`
		} `json:"session"`
	}
	if err := json.Unmarshal(body.Data, &verifyData); err != nil {
		t.Fatalf("decode verify data: %v", err)
	}
	if verifyData.Session.ActiveMember != nil {
		t.Fatal("expected no auto-select with multiple memberships")
	}
	if len(verifyData.Session.Memberships) != 2 {
		t.Fatalf("expected 2 memberships, got %d", len(verifyData.Session.Memberships))
	}

	resp, body = env.post(t, "/api/v1/staff/session/organization", map[string]string{
		"member_id": memberNorth,
	}, authHeader(verifyData.SessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("select org status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var session struct {
		ActiveMember *struct {
			MemberID         string `json:"member_id"`
			OrganizationSlug string `json:"organization_slug"`
		} `json:"active_member"`
	}
	if err := json.Unmarshal(body.Data, &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if session.ActiveMember == nil || session.ActiveMember.MemberID != memberNorth {
		t.Fatalf("expected active member north, got %+v", session.ActiveMember)
	}
	if session.ActiveMember.OrganizationSlug != "north-hall" {
		t.Fatalf("active org slug=%q", session.ActiveMember.OrganizationSlug)
	}

	resp, body = env.get(t, "/api/v1/staff/me", authHeader(verifyData.SessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("staff me status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var me struct {
		MemberID         string `json:"member_id"`
		OrganizationSlug string `json:"organization_slug"`
	}
	if err := json.Unmarshal(body.Data, &me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if me.MemberID != memberNorth {
		t.Fatalf("me member_id=%q", me.MemberID)
	}

	resp, body = env.post(t, "/api/v1/staff/session/organization", map[string]string{
		"member_id": memberSouth,
	}, authHeader(verifyData.SessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("switch org status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if err := json.Unmarshal(body.Data, &session); err != nil {
		t.Fatalf("decode session after switch: %v", err)
	}
	if session.ActiveMember == nil || session.ActiveMember.MemberID != memberSouth {
		t.Fatalf("expected active member south, got %+v", session.ActiveMember)
	}
	if session.ActiveMember.OrganizationSlug != "south-hall" {
		t.Fatalf("active org slug after switch=%q", session.ActiveMember.OrganizationSlug)
	}
}

func TestStaffWorkflowForbiddenWithoutActiveMember(t *testing.T) {
	env := setupTest(t)
	email := "multi@example.com"
	seedMultiMembership(t, env, email)
	sessionID := verifyOTP(t, env, email)

	resp, body := env.get(t, "/api/v1/staff/me", authHeader(sessionID))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN, got %+v", body.Error)
	}
}

func TestStaffWorkflowUnauthorizedWithoutSession(t *testing.T) {
	env := setupTest(t)

	resp, body := env.get(t, "/api/v1/staff/me", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "UNAUTHORIZED" {
		t.Fatalf("expected UNAUTHORIZED, got %+v", body.Error)
	}
}

func TestSessionExtensionOnUse(t *testing.T) {
	env := setupTest(t)
	sessionID := verifyOTP(t, env, "staff@example.com")

	var expiresBefore time.Time
	if err := env.db.QueryRowContext(context.Background(), `
		SELECT expires_at FROM sessions WHERE id = $1
	`, sessionID).Scan(&expiresBefore); err != nil {
		t.Fatalf("query expires_at: %v", err)
	}

	expectedBefore := env.fixedClock.Add(14 * 24 * time.Hour)
	if !expiresBefore.Equal(expectedBefore) {
		t.Fatalf("initial expires_at=%v want=%v", expiresBefore, expectedBefore)
	}

	later := env.fixedClock.Add(2 * 24 * time.Hour)
	env.service.WithClock(func() time.Time { return later })

	resp, body := env.get(t, "/api/v1/auth/session", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get session status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var expiresAfter time.Time
	if err := env.db.QueryRowContext(context.Background(), `
		SELECT expires_at FROM sessions WHERE id = $1
	`, sessionID).Scan(&expiresAfter); err != nil {
		t.Fatalf("query expires_at after: %v", err)
	}

	expectedAfter := later.Add(14 * 24 * time.Hour)
	if !expiresAfter.Equal(expectedAfter) {
		t.Fatalf("extended expires_at=%v want=%v", expiresAfter, expectedAfter)
	}
}
