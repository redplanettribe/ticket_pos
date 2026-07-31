package repository

import (
	"context"
	"database/sql"
	"time"
)

// OrganizationBalance is the money side of an Organization: what its active
// Online Sales have left it once the Platform Fee and Fee IVA were withheld, and
// what the platform has already settled against that (ADR 0014). The terms are
// kept apart here so the service, not SQL, states which differences are the
// Withdrawable Balance and the Payable Balance.
type OrganizationBalance struct {
	Currency         string
	NetProceedsCents int
	// ClearedNetProceedsCents is the same sum over the subset of those sales
	// that have cleared: recorded before today in Ecuador, with no Reversal
	// Request still open on them (ADR 0026). A subset, so it never exceeds
	// NetProceedsCents — which is what makes the Payable Balance never exceed
	// the Withdrawable Balance, whatever the Payouts are.
	ClearedNetProceedsCents int
	PaidOutCents            int
}

// PayoutRow is one recorded Payout. PaidAt is a calendar day, not an instant:
// the column is a DATE, and the time part it scans as is meaningless.
type PayoutRow struct {
	ID          string
	AmountCents int
	PaidAt      time.Time
	Note        *string
}

// GetOrganizationBalance sums the Organization's Net Proceeds twice — over all
// its active Online Sales, and over the subset that has cleared — alongside its
// recorded Payouts.
//
// Net Proceeds per Ticket Sale Line is quantity × (unit_price_cents − fee_cents
// − fee_iva_cents), read straight off the line's snapshot: the snapshot is what
// the sale was actually transacted under, so this neither branches on the
// Event's Fee Handling nor re-derives anything from the configured rates. Only
// `active` sales on the `online` channel count — a reversed sale has left no
// money behind, and the platform never held the money from the other channels
// (their lines carry no withholding, so leaving them in would credit the
// Organization with cash it collected itself).
//
// The cleared figure is the SAME expression under a FILTER rather than a second
// query, deliberately. Two sums that must be orderable — Payable ≤ Withdrawable,
// always — are one sum with a narrower predicate or they are two things that
// drift, and the fee arithmetic in particular is never restated anywhere
// (ADR 0014).
//
// clearedBefore is the instant "today in Ecuador" began, and it is a PARAMETER
// rather than NOW() or CURRENT_DATE for a reason worth defending: the boundary
// is computed in Go from the service's injected clock (ADR 0026). SQL asking the
// database for the time would ignore that clock entirely, which would make the
// day rollover — the whole of this rule — untestable at the seam the rule is
// stated at, and would put the answer at the mercy of the database session's
// timezone besides. A future reader tidying this into NOW() would delete the
// only evidence the feature works.
//
// A sale clears when it was recorded before that instant AND carries no live
// Reversal Request. `status <> 'refused'` is the schema's own definition of a
// live request (migration 039) and is quoted rather than narrowed: a refusal
// means nothing happened and the sale was never in doubt, while every other
// state — in flight, succeeded, or an Unresolved Reversal awaiting an operator —
// is an open question about whose money it is, and open questions are not
// payable. The clock condition needs no companion check on the Reversal Window:
// the window shuts at 20:00 Ecuador time on the date of purchase at the latest
// (ADR 0018), so a sale recorded before today is one whose buyer can no longer
// undo it, and the settlement lag subsumes the window rather than sitting beside
// it.
//
// The anchor is ticket_sales.created_at and there is no join to `payments`. A
// Ticket Sale exists only for an approved Payment, so its own creation instant
// IS the approval instant; `payments` carries no approval timestamp to join to
// anyway; and a free Online Sale never had a provider at all (ADR 0017), so a
// join would drop it rather than clear it.
func (r *Repository) GetOrganizationBalance(ctx context.Context, orgID string, clearedBefore time.Time) (OrganizationBalance, error) {
	var b OrganizationBalance
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT
			org.currency,
			COALESCE(sales.net_proceeds_cents, 0),
			COALESCE(sales.cleared_net_proceeds_cents, 0),
			COALESCE((
				SELECT SUM(p.amount_cents) FROM payouts p WHERE p.organization_id = org.id
			), 0)
		FROM organizations org
		LEFT JOIN LATERAL (
			SELECT
				SUM(`+lineNetProceedsSQL+`) AS net_proceeds_cents,
				SUM(`+lineNetProceedsSQL+`) FILTER (
					WHERE ts.created_at < $2
					  AND NOT EXISTS (
						SELECT 1 FROM sale_reversals sr
						WHERE sr.ticket_sale_id = ts.id AND sr.status <> 'refused'
					)
				) AS cleared_net_proceeds_cents
			FROM ticket_sale_lines tsl
			JOIN ticket_sales ts ON ts.id = tsl.ticket_sale_id
			WHERE ts.organization_id = org.id
			  AND ts.channel = 'online'
			  AND ts.status = 'active'
		) sales ON TRUE
		WHERE org.id = $1
	`, orgID, clearedBefore).Scan(&b.Currency, &b.NetProceedsCents, &b.ClearedNetProceedsCents, &b.PaidOutCents)
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
