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
func TestMayRemindAReversedSaleIsNeverChased(t *testing.T) {
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

// DueAnswerReminder.Inputs is the only way a candidate becomes a decision, so
// what it carries across is worth pinning: a candidate that came back from the
// sweep has a debt by construction, and every rationing fact travels with the
// row rather than being re-read from somewhere else.
func TestDueAnswerReminderInputsCarryTheWholeRule(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	last := now.Add(-8 * 24 * time.Hour)
	due := DueAnswerReminder{
		TicketSaleID:   "sale-1",
		SaleStatus:     TicketSaleStatusActive,
		EventStartsAt:  now.Add(48 * time.Hour),
		RemindersSent:  1,
		LastRemindedAt: last,
	}

	in := due.Inputs(now)
	if !in.HasOutstandingAnswers {
		t.Fatal("a candidate the sweep returned owes Answers by construction")
	}
	if in.SaleStatus != due.SaleStatus || !in.EventStartsAt.Equal(due.EventStartsAt) ||
		in.RemindersSent != due.RemindersSent || !in.LastRemindedAt.Equal(due.LastRemindedAt) ||
		!in.Now.Equal(now) {
		t.Fatalf("Inputs dropped or transposed a fact: %+v", in)
	}
	if !MayRemind(in) {
		t.Fatal("an eight-day-old first reminder on an upcoming event is due its second")
	}
}
