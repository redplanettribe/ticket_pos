package catalog

import "time"

// The swept sender of the Assignment mails a Named Tickets checkout owes
// (#671, parent #665, ADR 0076).
//
// THE COMMIT OWES, THE SWEEP SENDS. The transaction that records a Ticket Sale
// assigns every Ticket the buyer named at checkout and writes one row in
// `owed_assignment_mails` (migration 127) per Ticket assigned to somebody other
// than the buyer. It cannot send: a transaction cannot take a mail back, and
// nine at once is the burst that tripped the provider's rate limit on the
// Assignment Reminder's first run. A paced sweep sends them afterwards, one by
// one, and keeps what fails for the next run.
//
// THE MAIL IS THE ORDINARY ASSIGNMENT MAIL, composed by the one composer and
// carrying the one Assignment Link (catalog/service mailTicketAssignment), so a
// Holder named at checkout and one named a week later cannot tell the two
// apart. What differs is only who decides it is time to send, and that the
// buyer's rolling window neither refuses nor is spent by it (migration 128).
//
// WHAT IS DECIDED HERE is whether a mail that WAS owed still is. Everything
// below is read live at send time, because the minutes or hours between the
// commit and the send are exactly when a buyer corrects a typo, a Sale is
// reversed, or the doors open.

// OwedAssignmentMailFate is what the sweep does with one owed mail.
type OwedAssignmentMailFate int

const (
	// OwedAssignmentMailSend: the Ticket still carries the assignment the mail
	// was owed for, nobody has accepted it, and it may still be accepted.
	OwedAssignmentMailSend OwedAssignmentMailFate = iota
	// OwedAssignmentMailDropStale: the Ticket no longer carries the assignment
	// the mail was owed for. Reassigned since (assigned_at moved), its address
	// taken (by the Holder Address Purge, or by the buyer taking it back), or
	// already accepted. The link would open nowhere, or there is nobody left to
	// tell.
	OwedAssignmentMailDropStale
	// OwedAssignmentMailDropSaleReversed: the Ticket Sale was reversed. A
	// refunded Ticket has nobody to hand it to.
	OwedAssignmentMailDropSaleReversed
	// OwedAssignmentMailDropEventStarted: the doors have opened, which closes
	// the assignment window, stops the link opening, and is when the Holder
	// Address Purge takes the address anyway.
	OwedAssignmentMailDropEventStarted
)

// String names a fate for the log line, which names no person.
func (f OwedAssignmentMailFate) String() string {
	switch f {
	case OwedAssignmentMailSend:
		return "send"
	case OwedAssignmentMailDropStale:
		return "stale"
	case OwedAssignmentMailDropSaleReversed:
		return "sale_reversed"
	case OwedAssignmentMailDropEventStarted:
		return "event_started"
	default:
		return "unknown"
	}
}

// OwedAssignmentMailInputs are the facts, and the only facts, that decide an
// owed mail's fate.
type OwedAssignmentMailInputs struct {
	// OwedFor is the `tickets.assigned_at` the commit copied onto the owed row.
	OwedFor time.Time
	// HolderEmail, AssignedAt and AcceptedAt are the Ticket's assignment NOW.
	HolderEmail string
	AssignedAt  *time.Time
	AcceptedAt  *time.Time
	// Channel and SaleStatus are the Ticket Sale's, read now.
	Channel    string
	SaleStatus string
	// EventStartsAt is the instant the doors open, nil on an Event that has not
	// said when. The column stores the instant the Event's local clock named, so
	// comparing it to Now is the comparison "in the Event's timezone".
	EventStartsAt *time.Time
	Now           time.Time
}

