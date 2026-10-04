package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// The swept sender's statements over `owed_assignment_mails` (migration 127):
// claim one, drop one, back one off, record one sent, and count what is due
// (#671, parent #665, ADR 0076).

// OwedAssignmentMail is one owed mail as a claim hands it to the sweep.
type OwedAssignmentMail struct {
	TicketID string
	// OwedFor is the `tickets.assigned_at` the mail was owed for, as the commit
	// copied it.
	OwedFor time.Time
	// AttemptCount includes the claim that returned this row.
	AttemptCount int
	// BuyerCustomerID is the Ticket Sale's buyer, whom the ledger row names.
	// Invalid only for a Sale with no Customer, which no Storefront checkout
	// records (ADR 0054).
	BuyerCustomerID sql.NullString
	// Channel is the Ticket Sale's, for the assignment window.
	Channel string
}

// ClaimDueOwedAssignmentMail takes the oldest due owed mail out of the queue
// and hides it from other sweeps until leaseUntil, returning nil when nothing
// is due.
//
// ClaimDueDigest's statement, for its reasons. FOR UPDATE SKIP LOCKED means two
// overlapping sweeps never wait on each other or both take one row, and moving
// next_attempt_at to the lease is what keeps the row out of every other claim
// after this statement commits - so a sweep that dies holding it costs the
// Holder the lease and not the mail. The attempt is counted by the claim, as
// migration 127 says.
//
// ONE STATEMENT, the claim and the read of the Sale together, so no second
// round trip can straddle it. The Ticket's own assignment is NOT read here: the
// sweep reads it through the accept flow's read just before sending, which is
// the read the mail is composed from.
func (r *Repository) ClaimDueOwedAssignmentMail(ctx context.Context, now, leaseUntil time.Time) (*OwedAssignmentMail, error) {
	var out OwedAssignmentMail
	err := r.db.Pool.QueryRowContext(ctx, `
		WITH claimed AS (
			UPDATE owed_assignment_mails
			SET next_attempt_at = $2,
			    attempt_count = attempt_count + 1
			WHERE ticket_id = (
				SELECT o.ticket_id
				FROM owed_assignment_mails o
				WHERE o.next_attempt_at <= $1
				ORDER BY o.next_attempt_at, o.created_at, o.ticket_id
				FOR UPDATE SKIP LOCKED
				LIMIT 1
			)
			RETURNING ticket_id, assigned_at, attempt_count
		)
		SELECT c.ticket_id, c.assigned_at, c.attempt_count, s.customer_id, s.channel
		FROM claimed c
		JOIN tickets tk ON tk.id = c.ticket_id
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		JOIN ticket_sales s ON s.id = l.ticket_sale_id
	`, now, leaseUntil).Scan(&out.TicketID, &out.OwedFor, &out.AttemptCount, &out.BuyerCustomerID, &out.Channel)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DropOwedAssignmentMail deletes an owed mail unsent: it is owed no longer.
func (r *Repository) DropOwedAssignmentMail(ctx context.Context, ticketID string) error {
	_, err := r.db.Pool.ExecContext(ctx, `DELETE FROM owed_assignment_mails WHERE ticket_id = $1`, ticketID)
	return err
}

// RescheduleOwedAssignmentMail puts a failed owed mail back in the queue, due
// at nextAttemptAt.
func (r *Repository) RescheduleOwedAssignmentMail(ctx context.Context, ticketID string, nextAttemptAt time.Time) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		UPDATE owed_assignment_mails SET next_attempt_at = $2 WHERE ticket_id = $1
	`, ticketID, nextAttemptAt)
	return err
}

// RecordOwedAssignmentMailSent writes the ledger row for a sent owed mail and
// clears the owed mark, in one transaction.
//
// THE CALLER MUST HAVE SENT ALREADY, on migration 082's rule. The row is marked
// checkout_named (migration 128): it spends the Ticket's lifetime allowance and
// not the buyer's rolling window.
//
// ONE TRANSACTION, so the two facts cannot come apart: a ledger row with the
// mark still standing would send the mail again once the lease lapsed, and a
// mark cleared with no ledger row would hand the Ticket a resend it never had.
func (r *Repository) RecordOwedAssignmentMailSent(ctx context.Context, ticketID, buyerCustomerID string, sentAt time.Time) error {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ticket_assignment_mails (ticket_id, buyer_customer_id, sent_at, checkout_named)
		VALUES ($1, $2, $3, TRUE)
	`, ticketID, buyerCustomerID, sentAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM owed_assignment_mails WHERE ticket_id = $1`, ticketID); err != nil {
		return err
	}
	return tx.Commit()
}

// CountOwedAssignmentMailsDue is how many owed mails are due now and held by no
// claim: the standing backlog, reported and never acted on. A mail backed off
// after a failure is not due until its next attempt, and is not counted.
func (r *Repository) CountOwedAssignmentMailsDue(ctx context.Context, now time.Time) (int, error) {
	var count int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM owed_assignment_mails WHERE next_attempt_at <= $1
	`, now).Scan(&count)
	return count, err
}
