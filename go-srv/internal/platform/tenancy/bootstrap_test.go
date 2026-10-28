package tenancy

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/accounts"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/config"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/coretest"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

const (
	bootstrapSlug  = "acme"
	bootstrapEmail = "admin@example.com"
)

// bootstrapEnv is one test's world: a database with the tenancy tables only,
// an account book standing in for identity and a log sink.
type bootstrapEnv struct {
	t     *testing.T
	pool  *pgxpool.Pool
	book  *coretest.AccountBook
	logs  *logSink
	admin accounts.Ref
}

func newBootstrapEnv(t *testing.T) *bootstrapEnv {
	t.Helper()
	name := "Admin"
	admin := accounts.Ref{
		ID:               reqctx.AccountID(randomUUID(t)),
		Email:            bootstrapEmail,
		FullName:         &name,
		Status:           accounts.StatusActive,
		PlatformOperator: true,
	}
	return &bootstrapEnv{
		t:     t,
		pool:  newTestPool(t),
		book:  coretest.NewAccountBook(nil, admin),
		logs:  &logSink{},
		admin: admin,
	}
}

func (e *bootstrapEnv) settings(extra map[string]string) map[string]string {
	settings := map[string]string{
		"APP_PUBLIC_BASE_URL":       publicBase,
		"APP_BOOTSTRAP_TENANT_SLUG": bootstrapSlug,
		"APP_BOOTSTRAP_ADMIN_EMAIL": bootstrapEmail,
	}
	for key, value := range extra {
		if value == "" {
			delete(settings, key)
			continue
		}
		settings[key] = value
	}
	return settings
}

// build runs core.Build with the tenancy module; a started app is closed on cleanup.
func (e *bootstrapEnv) build(extra map[string]string) (*core.App, error) {
	e.t.Helper()
	app, err := core.Build(context.Background(), core.Options{
		Logger:             slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Config:             config.MapSource(e.settings(extra)),
		Clock:              clock.System{},
		Pool:               e.pool,
		AccountDirectory:   e.book,
		AccountProvisioner: e.book,
	}, []core.Module{Module()})
	if err == nil {
		e.t.Cleanup(func() { _ = app.Close(context.Background()) })
	}
	return app, err
}

func (e *bootstrapEnv) mustBuild(extra map[string]string) {
	e.t.Helper()
	_, err := e.build(extra)
	if err != nil {
		e.t.Fatalf("Build: %v", err)
	}
}

func (e *bootstrapEnv) count(table string) int {
	e.t.Helper()
	var total int
	err := e.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&total)
	if err != nil {
		e.t.Fatalf("count %s: %v", table, err)
	}
	return total
}

// events returns every log record whose msg is name.
func (e *bootstrapEnv) events(name string) []map[string]any {
	e.t.Helper()
	var found []map[string]any
	for _, line := range strings.Split(e.logs.String(), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		err := json.Unmarshal([]byte(line), &record)
		if err != nil {
			e.t.Fatalf("log line is not JSON: %q", line)
		}
		if record["msg"] == name {
			found = append(found, record)
		}
	}
	return found
}

