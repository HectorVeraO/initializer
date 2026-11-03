package parameters

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/parameter"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

// Reader implements parameter.Reader over sys_parameters and
// sys_parameter_overrides: the active tenant's override, then the global value
// of the definition. It reads the database on every call and caches nothing.
type Reader struct {
	store  *store
	logger *slog.Logger
}

var _ parameter.Reader = (*Reader)(nil)

// NewReader builds the reader. It panics on a nil pool or logger: that is a
// wiring mistake, not a runtime condition.
func NewReader(pool *pgxpool.Pool, logger *slog.Logger) *Reader {
	if pool == nil || logger == nil {
		panic("parameters: NewReader requires a pool and a logger")
	}
	return &Reader{store: &store{pool: pool}, logger: logger}
}

// Get resolves key for the tenant of ctx. A malformed key and an unknown key are
// both not found, the same as the routes answer them.
func (r *Reader) Get(ctx context.Context, key string) (parameter.Value, bool, error) {
	if !validKey(key) {
		return parameter.Value{}, false, nil
	}
	tenant, err := readerTenant(ctx)
	if err != nil {
		return parameter.Value{}, false, err
	}
	row, err := r.store.effective(ctx, tenant, key)
	if errors.Is(err, errNotFound) {
		return parameter.Value{}, false, nil
	}
	if err != nil {
		r.logger.ErrorContext(ctx, "parameter_read_failed", slog.String("parameter_key", key), slog.Any("error", err))
		return parameter.Value{}, false, err
	}
	value, err := parameter.NewValue(parameter.Type(row.valueType), row.value)
	if err != nil {
		r.logger.ErrorContext(ctx, "parameter_read_failed",
			slog.String("parameter_key", key), slog.String("value_type", string(row.valueType)), slog.Any("error", err))
		return parameter.Value{}, false, fmt.Errorf("read parameter %s: %w", key, err)
	}
	r.logger.DebugContext(ctx, "parameter_read",
		slog.String("parameter_key", key), slog.String("parameter_source", row.source))
	return value, true, nil
}

// readerTenant returns the active tenant of ctx as UUID text, or nil when the
// context carries none, so a $1::uuid bind never receives an empty string.
func readerTenant(ctx context.Context) (*string, error) {
	tenant, hasTenant := reqctx.TenantFrom(ctx)
	if !hasTenant {
		return nil, nil
	}
	return tenantText(&tenant)
}
