package coreauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/auth"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/db"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
)

const (
	apiKeyPrefixLabel  = "gbk_live_"
	apiKeyIDChars      = 16
	apiKeySecretChars  = 52
	apiKeyIDBytes      = 10
	apiKeySecretBytes  = 32
	apiKeyPrefixLength = len(apiKeyPrefixLabel) + apiKeyIDChars
	apiKeyLength       = apiKeyPrefixLength + 1 + apiKeySecretChars

	hashSchemeSHA256 = "sha256"
	maxAPIKeyName    = 100
	// maxPrefixRetries bounds the retries after a key prefix collision.
	maxPrefixRetries = 3

	apiKeyActive  = "active"
	apiKeyExpired = "expired"
	apiKeyRevoked = "revoked"

	apiKeyColumns = "id::text, name, key_prefix, scopes, expires_at, revoked_at, created_at"
)

var (
	apiKeyPattern = regexp.MustCompile(`^gbk_live_[a-z2-7]{16}_[a-z2-7]{52}$`)
	base32Lower   = base32.StdEncoding.WithPadding(base32.NoPadding)
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

// ScopeInfo is the ScopeInfo schema.
type ScopeInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// newAPIKey returns a fresh key: the prefix label, 16 characters of id and 52
// of secret, both lower-case base32 from crypto/rand.
func newAPIKey() (string, error) {
	idPart, err := randomBase32(apiKeyIDBytes)
	if err != nil {
		return "", err
	}
	secretPart, err := randomBase32(apiKeySecretBytes)
	if err != nil {
		return "", err
	}
	key := apiKeyPrefixLabel + idPart + "_" + secretPart
	return key, nil
}

func randomBase32(size int) (string, error) {
	raw := make([]byte, size)
	_, err := rand.Read(raw)
	if err != nil {
		return "", fmt.Errorf("coreauth: random api key: %w", err)
	}
	encoded := strings.ToLower(base32Lower.EncodeToString(raw))
	return encoded, nil
}

// splitAPIKey strictly parses a key into its public prefix (first 25
// characters) and its secret (last 52).
func splitAPIKey(value string) (string, string, bool) {
	isWellFormed := len(value) == apiKeyLength && apiKeyPattern.MatchString(value)
	if !isWellFormed {
		return "", "", false
	}
	return value[:apiKeyPrefixLength], value[apiKeyPrefixLength+1:], true
}

// hashAPIKeySecret is the sha256 scheme: SHA-256 over the ASCII secret.
func hashAPIKeySecret(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// apiKeyStatusAt derives the status of a key at now.
func apiKeyStatusAt(expiresAt, revokedAt *time.Time, now time.Time) string {
	switch {
	case revokedAt != nil:
		return apiKeyRevoked
	case expiresAt != nil && !expiresAt.After(now):
		return apiKeyExpired
	}
	return apiKeyActive
}

// apiKeys is the service behind the API key routes.
type apiKeys struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

func newAPIKeys(pool *pgxpool.Pool, clk clock.Clock) *apiKeys {
	return &apiKeys{pool: pool, clock: clk}
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

// Create stores a key for the tenant and returns it with its plain text, which
// is not kept. The input is validated by the caller; a past expiresAt is
// accepted (the key is born expired).
func (s *apiKeys) Create(ctx context.Context, tenantID, actorID, name string, scopes []string, expiresAt *time.Time) (ApiKeyCreated, error) {
	tenant, tenantOK := api.ParseUUID(tenantID)
	actor, actorOK := api.ParseUUID(actorID)
	if !tenantOK || !actorOK || name == "" || len(scopes) == 0 {
		return ApiKeyCreated{}, errors.New("coreauth: create api key: tenant id, actor id, name and scopes are required")
	}
	now := s.clock.Now()
	var lastErr error
	for attempt := 0; attempt < maxPrefixRetries; attempt++ {
		plain, err := newAPIKey()
		if err != nil {
			return ApiKeyCreated{}, err
		}
		prefix, secret, ok := splitAPIKey(plain)
		if !ok {
			return ApiKeyCreated{}, errors.New("coreauth: create api key: generated key is malformed")
		}
		row := s.pool.QueryRow(ctx,
			`INSERT INTO iam_api_keys (tenant_id, name, key_prefix, secret_hash, hash_scheme, scopes, expires_at, created_by_account_id, created_at)
VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8::uuid, $9) RETURNING `+apiKeyColumns,
			tenant, name, prefix, hashAPIKeySecret(secret), hashSchemeSHA256, scopes, expiresAt, actor, now)
		view, err := scanAPIKey(row, now)
		if db.IsUniqueViolation(err) {
			lastErr = err
			continue
		}
		if err != nil {
			return ApiKeyCreated{}, fmt.Errorf("coreauth: insert api key: %w", err)
		}
		return ApiKeyCreated{ApiKey: view, PlainTextKey: plain}, nil
	}
	return ApiKeyCreated{}, fmt.Errorf("coreauth: insert api key: %w", lastErr)
}

// apiKeyStatusFilters are the SQL forms of apiKeyStatusAt; $2 is now.
var apiKeyStatusFilters = map[string]string{
	"":            "$2::timestamptz IS NOT NULL",
	apiKeyActive:  "revoked_at IS NULL AND (expires_at IS NULL OR expires_at > $2)",
	apiKeyExpired: "revoked_at IS NULL AND expires_at IS NOT NULL AND expires_at <= $2",
	apiKeyRevoked: "revoked_at IS NOT NULL AND $2::timestamptz IS NOT NULL",
}

// apiKeySortColumns maps the wire sort fields to columns.
var apiKeySortColumns = map[string]string{"createdAt": "created_at", "name": "name"}

// List pages the tenant's keys with the derived status (every status when
// status is empty), and the total count.
func (s *apiKeys) List(ctx context.Context, tenantID, status string, q api.PageQuery) ([]ApiKeyView, int64, error) {
	tenant, tenantOK := api.ParseUUID(tenantID)
	filter, filterOK := apiKeyStatusFilters[status]
	column, columnOK := apiKeySortColumns[q.SortField]
	if !tenantOK || !filterOK || !columnOK || q.Page < 1 || q.Size < 1 {
		return nil, 0, errors.New("coreauth: list api keys: tenant id, status, sort and page are required")
	}
	now := s.clock.Now()
	var total int64
	err := s.pool.QueryRow(ctx, "SELECT count(*) FROM iam_api_keys WHERE tenant_id = $1::uuid AND "+filter, tenant, now).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("coreauth: count api keys: %w", err)
	}
	direction := "ASC"
	if q.SortDesc {
		direction = "DESC"
	}
	rows, err := s.pool.Query(ctx,
		"SELECT "+apiKeyColumns+" FROM iam_api_keys WHERE tenant_id = $1::uuid AND "+filter+
			" ORDER BY "+column+" "+direction+", id "+direction+" LIMIT $3 OFFSET $4",
		tenant, now, q.Size, q.Offset())
	if err != nil {
		return nil, 0, fmt.Errorf("coreauth: list api keys: %w", err)
	}
	views, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ApiKeyView, error) {
		return scanAPIKey(row, now)
	})
	if err != nil {
		return nil, 0, fmt.Errorf("coreauth: list api keys: %w", err)
	}
	return views, total, nil
}

// Revoke sets revoked_at when it is null and returns the key. A foreign,
// unknown or malformed id is 404. Revoking twice keeps the first instant.
func (s *apiKeys) Revoke(ctx context.Context, tenantID, keyID string) (ApiKeyView, error) {
	tenant, tenantOK := api.ParseUUID(tenantID)
	id, idOK := api.ParseUUID(keyID)
	if !tenantOK || !idOK {
		return ApiKeyView{}, api.NotFound()
	}
	now := s.clock.Now()
	row := s.pool.QueryRow(ctx,
		"UPDATE iam_api_keys SET revoked_at = COALESCE(revoked_at, $3) WHERE id = $1::uuid AND tenant_id = $2::uuid RETURNING "+apiKeyColumns,
		id, tenant, now)
	view, err := scanAPIKey(row, now)
	if errors.Is(err, pgx.ErrNoRows) {
		return ApiKeyView{}, api.NotFound()
	}
	if err != nil {
		return ApiKeyView{}, fmt.Errorf("coreauth: revoke api key: %w", err)
	}
	return view, nil
}

// apiKeyRoutes holds what the API key handlers use.
type apiKeyRoutes struct {
	keys   *apiKeys
	scopes *auth.ScopeRegistry
	writer *api.Writer
}

// mountAPIKeys registers the API key and scope routes. Only the listing
// accepts an API key; no write route does.
func (s *service) mountAPIKeys(h *core.Host) {
	routes := &apiKeyRoutes{keys: s.apiKeys, scopes: h.Scopes(), writer: h.API()}
	admin := auth.Policy{Kind: auth.KindSession, MinRole: reqctx.RoleAdmin}
	list := auth.Policy{Kind: auth.KindSessionOrAPIKey, Scope: "api-keys:read", MinRole: reqctx.RoleAdmin}
	h.Handle("GET /api/v1/api-keys", list, http.HandlerFunc(routes.listAPIKeys))
	h.Handle("POST /api/v1/api-keys", admin, http.HandlerFunc(routes.createAPIKey))
	h.Handle("DELETE /api/v1/api-keys/{id}", admin, http.HandlerFunc(routes.revokeAPIKey))
	h.Handle("GET /api/v1/api-key-scopes", admin, http.HandlerFunc(routes.listScopes))
}

