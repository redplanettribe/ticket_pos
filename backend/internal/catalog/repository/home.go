package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// eventOverSQL says an Event `e` is over at the instant $2: its end has
// passed, or its start where it has no end. The SQL twin of the staff Events
// list's tabs and of the Storefront's filter (COALESCE(e.ends_at,
// e.starts_at) >= now), half-open on the same side, so an Event ending
// exactly now is not over yet. An Event with no start is never over.
const eventOverSQL = `COALESCE(e.ends_at, e.starts_at) < $2`

// OrganizationEventCounts is how many Events an Organization has at all, in
// any status, and how many of them are Active Events.
type OrganizationEventCounts struct {
	Events       int
	ActiveEvents int
}

// OrganizationEventCounts counts one Organization's Events and its Active
// Events at the instant now, for Home (#859): an Active Event is neither
// `cancelled` nor over, drafts included, and a dateless draft is Active.
func (r *Repository) OrganizationEventCounts(ctx context.Context, orgID string, now time.Time) (OrganizationEventCounts, error) {
	var out OrganizationEventCounts
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT
			COUNT(*)::bigint,
			COUNT(*) FILTER (WHERE e.status <> 'cancelled' AND NOT COALESCE(`+eventOverSQL+`, FALSE))::bigint
		FROM events e
		WHERE e.organization_id = $1
	`, orgID, now).Scan(&out.Events, &out.ActiveEvents)
	return out, err
}

// NextEventRow is the Event Home names as next: its id, name, start and
// timezone, and whether it is Ongoing at the instant it was read.
type NextEventRow struct {
	ID       string
	Name     string
	StartsAt time.Time
	Timezone string
	Ongoing  bool
}

// OrganizationNextEvent is the published Event of one Organization with the
// earliest start that is not over at now, for Home (#859). A draft is never
// next and neither is a cancelled Event, whatever their dates; an Event that
// has started and not ended is earlier than any still to start, so an Ongoing
// Event is chosen over a later one. Ongoing is the Timeline's pure instant
// comparison: started, and with an end not yet reached. False when the
// Organization has no such Event.
func (r *Repository) OrganizationNextEvent(ctx context.Context, orgID string, now time.Time) (NextEventRow, bool, error) {
	var out NextEventRow
	var timezone sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT e.id, e.name, e.starts_at, e.timezone,
			(e.starts_at <= $2 AND e.ends_at IS NOT NULL AND e.ends_at > $2)
		FROM events e
		WHERE e.organization_id = $1
		  AND e.status = 'published'
		  AND e.starts_at IS NOT NULL
		  AND NOT `+eventOverSQL+`
		ORDER BY e.starts_at ASC, e.id ASC
		LIMIT 1
	`, orgID, now).Scan(&out.ID, &out.Name, &out.StartsAt, &timezone, &out.Ongoing)
	if errors.Is(err, sql.ErrNoRows) {
		return NextEventRow{}, false, nil
	}
	if err != nil {
		return NextEventRow{}, false, err
	}
	out.Timezone = timezone.String
	return out, true, nil
}

// OrganizationEventsHeldByDay counts one Organization's Events held per day,
// for Organization Trends (#863): an Event is held on the day, on the zone the
// caller passes, its start instant falls in, when that start is in [from,
// until) and the Event is published. A draft and a cancelled Event are never
// held; an Event that registers its audience elsewhere is held like any
// other. Days on which no Event was held have no entry.
func (r *Repository) OrganizationEventsHeldByDay(ctx context.Context, orgID string, from, until time.Time, timezone string) ([]platform.DayCount, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT
			to_char((e.starts_at AT TIME ZONE $4)::date, 'YYYY-MM-DD') AS day,
			COUNT(*)::bigint
		FROM events e
		WHERE e.organization_id = $1
		  AND e.status = 'published'
		  AND e.starts_at >= $2 AND e.starts_at < $3
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

// OrganizationEventsHeld is every Event of one Organization held in [from,
// until), for the breakdown of Organization Trends (#864): the Events
// OrganizationEventsHeldByDay counts, each once, earliest start first. A draft
// and a cancelled Event are never held; an External Registration Event is.
func (r *Repository) OrganizationEventsHeld(ctx context.Context, orgID string, from, until time.Time) ([]platform.EventCount, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT e.id, e.name
		FROM events e
		WHERE e.organization_id = $1
		  AND e.status = 'published'
		  AND e.starts_at >= $2 AND e.starts_at < $3
		ORDER BY e.starts_at, e.id
	`, orgID, from, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []platform.EventCount
	for rows.Next() {
		event := platform.EventCount{OrganizationID: orgID, Count: 1}
		if err := rows.Scan(&event.EventID, &event.EventName); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}
