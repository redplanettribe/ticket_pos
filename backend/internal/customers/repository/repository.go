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
	// TaxID is the Tax ID this sale was transacted under, unset on a sale that
	// carries none (the `import` channel). Its SelfAsserted flag decides whether
	// a Verified Customer's stored Tax ID may be overwritten; see Upsert.
	TaxID platform.SaleTaxID
	Now   time.Time
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
//
// The Tax ID below follows the same shape with one asymmetry of its own
// (ADR 0016). Like the name it may fill a blank on any channel and refresh
// freely while the Customer is unverified. Unlike the name it has an override:
// when the sale was transacted under this Customer's own Customer Session
// (in.TaxID.SelfAsserted), the value is the person's own assertion about
// themselves and replaces the stored one even on a Verified Customer. The name
// has no such escape because nothing proves a name; ownership of the email is
// exactly what the session proves, and the Tax ID is the thing the person is
// entitled to restate.
//
// A sale carrying no Tax ID at all — the `import` channel, where the ID may
// never have been collected — writes nothing: the EXCLUDED-is-not-null guard
// below means an import can never blank a Tax ID somebody supplied.
func (r *Repository) Upsert(ctx context.Context, tx *sql.Tx, in UpsertInput) (string, error) {
	var taxIDType, taxIDNumber any
	if in.TaxID.Set() {
		taxIDType, taxIDNumber = in.TaxID.Type, in.TaxID.Number
	}

	var id string
	err := tx.QueryRowContext(ctx, `
		INSERT INTO customers (email, first_name, last_name, tax_id_type, tax_id_number, created_at)
		VALUES ($1, $2, $3, $5, $6, $4)
		ON CONFLICT (email) DO UPDATE SET
			first_name = CASE WHEN customers.verified_at IS NULL OR (customers.first_name = '' AND customers.last_name = '')
				THEN EXCLUDED.first_name ELSE customers.first_name END,
			last_name  = CASE WHEN customers.verified_at IS NULL OR (customers.first_name = '' AND customers.last_name = '')
				THEN EXCLUDED.last_name  ELSE customers.last_name  END,
			tax_id_type = CASE WHEN EXCLUDED.tax_id_type IS NOT NULL
				AND (customers.tax_id_type IS NULL OR customers.verified_at IS NULL OR $7)
				THEN EXCLUDED.tax_id_type ELSE customers.tax_id_type END,
			tax_id_number = CASE WHEN EXCLUDED.tax_id_number IS NOT NULL
				AND (customers.tax_id_type IS NULL OR customers.verified_at IS NULL OR $7)
				THEN EXCLUDED.tax_id_number ELSE customers.tax_id_number END
		RETURNING id
	`, in.Email, in.FirstName, in.LastName, in.Now, taxIDType, taxIDNumber, in.TaxID.SelfAsserted).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}
