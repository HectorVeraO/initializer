package kratos_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"errors"
	"fmt"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity/kratos"
)

// These tests run against the compose Kratos (oryd/kratos). They skip with a
// message when it is unreachable. The public base URL must equal the origin the
// stack's Kratos was started with.

// envChain returns the first non-empty value among keys, or fallback.
func envChain(fallback string, keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return fallback
}

// kratosURLs are the raw URLs of the Kratos under test.
type kratosURLs struct {
	public string
	admin  string
	base   string
}

// kratosURLsFromEnv resolves the URLs: the per-instance test override first,
// then the app configuration, then the ports of the reference band this file
// ships with.
func kratosURLsFromEnv() kratosURLs {
	return kratosURLs{
		public: envChain("http://127.0.0.1:10133", "APP_TEST_KRATOS_PUBLIC_URL", "APP_KRATOS_PUBLIC_URL"),
		admin:  envChain("http://127.0.0.1:10134", "APP_TEST_KRATOS_ADMIN_URL", "APP_KRATOS_ADMIN_URL"),
		base:   strings.TrimRight(envChain("http://localhost:10120", "APP_TEST_PUBLIC_BASE_URL", "APP_PUBLIC_BASE_URL"), "/"),
	}
}

type liveKratos struct {
	publicURL  *url.URL
	adminURL   *url.URL
	publicBase string
	public     *kratos.Public
	admin      *kratos.Admin
}

func randomHex(t *testing.T, n int) string {
	t.Helper()
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("random: %v", err)
	}
	return hex.EncodeToString(raw)
}

func newLiveKratos(t *testing.T) liveKratos {
	t.Helper()
	urls := kratosURLsFromEnv()
	live, err := newLiveKratosAt(mustParse(t, urls.public), mustParse(t, urls.admin), urls.base)
	if err != nil {
		t.Skipf("%v", err)
	}
	return live
}

