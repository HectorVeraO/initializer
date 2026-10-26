package coreauth

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/clock"
)

var testNow = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

func newTestStore(t *testing.T) (*store, *pgxpool.Pool) {
	t.Helper()
	pool := newTestPool(t)
	return newStore(pool, clock.Fixed(testNow)), pool
}

func mustTenant(t *testing.T, s *store, slug string) Tenant {
	t.Helper()
	tenant, err := s.InsertTenant(context.Background(), slug, "Tenant "+slug)
	if err != nil {
		t.Fatalf("insert tenant %s: %v", slug, err)
	}
	return tenant
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
	_, err = s.InsertTenant(context.Background(), "acme", "Acme")
	if err != nil {
		t.Fatalf("schema unusable after the second apply: %v", err)
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
	if err == nil {
		t.Fatal("upper-case email accepted")
	}
}

func TestMembershipRoundTripAndUniqueness(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	tenantA := mustTenant(t, s, "tenant-a")
	tenantB := mustTenant(t, s, "tenant-b")
	account := mustAccount(t, s, "admin@example.com")

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
}

func TestMembershipOfAnotherTenantIsNotFound(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	tenantA := mustTenant(t, s, "tenant-a")
	tenantB := mustTenant(t, s, "tenant-b")
	account := mustAccount(t, s, "admin@example.com")
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
	account := mustAccount(t, s, "admin@example.com")
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
	admin := mustAccount(t, s, "admin@example.com")

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
	admin := mustAccount(t, s, "admin@example.com")
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

func insertAPIKey(pool *pgxpool.Pool, tenantID, createdBy, prefix string, scopes []string) error {
	_, err := pool.Exec(context.Background(),
		`INSERT INTO iam_api_keys (tenant_id, name, key_prefix, secret_hash, scopes, created_by_account_id)
VALUES ($1::uuid, 'ci', $2, $3, $4, $5::uuid)`,
		tenantID, prefix, make([]byte, 32), scopes, createdBy)
	return err
}

func TestAPIKeyConstraints(t *testing.T) {
	s, pool := newTestStore(t)
	tenant := mustTenant(t, s, "acme")
	admin := mustAccount(t, s, "admin@example.com")
	valid := "gbk_live_abcdefghijklmnop"

	err := insertAPIKey(pool, tenant.ID, admin.ID, valid, []string{})
	if err == nil {
		t.Error("empty scopes accepted")
	}
	err = insertAPIKey(pool, tenant.ID, admin.ID, "gbk_live_ABCDEFGHIJKLMNOP", []string{"tenant:read"})
	if err == nil {
		t.Error("upper-case prefix accepted")
	}
	err = insertAPIKey(pool, tenant.ID, admin.ID, valid, []string{"tenant:read"})
	if err != nil {
		t.Fatalf("valid key: %v", err)
	}
	err = insertAPIKey(pool, tenant.ID, admin.ID, valid, []string{"tenant:read"})
	if err == nil {
		t.Error("duplicate prefix accepted")
	}
}
