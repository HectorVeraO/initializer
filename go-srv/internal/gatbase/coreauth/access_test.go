package coreauth_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/coreauth"
)

var accessNow = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

type accessEnv struct {
	t      *testing.T
	pool   *pgxpool.Pool
	access *coreauth.Access
}

func newAccessEnv(t *testing.T, operatorEmails ...string) *accessEnv {
	t.Helper()
	pool := coreauth.NewTestPool(t)
	return &accessEnv{t: t, pool: pool, access: coreauth.NewAccess(pool, clock.Fixed(accessNow), operatorEmails)}
}

func uniqueSuffix(t testing.TB) string {
	t.Helper()
	b := make([]byte, 4)
	_, err := rand.Read(b)
	if err != nil {
		t.Fatalf("random: %v", err)
	}
	return hex.EncodeToString(b)
}

func (e *accessEnv) exec(sql string, args ...any) {
	e.t.Helper()
	_, err := e.pool.Exec(context.Background(), sql, args...)
	if err != nil {
		e.t.Fatalf("exec %q: %v", sql, err)
	}
}

func (e *accessEnv) scanString(sql string, args ...any) string {
	e.t.Helper()
	var out string
	err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&out)
	if err != nil {
		e.t.Fatalf("query %q: %v", sql, err)
	}
	return out
}

// account links a fresh identity and returns its account.
func (e *accessEnv) account() coreauth.Account {
	e.t.Helper()
	suffix := uniqueSuffix(e.t)
	identity := e.scanString("SELECT gen_random_uuid()::text")
	account, err := e.access.LinkIdentity(context.Background(), identity, "jane-"+suffix+"@example.com")
	if err != nil {
		e.t.Fatalf("link identity: %v", err)
	}
	return account
}

func (e *accessEnv) tenant(slug, status string) string {
	e.t.Helper()
	return e.scanString(
		"INSERT INTO ten_tenants (slug, name, status) VALUES ($1, $1, $2) RETURNING id::text", slug, status)
}

func (e *accessEnv) member(tenantID, accountID, role, status string, createdAt time.Time) {
	e.t.Helper()
	e.exec(`INSERT INTO ten_memberships (tenant_id, account_id, role, status, created_at)
VALUES ($1::uuid, $2::uuid, $3, $4, $5)`, tenantID, accountID, role, status, createdAt)
}

func (e *accessEnv) setAccountStatus(accountID, status string) {
	e.t.Helper()
	e.exec("UPDATE iam_accounts SET status = $2 WHERE id = $1::uuid", accountID, status)
}

func requireDenied(t *testing.T, err error, code api.Code) *api.Error {
	t.Helper()
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("want 403 %s, got %v", code, err)
	}
	if apiErr.Status != http.StatusForbidden || apiErr.Code != code {
		t.Fatalf("want 403 %s, got %d %s", code, apiErr.Status, apiErr.Code)
	}
	return apiErr
}

func TestCheckAccessAccountStates(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	for _, status := range []string{"locked", "disabled"} {
		account := env.account()
		env.setAccountStatus(account.ID, status)
		_, err := env.access.CheckAccess(ctx, account.ID, nil)
		requireDenied(t, err, coreauth.CodeAccountLockedOrDisabled)
	}
	missing := env.scanString("SELECT gen_random_uuid()::text")
	for _, id := range []string{missing, "not-a-uuid", ""} {
		_, err := env.access.CheckAccess(ctx, id, nil)
		requireDenied(t, err, coreauth.CodeAccountLockedOrDisabled)
	}
}

func TestCheckAccessAccountStepWinsOverTenantSteps(t *testing.T) {
	env := newAccessEnv(t)
	account := env.account()
	tenantID := env.tenant("acme", "suspended")
	env.member(tenantID, account.ID, "admin", "revoked", accessNow)
	env.setAccountStatus(account.ID, "locked")
	_, err := env.access.CheckAccess(context.Background(), account.ID, &tenantID)
	requireDenied(t, err, coreauth.CodeAccountLockedOrDisabled)
}

