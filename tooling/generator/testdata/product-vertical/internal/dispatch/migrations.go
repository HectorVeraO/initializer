package dispatch

import "embed"

// migrationsFS holds the goose files applied into cor_migrations_dispatch.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS
