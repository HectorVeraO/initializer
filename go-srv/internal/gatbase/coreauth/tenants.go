package coreauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/db"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/coreauth/kratos"
)

// TenantView is the TenantView schema of the core-auth contract.
type TenantView struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// TenantCreated is the TenantCreated schema: the tenant, its first-admin
// invitation and the one-time URL that carries the token.
type TenantCreated struct {
	Tenant        TenantView     `json:"tenant"`
	Invitation    InvitationView `json:"invitation"`
	InvitationURL string         `json:"invitationUrl"`
}

// tenancy holds the tenant, invitation and membership services. It is
// package-internal and reachable from no route yet: the route tracks call it.
// Every timestamp it writes or compares comes from clock, never SQL now().
type tenancy struct {
	pool           *pgxpool.Pool
	clock          clock.Clock
	admin          *kratos.Admin
	publicBaseURL  *url.URL
	invitationTTL  time.Duration
	operatorEmails []string
}

func newTenancy(pool *pgxpool.Pool, clk clock.Clock, admin *kratos.Admin, cfg Config) *tenancy {
	return &tenancy{
		pool:           pool,
		clock:          clk,
		admin:          admin,
		publicBaseURL:  cfg.PublicBaseURL,
		invitationTTL:  cfg.InvitationTTL,
		operatorEmails: append([]string(nil), cfg.PlatformOperatorEmails...),
	}
}

func conflict(code api.Code, message string) *api.Error {
	return api.NewError(http.StatusConflict, code, message)
}

// CreateTenant inserts an active tenant and an admin invitation for
// adminEmail in one transaction. A taken slug is 409 TENANT_SLUG_TAKEN. The
// input is validated by the caller; operatorID is the acting account.
func (s *tenancy) CreateTenant(ctx context.Context, operatorID, slug, name, adminEmail string) (TenantCreated, error) {
	operator, isUUID := canonicalID(operatorID)
	if !isUUID || slug == "" || name == "" || adminEmail == "" {
		return TenantCreated{}, errors.New("coreauth: create tenant: operator id, slug, name and admin email are required")
	}
	email := strings.ToLower(adminEmail)
	var result TenantCreated
	err := db.InTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		now := s.clock.Now()
		row := tx.QueryRow(ctx,
			"INSERT INTO ten_tenants (slug, name, created_at, updated_at) VALUES ($1, $2, $3, $3) RETURNING "+tenantColumns,
			slug, name, now)
		tenant, err := scanTenant(row)
		if err != nil {
			return err
		}
		invitation, token, err := s.insertInvitation(ctx, tx, tenant.ID, operator, email, "admin", now)
		if err != nil {
			return err
		}
		result = TenantCreated{
			Tenant:        tenantView(tenant),
			Invitation:    invitation,
			InvitationURL: s.invitationURL(token),
		}
		return nil
	})
	if db.IsUniqueViolation(err) {
		return TenantCreated{}, conflict(CodeTenantSlugTaken, "The tenant slug is already taken.")
	}
	if err != nil {
		return TenantCreated{}, fmt.Errorf("coreauth: create tenant: %w", err)
	}
	return result, nil
}

func tenantView(t Tenant) TenantView {
	return TenantView{ID: t.ID, Slug: t.Slug, Name: t.Name, Status: t.Status, CreatedAt: t.CreatedAt.UTC()}
}
