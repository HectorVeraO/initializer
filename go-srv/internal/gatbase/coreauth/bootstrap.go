package coreauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/db"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/coreauth/kratos"
)

// bootstrapLockKey is the session-level advisory lock replicas serialize on
// while they run the Day-0 bootstrap; it differs from core's migration lock.
const bootstrapLockKey int64 = 7142001001

const (
	bootstrapRole      = "admin"
	initialBackoff     = time.Second
	maxBackoff         = 8 * time.Second
	unlockTimeout      = 5 * time.Second
	reasonTenantsExist = "tenants_exist"
)

// BootstrapOutcome reports what Bootstrap did; it carries no secret.
type BootstrapOutcome struct {
	Skipped        bool
	IdentityReused bool
	AccountID      string
	TenantID       string
}

// start is the Start hook: the Day-0 bootstrap, then the platform operator
// grants. An error stops the process before the listener opens.
func (s *service) start(ctx context.Context, h *core.Host) error {
	logger := h.Logger()
	if s.cfg.Bootstrap.Enabled {
		_, err := s.bootstrap(ctx, logger, s.cfg.Bootstrap)
		if err != nil {
			return fmt.Errorf("coreauth: bootstrap: %w", err)
		}
	}
	err := s.applyPlatformOperators(ctx, logger, s.cfg.PlatformOperatorEmails)
	if err != nil {
		return fmt.Errorf("coreauth: platform operators: %w", err)
	}
	return nil
}

// bootstrap provisions the first tenant and its operator-admin idempotently.
// It is a no-op once any tenant exists. The whole run, lock wait included, is
// bounded by cfg.Timeout. A failure is returned, not logged: core logs it once
// as server_failed.
func (s *service) bootstrap(ctx context.Context, logger *slog.Logger, cfg BootstrapConfig) (BootstrapOutcome, error) {
	if !cfg.Enabled || s.admin == nil || s.store == nil || s.store.pool == nil {
		return BootstrapOutcome{}, errors.New("bootstrap is not configured")
	}
	runCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	return s.runBootstrap(runCtx, logger, cfg)
}

func (s *service) runBootstrap(ctx context.Context, logger *slog.Logger, cfg BootstrapConfig) (BootstrapOutcome, error) {
	conn, err := s.store.pool.Acquire(ctx)
	if err != nil {
		return BootstrapOutcome{}, fmt.Errorf("acquire connection: %w", err)
	}
	_, err = conn.Exec(ctx, "SELECT pg_advisory_lock($1)", bootstrapLockKey)
	if err != nil {
		conn.Release()
		return BootstrapOutcome{}, fmt.Errorf("advisory lock: %w", err)
	}
	defer unlock(conn)

	var hasTenants bool
	err = conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM ten_tenants)").Scan(&hasTenants)
	if err != nil {
		return BootstrapOutcome{}, fmt.Errorf("check tenants: %w", err)
	}
	if hasTenants {
		logger.InfoContext(ctx, "bootstrap_skipped", slog.String("reason", reasonTenantsExist))
		return BootstrapOutcome{Skipped: true}, nil
	}

	identity, reused, err := s.ensureIdentity(ctx, cfg)
	if err != nil {
		return BootstrapOutcome{}, err
	}
	if reused {
		logger.InfoContext(ctx, "bootstrap_identity_reused", slog.String("identity_id", identity.ID))
	}

	accountID, tenantID, err := s.createAdmin(ctx, cfg, identity.ID)
	if err != nil {
		return BootstrapOutcome{}, err
	}
	logger.InfoContext(ctx, "bootstrap_admin_created",
		slog.String("account_id", accountID), slog.String("tenant_id", tenantID))
	return BootstrapOutcome{IdentityReused: reused, AccountID: accountID, TenantID: tenantID}, nil
}

// unlock releases the advisory lock and the connection. A connection whose
// unlock failed is closed, which ends the session and so the lock.
func unlock(conn *pgxpool.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
	defer cancel()
	_, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", bootstrapLockKey)
	if err != nil {
		_ = conn.Conn().Close(ctx)
	}
	conn.Release()
}

