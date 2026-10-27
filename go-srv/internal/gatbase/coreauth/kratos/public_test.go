package kratos_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/coreauth/kratos"
)

const kratosCookieName = "ory_kratos_session"

// recorded is one request as the fake Kratos saw it.
type recorded struct {
	Method string
	Path   string
	Query  url.Values
	Accept string
	Cookie string
	Header http.Header
	Body   string
}

// fakeKratos is an httptest.Server playing Kratos; it records every request.
type fakeKratos struct {
	*httptest.Server
	mu   sync.Mutex
	seen []recorded
}

func newFakeKratos(t *testing.T, handler http.HandlerFunc) *fakeKratos {
	t.Helper()
	fake := &fakeKratos{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		fake.mu.Lock()
		fake.seen = append(fake.seen, recorded{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Accept: r.Header.Get("Accept"),
			Cookie: r.Header.Get("Cookie"),
			Header: r.Header.Clone(),
			Body:   string(body),
		})
		fake.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(fake.Close)
	return fake
}

func (f *fakeKratos) requests() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.seen...)
}

func (f *fakeKratos) url(t *testing.T) *url.URL {
	t.Helper()
	parsed, err := url.Parse(f.URL)
	if err != nil {
		t.Fatalf("parse fake url: %v", err)
	}
	return parsed
}

func jsonReply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// redirectReply answers 302 with a Location header.
func redirectReply(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Location", "/elsewhere")
	w.WriteHeader(http.StatusFound)
}

