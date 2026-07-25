package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Google Sign-In on the Staff app (ADR 0011, PRD google-sign-in decisions 3, 5,
// 6, 7). Google proves the email; it is not an identity. Everything past that
// proof is the passcode path — the same `sessions` row, the same fourteen-day
// sliding window, the same auto-selection of a lone membership and therefore the
// same auth-fork — so most of what these tests assert is sameness: that a
// Google-minted Staff Session is indistinguishable from a passcode-minted one,
// and that the sign-in method never changes where somebody lands.
//
// Google itself is stood in for by the same stub token endpoint the customer
// tests use, reached through the configurable GOOGLE_TOKEN_ENDPOINT. Note what
// is NOT asserted here: that a Storefront code is unredeemable at this endpoint.
// That isolation comes from Google binding an authorization code to its issuing
// client, so against a stub configured with both clients it would prove only
// that the stub was configured with both clients. It is covered by the
// first-deploy runbook, exactly as it is for the Storefront.

const (
	staffGoogleVerifyPath = "/api/v1/auth/google/verify"
	staffSessionPath      = "/api/v1/auth/session"
	staffLogoutPath       = "/api/v1/auth/logout"

	// The Staff app's Google OAuth client, as the harness configures the API with
	// it. A different client from the Storefront's, and the stub asserts the
	// staff exchange presents exactly these.
	staffGoogleClientID     = "staff-client-id.apps.googleusercontent.com"
	staffGoogleClientSecret = "staff-client-secret"

	// The staff redirect URI, which is the Staff app's own callback route and not
	// the Storefront's — Google matches it against the one registered for this
	// client.
	staffGoogleRedirectURI = "http://localhost:64301/api/auth/google/callback"

	staffSessionDuration = 14 * 24 * time.Hour
)

// verifiedStaffGoogleClaims is what Google returns for an account whose address
// it vouches for, issued to the staff client.
func verifiedStaffGoogleClaims(email string) map[string]any {
	return map[string]any{
		"iss":            "https://accounts.google.com",
		"aud":            staffGoogleClientID,
		"sub":            "998877665544332211000",
		"email":          email,
		"email_verified": true,
		"exp":            fixedClock.Add(time.Hour).Unix(),
		"iat":            fixedClock.Unix(),
	}
}

// staffVerifyData is the verify response, which is byte-identical in shape to
// the passcode path's: a session view and a token, and nothing that says which
// door was used.
type staffVerifyData struct {
	Session   staffSessionView `json:"session"`
	SessionID string           `json:"session_id"`
}

type staffSessionView struct {
	Email        string `json:"email"`
	ActiveMember *struct {
		MemberID         string `json:"member_id"`
		OrganizationSlug string `json:"organization_slug"`
	} `json:"active_member"`
	Memberships []struct {
		MemberID         string `json:"member_id"`
		OrganizationSlug string `json:"organization_slug"`
	} `json:"memberships"`
}

// postStaffGoogleVerify relays an authorization code the way the Staff app's
// callback route does: a code, the PKCE verifier, and the redirect URI, and no
// email address anywhere.
func postStaffGoogleVerify(t *testing.T, env *testEnv) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, staffGoogleVerifyPath, map[string]string{
		"code":          googleAuthCode,
		"code_verifier": googleCodeVerifier,
		"redirect_uri":  staffGoogleRedirectURI,
	}, nil)
}

func decodeStaffVerify(t *testing.T, body envelope) staffVerifyData {
	t.Helper()
	var data staffVerifyData
	if err := json.Unmarshal(body.Data, &data); err != nil {
		t.Fatalf("decode verify data: %v", err)
	}
	return data
}

// staffGoogleSignIn completes a Google Sign-In for an address Google vouches for
// and returns the whole verify payload — the Google-side counterpart of
// verifyOTP, which the auth-fork tests below read the session view from.
func staffGoogleSignIn(t *testing.T, env *testEnv, email string) staffVerifyData {
	t.Helper()
	googleStub.returns(verifiedStaffGoogleClaims(email))

	resp, body := postStaffGoogleVerify(t, env)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("staff google verify status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return decodeStaffVerify(t, body)
}

// staffSessionExpiry reads a Staff Session's sliding expiry. SQL because the
// expiry is deliberately not on the wire.
func staffSessionExpiry(t *testing.T, env *testEnv, token string) time.Time {
	t.Helper()
	var expiresAt time.Time
	if err := env.db.QueryRowContext(context.Background(),
		`SELECT expires_at FROM sessions WHERE id = $1`, token).Scan(&expiresAt); err != nil {
		t.Fatalf("read staff session expiry: %v", err)
	}
	return expiresAt.UTC()
}

func countStaffSessions(t *testing.T, env *testEnv) int {
	t.Helper()
	var n int
	if err := env.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatalf("count staff sessions: %v", err)
	}
	return n
}

// TestStaffGoogleSignInMintsAStaffSession proves the happy path: the exchange
// presents the STAFF client's own credentials — not the Storefront's — with the
// PKCE verifier and the staff redirect URI, and what comes back is an ordinary
// Staff Session on the address Google vouched for, with the same fourteen-day
// window a passcode earns.
func TestStaffGoogleSignInMintsAStaffSession(t *testing.T) {
	env := setupTest(t)
	googleStub.returns(verifiedStaffGoogleClaims("member@example.com"))

	resp, body := postStaffGoogleVerify(t, env)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("error=%+v, want none", body.Error)
	}

	// What the API asked Google. The staff client id and secret are the half of
	// cross-surface isolation this code is answerable for: presenting the
	// Storefront's here would be the bug ADR 0011 is written against.
	form := googleStub.exchangeForm()
	for field, want := range map[string]string{
		"grant_type":    "authorization_code",
		"code":          googleAuthCode,
		"code_verifier": googleCodeVerifier,
		"redirect_uri":  staffGoogleRedirectURI,
		"client_id":     staffGoogleClientID,
		"client_secret": staffGoogleClientSecret,
	} {
		if got := form.Get(field); got != want {
			t.Fatalf("exchange %s = %q, want %q", field, got, want)
		}
	}
	if form.Get("client_id") == storefrontGoogleClientID {
		t.Fatal("the staff endpoint exchanged against the Storefront client")
	}

	data := decodeStaffVerify(t, body)
	if data.SessionID == "" {
		t.Fatal("expected a Staff Session token")
	}
	if data.Session.Email != "member@example.com" {
		t.Fatalf("session email = %q, want member@example.com", data.Session.Email)
	}

	// The session is real and reads like any other.
	resp, body = env.get(t, staffSessionPath, authHeader(data.SessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session read status=%d error=%+v", resp.StatusCode, body.Error)
	}

	wantExpiry := env.fixedClock.Add(staffSessionDuration)
	if got := staffSessionExpiry(t, env, data.SessionID); !got.Equal(wantExpiry) {
		t.Fatalf("session expires_at = %s, want %s", got, wantExpiry)
	}
}

// TestStaffGoogleSignInNormalisesTheEmail proves NormalizeEmail is the one rule
// here too: a differently-cased Google address reaches the same `members` row,
// so it signs in with its Organization rather than as a stranger.
func TestStaffGoogleSignInNormalisesTheEmail(t *testing.T) {
	env := setupTest(t)

	data := staffGoogleSignIn(t, env, "PreSeeded@Example.COM")

	if data.Session.Email != "preseeded@example.com" {
		t.Fatalf("session email = %q, want the normalised preseeded@example.com", data.Session.Email)
	}
	if len(data.Session.Memberships) != 1 {
		t.Fatalf("memberships = %d, want 1 — letter case must not hide a membership", len(data.Session.Memberships))
	}
}

// TestStaffGoogleSignInAutoSelectsALoneMembership is acceptance criterion two: a
// Member of one Organization has it selected already, which is what lands them
// on the dashboard rather than the picker. The auto-select lives in the shared
// sign-in tail, so it cannot know a Google Sign-In from a passcode.
func TestStaffGoogleSignInAutoSelectsALoneMembership(t *testing.T) {
	env := setupTest(t)

	// The pre-seeded member of exactly one Organization.
	data := staffGoogleSignIn(t, env, "preseeded@example.com")

	if data.Session.ActiveMember == nil {
		t.Fatal("active_member is null after a Google Sign-In by a Member of one Organization")
	}
	if data.Session.ActiveMember.OrganizationSlug != "demo-venue" {
		t.Fatalf("active organization = %q, want demo-venue", data.Session.ActiveMember.OrganizationSlug)
	}
	if len(data.Session.Memberships) != 1 {
		t.Fatalf("memberships = %d, want 1", len(data.Session.Memberships))
	}

	// And identical to what the passcode door yields for the same person.
	passcodeSession := verifyOTP(t, env, "preseeded@example.com")
	resp, body := env.get(t, staffSessionPath, authHeader(passcodeSession))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session read status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var passcodeView staffSessionView
	if err := json.Unmarshal(body.Data, &passcodeView); err != nil {
		t.Fatalf("decode session view: %v", err)
	}
	if passcodeView.ActiveMember == nil ||
		passcodeView.ActiveMember.MemberID != data.Session.ActiveMember.MemberID {
		t.Fatalf("passcode active_member = %+v, want the same as Google's %+v",
			passcodeView.ActiveMember, data.Session.ActiveMember)
	}
}

// TestStaffGoogleSignInLeavesSeveralMembershipsUnselected is acceptance
// criterion three: nothing is chosen for a Member of several Organizations, which
// is what sends them to the picker.
func TestStaffGoogleSignInLeavesSeveralMembershipsUnselected(t *testing.T) {
	env := setupTest(t)
	seedMultiMembership(t, env, "multi@example.com")

	data := staffGoogleSignIn(t, env, "multi@example.com")

	if data.Session.ActiveMember != nil {
		t.Fatalf("active_member = %+v, want null — a Member of several must choose", data.Session.ActiveMember)
	}
	if len(data.Session.Memberships) != 2 {
		t.Fatalf("memberships = %d, want 2", len(data.Session.Memberships))
	}
}

// TestStaffGoogleSignInWithNoMembershipLandsOnTheCreationFork is acceptance
// criterion four, as far as the API can see it: an address Google vouches for
// that no `members` row matches gets a perfectly good Staff Session with nothing
// on it. That empty session is what the app reads to show the
// create-organization fork, and the different-address copy with it — the
// mismatch of PRD decision 5, which Google makes more visible and does not
// cause.
func TestStaffGoogleSignInWithNoMembershipLandsOnTheCreationFork(t *testing.T) {
	env := setupTest(t)

	data := staffGoogleSignIn(t, env, "stranger@example.com")

	if data.SessionID == "" {
		t.Fatal("expected a Staff Session even with no membership")
	}
	if data.Session.ActiveMember != nil {
		t.Fatalf("active_member = %+v, want null", data.Session.ActiveMember)
	}
	if len(data.Session.Memberships) != 0 {
		t.Fatalf("memberships = %d, want 0", len(data.Session.Memberships))
	}

	// The session is usable: this person can create an Organization, exactly as a
	// passcode sign-in for an unknown address can.
	createOrganization(t, env, data.SessionID, "Stranger Hall", "stranger-hall")
}

// TestStaffGoogleSignInRefusesAnUnverifiedEmail proves the claim is the whole of
// what is consumed: an address Google will not vouch for opens no session.
func TestStaffGoogleSignInRefusesAnUnverifiedEmail(t *testing.T) {
	env := setupTest(t)
	claims := verifiedStaffGoogleClaims("unverified@example.com")
	claims["email_verified"] = false
	googleStub.returns(claims)

	resp, body := postStaffGoogleVerify(t, env)
	assertAPIError(t, resp, body, http.StatusUnauthorized, "GOOGLE_SIGN_IN_FAILED")

	if n := countStaffSessions(t, env); n != 0 {
		t.Fatalf("staff sessions = %d, want 0 — a refused Google Sign-In must write no row", n)
	}
}

// TestStaffGoogleSignInRefusesATokenWithNoEmailClaim proves there is no degraded
// mode when the one claim being consumed is absent.
func TestStaffGoogleSignInRefusesATokenWithNoEmailClaim(t *testing.T) {
	env := setupTest(t)
	claims := verifiedStaffGoogleClaims("")
	delete(claims, "email")
	googleStub.returns(claims)

	resp, body := postStaffGoogleVerify(t, env)
	assertAPIError(t, resp, body, http.StatusUnauthorized, "GOOGLE_SIGN_IN_FAILED")

	if n := countStaffSessions(t, env); n != 0 {
		t.Fatalf("staff sessions = %d, want 0", n)
	}
}

