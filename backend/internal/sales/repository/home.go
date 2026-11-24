package repository

import (
	"context"
	"database/sql"
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

// OrganizationSalesDay is one (day, Sales Channel) cell of an Organization's
// active Ticket Sales: their Tickets Sold, how many Ticket Sales they are,
// and their Takings in the Organization's minor units. Day is a calendar
// date on the clock the caller named, YYYY-MM-DD.
type OrganizationSalesDay struct {
	Day          string
	Channel      string
	TicketsSold  int
	TicketSales  int
	TakingsCents int
}

// OrganizationSalesByDay totals one Organization's active Ticket Sales sold in
// [from, until) per day and Sales Channel, for Organization Trends (#861, ADR
// 0081).
//
// It keeps Sales Trends' rules, as OrganizationSalesBetween does: by sold_at,
// the instant the sale was MADE, so a backdated Sale Import lands on its own
// day; reversed sales left out, live; and the money is Takings, the
// single-sourced per-line expression with no channel filter (ADR 0040, #862),
// so each channel's cell is what Sales Trends draws for it. The day is read
// on the zone the caller passes, the platform's own clock. Days that sold
// nothing have no row.
//
// No index is added: the read is bounded by the Organization and by the
// span, over ticket_sales(organization_id, ...), as Home now's is.
func (r *Repository) OrganizationSalesByDay(ctx context.Context, orgID string, from, until time.Time, timezone string) ([]OrganizationSalesDay, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT
			to_char((ts.sold_at AT TIME ZONE $4)::date, 'YYYY-MM-DD') AS day,
			ts.channel,
			SUM(tsl.quantity)::bigint,
			COUNT(DISTINCT ts.id)::bigint,
			COALESCE(SUM(`+lineNetProceedsSQL+`), 0)::bigint
		FROM ticket_sales ts
		JOIN ticket_sale_lines tsl ON tsl.ticket_sale_id = ts.id
		WHERE ts.organization_id = $1 AND ts.status = 'active'
		  AND ts.sold_at >= $2 AND ts.sold_at < $3
		GROUP BY 1, 2
		ORDER BY 1, 2
	`, orgID, from, until, timezone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []OrganizationSalesDay
	for rows.Next() {
		var day OrganizationSalesDay
		if err := rows.Scan(&day.Day, &day.Channel, &day.TicketsSold, &day.TicketSales, &day.TakingsCents); err != nil {
			return nil, err
		}
		out = append(out, day)
	}
	return out, rows.Err()
}

// OrganizationOrigin is where an Organization's history begins: when it was
// created, the instant its earliest Ticket Sale was made (nil when it has
// none), and its currency.
type OrganizationOrigin struct {
	Currency    string
	CreatedAt   time.Time
	FirstSoldAt *time.Time
}

// OrganizationOrigin reads where one Organization's history begins, for
// Organization Trends' all time span (#861). The earliest Ticket Sale is
// read whatever its status: a reversed sale was still made that day, and a
// Sale Import of last year's history reaches back to its own first sale.
func (r *Repository) OrganizationOrigin(ctx context.Context, orgID string) (OrganizationOrigin, error) {
	var out OrganizationOrigin
	var first sql.NullTime
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT org.currency, org.created_at,
			(SELECT MIN(ts.sold_at) FROM ticket_sales ts WHERE ts.organization_id = org.id)
		FROM organizations org
		WHERE org.id = $1
	`, orgID).Scan(&out.Currency, &out.CreatedAt, &first)
	if first.Valid {
		out.FirstSoldAt = &first.Time
	}
	return out, err
}

// OrganizationEventSales is what one Event of an Organization sold over a
// window: its Tickets Sold, how many Ticket Sales they are, and their Takings
// in the Organization's minor units.
type OrganizationEventSales struct {
	EventID      string
	EventName    string
	TicketsSold  int
	TicketSales  int
	TakingsCents int
}

// OrganizationSalesByEvent totals one Organization's active Ticket Sales sold
// in [from, until) per Event, for the breakdown of Organization Trends (#864):
// what a bucket of Tickets Sold, Ticket Sales or Takings was made of.
//
// It keeps OrganizationSalesByDay's rules over the same sales, so the Events
// of a window add up to what that read states for its days: by sold_at,
// reversed sales left out, every Sales Channel, Takings by the single-sourced
// per-line expression (ADR 0040). The window is one of instants, which the
// caller bounds to whole platform days, and nothing is grouped by day. An
// Event that sold nothing in the window is not returned.
func (r *Repository) OrganizationSalesByEvent(ctx context.Context, orgID string, from, until time.Time) ([]OrganizationEventSales, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT
			e.id, e.name,
			SUM(tsl.quantity)::bigint,
			COUNT(DISTINCT ts.id)::bigint,
			COALESCE(SUM(`+lineNetProceedsSQL+`), 0)::bigint
		FROM ticket_sales ts
		JOIN ticket_sale_lines tsl ON tsl.ticket_sale_id = ts.id
		JOIN events e ON e.id = ts.event_id
		WHERE ts.organization_id = $1 AND ts.status = 'active'
		  AND ts.sold_at >= $2 AND ts.sold_at < $3
		GROUP BY e.id
		ORDER BY e.id
	`, orgID, from, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []OrganizationEventSales
	for rows.Next() {
		var event OrganizationEventSales
		if err := rows.Scan(&event.EventID, &event.EventName, &event.TicketsSold, &event.TicketSales, &event.TakingsCents); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}
