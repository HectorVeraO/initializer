package apikeys

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/config"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/coretest"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/migrate"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

const (
	adminURLEnv = "APP_TEST_DATABASE_URL"
	csrfHeader  = "X-XSRF-TOKEN"
	csrfValue   = "ok"
	sessionName = "app_session"
	publicBase  = "http://localhost:10120"
	whoamiPath  = "/api/v1/whoami"
	whoamiScope = "probe:whoami"
)

// randomUUID returns a random version 4 UUID in canonical text.
func randomUUID(t testing.TB) string {
	t.Helper()
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		t.Fatalf("random: %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// newTestPool creates an it_ database on the server of APP_TEST_DATABASE_URL,
// applies the api-keys migrations only and drops the database on cleanup. It
// skips the test when the variable is unset.
func newTestPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	adminURL := os.Getenv(adminURLEnv)
	if adminURL == "" {
		t.Skipf("%s is not set; skipping database test", adminURLEnv)
	}
	suffix := make([]byte, 4)
	_, err := rand.Read(suffix)
	if err != nil {
		t.Fatalf("random: %v", err)
	}
	name := "it_apikeys_" + hex.EncodeToString(suffix)
	quoted := pgx.Identifier{name}.Sanitize()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("admin database unreachable: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()
	_, err = admin.Exec(ctx, "CREATE DATABASE "+quoted)
	if err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}
	t.Cleanup(func() { dropDatabase(t, adminURL, quoted) })

	cfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	cfg.ConnConfig.Database = name
	cfg.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	err = applyMigrations(ctx, pool)
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return pool
}

func applyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sets := []migrate.Set{{Module: moduleID, Files: migrationsFS}}
	return migrate.Apply(ctx, pool, logger, sets)
}

func dropDatabase(t testing.TB, adminURL, quoted string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Errorf("connect to drop %s: %v", quoted, err)
		return
	}
	defer func() { _ = admin.Close(ctx) }()
	_, err = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)")
	if err != nil {
		t.Errorf("drop %s: %v", quoted, err)
	}
}

// settableClock is a clock tests move forward.
type settableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *settableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *settableClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// logSink is a log sink safe for the concurrent handlers.
type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logSink) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logSink) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// sessionFake is a SessionResolver over cookies a test minted for a principal.
type sessionFake struct {
	mu         sync.Mutex
	principals map[string]reqctx.Principal
}

var _ coreaccess.SessionResolver = (*sessionFake)(nil)

func newSessionFake() *sessionFake {
	return &sessionFake{principals: map[string]reqctx.Principal{}}
}

func (f *sessionFake) mint(p reqctx.Principal) string {
	raw := make([]byte, 16)
	_, err := rand.Read(raw)
	if err != nil {
		panic("random: " + err.Error())
	}
	token := hex.EncodeToString(raw)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.principals[token] = p
	return token
}

func (f *sessionFake) Present(r *http.Request) bool {
	_, err := r.Cookie(sessionName)
	hasCookie := err == nil
	return hasCookie
}

func (f *sessionFake) Resolve(r *http.Request) (coreaccess.SessionResult, error) {
	cookie, err := r.Cookie(sessionName)
	if err != nil {
		return coreaccess.SessionResult{State: coreaccess.SessionInvalid}, nil
	}
	f.mu.Lock()
	principal, known := f.principals[cookie.Value]
	f.mu.Unlock()
	if !known {
		return coreaccess.SessionResult{State: coreaccess.SessionInvalid}, nil
	}
	return coreaccess.SessionResult{
		State:     coreaccess.SessionValid,
		Principal: principal,
		CSRFValid: r.Header.Get(csrfHeader) == csrfValue,
	}, nil
}

