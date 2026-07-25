package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// Capacity Holds derived from pending Payments (issue #85, ADR 0013): a
// pending Payment younger than the hold window IS the hold on its quantities.
// Begin-checkout and the sale-commit spine count sold + held; the public
// remaining/sold-out figures subtract live holds; committing a Payment
// converts its own hold into sold_count; pendings past the window hold
// nothing and are lazily marked 'expired' — a transition no correctness
// depends on.

// holdClocksAt moves the sales and catalog clocks — the two consumers of the
// shared hold window — to the same moment. setupTest resets both to fixedClock.
func holdClocksAt(at time.Time) {
	sharedApp.SalesService.WithClock(func() time.Time { return at })
	sharedApp.CatalogService.WithClock(func() time.Time { return at })
}

// afterHoldWindow is a moment safely past the Capacity Hold window relative to
// the fixed clock every Payment in these tests is created at.
func afterHoldWindow() time.Time {
	return fixedClock.Add(sales.CapacityHoldWindow + time.Minute)
}

type publicTicketTypeView struct {
	Name      string `json:"name"`
	Remaining int    `json:"remaining"`
	SoldOut   bool   `json:"sold_out"`
}

// publicTicketTypes reads the Storefront event page and returns its Ticket
// Types keyed by name.
func publicTicketTypes(t *testing.T, env *testEnv, orgSlug, eventSlug string) map[string]publicTicketTypeView {
	t.Helper()
	resp, body := env.get(t, "/api/v1/public/organizations/"+orgSlug+"/events/"+eventSlug, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var detail struct {
		TicketTypes []publicTicketTypeView `json:"ticket_types"`
	}
	if err := json.Unmarshal(body.Data, &detail); err != nil {
		t.Fatalf("decode public event: %v", err)
	}
	byName := make(map[string]publicTicketTypeView, len(detail.TicketTypes))
	for _, tt := range detail.TicketTypes {
		byName[tt.Name] = tt
	}
	return byName
}

// explorerSoldOut reads the global explorer and returns the sold_out flag of
// the card with the given slug.
func explorerSoldOut(t *testing.T, env *testEnv, slug string) bool {
	t.Helper()
	resp, body := env.get(t, "/api/v1/public/events", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public events status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var page struct {
		Events []publicEventCard `json:"events"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		t.Fatalf("decode explorer page: %v", err)
	}
	for _, ev := range page.Events {
		if ev.Slug == slug {
			return ev.SoldOut
		}
	}
	t.Fatalf("event %s not in explorer: %+v", slug, page.Events)
	return false
}

// paymentStatus reads a Payment's status directly. SQL because no public API
// exposes Payment state — the lazy 'expired' transition is exactly the side
// effect under test.
func paymentStatus(t *testing.T, env *testEnv, clientTransactionID string) string {
	t.Helper()
	var status string
	if err := env.db.QueryRow(`
		SELECT status FROM payments WHERE client_transaction_id = $1
	`, clientTransactionID).Scan(&status); err != nil {
		t.Fatalf("read payment status: %v", err)
	}
	return status
}

// TestBeginCheckoutRejectsWhenLiveHoldsExhaustCapacity: sold_count alone would
// allow the request, but sold + held does not — the pending Payment's hold is
// real capacity the next buyer cannot claim.
func TestBeginCheckoutRejectsWhenLiveHoldsExhaustCapacity(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Held Fest", "held-fest", 1000, 5)

	// 2 of 5 actually sold through the import channel.
	commitBatch(t, env, sessionID, eventID, "seed-sold", []map[string]any{
		{"customer_email": "early@example.com", "customer_first_name": "Eva", "customer_last_name": "Early",
			"ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})

	// A pending Payment holds 2 more: sold 2 + held 2 leaves 1 of 5.
	beginCheckoutOK(t, env, "test-org", "held-fest",
		checkoutBody("holder@example.com", "Hana", "Holder", map[string]any{"ticket_type_id": gaID, "quantity": 2}))

	// sold_count alone (2 of 5) would allow 2 more, but the live hold says no.
	resp, body := beginCheckout(t, env, "test-org", "held-fest",
		checkoutBody("late@example.com", "Lena", "Late", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "CAPACITY_EXCEEDED" {
		t.Fatalf("error=%+v, want CAPACITY_EXCEEDED", body.Error)
	}
	details, _ := body.Error.Details.(map[string]any)
	if details["available"] != float64(1) {
		t.Fatalf("details=%+v, want available=1 (5 capacity − 2 sold − 2 held)", details)
	}

	// The one genuinely available ticket is still sellable…
	fill := beginCheckoutOK(t, env, "test-org", "held-fest",
		checkoutBody("late@example.com", "Lena", "Late", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if fill.ClientTransactionID == "" {
		t.Fatal("expected the exact-fit begin to succeed")
	}

	// …and now sold + held equals capacity: nothing more can be begun.
	resp, body = beginCheckout(t, env, "test-org", "held-fest",
		checkoutBody("none@example.com", "Nora", "None", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "CAPACITY_EXCEEDED" {
		t.Fatalf("status=%d error=%+v, want 409 CAPACITY_EXCEEDED", resp.StatusCode, body.Error)
	}
}

// TestCapacityHoldLapsesAfterWindow: a pending Payment older than the hold
// window holds nothing — the same request that was rejected then succeeds —
// and the stale pending is lazily marked 'expired' along the way.
func TestCapacityHoldLapsesAfterWindow(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Lapse Fest", "lapse-fest", 1000, 2)

	stale := beginCheckoutOK(t, env, "test-org", "lapse-fest",
		checkoutBody("slow@example.com", "Sam", "Slow", map[string]any{"ticket_type_id": gaID, "quantity": 2}))

	// While the hold is live, the same request is rejected outright.
	resp, body := beginCheckout(t, env, "test-org", "lapse-fest",
		checkoutBody("next@example.com", "Nina", "Next", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "CAPACITY_EXCEEDED" {
		t.Fatalf("status=%d error=%+v, want 409 CAPACITY_EXCEEDED while the hold is live", resp.StatusCode, body.Error)
	}

	// Past the window the hold has lapsed: the identical request succeeds.
	holdClocksAt(afterHoldWindow())
	beginCheckoutOK(t, env, "test-org", "lapse-fest",
		checkoutBody("next@example.com", "Nina", "Next", map[string]any{"ticket_type_id": gaID, "quantity": 2}))

	// That begin also lazily expired the stale pending — bookkeeping the hold
	// arithmetic above never needed.
	if got := paymentStatus(t, env, stale.ClientTransactionID); got != "expired" {
		t.Fatalf("stale payment status = %q, want expired (lazy expiry at begin-checkout)", got)
	}
	// Nothing was ever sold by any of this.
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 0 {
		t.Fatalf("sold_count = %d, want 0", got)
	}
}

// TestPublicSurfacesSubtractLiveHolds: the Storefront event page and explorer
// cards advertise capacity net of live holds, and recover once the window
// passes — driven purely by the created_at cutoff, while the payments are
// still 'pending' in the database.
func TestPublicSurfacesSubtractLiveHolds(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Visible Fest", "visible-fest", 1000, 5)
	resp, body := env.put(t, "/api/v1/staff/events/"+eventID+"/discoverable", map[string]any{
		"discoverable": true,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set discoverable status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// A pending Payment holds 3 of the 5.
	first := beginCheckoutOK(t, env, "test-org", "visible-fest",
		checkoutBody("one@example.com", "Uma", "One", map[string]any{"ticket_type_id": gaID, "quantity": 3}))

	types := publicTicketTypes(t, env, "test-org", "visible-fest")
	if got := types["GA"]; got.Remaining != 2 || got.SoldOut {
		t.Fatalf("GA with 3 held = %+v, want remaining 2, not sold out", got)
	}
	if explorerSoldOut(t, env, "visible-fest") {
		t.Fatal("explorer card sold_out = true with 2 remaining, want false")
	}

	// A second Payment holds the rest: fully spoken for, nothing sold.
	second := beginCheckoutOK(t, env, "test-org", "visible-fest",
		checkoutBody("two@example.com", "Tara", "Two", map[string]any{"ticket_type_id": gaID, "quantity": 2}))

	types = publicTicketTypes(t, env, "test-org", "visible-fest")
	if got := types["GA"]; got.Remaining != 0 || !got.SoldOut {
		t.Fatalf("GA fully held = %+v, want remaining 0 and sold out", got)
	}
	if !explorerSoldOut(t, env, "visible-fest") {
		t.Fatal("explorer card sold_out = false while fully held, want true")
	}

	// Past the window the figures recover — while both payments still read
	// 'pending' in the database, because the cutoff in the query, not the lazy
	// status flip, is what bounds a hold.
	holdClocksAt(afterHoldWindow())
	if got := paymentStatus(t, env, first.ClientTransactionID); got != "pending" {
		t.Fatalf("first payment status = %q, want still pending (nothing has expired it)", got)
	}
	if got := paymentStatus(t, env, second.ClientTransactionID); got != "pending" {
		t.Fatalf("second payment status = %q, want still pending", got)
	}
	types = publicTicketTypes(t, env, "test-org", "visible-fest")
	if got := types["GA"]; got.Remaining != 5 || got.SoldOut {
		t.Fatalf("GA after holds lapsed = %+v, want remaining 5, not sold out", got)
	}
	if explorerSoldOut(t, env, "visible-fest") {
		t.Fatal("explorer card sold_out = true after holds lapsed, want false")
	}
}

// TestConfirmSucceedsWhenOwnHoldFillsLastCapacity: the commit-time check
// counts sold + OTHER live holds, so a Payment whose own hold is what fills
// the last capacity still commits — the hold converts into sold_count, it
// never double-counts against itself.
func TestConfirmSucceedsWhenOwnHoldFillsLastCapacity(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Exact Fest", "exact-fest", 1000, 3)

	// Two live Payments together hold the whole capacity: 2 + 1 of 3.
	first := beginCheckoutOK(t, env, "test-org", "exact-fest",
		checkoutBody("first@example.com", "Fay", "First", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	second := beginCheckoutOK(t, env, "test-org", "exact-fest",
		checkoutBody("second@example.com", "Sol", "Second", map[string]any{"ticket_type_id": gaID, "quantity": 1}))

	// Each confirm commits despite capacity being fully spoken for, because
	// each Payment's own hold is excluded from its own commit check.
	if got := confirmCheckoutOK(t, env, first.ClientTransactionID, "approved"); got.Status != "approved" {
		t.Fatalf("first confirm = %+v, want approved", got)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 2 {
		t.Fatalf("sold_count after first confirm = %d, want 2", got)
	}
	if got := confirmCheckoutOK(t, env, second.ClientTransactionID, "approved"); got.Status != "approved" {
		t.Fatalf("second confirm = %+v, want approved (its own hold fills the last seat)", got)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 3 {
		t.Fatalf("sold_count after both confirms = %d, want 3 (holds converted, never double-counted)", got)
	}

	// And the room really is gone now.
	resp, body := beginCheckout(t, env, "test-org", "exact-fest",
		checkoutBody("late@example.com", "Lars", "Late", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "CAPACITY_EXCEEDED" {
		t.Fatalf("status=%d error=%+v, want 409 CAPACITY_EXCEEDED once everything is sold", resp.StatusCode, body.Error)
	}
}

// TestImportChannelRespectsLiveHolds: the shared sale-commit spine counts live
// holds too, so a staff Sale Import cannot take the ticket a Customer is on
// the provider's payment page paying for — which is exactly what would
// otherwise manufacture an approved-without-sale incident.
func TestImportChannelRespectsLiveHolds(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Guarded Fest", "guarded-fest", 1000, 1)

	begin := beginCheckoutOK(t, env, "test-org", "guarded-fest",
		checkoutBody("payer@example.com", "Pia", "Payer", map[string]any{"ticket_type_id": gaID, "quantity": 1}))

	// The import channel is refused the held ticket.
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", map[string]any{
		"idempotency_key": "race-the-hold",
		"source":          "direct",
		"sales": []map[string]any{
			{"customer_email": "walkin@example.com", "customer_first_name": "Wes", "customer_last_name": "Walkin",
				"ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		},
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "IMPORT_BATCH_FAILED" {
		t.Fatalf("import against a live hold: status=%d error=%+v, want 409 IMPORT_BATCH_FAILED", resp.StatusCode, body.Error)
	}
	details, _ := body.Error.Details.(map[string]any)
	if details["reason"] != "CAPACITY_EXCEEDED" {
		t.Fatalf("import failure details=%+v, want reason CAPACITY_EXCEEDED", details)
	}

	// The Customer the hold protected completes their purchase.
	if got := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved"); got.Status != "approved" {
		t.Fatalf("confirm = %+v, want approved", got)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 1 {
		t.Fatalf("sold_count = %d, want 1", got)
	}
}

// TestLateConfirmLosesLastCapacityLoudly: two Payments end up competing for
// the last ticket — the second begun only after the first's hold lapsed. The
// first to confirm wins; the late confirm's provider-approved charge cannot be
// committed, which is the loudly-logged PAYMENT_APPROVED_WITHOUT_SALE path
// from the checkout ticket: the Payment ends 'approved' with no sale as the
// durable incident marker and the Customer is told to contact support.
func TestLateConfirmLosesLastCapacityLoudly(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Contested Fest", "contested-fest", 1000, 1)

	slow := beginCheckoutOK(t, env, "test-org", "contested-fest",
		checkoutBody("slow@example.com", "Sam", "Slow", map[string]any{"ticket_type_id": gaID, "quantity": 1}))

	// The hold lapses; a second buyer begins (lazily expiring the first) and
	// confirms first, taking the only ticket.
	holdClocksAt(afterHoldWindow())
	fast := beginCheckoutOK(t, env, "test-org", "contested-fest",
		checkoutBody("fast@example.com", "Fern", "Fast", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if got := confirmCheckoutOK(t, env, fast.ClientTransactionID, "approved"); got.Status != "approved" {
		t.Fatalf("fast confirm = %+v, want approved", got)
	}

	// The slow Customer's late confirm: the provider approves the charge, but
	// there is no capacity left to commit.
	resp, body := confirmCheckout(t, env, slow.ClientTransactionID, "approved")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("late confirm status=%d, want 500", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "PAYMENT_SALE_COMMIT_FAILED" {
		t.Fatalf("late confirm error=%+v, want PAYMENT_SALE_COMMIT_FAILED", body.Error)
	}

	// The durable incident marker: approved, no sale attached. (The matching
	// PAYMENT_APPROVED_WITHOUT_SALE log line is emitted on this same path.)
	status, ticketSaleID, _ := paymentRecord(t, env, slow.ClientTransactionID)
	if status != "approved" || ticketSaleID != nil {
		t.Fatalf("losing payment = %s/%v, want approved without a sale", status, ticketSaleID)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 1 {
		t.Fatalf("sold_count = %d, want 1 (only the winner's sale)", got)
	}
	if got := salesCountByEmail(t, env, eventID, "slow@example.com"); got != 0 {
		t.Fatalf("sales for the loser = %d, want 0", got)
	}

	// The incident persists across re-confirms: still no sale to report.
	resp, body = confirmCheckout(t, env, slow.ClientTransactionID, "approved")
	if resp.StatusCode != http.StatusInternalServerError || body.Error == nil || body.Error.Code != "PAYMENT_SALE_COMMIT_FAILED" {
		t.Fatalf("re-confirm status=%d error=%+v, want the recorded incident again", resp.StatusCode, body.Error)
	}
}

// TestExpiredPaymentConfirmRecordsProviderOutcome pins the expired-then-
// confirmed choice: lazy expiry released the hold but passed no verdict on the
// money, so a late confirm records the provider's real outcome — approved
// commits the sale while capacity remains (flipping expired → approved),
// declined settles it failed.
func TestExpiredPaymentConfirmRecordsProviderOutcome(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Patient Fest", "patient-fest", 1000, 5)

	slowApproved := beginCheckoutOK(t, env, "test-org", "patient-fest",
		checkoutBody("patient@example.com", "Pat", "Patient", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	slowDeclined := beginCheckoutOK(t, env, "test-org", "patient-fest",
		checkoutBody("waver@example.com", "Wen", "Waver", map[string]any{"ticket_type_id": gaID, "quantity": 1}))

	// Past the window, a fresh begin lazily expires both stale pendings.
	holdClocksAt(afterHoldWindow())
	beginCheckoutOK(t, env, "test-org", "patient-fest",
		checkoutBody("newcomer@example.com", "Noa", "New", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if got := paymentStatus(t, env, slowApproved.ClientTransactionID); got != "expired" {
		t.Fatalf("first stale payment = %q, want expired", got)
	}
	if got := paymentStatus(t, env, slowDeclined.ClientTransactionID); got != "expired" {
		t.Fatalf("second stale payment = %q, want expired", got)
	}

	// Provider approves the first: capacity remains, so the sale commits and
	// the Payment flips expired → approved.
	confirm := confirmCheckoutOK(t, env, slowApproved.ClientTransactionID, "approved")
	if confirm.Status != "approved" || confirm.ConfirmationRef == "" {
		t.Fatalf("expired-then-approved confirm = %+v, want approved with a reference", confirm)
	}
	status, ticketSaleID, _ := paymentRecord(t, env, slowApproved.ClientTransactionID)
	if status != "approved" || ticketSaleID == nil {
		t.Fatalf("payment = %s/%v, want approved with its sale", status, ticketSaleID)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 2 {
		t.Fatalf("sold_count = %d, want 2", got)
	}

	// Provider declines the second: it settles failed, selling nothing.
	declined := confirmCheckoutOK(t, env, slowDeclined.ClientTransactionID, "declined")
	if declined.Status != "failed" {
		t.Fatalf("expired-then-declined confirm = %+v, want failed", declined)
	}
	if got := paymentStatus(t, env, slowDeclined.ClientTransactionID); got != "failed" {
		t.Fatalf("declined payment status = %q, want failed", got)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 2 {
		t.Fatalf("sold_count after decline = %d, want unchanged 2", got)
	}
}
