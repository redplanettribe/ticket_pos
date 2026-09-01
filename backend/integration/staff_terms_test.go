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
	// AdulthoodDeclarationLabel is the second box's words, ABSENT FROM THE
	// PAYLOAD when the edition being shown does not carry the Artifact (#587,
	// ADR 0069) — which is what makes the empty string here mean "this edition
	// does not ask" rather than "asks with nothing written beside the box".
	AdulthoodDeclarationLabel string `json:"adulthood_declaration_label"`
	// LabelLocale is the language the label was ACTUALLY served in — the
	// requested one, or the prevailing one when this edition does not publish it
	// (#559's floor, told rather than guessed).
	LabelLocale string `json:"label_locale"`
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

// acceptStaffTerms finishes the sign-in door's terms step with the Terms box
// and NOTHING ELSE on the body.
//
// The absent `adulthood_declaration` is deliberate and load-bearing (#587):
// under an edition that does not carry the Artifact this must go on working
// exactly as it did, which is what every existing caller of this helper
// asserts. Under an edition that DOES ask, this same body is the untick, and
// the tests below use it as one.
func acceptStaffTerms(t *testing.T, env *testEnv, token string, accepted bool) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, "/api/v1/auth/terms/accept", map[string]any{
		"pending_terms_token": token,
		"terms_acceptance":    accepted,
	}, nil)
}

// acceptStaffTermsDeclaring answers both boxes at the sign-in door (#587).
func acceptStaffTermsDeclaring(t *testing.T, env *testEnv, token string, accepted, declared bool) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, "/api/v1/auth/terms/accept", map[string]any{
		"pending_terms_token":   token,
		"terms_acceptance":      accepted,
		"adulthood_declaration": declared,
	}, nil)
}

