package integration

import (
	"bytes"
	"database/sql"
	"net/http"
	"testing"
	"time"
)

// Consent capture at sign-in (#251, parent #249): the gate between Proof of
// Email Ownership and a Customer Session, the evidence the crossing writes, and
// the state it makes true.
//
// The seam is HTTP, as always. What is asserted through SQL — the Consent
// Records and the consent columns on `customers` — is asserted that way because
// the platform deliberately publishes neither: evidence is for a compliance
// officer and a court, not for an endpoint, and current state is read only by
// the gate itself. Everything a person can observe is asserted through the API.

const customerConsentPath = "/api/v1/customer/auth/consent"

// consentBoxes is which checkboxes a capture surface was told to render.
type consentBoxes struct {
	PolicyAcceptance  bool `json:"policy_acceptance"`
	MarketingConsent  bool `json:"marketing_consent"`
	NetworkingConsent bool `json:"networking_consent"`
}

// consentRequiredOutcome is the shape a verify answers with when it minted no
// session.
type consentRequiredOutcome struct {
	PendingConsentToken string       `json:"pending_consent_token"`
	ExpiresAt           string       `json:"expires_at"`
	Boxes               consentBoxes `json:"boxes"`
}

// consentRecordRow is one row of the append-only evidence log.
type consentRecordRow struct {
	Email             string
	Channel           string
	CapturedAt        time.Time
	PolicyVersionID   string
	PolicyAcceptance  sql.NullBool
	MarketingConsent  sql.NullBool
	NetworkingConsent sql.NullBool
	EmailProven       bool
	ConfirmedAt       sql.NullTime
	IP                sql.NullString
	UserAgent         sql.NullString
	SessionID         sql.NullString
	OriginURL         sql.NullString
	// What each optional consent's state was IMMEDIATELY BEFORE this act
	// (#266). Invalid where the box was not shown, exactly as the answer beside
	// it is — and also where it was shown for the first time, which the answer
	// column tells apart.
	PriorMarketingConsent  sql.NullString
	PriorNetworkingConsent sql.NullString
	// When the Customer was told about the Consent Withdrawal this act performed
	// (#267). Invalid on every act that took nothing away, which is most of them,
	// and on a withdrawal whose confirmation the provider refused.
	ConfirmationSentAt sql.NullTime
}

// customerConsentState is what is TRUE NOW about one Customer, as against the
// log of what happened.
type customerConsentState struct {
	PolicyAcceptedAt  sql.NullTime
	PolicyVersionID   sql.NullString
	MarketingConsent  sql.NullString
	NetworkingConsent sql.NullString
	DigestEnabled     bool
}

// startSignIn requests a passcode and redeems it, returning whatever the verify
// answered — a session, or a consent step. It is customerSignIn without the
// consent step folded in, for the tests that are about the gate itself.
func startSignIn(t *testing.T, env *testEnv, email string) customerVerifyData {
	t.Helper()
	resp, body := requestCustomerPasscode(t, env, email, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.post(t, customerOTPVerifyPath, map[string]string{
		"email": email,
		"code":  env.email.LastCode,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return decodeCustomerVerify(t, body)
}

// consentAnswers is a submission's three boxes as a browser would send them.
func consentAnswers(token string, policy, marketing, networking bool) map[string]any {
	return map[string]any{
		"pending_consent_token": token,
		"policy_acceptance":     policy,
		"marketing_consent":     marketing,
		"networking_consent":    networking,
	}
}

// consentEvidenceHeaders are the circumstances a real request arrives with: the
// client IP as the BFF derives it (never a forwarding header the browser could
// forge), the browser's user agent, and the page the capture happened on.
func consentEvidenceHeaders() map[string]string {
	return map[string]string{
		"X-BFF-Client-IP": "198.51.100.24",
		"User-Agent":      "Mozilla/5.0 (consent-test)",
		"Referer":         "http://storefront.example/es/signin",
	}
}

// readConsentRecords reads one Customer's evidence log, newest last. SQL
// because the log has no endpoint and deliberately never will: it is evidence
// for a compliance request, not a resource for a client.
func readConsentRecords(t *testing.T, env *testEnv, email string) []consentRecordRow {
	t.Helper()
	rows, err := env.db.Query(`
		SELECT r.email, r.channel, r.captured_at, r.policy_version_id,
		       r.policy_acceptance, r.marketing_consent, r.networking_consent,
		       r.email_proven, r.confirmed_at, r.ip, r.user_agent, r.session_id, r.origin_url,
		       r.prior_marketing_consent, r.prior_networking_consent,
		       r.confirmation_sent_at
		FROM consent_records r
		JOIN customers c ON c.id = r.customer_id
		WHERE c.email = $1
		ORDER BY r.captured_at ASC, r.id ASC
	`, email)
	if err != nil {
		t.Fatalf("read consent records for %q: %v", email, err)
	}
	defer rows.Close()

	var records []consentRecordRow
	for rows.Next() {
		var r consentRecordRow
		if err := rows.Scan(&r.Email, &r.Channel, &r.CapturedAt, &r.PolicyVersionID,
			&r.PolicyAcceptance, &r.MarketingConsent, &r.NetworkingConsent,
			&r.EmailProven, &r.ConfirmedAt, &r.IP, &r.UserAgent, &r.SessionID, &r.OriginURL,
			&r.PriorMarketingConsent, &r.PriorNetworkingConsent,
			&r.ConfirmationSentAt); err != nil {
			t.Fatalf("scan consent record: %v", err)
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate consent records: %v", err)
	}
	return records
}

// consentRecordsOn narrows one Customer's evidence log to the acts that
// happened on one surface.
//
// Selected by CHANNEL rather than by position in the log: the harness runs on a
// fixed clock, so two acts in one test share a captured_at and the tie falls to
// a random uuid. Which surface an act happened on is a fact about it, and is
// what these assertions are about anyway.
func consentRecordsOn(t *testing.T, env *testEnv, email, channel string) []consentRecordRow {
	t.Helper()
	var on []consentRecordRow
	for _, record := range readConsentRecords(t, env, email) {
		if record.Channel == channel {
			on = append(on, record)
		}
	}
	return on
}

// readConsentState reads the current-state columns. SQL for the same reason.
func readConsentState(t *testing.T, env *testEnv, email string) customerConsentState {
	t.Helper()
	var s customerConsentState
	if err := env.db.QueryRow(`
		SELECT policy_accepted_at, policy_version_id, marketing_consent, networking_consent, digest_enabled
		FROM customers WHERE email = $1
	`, email).Scan(&s.PolicyAcceptedAt, &s.PolicyVersionID, &s.MarketingConsent, &s.NetworkingConsent, &s.DigestEnabled); err != nil {
		t.Fatalf("read consent state for %q: %v", email, err)
	}
	return s
}

// currentPolicyVersionID is the edition in effect, by the same rule the API
// uses: latest effective date that has arrived, ties broken by insertion order.
func currentPolicyVersionID(t *testing.T, env *testEnv) string {
	t.Helper()
	var id string
	if err := env.db.QueryRow(`
		SELECT id FROM policy_versions
		WHERE effective_date <= CURRENT_DATE
		ORDER BY effective_date DESC, created_at DESC
		LIMIT 1
	`).Scan(&id); err != nil {
		t.Fatalf("read current policy version: %v", err)
	}
	return id
}

// publishPolicyVersion inserts a new edition, which is what re-gates every
// Customer who accepted the old one. SQL because publishing an edition is
// deliberately a migration and a deploy, never an API call (migration 060).
func publishPolicyVersion(t *testing.T, env *testEnv, label string) string {
	t.Helper()
	var id string
	if err := env.db.QueryRow(`
		INSERT INTO policy_versions (label, effective_date, content_hash)
		VALUES ($1, CURRENT_DATE, repeat('a', 64))
		RETURNING id
	`, label).Scan(&id); err != nil {
		t.Fatalf("publish policy version %q: %v", label, err)
	}
	return id
}

// setSignInClock moves BOTH clocks a sign-in depends on: the door's, which
// decides whether a pending-consent token is still worth anything, and the
// consent module's, which stamps the evidence. Moving one without the other
// would let "the token expired" and "the record says when" disagree, which is a
// state production cannot be in.
func setSignInClock(t *testing.T, env *testEnv, at time.Time) {
	t.Helper()
	setCustomerClock(t, env, at)
	sharedApp.ConsentService.WithClock(func() time.Time { return at })
}

func countPendingConsents(t *testing.T, env *testEnv) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM pending_consents`).Scan(&n); err != nil {
		t.Fatalf("count pending consents: %v", err)
	}
	return n
}

// TestSignInWithConsentOutstandingMintsNoSession is the gate itself: a proven
// email with no Policy Acceptance of the current edition buys a consent step and
// nothing else.
func TestSignInWithConsentOutstandingMintsNoSession(t *testing.T) {
	env := setupTest(t)

	data := startSignIn(t, env, "ana@example.com")

	if data.SessionID != "" {
		t.Fatalf("session_id = %q — a Customer with no Policy Acceptance must be minted no session", data.SessionID)
	}
	if data.Session != nil {
		t.Fatalf("session = %+v, want null", data.Session)
	}
	if data.ConsentRequired == nil {
		t.Fatal("expected a consent-required outcome after a correct passcode")
	}
	if data.ConsentRequired.PendingConsentToken == "" {
		t.Fatal("expected a pending-consent token")
	}
	// A first sign-in has answered nothing, so all three boxes are shown.
	want := consentBoxes{PolicyAcceptance: true, MarketingConsent: true, NetworkingConsent: true}
	if data.ConsentRequired.Boxes != want {
		t.Fatalf("boxes = %+v, want %+v", data.ConsentRequired.Boxes, want)
	}
	// The token is worth finishing for minutes, not days.
	expires, err := time.Parse(time.RFC3339, data.ConsentRequired.ExpiresAt)
	if err != nil {
		t.Fatalf("parse expires_at %q: %v", data.ConsentRequired.ExpiresAt, err)
	}
	if !expires.After(env.fixedClock) || expires.After(env.fixedClock.Add(time.Hour)) {
		t.Fatalf("expires_at = %s, want a short window after %s", expires, env.fixedClock)
	}

	// Proof of ownership still happened: the Customer exists and is verified.
	// Only the credential was withheld.
	if c := readCustomer(t, env, "ana@example.com"); !c.VerifiedAt.Valid {
		t.Fatal("a proven passcode must still stamp verified_at, even when the session is withheld")
	}
	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0 — no session may exist before consent is recorded", n)
	}
	// The gate is not a capture: nothing was answered, so nothing is evidenced.
	if records := readConsentRecords(t, env, "ana@example.com"); len(records) != 0 {
		t.Fatalf("consent records = %d, want 0 — being asked is not answering", len(records))
	}
}

// TestAbandonedConsentStepLeavesTheVisitorSignedOut is the story that makes
// refusing consent free: closing the tab costs nothing and grants nothing.
func TestAbandonedConsentStepLeavesTheVisitorSignedOut(t *testing.T) {
	env := setupTest(t)

	data := startSignIn(t, env, "ana@example.com")
	if data.ConsentRequired == nil {
		t.Fatal("expected a consent-required outcome")
	}

	// ... and nothing else happens. No submission, no second request.

	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0 after an abandoned consent step", n)
	}
	if records := readConsentRecords(t, env, "ana@example.com"); len(records) != 0 {
		t.Fatalf("consent records = %d, want 0 — evidence exists only where somebody answered", len(records))
	}
	state := readConsentState(t, env, "ana@example.com")
	if state.PolicyAcceptedAt.Valid || state.MarketingConsent.Valid || state.NetworkingConsent.Valid {
		t.Fatalf("consent state was written by an abandoned step: %+v", state)
	}
	// The token the visitor walked away from cannot be presented by anybody else
	// either, because they never saw it — but it is also worthless to them: the
	// session it would have bought does not exist until it is spent.
	if n := countPendingConsents(t, env); n != 1 {
		t.Fatalf("pending consents = %d, want the one unspent row", n)
	}
}

// TestConsentSubmissionRecordsEvidenceAndMintsTheSession is the crossing: the
// evidence is written with every field the guidance's template asks for, and
// the session that comes back is the one the sign-in would have minted.
func TestConsentSubmissionRecordsEvidenceAndMintsTheSession(t *testing.T) {
	env := setupTest(t)

	data := startSignIn(t, env, "ana@example.com")
	token := data.ConsentRequired.PendingConsentToken

	resp, body := env.post(t, customerConsentPath,
		consentAnswers(token, true, true, false), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("error=%+v, want none", body.Error)
	}
	submitted := decodeCustomerVerify(t, body)
	if submitted.SessionID == "" {
		t.Fatal("expected a Customer Session token from the consent submission")
	}
	if submitted.ConsentRequired != nil {
		t.Fatalf("consent_required = %+v, want null once consent is recorded", submitted.ConsentRequired)
	}
	if submitted.Session.Email != "ana@example.com" {
		t.Fatalf("session email = %q, want ana@example.com", submitted.Session.Email)
	}

	// The session is a real, full Customer Session: it authenticates, and it is
	// scoped to the Customer rather than to one Ticket Sale.
	resp, body = env.get(t, customerSessionPath, authHeader(submitted.SessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session read status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if submitted.Session.TicketSaleID != nil {
		t.Fatalf("ticket_sale_id = %v, want null on a full Customer Session", *submitted.Session.TicketSaleID)
	}
	// The same 180-day window an ordinary sign-in earns.
	if got, want := sessionExpiry(t, env, submitted.SessionID), env.fixedClock.Add(customerSessionDuration); !got.Equal(want) {
		t.Fatalf("session expires_at = %s, want %s", got, want)
	}

	records := readConsentRecords(t, env, "ana@example.com")
	if len(records) != 1 {
		t.Fatalf("consent records = %d, want exactly one per capture act", len(records))
	}
	record := records[0]
	if record.Email != "ana@example.com" {
		t.Fatalf("record email = %q", record.Email)
	}
	if record.Channel != "signin" {
		t.Fatalf("channel = %q, want signin", record.Channel)
	}
	if !record.CapturedAt.Equal(env.fixedClock) {
		t.Fatalf("captured_at = %s, want the server clock %s", record.CapturedAt, env.fixedClock)
	}
	if record.PolicyVersionID != currentPolicyVersionID(t, env) {
		t.Fatalf("policy_version_id = %q, want the current edition %q", record.PolicyVersionID, currentPolicyVersionID(t, env))
	}
	if !record.PolicyAcceptance.Valid || !record.PolicyAcceptance.Bool {
		t.Fatalf("policy_acceptance = %+v, want true", record.PolicyAcceptance)
	}
	if !record.MarketingConsent.Valid || !record.MarketingConsent.Bool {
		t.Fatalf("marketing_consent = %+v, want the tick that was made", record.MarketingConsent)
	}
	// Shown and left unticked: false, and emphatically not null. Null is "not
	// shown", which is a different fact.
	if !record.NetworkingConsent.Valid || record.NetworkingConsent.Bool {
		t.Fatalf("networking_consent = %+v, want an explicit false", record.NetworkingConsent)
	}
	if !record.EmailProven {
		t.Fatal("email_proven = false — a passcode is proof of email ownership")
	}
	if record.ConfirmedAt.Valid {
		t.Fatalf("confirmed_at = %v, want null — nothing here is pending", record.ConfirmedAt.Time)
	}
	// The technical proof, all four fields.
	if record.IP.String != "198.51.100.24" {
		t.Fatalf("ip = %q, want the address the BFF derived", record.IP.String)
	}
	if record.UserAgent.String != "Mozilla/5.0 (consent-test)" {
		t.Fatalf("user_agent = %q", record.UserAgent.String)
	}
	if record.OriginURL.String != "http://storefront.example/es/signin" {
		t.Fatalf("origin_url = %q", record.OriginURL.String)
	}
	// The session identifier ties the evidence to what the person did next, and
	// it is the session this very act minted.
	if record.SessionID.String != submitted.SessionID {
		t.Fatalf("session_id = %q, want the session this capture minted", record.SessionID.String)
	}

	state := readConsentState(t, env, "ana@example.com")
	if !state.PolicyAcceptedAt.Valid || !state.PolicyAcceptedAt.Time.Equal(env.fixedClock) {
		t.Fatalf("policy_accepted_at = %+v, want the capture's server clock", state.PolicyAcceptedAt)
	}
	if state.PolicyVersionID.String != currentPolicyVersionID(t, env) {
		t.Fatalf("stored policy_version_id = %q, want the current edition", state.PolicyVersionID.String)
	}
	if state.MarketingConsent.String != "granted" {
		t.Fatalf("marketing_consent = %q, want granted", state.MarketingConsent.String)
	}
	if state.NetworkingConsent.String != "denied" {
		t.Fatalf("networking_consent = %q, want denied", state.NetworkingConsent.String)
	}
}

// TestConsentSubmissionWithoutPolicyAcceptanceIsRefusedByTheAPI is the property
// that a disabled submit button cannot provide: the refusal is the platform's,
// not the form's.
func TestConsentSubmissionWithoutPolicyAcceptanceIsRefusedByTheAPI(t *testing.T) {
	env := setupTest(t)

	data := startSignIn(t, env, "ana@example.com")
	token := data.ConsentRequired.PendingConsentToken

	resp, body := env.post(t, customerConsentPath,
		consentAnswers(token, false, true, true), consentEvidenceHeaders())
	assertAPIError(t, resp, body, http.StatusBadRequest, "POLICY_ACCEPTANCE_REQUIRED")

	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0 after a refused submission", n)
	}
	// Nothing was recorded and nothing became true — including the two optional
	// boxes the refused request ticked.
	if records := readConsentRecords(t, env, "ana@example.com"); len(records) != 0 {
		t.Fatalf("consent records = %d, want 0", len(records))
	}
	state := readConsentState(t, env, "ana@example.com")
	if state.PolicyAcceptedAt.Valid || state.MarketingConsent.Valid || state.NetworkingConsent.Valid {
		t.Fatalf("a refused submission wrote state: %+v", state)
	}

	// The proof is spent with the token: correcting the answer means signing in
	// again, not retrying against the same credential.
	resp, body = env.post(t, customerConsentPath,
		consentAnswers(token, true, false, false), consentEvidenceHeaders())
	assertAPIError(t, resp, body, http.StatusUnauthorized, "PENDING_CONSENT_INVALID")
}

// TestPendingConsentTokenIsSingleUse proves the credential is spent by using it,
// so a replayed submission mints no second session.
func TestPendingConsentTokenIsSingleUse(t *testing.T) {
	env := setupTest(t)

	data := startSignIn(t, env, "ana@example.com")
	token := data.ConsentRequired.PendingConsentToken

	resp, body := env.post(t, customerConsentPath, consentAnswers(token, true, false, false), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first submission status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.post(t, customerConsentPath, consentAnswers(token, true, true, true), consentEvidenceHeaders())
	assertAPIError(t, resp, body, http.StatusUnauthorized, "PENDING_CONSENT_INVALID")

	if n := countCustomerSessions(t, env); n != 1 {
		t.Fatalf("customer sessions = %d, want the single session the first submission minted", n)
	}
	if records := readConsentRecords(t, env, "ana@example.com"); len(records) != 1 {
		t.Fatalf("consent records = %d, want one — a replay must not write a second act", len(records))
	}
}

// TestPendingConsentTokenExpires: proof of email ownership held in suspension
// does not stay spendable.
func TestPendingConsentTokenExpires(t *testing.T) {
	env := setupTest(t)

	data := startSignIn(t, env, "ana@example.com")
	token := data.ConsentRequired.PendingConsentToken

	setCustomerClock(t, env, env.fixedClock.Add(16*time.Minute))

	resp, body := env.post(t, customerConsentPath, consentAnswers(token, true, true, true), consentEvidenceHeaders())
	assertAPIError(t, resp, body, http.StatusUnauthorized, "PENDING_CONSENT_INVALID")
	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0", n)
	}
}

// TestUnknownPendingConsentTokenIsRefusedIndistinguishably: the three ways a
// token can fail — never existed, already spent, expired — answer alike, so the
// endpoint cannot be probed for which sign-ins are in flight.
func TestUnknownPendingConsentTokenIsRefusedIndistinguishably(t *testing.T) {
	env := setupTest(t)

	data := startSignIn(t, env, "ana@example.com")
	spent := data.ConsentRequired.PendingConsentToken
	resp, body := env.post(t, customerConsentPath, consentAnswers(spent, true, false, false), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first submission status=%d error=%+v", resp.StatusCode, body.Error)
	}

	spentResp, spentBody := env.post(t, customerConsentPath, consentAnswers(spent, true, false, false), consentEvidenceHeaders())
	unknownResp, unknownBody := env.post(t, customerConsentPath,
		consentAnswers("0000000000000000000000000000000000000000000000000000000000000000", true, false, false),
		consentEvidenceHeaders())

	if spentResp.StatusCode != unknownResp.StatusCode {
		t.Fatalf("statuses spent=%d unknown=%d — the two must not be distinguishable",
			spentResp.StatusCode, unknownResp.StatusCode)
	}
	if spentBody.Error == nil || unknownBody.Error == nil ||
		spentBody.Error.Code != unknownBody.Error.Code ||
		spentBody.Error.Message != unknownBody.Error.Message {
		t.Fatalf("errors spent=%+v unknown=%+v — the two must not be distinguishable",
			spentBody.Error, unknownBody.Error)
	}
}

// TestAnsweredCustomerSignsInWithNoConsentStep is user story 4: a returning
// Customer who has accepted the current edition and answered every box sees no
// consent UI at all.
func TestAnsweredCustomerSignsInWithNoConsentStep(t *testing.T) {
	env := setupTest(t)

	first := startSignIn(t, env, "ana@example.com")
	resp, body := env.post(t, customerConsentPath,
		consentAnswers(first.ConsentRequired.PendingConsentToken, true, true, true), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}

	second := startSignIn(t, env, "ana@example.com")
	if second.ConsentRequired != nil {
		t.Fatalf("consent_required = %+v — an answered Customer must sail through", second.ConsentRequired)
	}
	if second.SessionID == "" {
		t.Fatal("expected a session straight from the verify")
	}
	if second.Session.Email != "ana@example.com" {
		t.Fatalf("session email = %q", second.Session.Email)
	}
	// One capture act, one record: signing in again is not a capture.
	if records := readConsentRecords(t, env, "ana@example.com"); len(records) != 1 {
		t.Fatalf("consent records = %d, want one", len(records))
	}
}

// TestMarketingBoxUntickedRecordsDeniedAndSwitchesTheDigestOff is ADR 0034's
// lockstep, in the direction that costs something: a Customer who Follows
// busily and skips the box has said No, and the Digest stops.
func TestMarketingBoxUntickedRecordsDeniedAndSwitchesTheDigestOff(t *testing.T) {
	env := setupTest(t)

	data := startSignIn(t, env, "ana@example.com")
	// A legacy subscriber, made by hand: the flag on and nothing answered, which
	// is what every Customer row carried before migration 065 and what the rows
	// that predate consent carry still. It is not consent and is never claimed as
	// such — but until somebody answers it is operative, and that is what makes
	// skipping the box below cost this Customer something rather than nothing.
	forgetConsentAnswers(t, env, "ana@example.com", true)
	resp, body := env.post(t, customerConsentPath,
		consentAnswers(data.ConsentRequired.PendingConsentToken, true, false, false), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "denied" {
		t.Fatalf("marketing_consent = %q, want denied — silence at a capture moment is a No", state.MarketingConsent.String)
	}
	if state.DigestEnabled {
		t.Fatal("digest_enabled is still true after Marketing Consent was denied — the two are one switch (ADR 0034)")
	}
}

// TestMarketingBoxTickedGrantsAndSwitchesTheDigestOn is the same switch in the
// other direction, from a Customer whose Digest was off when they arrived.
func TestMarketingBoxTickedGrantsAndSwitchesTheDigestOn(t *testing.T) {
	env := setupTest(t)

	data := startSignIn(t, env, "ana@example.com")
	// Off before the capture. SQL because the only ways to switch it off today
	// are the Customer Area toggle and the unsubscribe link, both of which need a
	// session this Customer does not have yet — and the state under test is
	// precisely somebody who unsubscribed long ago and is now being asked
	// properly for the first time.
	if _, err := env.db.Exec(`UPDATE customers SET digest_enabled = FALSE WHERE email = $1`, "ana@example.com"); err != nil {
		t.Fatalf("switch the digest off: %v", err)
	}

	resp, body := env.post(t, customerConsentPath,
		consentAnswers(data.ConsentRequired.PendingConsentToken, true, true, false), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "granted" {
		t.Fatalf("marketing_consent = %q, want granted", state.MarketingConsent.String)
	}
	if !state.DigestEnabled {
		t.Fatal("digest_enabled is false after Marketing Consent was granted — the two are one switch (ADR 0034)")
	}
}

// TestPublishingAPolicyVersionRegatesSignInWithTheRequiredBoxOnly is the
// version-awareness proof, and the reason acceptance stores an edition rather
// than a timestamp: a new row re-gates everybody, and re-prompting must not
// churn standing optional answers (user story 22).
func TestPublishingAPolicyVersionRegatesSignInWithTheRequiredBoxOnly(t *testing.T) {
	env := setupTest(t)

	first := startSignIn(t, env, "ana@example.com")
	resp, body := env.post(t, customerConsentPath,
		consentAnswers(first.ConsentRequired.PendingConsentToken, true, true, false), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	firstVersion := currentPolicyVersionID(t, env)

	// An hour later, so the two capture acts are distinguishable by their own
	// timestamps rather than by the order a query happens to return them in.
	setSignInClock(t, env, env.fixedClock.Add(time.Hour))

	newVersion := publishPolicyVersion(t, env, "1-test-edition")
	if newVersion == firstVersion {
		t.Fatal("expected the published edition to be a new row")
	}

	regated := startSignIn(t, env, "ana@example.com")
	if regated.ConsentRequired == nil {
		t.Fatal("publishing an edition must re-gate a Customer who accepted the previous one")
	}
	want := consentBoxes{PolicyAcceptance: true}
	if regated.ConsentRequired.Boxes != want {
		t.Fatalf("boxes = %+v, want the required box alone: %+v", regated.ConsentRequired.Boxes, want)
	}

	resp, body = env.post(t, customerConsentPath,
		// The browser sends nothing for the boxes it was not shown; a crafted body
		// that DID name them changes nothing either, which is what the two false
		// values here quietly assert.
		consentAnswers(regated.ConsentRequired.PendingConsentToken, true, false, false), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-acceptance status=%d error=%+v", resp.StatusCode, body.Error)
	}

	state := readConsentState(t, env, "ana@example.com")
	if state.PolicyVersionID.String != newVersion {
		t.Fatalf("stored policy_version_id = %q, want the newly published edition %q", state.PolicyVersionID.String, newVersion)
	}
	if state.MarketingConsent.String != "granted" {
		t.Fatalf("marketing_consent = %q — a version bump must not churn a standing optional answer", state.MarketingConsent.String)
	}
	if state.NetworkingConsent.String != "denied" {
		t.Fatalf("networking_consent = %q — a version bump must not churn a standing optional answer", state.NetworkingConsent.String)
	}
	if !state.DigestEnabled {
		t.Fatal("digest_enabled changed on a re-acceptance that never showed the marketing box")
	}

	records := readConsentRecords(t, env, "ana@example.com")
	if len(records) != 2 {
		t.Fatalf("consent records = %d, want one per capture act", len(records))
	}
	// The second act records NULL for the boxes it did not show — "not shown" and
	// "answered No" are different facts, and only one of them is true here.
	second := records[1]
	if second.PolicyVersionID != newVersion {
		t.Fatalf("second record policy_version_id = %q, want %q", second.PolicyVersionID, newVersion)
	}
	if !second.PolicyAcceptance.Valid || !second.PolicyAcceptance.Bool {
		t.Fatalf("second record policy_acceptance = %+v, want true", second.PolicyAcceptance)
	}
	if second.MarketingConsent.Valid || second.NetworkingConsent.Valid {
		t.Fatalf("second record recorded answers for boxes nobody was shown: marketing=%+v networking=%+v",
			second.MarketingConsent, second.NetworkingConsent)
	}
}

// TestBoxOfficeCustomerIsGatedByTheSamePredicate: no special cases. A Customer a
// staff sale created has never accepted anything, and is stopped at their first
// sign-in exactly as a stranger is — because nobody may attest on their behalf.
func TestBoxOfficeCustomerIsGatedByTheSamePredicate(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	seedSaleForCustomer(t, env, sessionID, "Box Office Fest", "box-office-fest",
		env.fixedClock.Add(30*24*time.Hour), "box-1", "ana@example.com", "Ana", "Lopez")

	data := startSignIn(t, env, "ana@example.com")

	if data.ConsentRequired == nil {
		t.Fatal("a Customer created by a box-office sale must be prompted at their first sign-in")
	}
	want := consentBoxes{PolicyAcceptance: true, MarketingConsent: true, NetworkingConsent: true}
	if data.ConsentRequired.Boxes != want {
		t.Fatalf("boxes = %+v, want %+v — the same predicate, with no special case", data.ConsentRequired.Boxes, want)
	}
	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0", n)
	}
}

// TestNothingAboutConsentIsDisclosedBeforeProofOfEmailOwnership is the oracle
// discipline (ADR 0035, user story 29). Consent state is a fact about a known
// Customer, so it must not be observable from anything an anonymous caller can
// ask — not from the passcode request, and not from a failed verification.
func TestNothingAboutConsentIsDisclosedBeforeProofOfEmailOwnership(t *testing.T) {
	env := setupTest(t)

	// A Customer with everything answered, and one with nothing — the two ends of
	// the state the gate reads.
	answered := startSignIn(t, env, "answered@example.com")
	resp, body := env.post(t, customerConsentPath,
		consentAnswers(answered.ConsentRequired.PendingConsentToken, true, true, true), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	startSignIn(t, env, "outstanding@example.com")

	// The passcode request: byte-identical for the answered Customer, the one
	// with consent outstanding, and an address the platform has never seen.
	_, answeredBody := requestCustomerPasscode(t, env, "answered@example.com", "")
	_, outstandingBody := requestCustomerPasscode(t, env, "outstanding@example.com", "")
	_, strangerBody := requestCustomerPasscode(t, env, "nobody-here@example.com", "")
	if !bytes.Equal(answeredBody.Data, outstandingBody.Data) || !bytes.Equal(answeredBody.Data, strangerBody.Data) {
		t.Fatalf("passcode request became a consent oracle: answered=%s outstanding=%s stranger=%s",
			answeredBody.Data, outstandingBody.Data, strangerBody.Data)
	}

	// A wrong passcode: identical refusal whichever address it was for, so the
	// consent-required outcome is reachable only past a correct code.
	wrongForOutstanding := verifyWrongPasscode(t, env, "outstanding@example.com")
	wrongForStranger := verifyWrongPasscode(t, env, "nobody-here@example.com")
	if wrongForOutstanding != wrongForStranger {
		t.Fatalf("a wrong passcode answered differently for a known address (%q) than an unknown one (%q)",
			wrongForOutstanding, wrongForStranger)
	}
}

// verifyWrongPasscode redeems a deliberately wrong code and returns the refusal
// as one comparable string: status and error code together.
func verifyWrongPasscode(t *testing.T, env *testEnv, email string) string {
	t.Helper()
	resp, body := requestCustomerPasscode(t, env, email, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body = env.post(t, customerOTPVerifyPath, map[string]string{
		"email": email,
		"code":  "000000",
	}, nil)
	if body.Error == nil {
		t.Fatalf("expected a refusal for a wrong passcode, got %s", body.Data)
	}
	return http.StatusText(resp.StatusCode) + ":" + body.Error.Code
}

// TestGoogleSignInIsGatedAtTheSameConvergence proves the two doors cannot
// diverge: Google Sign-In is Proof of Email Ownership by the same rule a
// passcode is, and it meets the same gate because both converge on one function.
// (#252 owns Google's own consent tests and the Storefront work; this asserts
// the convergence exists.)
func TestGoogleSignInIsGatedAtTheSameConvergence(t *testing.T) {
	env := setupTest(t)
	googleStub.returns(verifiedGoogleClaims("ana@example.com"))

	resp, body := postGoogleVerify(t, env)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("google verify status=%d error=%+v", resp.StatusCode, body.Error)
	}
	data := decodeCustomerVerify(t, body)
	if data.SessionID != "" {
		t.Fatalf("session_id = %q — the Google door must be gated exactly as the passcode door is", data.SessionID)
	}
	if data.ConsentRequired == nil {
		t.Fatal("expected a consent-required outcome from the Google door")
	}
	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0", n)
	}

	// And the token it minted finishes the sign-in through the same endpoint.
	resp, body = env.post(t, customerConsentPath,
		consentAnswers(data.ConsentRequired.PendingConsentToken, true, false, false), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if decodeCustomerVerify(t, body).SessionID == "" {
		t.Fatal("expected a Customer Session after the Google door's consent step")
	}
	records := readConsentRecords(t, env, "ana@example.com")
	if len(records) != 1 || records[0].Channel != "signin" || !records[0].EmailProven {
		t.Fatalf("records = %+v, want one proven sign-in capture", records)
	}
}
