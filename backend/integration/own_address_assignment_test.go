package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// A Ticket the buyer assigns to their own address is `accepted` at once (#668,
// spec #665, ADR 0076).
//
// THE ADDRESS HAS ALREADY BEEN PROVEN BY THE ACT THAT NAMES IT. Checkout needs a
// Customer Session (ADR 0054) and a Confirmation Link session is reached through
// the buyer's inbox, so mailing the buyer a link to prove that inbox again would
// only leave a family the platform fully knows reading "assigned, no name" on
// the Holder List. On every Event, not only those requiring Named Tickets.
//
// The fixture is newAssignmentFixture's: an `import` Sale of two Tickets to Ana,
// on an Event 30 days out, with a required size question.

// assertOwnAddressAccepted insists one Ticket reads as the buyer's own: accepted,
// at the buyer's address, held by the session's Customer.
func assertOwnAddressAccepted(t *testing.T, row buyerTicket, wantEmail string) {
	t.Helper()
	if row.AssignmentState != "accepted" {
		t.Errorf("own-address Ticket reads %q, want accepted at once (ADR 0076)", row.AssignmentState)
	}
	if row.HolderEmail != wantEmail {
		t.Errorf("own-address Ticket holder_email = %q, want %q", row.HolderEmail, wantEmail)
	}
	if row.AcceptedAt == nil {
		t.Error("own-address Ticket carries no accepted_at")
	}
	if !row.SelfHeld {
		t.Error("own-address Ticket is not reported as held by the buyer")
	}
}

// THE CENTRE OF THE TICKET, from a Customer Session: the buyer types their own
// address the way an address book gives it, and the Ticket is theirs at once,
// with nothing mailed to anybody.
func TestAssigningTheBuyersOwnAddressAcceptsItAtOnceWithNoMail(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	mailBefore := captureMailBaseline(env)

	// CASE AND WHITESPACE ARE THE SAME ADDRESS, by the one normalisation rule
	// accepting already uses.
	returned := assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "  Ana@Example.COM ")

	assertOwnAddressAccepted(t, findBuyerRow(t, returned, f.anaTicketIDs[0]), "ana@example.com")
	// The read agrees with the write.
	assertOwnAddressAccepted(t, findBuyerRow(t, listBuyerTickets(t, env, f.ana, f.anaSaleID), f.anaTicketIDs[0]),
		"ana@example.com")

	// NO ASSIGNMENT LINK, and nothing else either.
	if got := assignmentMailCount(env); got != 0 {
		t.Errorf("assigning a Ticket to the buyer's own address sent %d Assignment mail(s), want none", got)
	}
	assertNoAssignmentMailWasSent(t, env, mailBefore)

	// The other Ticket is untouched.
	if other := findBuyerRow(t, returned, f.anaTicketIDs[1]); other.AssignmentState != "unassigned" {
		t.Errorf("the other Ticket became %q", other.AssignmentState)
	}
}

// THE SAME FROM A CONFIRMATION LINK SESSION, which reached the buyer through
// their inbox and so has proven the address just as well. The route the buyer
// came in by must not change the result.
func TestAConfirmationLinkSessionAssigningTheBuyersOwnAddressAcceptsIt(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)

	_, linkSession := redeemConfirmationLinkOK(t, env, confirmationLinkTokenForRef(t, env, f.anaRef), "")

	returned := assignTicketOK(t, env, linkSession, f.anaSaleID, f.anaTicketIDs[1], "ANA@example.com")
	assertOwnAddressAccepted(t, findBuyerRow(t, returned, f.anaTicketIDs[1]), "ana@example.com")
	if got := assignmentMailCount(env); got != 0 {
		t.Errorf("a Confirmation Link session's own-address assignment sent %d Assignment mail(s)", got)
	}
}

// NO ALLOWANCE IS SPENT. The rationing exists to stop the platform writing to
// strangers on a buyer's say-so; an own-address assignment writes to nobody, so
// it may neither be refused by the rationing nor count against it.
func TestOwnAddressAssignmentNeitherSpendsNorHitsTheMailRationing(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	withAssignmentMailLimits(t, catalog.AssignmentMailLimits{PerTicket: 1, PerBuyer: 1})

	// The one mail the buyer's window allows is spent on Carla.
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	if got := assignmentMailCount(env); got != 1 {
		t.Fatalf("sent %d Assignment mail(s), want 1", got)
	}

	// Out of window and the Ticket out of its own allowance, the buyer can still
	// take both Tickets themselves.
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "ana@example.com")
	returned := assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "ana@example.com")
	assertOwnAddressAccepted(t, findBuyerRow(t, returned, f.anaTicketIDs[0]), "ana@example.com")
	assertOwnAddressAccepted(t, findBuyerRow(t, returned, f.anaTicketIDs[1]), "ana@example.com")

	// And once the window rolls, Ticket 1, which has never mailed anybody, has
	// its whole allowance: the own-address assignment spent none of it.
	holdClocksAt(fixedClock.Add(catalog.AssignmentMailWindow + time.Minute))
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "diego@example.com")
	assignmentMailFor(t, env, "diego@example.com")
	// Ticket 0's single mail went to Carla; its own-address turn spent nothing,
	// so it is refused for exactly the cap it already had.
	resp, body := assignTicket(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "elena@example.com")
	assertAPIError(t, resp, body, http.StatusConflict, "ASSIGNMENT_MAIL_CAP_REACHED")
}

