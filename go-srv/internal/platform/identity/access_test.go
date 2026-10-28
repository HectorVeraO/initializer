package identity_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity"
)

var accessNow = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

type accessEnv struct {
	t      *testing.T
	pool   *pgxpool.Pool
	access *identity.Access
}

func newAccessEnv(t *testing.T, operatorEmails ...string) *accessEnv {
	t.Helper()
	pool := identity.NewTestPool(t)
	return &accessEnv{t: t, pool: pool, access: identity.NewAccess(pool, clock.Fixed(accessNow), operatorEmails)}
}

func uniqueSuffix(t testing.TB) string {
	t.Helper()
	b := make([]byte, 4)
	_, err := rand.Read(b)
	if err != nil {
		t.Fatalf("random: %v", err)
	}
	return hex.EncodeToString(b)
}

func (e *accessEnv) exec(sql string, args ...any) {
	e.t.Helper()
	_, err := e.pool.Exec(context.Background(), sql, args...)
	if err != nil {
		e.t.Fatalf("exec %q: %v", sql, err)
	}
}

func (e *accessEnv) scanString(sql string, args ...any) string {
	e.t.Helper()
	var out string
	err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&out)
	if err != nil {
		e.t.Fatalf("query %q: %v", sql, err)
	}
	return out
}

// account links a fresh identity and returns its account.
func (e *accessEnv) account() identity.Account {
	e.t.Helper()
	suffix := uniqueSuffix(e.t)
	identityID := e.scanString("SELECT gen_random_uuid()::text")
	account, err := e.access.LinkIdentity(context.Background(), identityID, "jane-"+suffix+"@example.com")
	if err != nil {
		e.t.Fatalf("link identity: %v", err)
	}
	return account
}

func (e *accessEnv) tenant(slug, status string) string {
	e.t.Helper()
	return e.scanString(
		"INSERT INTO ten_tenants (slug, name, status) VALUES ($1, $1, $2) RETURNING id::text", slug, status)
}

func (e *accessEnv) member(tenantID, accountID, role, status string, createdAt time.Time) {
	e.t.Helper()
	e.exec(`INSERT INTO ten_memberships (tenant_id, account_id, role, status, created_at)
VALUES ($1::uuid, $2::uuid, $3, $4, $5)`, tenantID, accountID, role, status, createdAt)
}

func (e *accessEnv) setAccountStatus(accountID, status string) {
	e.t.Helper()
	e.exec("UPDATE iam_accounts SET status = $2 WHERE id = $1::uuid", accountID, status)
}

func requireDenied(t *testing.T, err error, code api.Code) *api.Error {
	t.Helper()
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("want 403 %s, got %v", code, err)
	}
	if apiErr.Status != http.StatusForbidden || apiErr.Code != code {
		t.Fatalf("want 403 %s, got %d %s", code, apiErr.Status, apiErr.Code)
	}
	return apiErr
}

func TestCheckAccessAccountStates(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	for _, status := range []string{"locked", "disabled"} {
		account := env.account()
		env.setAccountStatus(account.ID, status)
		_, err := env.access.CheckAccount(ctx, account.ID)
		requireDenied(t, err, identity.CodeAccountLockedOrDisabled)
	}
	missing := env.scanString("SELECT gen_random_uuid()::text")
	for _, id := range []string{missing, "not-a-uuid", ""} {
		_, err := env.access.CheckAccount(ctx, id)
		requireDenied(t, err, identity.CodeAccountLockedOrDisabled)
	}
}

func TestCheckAccountReturnsTheOperatorFlag(t *testing.T) {
	env := newAccessEnv(t, "operator@example.com")
	ctx := context.Background()
	operator, err := env.access.LinkIdentity(ctx, env.scanString("SELECT gen_random_uuid()::text"), "operator@example.com")
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	isOperator, err := env.access.CheckAccount(ctx, operator.ID)
	if err != nil || !isOperator {
		t.Fatalf("operator: got %v %v", isOperator, err)
	}
	plain := env.account()
	isOperator, err = env.access.CheckAccount(ctx, plain.ID)
	if err != nil || isOperator {
		t.Fatalf("plain: got %v %v", isOperator, err)
	}
}

func TestCheckAccountReadFailureIsOperational(t *testing.T) {
	env := newAccessEnv(t)
	account := env.account()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := env.access.CheckAccount(cancelled, account.ID)
	var apiErr *api.Error
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want a non-API operational error", err)
	}
}

func TestLinkIdentityCreatesOnFirstSight(t *testing.T) {
	env := newAccessEnv(t)
	identityID := env.scanString("SELECT gen_random_uuid()::text")
	email := "Jane-" + uniqueSuffix(t) + "@Example.COM"
	account, err := env.access.LinkIdentity(context.Background(), identityID, email)
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if account.Email != strings.ToLower(email) || account.KratosIdentityID != identityID ||
		account.Status != "active" || account.IsPlatformOperator || !account.CreatedAt.Equal(accessNow) {
		t.Fatalf("account = %+v", account)
	}
}

