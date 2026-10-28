package identity

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/accounts"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity/kratos"
)

const (
	tenPublicBase = "http://localhost:10120"
	tenTTL        = 168 * time.Hour
	janeEmail     = "jane.doe@example.com"
)

var (
	tenStart     = time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	urlPattern   = regexp.MustCompile(`^http://localhost:10120/invitations/accept#token=([A-Za-z0-9_-]{43})$`)
	tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

// tenEnv is the tenancy service over a fresh database, a fake Kratos admin
// API and a clock the test moves.
type tenEnv struct {
	t      *testing.T
	pool   *pgxpool.Pool
	store  *store
	clock  *settableClock
	kratos *fakeKratos
	svc    *tenancy
	access *access
}

func newTenEnv(t *testing.T, operatorEmails ...string) *tenEnv {
	t.Helper()
	pool := newTestPool(t)
	clk := &settableClock{now: tenStart}
	fake := newFakeKratos(t)
	adminURL, err := url.Parse(fake.server.URL)
	if err != nil {
		t.Fatalf("parse fake url: %v", err)
	}
	base, err := url.Parse(tenPublicBase)
	if err != nil {
		t.Fatalf("parse base url: %v", err)
	}
	cfg := Config{PublicBaseURL: base, InvitationTTL: tenTTL, PlatformOperatorEmails: operatorEmails}
	svc := newTenancy(pool, clk, cfg)
	port := newAccountDirectory(pool, clk, kratos.NewAdmin(adminURL), operatorEmails)
	svc.accounts = port
	svc.provisioner = port
	return &tenEnv{
		t:      t,
		pool:   pool,
		store:  newStore(pool, clk),
		clock:  clk,
		kratos: fake,
		svc:    svc,
		access: newAccess(pool, clk, operatorEmails),
	}
}

func (e *tenEnv) exec(sql string, args ...any) {
	e.t.Helper()
	_, err := e.pool.Exec(context.Background(), sql, args...)
	if err != nil {
		e.t.Fatalf("exec %q: %v", sql, err)
	}
}

func (e *tenEnv) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n)
	if err != nil {
		e.t.Fatalf("count %q: %v", sql, err)
	}
	return n
}

func (e *tenEnv) tenant(slug string) Tenant {
	e.t.Helper()
	return mustTenant(e.t, e.store, slug)
}

func (e *tenEnv) account(email string) Account {
	e.t.Helper()
	return mustAccount(e.t, e.store, email)
}

func (e *tenEnv) member(tenantID, accountID, role, status string) Membership {
	e.t.Helper()
	m, err := e.store.InsertMembership(context.Background(), tenantID, accountID, role)
	if err != nil {
		e.t.Fatalf("insert membership: %v", err)
	}
	if status != statusActive {
		e.exec("UPDATE ten_memberships SET status = $2 WHERE id = $1::uuid", m.ID, status)
		m.Status = status
	}
	return m
}

// invite creates an invitation through the service and returns its token.
func (e *tenEnv) invite(tenantID, actorID, email string, role reqctx.Role) (InvitationCreated, string) {
	e.t.Helper()
	created, err := e.svc.CreateInvitation(context.Background(), tenantID, actorID, email, role)
	if err != nil {
		e.t.Fatalf("create invitation: %v", err)
	}
	return created, tokenOf(e.t, created.InvitationURL)
}

func tokenOf(t *testing.T, invitationURL string) string {
	t.Helper()
	match := urlPattern.FindStringSubmatch(invitationURL)
	if match == nil {
		t.Fatalf("invitation url has the wrong shape: %q", invitationURL)
	}
	return match[1]
}

func requireAPIError(t *testing.T, err error, status int, code api.Code) *api.Error {
	t.Helper()
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("want api error %d %s, got %v", status, code, err)
	}
	if apiErr.Status != status || apiErr.Code != code {
		t.Fatalf("want %d %s, got %d %s (%s)", status, code, apiErr.Status, apiErr.Code, apiErr.Message)
	}
	return apiErr
}

func (e *tenEnv) invitationStatus(id string) string {
	e.t.Helper()
	var accepted, revoked *time.Time
	var expires time.Time
	err := e.pool.QueryRow(context.Background(),
		"SELECT accepted_at, revoked_at, expires_at FROM ten_invitations WHERE id = $1::uuid", id).
		Scan(&accepted, &revoked, &expires)
	if err != nil {
		e.t.Fatalf("read invitation: %v", err)
	}
	row := invitationRow{ExpiresAt: expires, AcceptedAt: accepted, RevokedAt: revoked}
	return row.status(e.clock.Now())
}

func (e *tenEnv) membershipRow(tenantID, accountID string) Membership {
	e.t.Helper()
	m, err := e.store.MembershipByTenantAccount(context.Background(), tenantID, accountID)
	if err != nil {
		e.t.Fatalf("membership row: %v", err)
	}
	return m
}

func TestCreateTenantReturnsTenantAndAdminInvitation(t *testing.T) {
	e := newTenEnv(t)
	operator := e.account("operator@example.com")

	got, err := e.svc.CreateTenant(context.Background(), operator.ID, "tenant-a", "Tenant A", "Jane.Doe@example.com")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	if got.Tenant.Slug != "tenant-a" || got.Tenant.Name != "Tenant A" || got.Tenant.Status != "active" {
		t.Fatalf("tenant view: %+v", got.Tenant)
	}
	if got.Invitation.Email != janeEmail || got.Invitation.Role != "admin" || got.Invitation.Status != "pending" {
		t.Fatalf("invitation view: %+v", got.Invitation)
	}
	wantExpiry := tenStart.Add(tenTTL)
	if !got.Invitation.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("expiresAt = %v, want %v", got.Invitation.ExpiresAt, wantExpiry)
	}
	tokenOf(t, got.InvitationURL)
	createdBy := e.count("SELECT count(*) FROM ten_invitations WHERE tenant_id = $1::uuid AND created_by_account_id = $2::uuid",
		got.Tenant.ID, operator.ID)
	if createdBy != 1 {
		t.Fatalf("invitations created by the operator = %d, want 1", createdBy)
	}
}

