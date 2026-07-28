package integration

import (
	"net/http"
	"testing"
)

// Reversed sales made visible on the Event's sales page (issue #122, ADR 0018).
//
// The failure this covers is an absence, which is why it needs its own tests:
// an organizer sees 84 sales at 17:00, six buyers undo during the evening, and
// at 19:30 the page shows 78 with nothing explaining the difference. Until a
// Customer could undo their own Online Sale the only reversal path was a Sale
// Import undo that staff performed themselves, so a row leaving the default
// active view was never a surprise. Now money and capacity move with no staff
// action at all — and no email is sent, deliberately (ADR 0018), so the Sales
// list's own count is the whole of the telling.
//
// The count rides the Sales list rather than the Net Proceeds strip because it
// must reach every Member of the Event, and the strip is refused to Event Staff.

// listSalesOK reads the Sales list for an Event under a staff session, with an
// optional raw query string ("" for the default view).
func listSalesOK(t *testing.T, env *testEnv, sessionID, eventID, query string) salesListEnvelope {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales"+query, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales list%q status=%d error=%+v", query, resp.StatusCode, body.Error)
	}
	return salesList(t, body)
}

// TestSalesListReversedCountIsZeroWithoutAReversal: an Event that has never had
// a Sale Reversal reports zero. The figure has to be present and quiet on the
// ordinary Event — an organizer who has never lost a sale must not be shown
// something that reads as an incident.
func TestSalesListReversedCountIsZeroWithoutAReversal(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Quiet Fest", "quiet-reversal-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)

	commitBatch(t, env, sessionID, eventID, "quiet-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
	})

	list := listSalesOK(t, env, sessionID, eventID, "")
	if list.ReversedCount != 0 {
		t.Fatalf("reversed_count = %d on an Event that has never had a reversal, want 0", list.ReversedCount)
	}
	if list.Pagination.Total != 2 || len(list.Data) != 2 {
		t.Fatalf("default view total/rows = %d/%d, want 2/2 — the active sales are untouched",
			list.Pagination.Total, len(list.Data))
	}
}

