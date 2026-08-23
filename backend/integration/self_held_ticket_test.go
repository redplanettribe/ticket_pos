package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// THE BUYER HOLDS ONE TICKET BY PAYING (ADR 0048). One Ticket of every Online
// Sale is the buyer's own from the moment the Sale is made: assigned to the
// buyer's address and accepted, so the Organization's roster names the buyer
// and the storefront can say "your ticket" truthfully. It is the first Ticket
// of the line whose Ticket Type comes FIRST IN THE CATALOG, not first in the
// cart, and every other Ticket starts `unassigned` exactly as before.
func TestOnlineCheckoutMakesTheFirstCatalogTicketTheBuyersOwn(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Held Fest", "held-fest", 2000, 20)
	// Created second, so it sorts AFTER GA in the catalog — and goes first in
	// the cart, so the test tells the two orders apart.
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 5000, 5)

	begun := beginCheckoutOK(t, env, "test-org", "held-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(vipID, 1), cartLine(gaID, 2)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	saleID := saleIDOfPayment(t, env, begun.ClientTransactionID)

	ana := customerSignIn(t, env, "ana@example.com")
	tickets := listBuyerTickets(t, env, ana, saleID)
	if len(tickets) != 3 {
		t.Fatalf("sale has %d Tickets, want 3", len(tickets))
	}
	var own, others int
	for _, ticket := range tickets {
		switch {
		case ticket.TicketTypeName == "GA" && ticket.Ordinal == 1:
			own++
			if ticket.AssignmentState != "accepted" || ticket.HolderEmail != "ana@example.com" || ticket.AcceptedAt == nil {
				t.Errorf("the buyer's own Ticket reads state=%q holder=%q accepted=%v; want accepted by the buyer",
					ticket.AssignmentState, ticket.HolderEmail, ticket.AcceptedAt)
			}
			if !ticket.SelfHeld {
				t.Error("the buyer's own Ticket is not reported self_held")
			}
		default:
			others++
			if ticket.SelfHeld {
				t.Errorf("%s #%d is reported self_held", ticket.TicketTypeName, ticket.Ordinal)
			}
			if ticket.AssignmentState != "unassigned" {
				t.Errorf("%s #%d reads %q; every Ticket but the buyer's own starts unassigned",
					ticket.TicketTypeName, ticket.Ordinal, ticket.AssignmentState)
			}
		}
	}
	if own != 1 || others != 2 {
		t.Fatalf("found %d own and %d other Tickets", own, others)
	}

	// And it is NOT among "tickets someone gave you": that list is other
	// people's purchases, and the buyer's own sits on their Sale.
	resp, body := env.get(t, "/api/v1/customer/ticket-sales", authHeader(ana))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("customer area status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var area struct {
		Holding []json.RawMessage `json:"holding"`
	}
	if err := json.Unmarshal(body.Data, &area); err != nil {
		t.Fatalf("decode customer area: %v", err)
	}
	if len(area.Holding) != 0 {
		t.Errorf("the buyer's own Ticket is listed as given to them: %s", area.Holding)
	}

	// The Organization sees an ordinary accepted Ticket under the buyer's
	// checkout name — no fourth state, nothing to tell it apart by.
	resp, body = env.get(t, outstandingPath(eventID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holder list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var seen bool
	for _, row := range decodeOutstanding(t, body.Data).Data {
		if row.TicketTypeName == "GA" && row.Ordinal == 1 {
			seen = true
			if row.AssignmentState != "accepted" || row.HolderEmail != "ana@example.com" ||
				row.HolderFirstName != "Ana" || row.HolderLastName != "Lopez" {
				t.Errorf("holder list shows state=%q holder=%q %q %q", row.AssignmentState,
					row.HolderEmail, row.HolderFirstName, row.HolderLastName)
			}
		}
	}
	if !seen {
		t.Fatal("the buyer's own Ticket is not on the Holder List")
	}
}

// ACCEPTED BY PURCHASE IS NOT PROOF OF EMAIL OWNERSHIP. A buyer holds their
// Ticket, but the customers row is not marked Verified BY THE SALE: verification
// stays the sign-in module's authority (ADR 0035), and a self-held Ticket
// asserts nothing about who controls the inbox.
//
// Since ADR 0054 every online buyer is verified anyway — by the sign-in that let
// them buy — so the property is read here as it is now readable: the sale is
// settled by a Payment whose buyer never proved anything (the shape of every
// checkout begun before #386, and of the rows this rule was written for), and
// the commit that mints the self-held Ticket verifies nobody.
func TestASelfHeldTicketMakesNobodyVerified(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Guest Fest", "guest-fest", 1000, 10)

	begun := beginLegacyGuestCheckout(t, env, "guest-fest", "guest@example.com", "Gus", "Perez",
		boolPtr(true), nil, nil, cartLine(gaID, 1))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	var verified *string
	var accepted int
	if err := env.db.QueryRow(`
		SELECT c.verified_at::text,
		       (SELECT count(*) FROM tickets tk WHERE tk.holder_customer_id = c.id AND tk.accepted_at IS NOT NULL)
		FROM customers c WHERE c.email = 'guest@example.com'
	`).Scan(&verified, &accepted); err != nil {
		t.Fatalf("read customer: %v", err)
	}
	if accepted != 1 {
		t.Fatalf("guest holds %d accepted Tickets, want 1", accepted)
	}
	if verified != nil {
		t.Errorf("a guest checkout marked the buyer Verified (%s); paying is not Proof of Email Ownership", *verified)
	}
}

// OFF BY DEFAULT, WITH THE REST OF ASSIGNMENT. While TICKET_ASSIGNMENT_ENABLED
// is closed an Online Sale writes no Holder at all, so a deployment without the
// feature is byte-for-byte the one it was.
func TestNoSelfHeldTicketWhileAssignmentIsClosed(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Closed Fest", "closed-fest", 1000, 10)

	begun := beginCheckoutOK(t, env, "test-org", "closed-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	var held int
	if err := env.db.QueryRow(`
		SELECT count(*) FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		WHERE l.ticket_sale_id = $1 AND tk.holder_email IS NOT NULL
	`, saleIDOfPayment(t, env, begun.ClientTransactionID)).Scan(&held); err != nil {
		t.Fatalf("count held: %v", err)
	}
	if held != 0 {
		t.Fatalf("%d Tickets carry a Holder with assignment closed", held)
	}
}
