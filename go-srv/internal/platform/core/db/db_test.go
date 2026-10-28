package db_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/config"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/db"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/internal/testpg"
)

func secret(t *testing.T, value string) config.Secret {
	t.Helper()
	reader := config.NewReader(config.MapSource(map[string]string{"URL": value}))
	return reader.Secret("URL")
}

func TestOpenIsLazy(t *testing.T) {
	pool, err := db.Open(context.Background(), secret(t, "postgres://u:change-me@127.0.0.1:1/x?sslmode=disable"), 2)
	if err != nil {
		t.Fatalf("Open returned %v for an unreachable server", err)
	}
	defer pool.Close()
	if err := db.Ping(context.Background(), pool); err == nil {
		t.Fatal("Ping succeeded against a closed port")
	}
}

func TestOpenParseErrorHidesPassword(t *testing.T) {
	bad := []string{
		"postgres://user:change-me-pw@host:notaport/db",
		"postgres://user:change-me-pw@host/db?sslmode=bogus",
		"postgres://user:change-me-pw@%zz/db",
	}
	for _, raw := range bad {
		_, err := db.Open(context.Background(), secret(t, raw), 2)
		if err == nil {
			t.Fatalf("Open(%q) returned nil error", raw)
		}
		if strings.Contains(err.Error(), "change-me-pw") {
			t.Fatalf("error leaks the password: %v", err)
		}
	}
}

func TestOpenRejectsBadMaxConns(t *testing.T) {
	for _, n := range []int{0, -1, math.MaxInt32 + 1} {
		if _, err := db.Open(context.Background(), secret(t, "postgres://u@h/d"), n); err == nil {
			t.Fatalf("maxConns %d accepted", n)
		}
	}
}

func TestPingCheckIsNamedPostgresAndProbesThePool(t *testing.T) {
	pool := testpg.New(t, "dbcheck").Pool(t, 2)
	check := db.PingCheck(pool)

	if check.Name != "postgres" {
		t.Fatalf("Name = %q", check.Name)
	}
	if err := check.Probe(context.Background()); err != nil {
		t.Fatalf("Probe on a live pool: %v", err)
	}
	pool.Close()
	if err := check.Probe(context.Background()); err == nil {
		t.Fatal("Probe succeeded on a closed pool")
	}
}

func TestPingCheckHonoursTheContextDeadline(t *testing.T) {
	pool, err := db.Open(context.Background(), secret(t, "postgres://u:change-me@127.0.0.1:1/x?sslmode=disable"), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := db.PingCheck(pool).Probe(ctx); err == nil {
		t.Fatal("Probe ignored a canceled context")
	}
}

func TestPingCheckPanicsOnNilPool(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic")
		}
	}()
	db.PingCheck(nil)
}

func TestPingSucceedsOnLiveDatabase(t *testing.T) {
	pool := testpg.New(t, "db").Pool(t, 2)
	if err := db.Ping(context.Background(), pool); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func createTable(t *testing.T, ctx context.Context, d testpg.Database) {
	t.Helper()
	pool := d.Pool(t, 4)
	_, err := pool.Exec(ctx, "CREATE TABLE sys_examples (id int PRIMARY KEY)")
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
}

func count(t *testing.T, ctx context.Context, d testpg.Database) int {
	t.Helper()
	var n int
	err := d.Pool(t, 2).QueryRow(ctx, "SELECT count(*) FROM sys_examples").Scan(&n)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// assertNoOpenTx fails when a transaction still holds a pool connection: an
// omitted rollback leaves the connection acquired, which a row count cannot see.
func assertNoOpenTx(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if acquired := pool.Stat().AcquiredConns(); acquired != 0 {
		t.Fatalf("acquired connections = %d, want 0: a transaction was left open", acquired)
	}
}

func TestInTxCommitsOnNil(t *testing.T) {
	ctx := context.Background()
	d := testpg.New(t, "db")
	createTable(t, ctx, d)
	pool := d.Pool(t, 2)
	err := db.InTx(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO sys_examples VALUES (1)")
		return err
	})
	if err != nil {
		t.Fatalf("InTx: %v", err)
	}
	if got := count(t, ctx, d); got != 1 {
		t.Fatalf("rows = %d, want 1", got)
	}
}

func TestInTxRollsBackOnError(t *testing.T) {
	ctx := context.Background()
	d := testpg.New(t, "db")
	createTable(t, ctx, d)
	sentinel := errors.New("boom")
	pool := d.Pool(t, 2)
	err := db.InTx(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO sys_examples VALUES (1)"); err != nil {
			return err
		}
		return sentinel
	})
	if err != sentinel {
		t.Fatalf("err = %v, want the sentinel itself", err)
	}
	assertNoOpenTx(t, pool)
	if got := count(t, ctx, d); got != 0 {
		t.Fatalf("rows = %d, want 0 after rollback", got)
	}
}

func TestInTxRollsBackWhenContextCanceled(t *testing.T) {
	d := testpg.New(t, "db")
	createTable(t, context.Background(), d)
	pool := d.Pool(t, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := db.InTx(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO sys_examples VALUES (1)"); err != nil {
			return err
		}
		cancel()
		return ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	assertNoOpenTx(t, pool)
	if got := count(t, context.Background(), d); got != 0 {
		t.Fatalf("rows = %d, want 0 after rollback", got)
	}
}

func TestInTxRollsBackOnPanic(t *testing.T) {
	ctx := context.Background()
	d := testpg.New(t, "db")
	createTable(t, ctx, d)
	pool := d.Pool(t, 2)
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = db.InTx(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
			_, _ = tx.Exec(ctx, "INSERT INTO sys_examples VALUES (1)")
			panic("boom")
		})
	}()
	if recovered != "boom" {
		t.Fatalf("recovered = %v, want the panic value to reach the caller", recovered)
	}
	assertNoOpenTx(t, pool)
	if got := count(t, ctx, d); got != 0 {
		t.Fatalf("rows = %d, want 0 after panic", got)
	}
}

func TestInTxKeepsFnErrorWhenRollbackFails(t *testing.T) {
	ctx := context.Background()
	d := testpg.New(t, "db")
	pool := d.Pool(t, 2)
	sentinel := errors.New("boom")
	err := db.InTx(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		// Killing the backend makes the later rollback fail.
		_, _ = tx.Exec(ctx, "SELECT pg_terminate_backend(pg_backend_pid())")
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want it to wrap the fn error", err)
	}
	if !strings.Contains(err.Error(), "rollback") {
		t.Fatalf("err = %v, want the rollback failure joined", err)
	}
}

func TestIsUniqueViolation(t *testing.T) {
	ctx := context.Background()
	d := testpg.New(t, "db")
	createTable(t, ctx, d)
	pool := d.Pool(t, 2)
	_, _ = pool.Exec(ctx, "INSERT INTO sys_examples VALUES (1)")
	_, dup := pool.Exec(ctx, "INSERT INTO sys_examples VALUES (1)")
	if !db.IsUniqueViolation(dup) {
		t.Fatalf("duplicate key error not detected: %v", dup)
	}
	_, other := pool.Exec(ctx, "INSERT INTO sys_missing VALUES (1)")
	if db.IsUniqueViolation(other) {
		t.Fatal("undefined-table error reported as unique violation")
	}
	if db.IsUniqueViolation(nil) || db.IsUniqueViolation(errors.New("x")) {
		t.Fatal("non-pg error reported as unique violation")
	}
}
