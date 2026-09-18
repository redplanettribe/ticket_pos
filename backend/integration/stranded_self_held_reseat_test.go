package integration

import (
	"database/sql"
	"testing"
	"time"
)

// MIGRATION 122 MOVES THE STRANDED BUYER ONTO THE TICKET THEY PAID FOR (#647,
// parent #645, ADR 0074). ADR 0048 seated the buyer on the Sale's CATALOG-FIRST
// Ticket; catalogs are listed cheap-to-dear, so on a mixed basket that meant the
// cheapest thing in it, and where the cheapest thing was free it meant the
// giveaway. #646 moved the commit spine to the Sale's DEAREST line, which fixes
// every Sale made after it and no Sale already made. This migration is the other
// half, and these are its tests.
//
// WHAT IS UNDER TEST IS A PREDICATE, not three confirmation references. The
// migration names no Sale; it states the shape it corrects — an active Online
// Sale whose buyer's one Ticket cost nothing, where the dearest line's Ticket is
// held by NOBODY — and every test below is one shape either admitted or refused
// by it. The shapes refused are the ones production actually contains: a buyer
// holding both Tickets, and a buyer who gave the paid Ticket to a friend.
//
// Each case stages the old rule's output the only way it can be produced now —
// a real checkout, then the seat moved back onto the free Ticket in SQL, which
// is byte-for-byte the state migration 084 left a pre-0074 Sale in. What is
// asserted is what the buyer sees on their Sale page and what the Organizer sees
// on the Holder List, with the holder columns read directly only for the
// promises no surface makes: the timestamps and idempotence.

const reseatStranded = "122_reseat_stranded_self_held_tickets.sql"

// strandedFixture is the catalog production has: a free Ticket Type and a
// dearer paid one on one Event, which is the only way to make the shape.
type strandedFixture struct {
	env       *testEnv
	sessionID string
	eventID   string
	freeID    string
	paidID    string
	slug      string
}

// strandedPaidCents is what the dearer Ticket Type costs in every fixture here.
// One figure rather than a parameter: no test in this file turns on the amount,
// only on the fact that one line cost money and the other cost nothing.
const strandedPaidCents = 3000

// newStrandedFixture publishes an Event whose "GA" costs nothing and whose
// "Senior" costs money, mirroring the production Event where the Community
// Ticket was free and the Community Senior was not.
func newStrandedFixture(t *testing.T, name, slug string) strandedFixture {
	t.Helper()
	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID, freeID := publishCheckoutEvent(t, env, sessionID, name, slug, 0, 40)
	paidID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "Senior", strandedPaidCents, 40)
	return strandedFixture{env: env, sessionID: sessionID, eventID: eventID, freeID: freeID, paidID: paidID, slug: slug}
}

// buyMixed makes one Online Sale of one free Ticket and one paid Ticket — the
// basket ADR 0074 opened on — and returns the Sale and its reference.
func (f strandedFixture) buyMixed(t *testing.T, email string) (saleID, ref string) {
	t.Helper()
	begun := beginCheckoutOK(t, f.env, "test-org", f.slug,
		checkoutBody(email, "Ana", "Lopez", cartLine(f.freeID, 1), cartLine(f.paidID, 1)))
	settled := confirmCheckoutOK(t, f.env, begun.ClientTransactionID, "approved")
	return saleIDOfPayment(t, f.env, begun.ClientTransactionID), settled.ConfirmationRef
}

// strand puts the Sale back into the state the OLD rule left it in: the buyer
// seated on the free Ticket, the paid Ticket unassigned. It returns the two.
func (f strandedFixture) strand(t *testing.T, saleID string) (freeTicket, paidTicket string) {
	t.Helper()
	freeTicket = ticketOfType(t, f.env, saleID, f.freeID)
	paidTicket = ticketOfType(t, f.env, saleID, f.paidID)
	seatBuyerOn(t, f.env, saleID, freeTicket)
	return freeTicket, paidTicket
}

