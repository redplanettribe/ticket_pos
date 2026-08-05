package integration

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Unsubscribing from the Follow Digest (#224, parent #215, ADR 0030).
//
// UNSUBSCRIBING IS A SWITCH, NOT A PURGE, and every test below exists to hold
// one half of that sentence. The Digest is the platform's first non-transactional
// mail, so it is the first that needs an opt-out reachable without signing in —
// and the moment an opt-out is reachable without signing in, a corporate mail
// scanner prefetching links in an inbox can press it. Had Unsubscribe meant
// "Unfollow everything", those scanners would have silently wiped Customers'
// lists. It flips one reversible flag, the link confirms with a POST so a
// prefetch alone does nothing, and the Follows stand untouched either way.
//
// Transactional mail is not on this switch and never will be: a One-time
// Passcode is how a person signs in and a Sale Confirmation is the receipt for
// something they just paid for. Silencing either because somebody wanted fewer
// weekly emails would be a discovery feature taking down authentication.

const (
	customerUnsubscribePath = "/api/v1/customer/unsubscribe"
	customerDigestPath      = "/api/v1/customer/digest"
)

// digestSubscriptionView is the Customer's Digest switch as the API reports it.
type digestSubscriptionView struct {
	DigestEnabled bool `json:"digest_enabled"`
}

// unsubscribeLinkPattern finds the signed link in a rendered Digest.
//
// The test reads the link out of the MESSAGE rather than minting one through a
// back door, because "every Digest carries an unsubscribe link" is the criterion
// and a link the test made for itself would prove nothing about the mail.
var unsubscribeLinkPattern = regexp.MustCompile(`http://storefront\.example/unsubscribe\?token=([A-Za-z0-9_.-]+)`)

// unsubscribeTokenFrom pulls the signed token out of a Digest a Customer
// actually received.
func unsubscribeTokenFrom(t *testing.T, digest capturedFollowDigest) string {
	t.Helper()
	match := unsubscribeLinkPattern.FindStringSubmatch(digest.Text)
	if match == nil {
		t.Fatalf("the Digest carries no unsubscribe link; body:\n%s", digest.Text)
	}
	return match[1]
}

// unsubscribeWithToken presses the confirmation the landing page presses.
//
// NO CREDENTIAL IS SENT, deliberately and as the contract: a Digest is read in a
// mail client months after anybody last signed in, and an opt-out that demanded
// a sign-in first would be no opt-out at all. The signed token is the whole
// authority, and it names one Customer and nothing else.
func unsubscribeWithToken(t *testing.T, env *testEnv, token string) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, customerUnsubscribePath, map[string]string{"token": token}, nil)
}

func unsubscribeWithTokenOK(t *testing.T, env *testEnv, token string) {
	t.Helper()
	resp, body := unsubscribeWithToken(t, env, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unsubscribe status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("unsubscribe error=%+v, want none", body.Error)
	}
}

// setDigestEnabled is the Customer Area's toggle: the other entry point, and the
// only one that requires a session, because it is reached from inside the Area.
func setDigestEnabled(t *testing.T, env *testEnv, token string, enabled bool) digestSubscriptionView {
	t.Helper()
	resp, body := env.put(t, customerDigestPath, map[string]bool{"enabled": enabled}, authHeader(token))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set digest enabled=%v status=%d error=%+v", enabled, resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("set digest enabled=%v error=%+v, want none", enabled, body.Error)
	}
	var view digestSubscriptionView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode digest subscription: %v", err)
	}
	return view
}

// rawGet issues a bare GET and returns the status without insisting on an
// envelope, which is what a mail scanner's prefetch looks like on the wire.
func rawGet(t *testing.T, env *testEnv, path string) int {
	t.Helper()
	resp, err := http.Get(env.server.URL + path)
	if err != nil {
		t.Fatalf("prefetch %s: %v", path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// digestEnabledInDatabase reads the switch itself.
//
// SQL because it is the one fact that has to be true whichever surface asked:
// the Follows listing publishes it, but a test that only read the listing could
// pass against a listing that computed it rather than a column that stores it.
func digestEnabledInDatabase(t *testing.T, env *testEnv, email string) bool {
	t.Helper()
	var enabled bool
	if err := env.db.QueryRow(`SELECT digest_enabled FROM customers WHERE email = $1`, email).Scan(&enabled); err != nil {
		t.Fatalf("read digest_enabled for %s: %v", email, err)
	}
	return enabled
}

// unsubscribedFollowingCustomer is the setup almost every test below shares: a
// Customer who Follows, receives one Digest, and unsubscribes from the link in
// it. The returned token is their Customer Session.
func unsubscribedFollowingCustomer(t *testing.T, env *testEnv, sessionID, email string) string {
	t.Helper()
	discoverableEvent(t, env, sessionID, "Week One Fest", "week-one-fest", env.fixedClock.Add(72*time.Hour))
	customerToken := followingCustomer(t, env, email)
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, email)
	if len(digests) != 1 {
		t.Fatalf("%s received %d Digests in the first week, want exactly 1", email, len(digests))
	}
	unsubscribeWithTokenOK(t, env, unsubscribeTokenFrom(t, digests[0]))
	return customerToken
}

// TestFollowDigestCarriesAnUnsubscribeLink is the first acceptance criterion and
// the one every other one depends on: there is no opt-out at all if the message
// does not carry it.
func TestFollowDigestCarriesAnUnsubscribeLink(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Footer Fest", "footer-fest", env.fixedClock.Add(72*time.Hour))
	followingCustomer(t, env, "ana@example.com")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	if token := unsubscribeTokenFrom(t, digests[0]); token == "" {
		t.Fatalf("the unsubscribe link carries no token; body:\n%s", digests[0].Text)
	}
}

// TestUnsubscribingSilencesTheDigest is the criterion itself: following the link
// and confirming stops future Digests.
//
// The second week publishes a NEW Event, so the silence is a decision about this
// reader rather than an artefact of there being nothing to say — which is the
// way this test would otherwise pass without the feature existing.
func TestUnsubscribingSilencesTheDigest(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	unsubscribedFollowingCustomer(t, env, sessionID, "ana@example.com")

	advanceDigestClock(t, env, 7*24*time.Hour)
	discoverableEvent(t, env, sessionID, "Week Two Fest", "week-two-fest", env.fixedClock.Add(21*24*time.Hour))
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	if got := len(digestsFor(t, env, "ana@example.com")); got != 1 {
		t.Fatalf("ana received %d Digests in total, want 1 — the second week must be silent", got)
	}
	if digestEnabledInDatabase(t, env, "ana@example.com") {
		t.Fatal("digest_enabled is still true after unsubscribing")
	}
}

// TestUnsubscribingLeavesEveryFollowStanding is the crux of ADR 0030 and the
// reason Unsubscribe is a separate act from Unfollow.
//
// A mail scanner prefetching a link must not be able to destroy somebody's list.
// The Follows are read back through the Customer's own Area, which is where the
// criterion says they must still be visible — a row surviving in a table nobody
// renders would not be the Customer keeping their list.
func TestUnsubscribingLeavesEveryFollowStanding(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	customerToken := unsubscribedFollowingCustomer(t, env, sessionID, "ana@example.com")
	followTagOK(t, env, customerToken, "music")
	// The Tag Follow is made after the first Digest and before nothing else; it
	// is here so the surviving list has both kinds in it.
	unsubscribeWithTokenOK(t, env, unsubscribeTokenFrom(t, digestsFor(t, env, "ana@example.com")[0]))

	view := listFollows(t, env, customerToken)
	if len(view.Follows) != 2 {
		t.Fatalf("ana holds %d Follows after unsubscribing, want 2 — unsubscribing is a switch, not a purge (%+v)", len(view.Follows), view.Follows)
	}
	if view.DigestEnabled {
		t.Fatal("the Follows listing reports the Digest as on after unsubscribing; the Customer Area could not tell them it is off")
	}
}

// TestPrefetchingTheUnsubscribeLinkDoesNotUnsubscribe is the acceptance
// criterion ADR 0030 was written around, and the whole reason confirmation is a
// POST.
//
// Mail security scanners open every link in every message before a human sees
// it. A GET that acted would mean a Customer at a company running one could
// never receive a second Digest, and nobody — not them, not us — would ever know
// why. So a GET must change nothing, and the Digest must still arrive next week.
func TestPrefetchingTheUnsubscribeLinkDoesNotUnsubscribe(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Scanner Fest", "scanner-fest", env.fixedClock.Add(72*time.Hour))
	followingCustomer(t, env, "ana@example.com")
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests in the first week, want exactly 1", len(digests))
	}
	token := unsubscribeTokenFrom(t, digests[0])

	// The scanner's prefetch: a bare GET of the address in the message.
	if status := rawGet(t, env, customerUnsubscribePath+"?token="+token); status != http.StatusMethodNotAllowed {
		t.Fatalf("GET of the unsubscribe endpoint status=%d, want 405 — a prefetch must never reach an act", status)
	}
	if !digestEnabledInDatabase(t, env, "ana@example.com") {
		t.Fatal("a bare GET unsubscribed the Customer; a mail scanner would silence everybody it protects")
	}

	// And the proof that matters to the reader: next week's Digest still arrives.
	advanceDigestClock(t, env, 7*24*time.Hour)
	discoverableEvent(t, env, sessionID, "Still Fest", "still-fest", env.fixedClock.Add(21*24*time.Hour))
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	after := digestsFor(t, env, "ana@example.com")
	if len(after) != 2 {
		t.Fatalf("ana received %d Digests across two weeks after a prefetch, want 2", len(after))
	}
}

// TestUnsubscribedCustomerIsNotEnqueued: the skip happens where the queue is
// built, not by composing a message and throwing it away.
//
// It is the cheaper place and the honest one — a row in the queue is a promise
// that somebody is owed a Digest, and an unsubscribed Customer is owed nothing.
func TestUnsubscribedCustomerIsNotEnqueued(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	unsubscribedFollowingCustomer(t, env, sessionID, "ana@example.com")

	advanceDigestClock(t, env, 7*24*time.Hour)
	discoverableEvent(t, env, sessionID, "Skipped Fest", "skipped-fest", env.fixedClock.Add(21*24*time.Hour))
	result := enqueueFollowDigests(t, env)

	if result.Eligible != 0 || result.Enqueued != 0 {
		t.Fatalf("enqueue eligible=%d enqueued=%d for an unsubscribed Customer, want 0 and 0 (%+v)", result.Eligible, result.Enqueued, result)
	}
	// One row, from the first week, and no second.
	if got := digestRowCount(t, env); got != 1 {
		t.Fatalf("follow_digests rows=%d, want 1 — the second week must not have been enqueued", got)
	}
}

// TestUnsubscribingAfterEnqueueIsSkippedByTheDrain closes the window the enqueue
// filter cannot: a Customer who unsubscribes between the weekly enqueue and the
// minute their Digest is drained.
//
// The queue row is already there, so the skip has to happen again at send time —
// before anything is composed, and certainly before anything is sent. The row is
// recorded `skipped` rather than `empty`, because "we decided not to write to
// this person" and "their Follows matched nothing" are different facts and only
// one of them is a reason to look at their Follows.
func TestUnsubscribingAfterEnqueueIsSkippedByTheDrain(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Window Fest", "window-fest", env.fixedClock.Add(72*time.Hour))
	customerToken := followingCustomer(t, env, "ana@example.com")

	enqueueFollowDigests(t, env)
	// Unsubscribed from the Customer Area rather than from a link, because there
	// is no Digest yet to carry one — which is exactly the window this covers.
	setDigestEnabled(t, env, customerToken, false)

	result := drainFollowDigests(t, env)
	if result.Claimed != 1 || result.Skipped != 1 || result.Sent != 0 {
		t.Fatalf("drain claimed=%d skipped=%d sent=%d, want 1, 1 and 0 (%+v)", result.Claimed, result.Skipped, result.Sent, result)
	}
	if got := len(digestsFor(t, env, "ana@example.com")); got != 0 {
		t.Fatalf("ana received %d Digests after unsubscribing, want 0", got)
	}
	if status, _ := digestRowStatus(t, env, "ana@example.com"); status != "skipped" {
		t.Fatalf("Digest status=%q for an unsubscribed Customer, want skipped", status)
	}
	// Nothing was shown to them, so nothing may be recorded as shown. A ledger
	// row here would make that Event invisible to them forever after they came
	// back.
	if ledger := digestLedgerEvents(t, env, "ana@example.com"); len(ledger) != 0 {
		t.Fatalf("sent-ledger holds %v for a Customer who was never written to, want nothing", ledger)
	}
}

// TestReEnablingTheDigestResumesIt is the "reversible" in ADR 0030: a Customer
// who wanted quiet can have the Digest back, from the Customer Area, and the
// Follows they kept are what it is about.
func TestReEnablingTheDigestResumesIt(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	customerToken := unsubscribedFollowingCustomer(t, env, sessionID, "ana@example.com")

	if view := setDigestEnabled(t, env, customerToken, true); !view.DigestEnabled {
		t.Fatalf("re-enabling reported digest_enabled=%v, want true", view.DigestEnabled)
	}
	if view := listFollows(t, env, customerToken); !view.DigestEnabled {
		t.Fatal("the Follows listing still reports the Digest as off after re-enabling")
	}

	advanceDigestClock(t, env, 7*24*time.Hour)
	discoverableEvent(t, env, sessionID, "Resumed Fest", "resumed-fest", env.fixedClock.Add(21*24*time.Hour))
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 2 {
		t.Fatalf("ana received %d Digests after re-enabling, want 2", len(digests))
	}
	if !strings.Contains(digests[1].Text, "Resumed Fest") {
		t.Fatalf("the resumed Digest does not carry the new Event; body:\n%s", digests[1].Text)
	}
}

// TestUnsubscribedCustomerStillGetsTransactionalMail is the line CONTEXT.md
// draws and the one most likely to be crossed by a well-meaning "respect the
// Customer's preferences" check added later in the wrong place.
//
// A One-time Passcode is how a person signs in. A Sale Confirmation is the
// receipt — and the Confirmation Link in it is how they get through the gate.
// Neither answers a standing subscription; both answer something the Customer
// did seconds ago. Unsubscribing from a weekly discovery email must not cost a
// person their tickets or their account.
func TestUnsubscribedCustomerStillGetsTransactionalMail(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	unsubscribedFollowingCustomer(t, env, sessionID, "ana@example.com")

	// The passcode: a fresh sign-in, from an address that has opted out.
	before := env.email.OTPSendCount()
	customerSignIn(t, env, "ana@example.com")
	if after := env.email.OTPSendCount(); after != before+1 {
		t.Fatalf("OTP emails sent = %d, want %d — an unsubscribed Customer must still be able to sign in", after, before+1)
	}

	// The receipt: a real purchase by the same address.
	_, freeID := publishCheckoutEvent(t, env, sessionID, "Receipt Fest", "receipt-fest", 0, 10)
	beginCheckoutSettled(t, env, testOrgSlug, "receipt-fest", "",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(freeID, 1)))

	var confirmations int
	for _, confirmation := range env.email.Confirmations() {
		if confirmation.To == "ana@example.com" {
			confirmations++
		}
	}
	if confirmations != 1 {
		t.Fatalf("Sale Confirmations to an unsubscribed Customer = %d, want 1", confirmations)
	}
}

// TestUnsubscribeRefusesAForgedToken: the signature is checked before anything
// in the token is believed, so a made-up or edited one reaches no write.
//
// Without it the endpoint would be an unauthenticated way to silence any
// Customer whose id somebody could guess.
func TestUnsubscribeRefusesAForgedToken(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Forged Fest", "forged-fest", env.fixedClock.Add(72*time.Hour))
	followingCustomer(t, env, "ana@example.com")
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	genuine := unsubscribeTokenFrom(t, digestsFor(t, env, "ana@example.com")[0])
	// The payload kept, the signature replaced: the shape of every forgery worth
	// testing, and the one a stored-token scheme would have caught by accident.
	forged := strings.SplitN(genuine, ".", 2)[0] + ".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	resp, body := unsubscribeWithToken(t, env, forged)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("forged token status=%d, want 400", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "UNSUBSCRIBE_LINK_INVALID" {
		t.Fatalf("forged token error=%+v, want UNSUBSCRIBE_LINK_INVALID", body.Error)
	}
	if !digestEnabledInDatabase(t, env, "ana@example.com") {
		t.Fatal("a forged token unsubscribed the Customer")
	}
}

// TestUnsubscribingTwiceIsNotAnError: the same link is in every Digest a person
// has ever received, and a second press is the same request arriving twice with
// the same meaning.
func TestUnsubscribingTwiceIsNotAnError(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Twice Fest", "twice-fest", env.fixedClock.Add(72*time.Hour))
	followingCustomer(t, env, "ana@example.com")
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	token := unsubscribeTokenFrom(t, digestsFor(t, env, "ana@example.com")[0])
	unsubscribeWithTokenOK(t, env, token)
	unsubscribeWithTokenOK(t, env, token)

	if digestEnabledInDatabase(t, env, "ana@example.com") {
		t.Fatal("the second unsubscribe turned the Digest back on")
	}
}
