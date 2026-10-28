package telemetry_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/telemetry"
)

func hex16(s string) bool {
	return len(s) == 16 && hex32.MatchString(s+s)
}

func TestStartJobContinuesValidTraceparent(t *testing.T) {
	ctx, span := newTelemetry(t).StartJob(context.Background(), "send_email", validParent)
	defer span.End()
	if got := telemetry.TraceID(ctx); got != validTraceID {
		t.Fatalf("TraceID = %q, want %q", got, validTraceID)
	}
	if got := telemetry.SpanID(ctx); !hex16(got) || got == "00f067aa0ba902b7" {
		t.Fatalf("SpanID = %q, want a new 16-hex id", got)
	}
}

func TestStartJobOpensNewTraceOnInvalidTraceparent(t *testing.T) {
	tel := newTelemetry(t)
	invalid := []string{
		"",
		"garbage",
		"ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01",
	}
	for _, bad := range invalid {
		ctx, span := tel.StartJob(context.Background(), "job", bad)
		got := telemetry.TraceID(ctx)
		span.End()
		if !hex32.MatchString(got) || got == zeroTraceID || got == validTraceID {
			t.Fatalf("traceparent %q: TraceID = %q, want a new non-zero id", bad, got)
		}
	}
}

func TestStartJobIgnoresSpanAlreadyInContextWhenTraceparentInvalid(t *testing.T) {
	tel := newTelemetry(t)
	parentCtx, parent := tel.StartJob(context.Background(), "parent", "")
	defer parent.End()
	ctx, span := tel.StartJob(parentCtx, "job", "garbage")
	defer span.End()
	if telemetry.TraceID(ctx) == telemetry.TraceID(parentCtx) {
		t.Fatal("job joined the trace of an unrelated span in ctx")
	}
}

func FuzzStartJob(f *testing.F) {
	f.Add(validParent)
	f.Add("")
	f.Add("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7")
	f.Add("ff-00000000000000000000000000000000-0000000000000000-00")
	tel := newTelemetry(f)
	f.Fuzz(func(t *testing.T, traceparent string) {
		ctx, span := tel.StartJob(context.Background(), "job", traceparent)
		defer span.End()
		traceID, spanID := telemetry.TraceID(ctx), telemetry.SpanID(ctx)
		if !hex32.MatchString(traceID) || traceID == zeroTraceID || !hex16(spanID) {
			t.Fatalf("traceparent %q gave trace %q span %q", traceparent, traceID, spanID)
		}
	})
}

func FuzzMiddlewareTraceparent(f *testing.F) {
	f.Add(validParent)
	f.Add("")
	f.Add("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01\r\nX-Evil: 1")
	tel := newTelemetry(f)
	f.Fuzz(func(t *testing.T, traceparent string) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/items/7", nil)
		req.Header["Traceparent"] = []string{traceparent}
		rec := httptest.NewRecorder()
		tel.Middleware(echoHandler()).ServeHTTP(rec, req)
		got := rec.Header().Get("X-Trace-Id")
		if !hex32.MatchString(got) || got == zeroTraceID {
			t.Fatalf("traceparent %q gave X-Trace-Id %q", traceparent, got)
		}
		if rec.Header().Get("X-Evil") != "" {
			t.Fatal("an incoming header value reached the response")
		}
	})
}
