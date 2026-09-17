package service

import (
	"context"
	"time"
)

// What each Ticket on the Customer Dossier answered, still owes and was last
// reminded about (#641, spec #635).
//
// NOTHING IS RE-DECIDED HERE. The Answers are ticketQuestionAnswers, the pairs
// the staff Answers dialog shows, kept only where an Answer exists; the debt is
// outstandingQuestionsByTicket, the Holder List's own reading; the reminder is
// the Answer Reminder's ledger. Each is read once for every Ticket on the
// Dossier, keyed by the Ticket ids already loaded.
//
// A VOID TICKET OWES NOTHING. A Ticket of a reversed or corrected Sale keeps
// its Answers readable — a reversal voids a Sale, it does not erase what was
// said — and carries no Outstanding Answers and no reminder time, which are
// about a debt that no longer exists.
//
// DARK WHILE TICKET_QUESTIONS_ENABLED IS CLOSED (ADR 0045): every key is a
// pointer with `omitempty` and nothing is filled, so the Dossier reads as a
// build without the feature would.

// DossierTicketAnswers is what one Ticket on the Dossier answered, owes and was
// last reminded about. Embedded in both Ticket views so the two read alike.
type DossierTicketAnswers struct {
	// Answers are the Ticket's Answers: the Answers dialog's question/Answer
	// pairs that have an Answer, in the order the questions are asked; `[]`
	// when it answered nothing. Readable on a reversed Sale too. Absent while
	// TICKET_QUESTIONS_ENABLED is closed.
	Answers *[]TicketQuestionAnswerView `json:"answers,omitempty"`
	// OutstandingAnswers are the required questions it has not answered — the
	// Holder List's `outstanding` for the same Ticket; `[]` when it owes
	// nothing. Absent on a reversed Sale and while the questions flag is closed.
	OutstandingAnswers *[]OutstandingQuestionView `json:"outstanding_answers,omitempty"`
	// LastAnswerReminderSentAt is when an Answer Reminder was last sent about
	// this Ticket, or null when none was. Absent on a reversed Sale and while
	// the questions flag is closed.
	LastAnswerReminderSentAt **time.Time `json:"last_answer_reminder_sent_at,omitempty" swaggertype:"string" format:"date-time"`
}

// fillDossierAnswers puts the Answers, Outstanding Answers and last Answer
// Reminder onto the Tickets of the Dossier's Sales and its held Tickets.
func (s *Service) fillDossierAnswers(
	ctx context.Context, actor ActorContext, eventID string,
	sales []DossierSaleView, held []DossierHeldTicketView,
) error {
	if !s.ticketQuestionsEnabled {
		return nil
	}

	var tickets []ticketOfType
	var liveIDs []string
	for _, sale := range sales {
		for _, ticket := range sale.Tickets {
			tickets = append(tickets, ticketOfType{ID: ticket.TicketID, TicketTypeID: ticket.TicketTypeID})
			if sale.Status == DossierSaleActive {
				liveIDs = append(liveIDs, ticket.TicketID)
			}
		}
	}
	for _, ticket := range held {
		tickets = append(tickets, ticketOfType{ID: ticket.TicketID, TicketTypeID: ticket.TicketTypeID})
		if ticket.SaleStatus == DossierSaleActive {
			liveIDs = append(liveIDs, ticket.TicketID)
		}
	}
	if len(tickets) == 0 {
		return nil
	}

	reads := dossierAnswerReads{owed: map[string][]OutstandingQuestionView{}, lastReminded: map[string]time.Time{}}
	var err error
	if reads.pairs, err = s.ticketQuestionAnswers(ctx, tickets); err != nil {
		return err
	}
	if len(liveIDs) > 0 {
		if reads.owed, err = s.outstandingQuestionsByTicket(ctx, actor.OrganizationID, eventID, liveIDs); err != nil {
			return err
		}
		reminders, err := s.repo.ListDossierLastAnswerReminders(ctx, actor.OrganizationID, eventID, liveIDs)
		if err != nil {
			return err
		}
		for _, reminder := range reminders {
			reads.lastReminded[reminder.TicketID] = reminder.LastSentAt
		}
	}

	for i := range sales {
		live := sales[i].Status == DossierSaleActive
		for j := range sales[i].Tickets {
			ticket := &sales[i].Tickets[j]
			ticket.DossierTicketAnswers = reads.answersOf(ticket.TicketID, live)
		}
	}
	for i := range held {
		held[i].DossierTicketAnswers = reads.answersOf(held[i].TicketID, held[i].SaleStatus == DossierSaleActive)
	}
	return nil
}

// dossierAnswerReads are the three batched reads, keyed by Ticket id.
type dossierAnswerReads struct {
	pairs        map[string][]TicketQuestionAnswerView
	owed         map[string][]OutstandingQuestionView
	lastReminded map[string]time.Time
}

// answersOf shapes one Ticket's block: its Answers always, and its debt and last
// reminder only while the Ticket is live.
func (r dossierAnswerReads) answersOf(ticketID string, live bool) DossierTicketAnswers {
	answered := make([]TicketQuestionAnswerView, 0)
	for _, pair := range r.pairs[ticketID] {
		if pair.Answer != nil {
			answered = append(answered, pair)
		}
	}
	block := DossierTicketAnswers{Answers: &answered}
	if !live {
		return block
	}
	owed := outstandingOrEmpty(r.owed[ticketID])
	block.OutstandingAnswers = &owed
	var sentAt *time.Time
	if at, ok := r.lastReminded[ticketID]; ok {
		sentAt = &at
	}
	block.LastAnswerReminderSentAt = &sentAt
	return block
}
