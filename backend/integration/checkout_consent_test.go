package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
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
//   - Nothing a guest ticked can speak for an inbox they had not proven.
//
// THE THIRD IS IN THE PAST TENSE NOW (ADR 0054, #386). Checkout begins signed
// in, so every answer captured here is given by somebody who proved the address
// it is recorded against, and nothing this platform does creates a Pending
// Confirmation any more. The rule survives its producer, and so do the tests
// about it: the Payments a guest began before the door closed are still in the
// table, they still settle through this exact commit path, and settling one must
// still not silence a legacy subscriber or overwrite an answer its owner gave.
// Those tests run through beginLegacyGuestCheckout, which ages a Payment into
// the shape the deleted route wrote. Everything else here runs under a session.
//
// WHO IS OWED A BOX AT CHECKOUT AT ALL, now that consent is collected at the
// sign-in door: a Customer caught by a Policy Version published under a live
// session, and one whose optional boxes have never been answered — which is the
// state every address a legacy guest checkout minted is in, and which the
// sign-in gate lets straight through because it holds on Policy Acceptance
// alone. signedInOwingEveryBox builds both at once.

// consentCheckoutBody is a begin-checkout body with the three boxes stated
// explicitly. A nil answer omits the key entirely, which is the wire's way of
// saying the box was not shown — not the same as sending false.
//
// It names no buyer. On this file's helper the buyer is the SESSION TOKEN passed
// to beginCheckoutWithEvidence, which is the whole subject here: who is asked
// what, and whose answer it counts as, are now one question with one answer.
func consentCheckoutBody(firstName, lastName string, policy, marketing, networking *bool, lines ...map[string]any) map[string]any {
	body := checkoutBody("", firstName, lastName, lines...)
	delete(body, buyerEmailKey)
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

// beginCheckoutWithEvidence begins a checkout as the Customer holding `token`,
// carrying the circumstances a real request arrives with — the client IP as the
// BFF derived it, the browser's user agent, and the page the dialog was open on.
// The same headers the sign-in consent step is tested with, because the evidence
// is the same evidence.
//
// The token is passed rather than minted (as beginCheckout mints one) because
// every test in this file and its neighbour is about a Customer in a PARTICULAR
// consent state, built deliberately at sign-in. An empty token posts no
// Authorization at all, which is how the tests about the wall read the refusal.
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
	return env.post(t, "/api/v1/customer/organizations/"+orgSlug+"/events/"+eventSlug+"/checkout", body, headers)
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

// legacyGuestBuyerEmail is the proven address a legacy Payment is BEGUN under
// before it is rewritten into a guest's. Nothing is ever asserted about it: it
// exists only because beginning a checkout now requires somebody real.
const legacyGuestBuyerEmail = "legacy-buyer@example.com"

// beginLegacyGuestCheckout produces the one thing this platform can no longer
// produce: a Payment shaped like a guest checkout begun before ADR 0054 — a
// typed, UNPROVEN address and consent answers given for it — sitting in the
// table, waiting for a return leg that runs under the new code.
//
// WHY IT EXISTS AT ALL. #386 deleted the guest begin-checkout, so a Pending
// Confirmation has no producer any more; but Pending Confirmations are RECORDED,
// their Consent Confirmation Links are in people's inboxes, and every path that
// resolves one has to keep working (ADR 0054, which supersedes ADR 0035's
// producer and not its state). Testing those paths needs the state, and the
// state has to be built the way history built it or the tests prove nothing
// about history.
//
// WHY IT IS SURGERY RATHER THAN A FIXTURE. The pending write happens deep in the
// commit — the repository reads `customer_session_authorized` off the Payment
// and passes it to the consent capture as EmailProven — so seeding the consent
// columns directly would skip exactly the code under test. Rewriting the Payment
// instead runs the real confirm, the real capture, the real receipt and the real
// token, and it is a faithful picture of the deploy: Payments begun under the
// old rules do settle under the new binary, and this is what they look like.
func beginLegacyGuestCheckout(
	t *testing.T, env *testEnv, eventSlug, email, firstName, lastName string,
	policy, marketing, networking *bool, lines ...map[string]any,
) beginCheckoutResult {
	t.Helper()
	begin := beginCheckoutWithEvidenceOK(t, env, testOrgSlug, eventSlug,
		buyerSession(t, env, legacyGuestBuyerEmail),
		consentCheckoutBody(firstName, lastName, nil, nil, nil, lines...))

	// The Payment as the deleted route would have written it: addressed to an
	// address nobody proved, carrying the three answers a guest was always shown.
	if _, err := env.db.Exec(`
		UPDATE payments
		   SET customer_email = $2,
		       customer_session_authorized = FALSE,
		       consent_policy_acceptance = $3,
		       consent_marketing = $4,
		       consent_networking = $5
		 WHERE client_transaction_id = $1
	`, begin.ClientTransactionID, email,
		nullableBoolArg(policy), nullableBoolArg(marketing), nullableBoolArg(networking)); err != nil {
		t.Fatalf("age the payment into a guest checkout: %v", err)
	}
	return begin
}

// nullableBoolArg keeps the nil load-bearing across the SQL boundary: a box that
// was not shown is a NULL column, never a false.
func nullableBoolArg(v *bool) any {
	if v == nil {
		return nil
	}
	return *v
}

// signedInOwingEveryBox returns a Customer Session whose holder is owed all
// three consent boxes at the checkout dialog — the nearest thing left to the
// guest most of this file was written about, and reachable without inventing
// anything.
//
// The optional pair is unanswered because that is a state real people are in:
// every address a legacy guest checkout created carries it, and the sign-in gate
// holds on Policy Acceptance ALONE, so such a person signs straight through and
// meets those two boxes at the dialog for the first time. The required box is
// outstanding because a new Policy Version is published under her live session,
// which is the one way that happens now that acceptance is collected at sign-in
// and a session cannot be re-gated once minted.
//
// Both steps are writes no surface performs — you cannot UNanswer a consent, and
// nothing lets a test publish a policy through the API — so both are SQL. They
// are a starting state, not a transition.
func signedInOwingEveryBox(t *testing.T, env *testEnv, email string) string {
	t.Helper()
	token := customerSignIn(t, env, email)
	forgetConsentAnswers(t, env, email, false)
	publishPolicyVersion(t, env, 2, 0)
	assertBoxes(t, signedInConsentBoxes(t, env, token), true, true, true,
		"a re-gated Customer who has never answered either optional box")
	return token
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
//
// The buyer here is the only person left who can meet a required box at a
// checkout: one holding a live session when a new Policy Version is published.
// The gate did not change with ADR 0054; who reaches it did.
func TestCheckoutRefusedWithoutPolicyAcceptance(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)
	line := cartLine(gaID, 1)

	token := signedInOwingEveryBox(t, env, "ana@example.com")
	recordsBefore := countConsentRecords(t, env)

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
		resp, body := beginCheckoutWithEvidence(t, env, "test-org", "consent-fest", token,
			consentCheckoutBody("Ana", "Lopez", tc.policy, boolPtr(true), boolPtr(true), line))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status=%d, want 400", tc.name, resp.StatusCode)
		}
		if body.Error == nil || body.Error.Code != "POLICY_ACCEPTANCE_REQUIRED" {
			t.Fatalf("%s: error=%+v, want POLICY_ACCEPTANCE_REQUIRED", tc.name, body.Error)
		}
	}

	// And the gate in front of the gate: no Customer Session, no checkout at all
	// (ADR 0054). A body that accepts everything cannot buy its way past it,
	// because there is no longer a route on which an unproven buyer is a buyer.
	resp, body := beginCheckoutWithEvidence(t, env, "test-org", "consent-fest", "",
		consentCheckoutBody("Ana", "Lopez", boolPtr(true), boolPtr(true), boolPtr(true), line))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous checkout: status=%d error=%+v, want 401", resp.StatusCode, body.Error)
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
	if got := countConsentRecords(t, env); got != recordsBefore {
		t.Fatalf("consent records after refused begins = %d, want the %d her sign-in left", got, recordsBefore)
	}
}

