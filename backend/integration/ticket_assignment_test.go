package integration

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The buyer assigns a Ticket to an email address (#324, parent #322).
//
// WHAT THIS SLICE IS, AND WHAT IT DELIBERATELY IS NOT. A Customer who bought
// four tickets gives an address for each one from their own sale page, reassigns
// when a friend drops out, corrects a typo, and — for the first time — can tell
// their four otherwise identical Tickets apart. NO MAIL IS SENT. There is no
// Assignment Link and no accept flow here; `accepted` is modelled and
// unreachable, and #325 is what makes it reachable. Several tests below assert
// that silence rather than assuming it, because "we did not build the mail yet"
// and "the mail is not sent" look the same from the code and different from an
// inbox.
//
// THE TESTS RUN AT THE HTTP SEAM, per docs/testing.md, and reach for SQL only
// where the API deliberately hands a buyer nothing: Ticket ids, and the
// timestamps behind the state. Everything a buyer, a stranger or an operator can
// observe is observed through a request.
//
// THE FLAG IS OPENED PER TEST and closed again by setupTest, exactly as
// enableTicketQuestions is — the dark default is how the feature ships and is
// what every other test in this package must keep seeing.

func buyerAssignmentPath(ticketSaleID, ticketID string) string {
	return buyerTicketsPath(ticketSaleID) + "/" + ticketID + "/assignment"
}

// enableTicketAssignment opens Ticket Assignment for one test.
//
// ITS OWN HELPER, CALLING ITS OWN SETTER, and pointedly NOT a second line inside
// enableTicketQuestions. TICKET_ASSIGNMENT_ENABLED is a separate deployment
// flag from TICKET_QUESTIONS_ENABLED, and folding the two here would destroy the
// suite's ability to state the property the second flag exists to buy: that
// assignment can be closed while questions stay open, and the reverse.
func enableTicketAssignment(t *testing.T) {
	t.Helper()
	sharedApp.CatalogService.WithTicketAssignment(true)
	// And sales, which reads the SAME environment variable for one thing: the
	// Holder columns on the Sales Export's per-Ticket sheet (#330, ADR 0047).
	// Both services here because server wiring hands both the one value, and a
	// helper that opened only half of it would let a test pass against a
	// deployment shape that does not exist.
	sharedApp.SalesService.WithTicketAssignment(true)
}

// assignTicket puts one address on one Ticket and returns the whole response,
// refusal and all — the callers that expect success check the status themselves.
func assignTicket(
	t *testing.T, env *testEnv, session, ticketSaleID, ticketID, holderEmail string,
) (*http.Response, envelope) {
	t.Helper()
	return env.put(t, buyerAssignmentPath(ticketSaleID, ticketID),
		map[string]any{"holder_email": holderEmail}, authHeader(session))
}

// assignTicketOK assigns and returns the whole Sale's rows as the write handed
// them back.
func assignTicketOK(
	t *testing.T, env *testEnv, session, ticketSaleID, ticketID, holderEmail string,
) []buyerTicket {
	t.Helper()
	resp, body := assignTicket(t, env, session, ticketSaleID, ticketID, holderEmail)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("assign %s to %q status=%d error=%+v", ticketID, holderEmail, resp.StatusCode, body.Error)
	}
	return decodeBuyerTickets(t, body.Data)
}

// ticketAssignmentRow reads the four columns migration 080 added, which no
// surface hands a test for free — the timestamps in particular are the only way
// to tell "assigned again" from "did nothing", and the whole of what makes the
// no-op test an assertion rather than a hope.
type ticketAssignmentRow struct {
	holderEmail      sql.NullString
	holderCustomerID sql.NullString
	assignedAt       sql.NullTime
	acceptedAt       sql.NullTime
}

func readTicketAssignment(t *testing.T, env *testEnv, ticketID string) ticketAssignmentRow {
	t.Helper()
	var row ticketAssignmentRow
	if err := env.db.QueryRow(`
		SELECT holder_email, holder_customer_id, assigned_at, accepted_at
		FROM tickets WHERE id = $1
	`, ticketID).Scan(&row.holderEmail, &row.holderCustomerID, &row.assignedAt, &row.acceptedAt); err != nil {
		t.Fatalf("read Ticket %s assignment: %v", ticketID, err)
	}
	return row
}

// findBuyerRow picks one Ticket's row out of a buyer's list.
func findBuyerRow(t *testing.T, tickets []buyerTicket, ticketID string) buyerTicket {
	t.Helper()
	for _, ticket := range tickets {
		if ticket.TicketID == ticketID {
			return ticket
		}
	}
	t.Fatalf("Ticket %s is not on the buyer's list", ticketID)
	return buyerTicket{}
}

