package integration

import (
	"net/http"
	"testing"
)

// Which consent boxes a SIGNED-IN Customer is shown at checkout, and what the
// write side does with the answers they send back (#254, parent #249).
//
// #253 built the capture; this is the visibility half of the same feature, and
// the two are tested together here because they are one property seen from two
// sides: THE BOXES A SURFACE DRAWS AND THE ANSWERS THE API HONOURS ARE THE SAME
// SET. A test that only checked what the session read publishes would pass
// while a fully-answered Customer was refused at the pay button; one that only
// checked the write would pass while the dialog re-asked a question already
// answered. So every case below reads the set and then spends it.
//
// The seam is HTTP throughout. The set is read from the Customer Session
// endpoint — where the checkout dialog reads it, in the same breath as the
// email and Tax ID it prefills from — and the outcome is read from the sale, the
// evidence log and the consent state, the last two with SQL for the reason
// customer_consent_test.go gives.

// signedInConsentBoxes reads the consent boxes the API says this session still
// owes, exactly as the checkout dialog does when it opens.
func signedInConsentBoxes(t *testing.T, env *testEnv, token string) consentBoxes {
	t.Helper()
	resp, body := env.get(t, customerSessionPath, map[string]string{"Authorization": "Bearer " + token})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session read status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return decodeCustomerSession(t, body).ConsentBoxes
}

// assertBoxes states the whole matrix row at once, so a failure names which box
// went wrong rather than which assertion was reached first.
func assertBoxes(t *testing.T, got consentBoxes, policy, marketing, networking bool, why string) {
	t.Helper()
	want := consentBoxes{PolicyAcceptance: policy, MarketingConsent: marketing, NetworkingConsent: networking}
	if got != want {
		t.Fatalf("consent boxes = %+v, want %+v (%s)", got, want, why)
	}
}

// signInAnswering signs a Customer in and answers the consent step exactly as
// given, returning the session token. Unlike the suite's customerSignIn, which
// grants everything, this one is for the tests whose whole subject is a Customer
// who answered some things and not others.
func signInAnswering(t *testing.T, env *testEnv, email string, policy, marketing, networking bool) string {
	t.Helper()
	verify := startSignIn(t, env, email)
	if verify.ConsentRequired == nil {
		t.Fatal("expected a consent step for a Customer who has never accepted")
	}
	resp, body := env.post(t, customerConsentPath,
		consentAnswers(verify.ConsentRequired.PendingConsentToken, policy, marketing, networking),
		consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	token := decodeCustomerVerify(t, body).SessionID
	if token == "" {
		t.Fatal("expected a Customer Session after the consent step")
	}
	return token
}

// TestSignedInFullyAnsweredCheckoutShowsNoBoxes is the first row of the matrix
// and the one the whole ticket exists for: a Customer who has accepted the
// current Policy Version and answered both optional consents is asked nothing,
// and checks out exactly as they did before this feature existed — no consent
// fields on the wire, no Consent Record, nothing moved.
func TestSignedInFullyAnsweredCheckoutShowsNoBoxes(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)

	// Accepted the policy, granted marketing, DECLINED networking: every box
	// answered, and not all the same way — an implementation that reported
	// "unanswered" for a No would show a box here.
	token := signInAnswering(t, env, "ana@example.com", true, true, false)
	recordsBefore := len(readConsentRecords(t, env, "ana@example.com"))

	assertBoxes(t, signedInConsentBoxes(t, env, token), false, false, false,
		"a Customer who has answered everything is asked nothing")

	// The checkout the dialog would send with no boxes drawn: not `false` for the
	// required one, which would be a person declining, but ABSENT — no box, no
	// answer, no assertion at all.
	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", token,
		consentCheckoutBody("ana@example.com", "Ana", "Lopez", nil, nil, nil, cartLine(gaID, 1)))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	// No box was shown, so no capture act happened, so there is no evidence of
	// one. The log records what happened, and here nothing did.
	if got := len(consentRecordsOn(t, env, "ana@example.com", "checkout")); got != 0 {
		t.Fatalf("checkout consent records = %d, want none: no box was shown", got)
	}
	if got := len(readConsentRecords(t, env, "ana@example.com")); got != recordsBefore {
		t.Fatalf("consent records = %d, want the %d her sign-in left", got, recordsBefore)
	}

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "granted" || state.NetworkingConsent.String != "denied" {
		t.Fatalf("state after a checkout that asked nothing = %+v/%+v, want granted/denied untouched",
			state.MarketingConsent, state.NetworkingConsent)
	}
	if !state.DigestEnabled {
		t.Fatal("digest_enabled = false; a checkout that asked nothing may not switch the Digest off")
	}
}

// TestSignedInWithoutPolicyAcceptanceIsGatedAtCheckout is the legacy Customer:
// signed in since before consent existed, or caught by a Policy Version bump
// mid-session. The full notice and the required box appear at checkout too, and
// they gate the purchase — the guidance's "only optional boxes" case presumes
// acceptance happened at registration, and for this platform's Customers it may
// never have.
//
// The Customer here holds a session and no acceptance at all, which is the state
// a Policy Version published under a live session produces: the sign-in gate
// cannot re-run on a session already minted, so the checkout is where they are
// caught.
func TestSignedInWithoutPolicyAcceptanceIsGatedAtCheckout(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)
	line := cartLine(gaID, 1)

	token := signInAnswering(t, env, "ana@example.com", true, true, true)
	assertBoxes(t, signedInConsentBoxes(t, env, token), false, false, false, "she has just answered everything")

	// A new edition is published under her feet. Nothing about her row changes;
	// what changes is which edition "current" names.
	published := publishPolicyVersion(t, env, "1-test")

	assertBoxes(t, signedInConsentBoxes(t, env, token), true, false, false,
		"a version bump re-gates the required box ALONE and never churns standing optional answers")

	// The API refuses her checkout without it, exactly as it refuses a guest's.
	resp, body := beginCheckoutWithEvidence(t, env, "test-org", "consent-fest", token,
		consentCheckoutBody("ana@example.com", "Ana", "Lopez", nil, nil, nil, line))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "POLICY_ACCEPTANCE_REQUIRED" {
		t.Fatalf("checkout by a re-gated Customer: status=%d error=%+v, want 400 POLICY_ACCEPTANCE_REQUIRED",
			resp.StatusCode, body.Error)
	}

	// And accepts it with the required box alone — the shape the dialog sends
	// when that is the only box it drew.
	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", token,
		consentCheckoutBody("ana@example.com", "Ana", "Lopez", boolPtr(true), nil, nil, line))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	state := readConsentState(t, env, "ana@example.com")
	if state.PolicyVersionID.String != published {
		t.Fatalf("stored policy_version_id = %q, want the newly published edition %q", state.PolicyVersionID.String, published)
	}
	if state.MarketingConsent.String != "granted" || state.NetworkingConsent.String != "granted" {
		t.Fatalf("optional state after a re-acceptance = %+v/%+v, want the grants she gave at sign-in",
			state.MarketingConsent, state.NetworkingConsent)
	}
	checkouts := consentRecordsOn(t, env, "ana@example.com", "checkout")
	if len(checkouts) != 1 {
		t.Fatalf("checkout consent records = %d, want one", len(checkouts))
	}
	if !checkouts[0].PolicyAcceptance.Valid || !checkouts[0].PolicyAcceptance.Bool {
		t.Fatalf("recorded policy_acceptance = %+v, want the box she ticked", checkouts[0].PolicyAcceptance)
	}
	if checkouts[0].MarketingConsent.Valid || checkouts[0].NetworkingConsent.Valid {
		t.Fatalf("recorded optional answers = %+v/%+v, want NULL for boxes that were not shown",
			checkouts[0].MarketingConsent, checkouts[0].NetworkingConsent)
	}
	if !checkouts[0].EmailProven {
		t.Fatal("email_proven = false on a checkout under the buyer's own session for their own address")
	}
}

// TestSignedInSeesOnlyUnansweredOptionalBoxes: the middle of the matrix. The
// policy is accepted at the current edition, one optional consent is answered
// and one is not, so exactly one box appears — and answering it is a proven
// answer that moves the state, while the box that never appeared moves nothing.
func TestSignedInSeesOnlyUnansweredOptionalBoxes(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)

	token := signInAnswering(t, env, "ana@example.com", true, false, true)
	// Networking answered at sign-in; marketing declined there too, so nothing is
	// outstanding yet. The interesting state is built by hand below.
	assertBoxes(t, signedInConsentBoxes(t, env, token), false, false, false, "she answered both at sign-in")

	// A Customer whose networking consent has never been answered at all: the
	// state a Customer created by a box-office sale or a Sale Import carries into
	// their first Storefront appearance. Written directly because no surface can
	// UNanswer a consent, which is the point — this is a starting state, not a
	// transition.
	if _, err := env.db.Exec(`UPDATE customers SET networking_consent = NULL WHERE email = $1`, "ana@example.com"); err != nil {
		t.Fatalf("clear networking consent: %v", err)
	}

	assertBoxes(t, signedInConsentBoxes(t, env, token), false, false, true,
		"only the unanswered optional box, and never the required one she has accepted")

	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", token,
		consentCheckoutBody("ana@example.com", "Ana", "Lopez", nil, nil, boolPtr(true), cartLine(gaID, 1)))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	state := readConsentState(t, env, "ana@example.com")
	if state.NetworkingConsent.String != "granted" {
		t.Fatalf("networking_consent = %q, want granted: she answered it herself, behind her own session",
			state.NetworkingConsent.String)
	}
	if state.MarketingConsent.String != "denied" {
		t.Fatalf("marketing_consent = %q, want the denied she gave at sign-in, untouched", state.MarketingConsent.String)
	}
	if state.DigestEnabled {
		t.Fatal("digest_enabled = true; the marketing box was never shown and nothing about it moved")
	}

	checkouts := consentRecordsOn(t, env, "ana@example.com", "checkout")
	if len(checkouts) != 1 {
		t.Fatalf("checkout consent records = %d, want one", len(checkouts))
	}
	// The record says what was ASKED as well as what was answered: NULL is "not
	// shown", and the two boxes that were not shown are both NULL.
	if checkouts[0].PolicyAcceptance.Valid || checkouts[0].MarketingConsent.Valid {
		t.Fatalf("recorded policy/marketing = %+v/%+v, want NULL for boxes that were not shown",
			checkouts[0].PolicyAcceptance, checkouts[0].MarketingConsent)
	}
	if !checkouts[0].NetworkingConsent.Valid || !checkouts[0].NetworkingConsent.Bool {
		t.Fatalf("recorded networking_consent = %+v, want the tick she gave", checkouts[0].NetworkingConsent)
	}
}

