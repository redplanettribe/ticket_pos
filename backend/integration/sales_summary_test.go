package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The organizer's side of the Platform Fee (issue #92): the Sales tab's stat
// strip. Its money question — what has this Event left the Organization after
// the platform's withholding — is answered from the per-line snapshots the sale
// froze, never from the current rates (ADR 0014). The platform's cut is never
// a number on this surface.
//
// Beside the money it counts the Event two ways, and the pair is the point:
// sales_count is checkouts, tickets_sold is Tickets Sold. One Ticket Sale of
// four tickets is 1 and 4, which is why neither figure can stand for the other.
//
// Prices are the $7.99-shaped ones from checkout_fees_test.go so the figures
// below depend on the rounding, not on round numbers.

// salesSummary mirrors GET /api/v1/staff/events/{id}/sales/summary.
type salesSummary struct {
	NetProceedsCents int    `json:"net_proceeds_cents"`
	Currency         string `json:"currency"`
	SalesCount       int    `json:"sales_count"`
	TicketsSold      int    `json:"tickets_sold"`
}

func getSalesSummary(t *testing.T, env *testEnv, sessionID, eventID string) (*http.Response, envelope) {
	t.Helper()
	return env.get(t, "/api/v1/staff/events/"+eventID+"/sales/summary", authHeader(sessionID))
}

func salesSummaryOK(t *testing.T, env *testEnv, sessionID, eventID string) salesSummary {
	t.Helper()
	resp, body := getSalesSummary(t, env, sessionID, eventID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales summary status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var out salesSummary
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode sales summary: %v", err)
	}
	return out
}

