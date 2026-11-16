package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// The Annulment Check's queue and its asks (#746, spec #742, ADR 0078,
// migration 132). The check lives on the annulment it is about: its verdict,
// the instant that verdict was reached, the next scheduled ask and a claim
// lease, with one row per ask on invoicing_annulment_asks. The verdict is
// never written from outside: it is recomputed by the one pure rule
// (invoicing.AnnulmentVerdictOf) over every ask, under a row lock, each time
// an ask is recorded, so a scheduled ask and an operator's Check status that
// land together cannot lose one another.

// AnnulmentCheckClaim is one annulment's check a tick has claimed: the
// annulment and its factura, read in full for the ask.
type AnnulmentCheckClaim struct {
	AnnulmentID string
	Invoice     *InvoiceRow
}

// ClaimDueAnnulmentCheck takes ONE annulment whose next ask is due at now
// and hides it from every other tick until leaseUntil, or returns nil when
// none is due. Only a standing annulment (never reinstated) of a factura
// still annulled, whose check exists and is not confirmed, is claimable: an
// annulment recorded before the check existed has no next ask and is never
// claimed. SKIP LOCKED and the lease together are what keep two concurrent
// ticks from asking about one factura twice.
func (r *Repository) ClaimDueAnnulmentCheck(ctx context.Context, now, leaseUntil time.Time) (*AnnulmentCheckClaim, error) {
	var claim AnnulmentCheckClaim
	var invoiceID string
	err := r.db.Pool.QueryRowContext(ctx, `
		UPDATE invoicing_annulments
		SET check_leased_until = $2
		WHERE id = (
			SELECT a.id
			FROM invoicing_annulments a
			JOIN invoicing_invoices i ON i.id = a.invoice_id
			WHERE a.next_ask_at <= $1
			  AND a.reinstated_at IS NULL
			  AND a.check_verdict IN ('checking', 'mismatch', 'unconfirmed')
			  AND (a.check_leased_until IS NULL OR a.check_leased_until <= $1)
			  AND i.status = 'annulled'
			ORDER BY a.next_ask_at, a.id
			FOR UPDATE OF a SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, invoice_id
	`, now, leaseUntil).Scan(&claim.AnnulmentID, &invoiceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim due annulment check: %w", err)
	}
	row, err := r.GetInvoice(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("claim due annulment check: invoice %s not found", invoiceID)
	}
	claim.Invoice = row
	return &claim, nil
}

// ReleaseAnnulmentCheck gives a claimed check back to the queue untouched:
// its next ask stays due, for the next tick. For an ask the tick's budget
// cut short, which is no ask at all.
func (r *Repository) ReleaseAnnulmentCheck(ctx context.Context, annulmentID string) error {
	if _, err := r.db.Pool.ExecContext(ctx, `
		UPDATE invoicing_annulments SET check_leased_until = NULL WHERE id = $1
	`, annulmentID); err != nil {
		return fmt.Errorf("release annulment check: %w", err)
	}
	return nil
}

// AnnulmentAskRecord is one finished ask to record: what it read, when, in
// how many calls, and whether it was the schedule's (which moves the next
// ask and frees the claim) or an operator's Check status (which does
// neither). WindowClosesAt is the factura's Annulment Window's close, which
// the schedule's end is the later of.
type AnnulmentAskRecord struct {
	Ask            invoicing.AnnulmentAsk
	Calls          int
	WindowClosesAt time.Time
}

