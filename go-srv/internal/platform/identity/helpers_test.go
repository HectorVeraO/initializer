package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/migrate"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/testkit/testcore"
)

const adminURLEnv = "APP_TEST_DATABASE_URL"

// dropAttempts and dropRetryDelay bound the retries while PostgreSQL answers
// 55006 "database is being accessed by other users". PostgreSQL waits about five
// seconds inside one attempt before it answers, so two attempts cover a backend
// that is still draining.
const (
	dropAttempts   = 2
	dropRetryDelay = 100 * time.Millisecond
)

// sqlstateObjectInUse is PostgreSQL's "database is being accessed by other users".
const sqlstateObjectInUse = "55006"

// sqlstateSignalDenied is PostgreSQL's "permission denied to terminate process",
// the answer a role without pg_signal_backend gets from a FORCE drop.
const sqlstateSignalDenied = "42501"

// randomSessionKeys is a keyring entry built from fresh random bytes.
func randomSessionKeys(t testing.TB) string {
	t.Helper()
	seed := make([]byte, 32)
	_, err := rand.Read(seed)
	if err != nil {
		t.Fatalf("random: %v", err)
	}
	return "test-1:" + base64.RawURLEncoding.EncodeToString(seed)
}

// randomUUID returns a random version 4 UUID in canonical text.
func randomUUID(t testing.TB) string {
	t.Helper()
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		t.Fatalf("random: %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// lazyPool is a pool that never connects unless used; Build checks the
// configuration before it pings.
func lazyPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://nobody@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatalf("lazy pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// newTestPool creates an it_ database on the server of APP_TEST_DATABASE_URL,
// applies the module migrations and drops the database on cleanup. It skips
// the test when the variable is unset.
func newTestPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	adminURL := os.Getenv(adminURLEnv)
	if adminURL == "" {
		t.Skipf("%s is not set; skipping database test", adminURLEnv)
	}
	suffix := make([]byte, 4)
	_, err := rand.Read(suffix)
	if err != nil {
		t.Fatalf("random: %v", err)
	}
	name := "it_identity_" + hex.EncodeToString(suffix)
	quoted := pgx.Identifier{name}.Sanitize()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("admin database unreachable: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()
	_, err = admin.Exec(ctx, "CREATE DATABASE "+quoted)
	if err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}
	t.Cleanup(func() { dropDatabase(t, adminURL, quoted) })

	cfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	cfg.ConnConfig.Database = name
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	err = applyMigrations(ctx, pool)
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return pool
}

func applyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sets := []migrate.Set{{Module: moduleID, Files: migrationsFS}}
	return migrate.Apply(ctx, pool, logger, sets)
}

// dropDatabase terminates this role's backends on the database and drops it,
// reporting a failure that names the roles still attached.
func dropDatabase(t testing.TB, adminURL, quoted string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Errorf("connect to drop %s: %v", quoted, err)
		return
	}
	defer func() { _ = admin.Close(ctx) }()
	err = deleteDatabase(ctx, admin, quoted, databaseName(quoted))
	if err != nil {
		t.Errorf("drop %s: %v", quoted, err)
	}
}

// deleteDatabase terminates the backends of the connected role on the database
// and drops it, returning the failure instead of reporting it.
//
// The first attempts carry no FORCE. FORCE terminates every backend attached to
// the database, and a role may terminate only the backends of its own role
// unless it is a superuser or a member of pg_signal_backend, so on a server
// several components share FORCE answers 42501 "permission denied to terminate
// process" and the cleanup fails after the test passed. A plain drop first, then,
// for a role that may signal every backend, one FORCE attempt.
func deleteDatabase(ctx context.Context, admin *pgx.Conn, quoted, name string) error {
	terminateErr := terminateOwnBackends(ctx, admin, name)
	err := dropWithRetry(ctx, admin, quoted, name)
	if err == nil {
		return nil
	}
	if terminateErr != nil {
		return fmt.Errorf("%w (terminating this role's backends failed: %v)", err, terminateErr)
	}
	return err
}

