// Package coreauth is the core-auth catalog module: tenants, accounts,
// memberships, invitations and API keys on top of Ory Kratos identities.
package coreauth

import (
	"context"
	"fmt"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/coreauth/session"
)

const moduleID = "core-auth"

// service is the state Provide builds and later phases use.
type service struct {
	cfg     Config
	store   *store
	codec   *session.Codec
	cookies session.Cookies
}

// provideSession builds the token codec and the cookie helpers. A bad
// GATBASE_SESSION_KEYS joins the shared report (rule text only, never the
// value); a missing one is already reported as required.
func (s *service) provideSession(b *core.Builder) {
	s.cookies = session.NewCookies(s.cfg.PublicBaseURL, s.cfg.SessionRefreshMaxAge)
	raw := s.cfg.SessionKeys.Reveal()
	if raw == "" {
		return
	}
	ring, problems := session.ParseKeyring(raw)
	for _, problem := range problems {
		b.Config().Invalid(keySessionKeys, problem.Error())
	}
	if ring == nil {
		return
	}
	isHTTPS := s.cfg.PublicBaseURL != nil && s.cfg.PublicBaseURL.Scheme == "https"
	if ring.HasPassphrase() && isHTTPS {
		b.Config().Invalid(keySessionKeys, "passphrase entries are local only")
		return
	}
	s.codec = session.NewCodec(ring, b.Clock())
}

// Module returns the core-auth catalog module. modules.go lists it first.
func Module() core.Module {
	s := &service{}
	return core.Module{
		ID:         moduleID,
		Migrations: migrationsFS,
		Provide:    s.provide,
	}
}

// provide parses the configuration (every problem goes to the shared reader,
// so core reports them together), keeps the pool without querying it, and
// registers the scopes later routes declare in their policies.
func (s *service) provide(_ context.Context, b *core.Builder) error {
	s.cfg = parseConfig(b.Config(), b.PublicBaseURL())
	s.store = newStore(b.Pool(), b.Clock())
	s.provideSession(b)

	scopes := []struct{ name, description string }{
		{"tenant:read", "Read the key's own tenant"},
		{"api-keys:read", "List the tenant's API key metadata"},
	}
	for _, scope := range scopes {
		err := b.Scopes().Register(scope.name, scope.description)
		if err != nil {
			return fmt.Errorf("register scope: %w", err)
		}
	}
	return nil
}
