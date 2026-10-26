package kratos_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/coreauth/kratos"
)

var bffCookies = []string{"gb_session", "__Host-gb_session"}

func mustParse(t testing.TB, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return parsed
}

func newProxyServer(t *testing.T, upstream *fakeKratos) *httptest.Server {
	t.Helper()
	handler := kratos.NewProxy(upstream.url(t), mustParse(t, "https://app.example.com"), bffCookies)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func send(t *testing.T, method, rawURL string, mutate func(*http.Request)) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if mutate != nil {
		mutate(req)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestProxyKeepsTheUpstreamBasePath(t *testing.T) {
	upstream := newFakeKratos(t, jsonReply(200, `{}`))
	base := upstream.url(t)
	base.Path = "/base"
	server := httptest.NewServer(kratos.NewProxy(base, mustParse(t, "https://app.example.com"), bffCookies))
	t.Cleanup(server.Close)

	send(t, "GET", server.URL+"/kratos/self-service/login/browser", nil)

	seen := upstream.requests()
	if len(seen) != 1 || seen[0].Path != "/base/self-service/login/browser" {
		t.Fatalf("upstream saw %+v, want one request at /base/self-service/login/browser", seen)
	}
}

func TestProxyForwardsOnlyAllowlistedPairs(t *testing.T) {
	cases := []struct {
		method  string
		path    string
		reaches bool
	}{
		{"GET", "/kratos/self-service/login/browser", true},
		{"GET", "/kratos/self-service/login/flows?id=abc", true},
		{"POST", "/kratos/self-service/login?flow=abc", true},
		{"GET", "/kratos/self-service/errors?id=abc", true},
		{"POST", "/kratos/self-service/login/browser", false},
		{"GET", "/kratos/self-service/login", false},
		{"PUT", "/kratos/self-service/login", false},
		{"DELETE", "/kratos/self-service/login", false},
		{"HEAD", "/kratos/self-service/login/browser", false},
		{"POST", "/kratos/self-service/login/flows", false},
		{"GET", "/kratos/admin/identities", false},
		{"GET", "/kratos/self-service/registration/browser", false},
		{"GET", "/kratos/self-service/logout/browser", false},
		{"GET", "/kratos/sessions/whoami", false},
		{"GET", "/kratos/../admin/identities", false},
		{"GET", "/kratos/self-service/login/../../admin/identities", false},
		{"GET", "/kratos/%2e%2e/admin/identities", false},
		{"GET", "/kratos/%252e%252e/admin/identities", false},
		{"GET", "/kratos/self-service/login/browser/", false},
		{"GET", "/kratos", false},
		{"GET", "/kratos/", false},
		{"GET", "/kratosx/self-service/login/browser", false},
		{"GET", "/self-service/login/browser", false},
		{"GET", "/KRATOS/self-service/login/browser", false},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			upstream := newFakeKratos(t, jsonReply(200, `{}`))
			server := newProxyServer(t, upstream)

			resp := send(t, tc.method, server.URL+tc.path, nil)

			reached := len(upstream.requests()) > 0
			if reached != tc.reaches {
				t.Fatalf("reached upstream = %v, want %v", reached, tc.reaches)
			}
			if !tc.reaches {
				body, _ := io.ReadAll(resp.Body)
				if resp.StatusCode != http.StatusNotFound {
					t.Fatalf("want 404, got %d %q", resp.StatusCode, body)
				}
				isPlainBody := strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") &&
					strings.TrimSpace(string(body)) == "Not Found"
				if tc.method != "HEAD" && !isPlainBody {
					t.Fatalf("want plain 404 body, got %d %q", resp.StatusCode, body)
				}
			}
		})
	}
}

