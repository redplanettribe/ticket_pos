package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
)

// The Holder List (#333, rulings of 2026-08-22; formerly the Outstanding
// Answers surface of #313): every Ticket of an Event, each saying who is coming
// on it and — where the Event asks Ticket Questions — what it still owes.
//
// EVERY TICKET, NOT EVERY TICKET THAT OWES. The roster is the point and the
// questions are a column on it: a fully answered Ticket stays on the list, and
// an Event that asks no questions still has one, because "who is coming to my
// Event" is the headline value of Ticket Assignment and must not depend on
// anything having been asked. Outstanding Answers is a FILTER of this list —
// the owingOnly parameter — never its definition.
//
// THE DEBT IS STILL DERIVED, NEVER STORED. The rule is stated once in
// catalog.IsOutstandingAnswer and implemented once in SQL beside
// repository.outstandingAnswerWhere; nothing here re-decides it. This file's
// job is to page the roster and shape it for a screen.

// HolderListPage is one page of the Event's Holder List.
type HolderListPage struct {
	Data       []HolderTicketView    `json:"data"`
	Pagination OutstandingPagination `json:"pagination"`
	// OutstandingCount is how many Outstanding Answers the Event carries in ALL
	// — debts, not Tickets, so a Ticket owing three counts three. It is the
	// whole Event and never the page, because "how much don't I know yet" is a
	// question about the Event. It is unaffected by the owingOnly filter, for
	// the same reason.
	//
	// A POINTER, ABSENT WHILE TICKET_QUESTIONS_ENABLED IS CLOSED. This list is
	// readable on assignment alone (#333), and a build in that state must not
	// speak of debts a dark feature cannot define (ADR 0045).
	OutstandingCount *int `json:"outstanding_count,omitempty"`
}

