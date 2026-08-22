package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// The Answer Reminder's sweep and its ledger (#317, ADR 0044; #328, ADR 0046).
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
// THE GRAIN OF THIS FILE IS THE TICKET SINCE #328. It used to group one row per
// (Ticket, question) pair down to one row per SALE, because the mail was
// addressed to the buyer and the buyer is a property of the Sale. A Ticket now
// has a Holder, who since ADR 0049 is the ONLY person written to, so the query
// stops one level lower: it returns one row per candidate held TICKET with that
// Ticket's own ledger, and the SERVICE groups the Tickets one Holder holds into
// one message. Grouping in Go rather than in SQL is deliberate — see the note
// on ListAnswerReminderCandidates.
//
// The mails themselves are composed and sent by the sales module, which is where
// a Ticket Sale's Confirmation Link, Mail Locale and email sender already live.
// This file answers "which Tickets may be chased and who for"; that one answers
// "what a reader sees".

// answerReminderLedger attaches each candidate TICKET's mailing history: how
// many Answer Reminders it has produced, and when the last one went.
//
// A LATERAL rather than a join-and-group, because the outer query is already
// grouping one row per (Ticket, question) pair down to one row per Ticket, and
// folding a second one-to-many into that GROUP BY would multiply the ledger rows
// by the debt rows and count both wrong. This way the aggregate is computed once
// per Ticket, against the (ticket_id, sent_at DESC) index migration 083 exists
// for, and arrives as two scalars the outer GROUP BY can carry unchanged.
//
// COALESCE on the count and not on the max: "never reminded" must reach Go as a
// ZERO TIME rather than as some sentinel date, because catalog.MayRemind names
// that case explicitly — the first reminder is due the day the debt appears, and
// not because a very old timestamp happened to clear a seven-day window.
//
// IT READS `answer_reminders` AND NEVER `ticket_assignment_mails`. Both are
// keyed on a Ticket and both exist to say no, and they are different allowances
// for different messages (#332, migration 082): one bounds how often somebody is
// chased about an unanswered question, the other bounds how often a stranger is
// written to about being handed a ticket. A sweep that spent the wrong one would
// ration a buyer out of assigning a Ticket because of a t-shirt size.
const answerReminderLedger = `
	LEFT JOIN LATERAL (
		SELECT COUNT(*) AS sent_count, MAX(ar.sent_at) AS last_sent_at
		FROM answer_reminders ar
		WHERE ar.ticket_id = tk.id
	) r ON TRUE
`

// answerReminderRation is catalog.MayRemind in SQL: the clauses deciding
// whether this TICKET may be chased now.
//
// THE SAME RULE LIVES TWICE, and the reason is starvation rather than
// performance. The Go statement is the authoritative one and the service applies
// it to every row this query returns — but if the query did NOT ration, a
// platform whose oldest hundred Tickets had all been chased twice would hand the
// job the same hundred unmailable candidates every night, and the
// hundred-and-first would never be reached inside any batch. The database has to
// be able to skip what it may not mail.
//
// FOUR CLAUSES OF MayRemind'S, AND ONE OF ITS OWN. The fifth of MayRemind's
// five — the debt itself — is already outstandingAnswerWhere above, which is
// the point of assembling the two:
//
//   - $1, twice: the Event must have a start AND it must still be ahead. NULL
//     starts_at is refused rather than allowed, matching MayRemind's zero-time
//     case: an Event nobody has placed in time has no doors for this mail to be
//     before, and the answer window a reader would be sent to closes at a start
//     that does not exist.
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
//
// AND ONE CLAUSE ABOUT THE HOLDER, which is catalog.AnswerReminderRecipient in
// SQL: only an `accepted` Ticket has anybody to write to (ADR 0049). An
// `unassigned`, `assigned` or purged Ticket is SKIPPED here — not deferred, not
// counted as due — because nobody can answer for it, and a batch that carried
// such rows to the service only to drop them would be a batch those rows could
// starve. accepted_at is tested directly here and only here, because SQL has
// no AssignmentState to go through; the Go function is applied to every row
// that comes back, and a disagreement resolves as silence.
const answerReminderRation = `
	AND e.starts_at IS NOT NULL
	AND e.starts_at > $1
	AND r.sent_count < $2
	AND (r.last_sent_at IS NULL OR r.last_sent_at <= $3)
	AND tk.accepted_at IS NOT NULL
`

