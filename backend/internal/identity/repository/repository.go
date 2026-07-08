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
	RoleOrgAdmin    MemberRole = "org_admin"
	RoleEventOwner  MemberRole = "event_owner"
	RoleEventStaff  MemberRole = "event_staff"
)

// OTPChallenge is a stored OTP verification challenge.
type OTPChallenge struct {
	ID          string
	Email       string
	CodeHash    string
	RequestIP   string
	ExpiresAt   time.Time
	Attempts    int
	Invalidated bool
	CreatedAt   time.Time
}

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
	MemberID         string
	OrganizationID   string
	OrganizationName string
	OrganizationSlug string
	Role             MemberRole
	Email            string
}

// Repository provides SQL access for identity data.
type Repository struct {
	db *platform.DB
}

// New returns a repository backed by the given database pool.
func New(db *platform.DB) *Repository {
	return &Repository{db: db}
}

// CountOTPRequestsByEmail counts OTP challenges created for an email since the given time.
func (r *Repository) CountOTPRequestsByEmail(ctx context.Context, email string, since time.Time) (int, error) {
	var count int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM otp_challenges
		WHERE email = $1 AND created_at >= $2
	`, email, since).Scan(&count)
	return count, err
}

// CountOTPRequestsByIP counts OTP challenges created for an IP since the given time.
func (r *Repository) CountOTPRequestsByIP(ctx context.Context, ip string, since time.Time) (int, error) {
	if ip == "" {
		return 0, nil
	}
	var count int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM otp_challenges
		WHERE request_ip = $1 AND created_at >= $2
	`, ip, since).Scan(&count)
	return count, err
}

// CreateOTPChallenge inserts a new OTP challenge.
func (r *Repository) CreateOTPChallenge(ctx context.Context, challenge OTPChallenge) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO otp_challenges (id, email, code_hash, request_ip, expires_at, attempts, invalidated, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, challenge.ID, challenge.Email, challenge.CodeHash, challenge.RequestIP, challenge.ExpiresAt, challenge.Attempts, challenge.Invalidated, challenge.CreatedAt)
	return err
}

// LatestOTPChallenge returns the newest active challenge for an email.
func (r *Repository) LatestOTPChallenge(ctx context.Context, email string) (*OTPChallenge, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT id, email, code_hash, request_ip, expires_at, attempts, invalidated, created_at
		FROM otp_challenges
		WHERE email = $1 AND invalidated = FALSE
		ORDER BY created_at DESC
		LIMIT 1
	`, email)

	var c OTPChallenge
	if err := row.Scan(&c.ID, &c.Email, &c.CodeHash, &c.RequestIP, &c.ExpiresAt, &c.Attempts, &c.Invalidated, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// IncrementOTPAttempts increments wrong-attempt count for a challenge.
func (r *Repository) IncrementOTPAttempts(ctx context.Context, id string) (int, error) {
	var attempts int
	err := r.db.Pool.QueryRowContext(ctx, `
		UPDATE otp_challenges
		SET attempts = attempts + 1
		WHERE id = $1
		RETURNING attempts
	`, id).Scan(&attempts)
	return attempts, err
}

// InvalidateOTPChallenge marks a challenge as no longer usable.
func (r *Repository) InvalidateOTPChallenge(ctx context.Context, id string) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		UPDATE otp_challenges SET invalidated = TRUE WHERE id = $1
	`, id)
	return err
}

// InvalidateOTPChallengesForEmail invalidates all active challenges for an email.
func (r *Repository) InvalidateOTPChallengesForEmail(ctx context.Context, email string) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		UPDATE otp_challenges SET invalidated = TRUE
		WHERE email = $1 AND invalidated = FALSE
	`, email)
	return err
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
		SELECT m.id, m.organization_id, o.name, o.slug, m.role, m.email
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
		if err := rows.Scan(&m.MemberID, &m.OrganizationID, &m.OrganizationName, &m.OrganizationSlug, &role, &m.Email); err != nil {
			return nil, err
		}
		m.Role = MemberRole(role)
		memberships = append(memberships, m)
	}
	return memberships, rows.Err()
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
		SELECT m.id, m.organization_id, o.name, o.slug, m.role, m.email
		FROM members m
		JOIN organizations o ON o.id = m.organization_id
		WHERE m.id = $1
	`, memberID)

	var m Membership
	var role, email string
	if err := row.Scan(&m.MemberID, &m.OrganizationID, &m.OrganizationName, &m.OrganizationSlug, &role, &email); err != nil {
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
