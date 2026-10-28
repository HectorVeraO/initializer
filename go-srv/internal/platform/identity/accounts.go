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

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/accounts"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/db"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity/kratos"
)

const accountRefColumns = "id::text, email, full_name, status, is_platform_operator"

// accountDirectory is core's accounts.Directory and accounts.Provisioner over
// iam_accounts and the Kratos admin client.
type accountDirectory struct {
	pool  *pgxpool.Pool
	clock clock.Clock
	// admin is nil only when APP_KRATOS_ADMIN_URL is invalid, which fails Build.
	admin *kratos.Admin
	// operatorEmails are the bare, lower-cased APP_PLATFORM_OPERATOR_EMAILS entries.
	operatorEmails []string
}

var (
	_ accounts.Directory   = (*accountDirectory)(nil)
	_ accounts.Provisioner = (*accountDirectory)(nil)
)

func newAccountDirectory(pool *pgxpool.Pool, clk clock.Clock, admin *kratos.Admin, operatorEmails []string) *accountDirectory {
	return &accountDirectory{pool: pool, clock: clk, admin: admin, operatorEmails: slices.Clone(operatorEmails)}
}

func scanAccountRef(row rowScanner) (accounts.Ref, error) {
	var ref accounts.Ref
	var status string
	err := row.Scan(&ref.ID, &ref.Email, &ref.FullName, &status, &ref.PlatformOperator)
	ref.Status = accounts.Status(status)
	return ref, err
}

// Get returns the accounts of ids; an id with no account or that is not a UUID
// is absent from the map.
func (d *accountDirectory) Get(ctx context.Context, ids []reqctx.AccountID) (map[reqctx.AccountID]accounts.Ref, error) {
	found := make(map[reqctx.AccountID]accounts.Ref, len(ids))
	canonical := make([]string, 0, len(ids))
	for _, id := range ids {
		text, isUUID := canonicalID(string(id))
		if isUUID {
			canonical = append(canonical, text)
		}
	}
	if len(canonical) == 0 {
		return found, nil
	}
	rows, err := d.pool.Query(ctx, "SELECT "+accountRefColumns+" FROM iam_accounts WHERE id = ANY($1::uuid[])", canonical)
	if err != nil {
		return nil, fmt.Errorf("identity: get accounts: %w", err)
	}
	refs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (accounts.Ref, error) {
		return scanAccountRef(row)
	})
	if err != nil {
		return nil, fmt.Errorf("identity: get accounts: %w", err)
	}
	for _, ref := range refs {
		found[ref.ID] = ref
	}
	return found, nil
}

// ByEmail returns every account with the lower-cased, trimmed email, oldest first.
func (d *accountDirectory) ByEmail(ctx context.Context, email string) ([]accounts.Ref, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	rows, err := d.pool.Query(ctx,
		"SELECT "+accountRefColumns+" FROM iam_accounts WHERE email = $1 ORDER BY created_at, id", normalized)
	if err != nil {
		return nil, fmt.Errorf("identity: accounts by email: %w", err)
	}
	refs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (accounts.Ref, error) {
		return scanAccountRef(row)
	})
	if err != nil {
		return nil, fmt.Errorf("identity: accounts by email: %w", err)
	}
	if refs == nil {
		refs = []accounts.Ref{}
	}
	return refs, nil
}

// GrantsOperator reports whether email is on the operator list.
func (d *accountDirectory) GrantsOperator(email string) bool {
	return slices.Contains(d.operatorEmails, strings.ToLower(email))
}

// Provision creates the Kratos identity, then the account, which is committed
// when it returns; an INSERT failure deletes the identity. The password reaches only Kratos.
func (d *accountDirectory) Provision(ctx context.Context, a accounts.NewAccount) (accounts.Ref, error) {
	name := strings.TrimSpace(a.FullName)
	if a.Email == "" || a.Password == "" || name == "" {
		return accounts.Ref{}, errors.New("identity: provision: email, password and full name are required")
	}
	if d.admin == nil {
		return accounts.Ref{}, errors.New("identity: provision: the Kratos admin client is not configured")
	}
	exists := conflict(accounts.CodeIdentityAlreadyExists, "An account with this email already exists. Sign in and accept the invitation.")
	_, found, err := d.admin.FindIdentityByEmail(ctx, a.Email)
	if err != nil {
		return accounts.Ref{}, identityError(err)
	}
	if found {
		return accounts.Ref{}, exists
	}
	identity, err := d.admin.CreatePasswordIdentity(ctx, a.Email, a.Password)
	if errors.Is(err, kratos.ErrIdentityExists) {
		return accounts.Ref{}, exists
	}
	if errors.Is(err, kratos.ErrPasswordRejected) {
		var problems api.Problems
		problems.Add("password", accounts.CodePasswordRejected, "The password was rejected by the password policy.")
		return accounts.Ref{}, problems.Err()
	}
	if err != nil {
		return accounts.Ref{}, identityError(err)
	}
	now := d.clock.Now()
	row := d.pool.QueryRow(ctx,
		`INSERT INTO iam_accounts (kratos_identity_id, email, full_name, is_platform_operator, created_at, updated_at)
VALUES ($1::uuid, $2, $3, $4, $5, $5) RETURNING `+accountRefColumns,
		identity.ID, a.Email, name, d.GrantsOperator(a.Email), now)
	ref, err := scanAccountRef(row)
	if err != nil {
		// The identity has no account: remove it so a retry can succeed.
		cleanupErr := d.admin.DeleteIdentity(context.WithoutCancel(ctx), identity.ID)
		if db.IsUniqueViolation(err) && cleanupErr == nil {
			return accounts.Ref{}, exists
		}
		return accounts.Ref{}, fmt.Errorf("identity: provision account: %w", errors.Join(err, cleanupErr))
	}
	return ref, nil
}

func conflict(code api.Code, message string) *api.Error {
	return api.NewError(http.StatusConflict, code, message)
}

// identityError maps an unreachable Kratos to 503. The cause is kept for the
// log line and carries no password or email.
func identityError(err error) error {
	if errors.Is(err, kratos.ErrUnavailable) {
		return api.Wrap(err, http.StatusServiceUnavailable, CodeIdentityProviderUnavailable,
			"Sign-in is unavailable right now.")
	}
	return err
}
