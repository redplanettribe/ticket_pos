package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Consent capture at the Storefront checkout (#253, parent #249): the gate that
// refuses a checkout without Policy Acceptance, the answers riding the Payment
// Provider redirect on the Payment, and the evidence written only when the sale
// commits.
//
// The seam is HTTP, as everywhere else in this package: begin, confirm, and what
// the platform is observably left holding. The Consent Records and the consent
// columns on `customers` are read with SQL for the reason customer_consent_test.go
// states — the platform publishes neither, deliberately — and the helpers for
// both live there.
//
// The three properties every test here is a reading of:
//
//   - Nobody reaches a Payment Provider under an unaccepted Privacy Policy.
//   - No Payment that failed to become a sale leaves any trace of consent.
//   - Nothing a guest ticks can speak for an inbox they have not proven.

// consentCheckoutBody is a begin-checkout body with the three boxes stated
// explicitly. A nil answer omits the key entirely, which is the wire's way of
// saying the box was not shown — not the same as sending false.
func consentCheckoutBody(email, firstName, lastName string, policy, marketing, networking *bool, lines ...map[string]any) map[string]any {
	body := checkoutBody(email, firstName, lastName, lines...)
	delete(body, "policy_acceptance")
	if policy != nil {
		body["policy_acceptance"] = *policy
	}
	if marketing != nil {
		body["marketing_consent"] = *marketing
	}
	if networking != nil {
		body["networking_consent"] = *networking
	}
	return body
}

// beginCheckoutWithEvidence begins a checkout carrying the circumstances a real
// request arrives with — the client IP as the BFF derived it, the browser's user
// agent, and the page the dialog was open on. The same headers the sign-in
// consent step is tested with, because the evidence is the same evidence.
func beginCheckoutWithEvidence(t *testing.T, env *testEnv, orgSlug, eventSlug, token string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	headers := map[string]string{
		"X-BFF-Client-IP": "198.51.100.24",
		"User-Agent":      "Mozilla/5.0 (checkout-consent-test)",
		"Referer":         "http://storefront.example/es/test-org/events/consent-fest",
	}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	return env.post(t, "/api/v1/public/organizations/"+orgSlug+"/events/"+eventSlug+"/checkout", body, headers)
}

func beginCheckoutWithEvidenceOK(t *testing.T, env *testEnv, orgSlug, eventSlug, token string, body map[string]any) beginCheckoutResult {
	t.Helper()
	resp, envBody := beginCheckoutWithEvidence(t, env, orgSlug, eventSlug, token, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("begin checkout status=%d error=%+v", resp.StatusCode, envBody.Error)
	}
	var result beginCheckoutResult
	if err := json.Unmarshal(envBody.Data, &result); err != nil {
		t.Fatalf("decode begin checkout result: %v", err)
	}
	return result
}

// countConsentRecords counts the whole evidence log, for the assertions whose
// point is that there is nothing in it at all.
func countConsentRecords(t *testing.T, env *testEnv) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM consent_records`).Scan(&n); err != nil {
		t.Fatalf("count consent records: %v", err)
	}
	return n
}

// paymentConsentSnapshot is what a Payment is holding between begin and commit
// (migration 064): the three answers, and the circumstances they were given in.
type paymentConsentSnapshot struct {
	PolicyAcceptance  *bool
	MarketingConsent  *bool
	NetworkingConsent *bool
	IP                *string
	UserAgent         *string
	OriginURL         *string
}

func readPaymentConsent(t *testing.T, env *testEnv, clientTransactionID string) paymentConsentSnapshot {
	t.Helper()
	var s paymentConsentSnapshot
	if err := env.db.QueryRow(`
		SELECT consent_policy_acceptance, consent_marketing, consent_networking,
		       consent_ip, consent_user_agent, consent_origin_url
		FROM payments WHERE client_transaction_id = $1
	`, clientTransactionID).Scan(&s.PolicyAcceptance, &s.MarketingConsent, &s.NetworkingConsent,
		&s.IP, &s.UserAgent, &s.OriginURL); err != nil {
		t.Fatalf("read payment consent snapshot %q: %v", clientTransactionID, err)
	}
	return s
}

// TestCheckoutRefusedWithoutPolicyAcceptance is the gate itself, and it is the
// API's gate rather than the dialog's: a caller that is not the Storefront gets
// the same refusal, and nothing at all is recorded.
func TestCheckoutRefusedWithoutPolicyAcceptance(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)
	line := cartLine(gaID, 1)

	cases := []struct {
		name   string
		policy *bool
	}{
		// A caller that never heard of the boxes: refused, so the deploy cannot be
		// bypassed by an old client.
		{name: "box absent", policy: nil},
		// Somebody who read the notice and declined. Their purchase does not
		// happen, and that is the parent spec's decision, not an accident.
		{name: "box explicitly declined", policy: boolPtr(false)},
	}
	for _, tc := range cases {
		resp, body := beginCheckoutWithEvidence(t, env, "test-org", "consent-fest", "",
			consentCheckoutBody("ana@example.com", "Ana", "Lopez", tc.policy, boolPtr(true), boolPtr(true), line))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status=%d, want 400", tc.name, resp.StatusCode)
		}
		if body.Error == nil || body.Error.Code != "POLICY_ACCEPTANCE_REQUIRED" {
			t.Fatalf("%s: error=%+v, want POLICY_ACCEPTANCE_REQUIRED", tc.name, body.Error)
		}
	}

	// The refusal is total. No Payment means no Capacity Hold on tickets nobody
	// is buying, and no reachable Payment Provider page: the whole reason the gate
	// is at begin rather than at confirm.
	var payments int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&payments); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	if payments != 0 {
		t.Fatalf("payments after refused begins = %d, want 0", payments)
	}
	if got := countConsentRecords(t, env); got != 0 {
		t.Fatalf("consent records after refused begins = %d, want 0", got)
	}
}

// TestCheckoutConsentSurvivesTheProviderRedirect walks the whole journey: the
// answers are held on the Payment across the redirect and become evidence only
// when the sale commits.
func TestCheckoutConsentSurvivesTheProviderRedirect(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)

	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", "",
		consentCheckoutBody("ana@example.com", "Ana", "Lopez",
			boolPtr(true), boolPtr(true), boolPtr(true), cartLine(gaID, 1)))

	// Between the two legs there is a Payment and nothing else. Not even a
	// Customer: the record the evidence would have to point at does not exist yet.
	if got := countConsentRecords(t, env); got != 0 {
		t.Fatalf("consent records before the sale commits = %d, want 0", got)
	}
	snapshot := readPaymentConsent(t, env, begin.ClientTransactionID)
	if snapshot.PolicyAcceptance == nil || !*snapshot.PolicyAcceptance {
		t.Fatalf("held policy_acceptance = %v, want true", snapshot.PolicyAcceptance)
	}
	if snapshot.MarketingConsent == nil || !*snapshot.MarketingConsent {
		t.Fatalf("held marketing_consent = %v, want true", snapshot.MarketingConsent)
	}
	if snapshot.NetworkingConsent == nil || !*snapshot.NetworkingConsent {
		t.Fatalf("held networking_consent = %v, want true", snapshot.NetworkingConsent)
	}
	// The circumstances of the request the buyer answered IN, not of the redirect
	// that will settle it.
	if snapshot.IP == nil || *snapshot.IP != "198.51.100.24" {
		t.Fatalf("held consent ip = %v, want the BFF-derived client address", snapshot.IP)
	}
	if snapshot.UserAgent == nil || *snapshot.UserAgent != "Mozilla/5.0 (checkout-consent-test)" {
		t.Fatalf("held consent user agent = %v", snapshot.UserAgent)
	}
	if snapshot.OriginURL == nil || *snapshot.OriginURL == "" {
		t.Fatalf("held consent origin url = %v, want the page the dialog was open on", snapshot.OriginURL)
	}

	confirmed := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	if confirmed.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirmed.Status)
	}

	records := readConsentRecords(t, env, "ana@example.com")
	if len(records) != 1 {
		t.Fatalf("consent records = %d, want exactly one for one capture act", len(records))
	}
	record := records[0]
	if record.Channel != "checkout" {
		t.Fatalf("channel = %q, want checkout", record.Channel)
	}
	// The address AS ASSERTED on the dialog, and unproven: a guest typed it.
	if record.Email != "ana@example.com" {
		t.Fatalf("record email = %q", record.Email)
	}
	if record.EmailProven {
		t.Fatal("email_proven = true on a guest checkout; nobody proved that address")
	}
	if !record.PolicyAcceptance.Valid || !record.PolicyAcceptance.Bool {
		t.Fatalf("recorded policy_acceptance = %+v, want true", record.PolicyAcceptance)
	}
	if !record.MarketingConsent.Valid || !record.MarketingConsent.Bool {
		t.Fatalf("recorded marketing_consent = %+v, want the tick as given", record.MarketingConsent)
	}
	if !record.NetworkingConsent.Valid || !record.NetworkingConsent.Bool {
		t.Fatalf("recorded networking_consent = %+v, want the tick as given", record.NetworkingConsent)
	}
	if record.PolicyVersionID != currentPolicyVersionID(t, env) {
		t.Fatalf("policy_version_id = %q, want the current edition, resolved server-side", record.PolicyVersionID)
	}
	if record.CapturedAt.IsZero() {
		t.Fatal("captured_at is zero; the server clock stamps every record")
	}
	if !record.IP.Valid || record.IP.String != "198.51.100.24" {
		t.Fatalf("recorded ip = %+v, want the address held since begin", record.IP)
	}
	if !record.UserAgent.Valid || record.UserAgent.String != "Mozilla/5.0 (checkout-consent-test)" {
		t.Fatalf("recorded user agent = %+v", record.UserAgent)
	}
	if !record.OriginURL.Valid || record.OriginURL.String == "" {
		t.Fatalf("recorded origin url = %+v", record.OriginURL)
	}
	// No session id, and that is the truth about a guest: there was no session.
	if record.SessionID.Valid {
		t.Fatalf("recorded session id = %+v, want none on a guest checkout", record.SessionID)
	}

	state := readConsentState(t, env, "ana@example.com")
	// Policy Acceptance is recorded unconditionally: it is a fact about the sale,
	// not a claim on an inbox (ADR 0035).
	if !state.PolicyAcceptedAt.Valid {
		t.Fatal("policy_accepted_at is null after an accepted checkout")
	}
	if state.PolicyVersionID.String != currentPolicyVersionID(t, env) {
		t.Fatalf("stored policy_version_id = %q, want the current edition", state.PolicyVersionID.String)
	}
	// The optional ticks are somebody's claim on an address nobody proved, so
	// they pend: recorded, denied for sending, unanswered for prompting.
	if state.MarketingConsent.String != "pending_confirmation" {
		t.Fatalf("marketing_consent = %q, want pending_confirmation from a guest tick", state.MarketingConsent.String)
	}
	if state.NetworkingConsent.String != "pending_confirmation" {
		t.Fatalf("networking_consent = %q, want pending_confirmation from a guest tick", state.NetworkingConsent.String)
	}
	// A pending marketing consent must not switch the Follow Digest on. The flag
	// is left exactly as it was — the legacy default on a Customer this sale just
	// created — and the sender's rule (never send on pending) is what stops the
	// mail (ADR 0034, ADR 0035).
	if !state.DigestEnabled {
		t.Fatal("digest_enabled = false; a pending tick must neither enable nor disable it")
	}
}

// TestGuestUntickedOptionalBoxesAnswerNothing: a guest's silence is evidence of
// what they were shown, and nothing at all about the person whose address they
// typed.
//
// The record keeps the false, because the log's job is to say what happened on
// the surface. The STATE does not move, because the state is the platform's
// belief about a Customer and this act proves nothing about them. Were it
// written as denied, two things would follow, both of which the feature exists
// to prevent: the Follow Digest of a legacy subscriber would stop, since the
// sender refuses a denial; and the real owner would never be asked again, since
// Outstanding treats denied as answered. A stranger would have decided for them,
// permanently, by leaving a box alone.
func TestGuestUntickedOptionalBoxesAnswerNothing(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)

	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", "",
		consentCheckoutBody("ana@example.com", "Ana", "Lopez",
			boolPtr(true), boolPtr(false), boolPtr(false), cartLine(gaID, 1)))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	records := readConsentRecords(t, env, "ana@example.com")
	if len(records) != 1 {
		t.Fatalf("consent records = %d, want one", len(records))
	}
	if !records[0].MarketingConsent.Valid || records[0].MarketingConsent.Bool {
		t.Fatalf("recorded marketing_consent = %+v, want a false that was SHOWN", records[0].MarketingConsent)
	}

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.Valid {
		t.Fatalf("marketing_consent = %q, want unanswered", state.MarketingConsent.String)
	}
	if state.NetworkingConsent.Valid {
		t.Fatalf("networking_consent = %q, want unanswered", state.NetworkingConsent.String)
	}
	// The flag does not move on an UNPROVEN answer, in either direction. A
	// stranger typing a legacy subscriber's address into a checkout must not be
	// able to silence their Digest (see service.Capture's digest lockstep).
	if !state.DigestEnabled {
		t.Fatal("digest_enabled = false after an unproven No; only a proven answer moves it")
	}
}

// TestUnprovenNoLeavesTheOwnerStillToBeAsked is the half of the rule above that
// the state assertions only imply: the person whose address was typed is still
// owed both boxes, so the next time they are at the keyboard themselves they get
// to answer for themselves.
func TestUnprovenNoLeavesTheOwnerStillToBeAsked(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)

	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", "",
		consentCheckoutBody("ana@example.com", "Ana", "Lopez",
			boolPtr(true), boolPtr(false), boolPtr(false), cartLine(gaID, 1)))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	// Ana signs in for the first time. She is not stopped for Policy Acceptance:
	// the checkout above accepted the edition in effect, and that acceptance is a
	// fact about the act rather than a claim on her inbox (ADR 0035).
	verify := startSignIn(t, env, "ana@example.com")
	if verify.ConsentRequired != nil {
		t.Fatal("consent step for a Customer whose Policy Acceptance is already stamped at the current edition")
	}

	// Both optional boxes are still hers to answer. Had the guest's silence been
	// recorded as her No, this would report nothing outstanding and she would
	// never be offered either again.
	assertBoxes(t, signedInConsentBoxes(t, env, verify.SessionID), false, true, true,
		"a guest's silence answered nothing on her behalf")
}

// TestUnshownOptionalBoxesRecordNothing: a body that omits the optional boxes is
// saying they were not shown, which is not a refusal. It is the shape #254's
// signed-in dialog will send, and the shape every checkout in the rest of this
// suite sends today.
func TestUnshownOptionalBoxesRecordNothing(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)

	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", "",
		consentCheckoutBody("ana@example.com", "Ana", "Lopez",
			boolPtr(true), nil, nil, cartLine(gaID, 1)))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	records := readConsentRecords(t, env, "ana@example.com")
	if len(records) != 1 {
		t.Fatalf("consent records = %d, want one", len(records))
	}
	if records[0].MarketingConsent.Valid || records[0].NetworkingConsent.Valid {
		t.Fatalf("recorded optional answers = %+v/%+v, want NULL for boxes that were not shown",
			records[0].MarketingConsent, records[0].NetworkingConsent)
	}

	state := readConsentState(t, env, "ana@example.com")
	if !state.PolicyAcceptedAt.Valid {
		t.Fatal("policy_accepted_at is null; the required box was answered")
	}
	if state.MarketingConsent.Valid || state.NetworkingConsent.Valid {
		t.Fatalf("optional state = %+v/%+v, want unanswered", state.MarketingConsent, state.NetworkingConsent)
	}
}

// TestAbandonedCheckoutRecordsNoConsent is the parent spec's decision 30, and
// the reason the answers are HELD rather than recorded at begin: evidence exists
// only where a transaction did.
func TestAbandonedCheckoutRecordsNoConsent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)
	line := cartLine(gaID, 1)

	// Declined at the provider.
	declined := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", "",
		consentCheckoutBody("declined@example.com", "Ana", "Lopez",
			boolPtr(true), boolPtr(true), boolPtr(true), line))
	if got := confirmCheckoutOK(t, env, declined.ClientTransactionID, "declined"); got.Status != "failed" {
		t.Fatalf("confirm status = %q, want failed", got.Status)
	}

	// Never returned from the payment page at all.
	beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", "",
		consentCheckoutBody("abandoned@example.com", "Ana", "Lopez",
			boolPtr(true), boolPtr(true), boolPtr(true), line))

	if got := countConsentRecords(t, env); got != 0 {
		t.Fatalf("consent records after a declined and an abandoned Payment = %d, want 0", got)
	}
	// Exactly the same rule that leaves them no Customer: the two are one
	// transaction, so neither can exist without the other.
	var customers int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM customers`).Scan(&customers); err != nil {
		t.Fatalf("count customers: %v", err)
	}
	if customers != 0 {
		t.Fatalf("customers after unsettled Payments = %d, want 0", customers)
	}
}

