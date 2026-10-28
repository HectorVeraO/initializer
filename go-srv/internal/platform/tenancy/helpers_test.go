package tenancy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/accounts"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/config"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/migrate"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

const (
	adminURLEnv  = "APP_TEST_DATABASE_URL"
	csrfHeader   = "X-XSRF-TOKEN"
	csrfValue    = "ok"
	apiKeyHeader = "X-API-Key"
	sessionName  = "app_session"
	publicBase   = "http://localhost:10120"
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

// randomPassword is 20 random alphanumeric characters, built at test time.
func randomPassword(t testing.TB) string {
	t.Helper()
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	raw := make([]byte, 20)
	_, err := rand.Read(raw)
	if err != nil {
		t.Fatalf("random: %v", err)
	}
	for i, v := range raw {
		raw[i] = alphabet[int(v)%len(alphabet)]
	}
	return string(raw)
}

// newTestPool creates an it_ database on the server of APP_TEST_DATABASE_URL,
// applies the tenancy migrations only and drops the database on cleanup. It
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
	name := "it_tenancy_" + hex.EncodeToString(suffix)
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

// lazyPool is a pool that never connects unless used.
func lazyPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://nobody@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatalf("lazy pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
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

// Account is an account a test seeded into the accounts fake.
type Account struct {
	ID    string
	Email string
}

// accountsFake is an accounts.Directory and accounts.Provisioner that follows
// the port contracts like coretest.AccountBook, and also lets a test add
// accounts after the host exists, put several accounts on one email, change
// an account's status, operator flag or email, hide an account and make a
// call fail. It is safe for concurrent use.
type accountsFake struct {
	mu        sync.Mutex
	operators map[string]bool
	order     []reqctx.AccountID
	refs      map[reqctx.AccountID]accounts.Ref
	hidden    map[reqctx.AccountID]bool
	// upperCase answers every email upper-cased, as a directory that does not
	// normalize would.
	upperCase    bool
	getErr       error
	provisionErr error
	// getCalls and provisionCalls count the port calls.
	getCalls       int
	provisionCalls int
	// passwords are the passwords Provision received.
	passwords []string
}

var (
	_ accounts.Directory   = (*accountsFake)(nil)
	_ accounts.Provisioner = (*accountsFake)(nil)
)

func newAccountsFake(operatorEmails ...string) *accountsFake {
	f := &accountsFake{
		operators: map[string]bool{},
		refs:      map[reqctx.AccountID]accounts.Ref{},
		hidden:    map[reqctx.AccountID]bool{},
	}
	for _, email := range operatorEmails {
		f.operators[strings.ToLower(email)] = true
	}
	return f
}

// seed adds an active account with email (several accounts may share one) and
// returns it.
func (f *accountsFake) seed(email string) Account {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := "Jane Doe"
	ref := accounts.Ref{
		ID:       reqctx.AccountID(newTestUUID()),
		Email:    strings.ToLower(email),
		FullName: &name,
		Status:   accounts.StatusActive,
	}
	f.refs[ref.ID] = ref
	f.order = append(f.order, ref.ID)
	return Account{ID: string(ref.ID), Email: ref.Email}
}

func (f *accountsFake) update(id string, change func(r *accounts.Ref)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ref := f.refs[reqctx.AccountID(id)]
	change(&ref)
	f.refs[ref.ID] = ref
}

func (f *accountsFake) setStatus(id string, status accounts.Status) {
	f.update(id, func(r *accounts.Ref) { r.Status = status })
}

func (f *accountsFake) setOperator(id string, isOperator bool) {
	f.update(id, func(r *accounts.Ref) { r.PlatformOperator = isOperator })
}

func (f *accountsFake) setEmail(id, email string) {
	f.update(id, func(r *accounts.Ref) { r.Email = strings.ToLower(email) })
}

func (f *accountsFake) hide(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hidden[reqctx.AccountID(id)] = true
}

func (f *accountsFake) failGet(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getErr = err
}

func (f *accountsFake) failProvision(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.provisionErr = err
}

func (f *accountsFake) calls() (get, provision int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.getCalls, f.provisionCalls
}

func (f *accountsFake) view(ref accounts.Ref) accounts.Ref {
	if f.upperCase {
		ref.Email = strings.ToUpper(ref.Email)
	}
	return ref
}

// Get follows accounts.Directory.Get.
func (f *accountsFake) Get(_ context.Context, ids []reqctx.AccountID) (map[reqctx.AccountID]accounts.Ref, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	found := make(map[reqctx.AccountID]accounts.Ref, len(ids))
	for _, id := range ids {
		canonical, isUUID := reqctx.ParseTenantID(string(id))
		if !isUUID || f.hidden[reqctx.AccountID(canonical)] {
			continue
		}
		ref, ok := f.refs[reqctx.AccountID(canonical)]
		if ok {
			found[ref.ID] = f.view(ref)
		}
	}
	return found, nil
}

// ByEmail follows accounts.Directory.ByEmail: oldest first.
func (f *accountsFake) ByEmail(_ context.Context, email string) ([]accounts.Ref, error) {
	wanted := strings.ToLower(strings.TrimSpace(email))
	f.mu.Lock()
	defer f.mu.Unlock()
	list := []accounts.Ref{}
	for _, id := range f.order {
		ref := f.refs[id]
		if ref.Email == wanted && !f.hidden[id] {
			list = append(list, f.view(ref))
		}
	}
	return list, nil
}

// Provision follows accounts.Provisioner.Provision.
func (f *accountsFake) Provision(_ context.Context, a accounts.NewAccount) (accounts.Ref, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.provisionCalls++
	f.passwords = append(f.passwords, a.Password)
	if f.provisionErr != nil {
		return accounts.Ref{}, f.provisionErr
	}
	email := strings.ToLower(strings.TrimSpace(a.Email))
	for _, ref := range f.refs {
		if ref.Email == email {
			return accounts.Ref{}, api.NewError(http.StatusConflict, accounts.CodeIdentityAlreadyExists,
				"An account with this email already exists. Sign in and accept the invitation.")
		}
	}
	name := a.FullName
	ref := accounts.Ref{
		ID:               reqctx.AccountID(newTestUUID()),
		Email:            email,
		FullName:         &name,
		Status:           accounts.StatusActive,
		PlatformOperator: f.operators[email],
	}
	f.refs[ref.ID] = ref
	f.order = append(f.order, ref.ID)
	return ref, nil
}

// GrantsOperator follows accounts.Provisioner.GrantsOperator.
func (f *accountsFake) GrantsOperator(email string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.operators[strings.ToLower(email)]
}

func newTestUUID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		panic("random: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
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

// keyFake is an APIKeyResolver: the header value is "<tenantID>:<scope>,<scope>".
type keyFake struct {
	pool *pgxpool.Pool
}

var _ coreaccess.APIKeyResolver = (*keyFake)(nil)

func (k *keyFake) Present(r *http.Request) bool {
	return r.Header.Get(apiKeyHeader) != ""
}

func (k *keyFake) Resolve(ctx context.Context, r *http.Request) (reqctx.Principal, error) {
	tenantText, scopeText, _ := strings.Cut(r.Header.Get(apiKeyHeader), ":")
	var slug string
	err := k.pool.QueryRow(ctx, "SELECT slug FROM ten_tenants WHERE id = $1::uuid", tenantText).Scan(&slug)
	if err != nil {
		return reqctx.Principal{}, errors.New("key fake: unknown tenant")
	}
	tenant := reqctx.TenantID(tenantText)
	return reqctx.Principal{
		Kind:       reqctx.ActorAPIKey,
		ActorID:    "apikey:fake",
		TenantID:   &tenant,
		TenantSlug: slug,
		Scopes:     strings.Split(scopeText, ","),
	}, nil
}

// tenantsRoot is the module behind core.Build with fakes at the boundary.
type tenantsRoot struct {
	t        *testing.T
	handler  http.Handler
	pool     *pgxpool.Pool
	clock    *settableClock
	logs     *logSink
	accounts *accountsFake
	sessions *sessionFake
	app      *core.App
	// directory is the TenantDirectory port the module provides, read from the host.
	directory coreaccess.TenantDirectory
	// access is the TenantAccess port the module provides, read from the host.
	access coreaccess.TenantAccess
}

func newTenantsRoot(t *testing.T, operatorEmails ...string) *tenantsRoot {
	t.Helper()
	pool := newTestPool(t)
	clk := &settableClock{now: time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)}
	logs := &logSink{}
	fake := newAccountsFake(operatorEmails...)
	sessions := newSessionFake()
	root := &tenantsRoot{t: t, pool: pool, clock: clk, logs: logs, accounts: fake, sessions: sessions}
	probe := core.Module{
		ID: "port-probe",
		Mount: func(_ context.Context, h *core.Host) error {
			root.directory = h.TenantDirectory()
			root.access = h.TenantAccess()
			return nil
		},
	}
	app, err := core.Build(context.Background(), core.Options{
		Logger:             slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Config:             config.MapSource(map[string]string{"APP_PUBLIC_BASE_URL": publicBase}),
		Clock:              clk,
		Pool:               pool,
		SessionResolver:    sessions,
		APIKeyResolver:     &keyFake{pool: pool},
		AccountDirectory:   fake,
		AccountProvisioner: fake,
	}, []core.Module{Module(), probe})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { _ = app.Close(context.Background()) })
	app.Readiness().SetServing(true)
	root.app = app
	root.handler = app.Handler()
	return root
}

