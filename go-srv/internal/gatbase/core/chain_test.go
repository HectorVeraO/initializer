package core_test

import (
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/auth"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/config"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/internal/testpg"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/telemetry"
)

// A panic in a guarded handler is logged after the guard resolved the caller,
// so panic_recovered carries the same tenant_id and actor_id as the
// http_request_completed line of the request.
func TestPanicLogCarriesCaller(t *testing.T) {
	database := testpg.New(t, "core")
	sink := &logBuffer{}
	inner := slog.NewJSONHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(telemetry.NewLogHandler(inner, "go-srv"))
	module := mounting("boom", func(h *core.Host) {
		h.Handle("GET /api/v1/boom", auth.Policy{Kind: auth.KindSession}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("kaboom")
		}))
	})
	app, err := core.Build(context.Background(), core.Options{
		Logger:          logger,
		Pool:            database.Pool(t, 4),
		Config:          config.MapSource(baseConfig()),
		SessionResolver: fakeSession{},
	}, []core.Module{module})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { _ = app.Close(context.Background()) })
	app.Readiness().SetServing(true)

	rec := do(app, http.MethodGet, "/api/v1/boom", map[string]string{"X-Test-Session": "x"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	completed := sink.find(t, "http_request_completed")
	if len(completed) != 1 || completed[0]["actor_id"] == nil || completed[0]["tenant_id"] != tenantUUID {
		t.Fatalf("precondition: access log knows the caller: %v", completed)
	}
	panics := sink.find(t, "panic_recovered")
	if len(panics) != 1 {
		t.Fatalf("panic_recovered events = %v", panics)
	}
	record := panics[0]
	if record["trace_id"] != completed[0]["trace_id"] {
		t.Errorf("panic_recovered trace_id = %v, want %v", record["trace_id"], completed[0]["trace_id"])
	}
	if record["actor_id"] != completed[0]["actor_id"] || record["tenant_id"] != tenantUUID {
		t.Fatalf("panic_recovered lacks the caller the guard resolved: actor_id=%v tenant_id=%v (access log: actor_id=%v tenant_id=%v)",
			record["actor_id"], record["tenant_id"], completed[0]["actor_id"], completed[0]["tenant_id"])
	}
}
