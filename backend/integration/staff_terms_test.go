package integration

// The Staff platform's first and only consent gate (#538, parent #533,
// ADR 0066), tested at the identity-service sign-in seam: a sign-in either
// mints a Staff Session or returns what is owed, an acceptance either exists
// with the right edition or doesn't, and nothing here asserts on wiring.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// staffTermsRequiredView is the terms-required outcome as the wire carries it.
type staffTermsRequiredView struct {
	PendingTermsToken string `json:"pending_terms_token"`
	ExpiresAt         string `json:"expires_at"`
	Version           string `json:"version"`
	AcceptanceLabel   string `json:"acceptance_label"`
}

// staffSignInOutcome is a verify (or accept) response's data.
type staffSignInOutcome struct {
	SessionID     string                  `json:"session_id"`
	TermsRequired *staffTermsRequiredView `json:"terms_required"`
}

func decodeStaffSignInOutcome(t *testing.T, body envelope) staffSignInOutcome {
	t.Helper()
	var outcome staffSignInOutcome
	if err := json.Unmarshal(body.Data, &outcome); err != nil {
		t.Fatalf("decode sign-in outcome: %v", err)
	}
	return outcome
}

func acceptStaffTerms(t *testing.T, env *testEnv, token string, accepted bool) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, "/api/v1/auth/terms/accept", map[string]any{
		"pending_terms_token": token,
		"terms_acceptance":    accepted,
	}, nil)
}

// finishTermsGate takes a verify response and returns the envelope that
// carries the session: the acceptance response when the sign-in was gated, the
// verify response itself when it was not. Tests that assert session shapes
// read them off the result exactly as they did before the gate existed.
func finishTermsGate(t *testing.T, env *testEnv, body envelope) envelope {
	t.Helper()
	outcome := decodeStaffSignInOutcome(t, body)
	if outcome.TermsRequired == nil {
		return body
	}
	resp, accepted := acceptStaffTerms(t, env, outcome.TermsRequired.PendingTermsToken, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept terms status=%d error=%+v", resp.StatusCode, accepted.Error)
	}
	return accepted
}

// sessionThroughTermsGate finishes a possibly-gated sign-in and returns the
// Staff Session it minted.
func sessionThroughTermsGate(t *testing.T, env *testEnv, body envelope) string {
	t.Helper()
	return decodeStaffSignInOutcome(t, finishTermsGate(t, env, body)).SessionID
}

// requestAndVerify runs the passcode half of a sign-in and returns the verify
// response untouched, so a test can assert on the outcome itself.
func requestAndVerify(t *testing.T, env *testEnv, email string) envelope {
	t.Helper()
	return requestAndVerifyIn(t, env, email, "")
}

// requestAndVerifyIn is the same sign-in with a detected page language on it —
// the language the terms step is worded in (#538, ADR 0066).
func requestAndVerifyIn(t *testing.T, env *testEnv, email, locale string) envelope {
	t.Helper()
	_, _ = env.post(t, "/api/v1/auth/otp/request", map[string]string{"email": email}, nil)
	verify := map[string]string{
		"email": email,
		"code":  env.email.LastCode,
	}
	if locale != "" {
		verify["locale"] = locale
	}
	resp, body := env.post(t, "/api/v1/auth/otp/verify", verify, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify otp status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return body
}

func countStaffTermsAcceptances(t *testing.T, env *testEnv, email string) int {
	t.Helper()
	var count int
	if err := env.db.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM staff_terms_acceptances WHERE email = $1
	`, email).Scan(&count); err != nil {
		t.Fatalf("count acceptances: %v", err)
	}
	return count
}

// TestStaffTermsGateFirstSignIn: a proven email with no acceptance of the
// current edition is minted NO session and owes the terms box; an unticked
// submission is refused and spends the token; a fresh sign-in's acceptance
// mints the session and appends exactly one evidence row; the second sign-in
// sails through.
func TestStaffTermsGateFirstSignIn(t *testing.T) {
	env := setupTest(t)
	email := "gated@example.com"

	body := requestAndVerify(t, env, email)
	outcome := decodeStaffSignInOutcome(t, body)
	if outcome.SessionID != "" {
		t.Fatalf("expected no session while terms outstanding, got %q", outcome.SessionID)
	}
	if outcome.TermsRequired == nil {
		t.Fatal("expected terms_required on first sign-in")
	}
	if outcome.TermsRequired.Version != "1" {
		t.Fatalf("terms version=%q, want 1", outcome.TermsRequired.Version)
	}
	// English: no locale was detected on this sign-in, and the label is floored
	// at the platform's default language rather than left blank.
	if !strings.Contains(outcome.TermsRequired.AcceptanceLabel, "Terms and Conditions") {
		t.Fatalf("acceptance label does not name the terms: %q", outcome.TermsRequired.AcceptanceLabel)
	}
	if outcome.TermsRequired.PendingTermsToken == "" {
		t.Fatal("expected pending_terms_token")
	}
	if countStaffTermsAcceptances(t, env, email) != 0 {
		t.Fatal("no acceptance may exist before the box is ticked")
	}

	// The required box, refused by the API — and the refusal spends the token,
	// so the same proof cannot be retried.
	resp, refused := acceptStaffTerms(t, env, outcome.TermsRequired.PendingTermsToken, false)
	if resp.StatusCode != http.StatusBadRequest || refused.Error == nil || refused.Error.Code != "TERMS_ACCEPTANCE_REQUIRED" {
		t.Fatalf("unticked box: status=%d error=%+v, want 400 TERMS_ACCEPTANCE_REQUIRED", resp.StatusCode, refused.Error)
	}
	resp, replayed := acceptStaffTerms(t, env, outcome.TermsRequired.PendingTermsToken, true)
	if resp.StatusCode != http.StatusUnauthorized || replayed.Error == nil || replayed.Error.Code != "PENDING_TERMS_INVALID" {
		t.Fatalf("spent token: status=%d error=%+v, want 401 PENDING_TERMS_INVALID", resp.StatusCode, replayed.Error)
	}
	if countStaffTermsAcceptances(t, env, email) != 0 {
		t.Fatal("a refused submission must record nothing")
	}

	// Start again, tick the box: the acceptance mints the session.
	body = requestAndVerify(t, env, email)
	outcome = decodeStaffSignInOutcome(t, body)
	if outcome.TermsRequired == nil {
		t.Fatal("expected terms_required again after an abandoned step")
	}
	resp, acceptedBody := acceptStaffTerms(t, env, outcome.TermsRequired.PendingTermsToken, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept status=%d error=%+v", resp.StatusCode, acceptedBody.Error)
	}
	accepted := decodeStaffSignInOutcome(t, acceptedBody)
	if accepted.SessionID == "" || accepted.TermsRequired != nil {
		t.Fatalf("acceptance must mint the session and owe nothing: %+v", accepted)
	}

	// Exactly one append-only row: the edition, the capacity, the clock, and
	// the session the act minted.
	var (
		count      int
		label      string
		capacity   string
		acceptedAt time.Time
		sessionID  *string
	)
	if err := env.db.QueryRowContext(context.Background(), `
		SELECT COUNT(*) OVER (), tv.label, a.capacity, a.accepted_at, a.session_id
		FROM staff_terms_acceptances a
		JOIN terms_versions tv ON tv.id = a.terms_version_id
		WHERE a.email = $1
	`, email).Scan(&count, &label, &capacity, &acceptedAt, &sessionID); err != nil {
		t.Fatalf("read acceptance row: %v", err)
	}
	if count != 1 {
		t.Fatalf("acceptance rows=%d, want exactly 1", count)
	}
	if label != "1" {
		t.Fatalf("accepted edition=%q, want 1", label)
	}
	if capacity != "organizer" {
		t.Fatalf("capacity=%q, want organizer", capacity)
	}
	if !acceptedAt.Equal(env.fixedClock) {
		t.Fatalf("accepted_at=%v, want the fixed clock %v", acceptedAt, env.fixedClock)
	}
	if sessionID == nil || *sessionID != accepted.SessionID {
		t.Fatalf("evidence session_id=%v, want the minted session %q", sessionID, accepted.SessionID)
	}

	// The gate costs one click per person per edition: the second sign-in
	// sails through, and appends nothing.
	second := decodeStaffSignInOutcome(t, requestAndVerify(t, env, email))
	if second.TermsRequired != nil || second.SessionID == "" {
		t.Fatalf("second sign-in must mint a session with no terms step: %+v", second)
	}
	if countStaffTermsAcceptances(t, env, email) != 1 {
		t.Fatal("a second sign-in must not append a second acceptance")
	}
}

// TestStaffTermsOneAcceptancePerEmailAcrossOrganizations: the acceptance is
// keyed on the person, not the membership — several Organizations, one row,
// one ask.
func TestStaffTermsOneAcceptancePerEmailAcrossOrganizations(t *testing.T) {
	env := setupTest(t)
	email := "multi-org@example.com"
	seedMultiMembership(t, env, email)

	sessionID := sessionThroughTermsGate(t, env, requestAndVerify(t, env, email))
	if sessionID == "" {
		t.Fatal("expected a session")
	}
	if countStaffTermsAcceptances(t, env, email) != 1 {
		t.Fatalf("acceptances=%d, want 1 for a person with two Organizations", countStaffTermsAcceptances(t, env, email))
	}

	second := decodeStaffSignInOutcome(t, requestAndVerify(t, env, email))
	if second.TermsRequired != nil {
		t.Fatal("a person with several Organizations is asked once, not per membership")
	}
	if len(secondMemberships(t, env, second.SessionID)) != 2 {
		t.Fatal("expected both memberships on the ungated session")
	}
}

func secondMemberships(t *testing.T, env *testEnv, sessionID string) []json.RawMessage {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/memberships", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("memberships status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var memberships []json.RawMessage
	if err := json.Unmarshal(body.Data, &memberships); err != nil {
		t.Fatalf("decode memberships: %v", err)
	}
	return memberships
}

// TestStaffTermsGatesPlatformOperators: operator authority is orthogonal to
// Membership and buys no way past the gate — every human on the Staff platform
// accepts.
func TestStaffTermsGatesPlatformOperators(t *testing.T) {
	env := setupTest(t)
	email := "operator@example.com"
	if _, err := env.db.ExecContext(context.Background(),
		`INSERT INTO platform_operators (email) VALUES ($1) ON CONFLICT (email) DO NOTHING`, email); err != nil {
		t.Fatalf("seed platform operator: %v", err)
	}

	outcome := decodeStaffSignInOutcome(t, requestAndVerify(t, env, email))
	if outcome.TermsRequired == nil || outcome.SessionID != "" {
		t.Fatalf("an operator's first sign-in must be gated: %+v", outcome)
	}

	resp, acceptedBody := acceptStaffTerms(t, env, outcome.TermsRequired.PendingTermsToken, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("operator accept status=%d error=%+v", resp.StatusCode, acceptedBody.Error)
	}
	var accepted struct {
		SessionID string `json:"session_id"`
		Session   struct {
			IsPlatformOperator bool `json:"is_platform_operator"`
		} `json:"session"`
	}
	if err := json.Unmarshal(acceptedBody.Data, &accepted); err != nil {
		t.Fatalf("decode operator outcome: %v", err)
	}
	if accepted.SessionID == "" || !accepted.Session.IsPlatformOperator {
		t.Fatalf("expected an operator session past the gate: %+v", accepted)
	}
}

// TestStaffTermsNewEditionRegates: inserting a later Terms Versions row makes
// every stored reference stop matching — no code, no data migration — and the
// person accepts again, appending a second row rather than touching the first.
func TestStaffTermsNewEditionRegates(t *testing.T) {
	env := setupTest(t)
	email := "regated@example.com"

	if sessionThroughTermsGate(t, env, requestAndVerify(t, env, email)) == "" {
		t.Fatal("expected a session on edition 1")
	}

	// Edition 2 arrives: same effective date is fine — ties break by insertion
	// order, which is how an edition published today supersedes this morning's.
	if _, err := env.db.ExecContext(context.Background(), `
		INSERT INTO terms_versions (label, effective_date, content_hash)
		VALUES ('2', CURRENT_DATE, $1)
	`, strings.Repeat("ab", 32)); err != nil {
		t.Fatalf("insert edition 2: %v", err)
	}

	outcome := decodeStaffSignInOutcome(t, requestAndVerify(t, env, email))
	if outcome.TermsRequired == nil {
		t.Fatal("a later edition must re-gate the next sign-in")
	}
	if outcome.TermsRequired.Version != "2" {
		t.Fatalf("re-gate names version=%q, want 2", outcome.TermsRequired.Version)
	}

	resp, acceptedBody := acceptStaffTerms(t, env, outcome.TermsRequired.PendingTermsToken, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept edition 2 status=%d error=%+v", resp.StatusCode, acceptedBody.Error)
	}
	if countStaffTermsAcceptances(t, env, email) != 2 {
		t.Fatalf("acceptances=%d, want one per edition", countStaffTermsAcceptances(t, env, email))
	}
}

// TestStaffTermsEditionBumpInsideTokenWindow: the acceptance evidences the
// edition the person was SHOWN, pinned to the pending token — never a re-read
// of whatever is current at the write (#537's held-answer rule, kept on the
// staff side). An edition published between the terms step and the tick means
// the person accepted the superseded text, so the next sign-in re-gates them
// on the new one.
func TestStaffTermsEditionBumpInsideTokenWindow(t *testing.T) {
	env := setupTest(t)
	email := "mid-window@example.com"

	outcome := decodeStaffSignInOutcome(t, requestAndVerify(t, env, email))
	if outcome.TermsRequired == nil || outcome.TermsRequired.Version != "1" {
		t.Fatalf("expected a terms step for edition 1: %+v", outcome.TermsRequired)
	}

	// Edition 2 lands while the person is reading the label.
	if _, err := env.db.ExecContext(context.Background(), `
		INSERT INTO terms_versions (label, effective_date, content_hash)
		VALUES ('2', CURRENT_DATE, $1)
	`, strings.Repeat("cd", 32)); err != nil {
		t.Fatalf("insert edition 2: %v", err)
	}

	resp, acceptedBody := acceptStaffTerms(t, env, outcome.TermsRequired.PendingTermsToken, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept status=%d error=%+v", resp.StatusCode, acceptedBody.Error)
	}
	if decodeStaffSignInOutcome(t, acceptedBody).SessionID == "" {
		t.Fatal("the acceptance of the shown edition still mints the session")
	}

	// Direct SQL because no API discloses which edition an acceptance row
	// names — the id never goes on the wire, and the row IS the assertion.
	var label string
	if err := env.db.QueryRowContext(context.Background(), `
		SELECT tv.label
		FROM staff_terms_acceptances a
		JOIN terms_versions tv ON tv.id = a.terms_version_id
		WHERE a.email = $1
	`, email).Scan(&label); err != nil {
		t.Fatalf("read accepted edition: %v", err)
	}
	if label != "1" {
		t.Fatalf("accepted edition=%q, want the shown edition 1", label)
	}

	// And the new edition is still owed: the next sign-in re-gates on "2".
	regated := decodeStaffSignInOutcome(t, requestAndVerify(t, env, email))
	if regated.TermsRequired == nil || regated.TermsRequired.Version != "2" {
		t.Fatalf("expected a re-gate on edition 2: %+v", regated.TermsRequired)
	}
}

// TestStaffTermsExpiredToken: the held proof is minutes long, and an expired
// token gets the same refusal an unknown one does.
func TestStaffTermsExpiredToken(t *testing.T) {
	env := setupTest(t)
	email := "slow-reader@example.com"

	outcome := decodeStaffSignInOutcome(t, requestAndVerify(t, env, email))
	if outcome.TermsRequired == nil {
		t.Fatal("expected terms_required")
	}

	env.service.WithClock(func() time.Time {
		return env.fixedClock.Add(16 * time.Minute)
	})

	resp, body := acceptStaffTerms(t, env, outcome.TermsRequired.PendingTermsToken, true)
	if resp.StatusCode != http.StatusUnauthorized || body.Error == nil || body.Error.Code != "PENDING_TERMS_INVALID" {
		t.Fatalf("expired token: status=%d error=%+v, want 401 PENDING_TERMS_INVALID", resp.StatusCode, body.Error)
	}
	if countStaffTermsAcceptances(t, env, email) != 0 {
		t.Fatal("an expired step must record nothing")
	}
}

// The terms step is worded in the language the login page is rendered in, and
// the box beside it is the same box either way (#538, ADR 0066).
//
// Which language it is worded in is NOT which text binds: both translations are
// one edition under one fingerprint and the Spanish prevails (§37), so the
// version label is identical and the acceptance a Spanish reader gives is the
// acceptance an English reader gives.
func TestStaffTermsGateIsWordedInThePageLanguage(t *testing.T) {
	env := setupTest(t)

	spanish := decodeStaffSignInOutcome(t, requestAndVerifyIn(t, env, "spanish-reader@example.com", "es"))
	if spanish.TermsRequired == nil {
		t.Fatal("expected terms_required")
	}
	if !strings.Contains(spanish.TermsRequired.AcceptanceLabel, "Términos y Condiciones") {
		t.Errorf("the es label is not in spanish: %q", spanish.TermsRequired.AcceptanceLabel)
	}

	english := decodeStaffSignInOutcome(t, requestAndVerifyIn(t, env, "english-reader@example.com", "en"))
	if english.TermsRequired == nil {
		t.Fatal("expected terms_required")
	}
	if !strings.Contains(english.TermsRequired.AcceptanceLabel, "Terms and Conditions") {
		t.Errorf("the en label is not in english: %q", english.TermsRequired.AcceptanceLabel)
	}

	if spanish.TermsRequired.Version != english.TermsRequired.Version {
		t.Errorf("the two languages offer different editions: %q and %q",
			spanish.TermsRequired.Version, english.TermsRequired.Version)
	}
}
