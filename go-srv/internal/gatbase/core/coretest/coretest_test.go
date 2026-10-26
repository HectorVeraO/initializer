package coretest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/auth"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/coretest"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/featureflag"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
)

const (
	testActor  = "22222222-2222-4222-8222-222222222222"
	adminEnvVr = "GATBASE_TEST_DATABASE_URL"
)

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) records(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		err := json.Unmarshal([]byte(line), &record)
		if err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		out = append(out, record)
	}
	return out
}

func mounting(id string, mount func(h *core.Host)) core.Module {
	return core.Module{ID: id, Mount: func(_ context.Context, h *core.Host) error {
		mount(h)
		return nil
	}}
}

func send(handler http.Handler, method, target string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func envelopeOf(t *testing.T, rec *httptest.ResponseRecorder) api.Envelope[any] {
	t.Helper()
	var envelope api.Envelope[any]
	err := json.Unmarshal(rec.Body.Bytes(), &envelope)
	if err != nil {
		t.Fatalf("body is not an envelope: %q", rec.Body.String())
	}
	return envelope
}

func ping(h *core.Host, pattern string) {
	h.Handle(pattern, auth.Public, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "pong")
	}))
}

func TestNewServesModuleRoutesAndTheCoreFallback(t *testing.T) {
	handler := coretest.New(t, []core.Module{mounting("alpha", func(h *core.Host) {
		ping(h, "GET /api/v1/ping")
	})}, coretest.Options{})

	rec := send(handler, http.MethodGet, "/api/v1/ping", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "pong" {
		t.Fatalf("ping = %d %q", rec.Code, rec.Body.String())
	}

	missing := send(handler, http.MethodGet, "/api/v1/nowhere", nil)
	envelope := envelopeOf(t, missing)
	if missing.Code != http.StatusNotFound || envelope.Error == nil || envelope.Error.Code != api.CodeRouteNotFound {
		t.Fatalf("fallback = %d %+v", missing.Code, envelope.Error)
	}
	traceID := missing.Header().Get("X-Trace-Id")
	if traceID == "" || traceID != envelope.Meta.TraceID {
		t.Fatalf("X-Trace-Id %q must equal meta.traceId %q", traceID, envelope.Meta.TraceID)
	}
}

func TestNewAnswersHealthAsServing(t *testing.T) {
	handler := coretest.New(t, nil, coretest.Options{})
	rec := send(handler, http.MethodGet, "/health/ready", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("ready = %d, want 204", rec.Code)
	}
}

func TestNewMergesConfigOverDefaultsAndUsesTheFixedClock(t *testing.T) {
	var baseURL string
	var now time.Time
	module := core.Module{
		ID: "alpha",
		Provide: func(_ context.Context, b *core.Builder) error {
			baseURL = b.PublicBaseURL().String()
			now = b.Clock().Now()
			return nil
		},
	}
	coretest.New(t, []core.Module{module}, coretest.Options{})
	if baseURL != "http://localhost:10120" {
		t.Fatalf("default base URL = %q", baseURL)
	}
	if want := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC); !now.Equal(want) {
		t.Fatalf("default clock = %s, want %s", now, want)
	}

	custom := time.Date(2027, 5, 6, 7, 8, 9, 0, time.UTC)
	coretest.New(t, []core.Module{module}, coretest.Options{
		Config: map[string]string{"GATBASE_PUBLIC_BASE_URL": "https://app.example.com"},
		Clock:  clock.Fixed(custom),
	})
	if baseURL != "https://app.example.com" || !now.Equal(custom) {
		t.Fatalf("overrides = %q %s", baseURL, now)
	}
}

type allFlags struct{}

func (allFlags) Enabled(context.Context, featureflag.Key) bool { return true }

func (allFlags) Snapshot(context.Context, *reqctx.TenantID) map[featureflag.Key]bool {
	return map[featureflag.Key]bool{"x": true}
}

type keyFake struct{}

func (keyFake) Present(*http.Request) bool { return true }

func (keyFake) Resolve(context.Context, *http.Request) (reqctx.Principal, error) {
	return reqctx.Principal{Kind: reqctx.ActorAccount, ActorID: testActor}, nil
}

type tenantFake struct{}

func (tenantFake) TenantExists(context.Context, reqctx.TenantID) (bool, error) { return true, nil }

func TestNewHandsTheFakesToModules(t *testing.T) {
	var flags featureflag.Evaluator
	var keys auth.APIKeyResolver
	var tenants auth.TenantDirectory
	var sessions auth.SessionResolver
	module := mounting("alpha", func(h *core.Host) {
		flags = h.FlagEvaluator()
		keys = h.APIKeyResolver()
		tenants = h.TenantDirectory()
		sessions = h.SessionResolver()
	})
	coretest.New(t, []core.Module{module}, coretest.Options{
		FlagEvaluator:   allFlags{},
		SessionResolver: coretest.Session(reqctx.Principal{Kind: reqctx.ActorAccount, ActorID: testActor}),
		APIKeyResolver:  keyFake{},
		TenantDirectory: tenantFake{},
	})
	if !flags.Enabled(context.Background(), "x") {
		t.Fatalf("the module must see the supplied flag evaluator")
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "test_session", Value: "x"})
	result, err := sessions.Resolve(req)
	if err != nil || result.State != auth.SessionValid || result.Principal.ActorID != testActor {
		t.Fatalf("session resolver = %+v, %v", result, err)
	}
	principal, err := keys.Resolve(context.Background(), req)
	if err != nil || principal.ActorID != testActor {
		t.Fatalf("api key resolver = %+v, %v", principal, err)
	}
	exists, err := tenants.TenantExists(context.Background(), reqctx.TenantID("33333333-3333-4333-8333-333333333333"))
	if err != nil || !exists {
		t.Fatalf("tenant directory = %v, %v", exists, err)
	}
}