// reverseSale marks a Ticket Sale reversed directly, staging in SQL the state a
// reversal leaves behind. Both the Net Proceeds summary and the Withdrawable
// Balance must respect that status however the sale came to carry it.
//
// A Customer can now undo their own Online Sale through a real endpoint (#120),
// and undoOwnSale drives it where the sale qualifies — see the Net Proceeds test
// below. This helper stays for the sales that do NOT qualify: a purchase outside
// its Reversal Window, or one reversed by a Sale Import undo rather than by its
// buyer. Both reach the same status, and these surfaces are about the status.
func reverseSale(t *testing.T, env *testEnv, confirmationRef string) {
	t.Helper()
	res, err := env.db.Exec(`UPDATE ticket_sales SET status = 'reversed' WHERE confirmation_ref = $1`, confirmationRef)
	if err != nil {
		t.Fatalf("reverse sale: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("reverse sale affected %d rows, want 1", n)
	}
}

// TestSalesSummaryNetsThePlatformFeeOutOfOnlineSales is the pass-on figure: two
// tickets sold online net the Organization exactly the price it set, a reversed
// sale drops out entirely, and an imported cash sale adds to the count while
// contributing no Net Proceeds.
//
// It is also where the two scopes are held apart. Net Proceeds is online money
// only; Tickets Sold counts every Sales Channel, because a ticket sold at the
// door and a ticket imported from elsewhere each put a body in the room. A
// channel filter finding its way onto the ticket aggregate would fail here.
func TestSalesSummaryNetsThePlatformFeeOutOfOnlineSales(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Net Fest", "net-fest", feeTestBaseCents, 20)

	// An empty Event has nothing to show, and says so in cents rather than in
	// a missing field.
	if empty := salesSummaryOK(t, env, sessionID, eventID); empty != (salesSummary{NetProceedsCents: 0, Currency: "USD", SalesCount: 0}) {
		t.Fatalf("empty summary = %+v; want zeroes in USD", empty)
	}

	// Two tickets sold online under pass_on: the buyer paid the all-in price,
	// the Organization nets the price it set.
	begin := beginCheckoutOK(t, env, "test-org", "net-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	// A second online sale, undone by its buyer: money given back is never
	// proceeds. The undo goes through the Customer's own endpoint (#120) rather
	// than an UPDATE, so this figure is measured against a reversal the system
	// actually performed.
	reversed := beginCheckoutOK(t, env, "test-org", "net-fest",
		checkoutBody("bea@example.com", "Bea", "Ruiz", map[string]any{"ticket_type_id": gaID, "quantity": 3}))
	reversedConfirm := confirmCheckoutOK(t, env, reversed.ClientTransactionID, "approved")
	undoOwnSale(t, env, "bea@example.com", reversedConfirm.ConfirmationRef)

	// Cash at the door: the platform never held the money, so it withheld
	// nothing and the sale leaves the Net Proceeds figure alone.
	commitBatch(t, env, sessionID, eventID, "cash-batch", []map[string]any{
		{"customer_email": "caro@example.com", "customer_first_name": "Caro", "customer_last_name": "Diaz",
			"ticket_type_id": gaID, "quantity": 4, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})

	got := salesSummaryOK(t, env, sessionID, eventID)
	// Two sales, six tickets: the online pair and the imported four. The three
	// the buyer undid are in neither figure.
	want := salesSummary{NetProceedsCents: 2 * feeTestBaseCents, Currency: "USD", SalesCount: 2, TicketsSold: 6}
	if got != want {
		t.Fatalf("summary = %+v; want %+v", got, want)
	}
}

// TestSalesSummaryReadsTheSnapshotNotTheMode: an absorbed sale nets the set
// price minus the withholding, and the figure is the same subtraction over the
// stored snapshot as under pass-on — the summary never branches on Fee Handling
// and never re-derives a fee from a rate.
func TestSalesSummaryReadsTheSnapshotNotTheMode(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Absorb Net Fest", "absorb-net-fest", feeTestBaseCents, 20)
	setFeeHandling(t, env, sessionID, eventID, "Absorb Net Fest", "absorb-net-fest", "absorb")

	begin := beginCheckoutOK(t, env, "test-org", "absorb-net-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	// Flipping the Event afterwards moves nothing already recorded.
	setFeeHandling(t, env, sessionID, eventID, "Absorb Net Fest", "absorb-net-fest", "pass_on")

	got := salesSummaryOK(t, env, sessionID, eventID)
	wantNet := 2 * (feeTestBaseCents - feeTestFeeCents - feeTestIVACents)
	if got != (salesSummary{NetProceedsCents: wantNet, Currency: "USD", SalesCount: 1, TicketsSold: 2}) {
		t.Fatalf("absorb summary = %+v; want %d net over 1 sale", got, wantNet)
	}
}

// TestSalesSummaryNeverNamesThePlatformCut: the payload carries the take-home
// figure and nothing else — no fee, no IVA, no gross to subtract one from.
func TestSalesSummaryNeverNamesThePlatformCut(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Quiet Fest", "quiet-fest", feeTestBaseCents, 20)
	begin := beginCheckoutOK(t, env, "test-org", "quiet-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	_, body := getSalesSummary(t, env, sessionID, eventID)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body.Data, &fields); err != nil {
		t.Fatalf("decode summary fields: %v", err)
	}
	for _, field := range []string{"net_proceeds_cents", "currency", "sales_count", "tickets_sold"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("summary is missing %q; got %v", field, fields)
		}
	}
	if len(fields) != 4 {
		t.Fatalf("summary carries extra fields %v; the platform's cut is never a number here", fields)
	}
}

// TestSalesSummaryAccess: the Event's money is for the Org Admin and the Event
// Owner. Event Staff — hired for the door — are refused it, and a Member of
// another Organization cannot even see the Event.
func TestSalesSummaryAccess(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, _ := publishCheckoutEvent(t, env, sessionID, "Access Fest", "access-summary-fest", feeTestBaseCents, 20)

	addMember := func(email, role string) string {
		t.Helper()
		resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
			"email": email,
			"role":  role,
		}, authHeader(sessionID))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("add %s status=%d error=%+v", role, resp.StatusCode, body.Error)
		}
		return verifyOTP(t, env, email)
	}

	ownerSessionID := addMember("owner@example.com", "event_owner")
	if resp, body := getSalesSummary(t, env, ownerSessionID, eventID); resp.StatusCode != http.StatusOK {
		t.Fatalf("event owner summary status=%d error=%+v", resp.StatusCode, body.Error)
	}

	staffSessionID := addMember("doorstaff@example.com", "event_staff")
	resp, body := getSalesSummary(t, env, staffSessionID, eventID)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("event staff summary status=%d, want 403; error=%+v", resp.StatusCode, body.Error)
	}
	// Their Sales list is untouched by the refusal.
	if resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales", authHeader(staffSessionID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("event staff sales list status=%d error=%+v", resp.StatusCode, body.Error)
	}

	otherSessionID := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSessionID, "Other Org", "other-org")
	resp, body = getSalesSummary(t, env, otherSessionID, eventID)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("other-org summary status=%d, want 404; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_NOT_FOUND" {
		t.Fatalf("other-org error = %+v, want EVENT_NOT_FOUND", body.Error)
	}

	if resp, _ := env.get(t, "/api/v1/staff/events/"+eventID+"/sales/summary", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated summary status=%d, want 401", resp.StatusCode)
	}
}

