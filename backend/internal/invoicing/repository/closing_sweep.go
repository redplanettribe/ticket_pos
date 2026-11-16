package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// The Closing Sweep's queue and the Unrecorded Annulment (#750, spec #742,
// ADR 0078, migration 135). The sweep's state lives on the invoice row: the
// month swept, the final reading and when it landed, the last ask, an
// operator's Sweep again and a claim lease. So the population, the tick's
// claim and the dashboard's unswept count are each one statement over one
// table, and can never disagree about which facturas a month holds.

// sweepPopulationSQL is THE SWEEP'S POPULATION for the month whose day 1 is
// bound at the given placeholder: production facturas (Sale Invoices and
// manual Tax Invoices) the platform holds as authorized, emitted in that
// month, credited or not. Test-environment facturas are never declared,
// and an annulled one is the Annulment Check's, so both are out.
func sweepPopulationSQL(month string) string {
	return `(
		` + sweepableSQL + `
		AND i.issued_on >= ` + month + `::date
		AND i.issued_on < (` + month + `::date + INTERVAL '1 month')
	)`
}

// sweepableSQL is the population of every month at once: the factura `i`
// is a production Sale Invoice or manual Tax Invoice, signed, held as
// authorized. The month's population narrows it by issued_on; the tick's
// claim reads it whole, since a Sweep again names no month of its own.
const sweepableSQL = `i.environment = 'production'
		AND i.kind IN ('sale', 'manual')
		AND i.status = 'authorized'
		AND i.signed_xml IS NOT NULL`

// dateOnly is a date-only value as the database's DATE reads it.
func dateOnly(t time.Time) string {
	return t.Format(time.DateOnly)
}

// ClaimDueSweep takes ONE factura due a sweep ask at now and hides it from
// every other tick until leaseUntil, or returns nil when none is due.
//
// Due is: not yet swept (no final reading), not asked since tickStart, so
// a no reading is asked again on a LATER tick and never twice in one, and
// either in the population of the month the sweep is running for (runMonth,
// nil outside days 8 to 12) or made due again by an operator's Sweep again
// and not asked since. Never-asked facturas go first, then the longest
// since their last ask. SKIP LOCKED and the lease are what keep two
// concurrent ticks from asking about one factura twice.
func (r *Repository) ClaimDueSweep(ctx context.Context, now, tickStart, leaseUntil time.Time, runMonth *time.Time) (*InvoiceRow, error) {
	var month sql.NullString
	if runMonth != nil {
		month = sql.NullString{String: dateOnly(*runMonth), Valid: true}
	}
	var id string
	err := r.db.Pool.QueryRowContext(ctx, `
		UPDATE invoicing_invoices
		SET sweep_leased_until = $3
		WHERE id = (
			SELECT i.id FROM invoicing_invoices i
			WHERE `+sweepableSQL+`
			  AND i.sweep_reading IS NULL
			  AND (i.sweep_leased_until IS NULL OR i.sweep_leased_until <= $1)
			  AND (i.sweep_asked_at IS NULL OR i.sweep_asked_at < $2)
			  AND (
			      ($4::date IS NOT NULL AND `+sweepPopulationSQL("$4")+`)
			      OR (i.sweep_again_at IS NOT NULL AND (i.sweep_asked_at IS NULL OR i.sweep_asked_at < i.sweep_again_at))
			  )
			ORDER BY i.sweep_asked_at ASC NULLS FIRST, i.issued_on, i.id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id
	`, now, tickStart, leaseUntil, month).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim due sweep: %w", err)
	}
	row, err := r.GetInvoice(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("claim due sweep: invoice %s not found", id)
	}
	return row, nil
}

