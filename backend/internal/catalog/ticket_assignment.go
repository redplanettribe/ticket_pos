package catalog

import (
	"net/mail"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// This file holds everything a Ticket Assignment knows about itself: what a
// Holder address may be, which of three states a Ticket is in, and when a buyer
// may assign at all (#324, parent #322).
//
// ASSIGNMENT IS A FACT ABOUT A TICKET, exactly as an Answer is. The buyer names
// an email address for ONE Ticket — not for the Ticket Sale, which stays whole
// with them, and not for the Payment, which never sees a third party's address
// at all. A buyer of four tickets names four addresses, or two, or none.
//
// A HOLDER IS NOT AN OWNER. Nothing here moves a Ticket Sale, a Sale
// Confirmation, the money or the Reversal Window: the buyer may reassign any
// Ticket at any time, and does not stop being the party of record when they do.
// This is assignment, never transfer — see docs/prd-ticket-assignment.md.

// MaxHolderEmailLength caps a Holder address at the longest an address may be
// (RFC 5321's 254 for a forward path).
//
// It exists because this is the ONE text field on this platform typed by one
// person about a DIFFERENT person, so nothing downstream can sanity-check it
// against anything: nobody signs in with it, nobody corrects it but the buyer,
// and until #325 mails it nobody discovers it is nonsense. A cap is the only
// bound available at the moment of typing.
const MaxHolderEmailLength = 254

// TicketAssignmentState is which of the three states one Ticket is in.
//
// A STRING RATHER THAN AN INT, unlike AnswerRefusal beside it, because this one
// travels on the wire. It is the buyer's answer to "what happened to the address
// I typed", and a page that has to map 0/1/2 onto three words is a page holding
// a second copy of this enum.
type TicketAssignmentState string

const (
	// TicketUnassigned: no address has been given. Today's behaviour, unchanged
	// — the buyer, Event Staff and anyone holding the Answer Link may answer,
	// and this is what most Tickets will be for a long time.
	TicketUnassigned TicketAssignmentState = "unassigned"
	// TicketAssigned: an address has been given and nobody has accepted. In
	// #324 this is the terminal state, because no mail is sent: the address sits
	// on the Ticket, is shown back to the buyer so they can tell their Tickets
	// apart, and reaches nobody. #325 is what mails it.
	TicketAssigned TicketAssignmentState = "assigned"
	// TicketAccepted: a Holder proved the address and is a Customer.
	//
	// UNREACHABLE IN #324 AND DELIBERATELY MODELLED ANYWAY. Nothing in this
	// ticket writes accepted_at — there is no Assignment mail, so there is no
	// link to click. It exists here so that every reader added by this ticket
	// already knows the full state machine, rather than being found and changed
	// when #325 lands the accept flow.
	TicketAccepted TicketAssignmentState = "accepted"
)

// AssignmentState reads a Ticket's state off the two timestamps and the address.
//
// DERIVED AND NEVER STORED. There is no state column (migration 080): a stored
// state is a second copy of what these three columns already say, and the only
// thing a second copy can do is disagree. Every surface that reports a state —
// the buyer's list, the Organization's guest list, the export — comes through
// here, so "assigned" cannot mean one thing on one page and another elsewhere.
//
// THE ORDER OF THE TESTS IS THE MODEL. Acceptance is checked first because it is
// the strongest fact: a Ticket that has been accepted has necessarily been
// assigned, and reporting the weaker fact would lose the Holder.
func AssignmentState(holderEmail string, assignedAt, acceptedAt *time.Time) TicketAssignmentState {
	if acceptedAt != nil {
		return TicketAccepted
	}
	if holderEmail != "" && assignedAt != nil {
		return TicketAssigned
	}
	return TicketUnassigned
}

// The Sales Channels a Ticket Sale can have been recorded on (migration 010).
//
// Named here rather than compared as bare strings because assignment is the
// first feature to REFUSE one of them, and a channel test written as a literal
// in three places is three chances to spell it differently.
const (
	SalesChannelOnline   = "online"
	SalesChannelInPerson = "in_person"
	SalesChannelImport   = "import"
)

// AssignmentRefusal reports whether a Ticket may be assigned at all, and why not.
//
// A SEPARATE ENUM FROM AnswerRefusal even though two of its members read alike,
// because the two windows are separate rules that happen to agree today. The
// answer window has no opinion about the Sales Channel — an `in_person` Ticket's
// Answers are perfectly writable by Event Staff — and folding assignment into
// AnswerWindow would either close that door or smuggle a channel test into a
// function three other callers depend on.
type AssignmentRefusal int

const (
	// AssignmentWindowOpen: this Ticket may be assigned and reassigned.
	AssignmentWindowOpen AssignmentRefusal = iota
	// AssignmentRefusedChannel: the Ticket Sale was recorded at the door.
	//
	// `in_person` HAS NO BUYER SURFACE, and that is the whole reason. Assignment
	// happens after purchase, from the Confirmation Link page or the Customer
	// Area, and a door sale gives the buyer neither: the POS is unbuilt and
	// `in_person` exists today only as a Sales Channel value (see
	// integration/tickets_test.go). Refusing here is therefore stating a fact
	// about the sale rather than withholding a feature.
	AssignmentRefusedChannel
	// AssignmentRefusedSaleReversed: the Ticket's Ticket Sale has been reversed.
	// Nobody is coming on a ticket that was refunded, so there is nobody to hand
	// it to.
	AssignmentRefusedSaleReversed
	// AssignmentRefusedEventStarted: the Event has started.
	//
	// The window closes at the doors, exactly as the Answer window does and for
	// the same reason: assignment exists so that the right person is named
	// before the Organization has to act on it, and the last moment that is any
	// use is the moment the doors open. It is also when #322's retention purge
	// takes an unaccepted address, so an assignment made after it would be
	// naming somebody the platform is in the act of forgetting.
	AssignmentRefusedEventStarted
)

// AssignmentWindow is the single place the assignment window is decided, for
// every route into a Ticket Assignment.
//
// THE CHANNEL IS TESTED FIRST because it is the most fundamental fact: an
// `in_person` sale never had a buyer surface to assign from, whether or not its
// Event has started, and telling that buyer "the event has started" would send
// them looking for a deadline they never had. Same reasoning as AnswerWindow's
// reversal-before-clock ordering, one rung further down.
//
// eventStartsAt is nil for an Event that has not said when it starts. That Event
// has not started — reading nil as "started" would freeze assignment on every
// unscheduled Event, which is the wrong way to be wrong.
func AssignmentWindow(
	channel, saleStatus string,
	eventStartsAt *time.Time,
	now time.Time,
) AssignmentRefusal {
	if channel != SalesChannelOnline && channel != SalesChannelImport {
		return AssignmentRefusedChannel
	}
	if saleStatus != TicketSaleStatusActive {
		return AssignmentRefusedSaleReversed
	}
	if eventStartsAt != nil && !now.Before(*eventStartsAt) {
		return AssignmentRefusedEventStarted
	}
	return AssignmentWindowOpen
}

// ParseHolderEmail turns what the buyer typed into the address that will be
// stored, or reports that it is not one.
//
// IT NORMALISES THROUGH platform.NormalizeEmail, the single point at which an
// address is trimmed and lowercased on this platform. That matters more here
// than anywhere: #325 mints or matches a Customer from a click at this address,
// and an address stored as the buyer capitalised it would fail to match the
// Customer record the same person already has — quietly turning one person into
// two.
//
// THE SHAPE CHECK IS mail.ParseAddress, as at every other door an address comes
// through (checkout, Sale Import, passcode sign-in). It is a weak check by
// design: an address is only really validated by mail arriving at it, and this
// one belongs to somebody who is not here to confirm anything. What it catches
// is the typo with no `@` in it, which is worth catching because until #325
// mails the address NOBODY WILL DISCOVER IT IS WRONG — the buyer sees their own
// typo echoed back and believes the job done.
//
// It refuses the display-name form (`Ana <ana@example.com>`) that
// mail.ParseAddress otherwise accepts, because what is stored must be an address
// and nothing else: the extra text would travel into a mail header in #325 and
// into the Organization's export, and neither is a place for a string the buyer
// pasted out of their own address book.
func ParseHolderEmail(raw string) (string, bool) {
	email := platform.NormalizeEmail(raw)
	if email == "" || len(email) > MaxHolderEmailLength {
		return "", false
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil {
		return "", false
	}
	// ParseAddress accepts `Name <addr>` and reports the bare address; the two
	// agree only when the caller typed the bare address to begin with.
	if platform.NormalizeEmail(parsed.Address) != email {
		return "", false
	}
	return email, true
}
