package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// A MANUALLY RECORDED SALE (#368, parent #366, ADR 0052): one Sale Import row
// typed into a form instead of uploaded in a file. It is a Direct Sale in every
// respect — the `import` Sales Channel, Sales Source `direct`, a Payment Method,
// a Sale Confirmation to the buyer, Tickets minted and capacity taken — and it
// is held to exactly the rules an import row is held to.
//
// What it is NOT is a Sale Import. It belongs to no batch, never appears in the
// Import history, and recording one must not change which batch is the latest —
// so an uploaded batch stays undoable afterwards, and undoing it walks straight
// past the hand-typed sale.
//
// Three things depart from the file import, each deliberate and each recorded in
// ADR 0052: capacity is refused at PREVIEW time on the quantity rather than
// deferred to a batch commit; the Purchase Limit IS enforced, unlike the JSON
// commit whose exemption rests on a missing per-field complaint channel that the
// preview supplies; and the buyer is ALWAYS mailed, with no toggle, because no
// prior Sale Confirmation exists to fall back on.
//
// Everything is asserted at the HTTP seam: the endpoint's verdict, the Sales
// list rows, the Import history, sold counts and Takings read through the staff
// API, and the captured inbox.

// recordedSaleResult mirrors POST /api/v1/staff/events/{id}/sales.
type recordedSaleResult struct {
	SaleID            string `json:"sale_id"`
	ConfirmationRef   string `json:"confirmation_ref"`
	CustomerEmail     string `json:"customer_email"`
	CustomerFirstName string `json:"customer_first_name"`
	CustomerLastName  string `json:"customer_last_name"`
	TicketTypeID      string `json:"ticket_type_id"`
	TicketTypeName    string `json:"ticket_type_name"`
	Quantity          int    `json:"quantity"`
	AmountCents       int    `json:"amount_cents"`
	Currency          string `json:"currency"`
	SoldAt            string `json:"sold_at"`
	ConfirmationSent  bool   `json:"confirmation_sent"`
	PossibleDuplicate bool   `json:"possible_duplicate"`
	DuplicateOfDate   string `json:"duplicate_of_date"`
}

func recordManualSale(t *testing.T, env *testEnv, sessionID, eventID string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, "/api/v1/staff/events/"+eventID+"/sales", body, authHeader(sessionID))
}

func recordManualSaleOK(t *testing.T, env *testEnv, sessionID, eventID string, body map[string]any) recordedSaleResult {
	t.Helper()
	resp, env2 := recordManualSale(t, env, sessionID, eventID, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("record sale status=%d error=%+v", resp.StatusCode, env2.Error)
	}
	if env2.Error != nil {
		t.Fatalf("record sale error = %+v, want null on success", env2.Error)
	}
	if env2.RequestID == "" {
		t.Fatal("record sale carries no request_id")
	}
	var out recordedSaleResult
	if err := json.Unmarshal(env2.Data, &out); err != nil {
		t.Fatalf("decode record result: %v", err)
	}
	return out
}

func previewManualSale(t *testing.T, env *testEnv, sessionID, eventID string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, "/api/v1/staff/events/"+eventID+"/sales/preview", body, authHeader(sessionID))
}

func previewManualSaleOK(t *testing.T, env *testEnv, sessionID, eventID string, body map[string]any) previewResult {
	t.Helper()
	resp, env2 := previewManualSale(t, env, sessionID, eventID, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status=%d error=%+v", resp.StatusCode, env2.Error)
	}
	var out previewResult
	if err := json.Unmarshal(env2.Data, &out); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if len(out.Rows) != 1 {
		t.Fatalf("preview rows = %d, want exactly one — a form types one row", len(out.Rows))
	}
	return out
}

// manualSaleBody is the Sale Import template's columns as the modal sends them.
// There is deliberately no send_confirmation and no idempotency_key: the buyer
// is always mailed, and ADR 0052 records why the create carries no key.
func manualSaleBody(email, first, last, ttID string, quantity int, method, soldAt string) map[string]any {
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

// A SALE TYPED BY HAND IS ONE BATCHLESS DIRECT SALE. The response names the
// sale and its Sale Confirmation reference; the row reads `import`/`direct` with
// no batch and no correction link; the Import history never hears of it; a
// Ticket is minted per unit, all unassigned and none self-held; capacity,
// Tickets Sold and Takings all move, Net Proceeds does not; and the buyer is
// mailed exactly once.
func TestRecordingASaleByHandRecordsOneBatchlessDirectSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Hand Fest", "hand-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	env.email.Reset()

	body := manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 3, "cash", "2026-07-01T10:00:00Z")
	body["customer_tax_id_type"] = "passport"
	body["customer_tax_id_number"] = "ab123456"
	result := recordManualSaleOK(t, env, sessionID, eventID, body)

	if result.SaleID == "" || result.ConfirmationRef == "" {
		t.Fatalf("result = %+v, want a sale id and a Sale Confirmation reference", result)
	}
	if result.CustomerEmail != "ana@example.com" || result.CustomerFirstName != "Ana" || result.CustomerLastName != "Lopez" {
		t.Errorf("result buyer = %s %s <%s>", result.CustomerFirstName, result.CustomerLastName, result.CustomerEmail)
	}
	if result.TicketTypeID != gaID || result.TicketTypeName != "GA" || result.Quantity != 3 {
		t.Errorf("result line = %s/%s × %d, want GA × 3", result.TicketTypeID, result.TicketTypeName, result.Quantity)
	}
	if result.AmountCents != 3000 || result.Currency == "" {
		t.Errorf("result amount = %d %s, want 3000 in the Organization's currency", result.AmountCents, result.Currency)
	}
	if !result.ConfirmationSent {
		t.Error("confirmation_sent = false; the buyer is always mailed (ADR 0052)")
	}
	if result.PossibleDuplicate {
		t.Errorf("possible_duplicate = true on the Event's first sale, of %q", result.DuplicateOfDate)
	}

	// THE ROW IS AN IMPORTED DIRECT SALE THAT BELONGS TO NO BATCH.
	row := saleRowByID(t, env, sessionID, eventID, result.SaleID, "active")
	if row.Channel != "import" || row.Source == nil || *row.Source != "direct" {
		t.Errorf("row channel=%s source=%v, want import/direct", row.Channel, row.Source)
	}
	if row.ConfirmationRef != result.ConfirmationRef || row.PaymentMethod == nil || *row.PaymentMethod != "cash" {
		t.Errorf("row ref=%s method=%v, want %s / cash", row.ConfirmationRef, row.PaymentMethod, result.ConfirmationRef)
	}
	if row.TaxIDType == nil || *row.TaxIDType != "passport" || row.TaxIDNumber == nil || *row.TaxIDNumber != "AB123456" {
		t.Errorf("row Tax ID = %v %v, want passport AB123456 (normalised)", row.TaxIDType, row.TaxIDNumber)
	}
	if row.ReplacesSaleID != nil || row.ReplacedBySaleID != nil {
		t.Errorf("row carries a correction link (replaces=%v replaced_by=%v); a hand-typed sale corrects nothing",
			row.ReplacesSaleID, row.ReplacedBySaleID)
	}
	soldAt, err := time.Parse(time.RFC3339, row.SoldAt)
	if err != nil || !soldAt.Equal(time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("row sold_at = %s (err %v), want the day the sale was made, not the day it was typed", row.SoldAt, err)
	}
	var batchID *string
	if err := env.db.QueryRow(`SELECT import_batch_id FROM ticket_sales WHERE id = $1`, result.SaleID).Scan(&batchID); err != nil {
		t.Fatalf("read the sale: %v", err)
	}
	if batchID != nil {
		t.Errorf("import_batch_id = %v, want null — a Manually Recorded Sale belongs to no batch", *batchID)
	}

	// THE IMPORT HISTORY IS A LIST OF FILES, AND THIS WAS NOT ONE.
	if history := importHistoryStatus(t, env, sessionID, eventID); len(history) != 0 {
		t.Errorf("Import history = %v, want empty — nothing was uploaded", history)
	}

	// ONE TICKET PER UNIT, ALL UNASSIGNED, NONE SELF-HELD.
	tickets := ticketIDsOfSale(t, env, result.SaleID)
	if len(tickets) != 3 {
		t.Errorf("Tickets minted = %d, want one per unit", len(tickets))
	}
	var touched int
	if err := env.db.QueryRow(`
		SELECT COUNT(*) FROM tickets WHERE id = ANY($1) AND (holder_email IS NOT NULL OR accepted_at IS NOT NULL)
	`, tickets).Scan(&touched); err != nil {
		t.Fatalf("read the Tickets: %v", err)
	}
	if touched != 0 {
		t.Errorf("%d Tickets carry a Holder or an acceptance, want 0 — all unassigned, none self-held", touched)
	}

	// THE FIGURES MOVE, EXCEPT THE ONE A DIRECT SALE NEVER TOUCHES.
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 3 {
		t.Errorf("sold_count = %d, want 3", got)
	}
	summary := salesSummaryOK(t, env, sessionID, eventID)
	if summary.TicketsSold != 3 || summary.SalesCount != 1 {
		t.Errorf("summary = %+v, want 1 sale of 3 tickets", summary)
	}
	if summary.NetProceedsCents != 0 {
		t.Errorf("net_proceeds_cents = %d, want 0 — a Direct Sale confers no claim on the platform (ADR 0032)", summary.NetProceedsCents)
	}
	if got := takingsCents(t, env, sessionID, eventID); got != 3000 {
		t.Errorf("Takings = %d, want the sale's full 3000 (ADR 0040)", got)
	}

	// THE BUYER IS MAILED, EXACTLY ONCE, AND NOBODY ELSE IS.
	confs := env.email.Confirmations()
	if len(confs) != 1 {
		t.Fatalf("Sale Confirmations = %d, want exactly 1", len(confs))
	}
	conf := confs[0]
	if conf.To != "ana@example.com" || conf.Reference != result.ConfirmationRef || conf.AmountCents != 3000 || conf.EventName != "Hand Fest" {
		t.Errorf("confirmation = to %s ref %s amount %d event %s", conf.To, conf.Reference, conf.AmountCents, conf.EventName)
	}
	if conf.ConfirmationLink == "" {
		t.Error("the confirmation carries no Confirmation Link; the buyer could not assign a Ticket or answer a question")
	}
	if n := len(env.email.Voided()); n != 0 {
		t.Errorf("Sale Voided mails = %d, want 0", n)
	}
}

// EVERY FIELD-LEVEL REFUSAL BLAMES ITS OWN BARE COLUMN — "quantity", never
// "rows[1].quantity", because a form has an input per column and no row index
// anywhere on it. Nothing is written by any of them.
func TestAManuallyRecordedSaleBlamesTheColumnThatIsWrong(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Blame Fest", "blame-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	env.email.Reset()

	negative := -100
	cases := []struct {
		name  string
		mod   func(map[string]any)
		field string
	}{
		{"bad email", func(b map[string]any) { b["customer_email"] = "not-an-email" }, "customer_email"},
		{"missing email", func(b map[string]any) { b["customer_email"] = "" }, "customer_email"},
		{"blank first name", func(b map[string]any) { b["customer_first_name"] = "" }, "customer_first_name"},
		{"blank last name", func(b map[string]any) { b["customer_last_name"] = "" }, "customer_last_name"},
		{"tax id number without type", func(b map[string]any) { b["customer_tax_id_number"] = "1710034065" }, "customer_tax_id_type"},
		{"tax id type without number", func(b map[string]any) { b["customer_tax_id_type"] = "cedula" }, "customer_tax_id_number"},
		{"malformed cedula", func(b map[string]any) {
			b["customer_tax_id_type"] = "cedula"
			b["customer_tax_id_number"] = "123"
		}, "customer_tax_id_number"},
		{"unknown ticket type", func(b map[string]any) { b["ticket_type_id"] = unownedSaleID }, "ticket_type"},
		{"zero quantity", func(b map[string]any) { b["quantity"] = 0 }, "quantity"},
		{"negative quantity", func(b map[string]any) { b["quantity"] = -2 }, "quantity"},
		// A spreadsheet carries a fractional quantity as text and is told so on
		// the column; a form carries it as a JSON number, and must be told the
		// same thing rather than handed an unparseable-body error naming nothing.
		{"fractional quantity", func(b map[string]any) { b["quantity"] = 1.5 }, "quantity"},
		{"missing payment method", func(b map[string]any) { b["payment_method"] = "" }, "payment_method"},
		{"unknown payment method", func(b map[string]any) { b["payment_method"] = "card" }, "payment_method"},
		{"future sale date", func(b map[string]any) { b["sold_at"] = "2031-01-01T10:00:00Z" }, "sold_at"},
		{"missing sale date", func(b map[string]any) { b["sold_at"] = "" }, "sold_at"},
		{"negative amount", func(b map[string]any) { b["amount_cents"] = &negative }, "amount"},
	}
	for _, tc := range cases {
		body := manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z")
		tc.mod(body)

		resp, env2 := recordManualSale(t, env, sessionID, eventID, body)
		if resp.StatusCode != http.StatusBadRequest || env2.Error == nil || env2.Error.Code != "VALIDATION_FAILED" {
			t.Errorf("%s: status=%d error=%+v, want 400 VALIDATION_FAILED", tc.name, resp.StatusCode, env2.Error)
			continue
		}
		fields := correctionFieldErrors(t, env2)
		if fields[tc.field] == "" {
			t.Errorf("%s: field errors = %v, want one on %q", tc.name, fields, tc.field)
		}
		for name := range fields {
			if strings.Contains(name, "rows[") {
				t.Errorf("%s: field %q carries a row prefix; a form has no row index", tc.name, name)
			}
		}

		// The preview says the same thing, on the same column, so the live
		// verdict and the refusal can never disagree. A fractional quantity is
		// the one complaint the transport raises rather than the verdict —
		// there is no row to judge once the number will not be an integer — so
		// it comes back as the same refusal both routes give it.
		presp, pbody := previewManualSale(t, env, sessionID, eventID, body)
		if presp.StatusCode == http.StatusBadRequest {
			if fields := correctionFieldErrors(t, pbody); fields[tc.field] == "" {
				t.Errorf("%s: preview refusal = %v, want one on %q", tc.name, fields, tc.field)
			}
			continue
		}
		if got := previewManualSaleOK(t, env, sessionID, eventID, body); got.Rows[0].Valid || got.errorOn(tc.field) == "" {
			t.Errorf("%s: preview valid=%v errors=%v, want the same complaint on %q", tc.name, got.Rows[0].Valid, got.Rows[0].Errors, tc.field)
		}
	}

	// Nothing was written and nobody was mailed by any of it.
	if n := ticketSaleCount(t, env, eventID); n != 0 {
		t.Errorf("Ticket Sales after %d refusals = %d, want 0", len(cases), n)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 0 {
		t.Errorf("sold_count = %d, want 0", got)
	}
	if n := len(env.email.Confirmations()); n != 0 {
		t.Errorf("Sale Confirmations = %d, want 0 — a refused sale mails nobody", n)
	}
}

// CAPACITY IS REFUSED AT PREVIEW TIME, ON THE QUANTITY, with nothing written —
// ADR 0052's departure from the file import, which defers capacity to the batch
// commit because one overage there is a property of the whole file. One typed
// row's overage is a complaint on a cell the organizer can still fix.
func TestManualSaleCapacityIsRefusedOnTheQuantityBeforeAnythingIsWritten(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Small Fest", "small-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 4)
	env.email.Reset()

	over := manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 5, "cash", "2026-07-01T10:00:00Z")

	preview := previewManualSaleOK(t, env, sessionID, eventID, over)
	if preview.Rows[0].Valid || preview.Committable || preview.errorOn("quantity") == "" {
		t.Errorf("preview: valid=%v committable=%v errors=%v, want a refusal on quantity",
			preview.Rows[0].Valid, preview.Committable, preview.Rows[0].Errors)
	}
	if len(preview.CapacityImpact) != 1 || preview.CapacityImpact[0].Remaining != 4 {
		t.Errorf("capacity impact = %+v, want the Ticket Type's 4 remaining", preview.CapacityImpact)
	}
	// THE COMPLAINT STATES THE SHORTFALL PLAINLY — "exceeds the 4 remaining on
	// GA". A Sale Correction words this one differently ("...once this sale is
	// reversed") because its snapshot has been netted; nothing is netted here,
	// so the plain reading is the true one (#367, ADR 0052).
	if got := preview.errorOn("quantity"); !strings.Contains(got, "exceeds the 4 remaining on GA") {
		t.Errorf("capacity complaint = %q, want it to name the shortfall and the Ticket Type", got)
	}
	if strings.Contains(preview.errorOn("quantity"), "reversed") {
		t.Errorf("capacity complaint = %q; nothing is being reversed on a hand-typed sale", preview.errorOn("quantity"))
	}

	resp, body := recordManualSale(t, env, sessionID, eventID, over)
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("record over capacity: status=%d error=%+v, want 400 VALIDATION_FAILED", resp.StatusCode, body.Error)
	}
	if fields := correctionFieldErrors(t, body); fields["quantity"] == "" {
		t.Errorf("over capacity field errors = %v, want one on quantity", fields)
	}
	if n := ticketSaleCount(t, env, eventID); n != 0 {
		t.Errorf("Ticket Sales after a capacity refusal = %d, want 0", n)
	}
	if n := len(env.email.Confirmations()); n != 0 {
		t.Errorf("Sale Confirmations = %d, want 0", n)
	}

	// Exactly the remaining four fit, and the next one does not.
	recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 4, "cash", "2026-07-01T10:00:00Z"))
	resp, body = recordManualSale(t, env, sessionID, eventID,
		manualSaleBody("bob@example.com", "Bob", "Ng", gaID, 1, "cash", "2026-07-01T10:00:00Z"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("one past a full Ticket Type: status=%d error=%+v, want 400", resp.StatusCode, body.Error)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 4 {
		t.Errorf("sold_count = %d, want 4", got)
	}
}

// THE PURCHASE LIMIT IS ENFORCED, on the quantity — unlike the JSON commit,
// whose documented exemption rests on there being no per-field complaint channel
// to report a refusal through. This route has one, so the reason does not
// transfer (ADR 0052).
//
// And with commit-as-you-go, the sale recorded a moment earlier in the same
// sitting counts against the limit naturally, because it is a real sale.
func TestManualSalePurchaseLimitIsRefusedOnTheQuantity(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishRationedEvent(t, env, sessionID, "Rationed Fest", "manual-rationed-fest", 1000, 50, 2)
	env.email.Reset()

	// Three at once is over the limit of two before anything is recorded.
	tooMany := manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 3, "cash", "2026-07-01T10:00:00Z")
	if got := previewManualSaleOK(t, env, sessionID, eventID, tooMany); got.Rows[0].Valid || !strings.Contains(got.errorOn("quantity"), "Purchase Limit") {
		t.Errorf("preview: valid=%v errors=%v, want a Purchase Limit complaint on quantity", got.Rows[0].Valid, got.Rows[0].Errors)
	}
	resp, body := recordManualSale(t, env, sessionID, eventID, tooMany)
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("over limit: status=%d error=%+v, want 400 VALIDATION_FAILED", resp.StatusCode, body.Error)
	}
	if fields := correctionFieldErrors(t, body); !strings.Contains(fields["quantity"], "Purchase Limit") {
		t.Errorf("over limit field errors = %v, want a Purchase Limit complaint on quantity", fields)
	}

	// THE SALE RECORDED MOMENTS EARLIER COUNTS. Two are hers; the third is not.
	recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	resp, body = recordManualSale(t, env, sessionID, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 1, "transfer", "2026-07-02T10:00:00Z"))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil {
		t.Fatalf("one over her holding: status=%d error=%+v, want 400", resp.StatusCode, body.Error)
	}
	if fields := correctionFieldErrors(t, body); !strings.Contains(fields["quantity"], "already hold") {
		t.Errorf("field errors = %v, want a complaint about what she already holds", fields)
	}
	if got := salesCountByEmail(t, env, eventID, "ana@example.com"); got != 1 {
		t.Errorf("Ana's active sales = %d, want 1 — the limit held", got)
	}

	// The limit is per Customer, so somebody else's two still fit.
	recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("bob@example.com", "Bob", "Ng", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 4 {
		t.Errorf("sold_count = %d, want 4", got)
	}
}

