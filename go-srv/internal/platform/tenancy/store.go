package tenancy

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

// Tenant is one row of ten_tenants.
type Tenant struct {
	ID        string
	Slug      string
	Name      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Membership is one row of ten_memberships.
type Membership struct {
	ID        string
	TenantID  string
	AccountID string
	Role      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

const (
	tenantColumns     = "id::text, slug, name, status, created_at, updated_at"
	membershipColumns = "id::text, tenant_id::text, account_id::text, role, status, created_at, updated_at"
)

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

func scanTenant(row rowScanner) (Tenant, error) {
	var t Tenant
	err := row.Scan(&t.ID, &t.Slug, &t.Name, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

func scanMembership(row rowScanner) (Membership, error) {
	var m Membership
	err := row.Scan(&m.ID, &m.TenantID, &m.AccountID, &m.Role, &m.Status, &m.CreatedAt, &m.UpdatedAt)
	return m, err
}

// mapError turns no-rows and unique violations into the module errors.
func mapError(op string, err error) error {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotFound
	case db.IsUniqueViolation(err):
		return ErrAlreadyExists
	}
	return fmt.Errorf("tenancy: %s: %w", op, err)
}

func canonicalID(id string) (string, bool) {
	parsed, ok := reqctx.ParseTenantID(id)
	return string(parsed), ok
}

// InsertTenant creates an active tenant; a taken slug is ErrAlreadyExists.
func (s *store) InsertTenant(ctx context.Context, slug, name string) (Tenant, error) {
	row := s.pool.QueryRow(ctx,
		"INSERT INTO ten_tenants (slug, name, created_at, updated_at) VALUES ($1, $2, $3, $3) RETURNING "+tenantColumns,
		slug, name, s.clock.Now())
	tenant, err := scanTenant(row)
	if err != nil {
		return Tenant{}, mapError("insert tenant", err)
	}
	return tenant, nil
}

// TenantByID reads a tenant by id.
func (s *store) TenantByID(ctx context.Context, id string) (Tenant, error) {
	canonical, ok := canonicalID(id)
	if !ok {
		return Tenant{}, ErrNotFound
	}
	row := s.pool.QueryRow(ctx, "SELECT "+tenantColumns+" FROM ten_tenants WHERE id = $1::uuid", canonical)
	tenant, err := scanTenant(row)
	if err != nil {
		return Tenant{}, mapError("tenant by id", err)
	}
	return tenant, nil
}

// TenantBySlug reads a tenant by slug.
func (s *store) TenantBySlug(ctx context.Context, slug string) (Tenant, error) {
	row := s.pool.QueryRow(ctx, "SELECT "+tenantColumns+" FROM ten_tenants WHERE slug = $1", slug)
	tenant, err := scanTenant(row)
	if err != nil {
		return Tenant{}, mapError("tenant by slug", err)
	}
	return tenant, nil
}

// InsertMembership adds an active membership; a second one for the same
// (tenant, account) is ErrAlreadyExists.
func (s *store) InsertMembership(ctx context.Context, tenantID, accountID, role string) (Membership, error) {
	tenant, tenantOK := canonicalID(tenantID)
	account, accountOK := canonicalID(accountID)
	if !tenantOK || !accountOK {
		return Membership{}, fmt.Errorf("tenancy: insert membership: ids must be UUIDs")
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO ten_memberships (tenant_id, account_id, role, created_at, updated_at)
VALUES ($1::uuid, $2::uuid, $3, $4, $4) RETURNING `+membershipColumns,
		tenant, account, role, s.clock.Now())
	membership, err := scanMembership(row)
	if err != nil {
		return Membership{}, mapError("insert membership", err)
	}
	return membership, nil
}

// MembershipByTenantAccount reads the membership of account in tenant.
func (s *store) MembershipByTenantAccount(ctx context.Context, tenantID, accountID string) (Membership, error) {
	tenant, tenantOK := canonicalID(tenantID)
	account, accountOK := canonicalID(accountID)
	if !tenantOK || !accountOK {
		return Membership{}, ErrNotFound
	}
	row := s.pool.QueryRow(ctx,
		"SELECT "+membershipColumns+" FROM ten_memberships WHERE tenant_id = $1::uuid AND account_id = $2::uuid",
		tenant, account)
	membership, err := scanMembership(row)
	if err != nil {
		return Membership{}, mapError("membership by tenant and account", err)
	}
	return membership, nil
}

// MembershipsByAccount lists every membership of an account, oldest first.
// An id that is not a UUID answers ErrNotFound.
func (s *store) MembershipsByAccount(ctx context.Context, accountID string) ([]Membership, error) {
	account, ok := canonicalID(accountID)
	if !ok {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx,
		"SELECT "+membershipColumns+" FROM ten_memberships WHERE account_id = $1::uuid ORDER BY created_at, id",
		account)
	if err != nil {
		return nil, mapError("memberships by account", err)
	}
	defer rows.Close()
	var memberships []Membership
	for rows.Next() {
		membership, err := scanMembership(rows)
		if err != nil {
			return nil, mapError("scan membership", err)
		}
		memberships = append(memberships, membership)
	}
	err = rows.Err()
	if err != nil {
		return nil, mapError("memberships by account", err)
	}
	return memberships, nil
}
