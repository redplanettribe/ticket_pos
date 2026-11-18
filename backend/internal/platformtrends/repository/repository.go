// Package repository keeps what Platform Trends owns: the activity totals the
// daily close freezes (#815, ADR 0080), and the Organization markers it
// counts them from beside recorded Ticket Sales (#816). Every other figure is
// read through the module that owns its rows.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Repository reads and writes the frozen activity totals and the
// Organization markers.
type Repository struct {
	db *platform.DB
}

// New returns a repository backed by the given database pool.
func New(db *platform.DB) *Repository {
	return &Repository{db: db}
}

// ActivityTotals is one frozen platform day: how many Organizations were
// active on it, and how many different ones across the 7 and the 30 days
// ending on it. Day is a calendar date on the platform clock, YYYY-MM-DD.
type ActivityTotals struct {
	Day                       string
	ActiveOrganizations       int
	ActiveOrganizations7Days  int
	ActiveOrganizations30Days int
}

// FreezeActivityTotals writes a day's totals and reports whether it did. A
// day that already has a row keeps it untouched and reports false: a frozen
// day is never rewritten, which is also what makes two overlapping runs of
// the close harmless.
func (r *Repository) FreezeActivityTotals(ctx context.Context, totals ActivityTotals, frozenAt time.Time) (bool, error) {
	res, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO platform_activity_totals
			(day, active_organizations, active_organizations_7d, active_organizations_30d, frozen_at)
		VALUES ($1::date, $2, $3, $4, $5)
		ON CONFLICT (day) DO NOTHING
	`, totals.Day, totals.ActiveOrganizations, totals.ActiveOrganizations7Days, totals.ActiveOrganizations30Days, frozenAt)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ActivityTotalsBetween is every frozen day from firstDay through lastDay,
// both included and both YYYY-MM-DD, oldest first. A day the close has not
// frozen has no entry.
func (r *Repository) ActivityTotalsBetween(ctx context.Context, firstDay, lastDay string) ([]ActivityTotals, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT to_char(day, 'YYYY-MM-DD'), active_organizations, active_organizations_7d, active_organizations_30d
		FROM platform_activity_totals
		WHERE day >= $1::date AND day <= $2::date
		ORDER BY day
	`, firstDay, lastDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ActivityTotals
	for rows.Next() {
		var totals ActivityTotals
		if err := rows.Scan(&totals.Day, &totals.ActiveOrganizations, &totals.ActiveOrganizations7Days, &totals.ActiveOrganizations30Days); err != nil {
			return nil, err
		}
		out = append(out, totals)
	}
	return out, rows.Err()
}

// FirstFrozenActivityDay is the earliest frozen day, YYYY-MM-DD, and false
// when the close has frozen none.
func (r *Repository) FirstFrozenActivityDay(ctx context.Context) (string, bool, error) {
	var day sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, `SELECT to_char(MIN(day), 'YYYY-MM-DD') FROM platform_activity_totals`).Scan(&day)
	if err != nil {
		return "", false, err
	}
	return day.String, day.Valid, nil
}

// OrganizationMarker says an Organization was worked in by a Member on a
// platform day, YYYY-MM-DD, and nothing else (#816, ADR 0080).
type OrganizationMarker struct {
	Day            string
	OrganizationID string
}

// MarkOrganizationActive records that the Organization was worked in on the
// day. A marker that already exists is left as it is.
func (r *Repository) MarkOrganizationActive(ctx context.Context, organizationID, day string) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO organization_activity_markers (day, organization_id)
		VALUES ($1::date, $2)
		ON CONFLICT (day, organization_id) DO NOTHING
	`, day, organizationID)
	return err
}

// OrganizationMarkersBetween is every marker from firstDay through lastDay,
// both included and both YYYY-MM-DD.
func (r *Repository) OrganizationMarkersBetween(ctx context.Context, firstDay, lastDay string) ([]OrganizationMarker, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT to_char(day, 'YYYY-MM-DD'), organization_id
		FROM organization_activity_markers
		WHERE day >= $1::date AND day <= $2::date
	`, firstDay, lastDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []OrganizationMarker
	for rows.Next() {
		var marker OrganizationMarker
		if err := rows.Scan(&marker.Day, &marker.OrganizationID); err != nil {
			return nil, err
		}
		out = append(out, marker)
	}
	return out, rows.Err()
}

// FirstOrganizationMarkerDay is the day of the oldest marker still kept,
// YYYY-MM-DD, and false when there is none.
func (r *Repository) FirstOrganizationMarkerDay(ctx context.Context) (string, bool, error) {
	var day sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, `SELECT to_char(MIN(day), 'YYYY-MM-DD') FROM organization_activity_markers`).Scan(&day)
	if err != nil {
		return "", false, err
	}
	return day.String, day.Valid, nil
}

// DeleteOrganizationMarkersBefore deletes every marker of a day before the
// given one, YYYY-MM-DD, and reports how many it deleted.
func (r *Repository) DeleteOrganizationMarkersBefore(ctx context.Context, day string) (int, error) {
	res, err := r.db.Pool.ExecContext(ctx, `DELETE FROM organization_activity_markers WHERE day < $1::date`, day)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// RecordOrganizationMarkersBegan writes down the first day the daily close
// froze with markers. A day already written down is kept, never moved.
func (r *Repository) RecordOrganizationMarkersBegan(ctx context.Context, day string) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO organization_activity_recording (began_on)
		VALUES ($1::date)
		ON CONFLICT (singleton) DO NOTHING
	`, day)
	return err
}

// OrganizationMarkersBeganOn is the first day the daily close froze with
// markers, YYYY-MM-DD, and false while it has frozen none.
func (r *Repository) OrganizationMarkersBeganOn(ctx context.Context) (string, bool, error) {
	var day string
	err := r.db.Pool.QueryRowContext(ctx, `SELECT to_char(began_on, 'YYYY-MM-DD') FROM organization_activity_recording`).Scan(&day)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return day, true, nil
}
