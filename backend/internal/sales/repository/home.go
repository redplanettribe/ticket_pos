package repository

import (
	"context"
	"time"
)

// OrganizationSales is what one Organization's active Ticket Sales sold over
// a window: their Tickets Sold and their Takings, in Currency, the
// Organization's.
type OrganizationSales struct {
	Currency     string
	TicketsSold  int
	TakingsCents int
}

// OrganizationSalesBetween totals one Organization's active Ticket Sales sold
// in [from, until), for Home's today so far (#859, ADR 0081).
//
// It keeps Sales Trends' rules: sales are read by sold_at, the instant the
// sale was MADE, so a backdated Sale Import lands on its own day; reversed
// sales are left out; and the money is Takings, the single-sourced per-line
// expression with no channel filter (ADR 0040). The window is one of
// instants, which the caller bounds to the platform day.
//
// The Organization row is read even when it sold nothing, so the currency is
// always stated. No index is added: the read is bounded by the Organization
// and by a day, over ticket_sales(organization_id, ...).
func (r *Repository) OrganizationSalesBetween(ctx context.Context, orgID string, from, until time.Time) (OrganizationSales, error) {
	var out OrganizationSales
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT
			org.currency,
			COALESCE(sold.tickets, 0)::bigint,
			COALESCE(sold.takings, 0)::bigint
		FROM organizations org
		LEFT JOIN LATERAL (
			SELECT SUM(tsl.quantity) AS tickets, SUM(`+lineNetProceedsSQL+`) AS takings
			FROM ticket_sales ts
			JOIN ticket_sale_lines tsl ON tsl.ticket_sale_id = ts.id
			WHERE ts.organization_id = org.id AND ts.status = 'active'
			  AND ts.sold_at >= $2 AND ts.sold_at < $3
		) sold ON TRUE
		WHERE org.id = $1
	`, orgID, from, until).Scan(&out.Currency, &out.TicketsSold, &out.TakingsCents)
	return out, err
}

// EventTicketsSold is one Event's Tickets Sold so far: the quantities of its
// active Ticket Sales on every Sales Channel, scoped to the Organization so
// another Organization's Event reads as zero.
func (r *Repository) EventTicketsSold(ctx context.Context, orgID, eventID string) (int, error) {
	var sold int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(tsl.quantity), 0)::bigint
		FROM ticket_sales ts
		JOIN ticket_sale_lines tsl ON tsl.ticket_sale_id = ts.id
		WHERE ts.organization_id = $1 AND ts.event_id = $2 AND ts.status = 'active'
	`, orgID, eventID).Scan(&sold)
	return sold, err
}