// probeModule stands in for tenancy: it registers tenant:read and mounts
// GET /api/v1/tenant, a route a session or a key holding tenant:read may call
// that answers the credential's tenant, plus session-only probe routes.
func probeModule() core.Module {
	return core.Module{
		ID: "key-probe",
		Provide: func(_ context.Context, b *core.Builder) error {
			err := b.Scopes().Register("tenant:read", "Read the key's own tenant", coreaccess.ScopeTenant)
			if err != nil {
				return err
			}
			return b.Scopes().Register(whoamiScope, "Read the key's own principal", coreaccess.ScopeAny)
		},
		Mount: func(_ context.Context, h *core.Host) error {
			tenantRoute := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				principal, _ := reqctx.PrincipalFrom(r.Context())
				err := coreaccess.CheckTenantRef(principal, r.URL.Query().Get("tenantId"))
				if err != nil {
					h.API().Fail(w, r, err)
					return
				}
				h.API().Data(w, r, http.StatusOK, map[string]any{"id": principal.TenantID, "slug": principal.TenantSlug})
			})
			probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.API().Data(w, r, http.StatusOK, map[string]any{"ok": true})
			})
			whoami := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				principal, _ := reqctx.PrincipalFrom(r.Context())
				body := map[string]any{
					"kind": principal.Kind, "actorId": principal.ActorID, "tenantSlug": principal.TenantSlug,
					"accountId": principal.AccountID, "role": principal.Role,
					"isPlatformOperator": principal.IsPlatformOperator, "scopes": principal.Scopes,
				}
				if principal.TenantID != nil {
					body["tenantId"] = string(*principal.TenantID)
				}
				h.API().Data(w, r, http.StatusOK, body)
			})
			h.Handle("GET "+whoamiPath, coreaccess.Policy{Kind: coreaccess.KindAPIKey, Scope: whoamiScope}, whoami)
			h.Handle("GET /api/v1/tenant",
				coreaccess.Policy{Kind: coreaccess.KindSessionOrAPIKey, Scope: "tenant:read", MinRole: reqctx.RoleViewer}, tenantRoute)
			h.Handle("GET /api/v1/probe", coreaccess.Policy{Kind: coreaccess.KindSession}, probe)
			h.Handle("POST /api/v1/probe", coreaccess.Policy{Kind: coreaccess.KindSession}, probe)
			return nil
		},
	}
}

// env is core.Build with the api-keys module, a probe module and fakes at the
// boundary: sessions and the tenant directory.
type env struct {
	t        *testing.T
	handler  http.Handler
	pool     *pgxpool.Pool
	clock    *settableClock
	logs     *logSink
	sessions *sessionFake
	// tenants is the TenantDirectory the host exposes; tests add tenants and
	// change statuses through it.
	tenants *tenantDir
}

// newEnv builds the module over a fresh database with a TenantBook directory.
func newEnv(t *testing.T) *env {
	t.Helper()
	return buildEnv(t, true)
}

// newEnvWithoutTenancy builds the module with no TenantDirectory provider, the
// shape of a project that selects api-keys without tenancy.
func newEnvWithoutTenancy(t *testing.T) *env {
	t.Helper()
	return buildEnv(t, false)
}

func buildEnv(t *testing.T, withDirectory bool) *env {
	t.Helper()
	pool := newTestPool(t)
	clk := &settableClock{now: time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)}
	logs := &logSink{}
	sessions := newSessionFake()
	book := newTenantDir()
	options := core.Options{
		Logger:          slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Config:          config.MapSource(map[string]string{"APP_PUBLIC_BASE_URL": publicBase}),
		Clock:           clk,
		Pool:            pool,
		SessionResolver: sessions,
	}
	if withDirectory {
		options.TenantDirectory = book
	}
	app, err := core.Build(context.Background(), options, []core.Module{Module(), probeModule()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { _ = app.Close(context.Background()) })
	app.Readiness().SetServing(true)
	return &env{t: t, handler: app.Handler(), pool: pool, clock: clk, logs: logs, sessions: sessions, tenants: book}
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	_, err := e.pool.Exec(context.Background(), sql, args...)
	if err != nil {
		e.t.Fatalf("exec %q: %v", sql, err)
	}
}

func (e *env) scan(sql string, args ...any) string {
	e.t.Helper()
	var out string
	err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&out)
	if err != nil {
		e.t.Fatalf("query %q: %v", sql, err)
	}
	return out
}