func TestCheckAccessWithoutTenantGrantsAccountOnly(t *testing.T) {
	env := newAccessEnv(t, "operator@example.com")
	operator, err := env.access.LinkIdentity(context.Background(), env.scanString("SELECT gen_random_uuid()::text"), "operator@example.com")
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	grant, err := env.access.CheckAccess(context.Background(), operator.ID, nil)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if grant.Role != "" || grant.TenantSlug != "" || !grant.PlatformOperator {
		t.Fatalf("grant = %+v", grant)
	}
}

func TestCheckAccessNonMemberAndUnknownTenantLookAlike(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	account := env.account()
	suspended := env.tenant("tenant-a", "suspended")
	other := env.tenant("tenant-b", "active")
	unknown := env.scanString("SELECT gen_random_uuid()::text")
	badID := "not-a-uuid"
	for _, tenantID := range []string{suspended, other, unknown, badID, ""} {
		_, err := env.access.CheckAccess(ctx, account.ID, &tenantID)
		apiErr := requireDenied(t, err, coreauth.CodeTenantAccessDenied)
		if strings.Contains(apiErr.Message, "tenant-") {
			t.Fatalf("message leaks tenant data: %q", apiErr.Message)
		}
	}
}

func TestCheckAccessSuspendedTenantBeatsMembershipState(t *testing.T) {
	env := newAccessEnv(t)
	account := env.account()
	tenantID := env.tenant("acme", "suspended")
	env.member(tenantID, account.ID, "member", "revoked", accessNow)
	_, err := env.access.CheckAccess(context.Background(), account.ID, &tenantID)
	requireDenied(t, err, coreauth.CodeTenantSuspended)
}

func TestCheckAccessMembershipStates(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	tenantID := env.tenant("acme", "active")

	revoked := env.account()
	env.member(tenantID, revoked.ID, "member", "revoked", accessNow)
	_, err := env.access.CheckAccess(ctx, revoked.ID, &tenantID)
	revokedErr := requireDenied(t, err, coreauth.CodeMembershipRevoked)

	suspended := env.account()
	env.member(tenantID, suspended.ID, "member", "suspended", accessNow)
	_, err = env.access.CheckAccess(ctx, suspended.ID, &tenantID)
	suspendedErr := requireDenied(t, err, coreauth.CodeMembershipRevoked)

	if suspendedErr.Message != "The membership is suspended." {
		t.Fatalf("suspended message = %q", suspendedErr.Message)
	}
	if revokedErr.Message != "The membership was revoked." {
		t.Fatalf("revoked message = %q", revokedErr.Message)
	}
}

func TestCheckAccessGrantCarriesRoleOperatorAndSlug(t *testing.T) {
	env := newAccessEnv(t, "operator@example.com")
	ctx := context.Background()
	tenantID := env.tenant("acme", "active")
	operator, err := env.access.LinkIdentity(ctx, env.scanString("SELECT gen_random_uuid()::text"), "operator@example.com")
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	env.member(tenantID, operator.ID, "viewer", "active", accessNow)
	plain := env.account()
	env.member(tenantID, plain.ID, "admin", "active", accessNow)

	grant, err := env.access.CheckAccess(ctx, operator.ID, &tenantID)
	if err != nil {
		t.Fatalf("operator: %v", err)
	}
	want := coreauth.Grant{Role: reqctx.RoleViewer, PlatformOperator: true, TenantSlug: "acme"}
	if grant != want {
		t.Fatalf("operator grant = %+v, want %+v", grant, want)
	}
	grant, err = env.access.CheckAccess(ctx, plain.ID, &tenantID)
	if err != nil {
		t.Fatalf("plain: %v", err)
	}
	want = coreauth.Grant{Role: reqctx.RoleAdmin, PlatformOperator: false, TenantSlug: "acme"}
	if grant != want {
		t.Fatalf("plain grant = %+v, want %+v", grant, want)
	}
}

func TestCheckAccessIgnoresAnotherAccountsMembership(t *testing.T) {
	env := newAccessEnv(t)
	tenantID := env.tenant("acme", "active")
	owner := env.account()
	env.member(tenantID, owner.ID, "admin", "active", accessNow)
	stranger := env.account()
	_, err := env.access.CheckAccess(context.Background(), stranger.ID, &tenantID)
	requireDenied(t, err, coreauth.CodeTenantAccessDenied)
}

func TestSelectTenantPrefersLastActive(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	account := env.account()
	older := env.tenant("tenant-a", "active")
	newer := env.tenant("tenant-b", "active")
	env.member(older, account.ID, "member", "active", accessNow)
	env.member(newer, account.ID, "admin", "active", accessNow.Add(time.Hour))
	err := env.access.SetLastActiveTenant(ctx, account.ID, newer)
	if err != nil {
		t.Fatalf("set last active: %v", err)
	}
	got, grant, err := env.access.SelectTenant(ctx, account.ID)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got == nil || *got != newer || grant.Role != reqctx.RoleAdmin || grant.TenantSlug != "tenant-b" {
		t.Fatalf("got %v %+v", got, grant)
	}
}

func TestSelectTenantFallsBackToOldestActiveMembership(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	account := env.account()
	revokedFirst := env.tenant("tenant-a", "active")
	suspendedTenant := env.tenant("tenant-b", "suspended")
	oldestUsable := env.tenant("tenant-c", "active")
	newest := env.tenant("tenant-d", "active")
	env.member(revokedFirst, account.ID, "member", "revoked", accessNow)
	env.member(suspendedTenant, account.ID, "member", "active", accessNow.Add(time.Minute))
	env.member(oldestUsable, account.ID, "viewer", "active", accessNow.Add(2*time.Minute))
	env.member(newest, account.ID, "admin", "active", accessNow.Add(3*time.Minute))
	// The last active tenant no longer lets the account in.
	err := env.access.SetLastActiveTenant(ctx, account.ID, revokedFirst)
	if err != nil {
		t.Fatalf("set last active: %v", err)
	}
	got, grant, err := env.access.SelectTenant(ctx, account.ID)
	if err != nil {
		t.Fatalf("select must not fail with a 403: %v", err)
	}
	if got == nil || *got != oldestUsable || grant.Role != reqctx.RoleViewer || grant.TenantSlug != "tenant-c" {
		t.Fatalf("got %v %+v", got, grant)
	}
}

func TestSelectTenantTiesBreakOnMembershipID(t *testing.T) {
	env := newAccessEnv(t)
	account := env.account()
	first := env.tenant("tenant-a", "active")
	second := env.tenant("tenant-b", "active")
	env.member(first, account.ID, "member", "active", accessNow)
	env.member(second, account.ID, "member", "active", accessNow)
	wantTenant := env.scanString(
		"SELECT tenant_id::text FROM ten_memberships WHERE account_id = $1::uuid ORDER BY id LIMIT 1", account.ID)
	got, _, err := env.access.SelectTenant(context.Background(), account.ID)
	if err != nil || got == nil || *got != wantTenant {
		t.Fatalf("got %v %v, want %s", got, err, wantTenant)
	}
}

func TestSelectTenantNoneKeepsOperatorFlag(t *testing.T) {
	env := newAccessEnv(t, "operator@example.com")
	ctx := context.Background()
	operator, err := env.access.LinkIdentity(ctx, env.scanString("SELECT gen_random_uuid()::text"), "operator@example.com")
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	got, grant, err := env.access.SelectTenant(ctx, operator.ID)
	if err != nil || got != nil || !grant.PlatformOperator || grant.Role != "" {
		t.Fatalf("got %v %+v %v", got, grant, err)
	}
}

func TestSelectTenantNeverErrorsForInactiveOrUnknownAccounts(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	tenantID := env.tenant("acme", "active")
	locked := env.account()
	env.member(tenantID, locked.ID, "admin", "active", accessNow)
	env.setAccountStatus(locked.ID, "locked")
	for _, id := range []string{locked.ID, env.scanString("SELECT gen_random_uuid()::text"), "not-a-uuid"} {
		got, grant, err := env.access.SelectTenant(ctx, id)
		if err != nil || got != nil || grant != (coreauth.Grant{}) {
			t.Fatalf("id %q: got %v %+v %v", id, got, grant, err)
		}
	}
}

func TestLinkIdentityCreatesOnFirstSight(t *testing.T) {
	env := newAccessEnv(t)
	identity := env.scanString("SELECT gen_random_uuid()::text")
	email := "Jane-" + uniqueSuffix(t) + "@Example.COM"
	account, err := env.access.LinkIdentity(context.Background(), identity, email)
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if account.Email != strings.ToLower(email) || account.KratosIdentityID != identity ||
		account.Status != "active" || account.IsPlatformOperator || !account.CreatedAt.Equal(accessNow) {
		t.Fatalf("account = %+v", account)
	}
}

func TestLinkIdentityIsStableAndRefreshesChangedEmail(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	identity := env.scanString("SELECT gen_random_uuid()::text")
	suffix := uniqueSuffix(t)
	first, err := env.access.LinkIdentity(ctx, identity, "jane-"+suffix+"@example.com")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	again, err := env.access.LinkIdentity(ctx, identity, "jane-"+suffix+"@example.com")
	if err != nil || again.ID != first.ID {
		t.Fatalf("again = %+v %v", again, err)
	}
	renamed, err := env.access.LinkIdentity(ctx, identity, "Renamed-"+suffix+"@example.com")
	if err != nil {
		t.Fatalf("renamed: %v", err)
	}
	if renamed.ID != first.ID || renamed.Email != "renamed-"+suffix+"@example.com" {
		t.Fatalf("renamed = %+v", renamed)
	}
	count := env.scanString("SELECT count(*)::text FROM iam_accounts WHERE kratos_identity_id = $1::uuid", identity)
	if count != "1" {
		t.Fatalf("accounts for the identity = %s", count)
	}
}

func TestLinkIdentityMovesUpdatedAtOnlyWhenTheEmailChanges(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	later := coreauth.NewAccess(env.pool, clock.Fixed(accessNow.Add(time.Hour)), nil)
	latest := coreauth.NewAccess(env.pool, clock.Fixed(accessNow.Add(2*time.Hour)), nil)
	identity := env.scanString("SELECT gen_random_uuid()::text")
	email := "jane-" + uniqueSuffix(t) + "@example.com"
	created, err := env.access.LinkIdentity(ctx, identity, email)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !created.UpdatedAt.Equal(accessNow) {
		t.Fatalf("created updated_at = %v, want %v", created.UpdatedAt, accessNow)
	}
	unchanged, err := later.LinkIdentity(ctx, identity, email)
	if err != nil {
		t.Fatalf("unchanged: %v", err)
	}
	if !unchanged.UpdatedAt.Equal(accessNow) {
		t.Fatalf("an upsert without a change moved updated_at to %v", unchanged.UpdatedAt)
	}
	changed, err := latest.LinkIdentity(ctx, identity, "renamed-"+email)
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if !changed.UpdatedAt.Equal(accessNow.Add(2 * time.Hour)) {
		t.Fatalf("an email change left updated_at at %v", changed.UpdatedAt)
	}
	if !changed.CreatedAt.Equal(accessNow) {
		t.Fatalf("created_at moved to %v", changed.CreatedAt)
	}
}

