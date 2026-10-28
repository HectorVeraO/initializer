package identity

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

type tenantPage = api.Page[TenantView]
type memberPage = api.Page[MemberView]

// memberOf adds a person with the given email to the tenant.
func (e *invEnv) memberOf(tenantID, email, role, status string, createdAt time.Time) person {
	e.t.Helper()
	p := e.person()
	e.exec("UPDATE iam_accounts SET email = $2 WHERE id = $1::uuid", p.accountID, email)
	e.member(tenantID, p.accountID, role, status, createdAt)
	return p
}

func (e *invEnv) membershipID(tenantID, accountID string) string {
	e.t.Helper()
	return e.scan("SELECT id::text FROM ten_memberships WHERE tenant_id = $1::uuid AND account_id = $2::uuid", tenantID, accountID)
}

func statusBody(status string) string {
	return fmt.Sprintf(`{"status":%q}`, status)
}

func TestGetTenantAnswersTheCallersTenantForEveryMatchingReference(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	e.tenant("tenant-b", "active")
	_, viewer := e.actor(tenant, "viewer", false)
	refs := map[string]string{
		"no reference":    "",
		"uuid":            "?tenantId=" + tenant,
		"upper-case uuid": "?tenantId=" + strings.ToUpper(tenant),
		"slug":            "?tenantId=tenant-a",
		"upper-case slug": "?tenantId=TENANT-A",
	}
	for name, query := range refs {
		t.Run(name, func(t *testing.T) {
			rec := viewer.send(http.MethodGet, "/api/v1/tenant"+query, "")

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
			}
			summary := decodeData[tenantSummary](t, rec)
			want := tenantSummary{ID: tenant, Slug: "tenant-a", Name: "tenant-a", Status: "active"}
			if summary != want {
				t.Fatalf("summary = %+v, want %+v", summary, want)
			}
		})
	}
}

func TestGetTenantRefusesAnotherTenantsReferenceWithoutLookingItUp(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	other := e.tenant("tenant-b", "active")
	_, viewer := e.actor(tenant, "viewer", false)
	missing := randomUUID(t)

	for _, ref := range []string{"tenant-b", other, strings.ToUpper(other), "no-such-tenant", missing, "not-a-uuid"} {
		rec := viewer.send(http.MethodGet, "/api/v1/tenant?tenantId="+ref, "")

		expectError(t, rec, http.StatusForbidden, "TENANT_MISMATCH")
	}
}

