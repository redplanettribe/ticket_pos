package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// The Assignment Reminder's candidate query (#362, parent #361, ADR 0051): the
// Ticket Sales whose buyer may be written to now about Tickets still nobody's,
// in the shape catalog.MayRemindAssignment decides about.
//
// PER SALE, NEVER PER TICKET. One row here is one buyer, one inbox and one
// mail; the Tickets are aggregated into the two numbers the mail prints. The
// Answer Reminder's query beside this one is per Ticket because its reader is
// per Ticket (migration 083); this one's reader is the buyer, so the unit is
// the Sale (migration 087).
//
// THE SAME RULE LIVES TWICE, IN SQL AND IN GO. assignmentReminderRation is
// catalog.MayRemindAssignment's clauses in the same order, here so the sweep
// can SKIP inside the database what it may not send — a backlog whose oldest
// fifty Sales had all been reminded twice would otherwise be handed to the job
// every tick and the fifty-first never reached. The service applies the Go
// rule to every row anyway; a disagreement resolves as silence.

// assignmentReminderTickets is the per-Sale tally: how many Tickets, and how
// many have no holder address. `holder_email IS NULL` is the `unassigned`
// state as migration 080 defines it — a purged Ticket reads NULL again and
// counts; a reassigned Self-held Ticket has an address and does not.
const assignmentReminderTickets = `
	JOIN LATERAL (
		SELECT COUNT(*) AS ticket_count,
		       COUNT(*) FILTER (WHERE tk.holder_email IS NULL) AS unassigned_count
		FROM ticket_sale_lines l
		JOIN tickets tk ON tk.ticket_sale_line_id = l.id
		WHERE l.ticket_sale_id = s.id
	) t ON TRUE
`

// assignmentReminderLedger is THIS SALE'S ledger: how many, and when the last.
const assignmentReminderLedger = `
	LEFT JOIN LATERAL (
		SELECT COUNT(*) AS sent_count, MAX(ar.sent_at) AS last_sent_at
		FROM assignment_reminders ar
		WHERE ar.ticket_sale_id = s.id
	) r ON TRUE
`

// assignmentReminderRation is the rationing, in SQL, parameterised on the
// moment ($1), the Sale-age cutoff ($2 = now - 24h), the cap ($3) and the
// cooldown cutoff ($4 = now - 7d). The reversal-row clause is stricter than
// the status alone: a Sale whose reversal is in flight is on its way out and
// is not pointed at.
const assignmentReminderRation = `
	AND s.channel = 'online'
	AND s.status = 'active'
	AND NOT EXISTS (SELECT 1 FROM sale_reversals sr WHERE sr.ticket_sale_id = s.id)
	AND s.created_at <= $2
	AND t.ticket_count > 1
	AND t.unassigned_count > 0
	AND e.starts_at IS NOT NULL
	AND e.starts_at > $1
	AND r.sent_count < $3
	AND (r.last_sent_at IS NULL OR r.last_sent_at <= $4)
`

const assignmentReminderFrom = `
	FROM ticket_sales s
	JOIN events e ON e.id = s.event_id
` + assignmentReminderTickets + assignmentReminderLedger

// ListAssignmentReminderCandidates returns the Sales due an Assignment
// Reminder, OLDEST SALE FIRST, up to limit.
//
// Oldest first is the batch discipline of every sweep here: a backlog is
// drained in the order it accrued, and a Sale that missed one tick's batch is
// nearer the front of the next. `s.id` breaks ties so the order is total.
func (r *Repository) ListAssignmentReminderCandidates(
	ctx context.Context,
	now time.Time,
	saleAgeCutoff time.Time,
	cooldownCutoff time.Time,
	limit int,
) ([]catalog.AssignmentReminderCandidate, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT s.id, s.customer_email, s.customer_first_name,
		       s.channel, s.status, s.created_at,
		       t.ticket_count, t.unassigned_count,
		       e.starts_at, e.ends_at, COALESCE(e.timezone, ''), e.name,
		       COALESCE(s.locale, ''),
		       r.sent_count, r.last_sent_at
	`+assignmentReminderFrom+`
		WHERE TRUE
	`+assignmentReminderRation+`
		ORDER BY s.created_at ASC, s.id ASC
		LIMIT $5
	`, now, saleAgeCutoff, catalog.MaxAssignmentReminders, cooldownCutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candidates := make([]catalog.AssignmentReminderCandidate, 0)
	for rows.Next() {
		var c catalog.AssignmentReminderCandidate
		var startsAt, endsAt, lastSent sql.NullTime
		if err := rows.Scan(
			&c.TicketSaleID, &c.BuyerEmail, &c.BuyerFirstName,
			&c.SaleChannel, &c.SaleStatus, &c.SaleCreatedAt,
			&c.TicketCount, &c.UnassignedTickets,
			&startsAt, &endsAt, &c.EventTimezone, &c.EventName,
			&c.SaleLocale,
			&c.RemindersSent, &lastSent,
		); err != nil {
			return nil, err
		}
		if startsAt.Valid {
			c.EventStartsAt = startsAt.Time
		}
		if endsAt.Valid {
			c.EventEndsAt = endsAt.Time
		}
		if lastSent.Valid {
			c.LastRemindedAt = lastSent.Time
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// CountAssignmentRemindersDue is the standing backlog: every Sale the ration
// admits right now, with no limit. Reported by the sweep so an Operator can
// see a catch-up draining.
func (r *Repository) CountAssignmentRemindersDue(
	ctx context.Context,
	now time.Time,
	saleAgeCutoff time.Time,
	cooldownCutoff time.Time,
) (int, error) {
	var total int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*)
	`+assignmentReminderFrom+`
		WHERE TRUE
	`+assignmentReminderRation,
		now, saleAgeCutoff, catalog.MaxAssignmentReminders, cooldownCutoff,
	).Scan(&total)
	return total, err
}

// RecordAssignmentReminderSent appends the ledger row for one Sale. Called
// AFTER the provider accepted the mail, never before (migration 087).
func (r *Repository) RecordAssignmentReminderSent(ctx context.Context, ticketSaleID string, sentAt time.Time) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO assignment_reminders (ticket_sale_id, sent_at) VALUES ($1, $2)
	`, ticketSaleID, sentAt)
	return err
}
