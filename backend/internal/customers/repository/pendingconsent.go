package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PendingConsent is a sign-in held at the consent step: an address that has
// just been proven, a Customer Session that has not been minted, and the
// short-lived single-use token that is the only thing able to finish the job.
type PendingConsent struct {
	ID         string
	CustomerID string
	Email      string
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

// CreatePendingConsent stores a pending-consent token.
func (r *Repository) CreatePendingConsent(ctx context.Context, p PendingConsent) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO pending_consents (id, customer_id, email, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, p.ID, p.CustomerID, p.Email, p.ExpiresAt, p.CreatedAt)
	return err
}

// ConsumePendingConsent redeems a pending-consent token, returning it and
// deleting it in ONE STATEMENT.
//
// The DELETE ... RETURNING is what makes the token single-use, and it is single
// use in the only sense that survives concurrency: two requests presenting the
// same token race for one row, and exactly one of them gets it back — the other
// sees no rows and is refused, rather than both minting a session from one
// proof. A read-then-delete pair would have left that window open.
//
// The expiry is checked by the caller rather than in this WHERE clause, on
// purpose: an expired row is consumed and destroyed here too. A token whose
// window has closed should not stay on the table for a later attempt to keep
// finding, and burning it costs nothing — the caller refuses it either way,
// with the same code an unknown token gets.
func (r *Repository) ConsumePendingConsent(ctx context.Context, token string) (*PendingConsent, error) {
	if token == "" {
		return nil, nil
	}
	var p PendingConsent
	err := r.db.Pool.QueryRowContext(ctx, `
		DELETE FROM pending_consents
		WHERE id = $1
		RETURNING id, customer_id, email, expires_at, created_at
	`, token).Scan(&p.ID, &p.CustomerID, &p.Email, &p.ExpiresAt, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}