func TestGetTenantAnswersTheSuspendedStatusOfTheCallersTenant(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, viewer := e.actor(tenant, "viewer", false)
	e.exec("UPDATE ten_tenants SET status = 'suspended' WHERE id = $1::uuid", tenant)

	rec := viewer.send(http.MethodGet, "/api/v1/tenant", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := decodeData[tenantSummary](t, rec).Status; got != "suspended" {
		t.Fatalf("status = %q, want suspended", got)
	}
}

func TestGetTenantRequiresTheTenantReadScopeOnAnAPIKey(t *testing.T) {
	cases := map[string]struct {
		scopes []string
		status int
		code   string
	}{
		"scope held":    {[]string{"tenant:read"}, http.StatusOK, ""},
		"scope missing": {[]string{"api-keys:read"}, http.StatusForbidden, "INSUFFICIENT_SCOPE"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newSessEnv(t, sessEnvOptions{})
			tenant := e.tenant("tenant-a", "active")
			plain := e.apiKey(tenant, tc.scopes, nil)

			rec := e.client().do(http.MethodGet, "/api/v1/tenant", map[string]string{apiKeyHeader: plain})

			if tc.code != "" {
				expectError(t, rec, tc.status, tc.code)
				return
			}
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

func TestGetTenantRefusals(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, viewer := e.actor(tenant, "viewer", false)
	_, tenantless := e.actor("", "", false)

	t.Run("no credential", func(t *testing.T) {
		expectError(t, e.client().send(http.MethodGet, "/api/v1/tenant", ""), http.StatusUnauthorized, "SESSION_INVALID")
	})
	t.Run("altered session cookie", func(t *testing.T) {
		cookie := viewer.cookies[viewer.sessionCookieName()]
		last := cookie[len(cookie)-1]
		replacement := byte('A')
		if last == 'A' {
			replacement = 'B'
		}
		altered := &sessClient{e: e.sessEnv, cookies: map[string]string{viewer.sessionCookieName(): cookie[:len(cookie)-1] + string(replacement)}}

		expectError(t, altered.send(http.MethodGet, "/api/v1/tenant", ""), http.StatusUnauthorized, "SESSION_INVALID")
	})
	t.Run("no active tenant", func(t *testing.T) {
		expectError(t, tenantless.send(http.MethodGet, "/api/v1/tenant", ""), http.StatusForbidden, "ACTIVE_TENANT_REQUIRED")
	})
}

func TestListTenantsPagesAndSortsAcrossTenants(t *testing.T) {
	e := newInvEnv(t, "")
	_, operator := e.actor("", "", true)
	e.tenant("tenant-c", "active")
	e.tenant("tenant-a", "suspended")
	e.tenant("tenant-b", "active")

	all := decodeData[tenantPage](t, operator.send(http.MethodGet, "/api/v1/tenants", ""))
	if all.TotalItems != 3 || all.Page != 1 || all.Size != 20 {
		t.Fatalf("page = %+v", all)
	}
	slugs := func(items []TenantView) string {
		var out []string
		for _, item := range items {
			out = append(out, item.Slug)
		}
		return strings.Join(out, ",")
	}
	if got := slugs(all.Items); got != "tenant-a,tenant-b,tenant-c" {
		t.Fatalf("default order = %s", got)
	}
	desc := decodeData[tenantPage](t, operator.send(http.MethodGet, "/api/v1/tenants?sort=-slug", ""))
	if got := slugs(desc.Items); got != "tenant-c,tenant-b,tenant-a" {
		t.Fatalf("slug desc = %s", got)
	}
	created := decodeData[tenantPage](t, operator.send(http.MethodGet, "/api/v1/tenants?sort=createdAt", ""))
	if got := slugs(created.Items); got != "tenant-c,tenant-a,tenant-b" {
		t.Fatalf("createdAt order = %s", got)
	}
	second := decodeData[tenantPage](t, operator.send(http.MethodGet, "/api/v1/tenants?size=2&page=2", ""))
	if got := slugs(second.Items); got != "tenant-c" || second.TotalItems != 3 || second.Page != 2 || second.Size != 2 {
		t.Fatalf("second page = %s %+v", got, second)
	}
	beyond := decodeData[tenantPage](t, operator.send(http.MethodGet, "/api/v1/tenants?page=9", ""))
	if len(beyond.Items) != 0 || beyond.TotalItems != 3 {
		t.Fatalf("beyond = %+v", beyond)
	}
	suspended := decodeData[tenantPage](t, operator.send(http.MethodGet, "/api/v1/tenants?status=suspended", ""))
	if got := slugs(suspended.Items); got != "tenant-a" || suspended.TotalItems != 1 {
		t.Fatalf("suspended = %s %+v", got, suspended)
	}
	if suspended.Items[0].Status != "suspended" || suspended.Items[0].ID == "" || suspended.Items[0].CreatedAt.IsZero() {
		t.Fatalf("view = %+v", suspended.Items[0])
	}
}

func TestListTenantsValidatesTheQuery(t *testing.T) {
	e := newInvEnv(t, "")
	_, operator := e.actor("", "", true)
	cases := []struct {
		query string
		want  []wantIssue
	}{
		{"status=revoked", []wantIssue{{"status", "FIELD_NOT_ALLOWED"}}},
		{"page=0", []wantIssue{{"page", "FIELD_OUT_OF_RANGE"}}},
		{"size=101", []wantIssue{{"size", "FIELD_OUT_OF_RANGE"}}},
		{"size=abc", []wantIssue{{"size", "FIELD_TYPE_INVALID"}}},
		{"sort=unknown", []wantIssue{{"sort", "FIELD_NOT_ALLOWED"}}},
		{"status=x&page=0&sort=email", []wantIssue{{"status", "FIELD_NOT_ALLOWED"}, {"page", "FIELD_OUT_OF_RANGE"}, {"sort", "FIELD_NOT_ALLOWED"}}},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			expectIssues(t, operator.send(http.MethodGet, "/api/v1/tenants?"+tc.query, ""), tc.want)
		})
	}
}

func TestTenantRoutesRequireAnOperator(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	paths := []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/tenants", ""},
		{http.MethodPatch, "/api/v1/tenants/" + tenant + "/status", statusBody("suspended")},
	}
	for _, tc := range paths {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			expectError(t, admin.send(tc.method, tc.path, tc.body), http.StatusForbidden, "PLATFORM_OPERATOR_REQUIRED")
			expectError(t, e.client().send(tc.method, tc.path, tc.body), http.StatusUnauthorized, "SESSION_INVALID")
		})
	}
	if got := e.scan("SELECT status FROM ten_tenants WHERE id = $1::uuid", tenant); got != "active" {
		t.Fatalf("tenant status = %s after refused calls", got)
	}
}

