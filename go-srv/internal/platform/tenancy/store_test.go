package tenancy

import (
	"context"
	"crypto/rand"
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

func TestMigrationsApplyTwiceIdempotently(t *testing.T) {
	pool := newTestPool(t)
	err := applyMigrations(context.Background(), pool)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	s := newStore(pool, clock.Fixed(testNow))
	mustTenant(t, s, "acme")
}

func mustTenant(t *testing.T, s *store, slug string) Tenant {
	t.Helper()
	tenant, err := s.InsertTenant(context.Background(), slug, "Tenant "+slug)
	if err != nil {
		t.Fatalf("insert tenant %s: %v", slug, err)
	}
	return tenant
}

func TestMigrationsCreateOnlyTenancyTables(t *testing.T) {
	_, pool := newTestStore(t)
	rows, err := pool.Query(context.Background(),
		"SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_name <> 'cor_migrations_tenancy' ORDER BY table_name")
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("collect tables: %v", err)
	}
	if strings.Join(tables, ",") != "ten_invitations,ten_memberships,ten_tenants" {
		t.Fatalf("tables = %v, want only the three ten_ tables (no other module table)", tables)
	}
}

func TestOnlyTenantForeignKeysExistAndNoneReachesAnotherModule(t *testing.T) {
	_, pool := newTestStore(t)
	rows, err := pool.Query(context.Background(), `
SELECT conrelid::regclass::text || '->' || confrelid::regclass::text
FROM pg_constraint WHERE contype = 'f' ORDER BY 1`)
	if err != nil {
		t.Fatalf("list foreign keys: %v", err)
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("collect foreign keys: %v", err)
	}
	if strings.Join(keys, ",") != "ten_invitations->ten_tenants,ten_memberships->ten_tenants" {
		t.Fatalf("foreign keys = %v, want only the two tenant references inside the module", keys)
	}
}

func TestTenantRoundTrip(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	created := mustTenant(t, s, "acme")
	if created.Status != "active" || !created.CreatedAt.Equal(testNow) {
		t.Fatalf("unexpected tenant %+v", created)
	}
	byID, err := s.TenantByID(ctx, created.ID)
	if err != nil || byID.Slug != "acme" {
		t.Fatalf("by id: %+v %v", byID, err)
	}
	bySlug, err := s.TenantBySlug(ctx, "acme")
	if err != nil || bySlug.ID != created.ID {
		t.Fatalf("by slug: %+v %v", bySlug, err)
	}
}

func TestTenantReadsMissAsNotFound(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{randomUUID(t), "not-a-uuid", ""} {
		_, err := s.TenantByID(ctx, id)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("TenantByID(%q) = %v", id, err)
		}
	}
	_, err := s.TenantBySlug(ctx, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("TenantBySlug = %v", err)
	}
}

func TestDuplicateSlugIsAlreadyExists(t *testing.T) {
	s, _ := newTestStore(t)
	mustTenant(t, s, "acme")
	_, err := s.InsertTenant(context.Background(), "acme", "Other")
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("got %v", err)
	}
}

func TestBadSlugIsRejectedByTheSchema(t *testing.T) {
	s, _ := newTestStore(t)
	for _, slug := range []string{"Acme", "a", "-acme", "acme-", "ac me"} {
		_, err := s.InsertTenant(context.Background(), slug, "Name")
		var pgErr *pgconn.PgError
		isCheckViolation := errors.As(err, &pgErr) && pgErr.Code == "23514"
		if !isCheckViolation {
			t.Errorf("slug %q: want check_violation, got %v", slug, err)
		}
	}
}

