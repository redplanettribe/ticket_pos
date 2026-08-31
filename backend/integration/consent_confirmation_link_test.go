package integration

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The confirmation link that resolves a Pending Confirmation (#255, parent
// #249, ADR 0035): the double opt-in's second half.
//
// A guest ticked an optional box for an address nobody had proven, so it
// pended — recorded, denied for sending, unanswered for prompting, never
// expiring. These tests are the whole journey out of that state: the line
// appearing in the receipt of the sale that pended something and in no other,
// the press granting it with fresh evidence, and the three ways a press must
// NOT work — twice, against an answer the owner has since given, and from a
// token minted for something else.
//
// NOTHING CREATES A PENDING CONFIRMATION ANY MORE (ADR 0054, #386): the guest
// checkout was its only producer and that route is deleted. This file is
// therefore about a BACKLOG rather than about a flow — every link it exercises
// was issued before the door closed, and every one of them must still resolve,
// because the alternative to leaving a Pending Confirmation alone is either
// manufacturing consent or destroying evidence. Its setup ages a Payment into
// the shape the deleted route wrote (beginLegacyGuestCheckout) and settles it
// through the return leg that is still live, so what is under test is today's
// code meeting yesterday's rows.
//
// The seam is HTTP and the mail, as everywhere in this package. The token is
// read out of the MESSAGE a Customer actually received rather than minted
// through a back door, because "the receipt carries the link" is the criterion
// and a token the test made for itself would prove nothing about the mail.

const customerConsentConfirmPath = "/api/v1/customer/consent/confirm"

// consentConfirmationView is what a press did, as the API reports it.
type consentConfirmationView struct {
	MarketingConsent  bool `json:"marketing_consent"`
	NetworkingConsent bool `json:"networking_consent"`
	AlreadyResolved   bool `json:"already_resolved"`
	DigestEnabled     bool `json:"digest_enabled"`
}

var consentConfirmationLinkPattern = regexp.MustCompile(`http://storefront\.example/confirm-consent\?token=([A-Za-z0-9_.-]+)`)

// saleConfirmationFor returns the receipts one address actually received.
func saleConfirmationsFor(t *testing.T, env *testEnv, email string) []platform.SaleConfirmation {
	t.Helper()
	var mine []platform.SaleConfirmation
	for _, confirmation := range env.email.Confirmations() {
		if confirmation.To == email {
			mine = append(mine, confirmation)
		}
	}
	return mine
}

// consentTokenFrom pulls the signed token out of a Sale Confirmation, failing
// when the receipt carries no confirmation line at all.
func consentTokenFrom(t *testing.T, confirmation platform.SaleConfirmation) string {
	t.Helper()
	match := consentConfirmationLinkPattern.FindStringSubmatch(confirmation.Text())
	if match == nil {
		t.Fatalf("the Sale Confirmation carries no consent confirmation link; body:\n%s", confirmation.Text())
	}
	return match[1]
}

// hasConsentLink reports whether a receipt offers to confirm anything, for the
// assertions whose point is that it does not.
func hasConsentLink(confirmation platform.SaleConfirmation) bool {
	return consentConfirmationLinkPattern.MatchString(confirmation.Text())
}

// confirmConsentWithToken presses the confirmation the landing page presses.
//
// NO CREDENTIAL IS SENT, deliberately and as the contract: a guest checkout
// creates a Customer nobody has ever signed in as, so the owner of an address
// somebody else typed may have no account at all. The signed token is the whole
// authority. The evidence headers are the ones the BFF relays, exactly as for
// every other capture surface.
func confirmConsentWithToken(t *testing.T, env *testEnv, token string) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, customerConsentConfirmPath, map[string]string{"token": token}, map[string]string{
		"X-BFF-Client-IP": "203.0.113.9",
		"User-Agent":      "Mozilla/5.0 (confirm-consent-test)",
		"Referer":         "http://storefront.example/es/confirm-consent",
	})
}

func confirmConsentOK(t *testing.T, env *testEnv, token string) consentConfirmationView {
	t.Helper()
	resp, body := confirmConsentWithToken(t, env, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm consent status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("confirm consent error=%+v, want none", body.Error)
	}
	var view consentConfirmationView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode consent confirmation: %v", err)
	}
	return view
}