// THE DUPLICATE CHECK IS A WARNING AND NOT A GATE: a genuine duplicate must
// still record, because two people at the door with the same name on the same
// night is a thing that happens.
func TestManualSaleDuplicateIsWarnedAboutAndStillRecorded(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Twice Fest", "twice-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	first := recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 1, "cash", "2026-07-01T10:00:00Z"))
	if first.PossibleDuplicate {
		t.Error("the Event's first sale reads as a duplicate")
	}
	env.email.Reset()

	// The same buyer, Ticket Type and day, differing only in the hour — and, in
	// the preview, in the case of the address, which the signal ignores.
	shouting := manualSaleBody("ANA@example.com", "Ana", "Lopez", gaID, 1, "cash", "2026-07-01T18:00:00Z")
	preview := previewManualSaleOK(t, env, sessionID, eventID, shouting)
	if !preview.Rows[0].PossibleDuplicate || preview.Rows[0].DuplicateOfDate != "2026-07-01" {
		t.Errorf("preview possible_duplicate=%v of %q, want a warning naming 2026-07-01",
			preview.Rows[0].PossibleDuplicate, preview.Rows[0].DuplicateOfDate)
	}
	if !preview.Rows[0].Valid || !preview.Committable {
		t.Errorf("a duplicate is a warning, not a refusal: valid=%v committable=%v", preview.Rows[0].Valid, preview.Committable)
	}

	again := manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 1, "cash", "2026-07-01T18:00:00Z")
	second := recordManualSaleOK(t, env, sessionID, eventID, again)
	if !second.PossibleDuplicate || second.DuplicateOfDate != "2026-07-01" {
		t.Errorf("recorded sale possible_duplicate=%v of %q, want the warning carried back so the session receipt can show it",
			second.PossibleDuplicate, second.DuplicateOfDate)
	}
	if second.SaleID == first.SaleID {
		t.Error("the second record returned the first sale; there is no idempotency key here (ADR 0052)")
	}
	if n := salesCountByEmail(t, env, eventID, "ana@example.com"); n != 2 {
		t.Errorf("Ana's active sales = %d, want 2 — the warning does not block", n)
	}
	if n := len(env.email.Confirmations()); n != 1 {
		t.Errorf("Sale Confirmations = %d, want 1 for the second sale", n)
	}
}

