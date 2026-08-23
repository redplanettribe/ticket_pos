package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The session-gated begin-checkout (#384, ADR 0054): an online checkout that
// cannot be asked to address a Ticket Sale to an unproven inbox.
//
// It is the expand half of an expand–contract. The public begin-checkout is
// untouched and still serves the Storefront (checkout_test.go and neighbours
// assert it); this file is about the new route, which requires a Customer
// Session and takes NO customer_email — the address is read from the session and
// there is no field by which a different one can be named.
//
// The four properties every test here is a reading of:
//
//   - No Customer Session, no checkout.
//   - A Confirmation Link session is not Proof of Email Ownership, and cannot buy.
//   - The Sale is addressed to the address the session proved, whatever the body says.
//   - Everything downstream of "the email is proven" follows: the profile write-back,
//     and consent recorded as answered rather than as a Pending Confirmation.

// signedInCheckoutPath is the session-gated begin-checkout's address. It names
// the Event by the same two Storefront slugs the public route does, and lives
// under /customer because a Customer Session is the price of entry.
func signedInCheckoutPath(orgSlug, eventSlug string) string {
	return "/api/v1/customer/organizations/" + orgSlug + "/events/" + eventSlug + "/checkout"
}

// signedInCheckoutBody is a begin-checkout body for the session-gated route:
// checkoutBody with the address taken out, because on this route there is no
// such field.
func signedInCheckoutBody(firstName, lastName string, lines ...map[string]any) map[string]any {
	body := checkoutBody("unused@example.com", firstName, lastName, lines...)
	delete(body, "customer_email")
	return body
}

// beginSignedInCheckout posts to the session-gated route, carrying the same
// circumstances a real request arrives with — the client IP as the BFF derived
// it, the browser's user agent, and the page the dialog was open on. The token
// is empty for a request that presents no session at all.
func beginSignedInCheckout(t *testing.T, env *testEnv, orgSlug, eventSlug, token string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	headers := map[string]string{
		"X-BFF-Client-IP": "198.51.100.24",
		"User-Agent":      "Mozilla/5.0 (signed-in-checkout-test)",
		"Referer":         "http://storefront.example/es/test-org/events/session-fest",
	}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	return env.post(t, signedInCheckoutPath(orgSlug, eventSlug), body, headers)
}

// signedInCheckoutResult is the begin-checkout result as this route reports it:
// the ordinary one, plus the address the Ticket Sale was addressed to (#387).
//
// The address is echoed because the Storefront's return leg has no other
// authoritative source for it — the buyer is off at the Payment Provider when
// their session may end — and because the only alternative would be trusting a
// browser to say who it was buying as, which is the field ADR 0054 deleted.
type signedInCheckoutResult struct {
	freeCheckoutResult
	AddressedTo string `json:"addressed_to"`
}

func beginSignedInCheckoutOK(t *testing.T, env *testEnv, orgSlug, eventSlug, token string, body map[string]any) signedInCheckoutResult {
	t.Helper()
	resp, envBody := beginSignedInCheckout(t, env, orgSlug, eventSlug, token, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("begin signed-in checkout status=%d error=%+v, want 201", resp.StatusCode, envBody.Error)
	}
	var result signedInCheckoutResult
	if err := json.Unmarshal(envBody.Data, &result); err != nil {
		t.Fatalf("decode begin checkout result: %v", err)
	}
	return result
}

// paymentSessionAuthorized reads the flag the Payment snapshots at begin and the
// commit restores onto the buyer: platform.SaleCustomer.SelfAsserted, the whole
// of "we checked whether this checkout is the buyer speaking about themselves".
// SQL because no API publishes it, and it is exactly what this ticket claims is
// computed rather than defaulted.
func paymentSessionAuthorized(t *testing.T, env *testEnv, clientTransactionID string) bool {
	t.Helper()
	var authorized bool
	if err := env.db.QueryRow(`
		SELECT customer_session_authorized FROM payments WHERE client_transaction_id = $1
	`, clientTransactionID).Scan(&authorized); err != nil {
		t.Fatalf("read payment session flag %q: %v", clientTransactionID, err)
	}
	return authorized
}

// countPayments counts every Payment row, for the assertions whose whole point
// is that a refused checkout left nothing behind.
func countPayments(t *testing.T, env *testEnv) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&n); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	return n
}

