package integration

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"testing"
	"time"
)

// setEventTimezone stamps the Event's timezone directly (createDraftEvent leaves
// it unset), so the sold-at date-range filter can be exercised against a
// non-UTC zone.
func setEventTimezone(t *testing.T, env *testEnv, eventID, tz string) {
	t.Helper()
	if _, err := env.db.Exec(`UPDATE events SET timezone = $1 WHERE id = $2`, tz, eventID); err != nil {
		t.Fatalf("set event timezone: %v", err)
	}
}

// undoBatch reverses a committed Sale Import batch (marking its Ticket Sales
// reversed) so the status filter can be exercised.
func undoBatch(t *testing.T, env *testEnv, sessionID, eventID, batchID string) {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports/"+batchID+"/undo",
		map[string]any{"notify_buyers": false}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo batch status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// listSalesEmails returns the sorted customer emails of a Sales list response,
// for order-independent set assertions.
func listSalesEmails(list salesListEnvelope) []string {
	emails := make([]string, 0, len(list.Data))
	for _, row := range list.Data {
		emails = append(emails, row.CustomerEmail)
	}
	sort.Strings(emails)
	return emails
}

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
	// The Tax ID the sale was transacted under (#100): both halves null on
	// sales recorded without one.
	TaxIDType   *string `json:"tax_id_type"`
	TaxIDNumber *string `json:"tax_id_number"`
	// When the Ticket Sale was reversed and which side caused it (#117): both
	// null on an active sale, and on a sale reversed before this was recorded.
	ReversedAt *string `json:"reversed_at"`
	ReversedBy *string `json:"reversed_by"`
}

type salesListEnvelope struct {
	Data       []saleListRow `json:"data"`
	Pagination struct {
		Page       int `json:"page"`
		PageSize   int `json:"page_size"`
		Total      int `json:"total"`
		TotalPages int `json:"total_pages"`
	} `json:"pagination"`
	// How many of the Event's Ticket Sales are reversed — the whole Event, and
	// independent of every filter on the request (#122). Exercised in
	// sales_reversed_count_test.go.
	ReversedCount int `json:"reversed_count"`
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

// sortedEmails returns the customer emails of a sorted Sales list request, in
// the order the endpoint emitted them.
func sortedEmails(t *testing.T, env *testEnv, sessionID, eventID, sort, dir string) []string {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales?sort="+sort+"&dir="+dir, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales list sort=%s dir=%s status=%d error=%+v", sort, dir, resp.StatusCode, body.Error)
	}
	list := salesList(t, body)
	emails := make([]string, 0, len(list.Data))
	for _, row := range list.Data {
		emails = append(emails, row.CustomerEmail)
	}
	return emails
}

