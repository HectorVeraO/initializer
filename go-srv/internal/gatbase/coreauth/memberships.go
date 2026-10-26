package coreauth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/db"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
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
		return MemberView{}, errors.New("coreauth: update membership: a role or a status is required")
	}
	if role != nil && !role.Valid() {
		return MemberView{}, errors.New("coreauth: update membership: invalid role")
	}
	if status != nil && !isMembershipStatus(*status) {
		return MemberView{}, errors.New("coreauth: update membership: invalid status")
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
		view, err = readMemberView(ctx, tx, current)
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

func readMemberView(ctx context.Context, tx pgx.Tx, m Membership) (MemberView, error) {
	tenant, err := readTenant(ctx, tx, m.TenantID)
	if err != nil {
		return MemberView{}, err
	}
	var account MemberAccount
	err = tx.QueryRow(ctx, "SELECT id::text, email, full_name FROM iam_accounts WHERE id = $1::uuid", m.AccountID).
		Scan(&account.ID, &account.Email, &account.FullName)
	if err != nil {
		return MemberView{}, fmt.Errorf("read member account: %w", err)
	}
	return MemberView{
		MembershipView: newMembershipView(m.ID, tenant, m.Role, m.Status),
		Account:        account,
		CreatedAt:      m.CreatedAt.UTC(),
	}, nil
}