func TestLinkIdentityOperatorFlagAtCreationOnly(t *testing.T) {
	env := newAccessEnv(t, "admin@example.com")
	ctx := context.Background()
	listed, err := env.access.LinkIdentity(ctx, env.scanString("SELECT gen_random_uuid()::text"), "ADMIN@example.com")
	if err != nil || !listed.IsPlatformOperator {
		t.Fatalf("listed = %+v %v", listed, err)
	}
	unlistedIdentity := env.scanString("SELECT gen_random_uuid()::text")
	unlisted, err := env.access.LinkIdentity(ctx, unlistedIdentity, "jane-"+uniqueSuffix(t)+"@example.com")
	if err != nil || unlisted.IsPlatformOperator {
		t.Fatalf("unlisted = %+v %v", unlisted, err)
	}
	// An email change to a listed address never grants the flag after creation.
	changed, err := env.access.LinkIdentity(ctx, unlistedIdentity, "admin@example.com")
	if err != nil || changed.IsPlatformOperator {
		t.Fatalf("changed = %+v %v", changed, err)
	}
}

func TestLinkIdentityRejectsBadInput(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	identity := env.scanString("SELECT gen_random_uuid()::text")
	cases := map[string][2]string{
		"identity not a uuid": {"not-a-uuid", "jane@example.com"},
		"empty identity":      {"", "jane@example.com"},
		"empty email":         {identity, ""},
		"blank email":         {identity, "   "},
		"oversized email":     {identity, strings.Repeat("a", 250) + "@example.com"},
	}
	for name, in := range cases {
		_, err := env.access.LinkIdentity(ctx, in[0], in[1])
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	count := env.scanString("SELECT count(*)::text FROM iam_accounts")
	if count != "0" {
		t.Fatalf("a rejected call created %s accounts", count)
	}
}

func TestLinkIdentityEmailLengthBoundary(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	const domain = "@example.com"
	atLimit := strings.Repeat("a", 254-len(domain)) + domain
	overLimit := strings.Repeat("a", 255-len(domain)) + domain
	identity := env.scanString("SELECT gen_random_uuid()::text")
	_, err := env.access.LinkIdentity(ctx, identity, atLimit)
	if err != nil {
		t.Fatalf("254-character email rejected: %v", err)
	}
	_, err = env.access.LinkIdentity(ctx, env.scanString("SELECT gen_random_uuid()::text"), overLimit)
	if err == nil {
		t.Fatal("255-character email accepted")
	}
}

func TestSetLastActiveTenantPersistsAndRejectsBadIDs(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	account := env.account()
	tenantID := env.tenant("acme", "active")
	later := coreauth.NewAccess(env.pool, clock.Fixed(accessNow.Add(time.Hour)), nil)
	err := later.SetLastActiveTenant(ctx, account.ID, tenantID)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	stored := env.scanString("SELECT last_active_tenant_id::text FROM iam_accounts WHERE id = $1::uuid", account.ID)
	if stored != tenantID {
		t.Fatalf("stored %s, want %s", stored, tenantID)
	}
	updatedAt := env.scanString("SELECT updated_at::text FROM iam_accounts WHERE id = $1::uuid", account.ID)
	want := env.scanString("SELECT $1::timestamptz::text", accessNow.Add(time.Hour))
	if updatedAt != want {
		t.Fatalf("updated_at = %s, want %s", updatedAt, want)
	}
	for _, bad := range [][2]string{{"x", tenantID}, {account.ID, "x"}, {"", ""}} {
		err = env.access.SetLastActiveTenant(ctx, bad[0], bad[1])
		if err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

// FuzzCheckAccessIDs: whatever the ids, an unknown account or tenant is a
// 403 and never a grant or an operational error.
func FuzzCheckAccessIDs(f *testing.F) {
	pool := coreauth.NewTestPool(f)
	access := coreauth.NewAccess(pool, clock.Fixed(accessNow), nil)
	for _, seed := range []string{"", "x", "00000000-0000-0000-0000-000000000000", "' OR 1=1 --", "\x00", "//evil.com"} {
		f.Add(seed, seed)
	}
	f.Fuzz(func(t *testing.T, accountID, tenantID string) {
		_, err := access.CheckAccess(context.Background(), accountID, &tenantID)
		var apiErr *api.Error
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden {
			t.Fatalf("account %q tenant %q: err = %v", accountID, tenantID, err)
		}
	})
}