// TestSalesListSortFields proves each allowlisted sort column orders the Sales
// list in both directions: sold_at, recorded_at (created_at), customer name, and
// amount (summed lines). recorded_at is seeded distinct from sold_at so the two
// orders differ, proving the endpoint sorts by the requested column.
func TestSalesListSortFields(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Sort Fest", "sort-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)

	commitBatch(t, env, sessionID, eventID, "sort-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 3, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Adams", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
		{"customer_email": "cara@example.com", "customer_first_name": "Cara", "customer_last_name": "Zimmer", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-03T10:00:00Z"},
	})

	// Set recorded-at (created_at) independent of sold_at: ana newest, cara middle,
	// bob oldest — a different order than any sold_at/amount/customer ordering.
	for email, createdAt := range map[string]string{
		"ana@example.com":  "2026-06-03T00:00:00Z",
		"cara@example.com": "2026-06-02T00:00:00Z",
		"bob@example.com":  "2026-06-01T00:00:00Z",
	} {
		if _, err := env.db.Exec(`UPDATE ticket_sales SET created_at = $1 WHERE event_id = $2 AND customer_email = $3`, createdAt, eventID, email); err != nil {
			t.Fatalf("set created_at for %s: %v", email, err)
		}
	}

	cases := []struct {
		sort string
		dir  string
		want []string
	}{
		{"sold_at", "desc", []string{"cara@example.com", "bob@example.com", "ana@example.com"}},
		{"sold_at", "asc", []string{"ana@example.com", "bob@example.com", "cara@example.com"}},
		{"recorded_at", "desc", []string{"ana@example.com", "cara@example.com", "bob@example.com"}},
		{"recorded_at", "asc", []string{"bob@example.com", "cara@example.com", "ana@example.com"}},
		{"customer", "asc", []string{"bob@example.com", "ana@example.com", "cara@example.com"}},  // Adams, Lopez, Zimmer
		{"customer", "desc", []string{"cara@example.com", "ana@example.com", "bob@example.com"}}, // Zimmer, Lopez, Adams
		{"amount", "desc", []string{"ana@example.com", "cara@example.com", "bob@example.com"}},   // 3000, 2000, 1000
		{"amount", "asc", []string{"bob@example.com", "cara@example.com", "ana@example.com"}},    // 1000, 2000, 3000
	}
	for _, tc := range cases {
		got := sortedEmails(t, env, sessionID, eventID, tc.sort, tc.dir)
		if len(got) != len(tc.want) {
			t.Fatalf("sort=%s dir=%s rows=%v, want %v", tc.sort, tc.dir, got, tc.want)
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Fatalf("sort=%s dir=%s order=%v, want %v", tc.sort, tc.dir, got, tc.want)
			}
		}
	}

	// An invalid sort or dir normalizes to the default (sold_at desc) rather than
	// erroring, keeping shared/edited URLs robust.
	if got := sortedEmails(t, env, sessionID, eventID, "bogus", "sideways"); len(got) != 3 ||
		got[0] != "cara@example.com" || got[2] != "ana@example.com" {
		t.Fatalf("invalid sort/dir order=%v, want default sold_at desc", got)
	}
}

