package integration

// The staff gate on a LIVE session (#570, ADR 0067's amendment to ADR 0066),
// tested at the HTTP seam the staff app's middleware actually speaks to: does
// the gate predicate bite for THIS session against THIS floor, and what does
// answering it cost?
//
// Deliberately no Playwright anywhere near this. The e2e suite cannot be green
// in one run — passcodes are rationed to 10 per IP per 15 minutes and the suite
// needs more sign-ins than that — and it runs against a stale dev stack that
// builds nothing. The two things worth testing are this predicate and the
// middleware's own decision function (apps/staff/lib/terms-gate.test.ts), and
// neither needs a browser.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	staffTermsGatePath   = "/api/v1/staff/terms/gate"
	staffTermsAcceptPath = "/api/v1/staff/terms/accept"
)

// staffTermsGateView is the interstitial's read as the wire carries it.
type staffTermsGateView struct {
	Outstanding   bool                    `json:"outstanding"`
	TermsRequired *staffTermsRequiredView `json:"terms_required"`
}

// staffSessionTermsFlag is the one field the staff middleware reads off the
// session route. A POINTER, because null ("not asked", or "the read failed")
// and false ("nothing owed") are different answers and the middleware diverts
// on neither.
type staffSessionTermsFlag struct {
	TermsOutstanding *bool `json:"terms_outstanding"`
}

func readStaffTermsGate(t *testing.T, env *testEnv, sessionID, locale string) staffTermsGateView {
	t.Helper()
	path := staffTermsGatePath
	if locale != "" {
		path += "?locale=" + locale
	}
	resp, body := env.get(t, path, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("terms gate status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var view staffTermsGateView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode terms gate: %v", err)
	}
	return view
}

// acceptStaffTermsOnSession answers the interstitial with the Terms box and
// NOTHING ELSE on the body — which is the untick of the second box under an
// edition that asks (#587), and is exactly what every caller predating the
// declaration sends.
func acceptStaffTermsOnSession(t *testing.T, env *testEnv, sessionID, token string, accepted bool) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, staffTermsAcceptPath, map[string]any{
		"gate_token":       token,
		"terms_acceptance": accepted,
	}, authHeader(sessionID))
}

// acceptStaffTermsOnSessionDeclaring answers both of the interstitial's boxes.
func acceptStaffTermsOnSessionDeclaring(t *testing.T, env *testEnv, sessionID, token string, accepted, declared bool) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, staffTermsAcceptPath, map[string]any{
		"gate_token":            token,
		"terms_acceptance":      accepted,
		"adulthood_declaration": declared,
	}, authHeader(sessionID))
}

