package core_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/config"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/health"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/internal/testpg"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

const publicBaseURL = "http://localhost:10120"

const tenantUUID = "11111111-1111-4111-8111-111111111111"

// logBuffer is a goroutine-safe sink for the JSON log lines of a test.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logBuffer) records(t *testing.T) []map[string]any {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(l.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		out = append(out, record)
	}
	return out
}

func (l *logBuffer) find(t *testing.T, msg string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, record := range l.records(t) {
		if record["msg"] == msg {
			out = append(out, record)
		}
	}
	return out
}

func (l *logBuffer) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func newLogger() (*slog.Logger, *logBuffer) {
	sink := &logBuffer{}
	return slog.New(slog.NewJSONHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug})), sink
}

func baseConfig() map[string]string {
	return map[string]string{"APP_PUBLIC_BASE_URL": publicBaseURL}
}

// startApp builds modules over a private database and marks the app serving.
func startApp(t *testing.T, opts core.Options, modules ...core.Module) (*core.App, *logBuffer) {
	t.Helper()
	database := testpg.New(t, "core")
	logger, sink := newLogger()
	opts.Logger = logger
	opts.Pool = database.Pool(t, 4)
	if opts.Config == nil {
		opts.Config = config.MapSource(baseConfig())
	}
	app, err := core.Build(context.Background(), opts, modules)
	if err != nil {
		t.Fatalf("Build: %v\nlogs: %s", err, sink.text())
	}
	t.Cleanup(func() { _ = app.Close(context.Background()) })
	app.Readiness().SetServing(true)
	return app, sink
}

func do(app *core.App, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) api.Envelope[any] {
	t.Helper()
	var envelope api.Envelope[any]
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("body is not an envelope: %q", rec.Body.String())
	}
	return envelope
}

func expectError(t *testing.T, rec *httptest.ResponseRecorder, status int, code api.Code) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, status, rec.Body.String())
	}
	envelope := decode(t, rec)
	if envelope.Error == nil || envelope.Error.Code != code {
		t.Fatalf("error = %+v, want code %s", envelope.Error, code)
	}
	traceID := rec.Header().Get("X-Trace-Id")
	if traceID == "" || traceID != envelope.Meta.TraceID {
		t.Fatalf("X-Trace-Id %q must equal meta.traceId %q", traceID, envelope.Meta.TraceID)
	}
}

func okHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	})
}

func mounting(id string, mount func(h *core.Host)) core.Module {
	return core.Module{ID: id, Mount: func(_ context.Context, h *core.Host) error {
		mount(h)
		return nil
	}}
}

func TestBuildRejectsBadModuleListsWithEveryProblem(t *testing.T) {
	logger, sink := newLogger()
	cases := []struct {
		name string
		ids  []string
		want int
	}{
		{"uppercase", []string{"Core"}, 1},
		{"too short", []string{"a"}, 1},
		{"32 characters", []string{strings.Repeat("a", 32)}, 1},
		{"leading digit", []string{"1ab"}, 1},
		{"duplicate", []string{"abc", "abc"}, 1},
		{"several at once", []string{"Core", "a", "dup", "dup"}, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var modules []core.Module
			for _, id := range tc.ids {
				modules = append(modules, core.Module{ID: id})
			}
			_, err := core.Build(context.Background(), core.Options{Logger: logger, Config: config.MapSource(baseConfig())}, modules)
			if err == nil {
				t.Fatal("Build accepted the list")
			}
			if got := strings.Count(err.Error(), "core: module"); got != tc.want {
				t.Fatalf("reported %d problems, want %d: %v", got, tc.want, err)
			}
		})
	}
	if len(sink.find(t, "server_failed")) != len(cases) {
		t.Fatal("each failed Build must log server_failed once")
	}
}

func TestBuildAcceptsBoundaryModuleIDs(t *testing.T) {
	for _, id := range []string{"ab", strings.Repeat("a", 31), "identity", "feature-flags"} {
		modules := []core.Module{{ID: id}}
		logger, _ := newLogger()
		database := testpg.New(t, "coreid")
		app, err := core.Build(context.Background(), core.Options{Logger: logger, Pool: database.Pool(t, 4), Config: config.MapSource(baseConfig())}, modules)
		if err != nil {
			t.Fatalf("id %q rejected: %v", id, err)
		}
		_ = app.Close(context.Background())
	}
}

func TestBuildReportsEveryConfigProblemOnceWithoutValues(t *testing.T) {
	logger, sink := newLogger()
	source := config.MapSource(map[string]string{
		"APP_PUBLIC_BASE_URL":  "ftp://leak-url-value.example.com",
		"APP_DB_MAX_CONNS":     "9999-leak-conns",
		"APP_SHUTDOWN_TIMEOUT": "leak-timeout",
		"APP_DATABASE_URL":     "postgres://user:leak-password@127.0.0.1:1/x?sslmode=disable",
		"MODULE_LIMIT":         "leak-module-value",
	})
	module := core.Module{ID: "fake", Provide: func(_ context.Context, b *core.Builder) error {
		b.Config().Invalid("MODULE_LIMIT", "must be a number")
		return nil
	}}

	_, err := core.Build(context.Background(), core.Options{Logger: logger, Config: source}, []core.Module{module})

	if err == nil {
		t.Fatal("Build accepted invalid configuration")
	}
	for _, key := range []string{"APP_PUBLIC_BASE_URL", "APP_DB_MAX_CONNS", "APP_SHUTDOWN_TIMEOUT", "MODULE_LIMIT"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not name %s: %v", key, err)
		}
	}
	events := sink.find(t, "config_invalid")
	if len(events) != 1 {
		t.Fatalf("config_invalid logged %d times, want 1", len(events))
	}
	problems, _ := events[0]["problems"].([]any)
	if len(problems) != 4 {
		t.Fatalf("problems = %v, want 3 from core and 1 from the module", problems)
	}
	for _, leaked := range []string{"leak-url-value", "leak-conns", "leak-timeout", "leak-password", "leak-module-value"} {
		if strings.Contains(err.Error(), leaked) || strings.Contains(sink.text(), leaked) {
			t.Errorf("a configured value (%s) reached the error or the log", leaked)
		}
	}
}

func TestBuildRejectsPublicBaseURLWithPath(t *testing.T) {
	logger, _ := newLogger()
	source := config.MapSource(map[string]string{
		"APP_PUBLIC_BASE_URL": "http://localhost:10120/app",
		"APP_DATABASE_URL":    "postgres://user:change-me@127.0.0.1:1/x?sslmode=disable",
	})

	_, err := core.Build(context.Background(), core.Options{Logger: logger, Config: source}, nil)

	if err == nil || !strings.Contains(err.Error(), "APP_PUBLIC_BASE_URL") {
		t.Fatalf("err = %v, want a APP_PUBLIC_BASE_URL problem", err)
	}
}

func TestBuildRequiresDatabaseURLAndRejectsUnparseableOne(t *testing.T) {
	logger, _ := newLogger()
	for name, source := range map[string]map[string]string{
		"missing":      {},
		"unparseable":  {"APP_DATABASE_URL": "postgres://user:leak-pw@host:notaport/db"},
		"blank secret": {"APP_DATABASE_URL": "   "},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := core.Build(context.Background(), core.Options{Logger: logger, Config: config.MapSource(source)}, nil)
			if err == nil || !strings.Contains(err.Error(), "APP_DATABASE_URL") {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(err.Error(), "leak-pw") {
				t.Fatal("password leaked")
			}
		})
	}
}

func TestBuildFailsAtPingAfterConfigIsValid(t *testing.T) {
	logger, sink := newLogger()
	source := config.MapSource(map[string]string{
		"APP_DATABASE_URL": "postgres://user:change-me@127.0.0.1:1/x?sslmode=disable",
	})

	_, err := core.Build(context.Background(), core.Options{Logger: logger, Config: source}, nil)

	if err == nil || !strings.Contains(err.Error(), "db:") {
		t.Fatalf("err = %v, want a database error", err)
	}
	if len(sink.find(t, "config_invalid")) != 0 || len(sink.find(t, "server_failed")) != 1 {
		t.Fatalf("logs: %s", sink.text())
	}
}

func TestBuildRequiresLogger(t *testing.T) {
	_, err := core.Build(context.Background(), core.Options{}, nil)
	if err == nil {
		t.Fatal("Build accepted a missing logger")
	}
}

func TestStartupPhasesRunInOrder(t *testing.T) {
	var mu sync.Mutex
	var events []string
	record := func(event string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	}
	migrations := fstest.MapFS{
		"migrations/20260101120000_sys_create_seam_things.sql": {Data: []byte("-- +goose Up\nCREATE TABLE sys_seam_things (id int);\n")},
	}
	var tableAtMount bool
	first := core.Module{
		ID:         "first",
		Migrations: migrations,
		Provide: func(_ context.Context, b *core.Builder) error {
			record("provide first")
			return b.ProvideTenantDirectory(tenantDirectory{})
		},
		Mount: func(ctx context.Context, h *core.Host) error {
			record("mount first")
			err := h.Pool().QueryRow(ctx, "SELECT to_regclass('sys_seam_things') IS NOT NULL").Scan(&tableAtMount)
			h.Handle("GET /api/v1/tenants/exists", access.Public, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				exists, _ := h.TenantDirectory().TenantExists(r.Context(), reqctx.TenantID(tenantUUID))
				h.API().Data(w, r, http.StatusOK, map[string]bool{"exists": exists})
			}))
			return err
		},
		Start: func(context.Context, *core.Host) error { record("start first"); return nil },
	}
	second := core.Module{
		ID:      "second",
		Provide: func(context.Context, *core.Builder) error { record("provide second"); return nil },
		Mount:   func(context.Context, *core.Host) error { record("mount second"); return nil },
		Start:   func(context.Context, *core.Host) error { record("start second"); return nil },
	}

	app, sink := startApp(t, core.Options{}, first, second)

	want := "provide first,provide second,mount first,mount second,start first,start second"
	mu.Lock()
	got := strings.Join(events, ",")
	mu.Unlock()
	if got != want {
		t.Fatalf("phase order = %s, want %s", got, want)
	}
	if !tableAtMount {
		t.Fatal("migrations had not run when Mount started")
	}
	rec := do(app, http.MethodGet, "/api/v1/tenants/exists", nil)
	envelope := decode(t, rec)
	data, _ := (*envelope.Data).(map[string]any)
	if data["exists"] != true {
		t.Fatalf("a port provided in Provide was not visible in Mount: %s", rec.Body.String())
	}
	if len(sink.find(t, "migration_applied")) != 1 || len(sink.find(t, "migrations_complete")) != 1 {
		t.Fatalf("migration events missing: %s", sink.text())
	}
}

type tenantDirectory struct{}

func (tenantDirectory) TenantExists(context.Context, reqctx.TenantID) (bool, error) {
	return true, nil
}

func TestHostDefaultsWhenNoModuleProvidesPorts(t *testing.T) {
	var sessions, keys, tenants, flags bool
	module := mounting("defaults", func(h *core.Host) {
		sessions = h.SessionResolver() == access.NoSessions
		keys = h.APIKeyResolver() == access.NoAPIKeys
		tenants = h.TenantDirectory() == access.NoTenants
		flags = !h.FlagEvaluator().Enabled(context.Background(), "any")
	})

	startApp(t, core.Options{}, module)

	if !sessions || !keys || !tenants || !flags {
		t.Fatalf("defaults: sessions=%v keys=%v tenants=%v flags=%v", sessions, keys, tenants, flags)
	}
}

func TestPortsAreProvidedOnce(t *testing.T) {
	provider := func(id string) core.Module {
		return core.Module{ID: id, Provide: func(_ context.Context, b *core.Builder) error {
			return b.ProvideTenantDirectory(tenantDirectory{})
		}}
	}
	t.Run("two modules", func(t *testing.T) {
		expectBuildError(t, core.Options{}, "already provided", provider("one"), provider("two"))
	})
	t.Run("one module twice, error dropped", func(t *testing.T) {
		module := core.Module{ID: "twice", Provide: func(_ context.Context, b *core.Builder) error {
			_ = b.ProvideTenantDirectory(tenantDirectory{})
			_ = b.ProvideTenantDirectory(tenantDirectory{})
			return nil
		}}
		expectBuildError(t, core.Options{}, "already provided", module)
	})
	t.Run("pre-provided by options", func(t *testing.T) {
		expectBuildError(t, core.Options{TenantDirectory: tenantDirectory{}}, "already provided", provider("late"))
	})
	t.Run("outside phase one", func(t *testing.T) {
		var builder *core.Builder
		module := core.Module{
			ID:      "late",
			Provide: func(_ context.Context, b *core.Builder) error { builder = b; return nil },
			Mount: func(context.Context, *core.Host) error {
				return builder.ProvideSessionResolver(access.NoSessions)
			},
		}
		expectBuildError(t, core.Options{}, "Provide only", module)
	})
	t.Run("nil port", func(t *testing.T) {
		module := core.Module{ID: "nilport", Provide: func(_ context.Context, b *core.Builder) error {
			return b.ProvideSessionResolver(nil)
		}}
		expectBuildError(t, core.Options{}, "is nil", module)
	})
}

func expectBuildError(t *testing.T, opts core.Options, want string, modules ...core.Module) {
	t.Helper()
	database := testpg.New(t, "coreerr")
	logger, _ := newLogger()
	opts.Logger = logger
	opts.Pool = database.Pool(t, 4)
	opts.Config = config.MapSource(baseConfig())
	app, err := core.Build(context.Background(), opts, modules)
	if err == nil {
		_ = app.Close(context.Background())
		t.Fatalf("Build succeeded, want an error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want it to contain %q", err, want)
	}
}

func TestModuleErrorsStopStartup(t *testing.T) {
	boom := errors.New("boom")
	t.Run("provide", func(t *testing.T) {
		expectBuildError(t, core.Options{}, "provide: boom", core.Module{ID: "bad", Provide: func(context.Context, *core.Builder) error { return boom }})
	})
	t.Run("mount", func(t *testing.T) {
		expectBuildError(t, core.Options{}, "mount: boom", core.Module{ID: "bad", Mount: func(context.Context, *core.Host) error { return boom }})
	})
	t.Run("start", func(t *testing.T) {
		expectBuildError(t, core.Options{}, "start: boom", core.Module{ID: "bad", Start: func(context.Context, *core.Host) error { return boom }})
	})
}

func TestHandleMisusePanics(t *testing.T) {
	cases := map[string]struct {
		pattern string
		policy  access.Policy
		handler http.Handler
		want    string
	}{
		"api pattern without method":     {"/api/v1/x", access.Public, okHandler(""), "core: module bad: "},
		"api root pattern no method":     {"/api/v1", access.Public, okHandler(""), "core: module bad: "},
		"host qualified api no method":   {"example.com/api/v1/x", access.Public, okHandler(""), "core: module bad: "},
		"pattern under health":           {"GET /health/x", access.Public, okHandler(""), "core: module bad: "},
		"pattern under health no method": {"/health/x", access.Public, okHandler(""), "core: module bad: "},
		"invalid policy kind":            {"GET /api/v1/x", access.Policy{}, okHandler(""), "access: invalid kind"},
		"unregistered scope":             {"GET /api/v1/x", access.Policy{Kind: access.KindAPIKey, Scope: "nope:read"}, okHandler(""), "is not registered"},
		"nil handler":                    {"GET /api/v1/x", access.Public, nil, "Require needs a handler"},
		"pattern without path":           {"GET", access.Public, okHandler(""), "core: module bad: "},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			expectBuildPanic(t, tc.want, mounting("bad", func(h *core.Host) { h.Handle(tc.pattern, tc.policy, tc.handler) }))
		})
	}
	t.Run("duplicate pattern", func(t *testing.T) {
		expectBuildPanic(t, "conflicts with pattern", mounting("bad", func(h *core.Host) {
			h.Handle("GET /api/v1/x", access.Public, okHandler(""))
			h.Handle("GET /api/v1/x", access.Public, okHandler(""))
		}))
	})
	t.Run("during Start", func(t *testing.T) {
		var host *core.Host
		expectBuildPanic(t, "core: module bad: ", core.Module{
			ID:    "bad",
			Mount: func(_ context.Context, h *core.Host) error { host = h; return nil },
			Start: func(context.Context, *core.Host) error {
				host.Handle("GET /api/v1/late", access.Public, okHandler(""))
				return nil
			},
		})
	})
	t.Run("after Build", func(t *testing.T) {
		var host *core.Host
		startApp(t, core.Options{}, core.Module{ID: "kept", Mount: func(_ context.Context, h *core.Host) error { host = h; return nil }})
		defer func() {
			recovered := recover()
			message, isString := recovered.(string)
			if !isString || !strings.HasPrefix(message, "core: module kept: ") {
				t.Fatalf("Handle after Build panic = %v, want a core: module kept: message", recovered)
			}
		}()
		host.Handle("GET /api/v1/late", access.Public, okHandler(""))
	})
	t.Run("method pattern under api is accepted", func(t *testing.T) {
		startApp(t, core.Options{}, mounting("good", func(h *core.Host) {
			h.Handle("GET /api/v1/ok", access.Public, okHandler("ok"))
			h.Handle("POST example.com/api/v1/ok", access.Public, okHandler("ok"))
		}))
	})
}

// expectBuildPanic asserts Build panics with a string message containing want.
func expectBuildPanic(t *testing.T, want string, module core.Module) {
	t.Helper()
	database := testpg.New(t, "corepanic")
	logger, _ := newLogger()
	pool := database.Pool(t, 4)
	defer func() {
		recovered := recover()
		message := fmt.Sprint(recovered)
		if recovered == nil || !strings.Contains(message, want) {
			t.Fatalf("Build panic = %v, want a message containing %q", recovered, want)
		}
	}()
	app, err := core.Build(context.Background(), core.Options{Logger: logger, Pool: pool, Config: config.MapSource(baseConfig())}, []core.Module{module})
	if err == nil {
		_ = app.Close(context.Background())
	}
	t.Fatalf("Build returned err=%v without panicking", err)
}

func TestFallbackAnswersEnvelopeForUnknownPathsAndWrongMethods(t *testing.T) {
	app, _ := startApp(t, core.Options{}, mounting("things", func(h *core.Host) {
		h.Handle("GET /api/v1/things", access.Public, okHandler("things"))
	}))
	cases := []struct{ method, target string }{
		{http.MethodGet, "/api/v1/route-that-does-not-exist"},
		{http.MethodDelete, "/api/v1/route-that-does-not-exist"},
		{http.MethodPost, "/api/v1/things"},
		{http.MethodDelete, "/api/v1/things"},
		{http.MethodGet, "/api/v1/things/extra"},
		{http.MethodGet, "/api/v1/"},
		{http.MethodOptions, "/api/v1/things"},
		{http.MethodGet, "/api/v1"},
		{http.MethodGet, "/api/v1//route-that-does-not-exist"},
		{http.MethodGet, "/api/v1/./route-that-does-not-exist"},
		{http.MethodGet, "/api/v1/x/../route-that-does-not-exist"},
		{http.MethodPost, "/api/v1//things"},
		{http.MethodGet, "/api/v1/x/%2E%2E/route-that-does-not-exist"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.target, func(t *testing.T) {
			rec := do(app, tc.method, tc.target, nil)
			expectError(t, rec, http.StatusNotFound, api.CodeRouteNotFound)
			if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("Content-Type = %q, want JSON, never plain text", rec.Header().Get("Content-Type"))
			}
		})
	}
	if rec := do(app, http.MethodGet, "/api/v1/things", nil); rec.Body.String() != "things" {
		t.Fatalf("the mounted route no longer answers: %q", rec.Body.String())
	}
}

func TestCleanTrailingSlashSubtreeRouteIsNotRejectedAsUnclean(t *testing.T) {
	app, _ := startApp(t, core.Options{}, mounting("tree", func(h *core.Host) {
		h.Handle("GET /api/v1/tree/", access.Public, okHandler("tree"))
	}))
	for _, target := range []string{"/api/v1/tree/", "/api/v1/tree/leaf", "/api/v1/tree/leaf/"} {
		if rec := do(app, http.MethodGet, target, nil); rec.Body.String() != "tree" {
			t.Fatalf("GET %s = %d %q, want the mounted subtree handler", target, rec.Code, rec.Body.String())
		}
	}
}

func TestFallbackDoesNotShadowHealth(t *testing.T) {
	app, _ := startApp(t, core.Options{})
	if rec := do(app, http.MethodGet, "/health/live", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("/health/live = %d", rec.Code)
	}
	if rec := do(app, http.MethodGet, "/health/ready", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("/health/ready = %d", rec.Code)
	}
	rec := do(app, http.MethodGet, "/health/live", nil)
	if rec.Header().Get("X-Trace-Id") != "" {
		t.Fatal("health answers must not carry X-Trace-Id")
	}
}

func TestAPIResponsesCarryCacheHeadersEvenWhenRejected(t *testing.T) {
	app, _ := startApp(t, core.Options{}, mounting("things", func(h *core.Host) {
		h.Handle("GET /api/v1/things", access.Public, okHandler("things"))
	}))
	rejected := do(app, http.MethodPost, "/api/v1/things", map[string]string{"Origin": "https://evil.example.com"})
	app.Readiness().SetServing(false)
	draining := do(app, http.MethodGet, "/api/v1/things", nil)
	app.Readiness().SetServing(true)

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"mounted":  do(app, http.MethodGet, "/api/v1/things", nil),
		"fallback": do(app, http.MethodGet, "/api/v1/nope", nil),
		"csrf":     rejected,
		"draining": draining,
	} {
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: Cache-Control = %q", name, rec.Header().Get("Cache-Control"))
		}
		if !strings.Contains(strings.Join(rec.Header().Values("Vary"), ","), "Cookie") {
			t.Errorf("%s: Vary = %v", name, rec.Header().Values("Vary"))
		}
	}
}

