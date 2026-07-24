package integration

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The Confirmation Link (ADR 0010, PRD #55 decision 5): the signed link carried
// in every Sale Confirmation that opens that one Ticket Sale without signing in.
//
// Its lifetime is the reverse of a password-reset link's, and the tests below
// pin both halves: the TOKEN outlives the Event by a grace window so it still
// works at the gate months after the purchase, while each redemption mints only
// a SHORT session scoped to one Ticket Sale so a forwarded email's reach closes
// within a day.
//
// Three properties here are the ones easiest to get wrong, and each is attacked
// rather than merely demonstrated:
//
//   - Redeeming a link does not make anyone a Verified Customer.
//   - A link session cannot read any other Ticket Sale, including another
//     belonging to the same Customer.
//   - Redeeming while already holding a full Customer Session does not narrow it.

const confirmationLinkPath = "/api/v1/customer/auth/confirmation-link"

// confirmationLinkGrace mirrors the service's policy: a link keeps working for
// this long after its Event has finished.
const confirmationLinkGrace = 30 * 24 * time.Hour

// confirmationLinkSessionDuration mirrors the short, deliberately non-sliding
// window one redemption mints.
const confirmationLinkSessionDuration = 24 * time.Hour

// lastConfirmationLinkToken returns the token from the most recent Sale
// Confirmation's Confirmation Link.
//
// It goes through the captured email rather than minting a token directly,
// because "every Sale Confirmation carries a working link" is the claim under
// test: a token built in the test would prove the redeem endpoint works while
// saying nothing about whether a Customer ever receives one.
func lastConfirmationLinkToken(t *testing.T, env *testEnv) string {
	t.Helper()
	confs := env.email.Confirmations()
	if len(confs) == 0 {
		t.Fatal("no Sale Confirmation captured")
	}
	return confirmationLinkTokenFrom(t, confs[len(confs)-1].ConfirmationLink)
}

// confirmationLinkTokenFrom pulls the token out of a Confirmation Link URL,
// asserting on the way that the link points at the Storefront rather than at
// this API — a Customer must land on a page, and no browser may address the Go API
// directly (ADR 0008).
func confirmationLinkTokenFrom(t *testing.T, link string) string {
	t.Helper()
	if link == "" {
		t.Fatal("Sale Confirmation carried no Confirmation Link")
	}
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse confirmation link %q: %v", link, err)
	}
	if parsed.Path != "/tickets/confirm" {
		t.Fatalf("confirmation link path = %q, want the Storefront's /tickets/confirm", parsed.Path)
	}
	token := parsed.Query().Get("token")
	if token == "" {
		t.Fatalf("confirmation link %q carries no token", link)
	}
	return token
}

// redeemConfirmationLink exchanges a token for a Customer Session. existing is
// whatever Customer Session the caller already holds, if any — the wider
// credential's presence is what the "does not narrow" rule turns on.
func redeemConfirmationLink(t *testing.T, env *testEnv, token, existing string) (*http.Response, envelope) {
	t.Helper()
	var headers map[string]string
	if existing != "" {
		headers = authHeader(existing)
	}
	return env.post(t, confirmationLinkPath, map[string]string{"token": token}, headers)
}

// redeemConfirmationLinkOK redeems a link that is expected to work and returns
// the session it minted alongside its token.
func redeemConfirmationLinkOK(t *testing.T, env *testEnv, token, existing string) (customerSessionView, string) {
	t.Helper()
	resp, body := redeemConfirmationLink(t, env, token, existing)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("redeem confirmation link status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("redeem confirmation link error=%+v, want none", body.Error)
	}
	var data struct {
		Session   customerSessionView `json:"session"`
		SessionID string              `json:"session_id"`
	}
	if err := json.Unmarshal(body.Data, &data); err != nil {
		t.Fatalf("decode redeem data: %v", err)
	}
	if data.SessionID == "" {
		t.Fatal("expected a Customer Session token from a redeemed Confirmation Link")
	}
	return data.Session, data.SessionID
}

// saleIDByRef reads a Ticket Sale's database id from its Sale Confirmation
// reference. SQL because no API exposes the id, yet it is exactly what a
// Confirmation Link names.
func saleIDByRef(t *testing.T, env *testEnv, ref string) string {
	t.Helper()
	var id string
	if err := env.db.QueryRow(`SELECT id FROM ticket_sales WHERE confirmation_ref = $1`, ref).Scan(&id); err != nil {
		t.Fatalf("read sale id for %q: %v", ref, err)
	}
	return id
}

// TestEverySaleConfirmationCarriesAWorkingConfirmationLink is the path most
// Customers will ever take: no form, no passcode, no typing. The email arrives, the
// link is tapped, and the Ticket Sale it belongs to is there.
func TestEverySaleConfirmationCarriesAWorkingConfirmationLink(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	ref := seedSaleForCustomer(t, env, sessionID, "Link Fest", "link-fest",
		env.fixedClock.Add(60*24*time.Hour), "link-1", "ana@example.com", "Ana", "Lopez")

	token := lastConfirmationLinkToken(t, env)
	session, sessionToken := redeemConfirmationLinkOK(t, env, token, "")

	if session.Email != "ana@example.com" {
		t.Fatalf("session email = %q, want ana@example.com", session.Email)
	}
	// The session says out loud what it is scoped to, so a client never has to
	// guess whether it is holding a full session or a link one.
	saleID := saleIDByRef(t, env, ref)
	if session.TicketSaleID == nil || *session.TicketSaleID != saleID {
		t.Fatalf("session ticket_sale_id = %v, want %q", session.TicketSaleID, saleID)
	}

	area := readCustomerArea(t, env, sessionToken, "")
	if len(area.Upcoming) != 1 || len(area.Past) != 0 {
		t.Fatalf("area = %d upcoming / %d past, want exactly the one linked sale", len(area.Upcoming), len(area.Past))
	}
	if area.Upcoming[0].ConfirmationRef != ref {
		t.Fatalf("area sale = %q, want the linked sale %q", area.Upcoming[0].ConfirmationRef, ref)
	}
}

// TestConfirmationLinkWorksOnTheDayOfTheEventAndExpiresAfterTheGraceWindow is
// the reason the token is durable at all. A ticket bought months ahead is opened
// in a queue at the gate; an expiry measured in days would kill the link exactly
// when it is most needed. It does eventually stop, a grace window after the
// Event, so a forwarded email is not a permanent key.
func TestConfirmationLinkWorksOnTheDayOfTheEventAndExpiresAfterTheGraceWindow(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventStart := env.fixedClock.Add(90 * 24 * time.Hour)
	seedSaleForCustomer(t, env, sessionID, "Gate Fest", "gate-fest",
		eventStart, "link-gate", "ana@example.com", "Ana", "Lopez")

	token := lastConfirmationLinkToken(t, env)

	// The day of the Event, three months after the email landed.
	setCustomerClock(t, env, eventStart)
	session, _ := redeemConfirmationLinkOK(t, env, token, "")
	if session.TicketSaleID == nil {
		t.Fatal("expected a sale-scoped session on the day of the Event")
	}

	// Still inside the grace window: a late question about the purchase resolves.
	setCustomerClock(t, env, eventStart.Add(confirmationLinkGrace-time.Hour))
	redeemConfirmationLinkOK(t, env, token, "")

	// Past it: the link is spent, and says so as an expiry rather than as a
	// forgery, because this person really did buy a ticket.
	setCustomerClock(t, env, eventStart.Add(confirmationLinkGrace+time.Hour))
	resp, body := redeemConfirmationLink(t, env, token, "")
	assertAPIError(t, resp, body, http.StatusUnauthorized, "CONFIRMATION_LINK_EXPIRED")
}

// TestConfirmationLinkMintsAShortSessionRemintableByTappingAgain pins the other
// half of the bargain: the durable token buys only a day of access at a time, so
// a forwarded confirmation email stops opening anything within a day — while the
// person who owns it just taps the link again.
func TestConfirmationLinkMintsAShortSessionRemintableByTappingAgain(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	seedSaleForCustomer(t, env, sessionID, "Short Fest", "short-fest",
		env.fixedClock.Add(60*24*time.Hour), "link-short", "ana@example.com", "Ana", "Lopez")

	token := lastConfirmationLinkToken(t, env)
	_, first := redeemConfirmationLinkOK(t, env, token, "")

	mintedExpiry := env.fixedClock.Add(confirmationLinkSessionDuration)
	if got := sessionExpiry(t, env, first); !got.Equal(mintedExpiry) {
		t.Fatalf("link session expiry = %s, want %s (24h, far short of a full session's 180 days)", got, mintedExpiry)
	}

	// The half of this that mint-time alone cannot see: USING the session must not
	// buy more of it. A full Customer Session slides its window on every
	// authenticated request, and a sale-scoped one sharing that path would have the
	// first read behind a forwarded Sale Confirmation quietly promote a 24-hour
	// credential to 180 days. Several reads, spread across the window and through
	// both authenticated endpoints, and the expiry must still be the minted one.
	for _, at := range []time.Duration{time.Hour, 6 * time.Hour, 23 * time.Hour} {
		setCustomerClock(t, env, env.fixedClock.Add(at))

		readCustomerArea(t, env, first, "")
		resp, body := env.get(t, customerSessionPath, authHeader(first))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("session read at +%s status=%d error=%+v", at, resp.StatusCode, body.Error)
		}

		if got := sessionExpiry(t, env, first); !got.Equal(mintedExpiry) {
			t.Fatalf("link session expiry after use at +%s = %s, want %s unchanged — a sale-scoped session must not slide",
				at, got, mintedExpiry)
		}
	}

	// A day later the session is gone, even though the link is not — and it is gone
	// on the schedule set at mint time, not one pushed out by the reads above.
	setCustomerClock(t, env, env.fixedClock.Add(confirmationLinkSessionDuration+time.Minute))
	resp, body := env.get(t, customerAreaPath, authHeader(first))
	assertAPIError(t, resp, body, http.StatusUnauthorized, "CUSTOMER_SESSION_EXPIRED")

	// Tapping the link again mints a fresh one: re-mintable, not one-shot.
	_, second := redeemConfirmationLinkOK(t, env, token, "")
	if second == first {
		t.Fatal("expected a new Customer Session token from a second redemption")
	}
	area := readCustomerArea(t, env, second, "")
	if len(area.Upcoming) != 1 {
		t.Fatalf("re-minted session sees %d upcoming sales, want 1", len(area.Upcoming))
	}
}

// TestConfirmationLinkSessionCannotReadAnotherTicketSale is the property the
// link's durability is paid for with. Confirmation emails get forwarded — to a
// friend coming along, to whoever is paying — so a link must open one Ticket
// Sale and nothing else, including the Customer's own other purchases.
//
// The attack is deliberate: same Customer, two sales, a link session for one,
// then an active attempt to read the other both implicitly and by naming it in
// the request.
func TestConfirmationLinkSessionCannotReadAnotherTicketSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	linkedRef := seedSaleForCustomer(t, env, sessionID, "Linked Fest", "linked-fest",
		env.fixedClock.Add(30*24*time.Hour), "link-a", "ana@example.com", "Ana", "Lopez")
	linkedToken := lastConfirmationLinkToken(t, env)

	// A second purchase by the SAME Customer. Belonging to the same person is
	// exactly what must not be enough to see it.
	privateRef := seedSaleForCustomer(t, env, sessionID, "Private Fest", "private-fest",
		env.fixedClock.Add(45*24*time.Hour), "link-b", "ana@example.com", "Ana", "Lopez")
	privateSaleID := saleIDByRef(t, env, privateRef)

	_, linkSession := redeemConfirmationLinkOK(t, env, linkedToken, "")

	assertOnlyOwnSale(t, readCustomerArea(t, env, linkSession, ""), linkedRef, privateRef,
		"a Confirmation Link session")

	// Naming the other sale in the request changes nothing: the scope is on the
	// session row, and the read takes no identifier from the caller at all.
	for _, query := range []string{
		"ticket_sale_id=" + privateSaleID,
		"id=" + privateSaleID,
		"confirmation_ref=" + privateRef,
	} {
		assertOnlyOwnSale(t, readCustomerArea(t, env, linkSession, query), linkedRef, privateRef,
			"a Confirmation Link session with "+query)
	}

	// And a full sign-in by the same person does see both, so the narrowing above
	// is the link's doing rather than the sale being invisible generally.
	full := customerSignIn(t, env, "ana@example.com")
	area := readCustomerArea(t, env, full, "")
	if len(area.Upcoming) != 2 {
		t.Fatalf("full session sees %d upcoming sales, want both", len(area.Upcoming))
	}
}

