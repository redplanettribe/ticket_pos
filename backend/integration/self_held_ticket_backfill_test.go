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

// WHAT THE BACKFILL LEAVES ALONE (#340). Each exclusion below is one focused
// test, and each is read back the way the buyer and the Organizer read it: the
// Sale page and the Holder List. Where a surface cannot show a Sale at all (an
// operator-reversed Sale has no page to open) the holder columns stand in.

// skipFixture is the common staging: an Event with the flags open, and a buy
// that makes a pre-0048 Online Sale for one address.
type skipFixture struct {
	env       *testEnv
	sessionID string
	eventID   string
	gaID      string
	slug      string
}

func newSkipFixture(t *testing.T, name, slug string) skipFixture {
	t.Helper()
	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, name, slug, 1000, 40)
	return skipFixture{env: env, sessionID: sessionID, eventID: eventID, gaID: gaID, slug: slug}
}

// buy makes an Online Sale and clears the Self-held Ticket, which is exactly
// the state of every Sale made before ADR 0048.
func (f skipFixture) buy(t *testing.T, email string, quantity int) (saleID, ref string) {
	t.Helper()
	begun := beginCheckoutOK(t, f.env, "test-org", f.slug,
		checkoutBody(email, "Ana", "Lopez", cartLine(f.gaID, quantity)))
	settled := confirmCheckoutOK(t, f.env, begun.ClientTransactionID, "approved")
	saleID = saleIDOfPayment(t, f.env, begun.ClientTransactionID)
	clearSelfHeld(t, f.env, saleID)
	return saleID, settled.ConfirmationRef
}

// ticketAt is the Ticket of a Sale at one ordinal, as the buyer sees it.
func (f skipFixture) ticketAt(t *testing.T, session, saleID string, ordinal int) string {
	t.Helper()
	for _, tk := range listBuyerTickets(t, f.env, session, saleID) {
		if tk.Ordinal == ordinal {
			return tk.TicketID
		}
	}
	t.Fatalf("sale %s has no Ticket #%d", saleID, ordinal)
	return ""
}

// heldCount is how many Tickets of a Sale have any Holder at all.
func heldCount(t *testing.T, env *testEnv, saleID string) int {
	t.Helper()
	n := 0
	for _, r := range holderRows(t, env, saleID) {
		if r.holder.Valid {
			n++
		}
	}
	return n
}

// assertSaleUnassignedEverywhere asserts that no Ticket of the Sale has a
// Holder: in the columns, on the buyer's Sale page and on the Holder List —
// where an `unassigned` row is also the proof that no Holder reminder could be
// addressed, since the reminder sweep chases holders through accepted_at and
// these Tickets have none.
func assertSaleUnassignedEverywhere(t *testing.T, f skipFixture, buyerSession, saleID, what string) {
	t.Helper()
	if got := heldCount(t, f.env, saleID); got != 0 {
		t.Errorf("%s has %d held Tickets, want 0", what, got)
	}
	for _, tk := range listBuyerTickets(t, f.env, buyerSession, saleID) {
		if tk.AssignmentState != "unassigned" || tk.SelfHeld || tk.HolderEmail != "" {
			t.Errorf("%s: Sale page shows %s #%d state=%q holder=%q self_held=%v, want unassigned",
				what, tk.TicketTypeName, tk.Ordinal, tk.AssignmentState, tk.HolderEmail, tk.SelfHeld)
		}
	}
	for _, r := range listOutstanding(t, f.env, f.sessionID, f.eventID).Data {
		if r.TicketSaleID != saleID {
			continue
		}
		if r.AssignmentState != "unassigned" || r.HolderEmail != "" {
			t.Errorf("%s: Holder List shows Ticket #%d state=%q holder=%q, want unassigned",
				what, r.Ordinal, r.AssignmentState, r.HolderEmail)
		}
	}
}

// holderListRowsHeldBy counts the Holder List rows of one Sale naming an
// address as Holder — how many times the buyer is on the roster for it.
func holderListRowsHeldBy(t *testing.T, f skipFixture, saleID, email string) int {
	t.Helper()
	n := 0
	for _, r := range listOutstanding(t, f.env, f.sessionID, f.eventID).Data {
		if r.TicketSaleID == saleID && r.HolderEmail == email {
			n++
		}
	}
	return n
}

// A REVERSED ONLINE SALE: the Ticket admits nobody, so nobody is made its
// Holder. Both ways a Sale is reversed disqualify it: the status column (an
// operator's reversal) and a `sale_reversals` row (the buyer's own Reversal
// Request, whatever became of it).
func TestBackfillHoldsNothingOnAReversedSale(t *testing.T) {
	f := newSkipFixture(t, "Reversed Fest", "reversed-fest")

	t.Run("reversed by an operator", func(t *testing.T) {
		ref := claimFreeOnlineSale(t, f.env, f.sessionID, "Free Reversed Fest", "free-reversed-fest", "rev@example.com", 2)
		saleID := saleIDOfRef(t, f.env, ref)
		clearSelfHeld(t, f.env, saleID)
		operatorReverseOK(t, payphoneEnv, operatorSession(t, f.env, "operator@example.com"), ref,
			operatorReversalBody{Note: strPtr("refunded")})

		executeMigration(t, f.env, selfHeldBackfill)

		if got := heldCount(t, f.env, saleID); got != 0 {
			t.Errorf("an operator-reversed Sale has %d held Tickets, want 0", got)
		}
	})

	t.Run("reversal requested by the buyer", func(t *testing.T) {
		saleID, _ := f.buy(t, "req@example.com", 2)
		if _, err := f.env.db.Exec(`
			INSERT INTO sale_reversals (ticket_sale_id, client_transaction_id, requested_at, status, next_attempt_at)
			VALUES ($1, 'tx-req', NOW(), 'in_flight', NOW())
		`, saleID); err != nil {
			t.Fatalf("stage a reversal request: %v", err)
		}

		executeMigration(t, f.env, selfHeldBackfill)

		req := customerSignIn(t, f.env, "req@example.com")
		assertSaleUnassignedEverywhere(t, f, req, saleID, "a Sale with a Reversal Request")
	})
}

// TICKET 1 ALREADY HANDED TO A THIRD PARTY: the buyer's choice stands whether
// the friend has accepted or not, and the buyer is given nothing else.
func TestBackfillLeavesATicketOneHandedToAThirdPartyAlone(t *testing.T) {
	f := newSkipFixture(t, "Handed Fest", "handed-fest")
	ana := customerSignIn(t, f.env, "ana@example.com")

	assertUntouched := func(t *testing.T, saleID, first, wantState string) {
		t.Helper()
		tickets := listBuyerTickets(t, f.env, ana, saleID)
		if got := findBuyerRow(t, tickets, first); got.HolderEmail != "carla@example.com" || got.AssignmentState != wantState {
			t.Errorf("Ticket 1 handed to a friend now reads state=%q holder=%q, want %s by carla@example.com",
				got.AssignmentState, got.HolderEmail, wantState)
		}
		for _, tk := range tickets {
			if tk.TicketID != first && tk.AssignmentState != "unassigned" {
				t.Errorf("%s #%d was backfilled to %q; the buyer holds nothing on this Sale",
					tk.TicketTypeName, tk.Ordinal, tk.HolderEmail)
			}
		}
		if got := holderListRowsHeldBy(t, f, saleID, "ana@example.com"); got != 0 {
			t.Errorf("the buyer appears %d times on the Holder List for this Sale, want 0", got)
		}
		// The Holder List says what state Ticket 1 is in; a pending Holder's
		// address is never disclosed there (ADR 0047), so the state is the
		// whole of what it can say about the friend.
		var seen bool
		for _, r := range listOutstanding(t, f.env, f.sessionID, f.eventID).Data {
			if r.TicketID != first {
				continue
			}
			seen = true
			if r.AssignmentState != wantState {
				t.Errorf("the Holder List shows Ticket 1 as %q, want %q", r.AssignmentState, wantState)
			}
		}
		if !seen {
			t.Fatal("Ticket 1 is not on the Holder List")
		}
	}

	t.Run("pending", func(t *testing.T) {
		saleID, _ := f.buy(t, "ana@example.com", 2)
		first := f.ticketAt(t, ana, saleID, 1)
		assignTicketOK(t, f.env, ana, saleID, first, "carla@example.com")

		executeMigration(t, f.env, selfHeldBackfill)

		assertUntouched(t, saleID, first, "assigned")
	})

	t.Run("accepted", func(t *testing.T) {
		saleID, _ := f.buy(t, "ana@example.com", 2)
		first := f.ticketAt(t, ana, saleID, 1)
		f.env.email.Reset()
		assignTicketOK(t, f.env, ana, saleID, first, "carla@example.com")
		acceptAssignmentOK(t, f.env, assignmentTokenFrom(t, assignmentMailFor(t, f.env, "carla@example.com")))

		executeMigration(t, f.env, selfHeldBackfill)

		assertUntouched(t, saleID, first, "accepted")
	})
}

// THE BUYER ALREADY HOLDS ANOTHER TICKET OF THE SALE BY ITS ASSIGNMENT LINK:
// one Ticket, not two, and the buyer is on the roster once.
func TestBackfillHoldsNoSecondTicketForABuyerHoldingOneByLink(t *testing.T) {
	f := newSkipFixture(t, "Linked Fest", "linked-fest")
	ana := customerSignIn(t, f.env, "ana@example.com")
	saleID, _ := f.buy(t, "ana@example.com", 3)
	third := f.ticketAt(t, ana, saleID, 3)
	f.env.email.Reset()
	assignTicketOK(t, f.env, ana, saleID, third, "Ana@Example.com")
	acceptAssignmentOK(t, f.env, assignmentTokenFrom(t, assignmentMailFor(t, f.env, "ana@example.com")))

	executeMigration(t, f.env, selfHeldBackfill)

	for _, tk := range listBuyerTickets(t, f.env, ana, saleID) {
		switch tk.TicketID {
		case third:
			if tk.AssignmentState != "accepted" || tk.HolderEmail != "ana@example.com" {
				t.Errorf("the Ticket the buyer accepted by link reads state=%q holder=%q", tk.AssignmentState, tk.HolderEmail)
			}
		default:
			if tk.AssignmentState != "unassigned" {
				t.Errorf("a buyer already holding Ticket 3 was given %s #%d too (%q); one Ticket, not two",
					tk.TicketTypeName, tk.Ordinal, tk.HolderEmail)
			}
		}
	}
	if got := holderListRowsHeldBy(t, f, saleID, "ana@example.com"); got != 1 {
		t.Errorf("the buyer appears %d times on the Holder List for this Sale, want exactly once", got)
	}
}

// A DOOR SALE OR A SALE IMPORT: the buyer is a name somebody else typed, and
// the Sale is left entirely unassigned. Neither channel has a recording
// endpoint yet, so each is staged in SQL from an Online Sale — the same way
// the export tests stage them.
func TestBackfillLeavesDoorAndImportedSalesAlone(t *testing.T) {
	f := newSkipFixture(t, "Door Fest", "door-fest")

	t.Run("door sale", func(t *testing.T) {
		saleID, ref := f.buy(t, "door@example.com", 2)
		moveSaleToTheDoor(t, f.env, ref)

		executeMigration(t, f.env, selfHeldBackfill)

		door := customerSignIn(t, f.env, "door@example.com")
		assertSaleUnassignedEverywhere(t, f, door, saleID, "a door sale")
	})

	t.Run("sale import", func(t *testing.T) {
		saleID, _ := f.buy(t, "import@example.com", 2)
		res, err := f.env.db.Exec(`
			UPDATE ticket_sales SET channel = 'import', source = 'direct', payment_method = 'cash'
			WHERE id = $1
		`, saleID)
		if err != nil {
			t.Fatalf("move the sale onto the import channel: %v", err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			t.Fatalf("moving the sale to import affected %d rows, want 1", n)
		}

		executeMigration(t, f.env, selfHeldBackfill)

		imported := customerSignIn(t, f.env, "import@example.com")
		assertSaleUnassignedEverywhere(t, f, imported, saleID, "a Sale Import")
	})
}

// A HOLDER EMAIL DIFFERING ONLY IN CASE OR WHITESPACE from the buyer's still
// counts as "already holds". The product writes both addresses normalised, so
// the raw forms are staged in SQL on BOTH sides: the migration's comparison,
// not the checkout's, is what is under test.
func TestBackfillCountsACaseVariantHolderEmailAsAlreadyHeld(t *testing.T) {
	f := newSkipFixture(t, "Case Fest", "case-fest")
	ana := customerSignIn(t, f.env, "ana@example.com")
	saleID, _ := f.buy(t, "ana@example.com", 2)
	second := f.ticketAt(t, ana, saleID, 2)
	f.env.email.Reset()
	assignTicketOK(t, f.env, ana, saleID, second, "ana@example.com")
	acceptAssignmentOK(t, f.env, assignmentTokenFrom(t, assignmentMailFor(t, f.env, "ana@example.com")))

	if _, err := f.env.db.Exec(`UPDATE tickets SET holder_email = '  ANA@Example.com ' WHERE id = $1`, second); err != nil {
		t.Fatalf("stage a raw holder address: %v", err)
	}
	if _, err := f.env.db.Exec(`UPDATE ticket_sales SET customer_email = ' Ana@EXAMPLE.com' WHERE id = $1`, saleID); err != nil {
		t.Fatalf("stage a raw buyer address: %v", err)
	}

	executeMigration(t, f.env, selfHeldBackfill)

	rows := holderRows(t, f.env, saleID)
	if len(rows) != 2 {
		t.Fatalf("sale has %d Tickets, want 2", len(rows))
	}
	for _, r := range rows {
		if r.ticketID != second && r.holder.Valid {
			t.Errorf("Ticket 1 was backfilled to %q although the buyer already holds Ticket 2 as %q",
				r.holder.String, "  ANA@Example.com ")
		}
	}
	var accepted int
	for _, r := range listOutstanding(t, f.env, f.sessionID, f.eventID).Data {
		if r.TicketSaleID == saleID && r.AssignmentState == "accepted" {
			accepted++
		}
	}
	if accepted != 1 {
		t.Errorf("the Holder List shows %d accepted Tickets on the Sale, want 1", accepted)
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
