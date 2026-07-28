package integration

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"
)

// Customer-initiated Sale Reversal, end to end (issue #119, ADR 0018): a
// signed-in Customer undoes their own free Online Sale.
//
// Free sales are almost the whole of this slice, and that is what makes it a
// complete proof rather than a partial one. A zero-total checkout settles with no
// Payment Provider in the loop at all (ADR 0017), so every part of the domain —
// authorization, window enforcement, capacity restoration, the void notice, the
// badge, the money surfaces — is exercised here with nothing external mocked or
// skipped. A paid sale adds one call to a provider and reuses all of it, which
// is what customer_sale_reversal_payphone_test.go drives against the fake
// PayPhone server; the two paid tests below are about the provider SELECTION —
// the development stub, and a Payment Method naming a provider this deployment
// cannot ask.
//
// The buyer-facing word is "undo". Never "cancel", which belongs to an Event's
// lifecycle status, and never "refund", which is wrong where no money moved.

// reverseSaleRequest posts the undo for one Ticket Sale under a Customer Session
// token, returning the raw response so refusals can be asserted on.
//
// The token is the entire authorization. There is no Customer, email, or
// Organization anywhere in this request — the id in the path names WHICH of the
// caller's own sales to undo and can widen nothing.
func reverseSaleRequest(t *testing.T, env *testEnv, token, saleID string) (*http.Response, envelope) {
	t.Helper()
	var headers map[string]string
	if token != "" {
		headers = authHeader(token)
	}
	return env.post(t, "/api/v1/customer/ticket-sales/"+saleID+"/reverse", nil, headers)
}

// undoOwnSale drives a whole real undo from a Sale Confirmation reference: the
// buyer signs in, finds their own sale in their Area, and presses Undo.
//
// It exists for the tests that need a reversed Ticket Sale as a FIXTURE — the
// money surfaces, mostly — and used to be an UPDATE statement in each of them,
// because no endpoint could reverse an Online Sale. One now can, and a fixture
// staged by the code under test is worth more than one staged beside it: a
// reversal that stopped restoring capacity or stopped voiding the sale would
// break those tests too.
func undoOwnSale(t *testing.T, env *testEnv, email, confirmationRef string) {
	t.Helper()
	token := customerSignIn(t, env, email)
	sale := saleByRef(t, readCustomerArea(t, env, token, ""), confirmationRef)
	reverseSaleOK(t, env, token, sale.ID)
}

// saleReversalResult is the endpoint's success body.
type saleReversalResult struct {
	TicketSaleID    string `json:"ticket_sale_id"`
	ConfirmationRef string `json:"confirmation_ref"`
	Status          string `json:"status"`
	ReversedAt      string `json:"reversed_at"`
}

func reverseSaleOK(t *testing.T, env *testEnv, token, saleID string) saleReversalResult {
	t.Helper()
	resp, body := reverseSaleRequest(t, env, token, saleID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	var out saleReversalResult
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode undo result: %v", err)
	}
	return out
}

// assertRefused asserts an undo was refused with a given status and error code,
// which is only half of what a refusal promises. The other half — that nothing
// changed — is asserted by each test against the state it staged.
func assertRefused(t *testing.T, resp *http.Response, body envelope, wantStatus int, wantCode string) {
	t.Helper()
	if resp.StatusCode != wantStatus {
		t.Fatalf("undo status=%d error=%+v, want %d %s", resp.StatusCode, body.Error, wantStatus, wantCode)
	}
	if body.Error == nil || body.Error.Code != wantCode {
		t.Fatalf("undo error=%+v, want code %s", body.Error, wantCode)
	}
}