// THE ORGANIZATION SEES THE BUYER, accepted, under the buyer's name - a family
// of four reads as four known Tickets rather than one name and three blanks.
func TestTheHolderListShowsAnOwnAddressTicketAcceptedUnderTheBuyersName(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)

	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "ana@example.com")

	got := guestRow(t, listOutstanding(t, env, f.staffSession, f.eventID), f.anaTicketIDs[0])
	if got.state != "accepted" || got.firstName != "Ana" || got.lastName != "Lopez" || got.email != "ana@example.com" {
		t.Errorf("the own-address row reads %+v, want accepted under Ana Lopez <ana@example.com>", got)
	}
}

// AFTER THE DOORS, NOTHING CHANGES, own address or not: the assignment window is
// the same window.
func TestOwnAddressAssignmentIsStillRefusedOnceTheEventHasStarted(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)

	holdClocksAt(fixedClock.Add(31 * 24 * time.Hour))
	resp, body := assignTicket(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "ana@example.com")
	assertAPIError(t, resp, body, http.StatusConflict, "ASSIGNMENT_EVENT_STARTED")
	if row := readTicketAssignment(t, env, f.anaTicketIDs[0]); row.holderEmail.Valid || row.acceptedAt.Valid {
		t.Fatalf("a refused own-address assignment wrote holder=%v accepted_at=%v", row.holderEmail, row.acceptedAt)
	}
}

// HANDING AN OWN-ADDRESS TICKET ON IS AN ORDINARY REASSIGNMENT: the buyer's
// Answers go, because they are facts about the buyer, and the new address is
// mailed its Assignment Link under the usual rationing.
func TestReassigningAnOwnAddressTicketClearsItsAnswersAndMailsTheNewAddress(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "ana@example.com")
	putAnswer(t, env, f.staffSession, f.eventID, ticketID, f.sizeQuestion.ID, map[string]any{"text": "S"})

	holdClocksAt(fixedClock.Add(time.Minute))
	returned := assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")

	row := findBuyerRow(t, returned, ticketID)
	if row.AssignmentState != "assigned" || row.HolderEmail != "carla@example.com" || row.SelfHeld {
		t.Errorf("the handed-on Ticket reads state=%q holder=%q self_held=%v, want assigned to carla",
			row.AssignmentState, row.HolderEmail, row.SelfHeld)
	}
	if answer := staffAnswerText(t, env, f.staffSession, f.eventID, ticketID, f.sizeQuestion.ID); answer != nil {
		t.Errorf("Carla inherited the buyer's size %q", *answer)
	}
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))
}

// TAKING A TICKET BACK FROM A HOLDER WHO ACCEPTED: the buyer holds it at once,
// the Holder's Answers go with the Holder, and the Holder is told they no longer
// hold it - exactly as any reassignment away from an accepted Holder tells them.
func TestTheBuyerTakingATicketBackHoldsItAtOnceAndTellsTheDisplacedHolder(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))
	putAnswer(t, env, f.staffSession, f.eventID, ticketID, f.sizeQuestion.ID, map[string]any{"text": "XL"})
	mailsBefore := assignmentMailCount(env)

	holdClocksAt(fixedClock.Add(time.Minute))
	returned := assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "ana@example.com")

	assertOwnAddressAccepted(t, findBuyerRow(t, returned, ticketID), "ana@example.com")
	if answer := staffAnswerText(t, env, f.staffSession, f.eventID, ticketID, f.sizeQuestion.ID); answer != nil {
		t.Errorf("the buyer inherited Carla's size %q", *answer)
	}
	if got := assignmentMailCount(env); got != mailsBefore {
		t.Errorf("taking a Ticket back sent %d Assignment mail(s)", got-mailsBefore)
	}
	theOneNoLongerHoldingMailTo(t, env, "carla@example.com")
}

// A TICKET ALREADY CARRYING THE BUYER'S ADDRESS, `assigned` from before this
// rule existed, is accepted when the buyer submits that address again - with its
// Answers kept, since nobody changed.
func TestResubmittingTheBuyersOwnAddressAcceptsATicketLeftAssigned(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	// The shape a pre-#668 own-address assignment left behind, which no route
	// can produce any more.
	if _, err := env.db.Exec(`
		UPDATE tickets SET holder_email = 'ana@example.com', assigned_at = $2 WHERE id = $1
	`, ticketID, fixedClock); err != nil {
		t.Fatalf("seed a legacy own-address assignment: %v", err)
	}
	putAnswer(t, env, f.staffSession, f.eventID, ticketID, f.sizeQuestion.ID, map[string]any{"text": "M"})

	returned := assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "ana@example.com")
	assertOwnAddressAccepted(t, findBuyerRow(t, returned, ticketID), "ana@example.com")
	if staffAnswerText(t, env, f.staffSession, f.eventID, ticketID, f.sizeQuestion.ID) == nil {
		t.Error("accepting the buyer's own assignment cleared its Answers; nobody changed")
	}
	if got := assignmentMailCount(env); got != 0 {
		t.Errorf("sent %d Assignment mail(s)", got)
	}
}

