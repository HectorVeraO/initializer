// Package tenancy is the tenancy catalog module: tenants, memberships and
// roles, invitations and the first-tenant bootstrap. It requires identity for
// accounts and provides core's tenant ports.
package tenancy

import (
	"context"
	"fmt"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
)

const moduleID = "tenancy"

// service is the state Provide builds and later phases use.
type service struct {
	cfg     Config
	tenancy *tenancy
}

// Module returns the tenancy catalog module. modules.go lists it after identity.
func Module() core.Module {
	s := &service{}
	return core.Module{
		ID:         moduleID,
		Migrations: migrationsFS,
		Provide:    s.provide,
		Mount:      s.mount,
		Start:      s.start,
	}
}

// provide parses the configuration (every problem goes to the shared reader,
// so core reports them together), publishes the tenant ports and registers
// the scope later routes declare in their policies. It queries nothing.
func (s *service) provide(_ context.Context, b *core.Builder) error {
	s.cfg = parseConfig(b.Config(), b.PublicBaseURL())
	s.tenancy = newTenancy(b.Pool(), b.Clock(), s.cfg)
	err := b.ProvideTenantDirectory(&tenantDirectory{pool: b.Pool()})
	if err != nil {
		return fmt.Errorf("provide tenant directory: %w", err)
	}
	err = b.ProvideTenantAccess(newTenantAccess(b.Pool(), b.Clock()))
	if err != nil {
		return fmt.Errorf("provide tenant access: %w", err)
	}
	err = b.Scopes().Register("tenant:read", "Read the key's own tenant", coreaccess.ScopeTenant)
	if err != nil {
		return fmt.Errorf("register scope: %w", err)
	}
	return nil
}

// mount reads the account ports from the host and registers the tenant,
// membership and invitation routes.
func (s *service) mount(_ context.Context, h *core.Host) error {
	s.tenancy.accounts = h.AccountDirectory()
	s.tenancy.provisioner = h.AccountProvisioner()
	s.mountRoutes(h)
	return nil
}
