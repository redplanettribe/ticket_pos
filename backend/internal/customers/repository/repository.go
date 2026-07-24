// Package repository provides hand-written SQL data access for the customers
// domain. Customers owns the platform-global Customer record (ADR 0010).
package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Repository provides SQL access for Customer records.
type Repository struct {
	db *platform.DB
}

// New returns a repository backed by the given database pool.
func New(db *platform.DB) *Repository {
	return &Repository{db: db}
}

// UpsertInput is a Customer to create or reuse. Email must already be normalised
// (lowercase, trimmed) by the service — this layer holds no business rules.
type UpsertInput struct {
	Email     string
	FirstName string
	LastName  string
	Now       time.Time
}

// Upsert creates the Customer for the normalised email, or reuses the existing
// one, returning its id. It runs inside the caller's transaction so that a
// Customer and the Ticket Sale referencing it are recorded atomically.
//
// The profile name is refreshed from this sale only while the Customer is
// unverified; once verified_at is set the person owns their own name and no sale
// may rewrite it. Either way the Ticket Sale's own recorded name is untouched:
// the Customer holds what the person asserts, the sale holds what was transacted.
//
// The one exception is a Customer who verified before ever buying: signing in
// creates the record from an email alone, so it carries no name to own. A first
// sale fills that blank in rather than leaving the person permanently nameless;
// once a name is there, verification protects it as before. The rule, exactly:
// a sale may fill a name that has never been set, and may never overwrite one
// that has (PRD #55, decision 8).
//
// The blank test below is that rule and not merely an approximation of it,
// because in this system a name cannot become blank after being set. Only two
// statements ever write these columns: this one, and VerifyCustomer — which
// writes empty strings on insert alone and never touches names on conflict. Every
// path that reaches this one is a Ticket Sale, and every sale entry path rejects
// a blank first or last name before it gets here. There is no profile editor. So
// "currently blank" and "never set" cannot diverge; should a write path that can
// blank a set name ever be added, this guard must be replaced by one that records
// whether a name was ever set rather than inspecting the current value.
func (r *Repository) Upsert(ctx context.Context, tx *sql.Tx, in UpsertInput) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `
		INSERT INTO customers (email, first_name, last_name, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (email) DO UPDATE SET
			first_name = CASE WHEN customers.verified_at IS NULL OR (customers.first_name = '' AND customers.last_name = '')
				THEN EXCLUDED.first_name ELSE customers.first_name END,
			last_name  = CASE WHEN customers.verified_at IS NULL OR (customers.first_name = '' AND customers.last_name = '')
				THEN EXCLUDED.last_name  ELSE customers.last_name  END
		RETURNING id
	`, in.Email, in.FirstName, in.LastName, in.Now).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}