func TestSetTenantStatusSuspendsAndReactivates(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	e.clock.advance(time.Hour)
	_, operator := e.actor("", "", true)

	rec := operator.send(http.MethodPatch, "/api/v1/tenants/"+tenant+"/status", statusBody("suspended"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	view := decodeData[TenantView](t, rec)
	if view.ID != tenant || view.Slug != "tenant-a" || view.Status != "suspended" {
		t.Fatalf("view = %+v", view)
	}
	wantUpdated := e.clock.Now().Format(time.RFC3339)
	if got := e.scan("SELECT to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"') FROM ten_tenants WHERE id = $1::uuid", tenant); got != wantUpdated {
		t.Fatalf("updated_at = %s, want the clock's %s", got, wantUpdated)
	}

	back := operator.send(http.MethodPatch, "/api/v1/tenants/"+tenant+"/status", statusBody("active"))
	if got := decodeData[TenantView](t, back).Status; got != "active" {
		t.Fatalf("status = %s, want active", got)
	}
}

func TestSetTenantStatusToTheCurrentStatusIsANoOp(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "suspended")
	before := e.scan("SELECT updated_at::text FROM ten_tenants WHERE id = $1::uuid", tenant)
	e.clock.advance(time.Hour)
	_, operator := e.actor("", "", true)

	rec := operator.send(http.MethodPatch, "/api/v1/tenants/"+tenant+"/status", statusBody("suspended"))

	if rec.Code != http.StatusOK || decodeData[TenantView](t, rec).Status != "suspended" {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if after := e.scan("SELECT updated_at::text FROM ten_tenants WHERE id = $1::uuid", tenant); after != before {
		t.Fatalf("updated_at moved from %s to %s on a no-op", before, after)
	}
}

func TestSetTenantStatusValidatesTheBody(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, operator := e.actor("", "", true)
	cases := []struct {
		name string
		body string
		want []wantIssue
	}{
		{"missing", `{}`, []wantIssue{{"status", "FIELD_REQUIRED"}}},
		{"null", `{"status":null}`, []wantIssue{{"status", "FIELD_REQUIRED"}}},
		{"wrong type", `{"status":1}`, []wantIssue{{"status", "FIELD_TYPE_INVALID"}}},
		{"not allowed", `{"status":"revoked"}`, []wantIssue{{"status", "FIELD_NOT_ALLOWED"}}},
		{"wrong case", `{"status":"Active"}`, []wantIssue{{"status", "FIELD_NOT_ALLOWED"}}},
		{"empty", `{"status":""}`, []wantIssue{{"status", "FIELD_NOT_ALLOWED"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectIssues(t, operator.send(http.MethodPatch, "/api/v1/tenants/"+tenant+"/status", tc.body), tc.want)
		})
	}
	expectIssues(t, operator.send(http.MethodPatch, "/api/v1/tenants/"+tenant+"/status", `{`), []wantIssue{{"", "BODY_MALFORMED"}})
}

func TestSetTenantStatusAnswersTheSameNotFoundForUnknownAndMalformedIDs(t *testing.T) {
	e := newInvEnv(t, "")
	_, operator := e.actor("", "", true)

	unknown := operator.send(http.MethodPatch, "/api/v1/tenants/"+randomUUID(t)+"/status", statusBody("suspended"))
	malformed := operator.send(http.MethodPatch, "/api/v1/tenants/not-a-uuid/status", statusBody("suspended"))

	expectError(t, unknown, http.StatusNotFound, "RESOURCE_NOT_FOUND")
	expectError(t, malformed, http.StatusNotFound, "RESOURCE_NOT_FOUND")
	if !reflect.DeepEqual(errorBody(t, unknown), errorBody(t, malformed)) {
		t.Fatalf("bodies differ: %+v vs %+v", errorBody(t, unknown), errorBody(t, malformed))
	}
	invalid := operator.send(http.MethodPatch, "/api/v1/tenants/not-a-uuid/status", `{}`)
	expectIssues(t, invalid, []wantIssue{{"status", "FIELD_REQUIRED"}})
}

func TestSuspendingATenantRefusesItsSessionsAtTheNextRefreshAndSwitch(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	other := e.tenant("tenant-b", "active")
	member, memberClient := e.actor(tenant, "member", false)
	e.member(other, member.accountID, "member", "active", invStart)
	_, operator := e.actor("", "", true)

	rec := operator.send(http.MethodPatch, "/api/v1/tenants/"+tenant+"/status", statusBody("suspended"))
	if rec.Code != http.StatusOK {
		t.Fatalf("suspend answered %d: %s", rec.Code, rec.Body.String())
	}

	expectError(t, memberClient.mutate(http.MethodPost, "/api/v1/auth/refresh"), http.StatusForbidden, "TENANT_SUSPENDED")

	second := e.signIn(member)
	e.exec("UPDATE ten_tenants SET status = 'active' WHERE id = $1::uuid", tenant)
	e.exec("UPDATE ten_tenants SET status = 'suspended' WHERE id = $1::uuid", other)
	switchRec := second.send(http.MethodPost, "/api/v1/auth/switch-tenant", fmt.Sprintf(`{"tenantId":%q}`, other))
	expectError(t, switchRec, http.StatusForbidden, "TENANT_SUSPENDED")
}

func TestListMembershipsShowsActiveAndSuspendedMembersOfTheActiveTenantOnly(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	other := e.tenant("tenant-b", "active")
	admin := e.person()
	e.member(tenant, admin.accountID, "admin", "active", invStart)
	e.memberOf(tenant, "bob@example.com", "member", "suspended", invStart.Add(time.Hour))
	e.memberOf(tenant, "alice@example.com", "viewer", "revoked", invStart.Add(2*time.Hour))
	e.memberOf(other, "dave@example.com", "admin", "active", invStart)
	client := e.signIn(admin)
	e.exec("UPDATE iam_accounts SET email = 'carol@example.com' WHERE id = $1::uuid", admin.accountID)

	rec := client.send(http.MethodGet, "/api/v1/memberships", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	page := decodeData[memberPage](t, rec)
	if page.TotalItems != 2 || len(page.Items) != 2 {
		t.Fatalf("page = %+v", page)
	}
	first, second := page.Items[0], page.Items[1]
	if first.Account.Email != "bob@example.com" || second.Account.Email != "carol@example.com" {
		t.Fatalf("order = %s, %s", first.Account.Email, second.Account.Email)
	}
	wantTenant := tenantSummary{ID: tenant, Slug: "tenant-a", Name: "tenant-a", Status: "active"}
	if second.Tenant != wantTenant || second.Role != "admin" || second.Status != "active" {
		t.Fatalf("item = %+v", second)
	}
	if second.ID != e.membershipID(tenant, admin.accountID) || second.Account.ID != admin.accountID {
		t.Fatalf("ids = %s / %s", second.ID, second.Account.ID)
	}
	if !second.CreatedAt.Equal(invStart) {
		t.Fatalf("createdAt = %s", second.CreatedAt)
	}
	if first.Status != "suspended" || first.Role != "member" {
		t.Fatalf("suspended item = %+v", first)
	}
}

func TestListMembershipsFiltersSortsAndPages(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	admin := e.person()
	e.member(tenant, admin.accountID, "admin", "active", invStart)
	e.memberOf(tenant, "a@example.com", "viewer", "active", invStart.Add(2*time.Hour))
	e.memberOf(tenant, "b@example.com", "member", "revoked", invStart.Add(time.Hour))
	e.memberOf(tenant, "d@example.com", "viewer", "suspended", invStart.Add(3*time.Hour))
	client := e.signIn(admin)
	e.exec("UPDATE iam_accounts SET email = 'c@example.com' WHERE id = $1::uuid", admin.accountID)
	emails := func(items []MemberView) string {
		var out []string
		for _, item := range items {
			out = append(out, item.Account.Email)
		}
		return strings.Join(out, ",")
	}
	cases := []struct {
		query string
		want  string
		total int64
	}{
		{"", "a@example.com,c@example.com,d@example.com", 3},
		{"?sort=-email", "d@example.com,c@example.com,a@example.com", 3},
		{"?sort=createdAt", "c@example.com,a@example.com,d@example.com", 3},
		{"?sort=-createdAt", "d@example.com,a@example.com,c@example.com", 3},
		{"?status=revoked", "b@example.com", 1},
		{"?status=active", "a@example.com,c@example.com", 2},
		{"?status=suspended&role=viewer", "d@example.com", 1},
		{"?role=viewer", "a@example.com,d@example.com", 2},
		{"?role=admin&status=revoked", "", 0},
		{"?size=1&page=2", "c@example.com", 3},
		{"?size=2&page=2", "d@example.com", 3},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			rec := client.send(http.MethodGet, "/api/v1/memberships"+tc.query, "")

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
			}
			page := decodeData[memberPage](t, rec)
			if got := emails(page.Items); got != tc.want || page.TotalItems != tc.total {
				t.Fatalf("items = %q total %d, want %q total %d", got, page.TotalItems, tc.want, tc.total)
			}
		})
	}
}

func TestListMembershipsValidatesTheQuery(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, viewer := e.actor(tenant, "viewer", false)
	cases := []struct {
		query string
		want  []wantIssue
	}{
		{"page=0", []wantIssue{{"page", "FIELD_OUT_OF_RANGE"}}},
		{"page=-1", []wantIssue{{"page", "FIELD_OUT_OF_RANGE"}}},
		{"size=101", []wantIssue{{"size", "FIELD_OUT_OF_RANGE"}}},
		{"size=0", []wantIssue{{"size", "FIELD_OUT_OF_RANGE"}}},
		{"page=x", []wantIssue{{"page", "FIELD_TYPE_INVALID"}}},
		{"sort=unknown", []wantIssue{{"sort", "FIELD_NOT_ALLOWED"}}},
		{"sort=slug", []wantIssue{{"sort", "FIELD_NOT_ALLOWED"}}},
		{"status=pending", []wantIssue{{"status", "FIELD_NOT_ALLOWED"}}},
		{"role=owner", []wantIssue{{"role", "FIELD_NOT_ALLOWED"}}},
		{"status=pending&role=owner&page=0&sort=x", []wantIssue{
			{"status", "FIELD_NOT_ALLOWED"}, {"role", "FIELD_NOT_ALLOWED"}, {"page", "FIELD_OUT_OF_RANGE"}, {"sort", "FIELD_NOT_ALLOWED"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			expectIssues(t, viewer.send(http.MethodGet, "/api/v1/memberships?"+tc.query, ""), tc.want)
		})
	}
}

func TestListMembershipsRefusals(t *testing.T) {
	e := newInvEnv(t, "")
	_, tenantless := e.actor("", "", false)
	_, operator := e.actor("", "", true)

	expectError(t, e.client().send(http.MethodGet, "/api/v1/memberships", ""), http.StatusUnauthorized, "SESSION_INVALID")
	expectError(t, tenantless.send(http.MethodGet, "/api/v1/memberships", ""), http.StatusForbidden, "ACTIVE_TENANT_REQUIRED")
	expectError(t, operator.send(http.MethodGet, "/api/v1/memberships", ""), http.StatusForbidden, "ACTIVE_TENANT_REQUIRED")
}

func TestUpdateMembershipRouteTransitions(t *testing.T) {
	cases := []struct {
		name       string
		role       string
		status     string
		body       string
		wantStatus int
		wantCode   string
		wantRole   string
		wantState  string
	}{
		{"active to suspended", "member", "active", `{"status":"suspended"}`, 200, "", "member", "suspended"},
		{"active to revoked", "member", "active", `{"status":"revoked"}`, 200, "", "member", "revoked"},
		{"active role change", "member", "active", `{"role":"viewer"}`, 200, "", "viewer", "active"},
		{"active to active is a no-op", "member", "active", `{"status":"active"}`, 200, "", "member", "active"},
		{"active role and status", "member", "active", `{"role":"admin","status":"suspended"}`, 200, "", "admin", "suspended"},
		{"suspended to active", "member", "suspended", `{"status":"active"}`, 200, "", "member", "active"},
		{"suspended to revoked", "member", "suspended", `{"status":"revoked"}`, 200, "", "member", "revoked"},
		{"suspended role change", "member", "suspended", `{"role":"admin"}`, 200, "", "admin", "suspended"},
		{"revoked to active", "member", "revoked", `{"status":"active"}`, 409, "INVITATION_REQUIRED", "member", "revoked"},
		{"revoked to suspended", "member", "revoked", `{"status":"suspended"}`, 409, "INVITATION_REQUIRED", "member", "revoked"},
		{"revoked role change", "member", "revoked", `{"role":"admin"}`, 409, "INVITATION_REQUIRED", "member", "revoked"},
		{"revoked to revoked is a no-op", "member", "revoked", `{"status":"revoked"}`, 200, "", "member", "revoked"},
		{"revoked same role is a no-op", "member", "revoked", `{"role":"member","status":"revoked"}`, 200, "", "member", "revoked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newInvEnv(t, "")
			tenant := e.tenant("tenant-a", "active")
			_, admin := e.actor(tenant, "admin", false)
			target := e.memberOf(tenant, "target@example.com", tc.role, tc.status, invStart)
			id := e.membershipID(tenant, target.accountID)

			rec := admin.send(http.MethodPatch, "/api/v1/memberships/"+id, tc.body)

			if tc.wantCode != "" {
				expectError(t, rec, tc.wantStatus, tc.wantCode)
			} else {
				if rec.Code != tc.wantStatus {
					t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
				}
				view := decodeData[MemberView](t, rec)
				if view.ID != id || view.Role != tc.wantRole || view.Status != tc.wantState || view.Account.ID != target.accountID {
					t.Fatalf("view = %+v", view)
				}
				if view.Tenant.Slug != "tenant-a" || view.CreatedAt.IsZero() {
					t.Fatalf("view = %+v", view)
				}
			}
			got := e.scan("SELECT role || '/' || status FROM ten_memberships WHERE id = $1::uuid", id)
			if got != tc.wantRole+"/"+tc.wantState {
				t.Fatalf("stored = %s, want %s/%s", got, tc.wantRole, tc.wantState)
			}
		})
	}
}

func TestUpdateMembershipStampsUpdatedAtFromTheClockOnlyOnChange(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	e.clock.advance(time.Hour)
	_, admin := e.actor(tenant, "admin", false)
	target := e.memberOf(tenant, "target@example.com", "member", "active", invStart)
	id := e.membershipID(tenant, target.accountID)
	changedAt := e.clock.Now().UTC().Format("2006-01-02T15:04:05")
	stamp := func() string {
		return e.scan(`SELECT to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS') FROM ten_memberships WHERE id = $1::uuid`, id)
	}

	first := admin.send(http.MethodPatch, "/api/v1/memberships/"+id, `{"role":"viewer"}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first PATCH status = %d: %s", first.Code, first.Body.String())
	}
	if got := stamp(); got != changedAt {
		t.Fatalf("updated_at = %s, want %s", got, changedAt)
	}
	e.clock.advance(time.Minute)
	rec := admin.send(http.MethodPatch, "/api/v1/memberships/"+id, `{"role":"viewer"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("no-op PATCH status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := stamp(); got != changedAt {
		t.Fatalf("no-op moved updated_at to %s", got)
	}
}

func TestUpdateMembershipProtectsTheLastActiveAdmin(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	adminPerson, admin := e.actor(tenant, "admin", false)
	id := e.membershipID(tenant, adminPerson.accountID)
	for _, body := range []string{`{"status":"revoked"}`, `{"status":"suspended"}`, `{"role":"member"}`, `{"role":"viewer","status":"active"}`} {
		expectError(t, admin.send(http.MethodPatch, "/api/v1/memberships/"+id, body), http.StatusConflict, "LAST_TENANT_ADMIN")
	}
	listed := decodeData[memberPage](t, admin.send(http.MethodGet, "/api/v1/memberships", ""))
	if len(listed.Items) != 1 || listed.Items[0].Role != "admin" || listed.Items[0].Status != "active" {
		t.Fatalf("members = %+v", listed.Items)
	}

	// A second active admin lifts the rule; a suspended one does not count.
	second := e.memberOf(tenant, "second@example.com", "admin", "suspended", invStart)
	expectError(t, admin.send(http.MethodPatch, "/api/v1/memberships/"+id, `{"role":"member"}`), http.StatusConflict, "LAST_TENANT_ADMIN")
	secondID := e.membershipID(tenant, second.accountID)
	if rec := admin.send(http.MethodPatch, "/api/v1/memberships/"+secondID, `{"status":"active"}`); rec.Code != http.StatusOK {
		t.Fatalf("activate answered %d: %s", rec.Code, rec.Body.String())
	}
	if rec := admin.send(http.MethodPatch, "/api/v1/memberships/"+id, `{"role":"member"}`); rec.Code != http.StatusOK {
		t.Fatalf("demote answered %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateMembershipValidatesTheBody(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	target := e.memberOf(tenant, "target@example.com", "member", "active", invStart)
	path := "/api/v1/memberships/" + e.membershipID(tenant, target.accountID)
	cases := []struct {
		name string
		body string
		want []wantIssue
	}{
		{"neither", `{}`, []wantIssue{{"", "FIELD_REQUIRED"}}},
		{"both null", `{"role":null,"status":null}`, []wantIssue{{"", "FIELD_REQUIRED"}}},
		{"unknown field only", `{"name":"x"}`, []wantIssue{{"", "FIELD_REQUIRED"}}},
		{"role wrong type", `{"role":1}`, []wantIssue{{"role", "FIELD_TYPE_INVALID"}}},
		{"status wrong type", `{"status":true}`, []wantIssue{{"status", "FIELD_TYPE_INVALID"}}},
		{"role not allowed", `{"role":"owner"}`, []wantIssue{{"role", "FIELD_NOT_ALLOWED"}}},
		{"status not allowed", `{"status":"pending"}`, []wantIssue{{"status", "FIELD_NOT_ALLOWED"}}},
		{"empty role", `{"role":""}`, []wantIssue{{"role", "FIELD_NOT_ALLOWED"}}},
		{"both bad", `{"role":"owner","status":3}`, []wantIssue{{"role", "FIELD_NOT_ALLOWED"}, {"status", "FIELD_TYPE_INVALID"}}},
		{"bad role with good status", `{"role":"owner","status":"active"}`, []wantIssue{{"role", "FIELD_NOT_ALLOWED"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectIssues(t, admin.send(http.MethodPatch, path, tc.body), tc.want)
		})
	}
	expectIssues(t, admin.send(http.MethodPatch, path, `[`), []wantIssue{{"", "BODY_MALFORMED"}})
}

func TestUpdateMembershipAnswersTheSameNotFoundForForeignRandomAndMalformedIDs(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	other := e.tenant("tenant-b", "active")
	_, admin := e.actor(tenant, "admin", false)
	foreign := e.memberOf(other, "foreign@example.com", "member", "active", invStart)
	foreignID := e.membershipID(other, foreign.accountID)

	var bodies []*api.Error
	for _, id := range []string{foreignID, randomUUID(t), "not-a-uuid", strings.ToUpper(foreignID)} {
		rec := admin.send(http.MethodPatch, "/api/v1/memberships/"+id, `{"status":"revoked"}`)

		expectError(t, rec, http.StatusNotFound, "RESOURCE_NOT_FOUND")
		bodies = append(bodies, errorBody(t, rec))
	}
	for _, body := range bodies[1:] {
		if !reflect.DeepEqual(body, bodies[0]) {
			t.Fatalf("bodies differ: %+v vs %+v", body, bodies[0])
		}
	}
	if got := e.scan("SELECT status FROM ten_memberships WHERE id = $1::uuid", foreignID); got != "active" {
		t.Fatalf("foreign membership status = %s", got)
	}
}

func TestUpdateMembershipValidationPrecedesLookup(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)

	expectIssues(t, admin.send(http.MethodPatch, "/api/v1/memberships/not-a-uuid", `{}`), []wantIssue{{"", "FIELD_REQUIRED"}})
}

func TestUpdateMembershipRefusals(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	_, member := e.actor(tenant, "member", false)
	_, viewer := e.actor(tenant, "viewer", false)
	_, operator := e.actor("", "", true)
	target := e.memberOf(tenant, "target@example.com", "member", "active", invStart)
	path := "/api/v1/memberships/" + e.membershipID(tenant, target.accountID)
	body := `{"role":"viewer"}`

	expectError(t, e.client().send(http.MethodPatch, path, body), http.StatusUnauthorized, "SESSION_INVALID")
	expectError(t, member.send(http.MethodPatch, path, body), http.StatusForbidden, "INSUFFICIENT_ROLE")
	expectError(t, viewer.send(http.MethodPatch, path, body), http.StatusForbidden, "INSUFFICIENT_ROLE")
	expectError(t, operator.send(http.MethodPatch, path, body), http.StatusForbidden, "ACTIVE_TENANT_REQUIRED")

	for name, header := range map[string]map[string]string{
		"no token":    {},
		"wrong value": {csrfHeader: admin.cookies["XSRF-TOKEN"] + "x"},
	} {
		t.Run("csrf "+name, func(t *testing.T) {
			rec := admin.doBody(http.MethodPatch, path, header, body)

			expectError(t, rec, http.StatusForbidden, "CSRF_TOKEN_INVALID")
		})
	}
	if got := e.scan("SELECT role FROM ten_memberships WHERE id = $1::uuid", strings.TrimPrefix(path, "/api/v1/memberships/")); got != "member" {
		t.Fatalf("role = %s after refused calls", got)
	}
}

func TestListMembershipsIsReadableByEveryRole(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	for _, role := range []reqctx.Role{reqctx.RoleAdmin, reqctx.RoleMember, reqctx.RoleViewer} {
		_, client := e.actor(tenant, string(role), false)

		rec := client.send(http.MethodGet, "/api/v1/memberships", "")

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d: %s", role, rec.Code, rec.Body.String())
		}
	}
}

func TestRevokingAMembershipRefusesTheMembersNextRefresh(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	member, memberClient := e.actor(tenant, "member", false)
	id := e.membershipID(tenant, member.accountID)

	rec := admin.send(http.MethodPatch, "/api/v1/memberships/"+id, `{"status":"revoked"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke answered %d: %s", rec.Code, rec.Body.String())
	}

	expectError(t, memberClient.mutate(http.MethodPost, "/api/v1/auth/refresh"), http.StatusForbidden, "MEMBERSHIP_REVOKED")
}

func TestTenantDirectoryTellsExistingFromUnknownWhateverTheStatus(t *testing.T) {
	e := newInvEnv(t, "")
	active := e.tenant("tenant-a", "active")
	suspended := e.tenant("tenant-b", "suspended")
	directory := e.directory
	cases := []struct {
		name string
		id   string
		want bool
	}{
		{"active", active, true},
		{"suspended", suspended, true},
		{"upper-case id", strings.ToUpper(active), true},
		{"unknown", randomUUID(t), false},
		{"malformed", "not-a-uuid", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := directory.TenantExists(context.Background(), reqctx.TenantID(tc.id))

			if err != nil || got != tc.want {
				t.Fatalf("TenantExists(%q) = %v, %v; want %v", tc.id, got, err, tc.want)
			}
		})
	}
}

func TestTenantDirectoryReportsAnOperationalFailure(t *testing.T) {
	e := newInvEnv(t, "")
	directory := e.directory
	e.pool.Close()

	_, err := directory.TenantExists(context.Background(), reqctx.TenantID(randomUUID(t)))

	if err == nil {
		t.Fatal("a closed pool answered without error")
	}
}