// sessionTermsOutstanding reads the flag the staff middleware diverts on.
func sessionTermsOutstanding(t *testing.T, env *testEnv, sessionID string) *bool {
	t.Helper()
	resp, body := env.get(t, "/api/v1/auth/session", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var flag staffSessionTermsFlag
	if err := json.Unmarshal(body.Data, &flag); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return flag.TermsOutstanding
}

// sessionStillWorks asserts the session authenticates an ordinary staff API
// call — the "revokes nothing, refuses no in-flight mutation" assertion.
func sessionStillWorks(t *testing.T, env *testEnv, sessionID string) {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/memberships", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("staff API refused a live session: status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

func countStaffSessionsFor(t *testing.T, env *testEnv, email string) int {
	t.Helper()
	var count int
	if err := env.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM sessions WHERE email = $1`, email).Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return count
}

// TestStaffTermsGateBindsOnALiveSession is the whole amendment in one story: a
// person signs in under edition 1, a gating edition 2 arrives while they are
// working, and they are stopped on the next navigation rather than never.
//
// Everything they were holding survives it. The session is not revoked, not
// re-minted and not re-proved: the same token authenticates the staff API
// before the acceptance, during the interstitial and after it, the sessions
// table gains no row, and no passcode is spent.
func TestStaffTermsGateBindsOnALiveSession(t *testing.T) {
	env := setupTest(t)
	email := "live-session@example.com"

	sessionID := sessionThroughTermsGate(t, env, requestAndVerify(t, env, email))
	if sessionID == "" {
		t.Fatal("expected a session on edition 1")
	}
	if outstanding := sessionTermsOutstanding(t, env, sessionID); outstanding == nil || *outstanding {
		t.Fatalf("terms_outstanding=%v, want an explicit false for somebody who just accepted", outstanding)
	}

	// Edition 2 is published under their feet. It revokes nothing: no session
	// row is deleted and there is no control that offers to.
	publishTermsVersion(t, env, 2, 0)
	sessionStillWorks(t, env, sessionID)
	if countStaffSessionsFor(t, env, email) != 1 {
		t.Fatalf("a publish must neither revoke nor mint sessions: %d rows", countStaffSessionsFor(t, env, email))
	}

	// The next navigation asks, and is told to divert.
	if outstanding := sessionTermsOutstanding(t, env, sessionID); outstanding == nil || !*outstanding {
		t.Fatalf("terms_outstanding=%v, want true once a gating edition has arrived", outstanding)
	}

	gate := readStaffTermsGate(t, env, sessionID, "en")
	if !gate.Outstanding || gate.TermsRequired == nil {
		t.Fatalf("expected the interstitial's box: %+v", gate)
	}
	if gate.TermsRequired.Version != "2" {
		t.Fatalf("interstitial names edition %q, want 2", gate.TermsRequired.Version)
	}
	if gate.TermsRequired.PendingTermsToken == "" {
		t.Fatal("the interstitial must never render without an acceptance control")
	}
	// Reading the box costs the session nothing.
	sessionStillWorks(t, env, sessionID)

	resp, body := acceptStaffTermsOnSession(t, env, sessionID, gate.TermsRequired.PendingTermsToken, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept on session status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var accepted struct {
		Accepted bool `json:"accepted"`
	}
	if err := json.Unmarshal(body.Data, &accepted); err != nil {
		t.Fatalf("decode acceptance: %v", err)
	}
	if !accepted.Accepted {
		t.Fatal("expected the acceptance to be reported as recorded")
	}

	// The same session, still: nothing was minted and nothing re-minted.
	sessionStillWorks(t, env, sessionID)
	if countStaffSessionsFor(t, env, email) != 1 {
		t.Fatalf("the interstitial minted a session: %d rows", countStaffSessionsFor(t, env, email))
	}
	if outstanding := sessionTermsOutstanding(t, env, sessionID); outstanding == nil || *outstanding {
		t.Fatalf("terms_outstanding=%v after accepting, want false", outstanding)
	}
	if gate := readStaffTermsGate(t, env, sessionID, "en"); gate.Outstanding {
		t.Fatal("the interstitial must not be shown twice for one edition")
	}

	// One append-only row per edition, evidencing THE LIVE SESSION that
	// answered — not a new one, because there is no new one.
	if countStaffTermsAcceptances(t, env, email) != 2 {
		t.Fatalf("acceptances=%d, want one per edition", countStaffTermsAcceptances(t, env, email))
	}
	var (
		capacity       string
		acceptedAt     time.Time
		evidenceSessID *string
	)
	if err := env.db.QueryRowContext(context.Background(), `
		SELECT a.capacity, a.accepted_at, a.session_id
		FROM staff_terms_acceptances a
		JOIN terms_versions tv ON tv.id = a.terms_version_id
		WHERE a.email = $1 AND tv.label = '2'
	`, email).Scan(&capacity, &acceptedAt, &evidenceSessID); err != nil {
		t.Fatalf("read the interstitial's acceptance: %v", err)
	}
	if capacity != "organizer" {
		t.Fatalf("capacity=%q, want organizer", capacity)
	}
	if evidenceSessID == nil || *evidenceSessID != sessionID {
		t.Fatalf("evidence session_id=%v, want the live session %q", evidenceSessID, sessionID)
	}
}

// TestStaffTermsGateCorrectionDoesNotStopALiveSession: a correction re-gates
// nobody, and the live-session check is the same membership test the sign-in
// door makes (#560). Nobody is diverted to an interstitial for a typo fix.
func TestStaffTermsGateCorrectionDoesNotStopALiveSession(t *testing.T) {
	env := setupTest(t)
	email := "corrected@example.com"

	sessionID := sessionThroughTermsGate(t, env, requestAndVerify(t, env, email))
	publishTermsVersion(t, env, 1, 1)

	if outstanding := sessionTermsOutstanding(t, env, sessionID); outstanding == nil || *outstanding {
		t.Fatalf("terms_outstanding=%v, want false — a correction re-gates nobody", outstanding)
	}
	if gate := readStaffTermsGate(t, env, sessionID, "en"); gate.Outstanding {
		t.Fatal("a correction must not divert anybody to the interstitial")
	}
	if countStaffTermsAcceptances(t, env, email) != 1 {
		t.Fatal("a correction must record nothing")
	}
}

// TestStaffTermsGateUntickedBoxCostsNothingButAClick: the API refuses an
// unticked box, and — unlike the sign-in door — does NOT spend the token doing
// it. There the token is a Proof of Email Ownership and re-proving costs a
// passcode; here the session is the credential and the token proves nothing, so
// the same box is still there to tick.
func TestStaffTermsGateUntickedBoxCostsNothingButAClick(t *testing.T) {
	env := setupTest(t)
	email := "misclick@example.com"

	sessionID := sessionThroughTermsGate(t, env, requestAndVerify(t, env, email))
	publishTermsVersion(t, env, 2, 0)

	gate := readStaffTermsGate(t, env, sessionID, "en")
	if gate.TermsRequired == nil {
		t.Fatal("expected the interstitial's box")
	}
	token := gate.TermsRequired.PendingTermsToken

	resp, refused := acceptStaffTermsOnSession(t, env, sessionID, token, false)
	if resp.StatusCode != http.StatusBadRequest || refused.Error == nil || refused.Error.Code != "TERMS_ACCEPTANCE_REQUIRED" {
		t.Fatalf("unticked box: status=%d error=%+v, want 400 TERMS_ACCEPTANCE_REQUIRED", resp.StatusCode, refused.Error)
	}
	if countStaffTermsAcceptances(t, env, email) != 1 {
		t.Fatal("a refused submission must record nothing")
	}
	sessionStillWorks(t, env, sessionID)

	resp, body := acceptStaffTermsOnSession(t, env, sessionID, token, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the same token must still be good: status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if countStaffTermsAcceptances(t, env, email) != 2 {
		t.Fatal("expected the acceptance of edition 2")
	}
}

// TestStaffTermsGateTokenBelongsToTheSessionItWasIssuedTo: a gate token
// redeemed by somebody else's session buys nothing. It is the only thing a
// leaked one could ever be worth — an acceptance in the wrong person's name —
// and the refusal is the same indistinguishable one an unknown token gets.
func TestStaffTermsGateTokenBelongsToTheSessionItWasIssuedTo(t *testing.T) {
	env := setupTest(t)
	holder := "token-holder@example.com"
	other := "other-staff@example.com"

	holderSession := sessionThroughTermsGate(t, env, requestAndVerify(t, env, holder))
	otherSession := sessionThroughTermsGate(t, env, requestAndVerify(t, env, other))
	publishTermsVersion(t, env, 2, 0)

	gate := readStaffTermsGate(t, env, holderSession, "en")
	if gate.TermsRequired == nil {
		t.Fatal("expected the interstitial's box")
	}

	resp, body := acceptStaffTermsOnSession(t, env, otherSession, gate.TermsRequired.PendingTermsToken, true)
	if resp.StatusCode != http.StatusUnauthorized || body.Error == nil || body.Error.Code != "PENDING_TERMS_INVALID" {
		t.Fatalf("another session's token: status=%d error=%+v, want 401 PENDING_TERMS_INVALID", resp.StatusCode, body.Error)
	}
	if countStaffTermsAcceptances(t, env, holder) != 1 {
		t.Fatal("no acceptance may be recorded in the token holder's name")
	}
	if countStaffTermsAcceptances(t, env, other) != 1 {
		t.Fatal("no acceptance may be recorded for the redeeming session either")
	}
}

// TestStaffTermsGateExpiredTokenLeavesTheSessionAlone: the pinned box is
// minutes long, and a stale one is refused — but the refusal costs the person
// only a reload, never a passcode and never their session.
func TestStaffTermsGateExpiredTokenLeavesTheSessionAlone(t *testing.T) {
	env := setupTest(t)
	email := "slow-navigator@example.com"

	sessionID := sessionThroughTermsGate(t, env, requestAndVerify(t, env, email))
	publishTermsVersion(t, env, 2, 0)
	gate := readStaffTermsGate(t, env, sessionID, "en")
	if gate.TermsRequired == nil {
		t.Fatal("expected the interstitial's box")
	}

	env.service.WithClock(func() time.Time { return env.fixedClock.Add(16 * time.Minute) })

	resp, body := acceptStaffTermsOnSession(t, env, sessionID, gate.TermsRequired.PendingTermsToken, true)
	if resp.StatusCode != http.StatusUnauthorized || body.Error == nil || body.Error.Code != "PENDING_TERMS_INVALID" {
		t.Fatalf("expired token: status=%d error=%+v, want 401 PENDING_TERMS_INVALID", resp.StatusCode, body.Error)
	}
	sessionStillWorks(t, env, sessionID)

	// A reload issues a fresh box, which is the whole of the recovery.
	fresh := readStaffTermsGate(t, env, sessionID, "en")
	if fresh.TermsRequired == nil {
		t.Fatal("a reload must offer the box again")
	}
	resp, body = acceptStaffTermsOnSession(t, env, sessionID, fresh.TermsRequired.PendingTermsToken, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fresh token status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// TestStaffTermsGateFloorsAMissingLabelAtThePrevailingText: the interstitial
// must NEVER degrade to a card with no acceptance control. An edition that
// publishes no English label serves an English reader the Spanish one — the
// text that legally binds them (§37) — and says so, so the link beside the box
// opens the document the words came from rather than a page that is not there.
func TestStaffTermsGateFloorsAMissingLabelAtThePrevailingText(t *testing.T) {
	env := setupTest(t)
	email := "english-reader-live@example.com"

	sessionID := sessionThroughTermsGate(t, env, requestAndVerifyIn(t, env, email, "en"))

	// An edition published in the prevailing language only. The Spanish text can
	// never be dropped; a translation can be late.
	editionID := publishTermsVersion(t, env, 2, 0)
	if _, err := env.db.Exec(
		`DELETE FROM terms_version_artifacts WHERE version_id = $1 AND locale = 'en'`, editionID); err != nil {
		t.Fatalf("drop the english artifacts: %v", err)
	}

	gate := readStaffTermsGate(t, env, sessionID, "en")
	if !gate.Outstanding || gate.TermsRequired == nil {
		t.Fatalf("a missing translation must not drop the gate: %+v", gate)
	}
	if gate.TermsRequired.PendingTermsToken == "" {
		t.Fatal("a card with no acceptance control is the one thing this may never be")
	}
	if gate.TermsRequired.LabelLocale != "es" {
		t.Fatalf("label_locale=%q, want es — the reader must be told which document they were shown",
			gate.TermsRequired.LabelLocale)
	}
	if !strings.Contains(gate.TermsRequired.AcceptanceLabel, "(es,") {
		t.Fatalf("the served label is not the spanish artifact: %q", gate.TermsRequired.AcceptanceLabel)
	}

	resp, body := acceptStaffTermsOnSession(t, env, sessionID, gate.TermsRequired.PendingTermsToken, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// And the evidence records the language actually shown, not the one the page
	// was rendered in (#567).
	var presented *string
	if err := env.db.QueryRowContext(context.Background(), `
		SELECT a.presented_locale
		FROM staff_terms_acceptances a
		JOIN terms_versions tv ON tv.id = a.terms_version_id
		WHERE a.email = $1 AND tv.label = '2'
	`, email).Scan(&presented); err != nil {
		t.Fatalf("read presented locale: %v", err)
	}
	if presented == nil || *presented != "es" {
		t.Fatalf("presented_locale=%v, want the language that was on screen", presented)
	}
}

// TestStaffTermsGateStopsThePublishingOperatorToo: operator authority buys no
// way past the gate on a live session any more than it does at the sign-in
// door. The person who re-gated everybody meets the interstitial themselves,
// so they have demonstrably read what they published.
func TestStaffTermsGateStopsThePublishingOperatorToo(t *testing.T) {
	env := setupTest(t)
	email := "publishing-operator@example.com"
	if _, err := env.db.ExecContext(context.Background(),
		`INSERT INTO platform_operators (email) VALUES ($1) ON CONFLICT (email) DO NOTHING`, email); err != nil {
		t.Fatalf("seed platform operator: %v", err)
	}

	sessionID := sessionThroughTermsGate(t, env, requestAndVerify(t, env, email))
	publishTermsVersion(t, env, 2, 0)

	if outstanding := sessionTermsOutstanding(t, env, sessionID); outstanding == nil || !*outstanding {
		t.Fatalf("terms_outstanding=%v for the publishing operator, want true", outstanding)
	}
	gate := readStaffTermsGate(t, env, sessionID, "en")
	if !gate.Outstanding || gate.TermsRequired == nil {
		t.Fatalf("an operator must meet their own interstitial: %+v", gate)
	}
}

// TestStaffTermsGateIsNotAGateOnTheAPI: while an acceptance is outstanding,
// every other staff route answers exactly as it did. The gate diverts a page
// navigation and nothing else — a sale in progress commits, and no in-flight
// mutation is refused because an edition rolled over at midnight.
func TestStaffTermsGateIsNotAGateOnTheAPI(t *testing.T) {
	env := setupTest(t)
	email := "mid-shift@example.com"

	sessionID := sessionThroughTermsGate(t, env, requestAndVerify(t, env, email))
	publishTermsVersion(t, env, 2, 0)

	if outstanding := sessionTermsOutstanding(t, env, sessionID); outstanding == nil || !*outstanding {
		t.Fatal("expected an outstanding acceptance for this test to mean anything")
	}

	// A read and a WRITE, neither of which consults the Terms.
	sessionStillWorks(t, env, sessionID)
	resp, body := env.put(t, "/api/v1/staff/me/locale", map[string]string{"locale": "es"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a staff mutation was refused over an outstanding acceptance: status=%d error=%+v",
			resp.StatusCode, body.Error)
	}
}

// The Adulthood Declaration on the LIVE-SESSION gate (#587, ADR 0069).
//
// Everybody is re-gated the morning the Gating Edition takes effect, staff
// included and mid-work — that is the mechanism by which the declaration is
// collected from the whole organizer population with no backfill. What it must
// not cost is anything: not a passcode, not a session, and not, on a misclick,
// even the box itself.

// TestStaffTermsGateAsksTheDeclarationOnALiveSession: a Member working when the
// Artifact-carrying edition arrives meets both boxes on their next navigation,
// and answering both records one row carrying the declaration beside the
// acceptance — evidencing the live session, not a new one.
func TestStaffTermsGateAsksTheDeclarationOnALiveSession(t *testing.T) {
	env := setupTest(t)
	email := "mid-work-declarer@example.com"

	sessionID := sessionThroughTermsGate(t, env, requestAndVerify(t, env, email))
	publishTermsVersionAskingAdulthood(t, env, 2, 0)

	gate := readStaffTermsGate(t, env, sessionID, "en")
	if !gate.Outstanding || gate.TermsRequired == nil {
		t.Fatalf("expected the interstitial's boxes: %+v", gate)
	}
	if !strings.Contains(gate.TermsRequired.AdulthoodDeclarationLabel, "eighteen years of age") {
		t.Fatalf("the interstitial's second box is not worded by the artifact: %q",
			gate.TermsRequired.AdulthoodDeclarationLabel)
	}

	resp, body := acceptStaffTermsOnSessionDeclaring(t, env, sessionID, gate.TermsRequired.PendingTermsToken, true, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept both boxes status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// The same session throughout: nothing minted, nothing re-minted, no
	// passcode spent.
	sessionStillWorks(t, env, sessionID)
	if countStaffSessionsFor(t, env, email) != 1 {
		t.Fatalf("the interstitial touched the sessions table: %d rows", countStaffSessionsFor(t, env, email))
	}

	var (
		declared       *bool
		evidenceSessID *string
	)
	if err := env.db.QueryRowContext(context.Background(), `
		SELECT a.adulthood_declaration, a.session_id
		FROM staff_terms_acceptances a
		JOIN terms_versions tv ON tv.id = a.terms_version_id
		WHERE a.email = $1 AND tv.label = '2'
	`, email).Scan(&declared, &evidenceSessID); err != nil {
		t.Fatalf("read the interstitial's acceptance: %v", err)
	}
	if declared == nil || !*declared {
		t.Fatalf("adulthood_declaration=%v, want true on the interstitial's row", declared)
	}
	if evidenceSessID == nil || *evidenceSessID != sessionID {
		t.Fatalf("evidence session_id=%v, want the live session %q", evidenceSessID, sessionID)
	}
	// The declaration made under edition 1 is a null: that act never asked.
	declarations := staffAdulthoodDeclarations(t, env, email)
	if len(declarations) != 2 || declarations[0] != nil {
		t.Fatalf("declarations=%v, want the earlier acceptance to record a null", declarations)
	}
}

// TestStaffTermsGateUntickedDeclarationCostsNothingButAClick is the rule this
// surface exists to keep, extended to the second box: the API refuses an
// unticked declaration and does NOT spend the gate token doing it.
//
// The token proves nothing here — the session is the credential — so a Member
// re-gated mid-work who misses the second box ticks it and carries on with the
// SAME box, without burning a passcode and without ending their session. At the
// sign-in door the equivalent misclick costs a passcode, which is precisely the
// cost this path refuses to impose.
func TestStaffTermsGateUntickedDeclarationCostsNothingButAClick(t *testing.T) {
	env := setupTest(t)
	email := "misclick-declarer@example.com"

	sessionID := sessionThroughTermsGate(t, env, requestAndVerify(t, env, email))
	publishTermsVersionAskingAdulthood(t, env, 2, 0)

	gate := readStaffTermsGate(t, env, sessionID, "en")
	if gate.TermsRequired == nil || gate.TermsRequired.AdulthoodDeclarationLabel == "" {
		t.Fatalf("expected an interstitial that asks: %+v", gate.TermsRequired)
	}
	token := gate.TermsRequired.PendingTermsToken

	resp, refused := acceptStaffTermsOnSessionDeclaring(t, env, sessionID, token, true, false)
	if resp.StatusCode != http.StatusBadRequest || refused.Error == nil ||
		refused.Error.Code != "ADULTHOOD_DECLARATION_REQUIRED" {
		t.Fatalf("unticked declaration: status=%d error=%+v, want 400 ADULTHOOD_DECLARATION_REQUIRED",
			resp.StatusCode, refused.Error)
	}
	if countStaffTermsAcceptances(t, env, email) != 1 {
		t.Fatal("a refused declaration must record nothing")
	}
	sessionStillWorks(t, env, sessionID)
	if countStaffSessionsFor(t, env, email) != 1 {
		t.Fatal("a refused declaration must not end or re-mint the session")
	}

	// An absent field is the same untick, and is refused the same way.
	resp, refused = acceptStaffTermsOnSession(t, env, sessionID, token, true)
	if resp.StatusCode != http.StatusBadRequest || refused.Error == nil ||
		refused.Error.Code != "ADULTHOOD_DECLARATION_REQUIRED" {
		t.Fatalf("absent declaration: status=%d error=%+v, want 400 ADULTHOOD_DECLARATION_REQUIRED",
			resp.StatusCode, refused.Error)
	}

	// THE SAME TOKEN IS STILL GOOD. One click is the whole of the recovery.
	resp, body := acceptStaffTermsOnSessionDeclaring(t, env, sessionID, token, true, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the same token must still be good: status=%d error=%+v", resp.StatusCode, body.Error)
	}
	declarations := staffAdulthoodDeclarations(t, env, email)
	if len(declarations) != 2 || declarations[1] == nil || !*declarations[1] {
		t.Fatalf("declarations=%v, want the second acceptance to carry the declaration", declarations)
	}
}

// TestStaffTermsGateEditionWithoutTheArtifactAsksNoDeclaration: an edition that
// does not carry the Artifact owes no declaration on this gate either, draws no
// second box, and records the null that says the act never asked.
func TestStaffTermsGateEditionWithoutTheArtifactAsksNoDeclaration(t *testing.T) {
	env := setupTest(t)
	email := "no-second-box@example.com"

	sessionID := sessionThroughTermsGate(t, env, requestAndVerify(t, env, email))
	publishTermsVersion(t, env, 2, 0)

	gate := readStaffTermsGate(t, env, sessionID, "en")
	if gate.TermsRequired == nil {
		t.Fatal("expected the interstitial's box")
	}
	if gate.TermsRequired.AdulthoodDeclarationLabel != "" {
		t.Fatalf("an edition without the artifact must offer no second box: %q",
			gate.TermsRequired.AdulthoodDeclarationLabel)
	}

	resp, body := acceptStaffTermsOnSession(t, env, sessionID, gate.TermsRequired.PendingTermsToken, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept status=%d error=%+v", resp.StatusCode, body.Error)
	}
	for i, declared := range staffAdulthoodDeclarations(t, env, email) {
		if declared != nil {
			t.Fatalf("acceptance %d recorded %v, want a null on an edition that never asked", i, *declared)
		}
	}
}

// TestStaffTermsGateDeclarationIsNotAGateOnTheAPI: an outstanding declaration
// diverts a page navigation and nothing else. A sale in progress at the box
// office still commits — `/api/` is not gated, and this asserts it against the
// edition that asks rather than only against the one that does not.
func TestStaffTermsGateDeclarationIsNotAGateOnTheAPI(t *testing.T) {
	env := setupTest(t)
	email := "box-office-mid-sale@example.com"

	sessionID := sessionThroughTermsGate(t, env, requestAndVerify(t, env, email))
	publishTermsVersionAskingAdulthood(t, env, 2, 0)

	if outstanding := sessionTermsOutstanding(t, env, sessionID); outstanding == nil || !*outstanding {
		t.Fatal("expected an outstanding acceptance for this test to mean anything")
	}
	sessionStillWorks(t, env, sessionID)
	resp, body := env.put(t, "/api/v1/staff/me/locale", map[string]string{"locale": "es"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a staff mutation was refused over an owed declaration: status=%d error=%+v",
			resp.StatusCode, body.Error)
	}
}
