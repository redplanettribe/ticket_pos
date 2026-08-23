package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// A SALE CORRECTION (#351, parent #349, ADR 0050): one imported Ticket Sale is
// reversed and a replacement recorded in the same act, each pointing at the
// other. The replacement is a fresh sale in every respect — no batch, fresh
// unassigned Tickets, a new Sale Confirmation reference — held to every rule an
// import row is held to, with capacity and the Purchase Limit counted NET of the
// sale being reversed. Holders on the old sale are told; the buyer is told
// nothing unless the form asks for a new Sale Confirmation.
//
// Everything is asserted at the HTTP seam: the endpoint's verdict, the Sales
// list rows, sold counts read through the staff API, and the captured inbox.

// correctSaleResult mirrors POST /api/v1/staff/events/{id}/sales/{saleId}/correct.
type correctSaleResult struct {
	ReversedSaleID             string `json:"reversed_sale_id"`
	ReversedConfirmationRef    string `json:"reversed_confirmation_ref"`
	ReplacementSaleID          string `json:"replacement_sale_id"`
	ReplacementConfirmationRef string `json:"replacement_confirmation_ref"`
	ConfirmationSent           bool   `json:"confirmation_sent"`
}

func correctImportedSale(t *testing.T, env *testEnv, sessionID, eventID, saleID string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, "/api/v1/staff/events/"+eventID+"/sales/"+saleID+"/correct", body, authHeader(sessionID))
}

func correctImportedSaleOK(t *testing.T, env *testEnv, sessionID, eventID, saleID string, body map[string]any) correctSaleResult {
	t.Helper()
	resp, env2 := correctImportedSale(t, env, sessionID, eventID, saleID, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("correct sale status=%d error=%+v", resp.StatusCode, env2.Error)
	}
	var out correctSaleResult
	if err := json.Unmarshal(env2.Data, &out); err != nil {
		t.Fatalf("decode correct result: %v", err)
	}
	return out
}

// correctionBody is the template's columns as the form sends them, for one row.
func correctionBody(email, first, last, ttID string, quantity int, method, soldAt string) map[string]any {
	return map[string]any{
		"customer_email":      email,
		"customer_first_name": first,
		"customer_last_name":  last,
		"ticket_type_id":      ttID,
		"quantity":            quantity,
		"payment_method":      method,
		"sold_at":             soldAt,
	}
}

func correctionFieldErrors(t *testing.T, body envelope) map[string]string {
	t.Helper()
	if body.Error == nil {
		t.Fatal("no error in the refusal")
	}
	raw, _ := json.Marshal(body.Error.Details)
	var details platform.ValidationErrorDetails
	if err := json.Unmarshal(raw, &details); err != nil {
		t.Fatalf("decode details: %v", err)
	}
	out := map[string]string{}
	for _, f := range details.Fields {
		out[f.Field] = f.Message
	}
	return out
}

func saleRowByID(t *testing.T, env *testEnv, sessionID, eventID, saleID, status string) saleListRow {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales?status="+status+"&page_size=100", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	for _, row := range salesList(t, body).Data {
		if row.ID == saleID {
			return row
		}
	}
	t.Fatalf("no %s sale %s on the Sales list", status, saleID)
	return saleListRow{}
}