// TestGuestAnswerNeverFlipsAProvenAnswer is ADR 0035's adversarial case: the
// state keeps what its owner said, and the log still records what the stranger
// did. Both halves matter — the evidence records what happened, not only what
// became true.
func TestGuestAnswerNeverFlipsAProvenAnswer(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)
	line := cartLine(gaID, 1)

	// Ana proves her address and grants both optional consents herself.
	customerSignIn(t, env, "ana@example.com")
	before := readConsentState(t, env, "ana@example.com")
	if before.MarketingConsent.String != "granted" || before.NetworkingConsent.String != "granted" {
		t.Fatalf("state after her own sign-in = %+v/%+v, want granted", before.MarketingConsent, before.NetworkingConsent)
	}

	// A stranger checks out under her address and unticks both.
	stranger := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", "",
		consentCheckoutBody("ana@example.com", "Ana", "Lopez",
			boolPtr(true), boolPtr(false), boolPtr(false), line))
	confirmCheckoutOK(t, env, stranger.ClientTransactionID, "approved")

	after := readConsentState(t, env, "ana@example.com")
	if after.MarketingConsent.String != "granted" {
		t.Fatalf("marketing_consent = %q, want the untouched granted she gave herself", after.MarketingConsent.String)
	}
	if after.NetworkingConsent.String != "granted" {
		t.Fatalf("networking_consent = %q, want the untouched granted", after.NetworkingConsent.String)
	}
	if !after.DigestEnabled {
		t.Fatal("digest_enabled = false; a guest's No may not unsubscribe a proven Customer")
	}

	// The tick that changed nothing still left its record — two acts, two rows.
	if got := len(readConsentRecords(t, env, "ana@example.com")); got != 2 {
		t.Fatalf("consent records = %d, want two: her sign-in and the stranger's checkout", got)
	}
	checkouts := consentRecordsOn(t, env, "ana@example.com", "checkout")
	if len(checkouts) != 1 {
		t.Fatalf("checkout records = %d, want the stranger's one", len(checkouts))
	}
	guest := checkouts[0]
	if guest.EmailProven {
		t.Fatal("email_proven = true on the stranger's checkout; nobody proved that address")
	}
	if !guest.MarketingConsent.Valid || guest.MarketingConsent.Bool {
		t.Fatalf("second record marketing_consent = %+v, want the false the stranger submitted", guest.MarketingConsent)
	}
	// And her Policy Acceptance is re-stamped by the act, because that half IS
	// recorded unconditionally — it evidences that the person transacting was
	// informed, whoever they were.
	if !guest.PolicyAcceptance.Valid || !guest.PolicyAcceptance.Bool {
		t.Fatalf("second record policy_acceptance = %+v, want true", guest.PolicyAcceptance)
	}
}

