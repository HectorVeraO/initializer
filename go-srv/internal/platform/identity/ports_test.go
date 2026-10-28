package identity_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity"
)

func accountID(id string) reqctx.AccountID { return reqctx.AccountID(id) }

func TestSelectPrefersTheRememberedTenant(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	ta := identity.NewTenantAccess(env.pool, clock.Fixed(accessNow))
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
	ta := identity.NewTenantAccess(env.pool, clock.Fixed(accessNow))
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
	ta := identity.NewTenantAccess(env.pool, clock.Fixed(accessNow))
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
	ta := identity.NewTenantAccess(env.pool, clock.Fixed(accessNow))
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
	ta := identity.NewTenantAccess(env.pool, clock.Fixed(later))
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
	ta := identity.NewTenantAccess(env.pool, clock.Fixed(accessNow))
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
		ta := identity.NewTenantAccess(env.pool, clock.Fixed(step.at))
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
			ta := identity.NewTenantAccess(env.pool, clock.Fixed(accessNow))
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
	ta := identity.NewTenantAccess(env.pool, clock.Fixed(accessNow))
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
	pool := identity.NewTestPool(f)
	ta := identity.NewTenantAccess(pool, clock.Fixed(accessNow))
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

func TestTenantDirectoryReportsTenantSlugAndStatus(t *testing.T) {
	env := newAccessEnv(t)
	var directory coreaccess.TenantDirectory = identity.NewTenantDirectory(env.pool)
	tenantID := env.tenant("acme", "suspended")

	ref, found, err := directory.Tenant(context.Background(), reqctx.TenantID(tenantID))

	if err != nil || !found || ref.Slug != "acme" || ref.Status != coreaccess.TenantSuspended {
		t.Fatalf("tenant = %+v found=%v err=%v", ref, found, err)
	}
}

func TestAccountProvisionerGrantsOperatorOnlyToListedEmails(t *testing.T) {
	env := newAccessEnv(t)
	_, provisioner := identity.NewAccounts(env.pool, clock.Fixed(accessNow), nil, []string{"ops@example.com"})

	if !provisioner.GrantsOperator("ops@example.com") || provisioner.GrantsOperator("other@example.com") {
		t.Fatal("provisioner did not honor the operator list")
	}
}