// THE AMOUNT: blank snapshots the Ticket Type's catalog price, a supplied amount
// overrides it, and zero records a comp.
func TestManualSaleAmountSnapshotsOverridesOrComps(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Money Fest", "money-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2500, 50)

	blank := recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	if blank.AmountCents != 5000 {
		t.Errorf("blank amount = %d, want 2 × the catalog 2500", blank.AmountCents)
	}

	negotiated := manualSaleBody("bob@example.com", "Bob", "Ng", gaID, 1, "cash", "2026-07-01T10:00:00Z")
	negotiated["amount_cents"] = 4000
	override := recordManualSaleOK(t, env, sessionID, eventID, negotiated)
	if override.AmountCents != 4000 {
		t.Errorf("overridden amount = %d, want the 4000 the buyer actually paid, not the catalog 2500", override.AmountCents)
	}

	free := manualSaleBody("cleo@example.com", "Cleo", "Diaz", gaID, 1, "transfer", "2026-07-01T10:00:00Z")
	free["amount_cents"] = 0
	comp := recordManualSaleOK(t, env, sessionID, eventID, free)
	if comp.AmountCents != 0 {
		t.Errorf("comped amount = %d, want 0 — a comp is a sale at no charge, not a missing amount", comp.AmountCents)
	}

	// AND THE OVERRIDE IS THE PRICE OF ONE TICKET, not the sale's total: it
	// stands in for the Ticket Type's price on each ticket the row buys, so
	// three at 1000 is 3000.
	//
	// That was genuinely in question. Every code path read the cell as a unit
	// price, but the Sale Import spreadsheet's own prose called it "total paid",
	// and an Organizer who believed it recorded 6 tickets at 180.00 as 1080.00
	// and repaired the sale by hand. #379 settled it in favour of the per-ticket
	// reading — the sale total is derived and never stored, so a total would
	// have to be split across units by a rule the sales domain does not have,
	// and both Staff surfaces already said "Price per ticket" — and the
	// template's three strings were corrected to say so. See ADR 0053.
	//
	// This assertion is that decision, and nothing about this route may
	// reinterpret the column: ADR 0052's whole reason for sharing one validator
	// is that the same act must not mean two things depending on how it was
	// typed.
	perUnit := manualSaleBody("dana@example.com", "Dana", "Ruiz", gaID, 3, "cash", "2026-07-01T10:00:00Z")
	perUnit["amount_cents"] = 1000
	multi := recordManualSaleOK(t, env, sessionID, eventID, perUnit)
	if multi.AmountCents != 3000 {
		t.Errorf("three at an overridden 1000 = %d, want 3 × the per-ticket 1000 (#379)", multi.AmountCents)
	}

	// Each figure lands on the row and in the Event's Takings.
	if row := saleRowByID(t, env, sessionID, eventID, comp.SaleID, "active"); row.AmountCents != 0 {
		t.Errorf("comp row amount = %d, want 0", row.AmountCents)
	}
	if got := takingsCents(t, env, sessionID, eventID); got != 12000 {
		t.Errorf("Takings = %d, want 5000 + 4000 + 0 + 3000", got)
	}
}

