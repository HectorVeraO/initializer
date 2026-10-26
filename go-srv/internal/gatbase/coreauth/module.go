// Package coreauth is the core-auth catalog module: tenants, accounts,
// memberships, invitations and API keys on top of Ory Kratos identities.
package coreauth

import (
	"context"
	"fmt"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/coreauth/kratos"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/coreauth/session"
)

const moduleID = "core-auth"

// service is the state Provide builds and later phases use.
type service struct {
	cfg     Config
	store   *store
	codec   *session.Codec
	cookies session.Cookies
	// admin is nil only when GATBASE_KRATOS_ADMIN_URL is invalid, which fails Build.
	admin *kratos.Admin
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
		Start:      s.start,
	}
}

// provide parses the configuration (every problem goes to the shared reader,
// so core reports them together), keeps the pool without querying it, and
// registers the scopes later routes declare in their policies.
func (s *service) provide(_ context.Context, b *core.Builder) error {
	s.cfg = parseConfig(b.Config(), b.PublicBaseURL())
	s.store = newStore(b.Pool(), b.Clock())
	s.provideSession(b)
	s.provideKratos(b)

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

// provideKratos builds the admin client without any I/O. Its transport
// carries the W3C traceparent of the request or job context.
func (s *service) provideKratos(b *core.Builder) {
	if s.cfg.KratosAdminURL == nil {
		return
	}
	var opts []kratos.Option
	telemetry := b.Telemetry()
	if telemetry != nil {
		opts = append(opts, kratos.WithTransport(telemetry.Transport(nil)))
	}
	s.admin = kratos.NewAdmin(s.cfg.KratosAdminURL, opts...)
}
