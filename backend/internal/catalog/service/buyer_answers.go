package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
)

// The buyer's own surface on their Tickets' Answers (#315, ADR 0044): the page
// behind the Confirmation Link, and the Customer Area for a signed-in Customer.
//
// THESE ARE ONE SURFACE AND NOT TWO. The Confirmation Link does not open a page
// of its own — it redeems into a Customer Session narrowed to one Ticket Sale
// and lands on the Customer Area, which is a single page listing every purchase
// the session can reach. So "the Confirmation Link page" and "the Customer Area"
// differ only in how many Sales the session may see, and both arrive here. One
// endpoint, one view, and no chance of the two drifting into disagreement about
// what a buyer may do.
//
// WHAT THIS SURFACE IS FOR is distribution. A buyer who bought four tickets
// knows one t-shirt size and not the other three, and the platform cannot write
// to the other three people because it holds no address for them and asks for
// none (ADR 0044). So the buyer answers the ones they know and forwards the
// rest, by whatever channel they already use, and this is where they get the
// links to forward.
//
// THE ANSWER LINKS ARE MINTED HERE AND HANDED TO NOBODY ELSE. Each one is an
// unauthenticated credential that answers for one Ticket until the Event starts,
// so the read behind this is scoped to the Customer on the session and to
// nothing else — see repository.ListAnswerableTicketsForBuyer, where the
// customer_id clause is the whole of the property. Handing a Ticket to the wrong
// person here would not disclose a row; it would hand them a durable credential
// over somebody else's ticket.

// BuyerTicketAnswersView is one Ticket of the buyer's own Ticket Sale, with its
// Ticket Questions, whatever has been said in reply, and the Answer Link to pass
// on.
//
// IT IS ITS OWN TYPE and neither TicketAnswersView nor AnswerLinkView, for the
// same reason those two are separate from each other: each of the three has a
// different reader, and a shared struct is a field added for one of them
// appearing on the other two. The staff view carries the Sale's identity because
// staff reach a Ticket through a Sale they looked up; the Answer Link's view
// carries almost nothing at all because a group chat is entitled to almost
// nothing; this one carries an Answer Link, which is the one field that must
// never appear on either of the others.
type BuyerTicketAnswersView struct {
	// TicketID names which Ticket this is, so the buyer's own form can post back
	// against it. Safe here and absent from AnswerLinkView, and the difference is
	// the credential: this reader proved they own the Sale, so an id they could
	// try somewhere else is an id for their own Ticket.
	TicketID string `json:"ticket_id"`
	// Ordinal is which of its line's units this is, 1..quantity. It is what lets
	// the page say "ticket 2 of 4" — the only thing telling two Tickets of one
	// line apart, and the buyer's only handle on which link they are copying.
	Ordinal        int    `json:"ordinal"`
	TicketTypeName string `json:"ticket_type_name"`
	// AnswerLink is the per-Ticket link this page exists to hand out, and is
	// EMPTY once the Ticket can no longer be answered.
	//
	// Empty rather than present-but-dead, because the copy button is a promise:
	// a buyer who copies a link into a group chat has finished the task as far as
	// they know, and will not find out for weeks that what they sent opened
	// nothing. Better to have no button than a button that forwards a dead end.
	// The same reasoning applies to a link the deployment could not sign at all,
	// which is a misconfiguration rather than anything about this Sale, and which
	// must not take the rest of the page down with it.
	AnswerLink string `json:"answer_link"`
	// Answerable is whether this Ticket's Answers may still be written — false
	// once the Event has started and false on a reversed Sale. Never a reason to
	// hide anything below it: a reversed Sale keeps its place in the Customer
	// Area, and Answers that vanished with it would read as data destroyed.
	Answerable bool `json:"answerable"`
	// AnswerableRefusal names WHY not, or is empty while it is answerable, so the
	// page can say what happened rather than leaving somebody pressing a form
	// that will not take.
	AnswerableRefusal string `json:"answerable_refusal"`
	// OutstandingCount is how many required Ticket Questions this Ticket still
	// owes, from catalog.IsOutstandingAnswer — the ONE definition of the debt
	// (#313), shared with the Organization's chase list and the SQL behind it.
	// Counted rather than restated so that what the buyer is asked to chase and
	// what the Organization sees outstanding can never be two different numbers.
	OutstandingCount int `json:"outstanding_count"`
	// Questions carries the Ticket Type's questions in the order they are asked,
	// retired ones last, each with this Ticket's Answer or null. The labels read
	// AS THE ORGANIZATION COINED THEM in every Locale, like a Custom Tag
	// (ADR 0027) — only the page's chrome follows the reader's language.
	Questions []TicketQuestionAnswerView `json:"questions"`
}

