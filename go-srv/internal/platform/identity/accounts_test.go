package identity

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/accounts"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity/kratos"
)

const janeEmail = "jane.doe@example.com"

var accountsStart = time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)

// accountsEnv is the account port over a fresh database, a fake Kratos admin
// API and a clock the test moves.
type accountsEnv struct {
	t      *testing.T
	pool   *pgxpool.Pool
	store  *store
	clock  *settableClock
	kratos *fakeKratos
}

func newAccountsEnv(t *testing.T) *accountsEnv {
	t.Helper()
	pool := newTestPool(t)
	clk := &settableClock{now: accountsStart}
	return &accountsEnv{t: t, pool: pool, store: newStore(pool, clk), clock: clk, kratos: newFakeKratos(t)}
}

func (e *accountsEnv) exec(sql string, args ...any) {
	e.t.Helper()
	_, err := e.pool.Exec(context.Background(), sql, args...)
	if err != nil {
		e.t.Fatalf("exec %q: %v", sql, err)
	}
}

func (e *accountsEnv) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n)
	if err != nil {
		e.t.Fatalf("count %q: %v", sql, err)
	}
	return n
}

func (e *accountsEnv) account(email string) Account {
	e.t.Helper()
	return mustAccount(e.t, e.store, email)
}

func requireAPIError(t *testing.T, err error, status int, code api.Code) *api.Error {
	t.Helper()
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("want api error %d %s, got %v", status, code, err)
	}
	if apiErr.Status != status || apiErr.Code != code {
		t.Fatalf("want %d %s, got %d %s (%s)", status, code, apiErr.Status, apiErr.Code, apiErr.Message)
	}
	return apiErr
}

func newProvisioner(t *testing.T, e *accountsEnv, operatorEmails ...string) *accountDirectory {
	t.Helper()
	adminURL, err := url.Parse(e.kratos.server.URL)
	if err != nil {
		t.Fatalf("parse fake url: %v", err)
	}
	return newAccountDirectory(e.pool, e.clock, kratos.NewAdmin(adminURL), operatorEmails)
}