func TestCreateTenantDuplicateSlugIsConflictAndWritesNothing(t *testing.T) {
	e := newTenEnv(t)
	operator := e.account("operator@example.com")
	_, err := e.svc.CreateTenant(context.Background(), operator.ID, "tenant-a", "Tenant A", janeEmail)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err = e.svc.CreateTenant(context.Background(), operator.ID, "tenant-a", "Other", "other@example.com")

	requireAPIError(t, err, 409, CodeTenantSlugTaken)
	if n := e.count("SELECT count(*) FROM ten_tenants"); n != 1 {
		t.Fatalf("tenants = %d, want 1", n)
	}
	if n := e.count("SELECT count(*) FROM ten_invitations"); n != 1 {
		t.Fatalf("invitations = %d, want 1", n)
	}
}

func TestOnlyTheTokenHashIsStored(t *testing.T) {
	e := newTenEnv(t)
	operator := e.account("operator@example.com")
	created, err := e.svc.CreateTenant(context.Background(), operator.ID, "tenant-a", "Tenant A", janeEmail)
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	tokens := []string{tokenOf(t, created.InvitationURL)}
	_, second := e.invite(created.Tenant.ID, operator.ID, "joe.roe@example.com", reqctx.RoleMember)
	tokens = append(tokens, second)

	rows, err := e.pool.Query(context.Background(), `
SELECT 'ten_tenants', kv.value FROM ten_tenants t, jsonb_each_text(to_jsonb(t)) kv
UNION ALL SELECT 'iam_accounts', kv.value FROM iam_accounts t, jsonb_each_text(to_jsonb(t)) kv
UNION ALL SELECT 'ten_memberships', kv.value FROM ten_memberships t, jsonb_each_text(to_jsonb(t)) kv
UNION ALL SELECT 'ten_invitations', kv.value FROM ten_invitations t, jsonb_each_text(to_jsonb(t)) kv
  WHERE kv.key <> 'token_hash'
UNION ALL SELECT 'iam_api_keys', kv.value FROM iam_api_keys t, jsonb_each_text(to_jsonb(t)) kv
  WHERE kv.key NOT IN ('secret_hash')`)
	if err != nil {
		t.Fatalf("scan columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var table string
		var value *string
		err = rows.Scan(&table, &value)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if value != nil && tokenPattern.MatchString(*value) {
			t.Fatalf("%s holds a 43-character base64url value: %q", table, *value)
		}
	}
	if rows.Err() != nil {
		t.Fatalf("rows: %v", rows.Err())
	}
	for _, token := range tokens {
		var matches int
		err = e.pool.QueryRow(context.Background(),
			"SELECT count(*) FROM ten_invitations WHERE token_hash = sha256(convert_to($1, 'UTF8'))", token).Scan(&matches)
		if err != nil || matches != 1 {
			t.Fatalf("token hash is not sha256(token): matches=%d err=%v", matches, err)
		}
	}
}

func TestInvitationURLIsFragmentOnly(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")

	created, _ := e.invite(tenant.ID, admin.ID, janeEmail, reqctx.RoleViewer)

	if strings.Contains(created.InvitationURL, "?") {
		t.Fatalf("invitation url has a query: %q", created.InvitationURL)
	}
	if !strings.HasPrefix(created.InvitationURL, tenPublicBase+"/invitations/accept#token=") {
		t.Fatalf("invitation url prefix: %q", created.InvitationURL)
	}
}

func TestCreateInvitationSupersedesThePendingOne(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")
	first, firstToken := e.invite(tenant.ID, admin.ID, janeEmail, reqctx.RoleMember)

	second, secondToken := e.invite(tenant.ID, admin.ID, "JANE.DOE@example.com", reqctx.RoleAdmin)

	if got := e.invitationStatus(first.Invitation.ID); got != "revoked" {
		t.Fatalf("first invitation status = %s, want revoked", got)
	}
	pending := e.count("SELECT count(*) FROM ten_invitations WHERE tenant_id = $1::uuid AND email = $2 AND accepted_at IS NULL AND revoked_at IS NULL",
		tenant.ID, janeEmail)
	if pending != 1 || second.Invitation.Status != "pending" {
		t.Fatalf("pending rows = %d, second status = %s", pending, second.Invitation.Status)
	}
	jane := e.account(janeEmail)
	_, err := e.svc.AcceptInvitation(context.Background(), jane.ID, firstToken)
	requireAPIError(t, err, 404, CodeInvitationInvalid)
	view, err := e.svc.AcceptInvitation(context.Background(), jane.ID, secondToken)
	if err != nil || view.Role != "admin" {
		t.Fatalf("accept second: role=%s err=%v", view.Role, err)
	}
}

func TestConcurrentCreatesLeaveOnePendingInvitation(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")
	for round := range 8 {
		email := "person" + string(rune('a'+round)) + "@example.com"
		var wg sync.WaitGroup
		errs := make([]error, 3)
		for i := range errs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = e.svc.CreateInvitation(context.Background(), tenant.ID, admin.ID, email, reqctx.RoleMember)
			}()
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("round %d: concurrent create: %v", round, err)
			}
		}
		pending := e.count("SELECT count(*) FROM ten_invitations WHERE email = $1 AND accepted_at IS NULL AND revoked_at IS NULL", email)
		if pending != 1 {
			t.Fatalf("round %d: pending rows = %d, want 1", round, pending)
		}
		total := e.count("SELECT count(*) FROM ten_invitations WHERE email = $1", email)
		if total != 3 {
			t.Fatalf("round %d: rows = %d, want 3 (two superseded)", round, total)
		}
	}
}

