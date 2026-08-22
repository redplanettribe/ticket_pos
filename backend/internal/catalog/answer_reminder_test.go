package catalog

import (
	"testing"
	"time"
)

// The Answer Reminder's rationing (#317, ADR 0044), one clause per test.
//
// These are the rules the ticket exists for. Everything else about the reminder
// — the sweep, the copy, the scheduler — is machinery around the four sentences
// asserted here: not after the doors open, never for a reversed sale, at most
// one a week, at most two ever.

// remindNow is the one arrangement that DOES send: a live debt on an active
// sale, an event still to come, nobody chased yet. Every test below changes
// exactly one fact away from it, so each names the single clause it is about —
// the same shape outstanding_answer_test.go uses, for the same reason.
func remindNow() AnswerReminderInputs {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	return AnswerReminderInputs{
		HasOutstandingAnswers: true,
		SaleStatus:            TicketSaleStatusActive,
		EventStartsAt:         now.Add(30 * 24 * time.Hour),
		Now:                   now,
		RemindersSent:         0,
		LastRemindedAt:        time.Time{},
	}
}

func TestMayRemindALiveDebtOnAnUpcomingEvent(t *testing.T) {
	if !MayRemind(remindNow()) {
		t.Fatal("a never-reminded active sale owing Answers, on an event still to come, is exactly what this mail is for")
	}
}

// Nothing owed, nothing to say. The sweep's WHERE already guarantees this, and
// the clause is here so that the function reads as the whole rule rather than as
// the half the query does not cover.
func TestMayRemindNothingOutstandingIsNothingToSay(t *testing.T) {
	in := remindNow()
	in.HasOutstandingAnswers = false
	if MayRemind(in) {
		t.Fatal("a sale owing no Answers must never be reminded about them")
	}
}

// A REVERSED SALE IS NEVER CHASED. Its tickets have ceased to exist and its
// money has gone back: there is no shirt to order and nobody to order it for,
// and a mail asking that buyer to answer questions about tickets they no longer
// hold is the worst message this feature could send.
func TestMayRemindAReversedSaleIsNeverReminded(t *testing.T) {
	in := remindNow()
	in.SaleStatus = "reversed"
	if MayRemind(in) {
		t.Fatal("a reversed Ticket Sale must never receive an Answer Reminder")
	}
}

// SILENT ONCE THE EVENT HAS STARTED. The debt itself survives the doors opening
// — IsOutstandingAnswer has no clock in it, deliberately, because "twelve people
// never told us their size" is what somebody reviewing the event came to find
// out. What ends is the mailing: every route into an Answer is frozen by then
// (catalog.AnswerWindow), so a reminder would be an instruction its reader
// cannot follow.
func TestMayRemindIsSilentOnceTheEventHasStarted(t *testing.T) {
	in := remindNow()
	in.EventStartsAt = in.Now.Add(-time.Minute)
	if MayRemind(in) {
		t.Fatal("an event that has already started must be silent")
	}
}

// The boundary is the start itself, not a moment after it: at the instant the
// doors open the answering window has closed.
func TestMayRemindIsSilentAtTheExactStart(t *testing.T) {
	in := remindNow()
	in.EventStartsAt = in.Now
	if MayRemind(in) {
		t.Fatal("the doors opening is the moment the reminder goes quiet, not a moment after it")
	}
}

// An Event nobody has placed in time is refused rather than allowed. Silence is
// the safe direction: there are no doors to be before, and the Answer Links the
// buyer would be sent to distribute expire at a start that does not exist.
func TestMayRemindAnUnscheduledEventIsSilent(t *testing.T) {
	in := remindNow()
	in.EventStartsAt = time.Time{}
	if MayRemind(in) {
		t.Fatal("an Event with no start must not be reminded about: the mail's whole premise is a date it does not have")
	}
}

// AT MOST ONE A WEEK. The cooldown is measured from the last send, so an
// Organization that authors four questions in ten minutes cannot mail the same
// buyer four times — which is the entire reason this is a swept job rather than
// something the question editor triggers.
func TestMayRemindHoldsOffInsideTheWeek(t *testing.T) {
	in := remindNow()
	in.RemindersSent = 1
	in.LastRemindedAt = in.Now.Add(-6 * 24 * time.Hour)
	if MayRemind(in) {
		t.Fatal("a buyer reminded six days ago must not be reminded again today")
	}
}