// TestSalesSummaryCountsTicketsNotCheckouts is the distinction the strip exists
// to draw: one Customer, one checkout, four tickets across two Ticket Types.
// The Event made 1 sale and is expecting 4 people, and an organizer reading
// either figure for the other would be out by three.
func TestSalesSummaryCountsTicketsNotCheckouts(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Party Of Four", "party-of-four", feeTestBaseCents, 20)
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", feeTestBaseCents, 20)

	begin := beginCheckoutOK(t, env, "test-org", "party-of-four",
		checkoutBody("ana@example.com", "Ana", "Lopez",
			map[string]any{"ticket_type_id": gaID, "quantity": 3},
			map[string]any{"ticket_type_id": vipID, "quantity": 1}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	got := salesSummaryOK(t, env, sessionID, eventID)
	want := salesSummary{NetProceedsCents: 4 * feeTestBaseCents, Currency: "USD", SalesCount: 1, TicketsSold: 4}
	if got != want {
		t.Fatalf("summary = %+v; want %+v — one checkout, four tickets", got, want)
	}
}

// TestSalesSummaryReversalDropsEveryTicketOfTheSale: a Sale Reversal is always
// whole-Sale, so it takes the sale's ENTIRE quantity out of Tickets Sold rather
// than one ticket of it. The seats the reversed Customer had booked are free
// again, and the strip has to say so by the same number of seats.
func TestSalesSummaryReversalDropsEveryTicketOfTheSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Undo Fest", "undo-fest", feeTestBaseCents, 20)

	kept := beginCheckoutOK(t, env, "test-org", "undo-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	confirmCheckoutOK(t, env, kept.ClientTransactionID, "approved")

	undone := beginCheckoutOK(t, env, "test-org", "undo-fest",
		checkoutBody("bea@example.com", "Bea", "Ruiz", map[string]any{"ticket_type_id": gaID, "quantity": 5}))
	undoneConfirm := confirmCheckoutOK(t, env, undone.ClientTransactionID, "approved")

	if before := salesSummaryOK(t, env, sessionID, eventID); before.TicketsSold != 7 {
		t.Fatalf("tickets_sold before the undo = %d, want the 2 and the 5", before.TicketsSold)
	}

	undoOwnSale(t, env, "bea@example.com", undoneConfirm.ConfirmationRef)

	after := salesSummaryOK(t, env, sessionID, eventID)
	want := salesSummary{NetProceedsCents: 2 * feeTestBaseCents, Currency: "USD", SalesCount: 1, TicketsSold: 2}
	if after != want {
		t.Fatalf("summary after the undo = %+v; want %+v — all five tickets go, not one", after, want)
	}
}