// TestCheckoutConsentSurvivesTheProviderRedirect walks the whole journey: the
// answers are held on the Payment across the redirect and become evidence only
// when the sale commits.
func TestCheckoutConsentSurvivesTheProviderRedirect(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)

	token := signedInOwingEveryBox(t, env, "ana@example.com")

	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", token,
		consentCheckoutBody("Ana", "Lopez",
			boolPtr(true), boolPtr(true), boolPtr(true), cartLine(gaID, 1)))

	// Between the two legs there is a Payment and nothing else: the answers are
	// held, and the evidence of the checkout does not exist until the sale does.
	if got := len(consentRecordsOn(t, env, "ana@example.com", "checkout")); got != 0 {
		t.Fatalf("checkout consent records before the sale commits = %d, want 0", got)
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

	records := consentRecordsOn(t, env, "ana@example.com", "checkout")
	if len(records) != 1 {
		t.Fatalf("checkout consent records = %d, want exactly one for one capture act", len(records))
	}
	record := records[0]
	// The address the SESSION named, and there is no other kind: it is not
	// asserted on the dialog any more, it is read off the credential (ADR 0054).
	if record.Email != "ana@example.com" {
		t.Fatalf("record email = %q", record.Email)
	}
	if !record.EmailProven {
		t.Fatal("email_proven = false on a checkout that could only be begun with proof of that address")
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
	// No session id, and that is still the truth even though there is now always a
	// session: the act this record evidences is finished by a redirect back from a
	// Payment Provider, which no session outlives. What ties the evidence to what
	// happened is the Ticket Sale.
	if record.SessionID.Valid {
		t.Fatalf("recorded session id = %+v, want none on a checkout settled across a redirect", record.SessionID)
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
	// The optional ticks are the answer of somebody who proved this address, so
	// they GRANT. This is the line ADR 0054 moved: the same body, the same commit,
	// the same evidence — and no Pending Confirmation, because there is no longer
	// a way to tick a box for an inbox nobody has proven.
	if state.MarketingConsent.String != "granted" {
		t.Fatalf("marketing_consent = %q, want granted from a proven tick", state.MarketingConsent.String)
	}
	if state.NetworkingConsent.String != "granted" {
		t.Fatalf("networking_consent = %q, want granted from a proven tick", state.NetworkingConsent.String)
	}
	// And the Follow Digest moves with it, in lockstep, which is the half a
	// pending answer was never allowed to reach (ADR 0034).
	if !state.DigestEnabled {
		t.Fatal("digest_enabled = false; a proven grant switches the weekly Digest on")
	}
}

// TestGuestUntickedOptionalBoxesAnswerNothing: a guest's silence is evidence of
// what they were shown, and nothing at all about the person whose address they
// typed.
//
// NO CHECKOUT CAN BE BEGUN THIS WAY ANY MORE (ADR 0054, #386). What is under
// test is a Payment a guest began before the door closed, settling afterwards
// through the commit path that is still live — the only place an unproven answer
// can still arrive, and the reason none of this rule was deleted with its
// producer.
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

	begin := beginLegacyGuestCheckout(t, env, "consent-fest", "ana@example.com", "Ana", "Lopez",
		boolPtr(true), boolPtr(false), boolPtr(false), cartLine(gaID, 1))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	records := consentRecordsOn(t, env, "ana@example.com", "checkout")
	if len(records) != 1 {
		t.Fatalf("checkout consent records = %d, want one", len(records))
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
	// The flag does not move on an UNPROVEN answer, in either direction. Here
	// there was nothing to move — migration 065 starts a Customer born after
	// consent unsubscribed — so this says only that the No did not reach it. The
	// case where the flag has something to lose is the next test.
	if state.DigestEnabled {
		t.Fatal("digest_enabled = true after an unproven No; only a proven answer moves it")
	}
}

// TestUnprovenNoCannotSilenceALegacySubscriber is the harm the rule above exists
// to prevent, with a victim who has something to lose.
//
// Ana predates consent: she Follows, she has been receiving the Digest on the
// legacy flag, and nobody has asked her anything. A stranger then types her
// address into a checkout and leaves the marketing box unticked. Recorded as her
// No, that press would have ended a subscription she chose and never withdrawn —
// and taken away the prompt that could have restored it, since a denial reads as
// answered. Her Digest keeps arriving, and she is still the one who gets to say.
func TestUnprovenNoCannotSilenceALegacySubscriber(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)

	followingCustomer(t, env, "ana@example.com")
	forgetConsentAnswers(t, env, "ana@example.com", true)

	// A guest's Payment, begun before ADR 0054 closed the door and settling now.
	begin := beginLegacyGuestCheckout(t, env, "consent-fest", "ana@example.com", "Ana", "Lopez",
		boolPtr(true), boolPtr(false), boolPtr(false), cartLine(gaID, 1))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.Valid {
		t.Fatalf("marketing_consent = %q, want the unanswered state a stranger cannot change", state.MarketingConsent.String)
	}
	if !state.DigestEnabled {
		t.Fatal("digest_enabled = false: a stranger's silence ended a subscription she never withdrew")
	}

	digestWeek(t, env, sessionID, "Still Subscribed Fest", "still-subscribed-fest")
	if got := len(digestsFor(t, env, "ana@example.com")); got != 1 {
		t.Fatalf("ana received %d Digests after a stranger declined on her behalf, want 1", got)
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

	// Ana has signed in before — settling the Terms, the one thing a guest's
	// checkout can never do for her (#536) — and her optional consents are put
	// back to never-answered, the state genuinely reachable by an account whose
	// owner has not signed in since the boxes existed.
	customerSignIn(t, env, "ana@example.com")
	resetOptionalConsentsToUnanswered(t, env, "ana@example.com")

	begin := beginLegacyGuestCheckout(t, env, "consent-fest", "ana@example.com", "Ana", "Lopez",
		boolPtr(true), boolPtr(false), boolPtr(false), cartLine(gaID, 1))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	// Ana signs in again. She is not stopped for Policy Acceptance — the
	// checkout above accepted the edition in effect, and that acceptance is a
	// fact about the act rather than a claim on her inbox (ADR 0035) — and her
	// Terms were settled at her own earlier sign-in.
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
// saying they were not shown, which is not a refusal. It is the shape the dialog
// sends whenever a box was drawn and its neighbours were not, and — with consent
// collected at sign-in — very nearly the only shape it sends at all.
func TestUnshownOptionalBoxesRecordNothing(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)

	token := signedInOwingEveryBox(t, env, "ana@example.com")

	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", token,
		consentCheckoutBody("Ana", "Lopez",
			boolPtr(true), nil, nil, cartLine(gaID, 1)))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	records := consentRecordsOn(t, env, "ana@example.com", "checkout")
	if len(records) != 1 {
		t.Fatalf("checkout consent records = %d, want one", len(records))
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

	token := signedInOwingEveryBox(t, env, "ana@example.com")

	// Declined at the provider.
	declined := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", token,
		consentCheckoutBody("Ana", "Lopez",
			boolPtr(true), boolPtr(true), boolPtr(true), line))
	if got := confirmCheckoutOK(t, env, declined.ClientTransactionID, "declined"); got.Status != "failed" {
		t.Fatalf("confirm status = %q, want failed", got.Status)
	}

	// Never returned from the payment page at all.
	beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", token,
		consentCheckoutBody("Ana", "Lopez",
			boolPtr(true), boolPtr(true), boolPtr(true), line))

	if got := len(consentRecordsOn(t, env, "ana@example.com", "checkout")); got != 0 {
		t.Fatalf("checkout consent records after a declined and an abandoned Payment = %d, want 0", got)
	}
	// Exactly the same rule that leaves them no SALE: the evidence and the
	// transaction are one commit, so neither can exist without the other.
	//
	// The Customer half of that rule is no longer readable here and its absence is
	// the point — the buyer had to sign in to reach the dialog at all, so a
	// Customer exists before any Payment does (ADR 0054). What must be untouched
	// is the ANSWER: she ticked both boxes on two checkouts that never became
	// sales, and both boxes are still outstanding.
	var sales int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM ticket_sales`).Scan(&sales); err != nil {
		t.Fatalf("count ticket sales: %v", err)
	}
	if sales != 0 {
		t.Fatalf("ticket sales after unsettled Payments = %d, want 0", sales)
	}
	assertBoxes(t, signedInConsentBoxes(t, env, token), true, true, true,
		"answers on a Payment that never settled were never given")
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

	// A stranger's checkout under her address, begun before ADR 0054 closed that
	// door and settling now, with both boxes unticked.
	stranger := beginLegacyGuestCheckout(t, env, "consent-fest", "ana@example.com", "Ana", "Lopez",
		boolPtr(true), boolPtr(false), boolPtr(false), line)
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

	// The answer that changed nothing still left its record — two acts, two rows.
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

	guest := beginLegacyGuestCheckout(t, env, "consent-fest", "ana@example.com", "Ana", "Lopez",
		boolPtr(true), boolPtr(true), boolPtr(true), cartLine(gaID, 1))
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
		consentCheckoutBody("Ana", "Lopez",
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

	token := signedInOwingEveryBox(t, env, "ana@example.com")

	// Refused for the same reason a paid one is: a free ticket is still a Ticket
	// Sale recorded and a receipt emailed.
	resp, body := beginCheckoutWithEvidence(t, env, "test-org", "free-consent-fest", token,
		consentCheckoutBody("Ana", "Lopez", nil, nil, nil, line))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "POLICY_ACCEPTANCE_REQUIRED" {
		t.Fatalf("free checkout without acceptance: status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, envBody := beginCheckoutWithEvidence(t, env, "test-org", "free-consent-fest", token,
		consentCheckoutBody("Ana", "Lopez",
			boolPtr(true), boolPtr(true), nil, line))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("free checkout status=%d error=%+v", resp.StatusCode, envBody.Error)
	}
	var claim freeCheckoutResult
	if err := json.Unmarshal(envBody.Data, &claim); err != nil {
		t.Fatalf("decode free claim: %v", err)
	}
	approvedRef(t, claim)

	records := consentRecordsOn(t, env, "ana@example.com", "checkout")
	if len(records) != 1 {
		t.Fatalf("checkout consent records = %d, want one written by the free settlement", len(records))
	}
	if !records[0].EmailProven {
		t.Fatal("email_proven = false on a free claim that could only be begun with proof of the address")
	}
	if got := readConsentState(t, env, "ana@example.com"); got.MarketingConsent.String != "granted" {
		t.Fatalf("marketing_consent = %q, want granted from a proven tick", got.MarketingConsent.String)
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

	// She accepts at the door, which is where a first acceptance happens now
	// (ADR 0054): the checkout that follows is owed nothing and stamps nothing.
	token := customerSignIn(t, env, "ana@example.com")
	original := currentPolicyVersionID(t, env)
	if got := readConsentState(t, env, "ana@example.com"); got.PolicyVersionID.String != original {
		t.Fatalf("stored policy_version_id = %q, want %q", got.PolicyVersionID.String, original)
	}

	// A new edition is published under her live session. The sign-in gate cannot
	// re-run on a session already minted, so the CHECKOUT is where she is caught —
	// and the edition it stamps is the one current at the moment of capture,
	// resolved by the platform and never named by the client.
	published := publishPolicyVersion(t, env, 2, 0)
	second := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", token,
		consentCheckoutBody("Ana", "Lopez", boolPtr(true), nil, nil, line))
	confirmCheckoutOK(t, env, second.ClientTransactionID, "approved")

	if got := readConsentState(t, env, "ana@example.com"); got.PolicyVersionID.String != published {
		t.Fatalf("stored policy_version_id = %q, want the newly published edition %q", got.PolicyVersionID.String, published)
	}
	// Asserted as a SET rather than in order, because both acts share the
	// harness's fixed clock: what matters is that her sign-in's record still
	// points at the superseded edition — the log is never rewritten, and the state
	// moving on is the only thing that moved.
	editions := map[string]int{}
	for _, record := range readConsentRecords(t, env, "ana@example.com") {
		editions[record.PolicyVersionID]++
	}
	if editions[original] != 1 || editions[published] != 1 || len(editions) != 2 {
		t.Fatalf("recorded editions = %+v, want exactly one of %q and one of %q", editions, original, published)
	}
}

// TestCheckoutConsentEvidenceIsNotTakenFromTheBody: a client composes the body
// and can make it say anything, so the technical proof comes from the request the
// platform observed and nowhere else.
func TestCheckoutConsentEvidenceIsNotTakenFromTheBody(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)

	token := signedInOwingEveryBox(t, env, "ana@example.com")

	body := consentCheckoutBody("Ana", "Lopez",
		boolPtr(true), boolPtr(true), boolPtr(true), cartLine(gaID, 1))
	body["ip"] = "203.0.113.9"
	body["user_agent"] = "Definitely Not This"
	body["origin_url"] = "http://evil.example/"

	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", token, body)
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	record := consentRecordsOn(t, env, "ana@example.com", "checkout")[0]
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

// TestCheckoutCapturesEveryOwedBoxInOneAct is the owed set at its widest, which
// is the shape #588 completes: a Customer caught mid-session by a Policy Version
// AND by a Terms edition that carries the `label-adulthood-declaration`
// Artifact, with both optional boxes never answered. Five boxes, one dialog, one
// capture act, one Consent Record.
//
// It lives here rather than in checkout_terms_test.go because what it is about
// is the SET — that the boxes are owed independently, refused independently and
// recorded together — and this file is where the owed set is reasoned about.
// The declaration's own rules are pinned beside the Terms box they ride.
func TestCheckoutCapturesEveryOwedBoxInOneAct(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Every Box Fest", "every-box-fest", 1000, 10)

	token := signedInOwingEveryBox(t, env, "elena@example.com")
	shownEdition := publishTermsVersionAskingAdulthood(t, env, 2, 0)
	boxes := signedInConsentBoxes(t, env, token)
	if !boxes.PolicyAcceptance || !boxes.MarketingConsent || !boxes.NetworkingConsent ||
		!boxes.TermsAcceptance || !boxes.AdulthoodDeclaration {
		t.Fatalf("boxes = %+v, want every one of the five owed", boxes)
	}
	setSignInClock(t, env, env.fixedClock.Add(time.Hour))

	// The declaration is refused on its own account even when everything else
	// is in order: it is not folded into the Terms box, so accepting the
	// document does not answer it.
	unticked := consentCheckoutBody("Elena", "Ríos", boolPtr(true), boolPtr(true), boolPtr(false), cartLine(gaID, 1))
	unticked["terms_acceptance"] = true
	unticked["adulthood_declaration"] = false
	resp, envelope := beginCheckoutWithEvidence(t, env, "test-org", "every-box-fest", token, unticked)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("begin with the declaration unticked: status=%d, want 400", resp.StatusCode)
	}
	if envelope.Error == nil || envelope.Error.Code != "ADULTHOOD_DECLARATION_REQUIRED" {
		t.Fatalf("error = %+v, want ADULTHOOD_DECLARATION_REQUIRED", envelope.Error)
	}

	// And the whole set, answered as a person would leave it: the required
	// three ticked, one optional taken and one declined — a declined optional
	// being an explicit No and not a silence (ADR 0034).
	body := consentCheckoutBody("Elena", "Ríos", boolPtr(true), boolPtr(true), boolPtr(false), cartLine(gaID, 1))
	body["terms_acceptance"] = true
	body["adulthood_declaration"] = true
	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "every-box-fest", token, body)
	if confirmed := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved"); confirmed.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirmed.Status)
	}

	records := consentRecordsOn(t, env, "elena@example.com", "checkout")
	if len(records) != 1 {
		t.Fatalf("checkout consent records = %d, want one act for one dialog", len(records))
	}
	got := records[0]
	if !got.PolicyAcceptance.Bool || !got.MarketingConsent.Bool || got.NetworkingConsent.Bool {
		t.Fatalf("recorded privacy answers = %+v, want the pair she gave", got)
	}
	if !got.AdulthoodDeclaration.Valid || !got.AdulthoodDeclaration.Bool {
		t.Fatalf("recorded adulthood_declaration = %+v, want true", got.AdulthoodDeclaration)
	}
	terms := readTermsAnswers(t, env, "elena@example.com")
	last := terms[len(terms)-1]
	if !last.Answer.Valid || !last.Answer.Bool || !last.VersionID.Valid || last.VersionID.String != shownEdition {
		t.Fatalf("recorded terms pair = (%+v, %+v), want the acceptance beside the edition that worded both boxes", last.Answer, last.VersionID)
	}
}
