package access

import (
	"context"
	"net/http"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

// SessionState is the verdict of a SessionResolver.
type SessionState uint8

const (
	// SessionValid is a verified, unexpired session.
	SessionValid SessionState = iota + 1
	// SessionExpired is a session whose signature verifies but exp has passed.
	SessionExpired
	// SessionInvalid is malformed, badly signed or signed with an unknown key id.
	SessionInvalid
)

// SessionResult is what a SessionResolver learned from one request.
type SessionResult struct {
	State SessionState
	// Principal is set for SessionValid and SessionExpired.
	Principal reqctx.Principal
	// CSRFValid reports whether X-XSRF-TOKEN matches the value bound to the token.
	CSRFValid bool
}

// SessionResolver is the stateless session port; Resolve does no database or
// IdP I/O.
type SessionResolver interface {
	Present(r *http.Request) bool
	// Resolve returns an error for operational failures only (answered 500).
	Resolve(r *http.Request) (SessionResult, error)
}

// APIKeyResolver is the API key port.
type APIKeyResolver interface {
	Present(r *http.Request) bool
	// A rejected key returns an *api.Error: 401 API_KEY_INVALID_OR_REVOKED or 403
	// TENANT_SUSPENDED. Any other error is operational.
	Resolve(ctx context.Context, r *http.Request) (reqctx.Principal, error)
}

// TenantDirectory answers whether a tenant exists.
type TenantDirectory interface {
	// TenantExists reports existence whatever the tenant's status.
	TenantExists(ctx context.Context, id reqctx.TenantID) (bool, error)
}

var (
	// NoSessions is the SessionResolver of an instance without sessions.
	NoSessions SessionResolver = noSessions{}
	// NoAPIKeys is the APIKeyResolver of an instance without API keys.
	NoAPIKeys APIKeyResolver = noAPIKeys{}
	// NoTenants is the TenantDirectory of an instance without tenants.
	NoTenants TenantDirectory = noTenants{}
)

type noSessions struct{}

func (noSessions) Present(*http.Request) bool { return false }

func (noSessions) Resolve(*http.Request) (SessionResult, error) {
	return SessionResult{State: SessionInvalid}, nil
}

type noAPIKeys struct{}

func (noAPIKeys) Present(*http.Request) bool { return false }

func (noAPIKeys) Resolve(context.Context, *http.Request) (reqctx.Principal, error) {
	return reqctx.Principal{}, nil
}

type noTenants struct{}

func (noTenants) TenantExists(context.Context, reqctx.TenantID) (bool, error) {
	return false, nil
}