func TestLinkIdentityIsStableAndRefreshesChangedEmail(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	identityID := env.scanString("SELECT gen_random_uuid()::text")
	suffix := uniqueSuffix(t)
	first, err := env.access.LinkIdentity(ctx, identityID, "jane-"+suffix+"@example.com")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	again, err := env.access.LinkIdentity(ctx, identityID, "jane-"+suffix+"@example.com")
	if err != nil || again.ID != first.ID {
		t.Fatalf("again = %+v %v", again, err)
	}
	renamed, err := env.access.LinkIdentity(ctx, identityID, "Renamed-"+suffix+"@example.com")
	if err != nil {
		t.Fatalf("renamed: %v", err)
	}
	if renamed.ID != first.ID || renamed.Email != "renamed-"+suffix+"@example.com" {
		t.Fatalf("renamed = %+v", renamed)
	}
	count := env.scanString("SELECT count(*)::text FROM iam_accounts WHERE kratos_identity_id = $1::uuid", identityID)
	if count != "1" {
		t.Fatalf("accounts for the identity = %s", count)
	}
}

func TestLinkIdentityMovesUpdatedAtOnlyWhenTheEmailChanges(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	later := identity.NewAccess(env.pool, clock.Fixed(accessNow.Add(time.Hour)), nil)
	latest := identity.NewAccess(env.pool, clock.Fixed(accessNow.Add(2*time.Hour)), nil)
	identityID := env.scanString("SELECT gen_random_uuid()::text")
	email := "jane-" + uniqueSuffix(t) + "@example.com"
	created, err := env.access.LinkIdentity(ctx, identityID, email)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !created.UpdatedAt.Equal(accessNow) {
		t.Fatalf("created updated_at = %v, want %v", created.UpdatedAt, accessNow)
	}
	unchanged, err := later.LinkIdentity(ctx, identityID, email)
	if err != nil {
		t.Fatalf("unchanged: %v", err)
	}
	if !unchanged.UpdatedAt.Equal(accessNow) {
		t.Fatalf("an upsert without a change moved updated_at to %v", unchanged.UpdatedAt)
	}
	changed, err := latest.LinkIdentity(ctx, identityID, "renamed-"+email)
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if !changed.UpdatedAt.Equal(accessNow.Add(2 * time.Hour)) {
		t.Fatalf("an email change left updated_at at %v", changed.UpdatedAt)
	}
	if !changed.CreatedAt.Equal(accessNow) {
		t.Fatalf("created_at moved to %v", changed.CreatedAt)
	}
}

func TestLinkIdentityOperatorFlagAtCreationOnly(t *testing.T) {
	env := newAccessEnv(t, "admin@example.com")
	ctx := context.Background()
	listed, err := env.access.LinkIdentity(ctx, env.scanString("SELECT gen_random_uuid()::text"), "ADMIN@example.com")
	if err != nil || !listed.IsPlatformOperator {
		t.Fatalf("listed = %+v %v", listed, err)
	}
	unlistedIdentity := env.scanString("SELECT gen_random_uuid()::text")
	unlisted, err := env.access.LinkIdentity(ctx, unlistedIdentity, "jane-"+uniqueSuffix(t)+"@example.com")
	if err != nil || unlisted.IsPlatformOperator {
		t.Fatalf("unlisted = %+v %v", unlisted, err)
	}
	// An email change to a listed address never grants the flag after creation.
	changed, err := env.access.LinkIdentity(ctx, unlistedIdentity, "admin@example.com")
	if err != nil || changed.IsPlatformOperator {
		t.Fatalf("changed = %+v %v", changed, err)
	}
}

func TestLinkIdentityRejectsBadInput(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	identityID := env.scanString("SELECT gen_random_uuid()::text")
	cases := map[string][2]string{
		"identity not a uuid": {"not-a-uuid", "jane@example.com"},
		"empty identity":      {"", "jane@example.com"},
		"empty email":         {identityID, ""},
		"blank email":         {identityID, "   "},
		"oversized email":     {identityID, strings.Repeat("a", 250) + "@example.com"},
	}
	for name, in := range cases {
		_, err := env.access.LinkIdentity(ctx, in[0], in[1])
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	count := env.scanString("SELECT count(*)::text FROM iam_accounts")
	if count != "0" {
		t.Fatalf("a rejected call created %s accounts", count)
	}
}

func TestLinkIdentityEmailLengthBoundary(t *testing.T) {
	env := newAccessEnv(t)
	ctx := context.Background()
	const domain = "@example.com"
	atLimit := strings.Repeat("a", 254-len(domain)) + domain
	overLimit := strings.Repeat("a", 255-len(domain)) + domain
	identityID := env.scanString("SELECT gen_random_uuid()::text")
	_, err := env.access.LinkIdentity(ctx, identityID, atLimit)
	if err != nil {
		t.Fatalf("254-character email rejected: %v", err)
	}
	_, err = env.access.LinkIdentity(ctx, env.scanString("SELECT gen_random_uuid()::text"), overLimit)
	if err == nil {
		t.Fatal("255-character email accepted")
	}
}