// TestSignedInCheckoutRefusesAnAnonymousRequest: the wall, stated by the API
// rather than by the dialog. A caller that is not the Storefront gets the same
// refusal, which is the point of putting it here — the BFF is a hop, not a
// boundary (ADR 0008).
func TestSignedInCheckoutRefusesAnAnonymousRequest(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Session Fest", "session-fest", 1000, 10)

	resp, body := beginSignedInCheckout(t, env, "test-org", "session-fest", "",
		signedInCheckoutBody("Ana", "Lopez", cartLine(gaID, 1)))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d error=%+v, want 401 for a checkout with no Customer Session", resp.StatusCode, body.Error)
	}
	if got := countPayments(t, env); got != 0 {
		t.Fatalf("payments = %d, want none: a refused checkout creates nothing", got)
	}
}

// TestSignedInCheckoutRefusesAConfirmationLinkSession: a Confirmation Link is
// possession of an email that was sent to somebody, and it may have been
// forwarded. It is not Proof of Email Ownership, so it cannot buy — the same
// refusal, with the same code, that guards reversal and the profile edit.
func TestSignedInCheckoutRefusesAConfirmationLinkSession(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Session Fest", "session-fest", 1000, 10)

	// A sale made the old way, so there is a Sale Confirmation with a link in it.
	begin := beginCheckoutOK(t, env, "test-org", "session-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	_, linkToken := redeemConfirmationLinkOK(t, env, lastConfirmationLinkToken(t, env), "")

	resp, body := beginSignedInCheckout(t, env, "test-org", "session-fest", linkToken,
		signedInCheckoutBody("Ana", "Lopez", cartLine(gaID, 1)))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d error=%+v, want 403 for a Confirmation Link session", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "CUSTOMER_SESSION_SCOPE_INSUFFICIENT" {
		t.Fatalf("error=%+v, want CUSTOMER_SESSION_SCOPE_INSUFFICIENT", body.Error)
	}
	if got := countPayments(t, env); got != 1 {
		t.Fatalf("payments = %d, want only the one the guest sale left", got)
	}
}