// publishFreeEvent publishes a sellable Event whose single Ticket Type costs
// nothing, three days out so the Reversal Window is governed by the Ecuadorian
// cutoff rather than by the doors opening.
func publishFreeEvent(t *testing.T, env *testEnv, sessionID, name, slug string, capacity int) (eventID, ticketTypeID string) {
	t.Helper()
	return publishEventStarting(t, env, sessionID, name, slug,
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", 0, capacity)
}

// claimFree completes a free Online Sale and returns its Sale Confirmation
// reference. No provider is contacted and no confirm leg runs: the sale is
// recorded inside begin-checkout (ADR 0017).
func claimFree(t *testing.T, env *testEnv, eventSlug, ticketTypeID, email string, quantity int) string {
	t.Helper()
	return approvedRef(t, beginCheckoutSettled(t, env, testOrgSlug, eventSlug, "",
		checkoutBody(email, "Ana", "Lopez", cartLine(ticketTypeID, quantity))))
}

// remaining is how many of a Ticket Type the Storefront still offers, read from
// the public event page.
//
// It is deliberately the buyer-facing number rather than the sold_count column:
// what a reversal owes everybody who did not get a ticket is that the tickets
// are on sale again, and this is the figure that says so. A free claim takes no
// Capacity Hold (ADR 0017), so nothing else is subtracted from it here.
func remaining(t *testing.T, env *testEnv, eventSlug, ticketTypeName string) int {
	t.Helper()
	tt, ok := publicTicketTypes(t, env, testOrgSlug, eventSlug)[ticketTypeName]
	if !ok {
		t.Fatalf("ticket type %q is not on the public page for %q", ticketTypeName, eventSlug)
	}
	return tt.Remaining
}

// saleProvenance reads the reversal provenance recorded on a Ticket Sale (#117):
// its status, when it was reversed and which side asked. SQL because the
// Customer is shown that their purchase is reversed, not the audit columns
// behind it.
func saleProvenance(t *testing.T, env *testEnv, ref string) (status string, reversedAt sql.NullTime, reversedBy sql.NullString) {
	t.Helper()
	if err := env.db.QueryRow(
		`SELECT status, reversed_at, reversed_by FROM ticket_sales WHERE confirmation_ref = $1`, ref,
	).Scan(&status, &reversedAt, &reversedBy); err != nil {
		t.Fatalf("read sale provenance for %q: %v", ref, err)
	}
	return status, reversedAt, reversedBy
}

// saleByRef finds one of the Customer's sales in their own Area by its Sale
// Confirmation reference. Going through the Area rather than the database is the
// point: the id the undo endpoint takes is the id this Customer was given.
func saleByRef(t *testing.T, area customerAreaView, ref string) customerAreaSale {
	t.Helper()
	for _, sale := range append(append([]customerAreaSale{}, area.Upcoming...), area.Past...) {
		if sale.ConfirmationRef == ref {
			return sale
		}
	}
	t.Fatalf("sale %s is not in the Customer Area", ref)
	return customerAreaSale{}
}

// TestCustomerUndoesTheirOwnFreeOnlineSale is the tracer bullet, and it asserts
// every promise the feature makes at once: the sale is voided and stamped with
// the Customer as the actor, its tickets go back on sale, the buyer is told, and
// the sale itself is still there — reversed, not deleted, keeping the Sale
// Confirmation reference they were emailed.
func TestCustomerUndoesTheirOwnFreeOnlineSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, freeID := publishFreeEvent(t, env, sessionID, "Free Fest", "free-fest", 10)

	ref := claimFree(t, env, "free-fest", freeID, "ana@example.com", 3)
	if got := remaining(t, env, "free-fest", "GA"); got != 7 {
		t.Fatalf("remaining after a claim of 3 from 10 = %d, want 7", got)
	}
	env.email.Reset()

	token := customerSignIn(t, env, "ana@example.com")
	sale := saleByRef(t, readCustomerArea(t, env, token, ""), ref)
	if !sale.Reversible {
		t.Fatal("a free claim made this morning is not offered as reversible; the undo cannot be reached")
	}

	result := reverseSaleOK(t, env, token, sale.ID)
	if result.Status != "reversed" {
		t.Fatalf("status = %q, want reversed", result.Status)
	}
	if result.ConfirmationRef != ref {
		t.Fatalf("confirmation_ref = %q, want the reference the Customer was emailed, %q", result.ConfirmationRef, ref)
	}
	if result.ReversedAt != env.fixedClock.UTC().Format(time.RFC3339) {
		t.Fatalf("reversed_at = %q, want the server's own clock %q",
			result.ReversedAt, env.fixedClock.UTC().Format(time.RFC3339))
	}

	// The reversal time and the acting side are recorded (#117): 'customer',
	// because the buyer asked — not 'staff', which is what a Sale Import undo
	// writes.
	status, reversedAt, reversedBy := saleProvenance(t, env, ref)
	if status != "reversed" {
		t.Fatalf("stored status = %q, want reversed", status)
	}
	if !reversedAt.Valid || !reversedAt.Time.Equal(env.fixedClock) {
		t.Fatalf("reversed_at = %+v, want %v", reversedAt, env.fixedClock)
	}
	if !reversedBy.Valid || reversedBy.String != "customer" {
		t.Fatalf("reversed_by = %+v, want 'customer'", reversedBy)
	}

	// Capacity comes back, which is the whole point of a reversal for everyone
	// who did not get a ticket: the Storefront offers those tickets again.
	if ga := publicTicketTypes(t, env, testOrgSlug, "free-fest")["GA"]; ga.Remaining != 10 || ga.SoldOut {
		t.Fatalf("public GA = %+v after the undo, want all 10 remaining and not sold out", ga)
	}

	// The void notice goes to the Customer, and only after the reversal has
	// committed — there is nothing to tell somebody about until the transaction
	// that removes their tickets has actually removed them.
	voided := env.email.Voided()
	if len(voided) != 1 {
		t.Fatalf("captured %d void notices, want exactly 1", len(voided))
	}
	if voided[0].To != "ana@example.com" || voided[0].Reference != ref {
		t.Fatalf("void notice = %+v, want it addressed to the buyer quoting %q", voided[0], ref)
	}

	// The sale stays visible with its reference, so the Customer Area can draw a
	// Reversed badge against the same purchase rather than losing it.
	after := saleByRef(t, readCustomerArea(t, env, token, ""), ref)
	if after.Status != "reversed" {
		t.Fatalf("Customer Area status = %q, want reversed", after.Status)
	}
	if after.Reversible || after.ReversibleUntil != nil {
		t.Fatalf("a reversed sale is still offered as reversible (%+v); it cannot be undone twice", after)
	}
}