func TestMembershipRoundTripAndUniqueness(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	tenantA := mustTenant(t, s, "tenant-a")
	tenantB := mustTenant(t, s, "tenant-b")
	account := Account{ID: randomUUID(t), Email: "admin@example.com"}

	created, err := s.InsertMembership(ctx, tenantA.ID, account.ID, "admin")
	if err != nil || created.Status != "active" {
		t.Fatalf("insert: %+v %v", created, err)
	}
	_, err = s.InsertMembership(ctx, tenantA.ID, account.ID, "member")
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate membership: %v", err)
	}
	_, err = s.InsertMembership(ctx, tenantB.ID, account.ID, "viewer")
	if err != nil {
		t.Fatalf("second tenant: %v", err)
	}
	found, err := s.MembershipByTenantAccount(ctx, tenantA.ID, account.ID)
	if err != nil || found.ID != created.ID || found.Role != "admin" {
		t.Fatalf("by tenant and account: %+v %v", found, err)
	}
	list, err := s.MembershipsByAccount(ctx, account.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %+v %v", list, err)
	}
	_, err = s.MembershipsByAccount(ctx, "not-a-uuid")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-uuid account id: want ErrNotFound, got %v", err)
	}
}

func TestMembershipOfAnotherTenantIsNotFound(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	tenantA := mustTenant(t, s, "tenant-a")
	tenantB := mustTenant(t, s, "tenant-b")
	account := Account{ID: randomUUID(t), Email: "admin@example.com"}
	_, err := s.InsertMembership(ctx, tenantA.ID, account.ID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.MembershipByTenantAccount(ctx, tenantB.ID, account.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestInvalidMembershipRoleIsRejectedByTheSchema(t *testing.T) {
	s, _ := newTestStore(t)
	tenant := mustTenant(t, s, "acme")
	account := Account{ID: randomUUID(t), Email: "admin@example.com"}
	_, err := s.InsertMembership(context.Background(), tenant.ID, account.ID, "owner")
	if err == nil {
		t.Fatal("role owner accepted")
	}
}

func insertInvitation(pool *pgxpool.Pool, tenantID, email, createdBy string, hash []byte) error {
	_, err := pool.Exec(context.Background(),
		`INSERT INTO ten_invitations (tenant_id, email, role, token_hash, expires_at, created_by_account_id)
VALUES ($1::uuid, $2, 'member', $3, now() + interval '1 day', $4::uuid)`,
		tenantID, email, hash, createdBy)
	return err
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestInvitationConstraints(t *testing.T) {
	s, pool := newTestStore(t)
	tenant := mustTenant(t, s, "acme")
	admin := Account{ID: randomUUID(t), Email: "admin@example.com"}

	err := insertInvitation(pool, tenant.ID, "jane@example.com", admin.ID, randomBytes(t, 31))
	if err == nil {
		t.Error("31-byte token hash accepted")
	}
	err = insertInvitation(pool, tenant.ID, "Jane@example.com", admin.ID, randomBytes(t, 32))
	if err == nil {
		t.Error("upper-case invitation email accepted")
	}
	err = insertInvitation(pool, tenant.ID, "jane@example.com", admin.ID, randomBytes(t, 32))
	if err != nil {
		t.Fatalf("valid invitation: %v", err)
	}
	err = insertInvitation(pool, tenant.ID, "jane@example.com", admin.ID, randomBytes(t, 32))
	if err == nil {
		t.Error("second pending invitation for one (tenant, email) accepted")
	}
}

func TestSecondInvitationIsAllowedAfterRevocation(t *testing.T) {
	s, pool := newTestStore(t)
	tenant := mustTenant(t, s, "acme")
	admin := Account{ID: randomUUID(t), Email: "admin@example.com"}
	err := insertInvitation(pool, tenant.ID, "jane@example.com", admin.ID, randomBytes(t, 32))
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(context.Background(), "UPDATE ten_invitations SET revoked_at = now()")
	if err != nil {
		t.Fatal(err)
	}
	err = insertInvitation(pool, tenant.ID, "jane@example.com", admin.ID, randomBytes(t, 32))
	if err != nil {
		t.Fatalf("re-invite after revoke: %v", err)
	}
}
