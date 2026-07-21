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
			{"customer_email": "ana@example.com", "customer_name": "Ana", "ticket_type_id": ttID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
			{"customer_email": "bob@example.com", "customer_name": "Bob", "ticket_type_id": ttID, "quantity": 3, "payment_method": "transfer", "sold_at": "2026-07-02T10:00:00Z", "amount_cents": &comp},
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
	for _, c := range confs {
		if c.Reference == "" {
			t.Fatalf("confirmation missing reference: %+v", c)
		}
		if c.EventName != "Summer Fest" {
			t.Fatalf("confirmation event = %q, want Summer Fest", c.EventName)
		}
		seen[c.To] = true
	}
	if !seen["ana@example.com"] || !seen["bob@example.com"] {
		t.Fatalf("confirmations not sent to both buyers: %+v", confs)
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

func TestDirectSaleImportOversellRollsBackWholeBatch(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Small Show", "small-show")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 5)

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", map[string]any{
		"idempotency_key": "batch-oversell",
		"source":          "direct",
		"sales": []map[string]any{
			{"customer_email": "ana@example.com", "customer_name": "Ana", "ticket_type_id": ttID, "quantity": 3, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
			{"customer_email": "bob@example.com", "customer_name": "Bob", "ticket_type_id": ttID, "quantity": 4, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
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
			{"customer_email": "ana@example.com", "customer_name": "Ana", "ticket_type_id": ttID, "quantity": 4, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
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
			{"customer_email": "ana@example.com", "customer_name": "Ana", "ticket_type_id": "c0000000-0000-4000-8000-000000000099", "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
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
			{"customer_email": "not-an-email", "customer_name": "", "ticket_type_id": ttID, "quantity": 0, "payment_method": "bitcoin", "sold_at": "2026-07-01T10:00:00Z"},
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
			{"customer_email": "ana@example.com", "customer_name": "Ana", "ticket_type_id": ttID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		},
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
					{"customer_email": fmt.Sprintf("buyer%d@example.com", i), "customer_name": "Buyer", "ticket_type_id": ttID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
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