// TestSalesListSortTiebreakerStableAcrossPages seeds sales that all share the
// same primary sort value (identical sold_at) and pages through them one at a
// time. The id tiebreaker must give every page a stable slot so the union of
// pages covers each sale exactly once — no duplicates, none missing.
func TestSalesListSortTiebreakerStableAcrossPages(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Tie Fest", "tie-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)

	// Four sales with an identical sold_at — only the id tiebreaker separates them.
	commitBatch(t, env, sessionID, eventID, "tie-batch", []map[string]any{
		{"customer_email": "w@example.com", "customer_first_name": "W", "customer_last_name": "Same", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "x@example.com", "customer_first_name": "X", "customer_last_name": "Same", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "y@example.com", "customer_first_name": "Y", "customer_last_name": "Same", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "z@example.com", "customer_first_name": "Z", "customer_last_name": "Same", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})

	// Page through 2 at a time across every sort field; each must yield all four
	// ids exactly once with no overlap between the two pages.
	for _, sort := range []string{"sold_at", "recorded_at", "customer", "amount"} {
		seen := map[string]int{}
		var order []string
		for page := 1; page <= 2; page++ {
			url := "/api/v1/staff/events/" + eventID + "/sales?page=" + strconv.Itoa(page) + "&page_size=2&sort=" + sort + "&dir=asc"
			resp, body := env.get(t, url, authHeader(sessionID))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("sort=%s page=%d status=%d error=%+v", sort, page, resp.StatusCode, body.Error)
			}
			list := salesList(t, body)
			if list.Pagination.Total != 4 {
				t.Fatalf("sort=%s total=%d, want 4", sort, list.Pagination.Total)
			}
			if len(list.Data) != 2 {
				t.Fatalf("sort=%s page=%d rows=%d, want 2", sort, page, len(list.Data))
			}
			for _, row := range list.Data {
				seen[row.ID]++
				order = append(order, row.ID)
			}
		}
		if len(seen) != 4 {
			t.Fatalf("sort=%s saw %d distinct ids across pages, want 4 (order=%v)", sort, len(seen), order)
		}
		for id, n := range seen {
			if n != 1 {
				t.Fatalf("sort=%s id %s appeared %d times across pages, want 1", sort, id, n)
			}
		}
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

// TestSalesListStatusFilter proves the status filter defaults to active (hiding
// reversed sales) and that status=reversed reveals them.
func TestSalesListStatusFilter(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Status Fest", "status-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)

	// Two batches; the second is undone so its sales become reversed.
	commitBatch(t, env, sessionID, eventID, "active-batch", []map[string]any{
		{"customer_email": "keep@example.com", "customer_first_name": "Keep", "customer_last_name": "Active", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	reversedBatch := commitBatch(t, env, sessionID, eventID, "reversed-batch", []map[string]any{
		{"customer_email": "gone@example.com", "customer_first_name": "Gone", "customer_last_name": "Reversed", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
	})
	undoBatch(t, env, sessionID, eventID, reversedBatch)

	// Default view: only the active sale.
	_, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales", authHeader(sessionID))
	def := salesList(t, body)
	if got := listSalesEmails(def); len(got) != 1 || got[0] != "keep@example.com" {
		t.Fatalf("default status = %v, want [keep@example.com]", got)
	}

	// status=active is the explicit form of the default.
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?status=active", authHeader(sessionID))
	if got := listSalesEmails(salesList(t, body)); len(got) != 1 || got[0] != "keep@example.com" {
		t.Fatalf("status=active = %v, want [keep@example.com]", got)
	}

	// status=reversed reveals the reversed sale (and only it).
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?status=reversed", authHeader(sessionID))
	rev := salesList(t, body)
	if got := listSalesEmails(rev); len(got) != 1 || got[0] != "gone@example.com" {
		t.Fatalf("status=reversed = %v, want [gone@example.com]", got)
	}
	if rev.Data[0].Status != "reversed" {
		t.Fatalf("reversed row status = %q, want reversed", rev.Data[0].Status)
	}
}

// TestSalesListReversedRowCarriesReversalProvenance proves a reversed row tells
// staff when the Sale Reversal happened and which side caused it — a Sale Import
// undo is 'staff' — while an active row carries neither (#117, ADR 0018).
func TestSalesListReversedRowCarriesReversalProvenance(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Provenance Fest", "provenance-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)

	commitBatch(t, env, sessionID, eventID, "kept-batch", []map[string]any{
		{"customer_email": "keep@example.com", "customer_first_name": "Keep", "customer_last_name": "Active", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	undone := commitBatch(t, env, sessionID, eventID, "undone-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-03T10:00:00Z"},
	})
	undoBatch(t, env, sessionID, eventID, undone)

	// An active row shows nothing extra.
	_, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales", authHeader(sessionID))
	active := salesList(t, body)
	if len(active.Data) != 1 {
		t.Fatalf("active rows = %d, want 1", len(active.Data))
	}
	if active.Data[0].ReversedAt != nil || active.Data[0].ReversedBy != nil {
		t.Fatalf("active row carries reversal provenance: at=%v by=%v",
			active.Data[0].ReversedAt, active.Data[0].ReversedBy)
	}

	// Every sale in the undone batch records when and by whom.
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?status=reversed", authHeader(sessionID))
	reversed := salesList(t, body)
	if len(reversed.Data) != 2 {
		t.Fatalf("reversed rows = %d, want 2", len(reversed.Data))
	}
	for _, row := range reversed.Data {
		if row.ReversedBy == nil || *row.ReversedBy != "staff" {
			t.Fatalf("row %s reversed_by = %v, want staff", row.CustomerEmail, row.ReversedBy)
		}
		if row.ReversedAt == nil {
			t.Fatalf("row %s reversed_at is null, want the undo time", row.CustomerEmail)
		}
		at, err := time.Parse(time.RFC3339, *row.ReversedAt)
		if err != nil {
			t.Fatalf("row %s reversed_at = %q: %v", row.CustomerEmail, *row.ReversedAt, err)
		}
		if !at.Equal(env.fixedClock) {
			t.Fatalf("row %s reversed_at = %s, want the undo time %s", row.CustomerEmail, at, env.fixedClock)
		}
	}
}

// TestSalesListReversedBeforeProvenanceWasRecordedStaysNull proves a Ticket Sale
// already reversed before #117 shipped keeps a null reversal time and actor: the
// migration invents no timestamp, and the list reports the absence honestly.
func TestSalesListReversedBeforeProvenanceWasRecordedStaysNull(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Legacy Fest", "legacy-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)

	commitBatch(t, env, sessionID, eventID, "legacy-batch", []map[string]any{
		{"customer_email": "old@example.com", "customer_first_name": "Old", "customer_last_name": "Row", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	// Direct SQL: no API can produce a pre-migration reversed row, which is
	// exactly the shape under test — status flipped with no provenance beside it.
	if _, err := env.db.Exec(`UPDATE ticket_sales SET status = 'reversed' WHERE event_id = $1`, eventID); err != nil {
		t.Fatalf("seed legacy reversed sale: %v", err)
	}

	_, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales?status=reversed", authHeader(sessionID))
	list := salesList(t, body)
	if len(list.Data) != 1 {
		t.Fatalf("reversed rows = %d, want 1", len(list.Data))
	}
	if list.Data[0].ReversedAt != nil || list.Data[0].ReversedBy != nil {
		t.Fatalf("legacy reversed row = at:%v by:%v, want both null",
			list.Data[0].ReversedAt, list.Data[0].ReversedBy)
	}
}

// TestSalesListTicketTypeFilter proves the ticket-type filter returns every sale
// that includes the type exactly once — including a multi-line sale that also
// holds another type — with its full rollup preserved (no fan-out).
func TestSalesListTicketTypeFilter(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Type Fest", "type-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 5000, 100)

	commitBatch(t, env, sessionID, eventID, "type-batch", []map[string]any{
		{"customer_email": "ga-only@example.com", "customer_first_name": "Ga", "customer_last_name": "Only", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "vip-only@example.com", "customer_first_name": "Vip", "customer_last_name": "Only", "ticket_type_id": vipID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
		{"customer_email": "both@example.com", "customer_first_name": "Both", "customer_last_name": "Types", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-03T10:00:00Z"},
	})
	// Give "both" a second line (VIP) so it includes both types.
	if _, err := env.db.Exec(`
		INSERT INTO ticket_sale_lines (ticket_sale_id, ticket_type_id, quantity, unit_price_cents, created_at)
		SELECT ts.id, $1, 1, 5000, NOW()
		FROM ticket_sales ts
		WHERE ts.event_id = $2 AND ts.customer_email = 'both@example.com'
	`, vipID, eventID); err != nil {
		t.Fatalf("seed extra line: %v", err)
	}

	// Filtering by VIP returns the vip-only and the both sale — each once.
	_, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales?ticket_type_id="+vipID, authHeader(sessionID))
	list := salesList(t, body)
	if got := listSalesEmails(list); len(got) != 2 || got[0] != "both@example.com" || got[1] != "vip-only@example.com" {
		t.Fatalf("ticket_type=VIP = %v, want [both, vip-only]", got)
	}
	if list.Pagination.Total != 2 {
		t.Fatalf("ticket_type=VIP total = %d, want 2 (each sale once)", list.Pagination.Total)
	}
	// The both sale keeps its full rollup (GA ×2 and VIP ×1), not just the
	// filtered type.
	var both saleListRow
	for _, row := range list.Data {
		if row.CustomerEmail == "both@example.com" {
			both = row
		}
	}
	names := map[string]int{}
	for _, tt := range both.TicketTypes {
		names[tt.TicketTypeName] = tt.Quantity
	}
	if len(both.TicketTypes) != 2 || names["GA"] != 2 || names["VIP"] != 1 {
		t.Fatalf("both rollup = %v, want GA:2 VIP:1 (full rollup preserved)", names)
	}

	// Filtering by GA returns ga-only and both.
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?ticket_type_id="+gaID, authHeader(sessionID))
	if got := listSalesEmails(salesList(t, body)); len(got) != 2 || got[0] != "both@example.com" || got[1] != "ga-only@example.com" {
		t.Fatalf("ticket_type=GA = %v, want [both, ga-only]", got)
	}
}

// TestSalesListDateRangeFilter proves sold_from/sold_to are interpreted in the
// Event timezone as a half-open interval, inclusive of the end date's whole day,
// with either bound optionally omitted.
func TestSalesListDateRangeFilter(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Date Fest", "date-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	// America/New_York is UTC-4 in July (EDT), so 2026-07-01 local midnight is
	// 2026-07-01T04:00Z and the day ends just before 2026-07-02T04:00Z.
	setEventTimezone(t, env, eventID, "America/New_York")

	commitBatch(t, env, sessionID, eventID, "date-batch", []map[string]any{
		// 06-30 23:59 EDT — before 07-01.
		{"customer_email": "before@example.com", "customer_first_name": "B", "customer_last_name": "Efore", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T03:59:00Z"},
		// 07-01 00:00 EDT — the inclusive lower boundary.
		{"customer_email": "start@example.com", "customer_first_name": "S", "customer_last_name": "Tart", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T04:00:00Z"},
		// 07-01 23:59 EDT — last moment of the end day (must be included).
		{"customer_email": "end@example.com", "customer_first_name": "E", "customer_last_name": "Nd", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T03:59:00Z"},
		// 07-02 00:00 EDT — the exclusive upper boundary (must be excluded).
		{"customer_email": "after@example.com", "customer_first_name": "A", "customer_last_name": "Fter", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T04:00:00Z"},
	})

	// Both bounds on 07-01: only start and end.
	_, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales?sold_from=2026-07-01&sold_to=2026-07-01", authHeader(sessionID))
	if got := listSalesEmails(salesList(t, body)); len(got) != 2 || got[0] != "end@example.com" || got[1] != "start@example.com" {
		t.Fatalf("range 07-01..07-01 = %v, want [end, start]", got)
	}

	// Open upper bound (only sold_from): start, end, after.
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?sold_from=2026-07-01", authHeader(sessionID))
	if got := listSalesEmails(salesList(t, body)); len(got) != 3 || got[0] != "after@example.com" || got[1] != "end@example.com" || got[2] != "start@example.com" {
		t.Fatalf("range from 07-01 = %v, want [after, end, start]", got)
	}

	// Open lower bound (only sold_to): before, start, end.
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?sold_to=2026-07-01", authHeader(sessionID))
	if got := listSalesEmails(salesList(t, body)); len(got) != 3 || got[0] != "before@example.com" || got[1] != "end@example.com" || got[2] != "start@example.com" {
		t.Fatalf("range to 07-01 = %v, want [before, end, start]", got)
	}
}

// TestSalesListSearchFilter proves the q filter matches a case-insensitive
// substring over customer name, email, and confirmation reference.
func TestSalesListSearchFilter(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Search Fest", "search-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)

	commitBatch(t, env, sessionID, eventID, "search-batch", []map[string]any{
		{"customer_email": "maria.garcia@example.com", "customer_first_name": "María", "customer_last_name": "García", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "john@other.test", "customer_first_name": "John", "customer_last_name": "Smith", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
	})

	// Name substring, case-insensitive.
	_, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales?q=garc", authHeader(sessionID))
	if got := listSalesEmails(salesList(t, body)); len(got) != 1 || got[0] != "maria.garcia@example.com" {
		t.Fatalf("q=garc = %v, want [maria.garcia@example.com]", got)
	}

	// Email substring on the other row.
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?q=OTHER.test", authHeader(sessionID))
	if got := listSalesEmails(salesList(t, body)); len(got) != 1 || got[0] != "john@other.test" {
		t.Fatalf("q=OTHER.test = %v, want [john@other.test]", got)
	}

	// Confirmation reference: read one row's ref, then search for it.
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales", authHeader(sessionID))
	all := salesList(t, body)
	var ref, refEmail string
	for _, row := range all.Data {
		if row.CustomerEmail == "john@other.test" {
			ref = row.ConfirmationRef
			refEmail = row.CustomerEmail
		}
	}
	if ref == "" {
		t.Fatalf("no confirmation_ref to search")
	}
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?q="+ref, authHeader(sessionID))
	if got := listSalesEmails(salesList(t, body)); len(got) != 1 || got[0] != refEmail {
		t.Fatalf("q=%s (ref) = %v, want [%s]", ref, got, refEmail)
	}
}

// TestSalesListChannelSourcePaymentAndCombination proves the channel/source/
// payment-method filters function (degenerate today: every sale is
// import/direct) and that multiple filters combine to narrow the set.
func TestSalesListChannelSourcePaymentAndCombination(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Combo Fest", "combo-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 5000, 100)

	commitBatch(t, env, sessionID, eventID, "combo-batch", []map[string]any{
		{"customer_email": "cash-ga@example.com", "customer_first_name": "Cash", "customer_last_name": "Ga", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "transfer-vip@example.com", "customer_first_name": "Xfer", "customer_last_name": "Vip", "ticket_type_id": vipID, "quantity": 1, "payment_method": "transfer", "sold_at": "2026-07-05T10:00:00Z"},
	})

	// channel=import matches every sale; a wrong channel matches none.
	_, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales?channel=import", authHeader(sessionID))
	if list := salesList(t, body); len(list.Data) != 2 {
		t.Fatalf("channel=import rows = %d, want 2", len(list.Data))
	}
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?channel=online", authHeader(sessionID))
	if list := salesList(t, body); len(list.Data) != 0 {
		t.Fatalf("channel=online rows = %d, want 0", len(list.Data))
	}

	// source=direct matches all; payment_method=cash narrows to one.
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?source=direct", authHeader(sessionID))
	if list := salesList(t, body); len(list.Data) != 2 {
		t.Fatalf("source=direct rows = %d, want 2", len(list.Data))
	}
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?payment_method=cash", authHeader(sessionID))
	if got := listSalesEmails(salesList(t, body)); len(got) != 1 || got[0] != "cash-ga@example.com" {
		t.Fatalf("payment_method=cash = %v, want [cash-ga@example.com]", got)
	}

	// Combination: VIP + transfer + date window isolates the one matching sale;
	// adding an excluding payment method yields nothing.
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?ticket_type_id="+vipID+"&payment_method=transfer&sold_from=2026-07-04", authHeader(sessionID))
	if got := listSalesEmails(salesList(t, body)); len(got) != 1 || got[0] != "transfer-vip@example.com" {
		t.Fatalf("combined filter = %v, want [transfer-vip@example.com]", got)
	}
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?ticket_type_id="+vipID+"&payment_method=cash", authHeader(sessionID))
	if list := salesList(t, body); len(list.Data) != 0 {
		t.Fatalf("VIP+cash rows = %d, want 0 (no such sale)", len(list.Data))
	}
}

// TestSalesListOnlineSalePayPhone proves the Sales list carries Online Sales
// (issue #87): an approved checkout appears as one row with channel online, no
// Sales Source, and Payment Method payphone; the payment_method filter accepts
// payphone and separates online rows from cash/transfer ones; the channel
// filter works against a real online row; and pending or failed Payments
// (begin without confirm, begin then decline) never appear as rows.
func TestSalesListOnlineSalePayPhone(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Mixed Fest", "mixed-fest", 1000, 100)

	// Two Direct Sales through the import flow: one cash, one transfer.
	commitBatch(t, env, sessionID, eventID, "mixed-batch", []map[string]any{
		{"customer_email": "cash@example.com", "customer_first_name": "Cash", "customer_last_name": "Buyer", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "transfer@example.com", "customer_first_name": "Xfer", "customer_last_name": "Buyer", "ticket_type_id": gaID, "quantity": 1, "payment_method": "transfer", "sold_at": "2026-07-02T10:00:00Z"},
	})

	// One Online Sale the honest way: begin → confirm(approved) with the stub
	// Payment Provider.
	approved := beginCheckoutOK(t, env, "test-org", "mixed-fest",
		checkoutBody("online@example.com", "Onda", "Line", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	confirm := confirmCheckoutOK(t, env, approved.ClientTransactionID, "approved")
	if confirm.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirm.Status)
	}

	// A pending Payment (begun, never confirmed) and a failed one (declined):
	// neither is a Ticket Sale, so neither may surface as a row.
	beginCheckoutOK(t, env, "test-org", "mixed-fest",
		checkoutBody("pending@example.com", "Pen", "Ding", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	declined := beginCheckoutOK(t, env, "test-org", "mixed-fest",
		checkoutBody("declined@example.com", "Dec", "Lined", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if got := confirmCheckoutOK(t, env, declined.ClientTransactionID, "declined"); got.Status != "failed" {
		t.Fatalf("declined confirm status = %q, want failed", got.Status)
	}

	// Default view: exactly the three committed sales — the pending and failed
	// Payments contribute no rows.
	_, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales", authHeader(sessionID))
	all := salesList(t, body)
	if got := listSalesEmails(all); len(got) != 3 ||
		got[0] != "cash@example.com" || got[1] != "online@example.com" || got[2] != "transfer@example.com" {
		t.Fatalf("default rows = %v, want [cash, online, transfer]", got)
	}
	if all.Pagination.Total != 3 {
		t.Fatalf("total = %d, want 3", all.Pagination.Total)
	}
	var online saleListRow
	for _, row := range all.Data {
		if row.CustomerEmail == "online@example.com" {
			online = row
		}
	}
	if online.Channel != "online" {
		t.Fatalf("online row channel = %q, want online", online.Channel)
	}
	if online.Source != nil {
		t.Fatalf("online row source = %v, want none on an Online Sale", *online.Source)
	}
	if online.PaymentMethod == nil || *online.PaymentMethod != "payphone" {
		t.Fatalf("online row payment_method = %v, want payphone", online.PaymentMethod)
	}
	if online.ConfirmationRef != confirm.ConfirmationRef {
		t.Fatalf("online row confirmation_ref = %q, want %q", online.ConfirmationRef, confirm.ConfirmationRef)
	}

	// payment_method=payphone returns exactly the Online Sale.
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?payment_method=payphone", authHeader(sessionID))
	payphone := salesList(t, body)
	if got := listSalesEmails(payphone); len(got) != 1 || got[0] != "online@example.com" {
		t.Fatalf("payment_method=payphone = %v, want [online@example.com]", got)
	}
	if payphone.Pagination.Total != 1 {
		t.Fatalf("payment_method=payphone total = %d, want 1", payphone.Pagination.Total)
	}

	// cash/transfer filtering is unchanged and excludes the Online Sale.
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?payment_method=cash", authHeader(sessionID))
	if got := listSalesEmails(salesList(t, body)); len(got) != 1 || got[0] != "cash@example.com" {
		t.Fatalf("payment_method=cash = %v, want [cash@example.com]", got)
	}
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?payment_method=transfer", authHeader(sessionID))
	if got := listSalesEmails(salesList(t, body)); len(got) != 1 || got[0] != "transfer@example.com" {
		t.Fatalf("payment_method=transfer = %v, want [transfer@example.com]", got)
	}

	// channel=online works against a real online row; channel=import still
	// returns only the imported ones.
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?channel=online", authHeader(sessionID))
	if got := listSalesEmails(salesList(t, body)); len(got) != 1 || got[0] != "online@example.com" {
		t.Fatalf("channel=online = %v, want [online@example.com]", got)
	}
	_, body = env.get(t, "/api/v1/staff/events/"+eventID+"/sales?channel=import", authHeader(sessionID))
	if got := listSalesEmails(salesList(t, body)); len(got) != 2 ||
		got[0] != "cash@example.com" || got[1] != "transfer@example.com" {
		t.Fatalf("channel=import = %v, want [cash, transfer]", got)
	}
}

// TestSalesListInvalidFilters proves invalid enum and date filter values are
// rejected with a VALIDATION_FAILED envelope rather than silently ignored.
func TestSalesListInvalidFilters(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Invalid Fest", "invalid-fest")
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)

	cases := []string{
		"status=deleted",
		"channel=carrier-pigeon",
		"source=telepathy",
		"payment_method=barter",
		"sold_from=07-2026-01",
		"sold_to=not-a-date",
		"ticket_type_id=not-a-uuid",
	}
	for _, qs := range cases {
		resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales?"+qs, authHeader(sessionID))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s status=%d, want 400", qs, resp.StatusCode)
		}
		if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
			t.Fatalf("%s error = %+v, want VALIDATION_FAILED", qs, body.Error)
		}
	}
}
