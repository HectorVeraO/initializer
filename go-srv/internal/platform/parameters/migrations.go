package parameters

import "embed"

// migrationsFS holds the goose files applied into cor_migrations_parameters.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS
