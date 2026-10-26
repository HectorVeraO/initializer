package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/auth"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
)

func TestScopeRegistryRejectsBadNames(t *testing.T) {
	for _, name := range []string{"Tenant:read", "tenant", "tenant:", ":read", "1a:b", "a:b:c", "a b:c", "tenant:read\n", ""} {
		if err := auth.NewScopeRegistry().Register(name, "x"); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	if err := auth.NewScopeRegistry().Register("api-keys:read", "x"); err != nil {
		t.Fatal(err)
	}
}

func TestScopeRegistryDuplicateAndFreeze(t *testing.T) {
	r := auth.NewScopeRegistry()
	if err := r.Register("tenant:read", "x"); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("tenant:read", "again"); err == nil {
		t.Fatal("duplicate accepted")
	}
	r.Freeze()
	if err := r.Register("tenant:write", "x"); err == nil {
		t.Fatal("registered after Freeze")
	}
	if r.Known("tenant:write") || !r.Known("tenant:read") {
		t.Fatal("Known is wrong")
	}
}

func TestScopeRegistryAllIsSortedAndCopied(t *testing.T) {
	r := auth.NewScopeRegistry()
	for _, name := range []string{"tenant:read", "api-keys:read", "params:write"} {
		if err := r.Register(name, "d "+name); err != nil {
			t.Fatal(err)
		}
	}
	all := r.All()
	want := []string{"api-keys:read", "params:write", "tenant:read"}
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
	if auth.NoSessions.Present(req) || auth.NoAPIKeys.Present(req) {
		t.Fatal("a default resolver reported a credential")
	}
	exists, err := auth.NoTenants.TenantExists(context.Background(), reqctx.TenantID(tenantA))
	if err != nil || exists {
		t.Fatalf("exists %v err %v", exists, err)
	}
}

func TestNoResolversAnswer401OnProtectedRoutes(t *testing.T) {
	h := newHarness(t, auth.NoSessions, auth.NoAPIKeys)
	h.expect(t, h.do(sessionPolicy, http.MethodGet, nil), 401, api.CodeSessionInvalid)
	h = newHarness(t, auth.NoSessions, auth.NoAPIKeys)
	h.expect(t, h.do(keyPolicy, http.MethodGet, nil), 401, api.CodeAPIKeyInvalidOrRevoked)
}

func TestCheckTenantRef(t *testing.T) {
	tenant := reqctx.TenantID(tenantA)
	p := reqctx.Principal{TenantID: &tenant, TenantSlug: "tenant-a"}
	pass := []string{"", tenantA, "0B6F2A4E-7D1C-4C52-9A38-1F5E6D7C8B90", "tenant-a", "Tenant-A"}
	for _, ref := range pass {
		if err := auth.CheckTenantRef(p, ref); err != nil {
			t.Errorf("%q: %v", ref, err)
		}
	}
	fail := []string{tenantB, "tenant-b", "tenant-a ", " tenant-a", "tenant-", "tenant-K"}
	for _, ref := range fail {
		err := auth.CheckTenantRef(p, ref)
		apiErr, ok := err.(*api.Error)
		if !ok || apiErr.Status != 403 || apiErr.Code != "TENANT_MISMATCH" {
			t.Errorf("%q: %v", ref, err)
		}
	}
}

func TestCheckTenantRefKelvinSignDoesNotFold(t *testing.T) {
	tenant := reqctx.TenantID(tenantA)
	p := reqctx.Principal{TenantID: &tenant, TenantSlug: "k-shop"}
	if err := auth.CheckTenantRef(p, "K-shop"); err == nil {
		t.Fatal("Kelvin sign matched k")
	}
}

func TestCheckTenantRefWithoutTenant(t *testing.T) {
	if err := auth.CheckTenantRef(reqctx.Principal{}, ""); err != nil {
		t.Fatal(err)
	}
	if err := auth.CheckTenantRef(reqctx.Principal{}, "tenant-a"); err == nil {
		t.Fatal("matched without a tenant")
	}
}

func FuzzCheckTenantRef(f *testing.F) {
	f.Add(tenantA)
	f.Add("")
	f.Add("tenant-a")
	tenant := reqctx.TenantID(tenantA)
	p := reqctx.Principal{TenantID: &tenant, TenantSlug: "tenant-a"}
	f.Fuzz(func(t *testing.T, ref string) {
		err := auth.CheckTenantRef(p, ref)
		isKnown := ref == "" || ref == tenantA || ref == "tenant-a"
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
		r := auth.NewScopeRegistry()
		err := r.Register(name, "x")
		if (err == nil) != r.Known(name) {
			t.Fatalf("Register err %v but Known %v for %q", err, r.Known(name), name)
		}
	})
}