// TestPendingConfirmationCountsAsUnansweredAtCheckout: a stranger ticked her
// marketing box in a guest checkout, so the state is Pending Confirmation —
// denied for sending, and UNANSWERED for prompting. She is asked again, and her
// own answer supersedes the stranger's tick either way (ADR 0035).
func TestPendingConfirmationCountsAsUnansweredAtCheckout(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)
	line := cartLine(gaID, 1)

	// A stranger buys a ticket under her address and ticks everything.
	stranger := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", "",
		consentCheckoutBody("ana@example.com", "Ana", "Lopez",
			boolPtr(true), boolPtr(true), boolPtr(true), line))
	confirmCheckoutOK(t, env, stranger.ClientTransactionID, "approved")
	if got := readConsentState(t, env, "ana@example.com"); got.MarketingConsent.String != "pending_confirmation" {
		t.Fatalf("marketing_consent = %q, want pending_confirmation", got.MarketingConsent.String)
	}

	// She then signs in herself, and is NOT held at the door: the stranger's
	// Policy Acceptance was recorded against her Customer unconditionally, because
	// it is a fact about that sale rather than a claim on her inbox (ADR 0035).
	// So the checkout is the surface that gets to ask her about the pendings.
	verify := startSignIn(t, env, "ana@example.com")
	if verify.ConsentRequired != nil {
		t.Fatal("expected no consent step: the guest's acceptance is recorded on her row")
	}
	token := verify.SessionID
	if token == "" {
		t.Fatal("expected a Customer Session")
	}

	assertBoxes(t, signedInConsentBoxes(t, env, token), false, true, true,
		"a Pending Confirmation is unanswered for prompting, and the accepted policy is not re-asked (ADR 0035)")

	// She answers them herself, behind her own session: one grant and one refusal.
	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", token,
		consentCheckoutBody("ana@example.com", "Ana", "Lopez", nil, boolPtr(true), boolPtr(false), line))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "granted" {
		t.Fatalf("marketing_consent = %q, want granted: the proven owner answered and supersedes the pending",
			state.MarketingConsent.String)
	}
	if state.NetworkingConsent.String != "denied" {
		t.Fatalf("networking_consent = %q, want denied: her own No supersedes the stranger's tick either way",
			state.NetworkingConsent.String)
	}
	if !state.DigestEnabled {
		t.Fatal("digest_enabled = false; a proven grant turns the weekly Digest on (ADR 0034)")
	}

	assertBoxes(t, signedInConsentBoxes(t, env, token), false, false, false,
		"answered by their owner, so nothing is outstanding")
}

// TestGuestCheckoutStillSeesEveryBox: the row of the matrix that must not have
// moved. Nothing is known about a visitor with no session, so all three boxes
// are shown and the required one still gates the purchase (#253, unchanged).
func TestGuestCheckoutStillSeesEveryBox(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)
	line := cartLine(gaID, 1)

	// She has answered everything, behind proof. It buys a guest nothing: the
	// session read is the only place that set is published, and a guest holds no
	// session — the surface cannot become an oracle for who has answered what.
	signInAnswering(t, env, "ana@example.com", true, true, true)

	resp, body := env.get(t, customerSessionPath, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session read without a token: status=%d, want 401 — nothing is published to a guest", resp.StatusCode)
	}

	resp, body = beginCheckoutWithEvidence(t, env, "test-org", "consent-fest", "",
		consentCheckoutBody("ana@example.com", "Ana", "Lopez", nil, nil, nil, line))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "POLICY_ACCEPTANCE_REQUIRED" {
		t.Fatalf("guest checkout under an answered Customer's address: status=%d error=%+v, want 400 POLICY_ACCEPTANCE_REQUIRED",
			resp.StatusCode, body.Error)
	}
}

// TestSignedInBuyingForSomebodyElseIsAGuestCheckout: the session is not a
// blanket exemption. A signed-in Customer supplying a friend's address is
// speaking about the friend, whose consent nobody has, so every box comes back
// and the required one gates the purchase exactly as it does for a guest.
//
// This is the same predicate that stops the buyer overwriting the friend's Tax
// ID (selfAssertedCheckout), asked once and used for both.
func TestSignedInBuyingForSomebodyElseIsAGuestCheckout(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)
	line := cartLine(gaID, 1)

	token := signInAnswering(t, env, "ana@example.com", true, true, true)
	assertBoxes(t, signedInConsentBoxes(t, env, token), false, false, false, "she has answered everything")

	resp, body := beginCheckoutWithEvidence(t, env, "test-org", "consent-fest", token,
		consentCheckoutBody("friend@example.com", "Beto", "Ruiz", nil, nil, nil, line))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "POLICY_ACCEPTANCE_REQUIRED" {
		t.Fatalf("buying for a friend: status=%d error=%+v, want 400 POLICY_ACCEPTANCE_REQUIRED",
			resp.StatusCode, body.Error)
	}

	// With the boxes answered — which is what the dialog draws once the email in
	// the form stops matching the session's — the sale goes through, and the
	// friend's optional tick pends: nobody proved that address.
	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", token,
		consentCheckoutBody("friend@example.com", "Beto", "Ruiz",
			boolPtr(true), boolPtr(true), boolPtr(false), line))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	state := readConsentState(t, env, "friend@example.com")
	if state.MarketingConsent.String != "pending_confirmation" {
		t.Fatalf("friend's marketing_consent = %q, want pending_confirmation: her session proves nothing about his address",
			state.MarketingConsent.String)
	}
	records := consentRecordsOn(t, env, "friend@example.com", "checkout")
	if len(records) != 1 || records[0].EmailProven {
		t.Fatalf("friend's checkout record = %+v, want exactly one with email_proven false", records)
	}
}

