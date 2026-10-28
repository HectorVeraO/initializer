package identity

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/db"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

// MembershipView is the MembershipView schema (also the session view item).
type MembershipView = membershipView

// MemberAccount is the MemberAccount schema.
type MemberAccount struct {
	ID       string  `json:"id"`
	Email    string  `json:"email"`
	FullName *string `json:"fullName"`
}

// MemberView is the MemberView schema: a membership with its account.
type MemberView struct {
	MembershipView
	Account   MemberAccount `json:"account"`
	CreatedAt time.Time     `json:"createdAt"`
}

func newMembershipView(id string, tenant Tenant, role, status string) MembershipView {
	return MembershipView{
		ID:     id,
		Tenant: tenantSummary{ID: tenant.ID, Slug: tenant.Slug, Name: tenant.Name, Status: tenant.Status},
		Role:   role,
		Status: status,
	}
}

func isMembershipStatus(status string) bool {
	return status == statusActive || status == statusSuspended || status == statusRevoked
}

// transition is the outcome of the transition table for one update.
type transition struct {
	role   string
	status string
	// changed is false for a no-op, which writes nothing.
	changed bool
}

// planTransition applies the transition table to a membership currently in
// (role, status). A revoked membership only accepts a no-op: leaving revoked
// takes a new invitation.
func planTransition(role, status string, newRole *reqctx.Role, newStatus *string) (transition, error) {
	target := transition{role: role, status: status}
	if newRole != nil {
		target.role = string(*newRole)
	}
	if newStatus != nil {
		target.status = *newStatus
	}
	if status == statusRevoked {
		isStillRevoked := target.status == statusRevoked && target.role == role
		if !isStillRevoked {
			return transition{}, conflict(CodeInvitationRequired, "A revoked member needs a new invitation.")
		}
		return transition{role: role, status: status}, nil
	}
	target.changed = target.role != role || target.status != status
	return target, nil
}

// UpdateMembership changes the role and/or status of a membership of the
// tenant. The last-admin rule applies to every caller, self included: the
// rows it counts are locked first, so two concurrent demotions of the last
// two admins cannot both pass. A foreign, unknown or malformed id is 404.
func (s *tenancy) UpdateMembership(ctx context.Context, tenantID, membershipID string, role *reqctx.Role, status *string) (MemberView, error) {
	if role == nil && status == nil {
		return MemberView{}, errors.New("identity: update membership: a role or a status is required")
	}
	if role != nil && !role.Valid() {
		return MemberView{}, errors.New("identity: update membership: invalid role")
	}
	if status != nil && !isMembershipStatus(*status) {
		return MemberView{}, errors.New("identity: update membership: invalid status")
	}
	tenant, tenantOK := api.ParseUUID(tenantID)
	id, idOK := api.ParseUUID(membershipID)
	if !tenantOK || !idOK {
		return MemberView{}, api.NotFound()
	}
	var view MemberView
	err := db.InTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		now := s.clock.Now()
		admins, err := lockActiveAdmins(ctx, tx, tenant)
		if err != nil {
			return err
		}
		var current Membership
		row := tx.QueryRow(ctx,
			"SELECT "+membershipColumns+" FROM ten_memberships WHERE id = $1::uuid AND tenant_id = $2::uuid FOR UPDATE",
			id, tenant)
		current, err = scanMembership(row)
		if errors.Is(err, pgx.ErrNoRows) {
			return api.NotFound()
		}
		if err != nil {
			return err
		}
		plan, err := planTransition(current.Role, current.Status, role, status)
		if err != nil {
			return err
		}
		wasActiveAdmin := current.Role == string(reqctx.RoleAdmin) && current.Status == statusActive
		staysActiveAdmin := plan.role == string(reqctx.RoleAdmin) && plan.status == statusActive
		isLastAdmin := len(admins) == 1 && admins[0] == current.ID
		if wasActiveAdmin && !staysActiveAdmin && isLastAdmin {
			return conflict(CodeLastTenantAdmin, "The tenant must keep at least one active admin.")
		}
		if plan.changed {
			_, err = tx.Exec(ctx,
				"UPDATE ten_memberships SET role = $2, status = $3, updated_at = $4 WHERE id = $1::uuid",
				id, plan.role, plan.status, now)
			if err != nil {
				return err
			}
			current.Role, current.Status, current.UpdatedAt = plan.role, plan.status, now
		}
		view, err = s.readMemberView(ctx, tx, current)
		return err
	})
	if err != nil {
		return MemberView{}, wrapOperational("update membership", err)
	}
	return view, nil
}

// lockActiveAdmins locks the active admin rows of the tenant in id order, so
// concurrent updates take the same locks in the same order, and returns
// their ids.
func lockActiveAdmins(ctx context.Context, tx pgx.Tx, tenantID string) ([]string, error) {
	rows, err := tx.Query(ctx,
		`SELECT id::text FROM ten_memberships
WHERE tenant_id = $1::uuid AND role = 'admin' AND status = 'active' ORDER BY id FOR UPDATE`, tenantID)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	return slices.Clone(ids), nil
}