// RecordSweepAsk writes one finished sweep ask on a claimed factura and
// frees its claim. A real reading is final: AUTORIZADO marks it swept, and
// ANULADO marks it swept and raises its Unrecorded Annulment with the call
// that read it. A no reading stores only that it was asked, so a later
// tick asks again. The write lands only on a factura still authorized and
// unswept; one an operator annulled under the ask is released untouched,
// and recorded reports false.
func (r *Repository) RecordSweepAsk(ctx context.Context, invoiceID string, at time.Time, reading invoicing.StatusReading, attemptID int64) (bool, error) {
	var attempt sql.NullInt64
	if attemptID != 0 {
		attempt = sql.NullInt64{Int64: attemptID, Valid: true}
	}
	var final sql.NullString
	if reading.Real() {
		final = sql.NullString{String: string(reading), Valid: true}
	}
	res, err := r.db.Pool.ExecContext(ctx, `
		UPDATE invoicing_invoices SET
			sweep_month = date_trunc('month', issued_on)::date,
			sweep_asked_at = $2,
			sweep_leased_until = NULL,
			sweep_reading = $3,
			swept_at = CASE WHEN $3::text IS NOT NULL THEN $2::timestamptz END,
			unrecorded_annulment_found_at = CASE WHEN $3::text = 'annulled' THEN $2::timestamptz END,
			unrecorded_annulment_attempt_id = CASE WHEN $3::text = 'annulled' THEN $4::bigint END
		WHERE id = $1 AND status = 'authorized' AND sweep_reading IS NULL
	`, invoiceID, at, final, attempt)
	if err != nil {
		return false, fmt.Errorf("record sweep ask: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("record sweep ask: %w", err)
	}
	if n == 1 {
		return true, nil
	}
	return false, r.ReleaseSweep(ctx, invoiceID)
}

// ReleaseSweep gives a claimed factura back to the queue untouched: for an
// ask the caller cut short, which is no ask at all.
func (r *Repository) ReleaseSweep(ctx context.Context, invoiceID string) error {
	if _, err := r.db.Pool.ExecContext(ctx, `
		UPDATE invoicing_invoices SET sweep_leased_until = NULL WHERE id = $1
	`, invoiceID); err != nil {
		return fmt.Errorf("release sweep: %w", err)
	}
	return nil
}

// UnsweptCount is how many facturas of one month's population have no
// reading, and how many of those an operator's Sweep again has made due and
// no tick has asked since.
type UnsweptCount struct {
	Unswept  int
	DueAgain int
}

// CountUnswept counts the month's population with no final reading: what
// the Operator Dashboard says couldn't be swept, once the month's run has
// ended.
func (r *Repository) CountUnswept(ctx context.Context, month time.Time) (UnsweptCount, error) {
	var out UnsweptCount
	if err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE i.sweep_again_at IS NOT NULL AND (i.sweep_asked_at IS NULL OR i.sweep_asked_at < i.sweep_again_at))
		FROM invoicing_invoices i
		WHERE `+sweepPopulationSQL("$1")+` AND i.sweep_reading IS NULL
	`, dateOnly(month)).Scan(&out.Unswept, &out.DueAgain); err != nil {
		return out, fmt.Errorf("count unswept: %w", err)
	}
	return out, nil
}

// SweepAgain makes every unswept factura of the month due again from the
// next tick, whatever the day, and answers how many. Just those: a factura
// with a reading is final and never asked again.
func (r *Repository) SweepAgain(ctx context.Context, month, at time.Time) (int, error) {
	res, err := r.db.Pool.ExecContext(ctx, `
		UPDATE invoicing_invoices i SET sweep_again_at = $2
		WHERE `+sweepPopulationSQL("$1")+` AND i.sweep_reading IS NULL
	`, dateOnly(month), at)
	if err != nil {
		return 0, fmt.Errorf("sweep again: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("sweep again: %w", err)
	}
	return int(n), nil
}

// markAnnulledRefusedSQL is whether Mark annulled is refused on the
// authorized factura `i` by what stands against it: a live Credit Note
// (standingCreditNoteSQL) or a live Reissue successor (liveSuccessorID).
// The two facts that make an acknowledgement, and not the record, the act
// an Unrecorded Annulment admits.
const markAnnulledRefusedSQL = `(` + standingCreditNoteSQL + ` IS NOT NULL OR ` + liveSuccessorID + ` IS NOT NULL)`

// AcknowledgeUnrecordedAnnulment records an operator's acknowledgement of
// a factura's Unrecorded Annulment: who and when. The flag stays, for the
// archive; the queue entry clears. GUARDED ON WHAT THE GATE READ: the
// factura still authorized, flagged, unacknowledged, and Mark annulled
// still refused on it, so a press racing a Credit Note's death or a second
// press finds no row to update and the service reads the row back to say
// which.
func (r *Repository) AcknowledgeUnrecordedAnnulment(ctx context.Context, invoiceID, by string, at time.Time) (bool, error) {
	res, err := r.db.Pool.ExecContext(ctx, `
		UPDATE invoicing_invoices i SET
			unrecorded_annulment_acknowledged_by = $2,
			unrecorded_annulment_acknowledged_at = $3
		WHERE i.id = $1 AND `+unrecordedAnnulmentSQL+` AND `+markAnnulledRefusedSQL+`
	`, invoiceID, by, at)
	if err != nil {
		return false, fmt.Errorf("acknowledge unrecorded annulment: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("acknowledge unrecorded annulment: %w", err)
	}
	return n == 1, nil
}

// MarkAnnulledRefused is, of the given facturas, those Mark annulled is
// refused on by a live Credit Note or a live Reissue successor: where the
// needs-attention queue offers the acknowledgement.
func (r *Repository) MarkAnnulledRefused(ctx context.Context, invoiceIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(invoiceIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT i.id FROM invoicing_invoices i
		WHERE i.id = ANY($1::uuid[]) AND `+markAnnulledRefusedSQL+`
	`, invoiceIDs)
	if err != nil {
		return nil, fmt.Errorf("mark annulled refused: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan mark annulled refused: %w", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}
