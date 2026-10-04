package catalog

import (
	"testing"
	"time"
)

// The swept sender's judgement on one owed Assignment mail, as a rule (#671,
// parent #665, ADR 0076). The integration suite proves it is wired to a send;
// this states which fact drops a mail and in what order.

var owedNow = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

func owedInputs() OwedAssignmentMailInputs {
	assignedAt := owedNow.Add(-time.Minute)
	startsAt := owedNow.Add(30 * 24 * time.Hour)
	return OwedAssignmentMailInputs{
		OwedFor:       assignedAt,
		HolderEmail:   "ben@example.com",
		AssignedAt:    &assignedAt,
		Channel:       SalesChannelOnline,
		SaleStatus:    TicketSaleStatusActive,
		EventStartsAt: &startsAt,
		Now:           owedNow,
	}
}

func TestAnOwedMailForTheAssignmentTheTicketStillCarriesIsSent(t *testing.T) {
	if got := DecideOwedAssignmentMail(owedInputs()); got != OwedAssignmentMailSend {
		t.Fatalf("fate = %v, want send", got)
	}
}

func TestAnOwedMailOnAnEventWithNoStartIsSent(t *testing.T) {
	in := owedInputs()
	in.EventStartsAt = nil
	if got := DecideOwedAssignmentMail(in); got != OwedAssignmentMailSend {
		t.Fatalf("fate = %v, want send - an unscheduled Event has not started", got)
	}
}

// THE LINK IS SIGNED OVER assigned_at, so a Ticket reassigned since the mail
// was owed - to anybody, even the same address again after a detour - carries
// an assignment this mail is not about, and its link would open nowhere.
func TestAnOwedMailForAnAssignmentTheTicketNoLongerCarriesIsDropped(t *testing.T) {
	in := owedInputs()
	moved := in.OwedFor.Add(time.Second)
	in.AssignedAt = &moved
	if got := DecideOwedAssignmentMail(in); got != OwedAssignmentMailDropStale {
		t.Fatalf("fate = %v, want dropped as stale", got)
	}
}

// The Holder Address Purge takes an unaccepted address at the doors and leaves
// the owed row; and a Ticket taken back by the buyer carries no address either.
func TestAnOwedMailForATicketWithNoAddressIsDropped(t *testing.T) {
	in := owedInputs()
	in.HolderEmail = ""
	if got := DecideOwedAssignmentMail(in); got != OwedAssignmentMailDropStale {
		t.Fatalf("fate = %v, want dropped as stale", got)
	}
	in = owedInputs()
	in.AssignedAt = nil
	if got := DecideOwedAssignmentMail(in); got != OwedAssignmentMailDropStale {
		t.Fatalf("fate = %v, want dropped as stale", got)
	}
}

// Already accepted is already told: there is nothing left for the mail to do.
func TestAnOwedMailForAnAcceptedTicketIsDropped(t *testing.T) {
	in := owedInputs()
	accepted := owedNow
	in.AcceptedAt = &accepted
	if got := DecideOwedAssignmentMail(in); got != OwedAssignmentMailDropStale {
		t.Fatalf("fate = %v, want dropped as stale", got)
	}
}

func TestAnOwedMailOnAReversedSaleIsDropped(t *testing.T) {
	in := owedInputs()
	in.SaleStatus = "reversed"
	if got := DecideOwedAssignmentMail(in); got != OwedAssignmentMailDropSaleReversed {
		t.Fatalf("fate = %v, want dropped for the reversal", got)
	}
}

// AT THE DOORS AND NOT A MOMENT AFTER: the same instant the assignment window
// shuts and the accept link stops opening.
func TestAnOwedMailIsDroppedTheMomentTheEventStarts(t *testing.T) {
	in := owedInputs()
	startsAt := owedNow
	in.EventStartsAt = &startsAt
	if got := DecideOwedAssignmentMail(in); got != OwedAssignmentMailDropEventStarted {
		t.Fatalf("fate = %v, want dropped at the doors", got)
	}
	before := owedNow.Add(time.Second)
	in.EventStartsAt = &before
	if got := DecideOwedAssignmentMail(in); got != OwedAssignmentMailSend {
		t.Fatalf("fate a second before the doors = %v, want send", got)
	}
}

// The reversal is reported before the staleness, on AssignmentWindow's terms:
// the Sale is the larger fact, and an operator reading why a mail was dropped
// should hear it.
func TestTheSaleIsJudgedBeforeTheAssignment(t *testing.T) {
	in := owedInputs()
	in.SaleStatus = "reversed"
	in.HolderEmail = ""
	if got := DecideOwedAssignmentMail(in); got != OwedAssignmentMailDropSaleReversed {
		t.Fatalf("fate = %v, want dropped for the reversal", got)
	}
}

// A failed send is retried at the next run three times, then waits 5, 15, 45
// and 90 minutes, and never longer than the last step: the Event's start, not
// an attempt count, is what ends an owed mail.
func TestTheRetryDelayRetriesAtTheNextRunThenBacksOff(t *testing.T) {
	want := []time.Duration{
		time.Minute, time.Minute, time.Minute,
		5 * time.Minute, 15 * time.Minute, 45 * time.Minute, 90 * time.Minute,
	}
	for i, delay := range want {
		if got := OwedAssignmentMailRetryDelay(i + 1); got != delay {
			t.Fatalf("attempt %d waits %v, want %v", i+1, got, delay)
		}
	}
	if got := OwedAssignmentMailRetryDelay(100); got != 90*time.Minute {
		t.Fatalf("attempt 100 waits %v, want the last step, 90m", got)
	}
	if got := OwedAssignmentMailRetryDelay(0); got != OwedAssignmentMailRetryDelay(1) {
		t.Fatalf("a row never claimed waits %v, want the first step", got)
	}
	// The first retry is the next tick of a per-minute job, never sooner: a
	// provider that refused this second is not better in ten.
	if OwedAssignmentMailRetryDelay(1) < time.Minute {
		t.Fatalf("the first retry waits %v, under a minute", OwedAssignmentMailRetryDelay(1))
	}
	// And a claim's lease outlives any healthy send, so two sweeps never both
	// hold one row.
	if OwedAssignmentMailClaimLease < time.Minute {
		t.Fatalf("the claim lease is %v, too short to outlive a send", OwedAssignmentMailClaimLease)
	}
}
