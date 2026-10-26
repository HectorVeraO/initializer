package featureflags

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/featureflag"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
)

// Evaluation sources logged as flag_source.
const (
	sourceTenant  = "tenant"
	sourceGlobal  = "global"
	sourceDefault = "default"
)

// Evaluator implements featureflag.Evaluator over the manifest and flg_*:
// tenant override, then global override, then manifest default. It reads the
// database on every call and caches nothing.
type Evaluator struct {
	manifest Manifest
	store    store
	logger   *slog.Logger
}

var _ featureflag.Evaluator = (*Evaluator)(nil)

// NewEvaluator panics on a nil pool or logger.
func NewEvaluator(m Manifest, pool *pgxpool.Pool, logger *slog.Logger) *Evaluator {
	if pool == nil || logger == nil {
		panic("featureflags: NewEvaluator requires a pool and a logger")
	}
	return &Evaluator{manifest: m, store: store{pool: pool}, logger: logger}
}

// Enabled resolves one key for the tenant of ctx. An undeclared key is false.
// On a store error an ops flag reads its manifest default and a release flag
// false.
func (e *Evaluator) Enabled(ctx context.Context, key featureflag.Key) bool {
	flag, ok := e.manifest.Lookup(key)
	if !ok {
		e.logger.WarnContext(ctx, "flag_unknown_key", slog.String("flag_key", string(key)))
		return false
	}
	tenantID := ""
	tenant, hasTenant := reqctx.TenantFrom(ctx)
	if hasTenant {
		tenantID = string(tenant)
	}
	found, err := e.store.forKey(ctx, tenantID, string(key))
	if err != nil {
		e.logger.ErrorContext(ctx, "flag_evaluation_failed",
			slog.String("flag_key", string(key)), slog.Any("error", err))
		return failureValue(flag)
	}
	value, source := resolve(flag, found)
	e.logger.DebugContext(ctx, "flag_evaluated",
		slog.String("flag_key", string(key)),
		slog.Bool("flag_value", value),
		slog.String("flag_source", source),
	)
	return value
}

// Snapshot resolves every declared key for tenant (nil: no tenant level). The
// map is never nil and holds exactly the manifest keys.
func (e *Evaluator) Snapshot(ctx context.Context, tenant *reqctx.TenantID) map[featureflag.Key]bool {
	flags := e.manifest.Flags()
	out := make(map[featureflag.Key]bool, len(flags))
	global, tenantRows, err := e.loadAll(ctx, tenant)
	if err != nil {
		e.logger.ErrorContext(ctx, "flag_evaluation_failed",
			slog.Int("flag_count", len(flags)), slog.Any("error", err))
		for _, flag := range flags {
			out[flag.Key] = failureValue(flag)
		}
		return out
	}
	for _, flag := range flags {
		found := overrides{}
		value, ok := tenantRows[string(flag.Key)]
		if ok {
			found.tenant = &value
		}
		globalValue, ok := global[string(flag.Key)]
		if ok {
			found.global = &globalValue
		}
		out[flag.Key], _ = resolve(flag, found)
	}
	return out
}

func (e *Evaluator) loadAll(ctx context.Context, tenant *reqctx.TenantID) (global, tenantRows map[string]bool, err error) {
	global, err = e.store.allGlobal(ctx)
	if err != nil {
		return nil, nil, err
	}
	if tenant == nil {
		return global, nil, nil
	}
	tenantRows, err = e.store.allForTenant(ctx, string(*tenant))
	if err != nil {
		return nil, nil, err
	}
	return global, tenantRows, nil
}

// resolve applies tenant over global over the manifest default.
func resolve(flag Flag, found overrides) (bool, string) {
	switch {
	case found.tenant != nil:
		return *found.tenant, sourceTenant
	case found.global != nil:
		return *found.global, sourceGlobal
	}
	return flag.Default, sourceDefault
}

// failureValue is the value a flag takes when the store cannot be read.
func failureValue(flag Flag) bool {
	if flag.Type == TypeOps {
		return flag.Default
	}
	return false
}
