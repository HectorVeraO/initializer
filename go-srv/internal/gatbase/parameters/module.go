// Package parameters is the parameters catalog module: typed global
// parameters with value, definition and protection rules and per-tenant
// overrides, stored in the sys_* tables.
package parameters

import (
	"context"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core"
)

const moduleID = "parameters"

// module is the state Provide builds.
type module struct {
	service *Service
}

// Module returns the parameters catalog module with its six routes.
func Module() core.Module {
	m := &module{}
	return core.Module{
		ID:         moduleID,
		Migrations: migrationsFS,
		Provide:    m.provide,
		Mount:      m.mount,
	}
}

// provide builds the service from the shared pool, logger and clock; it does
// no I/O and publishes no port.
func (m *module) provide(_ context.Context, b *core.Builder) error {
	m.service = NewService(b.Pool(), b.Logger(), b.Clock())
	return nil
}
