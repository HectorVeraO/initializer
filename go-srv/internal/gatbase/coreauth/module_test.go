package coreauth

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

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/auth"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/config"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/coretest"
)

// exampleEnvSettings are the core-auth lines of .deploy/local/.example.env,
// read from the file so the test follows the template.
func exampleEnvSettings(t *testing.T) map[string]string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", ".deploy", "local", ".example.env")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read example env: %v", err)
	}
	wanted := map[string]bool{
		"GATBASE_SESSION_KEYS":             true,
		"GATBASE_BOOTSTRAP_TENANT_SLUG":    true,
		"GATBASE_BOOTSTRAP_ADMIN_EMAIL":    true,
		"GATBASE_BOOTSTRAP_ADMIN_PASSWORD": true,
		"GATBASE_KRATOS_PUBLIC_URL":        true,
		"GATBASE_KRATOS_ADMIN_URL":         true,
	}
	settings := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found && wanted[key] {
			settings[key] = strings.Trim(value, `"`)
		}
	}
	if len(settings) != len(wanted) {
		t.Fatalf("example env holds %d of %d core-auth lines", len(settings), len(wanted))
	}
	return settings
}

func TestExampleEnvLinesPassValidationInProvide(t *testing.T) {
	settings := exampleEnvSettings(t)
	settings["GATBASE_PUBLIC_BASE_URL"] = "http://localhost:10120"
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
}

func TestBuildBootsAndReportsReady(t *testing.T) {
	handler := coretest.New(t, []core.Module{Module()}, coretest.Options{
		Database: true,
		Config:   map[string]string{"GATBASE_SESSION_KEYS": randomSessionKeys(t)},
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
			"GATBASE_PUBLIC_BASE_URL":          "http://localhost:10120",
			"GATBASE_SESSION_KEYS":             randomSessionKeys(t),
			"GATBASE_SESSION_TTL":              "99h",
			"GATBASE_INVITATION_TTL":           "1s",
			"GATBASE_KRATOS_ADMIN_URL":         "not a url",
			"GATBASE_BOOTSTRAP_TENANT_SLUG":    "acme",
			"GATBASE_BOOTSTRAP_ADMIN_PASSWORD": secret,
		}),
		Clock: clock.System{},
		Pool:  lazyPool(t),
	}, []core.Module{Module()})
	if err == nil {
		t.Fatal("Build accepted a bad configuration")
	}
	text := err.Error()
	keys := []string{
		"GATBASE_SESSION_TTL", "GATBASE_INVITATION_TTL", "GATBASE_KRATOS_ADMIN_URL",
		"GATBASE_BOOTSTRAP_ADMIN_EMAIL", "GATBASE_BOOTSTRAP_ADMIN_PASSWORD",
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
		Config: config.MapSource(map[string]string{"GATBASE_PUBLIC_BASE_URL": "http://localhost:10120"}),
		Clock:  clock.System{},
		Pool:   lazyPool(t),
	}, []core.Module{Module()})
	if err == nil || !strings.Contains(err.Error(), "GATBASE_SESSION_KEYS") {
		t.Fatalf("want a GATBASE_SESSION_KEYS problem, got %v", err)
	}
}

func TestProvideRegistersTheCoreScopes(t *testing.T) {
	var registry *auth.ScopeRegistry
	probe := core.Module{
		ID: "scope-probe",
		Mount: func(_ context.Context, h *core.Host) error {
			registry = h.Scopes()
			return nil
		},
	}
	coretest.New(t, []core.Module{Module(), probe}, coretest.Options{
		Database: true,
		Config:   map[string]string{"GATBASE_SESSION_KEYS": randomSessionKeys(t)},
	})
	for _, name := range []string{"tenant:read", "api-keys:read"} {
		if !registry.Known(name) {
			t.Errorf("scope %s is not registered", name)
		}
	}
}

func TestModuleMountsNoRoutes(t *testing.T) {
	handler := coretest.New(t, []core.Module{Module()}, coretest.Options{
		Database: true,
		Config:   map[string]string{"GATBASE_SESSION_KEYS": randomSessionKeys(t)},
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tenant", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/v1/tenant answered %d, want 404", rec.Code)
	}
}
