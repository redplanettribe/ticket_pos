package repository

import (
	"context"
	"database/sql"
	"time"
)

// OrganizationBalance is the money side of an Organization: what its active
// Online Sales have left it once the Platform Fee and Fee IVA were withheld, and
// what the platform has already settled against that (ADR 0014). The two are
// kept apart here so the service, not SQL, states that their difference is the
// Withdrawable Balance.
type OrganizationBalance struct {
	Currency         string
	NetProceedsCents int
	PaidOutCents     int
}

// PayoutRow is one recorded Payout. PaidAt is a calendar day, not an instant:
// the column is a DATE, and the time part it scans as is meaningless.
type PayoutRow struct {
	ID          string
	AmountCents int
	PaidAt      time.Time
	Note        *string
}

// GetOrganizationBalance sums the Organization's Net Proceeds and its recorded
// Payouts.
//
// Net Proceeds per Ticket Sale Line is quantity × (unit_price_cents − fee_cents
// − fee_iva_cents), read straight off the line's snapshot: the snapshot is what
// the sale was actually transacted under, so this neither branches on the
// Event's Fee Handling nor re-derives anything from the configured rates. Only
// `active` sales on the `online` channel count — a reversed sale has left no
// money behind, and the platform never held the money from the other channels
// (their lines carry no withholding, so leaving them in would credit the
// Organization with cash it collected itself).
func (r *Repository) GetOrganizationBalance(ctx context.Context, orgID string) (OrganizationBalance, error) {
	var b OrganizationBalance
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT
			org.currency,
			COALESCE((
				SELECT SUM(`+lineNetProceedsSQL+`)
				FROM ticket_sale_lines tsl
				JOIN ticket_sales ts ON ts.id = tsl.ticket_sale_id
				WHERE ts.organization_id = org.id
				  AND ts.channel = 'online'
				  AND ts.status = 'active'
			), 0),
			COALESCE((
				SELECT SUM(p.amount_cents) FROM payouts p WHERE p.organization_id = org.id
			), 0)
		FROM organizations org
		WHERE org.id = $1
	`, orgID).Scan(&b.Currency, &b.NetProceedsCents, &b.PaidOutCents)
	return b, err
}

// ListPayouts returns the Organization's recorded Payouts, newest first.
func (r *Repository) ListPayouts(ctx context.Context, orgID string) ([]PayoutRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT id, amount_cents, paid_at, note
		FROM payouts
		WHERE organization_id = $1
		ORDER BY paid_at DESC, id DESC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PayoutRow
	for rows.Next() {
		var p PayoutRow
		var note sql.NullString
		if err := rows.Scan(&p.ID, &p.AmountCents, &p.PaidAt, &note); err != nil {
			return nil, err
		}
		if note.Valid {
			p.Note = &note.String
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
