package identity

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

// exampleEnvSettings are the identity lines of .deploy/local/.example.env,
// read from the file so the test follows the template.
func exampleEnvSettings(t *testing.T) map[string]string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", ".deploy", "local", ".example.env")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read example env: %v", err)
	}
	wanted := map[string]bool{
		"APP_SESSION_KEYS":             true,
		"APP_BOOTSTRAP_TENANT_SLUG":    true,
		"APP_BOOTSTRAP_ADMIN_EMAIL":    true,
		"APP_BOOTSTRAP_ADMIN_PASSWORD": true,
		"APP_KRATOS_PUBLIC_URL":        true,
		"APP_KRATOS_ADMIN_URL":         true,
	}
	settings := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found && wanted[key] {
			settings[key] = strings.Trim(value, `"`)
		}
	}
	if len(settings) != len(wanted) {
		t.Fatalf("example env holds %d of %d identity lines", len(settings), len(wanted))
	}
	return settings
}

func TestExampleEnvLinesPassValidationInProvide(t *testing.T) {
	settings := exampleEnvSettings(t)
	settings["APP_PUBLIC_BASE_URL"] = "http://localhost:10120"
	app, err := core.Build(context.Background(), core.Options{
		Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		Config: config.MapSource(settings),
		Clock:  clock.System{},
		Pool:   lazyPool(t),
	}, []core.Module{Module()})
	// The database is unreachable, so Build must get past configuration and
	// fail at the ping, never with config_invalid.
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

func TestBuildBootsAndReportsReady(t *testing.T) {
	handler := coretest.New(t, []core.Module{Module()}, coretest.Options{
		Database: true,
		Config:   map[string]string{"APP_SESSION_KEYS": randomSessionKeys(t)},
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("ready answered %d", rec.Code)
	}
}

func TestBuildFailsWithOneErrorNamingEveryBadKey(t *testing.T) {
	var logs bytes.Buffer
	secret := "tiny-pw"
	_, err := core.Build(context.Background(), core.Options{
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		Config: config.MapSource(map[string]string{
			"APP_PUBLIC_BASE_URL":          "http://localhost:10120",
			"APP_SESSION_KEYS":             randomSessionKeys(t),
			"APP_SESSION_TTL":              "99h",
			"APP_INVITATION_TTL":           "1s",
			"APP_KRATOS_ADMIN_URL":         "not a url",
			"APP_BOOTSTRAP_TENANT_SLUG":    "acme",
			"APP_BOOTSTRAP_ADMIN_PASSWORD": secret,
		}),
		Clock: clock.System{},
		Pool:  lazyPool(t),
	}, []core.Module{Module()})
	if err == nil {
		t.Fatal("Build accepted a bad configuration")
	}
	text := err.Error()
	keys := []string{
		"APP_SESSION_TTL", "APP_INVITATION_TTL", "APP_KRATOS_ADMIN_URL",
		"APP_BOOTSTRAP_ADMIN_EMAIL", "APP_BOOTSTRAP_ADMIN_PASSWORD",
	}
	for _, key := range keys {
		if !strings.Contains(text, key) {
			t.Errorf("error does not name %s: %s", key, text)
		}
	}
	for _, leaked := range []string{secret, "99h", "not a url"} {
		if strings.Contains(text, leaked) || strings.Contains(logs.String(), leaked) {
			t.Errorf("value %q leaked", leaked)
		}
	}
}

func TestMissingSessionKeysFailBuild(t *testing.T) {
	_, err := core.Build(context.Background(), core.Options{
		Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		Config: config.MapSource(map[string]string{"APP_PUBLIC_BASE_URL": "http://localhost:10120"}),
		Clock:  clock.System{},
		Pool:   lazyPool(t),
	}, []core.Module{Module()})
	if err == nil || !strings.Contains(err.Error(), "APP_SESSION_KEYS") {
		t.Fatalf("want a APP_SESSION_KEYS problem, got %v", err)
	}
}

func TestProvideRegistersTheCoreScopes(t *testing.T) {
	var registry *coreaccess.ScopeRegistry
	probe := core.Module{
		ID: "scope-probe",
		Mount: func(_ context.Context, h *core.Host) error {
			registry = h.Scopes()
			return nil
		},
	}
	coretest.New(t, []core.Module{Module(), probe}, coretest.Options{
		Database: true,
		Config:   map[string]string{"APP_SESSION_KEYS": randomSessionKeys(t)},
	})
	for _, name := range []string{"tenant:read", "api-keys:read"} {
		if !registry.Known(name) {
			t.Errorf("scope %s is not registered", name)
		}
	}
}

func TestModuleProvidesTheCorePorts(t *testing.T) {
	var host *core.Host
	probe := core.Module{
		ID: "port-probe",
		Mount: func(_ context.Context, h *core.Host) error {
			host = h
			return nil
		},
	}
	coretest.New(t, []core.Module{Module(), probe}, coretest.Options{
		Database: true,
		Config:   map[string]string{"APP_SESSION_KEYS": randomSessionKeys(t)},
	})
	ctx := context.Background()
	st := newStore(host.Pool(), host.Clock())
	tenant := mustTenant(t, st, "tenant-a")
	account := mustAccount(t, st, "member@example.com")
	if _, err := st.InsertMembership(ctx, tenant.ID, account.ID, "member"); err != nil {
		t.Fatalf("insert membership: %v", err)
	}

	if _, found, err := host.TenantDirectory().Tenant(ctx, reqctx.TenantID(tenant.ID)); err != nil || !found {
		t.Errorf("tenant directory found=%v err=%v, want the stored tenant", found, err)
	}
	if _, err := host.TenantAccess().Grant(ctx, reqctx.AccountID(account.ID), reqctx.TenantID(tenant.ID)); err != nil {
		t.Errorf("tenant access grant: %v, want the stored membership granted", err)
	}
	refs, err := host.AccountDirectory().ByEmail(ctx, "member@example.com")
	if err != nil || len(refs) != 1 {
		t.Errorf("account directory refs=%v err=%v, want the stored account", refs, err)
	}
	if host.AccountProvisioner().GrantsOperator("someone@example.com") {
		t.Error("account provisioner grants operator to an unlisted email, want identity's operator list")
	}
}

func TestTenantRouteIsMountedAndNeedsACredential(t *testing.T) {
	handler := coretest.New(t, []core.Module{Module()}, coretest.Options{
		Database: true,
		Config:   map[string]string{"APP_SESSION_KEYS": randomSessionKeys(t)},
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tenant", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/tenant answered %d, want 401", rec.Code)
	}
}

// buildWithKeys runs Build against an unreachable database with the given
// keyring text and public URL, and returns its error.
func buildWithKeys(t *testing.T, keys, publicURL string, extra map[string]string) error {
	t.Helper()
	settings := map[string]string{
		"APP_PUBLIC_BASE_URL": publicURL,
		"APP_SESSION_KEYS":    keys,
	}
	for key, value := range extra {
		settings[key] = value
	}
	app, err := core.Build(context.Background(), core.Options{
		Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		Config: config.MapSource(settings),
		Clock:  clock.System{},
		Pool:   lazyPool(t),
	}, []core.Module{Module()})
	if err == nil {
		_ = app.Close(context.Background())
	}
	return err
}

func TestBadSessionKeysJoinTheSingleBuildReport(t *testing.T) {
	leak := "dGhpcy1pcy1ub3QtMzItYnl0ZXM"
	err := buildWithKeys(t, "bad kid!:"+leak+",short:QUJD", "http://localhost:10120",
		map[string]string{"APP_SESSION_TTL": "99h"})
	if err == nil {
		t.Fatal("Build accepted bad session keys")
	}
	text := err.Error()
	for _, want := range []string{"APP_SESSION_KEYS", "kid must match", "seed must be 32 bytes", "APP_SESSION_TTL"} {
		if !strings.Contains(text, want) {
			t.Errorf("report does not mention %q: %s", want, text)
		}
	}
	if strings.Contains(text, leak) {
		t.Errorf("report leaks key material: %s", text)
	}
}

func TestPassphraseKeyIsRejectedUnderAnHTTPSPublicURL(t *testing.T) {
	keys := "local-1:passphrase:" + strings.Repeat("x", 30)
	err := buildWithKeys(t, keys, "https://app.example.com", nil)
	if err == nil || !strings.Contains(err.Error(), "APP_SESSION_KEYS") ||
		!strings.Contains(err.Error(), "passphrase entries are local only") {
		t.Fatalf("want the passphrase problem, got %v", err)
	}
}

func TestPassphraseKeyIsAcceptedUnderAnHTTPPublicURL(t *testing.T) {
	keys := "local-1:passphrase:" + strings.Repeat("x", 30)
	err := buildWithKeys(t, keys, "http://localhost:10120", nil)
	hasKeyProblem := err != nil && strings.Contains(err.Error(), "APP_SESSION_KEYS")
	if hasKeyProblem {
		t.Fatalf("want Build to accept the session keys, got %v", err)
	}
}

func TestSeedKeyIsAcceptedUnderAnHTTPSPublicURL(t *testing.T) {
	err := buildWithKeys(t, randomSessionKeys(t), "https://app.example.com", nil)
	hasKeyProblem := err != nil && strings.Contains(err.Error(), "APP_SESSION_KEYS")
	if hasKeyProblem {
		t.Fatalf("want Build to accept the session keys, got %v", err)
	}
}
