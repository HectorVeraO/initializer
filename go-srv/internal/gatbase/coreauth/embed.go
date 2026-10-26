package coreauth

import "embed"

// migrationsFS holds the goose files applied into cor_migrations_core_auth.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS
