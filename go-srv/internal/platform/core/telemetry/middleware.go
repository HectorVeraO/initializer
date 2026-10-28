package telemetry

import (
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/internal/ctxkey"
)

// TraceHeader carries the trace id of a traced request in every response.
const TraceHeader = "X-Trace-Id"

const healthPrefix = "/health/"

// Middleware is chain steps 1 and 2 (used by core only): a server span per
// request except /health/*, continuing an incoming traceparent or starting a
// trace, then X-Trace-Id and the Writer's trace-id context value.
func (t *Telemetry) Middleware(next http.Handler) http.Handler {
	traced := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		traceID := TraceID(r.Context())
		if traceID == "" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set(TraceHeader, traceID)
		ctx := ctxkey.WithTraceID(r.Context(), traceID)
		routed := r.WithContext(ctx)
		next.ServeHTTP(w, routed)
		// The mux sets Pattern on the request it receives (routed); the span-name
		// formatter ran before routing and saw none.
		if routed.Pattern != "" {
			span.SetName(routed.Pattern)
		}
	})
	return otelhttp.NewHandler(traced, "http.server",
		otelhttp.WithTracerProvider(t.provider),
		otelhttp.WithPropagators(t.propagator),
		otelhttp.WithFilter(func(r *http.Request) bool {
			return !isHealthPath(r.URL.Path)
		}),
		otelhttp.WithSpanNameFormatter(spanName),
	)
}

func isHealthPath(path string) bool {
	return strings.HasPrefix(path, healthPrefix)
}

func spanName(_ string, r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	return r.Method
}
