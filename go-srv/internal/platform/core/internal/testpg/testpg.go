// Package testpg gives database tests a private, throwaway database on the server
// named by APP_TEST_DATABASE_URL, so tests never touch the database in that URL.
package testpg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EnvURL is the environment variable holding the admin connection URL.
const EnvURL = "APP_TEST_DATABASE_URL"

const adminTimeout = 30 * time.Second

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

// staleAfter is how old a testpg database must be before the next New drops it.
// It sits above the 10-minute default go test timeout, so the database of a
// run that is still alive is never old enough.
const staleAfter = time.Hour

// maxPkgLen keeps "it_<pkg>_<10 digits>_<8 hex>" within the 63-byte name limit.
const maxPkgLen = 40

var unsafeName = regexp.MustCompile(`[^a-z0-9]+`)

// sweepPattern is the only shape of name the sweep may drop; the group is the
// creation time in unix seconds.
var sweepPattern = regexp.MustCompile(`^it_[a-z0-9]+_([0-9]{10})_[0-9a-f]{8}$`)

// Database is one test-owned database.
type Database struct {
	// URL connects to the test-owned database.
	URL string
	// Name is the database name, "it_<pkg>_<unix seconds>_<hex>".
	Name string
}

// New skips the test when EnvURL is unset and fails it when the server is
// unreachable; otherwise it drops the testpg databases that killed runs left
// behind (older than an hour, with no connection), creates the database
// it_<pkg>_<unix seconds>_<random hex> on that server and drops it in
// t.Cleanup.
func New(t testing.TB, pkg string) Database {
	t.Helper()
	adminURL := os.Getenv(EnvURL)
	if adminURL == "" {
		t.Skipf("%s is not set; skipping database test", EnvURL)
	}
	cleanPkg := unsafeName.ReplaceAllString(strings.ToLower(pkg), "")
	if len(cleanPkg) > maxPkgLen {
		cleanPkg = cleanPkg[:maxPkgLen]
	}
	name := "it_" + cleanPkg + "_" + strconv.FormatInt(time.Now().Unix(), 10) + "_" + randomHex(t)

	ctx, cancel := context.WithTimeout(context.Background(), adminTimeout)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("testpg: admin database unreachable: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()
	sweepStale(ctx, t, admin, time.Now())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		t.Fatalf("testpg: create database %s: %v", name, err)
	}
	t.Cleanup(func() { drop(t, adminURL, quoted) })

	return Database{URL: withDatabase(t, adminURL, name), Name: name}
}

// Pool opens a pool on the database and closes it in t.Cleanup (before the drop).
func (d Database) Pool(t testing.TB, maxConns int32) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(d.URL)
	if err != nil {
		t.Fatalf("testpg: parse url: %v", err)
	}
	cfg.MaxConns = maxConns
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("testpg: open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// sweepStale drops idle testpg databases created more than staleAfter before
// now. Every failure is only logged: parallel package binaries may race on the
// same leftover, and a sweep must never fail the calling test. The drop has no
// FORCE, so a database that gained a connection meanwhile is kept.
func sweepStale(ctx context.Context, t testing.TB, admin *pgx.Conn, now time.Time) {
	rows, err := admin.Query(ctx, `SELECT d.datname FROM pg_database d
WHERE d.datname ~ '^it_[a-z0-9]+_[0-9]{10}_[0-9a-f]{8}$'
AND NOT EXISTS (SELECT 1 FROM pg_stat_activity a WHERE a.datname = d.datname)`)
	if err != nil {
		t.Logf("testpg: list stale databases: %v", err)
		return
	}
	var stale []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Logf("testpg: read database name: %v", err)
			continue
		}
		if isStale(name, now) {
			stale = append(stale, name)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Logf("testpg: list stale databases: %v", err)
	}
	for _, name := range stale {
		if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()); err != nil {
			t.Logf("testpg: drop stale %s: %v", name, err)
		}
	}
}

// isStale reports whether name is a testpg name created over staleAfter before now.
func isStale(name string, now time.Time) bool {
	match := sweepPattern.FindStringSubmatch(name)
	if match == nil {
		return false
	}
	seconds, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return false
	}
	age := now.Sub(time.Unix(seconds, 0))
	return age > staleAfter
}

// drop terminates this role's backends on the database and drops it, reporting
// a failure that names the roles still attached.
func drop(t testing.TB, adminURL, quotedName string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), adminTimeout)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Errorf("testpg: connect to drop %s: %v", quotedName, err)
		return
	}
	defer func() { _ = admin.Close(ctx) }()
	name := databaseName(quotedName)
	err = deleteDatabase(ctx, admin, quotedName, name)
	if err != nil {
		t.Errorf("testpg: drop %s: %v", quotedName, err)
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
func deleteDatabase(ctx context.Context, admin *pgx.Conn, quotedName, name string) error {
	terminateErr := terminateOwnBackends(ctx, admin, name)
	err := dropWithRetry(ctx, admin, quotedName, name)
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
func dropWithRetry(ctx context.Context, admin *pgx.Conn, quotedName, name string) error {
	for attempt := 1; attempt <= dropAttempts; attempt++ {
		_, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+quotedName)
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
	_, forceErr := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+quotedName+" WITH (FORCE)")
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

func withDatabase(t testing.TB, adminURL, name string) string {
	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("testpg: %s is not a URL", EnvURL)
	}
	parsed.Path = "/" + name
	return parsed.String()
}

func randomHex(t testing.TB) string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("testpg: random: %v", err)
	}
	return hex.EncodeToString(buf)
}
