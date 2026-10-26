package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/auth"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
)

// spanAttributes runs one guarded request inside a recording span and returns
// the span's attributes by key and the response status.
func spanAttributes(t *testing.T, h *harness, p auth.Policy, headers map[string]string) (map[string]string, int) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/things", h.guard.Require(p, h.handler()))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/things", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	ctx, span := provider.Tracer("test").Start(req.Context(), "request")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req.WithContext(ctx))
	span.End()

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d", len(ended))
	}
	out := map[string]string{}
	for _, kv := range ended[0].Attributes() {
		if kv.Value.Type() == attribute.STRING {
			out[string(kv.Key)] = kv.Value.AsString()
		}
	}
	return out, rec.Code
}

func TestGuardSetsSpanAttributesForAuthorizedCaller(t *testing.T) {
	h := newHarness(t, validSession(memberPrincipal(reqctx.RoleMember)), &fakeKey{})

	got, _ := spanAttributes(t, h, auth.Policy{Kind: auth.KindSession}, nil)

	if got["http.route"] != "GET /api/v1/things" || got["actor_id"] != "acct-1" || got["tenant_id"] != tenantA {
		t.Fatalf("attributes = %v", got)
	}
}

func TestGuardSetsOnlyRouteOnPublicRoutes(t *testing.T) {
	h := newHarness(t, &fakeSession{}, &fakeKey{})

	got, _ := spanAttributes(t, h, auth.Public, nil)

	if got["http.route"] != "GET /api/v1/things" {
		t.Fatalf("attributes = %v", got)
	}
	if _, has := got["actor_id"]; has {
		t.Fatalf("public route must not carry an actor: %v", got)
	}
}

func TestGuardLeavesCallerAttributesOffRejectedRequests(t *testing.T) {
	h := newHarness(t, &fakeSession{present: false}, &fakeKey{})

	got, status := spanAttributes(t, h, auth.Policy{Kind: auth.KindSession}, nil)

	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", status)
	}
	if _, hasActor := got["actor_id"]; hasActor {
		t.Fatalf("rejected request carries an actor: %v", got)
	}
	if _, hasTenant := got["tenant_id"]; hasTenant {
		t.Fatalf("rejected request carries a tenant: %v", got)
	}
}
