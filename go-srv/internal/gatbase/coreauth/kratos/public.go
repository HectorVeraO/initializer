package kratos

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const publicTimeout = 2 * time.Second

// Session is what whoami returns that the BFF uses.
type Session struct {
	ID         string
	Active     bool
	ExpiresAt  time.Time
	IdentityID string
	// Email is identity.traits.email, lower-cased.
	Email string
}

// Public calls Kratos public on its internal URL, forwarding only the Kratos
// cookie.
type Public struct {
	base       *url.URL
	cookieName string
	client     *http.Client
}

// NewPublic returns a client for the Kratos public API at baseURL; cookieName
// is the Kratos session cookie name (ory_kratos_session).
func NewPublic(baseURL *url.URL, cookieName string) *Public {
	return &Public{
		base:       baseURL,
		cookieName: cookieName,
		client:     newHTTPClient(publicTimeout),
	}
}

type whoamiBody struct {
	ID        string    `json:"id"`
	Active    bool      `json:"active"`
	ExpiresAt time.Time `json:"expires_at"`
	Identity  struct {
		ID     string `json:"id"`
		Traits struct {
			Email string `json:"email"`
		} `json:"traits"`
	} `json:"identity"`
}

// WhoAmI returns the active session behind kratosCookie. It returns
// ErrNoSession when the cookie is absent, rejected or inactive, and
// ErrUnavailable when Kratos cannot answer.
func (p *Public) WhoAmI(ctx context.Context, kratosCookie *http.Cookie) (Session, error) {
	if kratosCookie == nil || kratosCookie.Value == "" {
		return Session{}, ErrNoSession
	}
	req, err := newRequest(ctx, http.MethodGet, endpoint(p.base, "/sessions/whoami"), nil)
	if err != nil {
		return Session{}, err
	}
	req.AddCookie(&http.Cookie{Name: p.cookieName, Value: kratosCookie.Value})
	status, body, err := do(p.client, req)
	if err != nil {
		return Session{}, err
	}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return Session{}, ErrNoSession
	case status != http.StatusOK:
		return Session{}, ErrUnavailable
	}
	var parsed whoamiBody
	if err := decode(body, &parsed); err != nil {
		return Session{}, err
	}
	if !parsed.Active {
		return Session{}, ErrNoSession
	}
	session := Session{
		ID:         parsed.ID,
		Active:     true,
		ExpiresAt:  parsed.ExpiresAt,
		IdentityID: parsed.Identity.ID,
		Email:      strings.ToLower(parsed.Identity.Traits.Email),
	}
	return session, nil
}

// Logout revokes the Kratos session behind kratosCookie. It returns
// ErrNoSession when Kratos holds no session for it.
func (p *Public) Logout(ctx context.Context, kratosCookie *http.Cookie) error {
	if kratosCookie == nil || kratosCookie.Value == "" {
		return ErrNoSession
	}
	token, err := p.logoutToken(ctx, kratosCookie)
	if err != nil {
		return err
	}
	target := endpoint(p.base, "/self-service/logout")
	target.RawQuery = url.Values{"token": {token}}.Encode()
	req, err := newRequest(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.AddCookie(&http.Cookie{Name: p.cookieName, Value: kratosCookie.Value})
	status, _, err := do(p.client, req)
	if err != nil {
		return err
	}
	switch {
	case status == http.StatusNoContent:
		return nil
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return ErrNoSession
	default:
		return ErrUnavailable
	}
}

func (p *Public) logoutToken(ctx context.Context, kratosCookie *http.Cookie) (string, error) {
	req, err := newRequest(ctx, http.MethodGet, endpoint(p.base, "/self-service/logout/browser"), nil)
	if err != nil {
		return "", err
	}
	req.AddCookie(&http.Cookie{Name: p.cookieName, Value: kratosCookie.Value})
	status, body, err := do(p.client, req)
	if err != nil {
		return "", err
	}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "", ErrNoSession
	case status != http.StatusOK:
		return "", ErrUnavailable
	}
	var parsed struct {
		LogoutToken string `json:"logout_token"`
	}
	if err := decode(body, &parsed); err != nil {
		return "", err
	}
	if parsed.LogoutToken == "" {
		return "", ErrUnavailable
	}
	return parsed.LogoutToken, nil
}