// mailBaseline is what the captured sender had sent BEFORE an assignment.
//
// A baseline rather than a bare zero, because the journeys these tests set up
// legitimately send mail of their own: a checkout sends a Sale Confirmation, and
// signing a Customer in sends a passcode. What must not move is the count AFTER
// the buyer assigns.
type mailBaseline struct {
	confirmations int
	reminders     int
	passcodes     int
}

func captureMailBaseline(env *testEnv) mailBaseline {
	return mailBaseline{
		confirmations: len(env.email.Confirmations()),
		reminders:     len(env.email.AnswerRemindersSent()),
		passcodes:     env.email.OTPSendCount(),
	}
}

// assertNoAssignmentMailWasSent is the assertion #324 exists to make, and it is
// made in several tests because it is the one property a reader will assume
// rather than check.
//
// NOTHING IS MAILED BY AN ASSIGNMENT IN THIS TICKET. The address the buyer typed
// belongs to somebody who has never been here and has agreed to nothing; #325
// gives it a purpose and #322's purge gives it an end, and until both exist the
// platform holds it and stays silent. The captured sender has no assignment
// accessor to check because platform.EmailSender has no assignment METHOD yet —
// so what is asserted here is that nothing ELSE started sending either, in
// particular that assigning did not fire a Sale Confirmation, an Answer Reminder
// or a passcode at the address the buyer named.
func assertNoAssignmentMailWasSent(t *testing.T, env *testEnv, before mailBaseline) {
	t.Helper()
	if got := len(env.email.Confirmations()); got != before.confirmations {
		t.Errorf("assigning sent %d new Sale Confirmation(s); #324 sends no mail at all",
			got-before.confirmations)
	}
	if got := len(env.email.AnswerRemindersSent()); got != before.reminders {
		t.Errorf("assigning sent %d new Answer Reminder(s); nothing about assignment mails anybody in #324",
			got-before.reminders)
	}
	if got := env.email.OTPSendCount(); got != before.passcodes {
		t.Errorf("assigning sent %d new passcode(s); an assignment proves nothing and mints no session",
			got-before.passcodes)
	}
}

