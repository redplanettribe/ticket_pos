package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// saleListRow mirrors one Sales list row emitted by
// GET /api/v1/staff/events/{id}/sales.
type saleListRow struct {
	ID                string `json:"id"`
	CustomerFirstName string `json:"customer_first_name"`
	CustomerLastName  string `json:"customer_last_name"`
	CustomerEmail     string `json:"customer_email"`
	TicketTypes       []struct {
		TicketTypeName string `json:"ticket_type_name"`
		Quantity       int    `json:"quantity"`
	} `json:"ticket_types"`
	AmountCents     int     `json:"amount_cents"`
	Currency        string  `json:"currency"`
	SoldAt          string  `json:"sold_at"`
	Channel         string  `json:"channel"`
	Source          *string `json:"source"`
	Status          string  `json:"status"`
	ConfirmationRef string  `json:"confirmation_ref"`
	RecordedAt      string  `json:"recorded_at"`
	PaymentMethod   *string `json:"payment_method"`
}

type salesListEnvelope struct {
	Data       []saleListRow `json:"data"`
	Pagination struct {
		Page       int `json:"page"`
		PageSize   int `json:"page_size"`
		Total      int `json:"total"`
		TotalPages int `json:"total_pages"`
	} `json:"pagination"`
}

func salesList(t *testing.T, body envelope) salesListEnvelope {
	t.Helper()
	var out salesListEnvelope
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode sales list: %v", err)
	}
	return out
}

// TestSalesListDefaultViewOrdersAndRollsUp seeds active Ticket Sales through the
// import flow and asserts the default Sales list view: rows ordered sold_at
// descending, correct per-sale fields, and — for a multi-line sale — a rolled-up
// Ticket Type list and summed amount with no join fan-out (one row per sale).
func TestSalesListDefaultViewOrdersAndRollsUp(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Sales Fest", "sales-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 5000, 100)

	commitBatch(t, env, sessionID, eventID, "sales-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "cara@example.com", "customer_first_name": "Cara", "customer_last_name": "Diaz", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": vipID, "quantity": 1, "payment_method": "transfer", "sold_at": "2026-07-03T10:00:00Z"},
	})

	// Give Ana's sale a second Ticket Sale Line (VIP ×1). The Sales list must keep
	// it a single row whose amount sums both lines (2×1000 + 1×5000 = 7000) and
	// whose Ticket Types roll up — proving the line join does not fan the sale out.
	if _, err := env.db.Exec(`
		INSERT INTO ticket_sale_lines (ticket_sale_id, ticket_type_id, quantity, unit_price_cents, created_at)
		SELECT ts.id, $1, 1, 5000, NOW()
		FROM ticket_sales ts
		WHERE ts.event_id = $2 AND ts.customer_email = 'ana@example.com'
	`, vipID, eventID); err != nil {
		t.Fatalf("seed extra line: %v", err)
	}

	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	list := salesList(t, body)

	// Default pagination: page 1, default page size 50, 3 matching sales, 1 page.
	if list.Pagination.Page != 1 || list.Pagination.PageSize != 50 {
		t.Fatalf("pagination page/size = %d/%d, want 1/50", list.Pagination.Page, list.Pagination.PageSize)
	}
	if list.Pagination.Total != 3 || list.Pagination.TotalPages != 1 {
		t.Fatalf("pagination total/total_pages = %d/%d, want 3/1", list.Pagination.Total, list.Pagination.TotalPages)
	}
	if len(list.Data) != 3 {
		t.Fatalf("rows = %d, want 3 (one per Ticket Sale)", len(list.Data))
	}

	// Newest first by sold_at: bob (07-03), cara (07-02), ana (07-01).
	if list.Data[0].CustomerEmail != "bob@example.com" ||
		list.Data[1].CustomerEmail != "cara@example.com" ||
		list.Data[2].CustomerEmail != "ana@example.com" {
		t.Fatalf("order = [%s, %s, %s], want bob, cara, ana",
			list.Data[0].CustomerEmail, list.Data[1].CustomerEmail, list.Data[2].CustomerEmail)
	}

	// Bob: single VIP line, transfer, import/direct, active, with a reference and
	// a recorded-at (created_at) for the expand.
	bob := list.Data[0]
	if bob.CustomerFirstName != "Bob" || bob.CustomerLastName != "Ng" {
		t.Fatalf("bob name = %q %q, want Bob Ng", bob.CustomerFirstName, bob.CustomerLastName)
	}
	if bob.AmountCents != 5000 {
		t.Fatalf("bob amount = %d, want 5000", bob.AmountCents)
	}
	if bob.Currency != "USD" {
		t.Fatalf("bob currency = %q, want USD", bob.Currency)
	}
	if bob.Channel != "import" || bob.Source == nil || *bob.Source != "direct" {
		t.Fatalf("bob channel/source = %q/%v, want import/direct", bob.Channel, bob.Source)
	}
	if bob.Status != "active" {
		t.Fatalf("bob status = %q, want active", bob.Status)
	}
	if bob.ConfirmationRef == "" || bob.RecordedAt == "" {
		t.Fatalf("bob missing confirmation_ref/recorded_at: %+v", bob)
	}
	if bob.PaymentMethod == nil || *bob.PaymentMethod != "transfer" {
		t.Fatalf("bob payment_method = %v, want transfer", bob.PaymentMethod)
	}
	if len(bob.TicketTypes) != 1 || bob.TicketTypes[0].TicketTypeName != "VIP" || bob.TicketTypes[0].Quantity != 1 {
		t.Fatalf("bob ticket_types = %+v, want [VIP ×1]", bob.TicketTypes)
	}

	// Ana: multi-line sale rolls up to a single row summing both lines.
	ana := list.Data[2]
	if ana.AmountCents != 7000 {
		t.Fatalf("ana amount = %d, want 7000 (2×1000 + 1×5000)", ana.AmountCents)
	}
	if len(ana.TicketTypes) != 2 {
		t.Fatalf("ana ticket_types = %+v, want 2 entries (GA, VIP)", ana.TicketTypes)
	}
	names := map[string]int{}
	for _, tt := range ana.TicketTypes {
		names[tt.TicketTypeName] = tt.Quantity
	}
	if names["GA"] != 2 || names["VIP"] != 1 {
		t.Fatalf("ana rollup = %v, want GA:2 VIP:1", names)
	}
}