// terminateOwnBackends ends every backend of the connected role on the database,
// so a pool that is still draining stops holding it. Terminating a backend of
// the same role needs no privilege. A backend of another role is left attached:
// the drop reports it instead.
func terminateOwnBackends(ctx context.Context, admin *pgx.Conn, name string) error {
	_, err := admin.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity
WHERE datname = $1 AND usename = current_user AND pid <> pg_backend_pid()`, name)
	return err
}

// dropWithRetry drops the database: plain attempts while PostgreSQL reports it
// still in use, then one FORCE attempt for a role allowed to signal every
// backend; the failure names the roles attached to it.
func dropWithRetry(ctx context.Context, admin *pgx.Conn, quoted, name string) error {
	for attempt := 1; attempt <= dropAttempts; attempt++ {
		_, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+quoted)
		if err == nil {
			return nil
		}
		if !isObjectInUse(err) {
			return err
		}
		if attempt < dropAttempts {
			time.Sleep(dropRetryDelay)
		}
	}
	_, forceErr := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)")
	if forceErr == nil {
		return nil
	}
	if !isObjectInUse(forceErr) && !isSignalDenied(forceErr) {
		return forceErr
	}
	attached, err := attachedBackends(ctx, admin, name)
	if err != nil {
		return fmt.Errorf("still in use, and listing the attached backends failed: %v: %w", err, forceErr)
	}
	return fmt.Errorf("still in use by %s: %w", attached, forceErr)
}

// isObjectInUse reports whether err is PostgreSQL's "database is being accessed
// by other users".
func isObjectInUse(err error) bool {
	return hasSQLState(err, sqlstateObjectInUse)
}

// isSignalDenied reports whether err is PostgreSQL's "permission denied to
// terminate process", the answer to a FORCE drop by a role that may not signal
// another role's backend.
func isSignalDenied(err error) bool {
	return hasSQLState(err, sqlstateSignalDenied)
}

// hasSQLState reports whether err carries a PostgreSQL SQLSTATE code.
func hasSQLState(err error, code string) bool {
	var pgErr *pgconn.PgError
	hasCode := errors.As(err, &pgErr) && pgErr.Code == code
	return hasCode
}

// attachedBackends names the roles and applications still attached to the
// database, so a failed drop says who held it.
func attachedBackends(ctx context.Context, admin *pgx.Conn, name string) (string, error) {
	rows, err := admin.Query(ctx, `SELECT COALESCE(usename, 'unknown'), COALESCE(application_name, ''), count(*)
FROM pg_stat_activity WHERE datname = $1 GROUP BY 1, 2 ORDER BY 1, 2`, name)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var attached []string
	for rows.Next() {
		var role, application string
		var backends int
		if scanErr := rows.Scan(&role, &application, &backends); scanErr != nil {
			return "", scanErr
		}
		entry := "role " + role
		if application != "" {
			entry += " application " + application
		}
		attached = append(attached, fmt.Sprintf("%s (%d backends)", entry, backends))
	}
	err = rows.Err()
	if err != nil {
		return "", err
	}
	if len(attached) == 0 {
		return "no attached backend", nil
	}
	return strings.Join(attached, ", "), nil
}

// databaseName returns the database name a sanitized identifier holds.
func databaseName(quoted string) string {
	isQuoted := len(quoted) >= 2 && quoted[0] == '"' && quoted[len(quoted)-1] == '"'
	if !isQuoted {
		return quoted
	}
	return strings.ReplaceAll(quoted[1:len(quoted)-1], `""`, `"`)
}

// tenantsFake is the TenantAccess and TenantDirectory a test hands to
// core.Build: a testcore.TenantBook rebuilt whenever the test changes tenants,
// memberships or statuses after the host exists. It follows the port
// contracts through the book; identity tests never import the tenancy module.
type tenantsFake struct {
	mu         sync.Mutex
	tenants    []coreaccess.TenantRef
	members    []testcore.Member
	remembered map[reqctx.AccountID]reqctx.TenantID
	// rememberErr, when set, is what every Remember call returns.
	rememberErr error
	book        *testcore.TenantBook
}

var (
	_ coreaccess.TenantAccess    = (*tenantsFake)(nil)
	_ coreaccess.TenantDirectory = (*tenantsFake)(nil)
)

func newTenantsFake() *tenantsFake {
	f := &tenantsFake{remembered: map[reqctx.AccountID]reqctx.TenantID{}}
	f.rebuildLocked()
	return f
}

func (f *tenantsFake) rebuildLocked() {
	book := testcore.NewTenantBook(f.tenants...)
	for _, member := range f.members {
		book.AddMember(member)
	}
	for account, tenant := range f.remembered {
		err := book.Remember(context.Background(), account, tenant)
		if err != nil {
			panic("tenantsFake: remember: " + err.Error())
		}
	}
	f.book = book
}

func (f *tenantsFake) addTenant(id, slug, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tenants = append(f.tenants, coreaccess.TenantRef{
		ID: reqctx.TenantID(id), Slug: slug, Name: slug, Status: coreaccess.TenantStatus(status),
	})
	f.rebuildLocked()
}

func (f *tenantsFake) setTenantStatus(id, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.tenants {
		if string(f.tenants[i].ID) == id {
			f.tenants[i].Status = coreaccess.TenantStatus(status)
		}
	}
	f.rebuildLocked()
}

func (f *tenantsFake) addMember(id, tenant, account, role, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.members = append(f.members, testcore.Member{
		ID: id, Account: reqctx.AccountID(account), Tenant: reqctx.TenantID(tenant),
		Role: reqctx.Role(role), Status: status,
	})
	f.rebuildLocked()
}

func (f *tenantsFake) updateMember(tenant, account string, change func(m *testcore.Member)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.members {
		isTarget := string(f.members[i].Tenant) == tenant && string(f.members[i].Account) == account
		if isTarget {
			change(&f.members[i])
		}
	}
	f.rebuildLocked()
}

func (f *tenantsFake) setMemberStatus(tenant, account, status string) {
	f.updateMember(tenant, account, func(m *testcore.Member) { m.Status = status })
}

func (f *tenantsFake) setMemberRole(tenant, account, role string) {
	f.updateMember(tenant, account, func(m *testcore.Member) { m.Role = reqctx.Role(role) })
}

func (f *tenantsFake) removeMember(tenant, account string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.members[:0:0]
	for _, member := range f.members {
		isTarget := string(member.Tenant) == tenant && string(member.Account) == account
		if !isTarget {
			kept = append(kept, member)
		}
	}
	f.members = kept
	f.rebuildLocked()
}

// rememberedTenant returns the tenant Remember last recorded for account.
func (f *tenantsFake) rememberedTenant(account string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tenant, ok := f.remembered[reqctx.AccountID(account)]
	return string(tenant), ok
}

// failRemember makes every later Remember call return err.
func (f *tenantsFake) failRemember(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rememberErr = err
}

func (f *tenantsFake) current() *testcore.TenantBook {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.book
}

func (f *tenantsFake) TenantExists(ctx context.Context, id reqctx.TenantID) (bool, error) {
	return f.current().TenantExists(ctx, id)
}

func (f *tenantsFake) Tenant(ctx context.Context, id reqctx.TenantID) (coreaccess.TenantRef, bool, error) {
	return f.current().Tenant(ctx, id)
}

func (f *tenantsFake) Grant(ctx context.Context, account reqctx.AccountID, tenant reqctx.TenantID) (coreaccess.TenantGrant, error) {
	return f.current().Grant(ctx, account, tenant)
}

func (f *tenantsFake) Select(ctx context.Context, account reqctx.AccountID) (*coreaccess.TenantGrant, error) {
	return f.current().Select(ctx, account)
}

func (f *tenantsFake) Remember(ctx context.Context, account reqctx.AccountID, tenant reqctx.TenantID) error {
	f.mu.Lock()
	err := f.rememberErr
	f.mu.Unlock()
	if err != nil {
		return err
	}
	err = f.current().Remember(ctx, account, tenant)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.remembered[account] = tenant
	return nil
}

func (f *tenantsFake) Memberships(ctx context.Context, account reqctx.AccountID) ([]coreaccess.MembershipRef, error) {
	return f.current().Memberships(ctx, account)
}
