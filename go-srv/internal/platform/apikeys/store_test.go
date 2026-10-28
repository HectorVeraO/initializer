package apikeys

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// insertAPIKey stores a key row; a nil tenantID is a key without a tenant.
func insertAPIKey(pool *pgxpool.Pool, tenantID any, createdBy, prefix string, scopes []string) error {
	_, err := pool.Exec(context.Background(),
		`INSERT INTO apk_keys (tenant_id, name, key_prefix, secret_hash, scopes, created_by_account_id)
VALUES ($1::uuid, 'ci', $2, $3, $4, $5::uuid)`,
		tenantID, prefix, make([]byte, 32), scopes, createdBy)
	return err
}

func isCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}

func TestMigrationsApplyTwiceIdempotently(t *testing.T) {
	pool := newTestPool(t)
	err := applyMigrations(context.Background(), pool)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
}

func TestMigrationsCreateOnlyTheKeyTable(t *testing.T) {
	pool := newTestPool(t)
	rows, err := pool.Query(context.Background(),
		"SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_name <> 'cor_migrations_api_keys' ORDER BY table_name")
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("collect tables: %v", err)
	}
	if strings.Join(tables, ",") != "apk_keys" {
		t.Fatalf("tables = %v, want only apk_keys (no iam_* or ten_* table)", tables)
	}
	var ledger int
	err = pool.QueryRow(context.Background(), "SELECT count(*) FROM cor_migrations_api_keys").Scan(&ledger)
	if err != nil || ledger == 0 {
		t.Fatalf("ledger cor_migrations_api_keys rows = %d, err = %v", ledger, err)
	}
}

func TestAPIKeyConstraints(t *testing.T) {
	pool := newTestPool(t)
	tenant := randomUUID(t)
	admin := randomUUID(t)
	valid := keyPrefixLabel + strings.Repeat("a", keyIDChars)

	err := insertAPIKey(pool, tenant, admin, valid, []string{})
	if !isCheckViolation(err) {
		t.Errorf("empty scopes: want check_violation, got %v", err)
	}
	err = insertAPIKey(pool, tenant, admin, keyPrefixLabel+strings.Repeat("A", keyIDChars), []string{"tenant:read"})
	if !isCheckViolation(err) {
		t.Errorf("upper-case prefix: want check_violation, got %v", err)
	}
	err = insertAPIKey(pool, tenant, admin, "ak_test_"+strings.Repeat("a", keyIDChars), []string{"tenant:read"})
	if !isCheckViolation(err) {
		t.Errorf("other label: want check_violation, got %v", err)
	}
	err = insertAPIKey(pool, tenant, admin, keyPrefixLabel+strings.Repeat("a", keyIDChars+1), []string{"tenant:read"})
	if !isCheckViolation(err) {
		t.Errorf("long prefix: want check_violation, got %v", err)
	}
	err = insertAPIKey(pool, tenant, admin, valid, []string{"tenant:read"})
	if err != nil {
		t.Fatalf("valid key: %v", err)
	}
	err = insertAPIKey(pool, tenant, admin, valid, []string{"tenant:read"})
	if err == nil {
		t.Error("duplicate prefix accepted")
	}
}

func TestAPIKeyRowsCarryNoForeignKeys(t *testing.T) {
	pool := newTestPool(t)
	global := keyPrefixLabel + strings.Repeat("b", keyIDChars)
	foreign := keyPrefixLabel + strings.Repeat("c", keyIDChars)

	err := insertAPIKey(pool, nil, randomUUID(t), global, []string{"tenant:read"})
	if err != nil {
		t.Errorf("a key without a tenant: %v", err)
	}
	err = insertAPIKey(pool, randomUUID(t), randomUUID(t), foreign, []string{"tenant:read"})
	if err != nil {
		t.Errorf("a key of a tenant and creator that no table holds: %v", err)
	}
	var references int
	err = pool.QueryRow(context.Background(),
		"SELECT count(*) FROM information_schema.table_constraints WHERE table_name = 'apk_keys' AND constraint_type = 'FOREIGN KEY'").Scan(&references)
	if err != nil || references != 0 {
		t.Errorf("foreign keys on apk_keys = %d, err = %v", references, err)
	}
}
