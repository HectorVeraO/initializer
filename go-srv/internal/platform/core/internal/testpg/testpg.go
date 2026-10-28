// Package testpg gives database tests a private, throwaway database on the server
// named by APP_TEST_DATABASE_URL, so tests never touch the database in that URL.
package testpg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EnvURL is the environment variable holding the admin connection URL.
const EnvURL = "APP_TEST_DATABASE_URL"

const adminTimeout = 30 * time.Second

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
// it_<pkg>_<unix seconds>_<random hex> on that server and drops it (WITH FORCE)
// in t.Cleanup.
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

func drop(t testing.TB, adminURL, quotedName string) {
	ctx, cancel := context.WithTimeout(context.Background(), adminTimeout)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Errorf("testpg: connect to drop %s: %v", quotedName, err)
		return
	}
	defer func() { _ = admin.Close(ctx) }()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+quotedName+" WITH (FORCE)"); err != nil {
		t.Errorf("testpg: drop %s: %v", quotedName, err)
	}
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
