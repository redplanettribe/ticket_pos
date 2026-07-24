package platform

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// DB wraps a database connection pool for repository use.
type DB struct {
	Pool *sql.DB
}

const (
	// One connection attempt. Long enough for a normal handshake, short enough
	// that a hung dial is abandoned and retried rather than eating the window.
	pingAttemptTimeout = 5 * time.Second
	// Total time to keep retrying. On Cloud Run with Direct VPC egress the
	// container can start before its VPC network interface is ready, so the
	// first dial to the Cloud SQL private IP times out even though the database
	// is healthy — that is what failed the migrate Job on 2026-07-24. Retrying
	// across a window rides that out; a genuinely unreachable database still
	// fails, just later.
	pingRetryWindow = 60 * time.Second
	// Pause between attempts, so a fast-failing error (bad credentials) does not
	// spin the window away in a tight loop.
	pingRetryInterval = 2 * time.Second
)

// OpenDB connects to PostgreSQL using DATABASE_URL and verifies the connection,
// retrying the verification until pingRetryWindow elapses or ctx is done.
func OpenDB(ctx context.Context, databaseURL string) (*DB, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	pool, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(5)
	pool.SetConnMaxLifetime(30 * time.Minute)

	ping := func(ctx context.Context) error { return pool.PingContext(ctx) }
	if err := retryPing(ctx, ping, pingRetryWindow, pingRetryInterval); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &DB{Pool: pool}, nil
}

// retryPing calls ping until it succeeds, the window elapses, or ctx is done.
// It returns the last ping error rather than a deadline error, so the log says
// why the database was unreachable and not merely that time ran out.
func retryPing(ctx context.Context, ping func(context.Context) error, window, interval time.Duration) error {
	deadline := time.Now().Add(window)
	var lastErr error

	for attempt := 1; ; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, pingAttemptTimeout)
		err := ping(attemptCtx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err

		// The caller's own deadline (or cancellation) is final: never keep
		// retrying past it.
		if ctx.Err() != nil {
			return lastErr
		}
		if time.Now().Add(interval).After(deadline) {
			return fmt.Errorf("%w (%d attempts over %s)", lastErr, attempt, window)
		}

		select {
		case <-ctx.Done():
			return lastErr
		case <-time.After(interval):
		}
	}
}

// OpenDBFromEnv connects using the DATABASE_URL environment variable.
func OpenDBFromEnv(ctx context.Context) (*DB, error) {
	return OpenDB(ctx, os.Getenv("DATABASE_URL"))
}

// Close releases database connections.
func (db *DB) Close() error {
	if db == nil || db.Pool == nil {
		return nil
	}
	return db.Pool.Close()
}
