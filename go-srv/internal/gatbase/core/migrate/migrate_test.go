package migrate_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/internal/testpg"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/migrate"
)

func fixture(module string) migrate.Set {
	return migrate.Set{Module: module, Files: os.DirFS("testdata/" + module)}
}

func logger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, nil))
}

func versions(t *testing.T, pool *pgxpool.Pool, table string) []int64 {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		"SELECT version_id FROM "+table+" WHERE version_id > 0 ORDER BY version_id")
	if err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", name).Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}
	return exists
}

func TestVersionTable(t *testing.T) {
	cases := map[string]string{
		"core-auth":     "cor_migrations_core_auth",
		"parameters":    "cor_migrations_parameters",
		"feature-flags": "cor_migrations_feature_flags",
	}
	for module, want := range cases {
		if got := migrate.VersionTable(module); got != want {
			t.Errorf("VersionTable(%q) = %q, want %q", module, got, want)
		}
	}
}

func TestApplyRejectsInvalidInput(t *testing.T) {
	fsys := fstest.MapFS{}
	cases := map[string]struct {
		sets []migrate.Set
		want string
	}{
		"sql in module id": {[]migrate.Set{{Module: "a; DROP TABLE x", Files: fsys}}, "invalid module id"},
		"uppercase":        {[]migrate.Set{{Module: "Alpha", Files: fsys}}, "invalid module id"},
		"empty":            {[]migrate.Set{{Module: "", Files: fsys}}, "invalid module id"},
		"too long":         {[]migrate.Set{{Module: strings.Repeat("a", 60), Files: fsys}}, "invalid module id"},
		"duplicate":        {[]migrate.Set{{Module: "alpha", Files: fsys}, {Module: "alpha", Files: fsys}}, "duplicate module"},
		"nil files":        {[]migrate.Set{{Module: "alpha"}}, "no files"},
	}
	// A lazy pool never connects: validation fails before any connection is used.
	pool, err := pgxpool.New(context.Background(), "postgres://u@127.0.0.1:1/x?pool_max_conns=2")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := migrate.Apply(context.Background(), pool, slog.Default(), tc.sets)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestApplyCreatesOneVersionTablePerModule(t *testing.T) {
	d := testpg.New(t, "migrate")
	pool := d.Pool(t, 4)
	var buf bytes.Buffer
	sets := []migrate.Set{fixture("alpha"), fixture("beta")}
	if err := migrate.Apply(context.Background(), pool, logger(&buf), sets); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	gotAlpha := versions(t, pool, "cor_migrations_alpha")
	if len(gotAlpha) != 2 || gotAlpha[0] != 20260101120000 || gotAlpha[1] != 20260102120000 {
		t.Fatalf("alpha versions = %v", gotAlpha)
	}
	gotBeta := versions(t, pool, "cor_migrations_beta")
	if len(gotBeta) != 1 || gotBeta[0] != 20250101120000 {
		t.Fatalf("beta versions = %v", gotBeta)
	}
	for _, table := range []string{"sys_examples", "flg_examples"} {
		if !tableExists(t, pool, table) {
			t.Errorf("table %s missing", table)
		}
	}
	for _, event := range []string{"migration_applied", "migrations_complete"} {
		if !strings.Contains(buf.String(), event) {
			t.Errorf("log lacks %s: %s", event, buf.String())
		}
	}
}

func TestApplySecondCallAppliesNothing(t *testing.T) {
	d := testpg.New(t, "migrate")
	pool := d.Pool(t, 4)
	sets := []migrate.Set{fixture("alpha"), fixture("beta")}
	if err := migrate.Apply(context.Background(), pool, logger(&bytes.Buffer{}), sets); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	var buf bytes.Buffer
	if err := migrate.Apply(context.Background(), pool, logger(&buf), sets); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if strings.Contains(buf.String(), "migration_applied") {
		t.Fatalf("second Apply applied migrations: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"applied":0`) {
		t.Fatalf("second Apply did not report zero applied: %s", buf.String())
	}
}

func TestApplyConcurrentCallsSerialize(t *testing.T) {
	d := testpg.New(t, "migrate")
	sets := []migrate.Set{fixture("alpha"), fixture("beta")}
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		// One pool per caller, like separate replicas.
		pool := d.Pool(t, 3)
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = migrate.Apply(context.Background(), pool, logger(&bytes.Buffer{}), sets)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: %v", i, err)
		}
	}
	check := d.Pool(t, 2)
	if got := versions(t, check, "cor_migrations_alpha"); len(got) != 2 {
		t.Fatalf("alpha versions = %v, want each file recorded once", got)
	}
	if got := versions(t, check, "cor_migrations_beta"); len(got) != 1 {
		t.Fatalf("beta versions = %v, want each file recorded once", got)
	}
}

