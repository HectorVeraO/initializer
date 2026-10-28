package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/config"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/featureflag"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity/session"
)

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

// kratosCall is one request the fake Kratos received.
type kratosCall struct {
	path        string
	cookies     string
	traceparent string
}

type fakeSession struct {
	id         string
	identityID string
	email      string
}

// stubKratos stands in for Kratos public at the network boundary.
type stubKratos struct {
	server *httptest.Server
	mu     sync.Mutex
	// sessions maps a Kratos cookie value to its session.
	sessions     map[string]fakeSession
	whoamiStatus int
	logoutStatus int
	calls        []kratosCall
}

func newStubKratos(t *testing.T) *stubKratos {
	t.Helper()
	f := &stubKratos{sessions: map[string]fakeSession{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *stubKratos) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, kratosCall{
		path: r.URL.Path, cookies: r.Header.Get("Cookie"), traceparent: r.Header.Get("traceparent"),
	})
	cookie, _ := r.Cookie(kratosCookieName)
	value := ""
	if cookie != nil {
		value = cookie.Value
	}
	switch r.URL.Path {
	case "/sessions/whoami":
		f.whoami(w, value)
	case "/self-service/logout/browser":
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"logout_token":"logout-token-1"}`))
	case "/self-service/logout":
		status := http.StatusNoContent
		if f.logoutStatus != 0 {
			status = f.logoutStatus
		}
		if status == http.StatusNoContent {
			delete(f.sessions, value)
		}
		w.WriteHeader(status)
	default:
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}

func (f *stubKratos) whoami(w http.ResponseWriter, cookieValue string) {
	if f.whoamiStatus != 0 {
		w.WriteHeader(f.whoamiStatus)
		return
	}
	found, ok := f.sessions[cookieValue]
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	body := map[string]any{
		"id": found.id, "active": true, "expires_at": "2030-01-01T00:00:00Z",
		"identity": map[string]any{"id": found.identityID, "traits": map[string]any{"email": found.email}},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (f *stubKratos) setWhoamiStatus(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.whoamiStatus = status
}

func (f *stubKratos) setLogoutStatus(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logoutStatus = status
}

func (f *stubKratos) callsTo(path string) []kratosCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var matched []kratosCall
	for _, call := range f.calls {
		if call.path == path {
			matched = append(matched, call)
		}
	}
	return matched
}

// login registers a Kratos session and returns its cookie value.
func (f *stubKratos) login(t *testing.T, identityID, email string) string {
	t.Helper()
	cookieValue := "k-" + randomUUID(t)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[cookieValue] = fakeSession{id: randomUUID(t), identityID: identityID, email: email}
	return cookieValue
}

// mapFlags is a featureflag.Evaluator with fixed answers.
type mapFlags map[featureflag.Key]bool

func (m mapFlags) Enabled(_ context.Context, key featureflag.Key) bool { return m[key] }

func (m mapFlags) Snapshot(_ context.Context, _ *reqctx.TenantID) map[featureflag.Key]bool {
	return m
}

type sessEnvOptions struct {
	flags featureflag.Evaluator
	// adminURL is the Kratos admin fake; empty keeps the default.
	adminURL string
	// operatorEmails is APP_PLATFORM_OPERATOR_EMAILS.
	operatorEmails string
}

// sessEnv is core.Build with the module, a probe route and fakes at the boundary.
type sessEnv struct {
	t       *testing.T
	handler http.Handler
	pool    *pgxpool.Pool
	kratos  *stubKratos
	clock   *settableClock
	logs    *logSink
	codec   *session.Codec
	access  *access
	app     *core.App
	// directory is the TenantDirectory port the host exposes to modules.
	directory coreaccess.TenantDirectory
}

const sessTestPublicURL = "http://localhost:10120"

func newSessEnv(t *testing.T, opts sessEnvOptions) *sessEnv {
	t.Helper()
	pool := newTestPool(t)
	fake := newStubKratos(t)
	clk := &settableClock{now: time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)}
	logs := &logSink{}
	keys := randomSessionKeys(t)
	var directory coreaccess.TenantDirectory
	probe := core.Module{
		ID: "session-probe",
		Mount: func(_ context.Context, h *core.Host) error {
			directory = h.TenantDirectory()
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				principal, _ := reqctx.PrincipalFrom(r.Context())
				h.API().Data(w, r, http.StatusOK, map[string]any{
					"actorId": principal.ActorID, "tenantSlug": principal.TenantSlug, "role": string(principal.Role),
				})
			})
			h.Handle("GET /api/v1/probe", coreaccess.Policy{Kind: coreaccess.KindSession}, handler)
			h.Handle("POST /api/v1/probe", coreaccess.Policy{Kind: coreaccess.KindSession}, handler)
			return nil
		},
	}
	settings := map[string]string{
		"APP_PUBLIC_BASE_URL":   sessTestPublicURL,
		"APP_SESSION_KEYS":      keys,
		"APP_KRATOS_PUBLIC_URL": fake.server.URL,
	}
	if opts.adminURL != "" {
		settings["APP_KRATOS_ADMIN_URL"] = opts.adminURL
	}
	if opts.operatorEmails != "" {
		settings["APP_PLATFORM_OPERATOR_EMAILS"] = opts.operatorEmails
	}
	app, err := core.Build(context.Background(), core.Options{
		Logger:        slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Config:        config.MapSource(settings),
		Clock:         clk,
		Pool:          pool,
		FlagEvaluator: opts.flags,
	}, []core.Module{Module(), probe})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { _ = app.Close(context.Background()) })
	app.Readiness().SetServing(true)

	ring, problems := session.ParseKeyring(keys)
	if len(problems) > 0 {
		t.Fatalf("keyring: %v", problems)
	}
	return &sessEnv{
		t: t, handler: app.Handler(), pool: pool, kratos: fake, clock: clk, logs: logs,
		codec: session.NewCodec(ring, clk), access: newAccess(pool, clk, nil), app: app, directory: directory,
	}
}

func (e *sessEnv) exec(sql string, args ...any) {
	e.t.Helper()
	_, err := e.pool.Exec(context.Background(), sql, args...)
	if err != nil {
		e.t.Fatalf("exec %q: %v", sql, err)
	}
}

func (e *sessEnv) scan(sql string, args ...any) string {
	e.t.Helper()
	var out string
	err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&out)
	if err != nil {
		e.t.Fatalf("query %q: %v", sql, err)
	}
	return out
}

// person is a synthetic user: a Kratos identity, a linked account and a Kratos cookie.
type person struct {
	identityID   string
	email        string
	accountID    string
	kratosCookie string
}

func (e *sessEnv) person() person {
	e.t.Helper()
	identity := randomUUID(e.t)
	email := "jane-" + identity[:8] + "@example.com"
	account, err := e.access.linkIdentity(context.Background(), identity, email)
	if err != nil {
		e.t.Fatalf("link identity: %v", err)
	}
	return person{
		identityID: identity, email: email, accountID: account.ID,
		kratosCookie: e.kratos.login(e.t, identity, email),
	}
}

func (e *sessEnv) tenant(slug, status string) string {
	e.t.Helper()
	return e.scan("INSERT INTO ten_tenants (slug, name, status) VALUES ($1, $1, $2) RETURNING id::text", slug, status)
}

func (e *sessEnv) member(tenantID, accountID, role, status string, createdAt time.Time) {
	e.t.Helper()
	e.exec(`INSERT INTO ten_memberships (tenant_id, account_id, role, status, created_at)
VALUES ($1::uuid, $2::uuid, $3, $4, $5)`, tenantID, accountID, role, status, createdAt)
}

// sessClient is a cookie jar over the handler.
type sessClient struct {
	e       *sessEnv
	cookies map[string]string
}

func (e *sessEnv) client() *sessClient {
	return &sessClient{e: e, cookies: map[string]string{}}
}

func (c *sessClient) sessionCookieName() string {
	return "app_session"
}

func (c *sessClient) do(method, path string, headers map[string]string) *httptest.ResponseRecorder {
	c.e.t.Helper()
	return c.doBody(method, path, headers, "")
}

// doBody is do with a JSON body (sent as application/json when not empty).
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
	for _, set := range rec.Result().Cookies() {
		if set.MaxAge < 0 {
			delete(c.cookies, set.Name)
			continue
		}
		c.cookies[set.Name] = set.Value
	}
	return rec
}

// mutate sends a cookie-authenticated unsafe request with the CSRF header.
func (c *sessClient) mutate(method, path string) *httptest.ResponseRecorder {
	return c.do(method, path, map[string]string{csrfHeader: c.cookies["XSRF-TOKEN"]})
}

func (c *sessClient) claims() session.Claims {
	c.e.t.Helper()
	claims, err := c.e.codec.VerifyAllowExpired(c.cookies[c.sessionCookieName()], 48*time.Hour)
	if err != nil {
		c.e.t.Fatalf("session cookie does not verify: %v", err)
	}
	return claims
}

// signIn issues a session for p on a fresh client and returns it.
func (e *sessEnv) signIn(p person) *sessClient {
	e.t.Helper()
	c := e.client()
	c.cookies[kratosCookieName] = p.kratosCookie
	rec := c.do(http.MethodGet, "/api/v1/auth/session", nil)
	if rec.Code != http.StatusOK {
		e.t.Fatalf("sign in answered %d: %s", rec.Code, rec.Body.String())
	}
	return c
}

type viewReply struct {
	Account struct {
		ID                 string  `json:"id"`
		Email              string  `json:"email"`
		FullName           *string `json:"fullName"`
		Role               *string `json:"role"`
		IsPlatformOperator bool    `json:"isPlatformOperator"`
	} `json:"account"`
	ActiveTenant *struct {
		ID, Slug, Name, Status string
	} `json:"activeTenant"`
	Memberships []struct {
		ID     string `json:"id"`
		Role   string `json:"role"`
		Status string `json:"status"`
		Tenant struct {
			Slug   string `json:"slug"`
			Status string `json:"status"`
		} `json:"tenant"`
	} `json:"memberships"`
	Flags map[string]bool `json:"flags"`
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
	var envelope api.Envelope[json.RawMessage]
	err := json.Unmarshal(rec.Body.Bytes(), &envelope)
	if err != nil || envelope.Error == nil {
		t.Fatalf("not an error envelope (status %d): %s", rec.Code, rec.Body.String())
	}
	if rec.Code != status || string(envelope.Error.Code) != code {
		t.Fatalf("got %d %s, want %d %s", rec.Code, envelope.Error.Code, status, code)
	}
}

func setCookieNames(rec *httptest.ResponseRecorder) []string {
	var names []string
	for _, set := range rec.Result().Cookies() {
		names = append(names, set.Name)
	}
	return names
}

func describeCalls(calls []kratosCall) string {
	return fmt.Sprintf("%+v", calls)
}
