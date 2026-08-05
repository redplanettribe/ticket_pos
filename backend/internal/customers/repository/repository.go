// Package repository provides hand-written SQL data access for the customers
// domain. Customers owns the platform-global Customer record (ADR 0010).
package repository

import (
	"context"
	"database/sql"
	"errors"
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

// UpsertInput is a Customer to create or reuse: the buyer exactly as the Ticket
// Sale that triggered this call collected them, plus the moment it happened.
//
// Customer.Email must already be normalised (lowercase, trimmed) by the service
// — this layer holds no business rules. Everything else arrives verbatim,
// including Customer.SelfAsserted, which is the whole of this statement's
// authority to overwrite a Verified Customer's stored values; see Upsert.
type UpsertInput struct {
	Customer platform.SaleCustomer
	Now      time.Time
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
// because in this system a name cannot become blank after being set. Only three
// statements ever write these columns: this one; VerifyCustomer — which writes
// empty strings on insert alone and never touches names on conflict; and
// UpdateProfile, the Customer Area's "My info" editor, which rejects a blank
// first or last name for exactly this reason (#102). Every path that reaches
// this one is a Ticket Sale, and every sale entry path likewise rejects a blank
// name before it gets here. So "currently blank" and "never set" cannot
// diverge; should a write path that can blank a set name ever be added, this
// guard must be replaced by one that records whether a name was ever set rather
// than inspecting the current value.
//
// The Tax ID below follows the same shape with one asymmetry of its own
// (ADR 0016). Like the name it may fill a blank on any channel and refresh
// freely while the Customer is unverified. Unlike the name it has an override:
// when the sale was transacted under this Customer's own Customer Session
// (in.Customer.SelfAsserted), the value is the person's own assertion about
// themselves and replaces the stored one even on a Verified Customer. The name
// has no such escape because nothing proves a name; ownership of the email is
// exactly what the session proves, and the Tax ID is the thing the person is
// entitled to restate.
//
// A sale carrying no Tax ID at all — the `import` channel, where the ID may
// never have been collected — writes nothing: the EXCLUDED-is-not-null guard
// below means an import can never blank a Tax ID somebody supplied.
//
// The phone (#107, parent #103) is guarded IDENTICALLY to the Tax ID, clause for
// clause, and that is a deliberate copy rather than an accident of shape. The
// tampering vector is the same one: anyone can type a known email address into a
// guest checkout, so without the guard a stranger could silently rewrite a
// Verified Customer's stored number — and a phone is if anything more likely
// than a Tax ID to be used later as a support identity check. The three arms
// that open the write are the same three, for the same reasons: a Customer who
// has never set a phone may be seeded by any checkout, an unverified record
// self-corrects until somebody claims it, and a checkout under the person's own
// Customer Session is that person restating a fact about themselves.
//
// Both CASE clauses read the same $7 — in.Customer.SelfAsserted — and that is
// the point of where the flag lives (#111). It is a fact about the buyer's
// CHECKOUT, not about either value it protects: the begin-checkout carried this
// Customer's own full Customer Session, and that one proof of email ownership is
// what authorises every write here. A second boolean per guarded column could
// only ever hold the same answer, and a future guarded field reads this same
// bind rather than reaching through whichever neighbour happened to carry it.
//
// A checkout that carried no phone leaves EXCLUDED.phone NULL and writes
// nothing: like the import channel and the Tax ID, an absent value never blanks
// one somebody supplied. Clearing a phone is "My info"'s alone (#108).
func (r *Repository) Upsert(ctx context.Context, tx *sql.Tx, in UpsertInput) (string, error) {
	var taxIDType, taxIDNumber any
	if in.Customer.TaxID.Set() {
		taxIDType, taxIDNumber = in.Customer.TaxID.Type, in.Customer.TaxID.Number
	}
	// nil, not "": the guard below distinguishes "the buyer gave a phone" from
	// "they did not" by NULL-ness, and an empty string is a value that would
	// pass IS NOT NULL and blank a stored number.
	var phone any
	if in.Customer.Phone != "" {
		phone = in.Customer.Phone
	}

	var id string
	err := tx.QueryRowContext(ctx, `
		INSERT INTO customers (email, first_name, last_name, tax_id_type, tax_id_number, phone, created_at)
		VALUES ($1, $2, $3, $5, $6, $8, $4)
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
				THEN EXCLUDED.tax_id_number ELSE customers.tax_id_number END,
			phone = CASE WHEN EXCLUDED.phone IS NOT NULL
				AND (customers.phone IS NULL OR customers.verified_at IS NULL OR $7)
				THEN EXCLUDED.phone ELSE customers.phone END
		RETURNING id
	`, in.Customer.Email, in.Customer.FirstName, in.Customer.LastName, in.Now,
		taxIDType, taxIDNumber, in.Customer.SelfAsserted, phone).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// UpdateProfileInput is a Customer's own edit of what the platform holds about
// them: their name, their one current Tax ID assertion, and their phone number.
//
// TaxIDType and TaxIDNumber are null together to clear the Tax ID and set
// together to assert one; the service never hands this layer one without the
// other, and the customers_tax_id_pair_ck constraint would refuse it if it did.
// There is no email here, and deliberately so: the address is the Customer's
// identity (ADR 0010) and this statement cannot touch it.
//
// The phone takes THREE states rather than the Tax ID's two, which is why it
// needs a flag beside the pointer (#108):
//
//	PhoneSet=false            — this edit is not about the phone; leave it be
//	PhoneSet=true, Phone=nil  — clear it
//	PhoneSet=true, Phone=&"…" — store this canonical E.164 number
//
// The third state exists because the phone joined a contract that already
// existed. A client written before #108 sends no phone at all, and reading that
// silence as "remove it" would have a deploy window's worth of stale Storefront
// pods quietly erasing numbers their buyers had stored. The Tax ID never needed
// the distinction: it has been on this endpoint since it was written, so absence
// there can only mean the Customer emptied the field.
type UpdateProfileInput struct {
	CustomerID  string
	FirstName   string
	LastName    string
	TaxIDType   *string
	TaxIDNumber *string
	PhoneSet    bool
	Phone       *string
}

// UpdateProfile writes the Customer's own assertion about themselves and returns
// the record as it now stands.
//
// It is the only statement in the system that may clear a Tax ID or a phone
// number, and the only one that writes a name outside a Ticket Sale. All three
// facts are load-bearing at Upsert above: its guard reads "blank name" as "never
// named", which stays true only because the service in front of this refuses a
// blank first or last name, and its phone arm never blanks a stored number
// because withdrawing a detail is the person's own decision to make here (#108).
//
// The phone's CASE is the three states of UpdateProfileInput read back out: when
// $6 is false the column is written with its own current value, which is the
// closest SQL gets to saying nothing about it. It carries none of Upsert's
// verification guard, and needs none — this statement is reached only through a
// full Customer Session, so the person editing has already proven ownership of
// the address, which is exactly what an anonymous checkout could not.
//
// Nothing here touches a Ticket Sale. The Customer holds what the person
// asserts now; each sale holds what was transacted, immutably (ADR 0016).
func (r *Repository) UpdateProfile(ctx context.Context, in UpdateProfileInput) (*Customer, error) {
	c, err := scanCustomer(r.db.Pool.QueryRowContext(ctx, `
		UPDATE customers
		SET first_name = $2, last_name = $3, tax_id_type = $4, tax_id_number = $5,
			phone = CASE WHEN $6::boolean THEN $7::text ELSE customers.phone END
		WHERE id = $1
		RETURNING `+customerColumns+`
	`, in.CustomerID, in.FirstName, in.LastName, in.TaxIDType, in.TaxIDNumber, in.PhoneSet, in.Phone))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// UpdateAvatarKey writes the Customer's Avatar object key — a set key attaches
// an Avatar, null removes it — and returns the record as it now stands.
//
// The key must already be validated by the service as belonging to this
// Customer's own object prefix. Nothing else on the record is touched: the
// Avatar is presentation, and setting or removing one asserts nothing about
// name, Tax ID, or verification.
func (r *Repository) UpdateAvatarKey(ctx context.Context, customerID string, key *string) (*Customer, error) {
	c, err := scanCustomer(r.db.Pool.QueryRowContext(ctx, `
		UPDATE customers
		SET avatar_image_key = $2
		WHERE id = $1
		RETURNING `+customerColumns+`
	`, customerID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// SeedAvatarKey sets the Avatar object key only when the Customer has none,
// reporting whether it was written. It is the set-once half of Google Sign-In
// seeding: the WHERE clause is the rule that a seeded picture may fill an empty
// slot and may never replace an Avatar the person already has — checked here,
// atomically, rather than by a read the moment before.
func (r *Repository) SeedAvatarKey(ctx context.Context, customerID, key string) (bool, error) {
	result, err := r.db.Pool.ExecContext(ctx, `
		UPDATE customers
		SET avatar_image_key = $2
		WHERE id = $1 AND avatar_image_key IS NULL
	`, customerID, key)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}
