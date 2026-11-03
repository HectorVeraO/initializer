// Package parameters is the parameters catalog module: typed global
// parameters with value, definition and protection rules and per-tenant
// overrides, stored in the sys_* tables. It publishes the core parameter
// reader port.
package parameters

import (
	"context"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
)

const moduleID = "parameters"

// module is the state Provide builds.
type module struct {
	service *Service
}

// Module returns the parameters catalog module with its six routes. It
// publishes the parameter reader port.
func Module() core.Module {
	m := &module{}
	return core.Module{
		ID:         moduleID,
		Migrations: migrationsFS,
		Provide:    m.provide,
		Mount:      m.mount,
	}
}

// provide builds the service from the shared pool, logger and clock and
// publishes the parameter reader; it does no I/O.
func (m *module) provide(_ context.Context, b *core.Builder) error {
	// Core's pool is nil when its own configuration is invalid; core reports
	// those problems together after every Provide has run.
	if b.Pool() == nil {
		return nil
	}
	m.service = NewService(b.Pool(), b.Logger(), b.Clock())
	return b.ProvideParameterReader(NewReader(b.Pool(), b.Logger()))
}