// TestSalesListPaginationAndClamp proves the ADR-0006 pagination behavior: the
// total spans the right number of pages, an out-of-range page_size clamps to the
// maximum, and page floors at 1.
func TestSalesListPaginationAndClamp(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Page Fest", "page-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)

	commitBatch(t, env, sessionID, eventID, "page-batch", []map[string]any{
		{"customer_email": "one@example.com", "customer_first_name": "One", "customer_last_name": "Uno", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "two@example.com", "customer_first_name": "Two", "customer_last_name": "Dos", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
		{"customer_email": "three@example.com", "customer_first_name": "Three", "customer_last_name": "Tres", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-03T10:00:00Z"},
	})

	// page_size 2 over 3 sales → 2 pages; page 1 holds 2 rows, page 2 holds 1.
	_, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales?page=1&page_size=2", authHeader(sessionID))
	first := salesList(t, body)
	if first.Pagination.Total != 3 || first.Pagination.TotalPages != 2 || first.Pagination.PageSize != 2 {
		t.Fatalf("page 1 pagination = %+v, want total 3 / pages 2 / size 2", first.Pagination)
	}
	if len(first.Data) != 2 {
		t.Fatalf("page 1 rows = %d, want 2", len(first.Data))
	}

	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?page=2&page_size=2", authHeader(sessionID))
	second := salesList(t, body)
	if second.Pagination.Page != 2 || len(second.Data) != 1 {
		t.Fatalf("page 2 = page %d with %d rows, want page 2 with 1 row", second.Pagination.Page, len(second.Data))
	}

	// A page across the two pages must not repeat: page 2's row differs from
	// page 1's rows (stable order across pages).
	seen := map[string]bool{first.Data[0].ID: true, first.Data[1].ID: true}
	if seen[second.Data[0].ID] {
		t.Fatalf("page 2 row %s already appeared on page 1", second.Data[0].ID)
	}

	// page_size above the maximum clamps to 100.
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?page_size=500", authHeader(sessionID))
	clamped := salesList(t, body)
	if clamped.Pagination.PageSize != 100 {
		t.Fatalf("page_size clamp = %d, want 100", clamped.Pagination.PageSize)
	}

	// page below 1 floors to 1.
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?page=0", authHeader(sessionID))
	floored := salesList(t, body)
	if floored.Pagination.Page != 1 {
		t.Fatalf("page floor = %d, want 1", floored.Pagination.Page)
	}
}

// TestSalesListAccess proves the Sales list is readable by any Member of the
// Event — including Event Staff, who cannot use the Sale Import tool — while a
// Member of a different Organization cannot see the Event's sales.
func TestSalesListAccess(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Access Fest", "access-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	commitBatch(t, env, sessionID, eventID, "access-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})

	// Event Staff of the same Organization can read the Sales list.
	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "staff@example.com",
		"role":  "event_staff",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}
	staffSessionID := verifyOTP(t, env, "staff@example.com")

	resp, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales", authHeader(staffSessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("event staff sales list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	staffList := salesList(t, body)
	if len(staffList.Data) != 1 || staffList.Pagination.Total != 1 {
		t.Fatalf("event staff saw %d rows / total %d, want 1 / 1", len(staffList.Data), staffList.Pagination.Total)
	}

	// A Member of a different Organization cannot see this Event's sales.
	otherSessionID := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSessionID, "Other Org", "other-org")
	resp, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales", authHeader(otherSessionID))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("non-member status=%d, want 404; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_NOT_FOUND" {
		t.Fatalf("non-member error = %+v, want EVENT_NOT_FOUND", body.Error)
	}

	// An unauthenticated request is rejected.
	resp, _ = env.get(t, "/api/v1/staff/events/"+eventID+"/sales", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d, want 401", resp.StatusCode)
	}
}
