package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
)

// createTicketTypeWithCapacity creates a Ticket Type with an explicit price and
// capacity and returns its ID.
func createTicketTypeWithCapacity(t *testing.T, env *testEnv, sessionID, eventID, name string, priceCents, capacity int) string {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/ticket-types", map[string]any{
		"name":        name,
		"price_cents": priceCents,
		"capacity":    capacity,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var tt struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &tt); err != nil {
		t.Fatalf("decode ticket type: %v", err)
	}
	return tt.ID
}

// raiseTicketTypeCapacity updates a Ticket Type's capacity via the standard
// catalog PATCH — the inline "raise capacity" action the oversell flow relies on.
func raiseTicketTypeCapacity(t *testing.T, env *testEnv, sessionID, eventID, ticketTypeID, name string, priceCents, capacity int) {
	t.Helper()
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+ticketTypeID, map[string]any{
		"name":        name,
		"price_cents": priceCents,
		"capacity":    capacity,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("raise capacity status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// salesCountByEmail counts active Ticket Sales recorded for a buyer on an Event.
func salesCountByEmail(t *testing.T, env *testEnv, eventID, email string) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`
		SELECT COUNT(*) FROM ticket_sales
		WHERE event_id = $1 AND customer_email = $2 AND status = 'active'
	`, eventID, email).Scan(&n); err != nil {
		t.Fatalf("count sales by email: %v", err)
	}
	return n
}

// soldCount reads the current sold_count for a Ticket Type via the staff API.
func soldCount(t *testing.T, env *testEnv, sessionID, eventID, ticketTypeID string) int {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/ticket-types", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list ticket types status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var types []struct {
		ID        string `json:"id"`
		SoldCount int    `json:"sold_count"`
	}
	if err := json.Unmarshal(body.Data, &types); err != nil {
		t.Fatalf("decode ticket types: %v", err)
	}
	for _, tt := range types {
		if tt.ID == ticketTypeID {
			return tt.SoldCount
		}
	}
	t.Fatalf("ticket type %s not found in list", ticketTypeID)
	return 0
}

type importResultBody struct {
	BatchID   string `json:"batch_id"`
	SaleCount int    `json:"sale_count"`
	Status    string `json:"status"`
	Replayed  bool   `json:"replayed"`
}

func importResult(t *testing.T, body envelope) importResultBody {
	t.Helper()
	var res importResultBody
	if err := json.Unmarshal(body.Data, &res); err != nil {
		t.Fatalf("decode import result: %v", err)
	}
	return res
}

