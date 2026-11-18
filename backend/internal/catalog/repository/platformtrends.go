package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// PlatformEventsPublishedByDay counts the Events first published on each
// day, across every Organization, for Platform Trends' Events Published
// (#814, ADR 0080), over the publications in [from, until).
//
// The instant is first_published_at and the status is not read: an Event
// cancelled since stays counted on the day it was published, a `draft` has
// no instant and counts nowhere, and an Event with External Registration
// counts like any other. For an Event published before migration 137 the
// instant is its creation, a stand-in; PlatformEventsPublishedExactSince
// says from which day it is exact.
//
// The day is read on the zone the caller passes, the platform's own clock,
// never the Event's timezone. No index is added: the read is operator-only
// and one pass over events.
func (r *Repository) PlatformEventsPublishedByDay(ctx context.Context, from, until time.Time, timezone string) ([]platform.DayCount, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT
			to_char((first_published_at AT TIME ZONE $3)::date, 'YYYY-MM-DD') AS day,
			COUNT(*)::bigint
		FROM events
		WHERE first_published_at >= $1 AND first_published_at < $2
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

// PlatformFirstEventPublishedDay is the day, on the clock the caller names,
// the platform's earliest Event was first published, formatted YYYY-MM-DD,
// and false when none ever was: the first day Events Published has data.
func (r *Repository) PlatformFirstEventPublishedDay(ctx context.Context, timezone string) (string, bool, error) {
	var day sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT to_char((MIN(first_published_at) AT TIME ZONE $1)::date, 'YYYY-MM-DD')
		FROM events
	`, timezone).Scan(&day)
	if err != nil {
		return "", false, err
	}
	return day.String, day.Valid, nil
}

// PlatformEventsPublishedExactSince is the platform day exact recording of
// Events Published began, formatted YYYY-MM-DD: the day migration 137 ran,
// which wrote it down as it ran. Events first published before it are
// counted on their creation day. False when no day was recorded.
func (r *Repository) PlatformEventsPublishedExactSince(ctx context.Context) (string, bool, error) {
	var day string
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT to_char(began_on, 'YYYY-MM-DD') FROM events_published_recording
	`).Scan(&day)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return day, true, nil
}

// PlatformEventsPublished is every Event first published in [from, until),
// oldest publication first, for the breakdown of Platform Trends' Events
// Published (#818, ADR 0080). It goes by first_published_at alone, as
// PlatformEventsPublishedByDay does, so it lists exactly the Events that
// read counts: one cancelled since is listed, a `draft` is not. Each counts
// once.
func (r *Repository) PlatformEventsPublished(ctx context.Context, from, until time.Time) ([]platform.EventCount, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT e.id, e.name, org.id, org.name
		FROM events e
		JOIN organizations org ON org.id = e.organization_id
		WHERE e.first_published_at >= $1 AND e.first_published_at < $2
		ORDER BY e.first_published_at, e.id
	`, from, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []platform.EventCount
	for rows.Next() {
		event := platform.EventCount{Count: 1}
		if err := rows.Scan(&event.EventID, &event.EventName, &event.OrganizationID, &event.OrganizationName); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}
