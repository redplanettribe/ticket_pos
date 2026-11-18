package repository

import (
	"context"
	"database/sql"
	"time"
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

// PlatformTicketsSoldDay is one (day, Sales Channel) cell of the platform's
// Tickets Sold: how many tickets every Organization together sold on that
// channel on that day. Day is a calendar date on the clock the caller named,
// formatted YYYY-MM-DD, for the reason SalesTrendBucket's is.
type PlatformTicketsSoldDay struct {
	Day      string
	Channel  string
	Quantity int
}

// PlatformTicketsSoldByDay totals every active Ticket Sale Line on the
// platform per day and Sales Channel, for Platform Trends (#810, ADR 0080).
// It is the platform-wide sibling of SalesTrends and keeps its rules: the
// quantity of active sales only, bucketed by sold_at (the day the sale was
// MADE, so a Sale Import lands on its historical day and not on the day it
// was typed in), with no channel filter and no Organization filter - House
// Organizations included, so the figure agrees with every other platform
// total.
//
// The day is read on the zone the caller passes, which is the platform's own
// clock and never an Event's: a sale belongs to the platform day it happened
// on, wherever its Event is held. Only sales sold in [from, until) are read;
// the caller bounds that window to whole days on the same clock.
//
// No index is added for it. The read is operator-only and bounded by the
// span, and a platform-wide window over ticket_sales is a large fraction of
// the table for any span worth reading, which a sequential scan serves as
// well as an index would; the figure is read live, as Sales Trends is, with
// no rollup table.
func (r *Repository) PlatformTicketsSoldByDay(ctx context.Context, from, until time.Time, timezone string) ([]PlatformTicketsSoldDay, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT
			to_char((ts.sold_at AT TIME ZONE $3)::date, 'YYYY-MM-DD') AS day,
			ts.channel,
			SUM(tsl.quantity)::bigint
		FROM ticket_sales ts
		JOIN ticket_sale_lines tsl ON tsl.ticket_sale_id = ts.id
		WHERE ts.status = 'active' AND ts.sold_at >= $1 AND ts.sold_at < $2
		GROUP BY 1, 2
		ORDER BY 1, 2
	`, from, until, timezone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PlatformTicketsSoldDay
	for rows.Next() {
		var day PlatformTicketsSoldDay
		if err := rows.Scan(&day.Day, &day.Channel, &day.Quantity); err != nil {
			return nil, err
		}
		out = append(out, day)
	}
	return out, rows.Err()
}

// PlatformFirstTicketSoldDay is the day, on the clock the caller names, the
// platform's earliest Ticket Sale was sold, formatted YYYY-MM-DD, and false
// when there is none: the first day Platform Trends' Tickets Sold has data,
// before which a comparison has no history to read (#811). It reads sold_at
// over every Organization as PlatformTicketsSoldByDay does, but counts
// reversed sales too: the platform was recording sales from that day, so
// reversing the earliest one does not turn the days after it from a measured
// zero into missing history. Like its sibling it adds no index: it is one
// aggregate over the table, read by operators alone.
func (r *Repository) PlatformFirstTicketSoldDay(ctx context.Context, timezone string) (string, bool, error) {
	var day sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT to_char((MIN(sold_at) AT TIME ZONE $1)::date, 'YYYY-MM-DD')
		FROM ticket_sales
	`, timezone).Scan(&day)
	if err != nil {
		return "", false, err
	}
	return day.String, day.Valid, nil
}