// A second short of the week is still inside it. The boundary is asserted from
// both sides because an off-by-one here is a mail nobody would ever notice was
// early.
func TestMayRemindHoldsOffASecondShortOfTheWeek(t *testing.T) {
	in := remindNow()
	in.RemindersSent = 1
	in.LastRemindedAt = in.Now.Add(-AnswerReminderInterval + time.Second)
	if MayRemind(in) {
		t.Fatal("the cooldown must not expire a second early")
	}
}

func TestMayRemindSendsAgainOnceTheWeekIsUp(t *testing.T) {
	in := remindNow()
	in.RemindersSent = 1
	in.LastRemindedAt = in.Now.Add(-AnswerReminderInterval)
	if !MayRemind(in) {
		t.Fatal("exactly seven days after the first reminder, the second is due")
	}
}

// AT MOST TWO EVER, and this is the clause with no way back. Once a Ticket Sale
// has had its two, no passage of time and no newly authored question buys
// another: the cap is per Sale over its whole life, because a buyer with four
// unanswered Tickets is still one person with one inbox.
func TestMayRemindStopsForeverAtTheCap(t *testing.T) {
	in := remindNow()
	in.RemindersSent = MaxAnswerReminders
	in.LastRemindedAt = in.Now.Add(-52 * 7 * 24 * time.Hour)
	if MayRemind(in) {
		t.Fatal("a Ticket Sale reminded twice must never be reminded again, however long ago the second one was")
	}
}

// The cap is a CAP and not an equality: a ledger that somehow holds more rows
// than the cap allows — a concurrent double-send, a bad backfill — must go quiet
// rather than wrap around into sending again.
func TestMayRemindStopsAboveTheCapToo(t *testing.T) {
	in := remindNow()
	in.RemindersSent = MaxAnswerReminders + 3
	in.LastRemindedAt = in.Now.Add(-52 * 7 * 24 * time.Hour)
	if MayRemind(in) {
		t.Fatal("more sends than the cap allows must still be silence, never a wrap-around")
	}
}

// Never reminded is a case in its own right, not an accident of comparing
// against a zero timestamp. The first reminder is due the day the debt appears.
func TestMayRemindTheFirstOneNeedsNoCooldown(t *testing.T) {
	in := remindNow()
	in.RemindersSent = 0
	in.LastRemindedAt = time.Time{}
	if !MayRemind(in) {
		t.Fatal("a sale nobody has ever chased is due its first reminder now")
	}
}

// The cap is two, and this is the test that fails if somebody quietly raises it.
// A third reminder would be nagging somebody about a t-shirt size on a mail they
// cannot turn off: this is transactional and carries no unsubscribe, so the
// constant is the only thing bounding the chase.
func TestTheAnswerReminderCapIsTwo(t *testing.T) {
	if MaxAnswerReminders != 2 {
		t.Fatalf("MaxAnswerReminders = %d, want 2: a transactional mail with no unsubscribe is bounded by this number and nothing else", MaxAnswerReminders)
	}
	if AnswerReminderInterval != 7*24*time.Hour {
		t.Fatalf("AnswerReminderInterval = %v, want 7 days", AnswerReminderInterval)
	}
}

