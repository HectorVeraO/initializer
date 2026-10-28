// Package coretest is the test host for module tests: it runs core.Build with
// boundary fakes and returns the full handler, chain included, so a test sees
// X-Trace-Id, envelopes, policies and the fallback exactly as production does.
package coretest

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/config"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/featureflag"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/internal/testpg"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/telemetry"
)

const (
	defaultPublicBaseURL = "http://localhost:10120"
	serviceName          = "go-srv"
	sessionCookie        = "test_session"
	csrfHeader           = "X-XSRF-TOKEN"
	csrfValue            = "ok"
	testPoolConns        = 4
	closeTimeout         = 10 * time.Second
)

// defaultNow is the instant of the default clock.
var defaultNow = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// Options are the boundary fakes and settings a module test may supply.
type Options struct {
	// Config is merged over the defaults (APP_PUBLIC_BASE_URL=http://localhost:10120).
	Config map[string]string
	// Clock is nil for clock.Fixed(2026-01-01T00:00:00Z).
	Clock           clock.Clock
	SessionResolver access.SessionResolver
	APIKeyResolver  access.APIKeyResolver
	TenantDirectory access.TenantDirectory
	FlagEvaluator   featureflag.Evaluator
	// Logger, when set, receives every record (wrapped by telemetry.NewLogHandler, so
	// trace_id, span_id, tenant_id and actor_id are attached); nil discards logs.
	Logger *slog.Logger
	// Database true: own it_* database on APP_TEST_DATABASE_URL's server,
	// migrations applied, dropped on cleanup; the test is skipped when the
	// variable is unset. False: the pool points at a server that rejects every
	// query, so a module that touches Pool fails.
	Database bool
}

// New runs core.Build with modules and opts and returns the full handler,
// marked serving. Cleanup is registered on t; a Build error fails t.
func New(t testing.TB, modules []core.Module, opts Options) http.Handler {
	t.Helper()
	pool := openPool(t, opts.Database)

	settings := map[string]string{"APP_PUBLIC_BASE_URL": defaultPublicBaseURL}
	for key, value := range opts.Config {
		settings[key] = value
	}
	clk := opts.Clock
	if clk == nil {
		clk = clock.Fixed(defaultNow)
	}
	app, err := core.Build(context.Background(), core.Options{
		Logger:          newLogger(opts.Logger),
		Config:          config.MapSource(settings),
		Clock:           clk,
		Pool:            pool,
		SessionResolver: opts.SessionResolver,
		APIKeyResolver:  opts.APIKeyResolver,
		TenantDirectory: opts.TenantDirectory,
		FlagEvaluator:   opts.FlagEvaluator,
	}, modules)
	if err != nil {
		t.Fatalf("coretest: Build: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		_ = app.Close(ctx)
	})
	app.Readiness().SetServing(true)
	handler := app.Handler()
	return handler
}

// newLogger wraps sink with the core log handler; a nil sink discards.
func newLogger(sink *slog.Logger) *slog.Logger {
	var inner slog.Handler = slog.NewTextHandler(io.Discard, nil)
	if sink != nil {
		inner = sink.Handler()
	}
	return slog.New(telemetry.NewLogHandler(inner, serviceName))
}

// openPool returns the pool core.Build runs on. The pool is closed before its
// database is dropped (cleanups run last-in first-out).
func openPool(t testing.TB, ownDatabase bool) *pgxpool.Pool {
	t.Helper()
	if ownDatabase {
		database := testpg.New(t, "coretest")
		return database.Pool(t, testPoolConns)
	}
	return newRejectingPool(t)
}

// Session returns a SessionResolver fake: a request with cookie "test_session"
// resolves to p (SessionValid), and CSRFValid is true when header
// X-XSRF-TOKEN is "ok".
func Session(p reqctx.Principal) access.SessionResolver {
	return &sessionFake{principal: p}
}

type sessionFake struct {
	principal reqctx.Principal
}

func (*sessionFake) Present(r *http.Request) bool {
	_, err := r.Cookie(sessionCookie)
	hasCookie := err == nil
	return hasCookie
}

func (f *sessionFake) Resolve(r *http.Request) (access.SessionResult, error) {
	_, err := r.Cookie(sessionCookie)
	if err != nil {
		return access.SessionResult{State: access.SessionInvalid}, nil
	}
	result := access.SessionResult{
		State:     access.SessionValid,
		Principal: f.principal,
		CSRFValid: r.Header.Get(csrfHeader) == csrfValue,
	}
	return result, nil
}
