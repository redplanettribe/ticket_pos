package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
)

// The held-ticket surface (#343, parent #342, ADR 0049): a Customer lists the
// Tickets they HOLD and answers one Ticket Question on one of them.
//
// "HELD" IS THE ONLY AUTHORISATION CONCEPT. A buyer holds the Self-held Ticket
// of their purchase (ADR 0048); a Holder holds the Ticket they accepted by
// Assignment Link (ADR 0046); to this surface the two are one thing, keyed on
// the holder customer id of the Ticket row and independent of the Sale. The
// buyer's own sale page and a Holder's Customer Area draw one panel from it,
// which is what ADR 0049 means by "only the Holder answers": there is no
// second route through which a buyer could answer for a Ticket they gave away.
//
// A TICKET THE CALLER DOES NOT HOLD IS NOT FOUND, indistinguishably from one
// that does not exist — a Ticket on the buyer's own Sale that somebody else
// accepted included. The alternative, a 403 saying "not yours", would tell a
// caller probing ids which guesses were real.
//
// THE DISCLOSURE RULE IS THE ASSIGNMENT LINK'S (CONTEXT.md, Holder): a Holder
// sees the Event, the Ticket Type and their own questions, never the buyer,
// the price, the Tax ID, the Sale Confirmation reference or the Sale's other
// Tickets. And because the buyer's row is the same shape, the buyer's row
// carries none of that either — the Sale-level facts are on the sale-scoped
// read, where the credential proves ownership of the Sale.

// HeldTicketAnswersView is one Ticket as its Holder sees it.
//
// ITS OWN TYPE, and deliberately not BuyerTicketAnswersView narrowed: that one
// carries an Answer Link, the Holder's address and the assignment state, every
// one of which is a fact about the SALE that the buyer alone is entitled to. A
// shared struct would be a field added for the buyer appearing on a stranger's
// payload the day it was added.
type HeldTicketAnswersView struct {
	// TicketID names which Ticket this is, so the panel can write back against
	// it and the buyer's sale page can match it to its row. Safe: the reader
	// holds it.
	TicketID string `json:"ticket_id"`
	// EventName and EventSlug are the two public facts about the Event — both
	// already readable on the Storefront — that let the panel say which Event
	// this Ticket is for and link there.
	EventName string `json:"event_name"`
	EventSlug string `json:"event_slug"`
	// TicketTypeName is the only thing telling two held Tickets on one Event
	// apart that this reader is entitled to. NOT the ordinal: "2 of 4" is a
	// fact about the Sale's other Tickets.
	TicketTypeName string `json:"ticket_type_name"`
	// Answerable and AnswerableRefusal are the write window, exactly as on the
	// staff and buyer views: false once the Event has started and on a
	// reversed Sale, with the refusal as a token the Storefront translates.
	// The READ is never gated by it.
	Answerable        bool   `json:"answerable"`
	AnswerableRefusal string `json:"answerable_refusal"`
	// OutstandingCount is how many required questions this Ticket has not yet
	// answered — what keeps the panel open (ADR 0049).
	OutstandingCount int `json:"outstanding_count"`
	// Questions carries the Ticket Type's questions in the order they are
	// asked, retired ones last, each with this Ticket's Answer or null.
	Questions []TicketQuestionAnswerView `json:"questions"`
}

// ListHeldTickets returns every Ticket the session's Customer holds.
//
// customerID and sessionTicketSaleID both come from the Customer Session the
// middleware validated. sessionTicketSaleID is empty on a full session and
// names one Sale on a Confirmation Link session, which is treated as the buyer
// for that Sale's Self-held Ticket and nothing beyond it: the narrowing can only
// ever NARROW, the same rule every other customer read is held to.
func (s *Service) ListHeldTickets(ctx context.Context, customerID, sessionTicketSaleID string) ([]HeldTicketAnswersView, error) {
	// The flag first, before anything is read, so that a dark build answers
	// exactly as a build that never had the feature (ADR 0045).
	if !s.ticketQuestionsEnabled {
		return nil, catalog.ErrTicketQuestionsUnavailable()
	}
	tickets, err := s.heldTickets(ctx, customerID, sessionTicketSaleID)
	if err != nil {
		return nil, err
	}
	return s.heldTicketViews(ctx, tickets)
}

