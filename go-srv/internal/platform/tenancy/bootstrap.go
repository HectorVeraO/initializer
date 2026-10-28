package tenancy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/db"
)

const (
	// tenantBootstrapLockKey is the session-level advisory lock replicas
	// serialize on while they create the first tenant; identity's operator
	// bootstrap and core's migration lock use other keys.
	tenantBootstrapLockKey int64 = 7142001002

	tenantBootstrapTimeout = 60 * time.Second
	unlockTimeout          = 5 * time.Second
	bootstrapRole          = "admin"
	reasonTenantsExist     = "tenants_exist"
)

// start is the Start hook: when APP_BOOTSTRAP_TENANT_SLUG is set it creates
// the first tenant for the bootstrap operator. An error stops the process
// before the listener opens.
func (s *service) start(ctx context.Context, h *core.Host) error {
	if !s.cfg.Bootstrap.Enabled {
		return nil
	}
	runCtx, cancel := context.WithTimeout(ctx, tenantBootstrapTimeout)
	defer cancel()
	err := s.bootstrap(runCtx, h)
	if err != nil {
		return fmt.Errorf("tenancy: bootstrap: %w", err)
	}
	return nil
}

// bootstrap creates the first tenant and the admin membership of the account
// that has the admin email, idempotently: it is a no-op once any tenant
// exists. A failure is returned, not logged: core logs it once as
// server_failed.
func (s *service) bootstrap(ctx context.Context, h *core.Host) error {
	logger := h.Logger()
	pool := s.tenancy.pool
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	_, err = conn.Exec(ctx, "SELECT pg_advisory_lock($1)", tenantBootstrapLockKey)
	if err != nil {
		conn.Release()
		return fmt.Errorf("advisory lock: %w", err)
	}
	defer unlock(conn)

	var hasTenants bool
	err = conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM ten_tenants)").Scan(&hasTenants)
	if err != nil {
		return fmt.Errorf("check tenants: %w", err)
	}
	if hasTenants {
		logger.InfoContext(ctx, "tenant_bootstrap_skipped", slog.String("reason", reasonTenantsExist))
		return nil
	}

	// The oldest account with the email is the operator (accounts.Directory.ByEmail).
	holders, err := h.AccountDirectory().ByEmail(ctx, s.cfg.Bootstrap.AdminEmail)
	if err != nil {
		return fmt.Errorf("find admin account: %w", err)
	}
	if len(holders) == 0 {
		return errors.New("no account has APP_BOOTSTRAP_ADMIN_EMAIL")
	}
	accountID := string(holders[0].ID)
	tenantID, err := s.createFirstTenant(ctx, accountID)
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "tenant_bootstrap_created",
		slog.String("tenant_id", tenantID), slog.String("account_id", accountID))
	return nil
}

// createFirstTenant writes the tenant and the admin membership in one
// transaction; the membership starts as the account's last selected tenant.
func (s *service) createFirstTenant(ctx context.Context, accountID string) (string, error) {
	var tenantID string
	now := s.tenancy.clock.Now()
	err := db.InTx(ctx, s.tenancy.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`INSERT INTO ten_tenants (slug, name, created_at, updated_at) VALUES ($1, $2, $3, $3) RETURNING id::text`,
			s.cfg.Bootstrap.TenantSlug, s.cfg.Bootstrap.TenantName, now).Scan(&tenantID)
		if err != nil {
			return fmt.Errorf("insert tenant: %w", err)
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO ten_memberships (tenant_id, account_id, role, last_selected_at, created_at, updated_at)
VALUES ($1::uuid, $2::uuid, $3, $4, $4, $4)`, tenantID, accountID, bootstrapRole, now)
		if err != nil {
			return fmt.Errorf("insert membership: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return tenantID, nil
}

// unlock releases the advisory lock and the connection. A connection whose
// unlock failed is closed, which ends the session and so the lock.
func unlock(conn *pgxpool.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
	defer cancel()
	_, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", tenantBootstrapLockKey)
	if err != nil {
		_ = conn.Conn().Close(ctx)
	}
	conn.Release()
}
