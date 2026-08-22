package catalog

import "testing"

// The rationing rule as a rule, beside answer_reminder_test.go and in the same
// spirit: the arithmetic of "may the platform write to one more stranger" is
// worth stating where it can be read in one screen, and the integration suite
// then proves it is actually WIRED to a send.
//
// These tests deliberately do not name the constants' values. What must hold is
// the shape — a lifetime cap, a windowed one, and which of the two is reported
// when both are tripped — and a test that hard-coded three would have to be
// edited to change a number that is explicitly configuration.

func allowedInputs() AssignmentMailInputs {
	return AssignmentMailInputs{
		SentForTicket:       0,
		SentByBuyerInWindow: 0,
		Limits:              AssignmentMailLimits{PerTicket: 3, PerBuyer: 5},
	}
}

func TestMayMailAssignmentAFirstSendIsAlwaysAllowed(t *testing.T) {
	if got := MayMailAssignment(allowedInputs()); got != AssignmentMailAllowed {
		t.Fatalf("a Ticket that has never mailed anybody is refused: %v", got)
	}
}

// THE RESEND ALLOWANCE IS THE POINT OF THE CAP BEING MORE THAN ONE. A buyer
// typing somebody else's address from memory gets it wrong, and nothing
// downstream can tell them so — a cap of one would make a single slipped
// character permanent.
func TestMayMailAssignmentACorrectedAddressIsNotBlockedOnTheFirstAttempt(t *testing.T) {
	in := allowedInputs()
	in.SentForTicket = 1
	if got := MayMailAssignment(in); got != AssignmentMailAllowed {
		t.Fatalf("correcting a mistyped address is refused: %v", got)
	}
}

func TestMayMailAssignmentTheTicketCapIsAHardStop(t *testing.T) {
	in := allowedInputs()
	in.SentForTicket = in.Limits.PerTicket
	if got := MayMailAssignment(in); got != AssignmentMailRefusedTicketCap {
		t.Fatalf("a Ticket at its lifetime cap is allowed another mail: %v", got)
	}
}

func TestMayMailAssignmentTheBuyerWindowIsAHardStop(t *testing.T) {
	in := allowedInputs()
	in.SentByBuyerInWindow = in.Limits.PerBuyer
	if got := MayMailAssignment(in); got != AssignmentMailRefusedBuyerRate {
		t.Fatalf("a buyer at their window's limit is allowed another mail: %v", got)
	}
}

// THE ORDER IS THE TEST. Both refusals are true at once here, and they say
// different things to the buyer: one clears by waiting, the other never does.
// Reporting "come back later" for a Ticket that is finished would send an honest
// buyer back in an hour to hear the same refusal forever.
func TestMayMailAssignmentReportsThePermanentRefusalAheadOfTheTemporaryOne(t *testing.T) {
	in := allowedInputs()
	in.SentForTicket = in.Limits.PerTicket
	in.SentByBuyerInWindow = in.Limits.PerBuyer
	if got := MayMailAssignment(in); got != AssignmentMailRefusedTicketCap {
		t.Fatalf("with both limits tripped the refusal is %v, want the per-Ticket cap", got)
	}
}

// AN UNSET LIMIT IS THE DEFAULT AND NEVER "SEND NOTHING". A service nobody
// configured must still mail the first Holder; turning the feature off is the
// flag's job.
func TestMayMailAssignmentUnsetLimitsFallBackToTheDefaults(t *testing.T) {
	in := AssignmentMailInputs{}
	if got := MayMailAssignment(in); got != AssignmentMailAllowed {
		t.Fatalf("a service with no limits configured sends nothing: %v", got)
	}
	in.SentForTicket = AssignmentMailsPerTicket
	if got := MayMailAssignment(in); got != AssignmentMailRefusedTicketCap {
		t.Fatalf("the default per-Ticket cap does not bind: %v", got)
	}
	in.SentForTicket = 0
	in.SentByBuyerInWindow = AssignmentMailsPerBuyer
	if got := MayMailAssignment(in); got != AssignmentMailRefusedBuyerRate {
		t.Fatalf("the default per-buyer limit does not bind: %v", got)
	}
}
