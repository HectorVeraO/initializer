package parameters

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/migrate"
)

const adminURLEnv = "APP_TEST_DATABASE_URL"

// dropAttempts and dropRetryDelay bound the retries while PostgreSQL answers
// 55006 "database is being accessed by other users". PostgreSQL waits about five
// seconds inside one attempt before it answers, so two attempts cover a backend
// that is still draining.
const (
	dropAttempts   = 2
	dropRetryDelay = 100 * time.Millisecond
)

// sqlstateObjectInUse is PostgreSQL's "database is being accessed by other users".
const sqlstateObjectInUse = "55006"

// sqlstateSignalDenied is PostgreSQL's "permission denied to terminate process",
// the answer a role without pg_signal_backend gets from a FORCE drop.
const sqlstateSignalDenied = "42501"

// Definition exposes a validated definition to the external tests.
type Definition = definition

// ParseCreate runs the POST body validation without a database.
func ParseCreate(b *api.Body) (Definition, error) {
	return parseCreate(b)
}

// CheckValue runs the value checks; ok is true when raw is acceptable, else
// code is the first failing rule.
func CheckValue(typeName string, minValue, maxValue *float64, allowed []json.RawMessage, raw json.RawMessage) (api.Code, bool) {
	rules := constraints{valueType: valueType(typeName), min: minValue, max: maxValue, allowed: allowed}
	failure := checkValue(rules, raw)
	if failure == nil {
		return "", true
	}
	return failure.code, false
}

// MigrationsVersionTable is the goose version table of the module.
func MigrationsVersionTable() string {
	return migrate.VersionTable(moduleID)
}

// NewTestPool gives the external tests an it_ database on the server of
// APP_TEST_DATABASE_URL with the module migrations applied, and drops it
// on cleanup. It skips the test when the variable is unset and never writes
// to the database named in the URL.
func NewTestPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	adminURL := os.Getenv(adminURLEnv)
	if adminURL == "" {
		t.Skipf("%s is not set; skipping database test", adminURLEnv)
	}
	suffix := make([]byte, 4)
	_, err := rand.Read(suffix)
	if err != nil {
		t.Fatalf("random: %v", err)
	}
	name := "it_parameters_domain_" + hex.EncodeToString(suffix)
	quoted := pgx.Identifier{name}.Sanitize()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("admin database unreachable: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()
	_, err = admin.Exec(ctx, "CREATE DATABASE "+quoted)
	if err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}
	t.Cleanup(func() { dropDatabase(t, adminURL, quoted) })

	cfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	cfg.ConnConfig.Database = name
	cfg.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sets := []migrate.Set{{Module: moduleID, Files: Module().Migrations}}
	err = migrate.Apply(ctx, pool, logger, sets)
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return pool
}

// dropDatabase terminates this role's backends on the database and drops it,
// reporting a failure that names the roles still attached.
func dropDatabase(t testing.TB, adminURL, quoted string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Errorf("connect to drop %s: %v", quoted, err)
		return
	}
	defer func() { _ = admin.Close(ctx) }()
	err = deleteDatabase(ctx, admin, quoted, databaseName(quoted))
	if err != nil {
		t.Errorf("drop %s: %v", quoted, err)
	}
}

// deleteDatabase terminates the backends of the connected role on the database
// and drops it, returning the failure instead of reporting it.
//
// The first attempts carry no FORCE. FORCE terminates every backend attached to
// the database, and a role may terminate only the backends of its own role
// unless it is a superuser or a member of pg_signal_backend, so on a server
// several components share FORCE answers 42501 "permission denied to terminate
// process" and the cleanup fails after the test passed. A plain drop first, then,
// for a role that may signal every backend, one FORCE attempt.
func deleteDatabase(ctx context.Context, admin *pgx.Conn, quoted, name string) error {
	terminateErr := terminateOwnBackends(ctx, admin, name)
	err := dropWithRetry(ctx, admin, quoted, name)
	if err == nil {
		return nil
	}
	if terminateErr != nil {
		return fmt.Errorf("%w (terminating this role's backends failed: %v)", err, terminateErr)
	}
	return err
}

// terminateOwnBackends ends every backend of the connected role on the database,
// so a pool that is still draining stops holding it. Terminating a backend of
// the same role needs no privilege. A backend of another role is left attached:
// the drop reports it instead.
func terminateOwnBackends(ctx context.Context, admin *pgx.Conn, name string) error {
	_, err := admin.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity
WHERE datname = $1 AND usename = current_user AND pid <> pg_backend_pid()`, name)
	return err
}

// dropWithRetry drops the database: plain attempts while PostgreSQL reports it
// still in use, then one FORCE attempt for a role allowed to signal every
// backend; the failure names the roles attached to it.
func dropWithRetry(ctx context.Context, admin *pgx.Conn, quoted, name string) error {
	for attempt := 1; attempt <= dropAttempts; attempt++ {
		_, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+quoted)
		if err == nil {
			return nil
		}
		if !isObjectInUse(err) {
			return err
		}
		if attempt < dropAttempts {
			time.Sleep(dropRetryDelay)
		}
	}
	_, forceErr := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)")
	if forceErr == nil {
		return nil
	}
	if !isObjectInUse(forceErr) && !isSignalDenied(forceErr) {
		return forceErr
	}
	attached, err := attachedBackends(ctx, admin, name)
	if err != nil {
		return fmt.Errorf("still in use, and listing the attached backends failed: %v: %w", err, forceErr)
	}
	return fmt.Errorf("still in use by %s: %w", attached, forceErr)
}

// isObjectInUse reports whether err is PostgreSQL's "database is being accessed
// by other users".
func isObjectInUse(err error) bool {
	return hasSQLState(err, sqlstateObjectInUse)
}

// isSignalDenied reports whether err is PostgreSQL's "permission denied to
// terminate process", the answer to a FORCE drop by a role that may not signal
// another role's backend.
func isSignalDenied(err error) bool {
	return hasSQLState(err, sqlstateSignalDenied)
}

// hasSQLState reports whether err carries a PostgreSQL SQLSTATE code.
func hasSQLState(err error, code string) bool {
	var pgErr *pgconn.PgError
	hasCode := errors.As(err, &pgErr) && pgErr.Code == code
	return hasCode
}

// attachedBackends names the roles and applications still attached to the
// database, so a failed drop says who held it.
func attachedBackends(ctx context.Context, admin *pgx.Conn, name string) (string, error) {
	rows, err := admin.Query(ctx, `SELECT COALESCE(usename, 'unknown'), COALESCE(application_name, ''), count(*)
FROM pg_stat_activity WHERE datname = $1 GROUP BY 1, 2 ORDER BY 1, 2`, name)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var attached []string
	for rows.Next() {
		var role, application string
		var backends int
		if scanErr := rows.Scan(&role, &application, &backends); scanErr != nil {
			return "", scanErr
		}
		entry := "role " + role
		if application != "" {
			entry += " application " + application
		}
		attached = append(attached, fmt.Sprintf("%s (%d backends)", entry, backends))
	}
	err = rows.Err()
	if err != nil {
		return "", err
	}
	if len(attached) == 0 {
		return "no attached backend", nil
	}
	return strings.Join(attached, ", "), nil
}

// databaseName returns the database name a sanitized identifier holds.
func databaseName(quoted string) string {
	isQuoted := len(quoted) >= 2 && quoted[0] == '"' && quoted[len(quoted)-1] == '"'
	if !isQuoted {
		return quoted
	}
	return strings.ReplaceAll(quoted[1:len(quoted)-1], `""`, `"`)
}