// EVERY FIELD CLASS CORRECTED AT ONCE, on a sale from an older batch: email,
// names, Tax ID, Ticket Type, quantity, Payment Method, sold-at and amount. The
// replacement is recorded under the other Customer, belongs to no batch, is
// linked both ways, and every figure moves by the difference.
func TestCorrectingAnImportedSaleRecordsALinkedReplacement(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Correct Fest", "correct-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 5000, 10)

	older := commitBatch(t, env, sessionID, eventID, "older", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	newer := commitBatch(t, env, sessionID, eventID, "newer", []map[string]any{
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": gaID, "quantity": 3, "payment_method": "transfer", "sold_at": "2026-07-02T10:00:00Z"},
	})
	env.email.Reset()

	ana := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "active")
	body := correctionBody("anna@example.com", "Anna", "López", vipID, 1, "transfer", "2026-07-03T12:00:00Z")
	body["customer_tax_id_type"] = "passport"
	body["customer_tax_id_number"] = "ab123456"
	body["amount_cents"] = 4500
	result := correctImportedSaleOK(t, env, sessionID, eventID, ana.ID, body)

	if result.ReversedSaleID != ana.ID || result.ReversedConfirmationRef != ana.ConfirmationRef {
		t.Errorf("result names reversed sale %s/%s, want Ana's %s/%s", result.ReversedSaleID, result.ReversedConfirmationRef, ana.ID, ana.ConfirmationRef)
	}
	if result.ReplacementSaleID == "" || result.ReplacementSaleID == ana.ID || result.ReplacementConfirmationRef == "" || result.ReplacementConfirmationRef == ana.ConfirmationRef {
		t.Errorf("replacement = %s/%s, want a fresh sale with a fresh reference", result.ReplacementSaleID, result.ReplacementConfirmationRef)
	}
	if result.ConfirmationSent {
		t.Error("confirmation_sent = true with send_confirmation unset, want false")
	}

	// THE OLD ROW READS "CORRECTED → TP-NEW", reversed by staff, no note.
	old := saleRowByID(t, env, sessionID, eventID, ana.ID, "reversed")
	if old.ReversedBy == nil || *old.ReversedBy != "staff" || old.ReversedAt == nil {
		t.Errorf("old row reversed_by=%v reversed_at=%v, want staff / set", old.ReversedBy, old.ReversedAt)
	}
	if old.ReplacedBySaleID == nil || *old.ReplacedBySaleID != result.ReplacementSaleID {
		t.Errorf("old row replaced_by_sale_id = %v, want %s", old.ReplacedBySaleID, result.ReplacementSaleID)
	}
	if old.ReplacedByConfirmationRef == nil || *old.ReplacedByConfirmationRef != result.ReplacementConfirmationRef {
		t.Errorf("old row replaced_by_confirmation_ref = %v, want %s", old.ReplacedByConfirmationRef, result.ReplacementConfirmationRef)
	}
	if old.ReplacesSaleID != nil {
		t.Errorf("old row replaces_sale_id = %v, want null", old.ReplacesSaleID)
	}
	// Its snapshot is untouched: the mistaken record stays what it was.
	if old.CustomerEmail != "ana@example.com" || old.CustomerFirstName != "Ana" || old.AmountCents != 2000 {
		t.Errorf("old row changed under the correction: %+v", old)
	}

	// THE NEW ROW READS "CORRECTS TP-OLD", carries every corrected value, and
	// belongs to no batch.
	repl := saleRowByID(t, env, sessionID, eventID, result.ReplacementSaleID, "active")
	if repl.ReplacesSaleID == nil || *repl.ReplacesSaleID != ana.ID || repl.ReplacesConfirmationRef == nil || *repl.ReplacesConfirmationRef != ana.ConfirmationRef {
		t.Errorf("replacement row replaces = %v / %v, want %s / %s", repl.ReplacesSaleID, repl.ReplacesConfirmationRef, ana.ID, ana.ConfirmationRef)
	}
	if repl.ReplacedBySaleID != nil {
		t.Errorf("replacement row replaced_by_sale_id = %v, want null", repl.ReplacedBySaleID)
	}
	if repl.CustomerEmail != "anna@example.com" || repl.CustomerFirstName != "Anna" || repl.CustomerLastName != "López" {
		t.Errorf("replacement buyer = %s %s <%s>", repl.CustomerFirstName, repl.CustomerLastName, repl.CustomerEmail)
	}
	if repl.TaxIDType == nil || *repl.TaxIDType != "passport" || repl.TaxIDNumber == nil || *repl.TaxIDNumber != "AB123456" {
		t.Errorf("replacement Tax ID = %v %v, want passport AB123456 (normalised)", repl.TaxIDType, repl.TaxIDNumber)
	}
	if len(repl.TicketTypes) != 1 || repl.TicketTypes[0].TicketTypeName != "VIP" || repl.TicketTypes[0].Quantity != 1 {
		t.Errorf("replacement lines = %+v, want 1 × VIP", repl.TicketTypes)
	}
	soldAt, err := time.Parse(time.RFC3339, repl.SoldAt)
	if err != nil || !soldAt.Equal(time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("replacement sold_at = %s (err %v), want 2026-07-03T12:00:00Z", repl.SoldAt, err)
	}
	if repl.AmountCents != 4500 || repl.PaymentMethod == nil || *repl.PaymentMethod != "transfer" {
		t.Errorf("replacement amount=%d method=%v, want 4500 / transfer", repl.AmountCents, repl.PaymentMethod)
	}
	if repl.Channel != "import" || repl.Source == nil || *repl.Source != "direct" {
		t.Errorf("replacement channel=%s source=%v, want import/direct", repl.Channel, repl.Source)
	}
	var batchID *string
	var oldCustomer, newCustomer string
	if err := env.db.QueryRow(`SELECT import_batch_id FROM ticket_sales WHERE id = $1`, result.ReplacementSaleID).Scan(&batchID); err != nil {
		t.Fatalf("read the replacement: %v", err)
	}
	if batchID != nil {
		t.Errorf("replacement import_batch_id = %v, want null — it belongs to no batch", *batchID)
	}
	if err := env.db.QueryRow(`SELECT a.customer_id, b.customer_id FROM ticket_sales a, ticket_sales b WHERE a.id = $1 AND b.id = $2`, ana.ID, result.ReplacementSaleID).Scan(&oldCustomer, &newCustomer); err != nil {
		t.Fatalf("read the customers: %v", err)
	}
	if oldCustomer == newCustomer {
		t.Error("the replacement was recorded under Ana's Customer, want the other address's")
	}

	// FRESH TICKETS, ALL UNASSIGNED.
	tickets := ticketIDsOfSale(t, env, result.ReplacementSaleID)
	if len(tickets) != 1 {
		t.Errorf("replacement Tickets = %d, want 1", len(tickets))
	}
	var assigned int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM tickets WHERE id = ANY($1) AND (holder_email IS NOT NULL OR accepted_at IS NOT NULL)`, tickets).Scan(&assigned); err == nil && assigned != 0 {
		t.Errorf("%d replacement Tickets carry an assignment, want 0", assigned)
	}

	// THE FIGURES MOVE BY THE DIFFERENCE, and the batches do not.
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 3 {
		t.Errorf("GA sold_count = %d, want Bob's 3", got)
	}
	if got := soldCount(t, env, sessionID, eventID, vipID); got != 1 {
		t.Errorf("VIP sold_count = %d, want 1", got)
	}
	if got := importHistoryStatus(t, env, sessionID, eventID); got[older] != "committed" || got[newer] != "committed" {
		t.Errorf("batch statuses = %v, want both committed", got)
	}

	// THE BUYER IS MAILED NOTHING BY DEFAULT.
	if n := len(env.email.Confirmations()); n != 0 {
		t.Errorf("Sale Confirmations = %d, want 0 without send_confirmation", n)
	}
	if n := len(env.email.Voided()); n != 0 {
		t.Errorf("voided mails = %d, want 0 — never a voided mail on a correction", n)
	}
}

// THE SAME BUYER, CORRECTED TWICE: a correction chain is walkable from either
// end, and a corrected sale cannot be corrected again.
func TestACorrectedSaleCannotBeCorrectedAgainButItsReplacementCan(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Chain Fest", "chain-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	commitBatch(t, env, sessionID, eventID, "chain", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	ana := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "active")

	first := correctImportedSaleOK(t, env, sessionID, eventID, ana.ID,
		correctionBody("ana@example.com", "Ana", "Lopez", gaID, 3, "cash", "2026-07-01T10:00:00Z"))
	resp, body := correctImportedSale(t, env, sessionID, eventID, ana.ID,
		correctionBody("ana@example.com", "Ana", "Lopez", gaID, 4, "cash", "2026-07-01T10:00:00Z"))
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "SALE_ALREADY_REVERSED" {
		t.Errorf("second correction of the old sale: status=%d error=%+v, want 409 SALE_ALREADY_REVERSED", resp.StatusCode, body.Error)
	}
	second := correctImportedSaleOK(t, env, sessionID, eventID, first.ReplacementSaleID,
		correctionBody("ana@example.com", "Ana", "Lopez", gaID, 4, "cash", "2026-07-01T10:00:00Z"))
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 4 {
		t.Errorf("sold_count = %d, want 4 after the chain", got)
	}
	middle := saleRowByID(t, env, sessionID, eventID, first.ReplacementSaleID, "reversed")
	if middle.ReplacesSaleID == nil || *middle.ReplacesSaleID != ana.ID || middle.ReplacedBySaleID == nil || *middle.ReplacedBySaleID != second.ReplacementSaleID {
		t.Errorf("middle row links = replaces %v / replaced_by %v", middle.ReplacesSaleID, middle.ReplacedBySaleID)
	}
	if n := salesCountByEmail(t, env, eventID, "ana@example.com"); n != 1 {
		t.Errorf("Ana's active sales = %d, want 1", n)
	}
}

// A FAILED REPLACEMENT LEAVES THE ORIGINAL ACTIVE: a bad email, an unknown
// Ticket Type, a Tax ID that fails its rule, a missing Payment Method, a
// future sold-at — each refused whole, with the complaint naming the cell.
func TestAFailedCorrectionLeavesTheOriginalActive(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Fail Fest", "fail-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	commitBatch(t, env, sessionID, eventID, "fail", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	ana := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "active")
	env.email.Reset()

	cases := []struct {
		name  string
		mod   func(map[string]any)
		field string
	}{
		{"bad email", func(b map[string]any) { b["customer_email"] = "not-an-email" }, "customer_email"},
		{"blank first name", func(b map[string]any) { b["customer_first_name"] = "" }, "customer_first_name"},
		{"unknown ticket type", func(b map[string]any) { b["ticket_type_id"] = unownedSaleID }, "ticket_type"},
		{"zero quantity", func(b map[string]any) { b["quantity"] = 0 }, "quantity"},
		{"missing payment method", func(b map[string]any) { b["payment_method"] = "" }, "payment_method"},
		{"unknown payment method", func(b map[string]any) { b["payment_method"] = "card" }, "payment_method"},
		{"future sold-at", func(b map[string]any) { b["sold_at"] = "2031-01-01T10:00:00Z" }, "sold_at"},
		{"tax id number without type", func(b map[string]any) { b["customer_tax_id_number"] = "1710034065" }, "customer_tax_id_type"},
		{"bad cedula", func(b map[string]any) {
			b["customer_tax_id_type"] = "cedula"
			b["customer_tax_id_number"] = "123"
		}, "customer_tax_id_number"},
	}
	for _, tc := range cases {
		body := correctionBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z")
		tc.mod(body)
		resp, env2 := correctImportedSale(t, env, sessionID, eventID, ana.ID, body)
		if resp.StatusCode != http.StatusBadRequest || env2.Error == nil || env2.Error.Code != "VALIDATION_FAILED" {
			t.Errorf("%s: status=%d error=%+v, want 400 VALIDATION_FAILED", tc.name, resp.StatusCode, env2.Error)
			continue
		}
		if fields := correctionFieldErrors(t, env2); fields[tc.field] == "" {
			t.Errorf("%s: field errors = %v, want one on %q", tc.name, fields, tc.field)
		}
	}

	// Nothing moved: the sale is active, the count is what it was, nobody was mailed.
	if status, _, _ := saleProvenance(t, env, ana.ConfirmationRef); status != "active" {
		t.Errorf("Ana's sale is %q after the refusals, want active", status)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 2 {
		t.Errorf("sold_count = %d, want 2", got)
	}
	if n := salesCountByEmail(t, env, eventID, "ana@example.com"); n != 1 {
		t.Errorf("Ana's active sales = %d, want 1", n)
	}
	if n := len(env.email.Confirmations()) + len(env.email.Voided()) + len(env.email.NoLongerHoldingsSent()); n != 0 {
		t.Errorf("%d mails sent by refused corrections, want 0", n)
	}
	var links int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM ticket_sales WHERE replaced_by_sale_id IS NOT NULL OR replaces_sale_id IS NOT NULL`).Scan(&links); err != nil || links != 0 {
		t.Errorf("%d sales carry a correction link after refusals (err=%v), want 0", links, err)
	}
}

// CAPACITY IS COUNTED NET OF THE SALE BEING REVERSED: on a full Ticket Type,
// the same quantity fits, one more does not, and the refusal leaves the
// original active.
func TestCorrectionCapacityIsNetOfTheReversedSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Full Fest", "full-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 5)
	commitBatch(t, env, sessionID, eventID, "full", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": gaID, "quantity": 3, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	ana := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "active")

	resp, body := correctImportedSale(t, env, sessionID, eventID, ana.ID,
		correctionBody("ana@example.com", "Ana", "Lopez", gaID, 3, "cash", "2026-07-01T10:00:00Z"))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("over capacity: status=%d error=%+v, want 400 VALIDATION_FAILED", resp.StatusCode, body.Error)
	}
	if fields := correctionFieldErrors(t, body); fields["quantity"] == "" {
		t.Errorf("over capacity field errors = %v, want one on quantity", fields)
	}
	if status, _, _ := saleProvenance(t, env, ana.ConfirmationRef); status != "active" {
		t.Errorf("Ana's sale is %q after the refusal, want active", status)
	}

	// The same 2 fit only because her own 2 come back first.
	result := correctImportedSaleOK(t, env, sessionID, eventID, ana.ID,
		correctionBody("ana@example.com", "Ana", "Lopez", gaID, 2, "transfer", "2026-07-01T10:00:00Z"))
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 5 {
		t.Errorf("sold_count = %d, want 5", got)
	}
	if row := saleRowByID(t, env, sessionID, eventID, result.ReplacementSaleID, "active"); row.PaymentMethod == nil || *row.PaymentMethod != "transfer" {
		t.Errorf("replacement payment method = %v, want transfer", row.PaymentMethod)
	}
}

