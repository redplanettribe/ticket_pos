package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Customer is a stored Customer record.
type Customer struct {
	ID        string
	Email     string
	FirstName string
	LastName  string
	// The Customer's one current Tax ID assertion, null together until they have
	// supplied one (ADR 0016). Nullable forever: most Customers on the platform
	// the day this shipped had none and nothing backfills them.
	TaxIDType   sql.NullString
	TaxIDNumber sql.NullString
	// Phone is the Customer's one stored phone number in canonical E.164 form
	// (#103, #107), null until a checkout or a "My info" edit supplies one. It is
	// a single column rather than a dialling code and a national part because its
	// whole purpose is to be handed to the Payment Provider in exactly that form;
	// splitting it for display is the Storefront's business alone (#108).
	//
	// Sparsely populated by design: only online checkout and "My info" ever write
	// it, so a Customer whose every purchase was recorded by staff has none. That
	// is correct for a prefill and would be wrong for anything that later reads
	// this as "the Customer's phone number".
	Phone sql.NullString
	// AvatarImageKey locates the Customer's Avatar in object storage, null when
	// they have none. Whether it was uploaded or seeded from Google Sign-In is
	// not recorded — once stored, every Avatar is the same kind of thing.
	AvatarImageKey sql.NullString
	VerifiedAt     sql.NullTime
	// DigestLocale is the language this Customer's Follow Digest is written in,
	// remembered from the Storefront they last signed in on (ADR 0030). Never
	// empty: a Customer a box office sale created has never been on a localized
	// surface, and carries the column's English default rather than nothing.
	DigestLocale string
	// DigestEnabled is whether this Customer's Follow Digest is switched on
	// (#224, ADR 0030). NOT NULL DEFAULT TRUE, so there is no third state: a
	// Customer who has never touched the switch carries the consent they gave by
	// pressing Follow, and this column records only its withdrawal.
	//
	// It is read back with every Customer rather than by a query of its own,
	// because the one surface that publishes it — the Follows listing — has to
	// say "the Digest is off and your Follows still stand" in one breath, and two
	// reads that can disagree is how that sentence becomes a lie.
	//
	// IT GATES NO TRANSACTIONAL MAIL. Nothing that sends a One-time Passcode or a
	// Sale Confirmation reads this field, and nothing ever should: unsubscribing
	// from a weekly discovery email must not cost a person their account or their
	// tickets.
	DigestEnabled bool
}

// customerColumns is every column a Customer is read back with, in the order
// scanCustomer expects. One list because five statements select it.
const customerColumns = `id, email, first_name, last_name, tax_id_type, tax_id_number, phone, avatar_image_key, verified_at, digest_locale, digest_enabled`

// scanRow is either a *sql.Row or a *sql.Rows positioned on one.
type scanRow interface {
	Scan(dest ...any) error
}

// scanCustomer reads one row selected with customerColumns.
func scanCustomer(row scanRow) (*Customer, error) {
	var c Customer
	if err := row.Scan(&c.ID, &c.Email, &c.FirstName, &c.LastName, &c.TaxIDType, &c.TaxIDNumber,
		&c.Phone, &c.AvatarImageKey, &c.VerifiedAt, &c.DigestLocale, &c.DigestEnabled); err != nil {
		return nil, err
	}
	return &c, nil
}

// CustomerSession is a stored Customer Session.
//
// TicketSaleID is the session's scope: invalid (SQL NULL) means a full Customer
// Session spanning every Ticket Sale the Customer owns. A set value narrows the
// session to that one sale, which is what redeeming a Confirmation Link mints.
type CustomerSession struct {
	ID           string
	CustomerID   string
	TicketSaleID sql.NullString
	ExpiresAt    time.Time
	CreatedAt    time.Time
}

