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

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity/kratos"
)

// These tests run against the compose Kratos (oryd/kratos). They skip with a
// message when it is unreachable. APP_TEST_PUBLIC_BASE_URL must equal the
// origin the shared stack's Kratos was started with.

func envOr(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
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
	publicRaw := envOr("APP_TEST_KRATOS_PUBLIC_URL", "http://127.0.0.1:10133")
	adminRaw := envOr("APP_TEST_KRATOS_ADMIN_URL", "http://127.0.0.1:10134")
	live := liveKratos{
		publicURL:  mustParse(t, publicRaw),
		adminURL:   mustParse(t, adminRaw),
		publicBase: strings.TrimRight(envOr("APP_TEST_PUBLIC_BASE_URL", "http://localhost:10120"), "/"),
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(publicRaw + "/health/ready")
	if err != nil {
		t.Skipf("Kratos unreachable at %s: %v", publicRaw, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("Kratos at %s not ready: %d", publicRaw, resp.StatusCode)
	}
	live.public = kratos.NewPublic(live.publicURL, "ory_kratos_session")
	live.admin = kratos.NewAdmin(live.adminURL)
	return live
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
