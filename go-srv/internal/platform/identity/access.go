package identity

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

const (
	statusActive    = "active"
	statusSuspended = "suspended"
)

// Grant is what an account may do once checkAccess passes. Role and
// TenantSlug are empty when no tenant was checked.
type Grant struct {
	Role             reqctx.Role
	PlatformOperator bool
	TenantSlug       string
}

// access holds the status checks and tenant selection the session routes
// call. It is package-internal: no other module calls it. Sessions are
// stateless: each call reads the current rows.
type access struct {
	pool           *pgxpool.Pool
	clock          clock.Clock
	operatorEmails []string
}

// newAccess builds the checks over pool. operatorEmails are the bare,
// lower-cased APP_PLATFORM_OPERATOR_EMAILS entries.
func newAccess(pool *pgxpool.Pool, clk clock.Clock, operatorEmails []string) *access {
	return &access{pool: pool, clock: clk, operatorEmails: slices.Clone(operatorEmails)}
}

func forbidden(code api.Code, message string) *api.Error {
	return api.NewError(http.StatusForbidden, code, message)
}

// accountState is the slice of iam_accounts the checks read.
type accountState struct {
	active             bool
	platformOperator   bool
	lastActiveTenantID *string
}

// accountState reads the account; a missing row or an id that is not a UUID
// is an inactive state.
func (a *access) accountState(ctx context.Context, accountID string) (accountState, error) {
	canonical, ok := canonicalID(accountID)
	if !ok {
		return accountState{}, nil
	}
	var status string
	var state accountState
	err := a.pool.QueryRow(ctx,
		"SELECT status, is_platform_operator, last_active_tenant_id::text FROM iam_accounts WHERE id = $1::uuid",
		canonical).Scan(&status, &state.platformOperator, &state.lastActiveTenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return accountState{}, nil
	}
	if err != nil {
		return accountState{}, fmt.Errorf("identity: read account state: %w", err)
	}
	state.active = status == statusActive
	return state, nil
}

// checkAccess runs the status checks in order and returns the first failure
// as a 403 *api.Error. Any other error is operational. A nil tenantID checks
// the account only.
func (a *access) checkAccess(ctx context.Context, accountID string, tenantID *string) (Grant, error) {
	state, err := a.accountState(ctx, accountID)
	if err != nil {
		return Grant{}, err
	}
	if !state.active {
		return Grant{}, forbidden(CodeAccountLockedOrDisabled, "The account is locked or disabled.")
	}
	if tenantID == nil {
		return Grant{PlatformOperator: state.platformOperator}, nil
	}
	return a.tenantGrant(ctx, accountID, *tenantID, state.platformOperator)
}

// tenantGrant is steps 3 to 6 of checkAccess: one query joins the membership
// to its tenant, so a non-member learns nothing about the tenant.
func (a *access) tenantGrant(ctx context.Context, accountID, tenantID string, platformOperator bool) (Grant, error) {
	denied := forbidden(CodeTenantAccessDenied, "The account has no access to this tenant.")
	tenant, tenantOK := canonicalID(tenantID)
	account, accountOK := canonicalID(accountID)
	if !tenantOK || !accountOK {
		return Grant{}, denied
	}
	var tenantStatus, slug, membershipStatus, role string
	err := a.pool.QueryRow(ctx,
		`SELECT t.status, t.slug, m.status, m.role FROM ten_tenants t
JOIN ten_memberships m ON m.tenant_id = t.id AND m.account_id = $2::uuid WHERE t.id = $1::uuid`,
		tenant, account).Scan(&tenantStatus, &slug, &membershipStatus, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return Grant{}, denied
	}
	if err != nil {
		return Grant{}, fmt.Errorf("identity: read membership: %w", err)
	}
	if tenantStatus == statusSuspended {
		return Grant{}, forbidden(CodeTenantSuspended, "The tenant is suspended.")
	}
	if membershipStatus != statusActive {
		message := "The membership was revoked."
		if membershipStatus == statusSuspended {
			message = "The membership is suspended."
		}
		return Grant{}, forbidden(CodeMembershipRevoked, message)
	}
	return Grant{Role: reqctx.Role(role), PlatformOperator: platformOperator, TenantSlug: slug}, nil
}

