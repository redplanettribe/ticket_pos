package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// The Answer Reminder's half that belongs to the catalog (#317, ADR 0044): who
// is due one, and the record of who has been told.
//
// THE SPLIT, STATED ONCE. The reminder is a mail about a Ticket Sale, so the
// sales module composes and sends it — that is where the Confirmation Link it
// points at, the Mail Locale it is written in and the transactional sender all
// already are. What this module owns is the DEBT and the RATIONING: what counts
// as an Outstanding Answer (#313), whether a buyer may be written to now
// (catalog.MayRemind), and the ledger that makes "at most two ever" a fact
// rather than a hope. Sales asks; the catalog decides who.
//
// TWO GATES, BOTH HERE. The sweep's SQL rations so that a batch cannot be
// starved by Sales it may not mail, and catalog.MayRemind is applied again to
// every row that comes back before the sales module ever sees it. That is not
// belt and braces for its own sake: the Go rule is the authoritative statement,
// the SQL exists so the database can skip, and a disagreement between them must
// resolve as SILENCE. A candidate refused here costs one wasted row; a candidate
// that slipped through would be a mail somebody was rationed out of receiving.

// TicketSalesDueAnswerReminder returns the Ticket Sales whose buyers may be sent
// an Answer Reminder now, oldest sale first, at most limit of them.
//
// THE FLAG IS READ HERE, and a dark deployment returns nothing at all (ADR
// 0045). It is the same gate TicketSaleHasOutstandingAnswers applies to the Sale
// Confirmation's one sentence, and it matters for the same reason with more
// force: this mail is entirely about Ticket Questions, and a whole message about
// them landing in an inbox before the Privacy Policy describes the collection is
// precisely the failure ADR 0045 exists to prevent.
//
// IT IS GATED WHERE THE PURGE IS NOT, which will look inconsistent to somebody
// reading the two jobs side by side and is deliberate. The purge is the promise
// the platform made about data it already collected, so gating it would mean a
// deployment that stopped asking questions kept every answer forever. This one
// SENDS. A send is the thing the flag exists to hold back, and a reminder about
// questions a closed deployment no longer asks anyone would be a mail with
// nothing behind it.
//
// The clock is this service's own and is never a caller's. The cooldown cutoff
// is derived from it here rather than accepted as an argument, on the standing
// rule of the internal namespace: a caller who could name the cutoff could lift
// the seven-day silence on demand and mail the platform's whole outstanding
// backlog twice in an afternoon.
func (s *Service) TicketSalesDueAnswerReminder(ctx context.Context, limit int) ([]catalog.DueAnswerReminder, error) {
	if !s.ticketQuestionsEnabled {
		return nil, nil
	}
	if limit <= 0 {
		return nil, nil
	}

	now := s.now()
	candidates, err := s.repo.ListTicketSalesDueAnswerReminder(ctx, now, now.Add(-catalog.AnswerReminderInterval), limit)
	if err != nil {
		return nil, err
	}

	// The second gate. Each candidate is re-decided through the Go statement of
	// the rule, against the same clock the query used, so that the SQL is a
	// bounded prefilter and never the last word on whether somebody is written
	// to.
	due := make([]catalog.DueAnswerReminder, 0, len(candidates))
	for _, candidate := range candidates {
		if catalog.MayRemind(candidate.Inputs(now)) {
			due = append(due, candidate)
		}
	}
	return due, nil
}

// CountTicketSalesDueAnswerReminder is how many Ticket Sales are due a reminder
// in all, ignoring any batch: the standing backlog an operator reads two runs
// apart to tell a sweep that is keeping up from one that is not.
//
// It is REPORTING and not the job, which is why a dark deployment answers zero
// rather than refusing: zero is the truthful backlog of a platform that will
// send nothing.
func (s *Service) CountTicketSalesDueAnswerReminder(ctx context.Context) (int, error) {
	if !s.ticketQuestionsEnabled {
		return 0, nil
	}
	now := s.now()
	return s.repo.CountTicketSalesDueAnswerReminder(ctx, now, now.Add(-catalog.AnswerReminderInterval))
}

// RecordAnswerReminderSent writes the ledger row that rations the next one.
//
// It takes no moment: the clock is this service's, for the same reason the
// sweep's is. A caller that could name when a reminder "was" sent could date one
// eight days ago and mail the same buyer again tomorrow.
//
// THE CALLER MUST HAVE SENT ALREADY. This is the record of a message a provider
// has accepted, and writing it first would ration a buyer out of a reminder they
// never received — permanently, since the cap counts for the life of the Sale.
// See repository.RecordAnswerReminderSent for why the other failure is the one
// worth having.
func (s *Service) RecordAnswerReminderSent(ctx context.Context, ticketSaleID string) error {
	return s.repo.RecordAnswerReminderSent(ctx, ticketSaleID, s.now())
}