// AnswerReminderCandidate.Inputs is the only way a candidate becomes a decision,
// so what it carries across is worth pinning: a candidate that came back from
// the sweep has a debt by construction, and every rationing fact travels with
// the row rather than being re-read from somewhere else.
//
// THE CANDIDATE IS A TICKET SINCE #328, and RemindersSent is that Ticket's own
// ledger rather than its Sale's. That is the whole of what moved here.
func TestAnswerReminderCandidateInputsCarryTheWholeRule(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	last := now.Add(-8 * 24 * time.Hour)
	candidate := AnswerReminderCandidate{
		TicketID:       "ticket-1",
		TicketSaleID:   "sale-1",
		SaleStatus:     TicketSaleStatusActive,
		EventStartsAt:  now.Add(48 * time.Hour),
		RemindersSent:  1,
		LastRemindedAt: last,
	}

	in := candidate.Inputs(now)
	if !in.HasOutstandingAnswers {
		t.Fatal("a candidate the sweep returned owes Answers by construction")
	}
	if in.SaleStatus != candidate.SaleStatus || !in.EventStartsAt.Equal(candidate.EventStartsAt) ||
		in.RemindersSent != candidate.RemindersSent || !in.LastRemindedAt.Equal(candidate.LastRemindedAt) ||
		!in.Now.Equal(now) {
		t.Fatalf("Inputs dropped or transposed a fact: %+v", in)
	}
	if !MayRemind(in) {
		t.Fatal("an eight-day-old first reminder on an upcoming event is due its second")
	}
}

// THE RULE HAS NO OPINION ABOUT WHO IS ADDRESSED. Whether a held Ticket may be
// chased is a fact about the Ticket; who is chased is AnswerReminderRecipient's
// question. AnswerReminderInputs has no field for a recipient, and the
// allowance is the TICKET'S: one that has spent it is silent whoever holds it
// now, which is what stops a reassignment from buying a fresh chase.
func TestMayRemindDoesNotDependOnWhoIsAddressed(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	candidate := AnswerReminderCandidate{
		TicketID:      "ticket-1",
		TicketSaleID:  "sale-1",
		HolderEmail:   "carla@example.com",
		SaleStatus:    TicketSaleStatusActive,
		EventStartsAt: now.Add(48 * time.Hour),
	}
	if !MayRemind(candidate.Inputs(now)) {
		t.Fatal("a held Ticket owing an Answer on an upcoming Event is due its first reminder")
	}

	spent := candidate
	spent.HolderEmail = "diego@example.com"
	spent.RemindersSent = MaxAnswerReminders
	if MayRemind(spent.Inputs(now)) {
		t.Fatal("a Ticket that had spent its allowance was chased again because it had changed hands: the cap is the Ticket's for its whole life")
	}
}

// HOLDER OR NOBODY (ADR 0049). An accepted Ticket is chased through the address
// that accepted it — a named Holder or the buyer of a Self-held Ticket alike —
// and every other assignment state has no recipient at all.
func TestAnswerReminderRecipientIsTheHolderOfAnAcceptedTicket(t *testing.T) {
	assigned := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	accepted := assigned.Add(time.Hour)

	if got := AnswerReminderRecipient("carla@example.com", &assigned, &accepted); got != "carla@example.com" {
		t.Fatalf("recipient = %q, want the address that accepted the Ticket", got)
	}
	// A Self-held Ticket is accepted by paying (ADR 0048) and reads exactly as
	// any accepted one: the buyer is chased as its Holder and as nothing else.
	if got := AnswerReminderRecipient("ana@example.com", &assigned, &assigned); got != "ana@example.com" {
		t.Fatalf("recipient of a Self-held Ticket = %q, want the buyer, as its Holder", got)
	}
}

func TestAnswerReminderRecipientIsNobodyForAnUnheldTicket(t *testing.T) {
	assigned := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)

	if got := AnswerReminderRecipient("", nil, nil); got != "" {
		t.Fatalf("an unassigned Ticket has recipient %q, want nobody: an unassigned Ticket is an assignment debt, not an Answer debt", got)
	}
	// Assigned and never accepted: an address that has agreed to nothing, and
	// ignoring the Assignment mail IS the decline (ADR 0046).
	if got := AnswerReminderRecipient("carla@example.com", &assigned, nil); got != "" {
		t.Fatalf("an assigned-but-unaccepted Ticket has recipient %q, want nobody", got)
	}
	// Purged by the Holder Address Purge: the address is gone and the Ticket
	// reads unassigned again, whatever assigned_at still says.
	if got := AnswerReminderRecipient("", &assigned, nil); got != "" {
		t.Fatalf("a purged never-accepted Ticket has recipient %q, want nobody", got)
	}
}
