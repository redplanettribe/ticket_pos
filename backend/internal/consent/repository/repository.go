// Package repository provides hand-written SQL data access for consent: the
// Policy Versions today, the Consent Records from #251.
package repository

import (
	"errors"
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
