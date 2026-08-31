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
	ID    string
	Email string
	// TermsVersionID is the edition the terms step showed, pinned at mint so
	// the acceptance evidences the text that was on screen (#537's rule).
	TermsVersionID string
	// LabelLocale is the language the acceptance label was SERVED in, pinned at
	// mint for the reason the edition beside it is (#567, migration 115): the
	// label is rendered by the request that issues this token and the evidence
	// is written by the request that spends it, and only the first of the two
	// knows what was on screen. It is the locale the renderer actually used —
	// which is not always the one the login page asked for, because an edition
	// that publishes no artifact in that language is floored at the prevailing
	// text (identity/service.acceptanceLabel).
	//
	// Empty is stored as SQL NULL, and reaches the acceptance row as NULL.
	LabelLocale string
	ExpiresAt   time.Time
	CreatedAt   time.Time
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
	// PresentedLocale is the language of the text this person was actually
	// shown, carried here from the held sign-in that showed it (#567). Empty
	// where nothing can say — stored as NULL, never guessed.
	PresentedLocale string
}

// HasSatisfyingTermsAcceptance reports whether this email has accepted, as an
// ORGANIZER, any Terms edition that still satisfies the gate — the one indexed
// lookup the sign-in gate performs. No such row IS the outstanding state; there
// is no current-state column anywhere (migration 107).
//
// MEMBERSHIP, NOT EQUALITY (#560). It used to be equality against the single
// current edition; the argument is now the satisfying set — the gating floor
// and every edition at or above it, corrections included
// (consent/legal.Satisfying). So a correction published under an Organizer's
// feet does not stop them signing in, while a new GATING edition still does.
// The outstanding predicate, over a person row `p`, is the negation:
//
//	NOT EXISTS (SELECT 1 FROM staff_terms_acceptances a
//	            WHERE a.email = p.email AND a.capacity = 'organizer'
//	              AND a.terms_version_id = ANY($1))
//
// `capacity = 'organizer'` IS NAMED EXPLICITLY and must stay named. Migration
// 107's CHECK is deliberately open for a later 'staff' capacity, and a
// capacity-agnostic lookup would read an acceptance made in some other capacity
// as clearance for this gate — which is precisely the mistake having capacities
// at all is meant to prevent.
func (r *Repository) HasSatisfyingTermsAcceptance(ctx context.Context, email string, satisfying []string) (bool, error) {
	var exists bool
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM staff_terms_acceptances a
			WHERE a.email = $1 AND a.capacity = 'organizer'
			  AND a.terms_version_id = ANY($2)
		)
	`, email, satisfying).Scan(&exists)
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
			(email, terms_version_id, capacity, accepted_at, ip, user_agent, session_id, origin_url,
			 presented_locale)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''),
		        NULLIF($9, ''))
		ON CONFLICT (email, terms_version_id, capacity) DO NOTHING
	`, acceptance.Email, acceptance.TermsVersionID, acceptance.Capacity, acceptance.AcceptedAt,
		acceptance.IP, acceptance.UserAgent, acceptance.SessionID, acceptance.OriginURL,
		acceptance.PresentedLocale)
	return err
}

// CreatePendingStaffTerms holds a proven email for the terms step.
func (r *Repository) CreatePendingStaffTerms(ctx context.Context, pending PendingStaffTerms) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO pending_staff_terms (id, email, terms_version_id, label_locale, expires_at, created_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6)
	`, pending.ID, pending.Email, pending.TermsVersionID, pending.LabelLocale, pending.ExpiresAt, pending.CreatedAt)
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
		RETURNING id, email, terms_version_id, label_locale, expires_at, created_at
	`, id)

	var (
		pending     PendingStaffTerms
		labelLocale sql.NullString
	)
	if err := row.Scan(&pending.ID, &pending.Email, &pending.TermsVersionID, &labelLocale, &pending.ExpiresAt, &pending.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	pending.LabelLocale = labelLocale.String
	return &pending, nil
}
