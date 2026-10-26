package featureflags

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	tenantAndGlobalSQL = `SELECT
	(SELECT enabled FROM flg_tenant_overrides WHERE tenant_id = $1 AND flag_key = $2),
	(SELECT enabled FROM flg_overrides WHERE flag_key = $2)`
	globalOnlySQL   = `SELECT (SELECT enabled FROM flg_overrides WHERE flag_key = $1)`
	allGlobalSQL    = `SELECT flag_key, enabled FROM flg_overrides`
	allForTenantSQL = `SELECT flag_key, enabled FROM flg_tenant_overrides WHERE tenant_id = $1`
)

// store runs the hand-written queries on the flg_* tables.
type store struct {
	pool *pgxpool.Pool
}

// overrides holds the override rows that apply to one key; nil means no row.
type overrides struct {
	tenant *bool
	global *bool
}

// forKey loads the tenant row (when tenantID is not empty) and the global row
// of one key in a single round trip.
func (s store) forKey(ctx context.Context, tenantID, key string) (overrides, error) {
	var found overrides
	if tenantID == "" {
		err := s.pool.QueryRow(ctx, globalOnlySQL, key).Scan(&found.global)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return overrides{}, fmt.Errorf("featureflags: load global override: %w", err)
		}
		return found, nil
	}
	err := s.pool.QueryRow(ctx, tenantAndGlobalSQL, tenantID, key).Scan(&found.tenant, &found.global)
	if err != nil {
		return overrides{}, fmt.Errorf("featureflags: load overrides: %w", err)
	}
	return found, nil
}

// allGlobal returns every global override by key.
func (s store) allGlobal(ctx context.Context) (map[string]bool, error) {
	return s.collect(ctx, allGlobalSQL)
}

// allForTenant returns every override of one tenant by key.
func (s store) allForTenant(ctx context.Context, tenantID string) (map[string]bool, error) {
	return s.collect(ctx, allForTenantSQL, tenantID)
}

func (s store) collect(ctx context.Context, sql string, args ...any) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("featureflags: query overrides: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var key string
		var enabled bool
		err := rows.Scan(&key, &enabled)
		if err != nil {
			return nil, fmt.Errorf("featureflags: scan override: %w", err)
		}
		out[key] = enabled
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("featureflags: read overrides: %w", err)
	}
	return out, nil
}
