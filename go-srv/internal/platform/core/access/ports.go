package access

import (
	"context"
	"errors"
	"net/http"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
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

// TenantStatus is the lifecycle state of a tenant; the set is closed.
type TenantStatus string

const (
	TenantActive    TenantStatus = "active"
	TenantSuspended TenantStatus = "suspended"
)

// TenantRef is what a module other than the tenant owner may know about a
// tenant.
type TenantRef struct {
	ID     reqctx.TenantID
	Slug   string
	Name   string
	Status TenantStatus
}

// TenantGrant is an account's verified access to one tenant.
type TenantGrant struct {
	Tenant TenantRef
	Role   reqctx.Role
}

// MembershipRef is one active membership as the session view shows it:
// { id, tenant: { id, slug, name, status }, role, status }.
type MembershipRef struct {
	ID     string
	Tenant TenantRef
	Role   reqctx.Role
	// Status is always "active": Memberships lists active memberships only.
	Status string
}

// TenantAccess is the tenant half of a session. The session owner consults
// it on login, refresh, switch and the session view, after it has checked
// the account itself (ACCOUNT_LOCKED_OR_DISABLED is never answered here).
//
// A rejection is a 403 *api.Error with code TENANT_ACCESS_DENIED,
// TENANT_SUSPENDED or MEMBERSHIP_REVOKED; any other error is operational
// (answered 500).
type TenantAccess interface {
	// Select picks the tenant a new or tenantless session starts on: the
	// tenant of the latest Remember call for account when Grant passes for
	// it; else the oldest active membership of an active tenant (membership
	// created_at, then membership id, ascending); else nil. It never returns
	// a rejection: a rejected remembered tenant falls through to the oldest.
	Select(ctx context.Context, account reqctx.AccountID) (*TenantGrant, error)
	// Grant checks account on tenant (refresh, switch). Checks in order: no
	// membership, unknown tenant or an id that is not a UUID -> 403
	// TENANT_ACCESS_DENIED (a non-member learns nothing about the tenant);
	// suspended tenant -> 403 TENANT_SUSPENDED; membership not active -> 403
	// MEMBERSHIP_REVOKED.
	Grant(ctx context.Context, account reqctx.AccountID, tenant reqctx.TenantID) (TenantGrant, error)
	// Remember records tenant as the account's last selected tenant for
	// Select. The caller has already called Grant; Remember does not check
	// access. Ids that are not UUIDs are an operational error.
	Remember(ctx context.Context, account reqctx.AccountID, tenant reqctx.TenantID) error
	// Memberships lists the account's active memberships, whatever the
	// tenant's status, ordered by tenant slug then membership id. Never nil;
	// empty for an unknown account or an id that is not a UUID.
	Memberships(ctx context.Context, account reqctx.AccountID) ([]MembershipRef, error)
}

// TenantDirectory answers what a tenant is, whatever its status.
type TenantDirectory interface {
	// TenantExists reports existence whatever the tenant's status.
	TenantExists(ctx context.Context, id reqctx.TenantID) (bool, error)
	// Tenant returns the tenant and true, or false when no tenant has id or
	// id is not a UUID. The error is operational only.
	Tenant(ctx context.Context, id reqctx.TenantID) (TenantRef, bool, error)
}

var (
	// NoSessions is the SessionResolver of an instance without sessions.
	NoSessions SessionResolver = noSessions{}
	// NoAPIKeys is the APIKeyResolver of an instance without API keys.
	NoAPIKeys APIKeyResolver = noAPIKeys{}
	// NoTenants is the TenantDirectory of an instance without tenants:
	// TenantExists is (false, nil); Tenant is (TenantRef{}, false, nil).
	NoTenants TenantDirectory = noTenants{}
	// NoTenantAccess is the TenantAccess of an instance without tenancy:
	// Select is (nil, nil); Grant is 403 TENANT_ACCESS_DENIED for every id;
	// Remember returns ErrNoTenancy (unreachable: Grant failed first);
	// Memberships is an empty, non-nil slice.
	NoTenantAccess TenantAccess = noTenantAccess{}
)

// ErrNoTenancy is the operational error of NoTenantAccess.Remember.
var ErrNoTenancy = errors.New("access: no module provides tenant access")

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

func (noTenants) Tenant(context.Context, reqctx.TenantID) (TenantRef, bool, error) {
	return TenantRef{}, false, nil
}

type noTenantAccess struct{}

func (noTenantAccess) Select(context.Context, reqctx.AccountID) (*TenantGrant, error) {
	return nil, nil
}

func (noTenantAccess) Grant(context.Context, reqctx.AccountID, reqctx.TenantID) (TenantGrant, error) {
	denied := api.NewError(http.StatusForbidden, api.CodeTenantAccessDenied, "The account has no access to this tenant.")
	return TenantGrant{}, denied
}

func (noTenantAccess) Remember(context.Context, reqctx.AccountID, reqctx.TenantID) error {
	return ErrNoTenancy
}

func (noTenantAccess) Memberships(context.Context, reqctx.AccountID) ([]MembershipRef, error) {
	return []MembershipRef{}, nil
}