func TestOnlyAPlatformOperatorMayInviteAnOperatorAddress(t *testing.T) {
	const operatorEmail = "ops@example.com"
	e := newTenEnv(t, operatorEmail)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")

	_, err := e.svc.CreateInvitation(context.Background(), tenant.ID, admin.ID, "OPS@example.com", reqctx.RoleMember)

	requireAPIError(t, err, 403, api.CodePlatformOperatorNeeded)
	if n := e.count("SELECT count(*) FROM ten_invitations"); n != 0 {
		t.Fatalf("invitations = %d, want 0", n)
	}
	e.exec("UPDATE iam_accounts SET is_platform_operator = true WHERE id = $1::uuid", admin.ID)
	created, err := e.svc.CreateInvitation(context.Background(), tenant.ID, admin.ID, operatorEmail, reqctx.RoleMember)
	if err != nil || created.Invitation.Status != "pending" {
		t.Fatalf("operator invite: status=%s err=%v", created.Invitation.Status, err)
	}
}

func TestCreateInvitationForActiveMemberIsConflict(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")
	jane := e.account(janeEmail)
	e.member(tenant.ID, jane.ID, "member", "active")

	_, err := e.svc.CreateInvitation(context.Background(), tenant.ID, admin.ID, janeEmail, reqctx.RoleAdmin)

	requireAPIError(t, err, 409, CodeMembershipAlreadyActive)
	if n := e.count("SELECT count(*) FROM ten_invitations"); n != 0 {
		t.Fatalf("invitations = %d, want 0", n)
	}
}

func TestCreateInvitationForSuspendedOrRevokedMemberIsAllowed(t *testing.T) {
	for _, status := range []string{"suspended", "revoked"} {
		t.Run(status, func(t *testing.T) {
			e := newTenEnv(t)
			tenant := e.tenant("tenant-a")
			admin := e.account("admin@example.com")
			jane := e.account(janeEmail)
			e.member(tenant.ID, jane.ID, "member", status)

			created, _ := e.invite(tenant.ID, admin.ID, janeEmail, reqctx.RoleMember)

			if created.Invitation.Status != "pending" {
				t.Fatalf("status = %s", created.Invitation.Status)
			}
		})
	}
}

func TestInvitationExpiryFollowsTheInjectedClock(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")
	jane := e.account(janeEmail)
	joe := e.account("joe.roe@example.com")
	_, janeToken := e.invite(tenant.ID, admin.ID, janeEmail, reqctx.RoleMember)
	_, joeToken := e.invite(tenant.ID, admin.ID, "joe.roe@example.com", reqctx.RoleMember)

	e.clock.advance(tenTTL - time.Second)
	_, err := e.svc.AcceptInvitation(context.Background(), jane.ID, janeToken)
	if err != nil {
		t.Fatalf("accept one second before expiry: %v", err)
	}
	e.clock.advance(time.Second)
	_, err = e.svc.AcceptInvitation(context.Background(), joe.ID, joeToken)

	requireAPIError(t, err, 404, CodeInvitationInvalid)
}

func TestRevokeInvitationForeignUnknownOrMalformedIsNotFound(t *testing.T) {
	e := newTenEnv(t)
	tenantA := e.tenant("tenant-a")
	tenantB := e.tenant("tenant-b")
	admin := e.account("admin@example.com")
	created, _ := e.invite(tenantA.ID, admin.ID, janeEmail, reqctx.RoleMember)
	ctx := context.Background()

	_, foreign := e.svc.RevokeInvitation(ctx, tenantB.ID, created.Invitation.ID)
	_, unknown := e.svc.RevokeInvitation(ctx, tenantA.ID, randomUUID(t))
	_, malformed := e.svc.RevokeInvitation(ctx, tenantA.ID, "not-a-uuid")

	requireAPIError(t, foreign, 404, api.CodeResourceNotFound)
	requireAPIError(t, unknown, 404, api.CodeResourceNotFound)
	requireAPIError(t, malformed, 404, api.CodeResourceNotFound)
	if got := e.invitationStatus(created.Invitation.ID); got != "pending" {
		t.Fatalf("failed revokes changed the status to %s", got)
	}
}

func TestRevokeInvitationMarksItRevokedAndBlocksAccept(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")
	jane := e.account(janeEmail)
	created, token := e.invite(tenant.ID, admin.ID, janeEmail, reqctx.RoleMember)
	ctx := context.Background()

	view, err := e.svc.RevokeInvitation(ctx, tenant.ID, created.Invitation.ID)

	if err != nil || view.Status != "revoked" || view.RevokedAt == nil || !view.RevokedAt.Equal(tenStart) {
		t.Fatalf("revoke: %+v err=%v", view, err)
	}
	_, err = e.svc.AcceptInvitation(ctx, jane.ID, token)
	requireAPIError(t, err, 404, CodeInvitationInvalid)
}

func TestRevokeInvitationTwiceIsNotPending(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")
	created, _ := e.invite(tenant.ID, admin.ID, janeEmail, reqctx.RoleMember)
	_, err := e.svc.RevokeInvitation(context.Background(), tenant.ID, created.Invitation.ID)
	if err != nil {
		t.Fatalf("first revoke: %v", err)
	}

	_, again := e.svc.RevokeInvitation(context.Background(), tenant.ID, created.Invitation.ID)

	requireAPIError(t, again, 409, CodeInvitationNotPending)
}

func TestRevokeExpiredInvitationIsNotPending(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")
	created, _ := e.invite(tenant.ID, admin.ID, janeEmail, reqctx.RoleMember)
	e.clock.advance(tenTTL)

	_, err := e.svc.RevokeInvitation(context.Background(), tenant.ID, created.Invitation.ID)

	requireAPIError(t, err, 409, CodeInvitationNotPending)
}

func TestAcceptInvitationAddsTheMemberAndClosesTheInvitation(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")
	jane := e.account(janeEmail)
	created, token := e.invite(tenant.ID, admin.ID, "Jane.Doe@example.com", reqctx.RoleViewer)

	view, err := e.svc.AcceptInvitation(context.Background(), jane.ID, token)

	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if view.Role != "viewer" || view.Status != "active" || view.Tenant.Slug != "tenant-a" {
		t.Fatalf("membership view: %+v", view)
	}
	if got := e.invitationStatus(created.Invitation.ID); got != "accepted" {
		t.Fatalf("invitation status = %s", got)
	}
	var acceptedBy string
	err = e.pool.QueryRow(context.Background(), "SELECT accepted_by_account_id::text FROM ten_invitations WHERE id = $1::uuid",
		created.Invitation.ID).Scan(&acceptedBy)
	if err != nil || acceptedBy != jane.ID {
		t.Fatalf("accepted_by = %q err=%v", acceptedBy, err)
	}
	_, err = e.svc.AcceptInvitation(context.Background(), jane.ID, token)
	requireAPIError(t, err, 404, CodeInvitationInvalid)
}

func TestAcceptInvitationFailures(t *testing.T) {
	type setup func(e *tenEnv, tenant Tenant, jane Account)
	cases := []struct {
		name   string
		email  string
		setup  setup
		status int
		code   api.Code
	}{
		{"email mismatch", "other@example.com", nil, 403, CodeInvitationEmailMismatch},
		{"locked account", janeEmail, func(e *tenEnv, _ Tenant, jane Account) {
			e.exec("UPDATE iam_accounts SET status = 'locked' WHERE id = $1::uuid", jane.ID)
		}, 403, CodeAccountLockedOrDisabled},
		{"disabled account", janeEmail, func(e *tenEnv, _ Tenant, jane Account) {
			e.exec("UPDATE iam_accounts SET status = 'disabled' WHERE id = $1::uuid", jane.ID)
		}, 403, CodeAccountLockedOrDisabled},
		{"suspended tenant", janeEmail, func(e *tenEnv, tenant Tenant, _ Account) {
			e.exec("UPDATE ten_tenants SET status = 'suspended' WHERE id = $1::uuid", tenant.ID)
		}, 403, api.CodeTenantSuspended},
		{"already active", janeEmail, func(e *tenEnv, tenant Tenant, jane Account) {
			e.member(tenant.ID, jane.ID, "member", "active")
		}, 409, CodeMembershipAlreadyActive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTenEnv(t)
			tenant := e.tenant("tenant-a")
			admin := e.account("admin@example.com")
			jane := e.account(janeEmail)
			created, token := e.invite(tenant.ID, admin.ID, tc.email, reqctx.RoleMember)
			if tc.setup != nil {
				tc.setup(e, tenant, jane)
			}

			_, err := e.svc.AcceptInvitation(context.Background(), jane.ID, token)

			requireAPIError(t, err, tc.status, tc.code)
			if got := e.invitationStatus(created.Invitation.ID); got != "pending" {
				t.Fatalf("invitation status after failure = %s, want pending", got)
			}
		})
	}
}

func TestAcceptInvitationRejectsAnUnknownAccount(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")
	created, token := e.invite(tenant.ID, admin.ID, janeEmail, reqctx.RoleMember)

	_, err := e.svc.AcceptInvitation(context.Background(), "00000000-0000-4000-8000-000000000001", token)

	requireAPIError(t, err, 403, CodeAccountLockedOrDisabled)
	if got := e.invitationStatus(created.Invitation.ID); got != "pending" {
		t.Fatalf("invitation status after failure = %s, want pending", got)
	}
}

func TestAcceptInvitationInvalidTokensShareOneBody(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")
	jane := e.account(janeEmail)
	ctx := context.Background()
	revoked, revokedToken := e.invite(tenant.ID, admin.ID, "r@example.com", reqctx.RoleMember)
	_, err := e.svc.RevokeInvitation(ctx, tenant.ID, revoked.Invitation.ID)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	_, acceptedToken := e.invite(tenant.ID, admin.ID, janeEmail, reqctx.RoleMember)
	_, err = e.svc.AcceptInvitation(ctx, jane.ID, acceptedToken)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	_, expiredToken := e.invite(tenant.ID, admin.ID, "x@example.com", reqctx.RoleMember)
	e.clock.advance(tenTTL)
	unknownToken, err := newToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	var bodies []string
	for _, token := range []string{unknownToken, revokedToken, acceptedToken, expiredToken} {
		_, err = e.svc.AcceptInvitation(ctx, jane.ID, token)
		apiErr := requireAPIError(t, err, 404, CodeInvitationInvalid)
		bodies = append(bodies, apiErr.Message)
	}

	for _, body := range bodies[1:] {
		if body != bodies[0] {
			t.Fatalf("bodies differ: %q vs %q", body, bodies[0])
		}
	}
}