// closeConnection drops the connection without answering.
func closeConnection(w http.ResponseWriter, _ *http.Request) {
	hijacker := w.(http.Hijacker)
	conn, _, err := hijacker.Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

const activeSession = `{"id":"sess-1","active":true,"expires_at":"2026-10-02T12:00:00Z",
"identity":{"id":"ident-1","traits":{"email":"Jane.Doe@example.com"}}}`

func TestWhoAmIReturnsActiveSession(t *testing.T) {
	fake := newFakeKratos(t, jsonReply(200, activeSession))
	public := kratos.NewPublic(fake.url(t), kratosCookieName)

	session, err := public.WhoAmI(context.Background(), &http.Cookie{Name: "anything", Value: "cookie-value"})
	if err != nil {
		t.Fatalf("WhoAmI: %v", err)
	}

	wantExpiry := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	if session.ID != "sess-1" || !session.Active || !session.ExpiresAt.Equal(wantExpiry) ||
		session.IdentityID != "ident-1" || session.Email != "jane.doe@example.com" {
		t.Fatalf("unexpected session: %+v", session)
	}
}

func TestWhoAmISendsOnlyTheKratosCookie(t *testing.T) {
	fake := newFakeKratos(t, jsonReply(200, activeSession))
	public := kratos.NewPublic(fake.url(t), kratosCookieName)

	if _, err := public.WhoAmI(context.Background(), &http.Cookie{Name: "x", Value: "cookie-value"}); err != nil {
		t.Fatalf("WhoAmI: %v", err)
	}

	got := fake.requests()
	if len(got) != 1 {
		t.Fatalf("want 1 request, got %d", len(got))
	}
	req := got[0]
	if req.Method != http.MethodGet || req.Path != "/sessions/whoami" {
		t.Fatalf("unexpected request %s %s", req.Method, req.Path)
	}
	if req.Cookie != kratosCookieName+"=cookie-value" {
		t.Fatalf("Cookie header = %q", req.Cookie)
	}
	if req.Accept != "application/json" {
		t.Fatalf("Accept = %q", req.Accept)
	}
	for _, header := range []string{"Authorization", "X-Api-Key", "X-Xsrf-Token"} {
		if req.Header.Get(header) != "" {
			t.Fatalf("header %s must not be sent", header)
		}
	}
}

func TestWhoAmIWithoutCookieNeverCallsKratos(t *testing.T) {
	fake := newFakeKratos(t, jsonReply(200, activeSession))
	public := kratos.NewPublic(fake.url(t), kratosCookieName)

	_, errNil := public.WhoAmI(context.Background(), nil)
	_, errEmpty := public.WhoAmI(context.Background(), &http.Cookie{Name: "x"})

	if !errors.Is(errNil, kratos.ErrNoSession) || !errors.Is(errEmpty, kratos.ErrNoSession) {
		t.Fatalf("want ErrNoSession, got %v and %v", errNil, errEmpty)
	}
	if n := len(fake.requests()); n != 0 {
		t.Fatalf("Kratos was called %d times", n)
	}
}

func TestWhoAmIMapsAnswersToErrors(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    error
	}{
		{"401", jsonReply(401, `{}`), kratos.ErrNoSession},
		{"403", jsonReply(403, `{}`), kratos.ErrNoSession},
		{"inactive", jsonReply(200, `{"id":"s","active":false,"identity":{"id":"i"}}`), kratos.ErrNoSession},
		{"302", redirectReply, kratos.ErrUnavailable},
		{"404", jsonReply(404, `{}`), kratos.ErrUnavailable},
		{"500", jsonReply(500, `{}`), kratos.ErrUnavailable},
		{"503", jsonReply(503, `{}`), kratos.ErrUnavailable},
		{"closed connection", closeConnection, kratos.ErrUnavailable},
		{"undecodable body", jsonReply(200, `not json`), kratos.ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeKratos(t, tc.handler)
			public := kratos.NewPublic(fake.url(t), kratosCookieName)

			_, err := public.WhoAmI(context.Background(), &http.Cookie{Name: "x", Value: "v"})

			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestWhoAmIAnswerOverOneMiBIsUnavailable(t *testing.T) {
	padding := strings.Repeat("a", 2<<20)
	body := `{"id":"s","active":true,"identity":{"id":"i"},"pad":"` + padding + `"}`
	fake := newFakeKratos(t, jsonReply(200, body))
	public := kratos.NewPublic(fake.url(t), kratosCookieName)

	_, err := public.WhoAmI(context.Background(), &http.Cookie{Name: "x", Value: "v"})

	if !errors.Is(err, kratos.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

func TestWhoAmIContextDeadlineIsUnavailable(t *testing.T) {
	release := make(chan struct{})
	fake := newFakeKratos(t, func(w http.ResponseWriter, _ *http.Request) {
		<-release
	})
	t.Cleanup(func() { close(release) })
	public := kratos.NewPublic(fake.url(t), kratosCookieName)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := public.WhoAmI(ctx, &http.Cookie{Name: "x", Value: "v"})

	if !errors.Is(err, kratos.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

func TestWhoAmIDoesNotLeakTheCookieValueInErrors(t *testing.T) {
	fake := newFakeKratos(t, closeConnection)
	public := kratos.NewPublic(fake.url(t), kratosCookieName)

	_, err := public.WhoAmI(context.Background(), &http.Cookie{Name: "x", Value: "secret-cookie-value"})

	if err == nil || strings.Contains(err.Error(), "secret-cookie-value") {
		t.Fatalf("error must exist and not carry the cookie: %v", err)
	}
}

// logoutFake answers the two-step logout like Kratos.
func logoutFake(t *testing.T, firstStatus int, secondStatus int) *fakeKratos {
	t.Helper()
	return newFakeKratos(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/self-service/logout/browser":
			jsonReply(firstStatus, `{"logout_token":"tok-1","logout_url":"http://x/logout"}`)(w, r)
		case "/self-service/logout":
			w.WriteHeader(secondStatus)
		default:
			w.WriteHeader(404)
		}
	})
}

func TestLogoutCallsBothStepsWithTheCookie(t *testing.T) {
	fake := logoutFake(t, 200, 204)
	public := kratos.NewPublic(fake.url(t), kratosCookieName)

	if err := public.Logout(context.Background(), &http.Cookie{Name: "x", Value: "cv"}); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	got := fake.requests()
	if len(got) != 2 {
		t.Fatalf("want 2 requests, got %d", len(got))
	}
	if got[0].Path != "/self-service/logout/browser" || got[1].Path != "/self-service/logout" {
		t.Fatalf("unexpected paths %q then %q", got[0].Path, got[1].Path)
	}
	if got[1].Query.Get("token") != "tok-1" {
		t.Fatalf("token = %q", got[1].Query.Get("token"))
	}
	for _, req := range got {
		if req.Method != http.MethodGet || req.Accept != "application/json" ||
			req.Cookie != kratosCookieName+"=cv" {
			t.Fatalf("unexpected request shape: %+v", req)
		}
	}
}

func TestLogoutMapsAnswersToErrors(t *testing.T) {
	cases := []struct {
		name   string
		first  int
		second int
		want   error
	}{
		{"no session on first call", 401, 204, kratos.ErrNoSession},
		{"first call 500", 500, 204, kratos.ErrUnavailable},
		{"second call 500", 200, 500, kratos.ErrUnavailable},
		{"second call unexpected 200", 200, 200, kratos.ErrUnavailable},
		{"second call 401", 200, 401, kratos.ErrNoSession},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := logoutFake(t, tc.first, tc.second)
			public := kratos.NewPublic(fake.url(t), kratosCookieName)

			err := public.Logout(context.Background(), &http.Cookie{Name: "x", Value: "cv"})

			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestLogoutDoesNotFollowRedirects(t *testing.T) {
	fake := newFakeKratos(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/self-service/logout/browser" {
			jsonReply(200, `{"logout_token":"tok-1"}`)(w, r)
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusSeeOther)
	})
	public := kratos.NewPublic(fake.url(t), kratosCookieName)

	err := public.Logout(context.Background(), &http.Cookie{Name: "x", Value: "cv"})

	if !errors.Is(err, kratos.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable for a 303, got %v", err)
	}
	for _, req := range fake.requests() {
		if req.Path == "/elsewhere" {
			t.Fatal("redirect was followed")
		}
	}
}

func TestLogoutWithoutCookieIsNoSession(t *testing.T) {
	fake := logoutFake(t, 200, 204)
	public := kratos.NewPublic(fake.url(t), kratosCookieName)

	err := public.Logout(context.Background(), nil)

	if !errors.Is(err, kratos.ErrNoSession) || len(fake.requests()) != 0 {
		t.Fatalf("want ErrNoSession without a call, got %v", err)
	}
}

func TestLogoutWithoutTokenIsUnavailable(t *testing.T) {
	fake := newFakeKratos(t, jsonReply(200, `{}`))
	public := kratos.NewPublic(fake.url(t), kratosCookieName)

	err := public.Logout(context.Background(), &http.Cookie{Name: "x", Value: "cv"})

	if !errors.Is(err, kratos.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

// headerStampTransport adds a header to every request it forwards.
type headerStampTransport struct{}

func (headerStampTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	stamped := req.Clone(req.Context())
	stamped.Header.Set("Traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	return http.DefaultTransport.RoundTrip(stamped)
}

func TestWithTransportSendsCallsThroughTheGivenTransport(t *testing.T) {
	fake := newFakeKratos(t, jsonReply(200, `{"id":"s","active":true,"identity":{"id":"i"}}`))
	public := kratos.NewPublic(fake.url(t), kratosCookieName, kratos.WithTransport(headerStampTransport{}))

	_, err := public.WhoAmI(context.Background(), &http.Cookie{Name: "x", Value: "v"})

	if err != nil {
		t.Fatalf("WhoAmI: %v", err)
	}
	got := fake.requests()[0]
	if got.Header.Get("Traceparent") == "" {
		t.Fatalf("traceparent not forwarded, headers %v", got.Header)
	}
}
