package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// The Answer Reminder's sweep and its ledger (#317, ADR 0044).
//
// It lives in THIS file rather than in outstanding_answer_repository.go, and in
// this package rather than in sales', for two reasons that pull in opposite
// directions and are both satisfied here:
//
//   - The sweep is the Outstanding Answer derivation with a different SELECT and
//     a different grouping, so it must reuse outstandingAnswerFrom and
//     outstandingAnswerWhere — which are this package's, unexported, and stay
//     that way. Restating those four clauses in the sales module would give the
//     platform a third opinion about what "required" means, and the reminder
//     would be the surface that chased somebody about a debt the Organization's
//     screen says they do not have. That is exactly what the note at the foot of
//     the derivation's file asks every new caller not to do.
//
//   - The RATIONING is the reminder's own and is a rule about mailing, so it is
//     added here as extra clauses rather than pushed down into those constants.
//     An Outstanding Answer that disappeared from #313's list because an Event
//     had started, or because two mails had already gone, would be a list
//     disagreeing with itself.
//
// The mail itself is composed and sent by the sales module, which is where a
// Ticket Sale's Confirmation Link, Mail Locale and email sender already live.
// This file answers "who is due and who has been told"; that one answers "what a
// buyer reads".

// answerReminderLedger attaches each candidate Sale's mailing history: how many
// Answer Reminders it has had, and when the last one went.
//
// A LATERAL rather than a join-and-group, because the outer query is already
// grouping one row per (Ticket, question) pair down to one row per Sale, and
// folding a second one-to-many into that GROUP BY would multiply the ledger rows
// by the debt rows and count both wrong. This way the aggregate is computed once
// per Sale, against the (ticket_sale_id, sent_at DESC) index migration 079
// exists for, and arrives as two scalars the outer GROUP BY can carry
// unchanged.
//
// COALESCE on the count and not on the max: "never reminded" must reach Go as a
// ZERO TIME rather than as some sentinel date, because catalog.MayRemind names
// that case explicitly — the first reminder is due the day the debt appears, and
// not because a very old timestamp happened to clear a seven-day window.
const answerReminderLedger = `
	LEFT JOIN LATERAL (
		SELECT COUNT(*) AS sent_count, MAX(ar.sent_at) AS last_sent_at
		FROM answer_reminders ar
		WHERE ar.ticket_sale_id = s.id
	) r ON TRUE
`

// answerReminderRation is catalog.MayRemind in SQL: the clauses deciding
// whether this Ticket Sale's buyer may be written to now.
//
// THE SAME RULE LIVES TWICE, and the reason is starvation rather than
// performance. The Go statement is the authoritative one and the service applies
// it to every row this query returns — but if the query did NOT ration, a
// platform whose oldest hundred Sales had all been mailed twice would hand the
// job the same hundred unmailable candidates every night, and the
// hundred-and-first would never be reached inside any batch. The database has to
// be able to skip what it may not mail.
//
// FOUR CLAUSES. The fifth of MayRemind's five — the debt itself — is already
// outstandingAnswerWhere above, which is the point of assembling the two:
//
//   - $1, twice: the Event must have a start AND it must still be ahead. NULL
//     starts_at is refused rather than allowed, matching MayRemind's zero-time
//     case: an Event nobody has placed in time has no doors for this mail to be
//     before, and the Answer Links the buyer would be sent to hand out expire at
//     a start that does not exist.
//
//   - $2: the lifetime cap, `<` and not `!=`, so a ledger holding more rows than
//     the cap allows goes quiet instead of wrapping around into sending again.
//
//   - $3: the cooldown, as a CUTOFF computed by the caller from its own clock
//     (now - catalog.AnswerReminderInterval) rather than as an interval spelled
//     in this SQL. One place decides how long a week is, and it is the same place
//     MayRemind reads it from.
//
// A reversed Sale needs no clause here: `s.status = 'active'` is already in
// outstandingAnswerWhere, and a reversed Sale's Tickets owe nothing at all.
const answerReminderRation = `
	AND e.starts_at IS NOT NULL
	AND e.starts_at > $1
	AND r.sent_count < $2
	AND (r.last_sent_at IS NULL OR r.last_sent_at <= $3)
`

// answerReminderFrom is the whole sweep's FROM: the debt derivation, the Event
// the silence is measured against, and the mailing ledger.
//
// The join to `events` is an INNER join, so a Sale whose Event has somehow gone
// is not a candidate. That is unreachable — ticket_sales cascade from events —
// and it is written this way because the alternative, a LEFT join with a NULL
// start, would be a Sale mailed about an Event that does not exist.
const answerReminderFrom = outstandingAnswerFrom + `
	JOIN events e ON e.id = s.event_id
` + answerReminderLedger

