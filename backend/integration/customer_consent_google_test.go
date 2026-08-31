package integration

import (
	"database/sql"
	"net/http"
	"testing"
	"time"
)

// The consent gate on the Google door (#252, parent #249).
//
// Everything here is a mirror of customer_consent_test.go, and the mirroring is
// the assertion. The two Proof of Email Ownership doors are equal (ADR 0011) and
// converge on one function, so what the passcode door owes a Customer the Google
// door owes them identically: no session without Policy Acceptance of the
// current Policy Version, the same pending-consent token, the same submission
// endpoint, the same evidence with the same channel. A test here that had to be
// written differently from its twin over there would be reporting a divergence.
//
// It is a separate file from the passcode suite because it is a separate door,
// not because it is a separate feature: nothing here reaches a Google-specific
// code path on the far side of the token exchange, and that is exactly what is
// being pinned.

// startGoogleSignIn redeems a Google authorization code for whatever the verify
// answers with — a session, or a consent step. It is the Google-side twin of
// startSignIn: customerGoogleSignIn without the consent step folded in, for the
// tests that are about the gate itself.
func startGoogleSignIn(t *testing.T, env *testEnv, email string) customerVerifyData {
	t.Helper()
	googleStub.returns(verifiedGoogleClaims(email))

	resp, body := postGoogleVerify(t, env)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("google verify status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return decodeCustomerVerify(t, body)
}

// readCustomerAvatarKey reads the stored Avatar object key, absent until
// something seeds or uploads one. SQL because the key is never published: the
// session view carries a URL derived from it, and this test needs to know the
// slot was filled even when no session exists to derive one from.
func readCustomerAvatarKey(t *testing.T, env *testEnv, email string) sql.NullString {
	t.Helper()
	var key sql.NullString
	if err := env.db.QueryRow(`SELECT avatar_image_key FROM customers WHERE email = $1`, email).Scan(&key); err != nil {
		t.Fatalf("read avatar key for %q: %v", email, err)
	}
	return key
}

// TestGoogleSignInWithConsentOutstandingMintsNoSession is the gate on this door,
// asserted in its own right rather than inferred from the passcode door's: a
// verified Google token for a Customer with no Policy Acceptance buys a consent
// step and nothing else.
func TestGoogleSignInWithConsentOutstandingMintsNoSession(t *testing.T) {
	env := setupTest(t)

	data := startGoogleSignIn(t, env, "ana@example.com")

	if data.SessionID != "" {
		t.Fatalf("session_id = %q — a Google Sign-In by a Customer with no Policy Acceptance must mint no session", data.SessionID)
	}
	if data.Session != nil {
		t.Fatalf("session = %+v, want null", data.Session)
	}
	if data.ConsentRequired == nil {
		t.Fatal("expected a consent-required outcome from a verified Google token")
	}
	if data.ConsentRequired.PendingConsentToken == "" {
		t.Fatal("expected a pending-consent token")
	}
	// A first sign-in has answered nothing, so all three boxes are shown — the
	// same set the passcode door is told to render.
	want := consentBoxes{PolicyAcceptance: true, MarketingConsent: true, NetworkingConsent: true, TermsAcceptance: true}
	if data.ConsentRequired.Boxes != want {
		t.Fatalf("boxes = %+v, want %+v", data.ConsentRequired.Boxes, want)
	}
	expires, err := time.Parse(time.RFC3339, data.ConsentRequired.ExpiresAt)
	if err != nil {
		t.Fatalf("parse expires_at %q: %v", data.ConsentRequired.ExpiresAt, err)
	}
	if !expires.After(env.fixedClock) || expires.After(env.fixedClock.Add(time.Hour)) {
		t.Fatalf("expires_at = %s, want a short window after %s", expires, env.fixedClock)
	}

	// Proof of ownership still happened. Google vouched for the address, so the
	// Customer exists and is verified; only the credential was withheld.
	if c := readCustomer(t, env, "ana@example.com"); !c.VerifiedAt.Valid {
		t.Fatal("a verified Google token must still stamp verified_at, even when the session is withheld")
	}
	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0 — no session may exist before consent is recorded", n)
	}
	// Being asked is not answering, on this door as on the other.
	if records := readConsentRecords(t, env, "ana@example.com"); len(records) != 0 {
		t.Fatalf("consent records = %d, want 0", len(records))
	}
}

