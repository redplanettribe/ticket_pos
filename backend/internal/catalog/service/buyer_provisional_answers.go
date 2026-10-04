package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
)

// The buyer's provisional Answers (#672, spec #665, ADR 0076).
//
// On an Event that requires Named Tickets the buyer answered every Ticket at
// checkout, the ones named for somebody else included. Those Answers are the
// buyer's to read and correct while the Ticket is `assigned`, and become the
// Holder's the moment the Holder accepts: the Holder finds them already given
// on the accept page and may change any of them, and the buyer is back to the
// assignment state alone, as ADR 0049 has it. Who gave an Answer is not
// recorded, as before.
//
// IT IS THE BUYER'S SALE-SCOPED SURFACE, NOT A NEW ONE. The read rides on the
// buyer's list (BuyerTicketAnswersView.ProvisionalAnswers) and the write takes
// the same Customer Session, the same Confirmation Link narrowing and the same
// customer_id-scoped read as the assignment write beside it. A Ticket that is
// not provisionally the buyer's is NOT FOUND, indistinguishably from one that
// does not exist, so the route offers nothing to probe with.
//
// ITS PATH IS NOT THE SALE-SCOPED ANSWER WRITE ADR 0049 RETIRED, and that one
// stays gone. The retired route answered any Ticket of the buyer's Sale; this
// one is named for the only Answers it reaches, and refuses everything else.
//
// A CORRECTION IS NOT A REASSIGNMENT. It goes through answerTicketQuestion,
// which writes ticket_answers and nothing else, so `assigned_at` - what every
// Assignment Link and the owed Assignment mail are signed over - never moves.

// ProvisionalAnswersView is an `assigned` Ticket's questions and Answers as its
// buyer reads them on a Named Tickets Event. The same fields, under the same
// names, as the held-ticket row's answering half (HeldTicketAnswersView), so
// the Storefront draws one panel from either.
//
// ITS OWN TYPE, for the reason every view here is: each has a different reader,
// and a shared struct is a field added for one appearing on the others.
type ProvisionalAnswersView struct {
	// Answerable and AnswerableRefusal are the write window: false once the
	// Event has started and on a reversed Sale, with the refusal as a token
	// the Storefront translates. The READ is never gated by it.
	Answerable        bool   `json:"answerable"`
	AnswerableRefusal string `json:"answerable_refusal"`
	// OutstandingCount is how many required questions this Ticket has not yet
	// answered, from the platform's one definition of the debt.
	OutstandingCount int `json:"outstanding_count"`
	// Questions carries the Ticket Type's questions in the order they are
	// asked, retired ones last, each with this Ticket's Answer or null.
	Questions []TicketQuestionAnswerView `json:"questions"`
}

// AnswerBuyerProvisionalTicketQuestion writes one Answer on one Ticket of the
// buyer's own Sale whose Answers are provisionally theirs, and returns the
// whole Sale's rows.
//
// customerID and sessionTicketSaleID come from the Customer Session the
// middleware validated, and NEITHER comes from the request - the rule
// ListBuyerTicketAnswers and AssignOwnTicket are held to.
//
// THE ORDER OF THE REFUSALS is the staff route's: the flag, then whether the
// Ticket is reachable, then the window, then the question and the value. A
// value checked before the Ticket was resolved would tell a stranger which of
// their guessed ids was real.
//
// THE WHOLE SALE COMES BACK, as after an assignment: the buyer's page is one
// list read together, and its tally counts every row.
func (s *Service) AnswerBuyerProvisionalTicketQuestion(
	ctx context.Context,
	customerID, sessionTicketSaleID, ticketSaleID, ticketID, questionID string,
	input AnswerInput,
) ([]BuyerTicketAnswersView, error) {
	if !s.ticketQuestionsEnabled {
		return nil, catalog.ErrTicketQuestionsUnavailable()
	}
	ticket, err := s.buyerProvisionalTicket(ctx, customerID, sessionTicketSaleID, ticketSaleID, ticketID)
	if err != nil {
		return nil, err
	}
	if err := s.answerWindowOpen(ticket); err != nil {
		return nil, err
	}
	if err := s.answerTicketQuestion(ctx, ticket.ID, ticket.TicketTypeID, questionID, input); err != nil {
		return nil, err
	}

	fresh, err := s.repo.ListAnswerableTicketsForBuyer(ctx, customerID, ticketSaleID)
	if err != nil {
		return nil, err
	}
	return s.buyerTicketAnswersViews(ctx, customerID, fresh)
}

// buyerProvisionalTicket resolves one Ticket of the buyer's own Sale whose
// Answers are provisionally the buyer's, or refuses with TICKET_NOT_FOUND.
//
// ONE REFUSAL FOR EVERY WAY OF MISSING: another Sale than the link session's,
// another Customer's Sale, a Ticket not on this Sale, and a Ticket of the
// buyer's own Sale whose Answers are not theirs - accepted, unassigned, or on
// an Event without Named Tickets. Telling those apart would turn the route into
// a way to learn what other people's Tickets are doing.
func (s *Service) buyerProvisionalTicket(
	ctx context.Context,
	customerID, sessionTicketSaleID, ticketSaleID, ticketID string,
) (*repository.AnswerableTicket, error) {
	if sessionTicketSaleID != "" && sessionTicketSaleID != ticketSaleID {
		return nil, catalog.ErrTicketNotFound()
	}
	tickets, err := s.repo.ListAnswerableTicketsForBuyer(ctx, customerID, ticketSaleID)
	if err != nil {
		return nil, err
	}
	ticket := findBuyerTicket(tickets, ticketID)
	if ticket == nil || !s.buyerAnswersProvisional(ticket) {
		return nil, catalog.ErrTicketNotFound()
	}
	return ticket, nil
}

// buyerAnswersProvisional asks the domain's one predicate about one row of the
// buyer's scoped read. The read and the write both come through here.
func (s *Service) buyerAnswersProvisional(ticket *repository.AnswerableTicket) bool {
	holderEmail := ""
	if ticket.HolderEmail.Valid {
		holderEmail = ticket.HolderEmail.String
	}
	state := catalog.AssignmentState(holderEmail, nullTimeOrNil(ticket.AssignedAt), nullTimeOrNil(ticket.AcceptedAt))
	return catalog.BuyerAnswersProvisional(ticket.RequiresNamedTickets, s.ticketAssignmentEnabled, state)
}

// provisionalAnswersViews builds the answering half of each provisional row,
// keyed by Ticket id, from the staff view and COPYING FIELD BY FIELD - exactly
// as heldTicketViews does, so nothing added to the staff view (the Sale's
// reference, its id) reaches the buyer's row unreviewed.
func (s *Service) provisionalAnswersViews(
	ctx context.Context,
	tickets []repository.AnswerableTicket,
) (map[string]*ProvisionalAnswersView, error) {
	views := make(map[string]*ProvisionalAnswersView, len(tickets))
	if len(tickets) == 0 {
		return views, nil
	}
	staffViews, err := s.ticketAnswersViews(ctx, tickets)
	if err != nil {
		return nil, err
	}
	for i, staff := range staffViews {
		views[staff.TicketID] = &ProvisionalAnswersView{
			Answerable:        staff.Answerable,
			AnswerableRefusal: staff.AnswerableRefusal,
			OutstandingCount:  outstandingAnswerCount(tickets[i].SaleStatus, staff.Questions),
			Questions:         staff.Questions,
		}
	}
	return views, nil
}