func TestAcceptInvitationReactivatesTheSameMembership(t *testing.T) {
	for _, status := range []string{"revoked", "suspended"} {
		t.Run(status, func(t *testing.T) {
			e := newTenEnv(t)
			tenant := e.tenant("tenant-a")
			admin := e.account("admin@example.com")
			jane := e.account(janeEmail)
			old := e.member(tenant.ID, jane.ID, "viewer", status)
			_, token := e.invite(tenant.ID, admin.ID, janeEmail, reqctx.RoleAdmin)
			e.clock.advance(time.Hour)

			view, err := e.svc.AcceptInvitation(context.Background(), jane.ID, token)

			if err != nil {
				t.Fatalf("accept: %v", err)
			}
			if view.ID != old.ID || view.Role != "admin" || view.Status != "active" {
				t.Fatalf("view = %+v, want id %s admin active", view, old.ID)
			}
			row := e.membershipRow(tenant.ID, jane.ID)
			if row.ID != old.ID || row.Role != "admin" || row.Status != "active" || !row.UpdatedAt.Equal(e.clock.Now()) {
				t.Fatalf("row = %+v", row)
			}
			if n := e.count("SELECT count(*) FROM ten_memberships WHERE tenant_id = $1::uuid", tenant.ID); n != 1 {
				t.Fatalf("memberships = %d, want 1", n)
			}
		})
	}
}

func TestAcceptNewCreatesIdentityAccountAndMembership(t *testing.T) {
	e := newTenEnv(t, janeEmail)
	tenant := e.tenant("tenant-a")
	operator := e.account("admin@example.com")
	e.exec("UPDATE iam_accounts SET is_platform_operator = true WHERE id = $1::uuid", operator.ID)
	created, token := e.invite(tenant.ID, operator.ID, janeEmail, reqctx.RoleMember)
	password := randomPassword(t)

	got, err := e.svc.AcceptNew(context.Background(), token, password, "  Jane Doe  ")

	if err != nil {
		t.Fatalf("accept new: %v", err)
	}
	if got.Account.Email != janeEmail || got.Account.FullName == nil || *got.Account.FullName != "Jane Doe" {
		t.Fatalf("account view: %+v", got.Account)
	}
	if got.Membership.Role != "member" || got.Membership.Status != "active" || got.Membership.Tenant.ID != tenant.ID {
		t.Fatalf("membership view: %+v", got.Membership)
	}
	var isOperator bool
	var identityID string
	err = e.pool.QueryRow(context.Background(),
		"SELECT is_platform_operator, kratos_identity_id::text FROM iam_accounts WHERE id = $1::uuid", got.Account.ID).
		Scan(&isOperator, &identityID)
	if err != nil || !isOperator {
		t.Fatalf("operator flag = %v err=%v", isOperator, err)
	}
	if identityID != e.kratos.identities[janeEmail] {
		t.Fatalf("account is not linked to the created identity")
	}
	if got := e.invitationStatus(created.Invitation.ID); got != "accepted" {
		t.Fatalf("invitation status = %s", got)
	}
	var sent bool
	for _, req := range e.kratos.received() {
		if req.Method == "POST" && strings.Contains(req.Body, password) {
			sent = true
		}
	}
	if !sent {
		t.Fatalf("the password never reached the Kratos create call")
	}
}

func TestAcceptNewDoesNotGrantOperatorWhenTheEmailIsNotListed(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")
	_, token := e.invite(tenant.ID, admin.ID, janeEmail, reqctx.RoleAdmin)

	got, err := e.svc.AcceptNew(context.Background(), token, randomPassword(t), "Jane Doe")

	if err != nil {
		t.Fatalf("accept new: %v", err)
	}
	var isOperator bool
	err = e.pool.QueryRow(context.Background(),
		"SELECT is_platform_operator FROM iam_accounts WHERE id = $1::uuid", got.Account.ID).Scan(&isOperator)
	if err != nil || isOperator {
		t.Fatalf("operator flag = %v err=%v, want false", isOperator, err)
	}
}

func TestAcceptNewWithExistingIdentityKeepsTheInvitationPendingAndAcceptRecovers(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")
	created, token := e.invite(tenant.ID, admin.ID, janeEmail, reqctx.RoleMember)
	identityID := e.kratos.addIdentity(janeEmail)

	_, err := e.svc.AcceptNew(context.Background(), token, randomPassword(t), "Jane Doe")

	requireAPIError(t, err, 409, CodeIdentityAlreadyExists)
	if got := e.invitationStatus(created.Invitation.ID); got != "pending" {
		t.Fatalf("invitation status = %s, want pending", got)
	}
	if e.kratos.count("POST") != 0 {
		t.Fatalf("an identity was created although one existed")
	}
	account, err := e.access.linkIdentity(context.Background(), identityID, janeEmail)
	if err != nil {
		t.Fatalf("link identity: %v", err)
	}
	view, err := e.svc.AcceptInvitation(context.Background(), account.ID, token)
	if err != nil || view.Role != "member" {
		t.Fatalf("accept after link: %+v err=%v", view, err)
	}
}

func TestAcceptNewKratosCreateConflictIsIdentityAlreadyExists(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")
	created, token := e.invite(tenant.ID, admin.ID, janeEmail, reqctx.RoleMember)
	e.kratos.addIdentity(janeEmail)
	e.kratos.hideFirstFind = true

	_, err := e.svc.AcceptNew(context.Background(), token, randomPassword(t), "Jane Doe")

	requireAPIError(t, err, 409, CodeIdentityAlreadyExists)
	if got := e.invitationStatus(created.Invitation.ID); got != "pending" {
		t.Fatalf("invitation status = %s", got)
	}
	if n := e.count("SELECT count(*) FROM iam_accounts WHERE email = $1", janeEmail); n != 0 {
		t.Fatalf("accounts = %d, want 0", n)
	}
}