// answerReminderFrom is the whole sweep's FROM: the debt derivation, the Event
// the silence is measured against, and the Ticket's mailing ledger.
//
// The join to `events` is an INNER join, so a Sale whose Event has somehow gone
// is not a candidate. That is unreachable — ticket_sales cascade from events —
// and it is written this way because the alternative, a LEFT join with a NULL
// start, would be a Ticket chased about an Event that does not exist.
//
// NOTHING IS JOINED FOR THE HOLDER, and that is worth stating because a reader
// will expect a `customers` join here. A Holder's mail names nobody: it carries
// the Event, the Ticket Type and the Customer Area's address, nothing more (ADR
// 0046, ADR 0049). Their address is on the Ticket, and their language is read
// by the sales module from the same seam every other mail reads a Mail Locale
// through. There is no Holder fact this query needs that `tickets` does not
// already hold — and NOTHING ABOUT THE BUYER IS SELECTED, because no reminder
// is about a purchase any more and a column that is never read is a column a
// template cannot print.
const answerReminderFrom = outstandingAnswerFrom + `
	JOIN events e ON e.id = s.event_id
` + answerReminderLedger

// answerReminderSelect is the candidate's columns, shared by the listing and
// nothing else. Written once beside the FROM and the GROUP BY it has to agree
// with, because three lists that must contain the same expressions are three
// chances to add a column to two of them.
const answerReminderSelect = `
	SELECT tk.id, tk.holder_email, tk.assigned_at, tk.accepted_at, tt.name,
	       s.id, s.status, e.starts_at, e.name, COALESCE(s.locale, ''),
	       r.sent_count, r.last_sent_at
`

// answerReminderGroupBy collapses the one row per (Ticket, required question)
// pair that the derivation produces down to one row per TICKET.
//
// A GROUP BY and not a DISTINCT, matching the shape the per-Sale query had: a
// Ticket owing three questions is one candidate and one ledger row, not three,
// and the debt's SIZE is deliberately never counted here — the mail names no
// figure, for the reason the receipt's sentence does not (#315).
//
// tk.ordinal is in the list only so the ORDER BY may use it; it names nothing
// the caller reads.
const answerReminderGroupBy = `
	GROUP BY tk.id, tk.holder_email, tk.assigned_at, tk.accepted_at, tk.ordinal,
	         tt.name, s.id, s.status, e.starts_at, e.name, s.locale,
	         s.sold_at, r.sent_count, r.last_sent_at
`

// ListAnswerReminderCandidates returns the held TICKETS that may be chased now,
// oldest sale first and in ticket order within a sale, at most limit of them.
//
// IT RETURNS TICKETS AND NOT MAILS, and the grouping into messages is the
// service's. That split is deliberate: one Holder's mail covers every owed
// Ticket they hold, so a query that returned mails would have to encode that
// rule in SQL — as an array_agg — and the rule would then live somewhere no
// unit test can reach. What SQL is good at here is skipping the Tickets nobody
// may be written to about, which is what the ration is for.
//
// ORDERED BY SALE AND THEN BY TICKET, which is what makes the service's grouping
// a single pass. A batch boundary may fall between two Tickets one Holder
// holds, in which case they are written to on two runs; each mail respects
// every per-Ticket cap, the split is invisible from inside one batch, and a
// rarity is the right price for a bound the query can actually promise.
//
// OLDEST SALE FIRST, matching the Organization's chase list. Under a batch
// smaller than the backlog this is what decides who waits, and the Holder whose
// Ticket was bought in January and has said nothing since is the one whose
// silence has run longest.
//
// IT NAMES NO ORGANIZATION AND NO EVENT, unlike every other query built on this
// derivation, and that is not a missing scope. Its caller is a scheduled job
// with no actor at all — nobody is asking, so there is nobody to scope to — and
// what leaves this process is not a list anybody reads, it is a mail addressed
// to a person about their own Ticket.
//
// IT READS NO FEATURE FLAG. With Ticket Assignment dark nothing is ever
// accepted — a Self-held Ticket is minted only while it is open (ADR 0048) —
// so there is nothing here for a flag to hide; and a deployment that closed it
// after Tickets had been accepted keeps chasing their Holders, who can still
// answer from the Customer Area.
//
// The LIMIT bounds one HTTP request and not the platform: what a run does not
// reach is still due tomorrow, and nothing about the rationing depends on a
// Ticket being reached on any particular day.
func (r *Repository) ListAnswerReminderCandidates(
	ctx context.Context,
	now time.Time,
	cooldownCutoff time.Time,
	limit int,
) ([]catalog.AnswerReminderCandidate, error) {
	rows, err := r.db.Pool.QueryContext(ctx, answerReminderSelect+
		answerReminderFrom+`
		WHERE `+outstandingAnswerWhere+answerReminderRation+
		answerReminderGroupBy+`
		ORDER BY s.sold_at ASC, s.id ASC, tk.ordinal ASC, tk.id ASC
		LIMIT $4
	`, now, catalog.MaxAnswerReminders, cooldownCutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candidates := make([]catalog.AnswerReminderCandidate, 0)
	for rows.Next() {
		var c catalog.AnswerReminderCandidate
		// starts_at is NOT NULL by the ration's own clause, so it scans into a
		// plain time. The nullable columns are the assignment's — which the ration
		// has already required to be set, and which are scanned as nullable anyway
		// so the Go rule below decides rather than a scan error — and
		// last_sent_at, which is NULL for a Ticket nobody has chased.
		var holderEmail sql.NullString
		var assignedAt, acceptedAt, lastSent sql.NullTime
		if err := rows.Scan(
			&c.TicketID, &holderEmail, &assignedAt, &acceptedAt, &c.TicketTypeName,
			&c.TicketSaleID, &c.SaleStatus, &c.EventStartsAt, &c.EventName,
			&c.SaleLocale, &c.RemindersSent, &lastSent,
		); err != nil {
			return nil, err
		}
		if lastSent.Valid {
			c.LastRemindedAt = lastSent.Time
		}
		// The Go statement of the recipient rule, applied to the row the SQL
		// clause let through. A row the two disagree about is dropped here and
		// mails nobody — the safe direction.
		c.HolderEmail = catalog.AnswerReminderRecipient(
			holderEmail.String, answerReminderTime(assignedAt), answerReminderTime(acceptedAt),
		)
		if c.HolderEmail == "" {
			continue
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// answerReminderTime is the pointer form catalog.AnswerReminderRecipient reads
// a timestamp in: nil for a column that is NULL.
//
// A LOCAL HELPER rather than a shared one, because the shape it converts to is
// AssignmentState's own signature and nothing else in this package speaks it.
// The pointer is how that function tells "no address was ever named" from "an
// address was named at the zero instant", which a plain time.Time cannot.
func answerReminderTime(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	return &v.Time
}

// CountAnswerRemindersDue is how many MAILS are due across the platform,
// ignoring any batch.
//
// It is the standing backlog, in the sense the Abandoned Answer Purge's
// answers_held is: what makes two runs a day apart legible. A figure that stays
// flat while the job reports sends is a sweep that is not keeping up; a figure
// that is zero forever on a live platform means either that nothing is being
// asked or that the rationing has quietly closed over everything.
//
// IT COUNTS MESSAGES AND NOT TICKETS, which is what an operator comparing it
// against `sent` needs, and it is the one place the grouping rule is stated in
// SQL. A distinct count over the Holder's address is exactly the fan-in the
// service performs: every owed Ticket one address holds collapses into that
// Holder's one mail — per Holder, not per Ticket, since #335's ruling.
//
// It is a SECOND QUERY rather than a window function on the listing, because
// that one is bounded by a LIMIT and a count taken through it could only ever
// report the batch size back at the operator.
func (r *Repository) CountAnswerRemindersDue(
	ctx context.Context,
	now time.Time,
	cooldownCutoff time.Time,
) (int, error) {
	var total int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT tk.holder_email)
	`+answerReminderFrom+`
		WHERE `+outstandingAnswerWhere+answerReminderRation,
		now, catalog.MaxAnswerReminders, cooldownCutoff,
	).Scan(&total)
	return total, err
}

// RecordAnswerRemindersSent appends one ledger row per TICKET a mail covered:
// these Tickets were chased at this moment.
//
// IT TAKES A SET AND NOT ONE ID, because one message spends several allowances.
// A Holder's reminder covers every owed Ticket they hold, so a Holder of three
// writes three rows for one mail. Rows here count TICKETS CHASED and never
// MESSAGES SENT, and nothing reads this table for a count of mail.
//
// ONE STATEMENT AND NOT A LOOP, so that a mail's rows land together or not at
// all. A partial write would leave some of a mail's Tickets rationed and others
// free, and the next tick would compose a second message to the same person
// about the remainder — a duplicate that looks, from the ledger, entirely
// correct.
//
// CALLED AFTER THE PROVIDER ACCEPTED THE MESSAGE and never before. The two
// orders fail differently and only one of them is acceptable: recording first
// and failing to send rations somebody out of a reminder they never received,
// silently and permanently, since the cap is a lifetime one. Sending first and
// failing to record costs at most one duplicate on a later tick — visible,
// bounded by the same cap once it lands, and the direction worth failing in.
//
// The moment is the CALLER'S CLOCK rather than the database's NOW(), so that the
// rows a run wrote and the rationing that run reasoned with agree to the instant,
// and so a fixed-clock test can move a week without touching a row.
func (r *Repository) RecordAnswerRemindersSent(ctx context.Context, ticketIDs []string, sentAt time.Time) error {
	if len(ticketIDs) == 0 {
		// A mail covering no Tickets is not a mail; the sweep never composes one.
		// Refusing to run the statement keeps that from becoming a silent no-op
		// somebody has to reason about at the send site.
		return nil
	}
	_, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO answer_reminders (ticket_id, sent_at)
		SELECT unnest($1::uuid[]), $2
	`, ticketIDs, sentAt)
	return err
}