// tenant adds a tenant to the directory and returns its id.
func (e *env) tenant(slug, status string) string {
	e.t.Helper()
	id := randomUUID(e.t)
	e.tenants.AddTenant(coreaccess.TenantRef{
		ID: reqctx.TenantID(id), Slug: slug, Name: slug, Status: coreaccess.TenantStatus(status),
	})
	return id
}

// person is a synthetic account id: keys store it as a soft id, no table holds it.
type person struct {
	accountID string
}

func (e *env) person() person {
	e.t.Helper()
	return person{accountID: randomUUID(e.t)}
}

// actor creates a person and signs them in with role on tenantID (an empty
// tenantID is a session without a tenant).
func (e *env) actor(tenantID, role string, operator bool) (person, *sessClient) {
	e.t.Helper()
	p := e.person()
	principal := reqctx.Principal{
		Kind:               reqctx.ActorAccount,
		ActorID:            p.accountID,
		AccountID:          reqctx.AccountID(p.accountID),
		IsPlatformOperator: operator,
	}
	if tenantID != "" {
		tenant := reqctx.TenantID(tenantID)
		ref, found, err := e.tenants.Tenant(context.Background(), tenant)
		if err != nil || !found {
			e.t.Fatalf("actor: unknown tenant %s", tenantID)
		}
		principal.TenantID = &tenant
		principal.TenantSlug = ref.Slug
		principal.Role = reqctx.Role(role)
	}
	c := e.client()
	c.cookies[sessionName] = e.sessions.mint(principal)
	c.cookies["XSRF-TOKEN"] = csrfValue
	return p, c
}

// sessClient is a cookie jar over the handler.
type sessClient struct {
	e       *env
	cookies map[string]string
}

func (e *env) client() *sessClient {
	return &sessClient{e: e, cookies: map[string]string{}}
}

// doBody sends a request with the jar's cookies and a JSON body (sent as
// application/json when not empty).
func (c *sessClient) doBody(method, path string, headers map[string]string, body string) *httptest.ResponseRecorder {
	c.e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range c.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	c.e.handler.ServeHTTP(rec, req)
	return rec
}

func (c *sessClient) do(method, path string, headers map[string]string) *httptest.ResponseRecorder {
	c.e.t.Helper()
	return c.doBody(method, path, headers, "")
}

// send sends an unsafe request with the CSRF header and an optional body.
func (c *sessClient) send(method, path, body string) *httptest.ResponseRecorder {
	c.e.t.Helper()
	return c.doBody(method, path, map[string]string{csrfHeader: c.cookies["XSRF-TOKEN"]}, body)
}

// decodeData decodes a 2xx envelope.
func decodeData[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var envelope api.Envelope[T]
	err := json.Unmarshal(rec.Body.Bytes(), &envelope)
	if err != nil || envelope.Data == nil {
		t.Fatalf("not a data envelope (status %d): %s", rec.Code, rec.Body.String())
	}
	return *envelope.Data
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) *api.Error {
	t.Helper()
	var envelope api.Envelope[json.RawMessage]
	err := json.Unmarshal(rec.Body.Bytes(), &envelope)
	if err != nil || envelope.Error == nil {
		t.Fatalf("not an error envelope (status %d): %s", rec.Code, rec.Body.String())
	}
	return envelope.Error
}

// expectError asserts an error envelope with status and code.
func expectError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	apiErr := errorBody(t, rec)
	if rec.Code != status || string(apiErr.Code) != code {
		t.Fatalf("got %d %s, want %d %s", rec.Code, apiErr.Code, status, code)
	}
}

type wantIssue struct{ field, code string }

func expectIssues(t *testing.T, rec *httptest.ResponseRecorder, want []wantIssue) {
	t.Helper()
	expectError(t, rec, http.StatusUnprocessableEntity, "VALIDATION_FAILED")
	var got []wantIssue
	for _, issue := range errorBody(t, rec).Issues {
		got = append(got, wantIssue{issue.Field, string(issue.Code)})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("issues = %v, want %v", got, want)
	}
}

// errorJSON returns the raw "error" member of an envelope.
func errorJSON(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope map[string]json.RawMessage
	err := json.Unmarshal(rec.Body.Bytes(), &envelope)
	if err != nil || len(envelope["error"]) == 0 {
		t.Fatalf("no error member (status %d): %s", rec.Code, rec.Body.String())
	}
	return string(envelope["error"])
}

func ptr[T any](v T) *T {
	return &v
}

// tenantArg is the SQL argument of a tenant id: NULL when empty.
func tenantArg(tenantID string) any {
	if tenantID == "" {
		return nil
	}
	return tenantID
}

// tenantScope is the key scope of a tenant id.
func tenantScope(tenantID string) *reqctx.TenantID {
	scope := reqctx.TenantID(tenantID)
	return &scope
}

// apiKey inserts a key of tenantID (empty: a key without a tenant) built from
// fresh random parts and returns its plain text. The creator is a synthetic
// account id.
func (e *env) apiKey(tenantID string, scopes []string, expiresAt *time.Time) string {
	e.t.Helper()
	plain, err := newAPIKey()
	if err != nil {
		e.t.Fatalf("new key: %v", err)
	}
	prefix, secret, ok := splitAPIKey(plain)
	if !ok {
		e.t.Fatal("generated key does not split")
	}
	e.exec(`INSERT INTO apk_keys (tenant_id, name, key_prefix, secret_hash, hash_scheme, scopes, expires_at, created_by_account_id, created_at)
VALUES ($1::uuid, 'seeded', $2, $3, 'sha256', $4, $5, $6::uuid, $7)`,
		tenantArg(tenantID), prefix, hashAPIKeySecret(secret), scopes, expiresAt, randomUUID(e.t), e.clock.Now())
	return plain
}

// withKey sends a GET with only the X-API-Key header.
func (e *env) withKey(path, key string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.client().do(http.MethodGet, path, map[string]string{apiKeyHeader: key})
}

// mutateWithKey sends an unsafe request with only the X-API-Key header.
func (e *env) mutateWithKey(method, path, key string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.client().doBody(method, path, map[string]string{apiKeyHeader: key}, `{}`)
}

func createKeyJSON(name string, scopes []string, expiresAt any) string {
	body, _ := json.Marshal(map[string]any{"name": name, "scopes": scopes, "expiresAt": expiresAt})
	return string(body)
}

// tenantDir is the TenantDirectory a test hands to core.Build: a
// coretest.TenantBook rebuilt whenever the test adds a tenant or changes a
// status after the host exists, with an injectable failure.
type tenantDir struct {
	mu      sync.Mutex
	tenants []coreaccess.TenantRef
	book    *coretest.TenantBook
	err     error
}

var _ coreaccess.TenantDirectory = (*tenantDir)(nil)

func newTenantDir() *tenantDir {
	return &tenantDir{book: coretest.NewTenantBook()}
}

func (d *tenantDir) AddTenant(ref coreaccess.TenantRef) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tenants = append(d.tenants, ref)
	d.book = coretest.NewTenantBook(d.tenants...)
}

func (d *tenantDir) SetTenantStatus(id reqctx.TenantID, status coreaccess.TenantStatus) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.tenants {
		if d.tenants[i].ID == id {
			d.tenants[i].Status = status
		}
	}
	d.book = coretest.NewTenantBook(d.tenants...)
}

// fail makes every later lookup return err.
func (d *tenantDir) fail(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.err = err
}

func (d *tenantDir) current() (*coretest.TenantBook, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.book, d.err
}

func (d *tenantDir) TenantExists(ctx context.Context, id reqctx.TenantID) (bool, error) {
	book, err := d.current()
	if err != nil {
		return false, err
	}
	return book.TenantExists(ctx, id)
}

func (d *tenantDir) Tenant(ctx context.Context, id reqctx.TenantID) (coreaccess.TenantRef, bool, error) {
	book, err := d.current()
	if err != nil {
		return coreaccess.TenantRef{}, false, err
	}
	return book.Tenant(ctx, id)
}