// ListTicketSalesDueAnswerReminder returns the Ticket Sales whose buyers may be
// sent an Answer Reminder now, oldest sale first.
//
// IT NAMES NO ORGANIZATION AND NO EVENT, unlike every other query built on this
// derivation, and that is not a missing scope. Its caller is a scheduled job
// with no actor at all — nobody is asking, so there is nobody to scope to — and
// the same reasoning TicketSaleHasOutstandingAnswers records applies with more
// force here: what leaves this process is not a list anybody reads, it is a mail
// addressed to the buyer of each row about their own purchase. There is no
// disclosure to get wrong, because every fact selected here goes only to the
// person it is already about.
//
// OLDEST SALE FIRST, matching the Organization's chase list. Under a batch
// smaller than the backlog this is what decides who waits, and the buyer who
// paid in January and has said nothing since is the one whose silence has run
// longest.
//
// The LIMIT bounds one HTTP request and not the platform: what a run does not
// reach is still due tomorrow, and nothing about the rationing depends on a Sale
// being reached on any particular day.
func (r *Repository) ListTicketSalesDueAnswerReminder(
	ctx context.Context,
	now time.Time,
	cooldownCutoff time.Time,
	limit int,
) ([]catalog.DueAnswerReminder, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT s.id, s.status, e.starts_at, COALESCE(e.ends_at, e.starts_at), e.name,
		       COALESCE(s.locale, ''), s.customer_email,
		       s.customer_first_name, s.customer_last_name,
		       r.sent_count, r.last_sent_at
	`+answerReminderFrom+`
		WHERE `+outstandingAnswerWhere+answerReminderRation+`
		GROUP BY s.id, s.status, e.starts_at, e.ends_at, e.name, s.locale,
		         s.customer_email, s.customer_first_name, s.customer_last_name,
		         s.sold_at, r.sent_count, r.last_sent_at
		ORDER BY s.sold_at ASC, s.id ASC
		LIMIT $4
	`, now, catalog.MaxAnswerReminders, cooldownCutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	due := make([]catalog.DueAnswerReminder, 0)
	for rows.Next() {
		var d catalog.DueAnswerReminder
		// starts_at is NOT NULL by the ration's own clause and ends_at falls back
		// to it, so both scan into plain times. last_sent_at is the one honestly
		// nullable column here: it is NULL for a Sale nobody has chased.
		var lastSent sql.NullTime
		if err := rows.Scan(
			&d.TicketSaleID, &d.SaleStatus, &d.EventStartsAt, &d.EventEnd, &d.EventName,
			&d.SaleLocale, &d.CustomerEmail,
			&d.CustomerFirstName, &d.CustomerLastName,
			&d.RemindersSent, &lastSent,
		); err != nil {
			return nil, err
		}
		if lastSent.Valid {
			d.LastRemindedAt = lastSent.Time
		}
		due = append(due, d)
	}
	return due, rows.Err()
}

// CountTicketSalesDueAnswerReminder is how many Ticket Sales are due a reminder
// in all, ignoring any batch.
//
// It is the standing backlog, in the sense the Abandoned Answer Purge's
// answers_held is: what makes two runs a day apart legible. A figure that stays
// flat while the job reports sends is a sweep that is not keeping up; a figure
// that is zero forever on a live platform means either that nothing is being
// asked or that the rationing has quietly closed over everything.
//
// It is a SECOND QUERY rather than a window function on the first, because the
// first is bounded by a LIMIT and a count taken through that LIMIT could only
// ever report the batch size back at the operator.
func (r *Repository) CountTicketSalesDueAnswerReminder(
	ctx context.Context,
	now time.Time,
	cooldownCutoff time.Time,
) (int, error) {
	var total int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT s.id)
	`+answerReminderFrom+`
		WHERE `+outstandingAnswerWhere+answerReminderRation,
		now, catalog.MaxAnswerReminders, cooldownCutoff,
	).Scan(&total)
	return total, err
}

// RecordAnswerReminderSent appends one row to the ledger: this Ticket Sale's
// buyer was written to at this moment.
//
// CALLED AFTER THE PROVIDER ACCEPTED THE MESSAGE and never before. The two
// orders fail differently and only one of them is acceptable: recording first
// and failing to send rations a buyer out of a reminder they never received,
// silently and permanently, since the cap is a lifetime one. Sending first and
// failing to record costs at most one duplicate on a later tick — visible,
// bounded by the same cap once it lands, and the direction worth failing in.
//
// The moment is the CALLER'S CLOCK rather than the database's NOW(), so that
// the row a run wrote and the rationing that run reasoned with agree to the
// instant, and so a fixed-clock test can move a week without touching a row.
func (r *Repository) RecordAnswerReminderSent(ctx context.Context, ticketSaleID string, sentAt time.Time) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO answer_reminders (ticket_sale_id, sent_at)
		VALUES ($1, $2)
	`, ticketSaleID, sentAt)
	return err
}
