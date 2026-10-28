package apikeys

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/db"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

const (
	// maxPrefixRetries bounds the retries after a key prefix collision.
	maxPrefixRetries = 3

	keyColumns = "id::text, name, key_prefix, scopes, expires_at, revoked_at, created_at"
)

// ApiKeyView is the ApiKeyView schema: never a secret, hash or plain key.
type ApiKeyView struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	KeyPrefix string     `json:"keyPrefix"`
	Scopes    []string   `json:"scopes"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expiresAt"`
	RevokedAt *time.Time `json:"revokedAt"`
	CreatedAt time.Time  `json:"createdAt"`
}

// ApiKeyCreated is the ApiKeyCreated schema. PlainTextKey is shown once: only
// its secret hash is stored.
type ApiKeyCreated struct {
	ApiKey       ApiKeyView `json:"apiKey"`
	PlainTextKey string     `json:"plainTextKey"`
}

// rowScanner is the Scan method of pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// apiKeys is the service behind the API key routes.
type apiKeys struct {
	pool  *pgxpool.Pool
	clock clock.Clock
	// generate yields a fresh plain key; newAPIKey in production.
	generate func() (string, error)
}

func newAPIKeys(pool *pgxpool.Pool, clk clock.Clock) *apiKeys {
	return &apiKeys{pool: pool, clock: clk, generate: newAPIKey}
}

func scanAPIKey(row rowScanner, now time.Time) (ApiKeyView, error) {
	var view ApiKeyView
	err := row.Scan(&view.ID, &view.Name, &view.KeyPrefix, &view.Scopes, &view.ExpiresAt, &view.RevokedAt, &view.CreatedAt)
	if err != nil {
		return ApiKeyView{}, err
	}
	view.Status = apiKeyStatusAt(view.ExpiresAt, view.RevokedAt, now)
	view.CreatedAt = view.CreatedAt.UTC()
	view.ExpiresAt = utcOrNil(view.ExpiresAt)
	view.RevokedAt = utcOrNil(view.RevokedAt)
	return view, nil
}

func utcOrNil(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}

// scopeArg returns the SQL argument for a key scope: nil (SQL NULL) for a
// global key, else the canonical tenant UUID. ok is false for a tenant id that
// is not a UUID.
func scopeArg(scope *reqctx.TenantID) (arg any, ok bool) {
	if scope == nil {
		return nil, true
	}
	tenant, isUUID := api.ParseUUID(string(*scope))
	if !isUUID {
		return nil, false
	}
	return tenant, true
}

// scopeFilter is the SQL condition that selects the keys of scope on
// placeholder $1, which always carries scopeArg. Built in Go: a global scope
// is "tenant_id IS NULL" and the $1 comparison keeps the placeholder typed.
func scopeFilter(scope *reqctx.TenantID) string {
	if scope == nil {
		return "(tenant_id IS NULL AND $1::uuid IS NULL)"
	}
	return "tenant_id = $1::uuid"
}

// Create stores a key in scope (nil: a global key) for account and returns it
// with its plain text, which is not kept. The caller validated the input; a
// past expiresAt is accepted (the key is born expired).
func (s *apiKeys) Create(ctx context.Context, scope *reqctx.TenantID, account reqctx.AccountID, name string, scopes []string, expiresAt *time.Time) (ApiKeyCreated, error) {
	tenant, tenantOK := scopeArg(scope)
	actor, actorOK := api.ParseUUID(string(account))
	if !tenantOK || !actorOK || name == "" || len(scopes) == 0 {
		return ApiKeyCreated{}, errors.New("apikeys: create api key: tenant scope, account id, name and scopes are required")
	}
	now := s.clock.Now()
	var lastErr error
	for attempt := 0; attempt < maxPrefixRetries; attempt++ {
		plain, err := s.generate()
		if err != nil {
			return ApiKeyCreated{}, err
		}
		prefix, secret, ok := splitAPIKey(plain)
		if !ok {
			return ApiKeyCreated{}, errors.New("apikeys: create api key: generated key is malformed")
		}
		row := s.pool.QueryRow(ctx,
			`INSERT INTO apk_keys (tenant_id, name, key_prefix, secret_hash, hash_scheme, scopes, expires_at, created_by_account_id, created_at)
VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8::uuid, $9) RETURNING `+keyColumns,
			tenant, name, prefix, hashAPIKeySecret(secret), hashSchemeSHA256, scopes, expiresAt, actor, now)
		view, err := scanAPIKey(row, now)
		if db.IsUniqueViolation(err) {
			lastErr = err
			continue
		}
		if err != nil {
			return ApiKeyCreated{}, fmt.Errorf("apikeys: insert api key: %w", err)
		}
		return ApiKeyCreated{ApiKey: view, PlainTextKey: plain}, nil
	}
	return ApiKeyCreated{}, fmt.Errorf("apikeys: insert api key: %w", lastErr)
}

// statusFilters are the SQL forms of apiKeyStatusAt; $2 is now.
var statusFilters = map[string]string{
	"":            "$2::timestamptz IS NOT NULL",
	statusActive:  "revoked_at IS NULL AND (expires_at IS NULL OR expires_at > $2)",
	statusExpired: "revoked_at IS NULL AND expires_at IS NOT NULL AND expires_at <= $2",
	statusRevoked: "revoked_at IS NOT NULL AND $2::timestamptz IS NOT NULL",
}

// sortColumns maps the wire sort fields to columns.
var sortColumns = map[string]string{"createdAt": "created_at", "name": "name"}

// List pages the keys of scope (nil: the global keys) with the derived status
// (every status when status is empty), and the total count.
func (s *apiKeys) List(ctx context.Context, scope *reqctx.TenantID, status string, q api.PageQuery) ([]ApiKeyView, int64, error) {
	tenant, tenantOK := scopeArg(scope)
	filter, filterOK := statusFilters[status]
	column, columnOK := sortColumns[q.SortField]
	if !tenantOK || !filterOK || !columnOK || q.Page < 1 || q.Size < 1 {
		return nil, 0, errors.New("apikeys: list api keys: tenant scope, status, sort and page are required")
	}
	where := scopeFilter(scope) + " AND " + filter
	now := s.clock.Now()
	var total int64
	err := s.pool.QueryRow(ctx, "SELECT count(*) FROM apk_keys WHERE "+where, tenant, now).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("apikeys: count api keys: %w", err)
	}
	direction := "ASC"
	if q.SortDesc {
		direction = "DESC"
	}
	rows, err := s.pool.Query(ctx,
		"SELECT "+keyColumns+" FROM apk_keys WHERE "+where+
			" ORDER BY "+column+" "+direction+", id "+direction+" LIMIT $3 OFFSET $4",
		tenant, now, q.Size, q.Offset())
	if err != nil {
		return nil, 0, fmt.Errorf("apikeys: list api keys: %w", err)
	}
	views, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ApiKeyView, error) {
		return scanAPIKey(row, now)
	})
	if err != nil {
		return nil, 0, fmt.Errorf("apikeys: list api keys: %w", err)
	}
	return views, total, nil
}

// Revoke sets revoked_at when it is null and returns the key. A key outside
// scope, unknown or malformed is 404 RESOURCE_NOT_FOUND. Revoking twice keeps
// the first instant.
func (s *apiKeys) Revoke(ctx context.Context, scope *reqctx.TenantID, keyID string) (ApiKeyView, error) {
	tenant, tenantOK := scopeArg(scope)
	id, idOK := api.ParseUUID(keyID)
	if !tenantOK || !idOK {
		return ApiKeyView{}, api.NotFound()
	}
	now := s.clock.Now()
	row := s.pool.QueryRow(ctx,
		"UPDATE apk_keys SET revoked_at = COALESCE(revoked_at, $3) WHERE id = $2::uuid AND "+scopeFilter(scope)+" RETURNING "+keyColumns,
		tenant, id, now)
	view, err := scanAPIKey(row, now)
	if errors.Is(err, pgx.ErrNoRows) {
		return ApiKeyView{}, api.NotFound()
	}
	if err != nil {
		return ApiKeyView{}, fmt.Errorf("apikeys: revoke api key: %w", err)
	}
	return view, nil
}