func TestDirectSaleImportRecordsSalesAndDecrementsCapacity(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Summer Fest", "summer-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	comp := 0
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", map[string]any{
		"idempotency_key": "batch-1",
		"source":          "direct",
		"sales": []map[string]any{
			{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": ttID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
			{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": ttID, "quantity": 3, "payment_method": "transfer", "sold_at": "2026-07-02T10:00:00Z", "amount_cents": &comp},
		},
	}, authHeader(sessionID))

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("import status=%d error=%+v", resp.StatusCode, body.Error)
	}
	res := importResult(t, body)
	if res.SaleCount != 2 || res.Status != "committed" || res.Replayed {
		t.Fatalf("unexpected result: %+v", res)
	}

	if got := soldCount(t, env, sessionID, eventID, ttID); got != 5 {
		t.Fatalf("sold_count = %d, want 5", got)
	}

	// Each buyer is emailed a Sale Confirmation with a reference code.
	confs := env.email.Confirmations()
	if len(confs) != 2 {
		t.Fatalf("confirmations = %d, want 2", len(confs))
	}
	seen := map[string]bool{}
	names := map[string]string{}
	for _, c := range confs {
		if c.Reference == "" {
			t.Fatalf("confirmation missing reference: %+v", c)
		}
		if c.EventName != "Summer Fest" {
			t.Fatalf("confirmation event = %q, want Summer Fest", c.EventName)
		}
		seen[c.To] = true
		names[c.To] = c.CustomerName
	}
	if !seen["ana@example.com"] || !seen["bob@example.com"] {
		t.Fatalf("confirmations not sent to both buyers: %+v", confs)
	}
	// The confirmation carries the joined "First Last" display name.
	if names["ana@example.com"] != "Ana Lopez" {
		t.Fatalf("ana confirmation name = %q, want %q", names["ana@example.com"], "Ana Lopez")
	}

	// The stored sale keeps the two name halves separate.
	var first, last string
	if err := env.db.QueryRow(`
		SELECT customer_first_name, customer_last_name FROM ticket_sales
		WHERE event_id = $1 AND customer_email = 'ana@example.com' AND status = 'active'
	`, eventID).Scan(&first, &last); err != nil {
		t.Fatalf("read stored name: %v", err)
	}
	if first != "Ana" || last != "Lopez" {
		t.Fatalf("stored name = %q / %q, want Ana / Lopez", first, last)
	}

	// Line price is snapshotted: default catalog price for Ana, 0 comp for Bob.
	var prices []int
	rows, err := env.db.Query(`
		SELECT unit_price_cents FROM ticket_sale_lines
		JOIN ticket_sales ON ticket_sales.id = ticket_sale_lines.ticket_sale_id
		WHERE ticket_sales.event_id = $1 ORDER BY unit_price_cents
	`, eventID)
	if err != nil {
		t.Fatalf("query lines: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan price: %v", err)
		}
		prices = append(prices, p)
	}
	if len(prices) != 2 || prices[0] != 0 || prices[1] != 1000 {
		t.Fatalf("unit prices = %v, want [0 1000]", prices)
	}
}

// THE AMOUNT ON A COMMITTED ROW IS THE PRICE OF ONE TICKET, not the row's
// total: 6 at 3000 is an 18000 sale, not six tickets at 500.
//
// This is the JSON commit's share of the decision on #379. The column was read
// as a unit price on every path, but the spreadsheet template said "total paid",
// and an Organizer who believed it recorded 6 tickets at 180.00 as 1080.00 and
// undid it by hand. #379 settled the meaning as per-ticket and corrected the
// template's prose; the existing coverage here could not tell the two readings
// apart, because its only explicit amount was a comp at zero, and 6 × 0 is 0
// whichever way the cell is read. A quantity of 1 pins nothing either.
func TestDirectSaleImportAmountIsThePricePerTicket(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Per Ticket Fest", "per-ticket-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "Community Senior", 5000, 50)

	commitBatch(t, env, sessionID, eventID, "per-ticket-1", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": ttID, "quantity": 6, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z", "amount_cents": 3000},
	})

	row := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "active")
	if row.AmountCents != 18000 {
		t.Errorf("sale amount = %d, want 6 × the per-ticket 3000 (#379)", row.AmountCents)
	}
	// Straight to SQL because no staff API exposes a Sale Line's snapshot price:
	// the sale total above already separates the two readings, and this pins the
	// figure the total is derived FROM, so a future change cannot keep the total
	// right by dividing on the way in.
	var unit int
	if err := env.db.QueryRow(`
		SELECT unit_price_cents FROM ticket_sale_lines
		JOIN ticket_sales ON ticket_sales.id = ticket_sale_lines.ticket_sale_id
		WHERE ticket_sales.event_id = $1
	`, eventID).Scan(&unit); err != nil {
		t.Fatalf("query line: %v", err)
	}
	if unit != 3000 {
		t.Errorf("unit_price_cents = %d, want the 3000 as typed — the cell is never divided", unit)
	}
}

