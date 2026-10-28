package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/db"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

// Account is one row of iam_accounts.
type Account struct {
	ID                 string
	KratosIdentityID   string
	Email              string
	FullName           *string
	Status             string
	IsPlatformOperator bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

const accountColumns = "id::text, kratos_identity_id::text, email, full_name, status, " +
	"is_platform_operator, created_at, updated_at"

// store is the SQL access of the module. Ids are canonical UUID text; an id
// that is not a UUID reads as not found.
type store struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

func newStore(pool *pgxpool.Pool, clk clock.Clock) *store {
	return &store{pool: pool, clock: clk}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAccount(row rowScanner) (Account, error) {
	var a Account
	err := row.Scan(&a.ID, &a.KratosIdentityID, &a.Email, &a.FullName, &a.Status,
		&a.IsPlatformOperator, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

// mapError turns no-rows and unique violations into the module errors.
func mapError(op string, err error) error {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotFound
	case db.IsUniqueViolation(err):
		return ErrAlreadyExists
	}
	return fmt.Errorf("identity: %s: %w", op, err)
}

func canonicalID(id string) (string, bool) {
	parsed, ok := reqctx.ParseTenantID(id)
	return string(parsed), ok
}

// AccountByID reads an account by id. Accounts are global identities, not
// tenant data; tenant-facing reads go through the membership.
func (s *store) AccountByID(ctx context.Context, id string) (Account, error) {
	canonical, ok := canonicalID(id)
	if !ok {
		return Account{}, ErrNotFound
	}
	row := s.pool.QueryRow(ctx, "SELECT "+accountColumns+" FROM iam_accounts WHERE id = $1::uuid", canonical)
	account, err := scanAccount(row)
	if err != nil {
		return Account{}, mapError("account by id", err)
	}
	return account, nil
}

// AccountByKratosIdentity reads an account by its Kratos identity id.
func (s *store) AccountByKratosIdentity(ctx context.Context, identityID string) (Account, error) {
	canonical, ok := canonicalID(identityID)
	if !ok {
		return Account{}, ErrNotFound
	}
	row := s.pool.QueryRow(ctx,
		"SELECT "+accountColumns+" FROM iam_accounts WHERE kratos_identity_id = $1::uuid", canonical)
	account, err := scanAccount(row)
	if err != nil {
		return Account{}, mapError("account by identity", err)
	}
	return account, nil
}

// UpsertAccount creates the account of a Kratos identity or refreshes its
// lower-cased email and full name (a nil name keeps the stored one).
func (s *store) UpsertAccount(ctx context.Context, identityID, email string, fullName *string) (Account, error) {
	canonical, ok := canonicalID(identityID)
	if !ok {
		return Account{}, fmt.Errorf("identity: upsert account: identity id is not a UUID")
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO iam_accounts (kratos_identity_id, email, full_name, created_at, updated_at)
VALUES ($1::uuid, lower($2), $3, $4, $4)
ON CONFLICT (kratos_identity_id) DO UPDATE
SET email = EXCLUDED.email, full_name = COALESCE(EXCLUDED.full_name, iam_accounts.full_name), updated_at = EXCLUDED.updated_at
RETURNING `+accountColumns,
		canonical, email, fullName, s.clock.Now())
	account, err := scanAccount(row)
	if err != nil {
		return Account{}, mapError("upsert account", err)
	}
	return account, nil
}