// TestStaffGoogleSignInFailuresAreIndistinguishable proves the endpoint is not
// an enumeration oracle, and the question is sharper here than on the
// Storefront: an answer that differed for a known address would say which
// addresses hold authority over an Organization. A code Google rejected, an
// unverified address that is a Member, and an unverified address nobody has
// heard of all answer byte-for-byte the same.
func TestStaffGoogleSignInFailuresAreIndistinguishable(t *testing.T) {
	env := setupTest(t)

	// Google rejects the exchange outright.
	googleStub.rejects()
	rejectedResp, rejectedBody := postStaffGoogleVerify(t, env)
	assertAPIError(t, rejectedResp, rejectedBody, http.StatusUnauthorized, "GOOGLE_SIGN_IN_FAILED")

	// A Member's address whose Google account is unverified.
	memberClaims := verifiedStaffGoogleClaims("preseeded@example.com")
	memberClaims["email_verified"] = false
	googleStub.returns(memberClaims)
	memberResp, memberBody := postStaffGoogleVerify(t, env)

	// An address that is a Member of nothing, likewise unverified.
	strangerClaims := verifiedStaffGoogleClaims("stranger@example.com")
	strangerClaims["email_verified"] = false
	googleStub.returns(strangerClaims)
	strangerResp, strangerBody := postStaffGoogleVerify(t, env)

	if memberResp.StatusCode != rejectedResp.StatusCode || strangerResp.StatusCode != rejectedResp.StatusCode {
		t.Fatalf("statuses rejected=%d member=%d stranger=%d — every failure must look the same",
			rejectedResp.StatusCode, memberResp.StatusCode, strangerResp.StatusCode)
	}
	for _, other := range []envelope{memberBody, strangerBody} {
		if *other.Error != *rejectedBody.Error {
			t.Fatalf("error = %+v, want the same as %+v — the failure must not say which address it was",
				other.Error, rejectedBody.Error)
		}
	}

	if n := countStaffSessions(t, env); n != 0 {
		t.Fatalf("staff sessions = %d, want 0", n)
	}
}

// TestStaffGoogleMintedSessionBehavesLikeAPasscodeMintedOne is the point of the
// whole feature: two doors, one room. The two sessions are read, slid and
// destroyed the same way, and nothing on the wire says which door was used.
func TestStaffGoogleMintedSessionBehavesLikeAPasscodeMintedOne(t *testing.T) {
	env := setupTest(t)

	googleToken := staffGoogleSignIn(t, env, "google-member@example.com").SessionID
	passcodeToken := verifyOTP(t, env, "passcode-member@example.com")

	readSession := func(token string) staffSessionView {
		t.Helper()
		resp, body := env.get(t, staffSessionPath, authHeader(token))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("session read status=%d error=%+v", resp.StatusCode, body.Error)
		}
		var view staffSessionView
		if err := json.Unmarshal(body.Data, &view); err != nil {
			t.Fatalf("decode session view: %v", err)
		}
		return view
	}

	googleView := readSession(googleToken)
	passcodeView := readSession(passcodeToken)
	if googleView.ActiveMember != nil || passcodeView.ActiveMember != nil {
		t.Fatal("neither session should have an active member")
	}
	if len(googleView.Memberships) != 0 || len(passcodeView.Memberships) != 0 {
		t.Fatal("neither session should have memberships")
	}

	// Sliding expiry: an authenticated read pushes the fourteen-day window out
	// from the moment of use, on both.
	later := env.fixedClock.Add(2 * 24 * time.Hour)
	env.service.WithClock(func() time.Time { return later })
	_ = readSession(googleToken)
	_ = readSession(passcodeToken)

	wantExpiry := later.Add(staffSessionDuration)
	for name, token := range map[string]string{"google": googleToken, "passcode": passcodeToken} {
		if got := staffSessionExpiry(t, env, token); !got.Equal(wantExpiry) {
			t.Fatalf("%s session expires_at = %s, want %s", name, got, wantExpiry)
		}
	}

	// And sign-out kills the Google-minted session as dead as any other.
	resp, body := env.post(t, staffLogoutPath, nil, authHeader(googleToken))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body = env.get(t, staffSessionPath, authHeader(googleToken))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session read after logout status=%d error=%+v, want 401", resp.StatusCode, body.Error)
	}

	// The passcode session is untouched by the other's sign-out.
	if got := readSession(passcodeToken); got.Email != "passcode-member@example.com" {
		t.Fatalf("passcode session email = %q, want passcode-member@example.com", got.Email)
	}
}

// TestStaffGoogleVerifyRequiresTheRelayedFields proves the request is the three
// things the Staff app can honestly supply — and that an email address is not
// among them anywhere.
func TestStaffGoogleVerifyRequiresTheRelayedFields(t *testing.T) {
	env := setupTest(t)

	resp, body := env.post(t, staffGoogleVerifyPath, map[string]string{
		"code": googleAuthCode,
	}, nil)
	assertAPIError(t, resp, body, http.StatusBadRequest, "VALIDATION_FAILED")

	if n := countStaffSessions(t, env); n != 0 {
		t.Fatalf("staff sessions = %d, want 0", n)
	}
}