func TestProxyStripsPrefixAndKeepsQuery(t *testing.T) {
	upstream := newFakeKratos(t, jsonReply(200, `{}`))
	server := newProxyServer(t, upstream)

	send(t, "GET", server.URL+"/kratos/self-service/login/flows?id=flow-1&x=y%20z", nil)

	got := upstream.requests()[0]
	if got.Path != "/self-service/login/flows" || got.Query.Get("id") != "flow-1" || got.Query.Get("x") != "y z" {
		t.Fatalf("unexpected upstream request %+v", got)
	}
}

func TestProxyRemovesBFFCredentials(t *testing.T) {
	upstream := newFakeKratos(t, jsonReply(200, `{}`))
	server := newProxyServer(t, upstream)

	send(t, "GET", server.URL+"/kratos/self-service/login/browser", func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "gb_session", Value: "bff-token"})
		r.AddCookie(&http.Cookie{Name: "__Host-gb_session", Value: "bff-token-2"})
		r.AddCookie(&http.Cookie{Name: "XSRF-TOKEN", Value: "csrf"})
		r.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "kratos-value"})
		r.AddCookie(&http.Cookie{Name: "csrf_token_abc", Value: "kratos-csrf"})
		r.Header.Set("Authorization", "Bearer bff")
		r.Header.Set("X-API-Key", "key")
		r.Header.Set("X-XSRF-TOKEN", "csrf")
		r.Header.Set("X-Active-Tenant-Id", "tenant-a")
		r.Header.Set("Accept", "application/json")
	})

	got := upstream.requests()[0]
	for _, leaked := range []string{"gb_session", "bff-token", "XSRF-TOKEN"} {
		if strings.Contains(got.Cookie, leaked) {
			t.Fatalf("Cookie still carries %q: %q", leaked, got.Cookie)
		}
	}
	if !strings.Contains(got.Cookie, "ory_kratos_session=kratos-value") ||
		!strings.Contains(got.Cookie, "csrf_token_abc=kratos-csrf") {
		t.Fatalf("Kratos cookies lost: %q", got.Cookie)
	}
	for _, header := range []string{"Authorization", "X-Api-Key", "X-Xsrf-Token", "X-Active-Tenant-Id"} {
		if got.Header.Get(header) != "" {
			t.Fatalf("header %s reached Kratos", header)
		}
	}
	if got.Accept != "application/json" {
		t.Fatalf("Accept = %q", got.Accept)
	}
}

func TestProxySetsForwardedHeadersFromPublicBase(t *testing.T) {
	upstream := newFakeKratos(t, jsonReply(200, `{}`))
	server := newProxyServer(t, upstream)

	send(t, "GET", server.URL+"/kratos/self-service/login/browser", func(r *http.Request) {
		r.Header.Set("X-Forwarded-Host", "attacker.example.net")
		r.Header.Set("X-Forwarded-Proto", "ftp")
	})

	got := upstream.requests()[0]
	if got.Header.Get("X-Forwarded-Proto") != "https" || got.Header.Get("X-Forwarded-Host") != "app.example.com" {
		t.Fatalf("forwarded headers: proto=%q host=%q",
			got.Header.Get("X-Forwarded-Proto"), got.Header.Get("X-Forwarded-Host"))
	}
}

func TestProxyPassesSetCookieThroughUnchanged(t *testing.T) {
	const setCookie = "ory_kratos_session=abc; Path=/; HttpOnly; SameSite=Lax"
	upstream := newFakeKratos(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", setCookie)
		w.Header().Add("Set-Cookie", "csrf_token_x=y; Path=/; HttpOnly")
		w.WriteHeader(200)
	})
	server := newProxyServer(t, upstream)

	resp := send(t, "POST", server.URL+"/kratos/self-service/login?flow=f", nil)

	got := resp.Header.Values("Set-Cookie")
	if len(got) != 2 || got[0] != setCookie || got[1] != "csrf_token_x=y; Path=/; HttpOnly" {
		t.Fatalf("Set-Cookie = %q", got)
	}
}