// AN EXTERNALLY REGISTERED EVENT IS REFUSED BEFORE ANY FIELD IS JUDGED, on both
// routes. Such an Event has no Ticket Types, so every body names one that does
// not exist — and answering "unknown ticket type" would send the organizer
// hunting for a data problem that is not there (ADR 0028).
func TestManualSaleRefusedOnAnExternallyRegisteredEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := publishExternalRegistrationEvent(t, env, sessionID, "https://lu.ma/my-meetup")

	// Every other cell is wrong too, so a field-level answer would be available
	// if the guard were not first.
	body := manualSaleBody("", "", "", "00000000-0000-0000-0000-000000000000", 0, "card", "2031-01-01T10:00:00Z")

	resp, envBody := recordManualSale(t, env, sessionID, eventID, body)
	assertExternalRegistrationRefusal(t, resp, envBody, "manual sale record")
	resp, envBody = previewManualSale(t, env, sessionID, eventID, body)
	assertExternalRegistrationRefusal(t, resp, envBody, "manual sale preview")

	if n := ticketSaleCount(t, env, eventID); n != 0 {
		t.Fatalf("Ticket Sales after the refusals = %d, want 0", n)
	}
}

// RECORDING IS GATED, READING IS NOT. Event Staff see the sale on the Sales list
// and are refused both the record and the preview; an admin of another
// Organization does not have the Event at all.
func TestManualSaleIsRefusedToEventStaffAndOutsidersWhoCanStillReadTheList(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Gate Fest", "gate-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	recorded := recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	good := manualSaleBody("bob@example.com", "Bob", "Ng", gaID, 1, "cash", "2026-07-01T10:00:00Z")

	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{"email": "staff@example.com", "role": "event_staff"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}
	staffSession := verifyOTP(t, env, "staff@example.com")

	resp, body = recordManualSale(t, env, staffSession, eventID, good)
	if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Errorf("event staff record: status=%d error=%+v, want 403 FORBIDDEN", resp.StatusCode, body.Error)
	}
	resp, body = previewManualSale(t, env, staffSession, eventID, good)
	if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Errorf("event staff preview: status=%d error=%+v, want 403 FORBIDDEN", resp.StatusCode, body.Error)
	}
	// And the list they may read still shows the sale somebody else recorded.
	if row := saleRowByID(t, env, staffSession, eventID, recorded.SaleID, "active"); row.CustomerEmail != "ana@example.com" {
		t.Errorf("Event Staff read the row as %+v, want Ana's sale", row)
	}

	outsider := verifyOTP(t, env, "outsider@example.com")
	createOrganization(t, env, outsider, "Other Org", "other-org")
	resp, body = recordManualSale(t, env, outsider, eventID, good)
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "EVENT_NOT_FOUND" {
		t.Errorf("outsider record: status=%d error=%+v, want 404 EVENT_NOT_FOUND", resp.StatusCode, body.Error)
	}
	resp, body = previewManualSale(t, env, outsider, eventID, good)
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "EVENT_NOT_FOUND" {
		t.Errorf("outsider preview: status=%d error=%+v, want 404 EVENT_NOT_FOUND", resp.StatusCode, body.Error)
	}

	if n := salesCountByEmail(t, env, eventID, "bob@example.com"); n != 0 {
		t.Errorf("Bob's sales = %d, want 0 — nobody refused recorded anything", n)
	}
}

