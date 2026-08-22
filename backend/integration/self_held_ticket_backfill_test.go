package integration

import (
	"database/sql"
	"net/http"
	"testing"
	"time"
)

// MIGRATION 084 HOLDS TICKET 1 FOR THE BUYER ON EXISTING ONLINE SALES (#339,
// parent #338, ADR 0048). Every Sale in production predates the Self-held
// Ticket, so on deploy a backfill makes the buyer of each existing Online Sale
// the accepted Holder of its first Ticket — the same Ticket the checkout would
// have picked — stamped with the Sale's own time.
//
// Every case here stages a pre-0048 Sale the only way one can be produced now:
// a real checkout, then the Self-held Ticket's holder columns cleared in SQL.
// The migration is then executed from the embedded set, and what is asserted
// is what a buyer sees on their Sale page and what the Organizer sees on the
// Holder List — never the rows the migration wrote.

const selfHeldBackfill = "084_backfill_self_held_ticket.sql"

// clearSelfHeld undoes what the checkout wrote for ADR 0048 on one Sale, which
// is exactly the state every Sale made before it was in.
func clearSelfHeld(t *testing.T, env *testEnv, saleID string) {
	t.Helper()
	if _, err := env.db.Exec(`
		UPDATE tickets tk
		SET holder_email = NULL, holder_customer_id = NULL, assigned_at = NULL, accepted_at = NULL
		FROM ticket_sale_lines l
		WHERE l.id = tk.ticket_sale_line_id AND l.ticket_sale_id = $1
	`, saleID); err != nil {
		t.Fatalf("clear the self-held Ticket: %v", err)
	}
}

// holderRow is one Ticket's holder columns as stored, for the assertions that
// are about the migration's own promises (timestamps, idempotence) rather than
// about a surface.
type holderRow struct {
	ticketID   string
	holder     sql.NullString
	customerID sql.NullString
	assignedAt sql.NullTime
	acceptedAt sql.NullTime
}

func holderRows(t *testing.T, env *testEnv, saleID string) []holderRow {
	t.Helper()
	rows, err := env.db.Query(`
		SELECT tk.id, tk.holder_email, tk.holder_customer_id, tk.assigned_at, tk.accepted_at
		FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		WHERE l.ticket_sale_id = $1
		ORDER BY tk.id
	`, saleID)
	if err != nil {
		t.Fatalf("read holder rows: %v", err)
	}
	defer rows.Close()
	var out []holderRow
	for rows.Next() {
		var r holderRow
		if err := rows.Scan(&r.ticketID, &r.holder, &r.customerID, &r.assignedAt, &r.acceptedAt); err != nil {
			t.Fatalf("scan holder row: %v", err)
		}
		out = append(out, r)
	}
	return out
}

func saleIDOfRef(t *testing.T, env *testEnv, ref string) string {
	t.Helper()
	var id string
	if err := env.db.QueryRow(`SELECT id FROM ticket_sales WHERE confirmation_ref = $1`, ref).Scan(&id); err != nil {
		t.Fatalf("sale of %s: %v", ref, err)
	}
	return id
}

// selfHeldTicketID is the Ticket the CHECKOUT held for the buyer — read before
// the test clears it, so the migration's choice can be compared to it.
func selfHeldTicketID(t *testing.T, env *testEnv, saleID string) string {
	t.Helper()
	var id string
	if err := env.db.QueryRow(`
		SELECT tk.id FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		WHERE l.ticket_sale_id = $1 AND tk.accepted_at IS NOT NULL
	`, saleID).Scan(&id); err != nil {
		t.Fatalf("the checkout held no single Ticket for the buyer: %v", err)
	}
	return id
}

// assertHeldByBuyer asserts, through the Sale page, that exactly the named
// Ticket is the buyer's own and the rest of the Sale is unassigned.
func assertHeldByBuyer(t *testing.T, tickets []buyerTicket, ticketID, email string) {
	t.Helper()
	var own int
	for _, ticket := range tickets {
		if ticket.TicketID == ticketID {
			own++
			if ticket.AssignmentState != "accepted" || ticket.HolderEmail != email || ticket.AcceptedAt == nil || !ticket.SelfHeld {
				t.Errorf("Ticket 1 reads state=%q holder=%q accepted=%v self_held=%v; want accepted by the buyer",
					ticket.AssignmentState, ticket.HolderEmail, ticket.AcceptedAt, ticket.SelfHeld)
			}
			continue
		}
		if ticket.AssignmentState != "unassigned" || ticket.SelfHeld {
			t.Errorf("%s #%d reads %q self_held=%v; only Ticket 1 is backfilled",
				ticket.TicketTypeName, ticket.Ordinal, ticket.AssignmentState, ticket.SelfHeld)
		}
	}
	if own != 1 {
		t.Fatalf("found %d own Tickets on the Sale page, want 1", own)
	}
}

// A ONE-TICKET SALE: after the backfill the buyer holds it, the Organizer's
// Holder List says `accepted` under the checkout name, the timestamps are the
// Sale's own, nobody was mailed and nobody was Verified by it.
func TestBackfillHoldsAOneTicketOnlineSaleForItsBuyer(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Old Fest", "old-fest", 1000, 10)

	// Typed with the case and whitespace a buyer types: the backfill must land
	// on the normalised address, as the checkout does.
	begun := beginCheckoutOK(t, env, "test-org", "old-fest",
		checkoutBody("  Gus@Example.com ", "Gus", "Perez", cartLine(gaID, 1)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	saleID := saleIDOfPayment(t, env, begun.ClientTransactionID)
	ticketID := selfHeldTicketID(t, env, saleID)
	clearSelfHeld(t, env, saleID)

	before := captureMailBaseline(env)
	executeMigration(t, env, selfHeldBackfill)
	assertNoAssignmentMailWasSent(t, env, before)

	// The migration's own promises, read before the buyer signs in (which
	// would Verify them and make the verified_at check meaningless).
	var saleCreated time.Time
	var customerID string
	var verified sql.NullTime
	if err := env.db.QueryRow(`
		SELECT ts.created_at, ts.customer_id, c.verified_at
		FROM ticket_sales ts JOIN customers c ON c.id = ts.customer_id
		WHERE ts.id = $1
	`, saleID).Scan(&saleCreated, &customerID, &verified); err != nil {
		t.Fatalf("read sale: %v", err)
	}
	if verified.Valid {
		t.Errorf("the backfill marked the buyer Verified at %v; paying is not Proof of Email Ownership", verified.Time)
	}
	rows := holderRows(t, env, saleID)
	if len(rows) != 1 {
		t.Fatalf("sale has %d Tickets, want 1", len(rows))
	}
	row := rows[0]
	if row.holder.String != "gus@example.com" || row.customerID.String != customerID {
		t.Errorf("backfilled holder=%q customer=%q, want gus@example.com / the Sale's Customer %s",
			row.holder.String, row.customerID.String, customerID)
	}
	if !row.assignedAt.Valid || !row.assignedAt.Time.Equal(saleCreated) ||
		!row.acceptedAt.Valid || !row.acceptedAt.Time.Equal(saleCreated) {
		t.Errorf("backfilled assigned_at=%v accepted_at=%v, want both = the Sale's created_at %v",
			row.assignedAt.Time, row.acceptedAt.Time, saleCreated)
	}

	gus := customerSignIn(t, env, "gus@example.com")
	assertHeldByBuyer(t, listBuyerTickets(t, env, gus, saleID), ticketID, "gus@example.com")

	var seen bool
	for _, r := range listOutstanding(t, env, sessionID, eventID).Data {
		if r.TicketID != ticketID {
			continue
		}
		seen = true
		if r.AssignmentState != "accepted" || r.HolderEmail != "gus@example.com" ||
			r.HolderFirstName != "Gus" || r.HolderLastName != "Perez" {
			t.Errorf("holder list shows state=%q holder=%q %q %q", r.AssignmentState,
				r.HolderEmail, r.HolderFirstName, r.HolderLastName)
		}
	}
	if !seen {
		t.Fatal("the backfilled Ticket is not on the Holder List")
	}
}

// A MULTI-TICKET SINGLE-LINE SALE: only the lowest ordinal is held.
func TestBackfillHoldsOnlyTheLowestOrdinalOfASingleLine(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Three Fest", "three-fest", 1000, 10)

	begun := beginCheckoutOK(t, env, "test-org", "three-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 3)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	saleID := saleIDOfPayment(t, env, begun.ClientTransactionID)
	ticketID := selfHeldTicketID(t, env, saleID)
	clearSelfHeld(t, env, saleID)

	executeMigration(t, env, selfHeldBackfill)

	ana := customerSignIn(t, env, "ana@example.com")
	tickets := listBuyerTickets(t, env, ana, saleID)
	if len(tickets) != 3 {
		t.Fatalf("sale has %d Tickets, want 3", len(tickets))
	}
	assertHeldByBuyer(t, tickets, ticketID, "ana@example.com")
	for _, ticket := range tickets {
		if ticket.TicketID == ticketID && ticket.Ordinal != 1 {
			t.Errorf("the held Ticket is ordinal %d, want 1", ticket.Ordinal)
		}
	}
}

// A MULTI-LINE SALE WHOSE CATALOG-FIRST LINE WAS NOT WRITTEN FIRST: the
// migration holds the Ticket the checkout's own rule picked — first by Ticket
// Type sort_order then name, not by cart order — so new and backfilled Sales
// never disagree about which Ticket is the buyer's.
func TestBackfillAgreesWithTheCheckoutAboutWhichTicketIsTheBuyers(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Order Fest", "order-fest", 2000, 20)
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 5000, 5)

	// VIP first in the cart; GA first in the catalog.
	begun := beginCheckoutOK(t, env, "test-org", "order-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(vipID, 2), cartLine(gaID, 2)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	saleID := saleIDOfPayment(t, env, begun.ClientTransactionID)
	checkoutPick := selfHeldTicketID(t, env, saleID)
	clearSelfHeld(t, env, saleID)

	executeMigration(t, env, selfHeldBackfill)

	ana := customerSignIn(t, env, "ana@example.com")
	tickets := listBuyerTickets(t, env, ana, saleID)
	assertHeldByBuyer(t, tickets, checkoutPick, "ana@example.com")
	held := findBuyerRow(t, tickets, checkoutPick)
	if held.TicketTypeName != "GA" || held.Ordinal != 1 {
		t.Errorf("the backfill held %s #%d, want GA #1 — the catalog-first line's first Ticket",
			held.TicketTypeName, held.Ordinal)
	}
}

// THE FOUR SALES LEFT ALONE: reversed; Ticket 1 already handed to somebody;
// the buyer already holding another Ticket by its Assignment Link; and a Sale
// that is not `online`.
func TestBackfillSkipsTheSalesItMustNotTouch(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Skip Fest", "skip-fest", 1000, 40)

	buy := func(email string, quantity int) (saleID, ref string) {
		begun := beginCheckoutOK(t, env, "test-org", "skip-fest",
			checkoutBody(email, "Ana", "Lopez", cartLine(gaID, quantity)))
		settled := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
		saleID = saleIDOfPayment(t, env, begun.ClientTransactionID)
		clearSelfHeld(t, env, saleID)
		return saleID, settled.ConfirmationRef
	}

	// Reversed by an operator, which records no `sale_reversals` row — the
	// status column alone says the Ticket admits nobody.
	reversedRef := claimFreeOnlineSale(t, env, sessionID, "Free Skip Fest", "free-skip-fest", "rev@example.com", 2)
	reversedSale := saleIDOfRef(t, env, reversedRef)
	clearSelfHeld(t, env, reversedSale)
	operatorReverseOK(t, payphoneEnv, operatorSession(t, env, "operator@example.com"), reversedRef,
		operatorReversalBody{Note: strPtr("refunded")})

	// A buyer's own Reversal Request, in flight: a `sale_reversals` row.
	requestedSale, _ := buy("req@example.com", 2)
	if _, err := env.db.Exec(`
		INSERT INTO sale_reversals (ticket_sale_id, client_transaction_id, requested_at, status, next_attempt_at)
		VALUES ($1, 'tx-req', NOW(), 'in_flight', NOW())
	`, requestedSale); err != nil {
		t.Fatalf("stage a reversal request: %v", err)
	}

	// Ticket 1 handed to a friend before the deploy.
	ana := customerSignIn(t, env, "ana@example.com")
	handedSale, _ := buy("ana@example.com", 2)
	var handedFirst string
	for _, tk := range listBuyerTickets(t, env, ana, handedSale) {
		if tk.Ordinal == 1 {
			handedFirst = tk.TicketID
		}
	}
	assignTicketOK(t, env, ana, handedSale, handedFirst, "carla@example.com")

	// The buyer accepted Ticket 2 for themself by link before the deploy.
	linkedSale, _ := buy("ana@example.com", 2)
	var linkedSecond string
	for _, tk := range listBuyerTickets(t, env, ana, linkedSale) {
		if tk.Ordinal == 2 {
			linkedSecond = tk.TicketID
		}
	}
	env.email.Reset()
	assignTicketOK(t, env, ana, linkedSale, linkedSecond, "Ana@Example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "ana@example.com")))

	// A door sale.
	doorSale, doorRef := buy("door@example.com", 2)
	moveSaleToTheDoor(t, env, doorRef)

	executeMigration(t, env, selfHeldBackfill)

	heldCount := func(saleID string) int {
		n := 0
		for _, r := range holderRows(t, env, saleID) {
			if r.holder.Valid {
				n++
			}
		}
		return n
	}
	if got := heldCount(reversedSale); got != 0 {
		t.Errorf("a reversed Sale has %d held Tickets, want 0", got)
	}
	if got := heldCount(requestedSale); got != 0 {
		t.Errorf("a Sale with a Reversal Request has %d held Tickets, want 0", got)
	}
	if got := heldCount(doorSale); got != 0 {
		t.Errorf("a door sale has %d held Tickets, want 0", got)
	}

	handed := listBuyerTickets(t, env, ana, handedSale)
	if first := findBuyerRow(t, handed, handedFirst); first.HolderEmail != "carla@example.com" || first.AssignmentState != "assigned" {
		t.Errorf("Ticket 1 handed to a friend now reads state=%q holder=%q", first.AssignmentState, first.HolderEmail)
	}
	for _, tk := range handed {
		if tk.TicketID != handedFirst && tk.AssignmentState != "unassigned" {
			t.Errorf("the friend's Sale had %s #%d backfilled to %q", tk.TicketTypeName, tk.Ordinal, tk.HolderEmail)
		}
	}

	linked := listBuyerTickets(t, env, ana, linkedSale)
	for _, tk := range linked {
		switch tk.TicketID {
		case linkedSecond:
			if tk.AssignmentState != "accepted" || tk.HolderEmail != "ana@example.com" {
				t.Errorf("the Ticket the buyer accepted by link reads state=%q holder=%q", tk.AssignmentState, tk.HolderEmail)
			}
		default:
			if tk.AssignmentState != "unassigned" {
				t.Errorf("a buyer already holding Ticket 2 was given %s #%d too (%q); one Ticket, not two",
					tk.TicketTypeName, tk.Ordinal, tk.HolderEmail)
			}
		}
	}
}

// RE-EXECUTING THE MIGRATION CHANGES NOTHING, on a backfilled Sale and on one
// the checkout held itself.
func TestBackfillIsIdempotent(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Twice Fest", "twice-fest", 1000, 10)

	begun := beginCheckoutOK(t, env, "test-org", "twice-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	oldSale := saleIDOfPayment(t, env, begun.ClientTransactionID)
	clearSelfHeld(t, env, oldSale)

	begun = beginCheckoutOK(t, env, "test-org", "twice-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	newSale := saleIDOfPayment(t, env, begun.ClientTransactionID)
	newBefore := holderRows(t, env, newSale)

	executeMigration(t, env, selfHeldBackfill)
	oldOnce := holderRows(t, env, oldSale)

	executeMigration(t, env, selfHeldBackfill)
	if got := holderRows(t, env, oldSale); !equalHolderRows(got, oldOnce) {
		t.Errorf("a second run changed the backfilled Sale:\n first %+v\nsecond %+v", oldOnce, got)
	}
	if got := holderRows(t, env, newSale); !equalHolderRows(got, newBefore) {
		t.Errorf("the backfill changed a Sale the checkout had already held:\nbefore %+v\n after %+v", newBefore, got)
	}
}

func equalHolderRows(a, b []holderRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ticketID != b[i].ticketID || a[i].holder != b[i].holder || a[i].customerID != b[i].customerID ||
			a[i].assignedAt.Valid != b[i].assignedAt.Valid || !a[i].assignedAt.Time.Equal(b[i].assignedAt.Time) ||
			a[i].acceptedAt.Valid != b[i].acceptedAt.Valid || !a[i].acceptedAt.Time.Equal(b[i].acceptedAt.Time) {
			return false
		}
	}
	return true
}

// INDEPENDENT OF TICKET_ASSIGNMENT_ENABLED. A Sale made with the flag closed
// is a pre-0048 Sale without any SQL staging; the migration runs with the flag
// still closed, and the data is there the moment it opens.
func TestBackfillRunsWhileAssignmentIsClosed(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Dark Fest", "dark-fest", 1000, 10)

	begun := beginCheckoutOK(t, env, "test-org", "dark-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	saleID := saleIDOfPayment(t, env, begun.ClientTransactionID)
	for _, r := range holderRows(t, env, saleID) {
		if r.holder.Valid {
			t.Fatalf("a checkout with assignment closed wrote a Holder; the fixture is not pre-0048")
		}
	}

	executeMigration(t, env, selfHeldBackfill)

	enableTicketQuestions(t)
	enableTicketAssignment(t)
	ana := customerSignIn(t, env, "ana@example.com")
	tickets := listBuyerTickets(t, env, ana, saleID)
	var held string
	for _, tk := range tickets {
		if tk.Ordinal == 1 {
			held = tk.TicketID
		}
	}
	assertHeldByBuyer(t, tickets, held, "ana@example.com")

	resp, body := env.get(t, outstandingPath(eventID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holder list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var accepted int
	for _, r := range decodeOutstanding(t, body.Data).Data {
		if r.TicketSaleID == saleID && r.AssignmentState == "accepted" {
			accepted++
		}
	}
	if accepted != 1 {
		t.Errorf("holder list shows %d accepted Tickets on the Sale, want 1", accepted)
	}
}
