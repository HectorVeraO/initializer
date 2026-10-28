package session_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity/session"
)

func cookiesFor(t *testing.T, base string) session.Cookies {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return session.NewCookies(u, 24*time.Hour)
}

func findCookie(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("cookie %s not set", name)
	return nil
}

func TestHTTPCookiesAreNotSecure(t *testing.T) {
	cookies := cookiesFor(t, "http://localhost:10120")
	rec := httptest.NewRecorder()
	cookies.SetSession(rec, "tok")
	cookies.SetCSRF(rec, "csrf")
	if cookies.SessionName() != "app_session" {
		t.Fatalf("name %q", cookies.SessionName())
	}
	s := findCookie(t, rec, "app_session")
	if s.Secure || !s.HttpOnly || s.SameSite != http.SameSiteLaxMode || s.Path != "/" || s.Domain != "" || s.MaxAge != 86400 || s.Value != "tok" {
		t.Errorf("session cookie: %+v", s)
	}
	x := findCookie(t, rec, "XSRF-TOKEN")
	if x.Secure || x.HttpOnly || x.SameSite != http.SameSiteLaxMode || x.Path != "/" || x.Domain != "" || x.MaxAge != 86400 || x.Value != "csrf" {
		t.Errorf("csrf cookie: %+v", x)
	}
}

func TestHTTPSCookiesUseHostPrefixAndSecure(t *testing.T) {
	cookies := cookiesFor(t, "https://app.example.com")
	rec := httptest.NewRecorder()
	cookies.SetSession(rec, "tok")
	cookies.SetCSRF(rec, "csrf")
	if cookies.SessionName() != "__Host-app_session" {
		t.Fatalf("name %q", cookies.SessionName())
	}
	s := findCookie(t, rec, "__Host-app_session")
	if !s.Secure || !s.HttpOnly || s.Domain != "" || s.Path != "/" {
		t.Errorf("session cookie: %+v", s)
	}
	x := findCookie(t, rec, "XSRF-TOKEN")
	if !x.Secure || x.HttpOnly {
		t.Errorf("csrf cookie: %+v", x)
	}
}

func TestClearingWritesMaxAgeZeroOnSameNameAndPath(t *testing.T) {
	cookies := cookiesFor(t, "https://app.example.com")
	rec := httptest.NewRecorder()
	cookies.ClearSession(rec)
	cookies.ClearCSRF(rec)
	for _, name := range []string{"__Host-app_session", "XSRF-TOKEN"} {
		c := findCookie(t, rec, name)
		if c.Value != "" || c.Path != "/" || c.MaxAge != -1 {
			t.Errorf("%s: %+v", name, c)
		}
	}
	header := strings.Join(rec.Result().Header.Values("Set-Cookie"), "\n")
	if strings.Count(header, "Max-Age=0") != 2 {
		t.Errorf("want two Max-Age=0, got %q", header)
	}
}

func TestSessionReadsOnlyItsOwnNonEmptyCookie(t *testing.T) {
	cookies := cookiesFor(t, "http://localhost:10120")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	_, ok := cookies.Session(req)
	if ok {
		t.Fatal("absent cookie reported present")
	}
	req.AddCookie(&http.Cookie{Name: "app_session", Value: ""})
	_, ok = cookies.Session(req)
	if ok {
		t.Fatal("empty cookie reported present")
	}
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-app_session", Value: "other"})
	_, ok = cookies.Session(req)
	if ok {
		t.Fatal("other scheme's cookie accepted")
	}
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "app_session", Value: "tok"})
	got, ok := cookies.Session(req)
	if !ok || got != "tok" {
		t.Fatalf("got %q %v", got, ok)
	}
}
