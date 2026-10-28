package identity

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/accounts"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity/kratos"
)

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

func TestTenantDirectoryTenant(t *testing.T) {
	e := newTenEnv(t)
	dir := &tenantDirectory{pool: e.pool}
	tenant := e.tenant("tenant-a")
	e.exec("UPDATE ten_tenants SET status = 'suspended' WHERE id = $1::uuid", tenant.ID)

	got, found, err := dir.Tenant(context.Background(), reqctx.TenantID(tenant.ID))
	if err != nil || !found {
		t.Fatalf("tenant = %+v found=%v err=%v", got, found, err)
	}
	if string(got.ID) != tenant.ID || got.Slug != "tenant-a" || got.Status != coreaccess.TenantSuspended {
		t.Fatalf("tenant ref = %+v", got)
	}
	for name, id := range map[string]string{
		"unknown id": "99999999-9999-4999-8999-999999999999",
		"non-uuid":   "not-a-uuid",
		"empty":      "",
	} {
		t.Run(name, func(t *testing.T) {
			ref, found, err := dir.Tenant(context.Background(), reqctx.TenantID(id))

			if err != nil || found || ref != (coreaccess.TenantRef{}) {
				t.Fatalf("tenant = %+v found=%v err=%v, want not found", ref, found, err)
			}
		})
	}
}

func newProvisioner(t *testing.T, e *tenEnv, operatorEmails ...string) *accountDirectory {
	t.Helper()
	adminURL, err := url.Parse(e.kratos.server.URL)
	if err != nil {
		t.Fatalf("parse fake url: %v", err)
	}
	return newAccountDirectory(e.pool, e.clock, kratos.NewAdmin(adminURL), operatorEmails)
}

func TestAccountProvision(t *testing.T) {
	ctx := context.Background()

	t.Run("listed email becomes platform operator", func(t *testing.T) {
		e := newTenEnv(t)
		dir := newProvisioner(t, e, janeEmail)

		ref, err := dir.Provision(ctx, accounts.NewAccount{Email: janeEmail, Password: randomPassword(t), FullName: "  Jane Doe "})

		if err != nil {
			t.Fatalf("provision: %v", err)
		}
		if !ref.PlatformOperator || ref.Email != janeEmail {
			t.Fatalf("ref = %+v, want a platform operator", ref)
		}
		if e.kratos.identities[janeEmail] == "" {
			t.Fatal("no Kratos identity was created")
		}
	})

	t.Run("unlisted email is not an operator", func(t *testing.T) {
		e := newTenEnv(t)
		dir := newProvisioner(t, e, "ops@example.com")

		ref, err := dir.Provision(ctx, accounts.NewAccount{Email: janeEmail, Password: randomPassword(t), FullName: "Jane Doe"})

		if err != nil {
			t.Fatalf("provision: %v", err)
		}
		if ref.PlatformOperator {
			t.Fatalf("ref = %+v, want no operator flag", ref)
		}
	})

	t.Run("existing identity is a conflict and creates nothing", func(t *testing.T) {
		e := newTenEnv(t)
		dir := newProvisioner(t, e, janeEmail)
		e.kratos.addIdentity(janeEmail)

		_, err := dir.Provision(ctx, accounts.NewAccount{Email: janeEmail, Password: randomPassword(t), FullName: "Jane Doe"})

		requireAPIError(t, err, http.StatusConflict, accounts.CodeIdentityAlreadyExists)
		if n := e.count("SELECT count(*) FROM iam_accounts WHERE email = $1", janeEmail); n != 0 {
			t.Fatalf("accounts = %d, want 0", n)
		}
	})

	t.Run("create conflict after a clear lookup is a conflict", func(t *testing.T) {
		e := newTenEnv(t)
		dir := newProvisioner(t, e)
		e.kratos.addIdentity(janeEmail)
		e.kratos.hideFirstFind = true

		_, err := dir.Provision(ctx, accounts.NewAccount{Email: janeEmail, Password: randomPassword(t), FullName: "Jane Doe"})

		requireAPIError(t, err, http.StatusConflict, accounts.CodeIdentityAlreadyExists)
		if n := e.count("SELECT count(*) FROM iam_accounts WHERE email = $1", janeEmail); n != 0 {
			t.Fatalf("accounts = %d, want 0", n)
		}
	})

	t.Run("account insert conflict is a conflict and deletes the new identity", func(t *testing.T) {
		e := newTenEnv(t)
		dir := newProvisioner(t, e)
		taken := e.account("taken@example.com")
		e.kratos.nextID = taken.KratosIdentityID

		_, err := dir.Provision(ctx, accounts.NewAccount{Email: janeEmail, Password: randomPassword(t), FullName: "Jane Doe"})

		requireAPIError(t, err, http.StatusConflict, accounts.CodeIdentityAlreadyExists)
		if e.kratos.identities[janeEmail] != "" {
			t.Fatal("the Kratos identity was left without an account")
		}
		retry, err := dir.Provision(ctx, accounts.NewAccount{Email: janeEmail, Password: randomPassword(t), FullName: "Jane Doe"})
		if err != nil || retry.Email != janeEmail {
			t.Fatalf("retry = %+v err=%v, want the account created", retry, err)
		}
	})

	t.Run("rejected password is a 422 password issue", func(t *testing.T) {
		e := newTenEnv(t)
		dir := newProvisioner(t, e)
		e.kratos.rejectStatus = http.StatusBadRequest

		_, err := dir.Provision(ctx, accounts.NewAccount{Email: janeEmail, Password: "weak", FullName: "Jane Doe"})

		apiErr := requireAPIError(t, err, http.StatusUnprocessableEntity, api.CodeValidationFailed)
		if len(apiErr.Issues) != 1 || apiErr.Issues[0].Field != "password" || apiErr.Issues[0].Code != accounts.CodePasswordRejected {
			t.Fatalf("issues = %+v", apiErr.Issues)
		}
	})

	t.Run("unreachable Kratos is a 503 and creates nothing", func(t *testing.T) {
		e := newTenEnv(t)
		dir := newProvisioner(t, e)
		e.kratos.rejectStatus = http.StatusServiceUnavailable

		_, err := dir.Provision(ctx, accounts.NewAccount{Email: janeEmail, Password: randomPassword(t), FullName: "Jane Doe"})

		requireAPIError(t, err, http.StatusServiceUnavailable, CodeIdentityProviderUnavailable)
		if n := e.count("SELECT count(*) FROM iam_accounts WHERE email = $1", janeEmail); n != 0 {
			t.Fatalf("accounts = %d, want 0", n)
		}
	})

	missing := map[string]accounts.NewAccount{
		"email":     {Email: "", Password: "placeholder-secret", FullName: "Jane Doe"},
		"password":  {Email: janeEmail, Password: "", FullName: "Jane Doe"},
		"full name": {Email: janeEmail, Password: "placeholder-secret", FullName: "   "},
	}
	for name, input := range missing {
		t.Run("missing "+name+" fails before calling Kratos", func(t *testing.T) {
			e := newTenEnv(t)
			dir := newProvisioner(t, e)

			_, err := dir.Provision(ctx, input)

			if err == nil {
				t.Fatal("provision succeeded with a missing field")
			}
			var apiErr *api.Error
			if errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want an operational error, not an API rejection", err)
			}
			if len(e.kratos.received()) != 0 {
				t.Fatal("Kratos was called for invalid input")
			}
			if n := e.count("SELECT count(*) FROM iam_accounts"); n != 0 {
				t.Fatalf("accounts = %d, want 0", n)
			}
		})
	}
}

func TestAccountDirectoryGet(t *testing.T) {
	ctx := context.Background()
	e := newTenEnv(t)
	dir := newProvisioner(t, e)
	first := e.account("first@example.com")
	second := e.account("second@example.com")
	e.exec("UPDATE iam_accounts SET status = 'locked' WHERE id = $1::uuid", second.ID)

	got, err := dir.Get(ctx, []reqctx.AccountID{
		reqctx.AccountID(first.ID), reqctx.AccountID(second.ID), reqctx.AccountID(first.ID),
		"99999999-9999-4999-8999-999999999999", "not-a-uuid", "",
	})

	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d accounts, want 2: %+v", len(got), got)
	}
	if ref := got[reqctx.AccountID(first.ID)]; ref.Email != "first@example.com" || ref.Status != accounts.StatusActive {
		t.Fatalf("first = %+v", ref)
	}
	if ref := got[reqctx.AccountID(second.ID)]; ref.Status != accounts.StatusLocked {
		t.Fatalf("second = %+v, want locked", ref)
	}

	t.Run("empty ids return an empty non-nil map", func(t *testing.T) {
		for name, ids := range map[string][]reqctx.AccountID{"nil": nil, "empty": {}, "only invalid": {"not-a-uuid"}} {
			got, err := dir.Get(ctx, ids)
			if err != nil || got == nil || len(got) != 0 {
				t.Fatalf("%s: got %v err=%v, want an empty non-nil map", name, got, err)
			}
		}
	})
}

func TestAccountDirectoryByEmail(t *testing.T) {
	ctx := context.Background()
	e := newTenEnv(t)
	dir := newProvisioner(t, e)
	older := e.account("shared@example.com")
	newer := e.account("shared@example.com")
	e.exec("UPDATE iam_accounts SET created_at = created_at + interval '1 hour' WHERE id = $1::uuid", newer.ID)
	e.account("other@example.com")

	t.Run("normalizes the email and lists oldest first", func(t *testing.T) {
		got, err := dir.ByEmail(ctx, "  Shared@example.com ")

		if err != nil {
			t.Fatalf("by email: %v", err)
		}
		if len(got) != 2 || string(got[0].ID) != older.ID || string(got[1].ID) != newer.ID {
			t.Fatalf("got %+v, want [%s %s]", got, older.ID, newer.ID)
		}
	})

	t.Run("no match is an empty non-nil list", func(t *testing.T) {
		got, err := dir.ByEmail(ctx, "nobody@example.com")

		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("got %v err=%v, want an empty non-nil list", got, err)
		}
	})
}

func TestAccountDirectoryGrantsOperator(t *testing.T) {
	e := newTenEnv(t)
	dir := newProvisioner(t, e, "ops@example.com")

	if !dir.GrantsOperator("ops@example.com") || !dir.GrantsOperator("OPS@example.com") {
		t.Fatal("a listed email must grant operator, case-insensitively")
	}
	if dir.GrantsOperator("other@example.com") || dir.GrantsOperator("") {
		t.Fatal("an unlisted or empty email must not grant operator")
	}
}

func TestTenantAccessRemember(t *testing.T) {
	ctx := context.Background()
	e := newTenEnv(t)
	ta := newTenantAccess(e.pool, e.clock)
	tenant := e.tenant("tenant-a")
	account := e.account("member@example.com")
	e.member(tenant.ID, account.ID, "member", statusActive)

	err := ta.Remember(ctx, reqctx.AccountID(account.ID), reqctx.TenantID(tenant.ID))

	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	var remembered string
	err = e.pool.QueryRow(ctx, "SELECT last_active_tenant_id::text FROM iam_accounts WHERE id = $1::uuid", account.ID).Scan(&remembered)
	if err != nil {
		t.Fatalf("read last active tenant: %v", err)
	}
	if remembered != tenant.ID {
		t.Fatalf("last active tenant = %q, want %q", remembered, tenant.ID)
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