func isAccessDenial(err error) bool {
	var apiErr *api.Error
	isAPIError := errors.As(err, &apiErr)
	return isAPIError && apiErr.Status == http.StatusForbidden
}

// selectTenant picks the tenant a new session starts on: the account's last
// active tenant when checkAccess passes for it, else its oldest active
// membership of an active tenant, else none. It never returns a 403; an
// account that is not active gets no tenant.
func (a *access) selectTenant(ctx context.Context, accountID string) (*string, Grant, error) {
	state, err := a.accountState(ctx, accountID)
	if err != nil {
		return nil, Grant{}, err
	}
	if !state.active {
		return nil, Grant{}, nil
	}
	if state.lastActiveTenantID != nil {
		grant, err := a.tenantGrant(ctx, accountID, *state.lastActiveTenantID, state.platformOperator)
		if err == nil {
			return state.lastActiveTenantID, grant, nil
		}
		if !isAccessDenial(err) {
			return nil, Grant{}, err
		}
	}
	return a.oldestActiveTenant(ctx, accountID, state.platformOperator)
}

// oldestActiveTenant is the fallback of selectTenant.
func (a *access) oldestActiveTenant(ctx context.Context, accountID string, platformOperator bool) (*string, Grant, error) {
	account, _ := canonicalID(accountID)
	var tenantID, role string
	grant := Grant{PlatformOperator: platformOperator}
	err := a.pool.QueryRow(ctx,
		`SELECT t.id::text, t.slug, m.role FROM ten_memberships m
JOIN ten_tenants t ON t.id = m.tenant_id
WHERE m.account_id = $1::uuid AND m.status = 'active' AND t.status = 'active'
ORDER BY m.created_at, m.id LIMIT 1`, account).Scan(&tenantID, &grant.TenantSlug, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grant, nil
	}
	if err != nil {
		return nil, Grant{}, fmt.Errorf("identity: select tenant: %w", err)
	}
	grant.Role = reqctx.Role(role)
	return &tenantID, grant, nil
}

// linkIdentity returns the account of a Kratos identity. The first sight
// creates it (email lower-cased, operator flag when the email is listed); a
// later call refreshes the stored email when it changed.
func (a *access) linkIdentity(ctx context.Context, kratosIdentityID, email string) (Account, error) {
	identity, ok := canonicalID(kratosIdentityID)
	if !ok {
		return Account{}, errors.New("identity: link identity: identity id is not a UUID")
	}
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" || len(normalized) > maxEmailLength {
		return Account{}, errors.New("identity: link identity: email is empty or too long")
	}
	isOperator := slices.Contains(a.operatorEmails, normalized)
	now := a.clock.Now()
	row := a.pool.QueryRow(ctx,
		`INSERT INTO iam_accounts (kratos_identity_id, email, is_platform_operator, created_at, updated_at)
VALUES ($1::uuid, $2, $3, $4, $4)
ON CONFLICT (kratos_identity_id) DO UPDATE
SET email = EXCLUDED.email,
    updated_at = CASE WHEN iam_accounts.email IS DISTINCT FROM EXCLUDED.email
                      THEN EXCLUDED.updated_at ELSE iam_accounts.updated_at END
RETURNING `+accountColumns, identity, normalized, isOperator, now)
	account, err := scanAccount(row)
	if err != nil {
		return Account{}, mapError("link identity", err)
	}
	return account, nil
}

// setLastActiveTenant records the tenant a session last used. It does not
// check access: callers run checkAccess first.
func (a *access) setLastActiveTenant(ctx context.Context, accountID, tenantID string) error {
	account, accountOK := canonicalID(accountID)
	tenant, tenantOK := canonicalID(tenantID)
	if !accountOK || !tenantOK {
		return errors.New("identity: set last active tenant: ids must be UUIDs")
	}
	_, err := a.pool.Exec(ctx,
		"UPDATE iam_accounts SET last_active_tenant_id = $2::uuid, updated_at = $3 WHERE id = $1::uuid",
		account, tenant, a.clock.Now())
	if err != nil {
		return mapError("set last active tenant", err)
	}
	return nil
}
