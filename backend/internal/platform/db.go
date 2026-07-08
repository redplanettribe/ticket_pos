// Package platform provides shared infrastructure for the Ticket POS backend:
// database connectivity, structured logging, and HTTP helpers.
package platform

import "database/sql"

// DB wraps a database connection pool for repository use.
type DB struct {
	Pool *sql.DB
}