// TestConfirmationLinkDoesNotVerifyTheCustomer guards the single most load-
// bearing null in the schema. Possession of a forwarded email is not proof of
// owning the address, and verified_at is what gates the profile-name rule and,
// later, whether we may email this person at all. Only a passcode may set it.
func TestConfirmationLinkDoesNotVerifyTheCustomer(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	seedSaleForCustomer(t, env, sessionID, "Unverified Fest", "unverified-fest",
		env.fixedClock.Add(30*24*time.Hour), "link-unverified", "ana@example.com", "Ana", "Lopez")

	token := lastConfirmationLinkToken(t, env)
	session, _ := redeemConfirmationLinkOK(t, env, token, "")

	if session.VerifiedAt != nil {
		t.Fatalf("session verified_at = %q, want null after a Confirmation Link redemption", *session.VerifiedAt)
	}
	// Read the record itself, not just what the response said about it.
	if customer := readCustomer(t, env, "ana@example.com"); customer.VerifiedAt.Valid {
		t.Fatalf("customers.verified_at = %v, want null: only a passcode makes a Verified Customer", customer.VerifiedAt.Time)
	}

	// Redeeming repeatedly does not accumulate into verification either.
	redeemConfirmationLinkOK(t, env, token, "")
	if customer := readCustomer(t, env, "ana@example.com"); customer.VerifiedAt.Valid {
		t.Fatalf("customers.verified_at = %v after a second redemption, want null", customer.VerifiedAt.Time)
	}
}

// TestConfirmationLinkDoesNotNarrowAFullCustomerSession pins the rule that the
// wider credential wins. Someone signed in who taps a link in an old email must
// not silently lose their history for the next day.
func TestConfirmationLinkDoesNotNarrowAFullCustomerSession(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	linkedRef := seedSaleForCustomer(t, env, sessionID, "Wide Fest", "wide-fest",
		env.fixedClock.Add(30*24*time.Hour), "link-wide-a", "ana@example.com", "Ana", "Lopez")
	linkToken := lastConfirmationLinkToken(t, env)
	otherRef := seedSaleForCustomer(t, env, sessionID, "Other Fest", "other-fest",
		env.fixedClock.Add(45*24*time.Hour), "link-wide-b", "ana@example.com", "Ana", "Lopez")

	full := customerSignIn(t, env, "ana@example.com")
	sessionsBefore := countCustomerSessions(t, env)

	session, returned := redeemConfirmationLinkOK(t, env, linkToken, full)

	if returned != full {
		t.Fatalf("redeeming while signed in returned a different session token; the existing full session must be kept")
	}
	if session.TicketSaleID != nil {
		t.Fatalf("session ticket_sale_id = %q, want null: a link must not narrow a full Customer Session", *session.TicketSaleID)
	}
	if got := countCustomerSessions(t, env); got != sessionsBefore {
		t.Fatalf("customer sessions = %d, want %d: no narrower session should have been minted", got, sessionsBefore)
	}

	// The proof that matters is the read, not the response shape.
	area := readCustomerArea(t, env, full, "")
	if len(area.Upcoming) != 2 {
		t.Fatalf("after redeeming, the full session sees %d upcoming sales, want both (%s and %s)",
			len(area.Upcoming), linkedRef, otherRef)
	}
	// The full session is also untouched in the database: still unscoped, still
	// on its own long window rather than the link's 24 hours.
	if got, want := sessionExpiry(t, env, full), env.fixedClock.Add(customerSessionDuration); !got.Equal(want) {
		t.Fatalf("full session expiry = %s, want %s: redeeming a link must not shorten it", got, want)
	}
}

