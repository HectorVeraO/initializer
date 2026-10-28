package tenancy

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

// tenantDirectory is core's coreaccess.TenantDirectory over ten_tenants.
type tenantDirectory struct {
	pool *pgxpool.Pool
}

// TenantExists reports whether the tenant exists, whatever its status.
func (d *tenantDirectory) TenantExists(ctx context.Context, id reqctx.TenantID) (bool, error) {
	canonical, isUUID := canonicalID(string(id))
	if !isUUID {
		return false, nil
	}
	var exists bool
	err := d.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM ten_tenants WHERE id = $1::uuid)", canonical).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("tenancy: tenant exists: %w", err)
	}
	return exists, nil
}

// Tenant returns the tenant of id, whatever its status; false when no tenant
// has id or id is not a UUID.
func (d *tenantDirectory) Tenant(ctx context.Context, id reqctx.TenantID) (coreaccess.TenantRef, bool, error) {
	canonical, isUUID := canonicalID(string(id))
	if !isUUID {
		return coreaccess.TenantRef{}, false, nil
	}
	var ref coreaccess.TenantRef
	err := d.pool.QueryRow(ctx,
		"SELECT id::text, slug, name, status FROM ten_tenants WHERE id = $1::uuid", canonical).Scan(
		&ref.ID, &ref.Slug, &ref.Name, &ref.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return coreaccess.TenantRef{}, false, nil
	}
	if err != nil {
		return coreaccess.TenantRef{}, false, fmt.Errorf("tenancy: tenant by id: %w", err)
	}
	return ref, true, nil
}

var _ coreaccess.TenantDirectory = (*tenantDirectory)(nil)
