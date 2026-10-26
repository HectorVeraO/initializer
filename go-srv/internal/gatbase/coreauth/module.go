// Package coreauth is the core-auth catalog module: tenants, accounts,
// memberships, invitations and API keys on top of Ory Kratos identities.
package coreauth

import (
	"context"
	"fmt"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core"
)

const moduleID = "core-auth"

// service is the state Provide builds and later phases use.
type service struct {
	cfg   Config
	store *store
}

// Module returns the core-auth catalog module. modules.go lists it first.
func Module() core.Module {
	s := &service{}
	return core.Module{
		ID:         moduleID,
		Migrations: migrationsFS,
		Provide:    s.provide,
	}
}

// provide parses the configuration (every problem goes to the shared reader,
// so core reports them together), keeps the pool without querying it, and
// registers the scopes later routes declare in their policies.
func (s *service) provide(_ context.Context, b *core.Builder) error {
	s.cfg = parseConfig(b.Config(), b.PublicBaseURL())
	s.store = newStore(b.Pool(), b.Clock())

	scopes := []struct{ name, description string }{
		{"tenant:read", "Read the key's own tenant"},
		{"api-keys:read", "List the tenant's API key metadata"},
	}
	for _, scope := range scopes {
		err := b.Scopes().Register(scope.name, scope.description)
		if err != nil {
			return fmt.Errorf("register scope: %w", err)
		}
	}
	return nil
}
