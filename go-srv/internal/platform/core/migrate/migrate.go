// Package migrate applies the embedded SQL migrations of every module with goose,
// one version table per module, all sets under one advisory lock.
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

const (
	lockTimeout   = 120 * time.Second
	unlockTimeout = 5 * time.Second
	tablePrefix   = "cor_migrations_"
	// Postgres truncates identifiers at 63 bytes.
	maxTableName = 63
)

// inProcess serializes Apply calls of this process: two concurrent calls on one
// small pool would each hold a connection while waiting for the other's.
var inProcess = make(chan struct{}, 1)

var moduleIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// Set is one module's migrations.
type Set struct {
	Module string // catalog id, e.g. "identity"
	Files  fs.FS  // its "migrations" directory holds the files
}

// VersionTable is the goose version table of a module: "cor_migrations_" plus
// the id with "-" replaced by "_" ("feature-flags" -> "cor_migrations_feature_flags").
func VersionTable(module string) string {
	return tablePrefix + strings.ReplaceAll(module, "-", "_")
}

// Apply applies every pending migration of every set, sets in the given order,
// files in version order within a set, under one Postgres session advisory lock
// held for the whole run (concurrent replicas serialize). It stops at the first
// failing file, whose transaction is rolled back. The pool needs at least two
// connections: one holds the lock, one runs the migrations. Calls in one process run one at a time.
func Apply(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger, sets []Set) (err error) {
	if err := validate(pool, logger, sets); err != nil {
		return err
	}
	select {
	case inProcess <- struct{}{}:
		defer func() { <-inProcess }()
	case <-ctx.Done():
		return fmt.Errorf("migrate: wait for another Apply in this process: %w", ctx.Err())
	}
	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("migrate: acquire lock connection: %w", err)
	}
	defer func() {
		unlockErr := unlock(lockConn)
		err = errors.Join(err, unlockErr)
	}()

	lockCtx, cancel := context.WithTimeout(ctx, lockTimeout)
	defer cancel()
	_, err = lockConn.Exec(lockCtx, "SELECT pg_advisory_lock(hashtextextended('platform.migrations', 0))")
	if err != nil {
		return fmt.Errorf("migrate: take advisory lock: %w", err)
	}

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() {
		closeErr := sqlDB.Close()
		err = errors.Join(err, closeErr)
	}()

	for _, set := range sets {
		if err := applySet(ctx, sqlDB, logger, set); err != nil {
			return err
		}
	}
	return nil
}

func applySet(ctx context.Context, sqlDB *sql.DB, logger *slog.Logger, set Set) error {
	sub, err := fs.Sub(set.Files, "migrations")
	if err != nil {
		return fmt.Errorf("migrate: %s: open migrations directory: %w", set.Module, err)
	}
	// The provider is never closed: Provider.Close would close the shared sqlDB.
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, sub,
		goose.WithTableName(VersionTable(set.Module)),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return fmt.Errorf("migrate: %s: create provider: %w", set.Module, err)
	}
	results, err := provider.Up(ctx)
	logResults(ctx, logger, set.Module, results)
	if err != nil {
		return fmt.Errorf("migrate: %s: %w", set.Module, err)
	}
	logger.InfoContext(ctx, "migrations_complete",
		slog.String("module", set.Module), slog.Int("applied", len(results)))
	return nil
}

func logResults(ctx context.Context, logger *slog.Logger, module string, results []*goose.MigrationResult) {
	for _, result := range results {
		if result.Error != nil {
			continue
		}
		logger.InfoContext(ctx, "migration_applied",
			slog.String("module", module),
			slog.Int64("version", result.Source.Version),
			slog.Int64("duration_ms", result.Duration.Milliseconds()))
	}
}

// unlock releases the session lock; when that fails the connection is closed,
// which frees the lock server-side.
func unlock(conn *pgxpool.Conn) error {
	ctx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
	defer cancel()
	_, err := conn.Exec(ctx, "SELECT pg_advisory_unlock(hashtextextended('platform.migrations', 0))")
	if err != nil {
		raw := conn.Hijack()
		_ = raw.Close(ctx)
		return fmt.Errorf("migrate: release advisory lock: %w", err)
	}
	conn.Release()
	return nil
}

func validate(pool *pgxpool.Pool, logger *slog.Logger, sets []Set) error {
	var problems []error
	if pool == nil {
		problems = append(problems, errors.New("migrate: Apply requires a pool"))
	} else if pool.Config().MaxConns < 2 {
		problems = append(problems, errors.New("migrate: the pool needs at least 2 connections"))
	}
	if logger == nil {
		problems = append(problems, errors.New("migrate: Apply requires a logger"))
	}
	seen := make(map[string]bool, len(sets))
	for _, set := range sets {
		problems = append(problems, validateSet(set, seen)...)
	}
	return errors.Join(problems...)
}

func validateSet(set Set, seen map[string]bool) []error {
	var problems []error
	validID := moduleIDPattern.MatchString(set.Module) && len(VersionTable(set.Module)) <= maxTableName
	if !validID {
		problems = append(problems, fmt.Errorf("migrate: invalid module id %q", set.Module))
	}
	if seen[set.Module] {
		problems = append(problems, fmt.Errorf("migrate: duplicate module %q", set.Module))
	}
	seen[set.Module] = true
	if set.Files == nil {
		problems = append(problems, fmt.Errorf("migrate: module %q has no files", set.Module))
	}
	return problems
}
