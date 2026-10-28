package parameters

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/migrate"
)

const adminURLEnv = "APP_TEST_DATABASE_URL"

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