// RecordAnnulmentAsk writes one ask on an annulment's check and recomputes
// the verdict over every ask, in one transaction under the annulment's row
// lock, and returns the check as it then stands. A scheduled ask sets the
// next ask by the schedule (invoicing.NextAnnulmentAsk) and frees the
// tick's claim; a manual one leaves both alone. A confirmed check asks
// nothing more, whoever confirmed it. An annulment with no check (recorded
// before it existed) or already reinstated records nothing and answers nil.
func (r *Repository) RecordAnnulmentAsk(ctx context.Context, annulmentID string, rec AnnulmentAskRecord) (*invoicing.AnnulmentCheck, error) {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin record annulment ask: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var annulledAt time.Time
	var verdict sql.NullString
	var nextAskAt sql.NullTime
	var reinstated bool
	if err := tx.QueryRowContext(ctx, `
		SELECT annulled_at, check_verdict, next_ask_at, reinstated_at IS NOT NULL
		FROM invoicing_annulments WHERE id = $1 FOR UPDATE
	`, annulmentID).Scan(&annulledAt, &verdict, &nextAskAt, &reinstated); err != nil {
		return nil, fmt.Errorf("lock annulment: %w", err)
	}
	if !verdict.Valid || reinstated {
		return nil, nil
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO invoicing_annulment_asks (annulment_id, asked_at, reading, manual, calls)
		VALUES ($1, $2, $3, $4, $5)
	`, annulmentID, rec.Ask.At, rec.Ask.Reading, rec.Ask.Manual, rec.Calls); err != nil {
		return nil, fmt.Errorf("record annulment ask: %w", err)
	}
	asks, err := annulmentAsks(ctx, tx, annulmentID)
	if err != nil {
		return nil, err
	}
	state := invoicing.AnnulmentVerdictOf(annulledAt, asks)
	check := &invoicing.AnnulmentCheck{Verdict: state.Verdict, Asks: asks}
	if !state.Since.IsZero() {
		since := state.Since
		check.Since = &since
	}
	switch {
	case state.Verdict == invoicing.AnnulmentConfirmed:
		check.NextAskAt = nil
	case rec.Ask.Manual:
		if nextAskAt.Valid {
			next := nextAskAt.Time
			check.NextAskAt = &next
		}
	default:
		if next, ok := invoicing.NextAnnulmentAsk(annulledAt, rec.WindowClosesAt, state.Verdict, rec.Ask.At); ok {
			check.NextAskAt = &next
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE invoicing_annulments SET
			check_verdict = $2,
			check_verdict_at = $3,
			next_ask_at = $4,
			check_leased_until = CASE WHEN $5 THEN check_leased_until ELSE NULL END,
			asks_made = asks_made + 1
		WHERE id = $1
	`, annulmentID, state.Verdict, check.Since, check.NextAskAt, rec.Ask.Manual); err != nil {
		return nil, fmt.Errorf("update annulment check: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit annulment ask: %w", err)
	}
	return check, nil
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// annulmentAsks reads one annulment's asks, oldest first.
func annulmentAsks(ctx context.Context, q queryer, annulmentID string) ([]invoicing.AnnulmentAsk, error) {
	byID, err := annulmentAsksOf(ctx, q, []string{annulmentID})
	if err != nil {
		return nil, err
	}
	return byID[annulmentID], nil
}

// annulmentAsksOf reads the asks of several annulments, oldest first, keyed
// by annulment.
func annulmentAsksOf(ctx context.Context, q queryer, annulmentIDs []string) (map[string][]invoicing.AnnulmentAsk, error) {
	out := map[string][]invoicing.AnnulmentAsk{}
	if len(annulmentIDs) == 0 {
		return out, nil
	}
	rows, err := q.QueryContext(ctx, `
		SELECT annulment_id, asked_at, reading, manual
		FROM invoicing_annulment_asks WHERE annulment_id = ANY($1::uuid[])
		ORDER BY asked_at, id
	`, annulmentIDs)
	if err != nil {
		return nil, fmt.Errorf("annulment asks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var a invoicing.AnnulmentAsk
		if err := rows.Scan(&id, &a.At, &a.Reading, &a.Manual); err != nil {
			return nil, fmt.Errorf("scan annulment ask: %w", err)
		}
		out[id] = append(out[id], a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("annulment asks: %w", err)
	}
	return out, nil
}

// AnnulmentMismatchSince is, for each of the given facturas whose standing
// annulment's check is at mismatch, the instant the mismatch was reached:
// what the needs-attention queue shows as "since" for an entry the
// mismatch put there.
func (r *Repository) AnnulmentMismatchSince(ctx context.Context, invoiceIDs []string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	if len(invoiceIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT invoice_id, check_verdict_at FROM invoicing_annulments
		WHERE invoice_id = ANY($1::uuid[]) AND reinstated_at IS NULL AND check_verdict = 'mismatch'
	`, invoiceIDs)
	if err != nil {
		return nil, fmt.Errorf("annulment mismatch since: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, fmt.Errorf("scan annulment mismatch: %w", err)
		}
		out[id] = at
	}
	return out, rows.Err()
}
