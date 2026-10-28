package parameters_test

import (
	"net/http"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/parameters"
)

// newEnv installs no tenancy provider, so every session below is the
// tenantless session identity alone mints and the core No* defaults apply.

func TestTenantlessListReturnsGlobalValues(t *testing.T) {
	e := newEnv(t, nil)
	wantData[parameters.View](t, e.json("operator", http.MethodPost, "/api/v1/parameters", numberDefinition), http.StatusCreated)
	wantData[parameters.View](t, e.json("adminA", http.MethodPut, "/api/v1/parameters/export.max_rows/tenant-override", `{"value":400}`), http.StatusOK)

	for _, actor := range []string{"tenantless", "operator"} {
		page := wantData[api.Page[parameters.View]](t, e.json(actor, http.MethodGet, "/api/v1/parameters", ""), http.StatusOK)

		if len(page.Items) != 1 {
			t.Fatalf("%s: items = %+v, want the one defined parameter", actor, page.Items)
		}
		item := page.Items[0]
		if item.Source != "global" || string(item.EffectiveValue) != "50" {
			t.Errorf("%s: item = %+v, want the global value 50 and no tenant override", actor, item)
		}
	}
}

func TestTenantlessSessionsCannotWriteTenantOverrides(t *testing.T) {
	e := newEnv(t, nil)
	wantData[parameters.View](t, e.json("operator", http.MethodPost, "/api/v1/parameters", numberDefinition), http.StatusCreated)
	cases := []struct {
		name   string
		actor  string
		method string
		body   string
	}{
		{"tenantless put", "tenantless", http.MethodPut, `{"value":5}`},
		{"tenantless delete", "tenantless", http.MethodDelete, ""},
		{"operator put", "operator", http.MethodPut, `{"value":5}`},
		{"operator delete", "operator", http.MethodDelete, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := e.json(tc.actor, tc.method, "/api/v1/parameters/export.max_rows/tenant-override", tc.body)

			wantError(t, rec, http.StatusForbidden, "ACTIVE_TENANT_REQUIRED")
		})
	}
	page := wantData[api.Page[parameters.View]](t, e.json("tenantless", http.MethodGet, "/api/v1/parameters", ""), http.StatusOK)
	if len(page.Items) != 1 || page.Items[0].Source != "global" || string(page.Items[0].EffectiveValue) != "50" {
		t.Errorf("items = %+v, want the value unchanged", page.Items)
	}
}