// guestCheckoutPending settles one guest checkout with the optional boxes as
// given, and returns the token out of the receipt it produced. It is the setup
// almost every test below shares: somebody typed an address they had not
// proven, ticked something, and paid.
//
// SUCH A CHECKOUT CAN NO LONGER BE BEGUN (ADR 0054, #386), which is why this
// goes through beginLegacyGuestCheckout: the Payment is aged into the shape the
// deleted route wrote, and then settled by the return leg exactly as it stands
// today. That is not a workaround, it is the situation — these links were issued
// before the door closed and have to keep resolving after it.
//
// The optional answers are POINTERS, because the three cases are three
// different facts: ticked, shown and left unticked (an explicit No, which
// occupies the state and cannot later be pended over), and not shown at all.
func guestCheckoutPending(t *testing.T, env *testEnv, sessionID, email, eventSlug string, marketing, networking *bool) string {
	t.Helper()
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Pending Fest", eventSlug, 1000, 10)
	begin := beginLegacyGuestCheckout(t, env, eventSlug, email, "Ana", "Lopez",
		boolPtr(true), marketing, networking, cartLine(gaID, 1))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	confirmations := saleConfirmationsFor(t, env, email)
	if len(confirmations) != 1 {
		t.Fatalf("%s received %d Sale Confirmations, want exactly 1", email, len(confirmations))
	}
	return consentTokenFrom(t, confirmations[0])
}

// TestSaleConfirmationCarriesTheConfirmationLineOnlyWhenSomethingPends is the
// first acceptance criterion, and the half most easily lost: receipts for sales
// that left nothing pending are UNCHANGED.
//
// Both directions in one test, because the property is a difference — a test of
// either side alone would pass against a line that was always there or never
// was.
func TestSaleConfirmationCarriesTheConfirmationLineOnlyWhenSomethingPends(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// A guest who ticked nothing: an explicit No, nothing pends, nothing to
	// confirm.
	_, quietID := publishCheckoutEvent(t, env, sessionID, "Quiet Fest", "quiet-fest", 1000, 10)
	quiet := beginLegacyGuestCheckout(t, env, "quiet-fest", "quiet@example.com", "Quiet", "Buyer",
		boolPtr(true), boolPtr(false), boolPtr(false), cartLine(quietID, 1))
	confirmCheckoutOK(t, env, quiet.ClientTransactionID, "approved")

	quietReceipts := saleConfirmationsFor(t, env, "quiet@example.com")
	if len(quietReceipts) != 1 {
		t.Fatalf("quiet buyer received %d receipts, want 1", len(quietReceipts))
	}
	if hasConsentLink(quietReceipts[0]) {
		t.Fatalf("a receipt that pended nothing offers a confirmation link; body:\n%s", quietReceipts[0].Text())
	}

	// And a guest who ticked: their receipt carries the line.
	if token := guestCheckoutPending(t, env, sessionID, "ana@example.com", "pending-fest", boolPtr(true), boolPtr(false)); token == "" {
		t.Fatal("the confirmation link carries no token")
	}
}

// TestConfirmingGrantsThePendingWithFreshEvidence is the criterion itself, and
// every clause of it is asserted because each is a separate promise: the state
// flips, a NEW Consent Record is written on its own channel with the press
// recorded as proven, `confirmed_at` stamps the tick it resolves, and nothing
// the press was not about is touched.
func TestConfirmingGrantsThePendingWithFreshEvidence(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	// Marketing ticked, Networking left unticked. The guest's silence answered
	// nothing — an unproven No writes no state, because it is not this person's
	// answer to give — so Networking stays unanswered, and this press must leave
	// it that way.
	token := guestCheckoutPending(t, env, sessionID, "ana@example.com", "pending-fest", boolPtr(true), boolPtr(false))

	before := readConsentState(t, env, "ana@example.com")
	if before.MarketingConsent.String != "pending_confirmation" {
		t.Fatalf("marketing_consent = %q before the press, want pending_confirmation", before.MarketingConsent.String)
	}

	view := confirmConsentOK(t, env, token)
	if !view.MarketingConsent {
		t.Fatalf("press reported marketing_consent=%v, want the box it confirmed (%+v)", view.MarketingConsent, view)
	}
	if view.AlreadyResolved {
		t.Fatalf("press reported already_resolved on a pending it actually confirmed (%+v)", view)
	}

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "granted" {
		t.Fatalf("marketing_consent = %q after the press, want granted", state.MarketingConsent.String)
	}
	// The other box was never in scope and is not touched: reading its absence
	// from this act as an answer would turn a confirmation into a withdrawal.
	if state.NetworkingConsent.Valid {
		t.Fatalf("networking_consent = %q, want the unanswered state the guest's silence left", state.NetworkingConsent.String)
	}
	// One switch (ADR 0034): a granted Marketing Consent IS the Follow Digest on.
	if !state.DigestEnabled {
		t.Fatal("digest_enabled = false after a granted Marketing Consent; the two are one switch")
	}
	if !view.DigestEnabled {
		t.Fatal("the press reported the Digest as off; the page could not tell the person what they will receive")
	}

	// A FRESH record, on its own channel, rather than an edit of the tick.
	presses := consentRecordsOn(t, env, "ana@example.com", "email_confirmation")
	if len(presses) != 1 {
		t.Fatalf("email_confirmation records = %d, want exactly one for one press", len(presses))
	}
	press := presses[0]
	// PROVEN. Clicking a link that only ever travelled to this address is proof
	// of control of it (ADR 0035) — and it is what makes the answer `granted`
	// rather than another pending.
	if !press.EmailProven {
		t.Fatal("email_proven = false on a press from the inbox; that proof is the whole of the double opt-in")
	}
	if !press.MarketingConsent.Valid || !press.MarketingConsent.Bool {
		t.Fatalf("recorded marketing_consent = %+v, want the confirmation", press.MarketingConsent)
	}
	// Only the confirmed box is answered. The other two are NULL — not shown on
	// this surface — so this press neither re-accepts a Policy Version nor
	// re-answers Networking Consent.
	if press.PolicyAcceptance.Valid {
		t.Fatalf("recorded policy_acceptance = %+v, want null: a press is not a re-acceptance", press.PolicyAcceptance)
	}
	if press.NetworkingConsent.Valid {
		t.Fatalf("recorded networking_consent = %+v, want null: that box was not on this surface", press.NetworkingConsent)
	}
	// The Customer's OWN stored address, because that is the only inbox this
	// link could have reached.
	if press.Email != "ana@example.com" {
		t.Fatalf("record email = %q, want the address the link was mailed to", press.Email)
	}
	if !press.IP.Valid || press.IP.String != "203.0.113.9" {
		t.Fatalf("recorded ip = %+v, want the address the BFF derived for the press", press.IP)
	}
	if !press.UserAgent.Valid || press.UserAgent.String != "Mozilla/5.0 (confirm-consent-test)" {
		t.Fatalf("recorded user agent = %+v", press.UserAgent)
	}
	if press.SessionID.Valid {
		t.Fatalf("recorded session id = %+v, want none: this route is session-less by contract", press.SessionID)
	}

	// And the tick it resolved is stamped, which is the one write migration 061
	// reserved on this table: the row still says exactly what happened, plus the
	// fact that it was later corroborated.
	checkouts := consentRecordsOn(t, env, "ana@example.com", "checkout")
	if len(checkouts) != 1 {
		t.Fatalf("checkout records = %d, want one", len(checkouts))
	}
	if !checkouts[0].ConfirmedAt.Valid {
		t.Fatal("confirmed_at is null on the tick this press confirmed")
	}
	if checkouts[0].EmailProven {
		t.Fatal("the checkout record was rewritten as proven; a stamp must not alter what the row says happened")
	}
}