func (s *tenancy) readMemberView(ctx context.Context, tx pgx.Tx, m Membership) (MemberView, error) {
	tenant, err := readTenant(ctx, tx, m.TenantID)
	if err != nil {
		return MemberView{}, err
	}
	id := reqctx.AccountID(m.AccountID)
	found, err := s.accounts.Get(ctx, []reqctx.AccountID{id})
	if err != nil {
		return MemberView{}, fmt.Errorf("read member account: %w", err)
	}
	holder, exists := found[id]
	if !exists {
		return MemberView{}, fmt.Errorf("read member account: account %s not found", m.AccountID)
	}
	account := MemberAccount{ID: string(holder.ID), Email: holder.Email, FullName: holder.FullName}
	return MemberView{
		MembershipView: newMembershipView(m.ID, tenant, m.Role, m.Status),
		Account:        account,
		CreatedAt:      m.CreatedAt.UTC(),
	}, nil
}

// memberSortColumn maps a wire sort name of the member list to its column.
func memberSortColumn(field string) (string, bool) {
	switch field {
	case "createdAt":
		return "m.created_at", true
	case "email":
		return "a.email", true
	}
	return "", false
}

// ListMembers pages the members of the tenant whose status is one of
// statuses, optionally only those with role (empty means any).
func (s *tenancy) ListMembers(ctx context.Context, tenantID string, statuses []string, role string, q api.PageQuery) ([]MemberView, int64, error) {
	tenantUUID, tenantOK := api.ParseUUID(tenantID)
	column, sortOK := memberSortColumn(q.SortField)
	if !tenantOK || !sortOK || len(statuses) == 0 || q.Page < 1 || q.Size < 1 {
		return nil, 0, errors.New("identity: list members: tenant id, statuses, a known sort field and a page are required")
	}
	const filter = "m.tenant_id = $1::uuid AND m.status = ANY($2) AND ($3 = '' OR m.role = $3)"
	var total int64
	err := s.pool.QueryRow(ctx,
		"SELECT count(*) FROM ten_memberships m WHERE "+filter, tenantUUID, statuses, role).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("identity: count members: %w", err)
	}
	tenantRow := s.pool.QueryRow(ctx, "SELECT "+tenantColumns+" FROM ten_tenants WHERE id = $1::uuid", tenantUUID)
	tenant, err := scanTenant(tenantRow)
	if err != nil {
		return nil, 0, fmt.Errorf("identity: read tenant: %w", err)
	}
	direction := sortDirection(q)
	rows, err := s.pool.Query(ctx,
		`SELECT m.id::text, m.role, m.status, m.created_at, a.id::text, a.email, a.full_name
FROM ten_memberships m JOIN iam_accounts a ON a.id = m.account_id
WHERE `+filter+" ORDER BY "+column+" "+direction+", m.id "+direction+" LIMIT $4 OFFSET $5",
		tenantUUID, statuses, role, q.Size, q.Offset())
	if err != nil {
		return nil, 0, fmt.Errorf("identity: list members: %w", err)
	}
	views, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (MemberView, error) {
		var (
			id, memberRole, memberStatus string
			createdAt                    time.Time
			account                      MemberAccount
		)
		scanErr := row.Scan(&id, &memberRole, &memberStatus, &createdAt, &account.ID, &account.Email, &account.FullName)
		view := MemberView{
			MembershipView: newMembershipView(id, tenant, memberRole, memberStatus),
			Account:        account,
			CreatedAt:      createdAt.UTC(),
		}
		return view, scanErr
	})
	if err != nil {
		return nil, 0, fmt.Errorf("identity: list members: %w", err)
	}
	return views, total, nil
}

var membershipRoles = []string{string(reqctx.RoleAdmin), string(reqctx.RoleMember), string(reqctx.RoleViewer)}

func (rt *tenancyRoutes) listMemberships(w http.ResponseWriter, r *http.Request) {
	principal, ok := callerOf(r)
	tenantID, hasTenant := activeTenantOf(principal)
	if !ok || !hasTenant {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	var p api.Problems
	statuses := []string{statusActive, statusSuspended}
	requested := r.URL.Query().Get("status")
	if requested != "" && api.CheckOneOf(&p, "status", requested, statusActive, statusSuspended, statusRevoked) {
		statuses = []string{requested}
	}
	role := r.URL.Query().Get("role")
	if role != "" {
		api.CheckOneOf(&p, "role", role, membershipRoles...)
	}
	query := api.ParsePageQuery(r, &p, api.PageSpec{Sorts: []string{"createdAt", "email"}, DefaultSort: "email"})
	err := p.Err()
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	items, total, err := rt.tenancy.ListMembers(r.Context(), tenantID, statuses, role, query)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, api.NewPage(items, query, total))
}

func (rt *tenancyRoutes) updateMembership(w http.ResponseWriter, r *http.Request) {
	principal, ok := callerOf(r)
	tenantID, hasTenant := activeTenantOf(principal)
	if !ok || !hasTenant {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	body, err := api.DecodeBody(r)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	var p api.Problems
	var role *reqctx.Role
	var status *string
	roleText, hasRole := body.String(&p, "role", api.Optional)
	if hasRole && api.CheckOneOf(&p, "role", roleText, membershipRoles...) {
		parsed := reqctx.Role(roleText)
		role = &parsed
	}
	statusText, hasStatus := body.String(&p, "status", api.Optional)
	if hasStatus && api.CheckOneOf(&p, "status", statusText, statusActive, statusSuspended, statusRevoked) {
		status = &statusText
	}
	noRole := !body.Has("role") || body.IsNull("role")
	noStatus := !body.Has("status") || body.IsNull("status")
	if noRole && noStatus {
		p.Add("", api.CodeFieldRequired, "At least one of role, status is required")
	}
	err = p.Err()
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	view, err := rt.tenancy.UpdateMembership(r.Context(), tenantID, r.PathValue("id"), role, status)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, view)
}
