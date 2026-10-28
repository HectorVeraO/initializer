package identity

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
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity/session"
)

const csrfHeader = "X-XSRF-TOKEN"

// sessionResolver implements coreaccess.SessionResolver over the signed BFF token.
// It reads no database and calls no identity provider.
type sessionResolver struct {
	codec      *session.Codec
	cookies    session.Cookies
	refreshMax time.Duration
	logger     *slog.Logger
}

func newSessionResolver(codec *session.Codec, cookies session.Cookies, refreshMax time.Duration, logger *slog.Logger) *sessionResolver {
	return &sessionResolver{codec: codec, cookies: cookies, refreshMax: refreshMax, logger: logger}
}

// Present reports whether the session cookie is present (any value).
func (s *sessionResolver) Present(r *http.Request) bool {
	_, err := r.Cookie(s.cookies.SessionName())
	isPresent := err == nil
	return isPresent
}

// Resolve verifies the cookie. An expired token inside the refresh window is
// SessionExpired; every other failure is SessionInvalid.
func (s *sessionResolver) Resolve(r *http.Request) (coreaccess.SessionResult, error) {
	invalid := coreaccess.SessionResult{State: coreaccess.SessionInvalid}
	token, ok := s.cookies.Session(r)
	if !ok {
		return invalid, nil
	}
	state := coreaccess.SessionValid
	claims, err := s.codec.Verify(token)
	if errors.Is(err, session.ErrExpired) {
		state = coreaccess.SessionExpired
		claims, err = s.codec.VerifyAllowExpired(token, s.refreshMax)
	}
	if err != nil {
		s.logVerifyFailure(r.Context(), err)
		return invalid, nil
	}
	result := coreaccess.SessionResult{
		State:     state,
		Principal: principalOf(claims, state == coreaccess.SessionExpired),
		CSRFValid: session.CSRFMatches(r.Header.Get(csrfHeader), claims),
	}
	return result, nil
}

// logVerifyFailure logs the key id and the reason, never the token.
func (s *sessionResolver) logVerifyFailure(ctx context.Context, err error) {
	var verifyErr *session.VerifyError
	if !errors.As(err, &verifyErr) {
		return
	}
	s.logger.DebugContext(ctx, "session_verify_failed",
		slog.String("kid", verifyErr.Kid),
		slog.String("reason", verifyErr.Reason))
}

// principalOf maps verified claims to the caller of the request.
func principalOf(claims session.Claims, expired bool) reqctx.Principal {
	principal := reqctx.Principal{
		Kind:               reqctx.ActorAccount,
		ActorID:            claims.AccountID,
		AccountID:          reqctx.AccountID(claims.AccountID),
		IsPlatformOperator: claims.PlatformOperator,
		SessionExpired:     expired,
	}
	if claims.TenantID != nil {
		tenant := reqctx.TenantID(*claims.TenantID)
		principal.TenantID = &tenant
	}
	if claims.TenantSlug != nil {
		principal.TenantSlug = *claims.TenantSlug
	}
	if claims.Role != nil {
		principal.Role = reqctx.Role(*claims.Role)
	}
	return principal
}

const apiKeyHeader = "X-API-Key"

// apiKeyRow is the key row the resolver reads for one key prefix.
type apiKeyRow struct {
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
		return zero, errors.New("identity: api key resolver has no tenant directory")
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
		return zero, fmt.Errorf("identity: api key tenant: %w", err)
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
func (k *apiKeyResolver) lookup(ctx context.Context, prefix string, wellFormed bool) (apiKeyRow, bool, error) {
	var row apiKeyRow
	if !wellFormed {
		return row, false, nil
	}
	err := k.pool.QueryRow(ctx,
		`SELECT id::text, key_prefix, tenant_id::text, secret_hash, hash_scheme, scopes, expires_at, revoked_at
FROM iam_api_keys WHERE key_prefix = $1`, prefix).
		Scan(&row.id, &row.prefix, &row.tenantID, &row.secretHash, &row.hashScheme, &row.scopes, &row.expiresAt, &row.revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return apiKeyRow{}, false, nil
	}
	if err != nil {
		return apiKeyRow{}, false, fmt.Errorf("identity: read api key: %w", err)
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