// A BUYER ASSIGNS ONE TICKET OF A SALE OF TWO, AND THE OTHER IS NOT BLOCKED ON
// IT. This is the centre of the ticket: partial assignment is the normal case,
// because a buyer of four who knows two addresses must not be held up by the two
// they do not.
//
// It also asserts the thing the Answer Links could never do. Four links disclose
// nothing — deliberately — so they are indistinguishable, and the buyer cannot
// tell which of their Tickets they forwarded where. An address on the row is the
// whole of the answer to "which of these is which".
//
// AND IT ASSERTS THE SILENCE: no mail, and `accepted` not reached. Both are
// properties of #324 that a later ticket removes, so both are stated here rather
// than left to be inferred from the absence of an inbox.
func TestBuyerAssignsOneTicketAndTheRestOfTheSaleIsUnaffected(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)
	ana := customerSignIn(t, env, "ana@example.com")
	mailBefore := captureMailBaseline(env)

	// Every Ticket starts `unassigned`, which is today's behaviour and what most
	// Tickets will be for a long time.
	for _, ticket := range listBuyerTickets(t, env, ana, f.anaSaleID) {
		if ticket.AssignmentState != "unassigned" {
			t.Fatalf("Ticket %s starts in state %q, want unassigned", ticket.TicketID, ticket.AssignmentState)
		}
		if !ticket.Assignable || ticket.AssignableRefusal != "" {
			t.Fatalf("Ticket %s assignable=%v refusal=%q, want an open window on an `import` sale",
				ticket.TicketID, ticket.Assignable, ticket.AssignableRefusal)
		}
	}

	// THE WHOLE SALE COMES BACK FROM THE WRITE, not the one row that changed:
	// the page's rows are read together and a single row would leave a fresh
	// state beside a stale list.
	returned := assignTicketOK(t, env, ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	if len(returned) != 2 {
		t.Fatalf("the write returned %d Tickets, want the whole Sale's 2", len(returned))
	}

	assigned := findBuyerRow(t, returned, f.anaTicketIDs[0])
	if assigned.AssignmentState != "assigned" {
		t.Errorf("assigned Ticket state = %q, want assigned", assigned.AssignmentState)
	}
	if assigned.HolderEmail != "carla@example.com" {
		t.Errorf("assigned Ticket holder_email = %q, want carla@example.com", assigned.HolderEmail)
	}
	if assigned.AssignedAt == nil {
		t.Error("assigned Ticket carries no assigned_at — the buyer cannot see when they did it")
	}
	// `accepted` IS UNREACHABLE IN #324. Nothing mails the address, so nothing
	// can be clicked, so nothing can accept. #325 is what changes this.
	if assigned.AcceptedAt != nil {
		t.Errorf("Ticket reports accepted_at=%v; #324 sends no Assignment mail, so nothing can have accepted it",
			*assigned.AcceptedAt)
	}

	// PARTIAL ASSIGNMENT: the second Ticket is untouched and still assignable.
	other := findBuyerRow(t, returned, f.anaTicketIDs[1])
	if other.AssignmentState != "unassigned" || other.HolderEmail != "" {
		t.Errorf("the other Ticket is state=%q holder=%q; a sale of two with one address given is not a sale half done",
			other.AssignmentState, other.HolderEmail)
	}

	// The read agrees with the write, which is what makes the page reload
	// truthful rather than the response being a fiction.
	reread := findBuyerRow(t, listBuyerTickets(t, env, ana, f.anaSaleID), f.anaTicketIDs[0])
	if reread.HolderEmail != "carla@example.com" || reread.AssignmentState != "assigned" {
		t.Fatalf("on re-reading, the Ticket is state=%q holder=%q", reread.AssignmentState, reread.HolderEmail)
	}

	// The database agrees too, and in particular the two `accepted` columns are
	// NULL — the state is unreachable rather than merely unreported.
	row := readTicketAssignment(t, env, f.anaTicketIDs[0])
	if row.acceptedAt.Valid || row.holderCustomerID.Valid {
		t.Errorf("Ticket row carries accepted_at=%v holder_customer_id=%v; both are #325's to write",
			row.acceptedAt, row.holderCustomerID)
	}

	assertNoAssignmentMailWasSent(t, env, mailBefore)

	// TICKETS SOLD IS UNAFFECTED. It sums Ticket Sale Line quantities (ADR 0043)
	// and assignment writes columns on `tickets`, which that sum never reads —
	// so a published figure cannot move because somebody named an address.
	if got, want := ticketsForEvent(t, env, f.eventID), summedQuantities(t, env, f.eventID, false); got != want {
		t.Fatalf("after assigning, Tickets = %d and summed quantities = %d", got, want)
	}
}

// REASSIGNING TO A DIFFERENT ADDRESS CLEARS THAT TICKET'S ANSWERS BACK TO
// OUTSTANDING, AND THIS IS THE MOST IMPORTANT TEST IN THE FILE.
//
// An Answer is a fact about a PERSON — a t-shirt size, a dietary requirement, an
// accessibility need — and a Ticket that changed hands carrying its Answers
// would attribute the previous Holder's facts to somebody who never said them.
// The Organization then acts on them: orders the wrong size, or serves a meal
// that makes the new Holder ill. Inheritance is not untidy, it is wrong.
//
// The other Ticket of the same Sale keeps its Answer, which is what makes this a
// rule about ONE Ticket rather than a Sale-wide wipe.
func TestReassigningATicketClearsItsAnswersAndLeavesTheSalesOthersAlone(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)
	ana := customerSignIn(t, env, "ana@example.com")

	// Both Tickets answered, and assigned to two different people.
	for _, ticketID := range f.anaTicketIDs {
		resp, body := env.put(t, buyerAnswerPath(f.anaSaleID, ticketID, f.sizeQuestion.ID),
			map[string]any{"text": "XL"}, authHeader(ana))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("answer status=%d error=%+v", resp.StatusCode, body.Error)
		}
	}
	assignTicketOK(t, env, ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	assignTicketOK(t, env, ana, f.anaSaleID, f.anaTicketIDs[1], "diego@example.com")

	// A FIRST assignment clears nothing: the buyer answered for the person they
	// are about to name, and wiping the work they just did would be a loss with
	// no explanation.
	before := listBuyerTickets(t, env, ana, f.anaSaleID)
	for _, ticketID := range f.anaTicketIDs {
		if buyerAnswerFor(t, findBuyerRow(t, before, ticketID), f.sizeQuestion.ID) == nil {
			t.Fatalf("naming a Holder for the first time cleared Ticket %s's Answer", ticketID)
		}
	}

	// Carla drops out. Her ticket goes to Elena — and Carla's size goes with her.
	returned := assignTicketOK(t, env, ana, f.anaSaleID, f.anaTicketIDs[0], "elena@example.com")

	reassigned := findBuyerRow(t, returned, f.anaTicketIDs[0])
	if reassigned.HolderEmail != "elena@example.com" {
		t.Fatalf("reassigned holder_email = %q, want elena@example.com", reassigned.HolderEmail)
	}
	if answer := buyerAnswerFor(t, reassigned, f.sizeQuestion.ID); answer != nil {
		t.Fatalf("the new Holder inherited the old one's answer (%+v).\n"+
			"An Answer is a fact about a person and must never survive a change of Holder.\n"+
			"See repository.AssignTicketToHolder.", answer)
	}
	// BACK TO OUTSTANDING, and not merely blank: the required question is owed
	// again, which is what puts this Ticket back on the Organization's chase
	// list and on the buyer's.
	if reassigned.OutstandingCount != 1 {
		t.Errorf("reassigned Ticket outstanding_count = %d, want 1 — the required question is owed afresh",
			reassigned.OutstandingCount)
	}

	// Diego's ticket is untouched. The rule is per-Ticket.
	untouched := findBuyerRow(t, returned, f.anaTicketIDs[1])
	if answer := buyerAnswerFor(t, untouched, f.sizeQuestion.ID); answer == nil {
		t.Fatal("reassigning one Ticket cleared the Answers of another Ticket on the same Sale")
	}
	if untouched.HolderEmail != "diego@example.com" {
		t.Errorf("the other Ticket's holder_email = %q, want diego@example.com", untouched.HolderEmail)
	}
}