func TestDirectSaleImportOversellRollsBackWholeBatch(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Small Show", "small-show")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 5)

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", map[string]any{
		"idempotency_key": "batch-oversell",
		"source":          "direct",
		"sales": []map[string]any{
			{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": ttID, "quantity": 3, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
			{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": ttID, "quantity": 4, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		},
	}, authHeader(sessionID))

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "IMPORT_BATCH_FAILED" {
		t.Fatalf("error = %+v, want IMPORT_BATCH_FAILED", body.Error)
	}
	details, _ := body.Error.Details.(map[string]any)
	if details["reason"] != "CAPACITY_EXCEEDED" {
		t.Fatalf("reason = %v, want CAPACITY_EXCEEDED", details["reason"])
	}
	if details["ticket_type_id"] != ttID {
		t.Fatalf("ticket_type_id = %v, want %s", details["ticket_type_id"], ttID)
	}

	// Nothing persisted: capacity untouched, no batch, no confirmations.
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 0 {
		t.Fatalf("sold_count = %d, want 0 after rollback", got)
	}
	if n := len(env.email.Confirmations()); n != 0 {
		t.Fatalf("confirmations = %d, want 0", n)
	}
	var batches int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM sale_import_batches WHERE event_id = $1`, eventID).Scan(&batches); err != nil {
		t.Fatalf("count batches: %v", err)
	}
	if batches != 0 {
		t.Fatalf("batches = %d, want 0 after rollback", batches)
	}
}

func TestDirectSaleImportIdempotentReplay(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Replay Fest", "replay-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	payload := map[string]any{
		"idempotency_key": "batch-replay",
		"source":          "direct",
		"sales": []map[string]any{
			{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": ttID, "quantity": 4, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		},
	}

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", payload, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first import status=%d error=%+v", resp.StatusCode, body.Error)
	}
	first := importResult(t, body)

	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", payload, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replay status=%d error=%+v", resp.StatusCode, body.Error)
	}
	second := importResult(t, body)
	if !second.Replayed || second.BatchID != first.BatchID {
		t.Fatalf("replay result = %+v, want replayed with batch %s", second, first.BatchID)
	}

	// Replay records nothing new.
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 4 {
		t.Fatalf("sold_count = %d, want 4 after replay", got)
	}
	if n := len(env.email.Confirmations()); n != 1 {
		t.Fatalf("confirmations = %d, want 1 (no resend on replay)", n)
	}
}

func TestDirectSaleImportUnknownTicketType(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Ghost Fest", "ghost-fest")
	realTT := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", map[string]any{
		"idempotency_key": "batch-unknown",
		"source":          "direct",
		"sales": []map[string]any{
			{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": "c0000000-0000-4000-8000-000000000099", "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		},
	}, authHeader(sessionID))

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "TICKET_TYPE_NOT_FOUND" {
		t.Fatalf("error = %+v, want TICKET_TYPE_NOT_FOUND", body.Error)
	}
	if got := soldCount(t, env, sessionID, eventID, realTT); got != 0 {
		t.Fatalf("sold_count = %d, want 0", got)
	}
}

func TestDirectSaleImportValidationErrors(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Bad Data Fest", "bad-data-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", map[string]any{
		"idempotency_key": "batch-invalid",
		"source":          "direct",
		"sales": []map[string]any{
			{"customer_email": "not-an-email", "customer_first_name": "", "customer_last_name": "", "ticket_type_id": ttID, "quantity": 0, "payment_method": "bitcoin", "sold_at": "2026-07-01T10:00:00Z"},
		},
	}, authHeader(sessionID))

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("error = %+v, want VALIDATION_FAILED", body.Error)
	}
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 0 {
		t.Fatalf("sold_count = %d, want 0", got)
	}
}

func TestDirectSaleImportForbiddenForNonOrgAdmin(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Gated Fest", "gated-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "staff@example.com",
		"role":  "event_staff",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}
	staffSessionID := verifyOTP(t, env, "staff@example.com")

	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", map[string]any{
		"idempotency_key": "batch-forbidden",
		"source":          "direct",
		"sales": []map[string]any{
			{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": ttID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		},
	}, authHeader(staffSessionID))

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Fatalf("error = %+v, want FORBIDDEN", body.Error)
	}
}

func TestDirectSaleImportHistoryListsBatchesNewestFirst(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "History Fest", "history-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	// Empty history before any import.
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sale-imports", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("history status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var empty []map[string]any
	if err := json.Unmarshal(body.Data, &empty); err != nil {
		t.Fatalf("decode empty history: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("history = %d, want 0 before any import", len(empty))
	}

	// Commit two batches: the first records one sale, the second two, so the
	// resulting sale_count distinguishes them. The harness clock is fixed, so the
	// batches share a created_at; ordering is by the monotonic seq (migration
	// 012), so newest-first is deterministic — the last-committed batch leads.
	batchSales := [][]map[string]any{
		{
			{"customer_email": "one@example.com", "customer_first_name": "One", "customer_last_name": "Uno", "ticket_type_id": ttID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		},
		{
			{"customer_email": "two-a@example.com", "customer_first_name": "Two A", "customer_last_name": "Alpha", "ticket_type_id": ttID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
			{"customer_email": "two-b@example.com", "customer_first_name": "Two B", "customer_last_name": "Beta", "ticket_type_id": ttID, "quantity": 1, "payment_method": "transfer", "sold_at": "2026-07-02T10:00:00Z"},
		},
	}
	for i, sales := range batchSales {
		resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", map[string]any{
			"idempotency_key": fmt.Sprintf("hist-%d", i),
			"source":          "direct",
			"sales":           sales,
		}, authHeader(sessionID))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("commit hist-%d status=%d error=%+v", i, resp.StatusCode, body.Error)
		}
	}

	resp, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sale-imports", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("history status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var history []struct {
		BatchID    string  `json:"batch_id"`
		SaleCount  int     `json:"sale_count"`
		Source     string  `json:"source"`
		Status     string  `json:"status"`
		ActorEmail *string `json:"actor_email"`
		CreatedAt  string  `json:"created_at"`
	}
	if err := json.Unmarshal(body.Data, &history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history = %d, want 2", len(history))
	}
	for _, h := range history {
		if h.Source != "direct" || h.Status != "committed" {
			t.Fatalf("unexpected batch: %+v", h)
		}
		if h.BatchID == "" || h.CreatedAt == "" {
			t.Fatalf("batch missing id/created_at: %+v", h)
		}
		if h.ActorEmail == nil || *h.ActorEmail != "admin@example.com" {
			t.Fatalf("actor_email = %v, want admin@example.com", h.ActorEmail)
		}
	}
	// Newest first: the second batch (2 sales) leads the first (1 sale),
	// deterministically ordered by seq despite the shared created_at.
	if history[0].SaleCount != 2 || history[1].SaleCount != 1 {
		t.Fatalf("history order = [%d, %d], want newest-first [2, 1]", history[0].SaleCount, history[1].SaleCount)
	}
}

func TestDirectSaleImportHistoryForbiddenForNonOrgAdmin(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Gated History", "gated-history")

	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "staff@example.com",
		"role":  "event_staff",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}
	staffSessionID := verifyOTP(t, env, "staff@example.com")

	resp, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sale-imports", authHeader(staffSessionID))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Fatalf("error = %+v, want FORBIDDEN", body.Error)
	}
}

type undoResultBody struct {
	BatchID   string `json:"batch_id"`
	SaleCount int    `json:"sale_count"`
	Status    string `json:"status"`
	Notified  bool   `json:"notified"`
}

// commitBatch commits a Direct Sale Import batch and returns its batch id.
func commitBatch(t *testing.T, env *testEnv, sessionID, eventID, idempotencyKey string, sales []map[string]any) string {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", map[string]any{
		"idempotency_key": idempotencyKey,
		"source":          "direct",
		"sales":           sales,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("commit %s status=%d error=%+v", idempotencyKey, resp.StatusCode, body.Error)
	}
	return importResult(t, body).BatchID
}

// TestDirectSaleImportUndoRestoresCapacity proves undoing the latest batch
// reverses its sales and restores each Ticket Type's sold_count to its
// pre-batch value, marks the batch reversed, and (with notify_buyers false)
// sends no void emails.
func TestDirectSaleImportUndoRestoresCapacity(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Undo Fest", "undo-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	batchID := commitBatch(t, env, sessionID, eventID, "batch-undo", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": ttID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": ttID, "quantity": 3, "payment_method": "transfer", "sold_at": "2026-07-02T10:00:00Z"},
	})
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 5 {
		t.Fatalf("sold_count = %d, want 5 before undo", got)
	}

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports/"+batchID+"/undo", map[string]any{
		"notify_buyers": false,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var res undoResultBody
	if err := json.Unmarshal(body.Data, &res); err != nil {
		t.Fatalf("decode undo result: %v", err)
	}
	if res.BatchID != batchID || res.SaleCount != 2 || res.Status != "reversed" || res.Notified {
		t.Fatalf("unexpected undo result: %+v", res)
	}

	// Capacity restored to the pre-batch value.
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 0 {
		t.Fatalf("sold_count = %d, want 0 after undo", got)
	}
	// Sales no longer active.
	if n := salesCountByEmail(t, env, eventID, "ana@example.com"); n != 0 {
		t.Fatalf("active sales for ana = %d, want 0 after undo", n)
	}
	// Batch marked reversed in history.
	resp, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sale-imports", authHeader(sessionID))
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
	if len(history) != 1 || history[0].BatchID != batchID || history[0].Status != "reversed" {
		t.Fatalf("history = %+v, want single reversed batch %s", history, batchID)
	}
	// notify_buyers false: no void emails.
	if n := len(env.email.Voided()); n != 0 {
		t.Fatalf("voided emails = %d, want 0 when notify_buyers is false", n)
	}
}

// TestDirectSaleImportUndoNotifiesBuyers proves that with notify_buyers true,
// each affected buyer is emailed a void notice referencing their confirmation.
func TestDirectSaleImportUndoNotifiesBuyers(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Notify Fest", "notify-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	batchID := commitBatch(t, env, sessionID, eventID, "batch-notify", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": ttID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": ttID, "quantity": 1, "payment_method": "transfer", "sold_at": "2026-07-02T10:00:00Z"},
	})

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports/"+batchID+"/undo", map[string]any{
		"notify_buyers": true,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var res undoResultBody
	if err := json.Unmarshal(body.Data, &res); err != nil {
		t.Fatalf("decode undo result: %v", err)
	}
	if !res.Notified {
		t.Fatalf("undo result notified = false, want true")
	}

	voided := env.email.Voided()
	if len(voided) != 2 {
		t.Fatalf("voided emails = %d, want 2", len(voided))
	}
	seen := map[string]bool{}
	for _, v := range voided {
		if v.Reference == "" {
			t.Fatalf("void notice missing reference: %+v", v)
		}
		if v.EventName != "Notify Fest" {
			t.Fatalf("void notice event = %q, want Notify Fest", v.EventName)
		}
		seen[v.To] = true
	}
	if !seen["ana@example.com"] || !seen["bob@example.com"] {
		t.Fatalf("void notices not sent to both buyers: %+v", voided)
	}
}

// TestDirectSaleImportUndoLatestOnly proves that only the most recent batch is
// reversible: undoing an older batch after a newer one exists is rejected with
// IMPORT_NOT_LATEST_BATCH, and a second undo of the same batch is rejected with
// IMPORT_ALREADY_REVERSED.
func TestDirectSaleImportUndoLatestOnly(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Latest Fest", "latest-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	firstBatch := commitBatch(t, env, sessionID, eventID, "batch-first", []map[string]any{
		{"customer_email": "one@example.com", "customer_first_name": "One", "customer_last_name": "Uno", "ticket_type_id": ttID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	// A newer batch exists, so the first is no longer the latest.
	secondBatch := commitBatch(t, env, sessionID, eventID, "batch-second", []map[string]any{
		{"customer_email": "two@example.com", "customer_first_name": "Two", "customer_last_name": "Dos", "ticket_type_id": ttID, "quantity": 3, "payment_method": "cash", "sold_at": "2026-07-03T10:00:00Z"},
	})

	// Undoing the older batch is rejected; capacity untouched.
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports/"+firstBatch+"/undo", map[string]any{
		"notify_buyers": false,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("undo older status=%d, want 409; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "IMPORT_NOT_LATEST_BATCH" {
		t.Fatalf("error = %+v, want IMPORT_NOT_LATEST_BATCH", body.Error)
	}
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 5 {
		t.Fatalf("sold_count = %d, want 5 (undo rejected)", got)
	}

	// Undoing the latest batch succeeds.
	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports/"+secondBatch+"/undo", map[string]any{
		"notify_buyers": false,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo latest status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 2 {
		t.Fatalf("sold_count = %d, want 2 after undoing latest", got)
	}

	// Undoing it again is rejected as already reversed.
	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports/"+secondBatch+"/undo", map[string]any{
		"notify_buyers": false,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("second undo status=%d, want 409; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "IMPORT_ALREADY_REVERSED" {
		t.Fatalf("error = %+v, want IMPORT_ALREADY_REVERSED", body.Error)
	}
}

// TestDirectSaleImportUndoForbiddenForNonOrgAdmin proves undo is gated by
// can_manage_event_sales.
func TestDirectSaleImportUndoForbiddenForNonOrgAdmin(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Gated Undo", "gated-undo")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	batchID := commitBatch(t, env, sessionID, eventID, "batch-gated", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": ttID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})

	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "staff@example.com",
		"role":  "event_staff",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}
	staffSessionID := verifyOTP(t, env, "staff@example.com")

	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports/"+batchID+"/undo", map[string]any{
		"notify_buyers": false,
	}, authHeader(staffSessionID))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Fatalf("error = %+v, want FORBIDDEN", body.Error)
	}
}

// TestDirectSaleImportConcurrentDoesNotOversell proves the FOR UPDATE row lock:
// many concurrent imports competing for the same capacity never oversell, and
// sold_count exactly matches the sales that were accepted.
func TestDirectSaleImportConcurrentDoesNotOversell(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Race Fest", "race-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 10)

	const attempts = 8 // each buys 2 → 16 requested against capacity 10
	var wg sync.WaitGroup
	statuses := make([]int, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, _ := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", map[string]any{
				"idempotency_key": fmt.Sprintf("race-%d", i),
				"source":          "direct",
				"sales": []map[string]any{
					{"customer_email": fmt.Sprintf("buyer%d@example.com", i), "customer_first_name": "Buyer", "customer_last_name": "Test", "ticket_type_id": ttID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
				},
			}, authHeader(sessionID))
			statuses[i] = resp.StatusCode
		}(i)
	}
	wg.Wait()

	successes := 0
	for _, s := range statuses {
		switch s {
		case http.StatusCreated:
			successes++
		case http.StatusConflict:
		default:
			t.Fatalf("unexpected status %d", s)
		}
	}

	got := soldCount(t, env, sessionID, eventID, ttID)
	if got > 10 {
		t.Fatalf("sold_count = %d exceeds capacity 10 — oversold", got)
	}
	if got != successes*2 {
		t.Fatalf("sold_count = %d, want %d (2 per accepted import)", got, successes*2)
	}
	if successes != 5 {
		t.Fatalf("accepted imports = %d, want exactly 5 (10 capacity / 2)", successes)
	}
}