// ListBuyerTicketAnswers returns every Ticket of one of the buyer's own Ticket
// Sales, with its questions, its Answers and its Answer Link.
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
func (s *Service) ListBuyerTicketAnswers(
	ctx context.Context,
	customerID, sessionTicketSaleID, ticketSaleID string,
) ([]BuyerTicketAnswersView, error) {
	// The flag first, before anything is read, so that a dark build answers
	// exactly as a build that never had the feature (ADR 0045).
	if !s.ticketQuestionsEnabled {
		return nil, catalog.ErrTicketQuestionsUnavailable()
	}
	// The link session's narrowing, applied before the query rather than after
	// it. A Confirmation Link session asking about a Sale other than its own is
	// answered as if that Sale did not exist, which is what it is entitled to
	// know.
	if sessionTicketSaleID != "" && sessionTicketSaleID != ticketSaleID {
		return []BuyerTicketAnswersView{}, nil
	}

	tickets, err := s.repo.ListAnswerableTicketsForBuyer(ctx, customerID, ticketSaleID)
	if err != nil {
		return nil, err
	}
	if len(tickets) == 0 {
		return []BuyerTicketAnswersView{}, nil
	}
	return s.buyerTicketAnswersViews(ctx, tickets)
}

// AnswerOwnTicketQuestion writes one Answer on a Ticket of the buyer's own
// Ticket Sale.
//
// THE BUYER MAY ANSWER ANY TICKET OF THEIR SALE, which is the third acceptance
// criterion of #315 and follows from ADR 0044: an Answer belongs to the TICKET,
// and three parties may supply it — the holder through an Answer Link, the
// buyer, and Event Staff. A mother buying for her children answers all four
// herself and should never have to mail herself four links to do it.
//
// It resolves the Ticket through the SAME scoped read the listing uses, so a
// Ticket id belonging to somebody else's Sale is not found rather than refused,
// and then goes through answerTicketQuestion — the one body every route into an
// Answer passes through, where catalog.ParseAnswer, the retired-question rule
// and the Option resolution live. Four surfaces deciding for themselves what a
// NUMERIC question may be answered with would be four ways for that column to
// end up holding something that is not a number.
func (s *Service) AnswerOwnTicketQuestion(
	ctx context.Context,
	customerID, sessionTicketSaleID, ticketSaleID, ticketID, questionID string,
	input AnswerInput,
) ([]BuyerTicketAnswersView, error) {
	if !s.ticketQuestionsEnabled {
		return nil, catalog.ErrTicketQuestionsUnavailable()
	}
	if sessionTicketSaleID != "" && sessionTicketSaleID != ticketSaleID {
		return nil, catalog.ErrTicketNotFound()
	}

	tickets, err := s.repo.ListAnswerableTicketsForBuyer(ctx, customerID, ticketSaleID)
	if err != nil {
		return nil, err
	}
	ticket := findBuyerTicket(tickets, ticketID)
	if ticket == nil {
		// Not this buyer's Ticket, or not on this Sale, or not a Ticket at all —
		// one answer for all three, so that trying ids here teaches nothing.
		return nil, catalog.ErrTicketNotFound()
	}

	// The edit window is checked on the WRITE and never on the read, exactly as
	// it is for Event Staff: a Sale Reversal voids a sale rather than erasing
	// what its Tickets said, and the doors opening freezes the Answers rather
	// than hiding them.
	if err := s.answerWindowOpen(ticket); err != nil {
		return nil, err
	}
	if err := s.answerTicketQuestion(ctx, ticket.ID, ticket.TicketTypeID, questionID, input); err != nil {
		return nil, err
	}

	// The WHOLE Sale comes back rather than the one Ticket that changed. The
	// page is a list of Tickets whose outstanding counts move together — the
	// Sale-level "some of these still need answers" is derived from all of them —
	// so returning one row would leave the surface holding a stale total beside a
	// fresh row, which is the shape of bug nobody notices until an Organization
	// asks why a buyer says they answered.
	return s.buyerTicketAnswersViews(ctx, tickets)
}

// findBuyerTicket picks one Ticket out of the buyer's own Sale.
//
// A linear scan over the Sale's own Tickets rather than a second scoped query.
// The rows are already loaded and already proven to be this Customer's, and a
// second query would be a second place for the customer_id clause to go missing.
func findBuyerTicket(tickets []repository.AnswerableTicket, ticketID string) *repository.AnswerableTicket {
	for i := range tickets {
		if tickets[i].ID == ticketID {
			return &tickets[i]
		}
	}
	return nil
}

