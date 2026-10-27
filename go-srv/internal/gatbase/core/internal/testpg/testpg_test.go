package testpg_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/internal/testpg"
)

func syntheticName(t *testing.T, prefix string, created time.Time) string {
	t.Helper()
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("random: %v", err)
	}
	return fmt.Sprintf("%s_%d_%s", prefix, created.Unix(), hex.EncodeToString(buf))
}

func connect(t *testing.T, ctx context.Context, adminURL, database string) *pgx.Conn {
	t.Helper()
	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	if database != "" {
		parsed.Path = "/" + database
	}
	conn, err := pgx.Connect(ctx, parsed.String())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return conn
}

func exists(t *testing.T, ctx context.Context, admin *pgx.Conn, name string) bool {
	t.Helper()
	var found bool
	err := admin.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&found)
	if err != nil {
		t.Fatalf("lookup %s: %v", name, err)
	}
	return found
}

func TestNewDropsOnlyIdleStaleTestDatabases(t *testing.T) {
	adminURL := os.Getenv(testpg.EnvURL)
	if adminURL == "" {
		t.Skipf("%s is not set; skipping database test", testpg.EnvURL)
	}
	ctx := context.Background()
	admin := connect(t, ctx, adminURL, "")
	t.Cleanup(func() { _ = admin.Close(ctx) })

	stale := syntheticName(t, "it_sweeptest", time.Now().Add(-2*time.Hour))
	staleHeld := syntheticName(t, "it_sweeptest", time.Now().Add(-2*time.Hour))
	fresh := syntheticName(t, "it_sweeptest", time.Now())
	nearThreshold := syntheticName(t, "it_sweeptest", time.Now().Add(-59*time.Minute))
	suffix := syntheticName(t, "", time.Time{})
	otherShape := "it_sweeptest_" + suffix[len(suffix)-8:]
	names := []string{stale, staleHeld, fresh, nearThreshold, otherShape}
	t.Cleanup(func() {
		for _, name := range names {
			_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		}
	})
	for _, name := range names {
		if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	held := connect(t, ctx, adminURL, staleHeld)
	t.Cleanup(func() { _ = held.Close(ctx) })

	testpg.New(t, "sweeptest")

	// Another package binary may sweep the idle stale one first; either way it must be gone.
	if exists(t, ctx, admin, stale) {
		t.Errorf("idle stale database %s was not dropped", stale)
	}
	for _, kept := range []string{staleHeld, fresh, nearThreshold, otherShape} {
		if !exists(t, ctx, admin, kept) {
			t.Errorf("database %s must not be dropped", kept)
		}
	}
}

func TestNewSweepsStaleDatabasesOfLongPackageNames(t *testing.T) {
	adminURL := os.Getenv(testpg.EnvURL)
	if adminURL == "" {
		t.Skipf("%s is not set; skipping database test", testpg.EnvURL)
	}
	ctx := context.Background()
	admin := connect(t, ctx, adminURL, "")
	t.Cleanup(func() { _ = admin.Close(ctx) })

	longPkg := strings.Repeat("a", 100)
	db := testpg.New(t, longPkg)

	// PostgreSQL truncates identifiers over 63 bytes, which would cut the timestamp.
	if len(db.Name) > 63 {
		t.Errorf("database name %q is %d bytes, want at most 63", db.Name, len(db.Name))
	}

	// Rebuild the same name shape with a timestamp two hours old.
	parts := strings.Split(db.Name, "_")
	if len(parts) < 4 {
		t.Fatalf("unexpected database name shape %q", db.Name)
	}
	parts[len(parts)-2] = fmt.Sprintf("%d", time.Now().Add(-2*time.Hour).Unix())
	stale := strings.Join(parts, "_")
	t.Cleanup(func() {
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{stale}.Sanitize()+" WITH (FORCE)")
	})
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{stale}.Sanitize()); err != nil {
		t.Fatalf("create %s: %v", stale, err)
	}

	testpg.New(t, longPkg)

	if exists(t, ctx, admin, stale) {
		t.Errorf("idle stale database %s of a long package name was not dropped", stale)
	}
}