// TestConfirmingTwiceIsHarmless: the same link is in the same email forever, and
// a second press is one request arriving twice with the same meaning.
//
// It must not be an error — telling somebody their own confirmation failed when
// it had already worked is the worst possible answer — and it must not write a
// second Consent Record, because there was no answer to record: no box was
// shown, nothing was ticked, and a row with three NULL answers means "not
// shown" in this schema, not "pressed again".
func TestConfirmingTwiceIsHarmless(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	token := guestCheckoutPending(t, env, sessionID, "ana@example.com", "pending-fest", boolPtr(true), boolPtr(true))

	first := confirmConsentOK(t, env, token)
	if !first.MarketingConsent || !first.NetworkingConsent {
		t.Fatalf("first press confirmed %+v, want both boxes", first)
	}

	second := confirmConsentOK(t, env, token)
	if !second.AlreadyResolved {
		t.Fatalf("second press reported %+v, want already_resolved", second)
	}
	if second.MarketingConsent || second.NetworkingConsent {
		t.Fatalf("second press claimed to confirm %+v, want nothing: it changed nothing", second)
	}

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "granted" || state.NetworkingConsent.String != "granted" {
		t.Fatalf("states after two presses = %q/%q, want both granted", state.MarketingConsent.String, state.NetworkingConsent.String)
	}
	if got := len(consentRecordsOn(t, env, "ana@example.com", "email_confirmation")); got != 1 {
		t.Fatalf("email_confirmation records after two presses = %d, want 1 — a press that confirmed nothing has no answer to record", got)
	}
}

// TestASupersededPendingIsNotResurrected is the staleness rule, and the reason
// the token may safely never expire.
//
// The link is pressed a long time after it was sent, by which point the owner
// has answered for themselves — here by switching the Digest off from their own
// Customer Area, which denies Marketing Consent under a proven session (#256,
// ADR 0034). A PROVEN LATER ANSWER OUTRANKS AN OLDER PENDING: the press finds
// nothing pending, confirms nothing, and their No stands.
func TestASupersededPendingIsNotResurrected(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// The owner has signed in before — settling the Terms (#536), so the
	// sign-in below is not stopped at a step that would answer the optional
	// boxes on her behalf — and her optional consents are put back to
	// never-answered so the guest's ticks below have something to pend over.
	customerSignIn(t, env, "ana@example.com")
	resetOptionalConsentsToUnanswered(t, env, "ana@example.com")

	token := guestCheckoutPending(t, env, sessionID, "ana@example.com", "pending-fest", boolPtr(true), boolPtr(true))

	// The owner turns up, proves the address, and answers for themselves. Their
	// sign-in is not gated: the checkout already recorded an acceptance of the
	// current Policy Version — a fact about the sale rather than a claim on the
	// inbox (ADR 0035) — and their Terms were settled at the earlier sign-in.
	customerToken := customerSignIn(t, env, "ana@example.com")
	setDigestEnabled(t, env, customerToken, false)

	if state := readConsentState(t, env, "ana@example.com"); state.MarketingConsent.String != "denied" {
		t.Fatalf("marketing_consent = %q after the owner declined, want denied", state.MarketingConsent.String)
	}

	view := confirmConsentOK(t, env, token)
	if view.MarketingConsent {
		t.Fatalf("the stale press resurrected a declined Marketing Consent (%+v)", view)
	}
	// The networking pending was never answered by the owner, so the same press
	// legitimately resolves it. The rule is per box, because the owner's answers
	// are per box.
	if !view.NetworkingConsent {
		t.Fatalf("the press left an untouched Networking pending unresolved (%+v)", view)
	}

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "denied" {
		t.Fatalf("marketing_consent = %q after the stale press, want the owner's denied to stand", state.MarketingConsent.String)
	}
	if state.DigestEnabled {
		t.Fatal("the stale press turned the Follow Digest back on for somebody who had switched it off")
	}
	if state.NetworkingConsent.String != "granted" {
		t.Fatalf("networking_consent = %q, want granted", state.NetworkingConsent.String)
	}
}

// TestAPressConfirmsOnlyWhatItsLinkWasMintedFor: the scope is baked into the
// signed token, so a link cannot pick up a Pending Confirmation created after
// the mail that carried it was written.
//
// The person read a line about the boxes that pended THEN. A later guest
// checkout by a stranger, pending a different box, was never on the page they
// read, and a press that swept it up would have them confirming something they
// were never shown.
func TestAPressConfirmsOnlyWhatItsLinkWasMintedFor(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	// First checkout: the Marketing box ticked and the Networking box NOT SHOWN,
	// so nothing at all is recorded for it. The token is minted for the one box
	// that pended.
	token := guestCheckoutPending(t, env, sessionID, "ana@example.com", "pending-fest", boolPtr(true), nil)

	// A later checkout on the same address ticks Networking, which pends because
	// its owner has still never answered it.
	_, secondID := publishCheckoutEvent(t, env, sessionID, "Later Fest", "later-fest", 1000, 10)
	later := beginLegacyGuestCheckout(t, env, "later-fest", "ana@example.com", "Ana", "Lopez",
		boolPtr(true), boolPtr(true), boolPtr(true), cartLine(secondID, 1))
	confirmCheckoutOK(t, env, later.ClientTransactionID, "approved")

	if state := readConsentState(t, env, "ana@example.com"); state.NetworkingConsent.String != "pending_confirmation" {
		t.Fatalf("networking_consent = %q after the later checkout, want pending_confirmation", state.NetworkingConsent.String)
	}

	view := confirmConsentOK(t, env, token)
	if !view.MarketingConsent {
		t.Fatalf("the press did not confirm the box its link named (%+v)", view)
	}
	if view.NetworkingConsent {
		t.Fatalf("the press confirmed a box its link was never minted for (%+v)", view)
	}
	if state := readConsentState(t, env, "ana@example.com"); state.NetworkingConsent.String != "pending_confirmation" {
		t.Fatalf("networking_consent = %q, want it still pending: nobody has confirmed it", state.NetworkingConsent.String)
	}
}

// TestConfirmRefusesAForgedOrMisusedToken.
//
// The signature is checked before anything in the payload is believed, so an
// edited, truncated or invented token reaches no write — without which this
// endpoint would be an unauthenticated way to grant a pending consent for any
// Customer whose id somebody could guess.
//
// AND A GENUINE TOKEN MINTED FOR ANOTHER PURPOSE IS REFUSED, which is the case
// the domain separator exists for. An unsubscribe token is a valid HMAC under
// the same key over a payload that means the OPPOSITE thing; only the purpose
// comparison stops "silence my mail" being spendable as "subscribe me".
func TestConfirmRefusesAForgedOrMisusedToken(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	genuine := guestCheckoutPending(t, env, sessionID, "ana@example.com", "pending-fest", boolPtr(true), boolPtr(false))

	// A genuine unsubscribe token, minted by the platform itself under the same
	// signing key, read out of a real Digest. It belongs to a DIFFERENT Customer
	// — ana's own marketing is pending, so no Digest is written to her, which is
	// itself the rule working — and that makes the case sharper: this is a valid
	// signature over a payload naming a person, presented where a payload naming
	// a person is expected.
	discoverableEvent(t, env, sessionID, "Digest Fest", "digest-fest", env.fixedClock.Add(72*time.Hour))
	followingCustomer(t, env, "bea@example.com")
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)
	digests := digestsFor(t, env, "bea@example.com")
	if len(digests) != 1 {
		t.Fatalf("bea received %d Digests, want 1 to read an unsubscribe token out of", len(digests))
	}
	unsubscribeToken := unsubscribeTokenFrom(t, digests[0])

	cases := []struct {
		name  string
		token string
	}{
		// The payload kept, the signature replaced: the shape of every forgery
		// worth testing.
		{name: "forged signature", token: strings.SplitN(genuine, ".", 2)[0] + ".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{name: "not a token at all", token: "nonsense"},
		{name: "an unsubscribe token, genuinely signed, for another purpose", token: unsubscribeToken},
	}
	for _, tc := range cases {
		resp, body := confirmConsentWithToken(t, env, tc.token)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status=%d, want 400", tc.name, resp.StatusCode)
		}
		if body.Error == nil || body.Error.Code != "CONSENT_CONFIRMATION_LINK_INVALID" {
			t.Fatalf("%s: error=%+v, want CONSENT_CONFIRMATION_LINK_INVALID", tc.name, body.Error)
		}
	}

	// Nothing any of them reached. The sign-in above was not stopped for consent
	// — the checkout already accepted the current Policy Version — so the tick is
	// exactly as pending as it was, and every refusal above left no trace.
	if got := len(consentRecordsOn(t, env, "ana@example.com", "email_confirmation")); got != 0 {
		t.Fatalf("email_confirmation records after refused tokens = %d, want 0", got)
	}
}

