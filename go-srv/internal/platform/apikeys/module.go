// Package apikeys is the api-keys catalog module: the X-API-Key credential
// (generation, hashing and resolution), the key lifecycle routes and the scope
// list route. It requires identity for the sessions that manage keys and
// provides core's API key resolver.
package apikeys

import (
	"context"
	"fmt"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
)

const (
	moduleID = "api-keys"

	scopeRead            = "api-keys:read"
	scopeReadDescription = "List the tenant's API key metadata"
)

// service is the state Provide builds and later phases use.
type service struct {
	keys *apiKeys
	// resolver reads tenants through the host's TenantDirectory, set in Mount.
	resolver *apiKeyResolver
}

// Module returns the api-keys catalog module. modules.go lists it after identity.
func Module() core.Module {
	s := &service{}
	return core.Module{
		ID:         moduleID,
		Migrations: migrationsFS,
		Provide:    s.provide,
		Mount:      s.mount,
	}
}

// provide builds the key service, publishes the API key resolver and
// registers the scope the list route declares in its policy. It queries nothing.
func (s *service) provide(_ context.Context, b *core.Builder) error {
	s.keys = newAPIKeys(b.Pool(), b.Clock())
	s.resolver = newAPIKeyResolver(b.Pool(), b.Clock(), b.Scopes(), b.Logger())
	err := b.ProvideAPIKeyResolver(s.resolver)
	if err != nil {
		return fmt.Errorf("provide api key resolver: %w", err)
	}
	err = b.Scopes().Register(scopeRead, scopeReadDescription, coreaccess.ScopeTenant)
	if err != nil {
		return fmt.Errorf("register scope: %w", err)
	}
	return nil
}

// mount wires the resolver to the host tenant directory and registers the
// key and scope routes.
func (s *service) mount(_ context.Context, h *core.Host) error {
	s.resolver.tenants = h.TenantDirectory()
	s.mountRoutes(h)
	return nil
}