func TestAcceptNewWithUnavailableKratosRollsBack(t *testing.T) {
	cases := map[string]func(f *fakeKratos){
		"lookup fails": func(f *fakeKratos) { f.failures = 1 },
		"create fails": func(f *fakeKratos) { f.rejectStatus = 503 },
	}
	for name, breakKratos := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTenEnv(t)
			tenant := e.tenant("tenant-a")
			admin := e.account("admin@example.com")
			created, token := e.invite(tenant.ID, admin.ID, janeEmail, reqctx.RoleMember)
			breakKratos(e.kratos)

			_, err := e.svc.AcceptNew(context.Background(), token, randomPassword(t), "Jane Doe")

			requireAPIError(t, err, 503, CodeIdentityProviderUnavailable)
			if got := e.invitationStatus(created.Invitation.ID); got != "pending" {
				t.Fatalf("invitation status = %s", got)
			}
			if n := e.count("SELECT count(*) FROM iam_accounts WHERE email = $1", janeEmail); n != 0 {
				t.Fatalf("accounts = %d, want 0", n)
			}
		})
	}
}

func TestAcceptNewRejectsBeforeCallingKratos(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")
	_, token := e.invite(tenant.ID, admin.ID, janeEmail, reqctx.RoleMember)
	unknown, err := newToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	ctx := context.Background()

	_, invalid := e.svc.AcceptNew(ctx, unknown, randomPassword(t), "Jane Doe")
	requireAPIError(t, invalid, 404, CodeInvitationInvalid)
	e.exec("UPDATE ten_tenants SET status = 'suspended' WHERE id = $1::uuid", tenant.ID)
	_, suspended := e.svc.AcceptNew(ctx, token, randomPassword(t), "Jane Doe")
	requireAPIError(t, suspended, 403, api.CodeTenantSuspended)

	if n := len(e.kratos.received()); n != 0 {
		t.Fatalf("Kratos received %d requests, want 0", n)
	}
}

func TestUpdateMembershipTransitions(t *testing.T) {
	type cell struct {
		from, to string
		code     api.Code
	}
	cells := []cell{
		{"active", "active", ""}, {"active", "suspended", ""}, {"active", "revoked", ""},
		{"suspended", "active", ""}, {"suspended", "suspended", ""}, {"suspended", "revoked", ""},
		{"revoked", "active", CodeInvitationRequired}, {"revoked", "suspended", CodeInvitationRequired},
		{"revoked", "revoked", ""},
	}
	for _, c := range cells {
		t.Run(c.from+" to "+c.to, func(t *testing.T) {
			e := newTenEnv(t)
			tenant := e.tenant("tenant-a")
			keeper := e.account("admin@example.com")
			e.member(tenant.ID, keeper.ID, "admin", "active")
			jane := e.account(janeEmail)
			target := e.member(tenant.ID, jane.ID, "member", c.from)
			to := c.to

			view, err := e.svc.UpdateMembership(context.Background(), tenant.ID, target.ID, nil, &to)

			wantStatus := c.to
			if c.code != "" {
				requireAPIError(t, err, 409, c.code)
				wantStatus = c.from
			} else if err != nil || view.Status != c.to || view.Account.Email != janeEmail {
				t.Fatalf("view = %+v err=%v", view, err)
			}
			if got := e.membershipRow(tenant.ID, jane.ID).Status; got != wantStatus {
				t.Fatalf("stored status = %s, want %s", got, wantStatus)
			}
		})
	}
}

func TestUpdateMembershipRoleChange(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	jane := e.account(janeEmail)
	suspended := e.member(tenant.ID, jane.ID, "member", "suspended")
	joe := e.account("joe.roe@example.com")
	revoked := e.member(tenant.ID, joe.ID, "member", "revoked")
	admin := reqctx.RoleAdmin
	ctx := context.Background()

	view, err := e.svc.UpdateMembership(ctx, tenant.ID, suspended.ID, &admin, nil)
	if err != nil || view.Role != "admin" || view.Status != "suspended" {
		t.Fatalf("role change on suspended: %+v err=%v", view, err)
	}
	_, err = e.svc.UpdateMembership(ctx, tenant.ID, revoked.ID, &admin, nil)
	requireAPIError(t, err, 409, CodeInvitationRequired)
	if got := e.membershipRow(tenant.ID, joe.ID).Role; got != "member" {
		t.Fatalf("revoked role = %s", got)
	}
}

func TestUpdateMembershipTenantIsolationAndBadIDs(t *testing.T) {
	e := newTenEnv(t)
	tenantA := e.tenant("tenant-a")
	tenantB := e.tenant("tenant-b")
	jane := e.account(janeEmail)
	inB := e.member(tenantB.ID, jane.ID, "member", "active")
	status := "suspended"
	ctx := context.Background()

	for _, id := range map[string]string{"foreign": inB.ID, "unknown": randomUUID(t), "malformed": "nope"} {
		_, err := e.svc.UpdateMembership(ctx, tenantA.ID, id, nil, &status)
		requireAPIError(t, err, 404, api.CodeResourceNotFound)
	}
	if got := e.membershipRow(tenantB.ID, jane.ID).Status; got != "active" {
		t.Fatalf("foreign membership changed to %s", got)
	}
}

func TestLastActiveAdminCannotLeaveAdminOrActive(t *testing.T) {
	changes := map[string]func() (*reqctx.Role, *string){
		"demote":  func() (*reqctx.Role, *string) { r := reqctx.RoleMember; return &r, nil },
		"suspend": func() (*reqctx.Role, *string) { s := "suspended"; return nil, &s },
		"revoke":  func() (*reqctx.Role, *string) { s := "revoked"; return nil, &s },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			e := newTenEnv(t)
			tenant := e.tenant("tenant-a")
			jane := e.account(janeEmail)
			sole := e.member(tenant.ID, jane.ID, "admin", "active")
			role, status := change()

			_, err := e.svc.UpdateMembership(context.Background(), tenant.ID, sole.ID, role, status)

			requireAPIError(t, err, 409, CodeLastTenantAdmin)
			row := e.membershipRow(tenant.ID, jane.ID)
			if row.Role != "admin" || row.Status != "active" {
				t.Fatalf("row changed: %+v", row)
			}
		})
	}
}

