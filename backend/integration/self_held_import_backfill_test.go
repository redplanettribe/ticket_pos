package integration

import (
	"database/sql"
	"testing"
	"time"
)

// MIGRATION 088 HOLDS TICKET 1 FOR THE BUYER ON EXISTING IMPORTED SALES (#394,
// parent #391, ADR 0055). Migration 084 did this for Online Sales and excluded
// `import` along with `in_person`; ADR 0055 supersedes that exclusion for
// `import` alone, and every imported Sale already recorded predates the rule.
//
// Every case here stages a pre-0055 imported Sale the way one is produced: a
// real Manually Recorded Sale or Sale Import (both land on the `import`
// channel), then the Self-held Ticket's holder columns cleared in SQL — which
// is a no-op until #393 seats the buyer at record time and is exactly what
// makes these tests state the migration's own behaviour either way.
//
// What is asserted is what a buyer sees on their Sale page and what the
// Organizer sees on the Holder List, never the rows the migration wrote — the
// exceptions being the migration's own promises, timestamp provenance and
// idempotence, which have no surface at all.
const importedSelfHeldBackfill = "088_backfill_imported_self_held_ticket.sql"

// importBackfillFixture is the common staging: a published Event with both
// flags open and one Ticket Type, on an Organization whose admin can record
// sales by hand.
type importBackfillFixture struct {
	env       *testEnv
	sessionID string
	eventID   string
	gaID      string
	slug      string
}

func newImportBackfillFixture(t *testing.T, name, slug string) importBackfillFixture {
	t.Helper()
	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, name, slug, 1000, 40)
	return importBackfillFixture{env: env, sessionID: sessionID, eventID: eventID, gaID: gaID, slug: slug}
}

// record makes an imported Ticket Sale through the Manually Recorded Sale
// endpoint — the shortest real route onto the `import` channel — and clears
// the Self-held Ticket, which is the state of every imported Sale recorded
// before ADR 0055.
func (f importBackfillFixture) record(t *testing.T, email string, quantity int) string {
	t.Helper()
	rec := recordManualSaleOK(t, f.env, f.sessionID, f.eventID,
		manualSaleBody(email, "Ana", "Lopez", f.gaID, quantity, "cash", "2026-07-01T10:00:00Z"))
	clearSelfHeld(t, f.env, rec.SaleID)
	return rec.SaleID
}

// ticketAtOrdinal is the Ticket of a Sale at one ordinal, as the buyer sees it.
func (f importBackfillFixture) ticketAtOrdinal(t *testing.T, session, saleID string, ordinal int) string {
	t.Helper()
	for _, tk := range listBuyerTickets(t, f.env, session, saleID) {
		if tk.Ordinal == ordinal {
			return tk.TicketID
		}
	}
	t.Fatalf("sale %s has no Ticket #%d", saleID, ordinal)
	return ""
}

// holderListNamed counts the Holder List rows of one Sale naming an address as
// Holder — how many times the buyer is on the roster for it.
func (f importBackfillFixture) holderListNamed(t *testing.T, saleID, email string) int {
	t.Helper()
	n := 0
	for _, r := range listOutstanding(t, f.env, f.sessionID, f.eventID).Data {
		if r.TicketSaleID == saleID && r.HolderEmail == email {
			n++
		}
	}
	return n
}

// assertUnassignedEverywhere asserts that no Ticket of the Sale has a Holder,
// on the buyer's Sale page and on the Holder List — where an `unassigned` row
// is also the proof that no Holder reminder could be addressed, since every
// sweep chases holders through accepted_at and these Tickets have none.
func (f importBackfillFixture) assertUnassignedEverywhere(t *testing.T, buyerSession, saleID, what string) {
	t.Helper()
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

// everyMailBaseline is every kind of mail this platform can send at a buyer or
// a holder, plus the assignment-mail ledger, counted before the backfill runs.
//
// A BASELINE OVER EVERY CHANNEL rather than the three of mailBaseline, because
// the property ADR 0055 promises about this migration is not "no assignment
// mail" but "no mail of any kind": the backfill writes an accepted Holder onto
// 184 real addresses at once, and any one of the senders firing off the back of
// it would be a mass mailing nobody asked for. Naming each sender here is what
// makes the assertion survive a new one being added — the compiler does not
// help, so the list is the check.
type everyMailBaseline struct {
	confirmations       int
	answerReminders     int
	assignmentReminders int
	assignmentLinks     int
	noLongerHolding     int
	voided              int
	passcodes           int
	ledgerRows          int
}

func captureEveryMail(t *testing.T, env *testEnv) everyMailBaseline {
	t.Helper()
	var ledger int
	if err := env.db.QueryRow(`SELECT count(*) FROM ticket_assignment_mails`).Scan(&ledger); err != nil {
		t.Fatalf("count assignment mail ledger: %v", err)
	}
	return everyMailBaseline{
		confirmations:       len(env.email.Confirmations()),
		answerReminders:     len(env.email.HolderAnswerRemindersSent()),
		assignmentReminders: len(env.email.AssignmentRemindersSent()),
		assignmentLinks:     len(env.email.TicketAssignmentsSent()),
		noLongerHolding:     len(env.email.NoLongerHoldingsSent()),
		voided:              len(env.email.Voided()),
		passcodes:           env.email.OTPSendCount(),
		ledgerRows:          ledger,
	}
}

// assertTheBackfillMailedNobody is the acceptance criterion stated as one
// assertion. Nothing about a backfill is a message to anybody: the Ticket is
// written as though it had been held since the Sale was recorded, so there is
// no event to announce and no ledger row to write.
func assertTheBackfillMailedNobody(t *testing.T, env *testEnv, before everyMailBaseline) {
	t.Helper()
	after := captureEveryMail(t, env)
	for _, c := range []struct {
		what          string
		before, after int
		why           string
	}{
		{"Sale Confirmation", before.confirmations, after.confirmations, "the Sale was recorded long ago"},
		{"Answer Reminder", before.answerReminders, after.answerReminders, "no sweep runs inside a migration"},
		{"Assignment Reminder", before.assignmentReminders, after.assignmentReminders, "no sweep runs inside a migration"},
		{"Assignment Link", before.assignmentLinks, after.assignmentLinks, "the buyer handed the Ticket to nobody"},
		{"No Longer Holding notice", before.noLongerHolding, after.noLongerHolding, "nothing was taken from anybody"},
		{"Sale Voided notice", before.voided, after.voided, "no Sale was reversed"},
		{"passcode", before.passcodes, after.passcodes, "a presumption proves nothing and mints no session"},
	} {
		if c.after != c.before {
			t.Errorf("the backfill sent %d new %s(s); it sends no mail of any kind — %s",
				c.after-c.before, c.what, c.why)
		}
	}
	if after.ledgerRows != before.ledgerRows {
		t.Errorf("the backfill wrote %d ticket_assignment_mails row(s); it mails nobody, so it records no mail",
			after.ledgerRows-before.ledgerRows)
	}
}

// AN IMPORTED SALE OF ONE TICKET: after the backfill the buyer holds it, the
// Organizer's Holder List names them, the timestamps are the Sale's own,
// nobody was mailed, no ledger row was written and nobody was made Verified.
func TestImportBackfillHoldsAnImportedSaleForItsBuyer(t *testing.T) {
	f := newImportBackfillFixture(t, "Imported Fest", "imported-fest")
	saleID := f.record(t, "gus@example.com", 1)

	before := captureEveryMail(t, f.env)
	executeMigration(t, f.env, importedSelfHeldBackfill)
	assertTheBackfillMailedNobody(t, f.env, before)

	// The migration's own promises, read before the buyer signs in — which
	// would Verify them and make the verified_at check meaningless.
	var saleCreated time.Time
	var customerID string
	var verified sql.NullTime
	if err := f.env.db.QueryRow(`
		SELECT ts.created_at, ts.customer_id, c.verified_at
		FROM ticket_sales ts JOIN customers c ON c.id = ts.customer_id
		WHERE ts.id = $1
	`, saleID).Scan(&saleCreated, &customerID, &verified); err != nil {
		t.Fatalf("read sale: %v", err)
	}
	if verified.Valid {
		t.Errorf("the backfill marked the buyer Verified at %v; a transcription is not Proof of Email Ownership", verified.Time)
	}
	rows := holderRows(t, f.env, saleID)
	if len(rows) != 1 {
		t.Fatalf("sale has %d Tickets, want 1", len(rows))
	}
	row := rows[0]
	if !row.assignedAt.Valid || !row.assignedAt.Time.Equal(saleCreated) ||
		!row.acceptedAt.Valid || !row.acceptedAt.Time.Equal(saleCreated) {
		t.Errorf("backfilled assigned_at=%v accepted_at=%v, want both = the Sale's created_at %v",
			row.assignedAt.Time, row.acceptedAt.Time, saleCreated)
	}

	gus := customerSignIn(t, f.env, "gus@example.com")
	assertHeldByBuyer(t, listBuyerTickets(t, f.env, gus, saleID), row.ticketID, "gus@example.com")

	var seen bool
	for _, r := range listOutstanding(t, f.env, f.sessionID, f.eventID).Data {
		if r.TicketID != row.ticketID {
			continue
		}
		seen = true
		if r.AssignmentState != "accepted" || r.HolderEmail != "gus@example.com" ||
			r.HolderFirstName != "Ana" || r.HolderLastName != "Lopez" {
			t.Errorf("holder list shows state=%q holder=%q %q %q, want the buyer named as accepted",
				r.AssignmentState, r.HolderEmail, r.HolderFirstName, r.HolderLastName)
		}
	}
	if !seen {
		t.Fatal("the backfilled Ticket is not on the Holder List")
	}
}

// A MULTI-TICKET IMPORTED SALE: only the lowest ordinal is held, and the buyer
// is on the roster once.
func TestImportBackfillHoldsOnlyTheLowestOrdinalOfAnImportedSale(t *testing.T) {
	f := newImportBackfillFixture(t, "Three Import Fest", "three-import-fest")
	saleID := f.record(t, "ana@example.com", 3)

	executeMigration(t, f.env, importedSelfHeldBackfill)

	ana := customerSignIn(t, f.env, "ana@example.com")
	tickets := listBuyerTickets(t, f.env, ana, saleID)
	if len(tickets) != 3 {
		t.Fatalf("sale has %d Tickets, want 3", len(tickets))
	}
	var first string
	for _, tk := range tickets {
		if tk.Ordinal == 1 {
			first = tk.TicketID
		}
	}
	assertHeldByBuyer(t, tickets, first, "ana@example.com")
	if got := f.holderListNamed(t, saleID, "ana@example.com"); got != 1 {
		t.Errorf("the buyer appears %d times on the Holder List for this Sale, want exactly once", got)
	}
}

// THE MIGRATION KEEPS 084'S RULE, WHICH THE COMMIT SPINE HAS LEFT. 088 was
// written to be byte-for-byte 084's pick — the lowest-ordinal Ticket of the line
// whose Ticket Type sorts first by sort_order then name — and it stays that way
// now that the spine seats the buyer on the dearest line instead (ADR 0074,
// #646). Both are historical backfills, both ran once and are idempotent by
// construction, and re-cutting either to the new rule would re-seat exactly the
// buyers ADR 0074 decided not to re-seat. The two backfills still agree with
// each other; what they no longer state is the forward rule.
//
// A multi-line imported Sale is staged from a real checkout — the import routes
// record one line per Sale — so the Ticket the spine chose and the Ticket this
// migration chooses can be told apart.
func TestImportBackfillHoldsTheCatalogFirstTicketTheCommitSpineNoLongerPicks(t *testing.T) {
	f := newImportBackfillFixture(t, "Order Import Fest", "order-import-fest")
	vipID := createTicketTypeWithCapacity(t, f.env, f.sessionID, f.eventID, "VIP", 5000, 5)

	// VIP first in the cart, GA first in the catalog, VIP the dearest.
	begun := beginCheckoutOK(t, f.env, "test-org", f.slug,
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(vipID, 2), cartLine(f.gaID, 2)))
	confirmCheckoutOK(t, f.env, begun.ClientTransactionID, "approved")
	saleID := saleIDOfPayment(t, f.env, begun.ClientTransactionID)
	checkoutPick := selfHeldTicketID(t, f.env, saleID)
	clearSelfHeld(t, f.env, saleID)
	if _, err := f.env.db.Exec(`
		UPDATE ticket_sales SET channel = 'import', source = 'direct', payment_method = 'cash'
		WHERE id = $1
	`, saleID); err != nil {
		t.Fatalf("move the sale onto the import channel: %v", err)
	}

	executeMigration(t, f.env, importedSelfHeldBackfill)

	ana := customerSignIn(t, f.env, "ana@example.com")
	tickets := listBuyerTickets(t, f.env, ana, saleID)
	catalogFirst := ticketOfSale(t, tickets, "GA", 1)
	assertHeldByBuyer(t, tickets, catalogFirst, "ana@example.com")
	if checkoutPick == catalogFirst {
		t.Error("the commit spine seated the buyer on GA #1; since ADR 0074 it seats them on the dearest line's VIP #1")
	}
}

// ONLINE AND DOOR SALES ARE NOT THIS MIGRATION'S. 084 held the Online Sales
// and its residue is not disturbed here; `in_person` stays out of both, and
// ADR 0055 keeps it out pending a buyer surface rather than on principle.
func TestImportBackfillLeavesOnlineAndDoorSalesAlone(t *testing.T) {
	f := newImportBackfillFixture(t, "Untouched Fest", "untouched-fest")

	t.Run("an Online Sale", func(t *testing.T) {
		begun := beginCheckoutOK(t, f.env, "test-org", f.slug,
			checkoutBody("online@example.com", "Ana", "Lopez", cartLine(f.gaID, 2)))
		confirmCheckoutOK(t, f.env, begun.ClientTransactionID, "approved")
		saleID := saleIDOfPayment(t, f.env, begun.ClientTransactionID)
		clearSelfHeld(t, f.env, saleID)

		executeMigration(t, f.env, importedSelfHeldBackfill)

		online := customerSignIn(t, f.env, "online@example.com")
		f.assertUnassignedEverywhere(t, online, saleID, "an Online Sale")
	})

	t.Run("a door sale", func(t *testing.T) {
		begun := beginCheckoutOK(t, f.env, "test-org", f.slug,
			checkoutBody("door@example.com", "Ana", "Lopez", cartLine(f.gaID, 2)))
		settled := confirmCheckoutOK(t, f.env, begun.ClientTransactionID, "approved")
		saleID := saleIDOfPayment(t, f.env, begun.ClientTransactionID)
		clearSelfHeld(t, f.env, saleID)
		moveSaleToTheDoor(t, f.env, settled.ConfirmationRef)

		executeMigration(t, f.env, importedSelfHeldBackfill)

		if got := heldCount(t, f.env, saleID); got != 0 {
			t.Errorf("a door sale has %d held Tickets, want 0 — `in_person` can be assigned by nobody", got)
		}
	})
}

