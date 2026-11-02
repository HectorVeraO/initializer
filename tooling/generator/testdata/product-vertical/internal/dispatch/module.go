// Package dispatch is a product vertical: the job queue of the application,
// stored in the dsp_* tables and served under /api/v1/dispatch.
package dispatch

import (
	"context"

	"example.com/fixture/srv/internal/platform/core"
)

// module is the state Provide builds.
type module struct {
	service *Service
}

// Module returns the dispatch vertical with its two routes.
func Module() core.Module {
	m := &module{}
	return core.Module{
		ID:         "dispatch",
		Migrations: migrationsFS,
		Provide:    m.provide,
		Mount:      m.mount,
	}
}

// provide builds the service from the shared pool; it does no I/O and
// publishes no port.
func (m *module) provide(_ context.Context, b *core.Builder) error {
	m.service = NewService(b.Pool())
	return nil
}
