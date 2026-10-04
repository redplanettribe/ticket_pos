package integration

import (
	"net/http"
	"testing"
)

// TestPublicTicketTypeCarriesItsSortOrder: the Event page publishes each Ticket
// Type's place in the catalog. The Storefront mirrors the Self-held seating rule
// (ADR 0074, ADR 0076) to know which Ticket a Named Tickets checkout does not
// ask a Holder for, and that rule breaks a price tie on sort_order. The list's
// order alone cannot stand in for it: two Ticket Types can share a sort_order.
func TestPublicTicketTypeCarriesItsSortOrder(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Sort Order Night", "sort-order-night")
	patchCheckoutEvent(t, env, sessionID, eventID, "Sort Order Night", "sort-order-night", false)
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 10)
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 1000, 10)
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+vipID, map[string]any{
		"name":        "VIP",
		"price_cents": 1000,
		"capacity":    10,
		"sort_order":  7,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/publish", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish event status=%d error=%+v", resp.StatusCode, body.Error)
	}

	types := publicTicketTypes(t, env, "test-org", "sort-order-night")
	if got := types["GA"].SortOrder; got != 0 {
		t.Fatalf("GA sort_order = %d, want 0", got)
	}
	if got := types["VIP"].SortOrder; got != 7 {
		t.Fatalf("VIP sort_order = %d, want 7", got)
	}
}