// ticketOfType names the first Ticket of the Sale's line of one Ticket Type.
//
// SQL because no surface addresses a Ticket that way: the buyer's Sale page
// names Tickets by Ticket Type NAME and ordinal, and these fixtures hold the
// Ticket Type's id. Reading it here also means a test can name its two Tickets
// before opening any surface, which the staging needs.
func ticketOfType(t *testing.T, env *testEnv, saleID, ticketTypeID string) string {
	t.Helper()
	var id string
	if err := env.db.QueryRow(`
		SELECT tk.id FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		WHERE l.ticket_sale_id = $1 AND l.ticket_type_id = $2
		ORDER BY tk.ordinal
		LIMIT 1
	`, saleID, ticketTypeID).Scan(&id); err != nil {
		t.Fatalf("Ticket of type %s on sale %s: %v", ticketTypeID, saleID, err)
	}
	return id
}

// seatBuyerOn writes the seat the way migration 084's rule wrote it: every other
// Ticket of the Sale unassigned, this one assigned to the buyer and accepted,
// both stamps the Sale's own created_at. A test that staged a seat any other way
// would be testing its own SQL rather than the state production is in.
func seatBuyerOn(t *testing.T, env *testEnv, saleID, ticketID string) {
	t.Helper()
	clearSelfHeld(t, env, saleID)
	res, err := env.db.Exec(`
		UPDATE tickets tk
		SET holder_email = lower(btrim(ts.customer_email)),
		    holder_customer_id = ts.customer_id,
		    assigned_at = ts.created_at,
		    accepted_at = ts.created_at
		FROM ticket_sale_lines l
		JOIN ticket_sales ts ON ts.id = l.ticket_sale_id
		WHERE l.id = tk.ticket_sale_line_id AND tk.id = $1
	`, ticketID)
	if err != nil {
		t.Fatalf("seat the buyer on %s: %v", ticketID, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("seating the buyer on %s affected %d rows, want 1", ticketID, n)
	}
}

// seatOf is the one Ticket of a Sale its buyer holds, or "" where they hold
// none. It fails on two, which is the ambiguity the migration refuses and which
// no fixture here means to stage by accident.
func seatOf(t *testing.T, env *testEnv, saleID string) string {
	t.Helper()
	// Who the buyer is, is a fact about the SALE and is read once, before the
	// Tickets are walked.
	var buyer string
	if err := env.db.QueryRow(
		`SELECT lower(btrim(customer_email)) FROM ticket_sales WHERE id = $1`, saleID).Scan(&buyer); err != nil {
		t.Fatalf("read the buyer of %s: %v", saleID, err)
	}
	var seats []string
	for _, r := range holderRows(t, env, saleID) {
		if !r.holder.Valid {
			continue
		}
		if r.holder.String == buyer {
			seats = append(seats, r.ticketID)
		}
	}
	switch len(seats) {
	case 0:
		return ""
	case 1:
		return seats[0]
	default:
		t.Fatalf("the buyer holds %d Tickets of sale %s", len(seats), saleID)
		return ""
	}
}

// untouchables is everything the correction promises not to move: the Sale's
// money, its Tax Invoices, and the Event's capacity. SQL on every count, because
// the promise is about rows and not about a reading — a surface that recomputed
// one of these from the Tickets would agree with itself while the stored figure
// drifted, which is exactly the regression worth catching.
type untouchables struct {
	amountCents int
	invoices    int
	soldCount   int
}

func readUntouchables(t *testing.T, env *testEnv, saleID string) untouchables {
	t.Helper()
	var u untouchables
	if err := env.db.QueryRow(`
		SELECT
			coalesce((
				SELECT sum(l.quantity * l.unit_price_cents)
				FROM ticket_sale_lines l WHERE l.ticket_sale_id = ts.id
			), 0),
			(SELECT count(*) FROM invoicing_invoices i WHERE i.ticket_sale_id = ts.id),
			coalesce((
				SELECT sum(tt.sold_count) FROM ticket_types tt WHERE tt.event_id = ts.event_id
			), 0)
		FROM ticket_sales ts WHERE ts.id = $1
	`, saleID).Scan(&u.amountCents, &u.invoices, &u.soldCount); err != nil {
		t.Fatalf("read what %s must keep: %v", saleID, err)
	}
	return u
}

// saleCreatedAt is the instant the Sale was recorded at — the stamp the
// migration must write, and the only stamp that makes the corrected row
// indistinguishable from one the spine wrote itself.
func saleCreatedAt(t *testing.T, env *testEnv, saleID string) time.Time {
	t.Helper()
	var at time.Time
	if err := env.db.QueryRow(`SELECT created_at FROM ticket_sales WHERE id = $1`, saleID).Scan(&at); err != nil {
		t.Fatalf("read created_at of %s: %v", saleID, err)
	}
	return at
}

// buyerVerification is the Sale's Customer and whether that Customer has proved
// their address, as a comparable value.
func buyerVerification(t *testing.T, env *testEnv, saleID string) (customerID string, verified sql.NullTime) {
	t.Helper()
	if err := env.db.QueryRow(`
		SELECT ts.customer_id, c.verified_at
		FROM ticket_sales ts JOIN customers c ON c.id = ts.customer_id
		WHERE ts.id = $1
	`, saleID).Scan(&customerID, &verified); err != nil {
		t.Fatalf("read the buyer of %s: %v", saleID, err)
	}
	return customerID, verified
}

// ticketCount is how many Tickets a Sale has, asserted before and after every
// run: this migration destroys nothing.
func ticketCount(t *testing.T, env *testEnv, saleID string) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`
		SELECT count(*) FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		WHERE l.ticket_sale_id = $1
	`, saleID).Scan(&n); err != nil {
		t.Fatalf("count Tickets of %s: %v", saleID, err)
	}
	return n
}

// THE CORRECTION ITSELF: the seat moves off the giveaway and onto the Ticket the
// buyer paid for, the free Ticket goes back to `unassigned`, nothing is
// destroyed, nobody is mailed and nobody is made Verified.
func TestReseatMovesTheStrandedBuyerOntoTheTicketTheyPaidFor(t *testing.T) {
	f := newStrandedFixture(t, "Stranded Fest", "stranded-fest")
	saleID, _ := f.buyMixed(t, "ana@example.com")
	freeTicket, paidTicket := f.strand(t, saleID)
	if seatOf(t, f.env, saleID) != freeTicket {
		t.Fatal("the fixture did not seat the buyer on the free Ticket")
	}
	before := captureMailBaseline(f.env)
	tickets := ticketCount(t, f.env, saleID)
	keeps := readUntouchables(t, f.env, saleID)
	created := saleCreatedAt(t, f.env, saleID)
	// WHETHER THE BUYER IS VERIFIED IS READ ON BOTH SIDES rather than asserted to
	// be NULL afterwards. Since ADR 0054 a checkout needs a Customer Session, so
	// every buyer these fixtures can make is ALREADY Verified by signing in, and
	// "still NULL" is a claim this package can no longer stage. What is asserted
	// is the claim that survives: the correction does not touch the column, in
	// either direction — the sign-in module keeps the authority over it and
	// paying is not Proof of Email Ownership (ADR 0048).
	customerID, verifiedBefore := buyerVerification(t, f.env, saleID)

	executeMigration(t, f.env, reseatStranded)

	assertNoAssignmentMailWasSent(t, f.env, before)
	if got := ticketCount(t, f.env, saleID); got != tickets {
		t.Errorf("the Sale has %d Tickets after the correction, want %d — nothing is destroyed", got, tickets)
	}
	if got := readUntouchables(t, f.env, saleID); got != keeps {
		t.Errorf("the correction moved money, a Tax Invoice or capacity: %+v, want %+v", got, keeps)
	}
	if _, verifiedAfter := buyerVerification(t, f.env, saleID); verifiedAfter != verifiedBefore {
		t.Errorf("the buyer's verified_at moved from %v to %v; the correction never touches it", verifiedBefore, verifiedAfter)
	}
	paid := readTicketAssignment(t, f.env, paidTicket)
	if paid.holderEmail.String != "ana@example.com" || paid.holderCustomerID.String != customerID {
		t.Errorf("the paid Ticket reads holder=%q customer=%q, want ana@example.com / the Sale's Customer %s",
			paid.holderEmail.String, paid.holderCustomerID.String, customerID)
	}
	if !paid.assignedAt.Valid || !paid.assignedAt.Time.Equal(created) ||
		!paid.acceptedAt.Valid || !paid.acceptedAt.Time.Equal(created) {
		t.Errorf("the paid Ticket reads assigned_at=%v accepted_at=%v, want both = the Sale's created_at %v",
			paid.assignedAt.Time, paid.acceptedAt.Time, created)
	}
	free := readTicketAssignment(t, f.env, freeTicket)
	if free.holderEmail.Valid || free.holderCustomerID.Valid || free.assignedAt.Valid || free.acceptedAt.Valid {
		t.Errorf("the free Ticket still reads holder=%v assigned=%v accepted=%v; the seat MOVED",
			free.holderEmail, free.assignedAt, free.acceptedAt)
	}

	ana := customerSignIn(t, f.env, "ana@example.com")
	assertHeldByBuyer(t, listBuyerTickets(t, f.env, ana, saleID), paidTicket, "ana@example.com")

	var seenPaid, seenFree bool
	for _, r := range listOutstanding(t, f.env, f.sessionID, f.eventID).Data {
		switch r.TicketID {
		case paidTicket:
			seenPaid = true
			if r.AssignmentState != "accepted" || r.HolderEmail != "ana@example.com" ||
				r.HolderFirstName != "Ana" || r.HolderLastName != "Lopez" {
				t.Errorf("the Holder List shows the paid Ticket as state=%q holder=%q %q %q",
					r.AssignmentState, r.HolderEmail, r.HolderFirstName, r.HolderLastName)
			}
		case freeTicket:
			seenFree = true
			if r.AssignmentState != "unassigned" || r.HolderEmail != "" {
				t.Errorf("the Holder List shows the free Ticket as state=%q holder=%q, want unassigned",
					r.AssignmentState, r.HolderEmail)
			}
		}
	}
	if !seenPaid || !seenFree {
		t.Fatalf("the Holder List shows the paid Ticket=%v and the free Ticket=%v; both are on the roster", seenPaid, seenFree)
	}
}

