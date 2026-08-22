package service

import (
	"context"
	"time"
)

// The Outstanding Answers surface (#313): which of an Event's Tickets still owe
// required Answers, and which questions they owe.
//
// WHAT THIS IS FOR. An Outstanding Answer is a debt, not a defect — the whole
// meaning of "required" on this platform. Nothing was ever refused for want of
// one, on any channel, so the only thing an Organization can do about a missing
// size is see that it is missing and chase it before it orders the shirts. This
// is that screen.
//
// THE DEBT IS DERIVED, NEVER STORED. The rule is stated once in
// catalog.IsOutstandingAnswer and implemented once in SQL in
// repository.outstandingAnswerWhere; nothing here re-decides it. This file's job
// is to page the result and shape it for a screen.

// OutstandingAnswersPage is one page of an Event's Tickets that owe Answers,
// with the two counts a reader needs to make sense of it.
type OutstandingAnswersPage struct {
	Data       []TicketOwingAnswersView `json:"data"`
	Pagination OutstandingPagination    `json:"pagination"`
	// OutstandingCount is how many Outstanding Answers the Event carries in ALL
	// — debts, not Tickets, so a Ticket owing three counts three. It is the
	// whole Event and never the page, because "how much don't I know yet" is a
	// question about the Event.
	//
	// It is a SECOND number beside Pagination.Total on purpose: the two answer
	// different questions — "nine Tickets are waiting on me" and "twenty-two
	// things are unknown" — and a surface with only one of them either
	// understates the work or overstates the number of people to write to.
	OutstandingCount int `json:"outstanding_count"`
}

// OutstandingPagination is the page metadata, in the shape every other paged
// staff list on this platform uses (ADR-0006).
type OutstandingPagination struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	// Total is how many TICKETS owe something, across the whole Event. A page
	// past the last still reports it truthfully, so a surface can say how many
	// there are rather than appearing to have emptied.
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// TicketOwingAnswersView is one Ticket that owes, and everything needed to chase
// it or to open it.
type TicketOwingAnswersView struct {
	TicketID string `json:"ticket_id"`
	// Ordinal is which of its Ticket Sale Line's units this Ticket is,
	// 1..quantity. Internal and not a seat number, but the only thing telling
	// two Tickets of one line apart — which is what lets staff say "the second
	// of Ana's four".
	Ordinal        int    `json:"ordinal"`
	TicketTypeID   string `json:"ticket_type_id"`
	TicketTypeName string `json:"ticket_type_name"`
	// TicketSaleID and ConfirmationRef are how this row is ACTED ON. The staff
	// Answers dialog is keyed on a Ticket Sale and names itself after the
	// buyer's reference, so a row carrying neither would be a complaint nobody
	// could act on. This is the jump from the list to answering the Ticket.
	TicketSaleID    string `json:"ticket_sale_id"`
	ConfirmationRef string `json:"confirmation_ref"`
	// Channel is 'online', 'in_person' or 'import', and it EXPLAINS the row
	// rather than filtering it. A door sale and a Sale Import start out owing
	// every question because nobody ever put the questions to those buyers —
	// there is no checkout form on either. They stand here beside the online
	// ones, and the channel is what stops that reading as lost data.
	Channel string `json:"channel"`
	// CustomerName and CustomerEmail are the buyer, who is the only person there
	// is to chase: the platform holds no address for a Ticket's holder and does
	// not ask for one, so a question added after a sale reaches its holder only
	// if the buyer forwards it.
	CustomerName  string    `json:"customer_name"`
	CustomerEmail string    `json:"customer_email"`
	SoldAt        time.Time `json:"sold_at"`
	// Outstanding names the required questions this Ticket has not answered, in
	// the order they are asked. Never empty: a Ticket with nothing outstanding
	// is not on this list at all.
	Outstanding []OutstandingQuestionView `json:"outstanding"`
}

// OutstandingQuestionView is one Outstanding Answer: a required Ticket Question
// this Ticket has not answered.
//
// It carries NO Answer field, and that absence is the point — there is no Answer,
// which is the entire fact being reported.
type OutstandingQuestionView struct {
	QuestionID string `json:"question_id"`
	// Label is the Organization's own words, read AS COINED in every Locale (ADR
	// 0027). Only the chrome around it follows the reader's Staff Locale.
	Label string `json:"label"`
	// Kind is the shape the Answer will take when it arrives, so the list can
	// show what is being asked for without a second read of the question.
	Kind      string `json:"kind"`
	SortOrder int    `json:"sort_order"`
}

