package repository

import (
	"context"
	"database/sql"
)

// OperatorEventRow is one Event as the Platform Operator sees it: what it is
// called, where it is in its lifecycle, when it happens, and whether it
// advertises itself. No catalog, no sales — the operator surface answers "what
// is this Organization running", not "how is it doing" (ADR 0015).
type OperatorEventRow struct {
	ID           string
	Name         string
	Status       EventStatus
	StartsAt     sql.NullTime
	Discoverable bool
}

// ListEventsByOrganizationIDForOperator returns every Event of one Organization
// whatever its status — a draft and a cancelled Event are both part of the
// picture here — soonest-last, with unscheduled Events at the end and an id
// tiebreaker so equal (or absent) dates keep a stable order.
func (r *Repository) ListEventsByOrganizationIDForOperator(ctx context.Context, orgID string) ([]OperatorEventRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT id, name, status, starts_at, discoverable
		FROM events
		WHERE organization_id = $1
		ORDER BY starts_at DESC NULLS LAST, id ASC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []OperatorEventRow
	for rows.Next() {
		var e OperatorEventRow
		var status string
		if err := rows.Scan(&e.ID, &e.Name, &status, &e.StartsAt, &e.Discoverable); err != nil {
			return nil, err
		}
		e.Status = EventStatus(status)
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountEventsByOrganizationIDs returns how many Events each of the given
// Organizations has, counting every status. Organizations with none are simply
// absent from the map.
func (r *Repository) CountEventsByOrganizationIDs(ctx context.Context, orgIDs []string) (map[string]int, error) {
	counts := make(map[string]int, len(orgIDs))
	if len(orgIDs) == 0 {
		return counts, nil
	}

	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT organization_id, COUNT(*)
		FROM events
		WHERE organization_id = ANY($1)
		GROUP BY organization_id
	`, orgIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var orgID string
		var count int
		if err := rows.Scan(&orgID, &count); err != nil {
			return nil, err
		}
		counts[orgID] = count
	}
	return counts, rows.Err()
}

// GetEventByIDForOperator returns one Event whatever Organization it belongs
// to, or nil for none, for the Platform Operator's view of its Ticket
// Questions (#410).
func (r *Repository) GetEventByIDForOperator(ctx context.Context, eventID string) (*OperatorEventRow, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT id, name, status, starts_at, discoverable
		FROM events
		WHERE id = $1
	`, eventID)
	var e OperatorEventRow
	var status string
	if err := row.Scan(&e.ID, &e.Name, &status, &e.StartsAt, &e.Discoverable); err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	e.Status = EventStatus(status)
	return &e, nil
}