// TestCraftedCheckoutBodyCannotChurnAStandingAnswer is the adversarial reading
// of the whole design: the boxes are the courtesy and the recomputation is the
// guarantee.
//
// A body is composed by a client and can say anything. This one, sent under the
// buyer's OWN session — the strongest position anybody can send one from — sends
// answers for two boxes she was never shown, one flipping her grant and one
// flipping her refusal. Both are dropped, because which boxes were owed is the
// platform's finding, not the body's assertion (#251's rule, at #253's surface).
func TestCraftedCheckoutBodyCannotChurnAStandingAnswer(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)

	// Marketing granted, networking declined, policy accepted: nothing owed.
	token := signInAnswering(t, env, "ana@example.com", true, true, false)
	assertBoxes(t, signedInConsentBoxes(t, env, token), false, false, false, "nothing is outstanding")

	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", token,
		consentCheckoutBody("ana@example.com", "Ana", "Lopez",
			boolPtr(true), boolPtr(false), boolPtr(true), cartLine(gaID, 1)))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "granted" {
		t.Fatalf("marketing_consent = %q, want the granted she gave: a box she was not shown cannot unsubscribe her",
			state.MarketingConsent.String)
	}
	if state.NetworkingConsent.String != "denied" {
		t.Fatalf("networking_consent = %q, want the denied she gave", state.NetworkingConsent.String)
	}
	if !state.DigestEnabled {
		t.Fatal("digest_enabled = false; a dropped answer may not move the Follow Digest either")
	}
	// And no evidence is manufactured for boxes that were never drawn: a Consent
	// Record with nothing in it is not written at all.
	if got := len(consentRecordsOn(t, env, "ana@example.com", "checkout")); got != 0 {
		t.Fatalf("checkout consent records = %d, want none: every answer in that body was dropped", got)
	}
}

// TestConfirmationLinkSessionIsShownEveryBox: a session minted from a link in a
// forwarded email proves nothing about who is holding it, so the capture surface
// asks it everything — which is exactly what the write side does with its
// answers, since selfAssertedCheckout refuses a sale-scoped session too.
func TestConfirmationLinkSessionIsShownEveryBox(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)

	// She buys under her own answered session, so there is a sale to link to.
	token := signInAnswering(t, env, "ana@example.com", true, true, true)
	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", token,
		consentCheckoutBody("ana@example.com", "Ana", "Lopez", nil, nil, nil, cartLine(gaID, 1)))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	view, saleScoped := redeemConfirmationLinkOK(t, env, lastConfirmationLinkToken(t, env), "")
	if view.TicketSaleID == nil {
		t.Fatal("expected a session scoped to the one sale the link names")
	}
	assertBoxes(t, view.ConsentBoxes, true, true, true,
		"the redemption's own view says the same thing the session read does")

	assertBoxes(t, signedInConsentBoxes(t, env, saleScoped), true, true, true,
		"a forwarded email is not Proof of Email Ownership, so nothing is taken as answered")
}
