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

// UpsertInput is a Customer to create or reuse. Email must already be normalised
// (lowercase, trimmed) by the service — this layer holds no business rules.
type UpsertInput struct {
	Email     string
	FirstName string
	LastName  string
	// TaxID is the Tax ID this sale was transacted under, unset on a sale that
	// carries none (the `import` channel). Its SelfAsserted flag decides whether
	// a Verified Customer's stored Tax ID may be overwritten; see Upsert.
	//
	// That flag describes the *checkout*, not the Tax ID: it reports that the
	// begin-checkout ran under this Customer's own full Customer Session. Phone
	// below is guarded by the same flag for that reason (#107).
	TaxID platform.SaleTaxID
	// Phone is the buyer's phone number in canonical E.164 form as typed at
	// checkout, empty when they gave none or when the channel never collects one
	// (import, and the staff-recorded sale). It is written back onto the Customer
	// under the same guard as the Tax ID; see Upsert.
	//
	// Unlike the Tax ID it is NOT recorded on the Ticket Sale — a phone number is
	// no fiscal fact of a sale (#103, #107) — so this is the only column any of
	// it reaches.
	Phone string
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
// (in.TaxID.SelfAsserted), the value is the person's own assertion about
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
// It reads $7 — in.TaxID.SelfAsserted — because that flag describes the
// CHECKOUT, not the Tax ID: it records that the begin-checkout carried this
// Customer's own full Customer Session. It is the same proof of email ownership
// whichever value it is protecting, so the phone reuses it rather than
// duplicating a second flag that could only ever hold the same boolean.
//
// A checkout that carried no phone leaves EXCLUDED.phone NULL and writes
// nothing: like the import channel and the Tax ID, an absent value never blanks
// one somebody supplied. Clearing a phone is "My info"'s alone (#108).
func (r *Repository) Upsert(ctx context.Context, tx *sql.Tx, in UpsertInput) (string, error) {
	var taxIDType, taxIDNumber any
	if in.TaxID.Set() {
		taxIDType, taxIDNumber = in.TaxID.Type, in.TaxID.Number
	}
	// nil, not "": the guard below distinguishes "the buyer gave a phone" from
	// "they did not" by NULL-ness, and an empty string is a value that would
	// pass IS NOT NULL and blank a stored number.
	var phone any
	if in.Phone != "" {
		phone = in.Phone
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
	`, in.Email, in.FirstName, in.LastName, in.Now, taxIDType, taxIDNumber, in.TaxID.SelfAsserted, phone).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// UpdateProfileInput is a Customer's own edit of what the platform holds about
// them: their name and their one current Tax ID assertion.
//
// TaxIDType and TaxIDNumber are null together to clear the Tax ID and set
// together to assert one; the service never hands this layer one without the
// other, and the customers_tax_id_pair_ck constraint would refuse it if it did.
// There is no email here, and deliberately so: the address is the Customer's
// identity (ADR 0010) and this statement cannot touch it.
type UpdateProfileInput struct {
	CustomerID  string
	FirstName   string
	LastName    string
	TaxIDType   *string
	TaxIDNumber *string
}

// UpdateProfile writes the Customer's own assertion about themselves and returns
// the record as it now stands.
//
// It is the only statement in the system that may clear a Tax ID, and the only
// one that writes a name outside a Ticket Sale. Both facts are load-bearing at
// Upsert above: its guard reads "blank name" as "never named", which stays true
// only because the service in front of this refuses a blank first or last name.
//
// Nothing here touches a Ticket Sale. The Customer holds what the person
// asserts now; each sale holds what was transacted, immutably (ADR 0016).
func (r *Repository) UpdateProfile(ctx context.Context, in UpdateProfileInput) (*Customer, error) {
	var c Customer
	err := r.db.Pool.QueryRowContext(ctx, `
		UPDATE customers
		SET first_name = $2, last_name = $3, tax_id_type = $4, tax_id_number = $5
		WHERE id = $1
		RETURNING id, email, first_name, last_name, tax_id_type, tax_id_number, avatar_image_key, verified_at
	`, in.CustomerID, in.FirstName, in.LastName, in.TaxIDType, in.TaxIDNumber).
		Scan(&c.ID, &c.Email, &c.FirstName, &c.LastName, &c.TaxIDType, &c.TaxIDNumber, &c.AvatarImageKey, &c.VerifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// UpdateAvatarKey writes the Customer's Avatar object key — a set key attaches
// an Avatar, null removes it — and returns the record as it now stands.
//
// The key must already be validated by the service as belonging to this
// Customer's own object prefix. Nothing else on the record is touched: the
// Avatar is presentation, and setting or removing one asserts nothing about
// name, Tax ID, or verification.
func (r *Repository) UpdateAvatarKey(ctx context.Context, customerID string, key *string) (*Customer, error) {
	var c Customer
	err := r.db.Pool.QueryRowContext(ctx, `
		UPDATE customers
		SET avatar_image_key = $2
		WHERE id = $1
		RETURNING id, email, first_name, last_name, tax_id_type, tax_id_number, avatar_image_key, verified_at
	`, customerID, key).
		Scan(&c.ID, &c.Email, &c.FirstName, &c.LastName, &c.TaxIDType, &c.TaxIDNumber, &c.AvatarImageKey, &c.VerifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
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
