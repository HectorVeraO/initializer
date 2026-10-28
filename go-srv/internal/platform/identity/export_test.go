package identity

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/accounts"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity/kratos"
)

// NewTestPool gives the external tests an it_ database with the module
// migrations applied.
func NewTestPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	return newTestPool(t)
}

// Access exposes the package-internal access checks to the external tests.
type Access struct {
	inner *access
}

// NewAccess builds the access checks over pool.
func NewAccess(pool *pgxpool.Pool, clk clock.Clock, operatorEmails []string) *Access {
	return &Access{inner: newAccess(pool, clk, operatorEmails)}
}

// CheckAccount runs checkAccount.
func (a *Access) CheckAccount(ctx context.Context, accountID string) (bool, error) {
	return a.inner.checkAccount(ctx, accountID)
}

// LinkIdentity runs linkIdentity.
func (a *Access) LinkIdentity(ctx context.Context, kratosIdentityID, email string) (Account, error) {
	return a.inner.linkIdentity(ctx, kratosIdentityID, email)
}

// NewAccounts builds identity's account Directory and Provisioner.
func NewAccounts(pool *pgxpool.Pool, clk clock.Clock, admin *kratos.Admin, operatorEmails []string) (accounts.Directory, accounts.Provisioner) {
	port := newAccountDirectory(pool, clk, admin, operatorEmails)
	return port, port
}

// SplitAPIKey runs splitAPIKey.
func SplitAPIKey(value string) (string, string, bool) {
	return splitAPIKey(value)
}

// NewAPIKey runs newAPIKey.
func NewAPIKey() (string, error) {
	return newAPIKey()
}
