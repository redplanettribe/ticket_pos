package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PendingStaffTerms is one Proof of Email Ownership held in suspension for the
// terms step: the staff counterpart of the customer side's pending consent row
// (migration 063), created when a proven email owes a Terms Acceptance and
// spent by the submission that records one (#538, ADR 0066).
type PendingStaffTerms struct {
	ID        string
	Email     string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// StaffTermsAcceptance is the append-only evidence row a terms submission
// writes: who accepted which edition, in what capacity, when, and under what
// circumstances (migration 107).
type StaffTermsAcceptance struct {
	Email          string
	TermsVersionID string
	Capacity       string
	AcceptedAt     time.Time
	IP             string
	UserAgent      string
	SessionID      string
	OriginURL      string
}

// HasTermsAcceptance reports whether this email has accepted this Terms
// edition in any capacity — the one indexed lookup the sign-in gate performs.
// No row for the current edition IS the outstanding state; there is no
// current-state column anywhere (migration 107).
func (r *Repository) HasTermsAcceptance(ctx context.Context, email, termsVersionID string) (bool, error) {
	var exists bool
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM staff_terms_acceptances
			WHERE email = $1 AND terms_version_id = $2
		)
	`, email, termsVersionID).Scan(&exists)
	return exists, err
}

// InsertTermsAcceptance appends one acceptance row. ON CONFLICT DO NOTHING
// against the (email, edition, capacity) uniqueness: a double-submit race
// appends one row and never two, and the second writer proceeds as if it had
// written — the acceptance it wanted recorded IS recorded.
//
// Evidence strings arrive possibly empty and are stored as NULL: "not
// collected" and "collected as blank" are different answers, and the columns
// mean the former (migration 061).
func (r *Repository) InsertTermsAcceptance(ctx context.Context, acceptance StaffTermsAcceptance) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO staff_terms_acceptances
			(email, terms_version_id, capacity, accepted_at, ip, user_agent, session_id, origin_url)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''))
		ON CONFLICT (email, terms_version_id, capacity) DO NOTHING
	`, acceptance.Email, acceptance.TermsVersionID, acceptance.Capacity, acceptance.AcceptedAt,
		acceptance.IP, acceptance.UserAgent, acceptance.SessionID, acceptance.OriginURL)
	return err
}

// CreatePendingStaffTerms holds a proven email for the terms step.
func (r *Repository) CreatePendingStaffTerms(ctx context.Context, pending PendingStaffTerms) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO pending_staff_terms (id, email, expires_at, created_at)
		VALUES ($1, $2, $3, $4)
	`, pending.ID, pending.Email, pending.ExpiresAt, pending.CreatedAt)
	return err
}

// ConsumePendingStaffTerms spends a pending-terms token: the row is deleted by
// the same statement that reads it, so a token cannot be presented twice —
// pending_consents' rule, kept here. Nil means unknown or already spent, which
// the caller reports indistinguishably from expired.
func (r *Repository) ConsumePendingStaffTerms(ctx context.Context, id string) (*PendingStaffTerms, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		DELETE FROM pending_staff_terms
		WHERE id = $1
		RETURNING id, email, expires_at, created_at
	`, id)

	var pending PendingStaffTerms
	if err := row.Scan(&pending.ID, &pending.Email, &pending.ExpiresAt, &pending.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &pending, nil
}
