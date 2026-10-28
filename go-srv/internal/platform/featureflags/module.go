// Package featureflags is the feature-flags catalog module: the flags.yaml
// manifest loader, the evaluator behind the core featureflag port, and the
// platform-operator routes over the global and per-tenant overrides in the
// flg_* tables.
package featureflags

import (
	"context"
	"errors"
	"log/slog"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
)

const (
	moduleID       = "feature-flags"
	flagsFileKey   = "APP_FLAGS_FILE"
	problemLogName = "flags_manifest_invalid"
)

// module is the state Provide builds and Mount reads.
type module struct {
	manifest Manifest
	store    store
	ready    bool
}

// Module returns the feature-flags module. It publishes the flag evaluator
// port and mounts the five operator routes.
func Module() core.Module {
	m := &module{}
	return core.Module{
		ID:         moduleID,
		Migrations: migrationsFS,
		Provide:    m.provide,
		Mount:      m.mount,
	}
}

// provide loads the manifest named by APP_FLAGS_FILE and publishes the
// evaluator. A missing key is recorded by the config reader and reported with
// the other config problems; an unreadable or invalid file fails Provide with
// every problem listed.
func (m *module) provide(ctx context.Context, b *core.Builder) error {
	path := b.Config().RequiredString(flagsFileKey)
	if path == "" {
		return nil
	}
	manifest, err := LoadManifestFile(path)
	if err != nil {
		logProblems(ctx, b.Logger(), err)
		return err
	}
	// Core's pool is nil when its own configuration is invalid; core reports
	// those problems together after every Provide has run.
	if b.Pool() == nil {
		return nil
	}
	m.manifest = manifest
	m.store = store{pool: b.Pool()}
	m.ready = true
	evaluator := NewEvaluator(manifest, b.Pool(), b.Logger())
	return b.ProvideFlagEvaluator(evaluator)
}

func logProblems(ctx context.Context, logger *slog.Logger, err error) {
	var manifestErr *ManifestError
	if !errors.As(err, &manifestErr) {
		return
	}
	for _, problem := range manifestErr.Problems {
		logger.ErrorContext(ctx, problemLogName,
			slog.Int("line", problem.Line),
			slog.String("flag_key", problem.Key),
			slog.String("code", problem.Code),
		)
	}
}