func TestAccountProvision(t *testing.T) {
	ctx := context.Background()

	t.Run("listed email becomes platform operator", func(t *testing.T) {
		e := newAccountsEnv(t)
		dir := newProvisioner(t, e, janeEmail)

		ref, err := dir.Provision(ctx, accounts.NewAccount{Email: janeEmail, Password: randomPassword(t), FullName: "  Jane Doe "})

		if err != nil {
			t.Fatalf("provision: %v", err)
		}
		if !ref.PlatformOperator || ref.Email != janeEmail {
			t.Fatalf("ref = %+v, want a platform operator", ref)
		}
		if e.kratos.identities[janeEmail] == "" {
			t.Fatal("no Kratos identity was created")
		}
	})

	t.Run("unlisted email is not an operator", func(t *testing.T) {
		e := newAccountsEnv(t)
		dir := newProvisioner(t, e, "ops@example.com")

		ref, err := dir.Provision(ctx, accounts.NewAccount{Email: janeEmail, Password: randomPassword(t), FullName: "Jane Doe"})

		if err != nil {
			t.Fatalf("provision: %v", err)
		}
		if ref.PlatformOperator {
			t.Fatalf("ref = %+v, want no operator flag", ref)
		}
	})

	t.Run("existing identity is a conflict and creates nothing", func(t *testing.T) {
		e := newAccountsEnv(t)
		dir := newProvisioner(t, e, janeEmail)
		e.kratos.addIdentity(janeEmail)

		_, err := dir.Provision(ctx, accounts.NewAccount{Email: janeEmail, Password: randomPassword(t), FullName: "Jane Doe"})

		requireAPIError(t, err, http.StatusConflict, accounts.CodeIdentityAlreadyExists)
		if n := e.count("SELECT count(*) FROM iam_accounts WHERE email = $1", janeEmail); n != 0 {
			t.Fatalf("accounts = %d, want 0", n)
		}
	})

	t.Run("create conflict after a clear lookup is a conflict", func(t *testing.T) {
		e := newAccountsEnv(t)
		dir := newProvisioner(t, e)
		e.kratos.addIdentity(janeEmail)
		e.kratos.hideFirstFind = true

		_, err := dir.Provision(ctx, accounts.NewAccount{Email: janeEmail, Password: randomPassword(t), FullName: "Jane Doe"})

		requireAPIError(t, err, http.StatusConflict, accounts.CodeIdentityAlreadyExists)
		if n := e.count("SELECT count(*) FROM iam_accounts WHERE email = $1", janeEmail); n != 0 {
			t.Fatalf("accounts = %d, want 0", n)
		}
	})

	t.Run("account insert conflict is a conflict and deletes the new identity", func(t *testing.T) {
		e := newAccountsEnv(t)
		dir := newProvisioner(t, e)
		taken := e.account("taken@example.com")
		e.kratos.nextID = taken.KratosIdentityID

		_, err := dir.Provision(ctx, accounts.NewAccount{Email: janeEmail, Password: randomPassword(t), FullName: "Jane Doe"})

		requireAPIError(t, err, http.StatusConflict, accounts.CodeIdentityAlreadyExists)
		if e.kratos.identities[janeEmail] != "" {
			t.Fatal("the Kratos identity was left without an account")
		}
		retry, err := dir.Provision(ctx, accounts.NewAccount{Email: janeEmail, Password: randomPassword(t), FullName: "Jane Doe"})
		if err != nil || retry.Email != janeEmail {
			t.Fatalf("retry = %+v err=%v, want the account created", retry, err)
		}
	})

	t.Run("account insert conflict with a failed identity delete is operational", func(t *testing.T) {
		e := newAccountsEnv(t)
		dir := newProvisioner(t, e)
		taken := e.account("taken@example.com")
		e.kratos.nextID = taken.KratosIdentityID
		e.kratos.failDelete = true

		_, err := dir.Provision(ctx, accounts.NewAccount{Email: janeEmail, Password: randomPassword(t), FullName: "Jane Doe"})

		var apiErr *api.Error
		if err == nil || errors.As(err, &apiErr) {
			t.Fatalf("err = %v, want an operational error, not an API rejection", err)
		}
	})

	t.Run("rejected password is a 422 password issue", func(t *testing.T) {
		e := newAccountsEnv(t)
		dir := newProvisioner(t, e)
		e.kratos.rejectStatus = http.StatusBadRequest

		_, err := dir.Provision(ctx, accounts.NewAccount{Email: janeEmail, Password: "weak", FullName: "Jane Doe"})

		apiErr := requireAPIError(t, err, http.StatusUnprocessableEntity, api.CodeValidationFailed)
		if len(apiErr.Issues) != 1 || apiErr.Issues[0].Field != "password" || apiErr.Issues[0].Code != accounts.CodePasswordRejected {
			t.Fatalf("issues = %+v", apiErr.Issues)
		}
	})

	t.Run("unreachable Kratos is a 503 and creates nothing", func(t *testing.T) {
		e := newAccountsEnv(t)
		dir := newProvisioner(t, e)
		e.kratos.rejectStatus = http.StatusServiceUnavailable

		_, err := dir.Provision(ctx, accounts.NewAccount{Email: janeEmail, Password: randomPassword(t), FullName: "Jane Doe"})

		requireAPIError(t, err, http.StatusServiceUnavailable, CodeIdentityProviderUnavailable)
		if n := e.count("SELECT count(*) FROM iam_accounts WHERE email = $1", janeEmail); n != 0 {
			t.Fatalf("accounts = %d, want 0", n)
		}
	})

	missing := map[string]accounts.NewAccount{
		"email":     {Email: "", Password: "placeholder-secret", FullName: "Jane Doe"},
		"password":  {Email: janeEmail, Password: "", FullName: "Jane Doe"},
		"full name": {Email: janeEmail, Password: "placeholder-secret", FullName: "   "},
	}
	for name, input := range missing {
		t.Run("missing "+name+" fails before calling Kratos", func(t *testing.T) {
			e := newAccountsEnv(t)
			dir := newProvisioner(t, e)

			_, err := dir.Provision(ctx, input)

			if err == nil {
				t.Fatal("provision succeeded with a missing field")
			}
			var apiErr *api.Error
			if errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want an operational error, not an API rejection", err)
			}
			if len(e.kratos.received()) != 0 {
				t.Fatal("Kratos was called for invalid input")
			}
			if n := e.count("SELECT count(*) FROM iam_accounts"); n != 0 {
				t.Fatalf("accounts = %d, want 0", n)
			}
		})
	}
}