// staffAdulthoodDeclarations reads the declaration off every Staff Terms
// Acceptance for one person, oldest EDITION first — nil where the act did not
// ask.
//
// Ordered by the edition and not by `accepted_at`, because the test clock is
// fixed: two acceptances made in one test share a timestamp to the microsecond,
// and ordering on it would make a multi-row assertion a coin toss. The edition
// order is the order the acts happened in, since an acceptance can only ever be
// of the edition current when it was shown.
func staffAdulthoodDeclarations(t *testing.T, env *testEnv, email string) []*bool {
	t.Helper()
	rows, err := env.db.QueryContext(context.Background(), `
		SELECT a.adulthood_declaration
		FROM staff_terms_acceptances a
		JOIN terms_versions tv ON tv.id = a.terms_version_id
		WHERE a.email = $1
		ORDER BY tv.effective_date, tv.created_at
	`, email)
	if err != nil {
		t.Fatalf("read staff adulthood declarations: %v", err)
	}
	defer rows.Close()

	var declarations []*bool
	for rows.Next() {
		var declared *bool
		if err := rows.Scan(&declared); err != nil {
			t.Fatalf("scan staff adulthood declaration: %v", err)
		}
		declarations = append(declarations, declared)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read staff adulthood declarations: %v", err)
	}
	return declarations
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
	// Text and row together, because since #558 an edition is both.
	publishTermsVersion(t, env, 2, 0)

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
	publishTermsVersion(t, env, 2, 0)

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

// The Adulthood Declaration at the STAFF SIGN-IN DOOR, in the organizer
// capacity (#587, ADR 0069).
//
// ADR 0066's organizer acceptance is the more contractual of the two capacities
// and capacity to contract is precisely what is being declared, so the same
// second box the Storefront draws for an attendee is drawn here — worded by the
// published Artifact, un-premarked, and refused before anything at all is
// written.

// TestStaffSignInDeclaresAdulthoodBesideTheTerms is the whole box in one story:
// a gating edition carrying the Artifact owes a second label, an unticked
// declaration is refused 400 with no row written, and ticking both records one
// acceptance carrying `adulthood_declaration = true`.
func TestStaffSignInDeclaresAdulthoodBesideTheTerms(t *testing.T) {
	env := setupTest(t)
	email := "declaring-organizer@example.com"

	// The publish is what introduces the box — never a deploy. Edition 2 carries
	// the Artifact, which changes the slug set, which is a structural change,
	// which is why it can only ever arrive as a gating edition.
	publishTermsVersionAskingAdulthood(t, env, 2, 0)

	outcome := decodeStaffSignInOutcome(t, requestAndVerifyIn(t, env, email, "en"))
	if outcome.TermsRequired == nil {
		t.Fatal("expected a terms step on an edition nobody has accepted")
	}
	if !strings.Contains(outcome.TermsRequired.AdulthoodDeclarationLabel, "eighteen years of age") {
		t.Fatalf("the second box is not worded by the artifact: %q",
			outcome.TermsRequired.AdulthoodDeclarationLabel)
	}
	if outcome.TermsRequired.AdulthoodDeclarationLabel == outcome.TermsRequired.AcceptanceLabel {
		t.Fatal("the two boxes must be two boxes, not one label shown twice")
	}

	// The Terms ticked and the declaration left alone. Absent is false on the
	// staff side, and false is refused — before the session is minted, before
	// the acceptance is appended, before anything at all.
	resp, refused := acceptStaffTerms(t, env, outcome.TermsRequired.PendingTermsToken, true)
	if resp.StatusCode != http.StatusBadRequest || refused.Error == nil ||
		refused.Error.Code != "ADULTHOOD_DECLARATION_REQUIRED" {
		t.Fatalf("unticked declaration: status=%d error=%+v, want 400 ADULTHOOD_DECLARATION_REQUIRED",
			resp.StatusCode, refused.Error)
	}
	if countStaffTermsAcceptances(t, env, email) != 0 {
		t.Fatal("a refused declaration must leave no acceptance row at all")
	}
	if countStaffSessionsFor(t, env, email) != 0 {
		t.Fatal("a refused declaration must mint no session")
	}
	// The platform keeps NO RECORD of anybody who says they are a minor: not a
	// false, not a row, not anywhere.
	if declarations := staffAdulthoodDeclarations(t, env, email); len(declarations) != 0 {
		t.Fatalf("a refusal recorded something: %v", declarations)
	}

	// Sign in again — the token is spent at this door whatever the outcome, as
	// it always has been — and tick both.
	outcome = decodeStaffSignInOutcome(t, requestAndVerifyIn(t, env, email, "en"))
	if outcome.TermsRequired == nil {
		t.Fatal("expected the terms step again after a refusal")
	}
	resp, acceptedBody := acceptStaffTermsDeclaring(t, env, outcome.TermsRequired.PendingTermsToken, true, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept both boxes status=%d error=%+v", resp.StatusCode, acceptedBody.Error)
	}
	if decodeStaffSignInOutcome(t, acceptedBody).SessionID == "" {
		t.Fatal("ticking both boxes must mint the session")
	}

	// One row, carrying the declaration beside the acceptance it travelled with,
	// under the pinned edition and in the organizer capacity.
	var (
		count     int
		label     string
		capacity  string
		declared  *bool
		presented *string
	)
	if err := env.db.QueryRowContext(context.Background(), `
		SELECT COUNT(*) OVER (), tv.label, a.capacity, a.adulthood_declaration, a.presented_locale
		FROM staff_terms_acceptances a
		JOIN terms_versions tv ON tv.id = a.terms_version_id
		WHERE a.email = $1
	`, email).Scan(&count, &label, &capacity, &declared, &presented); err != nil {
		t.Fatalf("read the acceptance row: %v", err)
	}
	if count != 1 || label != "2" || capacity != "organizer" {
		t.Fatalf("rows=%d edition=%q capacity=%q, want 1 row on edition 2 as organizer", count, label, capacity)
	}
	if declared == nil || !*declared {
		t.Fatalf("adulthood_declaration=%v, want true beside the acceptance", declared)
	}
	if presented == nil || *presented != "en" {
		t.Fatalf("presented_locale=%v, want the language both labels were served in", presented)
	}

	// And the person is not asked again: one declaration per person per edition,
	// exactly as one acceptance is.
	second := decodeStaffSignInOutcome(t, requestAndVerifyIn(t, env, email, "en"))
	if second.TermsRequired != nil || second.SessionID == "" {
		t.Fatalf("a Member Current on the edition must not be re-asked: %+v", second)
	}
	if len(staffAdulthoodDeclarations(t, env, email)) != 1 {
		t.Fatal("a second sign-in must append nothing")
	}
}

// TestStaffSignInDeclarationIsWordedInThePageLanguage: the second box is the
// published Artifact in the reader's language, exactly as its neighbour is, and
// the acceptance it buys is the same acceptance either way — both translations
// are one edition under one fingerprint (§37).
func TestStaffSignInDeclarationIsWordedInThePageLanguage(t *testing.T) {
	env := setupTest(t)
	publishTermsVersionAskingAdulthood(t, env, 2, 0)

	spanish := decodeStaffSignInOutcome(t, requestAndVerifyIn(t, env, "es-declarer@example.com", "es"))
	if spanish.TermsRequired == nil {
		t.Fatal("expected a terms step")
	}
	if !strings.Contains(spanish.TermsRequired.AdulthoodDeclarationLabel, "mayor de edad") {
		t.Errorf("the es declaration label is not in spanish: %q", spanish.TermsRequired.AdulthoodDeclarationLabel)
	}

	english := decodeStaffSignInOutcome(t, requestAndVerifyIn(t, env, "en-declarer@example.com", "en"))
	if english.TermsRequired == nil {
		t.Fatal("expected a terms step")
	}
	if !strings.Contains(english.TermsRequired.AdulthoodDeclarationLabel, "eighteen years of age") {
		t.Errorf("the en declaration label is not in english: %q", english.TermsRequired.AdulthoodDeclarationLabel)
	}
	if spanish.TermsRequired.Version != english.TermsRequired.Version {
		t.Errorf("the two languages offer different editions: %q and %q",
			spanish.TermsRequired.Version, english.TermsRequired.Version)
	}
}

// TestStaffSignInUnderAnEditionThatDoesNotAskRecordsNothing: an edition without
// the Artifact owes no declaration, draws no box, and records a null — the
// column's "this act did not ask", which is what every acceptance made before
// the Artifact was published reads as forever.
//
// It also asserts the other direction, which is the one a client could get
// wrong: a body claiming the declaration where the edition does not ask records
// a null anyway. What is stored is what the edition asked and the person
// answered, never what a request volunteered.
func TestStaffSignInUnderAnEditionThatDoesNotAskRecordsNothing(t *testing.T) {
	env := setupTest(t)
	email := "no-artifact@example.com"

	outcome := decodeStaffSignInOutcome(t, requestAndVerifyIn(t, env, email, "en"))
	if outcome.TermsRequired == nil {
		t.Fatal("expected a terms step on edition 1")
	}
	if outcome.TermsRequired.AdulthoodDeclarationLabel != "" {
		t.Fatalf("edition 1 carries no artifact and must offer no second box: %q",
			outcome.TermsRequired.AdulthoodDeclarationLabel)
	}

	resp, body := acceptStaffTermsDeclaring(t, env, outcome.TermsRequired.PendingTermsToken, true, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept status=%d error=%+v", resp.StatusCode, body.Error)
	}
	declarations := staffAdulthoodDeclarations(t, env, email)
	if len(declarations) != 1 || declarations[0] != nil {
		t.Fatalf("declarations=%v, want one acceptance recording a null — the edition never asked", declarations)
	}
}

// TestStaffAdulthoodDeclarationIsOncePerPersonNotPerMembership: a Member of
// several Organizations declares once, in the one organizer capacity that
// covers all of them. There is no per-membership declaration and no per-Event
// one — the contract delegates every event-specific age rule to the organizer,
// to be stated in the particular conditions and checked at the door.
func TestStaffAdulthoodDeclarationIsOncePerPersonNotPerMembership(t *testing.T) {
	env := setupTest(t)
	email := "multi-org-declarer@example.com"
	seedMultiMembership(t, env, email)
	publishTermsVersionAskingAdulthood(t, env, 2, 0)

	outcome := decodeStaffSignInOutcome(t, requestAndVerify(t, env, email))
	if outcome.TermsRequired == nil {
		t.Fatal("expected a terms step")
	}
	resp, body := acceptStaffTermsDeclaring(t, env, outcome.TermsRequired.PendingTermsToken, true, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept status=%d error=%+v", resp.StatusCode, body.Error)
	}

	declarations := staffAdulthoodDeclarations(t, env, email)
	if len(declarations) != 1 || declarations[0] == nil || !*declarations[0] {
		t.Fatalf("declarations=%v, want exactly one true for a person with two Organizations", declarations)
	}
	second := decodeStaffSignInOutcome(t, requestAndVerify(t, env, email))
	if second.TermsRequired != nil {
		t.Fatal("a person with several Organizations declares once, not per membership")
	}
}

// TestStaffSignInDeclarationIsPinnedToTheEditionShown: the declaration is
// judged against the edition the token PINNED, never against a re-read of
// whatever is current at the write (#537's held-answer rule).
//
// The dangerous direction is the one asserted: an edition that asks lands while
// somebody is reading a step that drew no second box, and they must not be
// refused over a checkbox that was never on their screen.
func TestStaffSignInDeclarationIsPinnedToTheEditionShown(t *testing.T) {
	env := setupTest(t)
	email := "mid-window-declarer@example.com"

	outcome := decodeStaffSignInOutcome(t, requestAndVerifyIn(t, env, email, "en"))
	if outcome.TermsRequired == nil || outcome.TermsRequired.AdulthoodDeclarationLabel != "" {
		t.Fatalf("expected edition 1's step, with no second box: %+v", outcome.TermsRequired)
	}

	// An edition carrying the Artifact arrives while the person reads.
	publishTermsVersionAskingAdulthood(t, env, 2, 0)

	resp, body := acceptStaffTerms(t, env, outcome.TermsRequired.PendingTermsToken, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a box that was never shown must not refuse the step: status=%d error=%+v",
			resp.StatusCode, body.Error)
	}
	declarations := staffAdulthoodDeclarations(t, env, email)
	if len(declarations) != 1 || declarations[0] != nil {
		t.Fatalf("declarations=%v, want a null — the edition shown never asked", declarations)
	}

	// And the new edition is still owed, so the declaration is collected at the
	// next sign-in rather than lost.
	regated := decodeStaffSignInOutcome(t, requestAndVerifyIn(t, env, email, "en"))
	if regated.TermsRequired == nil || regated.TermsRequired.AdulthoodDeclarationLabel == "" {
		t.Fatalf("expected a re-gate that asks: %+v", regated.TermsRequired)
	}
}
