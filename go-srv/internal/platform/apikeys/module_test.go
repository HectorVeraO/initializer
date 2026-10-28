package apikeys

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/coretest"
)

func TestModuleProvidesTheAPIKeyResolver(t *testing.T) {
	var resolver coreaccess.APIKeyResolver
	probe := core.Module{
		ID: "port-probe",
		Mount: func(_ context.Context, h *core.Host) error {
			resolver = h.APIKeyResolver()
			return nil
		},
	}
	coretest.New(t, []core.Module{Module(), probe}, coretest.Options{Database: true})

	if resolver == nil || resolver == coreaccess.NoAPIKeys {
		t.Fatalf("host resolver = %v, want the api-keys resolver", resolver)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/api-keys", nil)
	req.Header.Set(apiKeyHeader, "ak_live_"+strings.Repeat("a", 16)+"_"+strings.Repeat("a", 52))
	if !resolver.Present(req) {
		t.Error("the resolver does not see the X-API-Key header")
	}
}

func TestProvideRegistersTheTenantLevelScope(t *testing.T) {
	var registry *coreaccess.ScopeRegistry
	probe := core.Module{
		ID: "scope-probe",
		Mount: func(_ context.Context, h *core.Host) error {
			registry = h.Scopes()
			return nil
		},
	}
	coretest.New(t, []core.Module{Module(), probe}, coretest.Options{Database: true})

	var found *coreaccess.Scope
	for _, scope := range registry.All() {
		if scope.Name == "api-keys:read" {
			found = &scope
		}
	}
	if found == nil {
		t.Fatal("scope api-keys:read is not registered")
	}
	if found.Level != coreaccess.ScopeTenant || found.Description != "List the tenant's API key metadata" {
		t.Fatalf("scope = %+v, want level tenant and the documented description", found)
	}
	if registry.Known("tenant:read") {
		t.Error("scope tenant:read is registered by api-keys, it is tenancy's")
	}
}
