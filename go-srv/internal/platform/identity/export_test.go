package identity

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
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

// CheckAccess runs checkAccess.
func (a *Access) CheckAccess(ctx context.Context, accountID string, tenantID *string) (Grant, error) {
	return a.inner.checkAccess(ctx, accountID, tenantID)
}

// SelectTenant runs selectTenant.
func (a *Access) SelectTenant(ctx context.Context, accountID string) (*string, Grant, error) {
	return a.inner.selectTenant(ctx, accountID)
}

// LinkIdentity runs linkIdentity.
func (a *Access) LinkIdentity(ctx context.Context, kratosIdentityID, email string) (Account, error) {
	return a.inner.linkIdentity(ctx, kratosIdentityID, email)
}

// SetLastActiveTenant runs setLastActiveTenant.
func (a *Access) SetLastActiveTenant(ctx context.Context, accountID, tenantID string) error {
	return a.inner.setLastActiveTenant(ctx, accountID, tenantID)
}
