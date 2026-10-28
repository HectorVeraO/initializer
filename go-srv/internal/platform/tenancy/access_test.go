package tenancy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

var accessNow = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// accessEnv is a database with the tenancy tables only; accounts are soft ids
// no table holds.
type accessEnv struct {
	t    *testing.T
	pool *pgxpool.Pool
}

func newAccessEnv(t *testing.T) *accessEnv {
	t.Helper()
	return &accessEnv{t: t, pool: newTestPool(t)}
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

func (e *accessEnv) account() Account {
	e.t.Helper()
	raw := make([]byte, 4)
	_, err := rand.Read(raw)
	if err != nil {
		e.t.Fatalf("random: %v", err)
	}
	return Account{ID: randomUUID(e.t), Email: "jane-" + hex.EncodeToString(raw) + "@example.com"}
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

func accountID(id string) reqctx.AccountID { return reqctx.AccountID(id) }

func TestSelectPrefersTheRememberedTenant(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	ta := newTenantAccess(env.pool, clock.Fixed(accessNow))
	account := env.account()
	older := env.tenant("tenant-a", "active")
	newer := env.tenant("tenant-b", "active")
	env.member(older, account.ID, "member", "active", accessNow)
	env.member(newer, account.ID, "admin", "active", accessNow.Add(time.Hour))
	err := ta.Remember(ctx, accountID(account.ID), reqctx.TenantID(newer))
	if err != nil {
		t.Fatalf("remember: %v", err)
	}

	got, err := ta.Select(ctx, accountID(account.ID))

	if err != nil || got == nil || string(got.Tenant.ID) != newer || got.Role != reqctx.RoleAdmin || got.Tenant.Slug != "tenant-b" {
		t.Fatalf("got %+v err=%v", got, err)
	}
}

func TestSelectFallsBackToTheOldestActiveMembership(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	ta := newTenantAccess(env.pool, clock.Fixed(accessNow))
	account := env.account()
	revokedFirst := env.tenant("tenant-a", "active")
	suspendedTenant := env.tenant("tenant-b", "suspended")
	oldestUsable := env.tenant("tenant-c", "active")
	newest := env.tenant("tenant-d", "active")
	env.member(revokedFirst, account.ID, "member", "revoked", accessNow)
	env.member(suspendedTenant, account.ID, "member", "active", accessNow.Add(time.Minute))
	env.member(oldestUsable, account.ID, "viewer", "active", accessNow.Add(2*time.Minute))
	env.member(newest, account.ID, "admin", "active", accessNow.Add(3*time.Minute))
	// The remembered tenant no longer lets the account in.
	err := ta.Remember(ctx, accountID(account.ID), reqctx.TenantID(revokedFirst))
	if err != nil {
		t.Fatalf("remember: %v", err)
	}

	got, err := ta.Select(ctx, accountID(account.ID))

	if err != nil || got == nil || string(got.Tenant.ID) != oldestUsable || got.Role != reqctx.RoleViewer {
		t.Fatalf("got %+v err=%v, want the oldest usable tenant and no rejection", got, err)
	}
}

func TestSelectTiesBreakOnMembershipID(t *testing.T) {
	env := newAccessEnv(t)
	ta := newTenantAccess(env.pool, clock.Fixed(accessNow))
	account := env.account()
	first := env.tenant("tenant-a", "active")
	second := env.tenant("tenant-b", "active")
	env.member(first, account.ID, "member", "active", accessNow)
	env.member(second, account.ID, "member", "active", accessNow)
	wantTenant := env.scanString(
		"SELECT tenant_id::text FROM ten_memberships WHERE account_id = $1::uuid ORDER BY id LIMIT 1", account.ID)

	got, err := ta.Select(context.Background(), accountID(account.ID))

	if err != nil || got == nil || string(got.Tenant.ID) != wantTenant {
		t.Fatalf("got %+v err=%v, want %s", got, err, wantTenant)
	}
}

func TestSelectUnknownOrMemberlessAccountIsNil(t *testing.T) {
	env := newAccessEnv(t)
	ta := newTenantAccess(env.pool, clock.Fixed(accessNow))
	memberless := env.account()
	for _, id := range []string{memberless.ID, env.scanString("SELECT gen_random_uuid()::text"), "not-a-uuid"} {
		got, err := ta.Select(context.Background(), accountID(id))
		if err != nil || got != nil {
			t.Fatalf("id %q: got %+v err=%v, want nil", id, got, err)
		}
	}
}

func TestRememberStampsLastSelectedAtAndUpdatedAtFromTheClock(t *testing.T) {
	env := newAccessEnv(t)
	later := accessNow.Add(time.Hour)
	ta := newTenantAccess(env.pool, clock.Fixed(later))
	account := env.account()
	tenantID := env.tenant("acme", "active")
	env.member(tenantID, account.ID, "member", "active", accessNow)

	err := ta.Remember(context.Background(), accountID(account.ID), reqctx.TenantID(tenantID))

	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	want := env.scanString("SELECT $1::timestamptz::text", later)
	for _, column := range []string{"updated_at", "last_selected_at"} {
		got := env.scanString("SELECT "+column+"::text FROM ten_memberships WHERE account_id = $1::uuid", account.ID)
		if got != want {
			t.Fatalf("%s = %s, want %s", column, got, want)
		}
	}
}

func TestRememberWithoutAMembershipIsAnOperationalError(t *testing.T) {
	env := newAccessEnv(t)
	ta := newTenantAccess(env.pool, clock.Fixed(accessNow))
	account := env.account()
	tenantID := env.tenant("acme", "active")

	err := ta.Remember(context.Background(), accountID(account.ID), reqctx.TenantID(tenantID))

	var apiErr *api.Error
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want a non-API operational error", err)
	}
}

func TestSelectFollowsTheLatestRememberedTenant(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	account := env.account()
	first := env.tenant("tenant-a", "active")
	second := env.tenant("tenant-b", "active")
	env.member(first, account.ID, "member", "active", accessNow)
	env.member(second, account.ID, "member", "active", accessNow.Add(time.Minute))
	steps := []struct {
		remember string
		at       time.Time
	}{{second, accessNow.Add(time.Hour)}, {first, accessNow.Add(2 * time.Hour)}, {second, accessNow.Add(3 * time.Hour)}}
	for _, step := range steps {
		ta := newTenantAccess(env.pool, clock.Fixed(step.at))
		err := ta.Remember(ctx, accountID(account.ID), reqctx.TenantID(step.remember))
		if err != nil {
			t.Fatalf("remember: %v", err)
		}

		got, err := ta.Select(ctx, accountID(account.ID))

		if err != nil || got == nil || string(got.Tenant.ID) != step.remember {
			t.Fatalf("after remembering %s: got %+v err=%v", step.remember, got, err)
		}
	}
}

func TestSelectSkipsARememberedTenantTheAccountCannotEnter(t *testing.T) {
	cases := map[string]func(env *accessEnv, tenantID, accountID string){
		"suspended tenant": func(env *accessEnv, tenantID, _ string) {
			env.exec("UPDATE ten_tenants SET status = 'suspended' WHERE id = $1::uuid", tenantID)
		},
		"revoked membership": func(env *accessEnv, tenantID, accountID string) {
			env.exec("UPDATE ten_memberships SET status = 'revoked' WHERE tenant_id = $1::uuid AND account_id = $2::uuid", tenantID, accountID)
		},
	}
	for name, degrade := range cases {
		t.Run(name, func(t *testing.T) {
			env := newAccessEnv(t)
			ctx := context.Background()
			ta := newTenantAccess(env.pool, clock.Fixed(accessNow))
			account := env.account()
			remembered := env.tenant("tenant-a", "active")
			fallback := env.tenant("tenant-b", "active")
			env.member(remembered, account.ID, "member", "active", accessNow.Add(time.Minute))
			env.member(fallback, account.ID, "viewer", "active", accessNow)
			err := ta.Remember(ctx, accountID(account.ID), reqctx.TenantID(remembered))
			if err != nil {
				t.Fatalf("remember: %v", err)
			}
			degrade(env, remembered, account.ID)

			got, err := ta.Select(ctx, accountID(account.ID))

			if err != nil || got == nil || string(got.Tenant.ID) != fallback {
				t.Fatalf("got %+v err=%v, want the fallback tenant and no rejection", got, err)
			}
		})
	}
}

func TestReadFailureIsOperationalNotAccessDenial(t *testing.T) {
	env := newAccessEnv(t)
	ta := newTenantAccess(env.pool, clock.Fixed(accessNow))
	account := env.account()
	tenantID := env.tenant("acme", "active")
	env.member(tenantID, account.ID, "admin", "active", accessNow)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	_, grantErr := ta.Grant(cancelled, accountID(account.ID), reqctx.TenantID(tenantID))
	_, selectErr := ta.Select(cancelled, accountID(account.ID))

	for name, err := range map[string]error{"Grant": grantErr, "Select": selectErr} {
		var apiErr *api.Error
		if err == nil || errors.As(err, &apiErr) {
			t.Fatalf("%s: err = %v, want a non-API operational error", name, err)
		}
	}
}

// FuzzGrantIDs: whatever the ids, an unknown account or tenant is a 403 and
// never a grant or an operational error.
func FuzzGrantIDs(f *testing.F) {
	pool := newTestPool(f)
	ta := newTenantAccess(pool, clock.Fixed(accessNow))
	for _, seed := range []string{"", "x", "00000000-0000-0000-0000-000000000000", "' OR 1=1 --", "\x00", "//evil.com"} {
		f.Add(seed, seed)
	}
	f.Fuzz(func(t *testing.T, account, tenant string) {
		_, err := ta.Grant(context.Background(), accountID(account), reqctx.TenantID(tenant))
		var apiErr *api.Error
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden {
			t.Fatalf("account %q tenant %q: err = %v", account, tenant, err)
		}
	})
}

func TestTenantAccessGrant(t *testing.T) {
	e := newTenEnv(t)
	ta := newTenantAccess(e.pool, e.clock)
	active := e.tenant("tenant-a")
	suspended := e.tenant("tenant-b")
	e.exec("UPDATE ten_tenants SET status = 'suspended' WHERE id = $1::uuid", suspended.ID)
	member := e.account("member@example.com")
	outsider := e.account("outsider@example.com")
	e.member(active.ID, member.ID, "viewer", statusActive)
	e.member(suspended.ID, member.ID, "admin", statusActive)
	revoked := e.account("revoked@example.com")
	e.member(active.ID, revoked.ID, "member", "revoked")
	paused := e.account("paused@example.com")
	e.member(active.ID, paused.ID, "member", statusSuspended)

	t.Run("active member gets tenant and role", func(t *testing.T) {
		got, err := ta.Grant(context.Background(), reqctx.AccountID(member.ID), reqctx.TenantID(active.ID))
		if err != nil {
			t.Fatalf("grant: %v", err)
		}
		want := coreaccess.TenantGrant{
			Tenant: coreaccess.TenantRef{ID: reqctx.TenantID(active.ID), Slug: "tenant-a", Name: "Tenant tenant-a", Status: coreaccess.TenantActive},
			Role:   reqctx.RoleViewer,
		}
		if got != want {
			t.Fatalf("grant = %+v, want %+v", got, want)
		}
	})

	denied := map[string]struct {
		account string
		tenant  string
		code    api.Code
	}{
		"non-member":           {outsider.ID, active.ID, api.CodeTenantAccessDenied},
		"unknown tenant":       {member.ID, "99999999-9999-4999-8999-999999999999", api.CodeTenantAccessDenied},
		"non-uuid tenant":      {member.ID, "not-a-uuid", api.CodeTenantAccessDenied},
		"non-uuid account":     {"not-a-uuid", active.ID, api.CodeTenantAccessDenied},
		"suspended tenant":     {member.ID, suspended.ID, api.CodeTenantSuspended},
		"revoked membership":   {revoked.ID, active.ID, api.CodeMembershipRevoked},
		"suspended membership": {paused.ID, active.ID, api.CodeMembershipRevoked},
	}
	for name, tc := range denied {
		t.Run(name, func(t *testing.T) {
			_, err := ta.Grant(context.Background(), reqctx.AccountID(tc.account), reqctx.TenantID(tc.tenant))

			requireAPIError(t, err, http.StatusForbidden, tc.code)
		})
	}
}

func TestTenantAccessRemember(t *testing.T) {
	ctx := context.Background()
	e := newTenEnv(t)
	ta := newTenantAccess(e.pool, e.clock)
	tenant := e.tenant("tenant-a")
	account := e.account("member@example.com")
	older := e.tenant("tenant-older")
	olderMembership := e.member(older.ID, account.ID, "member", statusActive)
	e.member(tenant.ID, account.ID, "member", statusActive)
	// The fallback picks the oldest membership, so Select returning tenant-a proves Remember ran.
	e.exec("UPDATE ten_memberships SET created_at = created_at - interval '1 day' WHERE id = $1::uuid", olderMembership.ID)

	err := ta.Remember(ctx, reqctx.AccountID(account.ID), reqctx.TenantID(tenant.ID))

	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	grant, err := ta.Select(ctx, reqctx.AccountID(account.ID))
	if err != nil || grant == nil || string(grant.Tenant.ID) != tenant.ID {
		t.Fatalf("select = %+v err=%v, want the remembered tenant", grant, err)
	}
	for name, ids := range map[string][2]string{
		"non-uuid account": {"not-a-uuid", tenant.ID},
		"non-uuid tenant":  {account.ID, "not-a-uuid"},
	} {
		if err := ta.Remember(ctx, reqctx.AccountID(ids[0]), reqctx.TenantID(ids[1])); err == nil {
			t.Fatalf("%s: remember succeeded, want an error", name)
		}
	}
}

func TestTenantAccessMemberships(t *testing.T) {
	ctx := context.Background()
	e := newTenEnv(t)
	ta := newTenantAccess(e.pool, e.clock)
	zulu := e.tenant("zulu")
	alpha := e.tenant("alpha")
	gone := e.tenant("gone")
	e.exec("UPDATE ten_tenants SET status = 'suspended' WHERE id = $1::uuid", zulu.ID)
	account := e.account("member@example.com")
	other := e.account("other@example.com")
	e.member(zulu.ID, account.ID, "admin", statusActive)
	e.member(alpha.ID, account.ID, "viewer", statusActive)
	e.member(gone.ID, account.ID, "member", "revoked")
	e.member(alpha.ID, other.ID, "admin", statusActive)

	got, err := ta.Memberships(ctx, reqctx.AccountID(account.ID))

	if err != nil {
		t.Fatalf("memberships: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d memberships, want 2: %+v", len(got), got)
	}
	if got[0].Tenant.Slug != "alpha" || got[0].Role != reqctx.RoleViewer || got[0].Status != statusActive {
		t.Fatalf("first = %+v, want alpha viewer active", got[0])
	}
	if got[1].Tenant.Slug != "zulu" || got[1].Tenant.Status != coreaccess.TenantSuspended || got[1].Role != reqctx.RoleAdmin {
		t.Fatalf("second = %+v, want zulu admin with a suspended tenant", got[1])
	}

	t.Run("no memberships and bad ids give an empty non-nil list", func(t *testing.T) {
		for name, id := range map[string]string{"non-uuid": "not-a-uuid", "unknown": "99999999-9999-4999-8999-999999999999"} {
			got, err := ta.Memberships(ctx, reqctx.AccountID(id))
			if err != nil || got == nil || len(got) != 0 {
				t.Fatalf("%s: got %v err=%v, want an empty non-nil list", name, got, err)
			}
		}
	})
}
