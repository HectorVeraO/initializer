// This file is package testpg and not testpg_test on purpose: the drop path is
// unexported, and the failure it composes is the behaviour under test.
package testpg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// dropTestDatabase is the server named by APP_TEST_DATABASE_URL plus the admin
// role's connection, which the drop tests share.
type dropTestDatabase struct {
	url   string
	ctx   context.Context
	admin *pgx.Conn
}

// openDropTestDatabase skips the test when EnvURL is unset.
func openDropTestDatabase(t *testing.T) dropTestDatabase {
	t.Helper()
	adminURL := os.Getenv(EnvURL)
	if adminURL == "" {
		t.Skipf("%s is not set; skipping database test", EnvURL)
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("testpg: admin database unreachable: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close(ctx) })
	return dropTestDatabase{url: adminURL, ctx: ctx, admin: admin}
}

// scratch creates a database owned by owner (the admin role when owner is
// empty) and drops it on cleanup.
func (d dropTestDatabase) scratch(t *testing.T, prefix, owner string) string {
	t.Helper()
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("testpg: random: %v", err)
	}
	name := prefix + "_" + hex.EncodeToString(buf)
	statement := "CREATE DATABASE " + pgx.Identifier{name}.Sanitize()
	if owner != "" {
		statement += " OWNER " + pgx.Identifier{owner}.Sanitize()
	}
	if _, err := d.admin.Exec(d.ctx, statement); err != nil {
		t.Fatalf("testpg: create %s: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = d.admin.Exec(d.ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	return name
}

// exists reports whether the server still holds the database.
func (d dropTestDatabase) exists(t *testing.T, name string) bool {
	t.Helper()
	var found bool
	err := d.admin.QueryRow(d.ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&found)
	if err != nil {
		t.Fatalf("testpg: lookup %s: %v", name, err)
	}
	return found
}

// role returns the name of the admin connection's role.
func (d dropTestDatabase) role(t *testing.T) string {
	t.Helper()
	var name string
	if err := d.admin.QueryRow(d.ctx, "SELECT current_user").Scan(&name); err != nil {
		t.Fatalf("testpg: read the connected role: %v", err)
	}
	return name
}

// isSuperuser reports whether the admin connection's role is a superuser.
func (d dropTestDatabase) isSuperuser(t *testing.T) bool {
	t.Helper()
	var isSuperuser bool
	err := d.admin.QueryRow(d.ctx,
		"SELECT rolsuper FROM pg_roles WHERE rolname = current_user").Scan(&isSuperuser)
	if err != nil {
		t.Fatalf("testpg: read the connected role: %v", err)
	}
	return isSuperuser
}

// canSignalEveryBackend reports whether the admin role may terminate a backend
// of any role, the privilege a FORCE drop needs.
func (d dropTestDatabase) canSignalEveryBackend(t *testing.T) bool {
	t.Helper()
	var canSignal bool
	err := d.admin.QueryRow(d.ctx, `SELECT rolsuper OR pg_has_role(current_user, 'pg_signal_backend', 'member')
FROM pg_roles WHERE rolname = current_user`).Scan(&canSignal)
	if err != nil {
		t.Fatalf("testpg: read the connected role: %v", err)
	}
	return canSignal
}

// loginRole creates a login role with no privilege beyond logging in, and drops
// it on cleanup; it skips the test when the admin role may not create one.
func (d dropTestDatabase) loginRole(t *testing.T) (string, string) {
	t.Helper()
	role := "it_other_" + randomHex(t)
	password := "it-" + randomHex(t)
	_, err := d.admin.Exec(d.ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN PASSWORD '"+password+"'")
	if err != nil {
		t.Skipf("the admin role cannot create a login role (%v); the case needs a second role", err)
	}
	t.Cleanup(func() {
		_, _ = d.admin.Exec(d.ctx, "DROP ROLE IF EXISTS "+pgx.Identifier{role}.Sanitize())
	})
	return role, password
}

// grantConnect lets role connect to the database, so a case never depends on the
// template's own ACL; it skips the test when the admin role may not grant it.
func (d dropTestDatabase) grantConnect(t *testing.T, database, role string) {
	t.Helper()
	statement := "GRANT CONNECT ON DATABASE " + pgx.Identifier{database}.Sanitize() +
		" TO " + pgx.Identifier{role}.Sanitize()
	_, err := d.admin.Exec(d.ctx, statement)
	if err != nil {
		t.Skipf("the admin role cannot grant CONNECT on its test database (%v); the case needs a connecting role", err)
	}
}

// urlFor returns the admin URL with the credentials of role and a database.
func (d dropTestDatabase) urlFor(t *testing.T, role, password, database string) string {
	t.Helper()
	parsed, err := url.Parse(d.url)
	if err != nil {
		t.Fatalf("testpg: %s is not a URL", EnvURL)
	}
	parsed.User = url.UserPassword(role, password)
	parsed.Path = "/" + database
	return parsed.String()
}

// connect opens a connection and closes it after the test.
func (d dropTestDatabase) connect(t *testing.T, rawURL string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(d.ctx, rawURL)
	if err != nil {
		t.Fatalf("testpg: connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(d.ctx) })
	return conn
}

// hold keeps the backend of conn attached to its database.
func (d dropTestDatabase) hold(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	if err := conn.QueryRow(d.ctx, "SELECT pg_backend_pid()").Scan(new(int)); err != nil {
		t.Fatalf("testpg: hold a backend: %v", err)
	}
}

// TestDropSucceedsWhileAPoolIsStillOpen proves the drop survives the condition
// the flake came from: the pool has not finished closing when the drop runs, so
// a backend of this role is still attached.
func TestDropSucceedsWhileAPoolIsStillOpen(t *testing.T) {
	d := openDropTestDatabase(t)
	name := d.scratch(t, "it_dropopen", "")
	quoted := pgx.Identifier{name}.Sanitize()
	pool, err := pgxpool.New(d.ctx, withDatabase(t, d.url, name))
	if err != nil {
		t.Fatalf("testpg: open pool: %v", err)
	}
	// Deliberately not closed on cleanup: the pool must still hold a backend
	// when the drop runs. The deferred close runs before the cleanups.
	defer pool.Close()
	if _, err := pool.Exec(d.ctx, "SELECT 1"); err != nil {
		t.Fatalf("testpg: use the pool: %v", err)
	}

	drop(t, d.url, quoted)

	if d.exists(t, name) {
		t.Errorf("testpg: database %s survived the drop while its pool was open", name)
	}
}

// TestDropForcesThroughWhenAnotherRoleHoldsABackendItCanSignal proves the
// privileged path: a role that is a superuser or a member of pg_signal_backend
// still drops a database another role's backend holds.
func TestDropForcesThroughWhenAnotherRoleHoldsABackendItCanSignal(t *testing.T) {
	d := openDropTestDatabase(t)
	if !d.canSignalEveryBackend(t) {
		t.Skip("the admin role is neither a superuser nor a member of pg_signal_backend; a FORCE drop cannot succeed")
	}
	role, password := d.loginRole(t)
	name := d.scratch(t, "it_dropforce", "")
	d.grantConnect(t, name, role)
	quoted := pgx.Identifier{name}.Sanitize()
	foreign := d.connect(t, d.urlFor(t, role, password, name))
	d.hold(t, foreign)
	dropper := d.connect(t, d.url)

	err := deleteDatabase(d.ctx, dropper, quoted, name)

	if err != nil {
		t.Fatalf("testpg: drop of %s failed although this role can signal %s's backend: %v", name, role, err)
	}
	if d.exists(t, name) {
		t.Errorf("testpg: database %s survived the drop", name)
	}
}

// TestDropNamesTheAttachedRoleWhenItCannotSignalIt proves the case the cleanup
// was rewritten for: a role that may not signal another role's backend reports
// who held the database instead of a bare driver error.
func TestDropNamesTheAttachedRoleWhenItCannotSignalIt(t *testing.T) {
	d := openDropTestDatabase(t)
	// The database belongs to the dropping role, so only a superuser can remove
	// it once this test has left it behind.
	if !d.isSuperuser(t) {
		t.Skip("the admin role is not a superuser; it could not clean up a database another role owns")
	}
	holderRole := d.role(t)
	dropperRole, dropperPassword := d.loginRole(t)
	target := d.scratch(t, "it_dropsignal", dropperRole)
	// The dropper may not connect to the admin database: a role that owns its
	// own database is a role that may connect to it, so it works from there.
	home := d.scratch(t, "it_drophome", dropperRole)
	d.grantConnect(t, target, holderRole)
	quoted := pgx.Identifier{target}.Sanitize()
	holder := d.connect(t, withDatabase(t, d.url, target))
	d.hold(t, holder)
	dropper := d.connect(t, d.urlFor(t, dropperRole, dropperPassword, home))

	err := deleteDatabase(d.ctx, dropper, quoted, target)

	if err == nil {
		t.Fatalf("testpg: drop of %s succeeded although %s owns it and may not signal %s's backend",
			target, dropperRole, holderRole)
	}
	if !strings.Contains(err.Error(), holderRole) {
		t.Errorf("testpg: drop failure does not name the attached role %s: %v", holderRole, err)
	}
	if !isSignalDenied(err) && !isObjectInUse(err) {
		t.Errorf("testpg: drop failure carries neither SQLSTATE %s nor %s: %v",
			sqlstateSignalDenied, sqlstateObjectInUse, err)
	}
}

// TestDropRetriesOnceTheDatabaseIsFree proves the retry: the first plain attempt
// answers 55006 while another role's backend holds the database, and a later
// attempt succeeds once that backend has gone. The dropping role may not signal
// the holder, so the FORCE attempt cannot be what saves the drop.
func TestDropRetriesOnceTheDatabaseIsFree(t *testing.T) {
	d := openDropTestDatabase(t)
	// The database belongs to the dropping role, so only a superuser can remove
	// it once this test has left it behind.
	if !d.isSuperuser(t) {
		t.Skip("the admin role is not a superuser; it could not clean up a database another role owns")
	}
	dropperRole, dropperPassword := d.loginRole(t)
	holderRole, holderPassword := d.loginRole(t)
	target := d.scratch(t, "it_dropretry", dropperRole)
	// A role may connect to the database it owns, and the dropper may not connect
	// to the admin database, so it drops from its own home database.
	home := d.scratch(t, "it_drophome", dropperRole)
	d.grantConnect(t, target, holderRole)
	quoted := pgx.Identifier{target}.Sanitize()
	holder := d.connect(t, d.urlFor(t, holderRole, holderPassword, target))
	d.hold(t, holder)
	dropper := d.connect(t, d.urlFor(t, dropperRole, dropperPassword, home))

	dropped := make(chan error, 1)
	go func() {
		dropped <- deleteDatabase(d.ctx, dropper, quoted, target)
	}()
	// The release is keyed to the end of the first attempt, which PostgreSQL
	// marks by answering 55006 after its own wait for the attached backends. A
	// fixed delay would have to sit after that wait and before two of them run
	// out: with the wait measured at 5.006 s here, about 3.2 s in one suite run
	// and about 2.9 s in CI, that leaves a sliver under a second, narrower than
	// the spread itself.
	waitErr := d.waitForFirstAttempt(t, dropperRole)
	firstAttemptLeftIt := d.exists(t, target)
	_ = holder.Close(d.ctx)
	err := <-dropped

	if waitErr != nil {
		t.Fatalf("testpg: %v", waitErr)
	}
	if !firstAttemptLeftIt {
		t.Fatalf("testpg: the first attempt dropped %s; the case never exercised the retry", target)
	}
	if err != nil {
		t.Fatalf("testpg: drop of %s failed once the database was free: %v", target, err)
	}
	if d.exists(t, target) {
		t.Errorf("testpg: database %s survived the drop", target)
	}
}

// waitForFirstAttempt waits until role has run and finished one plain DROP
// DATABASE, the moment PostgreSQL has answered 55006 for the attached backend.
func (d dropTestDatabase) waitForFirstAttempt(t *testing.T, role string) error {
	t.Helper()
	deadline := time.Now().Add(adminTimeout)
	started := false
	for time.Now().Before(deadline) {
		var running bool
		err := d.admin.QueryRow(d.ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
WHERE usename = $1 AND state = 'active' AND query LIKE 'DROP DATABASE%')`, role).Scan(&running)
		if err != nil {
			return fmt.Errorf("read pg_stat_activity: %w", err)
		}
		if running {
			started = true
		}
		if started && !running {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !started {
		return fmt.Errorf("the dropping role never ran a plain DROP DATABASE")
	}
	return fmt.Errorf("the first DROP DATABASE did not end within %v", adminTimeout)
}
