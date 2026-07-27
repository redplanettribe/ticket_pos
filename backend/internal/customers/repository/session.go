package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
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
	VerifiedAt  sql.NullTime
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
	var c Customer
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT id, email, first_name, last_name, tax_id_type, tax_id_number, verified_at
		FROM customers WHERE email = $1
	`, email).Scan(&c.ID, &c.Email, &c.FirstName, &c.LastName, &c.TaxIDType, &c.TaxIDNumber, &c.VerifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// GetCustomerByID returns the Customer with the given id, or nil when it no
// longer exists.
func (r *Repository) GetCustomerByID(ctx context.Context, id string) (*Customer, error) {
	var c Customer
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT id, email, first_name, last_name, tax_id_type, tax_id_number, verified_at
		FROM customers WHERE id = $1
	`, id).Scan(&c.ID, &c.Email, &c.FirstName, &c.LastName, &c.TaxIDType, &c.TaxIDNumber, &c.VerifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// VerifyCustomer creates the Customer for a normalised email if none exists and
// marks it verified, returning the record. verified_at is stamped only the first
// time, so the moment a person claimed their record is preserved.
//
// This is the only write in the system that sets verified_at. No Sales Channel,
// no import, and no Confirmation Link redemption may reach it: only a completed
// one-time passcode proves ownership of the address.
func (r *Repository) VerifyCustomer(ctx context.Context, email string, now time.Time) (*Customer, error) {
	var c Customer
	err := r.db.Pool.QueryRowContext(ctx, `
		INSERT INTO customers (email, first_name, last_name, verified_at, created_at)
		VALUES ($1, '', '', $2, $2)
		ON CONFLICT (email) DO UPDATE SET
			verified_at = COALESCE(customers.verified_at, EXCLUDED.verified_at)
		RETURNING id, email, first_name, last_name, tax_id_type, tax_id_number, verified_at
	`, email, now).Scan(&c.ID, &c.Email, &c.FirstName, &c.LastName, &c.TaxIDType, &c.TaxIDNumber, &c.VerifiedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
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
