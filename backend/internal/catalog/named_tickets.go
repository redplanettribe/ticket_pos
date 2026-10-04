package catalog

import (
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
)

// Named Tickets (ADR 0076, #669): on an Event that requires them, a Ticket is
// complete when it names its Holder and answers every required Ticket Question
// of its Ticket Type, and a Storefront checkout is refused until every Ticket in
// the basket is complete.
//
// THE RULE IS PURE AND LIVES IN THE DOMAIN PACKAGE, beside
// HoldableCheckoutAnswers, because two callers ask it: begin-checkout of a whole
// basket (#669), and the buyer's reassignment of one Ticket (#673). Both refuse
// with the same domain error, built from what this file reports, so a Storefront
// that points a buyer at what is owed reads one shape from either.

// NamedTicketsApply reports whether an Event's Named Tickets requirement binds
// right now: the Event requires them, Ticket Assignment is open in this
// deployment, and the Event has not started.
//
// THE START IS AN INSTANT, so "read in the Event's timezone" needs no
// conversion: `starts_at` is stored as one, and the timezone only says how to
// print it. eventStartsAt is nil for an Event that has not said when it
// starts, which has not started - the reading AssignmentWindow makes.
//
// It falls silent at the doors because that is when an assignment can no
// longer be accepted nor an Answer changed: a requirement asking for either
// after that would gate a purchase on something nobody can act on.
func NamedTicketsApply(requiresNamedTickets, ticketAssignmentEnabled bool, eventStartsAt *time.Time, now time.Time) bool {
	if !requiresNamedTickets || !ticketAssignmentEnabled {
		return false
	}
	return eventStartsAt == nil || now.Before(*eventStartsAt)
}

// NamedTicket is one Ticket the requirement is judged on, before or after it
// exists: which Ticket Type, which of that Ticket Type's Tickets (1..quantity,
// the number that becomes `tickets.ordinal`), and the Holder named for it.
type NamedTicket struct {
	TicketTypeID string
	TicketIndex  int
	// SelfHeld is the buyer's own Ticket, which names no Holder: the buyer is
	// it (ADR 0048). It still owes its Answers.
	SelfHeld bool
	// HolderEmail is the address named for it, already through
	// ParseHolderEmail, or "" when none was given.
	HolderEmail string
}

// OwedTicket is one Ticket the requirement finds incomplete, and exactly what
// it still owes. It is the refusal's details, verbatim, so its JSON shape is the
// contract the Storefront reads.
type OwedTicket struct {
	TicketTypeID string `json:"ticket_type_id"`
	// TicketIndex is the Ticket's place in its Ticket Type's run, 1..quantity,
	// as the checkout body numbers it.
	TicketIndex int `json:"ticket_index"`
	// HolderEmailMissing is true when the Ticket owes a Holder's address.
	// Never true on the buyer's own Ticket.
	HolderEmailMissing bool `json:"holder_email_missing"`
	// MissingQuestionIDs are the required questions this Ticket has no usable
	// Answer to, in the order they are asked. Always an array, empty when only
	// the address is owed.
	MissingQuestionIDs []string `json:"missing_question_ids"`
}

// OwedByNamedTickets judges every Ticket against the requirement and returns
// the incomplete ones, in the order the Tickets were given. Empty means the
// basket may be bought.
//
// `asked` is every question the Tickets' Ticket Types put to a buyer now -
// CheckoutQuestionsSQL's answer, so approved, unretired and asked at checkout,
// and nothing at all while Ticket Questions are dark. A question awaiting
// review is therefore never owed, because it is not asked.
//
// `answered` is what HoldableCheckoutAnswers kept, and that is how "an invalid
// Answer to a required question counts as missing" holds: the reply that did
// not parse, named an Option the question does not offer, or named an index the
// basket does not hold was already dropped there, so it is simply absent here.
// An optional question is never owed, whether it was answered or not.
func OwedByNamedTickets(tickets []NamedTicket, asked []AskedQuestion, answered []HeldAnswer) []OwedTicket {
	type slot struct {
		ticketTypeID string
		ticketIndex  int
		questionID   string
	}
	given := make(map[slot]bool, len(answered))
	for _, answer := range answered {
		given[slot{answer.TicketTypeID, answer.TicketIndex, answer.TicketQuestionID}] = true
	}

	var owed []OwedTicket
	for _, ticket := range tickets {
		missing := []string{}
		for _, question := range asked {
			if !question.Required || question.TicketTypeID != ticket.TicketTypeID {
				continue
			}
			if !given[slot{ticket.TicketTypeID, ticket.TicketIndex, question.ID}] {
				missing = append(missing, question.ID)
			}
		}
		holderMissing := !ticket.SelfHeld && ticket.HolderEmail == ""
		if !holderMissing && len(missing) == 0 {
			continue
		}
		owed = append(owed, OwedTicket{
			TicketTypeID:       ticket.TicketTypeID,
			TicketIndex:        ticket.TicketIndex,
			HolderEmailMissing: holderMissing,
			MissingQuestionIDs: missing,
		})
	}
	return owed
}

// NamedTicketsIncomplete is the refusal's details: every Ticket that still owes
// something, under one key so the shape can grow without breaking a reader.
type NamedTicketsIncomplete struct {
	Tickets []OwedTicket `json:"tickets"`
}

// ErrNamedTicketsIncomplete refuses an act on an Event that requires Named
// Tickets because some Ticket still owes a Holder's address or a required
// Answer (ADR 0076). Its details name each such Ticket and what it owes, so the
// Storefront can point the buyer at the very fields.
//
// ONE CODE FOR EVERY ROUTE THE RULE BINDS: begin-checkout (#669) and the
// buyer's reassignment (#673). A 400 like POLICY_ACCEPTANCE_REQUIRED, because
// re-sending the request with the missing fields filled in is exactly what fixes
// it - and not a field error, because what is owed is decided against the
// Event's questions and the basket's seating, which no body shape check knows.
func ErrNamedTicketsIncomplete(owed []OwedTicket) apperror.DomainError {
	return apperror.New(
		"NAMED_TICKETS_INCOMPLETE",
		"Every ticket must name who it is for and answer its required questions.",
		NamedTicketsIncomplete{Tickets: owed},
	)
}
