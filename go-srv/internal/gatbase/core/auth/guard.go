package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/internal/ctxkey"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
)

const activeTenantHeader = "X-Active-Tenant-Id"

// Guard enforces a Policy per route.
type Guard struct {
	sessions SessionResolver
	keys     APIKeyResolver
	scopes   *ScopeRegistry
	writer   *api.Writer
}

// NewGuard builds a Guard; a nil argument is a programmer error and panics.
func NewGuard(sessions SessionResolver, keys APIKeyResolver, scopes *ScopeRegistry, w *api.Writer) *Guard {
	if sessions == nil || keys == nil || scopes == nil || w == nil {
		panic("auth: NewGuard requires sessions, keys, scopes and a writer")
	}
	return &Guard{sessions: sessions, keys: keys, scopes: scopes, writer: w}
}

// Require wraps h with policy p. It runs after the mux match and panics on an
// invalid policy or a nil handler.
func (g *Guard) Require(p Policy, h http.Handler) http.Handler {
	if h == nil {
		panic("auth: Require needs a handler")
	}
	if err := p.Validate(g.scopes); err != nil {
		panic(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		span.SetAttributes(attribute.String("http.route", r.Pattern))
		if p.Kind == KindNone {
			h.ServeHTTP(w, r)
			return
		}
		principal, err := g.authorize(p, r)
		if err != nil {
			g.writer.Fail(w, r, err)
			return
		}
		span.SetAttributes(attribute.String("actor_id", principal.ActorID))
		if principal.TenantID != nil {
			span.SetAttributes(attribute.String("tenant_id", string(*principal.TenantID)))
		}
		ctx := context.WithValue(r.Context(), ctxkey.Principal, principal)
		if info, ok := ctxkey.RequestInfoFrom(ctx); ok {
			info.ActorID = principal.ActorID
			info.TenantID = ""
			if principal.TenantID != nil {
				info.TenantID = string(*principal.TenantID)
			}
		}
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func wireError(status int, code api.Code, message string) *api.Error {
	return api.NewError(status, code, message)
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// authorize runs steps 1 to 7 of the evaluation order.
func (g *Guard) authorize(p Policy, r *http.Request) (reqctx.Principal, error) {
	var zero reqctx.Principal
	acceptsSession := p.Kind != KindAPIKey
	acceptsKey := p.Kind.needsScope()

	hasSession := g.sessions.Present(r)
	hasKey := g.keys.Present(r)
	if hasSession && hasKey {
		return zero, wireError(http.StatusConflict, api.CodeCredentialConflict, "Send either a session or an API key, not both")
	}
	if acceptsSession && !hasSession && !(acceptsKey && hasKey) {
		return zero, wireError(http.StatusUnauthorized, api.CodeSessionInvalid, "Session is missing or invalid")
	}
	if p.Kind == KindAPIKey && !hasKey {
		return zero, wireError(http.StatusUnauthorized, api.CodeAPIKeyInvalidOrRevoked, "API key is missing, invalid or revoked")
	}

	if hasKey {
		return g.authorizeKey(p, r)
	}
	return g.authorizeSession(p, r)
}

func (g *Guard) authorizeKey(p Policy, r *http.Request) (reqctx.Principal, error) {
	var zero reqctx.Principal
	principal, err := g.keys.Resolve(r.Context(), r)
	if err != nil {
		return zero, keyError(err)
	}
	if !holdsScope(principal.Scopes, p.Scope) {
		return zero, wireError(http.StatusForbidden, api.CodeInsufficientScope, "API key lacks the required scope")
	}
	return principal, nil
}

// keyError passes a rejected key (401 or 403 *api.Error) through; anything else
// is operational and answered 500 by the writer.
func keyError(err error) error {
	var apiErr *api.Error
	isRejection := errors.As(err, &apiErr) && apiErr != nil &&
		(apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden)
	if isRejection {
		return apiErr
	}
	// Strip the wire error so the writer cannot pass an unexpected status through.
	return fmt.Errorf("auth: api key resolver failed: %s", err.Error())
}

func holdsScope(scopes []string, scope string) bool {
	for _, s := range scopes {
		if s == scope {
			return true
		}
	}
	return false
}

func (g *Guard) authorizeSession(p Policy, r *http.Request) (reqctx.Principal, error) {
	var zero reqctx.Principal
	result, err := g.sessions.Resolve(r)
	if err != nil {
		// Strip the wire error: a session resolver fails only operationally (500).
		return zero, fmt.Errorf("auth: session resolver failed: %s", err.Error())
	}
	allowsExpired := p.Kind == KindSessionAllowExpired
	switch {
	case result.State == SessionValid:
	case result.State == SessionExpired && allowsExpired:
	case result.State == SessionExpired || result.State == SessionInvalid:
		return zero, wireError(http.StatusUnauthorized, api.CodeSessionInvalid, "Session is missing or invalid")
	default:
		return zero, errors.New("auth: session resolver returned an unknown state")
	}

	principal := result.Principal
	principal.SessionExpired = result.State == SessionExpired
	mutating := !isSafeMethod(r.Method)

	if mutating && !result.CSRFValid {
		return zero, wireError(http.StatusForbidden, api.CodeCSRFTokenInvalid, "CSRF token is missing or invalid")
	}
	if mutating && !allowsExpired && activeTenantChanged(r, principal) {
		return zero, wireError(http.StatusConflict, api.CodeActiveTenantChanged, "Active tenant changed; reload and retry")
	}
	if p.PlatformOperator && !principal.IsPlatformOperator {
		return zero, wireError(http.StatusForbidden, api.CodePlatformOperatorNeeded, "Platform operator required")
	}
	if p.MinRole != "" {
		if principal.TenantID == nil {
			return zero, wireError(http.StatusForbidden, api.CodeActiveTenantRequired, "An active tenant is required")
		}
		if !principal.Role.AtLeast(p.MinRole) {
			return zero, wireError(http.StatusForbidden, api.CodeInsufficientRole, "Insufficient role")
		}
	}
	return principal, nil
}

// activeTenantChanged compares X-Active-Tenant-Id, when sent, with the
// principal's tenant; an empty value means no tenant.
func activeTenantChanged(r *http.Request, principal reqctx.Principal) bool {
	values := r.Header.Values(activeTenantHeader)
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if !sameTenant(value, principal.TenantID) {
			return true
		}
	}
	return false
}

func sameTenant(header string, tenant *reqctx.TenantID) bool {
	if header == "" {
		return tenant == nil
	}
	if tenant == nil {
		return false
	}
	parsed, ok := reqctx.ParseTenantID(header)
	return ok && parsed == *tenant
}
