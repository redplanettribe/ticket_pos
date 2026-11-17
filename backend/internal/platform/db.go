package platform

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
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

	// DeepHealthTimeout bounds the deep health route's database check (#775):
	// a healthy pool answers a ping in milliseconds, and an uptime probe
	// waits for the Storefront, which waits for this, so the bound stays well
	// inside the Storefront's own.
	DeepHealthTimeout = 2 * time.Second
)

// OpenDB connects to PostgreSQL using DATABASE_URL and verifies the connection,
// retrying the verification until pingRetryWindow elapses or ctx is done.
//
// Every query on the pool is traced by one tracer (queryTracer): a span under
// a sampled request span, carrying the statement text and never its
// arguments. With the NoopReporter, no DSN or a process that
// does not report (the migrate job), the pool has no tracer at all.
func OpenDB(ctx context.Context, databaseURL string, reporter Reporter) (*DB, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// The no-op's spans would cost nothing to send, but a tracer still runs
	// on every query; with no DSN there is none, as "dormant" promises.
	if _, noop := reporter.(NoopReporter); !noop {
		config.Tracer = queryTracer{}
	}
	pool := stdlib.OpenDB(*config)
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

// OpenDBFromEnv connects using the DATABASE_URL environment variable, with no
// tracer.
func OpenDBFromEnv(ctx context.Context) (*DB, error) {
	return OpenDB(ctx, os.Getenv("DATABASE_URL"), NoopReporter{})
}

// queryTracer is the pool's one tracer: each query run under a request is a
// "db.sql" span named by its statement text, exactly as written with its $n
// placeholders. pgx hands the tracer the arguments too, and they are never
// read: a value bound to a statement is what a person typed (ADR 0079). The
// span is started through the request's own reporter (startChildSpan).
type queryTracer struct{}

type querySpanKey struct{}

func (t queryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx, span := startChildSpan(ctx, SpanOptions{Op: "db.sql", Name: data.SQL})
	if span == nil {
		return ctx
	}
	span.SetTag("db.system", "postgresql")
	return context.WithValue(ctx, querySpanKey{}, span)
}

func (t queryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if span, ok := ctx.Value(querySpanKey{}).(Span); ok {
		span.Finish()
	}
}

// Close releases database connections.
func (db *DB) Close() error {
	if db == nil || db.Pool == nil {
		return nil
	}
	return db.Pool.Close()
}