// advisoryLockHolders counts the granted tenant bootstrap locks in this
// database, read from pg_locks so a re-entrant lock on one session cannot hide a leak.
func (e *bootstrapEnv) advisoryLockHolders() int {
	e.t.Helper()
	var holders int
	err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM pg_locks
 WHERE locktype = 'advisory' AND granted
 AND database = (SELECT oid FROM pg_database WHERE datname = current_database())
 AND ((classid::bigint << 32) | objid::bigint) = $1`, tenantBootstrapLockKey).Scan(&holders)
	if err != nil {
		e.t.Fatalf("read pg_locks: %v", err)
	}
	return holders
}

func TestBootstrapCreatesTheTenantAndTheAdminMembership(t *testing.T) {
	env := newBootstrapEnv(t)
	env.mustBuild(nil)

	if env.count("ten_tenants") != 1 || env.count("ten_memberships") != 1 {
		t.Fatalf("rows: %d tenants, %d memberships; want 1 each", env.count("ten_tenants"), env.count("ten_memberships"))
	}
	var tenantID, name, status string
	err := env.pool.QueryRow(context.Background(), "SELECT id::text, name, status FROM ten_tenants WHERE slug = $1", bootstrapSlug).
		Scan(&tenantID, &name, &status)
	if err != nil || name != bootstrapSlug || status != "active" {
		t.Fatalf("tenant id=%q name=%q status=%q err=%v, want an active tenant named after the slug", tenantID, name, status, err)
	}
	var role, membershipStatus string
	var hasSelectedAt bool
	err = env.pool.QueryRow(context.Background(),
		"SELECT role, status, last_selected_at IS NOT NULL FROM ten_memberships WHERE tenant_id = $1::uuid AND account_id = $2::uuid",
		tenantID, string(env.admin.ID)).Scan(&role, &membershipStatus, &hasSelectedAt)
	if err != nil || role != "admin" || membershipStatus != "active" || !hasSelectedAt {
		t.Fatalf("membership role=%q status=%q selected=%v err=%v, want an active admin with last_selected_at", role, membershipStatus, hasSelectedAt, err)
	}
	grant, err := newTenantAccess(env.pool, clock.System{}).Select(context.Background(), env.admin.ID)
	if err != nil || grant == nil || string(grant.Tenant.ID) != tenantID {
		t.Fatalf("first session select = %+v err=%v, want the bootstrap tenant %s", grant, err, tenantID)
	}
}

func TestBootstrapUsesTheTenantNameWhenSet(t *testing.T) {
	env := newBootstrapEnv(t)
	env.mustBuild(map[string]string{"APP_BOOTSTRAP_TENANT_NAME": "Acme Corp"})

	var name string
	err := env.pool.QueryRow(context.Background(), "SELECT name FROM ten_tenants WHERE slug = $1", bootstrapSlug).Scan(&name)
	if err != nil || name != "Acme Corp" {
		t.Fatalf("name = %q err=%v", name, err)
	}
}

func TestBootstrapTakesTheOldestAccountOfTheEmail(t *testing.T) {
	env := newBootstrapEnv(t)
	newer := env.admin
	newer.ID = reqctx.AccountID(randomUUID(t))
	env.book = coretest.NewAccountBook(nil, env.admin, newer)

	env.mustBuild(nil)

	var accountID string
	err := env.pool.QueryRow(context.Background(), "SELECT account_id::text FROM ten_memberships").Scan(&accountID)
	if err != nil || accountID != string(env.admin.ID) {
		t.Fatalf("membership account = %q err=%v, want the oldest account %s", accountID, err, env.admin.ID)
	}
}

func TestBootstrapLogsTheCreation(t *testing.T) {
	env := newBootstrapEnv(t)
	env.mustBuild(nil)

	created := env.events("tenant_bootstrap_created")
	if len(created) != 1 || created[0]["account_id"] != string(env.admin.ID) || created[0]["tenant_id"] == "" {
		t.Fatalf("tenant_bootstrap_created = %v", created)
	}
}

func TestAnExistingTenantSkipsTheBootstrap(t *testing.T) {
	env := newBootstrapEnv(t)
	_, err := env.pool.Exec(context.Background(), "INSERT INTO ten_tenants (slug, name) VALUES ('existing', 'Existing')")
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	env.mustBuild(nil)

	if env.count("ten_tenants") != 1 || env.count("ten_memberships") != 0 {
		t.Error("the bootstrap ran although a tenant exists")
	}
	skipped := env.events("tenant_bootstrap_skipped")
	if len(skipped) != 1 || skipped[0]["reason"] != "tenants_exist" {
		t.Errorf("tenant_bootstrap_skipped = %v, want reason tenants_exist", skipped)
	}
}

func TestSecondSequentialRunSkipsAndChangesNothing(t *testing.T) {
	env := newBootstrapEnv(t)
	env.mustBuild(nil)
	env.mustBuild(map[string]string{"APP_BOOTSTRAP_TENANT_SLUG": "other-corp"})

	if env.count("ten_tenants") != 1 || env.count("ten_memberships") != 1 {
		t.Fatal("a second run changed the rows")
	}
	if len(env.events("tenant_bootstrap_skipped")) != 1 {
		t.Errorf("want one skip in the logs, got %v", env.events("tenant_bootstrap_skipped"))
	}
}

func TestTwoConcurrentRunsLeaveOneTenantOneMembership(t *testing.T) {
	env := newBootstrapEnv(t)
	const runs = 2
	errs := make([]error, runs)
	var wg sync.WaitGroup
	for i := 0; i < runs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = env.build(nil)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}

	if env.count("ten_tenants") != 1 || env.count("ten_memberships") != 1 {
		t.Fatalf("rows: %d tenants, %d memberships; want 1 each", env.count("ten_tenants"), env.count("ten_memberships"))
	}
	if len(env.events("tenant_bootstrap_created")) != 1 || len(env.events("tenant_bootstrap_skipped")) != 1 {
		t.Error("want one creation and one skip in the logs")
	}
	if env.advisoryLockHolders() != 0 {
		t.Error("the bootstrap lock is still held after the runs")
	}
}

func TestAnUnknownAdminEmailStopsStartAndReleasesTheLock(t *testing.T) {
	env := newBootstrapEnv(t)

	_, err := env.build(map[string]string{"APP_BOOTSTRAP_ADMIN_EMAIL": "nobody@example.com"})

	if err == nil || !strings.Contains(err.Error(), "tenancy: bootstrap: no account has APP_BOOTSTRAP_ADMIN_EMAIL") {
		t.Fatalf("err = %v, want the stated bootstrap failure", err)
	}
	if env.count("ten_tenants") != 0 || env.count("ten_memberships") != 0 {
		t.Error("a failed bootstrap left rows behind")
	}
	if env.advisoryLockHolders() != 0 {
		t.Error("the bootstrap lock is still held after a failed run")
	}
}

func TestADirectoryFailureStopsStart(t *testing.T) {
	env := newBootstrapEnv(t)
	app, err := core.Build(context.Background(), core.Options{
		Logger:             slog.New(slog.NewJSONHandler(env.logs, nil)),
		Config:             config.MapSource(env.settings(nil)),
		Clock:              clock.System{},
		Pool:               env.pool,
		AccountDirectory:   brokenDirectory{},
		AccountProvisioner: env.book,
	}, []core.Module{Module()})
	if err == nil {
		_ = app.Close(context.Background())
		t.Fatal("Build succeeded although the account directory failed")
	}
	if env.count("ten_tenants") != 0 {
		t.Error("a failed bootstrap left rows behind")
	}
}

// brokenDirectory is an accounts.Directory whose every call fails.
type brokenDirectory struct{}

func (brokenDirectory) Get(context.Context, []reqctx.AccountID) (map[reqctx.AccountID]accounts.Ref, error) {
	return nil, context.DeadlineExceeded
}

func (brokenDirectory) ByEmail(context.Context, string) ([]accounts.Ref, error) {
	return nil, context.DeadlineExceeded
}

func TestBootstrapDisabledCreatesNothing(t *testing.T) {
	env := newBootstrapEnv(t)
	env.mustBuild(map[string]string{"APP_BOOTSTRAP_TENANT_SLUG": ""})

	if env.count("ten_tenants") != 0 || env.count("ten_memberships") != 0 {
		t.Error("a disabled bootstrap created rows")
	}
}

func TestBootstrapConfigProblemsAreListedInOneError(t *testing.T) {
	env := newBootstrapEnv(t)

	_, err := env.build(map[string]string{
		"APP_BOOTSTRAP_TENANT_SLUG": "Not A Slug",
		"APP_BOOTSTRAP_ADMIN_EMAIL": "",
		"APP_BOOTSTRAP_TENANT_NAME": strings.Repeat("n", 101),
	})

	if err == nil {
		t.Fatal("Build accepted a bad bootstrap configuration")
	}
	for _, key := range []string{"APP_BOOTSTRAP_TENANT_SLUG", "APP_BOOTSTRAP_ADMIN_EMAIL", "APP_BOOTSTRAP_TENANT_NAME"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not name %s: %v", key, err)
		}
	}
	if env.count("ten_tenants") != 0 {
		t.Error("a rejected configuration ran the bootstrap")
	}
}

func TestBootstrapReportsNoEmailFormatProblem(t *testing.T) {
	env := newBootstrapEnv(t)

	_, err := env.build(map[string]string{"APP_BOOTSTRAP_ADMIN_EMAIL": "not-an-address"})

	if err == nil || strings.Contains(err.Error(), "invalid configuration") {
		t.Fatalf("err = %v, want the bootstrap to run and fail on the unknown account, not on the email format", err)
	}
}
