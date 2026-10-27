package featureflags

import "embed"

// migrationsFS holds the goose files applied into cor_migrations_feature_flags.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS
