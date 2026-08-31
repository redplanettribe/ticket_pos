package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// The Digest merge (#256, parent #249, ADR 0034): Marketing Consent and the
// Follow Digest are one switch, and the two surfaces that could already silence
// the Digest become consent acts that leave evidence.
//
// TWO THINGS ARE UNDER TEST HERE and they are the two halves of the ADR:
//
//   - THE SENDER'S RULE. Send when Marketing Consent is granted; send when it is
//     UNANSWERED and the legacy `digest_enabled` flag is on; never when it is
//     denied and never when it is Pending Confirmation. The middle arm is the
//     transition clause — the legacy default is not consent and is never claimed
//     as such, it merely keeps the Digest running for people nobody has asked
//     yet.
//   - THE TWO EXISTING SURFACES. The Customer Area toggle and the signed
//     unsubscribe link now write consent state and an ordinary Consent Record
//     under their own channels, instead of flipping a boolean in silence.
//
// The seam is HTTP throughout: Events are published, Follows are pressed, the
// enqueue and drain endpoints are called, and what a person receives is read off
// the captured mail. The consent columns and the evidence log are read through
// SQL for the reason customer_consent_test.go states — the platform publishes
// neither, deliberately.

// forgetConsentAnswers makes a Customer into a PRE-CONSENT one: a person who
// Follows, has never been asked anything, and carries only the legacy flag every
// Customer was ever created with.
//
// SQL because no API can produce one any more, which is the point of the
// transition clause. A Follow takes a full Customer Session, every sign-in now
// passes through the consent step, and every consent step records an answer — so
// the only Customers in this state are the ones who existed before #249 shipped,
// and this is how the suite builds one.
func forgetConsentAnswers(t *testing.T, env *testEnv, email string, digestEnabled bool) {
	t.Helper()
	if _, err := env.db.Exec(`
		UPDATE customers
		SET marketing_consent = NULL, networking_consent = NULL, digest_enabled = $2
		WHERE email = $1
	`, email, digestEnabled); err != nil {
		t.Fatalf("forget consent answers for %q: %v", email, err)
	}
}

// forceConsentState writes a Marketing Consent state and a legacy flag directly,
// so that a test can put the two DELIBERATELY OUT OF STEP.
//
// The write path keeps them in lockstep (ADR 0034), so this is the only way to
// prove that the sender obeys the consent and not the flag — which is what has
// to be true of a row left behind by an older version of this platform, or by a
// Pending Confirmation, whose whole nature is a state the flag does not follow.
func forceConsentState(t *testing.T, env *testEnv, email, marketing string, digestEnabled bool) {
	t.Helper()
	if _, err := env.db.Exec(`
		UPDATE customers SET marketing_consent = $2, digest_enabled = $3 WHERE email = $1
	`, email, marketing, digestEnabled); err != nil {
		t.Fatalf("force marketing consent %q for %q: %v", marketing, email, err)
	}
}

// decliningFollowingCustomer signs a Customer in, DECLINES Marketing Consent at
// the consent step, and Follows the test Organization.
//
// The shared customerSignIn helper grants both optional consents, which is what
// makes it model the ordinary Customer the rest of the suite means. A test about
// a No has to build its own person, and this is that person.
func decliningFollowingCustomer(t *testing.T, env *testEnv, email string) string {
	t.Helper()
	data := startSignIn(t, env, email)
	if data.ConsentRequired == nil {
		t.Fatal("expected a consent step for a Customer who has never answered")
	}
	resp, body := env.post(t, customerConsentPath,
		consentAnswers(data.ConsentRequired.PendingConsentToken, true, false, false),
		consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	token := decodeCustomerVerify(t, body).SessionID
	if token == "" {
		t.Fatal("declining an optional consent must still mint a Customer Session")
	}
	followOrganizationOK(t, env, token, testOrgSlug)
	return token
}

// evidenceHeaders merges the circumstances of a request with a credential, for
// the surfaces that carry both.
func evidenceHeaders(extra map[string]string) map[string]string {
	headers := consentEvidenceHeaders()
	for k, v := range extra {
		headers[k] = v
	}
	return headers
}

// setDigestEnabledWithEvidence presses the Customer Area toggle the way a
// browser does: a session, and the circumstances the handler derives evidence
// from.
func setDigestEnabledWithEvidence(t *testing.T, env *testEnv, token string, enabled bool) digestSubscriptionView {
	t.Helper()
	resp, body := env.put(t, customerDigestPath, map[string]bool{"enabled": enabled}, evidenceHeaders(authHeader(token)))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set digest enabled=%v status=%d error=%+v", enabled, resp.StatusCode, body.Error)
	}
	var view digestSubscriptionView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode digest subscription: %v", err)
	}
	return view
}

// unsubscribeWithEvidence presses the link's confirmation with the
// circumstances, and NO CREDENTIAL — which is the contract (ADR 0030).
func unsubscribeWithEvidence(t *testing.T, env *testEnv, token string) {
	t.Helper()
	resp, body := env.post(t, customerUnsubscribePath, map[string]string{"token": token}, consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unsubscribe status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("unsubscribe error=%+v, want none", body.Error)
	}
}

// oneConsentRecordOn insists there is exactly one record on a channel and
// returns it.
func oneConsentRecordOn(t *testing.T, env *testEnv, email, channel string) consentRecordRow {
	t.Helper()
	records := consentRecordsOn(t, env, email, channel)
	if len(records) != 1 {
		t.Fatalf("consent records on %s = %d, want exactly 1", channel, len(records))
	}
	return records[0]
}

// digestWeek publishes an Event, runs the weekly enqueue and drains it — one
// week of the Digest, end to end.
func digestWeek(t *testing.T, env *testEnv, sessionID, name, slug string) followDigestEnqueueResult {
	t.Helper()
	discoverableEvent(t, env, sessionID, name, slug, env.fixedClock.Add(72*time.Hour))
	result := enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)
	return result
}

// TestDigestSendsToGrantedMarketingConsent is the first arm of the rule, and the
// ordinary case: somebody who was asked and said yes.
func TestDigestSendsToGrantedMarketingConsent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	followingCustomer(t, env, "ana@example.com")

	if state := readConsentState(t, env, "ana@example.com"); state.MarketingConsent.String != "granted" {
		t.Fatalf("marketing_consent=%q after granting at sign-in, want granted", state.MarketingConsent.String)
	}

	digestWeek(t, env, sessionID, "Granted Fest", "granted-fest")

	if got := len(digestsFor(t, env, "ana@example.com")); got != 1 {
		t.Fatalf("ana received %d Digests with Marketing Consent granted, want 1", got)
	}
}

// TestDigestSendsToUnansweredCustomerWithLegacyFlag is the transition clause,
// and the story the parent issue tells: a Customer who Follows and has not yet
// been asked keeps hearing what they subscribed to.
//
// The legacy flag is NOT consent and is never claimed as such. It keeps the
// Digest running on the strength of Follow being a request to be written to,
// plus the unsubscribe link in every issue — until the person is asked.
func TestDigestSendsToUnansweredCustomerWithLegacyFlag(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	followingCustomer(t, env, "ana@example.com")
	forgetConsentAnswers(t, env, "ana@example.com", true)

	digestWeek(t, env, sessionID, "Legacy Fest", "legacy-fest")

	if got := len(digestsFor(t, env, "ana@example.com")); got != 1 {
		t.Fatalf("ana received %d Digests unanswered with the legacy flag on, want 1 — the transition must not silence a Follower nobody has asked yet", got)
	}
}

// resetDigestFlagOff puts one Customer's Follow Digest flag back to the off a
// new row is born with (migration 065). It exists beside
// resetOptionalConsentsToUnanswered for the same #536 reason: settling the
// Terms at a fixture sign-in answers the marketing box, which switches the
// Digest on — and the tests about a Customer nobody has asked need the flag as
// such an account would genuinely have it.
func resetDigestFlagOff(t *testing.T, env *testEnv, email string) {
	t.Helper()
	if _, err := env.db.Exec(`
		UPDATE customers SET digest_enabled = false WHERE email = $1
	`, email); err != nil {
		t.Fatalf("reset digest flag for %q: %v", email, err)
	}
}

// TestDigestSilentForACustomerBornDeclining is the other side of the transition
// clause, and the reason the legacy default had to stop applying to new rows
// (migration 065).
//
// A guest reads the notice, deliberately leaves the marketing box unticked, and
// buys a ticket. Their No is unproven — a stranger could have typed the address
// — so it is kept as evidence and writes no state, which is what stops it
// silencing somebody else's standing subscription. But the Customer that sale
// creates must not then be enrolled by the transition arm: it exists for people
// who were subscribed under the old opt-out regime, and this person has no
// history at all. Were `digest_enabled` still to default TRUE, unanswered plus
// the default would read exactly like a legacy subscriber, and the first thing
// they Followed would start sending them the mail they declined.
func TestDigestSilentForACustomerBornDeclining(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)

	// Her Terms are settled at a sign-in of her own first (#536), and her
	// optional consents and Digest flag put back to never-answered/off — the
	// state of an account whose owner has not signed in since the boxes existed
	// (migration 065 defaults new rows off) — so the Follow below is not
	// preceded by a consent step that would grant the very box she declines.
	customerSignIn(t, env, "ana@example.com")
	resetOptionalConsentsToUnanswered(t, env, "ana@example.com")
	resetDigestFlagOff(t, env, "ana@example.com")

	// The decline, on the surface that could not prove who was typing: a guest
	// Payment begun before ADR 0054 closed that door (#386), settling here. The
	// checkout can no longer be BEGUN this way, and the row it leaves is exactly
	// the row this test is about.
	begin := beginLegacyGuestCheckout(t, env, "consent-fest", "ana@example.com", "Ana", "Lopez",
		boolPtr(true), boolPtr(false), boolPtr(false), cartLine(gaID, 1))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	// She Follows something, which is the only way a Digest is ever about
	// anything — and the moment the transition arm would have caught her.
	followingCustomer(t, env, "ana@example.com")

	digestWeek(t, env, sessionID, "Born Declining Fest", "born-declining-fest")

	if got := len(digestsFor(t, env, "ana@example.com")); got != 0 {
		t.Fatalf("ana received %d Digests after declining marketing at the checkout that created her, want 0", got)
	}
}

// TestDigestSilentForUnansweredCustomerWithLegacyFlagOff: the legacy flag still
// means what it always meant. Somebody who unsubscribed before consent existed
// stays unsubscribed, and is not resubscribed by the merge.
func TestDigestSilentForUnansweredCustomerWithLegacyFlagOff(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	followingCustomer(t, env, "ana@example.com")
	forgetConsentAnswers(t, env, "ana@example.com", false)

	result := digestWeek(t, env, sessionID, "Quiet Fest", "quiet-fest")

	if result.Eligible != 0 || result.Enqueued != 0 {
		t.Fatalf("enqueue eligible=%d enqueued=%d for an unanswered Customer with the flag off, want 0 and 0", result.Eligible, result.Enqueued)
	}
	if got := len(digestsFor(t, env, "ana@example.com")); got != 0 {
		t.Fatalf("ana received %d Digests, want 0", got)
	}
}

// TestDigestNeverSendsToDeniedMarketingConsent is the arm with teeth: a No is a
// No whatever the legacy flag says.
//
// The flag is forced back ON after the refusal, which the write path would never
// do, because that is the only way to prove the sender reads the CONSENT. A row
// like this is what a stale flag from before the merge looks like, and treating
// it as permission would mail somebody who has explicitly refused.
func TestDigestNeverSendsToDeniedMarketingConsent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	decliningFollowingCustomer(t, env, "ana@example.com")

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "denied" {
		t.Fatalf("marketing_consent=%q after leaving the box unticked, want denied", state.MarketingConsent.String)
	}
	if state.DigestEnabled {
		t.Fatal("digest_enabled is true after Marketing Consent was declined; the two are one switch (ADR 0034)")
	}

	forceConsentState(t, env, "ana@example.com", "denied", true)
	result := digestWeek(t, env, sessionID, "Denied Fest", "denied-fest")

	if result.Eligible != 0 || result.Enqueued != 0 {
		t.Fatalf("enqueue eligible=%d enqueued=%d for a denied Customer, want 0 and 0", result.Eligible, result.Enqueued)
	}
	if got := len(digestsFor(t, env, "ana@example.com")); got != 0 {
		t.Fatalf("ana received %d Digests after declining Marketing Consent, want 0", got)
	}
}

// TestDigestNeverSendsToPendingConfirmation: a tick from an address nobody
// proved authorizes nothing (ADR 0035). Pending is denied for sending.
//
// The state is written directly because the surface that produces it is the
// guest checkout, which is another ticket's; the sender's obligation is the same
// however the state arrived.
func TestDigestNeverSendsToPendingConfirmation(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	followingCustomer(t, env, "ana@example.com")
	forceConsentState(t, env, "ana@example.com", "pending_confirmation", true)

	result := digestWeek(t, env, sessionID, "Pending Fest", "pending-fest")

	if result.Eligible != 0 || result.Enqueued != 0 {
		t.Fatalf("enqueue eligible=%d enqueued=%d for a Pending Confirmation, want 0 and 0", result.Eligible, result.Enqueued)
	}
	if got := len(digestsFor(t, env, "ana@example.com")); got != 0 {
		t.Fatalf("ana received %d Digests on a Pending Confirmation, want 0 — a stranger's tick is not consent", got)
	}
}

