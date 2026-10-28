package apikeys

import "embed"

// migrationsFS holds the goose files applied into cor_migrations_api_keys.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS
