package reqctx_test

import (
	"context"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/internal/ctxkey"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
)

func TestParseTenantIDCanonicalises(t *testing.T) {
	got, ok := reqctx.ParseTenantID("0A1B2C3D-0000-4000-8000-ABCDEF012345")
	if !ok || got != "0a1b2c3d-0000-4000-8000-abcdef012345" {
		t.Fatalf("got %q %v", got, ok)
	}
	for _, bad := range []string{"", "acme", "0a1b2c3d-0000-4000-8000-abcdef01234"} {
		if _, ok := reqctx.ParseTenantID(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestRoleOrdering(t *testing.T) {
	roles := []reqctx.Role{reqctx.RoleAdmin, reqctx.RoleMember, reqctx.RoleViewer}
	for i, r := range roles {
		for j, min := range roles {
			if got, want := r.AtLeast(min), i <= j; got != want {
				t.Errorf("%s.AtLeast(%s) = %v", r, min, got)
			}
		}
	}
}

func TestInvalidRolesNeverSatisfyAnything(t *testing.T) {
	invalid := reqctx.Role("")
	root := reqctx.Role("root")
	if invalid.Valid() || root.Valid() || !reqctx.RoleViewer.Valid() {
		t.Fatal("Valid")
	}
	if invalid.AtLeast(reqctx.RoleViewer) || root.AtLeast(reqctx.RoleViewer) ||
		reqctx.RoleAdmin.AtLeast(root) || reqctx.RoleAdmin.AtLeast(invalid) || invalid.AtLeast(invalid) {
		t.Fatal("invalid role satisfied a comparison")
	}
}

func TestPrincipalAbsentOutsideGuard(t *testing.T) {
	ctx := context.Background()
	if _, ok := reqctx.PrincipalFrom(ctx); ok {
		t.Fatal("principal on bare context")
	}
	if _, ok := reqctx.TenantFrom(ctx); ok {
		t.Fatal("tenant on bare context")
	}
	forged := context.WithValue(ctx, "principal", reqctx.Principal{})
	if _, ok := reqctx.PrincipalFrom(forged); ok {
		t.Fatal("string key forged a principal")
	}
}

func TestPrincipalAndTenantAccessors(t *testing.T) {
	tenant := reqctx.TenantID("0a1b2c3d-0000-4000-8000-abcdef012345")
	want := reqctx.Principal{
		Kind: reqctx.ActorAccount, ActorID: "acct-1", TenantID: &tenant, TenantSlug: "acme", Role: reqctx.RoleMember,
	}
	ctx := context.WithValue(context.Background(), ctxkey.Principal, want)

	got, ok := reqctx.PrincipalFrom(ctx)
	if !ok || got.ActorID != "acct-1" || got.Role != reqctx.RoleMember || got.TenantSlug != "acme" {
		t.Fatalf("principal = %+v %v", got, ok)
	}
	if id, ok := reqctx.TenantFrom(ctx); !ok || id != tenant {
		t.Fatalf("tenant = %q %v", id, ok)
	}
}

func TestTenantFromWithoutActiveTenant(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxkey.Principal, reqctx.Principal{Kind: reqctx.ActorAPIKey, ActorID: "apikey:abcd"})
	if _, ok := reqctx.PrincipalFrom(ctx); !ok {
		t.Fatal("principal missing")
	}
	if _, ok := reqctx.TenantFrom(ctx); ok {
		t.Fatal("tenant reported for a principal without one")
	}
}
