package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
)

var testNow = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

func newTestStore(t *testing.T) (*store, *pgxpool.Pool) {
	t.Helper()
	pool := newTestPool(t)
	return newStore(pool, clock.Fixed(testNow)), pool
}

func mustAccount(t *testing.T, s *store, email string) Account {
	t.Helper()
	name := "Jane Doe"
	account, err := s.UpsertAccount(context.Background(), randomUUID(t), email, &name)
	if err != nil {
		t.Fatalf("upsert account: %v", err)
	}
	return account
}

func TestMigrationsApplyTwiceIdempotently(t *testing.T) {
	pool := newTestPool(t)
	err := applyMigrations(context.Background(), pool)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	s := newStore(pool, clock.Fixed(testNow))
	mustAccount(t, s, "admin@example.com")
}

func TestMigrationsCreateOnlyIdentityTables(t *testing.T) {
	_, pool := newTestStore(t)
	rows, err := pool.Query(context.Background(),
		"SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_name <> 'cor_migrations_identity' ORDER BY table_name")
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("collect tables: %v", err)
	}
	if strings.Join(tables, ",") != "iam_accounts" {
		t.Fatalf("tables = %v, want only iam_accounts (no ten_* or apk_* table)", tables)
	}
}

func TestAccountUpsertRefreshesByIdentity(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	identity := randomUUID(t)
	first, err := s.UpsertAccount(ctx, identity, "Jane@example.com", nil)
	if err != nil || first.Email != "jane@example.com" {
		t.Fatalf("first: %+v %v", first, err)
	}
	name := "Jane Doe"
	second, err := s.UpsertAccount(ctx, identity, "jane.doe@example.com", &name)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.ID != first.ID || second.Email != "jane.doe@example.com" || second.FullName == nil || *second.FullName != name {
		t.Fatalf("not refreshed: %+v", second)
	}
	third, err := s.UpsertAccount(ctx, identity, "jane.doe@example.com", nil)
	if err != nil {
		t.Fatalf("third: %v", err)
	}
	if third.FullName == nil || *third.FullName != name {
		t.Fatalf("nil name must keep the stored one: %+v", third)
	}
	byIdentity, err := s.AccountByKratosIdentity(ctx, identity)
	if err != nil || byIdentity.ID != first.ID {
		t.Fatalf("by identity: %+v %v", byIdentity, err)
	}
	byID, err := s.AccountByID(ctx, first.ID)
	if err != nil || byID.KratosIdentityID != identity {
		t.Fatalf("by id: %+v %v", byID, err)
	}
}

func TestAccountReadsMissAsNotFound(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	_, err := s.AccountByID(ctx, randomUUID(t))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("AccountByID = %v", err)
	}
	_, err = s.AccountByKratosIdentity(ctx, "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("AccountByKratosIdentity = %v", err)
	}
}

func TestSharedEmailAcrossIdentitiesIsAllowed(t *testing.T) {
	s, _ := newTestStore(t)
	mustAccount(t, s, "admin@example.com")
	mustAccount(t, s, "admin@example.com")
}

func TestUpperCaseEmailIsRejectedByTheSchema(t *testing.T) {
	_, pool := newTestStore(t)
	_, err := pool.Exec(context.Background(),
		"INSERT INTO iam_accounts (kratos_identity_id, email) VALUES ($1::uuid, 'Jane@example.com')", randomUUID(t))
	var pgErr *pgconn.PgError
	isCheckViolation := errors.As(err, &pgErr) && pgErr.Code == "23514"
	if !isCheckViolation {
		t.Fatalf("want check_violation, got %v", err)
	}
}