// TestTamperedOrUnsignedConfirmationLinkTokenIsRejected proves the signature is
// what the endpoint trusts, not the payload. Every case here is a token whose
// contents look plausible and whose HMAC does not hold.
func TestTamperedOrUnsignedConfirmationLinkTokenIsRejected(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	seedSaleForCustomer(t, env, sessionID, "Tamper Fest", "tamper-fest",
		env.fixedClock.Add(30*24*time.Hour), "link-tamper", "ana@example.com", "Ana", "Lopez")

	token := lastConfirmationLinkToken(t, env)
	payload, mac, found := strings.Cut(token, ".")
	if !found {
		t.Fatalf("token %q is not the expected payload.signature shape", token)
	}

	cases := []struct {
		name  string
		token string
	}{
		// The signature edited: the classic forgery attempt.
		{"tampered signature", payload + "." + flipFirstRune(mac)},
		// The payload edited to point somewhere else, signature left alone. This
		// is the one that would expose another Ticket Sale if the MAC were not
		// checked before the payload is believed.
		{"tampered payload", flipFirstRune(payload) + "." + mac},
		// No signature at all.
		{"unsigned", payload},
		{"empty signature", payload + "."},
		// Invented wholesale.
		{"fabricated", "bm90LWEtdG9rZW4.bm90LWEtc2lnbmF0dXJl"},
		{"not base64", "!!!.???"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := redeemConfirmationLink(t, env, tc.token, "")
			assertAPIError(t, resp, body, http.StatusUnauthorized, "CONFIRMATION_LINK_INVALID")
		})
	}

	// A missing token is a malformed request rather than a failed credential.
	resp, body := redeemConfirmationLink(t, env, "", "")
	assertAPIError(t, resp, body, http.StatusBadRequest, "VALIDATION_FAILED")

	// Nothing above minted a session.
	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0 after only rejected tokens", n)
	}
}

// flipFirstRune changes a token segment's leading character to a different one
// from the same base64url alphabet: the segment stays decodable, and the byte it
// decodes to genuinely differs. (The final character is deliberately not used —
// its low bits can be spare, so editing it does not always change the payload.)
func flipFirstRune(s string) string {
	if s == "" {
		return "A"
	}
	replacement := byte('A')
	if s[0] == 'A' {
		replacement = 'B'
	}
	return string(replacement) + s[1:]
}

// TestConfirmationLinkForReversedTicketSaleSaysSo proves a link for a purchase
// that has since been reversed still opens and reports the reversal, rather than
// dying silently or — worse — showing tickets the holder no longer has.
func TestConfirmationLinkForReversedTicketSaleSaysSo(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Reversed Fest", "reversed-fest")
	scheduleEvent(t, env, sessionID, eventID, "Reversed Fest", "reversed-fest", env.fixedClock.Add(30*24*time.Hour))
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2500, 50)

	batchID := commitBatch(t, env, sessionID, eventID, "link-reversed", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
			"ticket_type_id": ttID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	token := lastConfirmationLinkToken(t, env)
	undoBatch(t, env, sessionID, eventID, batchID)

	_, linkSession := redeemConfirmationLinkOK(t, env, token, "")
	area := readCustomerArea(t, env, linkSession, "")

	sales := append(append([]customerAreaSale{}, area.Upcoming...), area.Past...)
	if len(sales) != 1 {
		t.Fatalf("area holds %d sales, want the one reversed sale the link names", len(sales))
	}
	// The status is what lets the Storefront say "reversed" plainly rather than
	// present the purchase as tickets still held.
	if sales[0].Status != "reversed" {
		t.Fatalf("sale status = %q, want reversed", sales[0].Status)
	}
}
