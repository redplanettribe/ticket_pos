package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// Assignment cannot be used as a bulk mailer (#332, parent #322, ADR 0046).
//
// WHAT THIS CLOSES. Reassignment is free by design, and every reassignment to a
// NEW address writes to that address. Uncapped, one Ticket is an unlimited
// mailer: point it somewhere fresh, a mail goes, repeat. Purchase Limit does not
// close it — CONTEXT.md calls that "a deterrent against taking too many rather
// than a defence against someone minting identities", and it is unset on most
// Ticket Types, so a Free Ticket Type would otherwise be a bulk sender on the
// platform's own transactional domain. The bounces and spam complaints land on
// ONE sending reputation, shared with passcodes and Sale Confirmations.
//
// EVERY LIMIT HERE IS ASSERTED THROUGH THE CAPTURED SENDER, never by reading a
// counter. A cap that refuses the request but sends the mail anyway is the exact
// failure these tests exist to catch, and it is invisible to any assertion made
// against the ledger that was supposed to stop it.
//
// THE ONE INTERACTION TO KEEP IN MIND is `assigned_at`. Every Assignment Link is
// signed over it, so a refusal that nonetheless wrote the new address would kill
// the previous Holder's live link and mail nobody a replacement — a Ticket
// unreachable by anyone, which is strictly worse than the refusal. That is why
// a spent allowance refuses the ASSIGNMENT and not merely the mail, and why the
// tests below check the row and the old link as well as the status.

// withAssignmentMailLimits lowers the rationing for one test and puts it back.
//
// The catalog service is shared by the whole package and integration tests run
// serially, so this mirrors withGlobalCeiling in otp_global_ceiling_test.go —
// and it exists for the same reason: proving the per-buyer window binds by
// actually sending twenty mails would be a slow test that says nothing the same
// test at two does not. The numbers are configuration; the behaviour AT them is
// what is under test.
func withAssignmentMailLimits(t *testing.T, limits catalog.AssignmentMailLimits) {
	t.Helper()
	original := sharedApp.CatalogService.AssignmentMailLimits()
	sharedApp.CatalogService.WithAssignmentMailLimits(limits)
	t.Cleanup(func() {
		sharedApp.CatalogService.WithAssignmentMailLimits(original)
	})
}

// assignmentMailCount is how many Assignment mails the platform has sent in all.
// The captured sender is the only honest place to ask.
func assignmentMailCount(env *testEnv) int {
	return len(env.email.TicketAssignmentsSent())
}

// ONE TICKET IS NOT AN UNLIMITED MAILER. This is the centre of #332: a buyer
// re-points one Ticket at address after address, and the platform stops writing
// to strangers on their say-so — permanently, for that Ticket.
//
// IT RUNS AT THE REAL DEFAULT CAP rather than a lowered one, deliberately, and
// it is the one test here that does. The per-Ticket number is a promise ADR 0046
// made about how many people the platform will write to on one ticket's behalf,
// and a test that lowered it first would pass just as happily if somebody
// shipped the default at fifty.
func TestOneTicketStopsSendingAssignmentMailsAtItsCap(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	// The first send, then the resend allowance: a mistyped address corrected,
	// and then the friend who says "that's my old address". Every one of these
	// must go through, because the commonest honest use of reassignment is a
	// buyer fixing a typo they cannot check.
	spent := []string{"carla@example.com", "dani@example.com", "elena@example.com"}
	for i, address := range spent {
		if i >= catalog.AssignmentMailsPerTicket {
			break
		}
		assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, address)
		if got := assignmentMailCount(env); got != i+1 {
			t.Fatalf("after %d assignment(s) the platform has sent %d Assignment mail(s), want %d",
				i+1, got, i+1)
		}
	}

	last := spent[catalog.AssignmentMailsPerTicket-1]
	sentAtTheCap := assignmentMailCount(env)
	rowAtTheCap := readTicketAssignment(t, env, ticketID)

	// The Holder at the last address has a live link in their inbox. It is
	// pulled out now, before the refusal, so the test can prove the refusal did
	// not quietly kill it.
	liveMail := assignmentMailFor(t, env, last)

	// ONE MORE ADDRESS, AND THE PLATFORM DECLINES TO WRITE TO IT.
	resp, body := assignTicket(t, env, f.ana, f.anaSaleID, ticketID, "fatima@example.com")
	assertAPIError(t, resp, body, http.StatusConflict, "ASSIGNMENT_MAIL_CAP_REACHED")

	// THE POINT OF THE CONTROL IS THAT NO MAIL LEAVES THE BUILDING.
	if got := assignmentMailCount(env); got != sentAtTheCap {
		t.Fatalf("%d Assignment mail(s) went out after the cap was reached; the whole point is that none does",
			got-sentAtTheCap)
	}

	// AND THE ASSIGNMENT ITSELF IS REFUSED, not merely the mail. Writing the new
	// address and skipping the send would move assigned_at, which every
	// Assignment Link is signed over — the Holder below would lose their link
	// and Fatima would never get one, leaving a Ticket nobody can reach.
	row := readTicketAssignment(t, env, ticketID)
	if row.holderEmail.String != last {
		t.Fatalf("the Ticket now holds %q, want %q: a refused assignment must not write the address",
			row.holderEmail.String, last)
	}
	if !row.assignedAt.Time.Equal(rowAtTheCap.assignedAt.Time) {
		t.Fatalf("assigned_at moved from %v to %v on a refused assignment.\n"+
			"Every outstanding Assignment Link is signed over it, so moving it without mailing a new one "+
			"kills the Holder's link and replaces it with nothing (#325, ADR 0046).",
			rowAtTheCap.assignedAt.Time, row.assignedAt.Time)
	}

	// The proof that survives is the link itself: the last Holder can still
	// accept, which is exactly what a write-without-send would have taken away.
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, liveMail))
}

// THE REFUSAL IS ABOUT SENDING, NOT ABOUT SAVING. A buyer who presses save twice
// on a Ticket whose allowance is spent is restating the address that is already
// there — nobody is written to, so nobody may be rationed. Refusing it would
// tell a buyer their own current state is forbidden.
func TestATicketAtItsCapStillAcceptsTheAddressItAlreadyHolds(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	// Lowered to one: the resend allowance is proved above, and what is under
	// test here is the no-op, which needs the cap spent and nothing else.
	withAssignmentMailLimits(t, catalog.AssignmentMailLimits{PerTicket: 1})
	ticketID := f.anaTicketIDs[0]

	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")
	sent := assignmentMailCount(env)
	before := readTicketAssignment(t, env, ticketID)

	// The same address again: allowed, silent, and it moves nothing.
	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")
	if got := assignmentMailCount(env); got != sent {
		t.Errorf("re-submitting the same address sent %d more mail(s); it changed nobody's Holder",
			got-sent)
	}
	after := readTicketAssignment(t, env, ticketID)
	if !after.assignedAt.Time.Equal(before.assignedAt.Time) {
		t.Errorf("assigned_at moved on a no-op: %v then %v", before.assignedAt.Time, after.assignedAt.Time)
	}

	// A DIFFERENT address is the thing the cap refuses.
	resp, body := assignTicket(t, env, f.ana, f.anaSaleID, ticketID, "dani@example.com")
	assertAPIError(t, resp, body, http.StatusConflict, "ASSIGNMENT_MAIL_CAP_REACHED")
	if got := assignmentMailCount(env); got != sent {
		t.Errorf("%d Assignment mail(s) went out past the cap", got-sent)
	}
}