// TestAbandonedGoogleConsentStepLeavesTheVisitorSignedOut: refusing consent
// costs a Google visitor nothing they had, exactly as it costs a passcode
// visitor nothing.
func TestAbandonedGoogleConsentStepLeavesTheVisitorSignedOut(t *testing.T) {
	env := setupTest(t)

	data := startGoogleSignIn(t, env, "ana@example.com")
	if data.ConsentRequired == nil {
		t.Fatal("expected a consent-required outcome")
	}

	// ... and the browser never comes back.

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
}

// TestGoogleConsentSubmissionRecordsEvidenceAndMintsTheSession is the crossing
// on this door: the SAME submission endpoint, the same evidence, and a session
// indistinguishable from the one a passcode's consent step produces.
func TestGoogleConsentSubmissionRecordsEvidenceAndMintsTheSession(t *testing.T) {
	env := setupTest(t)

	data := startGoogleSignIn(t, env, "ana@example.com")
	token := data.ConsentRequired.PendingConsentToken

	resp, body := env.post(t, customerConsentPath,
		consentAnswers(token, true, true, false), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
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
	// A full Customer Session, and a real one: it authenticates and it spans every
	// Ticket Sale rather than naming one.
	resp, body = env.get(t, customerSessionPath, authHeader(submitted.SessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session read status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if submitted.Session.TicketSaleID != nil {
		t.Fatalf("ticket_sale_id = %v, want null on a full Customer Session", *submitted.Session.TicketSaleID)
	}
	if got, want := sessionExpiry(t, env, submitted.SessionID), env.fixedClock.Add(customerSessionDuration); !got.Equal(want) {
		t.Fatalf("session expires_at = %s, want %s", got, want)
	}

	records := readConsentRecords(t, env, "ana@example.com")
	if len(records) != 1 {
		t.Fatalf("consent records = %d, want exactly one per capture act", len(records))
	}
	record := records[0]
	// SIGN-IN, not "google": the channel names the surface a person was on, and
	// which door they came through is deliberately not recorded anywhere (ADR
	// 0011). An evidence log that could tell them apart would be the first place
	// the two doors diverged.
	if record.Channel != "signin" {
		t.Fatalf("channel = %q, want signin — the evidence names the surface, never the door", record.Channel)
	}
	if record.Email != "ana@example.com" {
		t.Fatalf("record email = %q", record.Email)
	}
	if !record.CapturedAt.Equal(env.fixedClock) {
		t.Fatalf("captured_at = %s, want the server clock %s", record.CapturedAt, env.fixedClock)
	}
	if record.PolicyVersionID != currentPolicyVersionID(t, env) {
		t.Fatalf("policy_version_id = %q, want the current edition", record.PolicyVersionID)
	}
	if !record.PolicyAcceptance.Valid || !record.PolicyAcceptance.Bool {
		t.Fatalf("policy_acceptance = %+v, want true", record.PolicyAcceptance)
	}
	if !record.MarketingConsent.Valid || !record.MarketingConsent.Bool {
		t.Fatalf("marketing_consent = %+v, want the tick that was made", record.MarketingConsent)
	}
	// Shown and left unticked: an explicit false, never null.
	if !record.NetworkingConsent.Valid || record.NetworkingConsent.Bool {
		t.Fatalf("networking_consent = %+v, want an explicit false", record.NetworkingConsent)
	}
	// A Google token is Proof of Email Ownership by the same rule a passcode is,
	// so a tick here is a lawful basis and not a Pending Confirmation (ADR 0035).
	if !record.EmailProven {
		t.Fatal("email_proven = false — a verified Google token is proof of email ownership")
	}
	if record.ConfirmedAt.Valid {
		t.Fatalf("confirmed_at = %v, want null — nothing here is pending", record.ConfirmedAt.Time)
	}
	// The technical proof comes from the request the BROWSER made to the consent
	// submission, which on this door is a request the Storefront made after a
	// redirect — so it must be present all the same.
	if record.IP.String != "198.51.100.24" {
		t.Fatalf("ip = %q, want the address the BFF derived", record.IP.String)
	}
	if record.UserAgent.String != "Mozilla/5.0 (consent-test)" {
		t.Fatalf("user_agent = %q", record.UserAgent.String)
	}
	if record.OriginURL.String != "http://storefront.example/es/signin" {
		t.Fatalf("origin_url = %q", record.OriginURL.String)
	}
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
	if !state.DigestEnabled {
		t.Fatal("digest_enabled is false after Marketing Consent was granted — the two are one switch (ADR 0034)")
	}
	if state.NetworkingConsent.String != "denied" {
		t.Fatalf("networking_consent = %q, want denied", state.NetworkingConsent.String)
	}
}

// TestGoogleConsentSubmissionWithoutPolicyAcceptanceIsRefusedByTheAPI: the
// refusal is the platform's on this door too, and the proof is spent with the
// token — so a Google visitor who declines starts the sign-in again rather than
// retrying against the same credential.
func TestGoogleConsentSubmissionWithoutPolicyAcceptanceIsRefusedByTheAPI(t *testing.T) {
	env := setupTest(t)

	data := startGoogleSignIn(t, env, "ana@example.com")
	token := data.ConsentRequired.PendingConsentToken

	resp, body := env.post(t, customerConsentPath,
		consentAnswers(token, false, true, true), consentEvidenceHeaders())
	assertAPIError(t, resp, body, http.StatusBadRequest, "POLICY_ACCEPTANCE_REQUIRED")

	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0 after a refused submission", n)
	}
	if records := readConsentRecords(t, env, "ana@example.com"); len(records) != 0 {
		t.Fatalf("consent records = %d, want 0", len(records))
	}
	state := readConsentState(t, env, "ana@example.com")
	if state.PolicyAcceptedAt.Valid || state.MarketingConsent.Valid || state.NetworkingConsent.Valid {
		t.Fatalf("a refused submission wrote state: %+v", state)
	}

	resp, body = env.post(t, customerConsentPath,
		consentAnswers(token, true, false, false), consentEvidenceHeaders())
	assertAPIError(t, resp, body, http.StatusUnauthorized, "PENDING_CONSENT_INVALID")
}

// TestAnsweredCustomerGoogleSignsInWithNoConsentStep is user story 4 on this
// door: a returning Customer who has answered everything meets no consent UI.
func TestAnsweredCustomerGoogleSignsInWithNoConsentStep(t *testing.T) {
	env := setupTest(t)

	first := startGoogleSignIn(t, env, "ana@example.com")
	resp, body := env.post(t, customerConsentPath,
		consentAnswers(first.ConsentRequired.PendingConsentToken, true, true, true), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}

	second := startGoogleSignIn(t, env, "ana@example.com")
	if second.ConsentRequired != nil {
		t.Fatalf("consent_required = %+v — an answered Customer must sail through", second.ConsentRequired)
	}
	if second.SessionID == "" {
		t.Fatal("expected a session straight from the Google verify")
	}
	if second.Session.Email != "ana@example.com" {
		t.Fatalf("session email = %q", second.Session.Email)
	}
	// One capture act, one record: signing in again is not a capture.
	if records := readConsentRecords(t, env, "ana@example.com"); len(records) != 1 {
		t.Fatalf("consent records = %d, want one", len(records))
	}
}

// TestConsentAnsweredAtOneDoorSailsThroughTheOther is the ADR 0011 assertion
// stated as a fact about a person rather than about a protocol: consent is a
// property of the Customer, so answering at either door satisfies both. Somebody
// who accepted the policy after a passcode is not asked again because they came
// back through Google, and vice versa.
func TestConsentAnsweredAtOneDoorSailsThroughTheOther(t *testing.T) {
	env := setupTest(t)

	// Answered at the passcode door.
	viaPasscode := startSignIn(t, env, "passcode-first@example.com")
	resp, body := env.post(t, customerConsentPath,
		consentAnswers(viaPasscode.ConsentRequired.PendingConsentToken, true, true, true), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if google := startGoogleSignIn(t, env, "passcode-first@example.com"); google.ConsentRequired != nil {
		t.Fatalf("consent_required = %+v — a Customer who answered at the passcode door must not be re-asked at the Google one",
			google.ConsentRequired)
	}

	// Answered at the Google door.
	viaGoogle := startGoogleSignIn(t, env, "google-first@example.com")
	resp, body = env.post(t, customerConsentPath,
		consentAnswers(viaGoogle.ConsentRequired.PendingConsentToken, true, true, true), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if passcode := startSignIn(t, env, "google-first@example.com"); passcode.ConsentRequired != nil {
		t.Fatalf("consent_required = %+v — a Customer who answered at the Google door must not be re-asked at the passcode one",
			passcode.ConsentRequired)
	}

	// Two Customers, one capture act each. Neither second sign-in evidenced
	// anything, because neither asked anything.
	for _, email := range []string{"passcode-first@example.com", "google-first@example.com"} {
		if records := readConsentRecords(t, env, email); len(records) != 1 {
			t.Fatalf("consent records for %q = %d, want one", email, len(records))
		}
	}
}

// TestGoogleSignInSeedsTheAvatarAcrossTheConsentStep pins the one thing about
// this door that is not the other door: Google offers a profile picture and a
// passcode does not, so the Avatar seed is the single asymmetry the consent gate
// could plausibly have broken.
//
// It could not, and the reason is structural: the seed happens in
// signInProvenEmail BEFORE the gate, on the way to a session that may never be
// minted. So a visitor who abandons the consent step still leaves with an Avatar
// in their record, and the session the submission finally mints carries it —
// even though that session is created by a code path (SubmitConsent) that has
// never heard of Google, a picture, or a claim.
func TestGoogleSignInSeedsTheAvatarAcrossTheConsentStep(t *testing.T) {
	env := setupTest(t)
	pictures := pictureServer(t, "image/png", http.StatusOK)

	googleStub.returns(googleClaimsWithPicture("ana@example.com", pictures.URL+"/photo.png"))
	resp, body := postGoogleVerify(t, env)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("google verify status=%d error=%+v", resp.StatusCode, body.Error)
	}
	data := decodeCustomerVerify(t, body)
	if data.ConsentRequired == nil {
		t.Fatal("expected a consent-required outcome")
	}

	// Held at the gate, and the Avatar is already there: the seed belongs to
	// proving the address, not to being signed in.
	key := readCustomerAvatarKey(t, env, "ana@example.com")
	if !key.Valid || key.String == "" {
		t.Fatal("avatar_image_key is empty at the consent step — the seed must happen with the proof, not with the session")
	}

	resp, body = env.post(t, customerConsentPath,
		consentAnswers(data.ConsentRequired.PendingConsentToken, true, false, false), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	submitted := decodeCustomerVerify(t, body)

	// The session the consent step minted wears the Avatar the sign-in seeded.
	if submitted.Session.AvatarURL == nil {
		t.Fatal("session avatar_url = null after the consent step — the seed was lost across the gate")
	}
	if got := readCustomerAvatarKey(t, env, "ana@example.com"); got.String != key.String {
		t.Fatalf("avatar_image_key = %q after the consent step, want the seeded %q unchanged", got.String, key.String)
	}
}

// TestAbandonedGoogleConsentStepStillLeavesTheSeededAvatar is the same
// asymmetry from the side that costs nothing: everything upstream of the session
// happens whether or not consent is ever answered, so a visitor who walks away
// and comes back later — by either door — finds the Avatar already seeded and is
// not seeded a second one.
func TestAbandonedGoogleConsentStepStillLeavesTheSeededAvatar(t *testing.T) {
	env := setupTest(t)
	pictures := pictureServer(t, "image/png", http.StatusOK)

	googleStub.returns(googleClaimsWithPicture("ana@example.com", pictures.URL+"/first.png"))
	resp, body := postGoogleVerify(t, env)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("google verify status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if decodeCustomerVerify(t, body).ConsentRequired == nil {
		t.Fatal("expected a consent-required outcome")
	}
	seeded := readCustomerAvatarKey(t, env, "ana@example.com")
	if !seeded.Valid || seeded.String == "" {
		t.Fatal("an abandoned consent step lost the Avatar seed")
	}

	// Back later through the OTHER door, which offers no picture at all. The
	// set-once rule holds and the seed survives a completed sign-in it had no
	// part in.
	token := customerSignIn(t, env, "ana@example.com")
	if got := readCustomerAvatarKey(t, env, "ana@example.com"); got.String != seeded.String {
		t.Fatalf("avatar_image_key = %q, want the seeded %q kept", got.String, seeded.String)
	}
	resp, body = env.get(t, customerSessionPath, authHeader(token))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session read status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// TestGoogleDoorDisclosesNothingAboutConsentBeforeGoogleVouches is the oracle
// discipline on this door (ADR 0035, user story 29). The consent-required
// outcome names a known Customer, so it must be reachable only past a token
// Google actually honoured — and a refused exchange must look the same whether
// the address behind it has consent outstanding, has answered everything, or has
// never been heard of.
func TestGoogleDoorDisclosesNothingAboutConsentBeforeGoogleVouches(t *testing.T) {
	env := setupTest(t)

	// A Customer with everything answered, and one with nothing.
	answered := startGoogleSignIn(t, env, "answered@example.com")
	resp, body := env.post(t, customerConsentPath,
		consentAnswers(answered.ConsentRequired.PendingConsentToken, true, true, true), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	startGoogleSignIn(t, env, "outstanding@example.com")

	// Google refuses the exchange. Nothing downstream of the proof runs, so the
	// three addresses are indistinguishable from out here.
	refusal := func() string {
		t.Helper()
		googleStub.rejects()
		resp, body := postGoogleVerify(t, env)
		if body.Error == nil {
			t.Fatalf("expected a refusal for a rejected exchange, got %s", body.Data)
		}
		return http.StatusText(resp.StatusCode) + ":" + body.Error.Code + ":" + body.Error.Message
	}
	first := refusal()
	if second := refusal(); second != first {
		t.Fatalf("two refused exchanges answered differently: %q and %q", first, second)
	}

	// And an address Google will not vouch for answers the same way whether or
	// not consent is outstanding behind it.
	unverified := func(email string) string {
		t.Helper()
		claims := verifiedGoogleClaims(email)
		claims["email_verified"] = false
		googleStub.returns(claims)
		resp, body := postGoogleVerify(t, env)
		if body.Error == nil {
			t.Fatalf("expected a refusal for an unverified address, got %s", body.Data)
		}
		return http.StatusText(resp.StatusCode) + ":" + body.Error.Code + ":" + body.Error.Message
	}
	outstanding := unverified("outstanding@example.com")
	if got := unverified("answered@example.com"); got != outstanding {
		t.Fatalf("the refusal differed by consent state: outstanding=%q answered=%q", outstanding, got)
	}
	if got := unverified("nobody-here@example.com"); got != outstanding {
		t.Fatalf("the refusal differed for an unknown address: outstanding=%q stranger=%q", outstanding, got)
	}
	if got := unverified("answered@example.com"); got != first {
		t.Fatalf("a refused exchange (%q) and an unverified address (%q) must not be distinguishable", first, got)
	}
}