// A HAND-TYPED SALE IS FIXED THE WAY AN IMPORTED ONE IS: reversed on its own, or
// corrected by replacement. Nothing about the route it arrived by changes that.
func TestAManuallyRecordedSaleIsReversedAndCorrectedLikeAnyImportedOne(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Fix Fest", "fix-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	// Corrected by replacement: the wrong email is fixed without an edit.
	typo := recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("ana@exmaple.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	corrected := correctImportedSaleOK(t, env, sessionID, eventID, typo.SaleID,
		correctionBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	if corrected.ReversedSaleID != typo.SaleID {
		t.Errorf("correction reversed %s, want the hand-typed %s", corrected.ReversedSaleID, typo.SaleID)
	}
	repl := saleRowByID(t, env, sessionID, eventID, corrected.ReplacementSaleID, "active")
	if repl.CustomerEmail != "ana@example.com" || repl.ReplacesSaleID == nil || *repl.ReplacesSaleID != typo.SaleID {
		t.Errorf("replacement = %s replaces %v, want the typo corrected", repl.CustomerEmail, repl.ReplacesSaleID)
	}

	// Reversed on its own: a sale that should never have been recorded.
	mistake := recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("bob@example.com", "Bob", "Ng", gaID, 3, "cash", "2026-07-02T10:00:00Z"))
	reversed := reverseImportedSaleOK(t, env, sessionID, eventID, mistake.SaleID)
	if reversed.SaleID != mistake.SaleID || reversed.Status != "reversed" || reversed.ReversedBy != "staff" {
		t.Errorf("reverse result = %+v, want the hand-typed sale reversed by staff", reversed)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 2 {
		t.Errorf("sold_count = %d, want the replacement's 2 alone", got)
	}
}

// A HAND-TYPED SALE NEVER DISARMS BATCH UNDO. Batch undo is latest-only, a guard
// that exists so an undo cannot reach behind a later import — and minting a
// one-row batch per typed sale would have silently made yesterday's spreadsheet
// no longer the latest. It belongs to no batch, so the batch stays undoable, and
// the undo walks straight past it (ADR 0052).
func TestRecordingASaleByHandLeavesTheLatestBatchUndoable(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Undo Fest", "manual-undo-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	batchID := commitBatch(t, env, sessionID, eventID, "the-file", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	typed := recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("bob@example.com", "Bob", "Ng", gaID, 3, "cash", "2026-07-02T10:00:00Z"))
	if history := importHistoryStatus(t, env, sessionID, eventID); len(history) != 1 || history[batchID] != "committed" {
		t.Errorf("Import history = %v, want the one uploaded batch and nothing else", history)
	}
	env.email.Reset()

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports/"+batchID+"/undo",
		map[string]any{"notify_buyers": true}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo status=%d error=%+v — the hand-typed sale made the batch no longer the latest", resp.StatusCode, body.Error)
	}
	var res undoResultBody
	if err := json.Unmarshal(body.Data, &res); err != nil {
		t.Fatalf("decode undo: %v", err)
	}
	if res.SaleCount != 1 {
		t.Errorf("undo sale_count = %d, want 1 — only the file's own sale", res.SaleCount)
	}
	if row := saleRowByID(t, env, sessionID, eventID, typed.SaleID, "active"); row.Status != "active" {
		t.Errorf("the hand-typed sale is %q after the batch undo, want active", row.Status)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 3 {
		t.Errorf("sold_count = %d, want the hand-typed 3", got)
	}
	voided := env.email.Voided()
	if len(voided) != 1 || voided[0].To != "ana@example.com" {
		t.Errorf("Sale Voided mails = %+v, want exactly one, to the file's buyer", voided)
	}
}

// THE PREVIEW WRITES NOTHING, however often it is asked. It is the whole of the
// live verdict the modal runs as the organizer types, and it is the same verdict
// the record passes through — so what the form shows and what the save does can
// never disagree.
func TestManualSalePreviewJudgesTheRowAndWritesNothing(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Preview Hand", "preview-hand-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 10)
	env.email.Reset()

	clean := previewManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	if !clean.Rows[0].Valid || !clean.Committable || len(clean.Rows[0].Errors) != 0 {
		t.Errorf("clean row: valid=%v committable=%v errors=%v", clean.Rows[0].Valid, clean.Committable, clean.Rows[0].Errors)
	}
	if clean.Rows[0].TicketTypeName != "GA" {
		t.Errorf("preview ticket_type_name = %q, want GA resolved from the id", clean.Rows[0].TicketTypeName)
	}
	if len(clean.CapacityImpact) != 1 || clean.CapacityImpact[0].Remaining != 10 || clean.CapacityImpact[0].Requested != 2 {
		t.Errorf("capacity impact = %+v, want 2 of 10 remaining", clean.CapacityImpact)
	}

	// Several more, as a debounced form would send them.
	for i := 0; i < 3; i++ {
		previewManualSaleOK(t, env, sessionID, eventID,
			manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, i+1, "cash", "2026-07-01T10:00:00Z"))
	}
	if n := ticketSaleCount(t, env, eventID); n != 0 {
		t.Errorf("Ticket Sales after four previews = %d, want 0", n)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 0 {
		t.Errorf("sold_count = %d after four previews, want 0", got)
	}
	if n := len(env.email.Confirmations()); n != 0 {
		t.Errorf("Sale Confirmations = %d after four previews, want 0", n)
	}
}