func TestLastAdminRuleCountsOnlyActiveAdmins(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	jane := e.account(janeEmail)
	joe := e.account("joe.roe@example.com")
	active := e.member(tenant.ID, jane.ID, "admin", "active")
	e.member(tenant.ID, joe.ID, "admin", "suspended")
	member := reqctx.RoleMember

	_, err := e.svc.UpdateMembership(context.Background(), tenant.ID, active.ID, &member, nil)

	requireAPIError(t, err, 409, CodeLastTenantAdmin)
}

func TestAdminDemotionAllowedWhileAnotherAdminRemains(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	jane := e.account(janeEmail)
	joe := e.account("joe.roe@example.com")
	first := e.member(tenant.ID, jane.ID, "admin", "active")
	second := e.member(tenant.ID, joe.ID, "admin", "active")
	member := reqctx.RoleMember
	ctx := context.Background()

	view, err := e.svc.UpdateMembership(ctx, tenant.ID, first.ID, &member, nil)
	if err != nil || view.Role != "member" {
		t.Fatalf("first demotion: %+v err=%v", view, err)
	}
	_, err = e.svc.UpdateMembership(ctx, tenant.ID, second.ID, &member, nil)

	requireAPIError(t, err, 409, CodeLastTenantAdmin)
}

func TestConcurrentDemotionsOfTheLastTwoAdminsLeaveOne(t *testing.T) {
	e := newTenEnv(t)
	member := reqctx.RoleMember
	for round := range 10 {
		tenant := e.tenant("tenant-" + string(rune('a'+round)))
		first := e.member(tenant.ID, e.account("a"+string(rune('a'+round))+"@example.com").ID, "admin", "active")
		second := e.member(tenant.ID, e.account("b"+string(rune('a'+round))+"@example.com").ID, "admin", "active")
		ids := []string{first.ID, second.ID}
		errs := make([]error, len(ids))
		var wg sync.WaitGroup
		for i := range ids {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = e.svc.UpdateMembership(context.Background(), tenant.ID, ids[i], &member, nil)
			}()
		}
		wg.Wait()

		failures := 0
		for _, err := range errs {
			if err != nil {
				requireAPIError(t, err, 409, CodeLastTenantAdmin)
				failures++
			}
		}
		admins := e.count("SELECT count(*) FROM ten_memberships WHERE tenant_id = $1::uuid AND role = 'admin' AND status = 'active'", tenant.ID)
		if failures != 1 || admins != 1 {
			t.Fatalf("round %d: failures=%d admins=%d, want 1 and 1", round, failures, admins)
		}
	}
}

func TestUpdateMembershipWithoutChangeLeavesUpdatedAt(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	jane := e.account(janeEmail)
	e.member(tenant.ID, e.account("admin@example.com").ID, "admin", "active")
	target := e.member(tenant.ID, jane.ID, "member", "active")
	e.clock.advance(time.Hour)
	same := "active"

	_, err := e.svc.UpdateMembership(context.Background(), tenant.ID, target.ID, nil, &same)

	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := e.membershipRow(tenant.ID, jane.ID).UpdatedAt; got.Equal(e.clock.Now()) {
		t.Fatalf("updated_at moved on a no-op")
	}
}

// FuzzAcceptInvitationToken feeds arbitrary user-supplied tokens to the
// accept service: whatever the bytes, the answer is the one 404 and no row
// changes.
func FuzzAcceptInvitationToken(f *testing.F) {
	pool := newTestPool(f)
	clk := &settableClock{now: tenStart}
	base, err := url.Parse(tenPublicBase)
	if err != nil {
		f.Fatalf("parse: %v", err)
	}
	svc := newTenancy(pool, clk, Config{PublicBaseURL: base, InvitationTTL: tenTTL})
	port := newAccountDirectory(pool, clk, nil, nil)
	svc.accounts = port
	svc.provisioner = port
	st := newStore(pool, clk)
	tenant, err := st.InsertTenant(context.Background(), "tenant-a", "Tenant A")
	if err != nil {
		f.Fatalf("tenant: %v", err)
	}
	name := "Jane Doe"
	account, err := st.UpsertAccount(context.Background(), randomUUID(f), janeEmail, &name)
	if err != nil {
		f.Fatalf("account: %v", err)
	}
	for _, seed := range []string{"", "x", "' OR 1=1 --", "%", "\x00", strings.Repeat("A", 43), strings.Repeat("A", 4096), "../../etc/passwd"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, token string) {
		_, err := svc.AcceptInvitation(context.Background(), account.ID, token)
		if token == "" {
			if err == nil {
				t.Fatalf("empty token accepted")
			}
			return
		}
		var apiErr *api.Error
		if !errors.As(err, &apiErr) || apiErr.Code != CodeInvitationInvalid || apiErr.Status != 404 {
			t.Fatalf("token %q: got %v, want 404 INVITATION_INVALID", token, err)
		}
		var rows int
		qErr := pool.QueryRow(context.Background(), "SELECT count(*) FROM ten_memberships WHERE tenant_id = $1::uuid", tenant.ID).Scan(&rows)
		if qErr != nil && !errors.Is(qErr, pgx.ErrNoRows) {
			t.Fatalf("count: %v", qErr)
		}
		if rows != 0 {
			t.Fatalf("a membership appeared for token %q", token)
		}
	})
}