func (e *tenantsRoot) exec(sql string, args ...any) {
	e.t.Helper()
	_, err := e.pool.Exec(context.Background(), sql, args...)
	if err != nil {
		e.t.Fatalf("exec %q: %v", sql, err)
	}
}

func (e *tenantsRoot) scan(sql string, args ...any) string {
	e.t.Helper()
	var out string
	err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&out)
	if err != nil {
		e.t.Fatalf("query %q: %v", sql, err)
	}
	return out
}

// person is a synthetic user: an account in the accounts fake.
type person struct {
	email     string
	accountID string
}

func (e *tenantsRoot) person() person {
	e.t.Helper()
	suffix := randomUUID(e.t)[:8]
	account := e.accounts.seed("jane-" + suffix + "@example.com")
	return person{email: account.Email, accountID: account.ID}
}

func (e *tenantsRoot) tenant(slug, status string) string {
	e.t.Helper()
	return e.scan("INSERT INTO ten_tenants (slug, name, status) VALUES ($1, $1, $2) RETURNING id::text", slug, status)
}

func (e *tenantsRoot) member(tenantID, accountID, role, status string, createdAt time.Time) {
	e.t.Helper()
	e.exec(`INSERT INTO ten_memberships (tenant_id, account_id, role, status, created_at)
VALUES ($1::uuid, $2::uuid, $3, $4, $5)`, tenantID, accountID, role, status, createdAt)
}

// apiKey returns a key header value that resolves to tenantID with scopes.
func (e *tenantsRoot) apiKey(tenantID string, scopes []string) string {
	return tenantID + ":" + strings.Join(scopes, ",")
}

// sessClient is a cookie jar over the handler.
type sessClient struct {
	e       *tenantsRoot
	cookies map[string]string
}

func (e *tenantsRoot) client() *sessClient {
	return &sessClient{e: e, cookies: map[string]string{}}
}

func (c *sessClient) sessionCookieName() string {
	return sessionName
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

// signIn mints a session for p on a fresh client, on the tenant the module's
// Select picks, the way a new session would start.
func (e *tenantsRoot) signIn(p person) *sessClient {
	e.t.Helper()
	principal := reqctx.Principal{
		Kind:      reqctx.ActorAccount,
		ActorID:   p.accountID,
		AccountID: reqctx.AccountID(p.accountID),
	}
	got, err := e.accounts.Get(context.Background(), []reqctx.AccountID{principal.AccountID})
	if err != nil {
		e.t.Fatalf("read account: %v", err)
	}
	principal.IsPlatformOperator = got[principal.AccountID].PlatformOperator
	grant, err := newTenantAccess(e.pool, e.clock).Select(context.Background(), principal.AccountID)
	if err != nil {
		e.t.Fatalf("select tenant: %v", err)
	}
	if grant != nil {
		tenant := grant.Tenant.ID
		principal.TenantID = &tenant
		principal.TenantSlug = grant.Tenant.Slug
		principal.Role = grant.Role
	}
	c := e.client()
	c.cookies[sessionName] = e.sessions.mint(principal)
	c.cookies["XSRF-TOKEN"] = csrfValue
	return c
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

// expectError asserts an error envelope with status and code.
func expectError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	apiErr := errorBody(t, rec)
	if rec.Code != status || string(apiErr.Code) != code {
		t.Fatalf("got %d %s, want %d %s", rec.Code, apiErr.Code, status, code)
	}
}

func setCookieNames(rec *httptest.ResponseRecorder) []string {
	var names []string
	for _, set := range rec.Result().Cookies() {
		names = append(names, set.Name)
	}
	return names
}

func mustParseURL(t testing.TB, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return parsed
}

// receivedPassword reports whether a Provision call carried password.
func (f *accountsFake) receivedPassword(password string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.passwords, password)
}
