package featureflags

import (
	"context"
	"errors"
	"fmt"
	"time"

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

const (
	listGlobalSQL = `SELECT flag_key, enabled, updated_at FROM flg_overrides`
	listTenantSQL = `SELECT flag_key, tenant_id, enabled, updated_at FROM flg_tenant_overrides
	WHERE flag_key = ANY($1) ORDER BY flag_key, tenant_id`
	upsertGlobalSQL = `INSERT INTO flg_overrides (flag_key, enabled, updated_at, updated_by)
	VALUES ($1, $2, now(), $3)
	ON CONFLICT (flag_key) DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = now(), updated_by = EXCLUDED.updated_by`
	deleteGlobalSQL = `DELETE FROM flg_overrides WHERE flag_key = $1`
	upsertTenantSQL = `INSERT INTO flg_tenant_overrides (tenant_id, flag_key, enabled, updated_at, updated_by)
	VALUES ($1, $2, $3, now(), $4)
	ON CONFLICT (tenant_id, flag_key) DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = now(), updated_by = EXCLUDED.updated_by`
	deleteTenantSQL = `DELETE FROM flg_tenant_overrides WHERE tenant_id = $1 AND flag_key = $2`
)

// globalRow is one flg_overrides row.
type globalRow struct {
	enabled   bool
	updatedAt time.Time
}

// tenantRow is one flg_tenant_overrides row.
type tenantRow struct {
	tenantID  string
	enabled   bool
	updatedAt time.Time
}

// listGlobal returns every global override row by key.
func (s store) listGlobal(ctx context.Context) (map[string]globalRow, error) {
	rows, err := s.pool.Query(ctx, listGlobalSQL)
	if err != nil {
		return nil, fmt.Errorf("featureflags: list global overrides: %w", err)
	}
	defer rows.Close()
	out := map[string]globalRow{}
	for rows.Next() {
		var key string
		var row globalRow
		err := rows.Scan(&key, &row.enabled, &row.updatedAt)
		if err != nil {
			return nil, fmt.Errorf("featureflags: scan global override: %w", err)
		}
		out[key] = row
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("featureflags: read global overrides: %w", err)
	}
	return out, nil
}

// listTenant returns the tenant override rows of keys, ordered by tenant id.
func (s store) listTenant(ctx context.Context, keys []string) (map[string][]tenantRow, error) {
	rows, err := s.pool.Query(ctx, listTenantSQL, keys)
	if err != nil {
		return nil, fmt.Errorf("featureflags: list tenant overrides: %w", err)
	}
	defer rows.Close()
	out := map[string][]tenantRow{}
	for rows.Next() {
		var key string
		var row tenantRow
		err := rows.Scan(&key, &row.tenantID, &row.enabled, &row.updatedAt)
		if err != nil {
			return nil, fmt.Errorf("featureflags: scan tenant override: %w", err)
		}
		out[key] = append(out[key], row)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("featureflags: read tenant overrides: %w", err)
	}
	return out, nil
}

// setGlobal upserts the global override of key; actor may be nil.
func (s store) setGlobal(ctx context.Context, key string, enabled bool, actor *string) error {
	_, err := s.pool.Exec(ctx, upsertGlobalSQL, key, enabled, actor)
	if err != nil {
		return fmt.Errorf("featureflags: set global override: %w", err)
	}
	return nil
}

// clearGlobal deletes the global override of key when present.
func (s store) clearGlobal(ctx context.Context, key string) error {
	_, err := s.pool.Exec(ctx, deleteGlobalSQL, key)
	if err != nil {
		return fmt.Errorf("featureflags: clear global override: %w", err)
	}
	return nil
}

// setTenant upserts the override of key for tenantID; actor may be nil.
func (s store) setTenant(ctx context.Context, tenantID, key string, enabled bool, actor *string) error {
	_, err := s.pool.Exec(ctx, upsertTenantSQL, tenantID, key, enabled, actor)
	if err != nil {
		return fmt.Errorf("featureflags: set tenant override: %w", err)
	}
	return nil
}

// clearTenant deletes the override of key for tenantID when present.
func (s store) clearTenant(ctx context.Context, tenantID, key string) error {
	_, err := s.pool.Exec(ctx, deleteTenantSQL, tenantID, key)
	if err != nil {
		return fmt.Errorf("featureflags: clear tenant override: %w", err)
	}
	return nil
}