// A REVERSED IMPORTED SALE: the Ticket admits nobody, so nobody is made its
// Holder. Staff reversal and batch undo are the two ways an imported Sale is
// reversed and both write the status column.
func TestImportBackfillHoldsNothingOnAReversedImportedSale(t *testing.T) {
	t.Run("reversed by staff", func(t *testing.T) {
		f := newImportBackfillFixture(t, "Staff Reversed Fest", "staff-reversed-fest")
		saleID := f.record(t, "rev@example.com", 2)
		reverseImportedSaleOK(t, f.env, f.sessionID, f.eventID, saleID)

		executeMigration(t, f.env, importedSelfHeldBackfill)

		rev := customerSignIn(t, f.env, "rev@example.com")
		f.assertUnassignedEverywhere(t, rev, saleID, "a staff-reversed imported Sale")
	})

	t.Run("undone with its batch", func(t *testing.T) {
		f := newImportBackfillFixture(t, "Undone Fest", "undone-fest")
		batchID := commitBatch(t, f.env, f.sessionID, f.eventID, "batch-undone", []map[string]any{
			{"customer_email": "undone@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
				"ticket_type_id": f.gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		})
		var saleID string
		if err := f.env.db.QueryRow(
			`SELECT id FROM ticket_sales WHERE customer_email = $1`, "undone@example.com",
		).Scan(&saleID); err != nil {
			t.Fatalf("read the imported sale: %v", err)
		}
		clearSelfHeld(t, f.env, saleID)
		undoBatch(t, f.env, f.sessionID, f.eventID, batchID)

		executeMigration(t, f.env, importedSelfHeldBackfill)

		if got := heldCount(t, f.env, saleID); got != 0 {
			t.Errorf("an undone imported Sale has %d held Tickets, want 0", got)
		}
	})
}

// A LIVE REVERSAL REQUEST IS SKIPPED AND A REFUSED ONE IS NOT — the one place
// this migration deliberately departs from 084, which disqualified any Sale
// carrying a Reversal Request "whatever became of it". A buyer who asked for
// their money back, was refused and is still coming belongs on the roster. The
// predicate is the schema's own: `sale_reversals_live_per_sale_key` is partial
// ON `status <> 'refused'`.
func TestImportBackfillSkipsALiveReversalAndBackfillsARefusedOne(t *testing.T) {
	f := newImportBackfillFixture(t, "Reversal Fest", "reversal-fest")

	stageReversal := func(t *testing.T, saleID, tx, status string) {
		t.Helper()
		if _, err := f.env.db.Exec(`
			INSERT INTO sale_reversals (ticket_sale_id, client_transaction_id, requested_at, status, next_attempt_at)
			VALUES ($1, $2, NOW(), $3, NOW())
		`, saleID, tx, status); err != nil {
			t.Fatalf("stage a %s reversal request: %v", status, err)
		}
	}

	liveSale := f.record(t, "live@example.com", 2)
	stageReversal(t, liveSale, "tx-live", "in_flight")
	refusedSale := f.record(t, "refused@example.com", 2)
	stageReversal(t, refusedSale, "tx-refused", "refused")

	executeMigration(t, f.env, importedSelfHeldBackfill)

	live := customerSignIn(t, f.env, "live@example.com")
	f.assertUnassignedEverywhere(t, live, liveSale, "a Sale with a live Reversal Request")

	refused := customerSignIn(t, f.env, "refused@example.com")
	tickets := listBuyerTickets(t, f.env, refused, refusedSale)
	var first string
	for _, tk := range tickets {
		if tk.Ordinal == 1 {
			first = tk.TicketID
		}
	}
	assertHeldByBuyer(t, tickets, first, "refused@example.com")
	if got := f.holderListNamed(t, refusedSale, "refused@example.com"); got != 1 {
		t.Errorf("a buyer whose Reversal Request was refused appears %d times on the Holder List, want once — "+
			"a refusal means nothing happened and they are still coming", got)
	}
}

// TICKET 1 ALREADY HANDED TO A THIRD PARTY: the buyer's choice stands whether
// the friend has accepted or not, and the buyer is given nothing else. An
// imported Sale's buyer can assign — ADR 0055 is only possible because
// `import` already has a buyer surface.
func TestImportBackfillLeavesATicketOneHandedToAThirdPartyAlone(t *testing.T) {
	f := newImportBackfillFixture(t, "Handed Import Fest", "handed-import-fest")
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
		if got := f.holderListNamed(t, saleID, "ana@example.com"); got != 0 {
			t.Errorf("the buyer appears %d times on the Holder List for this Sale, want 0", got)
		}
	}

	t.Run("pending", func(t *testing.T) {
		saleID := f.record(t, "ana@example.com", 2)
		first := f.ticketAtOrdinal(t, ana, saleID, 1)
		assignTicketOK(t, f.env, ana, saleID, first, "carla@example.com")

		executeMigration(t, f.env, importedSelfHeldBackfill)

		assertUntouched(t, saleID, first, "assigned")
	})

	t.Run("accepted", func(t *testing.T) {
		saleID := f.record(t, "ana@example.com", 2)
		first := f.ticketAtOrdinal(t, ana, saleID, 1)
		f.env.email.Reset()
		assignTicketOK(t, f.env, ana, saleID, first, "carla@example.com")
		acceptAssignmentOK(t, f.env, assignmentTokenFrom(t, assignmentMailFor(t, f.env, "carla@example.com")))

		executeMigration(t, f.env, importedSelfHeldBackfill)

		assertUntouched(t, saleID, first, "accepted")
	})
}

// THE BUYER ALREADY HOLDS ANOTHER TICKET OF THE SALE BY ITS ASSIGNMENT LINK:
// one Ticket, not two, and the buyer is on the roster once. This is the skip
// that leaves migration 084's residue exactly where it is.
func TestImportBackfillHoldsNoSecondTicketForABuyerHoldingOneByLink(t *testing.T) {
	f := newImportBackfillFixture(t, "Linked Import Fest", "linked-import-fest")
	ana := customerSignIn(t, f.env, "ana@example.com")
	saleID := f.record(t, "ana@example.com", 3)
	third := f.ticketAtOrdinal(t, ana, saleID, 3)
	f.env.email.Reset()
	assignTicketOK(t, f.env, ana, saleID, third, "Ana@Example.com")
	acceptAssignmentOK(t, f.env, assignmentTokenFrom(t, assignmentMailFor(t, f.env, "ana@example.com")))

	executeMigration(t, f.env, importedSelfHeldBackfill)

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
	if got := f.holderListNamed(t, saleID, "ana@example.com"); got != 1 {
		t.Errorf("the buyer appears %d times on the Holder List for this Sale, want exactly once", got)
	}
}

// RE-EXECUTING THE MIGRATION CHANGES NOTHING, on a Sale it backfilled and on
// one whose Ticket 1 was already the buyer's before it ever ran.
//
// The second Sale is the case a replay on a live database actually meets, and
// it is the case #393 makes universal: once imported Sales seat their buyer at
// record time, EVERY imported Sale looks like this one, and a migration that
// restamped them would silently move 184 holder timestamps.
func TestImportBackfillIsIdempotent(t *testing.T) {
	f := newImportBackfillFixture(t, "Twice Import Fest", "twice-import-fest")
	oldSale := f.record(t, "ana@example.com", 2)

	heldSale := f.record(t, "bob@example.com", 2)
	bob := customerSignIn(t, f.env, "bob@example.com")
	firstOfHeld := f.ticketAtOrdinal(t, bob, heldSale, 1)
	f.env.email.Reset()
	assignTicketOK(t, f.env, bob, heldSale, firstOfHeld, "bob@example.com")
	acceptAssignmentOK(t, f.env, assignmentTokenFrom(t, assignmentMailFor(t, f.env, "bob@example.com")))
	heldBefore := holderRows(t, f.env, heldSale)

	executeMigration(t, f.env, importedSelfHeldBackfill)
	oldOnce := holderRows(t, f.env, oldSale)
	if got := holderRows(t, f.env, heldSale); !equalHolderRows(got, heldBefore) {
		t.Errorf("the first run changed a Sale already held:\nbefore %+v\n after %+v", heldBefore, got)
	}

	executeMigration(t, f.env, importedSelfHeldBackfill)
	if got := holderRows(t, f.env, oldSale); !equalHolderRows(got, oldOnce) {
		t.Errorf("a second run changed the backfilled Sale:\n first %+v\nsecond %+v", oldOnce, got)
	}
	if got := holderRows(t, f.env, heldSale); !equalHolderRows(got, heldBefore) {
		t.Errorf("a second run changed a Sale already held:\nbefore %+v\n after %+v", heldBefore, got)
	}
}

// INDEPENDENT OF TICKET_ASSIGNMENT_ENABLED. A backfill is a one-shot act, and
// a gated one running while the flag was closed would mean the data never
// existed at all — so it runs with the flag shut and the roster is named the
// moment it opens.
func TestImportBackfillRunsWhileAssignmentIsClosed(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Dark Import Fest", "dark-import-fest", 1000, 10)
	rec := recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	saleID := rec.SaleID
	clearSelfHeld(t, env, saleID)

	executeMigration(t, env, importedSelfHeldBackfill)

	enableTicketQuestions(t)
	enableTicketAssignment(t)
	ana := customerSignIn(t, env, "ana@example.com")
	tickets := listBuyerTickets(t, env, ana, saleID)
	var first string
	for _, tk := range tickets {
		if tk.Ordinal == 1 {
			first = tk.TicketID
		}
	}
	assertHeldByBuyer(t, tickets, first, "ana@example.com")

	var accepted int
	for _, r := range listOutstanding(t, env, sessionID, eventID).Data {
		if r.TicketSaleID == saleID && r.AssignmentState == "accepted" {
			accepted++
		}
	}
	if accepted != 1 {
		t.Errorf("holder list shows %d accepted Tickets on the Sale, want 1", accepted)
	}
}
