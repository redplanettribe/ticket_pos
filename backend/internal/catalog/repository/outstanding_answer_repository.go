package repository

import (
	"context"
	"time"
)

// The Outstanding Answers derivation (#313).
//
// An Outstanding Answer is a required Ticket Question one Ticket has not
// answered — DERIVED AND NEVER STORED. There is no table here and there must
// never be one; see catalog.IsOutstandingAnswer for why, and for the statement
// of the rule these queries implement.
//
// THE SAME RULE LIVES TWICE, IN GO AND IN SQL, and that is a deliberate cost.
// An Event's ticket roll can run to thousands of Tickets across several
// questions, so the debt cannot be computed by loading every Ticket into Go and
// filtering — the database has to know the rule. catalog.IsOutstandingAnswer is
// the statement of it, outstandingAnswerWhere below is the same four clauses in
// the same order, and the integration tests hold them together. Neither may be
// changed alone.

// outstandingAnswerFrom is the join that produces one row per (Ticket, required
// Ticket Question) pair, with the Answer that would discharge it if there is
// one.
//
// THE CROSS OF TICKETS AND THEIR TYPE'S QUESTIONS, which is what makes the debt
// derivable at all: a Ticket owes nothing that is written down anywhere, it owes
// whatever its Ticket Type asks and it has not replied to. The join to
// ticket_questions is on l.ticket_type_id, because Ticket Questions belong to
// the TICKET TYPE — never to the Event and never to the Organization — so a
// Ticket of the General type owes nothing the VIP type asks.
//
// THE LEFT JOIN IS THE WHOLE MECHANISM. `a.id IS NULL` in the WHERE is "this
// Ticket has said nothing about this question", and it is an ANTI-JOIN rather
// than a NOT EXISTS only because the same shape has to serve the grouped count
// and the per-question listing without being written twice.
//
// SCOPE IS THE SECURITY PROPERTY, and every caller must add
// `s.event_id = ... AND s.organization_id = ...` to the WHERE. Both, and never
// only the Event: an Event id alone would let one Organization's guess at an id
// resolve. The same reasoning as answerableTicketFrom above, which is scoped for
// the same reason.
const outstandingAnswerFrom = `
	FROM tickets tk
	JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
	JOIN ticket_sales s ON s.id = l.ticket_sale_id
	JOIN ticket_types tt ON tt.id = l.ticket_type_id
	JOIN ticket_questions q ON q.ticket_type_id = l.ticket_type_id
	LEFT JOIN ticket_answers a
		ON a.ticket_id = tk.id AND a.ticket_question_id = q.id
`

// outstandingAnswerWhere is the definition of the debt, in SQL.
//
// FOUR CLAUSES, THE SAME FOUR AS catalog.IsOutstandingAnswer, in the same order:
//
//   - q.required — the flag's only effect anywhere. An unanswered OPTIONAL
//     question is not a debt; nobody promised to answer it.
//
//   - q.retired_at IS NULL — a RETIRED question owes nothing. Every write path
//     into an Answer refuses a retired question, so a debt under one could never
//     be discharged by anybody: a row on the chase list with no working button
//     behind it. This erases no Answer — a retired question that WAS answered
//     still reads on the Ticket and still gets its export column. What ends is
//     the debt, not the record.
//
//   - s.status = 'active' — a reversed Ticket Sale's Tickets NEVER appear. Its
//     tickets have ceased to exist and its money has gone back, so there is
//     nobody to chase. Liveness is read from the Sale because a Ticket carries
//     no status of its own (ADR 0043).
//
//   - a.id IS NULL — nothing has been said. EXISTENCE AND NOT CONTENT: a
//     checkbox answered `false` is an Answer and discharges the debt, and a
//     blank Answer is never stored, so the row's presence is the whole test.
//
// WHAT IS DELIBERATELY ABSENT IS A CHANNEL FILTER. Tickets from `in_person` and
// `import` sales stand here beside the `online` ones, and start out owing
// EVERYTHING, because nobody ever put the questions to those buyers — there is
// no checkout form on a door sale or a spreadsheet import. That is the honest
// state of the debt and not a defect in the data, and hiding it would hide
// precisely the Tickets an Organization most needs to chase.
//
// AND SO IS THE CLOCK. An Event that has already started still reports its
// Outstanding Answers, even though catalog.AnswerWindow has by then frozen every
// route into an Answer. The doors opening un-asks nothing: the debt was real and
// went unpaid, and that is what somebody reviewing the event afterwards came to
// find out. Surfaces that must go quiet after the start impose their own
// silence; that is a rule about mailing, not about the debt.
const outstandingAnswerWhere = `
	q.required
	AND q.retired_at IS NULL
	AND s.status = 'active'
	AND a.id IS NULL
`

