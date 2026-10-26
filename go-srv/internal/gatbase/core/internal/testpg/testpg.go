// Package testpg gives database tests a private, throwaway database on the server
// named by GATBASE_TEST_DATABASE_URL, so tests never touch the database in that URL.
package testpg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EnvURL is the environment variable holding the admin connection URL.
const EnvURL = "GATBASE_TEST_DATABASE_URL"

const adminTimeout = 30 * time.Second

var unsafeName = regexp.MustCompile(`[^a-z0-9]+`)

// Database is one test-owned database.
type Database struct {
	// URL connects to the test-owned database.
	URL string
	// Name is the database name, "it_<pkg>_<hex>".
	Name string
}

// New skips the test when EnvURL is unset and fails it when the server is
// unreachable; otherwise it creates the database it_<pkg>_<random hex> on that
// server and drops it (WITH FORCE) in t.Cleanup.
func New(t testing.TB, pkg string) Database {
	t.Helper()
	adminURL := os.Getenv(EnvURL)
	if adminURL == "" {
		t.Skipf("%s is not set; skipping database test", EnvURL)
	}
	name := "it_" + unsafeName.ReplaceAllString(strings.ToLower(pkg), "") + "_" + randomHex(t)

	ctx, cancel := context.WithTimeout(context.Background(), adminTimeout)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("testpg: admin database unreachable: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()
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