// SUBMITTING THE ADDRESS A TICKET ALREADY CARRIES CHANGES NOTHING AT ALL.
//
// A buyer who presses save twice, or who "corrects" a typo back to what it
// already said, has not changed who holds this Ticket — so the Answers stand and
// assigned_at does not move. The timestamp matters beyond tidiness: #325's
// per-Ticket mail cap reads it, and a doubled click that moved it would spend
// somebody's allowance for them.
//
// CASE AND WHITESPACE ARE THE SAME ADDRESS. `Carla@Example.com ` and
// `carla@example.com` are one person, normalised at the one place this platform
// normalises addresses — which is what stops #325 minting a second Customer for
// somebody who already has one.
func TestReassigningToTheSameAddressIsANoOpAndKeepsTheAnswers(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)
	ana := customerSignIn(t, env, "ana@example.com")

	ticketID := f.anaTicketIDs[0]
	assignTicketOK(t, env, ana, f.anaSaleID, ticketID, "carla@example.com")
	resp, body := env.put(t, buyerAnswerPath(f.anaSaleID, ticketID, f.sizeQuestion.ID),
		map[string]any{"text": "M"}, authHeader(ana))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answer status=%d error=%+v", resp.StatusCode, body.Error)
	}
	first := readTicketAssignment(t, env, ticketID)

	// The clock is moved so that a rewritten assigned_at would be a DIFFERENT
	// value. Under the fixed clock alone, "did not move" and "was rewritten to
	// the same instant" are indistinguishable, and the test would pass on a
	// build that rewrites the row on every save.
	later := env.fixedClock.Add(48 * time.Hour)
	sharedApp.CatalogService.WithClock(func() time.Time { return later })
	defer sharedApp.CatalogService.WithClock(func() time.Time { return env.fixedClock })

	// The same address, typed the way somebody's address book would give it.
	returned := assignTicketOK(t, env, ana, f.anaSaleID, ticketID, "  Carla@Example.COM ")

	row := readTicketAssignment(t, env, ticketID)
	if row.holderEmail.String != "carla@example.com" {
		t.Errorf("stored holder_email = %q, want the normalised carla@example.com", row.holderEmail.String)
	}
	if !row.assignedAt.Time.Equal(first.assignedAt.Time) {
		t.Errorf("assigned_at moved from %v to %v on re-submitting the same address.\n"+
			"Pressing save twice is not an event, and #325's per-Ticket mail cap reads this column.",
			first.assignedAt.Time, row.assignedAt.Time)
	}
	if answer := buyerAnswerFor(t, findBuyerRow(t, returned, ticketID), f.sizeQuestion.ID); answer == nil {
		t.Fatal("re-submitting the address the Ticket already carried cleared its Answers")
	}
}

