package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The organizer's side of the Platform Fee (issue #92): the Sales tab's stat
// strip. It answers one question — what has this Event left the Organization
// after the platform's withholding — from the per-line snapshots the sale
// froze, never from the current rates (ADR 0014). The platform's cut is never
// a number on this surface.
//
// Prices are the $7.99-shaped ones from checkout_fees_test.go so the figures
// below depend on the rounding, not on round numbers.

// salesSummary mirrors GET /api/v1/staff/events/{id}/sales/summary.
type salesSummary struct {
	NetProceedsCents int    `json:"net_proceeds_cents"`
	Currency         string `json:"currency"`
	SalesCount       int    `json:"sales_count"`
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

// reverseSale marks a Ticket Sale reversed directly. Online Sales have no
// reversal endpoint yet (only Sale Import batches can be undone), so the state
// a refund would leave behind is staged in SQL.
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

	// A second online sale, later reversed: refunded money is never proceeds.
	reversed := beginCheckoutOK(t, env, "test-org", "net-fest",
		checkoutBody("bea@example.com", "Bea", "Ruiz", map[string]any{"ticket_type_id": gaID, "quantity": 3}))
	reversedConfirm := confirmCheckoutOK(t, env, reversed.ClientTransactionID, "approved")
	reverseSale(t, env, reversedConfirm.ConfirmationRef)

	// Cash at the door: the platform never held the money, so it withheld
	// nothing and the sale leaves the Net Proceeds figure alone.
	commitBatch(t, env, sessionID, eventID, "cash-batch", []map[string]any{
		{"customer_email": "caro@example.com", "customer_first_name": "Caro", "customer_last_name": "Diaz",
			"ticket_type_id": gaID, "quantity": 4, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})

	got := salesSummaryOK(t, env, sessionID, eventID)
	want := salesSummary{NetProceedsCents: 2 * feeTestBaseCents, Currency: "USD", SalesCount: 2}
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
	if got != (salesSummary{NetProceedsCents: wantNet, Currency: "USD", SalesCount: 1}) {
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
	for _, field := range []string{"net_proceeds_cents", "currency", "sales_count"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("summary is missing %q; got %v", field, fields)
		}
	}
	if len(fields) != 3 {
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
