package access_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

func TestScopeRegistryRejectsBadNames(t *testing.T) {
	for _, name := range []string{"Tenant:read", "tenant", "tenant:", ":read", "1a:b", "a:b:c", "a b:c", "tenant:read\n", ""} {
		if err := access.NewScopeRegistry().Register(name, "x", access.ScopeTenant); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	if err := access.NewScopeRegistry().Register("api-keys:read", "x", access.ScopeTenant); err != nil {
		t.Fatal(err)
	}
}

func TestScopeRegistryRejectsInvalidLevels(t *testing.T) {
	for _, level := range []access.ScopeLevel{0, 3, 255} {
		r := access.NewScopeRegistry()
		if err := r.Register("tenant:read", "x", level); err == nil {
			t.Errorf("accepted level %d", level)
		}
		if r.Known("tenant:read") {
			t.Errorf("level %d left the scope registered", level)
		}
	}
	for level, want := range map[access.ScopeLevel]string{access.ScopeTenant: "tenant", access.ScopeAny: "any", 0: "", 3: ""} {
		if got := level.String(); got != want {
			t.Errorf("ScopeLevel(%d).String() = %q, want %q", level, got, want)
		}
	}
}

func TestScopeRegistryLevel(t *testing.T) {
	r := access.NewScopeRegistry()
	if err := r.Register("tenant:read", "x", access.ScopeTenant); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("platform:read", "x", access.ScopeAny); err != nil {
		t.Fatal(err)
	}
	if level, ok := r.Level("tenant:read"); !ok || level != access.ScopeTenant {
		t.Fatalf("Level(tenant:read) = %v, %v", level, ok)
	}
	if level, ok := r.Level("platform:read"); !ok || level != access.ScopeAny {
		t.Fatalf("Level(platform:read) = %v, %v", level, ok)
	}
	if level, ok := r.Level("unknown:scope"); ok || level != 0 {
		t.Fatalf("Level(unknown:scope) = %v, %v; want 0, false", level, ok)
	}
}

func TestCheckMint(t *testing.T) {
	r := access.NewScopeRegistry()
	if err := r.Register("tenant:read", "x", access.ScopeTenant); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("any:read", "x", access.ScopeAny); err != nil {
		t.Fatal(err)
	}
	tenant := reqctx.TenantID(tenantA)
	scopes := []string{"tenant:read", "any:read", "unknown:x"}

	var without api.Problems
	r.CheckMint(&without, "scopes", scopes, nil)
	err := without.Err()
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || len(apiErr.Issues) != 1 {
		t.Fatalf("tenantless mint: err = %v, want exactly one issue", err)
	}
	issue := apiErr.Issues[0]
	if issue.Field != "scopes[0]" || issue.Code != api.CodeFieldNotAllowed || issue.Message != "Requires a key bound to a tenant" {
		t.Fatalf("issue = %+v", issue)
	}

	var withTenant api.Problems
	r.CheckMint(&withTenant, "scopes", scopes, &tenant)
	if err := withTenant.Err(); err != nil {
		t.Fatalf("mint with a tenant: %v", err)
	}

	var empty api.Problems
	r.CheckMint(&empty, "scopes", nil, nil)
	if err := empty.Err(); err != nil {
		t.Fatalf("mint without scopes: %v", err)
	}
}

func TestScopeRegistryDuplicateAndFreeze(t *testing.T) {
	r := access.NewScopeRegistry()
	if err := r.Register("tenant:read", "x", access.ScopeTenant); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("tenant:read", "again", access.ScopeTenant); err == nil {
		t.Fatal("duplicate accepted")
	}
	r.Freeze()
	if err := r.Register("tenant:write", "x", access.ScopeTenant); err == nil {
		t.Fatal("registered after Freeze")
	}
	if r.Known("tenant:write") || !r.Known("tenant:read") {
		t.Fatal("Known is wrong")
	}
}

func TestScopeRegistryAllIsSortedAndCopied(t *testing.T) {
	r := access.NewScopeRegistry()
	for _, name := range []string{"tenant:read", "api-keys:read", "params:write"} {
		if err := r.Register(name, "d "+name, access.ScopeAny); err != nil {
			t.Fatal(err)
		}
	}
	all := r.All()
	want := []string{"api-keys:read", "params:write", "tenant:read"}
	for _, s := range all {
		if s.Level != access.ScopeAny {
			t.Fatalf("scope %s level = %v, want ScopeAny", s.Name, s.Level)
		}
	}
	if len(all) != len(want) {
		t.Fatalf("All returned %d scopes, want %d: %+v", len(all), len(want), all)
	}
	for i, s := range all {
		if s.Name != want[i] || s.Description != "d "+want[i] {
			t.Fatalf("all = %+v", all)
		}
	}
	all[0].Name = "mutated:scope"
	if r.Known("mutated:scope") || r.All()[0].Name != "api-keys:read" {
		t.Fatal("All leaks internal state")
	}
}

func TestDefaultsAreInert(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", "test_session=ok")
	if access.NoSessions.Present(req) || access.NoAPIKeys.Present(req) {
		t.Fatal("a default resolver reported a credential")
	}
	exists, err := access.NoTenants.TenantExists(context.Background(), reqctx.TenantID(tenantA))
	if err != nil || exists {
		t.Fatalf("exists %v err %v", exists, err)
	}
	ref, found, err := access.NoTenants.Tenant(context.Background(), reqctx.TenantID(tenantA))
	if err != nil || found || ref != (access.TenantRef{}) {
		t.Fatalf("Tenant = %+v, %v, %v; want zero, false, nil", ref, found, err)
	}
}

func TestNoTenantAccessFailsClosed(t *testing.T) {
	ctx := context.Background()
	account := reqctx.AccountID(tenantB)
	tenant := reqctx.TenantID(tenantA)

	selected, err := access.NoTenantAccess.Select(ctx, account)
	if err != nil || selected != nil {
		t.Fatalf("Select = %+v, %v; want nil, nil", selected, err)
	}
	for _, id := range []reqctx.TenantID{tenant, "", "not-a-uuid"} {
		grant, err := access.NoTenantAccess.Grant(ctx, account, id)
		var apiErr *api.Error
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden || apiErr.Code != api.CodeTenantAccessDenied {
			t.Fatalf("Grant(%q) error = %v, want 403 TENANT_ACCESS_DENIED", id, err)
		}
		if grant != (access.TenantGrant{}) {
			t.Fatalf("Grant(%q) returned %+v with a rejection", id, grant)
		}
	}
	err = access.NoTenantAccess.Remember(ctx, account, tenant)
	var apiErr *api.Error
	if !errors.Is(err, access.ErrNoTenancy) || errors.As(err, &apiErr) {
		t.Fatalf("Remember = %v; want the operational ErrNoTenancy", err)
	}
	list, err := access.NoTenantAccess.Memberships(ctx, account)
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("Memberships = %#v, %v; want empty non-nil", list, err)
	}
}

func TestNoResolversAnswer401OnProtectedRoutes(t *testing.T) {
	h := newHarness(t, access.NoSessions, access.NoAPIKeys)
	h.expect(t, h.do(sessionPolicy, http.MethodGet, nil), 401, api.CodeSessionInvalid)
	h = newHarness(t, access.NoSessions, access.NoAPIKeys)
	h.expect(t, h.do(keyPolicy, http.MethodGet, nil), 401, api.CodeAPIKeyInvalidOrRevoked)
}

func TestCheckTenantRef(t *testing.T) {
	tenant := reqctx.TenantID(tenantA)
	p := reqctx.Principal{TenantID: &tenant, TenantSlug: "tenant-a"}
	pass := []string{"", tenantA, "0B6F2A4E-7D1C-4C52-9A38-1F5E6D7C8B90", "tenant-a", "Tenant-A"}
	for _, ref := range pass {
		if err := access.CheckTenantRef(p, ref); err != nil {
			t.Errorf("%q: %v", ref, err)
		}
	}
	fail := []string{tenantB, "tenant-b", "tenant-a ", " tenant-a", "tenant-", "tenant-K"}
	for _, ref := range fail {
		err := access.CheckTenantRef(p, ref)
		apiErr, ok := err.(*api.Error)
		if !ok || apiErr.Status != 403 || apiErr.Code != "TENANT_MISMATCH" {
			t.Errorf("%q: %v", ref, err)
		}
	}
}

func TestCheckTenantRefKelvinSignDoesNotFold(t *testing.T) {
	tenant := reqctx.TenantID(tenantA)
	p := reqctx.Principal{TenantID: &tenant, TenantSlug: "k-shop"}
	if err := access.CheckTenantRef(p, "K-shop"); err == nil {
		t.Fatal("Kelvin sign matched k")
	}
}

func TestCheckTenantRefWithoutTenant(t *testing.T) {
	if err := access.CheckTenantRef(reqctx.Principal{}, ""); err != nil {
		t.Fatal(err)
	}
	if err := access.CheckTenantRef(reqctx.Principal{}, "tenant-a"); err == nil {
		t.Fatal("matched without a tenant")
	}
}

// foldASCII lowers only A-Z, an oracle independent of the implementation.
func foldASCII(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + ('a' - 'A')
		}
	}
	return string(out)
}

func FuzzCheckTenantRef(f *testing.F) {
	f.Add(tenantA)
	f.Add("")
	f.Add("tenant-a")
	tenant := reqctx.TenantID(tenantA)
	p := reqctx.Principal{TenantID: &tenant, TenantSlug: "tenant-a"}
	f.Fuzz(func(t *testing.T, ref string) {
		err := access.CheckTenantRef(p, ref)
		folded := foldASCII(ref)
		isKnown := ref == "" || folded == foldASCII(tenantA) || folded == "tenant-a"
		if isKnown && err != nil {
			t.Fatalf("%q rejected", ref)
		}
		if !isKnown && err == nil {
			t.Fatalf("foreign ref %q accepted", ref)
		}
		if err != nil {
			if apiErr, ok := err.(*api.Error); !ok || apiErr.Code != "TENANT_MISMATCH" {
				t.Fatalf("unexpected error %v", err)
			}
		}
	})
}

func FuzzScopeRegister(f *testing.F) {
	f.Add("tenant:read")
	f.Add("A:b")
	f.Fuzz(func(t *testing.T, name string) {
		r := access.NewScopeRegistry()
		err := r.Register(name, "x", access.ScopeTenant)
		if (err == nil) != r.Known(name) {
			t.Fatalf("Register err %v but Known %v for %q", err, r.Known(name), name)
		}
	})
}
