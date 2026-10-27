package core

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/internal/ctxkey"
)

// buildChain assembles the request chain, outermost first: telemetry (span and
// X-Trace-Id), recover, access log, then for /api/v1/ the response headers,
// the drain gate and the cross-origin check, then the mux. The headers sit
// before the gate and the check so their 503 and 403 envelopes carry them too.
func buildChain(a *assembly, writer *api.Writer) (http.Handler, error) {
	cop := http.NewCrossOriginProtection()
	origin := a.baseURL.Scheme + "://" + a.baseURL.Host
	err := cop.AddTrustedOrigin(origin)
	if err != nil {
		return nil, fmt.Errorf("core: trusted origin of GATBASE_PUBLIC_BASE_URL: %w", err)
	}
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writer.Fail(w, r, api.NewError(http.StatusForbidden, api.CodeCSRFTokenInvalid, "Cross-origin request rejected"))
	}))

	routed := routeCapture(a.mux)
	apiStack := apiHeaders(drainGate(a, writer, cop.Handler(routed)))
	dispatch := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			apiStack.ServeHTTP(w, r)
			return
		}
		routed.ServeHTTP(w, r)
	})
	inner := recoverPanics(a.logger, writer, accessLog(a.logger, dispatch))
	return a.telemetry.Middleware(inner), nil
}

// trackedWriter records whether and with which status the response started.
type trackedWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (t *trackedWriter) WriteHeader(code int) {
	isInformational := code >= 100 && code < 200 && code != http.StatusSwitchingProtocols
	if !isInformational && !t.wrote {
		t.wrote = true
		t.status = code
	}
	t.ResponseWriter.WriteHeader(code)
}

func (t *trackedWriter) Write(b []byte) (int, error) {
	if !t.wrote {
		t.wrote = true
		t.status = http.StatusOK
	}
	return t.ResponseWriter.Write(b)
}

// Flush lets handlers stream; it starts the response like Write does.
func (t *trackedWriter) Flush() {
	if !t.wrote {
		t.wrote = true
		t.status = http.StatusOK
	}
	_ = http.NewResponseController(t.ResponseWriter).Flush()
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (t *trackedWriter) Unwrap() http.ResponseWriter {
	return t.ResponseWriter
}

// recoverPanics is chain step 3: it installs the request-info holder the
// guard fills (accessLog reuses it) so panic_recovered logs the resolved caller.
func recoverPanics(logger *slog.Logger, writer *api.Writer, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tracked := &trackedWriter{ResponseWriter: w}
		info := &ctxkey.RequestInfo{}
		r = r.WithContext(ctxkey.WithRequestInfo(r.Context(), info))
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			if recovered == http.ErrAbortHandler {
				panic(recovered)
			}
			attrs := []slog.Attr{
				slog.String("panic", fmt.Sprint(recovered)),
				slog.String("stack", string(debug.Stack())),
			}
			if info.TenantID != "" {
				attrs = append(attrs, slog.String("tenant_id", info.TenantID))
			}
			if info.ActorID != "" {
				attrs = append(attrs, slog.String("actor_id", info.ActorID))
			}
			logger.LogAttrs(r.Context(), slog.LevelError, "panic_recovered", attrs...)
			if tracked.wrote {
				panic(http.ErrAbortHandler)
			}
			if isAPIPath(r.URL.Path) {
				writer.Fail(tracked, r, api.NewError(http.StatusInternalServerError, api.CodeInternalError, "Internal error"))
				return
			}
			http.Error(tracked, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}()
		next.ServeHTTP(tracked, r)
	})
}

// accessLog is chain step 4: it reuses the request-info holder recoverPanics
// installed (or installs one) for the guard to fill, and logs one
// http_request_completed per request, also when a panic passes through (the
// recover step then answers 500).
func accessLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tracked, ok := w.(*trackedWriter)
		if !ok {
			tracked = &trackedWriter{ResponseWriter: w}
		}
		info, hasInfo := ctxkey.RequestInfoFrom(r.Context())
		withInfo := r
		if !hasInfo {
			info = &ctxkey.RequestInfo{}
			withInfo = r.WithContext(ctxkey.WithRequestInfo(r.Context(), info))
		}
		start := time.Now()
		completed := false
		defer func() {
			status := tracked.status
			if !tracked.wrote {
				status = http.StatusOK
				if !completed {
					status = http.StatusInternalServerError
				}
			}
			route := info.Route
			if route == "" {
				route = "unmatched"
			}
			level := slog.LevelInfo
			if strings.HasPrefix(r.URL.Path, healthPrefix) {
				level = slog.LevelDebug
			}
			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("route", route),
				slog.Int("status_code", status),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			}
			if info.TenantID != "" {
				attrs = append(attrs, slog.String("tenant_id", info.TenantID))
			}
			if info.ActorID != "" {
				attrs = append(attrs, slog.String("actor_id", info.ActorID))
			}
			logger.LogAttrs(r.Context(), level, "http_request_completed", attrs...)
		}()
		next.ServeHTTP(tracked, withInfo)
		completed = true
	})
}

// routeCapture records the matched pattern in the request-info holder and as
// the span name; the mux sets r.Pattern on the request it receives.
func routeCapture(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if r.Pattern == "" {
				return
			}
			info, ok := ctxkey.RequestInfoFrom(r.Context())
			if ok {
				info.Route = r.Pattern
			}
			trace.SpanFromContext(r.Context()).SetName(r.Pattern)
		}()
		mux.ServeHTTP(w, r)
	})
}

// apiHeaders sets the /api/v1 response headers.
func apiHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Cache-Control", "no-store")
		header.Add("Vary", "Cookie")
		next.ServeHTTP(w, r)
	})
}

// drainGate: not serving (draining) answers 503.
func drainGate(a *assembly, writer *api.Writer, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.readiness.Serving() {
			writer.Fail(w, r, api.NewError(http.StatusServiceUnavailable, api.CodeServiceUnavailable, "Service unavailable"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
