package core

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/auth"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/config"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/featureflag"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/health"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/telemetry"
)

const (
	apiPrefix    = "/api/v1/"
	apiRoot      = "/api/v1"
	healthPrefix = "/health/"
)

type phase int

const (
	phaseProvide phase = iota + 1
	phaseMount
	phaseStart
	phaseDone
)

func (p phase) String() string {
	switch p {
	case phaseProvide:
		return "Provide"
	case phaseMount:
		return "Mount"
	case phaseStart:
		return "Start"
	}
	return "after startup"
}

// assembly is the state every Builder and Host of one Build shares.
type assembly struct {
	phase     phase
	logger    *slog.Logger
	cfg       *config.Reader
	pool      *pgxpool.Pool
	clock     clock.Clock
	telemetry *telemetry.Telemetry
	baseURL   *url.URL
	scopes    *auth.ScopeRegistry
	readiness *health.Readiness
	mux       *http.ServeMux
	guard     *auth.Guard

	sessions auth.SessionResolver
	keys     auth.APIKeyResolver
	tenants  auth.TenantDirectory
	flags    featureflag.Evaluator

	// provided names the ports already provided (by Options or by a module).
	provided map[string]bool
	// portErrors are Provide* misuse errors; Build fails on them even if a module drops the returned error.
	portErrors []error
}

// Builder is what a module may use in Provide.
type Builder struct {
	a      *assembly
	id     string
	logger *slog.Logger
}

// Config is the shared config reader; problems accumulate and are reported
// together once every Provide has run.
func (b *Builder) Config() *config.Reader { return b.a.cfg }

// Logger is bound with component=<module id>.
func (b *Builder) Logger() *slog.Logger { return b.logger }

// Pool is the shared pool; it is nil when core's configuration is invalid. It
// connects lazily: Provide does no I/O.
func (b *Builder) Pool() *pgxpool.Pool { return b.a.pool }

// Clock is the shared clock.
func (b *Builder) Clock() clock.Clock { return b.a.clock }

// Telemetry is the shared tracer provider owner.
func (b *Builder) Telemetry() *telemetry.Telemetry { return b.a.telemetry }

// PublicBaseURL is GATBASE_PUBLIC_BASE_URL (scheme, host, port; no path).
func (b *Builder) PublicBaseURL() *url.URL { return copyURL(b.a.baseURL) }

// Scopes is the API-key scope registry; it is frozen after phase 1.
func (b *Builder) Scopes() *auth.ScopeRegistry { return b.a.scopes }

// ProvideSessionResolver publishes the session port. Each port can be
// provided once, and only in phase 1.
func (b *Builder) ProvideSessionResolver(r auth.SessionResolver) error {
	return b.provide("session resolver", r == nil, func() { b.a.sessions = r })
}

// ProvideAPIKeyResolver publishes the API key port; see ProvideSessionResolver.
func (b *Builder) ProvideAPIKeyResolver(r auth.APIKeyResolver) error {
	return b.provide("api key resolver", r == nil, func() { b.a.keys = r })
}

// ProvideTenantDirectory publishes the tenant directory port; see ProvideSessionResolver.
func (b *Builder) ProvideTenantDirectory(d auth.TenantDirectory) error {
	return b.provide("tenant directory", d == nil, func() { b.a.tenants = d })
}

// ProvideFlagEvaluator publishes the flag evaluator port; see ProvideSessionResolver.
func (b *Builder) ProvideFlagEvaluator(e featureflag.Evaluator) error {
	return b.provide("flag evaluator", e == nil, func() { b.a.flags = e })
}

func (b *Builder) provide(port string, isNil bool, set func()) error {
	var err error
	switch {
	case b.a.phase != phaseProvide:
		err = fmt.Errorf("core: module %s: %s provided in %s; ports are provided in Provide only", b.id, port, b.a.phase)
	case isNil:
		err = fmt.Errorf("core: module %s: %s is nil", b.id, port)
	case b.a.provided[port]:
		err = fmt.Errorf("core: module %s: %s is already provided", b.id, port)
	default:
		b.a.provided[port] = true
		set()
		return nil
	}
	b.a.portErrors = append(b.a.portErrors, err)
	return err
}

// Host is what a module may use in Mount and Start.
type Host struct {
	a      *assembly
	id     string
	logger *slog.Logger
	writer *api.Writer
}

// Handle registers handler for pattern behind policy p; it is the only way to
// mount a route. It panics on an invalid policy, a nil handler, a /api/v1/
// pattern without a method, a pattern under /health/, a call outside Mount,
// and a duplicate pattern.
func (h *Host) Handle(pattern string, p auth.Policy, handler http.Handler) {
	if h.a.phase != phaseMount {
		panic(fmt.Sprintf("core: module %s: Handle(%q) called in %s; routes are mounted in Mount only", h.id, pattern, h.a.phase))
	}
	err := checkPattern(pattern)
	if err != nil {
		panic(fmt.Sprintf("core: module %s: %v", h.id, err))
	}
	h.a.mux.Handle(pattern, h.a.guard.Require(p, handler))
}

// API is the envelope writer bound to the host clock and the module logger.
func (h *Host) API() *api.Writer { return h.writer }

// Readiness accepts extra checks for /health/ready.
func (h *Host) Readiness() *health.Readiness { return h.a.readiness }

// Logger is bound with component=<module id>.
func (h *Host) Logger() *slog.Logger { return h.logger }

// Pool is the shared pool.
func (h *Host) Pool() *pgxpool.Pool { return h.a.pool }

// Clock is the shared clock.
func (h *Host) Clock() clock.Clock { return h.a.clock }

// Telemetry is the shared tracer provider owner.
func (h *Host) Telemetry() *telemetry.Telemetry { return h.a.telemetry }

// PublicBaseURL is GATBASE_PUBLIC_BASE_URL (scheme, host, port; no path).
func (h *Host) PublicBaseURL() *url.URL { return copyURL(h.a.baseURL) }

// Scopes is the frozen scope registry; read only.
func (h *Host) Scopes() *auth.ScopeRegistry { return h.a.scopes }

// SessionResolver is the provided port, else auth.NoSessions.
func (h *Host) SessionResolver() auth.SessionResolver { return h.a.sessions }

// APIKeyResolver is the provided port, else auth.NoAPIKeys.
func (h *Host) APIKeyResolver() auth.APIKeyResolver { return h.a.keys }

// TenantDirectory is the provided port, else auth.NoTenants.
func (h *Host) TenantDirectory() auth.TenantDirectory { return h.a.tenants }

// FlagEvaluator is the provided port, else featureflag.Disabled{}.
func (h *Host) FlagEvaluator() featureflag.Evaluator { return h.a.flags }

func copyURL(u *url.URL) *url.URL {
	clone := *u
	return &clone
}

// checkPattern applies the mounting rules that depend on the pattern text.
// Patterns are "[METHOD ][HOST]/[PATH]" (net/http.ServeMux).
func checkPattern(pattern string) error {
	hasMethod := false
	rest := pattern
	if i := strings.IndexAny(pattern, " \t"); i >= 0 {
		hasMethod = true
		rest = strings.TrimLeft(pattern[i:], " \t")
	}
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return fmt.Errorf("pattern %q has no path", pattern)
	}
	path := rest[slash:]
	if strings.HasPrefix(path, healthPrefix) {
		return fmt.Errorf("pattern %q is under %s, which core owns", pattern, healthPrefix)
	}
	isAPI := path == apiRoot || strings.HasPrefix(path, apiPrefix)
	if isAPI && !hasMethod {
		return fmt.Errorf("pattern %q is under %s and needs a method", pattern, apiPrefix)
	}
	return nil
}

func isAPIPath(path string) bool {
	return path == apiRoot || strings.HasPrefix(path, apiPrefix)
}