// TestUndoIsRefusedAfterTheReversalWindowCloses: the window is enforced on the
// server, whatever the client believed. The purchase is backdated to 20:30
// Ecuador time the evening before, so its window shut before it ever opened, and
// the request is refused with nothing touched.
//
// The instant is moved with SQL because nothing in the public API sells a ticket
// into the past; a Storefront that had cached this morning's `reversible: true`
// would be sending exactly this request.
func TestUndoIsRefusedAfterTheReversalWindowCloses(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, freeID := publishFreeEvent(t, env, sessionID, "Late Fest", "late-fest", 10)

	ref := claimFree(t, env, "late-fest", freeID, "ana@example.com", 2)
	token := customerSignIn(t, env, "ana@example.com")
	saleID := saleByRef(t, readCustomerArea(t, env, token, ""), ref).ID
	env.email.Reset()

	// 2026-07-07T01:30:00Z is 20:30 on 6 July in Ecuador: half an hour past that
	// day's cutoff.
	if _, err := env.db.Exec(
		`UPDATE ticket_sales SET sold_at = $1 WHERE confirmation_ref = $2`,
		time.Date(2026, 7, 7, 1, 30, 0, 0, time.UTC), ref,
	); err != nil {
		t.Fatalf("backdate the sale: %v", err)
	}

	resp, body := reverseSaleRequest(t, env, token, saleID)
	assertRefused(t, resp, body, http.StatusConflict, "REVERSAL_WINDOW_CLOSED")
	assertNothingChanged(t, env, ref, "late-fest", "GA", 8)
}

// TestUndoIsRefusedOnceTheEventHasStarted: the window also ends at the doors,
// because the platform holds no record of attendance and cannot tell a change of
// mind from a completed visit. The Event here started an hour ago.
func TestUndoIsRefusedOnceTheEventHasStarted(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, freeID := publishEventStarting(t, env, sessionID, "Matinee", "matinee",
		env.fixedClock.Add(2*time.Hour), "America/Guayaquil", 0, 10)

	ref := claimFree(t, env, "matinee", freeID, "ana@example.com", 1)
	token := customerSignIn(t, env, "ana@example.com")
	saleID := saleByRef(t, readCustomerArea(t, env, token, ""), ref).ID
	env.email.Reset()

	// The doors open before the request arrives: the Event is moved back so it
	// started an hour before the harness clock.
	if _, err := env.db.Exec(
		`UPDATE events SET starts_at = $1 WHERE slug = 'matinee'`,
		env.fixedClock.Add(-time.Hour),
	); err != nil {
		t.Fatalf("move the event start: %v", err)
	}

	resp, body := reverseSaleRequest(t, env, token, saleID)
	assertRefused(t, resp, body, http.StatusConflict, "REVERSAL_WINDOW_CLOSED")
	assertNothingChanged(t, env, ref, "matinee", "GA", 9)
}

// assertNothingChanged is what every refusal owes: the Ticket Sale still stands,
// its capacity is still consumed, and nobody has been told their tickets are
// gone.
func assertNothingChanged(t *testing.T, env *testEnv, ref, eventSlug, ticketTypeName string, wantRemaining int) {
	t.Helper()
	status, reversedAt, reversedBy := saleProvenance(t, env, ref)
	if status != "active" {
		t.Fatalf("status = %q after a refused undo, want active", status)
	}
	if reversedAt.Valid || reversedBy.Valid {
		t.Fatalf("reversal provenance written on a refused undo: at=%+v by=%+v", reversedAt, reversedBy)
	}
	if got := remaining(t, env, eventSlug, ticketTypeName); got != wantRemaining {
		t.Fatalf("remaining = %d after a refused undo, want %d — capacity must not move", got, wantRemaining)
	}
	if voided := env.email.Voided(); len(voided) != 0 {
		t.Fatalf("captured %d void notices after a refused undo, want none", len(voided))
	}
}

// TestUndoIsRefusedOnAnotherCustomersSale: a Customer may only undo a Ticket
// Sale they own. The refusal is a plain not-found, identical to the one an id
// nobody owns gets, so the endpoint cannot be used to discover whose purchase a
// given id is.
func TestUndoIsRefusedOnAnotherCustomersSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, freeID := publishFreeEvent(t, env, sessionID, "Shared Fest", "shared-fest", 10)

	ref := claimFree(t, env, "shared-fest", freeID, "ana@example.com", 2)
	anaToken := customerSignIn(t, env, "ana@example.com")
	anasSaleID := saleByRef(t, readCustomerArea(t, env, anaToken, ""), ref).ID

	// A second, entirely legitimate Customer with a purchase of their own, so
	// this is a signed-in stranger rather than an unauthenticated caller.
	claimFree(t, env, "shared-fest", freeID, "bruno@example.com", 1)
	brunoToken := customerSignIn(t, env, "bruno@example.com")
	env.email.Reset()

	resp, body := reverseSaleRequest(t, env, brunoToken, anasSaleID)
	assertRefused(t, resp, body, http.StatusNotFound, "TICKET_SALE_NOT_FOUND")
	assertNothingChanged(t, env, ref, "shared-fest", "GA", 7)

	// A well-formed id that belongs to nobody is answered exactly the same way.
	resp, body = reverseSaleRequest(t, env, brunoToken, "d0000000-0000-4000-8000-00000000dead")
	assertRefused(t, resp, body, http.StatusNotFound, "TICKET_SALE_NOT_FOUND")
}

// TestUndoRequiresAFullCustomerSession: reversal is the first mutating,
// money-moving action a Customer can take, so it takes the credential that
// proves ownership of the address and nothing weaker.
//
// A Confirmation Link is refused explicitly. It is a read credential that
// travels by email and can be forwarded, quoted, or shared — it opens the sale
// perfectly well, and may not undo it.
func TestUndoRequiresAFullCustomerSession(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, freeID := publishFreeEvent(t, env, sessionID, "Guarded Fest", "guarded-fest", 10)

	ref := claimFree(t, env, "guarded-fest", freeID, "ana@example.com", 2)
	linkToken := lastConfirmationLinkToken(t, env)
	token := customerSignIn(t, env, "ana@example.com")
	saleID := saleByRef(t, readCustomerArea(t, env, token, ""), ref).ID
	env.email.Reset()

	// No credential at all.
	resp, body := reverseSaleRequest(t, env, "", saleID)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("undo without a session status=%d error=%+v, want 401", resp.StatusCode, body.Error)
	}
	assertNothingChanged(t, env, ref, "guarded-fest", "GA", 8)

	// A garbage token is worth exactly as much.
	resp, body = reverseSaleRequest(t, env, "not-a-session", saleID)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("undo with a bogus token status=%d error=%+v, want 401", resp.StatusCode, body.Error)
	}

	// The Confirmation Link session: genuine, scoped to this very sale, and
	// still refused. 403 rather than 401, because presenting it again can never
	// help — a wider credential is what is needed.
	_, linkSession := redeemConfirmationLinkOK(t, env, linkToken, "")
	resp, body = reverseSaleRequest(t, env, linkSession, saleID)
	assertRefused(t, resp, body, http.StatusForbidden, "CUSTOMER_SESSION_SCOPE_INSUFFICIENT")
	assertNothingChanged(t, env, ref, "guarded-fest", "GA", 8)

	// And the full session undoes the same sale, so the refusals above are about
	// the credential rather than about the sale.
	reverseSaleOK(t, env, token, saleID)
}

// TestUndoTwiceReversesOnce: pressing the button again — a double-click, a
// refresh, an impatient retry — must not restore capacity a second time. The
// second request is refused as already reversed, which is the same answer a
// request arriving tomorrow would get.
func TestUndoTwiceReversesOnce(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, freeID := publishFreeEvent(t, env, sessionID, "Twice Fest", "twice-fest", 10)

	ref := claimFree(t, env, "twice-fest", freeID, "ana@example.com", 4)
	token := customerSignIn(t, env, "ana@example.com")
	saleID := saleByRef(t, readCustomerArea(t, env, token, ""), ref).ID
	env.email.Reset()

	reverseSaleOK(t, env, token, saleID)

	resp, body := reverseSaleRequest(t, env, token, saleID)
	assertRefused(t, resp, body, http.StatusConflict, "SALE_ALREADY_REVERSED")

	if got := remaining(t, env, "twice-fest", "GA"); got != 10 {
		t.Fatalf("remaining = %d after undoing twice, want 10 — capacity was restored more than once", got)
	}
	if voided := env.email.Voided(); len(voided) != 1 {
		t.Fatalf("captured %d void notices, want exactly 1 — the refused second undo must not email anybody", len(voided))
	}
}

// TestConcurrentUndoReversesOnce is the same promise under a real race rather
// than in sequence: two requests for the same sale, in flight together. Exactly
// one succeeds, capacity is restored exactly once, and one void notice is sent.
//
// The serialization is the repository primitive's row lock (#117), not anything
// this endpoint adds, which is precisely why the reversal is never
// reimplemented per caller.
func TestConcurrentUndoReversesOnce(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, freeID := publishFreeEvent(t, env, sessionID, "Race Fest", "race-fest", 10)

	ref := claimFree(t, env, "race-fest", freeID, "ana@example.com", 5)
	token := customerSignIn(t, env, "ana@example.com")
	saleID := saleByRef(t, readCustomerArea(t, env, token, ""), ref).ID
	env.email.Reset()

	var wg sync.WaitGroup
	statuses := make([]int, 2)
	for i := range statuses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, _ := reverseSaleRequest(t, env, token, saleID)
			statuses[i] = resp.StatusCode
		}(i)
	}
	wg.Wait()

	ok, conflicts := 0, 0
	for _, status := range statuses {
		switch status {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("concurrent undo returned %d; want one 200 and one 409", status)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatalf("concurrent undos returned %d ok and %d conflicts, want exactly one of each", ok, conflicts)
	}
	if got := remaining(t, env, "race-fest", "GA"); got != 10 {
		t.Fatalf("remaining = %d after a concurrent double undo, want 10", got)
	}
	if voided := env.email.Voided(); len(voided) != 1 {
		t.Fatalf("captured %d void notices from a concurrent double undo, want exactly 1", len(voided))
	}
	if status, _, _ := saleProvenance(t, env, ref); status != "reversed" {
		t.Fatalf("status = %q, want reversed", status)
	}
}

// TestUndoIsRefusedOnASaleThatIsNotAnOnlineSale: a sale the Organization
// recorded off-platform was never collected by the platform, so the platform has
// nothing to give back. The Customer Area never offers it, and the endpoint
// refuses it whether or not anything offered it.
func TestUndoIsRefusedOnAnImportedSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	seedSaleForCustomer(t, env, sessionID, "Imported Fest", "imported-fest",
		env.fixedClock.Add(30*24*time.Hour), "undo-import-1", "ana@example.com", "Ana", "Lopez")

	token := customerSignIn(t, env, "ana@example.com")
	sale := onlySale(t, readCustomerArea(t, env, token, ""))
	if sale.Reversible {
		t.Fatal("an imported sale is offered as reversible")
	}
	env.email.Reset()

	resp, body := reverseSaleRequest(t, env, token, sale.ID)
	assertRefused(t, resp, body, http.StatusConflict, "SALE_NOT_REVERSIBLE")
	if status, _, _ := saleProvenance(t, env, sale.ConfirmationRef); status != "active" {
		t.Fatalf("status = %q after a refused undo, want active", status)
	}
	if voided := env.email.Voided(); len(voided) != 0 {
		t.Fatalf("captured %d void notices, want none", len(voided))
	}
}

// #119's TestUndoIsRefusedWhenThePaymentProviderCannotReverseIt stood here, and
// it is gone rather than rewritten.
//
// It proved that the offer and the refusal came from ONE fact — can the Payment
// Provider that collected this money give it back? — using the only answer
// available then: no provider could. The fact has not moved, only the answer
// has, and there is no longer a Ticket Sale this suite can stage that the answer
// is "no" for. A sale settled by some other provider is the remaining case, and
// the schema cannot hold one: ticket_sales.payment_method is constrained to
// cash, transfer, payphone and free, so a foreign provider's name is not a row
// this database will accept. That arm is pinned in TestPaymentReversalSupports
// (internal/platform), against the rule itself.
//
// What replaces it here is the case the deployment actually has: the stub.

// TestCustomerUndoesAPaidSaleOnTheStubProvider is the development path, and it
// is the reason the stub reverses at all (#120).
//
// A deployment with no PayPhone credentials configured — every developer's, and
// the whole Playwright suite's — settles its Online Sales through the stub, and
// those sales record Payment Method `payphone` all the same (ADR 0012). If the
// stub refused reversal, or were mistaken for a provider that never collected
// the money, the Undo would be missing from every local paid purchase and the
// first place the feature ran end to end would be production.
//
// The provider leg's real behaviour is proved against the fake PayPhone server
// in customer_sale_reversal_payphone_test.go; what is proved here is that the
// stub does not stand in the way of it.
func TestCustomerUndoesAPaidSaleOnTheStubProvider(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, paidID := publishEventStarting(t, env, sessionID, "Stub Paid Fest", "stub-paid-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	ref := buyOnline(t, env, "stub-paid-fest", paidID, "ana@example.com")
	token := customerSignIn(t, env, "ana@example.com")
	sale := saleByRef(t, readCustomerArea(t, env, token, ""), ref)
	env.email.Reset()

	if !sale.Reversible {
		t.Fatal("a paid sale the stub settled is not offered as reversible; the flow is then unreachable without PayPhone credentials")
	}

	reverseSaleOK(t, env, token, sale.ID)
	if status, _, _ := saleProvenance(t, env, ref); status != "reversed" {
		t.Fatalf("status = %q, want reversed", status)
	}
	if got := remaining(t, env, "stub-paid-fest", "GA"); got != 10 {
		t.Fatalf("remaining = %d after the undo, want 10", got)
	}
	if voided := env.email.Voided(); len(voided) != 1 {
		t.Fatalf("captured %d void notices, want exactly 1", len(voided))
	}
}

// TestReversedFreeClaimDropsOutOfTheMoneySurfaces walks the reversal's effect on
// every figure the platform reports, through the real endpoint.
//
// A free claim's contribution to each of those figures is zero cents, and that
// is not a gap in this test but the definition of a free Online Sale: a cart of
// Free Ticket Types totals nothing, so it yields no Net Proceeds, no Platform
// Fee and no Fee IVA to begin with (ADR 0014, ADR 0017). What must therefore be
// true after the undo is that all three money figures are EXACTLY the surviving
// paid sale's — unchanged to the cent, with no reversed contribution added or
// subtracted — while the one figure a free claim did move, the Event's sales
// count, falls by exactly one.
//
// The paid sale in the fixture is what gives those figures a value worth
// comparing: a test where everything is zero would pass with the aggregation
// removed entirely.
func TestReversedFreeClaimDropsOutOfTheMoneySurfaces(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, paidID := publishEventStarting(t, env, sessionID, "Mixed Fest", "mixed-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 20)
	freeID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "Free GA", 0, 20)

	buyOnline(t, env, "mixed-fest", paidID, "bruno@example.com")
	ref := claimFree(t, env, "mixed-fest", freeID, "ana@example.com", 2)

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	before := moneySurfaces(t, env, sessionID, operatorSessionID, eventID)
	if before.summary.SalesCount != 2 {
		t.Fatalf("sales_count before = %d, want the paid sale and the free claim", before.summary.SalesCount)
	}
	// Under pass-on Fee Handling the Organization nets exactly the price it set,
	// and the platform earns the fee and its IVA — all of it from the paid sale.
	if before.summary.NetProceedsCents != feeTestBaseCents {
		t.Fatalf("net proceeds before = %d, want %d from the paid sale alone",
			before.summary.NetProceedsCents, feeTestBaseCents)
	}
	if before.platformFeeCents != feeTestFeeCents || before.feeIVACents != feeTestIVACents {
		t.Fatalf("platform revenue before = fee %d, iva %d; want %d and %d",
			before.platformFeeCents, before.feeIVACents, feeTestFeeCents, feeTestIVACents)
	}

	token := customerSignIn(t, env, "ana@example.com")
	saleID := saleByRef(t, readCustomerArea(t, env, token, ""), ref).ID
	reverseSaleOK(t, env, token, saleID)

	after := moneySurfaces(t, env, sessionID, operatorSessionID, eventID)
	if after.summary.SalesCount != 1 {
		t.Fatalf("sales_count after the undo = %d, want 1 — the reversed claim must stop counting",
			after.summary.SalesCount)
	}
	// Each figure falls by the reversed sale's own contribution, which for a free
	// claim is zero cents — so each must equal the paid sale's contribution and
	// nothing else.
	if after.summary.NetProceedsCents != feeTestBaseCents {
		t.Fatalf("net proceeds after = %d, want %d", after.summary.NetProceedsCents, feeTestBaseCents)
	}
	if after.balanceCents != before.balanceCents || after.balanceCents != feeTestBaseCents {
		t.Fatalf("withdrawable balance after = %d (before %d), want %d",
			after.balanceCents, before.balanceCents, feeTestBaseCents)
	}
	if after.platformFeeCents != feeTestFeeCents || after.feeIVACents != feeTestIVACents {
		t.Fatalf("platform revenue after = fee %d, iva %d; want %d and %d",
			after.platformFeeCents, after.feeIVACents, feeTestFeeCents, feeTestIVACents)
	}
	if after.totalOwedCents != feeTestBaseCents {
		t.Fatalf("platform total owed after = %d, want the surviving sale's Net Proceeds %d",
			after.totalOwedCents, feeTestBaseCents)
	}

	// The tickets are back on sale, which is the reversal's visible effect on an
	// Event whose free tier is the thing people actually queue for.
	if free := publicTicketTypes(t, env, testOrgSlug, "mixed-fest")["Free GA"]; free.Remaining != 20 {
		t.Fatalf("free tier remaining = %d after the undo, want 20", free.Remaining)
	}
}

// moneySurfaces reads all three money surfaces at one instant: the Event's Net
// Proceeds strip, the Organization's Withdrawable Balance, and the Operator
// Dashboard's platform revenue.
type moneyReading struct {
	summary          salesSummary
	balanceCents     int
	platformFeeCents int
	feeIVACents      int
	totalOwedCents   int
	// The part of the two figures above that comes from fees the platform kept
	// on Operator Reversals (#127). Zero on every route a Customer or a Member
	// can take, which is what the reversal tests around this helper assert.
	keptFeeCents    int
	keptFeeIVACents int
}

func moneySurfaces(t *testing.T, env *testEnv, staffSession, operatorSessionID, eventID string) moneyReading {
	t.Helper()
	var summary operatorSummary
	operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/summary", &summary)
	if len(summary.Totals) != 1 {
		t.Fatalf("operator totals = %+v, want one currency row", summary.Totals)
	}
	return moneyReading{
		summary:          salesSummaryOK(t, env, staffSession, eventID),
		balanceCents:     getPayouts(t, env, staffSession).WithdrawableBalanceCents,
		platformFeeCents: summary.Totals[0].PlatformFeeCents,
		feeIVACents:      summary.Totals[0].FeeIVACents,
		totalOwedCents:   summary.Totals[0].TotalOwedCents,
		keptFeeCents:     summary.Totals[0].KeptFeeCents,
		keptFeeIVACents:  summary.Totals[0].KeptFeeIVACents,
	}
}
