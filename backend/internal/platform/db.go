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

// OpenDB connects to PostgreSQL using DATABASE_URL and verifies the connection.
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

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.PingContext(ctx); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &DB{Pool: pool}, nil
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