// GetCustomerByEmail returns the Customer for a normalised email, or nil when no
// record exists. The email must already be normalised by the service.
func (r *Repository) GetCustomerByEmail(ctx context.Context, email string) (*Customer, error) {
	c, err := scanCustomer(r.db.Pool.QueryRowContext(ctx, `
		SELECT `+customerColumns+`
		FROM customers WHERE email = $1
	`, email))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// GetCustomerByID returns the Customer with the given id, or nil when it no
// longer exists.
func (r *Repository) GetCustomerByID(ctx context.Context, id string) (*Customer, error) {
	c, err := scanCustomer(r.db.Pool.QueryRowContext(ctx, `
		SELECT `+customerColumns+`
		FROM customers WHERE id = $1
	`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// VerifyCustomer creates the Customer for a normalised email if none exists and
// marks it verified, returning the record. verified_at is stamped only the first
// time, so the moment a person claimed their record is preserved.
//
// This is the only write in the system that sets verified_at. No Sales Channel,
// no import, and no Confirmation Link redemption may reach it: only a completed
// one-time passcode proves ownership of the address.
//
// digestLocale is the Locale of the Storefront this sign-in happened on, or
// empty when the caller has no page to name one from. Empty leaves the stored
// value exactly as it was — the column's English default on a record no
// localized surface has ever touched — rather than blanking it, which is why the
// guard is COALESCE on the parameter and not on the column. A Locale that IS
// named always wins: it is the language the person was reading a moment ago, and
// it is what their next Follow Digest must be written in (ADR 0030).
func (r *Repository) VerifyCustomer(ctx context.Context, email string, now time.Time, digestLocale string) (*Customer, error) {
	// Two binds for one value: $3 is NULL when no Locale was named, which is what
	// leaves an existing Customer's remembered one alone; $4 is what a brand new
	// record starts at, and cannot be NULL because the column is NOT NULL.
	var named any
	fresh := string(platform.DefaultLocale)
	if digestLocale != "" {
		named, fresh = digestLocale, digestLocale
	}
	c, err := scanCustomer(r.db.Pool.QueryRowContext(ctx, `
		INSERT INTO customers (email, first_name, last_name, verified_at, created_at, digest_locale)
		VALUES ($1, '', '', $2, $2, $4)
		ON CONFLICT (email) DO UPDATE SET
			verified_at = COALESCE(customers.verified_at, EXCLUDED.verified_at),
			digest_locale = COALESCE($3, customers.digest_locale)
		RETURNING `+customerColumns+`
	`, email, now, named, fresh))
	if err != nil {
		return nil, err
	}
	return c, nil
}

// GetTicketSaleCustomer returns the Customer a Ticket Sale belongs to, and
// whether the sale exists at all.
//
// It is the one lookup a Confirmation Link redemption performs. The sale id
// comes from the token's signed payload, never from a caller-supplied
// parameter, so this cannot be aimed at an arbitrary sale by anyone without the
// signing key. Nothing about the sale's status is consulted: a reversed sale
// still opens, because saying plainly that a purchase was reversed is more use
// to the holder than a dead link.
func (r *Repository) GetTicketSaleCustomer(ctx context.Context, ticketSaleID string) (string, bool, error) {
	var customerID string
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT customer_id FROM ticket_sales WHERE id = $1
	`, ticketSaleID).Scan(&customerID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return customerID, true, nil
}

// CreateSession inserts a Customer Session.
func (r *Repository) CreateSession(ctx context.Context, s CustomerSession) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO customer_sessions (id, customer_id, ticket_sale_id, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, s.ID, s.CustomerID, s.TicketSaleID, s.ExpiresAt, s.CreatedAt)
	return err
}

// GetSession returns the Customer Session for a token, or nil when none exists.
func (r *Repository) GetSession(ctx context.Context, token string) (*CustomerSession, error) {
	var s CustomerSession
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT id, customer_id, ticket_sale_id, expires_at, created_at
		FROM customer_sessions WHERE id = $1
	`, token).Scan(&s.ID, &s.CustomerID, &s.TicketSaleID, &s.ExpiresAt, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ExtendSession pushes a Customer Session's sliding expiry out.
func (r *Repository) ExtendSession(ctx context.Context, token string, expiresAt time.Time) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		UPDATE customer_sessions SET expires_at = $2 WHERE id = $1
	`, token, expiresAt)
	return err
}

// DeleteSession destroys a Customer Session. Sessions are server-side rows
// precisely so this is possible.
func (r *Repository) DeleteSession(ctx context.Context, token string) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		DELETE FROM customer_sessions WHERE id = $1
	`, token)
	return err
}
