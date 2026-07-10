package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Organization is a tenant that owns Events and Members.
type Organization struct {
	ID        string
	Name      string
	Slug      string
	Currency  string
	CreatedAt time.Time
}

// Member is a person belonging to an Organization.
type Member struct {
	ID             string
	OrganizationID string
	Email          string
	Role           MemberRole
	CreatedAt      time.Time
}

// Event is a scheduled occurrence belonging to an Organization.
type Event struct {
	ID             string
	OrganizationID string
	Name           string
	Slug           string
	CreatedAt      time.Time
}

// EventAssignment links a Member to an Event with a delegated role.
type EventAssignment struct {
	ID        string
	MemberID  string
	EventID   string
	Role      MemberRole
	Email     string
	CreatedAt time.Time
}

// GetOrganizationByID loads an organization by ID.
func (r *Repository) GetOrganizationByID(ctx context.Context, orgID string) (*Organization, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT id, name, slug, currency, created_at
		FROM organizations
		WHERE id = $1
	`, orgID)

	var o Organization
	if err := row.Scan(&o.ID, &o.Name, &o.Slug, &o.Currency, &o.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &o, nil
}

// UpdateOrganizationName updates the display name for an organization.
func (r *Repository) UpdateOrganizationName(ctx context.Context, orgID, name string) (*Organization, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		UPDATE organizations
		SET name = $2
		WHERE id = $1
		RETURNING id, name, slug, currency, created_at
	`, orgID, name)

	var o Organization
	if err := row.Scan(&o.ID, &o.Name, &o.Slug, &o.Currency, &o.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &o, nil
}

// UpdateOrganizationCurrency updates the currency for an organization.
func (r *Repository) UpdateOrganizationCurrency(ctx context.Context, orgID, currency string) (*Organization, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		UPDATE organizations
		SET currency = $2
		WHERE id = $1
		RETURNING id, name, slug, currency, created_at
	`, orgID, currency)

	var o Organization
	if err := row.Scan(&o.ID, &o.Name, &o.Slug, &o.Currency, &o.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &o, nil
}

// DeleteOrganization removes an organization and clears affected sessions atomically.
func (r *Repository) DeleteOrganization(ctx context.Context, orgID string) error {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, `
		UPDATE sessions
		SET active_member_id = NULL
		WHERE active_member_id IN (
			SELECT id FROM members WHERE organization_id = $1
		)
	`, orgID); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("organization not found")
	}

	return tx.Commit()
}

// ListMembersByOrganizationID returns all members for an organization.
func (r *Repository) ListMembersByOrganizationID(ctx context.Context, orgID string) ([]Member, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT id, organization_id, email, role, created_at
		FROM members
		WHERE organization_id = $1
		ORDER BY created_at ASC, email ASC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []Member
	for rows.Next() {
		var m Member
		var role string
		if err := rows.Scan(&m.ID, &m.OrganizationID, &m.Email, &role, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Role = MemberRole(role)
		members = append(members, m)
	}
	return members, rows.Err()
}

// GetMemberByID loads a member scoped to an organization.
func (r *Repository) GetMemberByID(ctx context.Context, orgID, memberID string) (*Member, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT id, organization_id, email, role, created_at
		FROM members
		WHERE id = $1 AND organization_id = $2
	`, memberID, orgID)

	var m Member
	var role string
	if err := row.Scan(&m.ID, &m.OrganizationID, &m.Email, &role, &m.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	m.Role = MemberRole(role)
	return &m, nil
}

// MemberEmailExistsInOrganization reports whether an email is already a member of the org.
func (r *Repository) MemberEmailExistsInOrganization(ctx context.Context, orgID, email string) (bool, error) {
	var exists bool
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM members WHERE organization_id = $1 AND email = $2
		)
	`, orgID, email).Scan(&exists)
	return exists, err
}

// CreateMember inserts a new member for an organization.
func (r *Repository) CreateMember(ctx context.Context, orgID, email string, role MemberRole, createdAt time.Time) (*Member, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		INSERT INTO members (organization_id, email, role, created_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id, organization_id, email, role, created_at
	`, orgID, email, role, createdAt)

	var m Member
	var roleStr string
	if err := row.Scan(&m.ID, &m.OrganizationID, &m.Email, &roleStr, &m.CreatedAt); err != nil {
		return nil, err
	}
	m.Role = MemberRole(roleStr)
	return &m, nil
}

// UpdateMemberRole changes the org-level role for a member.
func (r *Repository) UpdateMemberRole(ctx context.Context, orgID, memberID string, role MemberRole) (*Member, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		UPDATE members
		SET role = $3
		WHERE id = $1 AND organization_id = $2
		RETURNING id, organization_id, email, role, created_at
	`, memberID, orgID, role)

	var m Member
	var roleStr string
	if err := row.Scan(&m.ID, &m.OrganizationID, &m.Email, &roleStr, &m.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	m.Role = MemberRole(roleStr)
	return &m, nil
}

// DeleteMember removes a member from an organization.
func (r *Repository) DeleteMember(ctx context.Context, orgID, memberID string) error {
	result, err := r.db.Pool.ExecContext(ctx, `
		DELETE FROM members WHERE id = $1 AND organization_id = $2
	`, memberID, orgID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("member not found")
	}
	return nil
}

// CountOrgAdmins returns how many org_admin members exist in an organization.
func (r *Repository) CountOrgAdmins(ctx context.Context, orgID string) (int, error) {
	var count int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM members
		WHERE organization_id = $1 AND role = 'org_admin'
	`, orgID).Scan(&count)
	return count, err
}

// GetEventByID loads an event scoped to an organization.
func (r *Repository) GetEventByID(ctx context.Context, orgID, eventID string) (*Event, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT id, organization_id, name, slug, created_at
		FROM events
		WHERE id = $1 AND organization_id = $2
	`, eventID, orgID)

	var e Event
	if err := row.Scan(&e.ID, &e.OrganizationID, &e.Name, &e.Slug, &e.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

// ListEventAssignments returns assignments for an event with member emails.
func (r *Repository) ListEventAssignments(ctx context.Context, eventID string) ([]EventAssignment, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT ea.id, ea.member_id, ea.event_id, ea.role, m.email, ea.created_at
		FROM event_assignments ea
		JOIN members m ON m.id = ea.member_id
		WHERE ea.event_id = $1
		ORDER BY m.email ASC
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var assignments []EventAssignment
	for rows.Next() {
		var a EventAssignment
		var role string
		if err := rows.Scan(&a.ID, &a.MemberID, &a.EventID, &role, &a.Email, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Role = MemberRole(role)
		assignments = append(assignments, a)
	}
	return assignments, rows.Err()
}

// UpsertEventAssignment creates or updates an assignment for a member on an event.
func (r *Repository) UpsertEventAssignment(
	ctx context.Context,
	memberID, eventID string,
	role MemberRole,
	createdAt time.Time,
) (*EventAssignment, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		INSERT INTO event_assignments (member_id, event_id, role, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (member_id, event_id)
		DO UPDATE SET role = EXCLUDED.role
		RETURNING id, member_id, event_id, role, created_at
	`, memberID, eventID, role, createdAt)

	var a EventAssignment
	var roleStr string
	if err := row.Scan(&a.ID, &a.MemberID, &a.EventID, &roleStr, &a.CreatedAt); err != nil {
		return nil, err
	}
	a.Role = MemberRole(roleStr)
	return &a, nil
}

// DeleteEventAssignment removes a member's assignment from an event.
func (r *Repository) DeleteEventAssignment(ctx context.Context, eventID, memberID string) error {
	result, err := r.db.Pool.ExecContext(ctx, `
		DELETE FROM event_assignments
		WHERE event_id = $1 AND member_id = $2
	`, eventID, memberID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("assignment not found")
	}
	return nil
}

// MemberHasEventAssignment reports whether a member is assigned to an event.
func (r *Repository) MemberHasEventAssignment(ctx context.Context, memberID, eventID string) (bool, error) {
	var exists bool
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM event_assignments WHERE member_id = $1 AND event_id = $2
		)
	`, memberID, eventID).Scan(&exists)
	return exists, err
}