// outstandingAnswerScope narrows the derivation to one Event of one
// Organization. Written once beside the FROM so that no caller can assemble the
// join and forget half of it.
const outstandingAnswerScope = `
	AND s.event_id = $1 AND s.organization_id = $2
`

// TicketOwingAnswers is one Ticket that still owes at least one required Ticket
// Question an Answer, with everything needed to chase it and to open it.
//
// IT CARRIES THE TICKET SALE'S IDENTITY AND REFERENCE because that is how staff
// reach the Answers at all: the Answers dialog is keyed on a Ticket Sale, and a
// list that named only the Ticket would be a list nobody could act on. The
// buyer's name and email travel for the same reason — chasing means writing to
// somebody, and this list exists to be chased from.
type TicketOwingAnswers struct {
	ID string
	// Ordinal is which of its Ticket Sale Line's units this Ticket is,
	// 1..quantity, and the only thing telling two Tickets on one line apart —
	// which is what lets staff say "the second of Ana's four".
	Ordinal        int
	TicketTypeID   string
	TicketTypeName string
	TicketSaleID   string
	// ConfirmationRef is the buyer's own reference, which is what staff on the
	// phone match against and what the Answers dialog names itself after.
	ConfirmationRef string
	// Channel is 'online', 'in_person' or 'import'. It is here to EXPLAIN the
	// row rather than to filter it: a door sale or an import owing every
	// question is a buyer who was never asked, and a surface that could not say
	// so would look like it had lost their Answers.
	Channel           string
	CustomerFirstName string
	CustomerLastName  string
	CustomerEmail     string
	SoldAt            time.Time
	// OutstandingCount is how many required questions this Ticket owes. It is
	// counted in the same GROUP BY that found the Ticket, so it can never
	// disagree with the questions listed beside it.
	OutstandingCount int
}

// OutstandingQuestion is one required Ticket Question one Ticket has not
// answered — one Outstanding Answer, named.
//
// The label travels as the Organization COINED it and is never translated (ADR
// 0027), like every other Ticket Question label on every other surface.
type OutstandingQuestion struct {
	TicketID   string
	QuestionID string
	Label      string
	Kind       string
	SortOrder  int
}

// ListTicketsOwingAnswers returns a page of the Event's Tickets that still owe
// required Answers, oldest sale first, together with how many there are in all.
//
// OLDEST SALE FIRST, and not newest as the Sales list is. This is a chase list:
// the buyer who paid in January and has said nothing since is the one whose
// silence has run longest and whose shirt is least likely to arrive, so they
// belong at the top. The Sales list is a ledger and reads newest-first for the
// opposite and equally good reason.
//
// ONE TICKET IS ONE ROW however many questions it owes, because the unit of
// chasing is the Ticket — the thing staff open, and the thing a size gets
// ordered for. The individual debts hang off it, from
// ListOutstandingQuestionsForTickets.
//
// The total counts DISTINCT TICKETS and not debts, so it agrees with the rows
// being paged. "Nine Tickets owe something" is the sentence this list is; the
// number of individual questions owed is per-row and already in
// OutstandingCount.
func (r *Repository) ListTicketsOwingAnswers(
	ctx context.Context,
	organizationID, eventID string,
	limit, offset int,
) ([]TicketOwingAnswers, int, error) {
	var total int
	if err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT tk.id)
	`+outstandingAnswerFrom+`
		WHERE `+outstandingAnswerWhere+outstandingAnswerScope,
		eventID, organizationID,
	).Scan(&total); err != nil {
		return nil, 0, err
	}
	// A page past the end still reports the true total, so a surface can say how
	// many there are rather than appearing to have emptied. Same arrangement the
	// Sales list has.
	if total == 0 {
		return []TicketOwingAnswers{}, 0, nil
	}

	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT tk.id, tk.ordinal, l.ticket_type_id, tt.name,
		       s.id, s.confirmation_ref, s.channel,
		       s.customer_first_name, s.customer_last_name, s.customer_email, s.sold_at,
		       COUNT(*) AS outstanding_count
	`+outstandingAnswerFrom+`
		WHERE `+outstandingAnswerWhere+outstandingAnswerScope+`
		GROUP BY tk.id, tk.ordinal, l.ticket_type_id, tt.name,
		         s.id, s.confirmation_ref, s.channel,
		         s.customer_first_name, s.customer_last_name, s.customer_email, s.sold_at
		ORDER BY s.sold_at ASC, s.id ASC, tk.ordinal ASC
		LIMIT $3 OFFSET $4
	`, eventID, organizationID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	tickets := make([]TicketOwingAnswers, 0)
	for rows.Next() {
		var t TicketOwingAnswers
		if err := rows.Scan(
			&t.ID, &t.Ordinal, &t.TicketTypeID, &t.TicketTypeName,
			&t.TicketSaleID, &t.ConfirmationRef, &t.Channel,
			&t.CustomerFirstName, &t.CustomerLastName, &t.CustomerEmail, &t.SoldAt,
			&t.OutstandingCount,
		); err != nil {
			return nil, 0, err
		}
		tickets = append(tickets, t)
	}
	return tickets, total, rows.Err()
}

