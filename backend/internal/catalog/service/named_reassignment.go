package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
)

// Reassignment on a Named Tickets Event (#673, spec #665, ADR 0076).
//
// A reassignment clears the old Holder's Answers (ADR 0046), so on an Event that
// requires Named Tickets a change of plans would undo the roster the checkout
// was refused until it had. So the rule is kept: the buyer's assign route takes
// the new Holder's Answers with the address, and refuses without the required
// ones in the very shape begin-checkout refuses with (#669), so the Storefront
// points the buyer at the missing fields the same way on either surface.
//
// THE RULE FOLLOWS THE EVENT, NOT THE CHANNEL. An imported Sale's buyer is asked
// the same, although the import itself was never refused by it: the import
// records a transaction that already happened, while this is the buyer acting
// now, on an Event whose Organization chose the friction.
//
// EVERY CALL WHILE IT BINDS, not only the ones that turn out to change the
// address. Whether a call changes anything is decided under the row lock, and a
// rule judged before it on a guess about that would leave a window in which a
// concurrent reassignment turned this call into a change made without Answers.
// A same-address resubmission is still a no-op: its Answers are not written.

// AssignmentAnswerInput is one Answer the buyer gives with a Holder's address:
// which Ticket Question, and the reply in the shape its kind takes.
type AssignmentAnswerInput struct {
	TicketQuestionID string
	Answer           AnswerInput
}

// namedTicketsBind reports whether the Named Tickets requirement binds one of
// the buyer's Tickets right now, through the domain's one predicate.
func (s *Service) namedTicketsBind(ticket *repository.AnswerableTicket) bool {
	return catalog.NamedTicketsApply(
		ticket.RequiresNamedTickets, s.ticketAssignmentEnabled, nullTimeOrNil(ticket.EventStartsAt), s.now(),
	)
}

// namedTicketAnswers judges the Answers given with an address against the Named
// Tickets requirement, and returns the ones to write with the assignment.
//
// NIL AND NO ERROR WHERE THE REQUIREMENT DOES NOT BIND - the setting is off,
// Ticket Assignment is dark, or the doors have opened - and then any Answers in
// the body are ignored, never written: on such an Event only the Holder answers
// (ADR 0049), and the route is what it always was.
//
// THE CHECKOUT'S OWN RULES, reused rather than restated. The questions are the
// ones the checkout form asks (CheckoutQuestionsSQL, so approved, unretired and
// none at all while Ticket Questions are dark), the replies are kept by
// HoldableCheckoutAnswers, which drops one that does not fit its question, and
// what is owed is OwedByNamedTickets' verdict on this one Ticket, numbered by
// its ordinal as the checkout numbers a basket's Tickets. So an invalid Answer
// to a required question is missing here exactly as it is at checkout, and an
// invalid optional one is dropped as it is there.
func (s *Service) namedTicketAnswers(
	ctx context.Context,
	ticket *repository.AnswerableTicket,
	holderEmail string,
	given []AssignmentAnswerInput,
) ([]repository.AssignedAnswer, error) {
	if !s.namedTicketsBind(ticket) {
		return nil, nil
	}

	var asked []catalog.AskedQuestion
	var held []catalog.HeldAnswer
	if s.ticketQuestionsEnabled {
		var err error
		asked, err = s.repo.ListCheckoutQuestions(ctx, []string{ticket.TicketTypeID})
		if err != nil {
			return nil, err
		}
		submitted := make([]catalog.SubmittedCheckoutAnswer, 0, len(given))
		for _, answer := range given {
			submitted = append(submitted, catalog.SubmittedCheckoutAnswer{
				TicketTypeID:     ticket.TicketTypeID,
				TicketIndex:      ticket.Ordinal,
				TicketQuestionID: answer.TicketQuestionID,
				Answer: catalog.SubmittedAnswer{
					Text:      answer.Answer.Text,
					Number:    answer.Answer.Number,
					Date:      answer.Answer.Date,
					Checked:   answer.Answer.Checked,
					OptionIDs: answer.Answer.OptionIDs,
				},
			})
		}
		held = catalog.HoldableCheckoutAnswers(asked, map[string]int{ticket.TicketTypeID: ticket.Ordinal}, submitted)
	}

	owed := catalog.OwedByNamedTickets([]catalog.NamedTicket{{
		TicketTypeID: ticket.TicketTypeID,
		TicketIndex:  ticket.Ordinal,
		HolderEmail:  holderEmail,
	}}, asked, held)
	if len(owed) > 0 {
		return nil, catalog.ErrNamedTicketsIncomplete(owed)
	}

	assigned := make([]repository.AssignedAnswer, 0, len(held))
	for _, answer := range held {
		options := make([]repository.UpsertTicketAnswerOption, 0, len(answer.Options))
		for _, option := range answer.Options {
			options = append(options, repository.UpsertTicketAnswerOption{
				TicketQuestionOptionID: option.TicketQuestionOptionID,
				LabelSnapshot:          option.LabelSnapshot,
			})
		}
		assigned = append(assigned, repository.AssignedAnswer{
			TicketQuestionID: answer.TicketQuestionID,
			Params:           upsertParams(answer.Value, options),
		})
	}
	return assigned, nil
}

// reassignmentQuestions reads, for the buyer's rows that may be reassigned
// under the Named Tickets requirement right now, the questions a reassignment
// must be given Answers to, keyed by Ticket id.
//
// THE QUESTIONS AND NEVER THEIR ANSWERS. These are the Organization's own words,
// published on the Event page's checkout form already; what any Holder said in
// reply is not on this list, so a row whose Answers belong to an accepted Holder
// gains nothing about that Holder by carrying it.
//
// One read for the whole Sale, as provisionalAnswersViews makes one.
func (s *Service) reassignmentQuestions(
	ctx context.Context,
	tickets []repository.AnswerableTicket,
) (map[string][]PublicTicketQuestion, error) {
	byTicket := map[string][]PublicTicketQuestion{}
	if !s.ticketQuestionsEnabled || !s.ticketAssignmentEnabled {
		return byTicket, nil
	}
	var bound []repository.AnswerableTicket
	seen := map[string]bool{}
	var ticketTypeIDs []string
	for i := range tickets {
		ticket := &tickets[i]
		if !s.namedTicketsBind(ticket) || s.assignmentWindowOpen(ticket) != nil {
			continue
		}
		bound = append(bound, *ticket)
		if !seen[ticket.TicketTypeID] {
			seen[ticket.TicketTypeID] = true
			ticketTypeIDs = append(ticketTypeIDs, ticket.TicketTypeID)
		}
	}
	if len(bound) == 0 {
		return byTicket, nil
	}

	asked, err := s.repo.ListCheckoutQuestions(ctx, ticketTypeIDs)
	if err != nil {
		return nil, err
	}
	byTicketType := map[string][]PublicTicketQuestion{}
	for _, question := range asked {
		byTicketType[question.TicketTypeID] = append(byTicketType[question.TicketTypeID], toPublicTicketQuestion(question))
	}
	for _, ticket := range bound {
		if questions := byTicketType[ticket.TicketTypeID]; len(questions) > 0 {
			byTicket[ticket.ID] = questions
		}
	}
	return byTicket, nil
}