// TestPrefetchingTheConfirmationLinkDoesNotConfirm is the reason confirmation is
// a POST, and here the hazard is sharper than it is for the unsubscribe link.
//
// Mail security scanners open every link in every message before a human sees
// it. A GET that acted would let one GRANT a marketing opt-in nobody ever
// confirmed — leaving the platform holding an evidence row that says an inbox
// confirmed itself, when what confirmed it was a robot. So a GET must change
// nothing, and the pending must still be pending afterwards.
func TestPrefetchingTheConfirmationLinkDoesNotConfirm(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	token := guestCheckoutPending(t, env, sessionID, "ana@example.com", "pending-fest", boolPtr(true), boolPtr(false))

	if status := rawGet(t, env, customerConsentConfirmPath+"?token="+token); status != http.StatusMethodNotAllowed {
		t.Fatalf("GET of the confirmation endpoint status=%d, want 405 — a prefetch must never reach an act", status)
	}

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "pending_confirmation" {
		t.Fatalf("marketing_consent = %q after a bare GET, want it untouched", state.MarketingConsent.String)
	}
	if got := len(consentRecordsOn(t, env, "ana@example.com", "email_confirmation")); got != 0 {
		t.Fatalf("email_confirmation records after a prefetch = %d, want 0", got)
	}

	// And the press still works afterwards: a prefetch must not spend the link
	// either.
	if view := confirmConsentOK(t, env, token); !view.MarketingConsent {
		t.Fatalf("the press after a prefetch confirmed nothing (%+v)", view)
	}
}

// TestConfirmingReleasesTheFollowDigest is the consequence the whole feature is
// for, observed where a person would observe it: in their inbox.
//
// A Pending Confirmation sends NOTHING — the sender's rule is "granted, or
// unanswered with the legacy flag on; never denied or pending" (ADR 0034) — so a
// Customer who Follows busily and has an unconfirmed tick gets no Digest at all.
// Pressing the link is what starts it.
func TestConfirmingReleasesTheFollowDigest(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	// The owner's Terms are settled at a sign-in of her own first (#536), and
	// her optional consents and Digest flag put back to never-answered/off —
	// the state of an account whose owner has not signed in since the boxes
	// existed (migration 065 defaults new rows off) — so the sign-in below is
	// not stopped at a step that would answer the marketing box for her.
	customerSignIn(t, env, "ana@example.com")
	resetOptionalConsentsToUnanswered(t, env, "ana@example.com")
	resetDigestFlagOff(t, env, "ana@example.com")

	// The pending comes before her next sign-in, which — owing nothing — asks
	// nothing on this person's behalf.
	token := guestCheckoutPending(t, env, sessionID, "ana@example.com", "pending-fest", boolPtr(true), boolPtr(false))

	customerToken := customerSignIn(t, env, "ana@example.com")
	followOrganizationOK(t, env, customerToken, testOrgSlug)
	discoverableEvent(t, env, sessionID, "Week One Fest", "week-one-fest", env.fixedClock.Add(72*time.Hour))

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)
	if got := len(digestsFor(t, env, "ana@example.com")); got != 0 {
		t.Fatalf("a Customer with an unconfirmed tick received %d Digests, want 0 — pending sends nothing", got)
	}

	confirmConsentOK(t, env, token)

	advanceDigestClock(t, env, 7*24*time.Hour)
	discoverableEvent(t, env, sessionID, "Week Two Fest", "week-two-fest", env.fixedClock.Add(21*24*time.Hour))
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	if got := len(digestsFor(t, env, "ana@example.com")); got != 1 {
		t.Fatalf("Digests after confirming = %d, want 1 — confirming is what makes the mail lawful", got)
	}
}
