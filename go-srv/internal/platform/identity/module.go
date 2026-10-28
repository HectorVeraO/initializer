// Package identity is the identity catalog module: tenants, accounts,
// memberships, invitations and API keys on top of Ory Kratos identities.
package identity

import (
	"context"
	"fmt"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity/kratos"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity/session"
)

const moduleID = "identity"

// service is the state Provide builds and later phases use.
type service struct {
	cfg     Config
	store   *store
	codec   *session.Codec
	cookies session.Cookies
	// admin is nil only when APP_KRATOS_ADMIN_URL is invalid, which fails Build.
	admin *kratos.Admin
	// public is nil only when APP_KRATOS_PUBLIC_URL is invalid, which fails Build.
	public *kratos.Public
	access *access
	// tenancy backs the tenant, membership and invitation routes.
	tenancy *tenancy
	// apiKeys backs the API key routes.
	apiKeys *apiKeys
}

// provideSession builds the token codec and the cookie helpers. A bad
// APP_SESSION_KEYS joins the shared report (rule text only, never the
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
	resolver := newSessionResolver(s.codec, s.cookies, s.cfg.SessionRefreshMaxAge, b.Logger())
	err := b.ProvideSessionResolver(resolver)
	if err != nil {
		b.Config().Invalid(keySessionKeys, "session resolver: "+err.Error())
	}
}

// Module returns the identity catalog module. modules.go lists it first.
func Module() core.Module {
	s := &service{}
	return core.Module{
		ID:         moduleID,
		Migrations: migrationsFS,
		Provide:    s.provide,
		Mount:      s.mount,
		Start:      s.start,
	}
}

// provide parses the configuration (every problem goes to the shared reader,
// so core reports them together), keeps the pool without querying it, and
// registers the scopes later routes declare in their policies.
func (s *service) provide(_ context.Context, b *core.Builder) error {
	s.cfg = parseConfig(b.Config(), b.PublicBaseURL())
	s.store = newStore(b.Pool(), b.Clock())
	s.access = newAccess(b.Pool(), b.Clock(), s.cfg.PlatformOperatorEmails)
	s.provideSession(b)
	s.provideKratos(b)
	s.tenancy = newTenancy(b.Pool(), b.Clock(), s.admin, s.cfg)
	s.apiKeys = newAPIKeys(b.Pool(), b.Clock())
	err := b.ProvideTenantDirectory(&tenantDirectory{pool: b.Pool()})
	if err != nil {
		return fmt.Errorf("provide tenant directory: %w", err)
	}
	keyResolver := newAPIKeyResolver(b.Pool(), b.Clock(), b.Scopes(), b.Logger())
	err = b.ProvideAPIKeyResolver(keyResolver)
	if err != nil {
		return fmt.Errorf("provide api key resolver: %w", err)
	}

	scopes := []struct{ name, description string }{
		{"tenant:read", "Read the key's own tenant"},
		{"api-keys:read", "List the tenant's API key metadata"},
	}
	for _, scope := range scopes {
		err = b.Scopes().Register(scope.name, scope.description)
		if err != nil {
			return fmt.Errorf("register scope: %w", err)
		}
	}
	return nil
}

// provideKratos builds the admin client without any I/O. Its transport
// carries the W3C traceparent of the request or job context.
func (s *service) provideKratos(b *core.Builder) {
	var opts []kratos.Option
	telemetry := b.Telemetry()
	if telemetry != nil {
		opts = append(opts, kratos.WithTransport(telemetry.Transport(nil)))
	}
	if s.cfg.KratosAdminURL != nil {
		s.admin = kratos.NewAdmin(s.cfg.KratosAdminURL, opts...)
	}
	if s.cfg.KratosPublicURL != nil {
		s.public = kratos.NewPublic(s.cfg.KratosPublicURL, kratosCookieName, opts...)
	}
}

// mount registers the session routes and the /kratos/ proxy.
func (s *service) mount(_ context.Context, h *core.Host) error {
	s.mountSession(h)
	s.mountTenancy(h)
	s.mountAPIKeys(h)
	return nil
}