func TestServicesRejectMalformedInput(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")
	member := e.member(tenant.ID, admin.ID, "admin", "active")
	ctx := context.Background()
	badRole := reqctx.Role("owner")
	badStatus := "frozen"

	cases := map[string]func() error{
		"create invitation empty email": func() error {
			_, err := e.svc.CreateInvitation(ctx, tenant.ID, admin.ID, "", reqctx.RoleMember)
			return err
		},
		"create invitation invalid role": func() error {
			_, err := e.svc.CreateInvitation(ctx, tenant.ID, admin.ID, janeEmail, badRole)
			return err
		},
		"create invitation malformed tenant id": func() error {
			_, err := e.svc.CreateInvitation(ctx, "not-a-uuid", admin.ID, janeEmail, reqctx.RoleMember)
			return err
		},
		"accept invitation empty token": func() error {
			_, err := e.svc.AcceptInvitation(ctx, admin.ID, "")
			return err
		},
		"accept invitation malformed account id": func() error {
			_, err := e.svc.AcceptInvitation(ctx, "not-a-uuid", "token")
			return err
		},
		"accept new empty password": func() error {
			_, err := e.svc.AcceptNew(ctx, "token", "", "Jane Doe")
			return err
		},
		"accept new blank name": func() error {
			_, err := e.svc.AcceptNew(ctx, "token", randomPassword(t), "   ")
			return err
		},
		"create tenant empty slug": func() error {
			_, err := e.svc.CreateTenant(ctx, admin.ID, "", "Name", janeEmail)
			return err
		},
		"create tenant malformed operator id": func() error {
			_, err := e.svc.CreateTenant(ctx, "not-a-uuid", "slug", "Name", janeEmail)
			return err
		},
		"update membership without fields": func() error {
			_, err := e.svc.UpdateMembership(ctx, tenant.ID, member.ID, nil, nil)
			return err
		},
		"update membership unknown status": func() error {
			_, err := e.svc.UpdateMembership(ctx, tenant.ID, member.ID, nil, &badStatus)
			return err
		},
		"update membership invalid role": func() error {
			_, err := e.svc.UpdateMembership(ctx, tenant.ID, member.ID, &badRole, nil)
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			err := call()
			var apiErr *api.Error
			// An input error is a leaf: a wrapped cause would be a database or Kratos failure.
			isPlainInputError := err != nil && !errors.As(err, &apiErr) && errors.Unwrap(err) == nil
			if !isPlainInputError {
				t.Fatalf("want a plain input error, got %v", err)
			}
		})
	}
	if n := e.count("SELECT count(*) FROM ten_invitations"); n != 0 {
		t.Fatalf("invitations = %d, want 0", n)
	}
}

func TestAcceptNewAfterProvisionFailureLeavesAccountAndAcceptRecovers(t *testing.T) {
	ctx := context.Background()
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")
	jane := e.account(janeEmail)
	e.member(tenant.ID, jane.ID, "member", "revoked")
	created, token := e.invite(tenant.ID, admin.ID, janeEmail, reqctx.RoleMember)
	e.kratos.rejectStatus = http.StatusInternalServerError

	_, err := e.svc.AcceptNew(ctx, token, randomPassword(t), "Jane Doe")

	requireAPIError(t, err, 503, CodeIdentityProviderUnavailable)
	if got := e.invitationStatus(created.Invitation.ID); got != "pending" {
		t.Fatalf("invitation status = %s, want pending", got)
	}

	e.kratos.rejectStatus = 0
	e.kratos.identities[janeEmail] = "11111111-1111-4111-8111-111111111111"
	_, err = e.svc.AcceptNew(ctx, token, randomPassword(t), "Jane Doe")
	requireAPIError(t, err, 409, CodeIdentityAlreadyExists)

	view, err := e.svc.AcceptInvitation(ctx, jane.ID, token)
	if err != nil || view.Status != statusActive {
		t.Fatalf("accept after failure: %+v err=%v", view, err)
	}
	if got := e.invitationStatus(created.Invitation.ID); got != "accepted" {
		t.Fatalf("invitation status = %s, want accepted", got)
	}
}

func TestAcceptInvitationComparesEmailsCaseInsensitively(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")
	jane := e.account(janeEmail)
	_, token := e.invite(tenant.ID, admin.ID, janeEmail, reqctx.RoleMember)
	e.svc.accounts = upperCaseDirectory{inner: e.svc.accounts}

	view, err := e.svc.AcceptInvitation(context.Background(), jane.ID, token)

	if err != nil || view.Status != statusActive {
		t.Fatalf("accept = %+v err=%v, want accepted", view, err)
	}
}

func TestCreateInvitationSeesActiveMemberAmongAccountsSharingTheEmail(t *testing.T) {
	e := newTenEnv(t)
	tenant := e.tenant("tenant-a")
	admin := e.account("admin@example.com")
	older := e.account("shared@example.com")
	newer := e.account("shared@example.com")
	e.exec("UPDATE iam_accounts SET created_at = created_at + interval '1 hour' WHERE id = $1::uuid", newer.ID)
	e.member(tenant.ID, older.ID, "member", "revoked")
	e.member(tenant.ID, newer.ID, "member", "active")

	_, err := e.svc.CreateInvitation(context.Background(), tenant.ID, admin.ID, "shared@example.com", reqctx.RoleMember)

	requireAPIError(t, err, 409, CodeMembershipAlreadyActive)
}

// upperCaseDirectory answers every email upper-cased, as a store that does not
// normalize would. iam_accounts_email_lower forbids seeding such a row, so the
// accounts port is the only place the case can be injected.
type upperCaseDirectory struct {
	inner accounts.Directory
}

func (d upperCaseDirectory) Get(ctx context.Context, ids []reqctx.AccountID) (map[reqctx.AccountID]accounts.Ref, error) {
	found, err := d.inner.Get(ctx, ids)
	for id, ref := range found {
		ref.Email = strings.ToUpper(ref.Email)
		found[id] = ref
	}
	return found, err
}

func (d upperCaseDirectory) ByEmail(ctx context.Context, email string) ([]accounts.Ref, error) {
	return d.inner.ByEmail(ctx, email)
}
