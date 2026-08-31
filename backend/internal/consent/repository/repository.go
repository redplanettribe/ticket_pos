// Package repository provides hand-written SQL data access for consent: the
// Policy Versions today, the Consent Records from #251.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Repository is the consent module's data access.
type Repository struct {
	db *platform.DB
}

// New builds a Repository over the shared connection pool.
func New(db *platform.DB) *Repository {
	return &Repository{db: db}
}

// PolicyVersion is one published edition of the Privacy Policy, as the
// `policy_versions` row records it.
//
// It carries no text of its own: this row is the label a human names the
// edition by, the day it took effect, and the fingerprint that ties the two to
// the words. The words are the edition's artifact rows
// (policy_version_artifacts, migration 109) — read WITH this row and never
// separately, see CurrentPolicyEdition.
type PolicyVersion struct {
	ID            string
	Label         string
	EffectiveDate time.Time
	ContentHash   string
}

// ErrNoCurrentPolicyVersion reports that no edition has taken effect. The
// service maps it to a domain error; see internal/consent/errors.go for why it
// should be unreachable.
var ErrNoCurrentPolicyVersion = errors.New("no current policy version")

// CurrentPolicyVersion reads the edition in effect: the latest effective date
// that has arrived, ties broken by insertion order.
//
// The `effective_date <= CURRENT_DATE` filter is what lets an edition be
// SCHEDULED — merged and reviewed today, current on its own date, with no
// deploy in between (see migration 060). Without it, adding the row would be
// the publication, and "effective date" would be decoration.
//
// A CANCELLED EDITION IS EXCLUDED BY THE SAME WHERE CLAUSE (#564, migration
// 113). The row is retained and marked, never deleted, so without this line a
// withdrawn edition would become current on its own date exactly as a live one
// does — the very property above, working against the operator who changed
// their mind. It is checked here rather than by anything firing at midnight, for
// the same reason the date is.
//
// CURRENT_DATE is the database's day, which is UTC in every environment this
// runs in. That is the coarsest thing about this query and it is fine: an
// edition becoming current a few hours early or late relative to Ecuador is
// invisible to everyone, because what changes at the boundary is which label a
// re-acceptance is recorded under, not whether anyone is asked.
func (r *Repository) CurrentPolicyVersion(ctx context.Context) (PolicyVersion, error) {
	const query = `
		SELECT id, label, effective_date, content_hash
		FROM policy_versions
		WHERE effective_date <= CURRENT_DATE
		  AND cancelled_at IS NULL
		ORDER BY effective_date DESC, created_at DESC
		LIMIT 1
	`

	var version PolicyVersion
	err := r.db.Pool.QueryRowContext(ctx, query).Scan(
		&version.ID,
		&version.Label,
		&version.EffectiveDate,
		&version.ContentHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PolicyVersion{}, ErrNoCurrentPolicyVersion
	}
	if err != nil {
		return PolicyVersion{}, fmt.Errorf("current policy version: %w", err)
	}
	return version, nil
}
