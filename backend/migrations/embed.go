package migrations

import "embed"

// Files contains plain SQL migration files applied at startup and via make migrate.
//
//go:embed *.sql
var Files embed.FS
