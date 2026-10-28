package tenancy

import (
	"context"
	"testing"

	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

func TestTenantDirectoryReportsTenantSlugAndStatus(t *testing.T) {
	env := newAccessEnv(t)
	var directory coreaccess.TenantDirectory = &tenantDirectory{pool: env.pool}
	tenantID := env.tenant("acme", "suspended")

	ref, found, err := directory.Tenant(context.Background(), reqctx.TenantID(tenantID))

	if err != nil || !found || ref.Slug != "acme" || ref.Status != coreaccess.TenantSuspended {
		t.Fatalf("tenant = %+v found=%v err=%v", ref, found, err)
	}
}

func TestTenantDirectoryTenant(t *testing.T) {
	e := newTenEnv(t)
	dir := &tenantDirectory{pool: e.pool}
	tenant := e.tenant("tenant-a")
	e.exec("UPDATE ten_tenants SET status = 'suspended' WHERE id = $1::uuid", tenant.ID)

	got, found, err := dir.Tenant(context.Background(), reqctx.TenantID(tenant.ID))
	if err != nil || !found {
		t.Fatalf("tenant = %+v found=%v err=%v", got, found, err)
	}
	if string(got.ID) != tenant.ID || got.Slug != "tenant-a" || got.Status != coreaccess.TenantSuspended {
		t.Fatalf("tenant ref = %+v", got)
	}
	for name, id := range map[string]string{
		"unknown id": "99999999-9999-4999-8999-999999999999",
		"non-uuid":   "not-a-uuid",
		"empty":      "",
	} {
		t.Run(name, func(t *testing.T) {
			ref, found, err := dir.Tenant(context.Background(), reqctx.TenantID(id))

			if err != nil || found || ref != (coreaccess.TenantRef{}) {
				t.Fatalf("tenant = %+v found=%v err=%v, want not found", ref, found, err)
			}
		})
	}
}
