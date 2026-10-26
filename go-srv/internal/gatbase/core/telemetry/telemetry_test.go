package telemetry_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/telemetry"
)

const (
	validParent  = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	validTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	zeroTraceID  = "00000000000000000000000000000000"
)

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

func newTelemetry(t testing.TB) *telemetry.Telemetry {
	t.Helper()
	tel, err := telemetry.Setup(context.Background(), telemetry.Config{ServiceName: "svc-test"})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() { _ = tel.Shutdown(context.Background()) })
	return tel
}

// echoHandler answers with the Writer envelope so meta.traceId can be compared with the header.
func echoHandler() http.Handler {
	writer := api.NewWriter(clock.Fixed(time.Unix(0, 0)), slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		writer.Data(w, r, http.StatusOK, map[string]string{"id": r.PathValue("id")})
	})
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func serve(tel *telemetry.Telemetry, path, traceparent string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if traceparent != "" {
		req.Header["Traceparent"] = []string{traceparent}
	}
	rec := httptest.NewRecorder()
	tel.Middleware(echoHandler()).ServeHTTP(rec, req)
	return rec
}

func metaTraceID(t testing.TB, rec *httptest.ResponseRecorder) string {
	var env struct {
		Meta struct {
			TraceID string `json:"traceId"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return env.Meta.TraceID
}

func TestMiddlewareOpensTraceWithoutTraceparent(t *testing.T) {
	rec := serve(newTelemetry(t), "/api/v1/items/7", "")
	got := rec.Header().Get("X-Trace-Id")
	if !hex32.MatchString(got) || got == zeroTraceID {
		t.Fatalf("X-Trace-Id = %q, want a non-zero 32-hex id", got)
	}
	if meta := metaTraceID(t, rec); meta != got {
		t.Fatalf("meta.traceId = %q, want %q", meta, got)
	}
}

func TestMiddlewareContinuesValidTraceparent(t *testing.T) {
	rec := serve(newTelemetry(t), "/api/v1/items/7", validParent)
	if got := rec.Header().Get("X-Trace-Id"); got != validTraceID {
		t.Fatalf("X-Trace-Id = %q, want %q", got, validTraceID)
	}
	if meta := metaTraceID(t, rec); meta != validTraceID {
		t.Fatalf("meta.traceId = %q, want %q", meta, validTraceID)
	}
}

func TestMiddlewareStartsNewTraceOnMalformedTraceparent(t *testing.T) {
	cases := map[string]string{
		"wrong length":     "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7",
		"version ff":       "ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"zero trace id":    "00-00000000000000000000000000000000-00f067aa0ba902b7-01",
		"zero span id":     "00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",
		"uppercase hex":    "00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01",
		"not hex":          "00-zzf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"garbage":          "not a traceparent",
		"header injection": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01\r\nX-Evil: 1",
	}
	tel := newTelemetry(t)
	for name, parent := range cases {
		t.Run(name, func(t *testing.T) {
			rec := serve(tel, "/api/v1/items/7", parent)
			got := rec.Header().Get("X-Trace-Id")
			if !hex32.MatchString(got) || got == zeroTraceID || got == validTraceID {
				t.Fatalf("X-Trace-Id = %q, want a new non-zero 32-hex id", got)
			}
		})
	}
}

func TestMiddlewareSkipsHealthPaths(t *testing.T) {
	rec := serve(newTelemetry(t), "/health/live", validParent)
	if got := rec.Header().Get("X-Trace-Id"); got != "" {
		t.Fatalf("X-Trace-Id = %q on /health/live, want none", got)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestMiddlewareNamesSpanAfterMuxPattern(t *testing.T) {
	tel := newTelemetry(t)
	var span trace.Span
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		span = trace.SpanFromContext(r.Context())
	})
	rec := httptest.NewRecorder()
	tel.Middleware(mux).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/items/7", nil))
	readOnly, ok := span.(sdktrace.ReadOnlySpan)
	if !ok {
		t.Fatalf("span is %T, want an SDK span", span)
	}
	if got, want := readOnly.Name(), "GET /api/v1/items/{id}"; got != want {
		t.Fatalf("span name = %q, want %q", got, want)
	}
}

func TestTransportInjectsTraceparent(t *testing.T) {
	tel := newTelemetry(t)
	var received string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Get("Traceparent")
	}))
	defer upstream.Close()

	ctx, span := tel.StartJob(context.Background(), "job", validParent)
	defer span.End()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{Transport: tel.Transport(nil)}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	carrier := propagation.MapCarrier{"traceparent": received}
	got := trace.SpanContextFromContext(propagation.TraceContext{}.Extract(context.Background(), carrier))
	if got.TraceID().String() != validTraceID {
		t.Fatalf("outgoing traceparent %q does not continue trace %s", received, validTraceID)
	}
}

func TestTraceIDAndSpanIDEmptyWithoutSpan(t *testing.T) {
	if got := telemetry.TraceID(context.Background()); got != "" {
		t.Fatalf("TraceID = %q, want empty", got)
	}
	if got := telemetry.SpanID(context.Background()); got != "" {
		t.Fatalf("SpanID = %q, want empty", got)
	}
}

func TestSetupRequiresServiceName(t *testing.T) {
	if _, err := telemetry.Setup(context.Background(), telemetry.Config{}); err == nil {
		t.Fatal("Setup with no service name returned nil error")
	}
}

func TestMiddlewareSetsTraceHeaderBeforeNextWrites(t *testing.T) {
	tel := newTelemetry(t)
	var seen string
	plain := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = w.Header().Get("X-Trace-Id")
		w.WriteHeader(http.StatusNoContent)
	})
	rec := httptest.NewRecorder()
	tel.Middleware(plain).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))
	if !hex32.MatchString(seen) || rec.Header().Get("X-Trace-Id") != seen {
		t.Fatalf("header seen by next = %q, final = %q", seen, rec.Header().Get("X-Trace-Id"))
	}
}