// TestGuestTickNeverUpgradesADeclinedConsent is the mirror image: the owner said
// No, and a stranger's tick may not turn it into a Pending Confirmation the
// owner would then be re-asked about.
func TestGuestTickNeverUpgradesADeclinedConsent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)

	// She signs in and declines marketing, which the helper does not do — so this
	// one is built by hand, through the same door.
	verify := startSignIn(t, env, "ana@example.com")
	if verify.ConsentRequired == nil {
		t.Fatal("expected a consent step for a Customer who has never accepted")
	}
	resp, body := env.post(t, customerConsentPath,
		consentAnswers(verify.ConsentRequired.PendingConsentToken, true, false, false),
		consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if got := readConsentState(t, env, "ana@example.com"); got.MarketingConsent.String != "denied" {
		t.Fatalf("marketing_consent after her own No = %q, want denied", got.MarketingConsent.String)
	}

	guest := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", "",
		consentCheckoutBody("ana@example.com", "Ana", "Lopez",
			boolPtr(true), boolPtr(true), boolPtr(true), cartLine(gaID, 1)))
	confirmCheckoutOK(t, env, guest.ClientTransactionID, "approved")

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "denied" {
		t.Fatalf("marketing_consent = %q, want the owner's denied, untouched by a guest tick", state.MarketingConsent.String)
	}
	if state.NetworkingConsent.String != "denied" {
		t.Fatalf("networking_consent = %q, want the owner's denied", state.NetworkingConsent.String)
	}
	if state.DigestEnabled {
		t.Fatal("digest_enabled = true; a guest tick may not resubscribe somebody who declined")
	}
}

// TestSignedInCheckoutCapturesAProvenAnswer: the same dialog, under the buyer's
// own Customer Session for their own address, is proof of ownership — so the
// tick is a grant and the Digest moves with it (ADR 0034).
//
// Which boxes a signed-in Customer is SHOWN is #254's; this is about what an
// answer from one means when it arrives.
func TestSignedInCheckoutCapturesAProvenAnswer(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)

	// Signed in with the two optional consents left UNANSWERED, so the checkout
	// is genuinely the surface that asks them and its answer is visibly the thing
	// that moved the state.
	//
	// It used to sign in DECLINING them, and #254 is why it cannot: a Customer
	// who has answered a box is not shown it again, and an answer for a box they
	// were not shown is dropped rather than applied
	// (service.owedConsentAnswers). The old shape would now assert that a body
	// can churn a standing answer, which is exactly what must not be true.
	verify := startSignIn(t, env, "ana@example.com")
	if verify.ConsentRequired == nil {
		t.Fatal("expected a consent step")
	}
	resp, body := env.post(t, customerConsentPath,
		consentAnswers(verify.ConsentRequired.PendingConsentToken, true, false, false),
		consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	token := decodeCustomerVerify(t, body).SessionID
	if token == "" {
		t.Fatal("expected a Customer Session after the consent step")
	}
	// Back to unanswered: no surface can UNanswer a consent, so this is written
	// directly. It is a starting state — the one a Customer created by a box
	// office sale or a Sale Import carries — and not a transition.
	if _, err := env.db.Exec(
		`UPDATE customers SET marketing_consent = NULL, networking_consent = NULL WHERE email = $1`,
		"ana@example.com"); err != nil {
		t.Fatalf("clear optional consents: %v", err)
	}

	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", token,
		consentCheckoutBody("ana@example.com", "Ana", "Lopez",
			boolPtr(true), boolPtr(true), boolPtr(true), cartLine(gaID, 1)))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	checkouts := consentRecordsOn(t, env, "ana@example.com", "checkout")
	if len(checkouts) != 1 {
		t.Fatalf("checkout records = %d, want one", len(checkouts))
	}
	if !checkouts[0].EmailProven {
		t.Fatal("email_proven = false on a checkout under the buyer's own session for their own address")
	}

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "granted" {
		t.Fatalf("marketing_consent = %q, want granted from a proven tick", state.MarketingConsent.String)
	}
	if state.NetworkingConsent.String != "granted" {
		t.Fatalf("networking_consent = %q, want granted", state.NetworkingConsent.String)
	}
	if !state.DigestEnabled {
		t.Fatal("digest_enabled = false; a proven grant turns the weekly Digest on (ADR 0034)")
	}
}

// TestFreeCheckoutCapturesConsentToo: a cart that totals zero settles inside the
// begin request with no provider involved (ADR 0017), and goes through the same
// commit spine — so the evidence is written there too, and the gate applies
// there too.
func TestFreeCheckoutCapturesConsentToo(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, freeID := publishCheckoutEvent(t, env, sessionID, "Free Consent Fest", "free-consent-fest", 0, 10)
	line := cartLine(freeID, 1)

	// Refused for the same reason a paid one is: a free ticket is still a Customer
	// record created and a receipt emailed.
	resp, body := beginCheckoutWithEvidence(t, env, "test-org", "free-consent-fest", "",
		consentCheckoutBody("declined@example.com", "Ana", "Lopez", nil, nil, nil, line))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "POLICY_ACCEPTANCE_REQUIRED" {
		t.Fatalf("free checkout without acceptance: status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, envBody := beginCheckoutWithEvidence(t, env, "test-org", "free-consent-fest", "",
		consentCheckoutBody("ana@example.com", "Ana", "Lopez",
			boolPtr(true), boolPtr(true), nil, line))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("free checkout status=%d error=%+v", resp.StatusCode, envBody.Error)
	}
	var claim freeCheckoutResult
	if err := json.Unmarshal(envBody.Data, &claim); err != nil {
		t.Fatalf("decode free claim: %v", err)
	}
	approvedRef(t, claim)

	records := readConsentRecords(t, env, "ana@example.com")
	if len(records) != 1 {
		t.Fatalf("consent records = %d, want one written by the free settlement", len(records))
	}
	if records[0].Channel != "checkout" {
		t.Fatalf("channel = %q, want checkout", records[0].Channel)
	}
	if got := readConsentState(t, env, "ana@example.com"); got.MarketingConsent.String != "pending_confirmation" {
		t.Fatalf("marketing_consent = %q, want pending_confirmation", got.MarketingConsent.String)
	}
}

