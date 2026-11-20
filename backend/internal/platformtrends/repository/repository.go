// Package repository keeps what Platform Trends owns: the activity totals the
// daily close freezes (#815, ADR 0080), the Organization markers it counts
// them from beside recorded Ticket Sales (#816) and the Customer markers
// (#817). Every other figure is read through the module that owns its rows.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Repository reads and writes the frozen activity totals and the
// Organization and Customer markers.
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
	// Customers is the day's Active Customers, and nil on a day that has no
	// such figure, which is different from one holding zeros: a day closed
	// while Customer recording was off, or before it began.
	Customers *CustomerActivityTotals
}

// CustomerActivityTotals is the Active Customers of one frozen platform day
// (#817): how many were active, how many different ones across the 7 and the
// 30 days ending on it, and the day's count split by kind. It holds counts
// and names nobody.
type CustomerActivityTotals struct {
	Active       int
	Active7Days  int
	Active30Days int
	// New is those who became Verified Customers that day and Returning
	// every other one: the two add up to Active.
	New       int
	Returning int
	// ReturningBought is those of Returning who own an Online Sale sold that
	// day. It is a part of Returning, not a third kind beside it.
	ReturningBought int
}

// customerTotalsColumns is the six Active Customers columns of a totals row,
// in the order customerTotalsArgs and ActivityTotalsBetween take them.
const customerTotalsColumns = `active_customers, active_customers_7d, active_customers_30d,
	active_customers_new, active_customers_returning, active_customers_returning_bought`

// customerTotalsArgs is the six Active Customers values of a row to insert,
// all NULL for a day that has no such figure.
func customerTotalsArgs(customers *CustomerActivityTotals) []any {
	if customers == nil {
		return []any{nil, nil, nil, nil, nil, nil}
	}
	return []any{customers.Active, customers.Active7Days, customers.Active30Days,
		customers.New, customers.Returning, customers.ReturningBought}
}

// FreezeActivityTotals writes a day's totals and reports whether it did. A
// day that already has a row keeps it untouched and reports false: a frozen
// day is never rewritten, which is also what makes two overlapping runs of
// the close harmless.
func (r *Repository) FreezeActivityTotals(ctx context.Context, totals ActivityTotals, frozenAt time.Time) (bool, error) {
	args := append([]any{totals.Day, totals.ActiveOrganizations, totals.ActiveOrganizations7Days, totals.ActiveOrganizations30Days, frozenAt},
		customerTotalsArgs(totals.Customers)...)
	res, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO platform_activity_totals
			(day, active_organizations, active_organizations_7d, active_organizations_30d, frozen_at,
			`+customerTotalsColumns+`)
		VALUES ($1::date, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (day) DO NOTHING
	`, args...)
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
		SELECT to_char(day, 'YYYY-MM-DD'), active_organizations, active_organizations_7d, active_organizations_30d,
			`+customerTotalsColumns+`
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
		var active, active7Days, active30Days, newCustomers, returning, returningBought sql.NullInt64
		if err := rows.Scan(&totals.Day, &totals.ActiveOrganizations, &totals.ActiveOrganizations7Days, &totals.ActiveOrganizations30Days,
			&active, &active7Days, &active30Days, &newCustomers, &returning, &returningBought); err != nil {
			return nil, err
		}
		// The six are NULL together or not at all (migration 140).
		if active.Valid {
			totals.Customers = &CustomerActivityTotals{
				Active: int(active.Int64), Active7Days: int(active7Days.Int64), Active30Days: int(active30Days.Int64),
				New: int(newCustomers.Int64), Returning: int(returning.Int64), ReturningBought: int(returningBought.Int64),
			}
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

// FirstActiveCustomersDay is the earliest frozen day that has an Active
// Customers figure, YYYY-MM-DD, and false while no day has one. It is the
// day recording of Active Customers began: the totals say it themselves, a
// day without the figure holding NULL and never zero.
func (r *Repository) FirstActiveCustomersDay(ctx context.Context) (string, bool, error) {
	var day sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT to_char(MIN(day), 'YYYY-MM-DD') FROM platform_activity_totals WHERE active_customers IS NOT NULL
	`).Scan(&day)
	if err != nil {
		return "", false, err
	}
	return day.String, day.Valid, nil
}

// CustomerMarker says a Customer acted as themselves on a platform day,
// YYYY-MM-DD, and nothing else (#817, ADR 0080). It never leaves the daily
// close: no read of Platform Trends returns one.
type CustomerMarker struct {
	Day        string
	CustomerID string
}

// MarkCustomerActive records that the Customer acted on the day. A marker
// that already exists is left as it is.
func (r *Repository) MarkCustomerActive(ctx context.Context, customerID, day string) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO customer_activity_markers (day, customer_id)
		VALUES ($1::date, $2)
		ON CONFLICT (day, customer_id) DO NOTHING
	`, day, customerID)
	return err
}

// CustomerMarkersBetween is every Customer marker from firstDay through
// lastDay, both included and both YYYY-MM-DD. It is the daily close's read
// and nothing else's.
func (r *Repository) CustomerMarkersBetween(ctx context.Context, firstDay, lastDay string) ([]CustomerMarker, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT to_char(day, 'YYYY-MM-DD'), customer_id
		FROM customer_activity_markers
		WHERE day >= $1::date AND day <= $2::date
	`, firstDay, lastDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CustomerMarker
	for rows.Next() {
		var marker CustomerMarker
		if err := rows.Scan(&marker.Day, &marker.CustomerID); err != nil {
			return nil, err
		}
		out = append(out, marker)
	}
	return out, rows.Err()
}

// FirstCustomerMarkerDay is the day of the oldest Customer marker still
// kept, YYYY-MM-DD, and false when there is none.
func (r *Repository) FirstCustomerMarkerDay(ctx context.Context) (string, bool, error) {
	var day sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, `SELECT to_char(MIN(day), 'YYYY-MM-DD') FROM customer_activity_markers`).Scan(&day)
	if err != nil {
		return "", false, err
	}
	return day.String, day.Valid, nil
}

// DeleteCustomerMarkersBefore deletes every Customer marker of a day before
// the given one, YYYY-MM-DD, and reports how many it deleted.
func (r *Repository) DeleteCustomerMarkersBefore(ctx context.Context, day string) (int, error) {
	res, err := r.db.Pool.ExecContext(ctx, `DELETE FROM customer_activity_markers WHERE day < $1::date`, day)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}
