package apikeys

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

const apiKeyHeader = "X-API-Key"

// keyRow is the key row the resolver reads for one key prefix.
type keyRow struct {
	id         string
	prefix     string
	tenantID   *string
	secretHash []byte
	hashScheme string
	scopes     []string
	expiresAt  *time.Time
	revokedAt  *time.Time
}

// apiKeyResolver implements coreaccess.APIKeyResolver: one indexed read per
// request and one tenant directory read for a key with a tenant, no cache.
type apiKeyResolver struct {
	pool   *pgxpool.Pool
	clock  clock.Clock
	scopes *coreaccess.ScopeRegistry
	logger *slog.Logger
	// tenants answers what a key's tenant is; Mount sets it from the host. A
	// nil value at Resolve is an operational failure.
	tenants   coreaccess.TenantDirectory
	dummyHash []byte
}

func newAPIKeyResolver(pool *pgxpool.Pool, clk clock.Clock, scopes *coreaccess.ScopeRegistry, logger *slog.Logger) *apiKeyResolver {
	return &apiKeyResolver{
		pool: pool, clock: clk, scopes: scopes, logger: logger,
		dummyHash: hashAPIKeySecret("platform-dummy-secret"),
	}
}

// Present reports whether the X-API-Key header is present, unverified.
func (k *apiKeyResolver) Present(r *http.Request) bool {
	values := r.Header.Values(apiKeyHeader)
	isPresent := len(values) > 0
	return isPresent
}

// rejectedKey is the one error every invalid key answers, byte for byte.
func rejectedKey() *api.Error {
	return api.NewError(http.StatusUnauthorized, api.CodeAPIKeyInvalidOrRevoked, "API key is missing, invalid or revoked")
}

// Resolve verifies the key. Unknown prefix, malformed value, wrong secret,
// revoked, expired and unknown-tenant keys answer the same 401; a suspended
// tenant answers 403 only after the secret verified. A key without a tenant
// yields a principal without one.
func (k *apiKeyResolver) Resolve(ctx context.Context, r *http.Request) (reqctx.Principal, error) {
	var zero reqctx.Principal
	if k.tenants == nil {
		return zero, errors.New("apikeys: api key resolver has no tenant directory")
	}
	values := r.Header.Values(apiKeyHeader)
	value := ""
	if len(values) == 1 {
		value = values[0]
	}
	prefix, secret, wellFormed := splitAPIKey(value)

	row, found, err := k.lookup(ctx, prefix, wellFormed)
	if err != nil {
		return zero, err
	}
	// A miss compares against a fixed dummy hash so both paths do the same work.
	stored := k.dummyHash
	if found && row.hashScheme == hashSchemeSHA256 {
		stored = row.secretHash
	}
	computed := hashAPIKeySecret(secret)
	matches := subtle.ConstantTimeCompare(computed, stored) == 1
	isVerified := matches && wellFormed && found && row.hashScheme == hashSchemeSHA256
	if !isVerified {
		k.logRejected(ctx, prefix, wellFormed, "secret_or_prefix")
		return zero, rejectedKey()
	}
	now := k.clock.Now()
	isUsable := row.revokedAt == nil && (row.expiresAt == nil || row.expiresAt.After(now))
	if !isUsable {
		k.logRejected(ctx, prefix, true, "revoked_or_expired")
		return zero, rejectedKey()
	}
	principal := reqctx.Principal{
		Kind:    reqctx.ActorAPIKey,
		ActorID: "apikey:" + row.prefix,
		Scopes:  k.knownScopes(row.scopes),
	}
	if row.tenantID == nil {
		return principal, nil
	}
	ref, tenantFound, err := k.tenants.Tenant(ctx, reqctx.TenantID(*row.tenantID))
	if err != nil {
		return zero, fmt.Errorf("apikeys: api key tenant: %w", err)
	}
	if !tenantFound {
		k.logRejected(ctx, prefix, true, "tenant_unknown")
		return zero, rejectedKey()
	}
	if ref.Status == coreaccess.TenantSuspended {
		k.logRejected(ctx, prefix, true, "tenant_suspended")
		return zero, forbidden(api.CodeTenantSuspended, "The tenant is suspended.")
	}
	tenantID := ref.ID
	principal.TenantID = &tenantID
	principal.TenantSlug = ref.Slug
	return principal, nil
}

func forbidden(code api.Code, message string) *api.Error {
	return api.NewError(http.StatusForbidden, code, message)
}

// knownScopes keeps the stored scopes the registry still knows.
func (k *apiKeyResolver) knownScopes(stored []string) []string {
	granted := make([]string, 0, len(stored))
	for _, scope := range stored {
		if k.scopes.Known(scope) {
			granted = append(granted, scope)
		}
	}
	return granted
}

// lookup reads the key row of a well-formed prefix; found is false on a miss.
func (k *apiKeyResolver) lookup(ctx context.Context, prefix string, wellFormed bool) (keyRow, bool, error) {
	var row keyRow
	if !wellFormed {
		return row, false, nil
	}
	err := k.pool.QueryRow(ctx,
		`SELECT id::text, key_prefix, tenant_id::text, secret_hash, hash_scheme, scopes, expires_at, revoked_at
FROM apk_keys WHERE key_prefix = $1`, prefix).
		Scan(&row.id, &row.prefix, &row.tenantID, &row.secretHash, &row.hashScheme, &row.scopes, &row.expiresAt, &row.revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return keyRow{}, false, nil
	}
	if err != nil {
		return keyRow{}, false, fmt.Errorf("apikeys: read api key: %w", err)
	}
	return row, true, nil
}

// logRejected logs the reason and the public key prefix, never the header,
// the secret or the hash.
func (k *apiKeyResolver) logRejected(ctx context.Context, prefix string, wellFormed bool, reason string) {
	attrs := []any{slog.String("reason", reason)}
	if wellFormed {
		attrs = append(attrs, slog.String("key_prefix", prefix))
	}
	k.logger.DebugContext(ctx, "api_key_rejected", attrs...)
}