// newLiveKratosAt probes Kratos at public and builds the harness on the URLs it
// is given. It returns an error instead of skipping, so the caller owns the skip
// message and a test can drive it against a stub.
func newLiveKratosAt(public, admin *url.URL, publicBase string) (liveKratos, error) {
	live := liveKratos{publicURL: public, adminURL: admin, publicBase: publicBase}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(public.String() + "/health/ready")
	if err != nil {
		return liveKratos{}, fmt.Errorf("Kratos unreachable at %s: %w", public, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return liveKratos{}, fmt.Errorf("Kratos at %s not ready: %d", public, resp.StatusCode)
	}
	live.public = kratos.NewPublic(public, "ory_kratos_session")
	live.admin = kratos.NewAdmin(admin)
	return live, nil
}

// kratosEnvKeys are every variable the URL defaults read.
var kratosEnvKeys = []string{
	"APP_TEST_KRATOS_PUBLIC_URL", "APP_KRATOS_PUBLIC_URL",
	"APP_TEST_KRATOS_ADMIN_URL", "APP_KRATOS_ADMIN_URL",
	"APP_TEST_PUBLIC_BASE_URL", "APP_PUBLIC_BASE_URL",
}

// setKratosEnv sets every variable of the chain, so a case sees exactly values.
func setKratosEnv(t *testing.T, values map[string]string) {
	t.Helper()
	for _, key := range kratosEnvKeys {
		t.Setenv(key, values[key])
	}
}

func TestKratosURLsFallBackFromTheTestOverrideToTheAppConfiguration(t *testing.T) {
	cases := []struct {
		name   string
		values map[string]string
		want   kratosURLs
	}{
		{
			name: "the per-instance test override wins",
			values: map[string]string{
				"APP_TEST_KRATOS_PUBLIC_URL": "http://override.test:10133",
				"APP_TEST_KRATOS_ADMIN_URL":  "http://override.test:10134",
				"APP_TEST_PUBLIC_BASE_URL":   "http://override.test:10120/",
				"APP_KRATOS_PUBLIC_URL":      "http://configured.test:10133",
				"APP_KRATOS_ADMIN_URL":       "http://configured.test:10134",
				"APP_PUBLIC_BASE_URL":        "http://configured.test:10120",
			},
			want: kratosURLs{
				public: "http://override.test:10133",
				admin:  "http://override.test:10134",
				base:   "http://override.test:10120",
			},
		},
		{
			name: "the app configuration defaults the URLs",
			values: map[string]string{
				"APP_KRATOS_PUBLIC_URL": "http://configured.test:10133",
				"APP_KRATOS_ADMIN_URL":  "http://configured.test:10134",
				"APP_PUBLIC_BASE_URL":   "http://configured.test:10120/",
			},
			want: kratosURLs{
				public: "http://configured.test:10133",
				admin:  "http://configured.test:10134",
				base:   "http://configured.test:10120",
			},
		},
		{
			name:   "the reference band ports are the last fallback",
			values: map[string]string{},
			want: kratosURLs{
				public: "http://127.0.0.1:10133",
				admin:  "http://127.0.0.1:10134",
				base:   "http://localhost:10120",
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			setKratosEnv(t, testCase.values)

			urls := kratosURLsFromEnv()

			if urls != testCase.want {
				t.Fatalf("urls = %+v, want %+v", urls, testCase.want)
			}
		})
	}
}

// stubKratos answers the harness's readiness probe and records every path it
// serves, so a test can see which URL the harness really called.
func stubKratos(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/health/ready":
			w.WriteHeader(http.StatusOK)
		case "/sessions/whoami":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server, &paths
}

func TestNewLiveKratosAtBuildsTheHarnessOnTheURLsItIsGiven(t *testing.T) {
	server, paths := stubKratos(t)

	live, err := newLiveKratosAt(mustParse(t, server.URL), mustParse(t, server.URL), server.URL)

	if err != nil {
		t.Fatalf("newLiveKratosAt on a ready stub: %v", err)
	}
	if live.publicURL.String() != server.URL || live.adminURL.String() != server.URL || live.publicBase != server.URL {
		t.Fatalf("harness URLs = %+v, want %s", live, server.URL)
	}
	_, err = live.public.WhoAmI(context.Background(), &http.Cookie{Name: "ory_kratos_session", Value: "probe"})
	if !errors.Is(err, kratos.ErrNoSession) {
		t.Fatalf("WhoAmI: want ErrNoSession from the stub's 401, got %v", err)
	}
	if got := strings.Join(*paths, " "); got != "/health/ready /sessions/whoami" {
		t.Fatalf("the stub saw %q: the harness called another URL", got)
	}
}

func TestNewLiveKratosAtRefusesAKratosThatIsNotReady(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	_, err := newLiveKratosAt(mustParse(t, server.URL), mustParse(t, server.URL), server.URL)

	if err == nil {
		t.Fatal("newLiveKratosAt accepted a Kratos answering 503")
	}
}

// account is an identity created for one test and deleted at cleanup.
type account struct {
	id       string
	email    string
	password string
}

func (l liveKratos) newAccount(t *testing.T) account {
	t.Helper()
	acc := account{
		email:    "it-" + randomHex(t, 6) + "@example.com",
		password: randomHex(t, 10),
	}
	identity, err := l.admin.CreatePasswordIdentity(context.Background(), acc.email, acc.password)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	acc.id = identity.ID
	t.Cleanup(func() {
		req, err := http.NewRequest(http.MethodDelete, l.adminURL.String()+"/admin/identities/"+acc.id, nil)
		if err != nil {
			t.Errorf("delete request: %v", err)
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("delete identity: %v", err)
			return
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("delete identity answered %d", resp.StatusCode)
		}
	})
	return acc
}

func (l liveKratos) proxyServer(t *testing.T) *httptest.Server {
	t.Helper()
	handler := kratos.NewProxy(l.publicURL, mustParse(t, l.publicBase), []string{"app_session"})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

type loginFlow struct {
	ID     string
	Action string
	CSRF   string
}

func createFlow(t *testing.T, client *http.Client, proxyURL string) loginFlow {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, proxyURL+"/kratos/self-service/login/browser", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create flow answered %d: %s", resp.StatusCode, body)
	}
	var parsed struct {
		ID string `json:"id"`
		UI struct {
			Action string `json:"action"`
			Nodes  []struct {
				Attributes struct {
					Name  string `json:"name"`
					Value any    `json:"value"`
				} `json:"attributes"`
			} `json:"nodes"`
		} `json:"ui"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode flow: %v", err)
	}
	flow := loginFlow{ID: parsed.ID, Action: parsed.UI.Action}
	for _, node := range parsed.UI.Nodes {
		if node.Attributes.Name == "csrf_token" {
			flow.CSRF, _ = node.Attributes.Value.(string)
		}
	}
	return flow
}

// submitLogin posts the flow through the proxy and returns the response.
func submitLogin(t *testing.T, client *http.Client, proxyURL string, flow loginFlow, acc account) (*http.Response, []byte) {
	t.Helper()
	payload, err := json.Marshal(map[string]string{
		"method":     "password",
		"identifier": acc.email,
		"password":   acc.password,
		"csrf_token": flow.CSRF,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost,
		proxyURL+"/kratos/self-service/login?flow="+url.QueryEscape(flow.ID), bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("submit login: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, body
}

func newJarClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("jar: %v", err)
	}
	return &http.Client{Jar: jar, Timeout: 10 * time.Second}
}

// login performs the whole proxied login and returns the Kratos session cookie.
func (l liveKratos) login(t *testing.T, acc account) *http.Cookie {
	t.Helper()
	server := l.proxyServer(t)
	client := newJarClient(t)
	flow := createFlow(t, client, server.URL)
	resp, body := submitLogin(t, client, server.URL, flow, acc)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login answered %d: %s", resp.StatusCode, body)
	}
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "ory_kratos_session" {
			return cookie
		}
	}
	t.Fatal("login set no ory_kratos_session cookie")
	return nil
}

func TestIntegrationFindIdentityByEmailIsCaseInsensitive(t *testing.T) {
	live := newLiveKratos(t)
	acc := live.newAccount(t)

	identity, found, err := live.admin.FindIdentityByEmail(context.Background(), strings.ToUpper(acc.email))
	if err != nil || !found {
		t.Fatalf("find: found=%v err=%v", found, err)
	}
	if identity.ID != acc.id || identity.Email != acc.email {
		t.Fatalf("identity = %+v, want id %s email %s", identity, acc.id, acc.email)
	}
}

func TestIntegrationDuplicateCreateAnswersIdentityExists(t *testing.T) {
	live := newLiveKratos(t)
	acc := live.newAccount(t)

	_, err := live.admin.CreatePasswordIdentity(context.Background(), strings.ToUpper(acc.email), randomHex(t, 10))

	if !errors.Is(err, kratos.ErrIdentityExists) {
		t.Fatalf("second create: want ErrIdentityExists, got %v", err)
	}
}

func TestIntegrationFindIdentityByEmailAbsent(t *testing.T) {
	live := newLiveKratos(t)

	_, found, err := live.admin.FindIdentityByEmail(context.Background(), "it-absent-"+randomHex(t, 6)+"@example.com")
	if err != nil || found {
		t.Fatalf("absent email: found=%v err=%v", found, err)
	}
}

func TestIntegrationLoginFlowActionPointsBackThroughTheProxy(t *testing.T) {
	live := newLiveKratos(t)
	server := live.proxyServer(t)

	flow := createFlow(t, newJarClient(t), server.URL)

	want := live.publicBase + "/kratos/self-service/login?flow="
	if !strings.HasPrefix(flow.Action, want) {
		t.Fatalf("ui.action = %q, want prefix %q", flow.Action, want)
	}
	if flow.CSRF == "" {
		t.Fatal("flow carries no csrf_token node")
	}
}

func TestIntegrationLoginThroughProxySetsSessionCookieAtRoot(t *testing.T) {
	live := newLiveKratos(t)
	acc := live.newAccount(t)

	cookie := live.login(t, acc)

	if cookie.Path != "/" || cookie.Value == "" {
		t.Fatalf("session cookie = %+v, want Path=/ and a value", cookie)
	}
}

func TestIntegrationWhoAmIReturnsTheLoggedInIdentity(t *testing.T) {
	live := newLiveKratos(t)
	acc := live.newAccount(t)
	cookie := live.login(t, acc)

	session, err := live.public.WhoAmI(context.Background(), cookie)

	if err != nil {
		t.Fatalf("WhoAmI: %v", err)
	}
	if session.IdentityID != acc.id || !session.Active || session.Email != acc.email ||
		session.ID == "" || !session.ExpiresAt.After(time.Now()) {
		t.Fatalf("session = %+v", session)
	}
}

func TestIntegrationLogoutRevokesTheSession(t *testing.T) {
	live := newLiveKratos(t)
	acc := live.newAccount(t)
	cookie := live.login(t, acc)

	if err := live.public.Logout(context.Background(), cookie); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	_, err := live.public.WhoAmI(context.Background(), cookie)
	if !errors.Is(err, kratos.ErrNoSession) {
		t.Fatalf("WhoAmI after logout: want ErrNoSession, got %v", err)
	}
	if err := live.public.Logout(context.Background(), cookie); !errors.Is(err, kratos.ErrNoSession) {
		t.Fatalf("second Logout: want ErrNoSession, got %v", err)
	}
}

func TestIntegrationProxyRejectsPathsOutsideTheAllowlist(t *testing.T) {
	live := newLiveKratos(t)
	server := live.proxyServer(t)
	paths := []string{
		"/kratos/admin/identities",
		"/kratos/self-service/registration/browser",
		"/kratos/sessions/whoami",
	}
	for _, path := range paths {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound || strings.TrimSpace(string(body)) != "Not Found" {
			t.Fatalf("GET %s answered %d %q, want the proxy's plain 404", path, resp.StatusCode, body)
		}
	}
}
