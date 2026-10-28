package tenancy

import "embed"

// migrationsFS holds the goose files applied into cor_migrations_tenancy.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS
