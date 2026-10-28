package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/migrate"
)

const adminURLEnv = "APP_TEST_DATABASE_URL"

// randomSessionKeys is a keyring entry built from fresh random bytes.
func randomSessionKeys(t testing.TB) string {
	t.Helper()
	seed := make([]byte, 32)
	_, err := rand.Read(seed)
	if err != nil {
		t.Fatalf("random: %v", err)
	}
	return "test-1:" + base64.RawURLEncoding.EncodeToString(seed)
}

// randomUUID returns a random version 4 UUID in canonical text.
func randomUUID(t testing.TB) string {
	t.Helper()
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		t.Fatalf("random: %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// lazyPool is a pool that never connects unless used; Build checks the
// configuration before it pings.
func lazyPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://nobody@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatalf("lazy pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// newTestPool creates an it_ database on the server of APP_TEST_DATABASE_URL,
// applies the module migrations and drops the database on cleanup. It skips
// the test when the variable is unset.
func newTestPool(t testing.TB) *pgxpool.Pool {
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
	name := "it_identity_" + hex.EncodeToString(suffix)
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
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	err = applyMigrations(ctx, pool)
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return pool
}

func applyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sets := []migrate.Set{{Module: moduleID, Files: migrationsFS}}
	return migrate.Apply(ctx, pool, logger, sets)
}

func dropDatabase(t testing.TB, adminURL, quoted string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Errorf("connect to drop %s: %v", quoted, err)
		return
	}
	defer func() { _ = admin.Close(ctx) }()
	_, err = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)")
	if err != nil {
		t.Errorf("drop %s: %v", quoted, err)
	}
}