// ON EVERY EVENT, NAMED TICKETS OR NOT. The setting decides what checkout asks
// for (#665); it has no say over how an assignment after the sale treats the
// buyer's own address, which the session has already proven either way. Both
// routes are walked on both settings: a Customer Session takes one Ticket, a
// Confirmation Link session the other.
func TestOwnAddressAssignmentIsAcceptedAtOnceWithAndWithoutNamedTickets(t *testing.T) {
	for _, tc := range []struct {
		name     string
		requires bool
	}{
		{"named tickets on", true},
		{"named tickets off", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := setupTest(t)
			f := newAssignmentFixture(t, env)
			setBuyerFestNamedTickets(t, env, f, tc.requires)
			mailBefore := captureMailBaseline(env)

			// The size travels with the address on both settings: on a Named
			// Tickets Event the buyer's own Ticket owes its Answers too (#673),
			// and without the setting they are ignored.
			size := givenAnswer(f.sizeQuestion.ID, map[string]any{"text": "M"})
			returned := assignWithAnswersOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], " ana@EXAMPLE.com", size)
			assertOwnAddressAccepted(t, findBuyerRow(t, returned, f.anaTicketIDs[0]), "ana@example.com")

			// The second Ticket is written at its own instant, so the two
			// acceptances are never told apart by a coin toss on the clock.
			holdClocksAt(fixedClock.Add(time.Minute))
			_, linkSession := redeemConfirmationLinkOK(t, env, confirmationLinkTokenForRef(t, env, f.anaRef), "")
			returned = assignWithAnswersOK(t, env, linkSession, f.anaSaleID, f.anaTicketIDs[1], "Ana@Example.com", size)
			assertOwnAddressAccepted(t, findBuyerRow(t, returned, f.anaTicketIDs[1]), "ana@example.com")

			if got := assignmentMailCount(env); got != 0 {
				t.Errorf("own-address assignment sent %d Assignment mail(s) with requires_named_tickets=%v, want none",
					got, tc.requires)
			}
			assertNoAssignmentMailWasSent(t, env, mailBefore)

			guests := listOutstanding(t, env, f.staffSession, f.eventID)
			for _, ticketID := range f.anaTicketIDs {
				got := guestRow(t, guests, ticketID)
				if got.state != "accepted" || got.firstName != "Ana" || got.lastName != "Lopez" {
					t.Errorf("Holder List row %s reads %+v, want accepted under Ana Lopez", ticketID, got)
				}
			}
		})
	}
}

// setBuyerFestNamedTickets sets the fixture Event's Named Tickets setting
// explicitly, through the Org Admin's Event update, and reads it back so the
// test cannot pass on a default it never meant to rely on.
func setBuyerFestNamedTickets(t *testing.T, env *testEnv, f assignmentFixture, requires bool) {
	t.Helper()
	setEventNamedTickets(t, env, f.staffSession, f.eventID, "Buyer Fest", "buyer-fest",
		env.fixedClock.Add(30*24*time.Hour), requires)
}

// scheduleEventWithoutNamedTickets is scheduleEvent on an Event that does not
// require Named Tickets, which a new Event does (ADR 0076). For the fixtures
// whose tests are about assignment as ADR 0046 has it: an address given alone,
// and the Holder answering for themself.
func scheduleEventWithoutNamedTickets(
	t *testing.T, env *testEnv, staffSession, eventID, name, slug string, startsAt time.Time,
) {
	t.Helper()
	setEventNamedTickets(t, env, staffSession, eventID, name, slug, startsAt, false)
}

// setEventNamedTickets schedules an Event and sets its Named Tickets setting in
// one Org Admin update, reading the setting back.
func setEventNamedTickets(
	t *testing.T, env *testEnv, staffSession, eventID, name, slug string, startsAt time.Time, requires bool,
) {
	t.Helper()
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":                   name,
		"slug":                   slug,
		"starts_at":              startsAt.Format(time.RFC3339),
		"timezone":               "Europe/Madrid",
		"requires_named_tickets": requires,
	}, authHeader(staffSession))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set requires_named_tickets=%v status=%d error=%+v", requires, resp.StatusCode, body.Error)
	}
	if got := getEventNamedTickets(t, env, staffSession, eventID); got != requires {
		t.Fatalf("the Event reads requires_named_tickets=%v after setting %v", got, requires)
	}
}