// THE PURCHASE LIMIT IS COUNTED NET OF THE SALE BEING REVERSED for the same
// buyer, and in full for a different one.
func TestCorrectionPurchaseLimitIsNetOfTheReversedSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishRationedEvent(t, env, sessionID, "Limit Fest", "correct-limit-fest", 0, 50, 1)
	commitImportFileOK(t, env, sessionID, eventID, "limit-1",
		rationedImportFile(rationedImportRow("ana@example.com", "Ana", 1), rationedImportRow("bob@example.com", "Bob", 1)))
	ana := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "active")

	// Two for Ana is over the limit on its own.
	resp, body := correctImportedSale(t, env, sessionID, eventID, ana.ID,
		correctionBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("over limit: status=%d error=%+v, want 400 VALIDATION_FAILED", resp.StatusCode, body.Error)
	}
	if fields := correctionFieldErrors(t, body); !strings.Contains(fields["quantity"], "Purchase Limit") {
		t.Errorf("over limit field errors = %v, want a Purchase Limit complaint on quantity", fields)
	}
	// Moving the sale to Bob, who already holds his one, is over his limit.
	resp, body = correctImportedSale(t, env, sessionID, eventID, ana.ID,
		correctionBody("bob@example.com", "Bob", "Ng", gaID, 1, "cash", "2026-07-01T10:00:00Z"))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil {
		t.Fatalf("to Bob: status=%d error=%+v, want 400", resp.StatusCode, body.Error)
	}
	if fields := correctionFieldErrors(t, body); !strings.Contains(fields["quantity"], "already hold") {
		t.Errorf("to Bob field errors = %v, want a complaint about what he already holds", fields)
	}
	if status, _, _ := saleProvenance(t, env, ana.ConfirmationRef); status != "active" {
		t.Errorf("Ana's sale is %q after the refusals, want active", status)
	}
	// Ana's own one, re-recorded as one, fits: her reversed one no longer counts.
	correctImportedSaleOK(t, env, sessionID, eventID, ana.ID,
		correctionBody("ana@example.com", "Ana", "Lopez", gaID, 1, "transfer", "2026-07-01T10:00:00Z"))
	if got := salesCountByEmail(t, env, eventID, "ana@example.com"); got != 1 {
		t.Errorf("Ana's active sales = %d, want 1", got)
	}
}

