package apikeys

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestDropDatabaseWhileAPoolIsStillOpen proves the cleanup survives the
// condition the flake came from: the pool has not finished closing when the
// drop runs, so a backend of this role is still attached.
func TestDropDatabaseWhileAPoolIsStillOpen(t *testing.T) {
	adminURL := os.Getenv(adminURLEnv)
	if adminURL == "" {
		t.Skipf("%s is not set; skipping database test", adminURLEnv)
	}
	suffix := make([]byte, 4)
	_, err := rand.Read(suffix)
	if err != nil {
		t.Fatalf("random: %v", err)
	}
	name := "it_apikeys_drop_" + hex.EncodeToString(suffix)
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

	cfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	// The pool stays open while the drop runs: that is the state the drop must
	// survive. The deferred close runs after dropDatabase returns.
	defer pool.Close()
	_, err = pool.Exec(ctx, "SELECT 1")
	if err != nil {
		t.Fatalf("use the pool: %v", err)
	}

	dropDatabase(t, adminURL, quoted)

	var survived bool
	err = admin.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&survived)
	if err != nil {
		t.Fatalf("lookup %s: %v", name, err)
	}
	if survived {
		t.Errorf("database %s survived the drop while its pool was open", name)
	}
}