// EACH CORRECTED TICKET CARRIES ITS OWN SALE'S PURCHASE TIME, which is the whole
// of what "indistinguishable from a row the spine wrote" means for the stamps.
//
// THE CLOCK IS ADVANCED BETWEEN THE TWO SALES DELIBERATELY. Every Sale in this
// package is recorded at the harness's FIXED clock, so two Sales made in one
// test share a created_at to the nanosecond and an assertion that each corrected
// row carries ITS OWN Sale's stamp would pass against a migration that copied
// the other Sale's. Moving the clock is what makes the two stamps distinguishable
// and the assertion real. It is also the shape production is in: these Sales
// were made minutes and months apart.
func TestReseatStampsEachSaleWithItsOwnPurchaseTime(t *testing.T) {
	f := newStrandedFixture(t, "Two Fest", "two-fest")
	defer moveClockTo(t, fixedClock)

	firstSale, _ := f.buyMixed(t, "ana@example.com")
	_, firstPaid := f.strand(t, firstSale)

	later := fixedClock.Add(90 * time.Minute)
	moveClockTo(t, later)
	secondSale, _ := f.buyMixed(t, "bea@example.com")
	_, secondPaid := f.strand(t, secondSale)

	firstCreated := saleCreatedAt(t, f.env, firstSale)
	secondCreated := saleCreatedAt(t, f.env, secondSale)
	if !secondCreated.After(firstCreated) {
		t.Fatalf("both Sales were recorded at %v; the clock did not move and this test cannot tell the stamps apart", firstCreated)
	}

	executeMigration(t, f.env, reseatStranded)

	for _, c := range []struct {
		what    string
		ticket  string
		created time.Time
	}{
		{"the first Sale", firstPaid, firstCreated},
		{"the second Sale", secondPaid, secondCreated},
	} {
		row := readTicketAssignment(t, f.env, c.ticket)
		if !row.assignedAt.Valid || !row.assignedAt.Time.Equal(c.created) ||
			!row.acceptedAt.Valid || !row.acceptedAt.Time.Equal(c.created) {
			t.Errorf("%s reads assigned_at=%v accepted_at=%v, want both = its own created_at %v",
				c.what, row.assignedAt.Time, row.acceptedAt.Time, c.created)
		}
	}
}

// A DEARER TICKET SOMEBODY HOLDS IS NEVER TAKEN, whoever the holder is. This is
// the whole safety argument, and production contains both shapes: two buyers
// hold both Tickets of their Sale, and one gave the paid Ticket to a friend.
func TestReseatLeavesTheDearerTicketsHolderAlone(t *testing.T) {
	f := newStrandedFixture(t, "Held Fest", "held-fest")
	ana := customerSignIn(t, f.env, "ana@example.com")

	// assertSeatDidNotMove reads the seat back off the buyer's own Sale page:
	// the buyer still holds the free Ticket and holds nothing else.
	assertSeatDidNotMove := func(t *testing.T, saleID, freeTicket, paidTicket, wantPaidState, wantPaidHolder string) {
		t.Helper()
		if got := seatOf(t, f.env, saleID); got != freeTicket {
			t.Errorf("the seat moved to %q; a Ticket somebody holds is never taken from them", got)
		}
		for _, tk := range listBuyerTickets(t, f.env, ana, saleID) {
			switch tk.TicketID {
			case freeTicket:
				if tk.AssignmentState != "accepted" || !tk.SelfHeld {
					t.Errorf("the free Ticket reads state=%q self_held=%v, want the buyer still on it",
						tk.AssignmentState, tk.SelfHeld)
				}
			case paidTicket:
				if tk.AssignmentState != wantPaidState || tk.HolderEmail != wantPaidHolder {
					t.Errorf("the paid Ticket reads state=%q holder=%q, want %q by %q",
						tk.AssignmentState, tk.HolderEmail, wantPaidState, wantPaidHolder)
				}
			}
		}
	}

	t.Run("assigned to a friend who has not answered", func(t *testing.T) {
		saleID, _ := f.buyMixed(t, "ana@example.com")
		freeTicket, paidTicket := f.strand(t, saleID)
		assignTicketOK(t, f.env, ana, saleID, paidTicket, "carla@example.com")

		executeMigration(t, f.env, reseatStranded)

		assertSeatDidNotMove(t, saleID, freeTicket, paidTicket, "assigned", "carla@example.com")
	})

	t.Run("accepted by a friend", func(t *testing.T) {
		saleID, _ := f.buyMixed(t, "ana@example.com")
		freeTicket, paidTicket := f.strand(t, saleID)
		f.env.email.Reset()
		assignTicketOK(t, f.env, ana, saleID, paidTicket, "carla@example.com")
		acceptAssignmentOK(t, f.env, assignmentTokenFrom(t, assignmentMailFor(t, f.env, "carla@example.com")))

		executeMigration(t, f.env, reseatStranded)

		assertSeatDidNotMove(t, saleID, freeTicket, paidTicket, "accepted", "carla@example.com")
	})

	t.Run("the buyer already holds both", func(t *testing.T) {
		saleID, _ := f.buyMixed(t, "ana@example.com")
		freeTicket, paidTicket := f.strand(t, saleID)
		f.env.email.Reset()
		assignTicketOK(t, f.env, ana, saleID, paidTicket, "ana@example.com")
		acceptAssignmentOK(t, f.env, assignmentTokenFrom(t, assignmentMailFor(t, f.env, "ana@example.com")))

		executeMigration(t, f.env, reseatStranded)

		// The seat is ambiguous — the buyer holds two Tickets of one Sale — so
		// nothing moves and nothing is released. seatOf fails on two, so the
		// columns are read directly here.
		free := readTicketAssignment(t, f.env, freeTicket)
		paid := readTicketAssignment(t, f.env, paidTicket)
		if !free.acceptedAt.Valid || free.holderEmail.String != "ana@example.com" {
			t.Errorf("the free Ticket reads holder=%v accepted=%v; a buyer holding both keeps both",
				free.holderEmail, free.acceptedAt)
		}
		if !paid.acceptedAt.Valid || paid.holderEmail.String != "ana@example.com" {
			t.Errorf("the paid Ticket reads holder=%v accepted=%v; a buyer holding both keeps both",
				paid.holderEmail, paid.acceptedAt)
		}
	})
}

// ONLY AN ACTIVE SALE IS CORRECTED. A reversed Sale's Tickets admit nobody, so
// moving a seat between them would put a no-show on a roster. A REFUSED Reversal
// Request is not a reversal and does not disqualify anything — the schema's own
// definition of a live request (migration 039's partial unique index) is the one
// used, which is 088's narrowing of 084 rather than 084's "whatever became of it".
func TestReseatCorrectsOnlyAnActiveSale(t *testing.T) {
	f := newStrandedFixture(t, "Active Fest", "active-fest")

	t.Run("reversed by an operator", func(t *testing.T) {
		saleID, ref := f.buyMixed(t, "rev@example.com")
		_, paidTicket := f.strand(t, saleID)
		operatorReverseOK(t, payphoneEnv, operatorSession(t, f.env, "operator@example.com"), ref,
			operatorReversalBody{RefundedAmountCents: intPtr(3000), PlatformFeeKept: boolPtr(false), Note: strPtr("refunded")})

		executeMigration(t, f.env, reseatStranded)

		if got := readTicketAssignment(t, f.env, paidTicket); got.holderEmail.Valid {
			t.Errorf("a reversed Sale's paid Ticket was given to %q; its Tickets admit nobody", got.holderEmail.String)
		}
	})

	t.Run("a live Reversal Request", func(t *testing.T) {
		saleID, _ := f.buyMixed(t, "req@example.com")
		freeTicket, paidTicket := f.strand(t, saleID)
		insertReversalRequest(t, f.env, saleID, "tx-live-122", "in_flight")

		executeMigration(t, f.env, reseatStranded)

		if got := seatOf(t, f.env, saleID); got != freeTicket {
			t.Errorf("the seat moved on a Sale with a live Reversal Request")
		}
		if got := readTicketAssignment(t, f.env, paidTicket); got.holderEmail.Valid {
			t.Errorf("the paid Ticket was given to %q while the Sale's reversal is in flight", got.holderEmail.String)
		}
	})

	t.Run("a refused Reversal Request", func(t *testing.T) {
		saleID, _ := f.buyMixed(t, "kept@example.com")
		_, paidTicket := f.strand(t, saleID)
		insertReversalRequest(t, f.env, saleID, "tx-refused-122", "refused")

		executeMigration(t, f.env, reseatStranded)

		if got := seatOf(t, f.env, saleID); got != paidTicket {
			t.Errorf("the seat is on %q, want the paid Ticket — a refused request means nothing happened and the buyer is still coming", got)
		}
	})
}

// insertReversalRequest stages a buyer's Reversal Request in one of its states,
// which no route reaches for a Sale whose money nobody is moving.
func insertReversalRequest(t *testing.T, env *testEnv, saleID, clientTransactionID, status string) {
	t.Helper()
	if _, err := env.db.Exec(`
		INSERT INTO sale_reversals (ticket_sale_id, client_transaction_id, requested_at, status, next_attempt_at)
		VALUES ($1, $2, NOW(), $3, NOW())
	`, saleID, clientTransactionID, status); err != nil {
		t.Fatalf("stage a %s reversal request: %v", status, err)
	}
}

// A BUYER MIS-SEATED ON THE CHEAPER OF TWO PAID TICKETS IS LEFT ALONE. The zero
// boundary is ADR 0074's and is not re-argued here: a Ticket that cost money has
// a buyer who paid for it, and "which of the two is yours" has a real answer on
// both sides that the platform does not know. The remedy for those is
// reassignment, exactly as ADR 0048 said.
func TestReseatLeavesACheaperPaidSeatAlone(t *testing.T) {
	f := newStrandedFixture(t, "Paid Fest", "paid-fest")
	cheapID := createTicketTypeWithCapacity(t, f.env, f.sessionID, f.eventID, "Cheap", 500, 40)

	begun := beginCheckoutOK(t, f.env, "test-org", f.slug,
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(cheapID, 1), cartLine(f.paidID, 1)))
	confirmCheckoutOK(t, f.env, begun.ClientTransactionID, "approved")
	saleID := saleIDOfPayment(t, f.env, begun.ClientTransactionID)
	cheapTicket := ticketOfType(t, f.env, saleID, cheapID)
	seatBuyerOn(t, f.env, saleID, cheapTicket)

	executeMigration(t, f.env, reseatStranded)

	if got := seatOf(t, f.env, saleID); got != cheapTicket {
		t.Errorf("the seat moved off a PAID Ticket to %q; only a Ticket that cost nothing is corrected", got)
	}
}

// AN IN-PERSON SALE AND A SALE IMPORT ARE NOT IN THIS SHAPE and are never
// touched: an In-Person Sale has no Self-held Ticket at all, and an imported
// Sale's seat was migration 088's. Neither channel has a recording endpoint that
// mixes a free line with a paid one, so each is staged in SQL from an Online
// Sale, the way the export and backfill tests stage them.
func TestReseatLeavesOtherChannelsAlone(t *testing.T) {
	f := newStrandedFixture(t, "Channel Fest", "channel-fest")

	t.Run("in-person sale", func(t *testing.T) {
		saleID, ref := f.buyMixed(t, "door@example.com")
		freeTicket, _ := f.strand(t, saleID)
		moveSaleToTheDoor(t, f.env, ref)

		executeMigration(t, f.env, reseatStranded)

		if got := seatOf(t, f.env, saleID); got != freeTicket {
			t.Errorf("the seat moved to %q on an In-Person Sale", got)
		}
	})

	t.Run("sale import", func(t *testing.T) {
		saleID, _ := f.buyMixed(t, "import@example.com")
		freeTicket, _ := f.strand(t, saleID)
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

		executeMigration(t, f.env, reseatStranded)

		if got := seatOf(t, f.env, saleID); got != freeTicket {
			t.Errorf("the seat moved to %q on a Sale Import", got)
		}
	})
}

// THE CROSS-SALE DOUBLE-HOLDER IS NOT TOUCHED — the 27 buyers who took a free
// Ticket and came back later for a paid one. Each of their Sales has exactly one
// seat and no dearer unheld line, so the predicate excludes them by construction
// and not by a clause. Moving a seat ACROSS Sales would mean reversing one of
// them, which is an Upgrade: the buyer's election, never the platform's.
//
// THE CLOCK IS ADVANCED BETWEEN THE TWO SALES for the reason the stamp test
// gives — two Sales on the fixed clock are recorded at the same instant, and
// "the later Sale" would then be whichever the database happened to return
// first. Ten minutes is the gap ADR 0074 measured on several of the 27.
func TestReseatLeavesCrossSaleDoubleHoldersAlone(t *testing.T) {
	f := newStrandedFixture(t, "Return Fest", "return-fest")
	defer moveClockTo(t, fixedClock)

	freeSale := saleIDOfRef(t, f.env, claimFree(t, f.env, f.slug, f.freeID, "ana@example.com", 1))

	moveClockTo(t, fixedClock.Add(10*time.Minute))
	paidBegun := beginCheckoutOK(t, f.env, "test-org", f.slug,
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(f.paidID, 1)))
	confirmCheckoutOK(t, f.env, paidBegun.ClientTransactionID, "approved")
	paidSale := saleIDOfPayment(t, f.env, paidBegun.ClientTransactionID)
	if !saleCreatedAt(t, f.env, paidSale).After(saleCreatedAt(t, f.env, freeSale)) {
		t.Fatal("both Sales were recorded at the same instant; the clock did not move")
	}

	freeSeat := seatOf(t, f.env, freeSale)
	paidSeat := seatOf(t, f.env, paidSale)
	if freeSeat == "" || paidSeat == "" {
		t.Fatalf("the fixture seated the buyer on free=%q paid=%q; both Sales seat their own buyer", freeSeat, paidSeat)
	}

	executeMigration(t, f.env, reseatStranded)

	if got := seatOf(t, f.env, freeSale); got != freeSeat {
		t.Errorf("the free Sale's seat moved to %q; an Upgrade is the buyer's election", got)
	}
	if got := seatOf(t, f.env, paidSale); got != paidSeat {
		t.Errorf("the paid Sale's seat moved to %q", got)
	}
}

// RE-EXECUTING THE MIGRATION CHANGES NOTHING, on a Sale it has corrected, on one
// it deliberately refused, and on one the spine seated itself under ADR 0074.
func TestReseatIsIdempotent(t *testing.T) {
	f := newStrandedFixture(t, "Twice Fest", "twice-fest")

	corrected, _ := f.buyMixed(t, "ana@example.com")
	f.strand(t, corrected)

	refused, _ := f.buyMixed(t, "bea@example.com")
	refusedFree, refusedPaid := f.strand(t, refused)
	assignTicketOK(t, f.env, customerSignIn(t, f.env, "bea@example.com"), refused, refusedPaid, "carla@example.com")

	untouched, _ := f.buyMixed(t, "cris@example.com")
	untouchedBefore := holderRows(t, f.env, untouched)

	executeMigration(t, f.env, reseatStranded)
	correctedOnce := holderRows(t, f.env, corrected)
	refusedOnce := holderRows(t, f.env, refused)

	executeMigration(t, f.env, reseatStranded)

	if got := holderRows(t, f.env, corrected); !equalHolderRows(got, correctedOnce) {
		t.Errorf("a second run changed the corrected Sale:\n first %+v\nsecond %+v", correctedOnce, got)
	}
	if got := holderRows(t, f.env, refused); !equalHolderRows(got, refusedOnce) {
		t.Errorf("a second run changed the Sale it refused:\n first %+v\nsecond %+v", refusedOnce, got)
	}
	if got := holderRows(t, f.env, untouched); !equalHolderRows(got, untouchedBefore) {
		t.Errorf("the migration changed a Sale the spine had already seated under ADR 0074:\nbefore %+v\n after %+v",
			untouchedBefore, got)
	}
	if got := seatOf(t, f.env, refused); got != refusedFree {
		t.Errorf("the refused Sale's seat is %q, want the free Ticket it started on", got)
	}
}

// INDEPENDENT OF TICKET_ASSIGNMENT_ENABLED, like 084 and 088. The flag gates
// surfaces; a correction that waited for it would mean the data was wrong for
// as long as the flag was closed, and a backfill gated on a switch somebody can
// forget to flip has not run at all.
func TestReseatRunsWhileAssignmentIsClosed(t *testing.T) {
	f := newStrandedFixture(t, "Dark Fest", "dark-reseat-fest")
	saleID, _ := f.buyMixed(t, "ana@example.com")
	_, paidTicket := f.strand(t, saleID)
	sharedApp.CatalogService.WithTicketAssignment(false)
	sharedApp.SalesService.WithTicketAssignment(false)

	executeMigration(t, f.env, reseatStranded)

	enableTicketAssignment(t)
	ana := customerSignIn(t, f.env, "ana@example.com")
	assertHeldByBuyer(t, listBuyerTickets(t, f.env, ana, saleID), paidTicket, "ana@example.com")
}
