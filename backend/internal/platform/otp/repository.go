package otp

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Challenge is a stored one-time passcode challenge.
type Challenge struct {
	ID          string
	Purpose     Purpose
	Email       string
	CodeHash    string
	RequestIP   string
	ExpiresAt   time.Time
	Attempts    int
	Invalidated bool
	CreatedAt   time.Time
}

// Repository provides SQL access for OTP challenges.
type Repository struct {
	db *platform.DB
}

// NewRepository returns a repository backed by the given database pool.
func NewRepository(db *platform.DB) *Repository {
	return &Repository{db: db}
}

// CountRequestsByEmail counts challenges of a purpose created for an email since the given time.
func (r *Repository) CountRequestsByEmail(ctx context.Context, purpose Purpose, email string, since time.Time) (int, error) {
	var count int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM otp_challenges
		WHERE purpose = $1 AND email = $2 AND created_at >= $3
	`, string(purpose), email, since).Scan(&count)
	return count, err
}

// CountRequestsByIP counts challenges of a purpose created for an IP since the given time.
func (r *Repository) CountRequestsByIP(ctx context.Context, purpose Purpose, ip string, since time.Time) (int, error) {
	if ip == "" {
		return 0, nil
	}
	var count int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM otp_challenges
		WHERE purpose = $1 AND request_ip = $2 AND created_at >= $3
	`, string(purpose), ip, since).Scan(&count)
	return count, err
}

// CountRequestsSince counts challenges of every purpose created since the given
// time, platform-wide.
//
// This is the global outbound ceiling's counter, and it is deliberately the
// otp_challenges table rather than a process-local tally: the API is a single
// Cloud Run service that scales to several instances, and an in-process counter
// would let N instances each send a full ceiling's worth. One row is already
// written per passcode issued, so the durable, shared count exists for free.
func (r *Repository) CountRequestsSince(ctx context.Context, since time.Time) (int, error) {
	var count int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM otp_challenges
		WHERE created_at >= $1
	`, since).Scan(&count)
	return count, err
}

// Create inserts a new challenge.
func (r *Repository) Create(ctx context.Context, challenge Challenge) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO otp_challenges (id, purpose, email, code_hash, request_ip, expires_at, attempts, invalidated, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, challenge.ID, string(challenge.Purpose), challenge.Email, challenge.CodeHash, challenge.RequestIP,
		challenge.ExpiresAt, challenge.Attempts, challenge.Invalidated, challenge.CreatedAt)
	return err
}

// LatestChallenge returns the newest active challenge for an email and purpose.
func (r *Repository) LatestChallenge(ctx context.Context, purpose Purpose, email string) (*Challenge, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT id, purpose, email, code_hash, request_ip, expires_at, attempts, invalidated, created_at
		FROM otp_challenges
		WHERE purpose = $1 AND email = $2 AND invalidated = FALSE
		ORDER BY created_at DESC
		LIMIT 1
	`, string(purpose), email)

	var c Challenge
	var challengePurpose string
	if err := row.Scan(&c.ID, &challengePurpose, &c.Email, &c.CodeHash, &c.RequestIP, &c.ExpiresAt, &c.Attempts, &c.Invalidated, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	c.Purpose = Purpose(challengePurpose)
	return &c, nil
}

// IncrementAttempts increments the wrong-attempt count for a challenge.
func (r *Repository) IncrementAttempts(ctx context.Context, id string) (int, error) {
	var attempts int
	err := r.db.Pool.QueryRowContext(ctx, `
		UPDATE otp_challenges
		SET attempts = attempts + 1
		WHERE id = $1
		RETURNING attempts
	`, id).Scan(&attempts)
	return attempts, err
}

// Invalidate marks a challenge as no longer usable.
func (r *Repository) Invalidate(ctx context.Context, id string) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		UPDATE otp_challenges SET invalidated = TRUE WHERE id = $1
	`, id)
	return err
}

// InvalidateForEmail invalidates all active challenges for an email and purpose.
func (r *Repository) InvalidateForEmail(ctx context.Context, purpose Purpose, email string) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		UPDATE otp_challenges SET invalidated = TRUE
		WHERE purpose = $1 AND email = $2 AND invalidated = FALSE
	`, string(purpose), email)
	return err
}
