package identity

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

const (
	statusActive    = "active"
	statusSuspended = "suspended"
)

// Grant is what the session claims need once the account and tenant steps
// pass. Role and TenantSlug are empty when no tenant was checked.
type Grant struct {
	Role             reqctx.Role
	PlatformOperator bool
	TenantSlug       string
}

// access holds the account half of the session checks: the account step and
// identity linking. The tenant half is the core TenantAccess port. It is
// package-internal; sessions are stateless: each call reads the current rows.
type access struct {
	pool           *pgxpool.Pool
	clock          clock.Clock
	operatorEmails []string
}

// newAccess builds the account checks over pool. operatorEmails are the bare,
// lower-cased APP_PLATFORM_OPERATOR_EMAILS entries.
func newAccess(pool *pgxpool.Pool, clk clock.Clock, operatorEmails []string) *access {
	return &access{pool: pool, clock: clk, operatorEmails: slices.Clone(operatorEmails)}
}

func forbidden(code api.Code, message string) *api.Error {
	return api.NewError(http.StatusForbidden, code, message)
}

// accountState is the slice of iam_accounts the account step reads.
type accountState struct {
	active           bool
	platformOperator bool
}

// accountState reads the account; a missing row or an id that is not a UUID
// is an inactive state.
func (a *access) accountState(ctx context.Context, accountID string) (accountState, error) {
	canonical, ok := canonicalID(accountID)
	if !ok {
		return accountState{}, nil
	}
	var status string
	var state accountState
	err := a.pool.QueryRow(ctx,
		"SELECT status, is_platform_operator FROM iam_accounts WHERE id = $1::uuid",
		canonical).Scan(&status, &state.platformOperator)
	if errors.Is(err, pgx.ErrNoRows) {
		return accountState{}, nil
	}
	if err != nil {
		return accountState{}, fmt.Errorf("identity: read account state: %w", err)
	}
	state.active = status == statusActive
	return state, nil
}

// checkAccount is the account step of every session check: a missing,
// non-UUID, locked or disabled account is 403 ACCOUNT_LOCKED_OR_DISABLED, any
// other failure is operational. It runs before any tenant access call.
func (a *access) checkAccount(ctx context.Context, accountID string) (bool, error) {
	state, err := a.accountState(ctx, accountID)
	if err != nil {
		return false, err
	}
	if !state.active {
		return false, forbidden(CodeAccountLockedOrDisabled, "The account is locked or disabled.")
	}
	return state.platformOperator, nil
}

func isAccessDenial(err error) bool {
	var apiErr *api.Error
	isAPIError := errors.As(err, &apiErr)
	return isAPIError && apiErr.Status == http.StatusForbidden
}

// linkIdentity returns the account of a Kratos identity. The first sight
// creates it (email lower-cased, operator flag when the email is listed); a
// later call refreshes the stored email when it changed.
func (a *access) linkIdentity(ctx context.Context, kratosIdentityID, email string) (Account, error) {
	identity, ok := canonicalID(kratosIdentityID)
	if !ok {
		return Account{}, errors.New("identity: link identity: identity id is not a UUID")
	}
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" || len(normalized) > maxEmailLength {
		return Account{}, errors.New("identity: link identity: email is empty or too long")
	}
	isOperator := slices.Contains(a.operatorEmails, normalized)
	now := a.clock.Now()
	row := a.pool.QueryRow(ctx,
		`INSERT INTO iam_accounts (kratos_identity_id, email, is_platform_operator, created_at, updated_at)
VALUES ($1::uuid, $2, $3, $4, $4)
ON CONFLICT (kratos_identity_id) DO UPDATE
SET email = EXCLUDED.email,
    updated_at = CASE WHEN iam_accounts.email IS DISTINCT FROM EXCLUDED.email
                      THEN EXCLUDED.updated_at ELSE iam_accounts.updated_at END
RETURNING `+accountColumns, identity, normalized, isOperator, now)
	account, err := scanAccount(row)
	if err != nil {
		return Account{}, mapError("link identity", err)
	}
	return account, nil
}