// ASSIGNMENT CANNOT BE SCRIPTED ACROSS MANY TICKETS AT ONCE. The per-Ticket cap
// alone is defeated by buying fifty free tickets and sending one mail from each;
// the buyer's own window is the only control that sees those fifty as one
// person.
//
// The limit is lowered to two because what matters is the behaviour at it, and
// because the fixture's buyer holds two Tickets — the refusal below lands on a
// Ticket with plenty of its OWN allowance left, which is the whole point of
// having a second control.
func TestABuyerCannotScriptAssignmentAcrossTheirTicketsFasterThanTheirWindow(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	withAssignmentMailLimits(t, catalog.AssignmentMailLimits{PerBuyer: 2})

	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "dani@example.com")
	spent := assignmentMailCount(env)
	if spent != 2 {
		t.Fatalf("the platform sent %d Assignment mail(s) for two assignments, want 2", spent)
	}
	before := readTicketAssignment(t, env, f.anaTicketIDs[0])

	// A third address, on a Ticket that has sent exactly one mail of its own and
	// is nowhere near its own cap. It is the BUYER who is out of room.
	resp, body := assignTicket(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "elena@example.com")
	assertAPIError(t, resp, body, http.StatusTooManyRequests, "ASSIGNMENT_RATE_LIMITED")

	if got := assignmentMailCount(env); got != spent {
		t.Fatalf("%d Assignment mail(s) went out past the buyer's rate limit", got-spent)
	}
	// Refused outright, exactly as the cap is, and for the same reason: Carla's
	// link stays alive rather than being killed by a write that mailed nobody.
	after := readTicketAssignment(t, env, f.anaTicketIDs[0])
	if after.holderEmail.String != "carla@example.com" || !after.assignedAt.Time.Equal(before.assignedAt.Time) {
		t.Fatalf("a rate-limited assignment wrote anyway: holder=%q assigned_at %v then %v",
			after.holderEmail.String, before.assignedAt.Time, after.assignedAt.Time)
	}

	// THE WINDOW ROLLS, and that is what makes this refusal different from the
	// cap above: it is an error the buyer can act on by waiting, and the message
	// tells them so. A limit that never cleared would be a cap wearing a 429.
	holdClocksAt(fixedClock.Add(catalog.AssignmentMailWindow + time.Minute))
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "elena@example.com")
	if got := assignmentMailCount(env); got != spent+1 {
		t.Fatalf("the platform sent %d mail(s) once the window had rolled, want 1", got-spent)
	}
	// And it really went to the new address, so the window rolling restores the
	// whole act and not just the status code.
	assignmentMailFor(t, env, "elena@example.com")
}

// THE TWO REFUSALS ARE NEVER THE SAME REFUSAL. Both stop a mail, and they say
// different things to the person reading them: one clears by waiting and the
// other never does. This is the discipline the OTP service already keeps between
// its per-key limit and its global ceiling, and the reason the Storefront can
// key two different sentences off two different codes (ADR 0023).
func TestTheTicketCapAndTheBuyerRateLimitAreToldApart(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	withAssignmentMailLimits(t, catalog.AssignmentMailLimits{PerTicket: 1, PerBuyer: 2})

	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "dani@example.com")
	spent := assignmentMailCount(env)

	// Ticket 0 is out of its own allowance AND its buyer is out of window. The
	// permanent refusal is the one reported: telling this buyer to come back
	// later would send them back forever, since this Ticket is finished.
	resp, body := assignTicket(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "elena@example.com")
	assertAPIError(t, resp, body, http.StatusConflict, "ASSIGNMENT_MAIL_CAP_REACHED")

	// Once the window has rolled, the same request hears the same permanent
	// refusal — which is what proves the first one was not the rate limit in
	// disguise.
	holdClocksAt(fixedClock.Add(catalog.AssignmentMailWindow + time.Minute))
	resp, body = assignTicket(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "elena@example.com")
	assertAPIError(t, resp, body, http.StatusConflict, "ASSIGNMENT_MAIL_CAP_REACHED")

	if got := assignmentMailCount(env); got != spent {
		t.Fatalf("%d Assignment mail(s) went out across two refusals", got-spent)
	}
}
