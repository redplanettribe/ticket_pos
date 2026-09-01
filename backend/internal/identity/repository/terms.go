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
	// text (identity/service.termsGateLabels).
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
	// AdulthoodDeclaration is the 18+ box beside the acceptance (#587,
	// ADR 0069): TRUE when this edition asked and the person ticked it, NIL when
	// the edition carried no `label-adulthood-declaration` Artifact and the act
	// therefore never asked.
	//
	// NEVER FALSE, on this table or any other. An untick is refused before
	// anything is written, so no row can exist for somebody who said they were a
	// minor — a permanent, unverified assertion that a named individual is a
	// child, on a table that is never edited and never deleted, is exactly what
	// ADR 0069 refuses to keep. Migration 119 adds no CHECK to say so, because
	// the row IS the acceptance: what stops a false from being written is that
	// nothing writes one.
	//
	// A pointer, and stored although it is derivable from the edition's artifact
	// set, so that a finished fact never depends on how rows are read in the
	// present.
	AdulthoodDeclaration *bool
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
			 presented_locale, adulthood_declaration)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''),
		        NULLIF($9, ''), $10)
		ON CONFLICT (email, terms_version_id, capacity) DO NOTHING
	`, acceptance.Email, acceptance.TermsVersionID, acceptance.Capacity, acceptance.AcceptedAt,
		acceptance.IP, acceptance.UserAgent, acceptance.SessionID, acceptance.OriginURL,
		acceptance.PresentedLocale,
		// No NULLIF: the nil pointer IS the null, and it means "this act did not
		// ask" rather than "collected as blank" (migration 119).
		acceptance.AdulthoodDeclaration)
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

// PendingStaffTerms READS a pending-terms token without spending it (#587).
//
// It exists for the live-session gate and for nothing else. There, an unticked
// box must cost a person a click and no more — the token proves nothing on that
// path, because the session is the credential — and judging the boxes needs the
// EDITION the token pinned, which is on this row. Consuming first to learn it
// and refusing afterwards would burn the token on a misclick, which is exactly
// what "costs nothing but a click" rules out.
//
// The sign-in door does not use this and must not: there the token is a Proof
// of Email Ownership, and spending it before the answer is judged is what stops
// a refused submission being retried against the same proof.
//
// A peek is not a claim. The row is still there afterwards, so the caller must
// still consume it to record anything, and the consume is what settles a race
// between two tabs — this read can go stale between the two, and the consume
// then answers nil, which is the refusal an unknown token already gets.
func (r *Repository) PendingStaffTerms(ctx context.Context, id string) (*PendingStaffTerms, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT id, email, terms_version_id, label_locale, expires_at, created_at
		FROM pending_staff_terms
		WHERE id = $1
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