func TestCrossOriginMutationIsRejected(t *testing.T) {
	var reached bool
	app, _ := startApp(t, core.Options{}, mounting("things", func(h *core.Host) {
		h.Handle("POST /api/v1/things", access.Public, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			reached = true
			w.WriteHeader(http.StatusCreated)
		}))
	}))
	rejected := []map[string]string{
		{"Origin": "https://evil.example.com"},
		{"Origin": "null"},
		{"Sec-Fetch-Site": "cross-site"},
		{"Sec-Fetch-Site": "same-site"},
		{"Origin": "http://localhost:10121"},
		{"Origin": "https://localhost:10120"},
	}
	for _, headers := range rejected {
		t.Run(fmt.Sprint(headers), func(t *testing.T) {
			rec := do(app, http.MethodPost, "/api/v1/things", headers)
			expectError(t, rec, http.StatusForbidden, api.CodeCSRFTokenInvalid)
		})
	}
	if reached {
		t.Fatal("a rejected request reached the handler")
	}
	accepted := []map[string]string{
		nil,
		{"Origin": publicBaseURL},
		{"Origin": "http://example.com"},
		{"Sec-Fetch-Site": "same-origin"},
	}
	for _, headers := range accepted {
		if rec := do(app, http.MethodPost, "/api/v1/things", headers); rec.Code != http.StatusCreated {
			t.Errorf("headers %v: status = %d, want 201", headers, rec.Code)
		}
	}
	if rec := do(app, http.MethodGet, "/api/v1/nope", map[string]string{"Origin": "https://evil.example.com"}); rec.Code != http.StatusNotFound {
		t.Errorf("a safe method must not be blocked by the origin check: %d", rec.Code)
	}
}

func TestDrainingAnswersServiceUnavailableOnAPIOnly(t *testing.T) {
	app, _ := startApp(t, core.Options{}, mounting("things", func(h *core.Host) {
		h.Handle("GET /api/v1/things", access.Public, okHandler("things"))
	}))
	app.Readiness().SetServing(false)

	expectError(t, do(app, http.MethodGet, "/api/v1/things", nil), http.StatusServiceUnavailable, api.CodeServiceUnavailable)
	expectError(t, do(app, http.MethodGet, "/api/v1/nope", nil), http.StatusServiceUnavailable, api.CodeServiceUnavailable)
	if rec := do(app, http.MethodGet, "/health/live", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("/health/live while draining = %d, want 204", rec.Code)
	}
	if rec := do(app, http.MethodGet, "/health/ready", nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/health/ready while draining = %d, want 503", rec.Code)
	}
}

func TestNotServingBeforeRunAnswers503(t *testing.T) {
	database := testpg.New(t, "corenotserving")
	logger, _ := newLogger()
	app, err := core.Build(context.Background(), core.Options{Logger: logger, Pool: database.Pool(t, 4), Config: config.MapSource(baseConfig())}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close(context.Background()) })

	expectError(t, do(app, http.MethodGet, "/api/v1/x", nil), http.StatusServiceUnavailable, api.CodeServiceUnavailable)
}

func TestPanicBecomesEnvelopeWithoutLeakingTheValue(t *testing.T) {
	app, sink := startApp(t, core.Options{}, mounting("boom", func(h *core.Host) {
		h.Handle("GET /api/v1/boom", access.Public, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("kaboom-secret-detail")
		}))
		h.Handle("GET /plain-boom", access.Public, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("kaboom-secret-detail")
		}))
	}))

	rec := do(app, http.MethodGet, "/api/v1/boom", nil)

	expectError(t, rec, http.StatusInternalServerError, api.CodeInternalError)
	if strings.Contains(rec.Body.String(), "kaboom") {
		t.Fatal("panic value leaked into the response")
	}
	events := sink.find(t, "panic_recovered")
	if len(events) != 1 || events[0]["level"] != "ERROR" {
		t.Fatalf("panic_recovered events = %v", events)
	}
	completed := sink.find(t, "http_request_completed")
	if len(completed) == 0 || completed[len(completed)-1]["status_code"] != float64(500) {
		t.Fatalf("access log must record the 500: %v", completed)
	}

	plain := do(app, http.MethodGet, "/plain-boom", nil)
	if plain.Code != http.StatusInternalServerError || strings.Contains(plain.Body.String(), "kaboom") {
		t.Fatalf("non-API panic: %d %q", plain.Code, plain.Body.String())
	}
}

func TestPanicAfterHeadersAbortsTheConnection(t *testing.T) {
	app, _ := startApp(t, core.Options{}, mounting("late", func(h *core.Host) {
		h.Handle("GET /api/v1/late", access.Public, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			panic("after headers")
		}))
	}))
	defer func() {
		if recovered := recover(); recovered != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler", recovered)
		}
	}()

	do(app, http.MethodGet, "/api/v1/late", nil)

	t.Fatal("the connection was not aborted")
}

func TestAbortHandlerPanicPassesThrough(t *testing.T) {
	app, sink := startApp(t, core.Options{}, mounting("abort", func(h *core.Host) {
		h.Handle("GET /api/v1/abort", access.Public, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic(http.ErrAbortHandler)
		}))
	}))
	defer func() {
		if recovered := recover(); recovered != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler", recovered)
		}
		if len(sink.find(t, "panic_recovered")) != 0 {
			t.Fatal("an intentional abort must not be logged as a panic")
		}
	}()

	do(app, http.MethodGet, "/api/v1/abort", nil)
}

type fakeSession struct{}

func (fakeSession) Present(r *http.Request) bool { return r.Header.Get("X-Test-Session") != "" }

func (fakeSession) Resolve(*http.Request) (access.SessionResult, error) {
	tenant := reqctx.TenantID(tenantUUID)
	principal := reqctx.Principal{
		Kind:      reqctx.ActorAccount,
		ActorID:   "22222222-2222-4222-8222-222222222222",
		AccountID: "22222222-2222-4222-8222-222222222222",
		TenantID:  &tenant,
		Role:      reqctx.RoleAdmin,
	}
	return access.SessionResult{State: access.SessionValid, Principal: principal, CSRFValid: true}, nil
}

func TestAccessLogRecordsRouteStatusAndCaller(t *testing.T) {
	app, sink := startApp(t, core.Options{SessionResolver: fakeSession{}}, mounting("things", func(h *core.Host) {
		h.Handle("GET /api/v1/things/{id}", access.Policy{Kind: access.KindSession}, okHandler("thing"))
	}))

	do(app, http.MethodGet, "/api/v1/things/42", map[string]string{"X-Test-Session": "x"})
	do(app, http.MethodGet, "/api/v1/things/42", nil)
	do(app, http.MethodGet, "/health/live", nil)

	var guarded, denied, health map[string]any
	for _, record := range sink.find(t, "http_request_completed") {
		switch {
		case record["status_code"] == float64(200) && record["route"] == "GET /api/v1/things/{id}":
			guarded = record
		case record["status_code"] == float64(401):
			denied = record
		case record["route"] == "GET /health/live":
			health = record
		}
	}
	if guarded == nil || guarded["tenant_id"] != tenantUUID || guarded["actor_id"] != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("guarded request record = %v", guarded)
	}
	if guarded["method"] != http.MethodGet {
		t.Fatalf("method = %v", guarded["method"])
	}
	if _, ok := guarded["duration_ms"]; !ok {
		t.Fatal("duration_ms missing")
	}
	if denied == nil || denied["route"] != "GET /api/v1/things/{id}" {
		t.Fatalf("denied request record = %v", denied)
	}
	if _, hasActor := denied["actor_id"]; hasActor {
		t.Fatal("an unauthenticated request must not log an actor")
	}
	if health == nil || health["level"] != "DEBUG" {
		t.Fatalf("health request record = %v, want DEBUG", health)
	}
}

func TestTraceparentIsContinuedAndMalformedOneIsReplaced(t *testing.T) {
	app, _ := startApp(t, core.Options{})
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"

	continued := do(app, http.MethodGet, "/api/v1/nope", map[string]string{"traceparent": "00-" + traceID + "-00f067aa0ba902b7-01"})
	malformed := do(app, http.MethodGet, "/api/v1/nope", map[string]string{"traceparent": "garbage"})
	fresh := do(app, http.MethodGet, "/api/v1/nope", nil)

	expectError(t, continued, http.StatusNotFound, api.CodeRouteNotFound)
	if continued.Header().Get("X-Trace-Id") != traceID {
		t.Fatalf("trace id = %q, want %s", continued.Header().Get("X-Trace-Id"), traceID)
	}
	for name, rec := range map[string]*httptest.ResponseRecorder{"malformed": malformed, "fresh": fresh} {
		expectError(t, rec, http.StatusNotFound, api.CodeRouteNotFound)
		if id := rec.Header().Get("X-Trace-Id"); id == traceID || id == strings.Repeat("0", 32) {
			t.Fatalf("%s: trace id = %q", name, id)
		}
	}
}

func TestReadinessReflectsPoolAndModuleChecks(t *testing.T) {
	failing := true
	var mu sync.Mutex
	app, _ := startApp(t, core.Options{}, mounting("probe", func(h *core.Host) {
		h.Readiness().Add(health.Check{Name: "extra", Probe: func(context.Context) error {
			mu.Lock()
			defer mu.Unlock()
			if failing {
				return errors.New("down")
			}
			return nil
		}})
	}))

	rec := do(app, http.MethodGet, "/health/ready", nil)
	want := `{"serving":true,"checks":[{"name":"postgres","status":"pass"},{"name":"extra","status":"fail"}]}`
	if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != want {
		t.Fatalf("ready = %d %s, want 503 %s", rec.Code, rec.Body.String(), want)
	}
	mu.Lock()
	failing = false
	mu.Unlock()
	if rec := do(app, http.MethodGet, "/health/ready", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("ready after recovery = %d", rec.Code)
	}
}

func TestReadinessFailsWhenThePoolIsClosed(t *testing.T) {
	database := testpg.New(t, "corepoolgone")
	logger, _ := newLogger()
	pool := database.Pool(t, 4)
	app, err := core.Build(context.Background(), core.Options{Logger: logger, Pool: pool, Config: config.MapSource(baseConfig())}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close(context.Background()) })
	app.Readiness().SetServing(true)
	pool.Close()

	rec := do(app, http.MethodGet, "/health/ready", nil)

	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"postgres","status":"fail"`) {
		t.Fatalf("ready = %d %s", rec.Code, rec.Body.String())
	}
}

func TestBuilderAndHostExposeSharedServices(t *testing.T) {
	var providedURL, mountedURL string
	var scopeKnownAtMount bool
	module := core.Module{
		ID: "svc",
		Provide: func(_ context.Context, b *core.Builder) error {
			providedURL = b.PublicBaseURL().String()
			b.PublicBaseURL().Host = "mutated.example.com"
			b.Logger().Info("provide_probe")
			return b.Scopes().Register("svc:read", "Read svc")
		},
		Mount: func(_ context.Context, h *core.Host) error {
			mountedURL = h.PublicBaseURL().String()
			scopeKnownAtMount = h.Scopes().Known("svc:read")
			h.Logger().Info("mount_probe")
			h.Handle("GET /api/v1/svc", access.Policy{Kind: access.KindAPIKey, Scope: "svc:read"}, okHandler(""))
			return nil
		},
	}
	logger, sink := newLogger()
	database := testpg.New(t, "coresvc")
	app, err := core.Build(context.Background(), core.Options{Logger: logger, Pool: database.Pool(t, 4), Config: config.MapSource(baseConfig())}, []core.Module{module})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close(context.Background()) })

	if providedURL != publicBaseURL || mountedURL != publicBaseURL {
		t.Fatalf("PublicBaseURL = %q / %q, want %q and no shared mutation", providedURL, mountedURL, publicBaseURL)
	}
	if !scopeKnownAtMount {
		t.Fatal("a scope registered in Provide is unknown in Mount")
	}
	for _, event := range []string{"provide_probe", "mount_probe"} {
		records := sink.find(t, event)
		if len(records) != 1 || records[0]["component"] != "svc" {
			t.Fatalf("%s records = %v, want one bound to component svc", event, records)
		}
	}
}

func TestRegisteringAScopeAfterProvideFails(t *testing.T) {
	var scopes *access.ScopeRegistry
	var lateErr error
	module := core.Module{
		ID:      "late-scope",
		Provide: func(_ context.Context, b *core.Builder) error { scopes = b.Scopes(); return nil },
		Mount: func(context.Context, *core.Host) error {
			lateErr = scopes.Register("late:read", "too late")
			return nil
		},
	}

	startApp(t, core.Options{}, module)

	if lateErr == nil {
		t.Fatal("the scope registry was not frozen after Provide")
	}
}

func TestAppCloseIsIdempotent(t *testing.T) {
	database := testpg.New(t, "coreclose")
	logger, _ := newLogger()
	app, err := core.Build(context.Background(), core.Options{Logger: logger, Pool: database.Pool(t, 4), Config: config.MapSource(baseConfig())}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first := app.Close(context.Background()); first != nil {
		t.Fatalf("first Close: %v", first)
	}
	if second := app.Close(context.Background()); second != nil {
		t.Fatalf("second Close: %v", second)
	}
}

func TestAppExposesCoreConfig(t *testing.T) {
	source := baseConfig()
	source["GO_SRV_ADDR"] = "127.0.0.1:12345"
	source["APP_SHUTDOWN_TIMEOUT"] = "3s"

	app, _ := startApp(t, core.Options{Config: config.MapSource(source)})

	if app.Addr() != "127.0.0.1:12345" || app.ShutdownTimeout() != 3*time.Second {
		t.Fatalf("Addr = %q ShutdownTimeout = %v", app.Addr(), app.ShutdownTimeout())
	}
}

func TestCoreConfigDefaults(t *testing.T) {
	var reached bool
	app, _ := startApp(t, core.Options{Config: config.MapSource(map[string]string{})}, mounting("things", func(h *core.Host) {
		h.Handle("POST /api/v1/things", access.Public, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			reached = true
			w.WriteHeader(http.StatusCreated)
		}))
	}))

	if app.Addr() != ":10100" || app.ShutdownTimeout() != 10*time.Second {
		t.Fatalf("Addr = %q ShutdownTimeout = %v, want :10100 and 10s", app.Addr(), app.ShutdownTimeout())
	}
	trusted := do(app, http.MethodPost, "/api/v1/things", map[string]string{"Origin": "http://localhost:10120"})
	if trusted.Code != http.StatusCreated || !reached {
		t.Fatalf("the default public origin must be trusted: %d", trusted.Code)
	}
	foreign := do(app, http.MethodPost, "/api/v1/things", map[string]string{"Origin": "http://localhost:10121"})
	expectError(t, foreign, http.StatusForbidden, api.CodeCSRFTokenInvalid)
}

func TestRequestSpansAreNamedByRoute(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	module := core.Module{
		ID: "things",
		Provide: func(_ context.Context, b *core.Builder) error {
			provider, ok := b.Telemetry().TracerProvider().(*sdktrace.TracerProvider)
			if !ok {
				return errors.New("tracer provider is not the SDK provider")
			}
			provider.RegisterSpanProcessor(recorder)
			return nil
		},
		Mount: func(_ context.Context, h *core.Host) error {
			h.Handle("GET /api/v1/things/{id}", access.Public, okHandler("thing"))
			return nil
		},
	}
	app, sink := startApp(t, core.Options{}, module)

	do(app, http.MethodGet, "/api/v1/things/42", nil)
	do(app, http.MethodGet, "/api/v1/missing", nil)
	do(app, http.MethodGet, "/nowhere", nil)

	names := []string{}
	for _, span := range recorder.Ended() {
		names = append(names, span.Name())
	}
	wantNames := []string{"GET /api/v1/things/{id}", "/api/v1/", http.MethodGet}
	if !slices.Equal(names, wantNames) {
		t.Fatalf("span names = %v, want %v", names, wantNames)
	}
	routes := []any{}
	for _, record := range sink.find(t, "http_request_completed") {
		routes = append(routes, record["route"])
	}
	if len(routes) != 3 || routes[0] != "GET /api/v1/things/{id}" || routes[1] != "/api/v1/" || routes[2] != "unmatched" {
		t.Fatalf("access log routes = %v, want the pattern, the API catch-all, then unmatched", routes)
	}
}

func TestRunServesAndShutsDownCleanly(t *testing.T) {
	database := testpg.New(t, "corerun")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	t.Setenv("GO_SRV_ADDR", addr)
	t.Setenv("APP_DATABASE_URL", database.URL)
	t.Setenv("APP_PUBLIC_BASE_URL", "http://"+addr)
	logger, sink := newLogger()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- core.Run(ctx, logger, nil) }()

	waitFor(t, func() bool {
		response, err := http.Get("http://" + addr + "/health/ready")
		if err != nil {
			return false
		}
		_ = response.Body.Close()
		return response.StatusCode == http.StatusNoContent
	})
	response, err := http.Get("http://" + addr + "/api/v1/x")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNotFound || response.Header.Get("X-Trace-Id") == "" {
		t.Fatalf("/api/v1/x = %d trace %q", response.StatusCode, response.Header.Get("X-Trace-Id"))
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v after a clean shutdown", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	for _, event := range []string{"server_starting", "server_ready", "server_stopping", "server_stopped"} {
		if len(sink.find(t, event)) != 1 {
			t.Errorf("event %s logged %d times, want 1", event, len(sink.find(t, event)))
		}
	}
}

func TestRunFailsWhenTheAddressIsTaken(t *testing.T) {
	database := testpg.New(t, "coretaken")
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	t.Setenv("GO_SRV_ADDR", taken.Addr().String())
	t.Setenv("APP_DATABASE_URL", database.URL)
	t.Setenv("APP_PUBLIC_BASE_URL", publicBaseURL)
	logger, sink := newLogger()

	err = core.Run(context.Background(), logger, nil)

	if err == nil || len(sink.find(t, "server_failed")) != 1 {
		t.Fatalf("err = %v logs = %s", err, sink.text())
	}
}

func TestRunReturnsBuildErrorFromConfig(t *testing.T) {
	t.Setenv("APP_DATABASE_URL", "")
	t.Setenv("APP_PUBLIC_BASE_URL", "")
	logger, sink := newLogger()

	err := core.Run(context.Background(), logger, nil)

	if err == nil || len(sink.find(t, "config_invalid")) != 1 || len(sink.find(t, "server_failed")) != 0 {
		t.Fatalf("err = %v logs = %s", err, sink.text())
	}
}

func TestMainExitsOneWhenStartupFails(t *testing.T) {
	t.Setenv("APP_DATABASE_URL", "")
	t.Setenv("APP_PUBLIC_BASE_URL", "")

	code := core.Main(nil)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func FuzzFallbackAlwaysAnswersAnEnvelope(f *testing.F) {
	for _, seed := range []string{"x", "a/b/c", "..", "%2e%2e/x", "x%00y", "{id}", "ünï", "//", "x?y=1", strings.Repeat("a", 300)} {
		f.Add(seed, uint8(0))
	}
	database := testpg.New(f, "corefuzz")
	logger, _ := newLogger()
	app, err := core.Build(context.Background(), core.Options{Logger: logger, Pool: database.Pool(f, 4), Config: config.MapSource(baseConfig())}, nil)
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { _ = app.Close(context.Background()) })
	app.Readiness().SetServing(true)
	methods := []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodPut, http.MethodPatch, http.MethodHead, http.MethodOptions}

	f.Fuzz(func(t *testing.T, suffix string, methodIndex uint8) {
		target := "/api/v1/" + suffix
		parsed, parseErr := http.NewRequest(http.MethodGet, "http://example.com"+target, nil)
		if parseErr != nil || !strings.HasPrefix(parsed.URL.Path, "/api/v1/") {
			t.Skip()
		}
		method := methods[int(methodIndex)%len(methods)]
		req := httptest.NewRequest(method, "http://example.com/", nil)
		req.URL = parsed.URL
		req.RequestURI = target
		rec := httptest.NewRecorder()
		app.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %q answered %d", method, target, rec.Code)
		}
		if method == http.MethodHead {
			return
		}
		var envelope api.Envelope[any]
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope.Error == nil || envelope.Error.Code != api.CodeRouteNotFound {
			t.Fatalf("%s %q body is not a ROUTE_NOT_FOUND envelope: %q", method, target, rec.Body.String())
		}
		if rec.Header().Get("X-Trace-Id") != envelope.Meta.TraceID {
			t.Fatalf("trace id mismatch on %q", target)
		}
	})
}

var moduleIDRule = regexp.MustCompile(`^[a-z][a-z0-9-]{1,30}$`)

func FuzzModuleIDsNeverPanic(f *testing.F) {
	for _, seed := range []string{"core", "Core", "a", "", "a--b", "-ab", "ab-", strings.Repeat("z", 40), "ünï", "a b"} {
		f.Add(seed)
	}
	logger, _ := newLogger()
	f.Fuzz(func(t *testing.T, id string) {
		_, err := core.Build(context.Background(), core.Options{Logger: logger, Config: config.MapSource(map[string]string{})}, []core.Module{{ID: id}})
		if err == nil {
			t.Fatal("Build with no database URL cannot succeed")
		}
		isValidID := moduleIDRule.MatchString(id)
		if !isValidID && !strings.Contains(err.Error(), "core: module 0: id") {
			t.Fatalf("id %q was not rejected as an id: %v", id, err)
		}
	})
}