func TestAccountDirectoryGet(t *testing.T) {
	ctx := context.Background()
	e := newAccountsEnv(t)
	dir := newProvisioner(t, e)
	first := e.account("first@example.com")
	second := e.account("second@example.com")
	e.exec("UPDATE iam_accounts SET status = 'locked' WHERE id = $1::uuid", second.ID)

	got, err := dir.Get(ctx, []reqctx.AccountID{
		reqctx.AccountID(first.ID), reqctx.AccountID(second.ID), reqctx.AccountID(first.ID),
		"99999999-9999-4999-8999-999999999999", "not-a-uuid", "",
	})

	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d accounts, want 2: %+v", len(got), got)
	}
	if ref := got[reqctx.AccountID(first.ID)]; ref.Email != "first@example.com" || ref.Status != accounts.StatusActive {
		t.Fatalf("first = %+v", ref)
	}
	if ref := got[reqctx.AccountID(second.ID)]; ref.Status != accounts.StatusLocked {
		t.Fatalf("second = %+v, want locked", ref)
	}

	t.Run("empty ids return an empty non-nil map", func(t *testing.T) {
		for name, ids := range map[string][]reqctx.AccountID{"nil": nil, "empty": {}, "only invalid": {"not-a-uuid"}} {
			got, err := dir.Get(ctx, ids)
			if err != nil || got == nil || len(got) != 0 {
				t.Fatalf("%s: got %v err=%v, want an empty non-nil map", name, got, err)
			}
		}
	})
}

func TestAccountDirectoryByEmail(t *testing.T) {
	ctx := context.Background()
	e := newAccountsEnv(t)
	dir := newProvisioner(t, e)
	older := e.account("shared@example.com")
	newer := e.account("shared@example.com")
	e.exec("UPDATE iam_accounts SET created_at = created_at + interval '1 hour' WHERE id = $1::uuid", newer.ID)
	e.account("other@example.com")

	t.Run("normalizes the email and lists oldest first", func(t *testing.T) {
		got, err := dir.ByEmail(ctx, "  Shared@example.com ")

		if err != nil {
			t.Fatalf("by email: %v", err)
		}
		if len(got) != 2 || string(got[0].ID) != older.ID || string(got[1].ID) != newer.ID {
			t.Fatalf("got %+v, want [%s %s]", got, older.ID, newer.ID)
		}
	})

	t.Run("no match is an empty non-nil list", func(t *testing.T) {
		got, err := dir.ByEmail(ctx, "nobody@example.com")

		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("got %v err=%v, want an empty non-nil list", got, err)
		}
	})
}

func TestAccountDirectoryGrantsOperator(t *testing.T) {
	e := newAccountsEnv(t)
	dir := newProvisioner(t, e, "ops@example.com")

	if !dir.GrantsOperator("ops@example.com") || !dir.GrantsOperator("OPS@example.com") {
		t.Fatal("a listed email must grant operator, case-insensitively")
	}
	if dir.GrantsOperator("other@example.com") || dir.GrantsOperator("") {
		t.Fatal("an unlisted or empty email must not grant operator")
	}
}

func TestAccountProvisionerGrantsOperatorOnlyToListedEmails(t *testing.T) {
	e := newAccountsEnv(t)
	port := newAccountDirectory(e.pool, e.clock, nil, []string{"ops@example.com"})

	if !port.GrantsOperator("ops@example.com") || port.GrantsOperator("other@example.com") {
		t.Fatal("provisioner did not honor the operator list")
	}
}
