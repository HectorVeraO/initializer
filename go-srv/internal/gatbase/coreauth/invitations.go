package coreauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/db"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/coreauth/kratos"
)

const (
	tokenBytes = 32
	// acceptPath is where the console reads the token from the URL fragment.
	acceptPath = "/invitations/accept#token="

	invitationPending  = "pending"
	invitationAccepted = "accepted"
	invitationRevoked  = "revoked"
	invitationExpired  = "expired"

	statusRevoked = "revoked"

	invitationColumns = "id::text, tenant_id::text, email, role, expires_at, created_at, accepted_at, revoked_at"
)

// InvitationView is the InvitationView schema: never a token, hash or URL.
type InvitationView struct {
	ID         string     `json:"id"`
	Email      string     `json:"email"`
	Role       string     `json:"role"`
	Status     string     `json:"status"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	CreatedAt  time.Time  `json:"createdAt"`
	AcceptedAt *time.Time `json:"acceptedAt"`
	RevokedAt  *time.Time `json:"revokedAt"`
}

// InvitationCreated is the InvitationCreated schema. InvitationURL is shown
// once: only the token hash is stored.
type InvitationCreated struct {
	Invitation    InvitationView `json:"invitation"`
	InvitationURL string         `json:"invitationUrl"`
}

// AcceptNewResult is the AcceptNewResult schema.
type AcceptNewResult struct {
	Membership MembershipView `json:"membership"`
	Account    MemberAccount  `json:"account"`
}

// invitationRow is one row of ten_invitations without the token hash.
type invitationRow struct {
	ID         string
	TenantID   string
	Email      string
	Role       string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	AcceptedAt *time.Time
	RevokedAt  *time.Time
}

func scanInvitation(row rowScanner) (invitationRow, error) {
	var r invitationRow
	err := row.Scan(&r.ID, &r.TenantID, &r.Email, &r.Role, &r.ExpiresAt, &r.CreatedAt, &r.AcceptedAt, &r.RevokedAt)
	return r, err
}

// status derives the invitation status at now.
func (r invitationRow) status(now time.Time) string {
	switch {
	case r.AcceptedAt != nil:
		return invitationAccepted
	case r.RevokedAt != nil:
		return invitationRevoked
	case !r.ExpiresAt.After(now):
		return invitationExpired
	}
	return invitationPending
}

func (r invitationRow) view(now time.Time) InvitationView {
	view := InvitationView{
		ID:        r.ID,
		Email:     r.Email,
		Role:      r.Role,
		Status:    r.status(now),
		ExpiresAt: r.ExpiresAt.UTC(),
		CreatedAt: r.CreatedAt.UTC(),
	}
	if r.AcceptedAt != nil {
		accepted := r.AcceptedAt.UTC()
		view.AcceptedAt = &accepted
	}
	if r.RevokedAt != nil {
		revoked := r.RevokedAt.UTC()
		view.RevokedAt = &revoked
	}
	return view
}

// newToken returns a fresh token: 32 random bytes as 43 base64url characters.
func newToken() (string, error) {
	raw := make([]byte, tokenBytes)
	_, err := rand.Read(raw)
	if err != nil {
		return "", fmt.Errorf("coreauth: random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// invitationURL is the one-time URL; the token sits in the fragment so it
// never reaches a server log or a Referer header.
func (s *tenancy) invitationURL(token string) string {
	base := strings.TrimRight(s.publicBaseURL.String(), "/")
	return base + acceptPath + token
}

func invalidInvitation() *api.Error {
	return api.NewError(http.StatusNotFound, CodeInvitationInvalid, "The invitation is invalid or has expired.")
}

// insertInvitation stores a pending invitation (create steps 3-4 without the
// supersede) and returns its view and the token, which is not stored.
func (s *tenancy) insertInvitation(ctx context.Context, tx pgx.Tx, tenantID, createdBy, email, role string, now time.Time) (InvitationView, string, error) {
	token, err := newToken()
	if err != nil {
		return InvitationView{}, "", err
	}
	row := tx.QueryRow(ctx,
		`INSERT INTO ten_invitations (tenant_id, email, role, token_hash, expires_at, created_by_account_id, created_at)
VALUES ($1::uuid, $2, $3, $4, $5, $6::uuid, $7) RETURNING `+invitationColumns,
		tenantID, email, role, hashToken(token), now.Add(s.invitationTTL), createdBy, now)
	inserted, err := scanInvitation(row)
	if err != nil {
		return InvitationView{}, "", err
	}
	return inserted.view(now), token, nil
}

// CreateInvitation supersedes any pending invitation for (tenant, email) and
// issues a new one. An active member is 409 MEMBERSHIP_ALREADY_ACTIVE. Two
// concurrent creates can both pass the supersede; the one-pending unique
// index rejects the later inserts, and the transaction runs again. Each round
// lets at least one racer win, so a few attempts cover a small burst.
func (s *tenancy) CreateInvitation(ctx context.Context, tenantID, actorID, email string, role reqctx.Role) (InvitationCreated, error) {
	tenant, tenantOK := canonicalID(tenantID)
	actor, actorOK := canonicalID(actorID)
	if !tenantOK || !actorOK || email == "" || !role.Valid() {
		return InvitationCreated{}, errors.New("coreauth: create invitation: tenant id, actor id, email and role are required")
	}
	lowered := strings.ToLower(email)
	if slices.Contains(s.operatorEmails, lowered) {
		err := s.requirePlatformOperator(ctx, actor)
		if err != nil {
			return InvitationCreated{}, err
		}
	}
	const attempts = 4
	var err error
	for range attempts {
		var created InvitationCreated
		created, err = s.createInvitationOnce(ctx, tenant, actor, lowered, string(role))
		if !db.IsUniqueViolation(err) {
			return created, wrapOperational("create invitation", err)
		}
	}
	return InvitationCreated{}, fmt.Errorf("coreauth: create invitation: %w", err)
}

// requirePlatformOperator answers 403 PLATFORM_OPERATOR_REQUIRED unless the
// actor's account is a platform operator. Accept-new grants the operator flag
// to a listed address without proving the address, so only an operator may
// invite one.
func (s *tenancy) requirePlatformOperator(ctx context.Context, actor string) error {
	denied := forbidden(api.CodePlatformOperatorNeeded, "Only a platform operator may invite this address.")
	var isOperator bool
	err := s.pool.QueryRow(ctx, `SELECT is_platform_operator FROM iam_accounts WHERE id = $1::uuid`, actor).Scan(&isOperator)
	if errors.Is(err, pgx.ErrNoRows) {
		return denied
	}
	if err != nil {
		return fmt.Errorf("coreauth: read inviter: %w", err)
	}
	if !isOperator {
		return denied
	}
	return nil
}

func (s *tenancy) createInvitationOnce(ctx context.Context, tenant, actor, email, role string) (InvitationCreated, error) {
	var created InvitationCreated
	err := db.InTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		now := s.clock.Now()
		rows, err := tx.Query(ctx,
			`SELECT m.status FROM ten_memberships m JOIN iam_accounts a ON a.id = m.account_id
WHERE m.tenant_id = $1::uuid AND a.email = $2 FOR UPDATE OF m`, tenant, email)
		if err != nil {
			return err
		}
		statuses, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		if slices.Contains(statuses, statusActive) {
			return conflict(CodeMembershipAlreadyActive, "The account is already an active member.")
		}
		_, err = tx.Exec(ctx,
			`UPDATE ten_invitations SET revoked_at = $3
WHERE tenant_id = $1::uuid AND email = $2 AND accepted_at IS NULL AND revoked_at IS NULL`,
			tenant, email, now)
		if err != nil {
			return err
		}
		invitation, token, err := s.insertInvitation(ctx, tx, tenant, actor, email, role, now)
		if err != nil {
			return err
		}
		created = InvitationCreated{Invitation: invitation, InvitationURL: s.invitationURL(token)}
		return nil
	})
	return created, err
}

// wrapOperational passes an *api.Error through and wraps anything else.
func wrapOperational(op string, err error) error {
	if err == nil {
		return nil
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		return err
	}
	return fmt.Errorf("coreauth: %s: %w", op, err)
}

// RevokeInvitation revokes a pending invitation of the tenant. A foreign,
// unknown or malformed id is 404; a non-pending one is 409 INVITATION_NOT_PENDING.
func (s *tenancy) RevokeInvitation(ctx context.Context, tenantID, invitationID string) (InvitationView, error) {
	tenant, tenantOK := api.ParseUUID(tenantID)
	id, idOK := api.ParseUUID(invitationID)
	if !tenantOK || !idOK {
		return InvitationView{}, api.NotFound()
	}
	var view InvitationView
	err := db.InTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		now := s.clock.Now()
		row := tx.QueryRow(ctx,
			"SELECT "+invitationColumns+" FROM ten_invitations WHERE id = $1::uuid AND tenant_id = $2::uuid FOR UPDATE",
			id, tenant)
		current, err := scanInvitation(row)
		if errors.Is(err, pgx.ErrNoRows) {
			return api.NotFound()
		}
		if err != nil {
			return err
		}
		if current.status(now) != invitationPending {
			return conflict(CodeInvitationNotPending, "The invitation is not pending.")
		}
		_, err = tx.Exec(ctx, "UPDATE ten_invitations SET revoked_at = $2 WHERE id = $1::uuid", id, now)
		if err != nil {
			return err
		}
		current.RevokedAt = &now
		view = current.view(now)
		return nil
	})
	if err != nil {
		return InvitationView{}, wrapOperational("revoke invitation", err)
	}
	return view, nil
}

// lockPendingInvitation finds the invitation of token FOR UPDATE and answers
// 404 INVITATION_INVALID, with one body, for unknown, expired, revoked and
// accepted tokens.
func lockPendingInvitation(ctx context.Context, tx pgx.Tx, token string, now time.Time) (invitationRow, error) {
	row := tx.QueryRow(ctx,
		"SELECT "+invitationColumns+" FROM ten_invitations WHERE token_hash = $1 FOR UPDATE", hashToken(token))
	found, err := scanInvitation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return invitationRow{}, invalidInvitation()
	}
	if err != nil {
		return invitationRow{}, err
	}
	if found.status(now) != invitationPending {
		return invitationRow{}, invalidInvitation()
	}
	return found, nil
}

// markAccepted closes the invitation for accountID.
func markAccepted(ctx context.Context, tx pgx.Tx, invitationID, accountID string, now time.Time) error {
	_, err := tx.Exec(ctx,
		"UPDATE ten_invitations SET accepted_at = $3, accepted_by_account_id = $2::uuid WHERE id = $1::uuid",
		invitationID, accountID, now)
	return err
}

func readTenant(ctx context.Context, tx pgx.Tx, tenantID string) (Tenant, error) {
	row := tx.QueryRow(ctx, "SELECT "+tenantColumns+" FROM ten_tenants WHERE id = $1::uuid", tenantID)
	return scanTenant(row)
}

// AcceptInvitation adds the signed-in account to the invitation's tenant. The
// checks run in the order of the contract; a revoked or suspended membership
// is reactivated with the invited role and keeps its id.
func (s *tenancy) AcceptInvitation(ctx context.Context, accountID, token string) (MembershipView, error) {
	account, isUUID := canonicalID(accountID)
	if !isUUID || token == "" {
		return MembershipView{}, errors.New("coreauth: accept invitation: account id and token are required")
	}
	var view MembershipView
	err := db.InTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		now := s.clock.Now()
		invitation, err := lockPendingInvitation(ctx, tx, token, now)
		if err != nil {
			return err
		}
		var email, accountStatus string
		err = tx.QueryRow(ctx, "SELECT email, status FROM iam_accounts WHERE id = $1::uuid", account).
			Scan(&email, &accountStatus)
		if errors.Is(err, pgx.ErrNoRows) {
			return forbidden(CodeAccountLockedOrDisabled, "The account is locked or disabled.")
		}
		if err != nil {
			return err
		}
		if !strings.EqualFold(email, invitation.Email) {
			return forbidden(CodeInvitationEmailMismatch, "The invitation was sent to another email address.")
		}
		if accountStatus != statusActive {
			return forbidden(CodeAccountLockedOrDisabled, "The account is locked or disabled.")
		}
		tenant, err := readTenant(ctx, tx, invitation.TenantID)
		if err != nil {
			return err
		}
		if tenant.Status != statusActive {
			return forbidden(CodeTenantSuspended, "The tenant is suspended.")
		}
		var existing string
		err = tx.QueryRow(ctx,
			"SELECT status FROM ten_memberships WHERE tenant_id = $1::uuid AND account_id = $2::uuid FOR UPDATE",
			tenant.ID, account).Scan(&existing)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil && existing == statusActive {
			return conflict(CodeMembershipAlreadyActive, "The account is already an active member.")
		}
		var membershipID string
		err = tx.QueryRow(ctx,
			`INSERT INTO ten_memberships (tenant_id, account_id, role, created_at, updated_at)
VALUES ($1::uuid, $2::uuid, $3, $4, $4)
ON CONFLICT (tenant_id, account_id) DO UPDATE SET role = EXCLUDED.role, status = 'active', updated_at = EXCLUDED.updated_at
RETURNING id::text`, tenant.ID, account, invitation.Role, now).Scan(&membershipID)
		if err != nil {
			return err
		}
		err = markAccepted(ctx, tx, invitation.ID, account, now)
		if err != nil {
			return err
		}
		view = newMembershipView(membershipID, tenant, invitation.Role, statusActive)
		return nil
	})
	if err != nil {
		return MembershipView{}, wrapOperational("accept invitation", err)
	}
	return view, nil
}

// AcceptNew creates the Kratos identity and the account of an invited person
// and adds it to the tenant, in one transaction. The Kratos admin calls run
// while the invitation row is locked. No session is issued, and password is
// only passed to Kratos.
func (s *tenancy) AcceptNew(ctx context.Context, token, password, fullName string) (AcceptNewResult, error) {
	name := strings.TrimSpace(fullName)
	if token == "" || password == "" || name == "" {
		return AcceptNewResult{}, errors.New("coreauth: accept new: token, password and full name are required")
	}
	if s.admin == nil {
		return AcceptNewResult{}, errors.New("coreauth: accept new: the Kratos admin client is not configured")
	}
	var result AcceptNewResult
	err := db.InTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		now := s.clock.Now()
		invitation, err := lockPendingInvitation(ctx, tx, token, now)
		if err != nil {
			return err
		}
		tenant, err := readTenant(ctx, tx, invitation.TenantID)
		if err != nil {
			return err
		}
		if tenant.Status != statusActive {
			return forbidden(CodeTenantSuspended, "The tenant is suspended.")
		}
		identity, err := s.createIdentity(ctx, invitation.Email, password)
		if err != nil {
			return err
		}
		// The flag is granted at creation when the invited email is listed in the operator emails.
		isOperator := slices.Contains(s.operatorEmails, invitation.Email)
		var accountID string
		err = tx.QueryRow(ctx,
			`INSERT INTO iam_accounts (kratos_identity_id, email, full_name, is_platform_operator, created_at, updated_at)
VALUES ($1::uuid, $2, $3, $4, $5, $5) RETURNING id::text`,
			identity.ID, invitation.Email, name, isOperator, now).Scan(&accountID)
		if err != nil {
			return err
		}
		var membershipID string
		err = tx.QueryRow(ctx,
			`INSERT INTO ten_memberships (tenant_id, account_id, role, created_at, updated_at)
VALUES ($1::uuid, $2::uuid, $3, $4, $4) RETURNING id::text`,
			tenant.ID, accountID, invitation.Role, now).Scan(&membershipID)
		if err != nil {
			return err
		}
		err = markAccepted(ctx, tx, invitation.ID, accountID, now)
		if err != nil {
			return err
		}
		result = AcceptNewResult{
			Membership: newMembershipView(membershipID, tenant, invitation.Role, statusActive),
			Account:    MemberAccount{ID: accountID, Email: invitation.Email, FullName: &name},
		}
		return nil
	})
	if err != nil {
		return AcceptNewResult{}, wrapOperational("accept new", err)
	}
	return result, nil
}

// createIdentity is steps 4-5 of accept-new: no identity may exist for the
// email, then one is created. An identity that already exists answers 409; a
// Kratos that cannot answer answers 503.
func (s *tenancy) createIdentity(ctx context.Context, email, password string) (kratos.Identity, error) {
	exists := conflict(CodeIdentityAlreadyExists, "An account with this email already exists. Sign in and accept the invitation.")
	_, found, err := s.admin.FindIdentityByEmail(ctx, email)
	if err != nil {
		return kratos.Identity{}, identityError(err)
	}
	if found {
		return kratos.Identity{}, exists
	}
	identity, err := s.admin.CreatePasswordIdentity(ctx, email, password)
	if errors.Is(err, kratos.ErrIdentityExists) {
		return kratos.Identity{}, exists
	}
	if errors.Is(err, kratos.ErrPasswordRejected) {
		var problems api.Problems
		problems.Add("password", CodePasswordRejected, "The password was rejected by the password policy.")
		return kratos.Identity{}, problems.Err()
	}
	if err != nil {
		return kratos.Identity{}, identityError(err)
	}
	return identity, nil
}

// ListInvitations pages the tenant's invitations whose derived status at now
// is status (pending, accepted, revoked or expired), and the total count.
func (s *tenancy) ListInvitations(ctx context.Context, tenantID, status string, q api.PageQuery) ([]InvitationView, int64, error) {
	tenant, tenantOK := api.ParseUUID(tenantID)
	filter, filterOK := statusFilters[status]
	if !tenantOK || !filterOK || q.Page < 1 || q.Size < 1 {
		return nil, 0, errors.New("coreauth: list invitations: tenant id, status and page are required")
	}
	now := s.clock.Now()
	var total int64
	err := s.pool.QueryRow(ctx, "SELECT count(*) FROM ten_invitations WHERE tenant_id = $1::uuid AND "+filter, tenant, now).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("coreauth: count invitations: %w", err)
	}
	direction := "ASC"
	if q.SortDesc {
		direction = "DESC"
	}
	rows, err := s.pool.Query(ctx,
		"SELECT "+invitationColumns+" FROM ten_invitations WHERE tenant_id = $1::uuid AND "+filter+
			" ORDER BY created_at "+direction+", id "+direction+" LIMIT $3 OFFSET $4",
		tenant, now, q.Size, q.Offset())
	if err != nil {
		return nil, 0, fmt.Errorf("coreauth: list invitations: %w", err)
	}
	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (invitationRow, error) {
		return scanInvitation(row)
	})
	if err != nil {
		return nil, 0, fmt.Errorf("coreauth: list invitations: %w", err)
	}
	views := make([]InvitationView, 0, len(found))
	for _, item := range found {
		views = append(views, item.view(now))
	}
	return views, total, nil
}

// statusFilters are the SQL forms of invitationRow.status; $2 is now.
var statusFilters = map[string]string{
	invitationPending:  "accepted_at IS NULL AND revoked_at IS NULL AND expires_at > $2",
	invitationAccepted: "accepted_at IS NOT NULL AND $2::timestamptz IS NOT NULL",
	invitationRevoked:  "revoked_at IS NOT NULL AND $2::timestamptz IS NOT NULL",
	invitationExpired:  "accepted_at IS NULL AND revoked_at IS NULL AND expires_at <= $2",
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
