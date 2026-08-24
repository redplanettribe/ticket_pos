package repository

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// ViewBucket is one stored row of the Event's anonymous hourly traffic
// (ADR 0057): the UTC hour, the Affiliate Link it belongs to — nil for the
// Event's whole-page bucket — and nothing but a count.
type ViewBucket struct {
	Hour   time.Time
	LinkID *string
	Views  int64
}

// ViewBuckets returns every Page View and Click bucket the Event has, ordered
// by hour and then link, exactly as stored. The read is bounded by
// construction: growth is time × links, never traffic, so there is nothing to
// paginate (ADR 0057).
func (r *Repository) ViewBuckets(ctx context.Context, eventID string) ([]ViewBucket, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT hour, affiliate_link_id, view_count
		FROM event_page_views
		WHERE event_id = $1
		ORDER BY hour, affiliate_link_id NULLS FIRST
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	buckets := make([]ViewBucket, 0)
	for rows.Next() {
		var bucket ViewBucket
		if err := rows.Scan(&bucket.Hour, &bucket.LinkID, &bucket.Views); err != nil {
			return nil, err
		}
		bucket.Hour = bucket.Hour.UTC()
		buckets = append(buckets, bucket)
	}
	return buckets, rows.Err()
}

// SalesBucket is one derived hour of one Affiliate Link's Attributed Sales:
// how many active attributed Ticket Sales landed in that hour, the tickets
// they moved, and the Net Proceeds they left the Organization. Hour is a civil
// hour in the Event's timezone, formatted YYYY-MM-DDTHH:00 — like the Sales
// Trends day, it is a label on a local clock and not an instant, so it travels
// as a string.
type SalesBucket struct {
	Hour             string
	LinkID           string
	Sales            int
	Tickets          int
	NetProceedsCents int
}

// AttributedSalesByHour derives the Event's per-hour Attributed Sales at read
// time — nothing is stored for this surface, which is what lets a Sale
// Reversal retroactively edit the graph exactly as it edits every other
// aggregate (ADR 0057).
//
// Only ACTIVE attributed sales count, and the channel filter restates the
// invariant that only an Online Sale can carry an attribution. The money is
// Net Proceeds via the single-sourced per-line expression every other surface
// sums (ADR 0014), so the graph and the tab's headline figures cannot drift by
// a penny. Hours bucket sold_at — the moment the sale was MADE — in the
// caller-resolved timezone, so the graph's hours agree with the day the Sales
// Trends chart puts the same sale on.
func (r *Repository) AttributedSalesByHour(ctx context.Context, eventID, timezone string) ([]SalesBucket, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT
			to_char(date_trunc('hour', ts.sold_at AT TIME ZONE $2), 'YYYY-MM-DD"T"HH24:00') AS hour,
			ts.affiliate_link_id,
			COUNT(*)::int,
			COALESCE(SUM(lines.tickets), 0)::int,
			COALESCE(SUM(lines.net_proceeds_cents), 0)::bigint
		FROM ticket_sales ts
		JOIN LATERAL (
			SELECT COALESCE(SUM(tsl.quantity), 0) AS tickets,
			       COALESCE(SUM(`+sales.LineNetProceedsSQL+`), 0) AS net_proceeds_cents
			FROM ticket_sale_lines tsl
			WHERE tsl.ticket_sale_id = ts.id
		) lines ON TRUE
		WHERE ts.event_id = $1
		  AND ts.affiliate_link_id IS NOT NULL
		  AND ts.status = 'active'
		  AND ts.channel = 'online'
		GROUP BY 1, 2
		ORDER BY 1, 2
	`, eventID, timezone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	buckets := make([]SalesBucket, 0)
	for rows.Next() {
		var bucket SalesBucket
		if err := rows.Scan(&bucket.Hour, &bucket.LinkID, &bucket.Sales, &bucket.Tickets, &bucket.NetProceedsCents); err != nil {
			return nil, err
		}
		buckets = append(buckets, bucket)
	}
	return buckets, rows.Err()
}
