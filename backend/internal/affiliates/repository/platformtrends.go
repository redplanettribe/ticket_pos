package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// PlatformPageViewsByDay totals the Page Views of every Event on the platform
// per day, for Platform Trends (#813, ADR 0080), over the hourly buckets in
// [from, until).
//
// Only the whole-page rows are summed (affiliate_link_id IS NULL). That row
// is incremented on every load; a load that came through a live Affiliate
// Link increments the link's row as well, as its Click, so adding the link
// rows in would count that load twice.
//
// The day is read on the zone the caller passes, which is the platform's own
// clock. Buckets are whole UTC hours and that clock is a whole number of
// hours from UTC, so each bucket falls inside exactly one platform day; a
// zone on a half-hour offset would split buckets and is not what this read is
// for. The range is served by event_page_views_whole_page_hour.
func (r *Repository) PlatformPageViewsByDay(ctx context.Context, from, until time.Time, timezone string) ([]platform.DayCount, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT
			to_char((hour AT TIME ZONE $3)::date, 'YYYY-MM-DD') AS day,
			SUM(view_count)::bigint
		FROM event_page_views
		WHERE affiliate_link_id IS NULL AND hour >= $1 AND hour < $2
		GROUP BY 1
		ORDER BY 1
	`, from, until, timezone)
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

// PlatformFirstPageViewDay is the day, on the clock the caller names, of the
// platform's earliest whole-page bucket, formatted YYYY-MM-DD, and false when
// no Page View was ever counted: the first day Platform Trends' Page Views
// has data. It is the head of event_page_views_whole_page_hour.
func (r *Repository) PlatformFirstPageViewDay(ctx context.Context, timezone string) (string, bool, error) {
	var day sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT to_char((MIN(hour) AT TIME ZONE $1)::date, 'YYYY-MM-DD')
		FROM event_page_views
		WHERE affiliate_link_id IS NULL
	`, timezone).Scan(&day)
	if err != nil {
		return "", false, err
	}
	return day.String, day.Valid, nil
}

// PlatformPageViewsByEvent totals the Page Views of each Event over the
// hourly buckets in [from, until), for the breakdown of Platform Trends'
// Page Views (#818, ADR 0080). It sums the whole-page rows alone, as
// PlatformPageViewsByDay does, so the Events of a window add up to what that
// read states for its days. An Event nobody loaded in the window is not
// returned. The range is served by event_page_views_whole_page_hour.
func (r *Repository) PlatformPageViewsByEvent(ctx context.Context, from, until time.Time) ([]platform.EventCount, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT e.id, e.name, org.id, org.name, SUM(v.view_count)::bigint
		FROM event_page_views v
		JOIN events e ON e.id = v.event_id
		JOIN organizations org ON org.id = e.organization_id
		WHERE v.affiliate_link_id IS NULL AND v.hour >= $1 AND v.hour < $2
		GROUP BY e.id, org.id
		ORDER BY e.id
	`, from, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []platform.EventCount
	for rows.Next() {
		var event platform.EventCount
		if err := rows.Scan(&event.EventID, &event.EventName, &event.OrganizationID, &event.OrganizationName, &event.Count); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}