func TestApplyStopsAtFailingFileAndRollsItBack(t *testing.T) {
	d := testpg.New(t, "migrate")
	pool := d.Pool(t, 4)
	err := migrate.Apply(context.Background(), pool, logger(&bytes.Buffer{}), []migrate.Set{fixture("broken")})
	if err == nil {
		t.Fatal("Apply succeeded with a syntax error in a file")
	}
	got := versions(t, pool, "cor_migrations_broken")
	if len(got) != 1 || got[0] != 20260101120000 {
		t.Fatalf("versions = %v, want only the file before the failure", got)
	}
	if tableExists(t, pool, "sys_bad") || tableExists(t, pool, "sys_never") {
		t.Fatal("failed or later file left tables behind")
	}
}

func TestApplyReleasesLockAfterFailure(t *testing.T) {
	d := testpg.New(t, "migrate")
	pool := d.Pool(t, 4)
	_ = migrate.Apply(context.Background(), pool, logger(&bytes.Buffer{}), []migrate.Set{fixture("broken")})
	if err := migrate.Apply(context.Background(), pool, logger(&bytes.Buffer{}), []migrate.Set{fixture("alpha")}); err != nil {
		t.Fatalf("Apply after a failed run: %v", err)
	}
}

func TestApplyNeedsTwoConnections(t *testing.T) {
	pool := testpg.New(t, "migrate").Pool(t, 1)
	err := migrate.Apply(context.Background(), pool, logger(&bytes.Buffer{}), []migrate.Set{fixture("alpha")})
	if err == nil || !strings.Contains(err.Error(), "at least 2 connections") {
		t.Fatalf("err = %v, want the two-connection requirement", err)
	}
}

func TestApplyWaitsForAConcurrentRun(t *testing.T) {
	ctx := context.Background()
	d := testpg.New(t, "migrate")
	pool := d.Pool(t, 6)

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- migrate.Apply(ctx, pool, logger(&bytes.Buffer{}), []migrate.Set{fixture("slow")})
	}()
	waitForRunningMigration(t, pool)

	blockedCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	err := migrate.Apply(blockedCtx, pool, logger(&bytes.Buffer{}), []migrate.Set{fixture("alpha")})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the lock wait to end by deadline", err)
	}
	if tableExists(t, pool, "sys_examples") {
		t.Fatal("Apply migrated while another run held the lock")
	}

	if err := <-firstDone; err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if err := migrate.Apply(ctx, pool, logger(&bytes.Buffer{}), []migrate.Set{fixture("alpha")}); err != nil {
		t.Fatalf("Apply after the first run finished: %v", err)
	}
}

// waitForRunningMigration blocks until a session is executing the slow fixture.
func waitForRunningMigration(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var running bool
		err := pool.QueryRow(context.Background(),
			"SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE state = 'active' AND query LIKE '%pg_sleep(2)%' AND pid <> pg_backend_pid())").Scan(&running)
		if err != nil {
			t.Fatal(err)
		}
		if running {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the slow migration never started")
}