// A BUYER MAY ASSIGN A TICKET TO THEIR OWN ADDRESS. A parent buying for three
// children holds one themselves, and a rule that refused the buyer's own address
// would refuse the commonest shape this feature has. Nothing about it makes them
// stop being the buyer: the Ticket Sale, and the other Ticket, stay exactly where
// they were.
func TestBuyerAssignsATicketToTheirOwnAddress(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)
	ana := customerSignIn(t, env, "ana@example.com")

	returned := assignTicketOK(t, env, ana, f.anaSaleID, f.anaTicketIDs[0], "ana@example.com")
	own := findBuyerRow(t, returned, f.anaTicketIDs[0])
	if own.AssignmentState != "assigned" || own.HolderEmail != "ana@example.com" {
		t.Fatalf("self-assignment gave state=%q holder=%q", own.AssignmentState, own.HolderEmail)
	}
	// Still `assigned` and not `accepted`, even though the platform already
	// knows this address belongs to a Verified Customer. Acceptance is a CLICK
	// FROM THE INBOX and nothing else — inferring it from a match would be the
	// platform proving ownership on somebody's behalf, and there is no mail to
	// click in #324 anyway.
	if own.AcceptedAt != nil {
		t.Error("assigning to the buyer's own address reported an acceptance; only a click accepts")
	}
	// The buyer's other Ticket, and their Sale, are untouched.
	if other := findBuyerRow(t, returned, f.anaTicketIDs[1]); other.AssignmentState != "unassigned" {
		t.Errorf("the other Ticket became %q when the buyer assigned one to themselves", other.AssignmentState)
	}
}

// ASKING ABOUT ANOTHER CUSTOMER'S TICKET SALE IS ANSWERED AS IF IT DID NOT
// EXIST, on the write as on the read beside it (#315).
//
// It is a 404 that says TICKET_NOT_FOUND and never a 403, and — the sharper half
// — it is the SAME 404 an unparseable address gets on somebody else's Ticket. A
// build that validated the body before it resolved the Ticket would answer 400
// INVALID_HOLDER_EMAIL here, which tells the caller their id was at least
// reachable. That is the oracle the empty-list rule on the read exists to deny,
// and it must not be re-opened by the write.
func TestAssigningAnotherCustomersTicketIsAnsweredAsIfItDidNotExist(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)
	bruno := customerSignIn(t, env, "bruno@example.com")

	resp, body := assignTicket(t, env, bruno, f.anaSaleID, f.anaTicketIDs[0], "mallory@example.com")
	assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_NOT_FOUND")

	// The same refusal, and pointedly not a 400 about the address, when the body
	// is nonsense as well.
	resp, body = assignTicket(t, env, bruno, f.anaSaleID, f.anaTicketIDs[0], "not-an-address")
	assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_NOT_FOUND")

	// Naming his OWN sale but somebody else's Ticket id is the same answer: the
	// Ticket is resolved among the Tickets of the caller's own Sale, so a real
	// id from elsewhere is not a Ticket at all.
	resp, body = assignTicket(t, env, bruno, f.brunoSaleID, f.anaTicketIDs[0], "mallory@example.com")
	assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_NOT_FOUND")

	// And nothing was written anywhere near Ana's Ticket.
	if row := readTicketAssignment(t, env, f.anaTicketIDs[0]); row.holderEmail.Valid {
		t.Fatalf("another Customer assigned Ana's Ticket to %q", row.holderEmail.String)
	}

	// The route is not simply broken: Bruno's own Ticket assigns perfectly
	// through the same session and the same call.
	own := assignTicketOK(t, env, bruno, f.brunoSaleID, f.brunoTicketIDs[0], "friend@example.com")
	if findBuyerRow(t, own, f.brunoTicketIDs[0]).HolderEmail != "friend@example.com" {
		t.Fatal("Bruno could not assign his own Ticket")
	}
}

// A CONFIRMATION LINK SESSION MAY ASSIGN THE ONE SALE IT NAMES, AND ONLY THAT
// ONE.
//
// Both halves matter. A buyer who paid as a guest reaches their tickets by
// clicking the link in their receipt, and that IS the surface this feature is
// for — refusing it would put the platform's own distribution route behind a
// stricter door than the forwarded Answer Link it replaces. But a receipt gets
// forwarded, so the session's scope may only ever narrow: naming another sale is
// answered as if it did not exist, exactly as the read is.
func TestAConfirmationLinkSessionAssignsOnlyTheSaleItNames(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)

	_, linkSession := redeemConfirmationLinkOK(t, env, confirmationLinkTokenForRef(t, env, f.anaRef), "")

	returned := assignTicketOK(t, env, linkSession, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	if findBuyerRow(t, returned, f.anaTicketIDs[0]).HolderEmail != "carla@example.com" {
		t.Fatal("a Confirmation Link session could not assign the sale its receipt was for")
	}

	// Another Customer's sale, through the same forwarded receipt: not found.
	resp, body := assignTicket(t, env, linkSession, f.brunoSaleID, f.brunoTicketIDs[0], "mallory@example.com")
	assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_NOT_FOUND")
	if row := readTicketAssignment(t, env, f.brunoTicketIDs[0]); row.holderEmail.Valid {
		t.Fatalf("a forwarded receipt assigned another Customer's Ticket to %q", row.holderEmail.String)
	}
}