// OutstandingPagination is the page metadata, in the shape every other paged
// staff list on this platform uses (ADR-0006).
type OutstandingPagination struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	// Total is how many Tickets the current view holds across the whole Event —
	// the roster, or the Tickets that owe when the filter is on. A page past
	// the last still reports it truthfully, so a surface can say how many there
	// are rather than appearing to have emptied.
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// HolderTicketView is one Ticket of the Event: its assignment, its buyer, and
// what it still owes.
type HolderTicketView struct {
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
	// THE HOLDER (#329, parent #322, ADR 0047). Who is coming on this Ticket.
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
	// retention purge has taken (migration 081) reads `assigned` here, with
	// NeverAccepted set beside it — see fillHolderListEntry.
	AssignmentState string `json:"assignment_state,omitempty"`
	// NeverAccepted marks a Ticket whose assignment the retention purge closed:
	// somebody was named, nobody ever accepted, and the address is gone by
	// definition (#334, migration 081).
	//
	// A PRESENTATION-LEVEL INDICATOR DERIVED AT READ TIME from
	// holder_address_purged_at — deliberately NOT a fourth value in
	// catalog.AssignmentState, which #331 rightly rejected. It exists because
	// after the Event starts every unaccepted assignment otherwise reads
	// `unassigned`, and the morning-after sheet could not distinguish "nobody
	// was named" from "named and never claimed". It discloses nothing personal.
	NeverAccepted bool `json:"never_accepted,omitempty"`
	// HolderFirstName, HolderLastName and HolderEmail are the person a Ticket was
	// handed to, and they are filled ONLY once that person has ACCEPTED.
	//
	// THE DISCLOSURE RULE IS DECIDED HERE AND NOWHERE ELSE — see
	// fillHolderListEntry, which is the one place to change if it is ever
	// revisited. The address is disclosed deliberately and at a stated cost (ADR
	// 0047): an Organizer needs a way to reach the people attending its Event,
	// and a name it cannot write to leaves it routing through buyers by hand,
	// which is the problem assignment was built to end.
	HolderFirstName string `json:"holder_first_name,omitempty"`
	HolderLastName  string `json:"holder_last_name,omitempty"`
	HolderEmail     string `json:"holder_email,omitempty"`
	// Outstanding names the required questions this Ticket has not answered, in
	// the order they are asked. Empty on a Ticket that owes nothing — which
	// since #333 is an ordinary row of this list, not an absent one.
	//
	// A POINTER, ABSENT WHILE TICKET_QUESTIONS_ENABLED IS CLOSED, for
	// OutstandingCount's reason — and a pointer to a slice rather than an
	// `omitempty` slice so that "owes nothing" still reads as `[]` on the wire:
	// a typed reader that has to check for null before iterating is a reader
	// that will one day forget.
	Outstanding *[]OutstandingQuestionView `json:"outstanding,omitempty"`
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

// ListHolderList returns a page of the Event's Holder List: every Ticket of
// every live Ticket Sale, oldest sale first, or — with owingOnly — only the
// Tickets that still owe required Answers.
//
// READABLE ON EITHER FLAG (#333). The list rides where the Outstanding Answers
// read always was, but it is the Holder List now, and an Organization that
// assigns tickets and asks nothing must still have one — so the gate opens on
// TICKET_ASSIGNMENT_ENABLED as well as TICKET_QUESTIONS_ENABLED, and only a
// build with both dark answers 404, exactly as a build without either feature
// would (ADR 0045).
//
// THE LIST'S DEBTS EMPTY BY THEMSELVES. Nothing here is invalidated or swept
// when an Answer arrives, because there is nothing to invalidate: what a Ticket
// owes is derived on every read, so an Answer written by Event Staff, by the
// checkout capture or through an Answer Link clears its row's debt on the next
// load, and a Sale Reversal removes all of that sale's rows at once.
func (s *Service) ListHolderList(
	ctx context.Context,
	actor ActorContext,
	eventID string,
	page, pageSize int,
	owingOnly bool,
) (*HolderListPage, error) {
	if err := s.holderListAvailable(ctx, actor, eventID); err != nil {
		return nil, err
	}
	// The FILTER belongs to the questions feature: with it dark there is no
	// debt to filter by, and a filtered read must not become a side channel
	// that admits the feature exists (ADR 0045).
	if owingOnly && !s.ticketQuestionsEnabled {
		owingOnly = false
	}

	tickets, total, err := s.repo.ListHolderTickets(
		ctx, actor.OrganizationID, eventID, owingOnly, pageSize, (page-1)*pageSize,
	)
	if err != nil {
		return nil, err
	}

	result := &HolderListPage{
		Data: make([]HolderTicketView, 0, len(tickets)),
		Pagination: OutstandingPagination{
			Page:       page,
			PageSize:   pageSize,
			Total:      total,
			TotalPages: totalPages(total, pageSize),
		},
	}

	if s.ticketQuestionsEnabled {
		outstandingCount, err := s.repo.CountOutstandingAnswers(ctx, actor.OrganizationID, eventID)
		if err != nil {
			return nil, err
		}
		result.OutstandingCount = &outstandingCount
	}

	// Bucketed by Ticket, preserving the query's order — which is the order the
	// questions are ASKED in, so the list reads the way the form does. Only
	// read at all while the questions feature is open; a dark feature owes
	// nobody anything.
	byTicket := map[string][]OutstandingQuestionView{}
	if s.ticketQuestionsEnabled && len(tickets) > 0 {
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
		for _, question := range questions {
			byTicket[question.TicketID] = append(byTicket[question.TicketID], OutstandingQuestionView{
				QuestionID: question.QuestionID,
				Label:      question.Label,
				Kind:       question.Kind,
				SortOrder:  question.SortOrder,
			})
		}
	}

	for _, ticket := range tickets {
		view := HolderTicketView{
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
		}
		if s.ticketQuestionsEnabled {
			outstanding := outstandingOrEmpty(byTicket[ticket.ID])
			view.Outstanding = &outstanding
		}
		s.fillHolderListEntry(&view, ticket)
		result.Data = append(result.Data, view)
	}
	return result, nil
}

// holderListAvailable is the Holder List's own gate: the read is available when
// EITHER Ticket Assignment or Ticket Questions is open (#333), and the Event
// must be the acting Organization's.
//
// The flag check comes FIRST, before the Event is looked at, for the reason
// ticketAnswersAvailable gives: a request while both features are dark must not
// be distinguishable from one against a build that never had them — same
// status, same code, and no read whose timing could differ between a real Event
// and an invented one (ADR 0045). The code stays TICKET_QUESTIONS_UNAVAILABLE,
// which is what this route has answered since #313; a dark build changing its
// refusal would itself be a tell.
func (s *Service) holderListAvailable(ctx context.Context, actor ActorContext, eventID string) error {
	if !s.ticketQuestionsEnabled && !s.ticketAssignmentEnabled {
		return catalog.ErrTicketQuestionsUnavailable()
	}
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return err
	}
	if event == nil {
		return catalog.ErrEventNotFound()
	}
	return nil
}

// fillHolderListEntry puts one Ticket's assignment onto the Organization's row,
// or leaves the row exactly as it was while the flag is closed (#329, ADR 0047).
//
// THE EARLY RETURN IS THE FLAG'S WHOLE EFFECT ON THIS READ, for the reason
// fillBuyerAssignment's is: every field it would otherwise set is `omitempty`, so
// a closed build sends the bytes a build without the feature sends and ADR 0045's
// "no surface differs" is an assertion a test can make about the body.
//
// THE STATE IS DERIVED THROUGH catalog.AssignmentState and never re-decided, so
// the word `assigned` cannot mean one thing on the buyer's page and another on
// the Organization's list — with ONE presentation-level exception, decided here
// (#334): a Ticket whose unaccepted address the retention purge took reads
// `assigned` with NeverAccepted beside it, derived at read time from migration
// 081's marker. catalog.AssignmentState itself still knows three states and
// still reads such a Ticket as `unassigned` everywhere else — on the buyer's
// page and in the export a purged Ticket is a Ticket nobody holds, which is the
// truth. The Holder List alone says more, because it is the morning-after sheet
// and "nobody was named" and "named and never claimed" are opposite facts to
// the person reading it. Nothing personal is disclosed: the address is gone by
// definition.
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
func (s *Service) fillHolderListEntry(view *HolderTicketView, ticket repository.HolderTicket) {
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
	if state != catalog.TicketAccepted && ticket.HolderAddressPurgedAt.Valid {
		view.AssignmentState = string(catalog.TicketAssigned)
		view.NeverAccepted = true
		return
	}
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
// already carry — a Ticket owing nothing simply owes nothing.
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
