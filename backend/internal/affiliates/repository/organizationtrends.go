package repository

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// OrganizationPageViewsByDay totals the Page Views of one Organization's
// Event pages per day, for Organization Trends (#863, ADR 0081), over the
// hourly buckets in [from, until).
//
// It sums the whole-page rows alone, as PlatformPageViewsByDay does, so a
// load that came through an Affiliate Link is counted once. Each bucket is a
// whole UTC hour and is read on the platform day of that hour, on the zone
// the caller passes, which must be a whole number of hours from UTC. The
// Organization's Events lead the read and each is served by the bucket key,
// (event_id, affiliate_link_id, hour).
func (r *Repository) OrganizationPageViewsByDay(ctx context.Context, orgID string, from, until time.Time, timezone string) ([]platform.DayCount, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT
			to_char((v.hour AT TIME ZONE $4)::date, 'YYYY-MM-DD') AS day,
			SUM(v.view_count)::bigint
		FROM events e
		JOIN event_page_views v ON v.event_id = e.id
		WHERE e.organization_id = $1
		  AND v.affiliate_link_id IS NULL AND v.hour >= $2 AND v.hour < $3
		GROUP BY 1
		ORDER BY 1
	`, orgID, from, until, timezone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []platform.DayCount
	for rows.Next() {
		var day platform.DayCount
		if err := rows.Scan(&day.Day, &day.Count); err != nil {
			return nil, err
		}
		out = append(out, day)
	}
	return out, rows.Err()
}