// TestSignedInCheckoutAddressesTheSaleToTheSession is the whole decision in one
// test: the Sale goes to the address the session proved, the body carries no
// field that could name another, and a body that smuggles one anyway changes
// nothing.
func TestSignedInCheckoutAddressesTheSaleToTheSession(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Session Fest", "session-fest", 1000, 10)

	token := customerSignIn(t, env, "ana@example.com")

	body := signedInCheckoutBody("Ana", "Lopez", cartLine(gaID, 1))
	// The field this route does not have. A stale client, or a crafted request,
	// still cannot address somebody else's inbox: there is nothing here that reads
	// it.
	body["customer_email"] = "stranger@example.com"
	body["customer_phone"] = ecuadorMobile

	begin := beginSignedInCheckoutOK(t, env, "test-org", "session-fest", token, body)
	if begin.Status != "pending" || begin.RedirectURL == nil {
		t.Fatalf("begin = %+v, want a pending provider checkout", begin)
	}
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	sales := eventSales(t, env, sessionID, eventID)
	if len(sales) != 1 {
		t.Fatalf("sales = %d, want one", len(sales))
	}
	if sales[0].CustomerEmail != "ana@example.com" {
		t.Fatalf("sale addressed to %q, want the address the session proved", sales[0].CustomerEmail)
	}

	// The buyer's own details, written back onto the Customer: every online
	// checkout is now the buyer speaking about themselves, so the Tax ID and the
	// phone land on the person as well as on the sale.
	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.is("cedula", validCedula) {
		t.Fatalf("customer tax id = %s, want the cedula she typed at checkout", got)
	}
	if got := readCustomerPhone(t, env, "ana@example.com"); got == nil || *got != ecuadorMobile {
		t.Fatalf("customer phone = %v, want %q written back", got, ecuadorMobile)
	}
	var first, last string
	if err := env.db.QueryRow(`SELECT first_name, last_name FROM customers WHERE email = $1`,
		"ana@example.com").Scan(&first, &last); err != nil {
		t.Fatalf("read customer name: %v", err)
	}
	if first != "Ana" || last != "Lopez" {
		t.Fatalf("customer name = %q %q, want the name she gave at checkout", first, last)
	}

	// Nobody was created for the address the body named.
	var strangers int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM customers WHERE email = $1`,
		"stranger@example.com").Scan(&strangers); err != nil {
		t.Fatalf("count strangers: %v", err)
	}
	if strangers != 0 {
		t.Fatal("a customer_email in the body minted a Customer; this route has no such field")
	}
}

// TestSignedInCheckoutReportsTheAddressItWasAddressedTo: the return leg's half
// of ADR 0054 (#387).
//
// A buyer who has begun a checkout is about to leave this origin for the Payment
// Provider, and their Customer Session may not survive the trip — a cleared jar,
// a provider webview that drops cookies, a revoked session, a return in another
// browser. The person who comes back has already paid, and the address they
// bought under is the one thing that turns the terminal page from a dead end into
// a door.
//
// So the response states it, and states it FROM THE SESSION: the body's smuggled
// address changes the answer no more here than it changes the sale. Anything the
// Storefront wrote down instead would be a browser reporting who it was buying
// as, which is the field this whole ADR deleted, pointed the other way.
func TestSignedInCheckoutReportsTheAddressItWasAddressedTo(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Session Fest", "session-fest", 1000, 10)
	_, freeID := publishCheckoutEvent(t, env, sessionID, "Free Fest", "free-fest", 0, 10)

	token := customerSignIn(t, env, "ana@example.com")

	body := signedInCheckoutBody("Ana", "Lopez", cartLine(gaID, 1))
	body["customer_email"] = "stranger@example.com"

	begin := beginSignedInCheckoutOK(t, env, "test-org", "session-fest", token, body)
	if begin.Status != "pending" {
		t.Fatalf("begin = %+v, want a pending provider checkout", begin)
	}
	if begin.AddressedTo != "ana@example.com" {
		t.Fatalf("addressed_to = %q, want the address the session proved", begin.AddressedTo)
	}

	// The free settlement says the same thing about itself, so a caller never has
	// to know which branch it took before reading the field.
	free := beginSignedInCheckoutOK(t, env, "test-org", "free-fest", token,
		signedInCheckoutBody("Ana", "Lopez", cartLine(freeID, 1)))
	if free.Status != "approved" {
		t.Fatalf("free begin = %+v, want an approved free claim", free)
	}
	if free.AddressedTo != "ana@example.com" {
		t.Fatalf("addressed_to = %q on a free claim, want the address the session proved", free.AddressedTo)
	}
}

// TestSignedInCheckoutComputesSelfAssertedFromTheSession: the flag is constant
// true on this route, and it is constant because the predicate is evaluated,
// not because it was hardcoded. What is asserted here is the value it takes for
// a request that genuinely carries the buyer's own session.
func TestSignedInCheckoutComputesSelfAssertedFromTheSession(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Session Fest", "session-fest", 1000, 10)

	token := customerSignIn(t, env, "ana@example.com")
	begin := beginSignedInCheckoutOK(t, env, "test-org", "session-fest", token,
		signedInCheckoutBody("Ana", "Lopez", cartLine(gaID, 1)))

	if !paymentSessionAuthorized(t, env, begin.ClientTransactionID) {
		t.Fatal("customer_session_authorized = false; the checkout ran under the buyer's own Customer Session")
	}
}

// TestSignedInCheckoutRecordsConsentAsAnswered: the buyer is proven, so a tick
// here is a consent and not a claim. Nothing on this route can produce a Pending
// Confirmation (ADR 0054 supersedes ADR 0035's producer, not its state).
func TestSignedInCheckoutRecordsConsentAsAnswered(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Session Fest", "session-fest", 1000, 10)

	token := signInAnswering(t, env, "ana@example.com", true, false, true)
	// A Customer whose networking consent has never been answered at all — the
	// state a Customer created by a box-office sale or a Sale Import carries into
	// their first Storefront appearance. Written directly because no surface can
	// UNanswer a consent.
	if _, err := env.db.Exec(`UPDATE customers SET networking_consent = NULL WHERE email = $1`, "ana@example.com"); err != nil {
		t.Fatalf("clear networking consent: %v", err)
	}

	body := signedInCheckoutBody("Ana", "Lopez", cartLine(gaID, 1))
	delete(body, "policy_acceptance") // already accepted; no box is drawn for it
	body["networking_consent"] = true

	begin := beginSignedInCheckoutOK(t, env, "test-org", "session-fest", token, body)
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	if got := readConsentState(t, env, "ana@example.com").NetworkingConsent.String; got != "granted" {
		t.Fatalf("networking_consent = %q, want granted: she answered it behind her own session, never pending_confirmation", got)
	}
	checkouts := consentRecordsOn(t, env, "ana@example.com", "checkout")
	if len(checkouts) != 1 {
		t.Fatalf("checkout consent records = %d, want one", len(checkouts))
	}
	if !checkouts[0].EmailProven {
		t.Fatal("email_proven = false on a checkout that ran under a Customer Session")
	}
	if checkouts[0].ConfirmationSentAt.Valid {
		t.Fatal("a Consent Confirmation was sent; a proven answer needs no confirming")
	}
}

// TestSignedInCheckoutAsksAFullyAnsweredCustomerNothing: Policy Acceptance is a
// fact about the Customer, evidenced once and carried. A Customer who has
// answered every question is owed no boxes, sends no consent fields, and is not
// re-asked at the pay button.
func TestSignedInCheckoutAsksAFullyAnsweredCustomerNothing(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Session Fest", "session-fest", 1000, 10)

	token := signInAnswering(t, env, "ana@example.com", true, true, false)
	assertBoxes(t, signedInConsentBoxes(t, env, token), false, false, false,
		"a Customer who answered everything at sign-in is asked nothing")

	body := signedInCheckoutBody("Ana", "Lopez", cartLine(gaID, 1))
	// No boxes were drawn, so no answers are sent — not `false` for the required
	// one, which would be a person declining, but ABSENT.
	delete(body, "policy_acceptance")

	begin := beginSignedInCheckoutOK(t, env, "test-org", "session-fest", token, body)
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	if got := len(consentRecordsOn(t, env, "ana@example.com", "checkout")); got != 0 {
		t.Fatalf("checkout consent records = %d, want none: no box was shown", got)
	}
}

// TestSignedInFreeCheckoutObeysTheSameRule: a free Ticket is still a Ticket that
// needs a reachable inbox (ADR 0017 meets ADR 0054). The zero-total checkout
// settles inside this request, so the gate has to be in front of it.
func TestSignedInFreeCheckoutObeysTheSameRule(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, freeID := publishCheckoutEvent(t, env, sessionID, "Free Fest", "free-fest", 0, 10)

	resp, body := beginSignedInCheckout(t, env, "test-org", "free-fest", "",
		signedInCheckoutBody("Ana", "Lopez", cartLine(freeID, 1)))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d error=%+v, want 401: a free claim is gated exactly as a paid one", resp.StatusCode, body.Error)
	}

	token := customerSignIn(t, env, "ana@example.com")
	result := beginSignedInCheckoutOK(t, env, "test-org", "free-fest", token,
		signedInCheckoutBody("Ana", "Lopez", cartLine(freeID, 1)))
	ref := approvedRef(t, result.freeCheckoutResult)
	if ref == "" {
		t.Fatal("free claim settled without a Sale Confirmation reference")
	}
	if !paymentSessionAuthorized(t, env, result.ClientTransactionID) {
		t.Fatal("customer_session_authorized = false on a free claim behind a Customer Session")
	}
}

// TestSignedInCheckoutStillValidatesTheRestOfTheForm: dropping the address field
// drops nothing else. The Tax ID is still required on this native Sales Channel
// (ADR 0016), and it still fails as a field error the form can mark.
func TestSignedInCheckoutStillValidatesTheRestOfTheForm(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Session Fest", "session-fest", 1000, 10)

	token := customerSignIn(t, env, "ana@example.com")
	body := signedInCheckoutBody("Ana", "Lopez", cartLine(gaID, 1))
	body["customer_tax_id_number"] = invalidCedula

	resp, envBody := beginSignedInCheckout(t, env, "test-org", "session-fest", token, body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d error=%+v, want 400 for a Tax ID that fails its check digit", resp.StatusCode, envBody.Error)
	}
	if fields := fieldErrors(t, envBody); fields["customer_tax_id_number"] == "" {
		t.Fatalf("fields=%+v, want an error on customer_tax_id_number", fields)
	}
}
