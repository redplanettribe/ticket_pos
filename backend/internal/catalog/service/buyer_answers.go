package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
)

// The buyer's own surface on the Tickets of their Sale (#315, ADR 0044; narrowed
// by #344, ADR 0049): the page behind the Confirmation Link, and the Customer
// Area for a signed-in Customer.
//
// THESE ARE ONE SURFACE AND NOT TWO. The Confirmation Link does not open a page
// of its own — it redeems into a Customer Session narrowed to one Ticket Sale
// and lands on the Customer Area, which is a single page listing every purchase
// the session can reach. So "the Confirmation Link page" and "the Customer Area"
// differ only in how many Sales the session may see, and both arrive here.
//
// WHAT THIS SURFACE IS FOR, since ADR 0049, is assignment and nothing else.
// An Answer is given only by a Ticket's Holder — the buyer for their Self-held
// Ticket, through the held-ticket routes (held_ticket_answers.go) — or by Event
// Staff. Of a Ticket the buyer does not hold they see its position, its Ticket
// Type and its assignment state, and NOT its questions, its Answers, what it
// still owes or any link that would open it. So this payload carries no
// Answer, no outstanding count and no Answer Link for ANY row, the Self-held
// one included: the buyer's own questions are read and written through the
// held-ticket routes, which is the one door every Holder uses, so there is no
// second copy of "may this person answer this Ticket" to drift.
//
// THE PREVIOUS SHAPE IS WORTH REMEMBERING, because the tests guard against it
// returning: this payload used to carry every Ticket's Answers and a per-Ticket
// Answer Link, an unauthenticated write credential over somebody else's Ticket.
// The assertion that no `answer_link`, no `questions` and no
// `outstanding_count` leave this route is in integration/buyer_answers_test.go.

