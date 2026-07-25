package sales

import (
	"database/sql"
	"time"
)

// Capacity Holds are derived from pending Payments, not stored (ADR 0013): a
// pending Payment younger than the hold window IS the hold on its Ticket Types.
// This file is the single definition of that derivation — the window and the
// query that turns pending Payments into held quantities — shared by the sales
// module (begin-checkout and the sale-commit spine) and the catalog module
// (public remaining/sold-out figures). Expiry is nothing but the created_at
// cutoff in the WHERE clause; the lazy status flip to 'expired' is bookkeeping
// that no correctness depends on.

// CapacityHoldWindow is how long a pending Payment holds its quantities against
// Ticket Type capacity. It comfortably covers PayPhone's 10-minute payment-form
// validity plus its 5-minute confirm window (ADR 0013). It bounds only the
// hold, never the Payment's validity: a confirm arriving after the window may
// still commit if capacity remains.
const CapacityHoldWindow = 20 * time.Minute

// HoldCutoff returns the moment before which a pending Payment no longer holds
// capacity: payments with created_at at or before the cutoff hold nothing.
func HoldCutoff(now time.Time) time.Time {
	return now.Add(-CapacityHoldWindow)
}

// LiveHoldsSQL builds the one query that turns pending Payments into Capacity
// Holds: SUM(payment_lines.quantity) per ticket_type_id over payments with
// status = 'pending' AND created_at strictly after the cutoff. Each argument is
// a compile-time SQL expression chosen by the caller — a placeholder like "$2"
// or a correlated column reference — never user input:
//
//   - cutoffExpr (required): the hold-window cutoff timestamp.
//   - eventExpr (optional, "" omits): narrows to one Event's payments.
//   - excludePaymentExpr (optional, "" omits): a payments.id to leave out — the
//     Payment whose own commit is running, so its hold converts into sold_count
//     rather than double-counting against itself.
//
// The (status, created_at) index on payments serves this shape (migration 019).
// ScanHeldQuantities collects the (ticket_type_id, held) rows a LiveHoldsSQL
// query produces into a map; Ticket Types with no live hold are absent. It
// closes the rows. Kept beside the query so every reader of the derivation
// scans it the same way.
func ScanHeldQuantities(rows *sql.Rows) (map[string]int, error) {
	defer rows.Close()
	held := map[string]int{}
	for rows.Next() {
		var id string
		var qty int
		if err := rows.Scan(&id, &qty); err != nil {
			return nil, err
		}
		held[id] = qty
	}
	return held, rows.Err()
}

func LiveHoldsSQL(cutoffExpr, eventExpr, excludePaymentExpr string) string {
	extra := ""
	if eventExpr != "" {
		extra += "\n\t\t  AND p.event_id = " + eventExpr
	}
	if excludePaymentExpr != "" {
		extra += "\n\t\t  AND p.id <> " + excludePaymentExpr
	}
	return `
		SELECT pl.ticket_type_id, SUM(pl.quantity)::int AS held
		FROM payment_lines pl
		JOIN payments p ON p.id = pl.payment_id
		WHERE p.status = 'pending'
		  AND p.created_at > ` + cutoffExpr + extra + `
		GROUP BY pl.ticket_type_id
	`
}