func TestProxyAnswersPlain502WhenKratosIsDown(t *testing.T) {
	upstream := newFakeKratos(t, jsonReply(200, `{}`))
	server := newProxyServer(t, upstream)
	upstream.Close()

	resp := send(t, "GET", server.URL+"/kratos/self-service/login/browser", nil)

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadGateway ||
		!strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") ||
		strings.TrimSpace(string(body)) != "Bad Gateway" {
		t.Fatalf("want plain 502, got %d %q", resp.StatusCode, body)
	}
}

func TestProxyPassesKratosStatusAndBodyThrough(t *testing.T) {
	upstream := newFakeKratos(t, jsonReply(400, `{"ui":{"messages":[]}}`))
	server := newProxyServer(t, upstream)

	resp := send(t, "POST", server.URL+"/kratos/self-service/login?flow=f", nil)

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 400 || string(body) != `{"ui":{"messages":[]}}` {
		t.Fatalf("got %d %q", resp.StatusCode, body)
	}
}

func FuzzProxyPath(f *testing.F) {
	allowedRequests := map[string]bool{
		"GET /self-service/login/browser": true,
		"GET /self-service/login/flows":   true,
		"GET /self-service/errors":        true,
		"POST /self-service/login":        true,
	}
	var reachedMu sync.Mutex
	var reachedPaths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reachedMu.Lock()
		reachedPaths = append(reachedPaths, r.Method+" "+r.URL.Path)
		reachedMu.Unlock()
		w.WriteHeader(200)
	}))
	f.Cleanup(upstream.Close)
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		f.Fatalf("parse: %v", err)
	}
	base, _ := url.Parse("http://localhost:10120")
	proxy := kratos.NewProxy(upstreamURL, base, bffCookies)

	for _, seed := range []string{
		"/kratos/self-service/login/browser", "/kratos/../admin/identities", "/kratos/%2e%2e/admin",
		"/kratos", "/kratos/", "/kratos//self-service/login", "/kratos/self-service/login/%2e%2e/%2e%2e/admin",
	} {
		f.Add(seed, "GET")
		f.Add(seed, "POST")
	}

	f.Fuzz(func(t *testing.T, path string, method string) {
		reachedMu.Lock()
		reachedPaths = reachedPaths[:0]
		reachedMu.Unlock()
		req := &http.Request{
			Method: method,
			URL:    &url.URL{Path: path},
			Header: make(http.Header),
			Host:   "localhost",
		}
		rec := httptest.NewRecorder()

		proxy.ServeHTTP(rec, req)

		reachedMu.Lock()
		reachedNow := append([]string(nil), reachedPaths...)
		reachedMu.Unlock()
		for _, reached := range reachedNow {
			if !allowedRequests[reached] {
				t.Fatalf("upstream received %q for %s %q", reached, method, path)
			}
			reachedPath := strings.TrimPrefix(reached, method+" ")
			if "/kratos"+reachedPath != path {
				t.Fatalf("upstream path %q does not match input %q", reached, path)
			}
		}
		if len(reachedNow) == 0 && rec.Code == http.StatusOK {
			t.Fatalf("200 without reaching upstream for %s %q", method, path)
		}
	})
}

type headerRoundTripper struct{ next http.RoundTripper }

func (h headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	return h.next.RoundTrip(req)
}

func TestProxyTransportWrapperDecoratesTheKratosHop(t *testing.T) {
	upstream := newFakeKratos(t, jsonReply(200, `{}`))
	wrap := func(next http.RoundTripper) http.RoundTripper { return headerRoundTripper{next} }
	handler := kratos.NewProxy(upstream.url(t), mustParse(t, "https://app.example.com"), bffCookies, kratos.WithTransportWrapper(wrap))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	resp := send(t, "GET", server.URL+"/kratos/self-service/login/browser", nil)

	reqs := upstream.requests()
	if resp.StatusCode != 200 || len(reqs) != 1 || reqs[0].Header.Get("Traceparent") == "" {
		t.Fatalf("status %d, upstream requests %+v", resp.StatusCode, reqs)
	}
}
