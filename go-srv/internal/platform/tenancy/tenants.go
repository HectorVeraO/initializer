package tenancy

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

	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/accounts"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/db"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

// TenantView is the TenantView schema of the tenancy contract.
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

// tenancy holds the tenant, invitation and membership services behind the routes.
// Every timestamp it writes or compares comes from clock, never SQL now().
type tenancy struct {
	pool          *pgxpool.Pool
	clock         clock.Clock
	publicBaseURL *url.URL
	invitationTTL time.Duration
	// accounts and provisioner are the core account ports, set in Mount
	// (Provide publishes them, so it cannot read them).
	accounts    accounts.Directory
	provisioner accounts.Provisioner
}

func newTenancy(pool *pgxpool.Pool, clk clock.Clock, cfg Config) *tenancy {
	return &tenancy{
		pool:          pool,
		clock:         clk,
		publicBaseURL: cfg.PublicBaseURL,
		invitationTTL: cfg.InvitationTTL,
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
		return TenantCreated{}, errors.New("tenancy: create tenant: operator id, slug, name and admin email are required")
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
		return TenantCreated{}, fmt.Errorf("tenancy: create tenant: %w", err)
	}
	return result, nil
}

func tenantView(t Tenant) TenantView {
	return TenantView{ID: t.ID, Slug: t.Slug, Name: t.Name, Status: t.Status, CreatedAt: t.CreatedAt.UTC()}
}

func (rt *tenancyRoutes) createTenant(w http.ResponseWriter, r *http.Request) {
	principal, ok := callerOf(r)
	if !ok {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	body, err := api.DecodeBody(r)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	var p api.Problems
	slug, slugOK := readSlug(body, &p)
	name, nameOK := readTrimmedName(body, &p, "name", maxTenantNameLength)
	adminEmail, emailOK := readEmail(body, &p, "adminEmail")
	err = p.Err()
	if err != nil || !slugOK || !nameOK || !emailOK {
		rt.writer.Fail(w, r, err)
		return
	}
	created, err := rt.tenancy.CreateTenant(r.Context(), principal.ActorID, slug, name, adminEmail)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusCreated, created)
}

// readSlug validates the slug field: required string matching the slug
// pattern and not UUID-shaped.
func readSlug(body *api.Body, p *api.Problems) (string, bool) {
	value, ok := body.String(p, "slug", api.Required)
	if !ok {
		return "", false
	}
	if value == "" {
		p.Add("slug", api.CodeFieldRequired, "Field is required")
		return "", false
	}
	if !isValidSlug(value) {
		p.Add("slug", api.CodeFieldInvalidFmt, "Must be 3 to 40 characters of a-z, 0-9 and -, not start or end with -, and not a UUID")
		return "", false
	}
	return value, true
}

// TenantSummary reads the tenant row of id for GET /api/v1/tenant. An
// unknown or malformed id is 404.
func (s *tenancy) TenantSummary(ctx context.Context, tenantID string) (tenantSummary, error) {
	id, isUUID := api.ParseUUID(tenantID)
	if !isUUID {
		return tenantSummary{}, api.NotFound()
	}
	row := s.pool.QueryRow(ctx, "SELECT "+tenantColumns+" FROM ten_tenants WHERE id = $1::uuid", id)
	tenant, err := scanTenant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return tenantSummary{}, api.NotFound()
	}
	if err != nil {
		return tenantSummary{}, fmt.Errorf("tenancy: read tenant: %w", err)
	}
	return tenantSummary{ID: tenant.ID, Slug: tenant.Slug, Name: tenant.Name, Status: tenant.Status}, nil
}

// tenantSortColumns maps the wire sort names of the tenant list to columns.
var tenantSortColumns = map[string]string{"createdAt": "created_at", "slug": "slug"}

// ListTenants pages every tenant, optionally only those in status (empty
// means all). It is the operator's cross-tenant view by design.
func (s *tenancy) ListTenants(ctx context.Context, status string, q api.PageQuery) ([]TenantView, int64, error) {
	column, sortOK := tenantSortColumns[q.SortField]
	if !sortOK || q.Page < 1 || q.Size < 1 {
		return nil, 0, errors.New("tenancy: list tenants: a known sort field and a page are required")
	}
	var total int64
	err := s.pool.QueryRow(ctx, "SELECT count(*) FROM ten_tenants WHERE ($1 = '' OR status = $1)", status).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("tenancy: count tenants: %w", err)
	}
	direction := sortDirection(q)
	rows, err := s.pool.Query(ctx,
		"SELECT "+tenantColumns+" FROM ten_tenants WHERE ($1 = '' OR status = $1) ORDER BY "+
			column+" "+direction+", id "+direction+" LIMIT $2 OFFSET $3",
		status, q.Size, q.Offset())
	if err != nil {
		return nil, 0, fmt.Errorf("tenancy: list tenants: %w", err)
	}
	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Tenant, error) {
		return scanTenant(row)
	})
	if err != nil {
		return nil, 0, fmt.Errorf("tenancy: list tenants: %w", err)
	}
	views := make([]TenantView, 0, len(found))
	for _, tenant := range found {
		views = append(views, tenantView(tenant))
	}
	return views, total, nil
}

func sortDirection(q api.PageQuery) string {
	if q.SortDesc {
		return "DESC"
	}
	return "ASC"
}

// SetTenantStatus sets a tenant's status to active or suspended. Setting the
// current status writes nothing. An unknown or malformed id is 404.
func (s *tenancy) SetTenantStatus(ctx context.Context, tenantID, status string) (TenantView, error) {
	if status != statusActive && status != statusSuspended {
		return TenantView{}, errors.New("tenancy: set tenant status: status must be active or suspended")
	}
	id, isUUID := api.ParseUUID(tenantID)
	if !isUUID {
		return TenantView{}, api.NotFound()
	}
	row := s.pool.QueryRow(ctx,
		`UPDATE ten_tenants SET status = $2,
updated_at = CASE WHEN status = $2 THEN updated_at ELSE $3 END
WHERE id = $1::uuid RETURNING `+tenantColumns,
		id, status, s.clock.Now())
	tenant, err := scanTenant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return TenantView{}, api.NotFound()
	}
	if err != nil {
		return TenantView{}, fmt.Errorf("tenancy: set tenant status: %w", err)
	}
	return tenantView(tenant), nil
}

// getTenant serves GET /api/v1/tenant: the caller's own tenant, never another.
func (rt *tenancyRoutes) getTenant(w http.ResponseWriter, r *http.Request) {
	principal, ok := reqctx.PrincipalFrom(r.Context())
	if !ok || principal.TenantID == nil {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	err := coreaccess.CheckTenantRef(principal, r.URL.Query().Get("tenantId"))
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	summary, err := rt.tenancy.TenantSummary(r.Context(), string(*principal.TenantID))
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, summary)
}

func (rt *tenancyRoutes) listTenants(w http.ResponseWriter, r *http.Request) {
	var p api.Problems
	status := r.URL.Query().Get("status")
	if status != "" {
		api.CheckOneOf(&p, "status", status, statusActive, statusSuspended)
	}
	query := api.ParsePageQuery(r, &p, api.PageSpec{Sorts: []string{"createdAt", "slug"}, DefaultSort: "slug"})
	err := p.Err()
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	items, total, err := rt.tenancy.ListTenants(r.Context(), status, query)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, api.NewPage(items, query, total))
}

func (rt *tenancyRoutes) setTenantStatus(w http.ResponseWriter, r *http.Request) {
	body, err := api.DecodeBody(r)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	var p api.Problems
	status, statusOK := body.String(&p, "status", api.Required)
	if statusOK {
		statusOK = api.CheckOneOf(&p, "status", status, statusActive, statusSuspended)
	}
	err = p.Err()
	if err != nil || !statusOK {
		rt.writer.Fail(w, r, err)
		return
	}
	view, err := rt.tenancy.SetTenantStatus(r.Context(), r.PathValue("id"), status)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, view)
}