// THE REPLACEMENT IS NEVER SWEPT BY A LATER BATCH UNDO, and the batch stays
// undoable for what it has left.
func TestABatchUndoNeverReversesAReplacement(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Sweep Fest", "sweep-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	batchID := commitBatch(t, env, sessionID, eventID, "sweep", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": gaID, "quantity": 3, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	ana := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "active")
	result := correctImportedSaleOK(t, env, sessionID, eventID, ana.ID,
		correctionBody("ana@example.com", "Ana", "Lopez", gaID, 1, "cash", "2026-07-01T10:00:00Z"))
	env.email.Reset()

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
		t.Errorf("undo sale_count = %d, want 1 — only Bob was left in the batch", res.SaleCount)
	}
	if row := saleRowByID(t, env, sessionID, eventID, result.ReplacementSaleID, "active"); row.Status != "active" {
		t.Errorf("the replacement is %q after the batch undo, want active", row.Status)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 1 {
		t.Errorf("sold_count = %d, want the replacement's 1", got)
	}
	voided := env.email.Voided()
	if len(voided) != 1 || voided[0].To != "bob@example.com" {
		t.Errorf("voided mails = %+v, want exactly one, to Bob", voided)
	}
}

// HOLDERS ARE TOLD, THE BUYER ONLY WHEN ASKED: with send_confirmation the
// replacement's Sale Confirmation goes to the replacement's email, carrying the
// outstanding-answers line because the fresh Tickets owe every answer.
func TestCorrectionTellsHoldersAndMailsTheBuyerOnlyWhenAsked(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)

	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))
	env.email.Reset()

	body := correctionBody("anna@example.com", "Ana", "Lopez", f.ticketTypeID, 2, "cash", "2026-07-02T10:00:00Z")
	body["send_confirmation"] = true
	result := correctImportedSaleOK(t, env, f.staffSession, f.eventID, f.anaSaleID, body)
	if !result.ConfirmationSent {
		t.Error("confirmation_sent = false with send_confirmation true")
	}

	mail := theOneNoLongerHoldingMailTo(t, env, "carla@example.com")
	assertMailGivesNoCauseAndNamesNoBuyer(t, mail, f.anaRef)
	if got := len(env.email.NoLongerHoldingsSent()); got != 1 {
		t.Errorf("No Longer Holding mails = %d, want 1", got)
	}
	if holding, _ := customerAreaOf(t, env, customerSignIn(t, env, "carla@example.com")); len(holding) != 0 {
		t.Errorf("Carla still holds %d Tickets on a corrected Sale", len(holding))
	}

	confs := env.email.Confirmations()
	if len(confs) != 1 {
		t.Fatalf("Sale Confirmations = %d, want exactly 1", len(confs))
	}
	conf := confs[0]
	if conf.To != "anna@example.com" || conf.Reference != result.ReplacementConfirmationRef || conf.EventName != "Buyer Fest" || conf.AmountCents != 4000 {
		t.Errorf("confirmation = to %s ref %s event %s amount %d, want the replacement's to anna@example.com", conf.To, conf.Reference, conf.EventName, conf.AmountCents)
	}
	if conf.ConfirmationLink == "" || strings.Contains(conf.ConfirmationLink, f.anaSaleID) {
		t.Errorf("confirmation link = %q, want a link to the replacement", conf.ConfirmationLink)
	}
	if !conf.HasOutstandingAnswers {
		t.Error("the replacement's confirmation does not carry the outstanding-answers line; its fresh Tickets owe every answer")
	}
	if n := len(env.email.Voided()); n != 0 {
		t.Errorf("voided mails = %d, want 0", n)
	}
	// Nothing carried over: the replacement's Tickets are all unassigned.
	var assigned int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM tickets tk JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id WHERE l.ticket_sale_id = $1 AND tk.holder_email IS NOT NULL`, result.ReplacementSaleID).Scan(&assigned); err != nil {
		t.Fatalf("read replacement tickets: %v", err)
	}
	if assigned != 0 {
		t.Errorf("%d replacement Tickets are assigned, want 0", assigned)
	}
}

// WORKS AFTER THE EVENT HAS ENDED.
func TestCorrectionWorksAfterTheEventHasEnded(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Past Fest", "past-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	commitBatch(t, env, sessionID, eventID, "past", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	if _, err := env.db.Exec(`UPDATE events SET starts_at = $1, ends_at = $2 WHERE id = $3`,
		env.fixedClock.AddDate(0, 0, -10), env.fixedClock.AddDate(0, 0, -9), eventID); err != nil {
		t.Fatalf("end the event: %v", err)
	}
	ana := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "active")
	result := correctImportedSaleOK(t, env, sessionID, eventID, ana.ID,
		correctionBody("ana@example.com", "Ana", "Lopez", gaID, 1, "cash", "2026-07-01T10:00:00Z"))
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 1 {
		t.Errorf("sold_count = %d, want 1", got)
	}
	if row := saleRowByID(t, env, sessionID, eventID, result.ReplacementSaleID, "active"); row.ReplacesSaleID == nil {
		t.Error("replacement carries no replaces_sale_id")
	}
}

// THE REFUSALS: an Online Sale, Event Staff, a sale that is not there, and an
// already-reversed sale — none of them records anything.
func TestCorrectingAnImportedSaleIsRefusedWhereItMustBe(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	ref := claimFreeOnlineSale(t, env, sessionID, "Online Fest", "correct-online-fest", "ana@example.com", 1)
	var onlineEventID, onlineSaleID, onlineTT string
	if err := env.db.QueryRow(`SELECT ts.event_id, ts.id, l.ticket_type_id FROM ticket_sales ts JOIN ticket_sale_lines l ON l.ticket_sale_id = ts.id WHERE ts.confirmation_ref = $1`, ref).Scan(&onlineEventID, &onlineSaleID, &onlineTT); err != nil {
		t.Fatalf("read the online sale: %v", err)
	}
	resp, body := correctImportedSale(t, env, sessionID, onlineEventID, onlineSaleID,
		correctionBody("ana@example.com", "Ana", "Lopez", onlineTT, 1, "cash", "2026-07-01T10:00:00Z"))
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "SALE_NOT_IMPORTED" {
		t.Errorf("online sale: status=%d error=%+v, want 409 SALE_NOT_IMPORTED", resp.StatusCode, body.Error)
	}
	if status, _, _ := saleProvenance(t, env, ref); status != "active" {
		t.Errorf("the online sale is %q after the refusal, want active", status)
	}

	eventID := createDraftEvent(t, env, sessionID, "Refuse Fest", "refuse-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	commitBatch(t, env, sessionID, eventID, "refuse", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	ana := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "active")
	good := correctionBody("ana@example.com", "Ana", "Lopez", gaID, 1, "cash", "2026-07-01T10:00:00Z")

	resp, body = env.post(t, "/api/v1/staff/members", map[string]string{"email": "staff@example.com", "role": "event_staff"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}
	staffSession := verifyOTP(t, env, "staff@example.com")
	resp, body = correctImportedSale(t, env, staffSession, eventID, ana.ID, good)
	if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Errorf("event staff: status=%d error=%+v, want 403 FORBIDDEN", resp.StatusCode, body.Error)
	}

	resp, body = correctImportedSale(t, env, sessionID, eventID, unownedSaleID, good)
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "TICKET_SALE_NOT_FOUND" {
		t.Errorf("unknown sale: status=%d error=%+v, want 404 TICKET_SALE_NOT_FOUND", resp.StatusCode, body.Error)
	}
	resp, body = correctImportedSale(t, env, sessionID, eventID, "not-a-uuid", good)
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "TICKET_SALE_NOT_FOUND" {
		t.Errorf("malformed id: status=%d error=%+v, want 404 TICKET_SALE_NOT_FOUND", resp.StatusCode, body.Error)
	}

	reverseImportedSaleOK(t, env, sessionID, eventID, ana.ID)
	resp, body = correctImportedSale(t, env, sessionID, eventID, ana.ID, good)
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "SALE_ALREADY_REVERSED" {
		t.Errorf("already reversed: status=%d error=%+v, want 409 SALE_ALREADY_REVERSED", resp.StatusCode, body.Error)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 0 {
		t.Errorf("sold_count = %d after the refusals, want 0 — nothing was recorded", got)
	}
	if n := salesCountByEmail(t, env, eventID, "ana@example.com"); n != 0 {
		t.Errorf("Ana's active sales = %d, want 0", n)
	}
}

// THE PREVIEW (#352). The Correct form asks, as the Member edits, what the
// commit would say: the same single-row verdict the Sale Import preview gives a
// row, computed net of the sale being corrected, plus the duplicate-of-an-active
// sale warning with the sale being corrected left out of the comparison. It
// writes nothing and is gated like the commit.

// previewResult mirrors the import preview's ValidateResult, which the
// correction preview reuses whole: one row, its verdict, the capacity impact.
type previewResult struct {
	Rows []struct {
		Valid             bool   `json:"valid"`
		PossibleDuplicate bool   `json:"possible_duplicate"`
		DuplicateOfDate   string `json:"duplicate_of_date"`
		TicketTypeName    string `json:"ticket_type_name"`
		Errors            []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"errors"`
	} `json:"rows"`
	CapacityImpact []struct {
		TicketTypeID string `json:"ticket_type_id"`
		Requested    int    `json:"requested"`
		Remaining    int    `json:"remaining"`
		Oversold     bool   `json:"oversold"`
	} `json:"capacity_impact"`
	Committable bool `json:"committable"`
}

func previewCorrection(t *testing.T, env *testEnv, sessionID, eventID, saleID string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, "/api/v1/staff/events/"+eventID+"/sales/"+saleID+"/correct/preview", body, authHeader(sessionID))
}

func previewCorrectionOK(t *testing.T, env *testEnv, sessionID, eventID, saleID string, body map[string]any) previewResult {
	t.Helper()
	resp, env2 := previewCorrection(t, env, sessionID, eventID, saleID, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status=%d error=%+v", resp.StatusCode, env2.Error)
	}
	var out previewResult
	if err := json.Unmarshal(env2.Data, &out); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if len(out.Rows) != 1 {
		t.Fatalf("preview rows = %d, want exactly one", len(out.Rows))
	}
	return out
}

func (p previewResult) errorOn(field string) string {
	for _, e := range p.Rows[0].Errors {
		if e.Field == field {
			return e.Message
		}
	}
	return ""
}

// THE PREVIEW'S VERDICTS ARE THE COMMIT'S, NET OF THE SALE BEING CORRECTED: the
// same quantity on a full Ticket Type is not an oversell, one more is; the
// Ticket Type, Tax ID and Purchase Limit rules speak through the row's errors;
// and nothing is written by any of it.
func TestCorrectionPreviewJudgesTheReplacementNetOfTheSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Preview Fest", "preview-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 5)
	commitBatch(t, env, sessionID, eventID, "preview", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": gaID, "quantity": 3, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	ana := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "active")

	// Her own two, re-submitted: they fit because they are hers.
	same := previewCorrectionOK(t, env, sessionID, eventID, ana.ID,
		correctionBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	if !same.Rows[0].Valid || !same.Committable {
		t.Errorf("same quantity: valid=%v committable=%v errors=%v, want a clean verdict", same.Rows[0].Valid, same.Committable, same.Rows[0].Errors)
	}
	if len(same.CapacityImpact) != 1 || same.CapacityImpact[0].Remaining != 2 || same.CapacityImpact[0].Oversold {
		t.Errorf("capacity impact = %+v, want 2 remaining once her sale is reversed, not oversold", same.CapacityImpact)
	}

	// Three is one more than the Event has left once hers come back.
	over := previewCorrectionOK(t, env, sessionID, eventID, ana.ID,
		correctionBody("ana@example.com", "Ana", "Lopez", gaID, 3, "cash", "2026-07-01T10:00:00Z"))
	if over.Rows[0].Valid || over.Committable || over.errorOn("quantity") == "" {
		t.Errorf("over capacity: valid=%v committable=%v errors=%v, want a refusal on quantity", over.Rows[0].Valid, over.Committable, over.Rows[0].Errors)
	}

	// An unknown Ticket Type and a malformed Tax ID are named by column.
	bad := correctionBody("ana@example.com", "Ana", "Lopez", unownedSaleID, 1, "cash", "2026-07-01T10:00:00Z")
	bad["customer_tax_id_type"] = "cedula"
	bad["customer_tax_id_number"] = "12"
	verdict := previewCorrectionOK(t, env, sessionID, eventID, ana.ID, bad)
	if verdict.Rows[0].Valid || verdict.errorOn("ticket_type") == "" || verdict.errorOn("customer_tax_id_number") == "" {
		t.Errorf("bad row: valid=%v errors=%v, want complaints on ticket_type and customer_tax_id_number", verdict.Rows[0].Valid, verdict.Rows[0].Errors)
	}

	// Nothing moved.
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 5 {
		t.Errorf("sold_count = %d after three previews, want 5", got)
	}
	if status, _, _ := saleProvenance(t, env, ana.ConfirmationRef); status != "active" {
		t.Errorf("Ana's sale is %q after the previews, want active", status)
	}
	if n := salesCountByEmail(t, env, eventID, "ana@example.com"); n != 1 {
		t.Errorf("Ana's active sales = %d, want 1", n)
	}
}

// THE PURCHASE LIMIT SPEAKS THROUGH THE PREVIEW TOO, net of the sale's own
// holding.
func TestCorrectionPreviewCountsThePurchaseLimitNetOfTheSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishRationedEvent(t, env, sessionID, "Limit Preview", "limit-preview-fest", 0, 50, 1)
	commitImportFileOK(t, env, sessionID, eventID, "limit-preview",
		rationedImportFile(rationedImportRow("ana@example.com", "Ana", 1), rationedImportRow("bob@example.com", "Bob", 1)))
	ana := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "active")

	own := previewCorrectionOK(t, env, sessionID, eventID, ana.ID,
		correctionBody("ana@example.com", "Ana", "Lopez", gaID, 1, "transfer", "2026-07-01T10:00:00Z"))
	if !own.Rows[0].Valid {
		t.Errorf("her own one: errors=%v, want valid — the reversed one no longer counts", own.Rows[0].Errors)
	}
	toBob := previewCorrectionOK(t, env, sessionID, eventID, ana.ID,
		correctionBody("bob@example.com", "Bob", "Ng", gaID, 1, "cash", "2026-07-01T10:00:00Z"))
	if toBob.Rows[0].Valid || !strings.Contains(toBob.errorOn("quantity"), "already hold") {
		t.Errorf("to Bob: valid=%v errors=%v, want a Purchase Limit complaint on quantity", toBob.Rows[0].Valid, toBob.Rows[0].Errors)
	}
}

