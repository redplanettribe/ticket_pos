package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Reversing ONE imported Ticket Sale from the Sales list (#350, parent #349,
// ADR 0050).
//
// Until now an imported sale could only leave through its whole batch, and only
// while that batch was the latest. The single-sale reversal is the same staff
// Sale Reversal scoped to one row: the figures drop for that sale and no other,
// every accepted Holder on it is told, the buyer is told nothing, and the batch
// it came from stays exactly as undoable as it was for whatever it has left.
//
// Everything is asserted at the HTTP seam: the endpoint's verdict, the Sales
// list row, sold counts read through the staff API, and the captured inbox.

// reverseSaleResult mirrors POST /api/v1/staff/events/{id}/sales/{saleId}/reverse.
type reverseSaleResult struct {
	SaleID          string `json:"sale_id"`
	ConfirmationRef string `json:"confirmation_ref"`
	Status          string `json:"status"`
	ReversedAt      string `json:"reversed_at"`
	ReversedBy      string `json:"reversed_by"`
}

func reverseImportedSale(t *testing.T, env *testEnv, sessionID, eventID, saleID string) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, "/api/v1/staff/events/"+eventID+"/sales/"+saleID+"/reverse", nil, authHeader(sessionID))
}

func reverseImportedSaleOK(t *testing.T, env *testEnv, sessionID, eventID, saleID string) reverseSaleResult {
	t.Helper()
	resp, body := reverseImportedSale(t, env, sessionID, eventID, saleID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reverse sale status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var out reverseSaleResult
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode reverse result: %v", err)
	}
	return out
}

// saleIDByEmail finds the one Ticket Sale a buyer has on an Event, through the
// Sales list — the id the endpoint takes is the id the row shows.
func saleRowByEmail(t *testing.T, env *testEnv, sessionID, eventID, email, status string) saleListRow {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales?status="+status+"&page_size=100", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	for _, row := range salesList(t, body).Data {
		if row.CustomerEmail == email {
			return row
		}
	}
	t.Fatalf("no %s sale for %s on the Sales list", status, email)
	return saleListRow{}
}

// importHistory reads the Event's Sale Import batches, keyed by batch id.
func importHistoryStatus(t *testing.T, env *testEnv, sessionID, eventID string) map[string]string {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sale-imports", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("history status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var history []struct {
		BatchID string `json:"batch_id"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(body.Data, &history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	out := map[string]string{}
	for _, b := range history {
		out[b.BatchID] = b.Status
	}
	return out
}

func takingsCents(t *testing.T, env *testEnv, sessionID, eventID string) int {
	t.Helper()
	total := 0
	for _, day := range salesTrendsOK(t, env, sessionID, eventID).Days {
		for _, line := range day.Lines {
			total += line.TakingsCents
		}
	}
	return total
}

// A SALE FROM AN OLDER BATCH IS REVERSED ON ITS OWN, and nothing else moves.
func TestReverseOneImportedSaleFromAnOlderBatch(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Correct Fest", "correct-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	older := commitBatch(t, env, sessionID, eventID, "older", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": ttID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": ttID, "quantity": 3, "payment_method": "transfer", "sold_at": "2026-07-02T10:00:00Z"},
	})
	newer := commitBatch(t, env, sessionID, eventID, "newer", []map[string]any{
		{"customer_email": "cleo@example.com", "customer_first_name": "Cleo", "customer_last_name": "Diaz", "ticket_type_id": ttID, "quantity": 4, "payment_method": "cash", "sold_at": "2026-07-03T10:00:00Z"},
	})
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 9 {
		t.Fatalf("sold_count = %d, want 9 before the reversal", got)
	}
	takingsBefore := takingsCents(t, env, sessionID, eventID)
	env.email.Reset()

	ana := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "active")
	if ana.HeldTicketCount != 0 {
		t.Errorf("held_ticket_count = %d on a sale nobody accepted, want 0", ana.HeldTicketCount)
	}
	result := reverseImportedSaleOK(t, env, sessionID, eventID, ana.ID)
	if result.SaleID != ana.ID || result.ConfirmationRef != ana.ConfirmationRef || result.Status != "reversed" || result.ReversedBy != "staff" || result.ReversedAt == "" {
		t.Fatalf("reverse result = %+v, want Ana's sale reversed by staff", result)
	}

	// The figures drop by Ana's two tickets and her 2000 cents, and no more.
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 7 {
		t.Errorf("sold_count = %d, want 7 — only Ana's 2 returned", got)
	}
	if got := salesSummaryOK(t, env, sessionID, eventID).TicketsSold; got != 7 {
		t.Errorf("tickets_sold = %d, want 7", got)
	}
	if got := takingsCents(t, env, sessionID, eventID); got != takingsBefore-2000 {
		t.Errorf("takings = %d, want %d — Ana's 2 × 1000 dropped", got, takingsBefore-2000)
	}
	if n := salesCountByEmail(t, env, eventID, "bob@example.com"); n != 1 {
		t.Errorf("Bob's active sales = %d, want 1 — the rest of the batch is untouched", n)
	}

	// THE ROW STAYS, READING "REVERSED BY STAFF", with no correction link.
	row := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "reversed")
	if row.Status != "reversed" || row.ReversedBy == nil || *row.ReversedBy != "staff" || row.ReversedAt == nil {
		t.Errorf("reversed row = status %q reversed_by %v reversed_at %v, want reversed / staff / set", row.Status, row.ReversedBy, row.ReversedAt)
	}
	if row.ReplacedBySaleID != nil || row.ReplacesSaleID != nil {
		t.Errorf("a plainly reversed sale carries a correction link: replaced_by=%v replaces=%v", row.ReplacedBySaleID, row.ReplacesSaleID)
	}
	var note *string
	var reversedBy string
	if err := env.db.QueryRow(`SELECT reversal_note, reversed_by FROM ticket_sales WHERE id = $1`, ana.ID).Scan(&note, &reversedBy); err != nil {
		t.Fatalf("read the reversed sale: %v", err)
	}
	if note != nil || reversedBy != "staff" {
		t.Errorf("reversed sale carries note=%v reversed_by=%q, want no note and staff", note, reversedBy)
	}

	// THE BATCH IS NOT TOUCHED: older stays committed (it was never undoable,
	// being older), newer stays committed and undoable.
	if got := importHistoryStatus(t, env, sessionID, eventID); got[older] != "committed" || got[newer] != "committed" {
		t.Errorf("batch statuses = %v, want both committed", got)
	}

	// THE BUYER IS MAILED NOTHING.
	if n := len(env.email.Voided()); n != 0 {
		t.Errorf("voided mails = %d, want 0 — the buyer of a reversed imported sale is told nothing", n)
	}
}

// THE BATCH STAYS UNDOABLE FOR WHAT IT HAS LEFT, and the undo skips the sale
// already reversed on its own rather than touching it twice.
func TestBatchUndoSkipsASaleAlreadyReversedOnItsOwn(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Skip Fest", "skip-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	batchID := commitBatch(t, env, sessionID, eventID, "skip", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": ttID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": ttID, "quantity": 3, "payment_method": "transfer", "sold_at": "2026-07-02T10:00:00Z"},
		{"customer_email": "cleo@example.com", "customer_first_name": "Cleo", "customer_last_name": "Diaz", "ticket_type_id": ttID, "quantity": 4, "payment_method": "cash", "sold_at": "2026-07-03T10:00:00Z"},
	})
	ana := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "active")
	reverseImportedSaleOK(t, env, sessionID, eventID, ana.ID)
	// The clock is fixed, so Ana's own reversal is moved back an hour by hand:
	// if the batch undo re-stamped her it would read the fixed clock again.
	if _, err := env.db.Exec(`UPDATE ticket_sales SET reversed_at = reversed_at - interval '1 hour' WHERE id = $1`, ana.ID); err != nil {
		t.Fatalf("backdate Ana's reversal: %v", err)
	}
	anaReversedAt := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "reversed").ReversedAt

	// Bob's sale is moved out of the batch by hand: a sale that belongs to no
	// batch (the shape #351's replacement takes) is never swept by an undo.
	bob := saleRowByEmail(t, env, sessionID, eventID, "bob@example.com", "active")
	if _, err := env.db.Exec(`UPDATE ticket_sales SET import_batch_id = NULL WHERE id = $1`, bob.ID); err != nil {
		t.Fatalf("detach Bob's sale: %v", err)
	}
	// Cleo, still in the batch, is what the undo has left to do.
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports/"+batchID+"/undo",
		map[string]any{"notify_buyers": true}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var res undoResultBody
	if err := json.Unmarshal(body.Data, &res); err != nil {
		t.Fatalf("decode undo: %v", err)
	}
	if res.SaleCount != 1 {
		t.Errorf("undo sale_count = %d, want 1 — Ana was already reversed and Bob belongs to no batch", res.SaleCount)
	}
	if got := importHistoryStatus(t, env, sessionID, eventID)[batchID]; got != "reversed" {
		t.Errorf("batch status = %q, want reversed", got)
	}

	// Ana's reversal is the one she already had — same moment, not re-stamped.
	after := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "reversed")
	if after.ReversedAt == nil || anaReversedAt == nil || *after.ReversedAt != *anaReversedAt {
		t.Errorf("Ana's reversed_at = %v after the undo, want the original %v", after.ReversedAt, anaReversedAt)
	}
	// Bob is still active, and capacity reflects exactly his 3.
	if n := salesCountByEmail(t, env, eventID, "bob@example.com"); n != 1 {
		t.Errorf("Bob's active sales = %d, want 1 — a batch-less sale is never swept by a batch undo", n)
	}
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 3 {
		t.Errorf("sold_count = %d, want Bob's 3", got)
	}
	// Only Cleo is sent a void notice: Ana's reversal was silent and Bob's never happened.
	voided := env.email.Voided()
	if len(voided) != 1 || voided[0].To != "cleo@example.com" {
		t.Errorf("voided mails = %+v, want exactly one, to Cleo", voided)
	}
}

// THE PURCHASE LIMIT TALLY RETURNS, like every other figure.
func TestReversingAnImportedSaleReturnsThePurchaseLimit(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishRationedEvent(t, env, sessionID, "Limit Fest", "reverse-limit-fest", 0, 50, 1)

	commitImportFileOK(t, env, sessionID, eventID, "limit-1",
		rationedImportFile(rationedImportRow("ana@example.com", "Ana", 1)))
	refusedByPurchaseLimit(t, env, "reverse-limit-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))

	ana := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "active")
	reverseImportedSaleOK(t, env, sessionID, eventID, ana.ID)

	approvedRef(t, beginCheckoutSettled(t, env, testOrgSlug, "reverse-limit-fest", "",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1))))
	if got := salesCountByEmail(t, env, eventID, "ana@example.com"); got != 1 {
		t.Fatalf("active sales for Ana = %d, want 1 — the reversed import no longer counts", got)
	}
}

// EVERY ACCEPTED HOLDER IS TOLD; THE BUYER IS NOT; and the row said beforehand
// how many Holders there were to tell.
func TestReversingAnImportedSaleTellsItsHoldersAndNotItsBuyer(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)

	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))
	// Elena is named and never clicks: assigned, not a Holder, told nothing.
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "elena@example.com")
	env.email.Reset()

	before := saleRowByEmail(t, env, f.staffSession, f.eventID, "ana@example.com", "active")
	if before.HeldTicketCount != 1 {
		t.Errorf("held_ticket_count = %d, want 1 — Carla accepted, Elena did not", before.HeldTicketCount)
	}

	reverseImportedSaleOK(t, env, f.staffSession, f.eventID, f.anaSaleID)

	mail := theOneNoLongerHoldingMailTo(t, env, "carla@example.com")
	if mail.EventName != "Buyer Fest" {
		t.Errorf("mail names event %q", mail.EventName)
	}
	assertMailGivesNoCauseAndNamesNoBuyer(t, mail, f.anaRef)
	if got := len(env.email.NoLongerHoldingsSent()); got != 1 {
		t.Errorf("No Longer Holding mails = %d, want 1 — Elena never accepted", got)
	}
	if got := len(env.email.Voided()); got != 0 {
		t.Errorf("the buyer received %d voided mails, want 0", got)
	}
	if holding, _ := customerAreaOf(t, env, customerSignIn(t, env, "carla@example.com")); len(holding) != 0 {
		t.Errorf("Carla still holds %d Tickets on a reversed Sale", len(holding))
	}
	// Bruno's sale on the same Event is untouched.
	if n := salesCountByEmail(t, env, f.eventID, "bruno@example.com"); n != 1 {
		t.Errorf("Bruno's active sales = %d, want 1", n)
	}
}

// THE REFUSALS: an Online Sale, a sale already reversed, a sale that does not
// exist, and Event Staff.
func TestReversingAnImportedSaleIsRefusedWhereItMustBe(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// An Online Sale: money (or the Customer's own window) is involved.
	ref := claimFreeOnlineSale(t, env, sessionID, "Online Fest", "reverse-online-fest", "ana@example.com", 1)
	var onlineEventID, onlineSaleID string
	if err := env.db.QueryRow(`SELECT event_id, id FROM ticket_sales WHERE confirmation_ref = $1`, ref).Scan(&onlineEventID, &onlineSaleID); err != nil {
		t.Fatalf("read the online sale: %v", err)
	}
	resp, body := reverseImportedSale(t, env, sessionID, onlineEventID, onlineSaleID)
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "SALE_NOT_IMPORTED" {
		t.Errorf("online sale: status=%d error=%+v, want 409 SALE_NOT_IMPORTED", resp.StatusCode, body.Error)
	}
	if status, _, _ := saleProvenance(t, env, ref); status != "active" {
		t.Errorf("the online sale is %q after the refusal, want active", status)
	}

	eventID := createDraftEvent(t, env, sessionID, "Refuse Fest", "refuse-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	commitBatch(t, env, sessionID, eventID, "refuse", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": ttID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	ana := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "active")

	// Event Staff see the row and get no lever.
	resp, body = env.post(t, "/api/v1/staff/members", map[string]string{"email": "staff@example.com", "role": "event_staff"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}
	staffSession := verifyOTP(t, env, "staff@example.com")
	resp, body = reverseImportedSale(t, env, staffSession, eventID, ana.ID)
	if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Errorf("event staff: status=%d error=%+v, want 403 FORBIDDEN", resp.StatusCode, body.Error)
	}

	// A sale that is not there, and a sale on somebody else's Event.
	resp, body = reverseImportedSale(t, env, sessionID, eventID, unownedSaleID)
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "TICKET_SALE_NOT_FOUND" {
		t.Errorf("unknown sale: status=%d error=%+v, want 404 TICKET_SALE_NOT_FOUND", resp.StatusCode, body.Error)
	}
	resp, body = reverseImportedSale(t, env, sessionID, onlineEventID, ana.ID)
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "TICKET_SALE_NOT_FOUND" {
		t.Errorf("sale under the wrong event: status=%d error=%+v, want 404 TICKET_SALE_NOT_FOUND", resp.StatusCode, body.Error)
	}
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 2 {
		t.Fatalf("sold_count = %d after the refusals, want 2 untouched", got)
	}

	// The second press on an already-reversed sale.
	reverseImportedSaleOK(t, env, sessionID, eventID, ana.ID)
	resp, body = reverseImportedSale(t, env, sessionID, eventID, ana.ID)
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "SALE_ALREADY_REVERSED" {
		t.Errorf("second reverse: status=%d error=%+v, want 409 SALE_ALREADY_REVERSED", resp.StatusCode, body.Error)
	}
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 0 {
		t.Errorf("sold_count = %d after a double reverse, want 0 — reversed once", got)
	}
}