// callerTenant returns the active tenant of the caller, session or key.
func callerTenant(r *http.Request) (reqctx.Principal, string, bool) {
	principal, ok := reqctx.PrincipalFrom(r.Context())
	if !ok {
		return reqctx.Principal{}, "", false
	}
	tenantID, hasTenant := activeTenantOf(principal)
	return principal, tenantID, hasTenant
}

// readScopes validates scopes: a non-empty array of registered, distinct names.
func (rt *apiKeyRoutes) readScopes(body *api.Body, p *api.Problems) ([]string, bool) {
	names, ok := body.StringList(p, "scopes", api.Required)
	if !ok {
		return nil, false
	}
	if len(names) == 0 {
		p.Add("scopes", api.CodeFieldRequired, "Field is required")
		return nil, false
	}
	seen := make(map[string]struct{}, len(names))
	allValid := true
	for i, name := range names {
		field := fmt.Sprintf("scopes[%d]", i)
		_, repeated := seen[name]
		seen[name] = struct{}{}
		switch {
		case repeated:
			p.Add(field, api.CodeFieldDuplicate, "Duplicate entry")
			allValid = false
		case !rt.scopes.Known(name):
			p.Add(field, api.CodeFieldNotAllowed, "Not a registered scope")
			allValid = false
		}
	}
	return names, allValid
}

// readExpiresAt validates expiresAt: absent, null or an RFC 3339 timestamp.
func readExpiresAt(body *api.Body, p *api.Problems) (*time.Time, bool) {
	text, present := body.String(p, "expiresAt", api.Optional)
	if !present {
		isTypeError := body.Has("expiresAt") && !body.IsNull("expiresAt")
		return nil, !isTypeError
	}
	parsed, ok := api.CheckTimestamp(p, "expiresAt", text)
	if !ok {
		return nil, false
	}
	return &parsed, true
}

func (rt *apiKeyRoutes) createAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := callerTenant(r)
	if !ok || principal.Kind != reqctx.ActorAccount {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	body, err := api.DecodeBody(r)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	var p api.Problems
	name, nameOK := readTrimmedName(body, &p, "name", maxAPIKeyName)
	scopes, scopesOK := rt.readScopes(body, &p)
	expiresAt, expiresOK := readExpiresAt(body, &p)
	err = p.Err()
	if err != nil || !nameOK || !scopesOK || !expiresOK {
		rt.writer.Fail(w, r, err)
		return
	}
	created, err := rt.keys.Create(r.Context(), tenantID, principal.ActorID, name, scopes, expiresAt)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusCreated, created)
}

func (rt *apiKeyRoutes) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := callerTenant(r)
	if !ok {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	var p api.Problems
	status := r.URL.Query().Get("status")
	if status != "" {
		api.CheckOneOf(&p, "status", status, apiKeyActive, apiKeyExpired, apiKeyRevoked)
	}
	query := api.ParsePageQuery(r, &p, api.PageSpec{Sorts: []string{"createdAt", "name"}, DefaultSort: "-createdAt"})
	err := p.Err()
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	items, total, err := rt.keys.List(r.Context(), tenantID, status, query)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, api.NewPage(items, query, total))
}

func (rt *apiKeyRoutes) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := callerTenant(r)
	if !ok {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	view, err := rt.keys.Revoke(r.Context(), tenantID, r.PathValue("id"))
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, view)
}

func (rt *apiKeyRoutes) listScopes(w http.ResponseWriter, r *http.Request) {
	registered := rt.scopes.All()
	infos := make([]ScopeInfo, 0, len(registered))
	for _, scope := range registered {
		infos = append(infos, ScopeInfo{Name: scope.Name, Description: scope.Description})
	}
	rt.writer.Data(w, r, http.StatusOK, infos)
}
