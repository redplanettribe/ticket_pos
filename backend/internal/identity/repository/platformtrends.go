package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// PlatformNewOrganizationsByDay counts the Organizations created on each day,
// for Platform Trends (#813, ADR 0080), over the creations in [from, until).
// Every Organization counts, House Organizations included, so the figure
// agrees with the Organizations the Operator Dashboard lists.
//
// The day is read on the zone the caller passes, the platform's own clock.
// No index is added: organizations is a small table read by operators alone.
func (r *Repository) PlatformNewOrganizationsByDay(ctx context.Context, from, until time.Time, timezone string) ([]platform.DayCount, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT
			to_char((created_at AT TIME ZONE $3)::date, 'YYYY-MM-DD') AS day,
			COUNT(*)::bigint
		FROM organizations
		WHERE created_at >= $1 AND created_at < $2
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

// PlatformFirstOrganizationDay is the day, on the clock the caller names, the
// platform's first Organization was created, formatted YYYY-MM-DD, and false
// when there is none: the first day Platform Trends' new Organizations has
// data.
func (r *Repository) PlatformFirstOrganizationDay(ctx context.Context, timezone string) (string, bool, error) {
	var day sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT to_char((MIN(created_at) AT TIME ZONE $1)::date, 'YYYY-MM-DD')
		FROM organizations
	`, timezone).Scan(&day)
	if err != nil {
		return "", false, err
	}
	return day.String, day.Valid, nil
}

// PlatformNewOrganizations is every Organization created in [from, until),
// oldest first, House Organizations included, for the breakdown of Platform
// Trends' new Organizations (#818, ADR 0080): the Organizations
// PlatformNewOrganizationsByDay counts, each once.
func (r *Repository) PlatformNewOrganizations(ctx context.Context, from, until time.Time) ([]platform.OrganizationCount, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT id, name
		FROM organizations
		WHERE created_at >= $1 AND created_at < $2
		ORDER BY created_at, id
	`, from, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []platform.OrganizationCount
	for rows.Next() {
		organization := platform.OrganizationCount{Count: 1}
		if err := rows.Scan(&organization.OrganizationID, &organization.OrganizationName); err != nil {
			return nil, err
		}
		out = append(out, organization)
	}
	return out, rows.Err()
}

// PlatformOrganizationNames is the name of each of the given Organizations,
// keyed by id, for the breakdown of Platform Trends' Active Organizations
// (#819, ADR 0080), which knows the Organizations active on a day by id. An
// id no Organization has is absent from the answer.
func (r *Repository) PlatformOrganizationNames(ctx context.Context, organizationIDs []string) (map[string]string, error) {
	names := make(map[string]string, len(organizationIDs))
	if len(organizationIDs) == 0 {
		return names, nil
	}
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT id, name FROM organizations WHERE id = ANY($1::uuid[])
	`, organizationIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	return names, rows.Err()
}
