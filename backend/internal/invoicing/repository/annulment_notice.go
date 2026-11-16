package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// The Annulment Notice's queue (#748, spec #742, ADR 0078, migration 133).
// An owed notice lives on the annulment it follows. It is claimable only
// while that annulment stands (never reinstated), its factura is still
// annulled, and its Annulment Check is confirmed or unconfirmed; while the
// check is checking or at mismatch it is held, and nothing is written to
// hold it: the claim simply does not see it.

// AnnulmentNoticeClaim is one owed notice a tick has claimed: the annulment,
// the sends tried so far, and its factura read in full.
type AnnulmentNoticeClaim struct {
	AnnulmentID string
	Attempts    int
	Invoice     *InvoiceRow
}

// ClaimDueAnnulmentNotice takes ONE owed notice that is due at now and hides
// it from every other tick until leaseUntil, or returns nil when none is.
//
// THE CLAIM READS THE MISMATCH PREDICATE (annulmentAtMismatchSQL) beside the
// verdict, so the one sentence "this annulment stands at mismatch" is what
// holds a notice here as it holds Issue again and Reissue. SKIP LOCKED and
// the lease keep two concurrent ticks from sending one notice twice; the
// sent stamp's guard (MarkAnnulmentNoticeSent) keeps a send whose lease ran
// out from being recorded twice.
func (r *Repository) ClaimDueAnnulmentNotice(ctx context.Context, now, leaseUntil time.Time) (*AnnulmentNoticeClaim, error) {
	var claim AnnulmentNoticeClaim
	var invoiceID string
	err := r.db.Pool.QueryRowContext(ctx, `
		UPDATE invoicing_annulments
		SET notice_leased_until = $2
		WHERE id = (
			SELECT a.id
			FROM invoicing_annulments a
			JOIN invoicing_invoices i ON i.id = a.invoice_id
			WHERE a.notice_state = 'owed'
			  AND a.reinstated_at IS NULL
			  AND a.check_verdict IN ('confirmed', 'unconfirmed')
			  AND NOT `+annulmentAtMismatchSQL+`
			  AND (a.notice_next_attempt_at IS NULL OR a.notice_next_attempt_at <= $1)
			  AND (a.notice_leased_until IS NULL OR a.notice_leased_until <= $1)
			  AND i.status = 'annulled'
			ORDER BY COALESCE(a.notice_next_attempt_at, a.check_verdict_at), a.id
			FOR UPDATE OF a SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, invoice_id, notice_attempts
	`, now, leaseUntil).Scan(&claim.AnnulmentID, &invoiceID, &claim.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim due annulment notice: %w", err)
	}
	row, err := r.GetInvoice(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("claim due annulment notice: invoice %s not found", invoiceID)
	}
	claim.Invoice = row
	return &claim, nil
}

// MarkAnnulmentNoticeSent records that the sender took the notice, and frees
// the claim. Guarded on the notice still being owed, so it is recorded once
// whoever sent it; it reports whether this call wrote it.
func (r *Repository) MarkAnnulmentNoticeSent(ctx context.Context, annulmentID string, at time.Time) (bool, error) {
	res, err := r.db.Pool.ExecContext(ctx, `
		UPDATE invoicing_annulments SET
			notice_state = 'sent',
			notice_sent_at = $2,
			notice_next_attempt_at = NULL,
			notice_leased_until = NULL,
			notice_attempts = notice_attempts + 1
		WHERE id = $1 AND notice_state = 'owed'
	`, annulmentID, at)
	if err != nil {
		return false, fmt.Errorf("mark annulment notice sent: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("mark annulment notice sent: %w", err)
	}
	return n == 1, nil
}

// RescheduleAnnulmentNotice records a refused send: the notice stays owed,
// due again at next, and the claim is freed.
func (r *Repository) RescheduleAnnulmentNotice(ctx context.Context, annulmentID string, next time.Time) error {
	if _, err := r.db.Pool.ExecContext(ctx, `
		UPDATE invoicing_annulments SET
			notice_next_attempt_at = $2,
			notice_leased_until = NULL,
			notice_attempts = notice_attempts + 1
		WHERE id = $1 AND notice_state = 'owed'
	`, annulmentID, next); err != nil {
		return fmt.Errorf("reschedule annulment notice: %w", err)
	}
	return nil
}
