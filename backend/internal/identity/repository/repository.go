package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// MemberRole is a staff role within an organization.
type MemberRole string

const (
	RoleOrgAdmin   MemberRole = "org_admin"
	RoleEventOwner MemberRole = "event_owner"
	RoleEventStaff MemberRole = "event_staff"
)

// Session is a server-side staff session.
type Session struct {
	ID             string
	Email          string
	ActiveMemberID sql.NullString
	ExpiresAt      time.Time
	CreatedAt      time.Time
}

// Membership summarizes a member record for session responses.
type Membership struct {
	MemberID            string
	OrganizationID      string
	OrganizationName    string
	OrganizationSlug    string
	OrganizationLogoKey sql.NullString
	Role                MemberRole
	Email               string
}

// Repository provides SQL access for identity data.
type Repository struct {
	db *platform.DB
}

// New returns a repository backed by the given database pool.
func New(db *platform.DB) *Repository {
	return &Repository{db: db}
}

// CreateSession inserts a new session row.
func (r *Repository) CreateSession(ctx context.Context, session Session) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO sessions (id, email, active_member_id, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, session.ID, session.Email, session.ActiveMemberID, session.ExpiresAt, session.CreatedAt)
	return err
}

// GetSession loads a session by ID.
func (r *Repository) GetSession(ctx context.Context, id string) (*Session, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT id, email, active_member_id, expires_at, created_at
		FROM sessions
		WHERE id = $1
	`, id)

	var s Session
	if err := row.Scan(&s.ID, &s.Email, &s.ActiveMemberID, &s.ExpiresAt, &s.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// ExtendSession updates expires_at for a session.
func (r *Repository) ExtendSession(ctx context.Context, id string, expiresAt time.Time) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		UPDATE sessions SET expires_at = $2 WHERE id = $1
	`, id, expiresAt)
	return err
}

// DeleteSession removes a session by ID.
func (r *Repository) DeleteSession(ctx context.Context, id string) error {
	_, err := r.db.Pool.ExecContext(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return err
}

// ListMemberships returns all member records for an email.
func (r *Repository) ListMemberships(ctx context.Context, email string) ([]Membership, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT m.id, m.organization_id, o.name, o.slug, o.logo_image_key, m.role, m.email
		FROM members m
		JOIN organizations o ON o.id = m.organization_id
		WHERE m.email = $1
		ORDER BY o.name ASC
	`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memberships []Membership
	for rows.Next() {
		var m Membership
		var role string
		if err := rows.Scan(&m.MemberID, &m.OrganizationID, &m.OrganizationName, &m.OrganizationSlug, &m.OrganizationLogoKey, &role, &m.Email); err != nil {
			return nil, err
		}
		m.Role = MemberRole(role)
		memberships = append(memberships, m)
	}
	return memberships, rows.Err()
}

// GetStaffLocale returns the Staff Locale stored for an email address, and
// whether one is stored at all.
//
// The second return is the whole point of this signature. Absence is a real
// answer here and it is NOT English: nobody is given a language by being
// invited, imported or paid out, so a person with no row has stated nothing and
// a sign-in is still free to record what it detects. The caller falls to the
// English floor for its own reasons; it does not learn that from here.
func (r *Repository) GetStaffLocale(ctx context.Context, email string) (string, bool, error) {
	var locale string
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT locale FROM staff_locales WHERE email = $1
	`, email).Scan(&locale)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return locale, true, nil
}

// SetStaffLocale records a language a person chose, replacing whatever was
// there. This is the switcher's write, and a deliberate choice outranks
// everything, including a deliberate choice made a minute ago.
func (r *Repository) SetStaffLocale(ctx context.Context, email, locale string, now time.Time) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO staff_locales (email, locale, created_at, updated_at)
		VALUES ($1, $2, $3, $3)
		ON CONFLICT (email) DO UPDATE SET locale = EXCLUDED.locale, updated_at = EXCLUDED.updated_at
	`, email, locale, now)
	return err
}

// RememberStaffLocale records a language DETECTED at sign-in, and only when the
// person has none.
//
// DO NOTHING rather than DO UPDATE, and that is the difference between this and
// SetStaffLocale. What a sign-in carries is a browser header and the fact that
// somebody read a login page in that language and carried on — enough to spare a
// new Spanish speaker from hunting for a setting written in English, and nowhere
// near enough to overrule a person who has already used the switcher. Someone
// who chose Spanish and then signs in from an English laptop stays in Spanish.
func (r *Repository) RememberStaffLocale(ctx context.Context, email, locale string, now time.Time) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO staff_locales (email, locale, created_at, updated_at)
		VALUES ($1, $2, $3, $3)
		ON CONFLICT (email) DO NOTHING
	`, email, locale, now)
	return err
}

// OrganizationSlugExists reports whether a slug is already taken.
func (r *Repository) OrganizationSlugExists(ctx context.Context, slug string) (bool, error) {
	var exists bool
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM organizations WHERE slug = $1)
	`, slug).Scan(&exists)
	return exists, err
}

// CreateOrganizationWithMember atomically creates an organization, org_admin member, and sets the session active member.
func (r *Repository) CreateOrganizationWithMember(
	ctx context.Context,
	sessionID, email, orgName, slug string,
	createdAt time.Time,
) (*Membership, error) {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var orgID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO organizations (name, slug, created_at)
		VALUES ($1, $2, $3)
		RETURNING id
	`, orgName, slug, createdAt).Scan(&orgID); err != nil {
		return nil, err
	}

	var memberID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO members (organization_id, email, role, created_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, orgID, email, RoleOrgAdmin, createdAt).Scan(&memberID); err != nil {
		return nil, err
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE sessions SET active_member_id = $2 WHERE id = $1
	`, sessionID, memberID)
	if err != nil {
		return nil, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, fmt.Errorf("session not found")
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &Membership{
		MemberID:         memberID,
		OrganizationID:   orgID,
		OrganizationName: orgName,
		OrganizationSlug: slug,
		Role:             RoleOrgAdmin,
	}, nil
}

// GetMembershipByID loads a membership by member ID.
func (r *Repository) GetMembershipByID(ctx context.Context, memberID string) (*Membership, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT m.id, m.organization_id, o.name, o.slug, o.logo_image_key, m.role, m.email
		FROM members m
		JOIN organizations o ON o.id = m.organization_id
		WHERE m.id = $1
	`, memberID)

	var m Membership
	var role, email string
	if err := row.Scan(&m.MemberID, &m.OrganizationID, &m.OrganizationName, &m.OrganizationSlug, &m.OrganizationLogoKey, &role, &email); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	m.Role = MemberRole(role)
	m.Email = email
	return &m, nil
}

// SetActiveMember updates the active member on a session.
func (r *Repository) SetActiveMember(ctx context.Context, sessionID, memberID string) error {
	result, err := r.db.Pool.ExecContext(ctx, `
		UPDATE sessions SET active_member_id = $2 WHERE id = $1
	`, sessionID, memberID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("session not found")
	}
	return nil
}
