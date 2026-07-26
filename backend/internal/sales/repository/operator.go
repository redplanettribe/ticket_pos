package repository

import (
	"context"
	"database/sql"
	"time"
)

// orgMoneySQL is every Organization's money in one row each: what the platform
// has withheld from it (Platform Fee and Fee IVA, accumulated from the amounts
// each active Online Sale line snapshotted) and its Withdrawable Balance (Net
// Proceeds minus recorded Payouts).
//
// Net Proceeds is lineNetProceedsSQL, the single definition the Event summary
// and the Organization's own payouts surface already read, so the operator's
// figures cannot drift from the ones the Organization is shown (ADR 0014). The
// channel and status filters are load-bearing in the same way they are there:
// only money the platform actually held and still holds counts.
//
// The balance is signed — a sale reversed after it was paid out leaves the
// Organization owing the platform — and callers decide what to do with that.
const orgMoneySQL = `
	SELECT
		org.id AS organization_id,
		org.currency AS currency,
		COALESCE(lines.fee_cents, 0) AS fee_cents,
		COALESCE(lines.fee_iva_cents, 0) AS fee_iva_cents,
		COALESCE(lines.net_proceeds_cents, 0) - COALESCE(paid.paid_cents, 0) AS balance_cents
	FROM organizations org
	LEFT JOIN LATERAL (
		SELECT
			SUM(` + lineNetProceedsSQL + `) AS net_proceeds_cents,
			SUM(tsl.quantity * tsl.fee_cents) AS fee_cents,
			SUM(tsl.quantity * tsl.fee_iva_cents) AS fee_iva_cents
		FROM ticket_sale_lines tsl
		JOIN ticket_sales ts ON ts.id = tsl.ticket_sale_id
		WHERE ts.organization_id = org.id
		  AND ts.channel = 'online'
		  AND ts.status = 'active'
	) lines ON TRUE
	LEFT JOIN LATERAL (
		SELECT SUM(p.amount_cents) AS paid_cents
		FROM payouts p
		WHERE p.organization_id = org.id
	) paid ON TRUE
`

// CurrencyTotalsRow is the platform's own money in one currency: what it has
// earned in Platform Fees and Fee IVA, and what it currently owes.
type CurrencyTotalsRow struct {
	Currency         string
	PlatformFeeCents int
	FeeIVACents      int
	TotalOwedCents   int
}

// OperatorPayoutRow is one recorded Payout with its audit trail: who entered it
// and when the row was written, alongside the bare fact it records. RecordedBy
// is null for Payouts entered directly in the database before the Operator
// Dashboard existed (ADR 0015).
type OperatorPayoutRow struct {
	ID          string
	AmountCents int
	PaidAt      time.Time
	Note        *string
	RecordedBy  *string
	CreatedAt   time.Time
}

// BalancesByOrganizationIDs returns each given Organization's Withdrawable
// Balance, signed. Organizations with neither sales nor Payouts come back as 0
// rather than missing, so the caller never has to tell "no data" from "zero".
func (r *Repository) BalancesByOrganizationIDs(ctx context.Context, orgIDs []string) (map[string]int, error) {
	balances := make(map[string]int, len(orgIDs))
	if len(orgIDs) == 0 {
		return balances, nil
	}

	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT organization_id, balance_cents
		FROM (`+orgMoneySQL+`) money
		WHERE organization_id = ANY($1)
	`, orgIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var orgID string
		var balance int
		if err := rows.Scan(&orgID, &balance); err != nil {
			return nil, err
		}
		balances[orgID] = balance
	}
	return balances, rows.Err()
}

// PlatformCurrencyTotals aggregates the platform's money one row per currency,
// with no FX conversion anywhere: currencies are never added together, so the
// numbers stay honest the day a second one appears (ADR 0015).
//
// Total owed sums only POSITIVE balances. A negative balance is an Organization
// owing the platform after a post-settlement reversal, and netting it off would
// understate the cash the platform must keep on hand to settle everyone else.
func (r *Repository) PlatformCurrencyTotals(ctx context.Context) ([]CurrencyTotalsRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT
			currency,
			SUM(fee_cents),
			SUM(fee_iva_cents),
			SUM(GREATEST(balance_cents, 0))
		FROM (`+orgMoneySQL+`) money
		GROUP BY currency
		ORDER BY currency ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CurrencyTotalsRow
	for rows.Next() {
		var t CurrencyTotalsRow
		if err := rows.Scan(&t.Currency, &t.PlatformFeeCents, &t.FeeIVACents, &t.TotalOwedCents); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListPayoutsWithRecorder returns an Organization's recorded Payouts newest
// first, carrying the recorder for the operator's reconciliation view.
func (r *Repository) ListPayoutsWithRecorder(ctx context.Context, orgID string) ([]OperatorPayoutRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT id, amount_cents, paid_at, note, recorded_by, created_at
		FROM payouts
		WHERE organization_id = $1
		ORDER BY paid_at DESC, id DESC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []OperatorPayoutRow
	for rows.Next() {
		p, err := scanOperatorPayout(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// InsertPayout records one Payout against an Organization and returns it.
//
// There is no balance check here or anywhere above it: by the time an operator
// types the amount the money has already left the bank, and refusing to record
// reality would corrupt the ledger. An over-balance Payout simply drives the
// Withdrawable Balance negative, which is what it means (ADR 0015).
func (r *Repository) InsertPayout(
	ctx context.Context,
	orgID string,
	amountCents int,
	paidAt time.Time,
	note *string,
	recordedBy string,
) (OperatorPayoutRow, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		INSERT INTO payouts (organization_id, amount_cents, paid_at, note, recorded_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, amount_cents, paid_at, note, recorded_by, created_at
	`, orgID, amountCents, paidAt, note, recordedBy)
	return scanOperatorPayout(row)
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanOperatorPayout(row rowScanner) (OperatorPayoutRow, error) {
	var p OperatorPayoutRow
	var note, recordedBy sql.NullString
	if err := row.Scan(&p.ID, &p.AmountCents, &p.PaidAt, &note, &recordedBy, &p.CreatedAt); err != nil {
		return OperatorPayoutRow{}, err
	}
	if note.Valid {
		p.Note = &note.String
	}
	if recordedBy.Valid {
		p.RecordedBy = &recordedBy.String
	}
	return p, nil
}
