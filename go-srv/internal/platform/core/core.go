// Package core is the always-installed foundation: it assembles the catalog
// modules of a project into one HTTP server.
package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/config"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/db"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/featureflag"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/health"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/migrate"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/telemetry"
)

const (
	defaultAddr       = ":10100"
	defaultPublicURL  = "http://localhost:10120"
	defaultService    = "go-srv"
	readHeaderTimeout = 5 * time.Second
	closeTimeout      = 10 * time.Second
	componentCore     = "core"
)

var moduleIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,30}$`)

// Module is one catalog module as the core sees it. A module's root package
// exports `func Module() core.Module`; there is no package-level registry.
// Every func field is optional.
type Module struct {
	// ID equals the "module" field of module.manifest.json; ^[a-z][a-z0-9-]{1,30}$, unique.
	ID string
	// Migrations is an embedded FS whose "migrations" dir holds goose SQL files; nil when none.
	Migrations fs.FS
	// Provide, phase 1, list order: read config, build services, register scopes,
	// publish ports. It does no I/O and must not read another module's port.
	Provide func(ctx context.Context, b *Builder) error
	// Mount, phase 2, after every Provide and after migrations: routes and readiness checks.
	Mount func(ctx context.Context, h *Host) error
	// Start, phase 3, after every Mount, before the listener opens. An error stops the process.
	Start func(ctx context.Context, h *Host) error
}

// Options configure Build. Production passes only Logger.
type Options struct {
	// Logger is required.
	Logger *slog.Logger
	// Config is nil for config.EnvSource().
	Config config.Source
	// Clock is nil for clock.System{}.
	Clock clock.Clock
	// Pool is nil for a pool core opens from APP_DATABASE_URL and closes in Close.
	Pool *pgxpool.Pool
	// Ports pre-provided before phase 1 (test fakes); a module providing the
	// same port fails startup.
	SessionResolver access.SessionResolver
	APIKeyResolver  access.APIKeyResolver
	TenantDirectory access.TenantDirectory
	FlagEvaluator   featureflag.Evaluator
}

// App is an assembled server that is not listening yet.
type App struct {
	handler         http.Handler
	addr            string
	readiness       *health.Readiness
	shutdownTimeout time.Duration

	telemetry *telemetry.Telemetry
	// ownedPool is the pool Build opened; nil when Options.Pool was given.
	ownedPool *pgxpool.Pool
	closeOnce sync.Once
	closeErr  error
}

// Handler is the full request chain.
func (a *App) Handler() http.Handler { return a.handler }

// Addr is GO_SRV_ADDR.
func (a *App) Addr() string { return a.addr }

// Readiness is the shared readiness registry. The chain answers /api/v1/ with
// 503 until SetServing(true).
func (a *App) Readiness() *health.Readiness { return a.readiness }

// ShutdownTimeout is APP_SHUTDOWN_TIMEOUT.
func (a *App) ShutdownTimeout() time.Duration { return a.shutdownTimeout }

// Close closes the pool when Build opened it and shuts telemetry down. It is
// safe to call more than once; later calls return the first result.
func (a *App) Close(ctx context.Context) error {
	a.closeOnce.Do(func() {
		if a.ownedPool != nil {
			a.ownedPool.Close()
		}
		if a.telemetry != nil {
			a.closeErr = a.telemetry.Shutdown(ctx)
		}
	})
	return a.closeErr
}

// coreConfig holds the keys core reads itself.
type coreConfig struct {
	addr            string
	serviceName     string
	publicURL       *url.URL
	maxConns        int
	shutdownTimeout time.Duration
	databaseURL     config.Secret
}

// readCoreConfig records every problem in r.
func readCoreConfig(r *config.Reader, needsDatabaseURL bool) coreConfig {
	cc := coreConfig{
		addr:            r.String("GO_SRV_ADDR", defaultAddr),
		serviceName:     r.String("OTEL_SERVICE_NAME", defaultService),
		maxConns:        r.Int("APP_DB_MAX_CONNS", 10, 1, 200),
		shutdownTimeout: r.Duration("APP_SHUTDOWN_TIMEOUT", 10*time.Second, time.Second, 120*time.Second),
	}
	cc.publicURL = readPublicURL(r)
	if needsDatabaseURL {
		cc.databaseURL = r.Secret("APP_DATABASE_URL")
	}
	return cc
}

// readPublicURL returns scheme, host and port of APP_PUBLIC_BASE_URL; a
// path other than "/" is a problem. On any problem it returns the default so
// the result is never nil.
func readPublicURL(r *config.Reader) *url.URL {
	const key = "APP_PUBLIC_BASE_URL"
	parsed := r.URL(key, defaultPublicURL)
	if parsed == nil {
		fallback, _ := url.Parse(defaultPublicURL)
		return fallback
	}
	hasPath := parsed.Path != "" && parsed.Path != "/"
	if hasPath {
		r.Invalid(key, "must not have a path")
	}
	return &url.URL{Scheme: parsed.Scheme, Host: parsed.Host}
}

// validateModules checks every id and reports all problems together.
func validateModules(modules []Module) error {
	var problems []error
	seen := make(map[string]bool, len(modules))
	for i, m := range modules {
		if !moduleIDPattern.MatchString(m.ID) {
			problems = append(problems, fmt.Errorf("core: module %d: id %q does not match %s", i, m.ID, moduleIDPattern))
		}
		if seen[m.ID] {
			problems = append(problems, fmt.Errorf("core: module %d: duplicate id %q", i, m.ID))
		}
		seen[m.ID] = true
	}
	return errors.Join(problems...)
}

// Build validates the module list, reads core config, sets up telemetry and
// the pool, runs every Provide, reports every config problem at once, applies
// migrations, then runs every Mount and every Start. Each failure is logged
// once ("config_invalid" or "server_failed") and returned; on error nothing
// stays open. The chain answers /api/v1/ with 503 until the caller sets
// App.Readiness to serving.
func Build(ctx context.Context, opts Options, modules []Module) (*App, error) {
	if ctx == nil || opts.Logger == nil {
		return nil, errors.New("core: Build requires a context and Options.Logger")
	}
	logger := opts.Logger.With(slog.String("component", componentCore))
	app := &App{}
	succeeded := false
	defer func() {
		if succeeded {
			return
		}
		closeApp(app)
	}()

	err := validateModules(modules)
	if err != nil {
		return nil, failed(logger, err)
	}

	source := opts.Config
	if source == nil {
		source = config.EnvSource()
	}
	reader := config.NewReader(source)
	cc := readCoreConfig(reader, opts.Pool == nil)

	tel, err := telemetry.Setup(ctx, telemetry.Config{ServiceName: cc.serviceName})
	if err != nil {
		return nil, failed(logger, err)
	}
	app.telemetry = tel

	a := newAssembly(opts, logger, reader, cc, tel)
	if opts.Pool == nil {
		app.ownedPool = openPool(ctx, reader, cc)
		a.pool = app.ownedPool
	}
	app.addr = cc.addr
	app.shutdownTimeout = cc.shutdownTimeout
	app.readiness = a.readiness

	err = runProvide(ctx, a, modules)
	if err != nil {
		return nil, failed(logger, err)
	}
	a.scopes.Freeze()
	writer := api.NewWriter(a.clock, logger)
	a.guard = access.NewGuard(a.sessions, a.keys, a.scopes, writer)

	configErr := reader.Err()
	if configErr != nil {
		return nil, configInvalid(logger, configErr)
	}
	err = db.Ping(ctx, a.pool)
	if err != nil {
		return nil, failed(logger, err)
	}
	err = applyMigrations(ctx, a, modules)
	if err != nil {
		return nil, failed(logger, err)
	}

	err = mountAll(ctx, a, modules, writer)
	if err != nil {
		return nil, failed(logger, err)
	}
	handler, err := buildChain(a, writer)
	if err != nil {
		return nil, failed(logger, err)
	}
	app.handler = handler

	err = startAll(ctx, a, modules)
	if err != nil {
		return nil, failed(logger, err)
	}
	a.phase = phaseDone
	succeeded = true
	return app, nil
}

func newAssembly(opts Options, logger *slog.Logger, reader *config.Reader, cc coreConfig, tel *telemetry.Telemetry) *assembly {
	clk := opts.Clock
	if clk == nil {
		clk = clock.System{}
	}
	a := &assembly{
		phase:     phaseProvide,
		logger:    logger,
		cfg:       reader,
		pool:      opts.Pool,
		clock:     clk,
		telemetry: tel,
		baseURL:   cc.publicURL,
		scopes:    access.NewScopeRegistry(),
		readiness: health.NewReadiness(),
		mux:       http.NewServeMux(),
		sessions:  access.NoSessions,
		keys:      access.NoAPIKeys,
		tenants:   access.NoTenants,
		flags:     featureflag.Disabled{},
		provided:  map[string]bool{},
	}
	if opts.SessionResolver != nil {
		a.sessions = opts.SessionResolver
		a.provided["session resolver"] = true
	}
	if opts.APIKeyResolver != nil {
		a.keys = opts.APIKeyResolver
		a.provided["api key resolver"] = true
	}
	if opts.TenantDirectory != nil {
		a.tenants = opts.TenantDirectory
		a.provided["tenant directory"] = true
	}
	if opts.FlagEvaluator != nil {
		a.flags = opts.FlagEvaluator
		a.provided["flag evaluator"] = true
	}
	return a
}

// openPool creates the lazy pool. A missing URL is already a config problem;
// an unparseable one is recorded here. Both yield a nil pool.
func openPool(ctx context.Context, reader *config.Reader, cc coreConfig) *pgxpool.Pool {
	if cc.databaseURL.Reveal() == "" {
		return nil
	}
	pool, err := db.Open(ctx, cc.databaseURL, cc.maxConns)
	if err != nil {
		reader.Invalid("APP_DATABASE_URL", "must be a valid PostgreSQL connection URL")
		return nil
	}
	return pool
}

func runProvide(ctx context.Context, a *assembly, modules []Module) error {
	var problems []error
	for _, m := range modules {
		if m.Provide == nil {
			continue
		}
		b := &Builder{a: a, id: m.ID, logger: a.logger.With(slog.String("component", m.ID))}
		err := m.Provide(ctx, b)
		if err != nil {
			problems = append(problems, fmt.Errorf("core: module %s: provide: %w", m.ID, err))
		}
	}
	problems = append(problems, a.portErrors...)
	return errors.Join(problems...)
}

func applyMigrations(ctx context.Context, a *assembly, modules []Module) error {
	var sets []migrate.Set
	for _, m := range modules {
		if m.Migrations != nil {
			sets = append(sets, migrate.Set{Module: m.ID, Files: m.Migrations})
		}
	}
	if len(sets) == 0 {
		return nil
	}
	return migrate.Apply(ctx, a.pool, a.logger, sets)
}

func newHost(a *assembly, id string) *Host {
	logger := a.logger.With(slog.String("component", id))
	return &Host{a: a, id: id, logger: logger, writer: api.NewWriter(a.clock, logger)}
}

func mountAll(ctx context.Context, a *assembly, modules []Module, writer *api.Writer) error {
	a.phase = phaseMount
	a.readiness.Add(db.PingCheck(a.pool))
	for _, m := range modules {
		if m.Mount == nil {
			continue
		}
		err := m.Mount(ctx, newHost(a, m.ID))
		if err != nil {
			return fmt.Errorf("core: module %s: mount: %w", m.ID, err)
		}
	}
	health.Mount(a.mux, a.readiness)
	notFound := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writer.Fail(w, r, api.NewError(http.StatusNotFound, api.CodeRouteNotFound, "Route not found"))
	})
	a.mux.Handle(apiPrefix, a.guard.Require(access.Public, notFound))
	return nil
}

func startAll(ctx context.Context, a *assembly, modules []Module) error {
	a.phase = phaseStart
	for _, m := range modules {
		if m.Start == nil {
			continue
		}
		err := m.Start(ctx, newHost(a, m.ID))
		if err != nil {
			return fmt.Errorf("core: module %s: start: %w", m.ID, err)
		}
	}
	return nil
}

// failed logs err once as server_failed and returns it.
func failed(logger *slog.Logger, err error) error {
	logger.Error("server_failed", slog.Any("err", err))
	return err
}

// configInvalid logs every problem (key and rule, never a value) in one
// config_invalid event and returns them as one error.
func configInvalid(logger *slog.Logger, problems error) error {
	var list []string
	joined, isJoined := problems.(interface{ Unwrap() []error })
	if isJoined {
		for _, problem := range joined.Unwrap() {
			list = append(list, problem.Error())
		}
	} else {
		list = append(list, problems.Error())
	}
	logger.Error("config_invalid", slog.Any("problems", list))
	return fmt.Errorf("core: invalid configuration:\n%w", problems)
}

// Run builds the app, listens on Addr, marks it serving and logs
// server_ready. When ctx is done it marks the app not serving, drains for
// ShutdownTimeout and calls Close. A Build failure is logged by Build; any
// other failure is logged here as server_failed.
func Run(ctx context.Context, logger *slog.Logger, modules []Module) error {
	if logger == nil {
		return errors.New("core: Run requires a logger")
	}
	app, err := Build(ctx, Options{Logger: logger}, modules)
	if err != nil {
		return err
	}
	logger.Info("server_starting", slog.String("addr", app.Addr()))
	listener, err := net.Listen("tcp", app.Addr())
	if err != nil {
		closeApp(app)
		return failed(logger, fmt.Errorf("core: listen: %w", err))
	}
	server := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: readHeaderTimeout}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	app.Readiness().SetServing(true)
	logger.Info("server_ready", slog.String("addr", app.Addr()))

	select {
	case err := <-serveErr:
		closeApp(app)
		return failed(logger, fmt.Errorf("core: serve: %w", err))
	case <-ctx.Done():
	}

	logger.Info("server_stopping")
	app.Readiness().SetServing(false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), app.ShutdownTimeout())
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		_ = server.Close()
	}
	<-serveErr
	closeCtx, closeCancel := context.WithTimeout(context.Background(), closeTimeout)
	defer closeCancel()
	closeErr := app.Close(closeCtx)
	stopErr := errors.Join(shutdownErr, closeErr)
	if stopErr != nil {
		return failed(logger, fmt.Errorf("core: shutdown: %w", stopErr))
	}
	logger.Info("server_stopped")
	return nil
}

func closeApp(app *App) {
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	_ = app.Close(ctx)
}

// Main is the whole body of cmd/server/main.go: a JSON slog logger on stdout
// wrapped by telemetry.NewLogHandler, a SIGINT/SIGTERM context and Run. It
// returns the process exit code: 0 after a clean shutdown, 1 on any startup
// or serve error (already logged once by Build or Run).
func Main(modules []Module) int {
	service := config.NewReader(config.EnvSource()).String("OTEL_SERVICE_NAME", defaultService)
	handler := telemetry.NewLogHandler(slog.NewJSONHandler(os.Stdout, nil), service)
	logger := slog.New(handler)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := Run(ctx, logger, modules)
	if err != nil {
		return 1
	}
	return 0
}
