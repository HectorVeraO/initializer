package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

// tenantAccess is core's coreaccess.TenantAccess over ten_memberships and
// ten_tenants. The remembered tenant lives in iam_accounts.last_active_tenant_id.
type tenantAccess struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

var _ coreaccess.TenantAccess = (*tenantAccess)(nil)

func newTenantAccess(pool *pgxpool.Pool, clk clock.Clock) *tenantAccess {
	return &tenantAccess{pool: pool, clock: clk}
}

// Grant checks the membership of account on tenant: one query joins the
// membership to its tenant, so a non-member learns nothing about the tenant.
func (a *tenantAccess) Grant(ctx context.Context, account reqctx.AccountID, tenant reqctx.TenantID) (coreaccess.TenantGrant, error) {
	denied := forbidden(api.CodeTenantAccessDenied, "The account has no access to this tenant.")
	tenantID, tenantOK := canonicalID(string(tenant))
	accountID, accountOK := canonicalID(string(account))
	if !tenantOK || !accountOK {
		return coreaccess.TenantGrant{}, denied
	}
	var ref coreaccess.TenantRef
	var membershipStatus, role string
	err := a.pool.QueryRow(ctx,
		`SELECT t.id::text, t.slug, t.name, t.status, m.status, m.role FROM ten_tenants t
JOIN ten_memberships m ON m.tenant_id = t.id AND m.account_id = $2::uuid WHERE t.id = $1::uuid`,
		tenantID, accountID).Scan(&ref.ID, &ref.Slug, &ref.Name, &ref.Status, &membershipStatus, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return coreaccess.TenantGrant{}, denied
	}
	if err != nil {
		return coreaccess.TenantGrant{}, fmt.Errorf("identity: read membership: %w", err)
	}
	if ref.Status == coreaccess.TenantSuspended {
		return coreaccess.TenantGrant{}, forbidden(api.CodeTenantSuspended, "The tenant is suspended.")
	}
	if membershipStatus != statusActive {
		message := "The membership was revoked."
		if membershipStatus == statusSuspended {
			message = "The membership is suspended."
		}
		return coreaccess.TenantGrant{}, forbidden(api.CodeMembershipRevoked, message)
	}
	return coreaccess.TenantGrant{Tenant: ref, Role: reqctx.Role(role)}, nil
}

// Select returns the remembered tenant when Grant passes for it, else the
// oldest active membership of an active tenant, else nil. The account step
// (a locked or disabled account) is the caller's.
func (a *tenantAccess) Select(ctx context.Context, account reqctx.AccountID) (*coreaccess.TenantGrant, error) {
	accountID, isUUID := canonicalID(string(account))
	if !isUUID {
		return nil, nil
	}
	var remembered *string
	err := a.pool.QueryRow(ctx,
		"SELECT last_active_tenant_id::text FROM iam_accounts WHERE id = $1::uuid", accountID).Scan(&remembered)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("identity: read last active tenant: %w", err)
	}
	if remembered != nil {
		grant, err := a.Grant(ctx, reqctx.AccountID(accountID), reqctx.TenantID(*remembered))
		if err == nil {
			return &grant, nil
		}
		if !isAccessDenial(err) {
			return nil, err
		}
	}
	return a.oldestActive(ctx, accountID)
}

// oldestActive is the fallback of Select.
func (a *tenantAccess) oldestActive(ctx context.Context, accountID string) (*coreaccess.TenantGrant, error) {
	var grant coreaccess.TenantGrant
	var role string
	err := a.pool.QueryRow(ctx,
		`SELECT t.id::text, t.slug, t.name, t.status, m.role FROM ten_memberships m
JOIN ten_tenants t ON t.id = m.tenant_id
WHERE m.account_id = $1::uuid AND m.status = 'active' AND t.status = 'active'
ORDER BY m.created_at, m.id LIMIT 1`, accountID).Scan(
		&grant.Tenant.ID, &grant.Tenant.Slug, &grant.Tenant.Name, &grant.Tenant.Status, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("identity: select tenant: %w", err)
	}
	grant.Role = reqctx.Role(role)
	return &grant, nil
}

// Remember records the tenant a session last used. It does not check access:
// callers run Grant first.
func (a *tenantAccess) Remember(ctx context.Context, account reqctx.AccountID, tenant reqctx.TenantID) error {
	accountID, accountOK := canonicalID(string(account))
	tenantID, tenantOK := canonicalID(string(tenant))
	if !accountOK || !tenantOK {
		return errors.New("identity: remember tenant: ids must be UUIDs")
	}
	_, err := a.pool.Exec(ctx,
		"UPDATE iam_accounts SET last_active_tenant_id = $2::uuid, updated_at = $3 WHERE id = $1::uuid",
		accountID, tenantID, a.clock.Now())
	if err != nil {
		return mapError("remember tenant", err)
	}
	return nil
}

// Memberships lists the account's active memberships, any tenant status,
// ordered by tenant slug then membership id.
func (a *tenantAccess) Memberships(ctx context.Context, account reqctx.AccountID) ([]coreaccess.MembershipRef, error) {
	list := []coreaccess.MembershipRef{}
	accountID, isUUID := canonicalID(string(account))
	if !isUUID {
		return list, nil
	}
	rows, err := a.pool.Query(ctx,
		`SELECT m.id::text, t.id::text, t.slug, t.name, t.status, m.role, m.status
FROM ten_memberships m JOIN ten_tenants t ON t.id = m.tenant_id
WHERE m.account_id = $1::uuid AND m.status = 'active' ORDER BY t.slug, m.id`, accountID)
	if err != nil {
		return nil, fmt.Errorf("identity: list memberships: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item coreaccess.MembershipRef
		var role string
		err := rows.Scan(&item.ID, &item.Tenant.ID, &item.Tenant.Slug, &item.Tenant.Name,
			&item.Tenant.Status, &role, &item.Status)
		if err != nil {
			return nil, fmt.Errorf("identity: scan membership: %w", err)
		}
		item.Role = reqctx.Role(role)
		list = append(list, item)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("identity: list memberships: %w", err)
	}
	return list, nil
}