// THE DUPLICATE WARNING names another active sale the replacement would match
// on (email, Ticket Type, sold-at date) — never the sale being corrected, which
// is about to be reversed, and never as a refusal.
func TestCorrectionPreviewWarnsOfADuplicateButNotOfItself(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Dup Fest", "dup-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	commitBatch(t, env, sessionID, eventID, "dup", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
	})
	ana := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "active")

	// Re-submitting her own row, unchanged, matches only herself: no warning.
	self := previewCorrectionOK(t, env, sessionID, eventID, ana.ID,
		correctionBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	if self.Rows[0].PossibleDuplicate {
		t.Errorf("her own row reads as a duplicate of itself (of %s)", self.Rows[0].DuplicateOfDate)
	}

	// Moving it onto Bob's email, type and day matches his active sale.
	dup := previewCorrectionOK(t, env, sessionID, eventID, ana.ID,
		correctionBody("BOB@example.com", "Bob", "Ng", gaID, 2, "cash", "2026-07-02T15:00:00Z"))
	if !dup.Rows[0].PossibleDuplicate || dup.Rows[0].DuplicateOfDate != "2026-07-02" {
		t.Errorf("Bob's row: possible_duplicate=%v of %q, want a warning naming 2026-07-02", dup.Rows[0].PossibleDuplicate, dup.Rows[0].DuplicateOfDate)
	}
	if !dup.Rows[0].Valid || !dup.Committable {
		t.Errorf("a duplicate is a warning, not a refusal: valid=%v committable=%v", dup.Rows[0].Valid, dup.Committable)
	}

	// And the commit itself is not blocked by the warning.
	correctImportedSaleOK(t, env, sessionID, eventID, ana.ID,
		correctionBody("bob@example.com", "Bob", "Ng", gaID, 2, "cash", "2026-07-02T15:00:00Z"))
	if n := salesCountByEmail(t, env, eventID, "bob@example.com"); n != 2 {
		t.Errorf("Bob's active sales = %d, want 2", n)
	}
}

