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

func TestNewSweepsOnlyIdleStaleTestDatabases(t *testing.T) {
	adminURL := os.Getenv(testpg.EnvURL)
	if adminURL == "" {
		t.Skipf("%s is not set; skipping database test", testpg.EnvURL)
	}
	ctx := context.Background()
	admin := connect(t, ctx, adminURL, "")
	t.Cleanup(func() { _ = admin.Close(ctx) })

	suffix := syntheticName(t, "", time.Time{})
	cases := []struct {
		name     string
		database string
		held     bool
		wantKept bool
	}{
		{"idle stale database is dropped", syntheticName(t, "it_sweeptest", time.Now().Add(-2*time.Hour)), false, false},
		{"stale database with a connection is kept", syntheticName(t, "it_sweeptest", time.Now().Add(-2*time.Hour)), true, true},
		{"fresh database is kept", syntheticName(t, "it_sweeptest", time.Now()), false, true},
		{"database near the threshold is kept", syntheticName(t, "it_sweeptest", time.Now().Add(-59*time.Minute)), false, true},
		{"name of another shape is kept", "it_sweeptest_" + suffix[len(suffix)-8:], false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(func() {
				_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{tc.database}.Sanitize()+" WITH (FORCE)")
			})
			if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{tc.database}.Sanitize()); err != nil {
				t.Fatalf("create %s: %v", tc.database, err)
			}
			if tc.held {
				held := connect(t, ctx, adminURL, tc.database)
				t.Cleanup(func() { _ = held.Close(ctx) })
			}

			testpg.New(t, "sweeptest")

			// Another package binary may sweep an idle stale database first; either way it must be gone.
			if got := exists(t, ctx, admin, tc.database); got != tc.wantKept {
				t.Errorf("database %s exists = %v, want %v", tc.database, got, tc.wantKept)
			}
		})
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

	// A stale database whose package part is already at the 40-character cap.
	stale := syntheticName(t, "it_"+strings.Repeat("a", 40), time.Now().Add(-2*time.Hour))
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
