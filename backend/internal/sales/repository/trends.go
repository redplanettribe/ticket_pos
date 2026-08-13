package repository

import (
	"context"
)

// SalesTrendBucket is one (day, Ticket Type) cell of the Sales Trends matrix:
// how many tickets of that type sold on that day, and what they earned the
// Organization. Day is a calendar date in the Event's timezone, formatted
// YYYY-MM-DD, because a day here is a civil date and not an instant — turning
// it into a time.Time would invite arithmetic that DST would then get wrong.
type SalesTrendBucket struct {
	Day          string
	TicketTypeID string
	Quantity     int
	TakingsCents int
}

// SalesTrends totals an Event's active Ticket Sale Lines per day and Ticket
// Type, ordered by day and then Ticket Type, for the Sales Trends surface.
//
// The money is TAKINGS (ADR 0040): the same single-sourced per-line expression
// the sales summary sums, WITH NO CHANNEL FILTER. That absence is deliberate and
// load-bearing. In-person and imported lines carry fee snapshots of zero, so the
// expression reduces to the line's full price on those channels — which is
// exactly what the Event made there, since the platform withheld nothing. The
// summary's `FILTER (WHERE ts.channel = 'online')` is right where it stands,
// because that surface answers "what will the platform hand over"; adding one
// here would answer that question again under the wrong name and would flatten
// the money chart of every door-heavy and import-heavy Event to zero. The
// expression is referenced rather than repeated so the two figures can never
// drift by a penny (ADR 0014).
//
// Days are bucketed by sold_at — the day the sale was MADE, never created_at,
// the day it was recorded — converted into the named timezone, so a backdated
// Sale Import lands on its historical days and a late-night sale falls on the
// day it felt like locally. The caller resolves that name (the Event's timezone,
// defaulting to UTC) through the same helper the Sales Export uses, and passes
// the resolved zone's name here so Go and Postgres cannot disagree about which
// day a sale is on.
//
// The access pattern needs no index of its own. EXPLAIN (ANALYZE) over a seeded
// 20,000-sale Event plans an index scan on ticket_sales(event_id, ...) joined to
// ticket_sale_lines(ticket_sale_id) and aggregates 19,600 rows in ~26ms; both
// sides are already covered, so this deliberately adds no migration. An Event
// large enough to change that shape would want the whole surface reconsidered
// rather than one more index.
//
// Reversed sales are excluded, as they are from every other aggregate. The read
// is bounded by the Event: it is a per-day aggregate, not a list, so there is no
// pagination to apply (ADR 0006 does not reach it).
func (r *Repository) SalesTrends(ctx context.Context, orgID, eventID, timezone string) ([]SalesTrendBucket, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT
			to_char((ts.sold_at AT TIME ZONE $3)::date, 'YYYY-MM-DD') AS day,
			tsl.ticket_type_id,
			SUM(tsl.quantity)::bigint,
			COALESCE(SUM(`+lineNetProceedsSQL+`), 0)::bigint
		FROM ticket_sales ts
		JOIN ticket_sale_lines tsl ON tsl.ticket_sale_id = ts.id
		WHERE ts.event_id = $1 AND ts.organization_id = $2 AND ts.status = 'active'
		GROUP BY 1, 2
		ORDER BY 1, 2
	`, eventID, orgID, timezone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SalesTrendBucket
	for rows.Next() {
		var bucket SalesTrendBucket
		if err := rows.Scan(&bucket.Day, &bucket.TicketTypeID, &bucket.Quantity, &bucket.TakingsCents); err != nil {
			return nil, err
		}
		out = append(out, bucket)
	}
	return out, rows.Err()
}
