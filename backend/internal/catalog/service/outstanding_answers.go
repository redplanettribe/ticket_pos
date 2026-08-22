package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
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
//
// AND IT IS THE GUEST LIST (#329, parent #322, ADR 0047). The same read, widened
// rather than duplicated: it already walks the Event's Tickets, and an Organizer
// asking "who is coming" and an Organizer asking "who has not told me their size"
// are one person looking at one list. A second staff endpoint over the same rows
// would be the same query twice, disagreeing eventually.

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
	// The buyer: the party of record for the Sale, and the person to chase for
	// any Ticket nobody has accepted. They are no longer the only one — an
	// accepted Ticket names its Holder below — but they remain here on every row,
	// because a Holder is the named person a Ticket was handed to and never its
	// owner, and the Sale stays whole with the buyer either way.
	//
	// The two name parts stay APART, as they are on the Sales list and in the
	// column they are read from. Joining them here would mean choosing an order
	// for them, and which part leads a person's name is the reader's question and
	// not this payload's.
	CustomerFirstName string    `json:"customer_first_name"`
	CustomerLastName  string    `json:"customer_last_name"`
	CustomerEmail     string    `json:"customer_email"`
	SoldAt            time.Time `json:"sold_at"`
	// THE GUEST LIST (#329, parent #322, ADR 0047). Who is coming, beside what
	// they still owe. This is the answer to "who is in the room", which this
	// Organization could previously give only as the buyer's name repeated once
	// per Ticket — and it is on THIS payload rather than a second endpoint's
	// because one list of Tickets is what an Organizer came to read.
	//
	// EVERY FIELD IS `omitempty`, AND THAT IS THE FLAG'S DOING, exactly as it is
	// on the buyer's row. With TICKET_ASSIGNMENT_ENABLED closed the service fills
	// none of them and this payload is byte-identical to the one a build without
	// the feature sends (ADR 0045).

	// AssignmentState is `unassigned`, `assigned` or `accepted`, derived by
	// catalog.AssignmentState and never stored.
	//
	// IT IS THE FIELD THAT MAKES THE REST READABLE, and the reason it is on the
	// wire at all: a name arrives only with acceptance, so without the state an
	// `assigned` Ticket whose Holder never clicked would be indistinguishable
	// from one nobody was ever named for — and those are opposite facts to an
	// Organizer deciding whether to chase.
	//
	// THREE VALUES AND NEVER FOUR. A Ticket whose unaccepted address the
	// retention purge has taken (migration 081) reads `unassigned` here, like
	// every other surface: nobody holds it, which is the truth. What happened to
	// it is a fact for the platform's records, not a state of the assignment.
	AssignmentState string `json:"assignment_state,omitempty"`
	// HolderFirstName, HolderLastName and HolderEmail are the person a Ticket was
	// handed to, and they are filled ONLY once that person has ACCEPTED.
	//
	// THE DISCLOSURE RULE IS DECIDED HERE AND NOWHERE ELSE — see
	// fillGuestListEntry, which is the one place to change if it is ever
	// revisited. The address is disclosed deliberately and at a stated cost (ADR
	// 0047): an Organizer needs a way to reach the people attending its Event,
	// and a name it cannot write to leaves it routing through buyers by hand,
	// which is the problem assignment was built to end.
	HolderFirstName string `json:"holder_first_name,omitempty"`
	HolderLastName  string `json:"holder_last_name,omitempty"`
	HolderEmail     string `json:"holder_email,omitempty"`
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
		view := TicketOwingAnswersView{
			TicketID:          ticket.ID,
			Ordinal:           ticket.Ordinal,
			TicketTypeID:      ticket.TicketTypeID,
			TicketTypeName:    ticket.TicketTypeName,
			TicketSaleID:      ticket.TicketSaleID,
			ConfirmationRef:   ticket.ConfirmationRef,
			Channel:           ticket.Channel,
			CustomerFirstName: ticket.CustomerFirstName,
			CustomerLastName:  ticket.CustomerLastName,
			CustomerEmail:     ticket.CustomerEmail,
			SoldAt:            ticket.SoldAt,
			Outstanding:       outstandingOrEmpty(byTicket[ticket.ID]),
		}
		s.fillGuestListEntry(&view, ticket)
		result.Data = append(result.Data, view)
	}
	return result, nil
}

// fillGuestListEntry puts one Ticket's Holder onto the Organization's row, or
// leaves the row exactly as it was while the flag is closed (#329, ADR 0047).
//
// THE EARLY RETURN IS THE FLAG'S WHOLE EFFECT ON THIS READ, for the reason
// fillBuyerAssignment's is: every field it would otherwise set is `omitempty`, so
// a closed build sends the bytes a build without the feature sends and ADR 0045's
// "no surface differs" is an assertion a test can make about the body.
//
// THE STATE IS DERIVED THROUGH catalog.AssignmentState and never re-decided, so
// the word `assigned` cannot mean one thing on the buyer's page and another on
// the Organization's list.
//
// AND THIS IS WHERE THE DISCLOSURE LINE IS DRAWN. Nothing about the Holder is
// filled until AcceptedAt, and the ONE test is the state. An address a buyer
// typed and its owner never accepted has no consent moment behind it at all —
// the person may not know a ticket was bought for them — and ADR 0047 rejects
// disclosing it outright, in the same breath as it accepts disclosing an accepted
// one. The Organization is told that such a Ticket is `assigned`, and not who it
// was assigned to.
//
// NOTE THE ASYMMETRY WITH THE BUYER'S ROW, which shows the address from the
// moment it is typed. It is the same address and two different readers: the buyer
// typed it and is telling their four Tickets apart, and the Organization is being
// handed a stranger's contact detail.
func (s *Service) fillGuestListEntry(view *TicketOwingAnswersView, ticket repository.TicketOwingAnswers) {
	if !s.ticketAssignmentEnabled {
		return
	}

	holderEmail := ""
	if ticket.HolderEmail.Valid {
		holderEmail = ticket.HolderEmail.String
	}
	state := catalog.AssignmentState(
		holderEmail, nullTimeOrNil(ticket.AssignedAt), nullTimeOrNil(ticket.AcceptedAt),
	)
	view.AssignmentState = string(state)
	if state != catalog.TicketAccepted {
		return
	}
	view.HolderFirstName = ticket.HolderFirstName.String
	view.HolderLastName = ticket.HolderLastName.String
	view.HolderEmail = holderEmail
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