// DecideOwedAssignmentMail is the single place an owed mail's fate is decided.
//
// THE WINDOW FIRST, THROUGH AssignmentWindow AND NOT A COPY OF IT. A Ticket
// whose assignment can no longer be made or accepted is owed no mail about one,
// and the reason is the same one every other route into an assignment gives.
// Only then the staleness, which is about this mail alone.
//
// assigned_at IS COMPARED AS AN INSTANT. Both values were read from the same
// TIMESTAMPTZ column, so they are equal to the microsecond when nothing moved.
func DecideOwedAssignmentMail(in OwedAssignmentMailInputs) OwedAssignmentMailFate {
	switch AssignmentWindow(in.Channel, in.SaleStatus, in.EventStartsAt, in.Now) {
	case AssignmentRefusedEventStarted:
		return OwedAssignmentMailDropEventStarted
	case AssignmentRefusedSaleReversed:
		return OwedAssignmentMailDropSaleReversed
	case AssignmentRefusedChannel:
		// Unreachable: only a Storefront checkout owes a mail. A Ticket that
		// cannot be assigned at all is owed nothing about an assignment.
		return OwedAssignmentMailDropStale
	}
	if in.HolderEmail == "" || in.AssignedAt == nil || !in.AssignedAt.Equal(in.OwedFor) || in.AcceptedAt != nil {
		return OwedAssignmentMailDropStale
	}
	return OwedAssignmentMailSend
}

// OwedAssignmentMailOutcome is what one step of the sweep did, reported to the
// sales module's loop, which paces by it and counts it.
type OwedAssignmentMailOutcome int

const (
	// OwedAssignmentMailNoneDue: nothing was claimed - nothing is due, or the
	// sender is held (Ticket Assignment closed, or no mailer or link secret
	// wired). The run stops.
	OwedAssignmentMailNoneDue OwedAssignmentMailOutcome = iota
	// OwedAssignmentMailSent: the provider accepted it; the ledger has it and
	// it is owed no more.
	OwedAssignmentMailSent
	// OwedAssignmentMailSentUnrecorded: the provider accepted it but the ledger
	// row could not be written. The Holder has the mail; the Ticket's lifetime
	// allowance under-counts by one.
	OwedAssignmentMailSentUnrecorded
	// OwedAssignmentMailFailed: the provider refused or failed it. It stays
	// owed, backed off, for a later run.
	OwedAssignmentMailFailed
	// OwedAssignmentMailDropped: it was owed no longer and went unsent.
	OwedAssignmentMailDropped
)

// ReachedProvider reports whether this step made a request of the mail
// provider, which is what the pacer spaces: a drop costs no pause.
func (o OwedAssignmentMailOutcome) ReachedProvider() bool {
	return o == OwedAssignmentMailSent || o == OwedAssignmentMailSentUnrecorded || o == OwedAssignmentMailFailed
}

// OwedAssignmentMailClaimLease is how long a claimed owed mail is hidden from
// other sweeps.
//
// It bounds only the ending that writes nothing: a sweep that died between
// claiming a mail and recording what became of it. Every other ending rewrites
// or deletes the row within seconds. Five minutes, as the Follow Digest's: long
// enough that no healthy send outlives it, short enough that a crash costs the
// Holder minutes.
const OwedAssignmentMailClaimLease = 5 * time.Minute

// owedAssignmentMailRetryBackoff is how long a failed send waits before the
// next attempt, indexed by how many attempts have been made.
//
// The Follow Digest's schedule, for its reason: the failures met here are the
// provider's - a rate limit, a 5xx, a timeout - and none is over in seconds.
//
// THERE IS NO LAST ATTEMPT. Unlike a Digest, which is about one week, this mail
// is about an assignment that stays acceptable until the doors open, and the
// doors opening is what drops it. Giving up earlier would leave a Holder the
// buyer named, and paid for, never told, with nothing on the buyer's page to
// say so.
var owedAssignmentMailRetryBackoff = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	45 * time.Minute,
	90 * time.Minute,
}

// OwedAssignmentMailRetryDelay is the wait after the given number of attempts,
// saturating at the schedule's last step.
func OwedAssignmentMailRetryDelay(attempts int) time.Duration {
	index := attempts - 1
	if index < 0 {
		index = 0
	}
	if index >= len(owedAssignmentMailRetryBackoff) {
		index = len(owedAssignmentMailRetryBackoff) - 1
	}
	return owedAssignmentMailRetryBackoff[index]
}
