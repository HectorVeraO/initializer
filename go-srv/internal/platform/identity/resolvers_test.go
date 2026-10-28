package identity

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity/session"
)

const resolverRefreshMax = 24 * time.Hour

type resolverFixture struct {
	resolver *sessionResolver
	codec    *session.Codec
	clock    *settableClock
	logs     *bytes.Buffer
}

func newResolverFixture(t *testing.T) *resolverFixture {
	t.Helper()
	ring, problems := session.ParseKeyring(randomSessionKeys(t))
	if len(problems) > 0 {
		t.Fatalf("keyring: %v", problems)
	}
	clk := &settableClock{now: time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)}
	codec := session.NewCodec(ring, clk)
	base, _ := url.Parse("http://localhost:10120")
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	resolver := newSessionResolver(codec, session.NewCookies(base, resolverRefreshMax), resolverRefreshMax, logger)
	return &resolverFixture{resolver: resolver, codec: codec, clock: clk, logs: logs}
}

func (f *resolverFixture) token(t *testing.T, withTenant bool) (string, session.Claims) {
	t.Helper()
	now := f.clock.Now()
	claims := session.Claims{
		AccountID: randomUUID(t), SessionID: "sid-1", KratosSessionID: "ksid-1", CSRF: "csrf-value-1",
		PlatformOperator: true, IssuedAt: now, ExpiresAt: now.Add(10 * time.Minute),
	}
	if withTenant {
		tenant, slug, role := randomUUID(t), "acme", "member"
		claims.TenantID, claims.TenantSlug, claims.Role = &tenant, &slug, &role
	}
	token, err := f.codec.Encode(claims)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return token, claims
}

func requestWith(token, csrf string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/probe", nil)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "app_session", Value: token})
	}
	if csrf != "" {
		req.Header.Set(csrfHeader, csrf)
	}
	return req
}

func TestResolverValidTokenYieldsThePrincipal(t *testing.T) {
	f := newResolverFixture(t)
	token, claims := f.token(t, true)

	result, err := f.resolver.Resolve(requestWith(token, claims.CSRF))

	if err != nil || result.State != coreaccess.SessionValid || !result.CSRFValid {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	p := result.Principal
	if p.Kind != reqctx.ActorAccount || p.ActorID != claims.AccountID || string(p.AccountID) != claims.AccountID ||
		p.TenantID == nil || string(*p.TenantID) != *claims.TenantID || p.TenantSlug != "acme" ||
		p.Role != reqctx.Role("member") || !p.IsPlatformOperator || p.SessionExpired {
		t.Errorf("principal = %+v", p)
	}
}

func TestResolverTenantlessTokenHasNoTenantOrRole(t *testing.T) {
	f := newResolverFixture(t)
	token, claims := f.token(t, false)

	result, _ := f.resolver.Resolve(requestWith(token, claims.CSRF))

	if result.State != coreaccess.SessionValid || result.Principal.TenantID != nil ||
		result.Principal.Role != "" || result.Principal.TenantSlug != "" {
		t.Errorf("result = %+v", result)
	}
}

func TestResolverCSRFValidityFollowsTheHeader(t *testing.T) {
	f := newResolverFixture(t)
	token, claims := f.token(t, true)
	for header, want := range map[string]bool{claims.CSRF: true, "": false, "other": false, claims.CSRF + "x": false} {
		result, _ := f.resolver.Resolve(requestWith(token, header))
		if result.CSRFValid != want {
			t.Errorf("header %q: CSRFValid = %v, want %v", header, result.CSRFValid, want)
		}
	}
}

func TestResolverExpiredTokenIsAcceptedOnlyWithinTheRefreshMaxAge(t *testing.T) {
	f := newResolverFixture(t)
	token, claims := f.token(t, true)

	f.clock.advance(11 * time.Minute)
	result, _ := f.resolver.Resolve(requestWith(token, claims.CSRF))
	if result.State != coreaccess.SessionExpired || !result.Principal.SessionExpired || !result.CSRFValid {
		t.Errorf("within max age: %+v", result)
	}

	f.clock.advance(24 * time.Hour)
	result, _ = f.resolver.Resolve(requestWith(token, claims.CSRF))
	if result.State != coreaccess.SessionInvalid {
		t.Errorf("beyond max age: state = %v", result.State)
	}
}

func TestResolverRejectsMissingEmptyAndTamperedTokens(t *testing.T) {
	f := newResolverFixture(t)
	token, _ := f.token(t, true)
	tampered := token[:len(token)-4] + "AAAA"
	if tampered == token {
		tampered = token[:len(token)-4] + "BBBB"
	}
	for name, candidate := range map[string]string{"missing": "", "garbage": "not.a.token", "tampered": tampered} {
		result, err := f.resolver.Resolve(requestWith(candidate, "x"))
		if err != nil || result.State != coreaccess.SessionInvalid || result.Principal.ActorID != "" {
			t.Errorf("%s: result = %+v, err = %v", name, result, err)
		}
	}
}

func TestResolverPresentMeansTheCookieExistsWhateverItsValue(t *testing.T) {
	f := newResolverFixture(t)
	if f.resolver.Present(requestWith("", "")) {
		t.Error("Present without cookie")
	}
	if !f.resolver.Present(requestWith("garbage", "")) {
		t.Error("Present must be true for any value")
	}
}

func TestResolverLogsTheKidAndReasonNeverTheToken(t *testing.T) {
	f := newResolverFixture(t)
	token, _ := f.token(t, true)
	tampered := token[:len(token)-4] + "AAAA"
	if tampered == token {
		tampered = token[:len(token)-4] + "BBBB"
	}

	_, _ = f.resolver.Resolve(requestWith(tampered, ""))

	logged := f.logs.String()
	if !strings.Contains(logged, "session_verify_failed") || !strings.Contains(logged, `"reason"`) || !strings.Contains(logged, `"kid"`) {
		t.Errorf("log = %s", logged)
	}
	if strings.Contains(logged, tampered) || strings.Contains(logged, token[:20]) {
		t.Error("the token leaked into the log")
	}
}

// FuzzResolve checks that no cookie value makes Resolve panic, error, or
// authenticate without a valid signature.
func FuzzResolve(f *testing.F) {
	f.Add("")
	f.Add("a.b.c")
	f.Add("eyJhbGciOiJFZDI1NTE5In0.e30.AAAA")
	f.Add(strings.Repeat("A", 5000))
	f.Fuzz(func(t *testing.T, cookieValue string) {
		g := newResolverFixture(t)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Add("Cookie", "app_session="+strings.Map(func(r rune) rune {
			if r < 0x21 || r > 0x7e || r == ';' || r == ',' || r == '"' || r == '\\' {
				return -1
			}
			return r
		}, cookieValue))
		result, err := g.resolver.Resolve(req)
		if err != nil {
			t.Fatalf("Resolve errored: %v", err)
		}
		if result.State != coreaccess.SessionInvalid {
			t.Fatalf("an unsigned cookie authenticated: %+v", result)
		}
	})
}

// fakeDirectory is a TenantDirectory answering from a map.
type fakeDirectory struct {
	tenants map[reqctx.TenantID]coreaccess.TenantRef
	err     error
}

func (d *fakeDirectory) TenantExists(_ context.Context, id reqctx.TenantID) (bool, error) {
	_, found := d.tenants[id]
	return found, d.err
}

func (d *fakeDirectory) Tenant(_ context.Context, id reqctx.TenantID) (coreaccess.TenantRef, bool, error) {
	ref, found := d.tenants[id]
	return ref, found, d.err
}

type keyResolverFixture struct {
	resolver  *apiKeyResolver
	directory *fakeDirectory
	pool      *pgxpool.Pool
	clock     *settableClock
	logs      *bytes.Buffer
}

func newKeyResolverFixture(t *testing.T) *keyResolverFixture {
	t.Helper()
	pool := newTestPool(t)
	scopes := coreaccess.NewScopeRegistry()
	for _, name := range []string{"tenant:read", "api-keys:read"} {
		err := scopes.Register(name, "synthetic", coreaccess.ScopeTenant)
		if err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	clk := &settableClock{now: time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)}
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	resolver := newAPIKeyResolver(pool, clk, scopes, logger)
	directory := &fakeDirectory{tenants: map[reqctx.TenantID]coreaccess.TenantRef{}}
	resolver.tenants = directory
	return &keyResolverFixture{resolver: resolver, directory: directory, pool: pool, clock: clk, logs: logs}
}

// key inserts a key row (tenantID nil: a key without a tenant) and returns its
// plain text. No tenant or account table is involved.
func (f *keyResolverFixture) key(t *testing.T, tenantID any, scopes []string) string {
	t.Helper()
	plain, err := newAPIKey()
	if err != nil {
		t.Fatalf("new key: %v", err)
	}
	prefix, secret, _ := splitAPIKey(plain)
	_, err = f.pool.Exec(context.Background(),
		`INSERT INTO iam_api_keys (tenant_id, name, key_prefix, secret_hash, scopes, created_by_account_id)
VALUES ($1::uuid, 'seeded', $2, $3, $4, $5::uuid)`,
		tenantID, prefix, hashAPIKeySecret(secret), scopes, randomUUID(t))
	if err != nil {
		t.Fatalf("insert key: %v", err)
	}
	return plain
}

func TestKeyResolverGivesAKeyWithoutATenantAPrincipalWithoutOne(t *testing.T) {
	f := newKeyResolverFixture(t)
	key := f.key(t, nil, []string{"tenant:read", "removed:scope"})
	prefix, _, _ := splitAPIKey(key)

	principal, err := f.resolver.Resolve(context.Background(), keyRequest(key))

	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if principal.Kind != reqctx.ActorAPIKey || principal.ActorID != "apikey:"+prefix ||
		principal.TenantID != nil || principal.TenantSlug != "" ||
		!reflect.DeepEqual(principal.Scopes, []string{"tenant:read"}) {
		t.Fatalf("principal = %+v", principal)
	}
}

func TestKeyResolverReadsTheTenantOfAKeyThroughTheDirectory(t *testing.T) {
	f := newKeyResolverFixture(t)
	tenantID := reqctx.TenantID(randomUUID(t))
	f.directory.tenants[tenantID] = coreaccess.TenantRef{ID: tenantID, Slug: "acme", Name: "Acme", Status: coreaccess.TenantActive}
	key := f.key(t, string(tenantID), []string{"tenant:read"})

	principal, err := f.resolver.Resolve(context.Background(), keyRequest(key))

	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if principal.TenantID == nil || *principal.TenantID != tenantID || principal.TenantSlug != "acme" ||
		!reflect.DeepEqual(principal.Scopes, []string{"tenant:read"}) {
		t.Fatalf("principal = %+v", principal)
	}
}

func TestKeyResolverAnswersTheSame401ForAnUnknownTenantAndASuspendedOneOnlyAfterTheSecret(t *testing.T) {
	f := newKeyResolverFixture(t)
	suspended := reqctx.TenantID(randomUUID(t))
	f.directory.tenants[suspended] = coreaccess.TenantRef{ID: suspended, Slug: "gone", Status: coreaccess.TenantSuspended}
	unknownTenantKey := f.key(t, randomUUID(t), []string{"tenant:read"})
	suspendedKey := f.key(t, string(suspended), []string{"tenant:read"})
	other, _ := newAPIKey()
	_, wrongSecret, _ := splitAPIKey(other)
	suspendedPrefix, _, _ := splitAPIKey(suspendedKey)
	suspendedWrongSecret := suspendedPrefix + "_" + wrongSecret

	_, unknownErr := f.resolver.Resolve(context.Background(), keyRequest(unknownTenantKey))
	_, suspendedErr := f.resolver.Resolve(context.Background(), keyRequest(suspendedKey))
	_, wrongSecretErr := f.resolver.Resolve(context.Background(), keyRequest(suspendedWrongSecret))

	var unknown, suspendedAPI, wrong *api.Error
	if !errors.As(unknownErr, &unknown) || unknown.Status != http.StatusUnauthorized || unknown.Code != api.CodeAPIKeyInvalidOrRevoked {
		t.Fatalf("unknown tenant: %v", unknownErr)
	}
	if !errors.As(suspendedErr, &suspendedAPI) || suspendedAPI.Status != http.StatusForbidden || suspendedAPI.Code != api.CodeTenantSuspended {
		t.Fatalf("suspended tenant: %v", suspendedErr)
	}
	if !errors.As(wrongSecretErr, &wrong) || wrong.Status != rejectedKey().Status || wrong.Code != rejectedKey().Code || wrong.Message != rejectedKey().Message {
		t.Fatalf("wrong secret of a suspended tenant's key: %v", wrongSecretErr)
	}
	if !strings.Contains(f.logs.String(), `"reason":"tenant_unknown"`) ||
		!strings.Contains(f.logs.String(), `"reason":"tenant_suspended"`) {
		t.Fatalf("rejection reasons are not logged: %s", f.logs.String())
	}
	if strings.Contains(f.logs.String(), unknownTenantKey) || strings.Contains(f.logs.String(), suspendedKey) {
		t.Fatal("a key reached the log")
	}
}

func TestKeyResolverFailsOperationallyWithoutADirectoryOrWhenItFails(t *testing.T) {
	f := newKeyResolverFixture(t)
	tenantID := reqctx.TenantID(randomUUID(t))
	key := f.key(t, string(tenantID), []string{"tenant:read"})

	f.directory.err = errors.New("directory down")
	_, err := f.resolver.Resolve(context.Background(), keyRequest(key))
	var apiErr *api.Error
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("a failing directory must be operational, got %v", err)
	}

	f.resolver.tenants = nil
	_, err = f.resolver.Resolve(context.Background(), keyRequest(key))
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("a missing directory must be operational, got %v", err)
	}
	_, err = f.resolver.Resolve(context.Background(), keyRequest("not-a-key"))
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("a missing directory must be operational for any request, got %v", err)
	}
}

func keyRequest(key string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/api-keys", nil)
	req.Header.Set(apiKeyHeader, key)
	return req
}