// ListOutstandingAnswers returns a page of the Event's Tickets that still owe
// required Answers, oldest sale first.
//
// It goes through ticketAnswersAvailable, so the feature flag is read FIRST and
// this surface 404s while the feature is dark exactly as every other Answer
// route does — same status, same code, and no read whose timing could tell a
// real Event from an invented one (ADR 0045).
//
// THE LIST EMPTIES BY ITSELF. Nothing here is invalidated or swept when an
// Answer arrives, because there is nothing to invalidate: the debt is derived on
// every read, so an Answer written by Event Staff, by the checkout capture or
// through an Answer Link removes its row on the next load, and a Sale Reversal
// removes all of that sale's rows at once. That is the whole reason this is not
// a stored list.
func (s *Service) ListOutstandingAnswers(
	ctx context.Context,
	actor ActorContext,
	eventID string,
	page, pageSize int,
) (*OutstandingAnswersPage, error) {
	if err := s.ticketAnswersAvailable(ctx, actor, eventID); err != nil {
		return nil, err
	}

	tickets, total, err := s.repo.ListTicketsOwingAnswers(
		ctx, actor.OrganizationID, eventID, pageSize, (page-1)*pageSize,
	)
	if err != nil {
		return nil, err
	}

	outstandingCount, err := s.repo.CountOutstandingAnswers(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}

	result := &OutstandingAnswersPage{
		Data: make([]TicketOwingAnswersView, 0, len(tickets)),
		Pagination: OutstandingPagination{
			Page:       page,
			PageSize:   pageSize,
			Total:      total,
			TotalPages: totalPages(total, pageSize),
		},
		OutstandingCount: outstandingCount,
	}
	if len(tickets) == 0 {
		return result, nil
	}

	ticketIDs := make([]string, 0, len(tickets))
	for _, ticket := range tickets {
		ticketIDs = append(ticketIDs, ticket.ID)
	}
	questions, err := s.repo.ListOutstandingQuestionsForTickets(
		ctx, actor.OrganizationID, eventID, ticketIDs,
	)
	if err != nil {
		return nil, err
	}

	// Bucketed by Ticket, preserving the query's order — which is the order the
	// questions are ASKED in, so the list reads the way the form does.
	byTicket := make(map[string][]OutstandingQuestionView, len(tickets))
	for _, question := range questions {
		byTicket[question.TicketID] = append(byTicket[question.TicketID], OutstandingQuestionView{
			QuestionID: question.QuestionID,
			Label:      question.Label,
			Kind:       question.Kind,
			SortOrder:  question.SortOrder,
		})
	}

	for _, ticket := range tickets {
		result.Data = append(result.Data, TicketOwingAnswersView{
			TicketID:        ticket.ID,
			Ordinal:         ticket.Ordinal,
			TicketTypeID:    ticket.TicketTypeID,
			TicketTypeName:  ticket.TicketTypeName,
			TicketSaleID:    ticket.TicketSaleID,
			ConfirmationRef: ticket.ConfirmationRef,
			Channel:         ticket.Channel,
			CustomerName:    ticket.CustomerName,
			CustomerEmail:   ticket.CustomerEmail,
			SoldAt:          ticket.SoldAt,
			Outstanding:     outstandingOrEmpty(byTicket[ticket.ID]),
		})
	}
	return result, nil
}

// outstandingOrEmpty keeps the field an ARRAY on the wire rather than null. A
// typed reader that has to check for null before iterating is a reader that will
// one day forget, and there is no meaning here for null that empty does not
// already carry — a Ticket owing nothing is simply not in this list.
func outstandingOrEmpty(questions []OutstandingQuestionView) []OutstandingQuestionView {
	if questions == nil {
		return []OutstandingQuestionView{}
	}
	return questions
}

// totalPages is the page count for a total, and 0 for an empty list — never 1,
// because "page 1 of 1" over nothing invites a reader to go looking for a page
// that has nothing on it.
func totalPages(total, pageSize int) int {
	if total <= 0 || pageSize <= 0 {
		return 0
	}
	return (total + pageSize - 1) / pageSize
}