// buyerTicketAnswersViews assembles the buyer's payload from the staff one.
//
// It BUILDS ON ticketAnswersViews rather than repeating it, because the reading
// of questions, Answers, retired Options and the edit window is the same work
// for every reader — and then it drops the fields the buyer's page has no use
// for and adds the one that is theirs alone. Assembling this list from a second
// set of reads would be a second chance for a Ticket's questions to come back
// in a different order on two pages showing the same Ticket.
//
// THE FIELDS ARE COPIED ACROSS ONE BY ONE, deliberately, rather than by
// embedding the staff view. Embedding would mean that anything added to
// TicketAnswersView appears here the day it is added, unreviewed — and this is
// the payload that also carries an Answer Link, so the two must not be able to
// grow into each other.
func (s *Service) buyerTicketAnswersViews(
	ctx context.Context,
	tickets []repository.AnswerableTicket,
) ([]BuyerTicketAnswersView, error) {
	staffViews, err := s.ticketAnswersViews(ctx, tickets)
	if err != nil {
		return nil, err
	}

	views := make([]BuyerTicketAnswersView, 0, len(staffViews))
	for i, staff := range staffViews {
		view := BuyerTicketAnswersView{
			TicketID:          staff.TicketID,
			Ordinal:           staff.Ordinal,
			TicketTypeName:    staff.TicketTypeName,
			Answerable:        staff.Answerable,
			AnswerableRefusal: staff.AnswerableRefusal,
			OutstandingCount:  outstandingAnswerCount(tickets[i].SaleStatus, staff.Questions),
			Questions:         staff.Questions,
		}
		// A link is minted only while the Ticket can still be answered. Handing
		// out a link that opens nothing would be worse than handing out none: the
		// buyer forwards it, believes the job done, and nobody finds out.
		if staff.Answerable {
			// A deployment with no link secret cannot sign, which is a
			// misconfiguration and not a fact about this Sale. The rest of the page
			// — the questions, the Answers, the buyer's own ability to answer — is
			// unaffected and still worth showing, so the error is dropped here
			// rather than failing the whole read.
			if link, linkErr := s.AnswerLinkURL(staff.TicketID); linkErr == nil {
				view.AnswerLink = link
			}
		}
		views = append(views, view)
	}
	return views, nil
}

// TicketSaleHasOutstandingAnswers reports whether any Ticket of one Ticket Sale
// still owes a required Ticket Question an Answer.
//
// IT EXISTS FOR THE SALE CONFIRMATION and for nothing else (#315): it decides
// whether the receipt carries its one extra sentence. The sales module asks it
// through a seam of its own, because a Ticket Sale's receipt is that module's
// and what counts as a debt is this one's.
//
// THE FLAG IS READ HERE, so that a dark deployment answers false and the receipt
// renders exactly as it always did (ADR 0045). That is the same reason every
// other route into this feature reads it first, and it matters more here than
// anywhere: this is the only surface of the whole feature that reaches somebody
// who never visited a page, and a sentence about Ticket Questions in an inbox
// before the Privacy Policy describes them is the failure ADR 0045 is about.
//
// A FAILURE IS NOT AN ERROR TO THE CALLER'S EYE — see the sales module's seam,
// which turns any error into "no sentence". A receipt is worth immeasurably more
// than a line on it, and the safe direction is silence.
func (s *Service) TicketSaleHasOutstandingAnswers(ctx context.Context, ticketSaleID string) (bool, error) {
	if !s.ticketQuestionsEnabled {
		return false, nil
	}
	return s.repo.TicketSaleHasOutstandingAnswers(ctx, ticketSaleID)
}

// outstandingAnswerCount counts one Ticket's Outstanding Answers through
// catalog.IsOutstandingAnswer.
//
// IT CALLS THE SHARED DEFINITION RATHER THAN RESTATING ITS CLAUSES (#313). The
// rule already lives twice on purpose — once in Go and once in the SQL behind
// the Organization's chase list — and that duplication is only affordable
// because it is exactly two. A third statement of "required, not retired, active
// Sale, nothing said" here would be the one that quietly disagreed, and it would
// disagree in the worst place: the buyer would be told they owe nothing while
// the Organization's list still named them.
func outstandingAnswerCount(saleStatus string, questions []TicketQuestionAnswerView) int {
	count := 0
	for _, pair := range questions {
		if catalog.IsOutstandingAnswer(catalog.OutstandingAnswerInputs{
			Required:        pair.Question.Required,
			QuestionRetired: pair.Question.Retired,
			SaleStatus:      saleStatus,
			// EXISTENCE AND NOT CONTENT, which is the whole of the fourth clause:
			// a checkbox answered `false` is an Answer and discharges the debt, and
			// a blank Answer is never stored, so the pointer's presence is the test.
			Answered: pair.Answer != nil,
		}) {
			count++
		}
	}
	return count
}