// TestDeniedAfterEnqueueIsSkippedByTheDrain closes the window the enqueue filter
// cannot span, for the consent rule rather than for the bare flag.
//
// The send-time read has to ask the same question the enqueue asked, or a
// Customer whose consent changed in that window is written to anyway.
func TestDeniedAfterEnqueueIsSkippedByTheDrain(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Window Fest", "consent-window-fest", env.fixedClock.Add(72*time.Hour))
	followingCustomer(t, env, "ana@example.com")

	enqueueFollowDigests(t, env)
	// Denied with the legacy flag left ON, so only the consent can stop this.
	forceConsentState(t, env, "ana@example.com", "denied", true)

	result := drainFollowDigests(t, env)
	if result.Claimed != 1 || result.Skipped != 1 || result.Sent != 0 {
		t.Fatalf("drain claimed=%d skipped=%d sent=%d, want 1, 1 and 0 (%+v)", result.Claimed, result.Skipped, result.Sent, result)
	}
	if got := len(digestsFor(t, env, "ana@example.com")); got != 0 {
		t.Fatalf("ana received %d Digests after denying in the drain window, want 0", got)
	}
	if status, _ := digestRowStatus(t, env, "ana@example.com"); status != "skipped" {
		t.Fatalf("Digest status=%q, want skipped", status)
	}
}

// TestDigestToggleOffWritesDeniedWithEvidence: the Customer Area switch is a
// consent act now, and leaves the record of one.
func TestDigestToggleOffWritesDeniedWithEvidence(t *testing.T) {
	env := setupTest(t)
	orgAdminSession(t, env)
	token := followingCustomer(t, env, "ana@example.com")
	before := len(readConsentRecords(t, env, "ana@example.com"))

	if view := setDigestEnabledWithEvidence(t, env, token, false); view.DigestEnabled {
		t.Fatal("turning the Digest off reported it as on")
	}

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "denied" {
		t.Fatalf("marketing_consent=%q after switching the Digest off, want denied", state.MarketingConsent.String)
	}
	if state.DigestEnabled {
		t.Fatal("digest_enabled is still true after switching the Digest off")
	}
	// The Networking Consent the sign-in granted is untouched: the box was not
	// shown on this surface, and a nil answer writes nothing.
	if state.NetworkingConsent.String != "granted" {
		t.Fatalf("networking_consent=%q after a marketing-only act, want granted — a box that was not shown must not be re-answered", state.NetworkingConsent.String)
	}

	if got := len(readConsentRecords(t, env, "ana@example.com")); got != before+1 {
		t.Fatalf("consent records = %d, want %d — one act, one record", got, before+1)
	}
	last := oneConsentRecordOn(t, env, "ana@example.com", "account_settings")
	if !last.MarketingConsent.Valid || last.MarketingConsent.Bool {
		t.Fatalf("marketing_consent=%+v in the record, want false", last.MarketingConsent)
	}
	if last.PolicyAcceptance.Valid || last.NetworkingConsent.Valid {
		t.Fatalf("record answers policy=%+v networking=%+v, want NULL for both — those boxes were not shown here", last.PolicyAcceptance, last.NetworkingConsent)
	}
	if !last.EmailProven {
		t.Fatal("email_proven=false for an act behind a Customer Session")
	}
	if last.Email != "ana@example.com" {
		t.Fatalf("record email=%q, want ana@example.com", last.Email)
	}
	if last.IP.String != "198.51.100.24" || last.UserAgent.String != "Mozilla/5.0 (consent-test)" {
		t.Fatalf("evidence ip=%+v user_agent=%+v, want the request's own", last.IP, last.UserAgent)
	}
	if last.OriginURL.String != "http://storefront.example/es/signin" {
		t.Fatalf("origin_url=%+v, want the page the act happened on", last.OriginURL)
	}
	if last.SessionID.String != token {
		t.Fatalf("session_id=%+v, want the Customer Session the act was made under", last.SessionID)
	}
}