// TestSalesListReversedCountReportsBothActorsAndDistinguishesThem is the whole
// feature in one Event: a buyer undoes their own Online Sale, staff undo a Sale
// Import batch, and one sale stays active. The organizer must be able to see
// that two sales went, reach them through the status filter the list already
// has, and tell their own batch undo from the buyer's.
func TestSalesListReversedCountReportsBothActorsAndDistinguishesThem(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishFreeEvent(t, env, sessionID, "Reversal Fest", "reversal-visible-fest", 20)

	// The sale that survives the evening. Committed first because only the most
	// recent Sale Import batch is reversible.
	commitBatch(t, env, sessionID, eventID, "keeper-batch", []map[string]any{
		{"customer_email": "keeper@example.com", "customer_first_name": "Kit", "customer_last_name": "Diaz", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})

	// The buyer's own undo, driven through the real Customer endpoint.
	customerRef := claimFree(t, env, "reversal-visible-fest", gaID, "ana@example.com", 1)
	undoOwnSale(t, env, "ana@example.com", customerRef)

	// The organizer's own batch undo.
	batchID := commitBatch(t, env, sessionID, eventID, "undone-batch", []map[string]any{
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
	})
	undoBatch(t, env, sessionID, eventID, batchID)

	// The default view still shows active sales only — the reversed pair is gone
	// from it, which is exactly the silence the count breaks.
	active := listSalesOK(t, env, sessionID, eventID, "")
	if active.Pagination.Total != 1 || len(active.Data) != 1 || active.Data[0].CustomerEmail != "keeper@example.com" {
		t.Fatalf("default view = %v (total %d), want only keeper@example.com",
			listSalesEmails(active), active.Pagination.Total)
	}
	if active.ReversedCount != 2 {
		t.Fatalf("reversed_count on the default view = %d, want 2 — the buyer's undo and the batch undo",
			active.ReversedCount)
	}

	// The count is the Event's, not the view's: it does not move as the organizer
	// narrows the list, or it could not answer "did anything get reversed".
	filtered := listSalesOK(t, env, sessionID, eventID, "?q=keeper@example.com")
	if filtered.Pagination.Total != 1 {
		t.Fatalf("search total = %d, want 1", filtered.Pagination.Total)
	}
	if filtered.ReversedCount != 2 {
		t.Fatalf("reversed_count under a search = %d, want 2 — the count is the Event's, not the page's",
			filtered.ReversedCount)
	}

	// Reached through the existing status filter, with the provenance #117
	// recorded: which side asked, and when.
	reversed := listSalesOK(t, env, sessionID, eventID, "?status=reversed")
	if reversed.ReversedCount != 2 {
		t.Fatalf("reversed_count on the reversed view = %d, want 2", reversed.ReversedCount)
	}
	if reversed.Pagination.Total != 2 || len(reversed.Data) != 2 {
		t.Fatalf("reversed view total/rows = %d/%d, want 2/2", reversed.Pagination.Total, len(reversed.Data))
	}

	byEmail := map[string]saleListRow{}
	for _, row := range reversed.Data {
		byEmail[row.CustomerEmail] = row
	}
	for email, wantActor := range map[string]string{
		"ana@example.com": "customer",
		"bob@example.com": "staff",
	} {
		row, ok := byEmail[email]
		if !ok {
			t.Fatalf("%s is missing from the reversed view: %v", email, listSalesEmails(reversed))
		}
		if row.Status != "reversed" {
			t.Fatalf("%s status = %q, want reversed", email, row.Status)
		}
		if row.ReversedBy == nil || *row.ReversedBy != wantActor {
			t.Fatalf("%s reversed_by = %v, want %q — the two undos must be tellable apart",
				email, row.ReversedBy, wantActor)
		}
		if row.ReversedAt == nil || *row.ReversedAt == "" {
			t.Fatalf("%s reversed_at = %v, want the time the sale went", email, row.ReversedAt)
		}
	}

	// The sale that stayed says nothing about a reversal it did not have.
	keeper := active.Data[0]
	if keeper.ReversedAt != nil || keeper.ReversedBy != nil {
		t.Fatalf("active sale carries reversal provenance %v/%v, want both null",
			keeper.ReversedAt, keeper.ReversedBy)
	}
}

// TestSalesListReversedCountReachesEveryMember: door staff see it too. The count
// sits on the Sales list rather than the Net Proceeds strip precisely so that
// Event Staff — refused the Event's money — are not also refused the fact that
// sales were reversed.
func TestSalesListReversedCountReachesEveryMember(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishFreeEvent(t, env, sessionID, "Door Fest", "door-reversal-fest", 20)

	ref := claimFree(t, env, "door-reversal-fest", gaID, "ana@example.com", 1)
	undoOwnSale(t, env, "ana@example.com", ref)

	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "doorstaff@example.com",
		"role":  "event_staff",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add event staff status=%d error=%+v", resp.StatusCode, body.Error)
	}
	staffSessionID := verifyOTP(t, env, "doorstaff@example.com")

	// The Event's money stays refused to them; the reversal does not.
	if summaryResp, summaryBody := getSalesSummary(t, env, staffSessionID, eventID); summaryResp.StatusCode != http.StatusForbidden {
		t.Fatalf("event staff summary status=%d, want 403; error=%+v", summaryResp.StatusCode, summaryBody.Error)
	}

	list := listSalesOK(t, env, staffSessionID, eventID, "")
	if list.ReversedCount != 1 {
		t.Fatalf("event staff reversed_count = %d, want 1", list.ReversedCount)
	}
	staffReversed := listSalesOK(t, env, staffSessionID, eventID, "?status=reversed")
	if len(staffReversed.Data) != 1 || staffReversed.Data[0].ReversedBy == nil ||
		*staffReversed.Data[0].ReversedBy != "customer" {
		t.Fatalf("event staff reversed view = %+v, want the buyer's own undo", staffReversed.Data)
	}
}
