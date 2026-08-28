package repository

import (
	"context"
	"fmt"
	"time"
)

// The Certificate Expiry Warning's ledger (#503, ADR 0063 §3): which rungs of
// the 30/7/1/0 ladder have been fired for which certificate, keyed on the
// certificate's SHA-256 fingerprint. Migration 103 says at length what this
// table is and is not; in one line, it exists to say no, and it is never
// read for a count of mail.

// FiredCertificateExpiryThresholds reads the rungs already fired for the
// certificate with this fingerprint. A certificate never warned about — a
// fresh upload — has none, which is how a re-upload restarts the ladder.
func (r *Repository) FiredCertificateExpiryThresholds(ctx context.Context, fingerprint string) ([]int, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT threshold_days FROM certificate_expiry_notices WHERE fingerprint_sha256 = $1
	`, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("read certificate expiry notices: %w", err)
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var threshold int
		if err := rows.Scan(&threshold); err != nil {
			return nil, fmt.Errorf("scan certificate expiry notice: %w", err)
		}
		out = append(out, threshold)
	}
	return out, rows.Err()
}

// RecordCertificateExpiryNotice writes that one fan-out fired these rungs
// for one certificate — the rung mailed and every rung below it the day
// count had reached, which that mail covers — after the fan-out and only
// once at least one address accepted the mail (ADR 0063 §3). A row already
// there — two ticks that overlapped, or a rung reached again after a paused
// scheduler — is left as it is: the rung is fired either way, and the first
// tick's count is as true as the second's.
func (r *Repository) RecordCertificateExpiryNotice(ctx context.Context, fingerprint string, thresholds []int, sentAt time.Time, recipientCount int) error {
	for _, threshold := range thresholds {
		if _, err := r.db.Pool.ExecContext(ctx, `
			INSERT INTO certificate_expiry_notices (fingerprint_sha256, threshold_days, sent_at, recipient_count)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (fingerprint_sha256, threshold_days) DO NOTHING
		`, fingerprint, threshold, sentAt, recipientCount); err != nil {
			return fmt.Errorf("record certificate expiry notice: %w", err)
		}
	}
	return nil
}