// ensureIdentity returns the Kratos identity of the admin email: an existing
// one is reused with its password untouched, otherwise one is created. A
// Kratos that cannot answer is retried with capped exponential backoff until
// ctx ends.
func (s *service) ensureIdentity(ctx context.Context, cfg BootstrapConfig) (kratos.Identity, bool, error) {
	delay := initialBackoff
	for {
		identity, reused, err := s.findOrCreateIdentity(ctx, cfg)
		if err == nil {
			return identity, reused, nil
		}
		if !errors.Is(err, kratos.ErrUnavailable) {
			return kratos.Identity{}, false, err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return kratos.Identity{}, false, fmt.Errorf("timed out waiting for Kratos: %w", err)
		case <-timer.C:
		}
		delay = min(delay*2, maxBackoff)
	}
}

func (s *service) findOrCreateIdentity(ctx context.Context, cfg BootstrapConfig) (kratos.Identity, bool, error) {
	identity, found, err := s.admin.FindIdentityByEmail(ctx, cfg.AdminEmail)
	if err != nil {
		return kratos.Identity{}, false, err
	}
	if found {
		return identity, true, nil
	}
	created, err := s.admin.CreatePasswordIdentity(ctx, cfg.AdminEmail, cfg.AdminPassword.Reveal())
	if err == nil {
		return created, false, nil
	}
	if !errors.Is(err, kratos.ErrIdentityExists) {
		return kratos.Identity{}, false, err
	}
	// An earlier run or another process created it between the lookup and the create.
	identity, found, err = s.admin.FindIdentityByEmail(ctx, cfg.AdminEmail)
	if err != nil {
		return kratos.Identity{}, false, err
	}
	if !found {
		return kratos.Identity{}, false, errors.New("kratos reports the identity exists but cannot find it")
	}
	return identity, true, nil
}

// createAdmin writes the account, the tenant and the admin membership in one
// transaction and points the account at the tenant.
func (s *service) createAdmin(ctx context.Context, cfg BootstrapConfig, identityID string) (string, string, error) {
	var accountID, tenantID string
	now := s.store.clock.Now()
	err := db.InTx(ctx, s.store.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`INSERT INTO iam_accounts (kratos_identity_id, email, is_platform_operator, created_at, updated_at)
VALUES ($1::uuid, lower($2), true, $3, $3)
ON CONFLICT (kratos_identity_id) DO UPDATE
SET email = EXCLUDED.email, is_platform_operator = true, updated_at = EXCLUDED.updated_at
RETURNING id::text`, identityID, cfg.AdminEmail, now).Scan(&accountID)
		if err != nil {
			return fmt.Errorf("upsert account: %w", err)
		}
		err = tx.QueryRow(ctx,
			`INSERT INTO ten_tenants (slug, name, created_at, updated_at) VALUES ($1, $2, $3, $3) RETURNING id::text`,
			cfg.TenantSlug, cfg.TenantName, now).Scan(&tenantID)
		if err != nil {
			return fmt.Errorf("insert tenant: %w", err)
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO ten_memberships (tenant_id, account_id, role, created_at, updated_at)
VALUES ($1::uuid, $2::uuid, $3, $4, $4)`, tenantID, accountID, bootstrapRole, now)
		if err != nil {
			return fmt.Errorf("insert membership: %w", err)
		}
		_, err = tx.Exec(ctx,
			`UPDATE iam_accounts SET last_active_tenant_id = $1::uuid, updated_at = $3 WHERE id = $2::uuid`,
			tenantID, accountID, now)
		if err != nil {
			return fmt.Errorf("set last active tenant: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return accountID, tenantID, nil
}

// applyPlatformOperators grants the operator flag to existing accounts whose
// email is listed. It only grants: an account that left the list keeps it.
func (s *service) applyPlatformOperators(ctx context.Context, logger *slog.Logger, emails []string) error {
	if len(emails) == 0 {
		return nil
	}
	tag, err := s.store.pool.Exec(ctx,
		`UPDATE iam_accounts SET is_platform_operator = true, updated_at = $2
 WHERE email = ANY($1) AND NOT is_platform_operator`, emails, s.store.clock.Now())
	if err != nil {
		return fmt.Errorf("grant: %w", err)
	}
	logger.InfoContext(ctx, "platform_operators_applied", slog.Int64("count", tag.RowsAffected()))
	return nil
}
