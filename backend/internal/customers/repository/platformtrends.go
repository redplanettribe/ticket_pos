package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// PlatformNewVerifiedCustomersByDay counts the Customers who became Verified
// Customers on each day, for Platform Trends (#813, ADR 0080), over the
// verifications in [from, until).
//
// The instant is verified_at, never created_at: a Customer minted by a
// Ticket Sale exists, unverified, before the person ever proves the address,
// and one never verified counts on no day. verified_at is stamped once, by
// the one statement every Proof of Email Ownership reaches
// (verifyCustomerSQL), so a Customer counts once however they verified and
// however often they sign in after.
//
// The day is read on the zone the caller passes, the platform's own clock.
// No index is added: the read is operator-only and one pass over customers,
// as the platform's sales reads are over ticket_sales.
func (r *Repository) PlatformNewVerifiedCustomersByDay(ctx context.Context, from, until time.Time, timezone string) ([]platform.DayCount, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT
			to_char((verified_at AT TIME ZONE $3)::date, 'YYYY-MM-DD') AS day,
			COUNT(*)::bigint
		FROM customers
		WHERE verified_at >= $1 AND verified_at < $2
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

// PlatformFirstVerifiedCustomerDay is the day, on the clock the caller names,
// the platform's first Customer was verified, formatted YYYY-MM-DD, and false
// when none ever was: the first day Platform Trends' new Verified Customers
// has data.
func (r *Repository) PlatformFirstVerifiedCustomerDay(ctx context.Context, timezone string) (string, bool, error) {
	var day sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT to_char((MIN(verified_at) AT TIME ZONE $1)::date, 'YYYY-MM-DD')
		FROM customers
	`, timezone).Scan(&day)
	if err != nil {
		return "", false, err
	}
	return day.String, day.Valid, nil
}

// PlatformCustomerDay names a Customer and a day, YYYY-MM-DD on the clock the
// caller named. It is read by the daily close alone, which counts with it
// and keeps no Customer (#817, ADR 0080).
type PlatformCustomerDay struct {
	Day        string
	CustomerID string
}

// PlatformCustomersVerifiedByDay is every Customer who became a Verified
// Customer in [from, until) with the day they did, for the daily close's
// split of Active Customers into new and returning (#817). The instant is
// verified_at, as in PlatformNewVerifiedCustomersByDay.
func (r *Repository) PlatformCustomersVerifiedByDay(ctx context.Context, from, until time.Time, timezone string) ([]PlatformCustomerDay, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT to_char((verified_at AT TIME ZONE $3)::date, 'YYYY-MM-DD'), id
		FROM customers
		WHERE verified_at >= $1 AND verified_at < $2
	`, from, until, timezone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PlatformCustomerDay
	for rows.Next() {
		var day PlatformCustomerDay
		if err := rows.Scan(&day.Day, &day.CustomerID); err != nil {
			return nil, err
		}
		out = append(out, day)
	}
	return out, rows.Err()
}