func TestSessionFakeResolvesTheCookieAndChecksTheCSRFHeader(t *testing.T) {
	principal := reqctx.Principal{Kind: reqctx.ActorAccount, ActorID: testActor}
	var seen reqctx.Principal
	module := mounting("alpha", func(h *core.Host) {
		route := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen, _ = reqctx.PrincipalFrom(r.Context())
			w.WriteHeader(http.StatusNoContent)
		})
		h.Handle("GET /api/v1/me", auth.Policy{Kind: auth.KindSession}, route)
		h.Handle("POST /api/v1/me", auth.Policy{Kind: auth.KindSession}, route)
	})
	handler := coretest.New(t, []core.Module{module}, coretest.Options{SessionResolver: coretest.Session(principal)})
	withCookie := func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "test_session", Value: "x"}) }

	cases := []struct {
		name   string
		method string
		mutate func(*http.Request)
		status int
		code   api.Code
	}{
		{"cookie resolves the principal", http.MethodGet, withCookie, http.StatusNoContent, ""},
		{"no cookie is an invalid session", http.MethodGet, nil, http.StatusUnauthorized, api.CodeSessionInvalid},
		{"mutation without the CSRF header is rejected", http.MethodPost, withCookie, http.StatusForbidden, api.CodeCSRFTokenInvalid},
		{"CSRF header other than ok is rejected", http.MethodPost, func(r *http.Request) {
			withCookie(r)
			r.Header.Set("X-XSRF-TOKEN", "nope")
		}, http.StatusForbidden, api.CodeCSRFTokenInvalid},
		{"CSRF header ok passes", http.MethodPost, func(r *http.Request) {
			withCookie(r)
			r.Header.Set("X-XSRF-TOKEN", "ok")
		}, http.StatusNoContent, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen = reqctx.Principal{}
			rec := send(handler, tc.method, "/api/v1/me", tc.mutate)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tc.status, rec.Body.String())
			}
			if tc.code != "" {
				envelope := envelopeOf(t, rec)
				if envelope.Error == nil || envelope.Error.Code != tc.code {
					t.Fatalf("error = %+v, want %s", envelope.Error, tc.code)
				}
				return
			}
			if seen.ActorID != testActor || seen.Kind != reqctx.ActorAccount {
				t.Fatalf("principal = %+v", seen)
			}
		})
	}
}

func TestLoggerReceivesModuleRecordsWithTraceFields(t *testing.T) {
	var sink syncBuffer
	logger := slog.New(slog.NewJSONHandler(&sink, nil))
	module := mounting("alpha", func(h *core.Host) {
		h.Handle("GET /api/v1/log", auth.Public, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.Logger().InfoContext(r.Context(), "alpha_checked")
			w.WriteHeader(http.StatusNoContent)
		}))
	})
	handler := coretest.New(t, []core.Module{module}, coretest.Options{Logger: logger})

	rec := send(handler, http.MethodGet, "/api/v1/log", nil)
	traceID := rec.Header().Get("X-Trace-Id")

	var found map[string]any
	for _, record := range sink.records(t) {
		if record["msg"] == "alpha_checked" {
			found = record
		}
	}
	if found == nil {
		t.Fatalf("record alpha_checked did not reach Options.Logger")
	}
	if found["trace_id"] != traceID || traceID == "" {
		t.Fatalf("trace_id = %v, want %q", found["trace_id"], traceID)
	}
	if spanID, _ := found["span_id"].(string); spanID == "" {
		t.Fatalf("record carries no span_id: %v", found)
	}
}

func TestNilLoggerDiscardsRecords(t *testing.T) {
	module := mounting("alpha", func(h *core.Host) {
		h.Logger().Info("alpha_started")
	})
	output := captureStdStreams(t, func() {
		coretest.New(t, []core.Module{module}, coretest.Options{})
	})
	if output != "" {
		t.Fatalf("a nil Options.Logger must discard records, got output: %q", output)
	}
}

// captureStdStreams returns what fn writes to os.Stdout and os.Stderr.
func captureStdStreams(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = writer, writer
	collected := make(chan string)
	go func() {
		data, _ := io.ReadAll(reader)
		collected <- string(data)
	}()
	func() {
		defer func() {
			os.Stdout, os.Stderr = oldOut, oldErr
			_ = writer.Close()
		}()
		fn()
	}()
	return <-collected
}

func TestWithoutADatabaseAModuleTouchingThePoolFails(t *testing.T) {
	var pool *pgxpool.Pool
	coretest.New(t, []core.Module{mounting("alpha", func(h *core.Host) { pool = h.Pool() })}, coretest.Options{})
	if pool == nil {
		t.Fatalf("the module must receive a pool value")
	}
	_, err := pool.Exec(context.Background(), "SELECT 1")
	if err == nil {
		t.Fatalf("a query against the Database:false pool must fail")
	}
}

var migrations = fstest.MapFS{
	"migrations/20260101120000_sys_create_examples.sql": &fstest.MapFile{
		Data: []byte("-- +goose Up\nCREATE TABLE sys_examples (id int PRIMARY KEY);\n"),
	},
}

func requireAdminURL(t *testing.T) string {
	t.Helper()
	adminURL := os.Getenv(adminEnvVr)
	if adminURL == "" {
		t.Skipf("%s is not set; skipping database test", adminEnvVr)
	}
	return adminURL
}

func TestDatabaseTrueGivesAnOwnedMigratedDatabase(t *testing.T) {
	requireAdminURL(t)
	var pool *pgxpool.Pool
	module := core.Module{
		ID:         "alpha",
		Migrations: migrations,
		Mount:      func(_ context.Context, h *core.Host) error { pool = h.Pool(); return nil },
	}
	coretest.New(t, []core.Module{module}, coretest.Options{Database: true})

	ctx := context.Background()
	var ownName string
	err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&ownName)
	if err != nil {
		t.Fatalf("current_database: %v", err)
	}
	if !strings.HasPrefix(ownName, "it_") {
		t.Fatalf("database %q must carry the it_ prefix", ownName)
	}
	var tableCount int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_name = 'sys_examples'").Scan(&tableCount)
	if err != nil || tableCount != 1 {
		t.Fatalf("migration not applied: count=%d err=%v", tableCount, err)
	}
}

func TestDatabaseTrueDropsItsDatabaseOnCleanup(t *testing.T) {
	adminURL := requireAdminURL(t)
	var ownName string

	t.Run("module sees its own database", func(t *testing.T) {
		var pool *pgxpool.Pool
		module := core.Module{
			ID:         "alpha",
			Migrations: migrations,
			Mount:      func(_ context.Context, h *core.Host) error { pool = h.Pool(); return nil },
		}
		coretest.New(t, []core.Module{module}, coretest.Options{Database: true})

		ctx := context.Background()
		err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&ownName)
		if err != nil {
			t.Fatalf("current_database: %v", err)
		}
		if !strings.HasPrefix(ownName, "it_") {
			t.Fatalf("database %q must carry the it_ prefix", ownName)
		}
		var tableCount int
		err = pool.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_name = 'sys_examples'").Scan(&tableCount)
		if err != nil || tableCount != 1 {
			t.Fatalf("migration not applied: count=%d err=%v", tableCount, err)
		}
	})

	admin, err := pgx.Connect(context.Background(), adminURL)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer func() { _ = admin.Close(context.Background()) }()
	var exists bool
	err = admin.QueryRow(context.Background(), "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", ownName).Scan(&exists)
	if err != nil {
		t.Fatalf("pg_database: %v", err)
	}
	if exists {
		t.Fatalf("database %s must be dropped after the test", ownName)
	}
	var adminTables int
	err = admin.QueryRow(context.Background(), "SELECT count(*) FROM information_schema.tables WHERE table_name = 'sys_examples'").Scan(&adminTables)
	if err != nil || adminTables != 0 {
		t.Fatalf("the URL's own database must stay untouched: count=%d err=%v", adminTables, err)
	}
}

func TestTwoDatabaseHostsDoNotShareData(t *testing.T) {
	requireAdminURL(t)
	count := func() int {
		var pool *pgxpool.Pool
		module := core.Module{
			ID:         "alpha",
			Migrations: migrations,
			Mount:      func(_ context.Context, h *core.Host) error { pool = h.Pool(); return nil },
		}
		coretest.New(t, []core.Module{module}, coretest.Options{Database: true})
		_, err := pool.Exec(context.Background(), "INSERT INTO sys_examples (id) VALUES (1)")
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
		var rows int
		err = pool.QueryRow(context.Background(), "SELECT count(*) FROM sys_examples").Scan(&rows)
		if err != nil {
			t.Fatalf("count: %v", err)
		}
		return rows
	}
	if first, second := count(), count(); first != 1 || second != 1 {
		t.Fatalf("each host must start empty: %d %d", first, second)
	}
}

func TestRejectedExtendedQueryLeavesThePoolConnectionUsable(t *testing.T) {
	var pool *pgxpool.Pool
	coretest.New(t, []core.Module{mounting("alpha", func(h *core.Host) { pool = h.Pool() })}, coretest.Options{})
	ctx := context.Background()
	_, err := pool.Exec(ctx, "SELECT $1::int", 1)
	if err == nil {
		t.Fatalf("a parameterized query against the Database:false pool must fail")
	}
	err = pool.Ping(ctx)
	if err != nil {
		t.Fatalf("ping after a rejected extended query: %v", err)
	}
}