// ListOutstandingQuestionsForTickets names which required questions each of the
// given Tickets owes.
//
// ONE QUERY FOR THE WHOLE PAGE rather than one per Ticket, the same shape
// ListTicketAnswers uses: a page of fifty Tickets across three questions is one
// read, not fifty.
//
// It re-applies the SCOPE as well as the ids. The ids came from
// ListTicketsOwingAnswers and are already this Organization's, so the scope is
// redundant — and it stays because the day something else passes ids in from
// somewhere less careful, the redundancy is the thing that refuses.
func (r *Repository) ListOutstandingQuestionsForTickets(
	ctx context.Context,
	organizationID, eventID string,
	ticketIDs []string,
) ([]OutstandingQuestion, error) {
	if len(ticketIDs) == 0 {
		return nil, nil
	}

	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT tk.id, q.id, q.label, q.kind, q.sort_order
	`+outstandingAnswerFrom+`
		WHERE `+outstandingAnswerWhere+outstandingAnswerScope+`
		AND tk.id = ANY($3)
		ORDER BY q.sort_order ASC, q.created_at ASC
	`, eventID, organizationID, ticketIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	questions := make([]OutstandingQuestion, 0)
	for rows.Next() {
		var q OutstandingQuestion
		if err := rows.Scan(&q.TicketID, &q.QuestionID, &q.Label, &q.Kind, &q.SortOrder); err != nil {
			return nil, err
		}
		questions = append(questions, q)
	}
	return questions, rows.Err()
}

// CountOutstandingAnswers is how many Outstanding Answers an Event carries in
// all — debts and not Tickets, so a Ticket owing three counts three.
//
// The headline figure the surface leads with, and the one an Organization means
// by "how much don't I know yet". Counted in its own query rather than summed
// from a page, because a page is a page.
func (r *Repository) CountOutstandingAnswers(ctx context.Context, organizationID, eventID string) (int, error) {
	var total int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*)
	`+outstandingAnswerFrom+`
		WHERE `+outstandingAnswerWhere+outstandingAnswerScope,
		eventID, organizationID,
	).Scan(&total)
	return total, err
}

// A NOTE FOR #317, THE ANSWER REMINDER.
//
// The reminder sweeps ACTIVE TICKET SALES that have any Outstanding Answer,
// which is a different SELECT and a different grouping over exactly the same
// derivation: `SELECT s.id, ... ` + outstandingAnswerFrom + ` WHERE ` +
// outstandingAnswerWhere + a scope of its own, grouped by s.id. Reuse those two
// constants rather than restating the four clauses, and reuse
// catalog.IsOutstandingAnswer for anything decided in Go. The rationing per
// Ticket Sale and the silence once the Event has started are the REMINDER's
// rules and belong to it — they are about mailing, not about the debt, and
// pushing them down into this derivation would make the #313 list disagree with
// itself the moment an Event began.
