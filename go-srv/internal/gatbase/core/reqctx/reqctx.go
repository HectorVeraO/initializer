// Package reqctx is the read side of the per-request caller context: the
// resolved Principal, its active tenant and role. It exports no setter; only
// the guard of core installs a principal.
package reqctx

import (
	"context"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/internal/ctxkey"
)

// TenantID is a tenant UUID in canonical lowercase text.
type TenantID string

// AccountID is an account UUID in canonical lowercase text.
type AccountID string

// ParseTenantID returns the canonical form of s, or false.
func ParseTenantID(s string) (TenantID, bool) {
	canonical, ok := api.ParseUUID(s)
	if !ok {
		return "", false
	}
	return TenantID(canonical), true
}

// Role is a tenant role; the set is closed.
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

func (r Role) rank() int {
	switch r {
	case RoleAdmin:
		return 3
	case RoleMember:
		return 2
	case RoleViewer:
		return 1
	}
	return 0
}

// Valid reports whether r is one of the closed set of roles.
func (r Role) Valid() bool {
	return r.rank() > 0
}

// AtLeast orders admin > member > viewer; it is false when r or min is invalid.
func (r Role) AtLeast(min Role) bool {
	bothValid := r.Valid() && min.Valid()
	return bothValid && r.rank() >= min.rank()
}

// ActorKind is the kind of caller.
type ActorKind string

const (
	ActorAccount ActorKind = "account"
	ActorAPIKey  ActorKind = "api_key"
)

// Principal is the resolved caller of one request.
type Principal struct {
	Kind ActorKind
	// ActorID is the account UUID, or "apikey:<keyPrefix>".
	ActorID string
	// AccountID is set for ActorAccount only.
	AccountID AccountID
	// TenantID is the active tenant; nil when none.
	TenantID *TenantID
	// TenantSlug is the slug of TenantID; empty when none.
	TenantSlug string
	// Role is set for ActorAccount with an active tenant; empty otherwise.
	Role               Role
	IsPlatformOperator bool
	// Scopes is set for ActorAPIKey only.
	Scopes []string
	// SessionExpired is true only on routes that allow an expired session.
	SessionExpired bool
}

// PrincipalFrom returns the principal the guard installed; false on public
// routes and outside a guarded handler.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(ctxkey.Principal).(Principal)
	return principal, ok
}

// TenantFrom returns the principal's active tenant, if any.
func TenantFrom(ctx context.Context) (TenantID, bool) {
	principal, ok := PrincipalFrom(ctx)
	hasTenant := ok && principal.TenantID != nil
	if !hasTenant {
		return "", false
	}
	return *principal.TenantID, true
}