// BuyerTicketAnswersView is one Ticket of the buyer's own Ticket Sale: which
// one it is, and whose it is.
//
// IT IS ITS OWN TYPE and neither TicketAnswersView nor HeldTicketAnswersView,
// for the same reason those two are separate from each other: each has a
// different reader, and a shared struct is a field added for one of them
// appearing on the other two. The name keeps the "Answers" it no longer carries
// because the route, the OpenAPI schema and the Storefront's type all spell it;
// a rename would be a diff about nothing.
type BuyerTicketAnswersView struct {
	// TicketID names which Ticket this is, so the buyer's page can post an
	// address back against it and match it to the held-ticket row when the
	// Ticket is their own. Safe: this reader proved they own the Sale.
	TicketID string `json:"ticket_id"`
	// Ordinal is which of its line's units this is, 1..quantity. It is what lets
	// the page say "ticket 2 of 4" — the only thing telling two Tickets of one
	// line apart until an address is given.
	Ordinal        int    `json:"ordinal"`
	TicketTypeName string `json:"ticket_type_name"`
	// THE TICKET ASSIGNMENT (#324, parent #322). The fields that answer the
	// buyer's question "which of my four Tickets is which".
	//
	// EVERY ONE OF THEM IS `omitempty`, AND THAT IS THE FLAG'S DOING. With
	// TICKET_ASSIGNMENT_ENABLED closed the service fills none of them, so this
	// payload is byte-identical to the one a build without the feature sends.

	// AssignmentState is `unassigned`, `assigned` or `accepted`, derived by
	// catalog.AssignmentState and never stored. Absent while the flag is closed.
	AssignmentState string `json:"assignment_state,omitempty"`
	// HolderEmail is the address this Ticket was assigned to, shown back to the
	// buyer who typed it. Empty while unassigned.
	//
	// SHOWN TO THE BUYER AND TO NOBODY ELSE ON THIS SURFACE. It is on the payload
	// because the buyer typed it and telling their four Tickets apart is the
	// whole point of the feature; it is on no public payload, where a third
	// party's address would be a disclosure.
	HolderEmail string `json:"holder_email,omitempty"`
	// AssignedAt is when this address was named, and AcceptedAt when the Holder
	// clicked. Both nil when they have not happened.
	AssignedAt *time.Time `json:"assigned_at,omitempty"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
	// SelfHeld is whether this Ticket's Holder is the buyer themself — the one
	// Ticket an Online Sale hands the buyer at purchase (ADR 0048), or any
	// Ticket they later assigned to their own address and accepted. The
	// Storefront says "your ticket" on it and draws its questions from the
	// held-ticket list; every other Ticket is its Holder's to answer. Absent
	// while the assignment flag is closed, like the rest of this block.
	SelfHeld bool `json:"self_held,omitempty"`
	// Assignable is whether this Ticket may be assigned or reassigned right now,
	// and AssignableRefusal names why not — a token and never a sentence,
	// because the Storefront owns the words in the reader's language.
	Assignable        bool   `json:"assignable,omitempty"`
	AssignableRefusal string `json:"assignable_refusal,omitempty"`
}

// ListBuyerTicketAnswers returns every Ticket of one of the buyer's own Ticket
// Sales with its assignment state.
//
// customerID and sessionTicketSaleID both come from the Customer Session the
// middleware validated, and NEITHER comes from the request. sessionTicketSaleID
// is empty on a full Customer Session and names one Sale on a Confirmation Link
// session; it can only ever NARROW, which is the same rule
// repository.ListTicketSalesForCustomer is held to and is what makes a forwarded
// receipt reach exactly the one purchase it was a receipt for.
//
// A Sale this session may not see resolves to an empty list rather than to a
// refusal, and that is deliberate: "you do not own this" and "this does not
// exist" must be one answer, or the endpoint becomes a way to learn which Sale
// ids are real by watching which of them refuse differently.
//
// STILL GATED ON TICKET_QUESTIONS_ENABLED, although it no longer carries a
// question: the route was born under that flag, the Storefront reads a 404 here
// as "no section", and unpicking the gate is a decision about the assignment
// feature's own flag that this narrowing does not make.
func (s *Service) ListBuyerTicketAnswers(
	ctx context.Context,
	customerID, sessionTicketSaleID, ticketSaleID string,
) ([]BuyerTicketAnswersView, error) {
	if !s.ticketQuestionsEnabled {
		return nil, catalog.ErrTicketQuestionsUnavailable()
	}
	if sessionTicketSaleID != "" && sessionTicketSaleID != ticketSaleID {
		return []BuyerTicketAnswersView{}, nil
	}

	tickets, err := s.repo.ListAnswerableTicketsForBuyer(ctx, customerID, ticketSaleID)
	if err != nil {
		return nil, err
	}
	return s.buyerTicketAnswersViews(customerID, tickets), nil
}

// buyerTicketAnswersViews assembles the buyer's rows from the scoped read.
//
// NO QUESTIONS ARE READ AND NO ANSWERS ARE LOADED, on purpose and not as an
// optimisation: a row that never carried an Answer cannot leak one. Anything the
// buyer may answer is on the held-ticket routes, behind the one "is this
// Ticket held by the caller" check.
func (s *Service) buyerTicketAnswersViews(
	customerID string,
	tickets []repository.AnswerableTicket,
) []BuyerTicketAnswersView {
	views := make([]BuyerTicketAnswersView, 0, len(tickets))
	for _, ticket := range tickets {
		view := BuyerTicketAnswersView{
			TicketID:       ticket.ID,
			Ordinal:        ticket.Ordinal,
			TicketTypeName: ticket.TicketTypeName,
		}
		s.fillBuyerAssignment(&view, customerID, ticket)
		views = append(views, view)
	}
	return views
}

// TicketSaleHasOutstandingAnswers is whether any Ticket of one Sale still owes a
// required Answer — the Sale Confirmation's one conditional sentence. False,
// not an error, while the feature is dark: a receipt is sent either way.
func (s *Service) TicketSaleHasOutstandingAnswers(ctx context.Context, ticketSaleID string) (bool, error) {
	if !s.ticketQuestionsEnabled {
		return false, nil
	}
	return s.repo.TicketSaleHasOutstandingAnswers(ctx, ticketSaleID)
}

// outstandingAnswerCount counts one Ticket's Outstanding Answers from the
// platform's ONE definition of the debt (catalog.IsOutstandingAnswer), so what a
// Holder is told they owe and what the Organization's chase list shows can
// never be two different numbers.
func outstandingAnswerCount(saleStatus string, questions []TicketQuestionAnswerView) int {
	count := 0
	for _, pair := range questions {
		if catalog.IsOutstandingAnswer(catalog.OutstandingAnswerInputs{
			Required:        pair.Question.Required,
			QuestionRetired: pair.Question.Retired,
			SaleStatus:      saleStatus,
			Answered:        pair.Answer != nil,
		}) {
			count++
		}
	}
	return count
}