// THE PREVIEW IS GATED AND REFUSED EXACTLY LIKE THE COMMIT.
func TestCorrectionPreviewIsRefusedWhereTheCommitIs(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Preview Refuse", "preview-refuse-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	commitBatch(t, env, sessionID, eventID, "preview-refuse", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	ana := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "active")
	good := correctionBody("ana@example.com", "Ana", "Lopez", gaID, 1, "cash", "2026-07-01T10:00:00Z")

	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{"email": "staff@example.com", "role": "event_staff"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}
	staffSession := verifyOTP(t, env, "staff@example.com")
	resp, body = previewCorrection(t, env, staffSession, eventID, ana.ID, good)
	if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Errorf("event staff: status=%d error=%+v, want 403 FORBIDDEN", resp.StatusCode, body.Error)
	}
	resp, body = previewCorrection(t, env, sessionID, eventID, unownedSaleID, good)
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "TICKET_SALE_NOT_FOUND" {
		t.Errorf("unknown sale: status=%d error=%+v, want 404 TICKET_SALE_NOT_FOUND", resp.StatusCode, body.Error)
	}
	reverseImportedSaleOK(t, env, sessionID, eventID, ana.ID)
	resp, body = previewCorrection(t, env, sessionID, eventID, ana.ID, good)
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "SALE_ALREADY_REVERSED" {
		t.Errorf("already reversed: status=%d error=%+v, want 409 SALE_ALREADY_REVERSED", resp.StatusCode, body.Error)
	}
}