// AN `in_person` TICKET SALE REFUSES ASSIGNMENT, because a door sale has no
// buyer surface to assign from: there is no Confirmation Link page and no
// Customer Area listing for a sale recorded at a till.
//
// IT IS A 409 AND NOT A 404. The buyer is entitled to know that this is a fact
// about the sale rather than about their entitlement — the same Customer's
// `online` sale can be assigned — so the refusal names the reason. The read
// carries the same reason as a token on the row, so the page can grey the field
// out rather than let somebody type an address into a form that will not take.
func TestInPersonTicketSalesRefuseAssignment(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)
	ana := customerSignIn(t, env, "ana@example.com")

	// Staged in SQL, as every other in-person test in this package stages it:
	// the POS is unbuilt and `in_person` exists today as a Sales Channel value
	// (see tickets_test.go and sales_export_test.go). The sale keeps Ana as its
	// Customer, so what is being tested is the CHANNEL and not ownership.
	moveSaleToTheDoor(t, env, f.anaRef)

	resp, body := assignTicket(t, env, ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	assertAPIError(t, resp, body, http.StatusConflict, "ASSIGNMENT_CHANNEL_UNSUPPORTED")
	if row := readTicketAssignment(t, env, f.anaTicketIDs[0]); row.holderEmail.Valid {
		t.Fatalf("a door sale's Ticket was assigned to %q", row.holderEmail.String)
	}

	// The read still works and says why, on every row.
	for _, ticket := range listBuyerTickets(t, env, ana, f.anaSaleID) {
		if ticket.Assignable {
			t.Errorf("Ticket %s reports itself assignable on an `in_person` sale", ticket.TicketID)
		}
		if ticket.AssignableRefusal != "channel_unsupported" {
			t.Errorf("Ticket %s assignable_refusal = %q, want channel_unsupported",
				ticket.TicketID, ticket.AssignableRefusal)
		}
		// AND THE ANSWERS ARE STILL WRITABLE. The two windows are separate rules:
		// a door sale's Ticket Questions are answerable by the buyer and by Event
		// Staff exactly as before. Collapsing assignment into the answer window
		// would have closed this door too.
		if !ticket.Answerable {
			t.Errorf("Ticket %s stopped being answerable because its sale is `in_person`", ticket.TicketID)
		}
	}
	resp, body = env.put(t, buyerAnswerPath(f.anaSaleID, f.anaTicketIDs[0], f.sizeQuestion.ID),
		map[string]any{"text": "L"}, authHeader(ana))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answering a door sale's Ticket status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// AN `online` TICKET SALE ASSIGNS, driven through a real Storefront checkout.
//
// The fixture every other test here uses is an `import` sale, so without this
// one the whole file would prove the feature on one of the two channels it is
// meant to serve — and `online` is the one nearly every buyer will arrive on.
//
// IT ALSO ASSERTS THAT NOTHING ABOUT ASSIGNMENT TOUCHED THE CHECKOUT. No address
// was asked for at the till, the sale completed exactly as it does today, and
// the Sale Confirmation went out unchanged. Assignment happens after purchase,
// which is what keeps an abandoned Payment free of a third party's address.
func TestAnOnlineTicketSaleCanBeAssignedAfterCheckout(t *testing.T) {
	env := setupTest(t)
	staff := orgAdminSession(t, env)
	enableTicketAssignment(t)
	_, ttID := publishCheckoutEvent(t, env, staff, "Online Fest", "online-fest", 1000, 50)

	begin := beginCheckoutOK(t, env, "test-org", "online-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": ttID, "quantity": 2}))
	if got := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved"); got.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", got.Status)
	}
	_, saleID, _ := paymentRecord(t, env, begin.ClientTransactionID)
	if saleID == nil {
		t.Fatal("approved Payment recorded no Ticket Sale")
	}
	// One Sale Confirmation, exactly as before this feature existed.
	if got := len(env.email.Confirmations()); got != 1 {
		t.Fatalf("checkout sent %d Sale Confirmations, want 1", got)
	}

	ticketIDs := ticketIDsOfSale(t, env, *saleID)
	ana := customerSignIn(t, env, "ana@example.com")
	mailBefore := captureMailBaseline(env)
	returned := assignTicketOK(t, env, ana, *saleID, ticketIDs[1], "carla@example.com")

	assigned := findBuyerRow(t, returned, ticketIDs[1])
	if assigned.AssignmentState != "assigned" || assigned.HolderEmail != "carla@example.com" {
		t.Fatalf("online sale's Ticket = state %q holder %q", assigned.AssignmentState, assigned.HolderEmail)
	}
	// Assigning mailed nobody — not the Holder, and not the buyer a second time.
	assertNoAssignmentMailWasSent(t, env, mailBefore)
}

// THE WINDOW CLOSES AT THE DOORS AND ON A REVERSED SALE, and both refusals name
// themselves rather than hiding behind one another.
//
// The deadline is the Event's start, exactly as the Answer window's is:
// assignment exists so that the right person is named before the Organization
// acts on it, and it is also when #322's retention purge takes an address nobody
// accepted — so an assignment made after it would name somebody the platform is
// in the act of forgetting.
//
// THE READ STAYS OPEN THROUGHOUT. A buyer whose Event has happened, or whose
// sale was reversed, must still see who they assigned their Tickets to; a
// reversed Sale keeps its place in the Customer Area, and an assignment that
// vanished with it would read as data destroyed.
func TestAssignmentIsRefusedOnAReversedSaleAndOnceTheEventHasStarted(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)
	ana := customerSignIn(t, env, "ana@example.com")

	assignTicketOK(t, env, ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")

	// The doors open. The Event was scheduled 30 days out by the fixture.
	afterTheDoors := env.fixedClock.Add(31 * 24 * time.Hour)
	sharedApp.CatalogService.WithClock(func() time.Time { return afterTheDoors })

	resp, body := assignTicket(t, env, ana, f.anaSaleID, f.anaTicketIDs[1], "diego@example.com")
	assertAPIError(t, resp, body, http.StatusConflict, "ASSIGNMENT_EVENT_STARTED")

	started := findBuyerRow(t, listBuyerTickets(t, env, ana, f.anaSaleID), f.anaTicketIDs[0])
	if started.Assignable || started.AssignableRefusal != "event_started" {
		t.Errorf("after the doors: assignable=%v refusal=%q", started.Assignable, started.AssignableRefusal)
	}
	// AND THE ASSIGNMENT IS STILL READABLE. The window closes the write, not the
	// record.
	if started.HolderEmail != "carla@example.com" || started.AssignmentState != "assigned" {
		t.Errorf("after the doors the buyer can no longer see who holds their Ticket: state=%q holder=%q",
			started.AssignmentState, started.HolderEmail)
	}

	sharedApp.CatalogService.WithClock(func() time.Time { return env.fixedClock })

	// Now the reversal, which is the more fundamental fact and is reported ahead
	// of the clock when both are true.
	undoBatch(t, env, f.staffSession, f.eventID, f.anaBatchID)

	resp, body = assignTicket(t, env, ana, f.anaSaleID, f.anaTicketIDs[1], "diego@example.com")
	assertAPIError(t, resp, body, http.StatusConflict, "ASSIGNMENT_SALE_REVERSED")

	reversed := findBuyerRow(t, listBuyerTickets(t, env, ana, f.anaSaleID), f.anaTicketIDs[0])
	if reversed.Assignable || reversed.AssignableRefusal != "sale_reversed" {
		t.Errorf("on a reversed Sale: assignable=%v refusal=%q", reversed.Assignable, reversed.AssignableRefusal)
	}
	if reversed.HolderEmail != "carla@example.com" {
		t.Errorf("a reversed Sale lost the assignment on its Ticket: holder=%q", reversed.HolderEmail)
	}
}

// A HOLDER ADDRESS THAT IS NOT AN ADDRESS IS REFUSED, and nothing is written.
//
// The check is weak on purpose — an address is only really validated by mail
// arriving at it, and this one belongs to somebody who is not here to confirm
// anything — but it is worth having, because until #325 mails the address NOBODY
// WILL DISCOVER IT IS WRONG: the buyer sees their own typo echoed back and
// believes the job done.
func TestAnAddressThatIsNotAnAddressIsRefused(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)
	ana := customerSignIn(t, env, "ana@example.com")

	for _, bad := range []string{
		"",
		"   ",
		"carla",
		"carla@",
		"carla example.com",
		// The display-name form: what is stored must be an ADDRESS and nothing
		// else, because it travels into a mail header in #325 and into the
		// Organization's export.
		"Carla Ruiz <carla@example.com>",
		strings.Repeat("a", 250) + "@example.com",
	} {
		resp, body := assignTicket(t, env, ana, f.anaSaleID, f.anaTicketIDs[0], bad)
		assertAPIError(t, resp, body, http.StatusBadRequest, "INVALID_HOLDER_EMAIL")
	}

	if row := readTicketAssignment(t, env, f.anaTicketIDs[0]); row.holderEmail.Valid {
		t.Fatalf("a refused address was stored anyway: %q", row.holderEmail.String)
	}
}

// THE FLAG. This is the test the second flag exists for.
//
// With TICKET_ASSIGNMENT_ENABLED closed — which is how it ships — no assignment
// is accepted, nothing is stored, and the buyer's payload is the one a build
// without the feature sends: not an `assignment_state: "unassigned"`, not an
// `assignable: false`, but the FIELDS ABSENT ENTIRELY. Asserted over the raw
// bytes rather than through the decode, because a decode of a struct that
// declares those fields cannot tell a default from an absence.
//
// AND TICKET QUESTIONS GO ON WORKING THROUGHOUT, which is the other half and the
// whole reason this is a second flag rather than a second use of the first one:
// killing assignment must not take questions dark. The last act opens assignment
// with questions still open, so the closed state above is shown to be the flag
// and not the feature being unbuilt.
func TestTicketAssignmentShipsClosedAndDoesNotTakeTicketQuestionsWithIt(t *testing.T) {
	env := setupTest(t)
	// newBuyerAnswersFixture opens TICKET_QUESTIONS_ENABLED and says nothing
	// about assignment, which is exactly the deployment under test: one flag
	// open, the other closed.
	f := newBuyerAnswersFixture(t, env)
	ana := customerSignIn(t, env, "ana@example.com")

	// The write answers 404 and names its own refusal — its OWN code, never the
	// Ticket Question one, so an operator reading logs can tell which flag is
	// shut.
	resp, body := assignTicket(t, env, ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_ASSIGNMENT_UNAVAILABLE")
	if row := readTicketAssignment(t, env, f.anaTicketIDs[0]); row.holderEmail.Valid {
		t.Fatalf("an assignment was stored while the flag was closed: %q", row.holderEmail.String)
	}

	// TICKET QUESTIONS ARE UNAFFECTED: the buyer's list reads, the Answer Links
	// are handed out, and an Answer writes.
	listResp, listBody := env.get(t, buyerTicketsPath(f.anaSaleID), authHeader(ana))
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("the buyer's Tickets stopped reading with assignment closed: status=%d error=%+v",
			listResp.StatusCode, listBody.Error)
	}
	raw := string(listBody.Data)
	for _, field := range []string{
		"assignment_state", "holder_email", "assigned_at", "accepted_at",
		"assignable", "assignable_refusal",
	} {
		if strings.Contains(raw, field) {
			t.Errorf("the buyer's payload carries %q while TICKET_ASSIGNMENT_ENABLED is closed.\n"+
				"A dark build must be byte-identical to a build without the feature (ADR 0045).\nbody: %s",
				field, raw)
		}
	}
	tickets := decodeBuyerTickets(t, listBody.Data)
	if len(tickets) != 2 || tickets[0].AnswerLink == "" {
		t.Fatalf("Ticket Questions went dark with assignment: %+v", tickets)
	}
	answerResp, answerBody := env.put(t, buyerAnswerPath(f.anaSaleID, f.anaTicketIDs[0], f.sizeQuestion.ID),
		map[string]any{"text": "XL"}, authHeader(ana))
	if answerResp.StatusCode != http.StatusOK {
		t.Fatalf("answering broke while assignment was closed: status=%d error=%+v",
			answerResp.StatusCode, answerBody.Error)
	}

	// And now the same deployment with assignment opened, which is what makes
	// every refusal above the FLAG rather than the feature being absent.
	enableTicketAssignment(t)
	returned := assignTicketOK(t, env, ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	if findBuyerRow(t, returned, f.anaTicketIDs[0]).AssignmentState != "assigned" {
		t.Fatal("opening TICKET_ASSIGNMENT_ENABLED did not open assignment")
	}
}
