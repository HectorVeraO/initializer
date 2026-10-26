package session

import (
	"net/http"
	"net/url"
	"time"
)

const (
	csrfCookieName          = "XSRF-TOKEN"
	sessionCookieName       = "gb_session"
	secureSessionCookieName = "__Host-gb_session"
)

// Cookies writes and reads the BFF cookies; built once from the public base URL.
type Cookies struct {
	sessionName string
	secure      bool
	maxAge      int
}

// NewCookies derives the cookie names and attributes from the scheme of
// publicBase and sets Max-Age to refreshMaxAge in whole seconds.
func NewCookies(publicBase *url.URL, refreshMaxAge time.Duration) Cookies {
	isHTTPS := publicBase != nil && publicBase.Scheme == "https"
	name := sessionCookieName
	if isHTTPS {
		name = secureSessionCookieName
	}
	return Cookies{
		sessionName: name,
		secure:      isHTTPS,
		maxAge:      int(refreshMaxAge / time.Second),
	}
}

// SessionName is the name of the session cookie.
func (c Cookies) SessionName() string {
	return c.sessionName
}

// Session returns the session cookie value; ok is false when absent or empty.
func (c Cookies) Session(r *http.Request) (token string, ok bool) {
	cookie, err := r.Cookie(c.sessionName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	return cookie.Value, true
}

// SetSession writes the session cookie carrying token.
func (c Cookies) SetSession(w http.ResponseWriter, token string) {
	http.SetCookie(w, c.build(c.sessionName, token, true, c.maxAge))
}

// SetCSRF writes the script-readable CSRF cookie carrying the csrf claim.
func (c Cookies) SetCSRF(w http.ResponseWriter, csrf string) {
	http.SetCookie(w, c.build(csrfCookieName, csrf, false, c.maxAge))
}

// ClearSession expires the session cookie.
func (c Cookies) ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, c.build(c.sessionName, "", true, -1))
}

// ClearCSRF expires the CSRF cookie.
func (c Cookies) ClearCSRF(w http.ResponseWriter) {
	http.SetCookie(w, c.build(csrfCookieName, "", false, -1))
}

// build sets every attribute in one place; maxAge -1 writes Max-Age=0.
func (c Cookies) build(name, value string, httpOnly bool, maxAge int) *http.Cookie {
	cookie := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: httpOnly,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
	}
	return cookie
}