// TestCheckoutAcceptanceStampsTheEditionInEffect: publishing a Policy Version
// re-gates everybody, and a checkout accepts the edition that is current at the
// moment of capture — which the platform resolves, never the client.
func TestCheckoutAcceptanceStampsTheEditionInEffect(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)
	line := cartLine(gaID, 1)

	first := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", "",
		consentCheckoutBody("ana@example.com", "Ana", "Lopez", boolPtr(true), nil, nil, line))
	confirmCheckoutOK(t, env, first.ClientTransactionID, "approved")
	original := currentPolicyVersionID(t, env)
	if got := readConsentState(t, env, "ana@example.com"); got.PolicyVersionID.String != original {
		t.Fatalf("stored policy_version_id = %q, want %q", got.PolicyVersionID.String, original)
	}

	published := publishPolicyVersion(t, env, "1-test")
	second := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", "",
		consentCheckoutBody("ana@example.com", "Ana", "Lopez", boolPtr(true), nil, nil, line))
	confirmCheckoutOK(t, env, second.ClientTransactionID, "approved")

	if got := readConsentState(t, env, "ana@example.com"); got.PolicyVersionID.String != published {
		t.Fatalf("stored policy_version_id = %q, want the newly published edition %q", got.PolicyVersionID.String, published)
	}
	// Asserted as a SET rather than in order, because both acts share the
	// harness's fixed clock: what matters is that the earlier record still points
	// at the superseded edition — the log is never rewritten, and the state moving
	// on is the only thing that moved.
	editions := map[string]int{}
	for _, record := range readConsentRecords(t, env, "ana@example.com") {
		editions[record.PolicyVersionID]++
	}
	if editions[original] != 1 || editions[published] != 1 || len(editions) != 2 {
		t.Fatalf("recorded editions = %+v, want exactly one of %q and one of %q", editions, original, published)
	}
}

// TestCheckoutConsentEvidenceIsNotTakenFromTheBody: a client composes the body
// and can make it say anything, so the prueba técnica comes from the request the
// platform observed and nowhere else.
func TestCheckoutConsentEvidenceIsNotTakenFromTheBody(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)

	body := consentCheckoutBody("ana@example.com", "Ana", "Lopez",
		boolPtr(true), boolPtr(true), boolPtr(true), cartLine(gaID, 1))
	body["ip"] = "203.0.113.9"
	body["user_agent"] = "Definitely Not This"
	body["origin_url"] = "http://evil.example/"

	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", "", body)
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	record := readConsentRecords(t, env, "ana@example.com")[0]
	if record.IP.String != "198.51.100.24" {
		t.Fatalf("recorded ip = %q, want the observed address rather than the body's claim", record.IP.String)
	}
	if record.UserAgent.String != "Mozilla/5.0 (checkout-consent-test)" {
		t.Fatalf("recorded user agent = %q, want the observed header", record.UserAgent.String)
	}
	if record.OriginURL.String == "http://evil.example/" {
		t.Fatal("recorded origin url came from the body")
	}
}