// TestDigestToggleOnWritesGrantedWithEvidence: turning it back on is a grant,
// not merely a flag, and the log holds both acts in order.
func TestDigestToggleOnWritesGrantedWithEvidence(t *testing.T) {
	env := setupTest(t)
	orgAdminSession(t, env)
	token := decliningFollowingCustomer(t, env, "ana@example.com")
	before := len(readConsentRecords(t, env, "ana@example.com"))

	if view := setDigestEnabledWithEvidence(t, env, token, true); !view.DigestEnabled {
		t.Fatal("turning the Digest on reported it as off")
	}

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "granted" {
		t.Fatalf("marketing_consent=%q after switching the Digest on, want granted", state.MarketingConsent.String)
	}
	if !state.DigestEnabled {
		t.Fatal("digest_enabled is false after switching the Digest on")
	}

	if got := len(readConsentRecords(t, env, "ana@example.com")); got != before+1 {
		t.Fatalf("consent records = %d, want %d", got, before+1)
	}
	last := oneConsentRecordOn(t, env, "ana@example.com", "account_settings")
	if !last.MarketingConsent.Valid || !last.MarketingConsent.Bool {
		t.Fatalf("record marketing=%+v, want true", last.MarketingConsent)
	}
	if !last.EmailProven {
		t.Fatal("email_proven=false for a grant made behind a Customer Session; only a proven answer can grant")
	}
}

// TestUnsubscribeLinkWritesDeniedWithEvidence: the one unauthenticated write in
// the Customer Area now records what it did, under its own channel.
func TestUnsubscribeLinkWritesDeniedWithEvidence(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	followingCustomer(t, env, "ana@example.com")
	digestWeek(t, env, sessionID, "Footer Fest", "consent-footer-fest")

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want 1", len(digests))
	}
	before := len(readConsentRecords(t, env, "ana@example.com"))
	unsubscribeWithEvidence(t, env, unsubscribeTokenFrom(t, digests[0]))

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "denied" {
		t.Fatalf("marketing_consent=%q after pressing unsubscribe, want denied", state.MarketingConsent.String)
	}
	if state.DigestEnabled {
		t.Fatal("digest_enabled is still true after pressing unsubscribe")
	}

	if got := len(readConsentRecords(t, env, "ana@example.com")); got != before+1 {
		t.Fatalf("consent records = %d, want %d", got, before+1)
	}
	last := oneConsentRecordOn(t, env, "ana@example.com", "unsubscribe_link")
	if !last.MarketingConsent.Valid || last.MarketingConsent.Bool {
		t.Fatalf("marketing_consent=%+v, want false", last.MarketingConsent)
	}
	// Proven: the token travelled only in a Digest addressed to this Customer,
	// so pressing it is an act by somebody with access to that inbox — the same
	// argument ADR 0035 makes for the confirmation link. It is also what makes
	// the No bite: an unproven No is written only over an unanswered state, and
	// this No has to silence a Customer who granted.
	if !last.EmailProven {
		t.Fatal("email_proven=false on the unsubscribe link; an unproven No would not silence a Customer who had granted")
	}
	if last.SessionID.Valid {
		t.Fatalf("session_id=%+v, want NULL — this route is session-less by contract (ADR 0030)", last.SessionID)
	}
	if last.IP.String != "198.51.100.24" || last.UserAgent.String != "Mozilla/5.0 (consent-test)" {
		t.Fatalf("evidence ip=%+v user_agent=%+v, want the request's own", last.IP, last.UserAgent)
	}
}

// TestUnsubscribingTwiceRecordsBothActs: pressing it twice stays non-erroneous —
// every Digest a person received carries the same link — and the log records two
// acts, because the log says what happened rather than what changed.
func TestUnsubscribingTwiceRecordsBothActs(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	followingCustomer(t, env, "ana@example.com")
	digestWeek(t, env, sessionID, "Twice Fest", "consent-twice-fest")

	token := unsubscribeTokenFrom(t, digestsFor(t, env, "ana@example.com")[0])
	before := len(readConsentRecords(t, env, "ana@example.com"))
	unsubscribeWithEvidence(t, env, token)
	unsubscribeWithEvidence(t, env, token)

	if state := readConsentState(t, env, "ana@example.com"); state.MarketingConsent.String != "denied" || state.DigestEnabled {
		t.Fatalf("state marketing=%q digest_enabled=%v after two presses, want denied and false", state.MarketingConsent.String, state.DigestEnabled)
	}
	if got := len(readConsentRecords(t, env, "ana@example.com")); got != before+2 {
		t.Fatalf("consent records = %d, want %d — a repeated act is still an act", got, before+2)
	}
	if got := len(consentRecordsOn(t, env, "ana@example.com", "unsubscribe_link")); got != 2 {
		t.Fatalf("unsubscribe_link records = %d, want 2 — the log says what happened, not only what changed", got)
	}
}
