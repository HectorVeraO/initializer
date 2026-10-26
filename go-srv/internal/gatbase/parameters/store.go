package parameters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// errNotFound is returned by a store read that matches no definition.
var errNotFound = errors.New("parameters: not found")

// lockMode is the row lock a definition load takes.
type lockMode string

const (
	lockShare  lockMode = "FOR SHARE"
	lockUpdate lockMode = "FOR UPDATE"
)

const definitionColumns = `p.key, p.value_type, p.default_value, p.min_value, p.max_value,
	p.allowed_values, p.description, p.is_system_required`

// tenantValue is one tenant override of a parameter.
type tenantValue struct {
	tenantID string
	value    json.RawMessage
}

// listedRow is one definition with the active tenant's override, if any.
type listedRow struct {
	def      definition
	override json.RawMessage
}

// store runs the hand-written queries on the sys_* tables.
type store struct {
	pool *pgxpool.Pool
}

type rowScanner interface {
	Scan(dest ...any) error
}

// scanDefinition reads definitionColumns followed by extra destinations.
func scanDefinition(row rowScanner, extra ...any) (definition, error) {
	var d definition
	var typeName string
	var defaultValue, allowed []byte
	dest := []any{&d.Key, &typeName, &defaultValue, &d.Min, &d.Max, &allowed, &d.Description, &d.IsSystemRequired}
	dest = append(dest, extra...)
	err := row.Scan(dest...)
	if err != nil {
		return definition{}, err
	}
	d.ValueType = valueType(typeName)
	d.DefaultValue = json.RawMessage(defaultValue)
	if allowed != nil {
		err = json.Unmarshal(allowed, &d.Allowed)
		if err != nil {
			return definition{}, fmt.Errorf("decode allowed_values: %w", err)
		}
	}
	return d, nil
}

// allowedArg is the jsonb argument for the allowed list; nil is SQL NULL.
func allowedArg(allowed []json.RawMessage) (any, error) {
	if allowed == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(allowed)
	if err != nil {
		return nil, fmt.Errorf("encode allowed_values: %w", err)
	}
	return string(encoded), nil
}

// load reads one definition and locks it for the rest of the transaction.
func (s *store) load(ctx context.Context, tx pgx.Tx, key string, mode lockMode) (definition, error) {
	query := "SELECT " + definitionColumns + " FROM sys_parameters p WHERE p.key = $1 " + string(mode)
	d, err := scanDefinition(tx.QueryRow(ctx, query, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return definition{}, errNotFound
	}
	if err != nil {
		return definition{}, fmt.Errorf("load parameter: %w", err)
	}
	return d, nil
}

// listOverrides returns every tenant override of key.
func (s *store) listOverrides(ctx context.Context, tx pgx.Tx, key string) ([]tenantValue, error) {
	rows, err := tx.Query(ctx, "SELECT tenant_id::text, value FROM sys_parameter_overrides WHERE parameter_key = $1", key)
	if err != nil {
		return nil, fmt.Errorf("list overrides: %w", err)
	}
	defer rows.Close()
	var overrides []tenantValue
	for rows.Next() {
		var item tenantValue
		var value []byte
		err = rows.Scan(&item.tenantID, &value)
		if err != nil {
			return nil, fmt.Errorf("scan override: %w", err)
		}
		item.value = json.RawMessage(value)
		overrides = append(overrides, item)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("list overrides: %w", err)
	}
	return overrides, nil
}

// countOverrides counts the overrides of key across tenants.
func (s *store) countOverrides(ctx context.Context, tx pgx.Tx, key string) (int, error) {
	var count int
	err := tx.QueryRow(ctx, "SELECT count(*) FROM sys_parameter_overrides WHERE parameter_key = $1", key).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count overrides: %w", err)
	}
	return count, nil
}

// insert stores a new definition; a taken key is a unique violation.
func (s *store) insert(ctx context.Context, tx pgx.Tx, d definition, updatedBy *string, now time.Time) error {
	allowed, err := allowedArg(d.Allowed)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO sys_parameters
		(key, value_type, default_value, min_value, max_value, allowed_values, description,
		 is_system_required, created_at, updated_at, updated_by)
		VALUES ($1, $2, $3::jsonb, $4, $5, $6::jsonb, $7, $8, $9, $9, $10::uuid)`,
		d.Key, string(d.ValueType), string(d.DefaultValue), d.Min, d.Max, allowed, d.Description,
		d.IsSystemRequired, now, updatedBy)
	if err != nil {
		return fmt.Errorf("insert parameter: %w", err)
	}
	return nil
}

// update rewrites the mutable fields of a definition.
func (s *store) update(ctx context.Context, tx pgx.Tx, d definition, updatedBy *string, now time.Time) error {
	allowed, err := allowedArg(d.Allowed)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE sys_parameters
		SET default_value = $2::jsonb, min_value = $3, max_value = $4, allowed_values = $5::jsonb,
		    description = $6, updated_at = $7, updated_by = $8::uuid
		WHERE key = $1`,
		d.Key, string(d.DefaultValue), d.Min, d.Max, allowed, d.Description, now, updatedBy)
	if err != nil {
		return fmt.Errorf("update parameter: %w", err)
	}
	return nil
}

// deleteDefinition removes a definition; the cascade removes its overrides.
func (s *store) deleteDefinition(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, "DELETE FROM sys_parameters WHERE key = $1", key)
	if err != nil {
		return fmt.Errorf("delete parameter: %w", err)
	}
	return nil
}

// upsertOverride sets the override of one tenant.
func (s *store) upsertOverride(ctx context.Context, tx pgx.Tx, tenantID, key string, value json.RawMessage, updatedBy *string, now time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO sys_parameter_overrides (tenant_id, parameter_key, value, updated_at, updated_by)
		VALUES ($1::uuid, $2, $3::jsonb, $4, $5::uuid)
		ON CONFLICT (tenant_id, parameter_key) DO UPDATE
		SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by`,
		tenantID, key, string(value), now, updatedBy)
	if err != nil {
		return fmt.Errorf("upsert override: %w", err)
	}
	return nil
}

// deleteOverride removes the override of one tenant, if present.
func (s *store) deleteOverride(ctx context.Context, tx pgx.Tx, tenantID, key string) error {
	_, err := tx.Exec(ctx, "DELETE FROM sys_parameter_overrides WHERE tenant_id = $1::uuid AND parameter_key = $2", tenantID, key)
	if err != nil {
		return fmt.Errorf("delete override: %w", err)
	}
	return nil
}

// list returns one page of definitions ordered by key, each with the
// override of tenantID (nil: no join), and the total number of definitions.
func (s *store) list(ctx context.Context, tenantID *string, isDescending bool, limit, offset int) ([]listedRow, int64, error) {
	direction := "ASC"
	if isDescending {
		direction = "DESC"
	}
	// COLLATE "C" orders keys by byte value, so the order does not depend on
	// the database locale.
	query := "SELECT " + definitionColumns + ", o.value FROM sys_parameters p " +
		"LEFT JOIN sys_parameter_overrides o ON o.parameter_key = p.key AND o.tenant_id = $1::uuid " +
		`ORDER BY p.key COLLATE "C" ` + direction + " LIMIT $2 OFFSET $3"
	rows, err := s.pool.Query(ctx, query, tenantID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list parameters: %w", err)
	}
	defer rows.Close()
	var items []listedRow
	for rows.Next() {
		var override []byte
		d, scanErr := scanDefinition(rows, &override)
		if scanErr != nil {
			return nil, 0, fmt.Errorf("scan parameter: %w", scanErr)
		}
		items = append(items, listedRow{def: d, override: json.RawMessage(override)})
	}
	err = rows.Err()
	if err != nil {
		return nil, 0, fmt.Errorf("list parameters: %w", err)
	}
	var total int64
	err = s.pool.QueryRow(ctx, "SELECT count(*) FROM sys_parameters").Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count parameters: %w", err)
	}
	return items, total, nil
}
