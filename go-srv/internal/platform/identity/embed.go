package identity

import "embed"

// migrationsFS holds the goose files applied into cor_migrations_identity.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS
