package identity

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
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
