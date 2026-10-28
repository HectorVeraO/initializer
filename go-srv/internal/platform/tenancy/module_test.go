package tenancy

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/config"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/coretest"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

// exampleEnvSettings are the tenancy lines of .deploy/local/.example.env,
// read from the file so the test follows the template.
func exampleEnvSettings(t *testing.T) map[string]string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", ".deploy", "local", ".example.env")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read example env: %v", err)
	}
	settings := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		isTenancyKey := key == "APP_BOOTSTRAP_TENANT_SLUG" || key == "APP_BOOTSTRAP_ADMIN_EMAIL"
		if found && isTenancyKey {
			settings[key] = strings.Trim(value, `"`)
		}
	}
	if len(settings) != 2 {
		t.Fatalf("example env holds %d of 2 tenancy lines", len(settings))
	}
	return settings
}

func TestExampleEnvLinesPassValidationInProvide(t *testing.T) {
	settings := exampleEnvSettings(t)
	settings["APP_PUBLIC_BASE_URL"] = publicBase
	app, err := core.Build(context.Background(), core.Options{
		Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		Config: config.MapSource(settings),
		Clock:  clock.System{},
		Pool:   lazyPool(t),
	}, []core.Module{Module()})
	// The database is unreachable, so Build must get past configuration and
	// fail at the ping, never with an invalid configuration.
	if err == nil {
		_ = app.Close(context.Background())
		t.Fatal("expected the ping to fail")
	}
	if strings.Contains(err.Error(), "invalid configuration") {
		t.Fatalf("example env rejected: %v", err)
	}
	if !strings.Contains(err.Error(), "db: ") {
		t.Fatalf("want a database ping failure, got %v", err)
	}
}

func TestProvideRegistersTheTenantReadScopeAtTenantLevel(t *testing.T) {
	var registry *coreaccess.ScopeRegistry
	probe := core.Module{
		ID: "scope-probe",
		Mount: func(_ context.Context, h *core.Host) error {
			registry = h.Scopes()
			return nil
		},
	}
	coretest.New(t, []core.Module{Module(), probe}, coretest.Options{Database: true})

	if !registry.Known("tenant:read") {
		t.Fatal("scope tenant:read is not registered")
	}
	level, _ := registry.Level("tenant:read")
	if level != coreaccess.ScopeTenant {
		t.Errorf("tenant:read level = %v, want tenant", level)
	}
}

func TestModuleProvidesTheTenantPorts(t *testing.T) {
	var host *core.Host
	probe := core.Module{
		ID: "port-probe",
		Mount: func(_ context.Context, h *core.Host) error {
			host = h
			return nil
		},
	}
	coretest.New(t, []core.Module{Module(), probe}, coretest.Options{Database: true})
	ctx := context.Background()
	var tenantID, accountID string
	err := host.Pool().QueryRow(ctx,
		`INSERT INTO ten_tenants (slug, name, created_at, updated_at) VALUES ('tenant-a', 'Tenant A', now(), now()) RETURNING id::text`).Scan(&tenantID)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	err = host.Pool().QueryRow(ctx,
		`INSERT INTO ten_memberships (tenant_id, account_id, role, created_at, updated_at)
VALUES ($1::uuid, gen_random_uuid(), 'member', now(), now()) RETURNING account_id::text`, tenantID).Scan(&accountID)
	if err != nil {
		t.Fatalf("seed membership: %v", err)
	}
	tenant := struct{ ID string }{ID: tenantID}
	account := accountID

	if ref, found, err := host.TenantDirectory().Tenant(ctx, reqctx.TenantID(tenant.ID)); err != nil || !found || ref.Slug != "tenant-a" {
		t.Errorf("tenant directory ref=%+v found=%v err=%v, want the stored tenant", ref, found, err)
	}
	grant, err := host.TenantAccess().Grant(ctx, reqctx.AccountID(account), reqctx.TenantID(tenant.ID))
	if err != nil || grant.Role != reqctx.RoleMember {
		t.Errorf("tenant access grant = %+v err=%v, want the stored membership granted", grant, err)
	}
}

func TestTenantRouteIsMountedAndNeedsACredential(t *testing.T) {
	handler := coretest.New(t, []core.Module{Module()}, coretest.Options{Database: true})
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/tenant"},
		{http.MethodGet, "/api/v1/tenants"},
		{http.MethodPost, "/api/v1/tenants"},
		{http.MethodGet, "/api/v1/memberships"},
		{http.MethodGet, "/api/v1/invitations"},
		{http.MethodPost, "/api/v1/invitations/accept"},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(route.method, route.path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s answered %d, want 401", route.method, route.path, rec.Code)
		}
	}
}

func TestAcceptNewIsPublic(t *testing.T) {
	handler := coretest.New(t, []core.Module{Module()}, coretest.Options{Database: true})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/invitations/accept-new", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("accept-new without a credential answered %d, want 422 from body validation", rec.Code)
	}
}