// AnswerHeldTicketQuestion writes one Answer on one Ticket the session's
// Customer holds, and returns that Ticket.
//
// It resolves the Ticket through the SAME scoped read the listing uses, so a
// Ticket the caller does not hold is not found rather than refused, and then
// goes through answerTicketQuestion — the one body every route into an Answer
// passes through. The edit window is checked on the write and never on the
// read, exactly as it is for Event Staff and the buyer.
//
// ONE TICKET COMES BACK, not the list: each held Ticket's panel stands alone,
// with its own outstanding count, and nothing on another Ticket moves when this
// one is answered.
func (s *Service) AnswerHeldTicketQuestion(
	ctx context.Context,
	customerID, sessionTicketSaleID, ticketID, questionID string,
	input AnswerInput,
) (*HeldTicketAnswersView, error) {
	if !s.ticketQuestionsEnabled {
		return nil, catalog.ErrTicketQuestionsUnavailable()
	}
	tickets, err := s.heldTickets(ctx, customerID, sessionTicketSaleID)
	if err != nil {
		return nil, err
	}
	var ticket *repository.HeldTicket
	for i := range tickets {
		if tickets[i].ID == ticketID {
			ticket = &tickets[i]
			break
		}
	}
	if ticket == nil {
		// Not held by this Customer, or not a Ticket at all — one answer for
		// both, so that trying ids here teaches nothing.
		return nil, catalog.ErrTicketNotFound()
	}
	if err := s.answerWindowOpen(&ticket.AnswerableTicket); err != nil {
		return nil, err
	}
	if err := s.answerTicketQuestion(ctx, ticket.ID, ticket.TicketTypeID, questionID, input); err != nil {
		return nil, err
	}
	views, err := s.heldTicketViews(ctx, []repository.HeldTicket{*ticket})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

// heldTickets is the one scoped read behind both routes, with the Confirmation
// Link session's narrowing applied.
func (s *Service) heldTickets(ctx context.Context, customerID, sessionTicketSaleID string) ([]repository.HeldTicket, error) {
	tickets, err := s.repo.ListHeldTicketsForCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	if sessionTicketSaleID == "" {
		return tickets, nil
	}
	narrowed := make([]repository.HeldTicket, 0, 1)
	for _, ticket := range tickets {
		if ticket.TicketSaleID == sessionTicketSaleID {
			narrowed = append(narrowed, ticket)
		}
	}
	return narrowed, nil
}

// heldTicketViews builds the Holder's rows from the staff view and COPIES
// FIELD BY FIELD. It does not embed TicketAnswersView, for the reason the buyer
// view does not: anything added to the staff view would appear here the day it
// was added, unreviewed, on a payload whose reader is not the party of record.
func (s *Service) heldTicketViews(ctx context.Context, tickets []repository.HeldTicket) ([]HeldTicketAnswersView, error) {
	answerable := make([]repository.AnswerableTicket, 0, len(tickets))
	for _, ticket := range tickets {
		answerable = append(answerable, ticket.AnswerableTicket)
	}
	staffViews, err := s.ticketAnswersViews(ctx, answerable)
	if err != nil {
		return nil, err
	}
	views := make([]HeldTicketAnswersView, 0, len(staffViews))
	for i, staff := range staffViews {
		views = append(views, HeldTicketAnswersView{
			TicketID:          staff.TicketID,
			EventName:         tickets[i].EventName,
			EventSlug:         tickets[i].EventSlug,
			TicketTypeName:    staff.TicketTypeName,
			Answerable:        staff.Answerable,
			AnswerableRefusal: staff.AnswerableRefusal,
			OutstandingCount:  outstandingAnswerCount(tickets[i].SaleStatus, staff.Questions),
			Questions:         staff.Questions,
		})
	}
	return views, nil
}
