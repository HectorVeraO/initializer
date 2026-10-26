package coreauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/featureflag"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
)

const (
	refreshPath = "/api/v1/auth/refresh"
	switchPath  = "/api/v1/auth/switch-tenant"
)

// perTenantFlags answers a different snapshot per tenant id.
type perTenantFlags map[reqctx.TenantID]map[featureflag.Key]bool

func (p perTenantFlags) Enabled(_ context.Context, _ featureflag.Key) bool { return false }

func (p perTenantFlags) Snapshot(_ context.Context, tenant *reqctx.TenantID) map[featureflag.Key]bool {
	if tenant == nil {
		return map[featureflag.Key]bool{}
	}
	return p[*tenant]
}

func (c *sessClient) switchTo(tenantID string) *httptest.ResponseRecorder {
	c.e.t.Helper()
	return c.switchBody(`{"tenantId":"`+tenantID+`"}`, nil)
}

func (c *sessClient) switchBody(body string, headers map[string]string) *httptest.ResponseRecorder {
	c.e.t.Helper()
	merged := map[string]string{csrfHeader: c.cookies["XSRF-TOKEN"]}
	for name, value := range headers {
		merged[name] = value
	}
	return c.doBody(http.MethodPost, switchPath, merged, body)
}

func (e *sessEnv) scanNullable(sql string, args ...any) string {
	e.t.Helper()
	var out *string
	err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&out)
	if err != nil {
		e.t.Fatalf("query %q: %v", sql, err)
	}
	if out == nil {
		return ""
	}
	return *out
}

func clearedNames(rec *httptest.ResponseRecorder) map[string]bool {
	cleared := map[string]bool{}
	for _, set := range rec.Result().Cookies() {
		if set.MaxAge < 0 {
			cleared[set.Name] = true
		}
	}
	return cleared
}

func expectNoCookies(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("cookies set: %v", setCookieNames(rec))
	}
}

func expectBothCleared(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	cleared := clearedNames(rec)
	if !cleared["gb_session"] || !cleared["XSRF-TOKEN"] {
		t.Errorf("cleared = %v, want gb_session and XSRF-TOKEN", cleared)
	}
}

// signedInOn signs p in with a membership on a new active tenant.
func signedInOn(e *sessEnv, p person, slug, role string) (*sessClient, string) {
	tenant := e.tenant(slug, "active")
	e.member(tenant, p.accountID, role, "active", e.clock.Now().Add(-time.Hour))
	return e.signIn(p), tenant
}

func errorObject(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &envelope)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return string(envelope.Error)
}

func TestRefreshKeepsTheChainTenantAndCSRFValue(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c, tenant := signedInOn(e, p, "acme", "admin")
	before := c.claims()
	csrfBefore := c.cookies["XSRF-TOKEN"]
	e.clock.advance(5 * time.Minute)

	rec := c.mutate(http.MethodPost, refreshPath)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	view := decodeData[viewReply](t, rec)
	if view.ActiveTenant == nil || view.ActiveTenant.ID != tenant {
		t.Errorf("activeTenant = %+v", view.ActiveTenant)
	}
	after := c.claims()
	if after.SessionID != before.SessionID || after.KratosSessionID != before.KratosSessionID || after.CSRF != before.CSRF {
		t.Errorf("the chain changed: before %+v after %+v", before, after)
	}
	if after.TenantID == nil || *after.TenantID != tenant || !after.IssuedAt.Equal(before.IssuedAt) {
		t.Errorf("tenant or iat changed: %+v", after)
	}
	if want := e.clock.Now().Add(10 * time.Minute); !after.ExpiresAt.Equal(want) {
		t.Errorf("exp = %v, want now + TTL = %v", after.ExpiresAt, want)
	}
	for _, set := range rec.Result().Cookies() {
		if set.Name == "XSRF-TOKEN" {
			t.Errorf("refresh must not rewrite the XSRF-TOKEN cookie")
		}
	}
	if c.cookies["XSRF-TOKEN"] != csrfBefore {
		t.Error("XSRF-TOKEN value changed")
	}
	if cache := rec.Header().Get("Cache-Control"); cache != "no-store" {
		t.Errorf("Cache-Control = %q", cache)
	}
	if calls := e.kratos.callsTo("/sessions/whoami"); len(calls) != 2 {
		t.Errorf("whoami calls = %d, want 2 (issue and refresh)", len(calls))
	}
}

func TestAMutationRetriedAfterRefreshStillPasses(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	c, _ := signedInOn(e, e.person(), "acme", "admin")
	preRefreshCSRF := c.cookies["XSRF-TOKEN"]
	e.clock.advance(11 * time.Minute)

	refreshed := c.do(http.MethodPost, refreshPath, map[string]string{csrfHeader: preRefreshCSRF})
	if refreshed.Code != http.StatusOK {
		t.Fatalf("refresh of an expired token: status %d: %s", refreshed.Code, refreshed.Body.String())
	}

	rec := c.do(http.MethodPost, "/api/v1/probe", map[string]string{csrfHeader: preRefreshCSRF})
	if rec.Code != http.StatusOK {
		t.Errorf("retried mutation: status %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRefreshReadsRoleAndPlatformFlagFromTheDatabase(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c, tenant := signedInOn(e, p, "acme", "admin")
	e.exec("UPDATE ten_memberships SET role = 'viewer' WHERE tenant_id = $1::uuid AND account_id = $2::uuid", tenant, p.accountID)
	e.exec("UPDATE iam_accounts SET is_platform_operator = true WHERE id = $1::uuid", p.accountID)

	rec := c.mutate(http.MethodPost, refreshPath)

	view := decodeData[viewReply](t, rec)
	claims := c.claims()
	if claims.Role == nil || *claims.Role != "viewer" || !claims.PlatformOperator {
		t.Errorf("claims role=%v plat=%v, want viewer and true", claims.Role, claims.PlatformOperator)
	}
	if view.Account.Role == nil || *view.Account.Role != "viewer" || !view.Account.IsPlatformOperator {
		t.Errorf("account = %+v", view.Account)
	}
}

func TestRefreshUpdatesTheStoredEmail(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c, _ := signedInOn(e, p, "acme", "admin")
	changed := "renamed-" + p.identityID[:8] + "@example.com"
	e.kratos.mu.Lock()
	for cookie, found := range e.kratos.sessions {
		found.email = changed
		e.kratos.sessions[cookie] = found
	}
	e.kratos.mu.Unlock()

	rec := c.mutate(http.MethodPost, refreshPath)

	view := decodeData[viewReply](t, rec)
	if view.Account.Email != changed {
		t.Errorf("email = %q, want %q", view.Account.Email, changed)
	}
}

func TestRefreshDoesNotApplyTheStaleTabGuard(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	c, _ := signedInOn(e, e.person(), "acme", "admin")

	rec := c.do(http.MethodPost, refreshPath, map[string]string{
		csrfHeader: c.cookies["XSRF-TOKEN"], "X-Active-Tenant-Id": randomUUID(t),
	})

	if rec.Code != http.StatusOK {
		t.Errorf("status %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRefreshRefusalsComeFromTheGuard(t *testing.T) {
	t.Run("missing csrf header", func(t *testing.T) {
		e := newSessEnv(t, sessEnvOptions{})
		c, _ := signedInOn(e, e.person(), "acme", "admin")
		rec := c.do(http.MethodPost, refreshPath, nil)
		expectError(t, rec, http.StatusForbidden, "CSRF_TOKEN_INVALID")
		expectNoCookies(t, rec)
	})
	t.Run("no session", func(t *testing.T) {
		e := newSessEnv(t, sessEnvOptions{})
		expectError(t, e.client().do(http.MethodPost, refreshPath, nil), http.StatusUnauthorized, "SESSION_INVALID")
	})
	t.Run("older than the refresh maximum age", func(t *testing.T) {
		e := newSessEnv(t, sessEnvOptions{})
		c, _ := signedInOn(e, e.person(), "acme", "admin")
		e.clock.advance(25 * time.Hour)
		rec := c.mutate(http.MethodPost, refreshPath)
		expectError(t, rec, http.StatusUnauthorized, "SESSION_INVALID")
		expectNoCookies(t, rec)
	})
}

func TestRefreshWithoutAnActiveKratosSessionClearsTheCookies(t *testing.T) {
	cases := map[string]func(e *sessEnv, c *sessClient){
		"no kratos cookie": func(_ *sessEnv, c *sessClient) { delete(c.cookies, kratosCookieName) },
		"unknown kratos session": func(_ *sessEnv, c *sessClient) {
			c.cookies[kratosCookieName] = "unknown-" + c.cookies[kratosCookieName]
		},
		"revoked kratos session": func(e *sessEnv, c *sessClient) {
			e.kratos.mu.Lock()
			delete(e.kratos.sessions, c.cookies[kratosCookieName])
			e.kratos.mu.Unlock()
		},
	}
	for name, mutateJar := range cases {
		t.Run(name, func(t *testing.T) {
			e := newSessEnv(t, sessEnvOptions{})
			c, _ := signedInOn(e, e.person(), "acme", "admin")
			mutateJar(e, c)

			rec := c.mutate(http.MethodPost, refreshPath)

			expectError(t, rec, http.StatusUnauthorized, "SESSION_INVALID")
			expectBothCleared(t, rec)
		})
	}
}

func TestRefreshWithAnotherIdentityClearsTheCookies(t *testing.T) {
	cases := map[string]func(t *testing.T, e *sessEnv) string{
		"linked to another account": func(_ *testing.T, e *sessEnv) string { return e.person().kratosCookie },
		"never linked": func(t *testing.T, e *sessEnv) string {
			identity := randomUUID(t)
			return e.kratos.login(t, identity, "stranger-"+identity[:8]+"@example.com")
		},
	}
	for name, otherCookie := range cases {
		t.Run(name, func(t *testing.T) {
			e := newSessEnv(t, sessEnvOptions{})
			c, _ := signedInOn(e, e.person(), "acme", "admin")
			c.cookies[kratosCookieName] = otherCookie(t, e)

			rec := c.mutate(http.MethodPost, refreshPath)

			expectError(t, rec, http.StatusUnauthorized, "SESSION_INVALID")
			expectBothCleared(t, rec)
		})
	}
	t.Run("an unlinked identity is not given an account", func(t *testing.T) {
		e := newSessEnv(t, sessEnvOptions{})
		c, _ := signedInOn(e, e.person(), "acme", "admin")
		identity := randomUUID(t)
		c.cookies[kratosCookieName] = e.kratos.login(t, identity, "stranger-"+identity[:8]+"@example.com")
		c.mutate(http.MethodPost, refreshPath)
		if count := e.scan("SELECT count(*)::text FROM iam_accounts WHERE kratos_identity_id = $1::uuid", identity); count != "0" {
			t.Errorf("accounts for the stranger = %s, want 0", count)
		}
	})
}

func TestRefreshAnswers503WhenKratosIsUnavailableAndTouchesNoCookie(t *testing.T) {
	cases := map[string]func(*sessEnv){
		"server error": func(e *sessEnv) { e.kratos.setWhoamiStatus(http.StatusInternalServerError) },
		"unreachable":  func(e *sessEnv) { e.kratos.server.Close() },
	}
	for name, breakKratos := range cases {
		t.Run(name, func(t *testing.T) {
			e := newSessEnv(t, sessEnvOptions{})
			c, _ := signedInOn(e, e.person(), "acme", "admin")
			breakKratos(e)

			rec := c.mutate(http.MethodPost, refreshPath)

			expectError(t, rec, http.StatusServiceUnavailable, "IDENTITY_PROVIDER_UNAVAILABLE")
			expectNoCookies(t, rec)
		})
	}
}

func TestRefreshStartsANewChainOnANewKratosLogin(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c, tenant := signedInOn(e, p, "acme", "admin")
	before := c.claims()
	c.cookies[kratosCookieName] = e.kratos.login(t, p.identityID, p.email)

	rec := c.mutate(http.MethodPost, refreshPath)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	after := c.claims()
	if after.SessionID == before.SessionID || after.CSRF == before.CSRF || after.KratosSessionID == before.KratosSessionID {
		t.Errorf("new Kratos login must start a new chain: before %+v after %+v", before, after)
	}
	if after.TenantID == nil || *after.TenantID != tenant {
		t.Errorf("tenant = %v, want the selected %s", after.TenantID, tenant)
	}
	if c.cookies["XSRF-TOKEN"] != after.CSRF {
		t.Error("the XSRF-TOKEN cookie must carry the new csrf")
	}
}

func TestRefreshOfANewKratosLoginOnALockedAccountIsRefusedAndClears(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c, _ := signedInOn(e, p, "acme", "admin")
	c.cookies[kratosCookieName] = e.kratos.login(t, p.identityID, p.email)
	e.exec("UPDATE iam_accounts SET status = 'locked' WHERE id = $1::uuid", p.accountID)

	rec := c.mutate(http.MethodPost, refreshPath)

	expectError(t, rec, http.StatusForbidden, "ACCOUNT_LOCKED_OR_DISABLED")
	expectBothCleared(t, rec)
}

func TestRefreshAccessRefusalsClearBothCookies(t *testing.T) {
	cases := []struct {
		name    string
		code    string
		degrade func(e *sessEnv, p person, tenant string)
	}{
		{"locked account", "ACCOUNT_LOCKED_OR_DISABLED", func(e *sessEnv, p person, _ string) {
			e.exec("UPDATE iam_accounts SET status = 'locked' WHERE id = $1::uuid", p.accountID)
		}},
		{"disabled account", "ACCOUNT_LOCKED_OR_DISABLED", func(e *sessEnv, p person, _ string) {
			e.exec("UPDATE iam_accounts SET status = 'disabled' WHERE id = $1::uuid", p.accountID)
		}},
		{"suspended tenant", "TENANT_SUSPENDED", func(e *sessEnv, _ person, tenant string) {
			e.exec("UPDATE ten_tenants SET status = 'suspended' WHERE id = $1::uuid", tenant)
		}},
		{"revoked membership", "MEMBERSHIP_REVOKED", func(e *sessEnv, p person, tenant string) {
			e.exec("UPDATE ten_memberships SET status = 'revoked' WHERE tenant_id = $1::uuid AND account_id = $2::uuid", tenant, p.accountID)
		}},
		{"suspended membership", "MEMBERSHIP_REVOKED", func(e *sessEnv, p person, tenant string) {
			e.exec("UPDATE ten_memberships SET status = 'suspended' WHERE tenant_id = $1::uuid AND account_id = $2::uuid", tenant, p.accountID)
		}},
		{"suspended tenant and revoked membership", "TENANT_SUSPENDED", func(e *sessEnv, p person, tenant string) {
			e.exec("UPDATE ten_tenants SET status = 'suspended' WHERE id = $1::uuid", tenant)
			e.exec("UPDATE ten_memberships SET status = 'revoked' WHERE tenant_id = $1::uuid AND account_id = $2::uuid", tenant, p.accountID)
		}},
		{"membership deleted", "TENANT_ACCESS_DENIED", func(e *sessEnv, p person, tenant string) {
			e.exec("DELETE FROM ten_memberships WHERE tenant_id = $1::uuid AND account_id = $2::uuid", tenant, p.accountID)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newSessEnv(t, sessEnvOptions{})
			p := e.person()
			c, tenant := signedInOn(e, p, "acme", "admin")
			tc.degrade(e, p, tenant)

			rec := c.mutate(http.MethodPost, refreshPath)

			expectError(t, rec, http.StatusForbidden, tc.code)
			expectBothCleared(t, rec)
		})
	}
}

func TestPlainSignInNeverFailsOnMembershipAfterARefusedRefresh(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c, tenant := signedInOn(e, p, "acme", "admin")
	e.exec("UPDATE ten_memberships SET status = 'revoked' WHERE tenant_id = $1::uuid AND account_id = $2::uuid", tenant, p.accountID)
	expectError(t, c.mutate(http.MethodPost, refreshPath), http.StatusForbidden, "MEMBERSHIP_REVOKED")

	rec := c.do(http.MethodGet, "/api/v1/auth/session", nil)

	view := decodeData[viewReply](t, rec)
	if view.ActiveTenant != nil || view.Account.Role != nil {
		t.Errorf("view = %+v, want tenantless", view)
	}
}

func TestRefreshOfATenantlessTokenSelectsATenantWhenOneExists(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c := e.signIn(p)
	if c.claims().TenantID != nil {
		t.Fatal("setup: the token must be tenantless")
	}
	tenant := e.tenant("acme", "active")
	e.member(tenant, p.accountID, "member", "active", e.clock.Now())
	before := c.claims()

	rec := c.mutate(http.MethodPost, refreshPath)

	view := decodeData[viewReply](t, rec)
	if view.ActiveTenant == nil || view.ActiveTenant.ID != tenant {
		t.Fatalf("activeTenant = %+v", view.ActiveTenant)
	}
	after := c.claims()
	if after.SessionID != before.SessionID || after.CSRF != before.CSRF {
		t.Error("the chain must be kept")
	}
}

func TestRefreshOfATenantlessTokenStaysTenantlessWithoutAUsableTenant(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	c := e.signIn(e.person())

	rec := c.mutate(http.MethodPost, refreshPath)

	view := decodeData[viewReply](t, rec)
	if view.ActiveTenant != nil || view.Account.Role != nil {
		t.Errorf("view = %+v", view)
	}
	if c.claims().TenantID != nil {
		t.Error("token must stay tenantless")
	}
}

func TestRefreshOfATenantlessTokenOnALockedAccountIsRefused(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c := e.signIn(p)
	e.exec("UPDATE iam_accounts SET status = 'locked' WHERE id = $1::uuid", p.accountID)

	rec := c.mutate(http.MethodPost, refreshPath)

	expectError(t, rec, http.StatusForbidden, "ACCOUNT_LOCKED_OR_DISABLED")
	expectBothCleared(t, rec)
}

func TestSwitchMovesTheSessionToTheTargetTenant(t *testing.T) {
	flags := perTenantFlags{}
	e := newSessEnv(t, sessEnvOptions{flags: flags})
	p := e.person()
	c, home := signedInOn(e, p, "tenant-a", "admin")
	target := e.tenant("tenant-b", "active")
	e.member(target, p.accountID, "viewer", "active", e.clock.Now())
	flags[reqctx.TenantID(home)] = map[featureflag.Key]bool{"beta": false}
	flags[reqctx.TenantID(target)] = map[featureflag.Key]bool{"beta": true}
	before := c.claims()
	csrfBefore := c.cookies["XSRF-TOKEN"]

	rec := c.switchTo(strings.ToUpper(target))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	view := decodeData[viewReply](t, rec)
	if view.ActiveTenant == nil || view.ActiveTenant.ID != target || view.ActiveTenant.Slug != "tenant-b" {
		t.Errorf("activeTenant = %+v", view.ActiveTenant)
	}
	if view.Account.Role == nil || *view.Account.Role != "viewer" {
		t.Errorf("account.role = %v, want the role in the target tenant", view.Account.Role)
	}
	if !view.Flags["beta"] {
		t.Errorf("flags = %v, want the target tenant's snapshot", view.Flags)
	}
	if len(view.Memberships) != 2 {
		t.Errorf("memberships = %d, want 2", len(view.Memberships))
	}
	after := c.claims()
	if after.SessionID != before.SessionID || after.KratosSessionID != before.KratosSessionID || after.CSRF != before.CSRF || !after.IssuedAt.Equal(before.IssuedAt) {
		t.Errorf("the chain changed: before %+v after %+v", before, after)
	}
	if after.TenantID == nil || *after.TenantID != target || after.TenantSlug == nil || *after.TenantSlug != "tenant-b" {
		t.Errorf("tenant claims = %+v", after)
	}
	if c.cookies["XSRF-TOKEN"] != csrfBefore {
		t.Error("the XSRF-TOKEN value must not change")
	}
	last := e.scan("SELECT last_active_tenant_id::text FROM iam_accounts WHERE id = $1::uuid", p.accountID)
	if last != target {
		t.Errorf("last_active_tenant_id = %s, want %s", last, target)
	}
	if cache := rec.Header().Get("Cache-Control"); cache != "no-store" {
		t.Errorf("Cache-Control = %q", cache)
	}
	probe := decodeData[map[string]any](t, c.do(http.MethodGet, "/api/v1/probe", nil))
	if probe["tenantSlug"] != "tenant-b" {
		t.Errorf("probe tenant = %v", probe["tenantSlug"])
	}
}

func TestSwitchMakesNoKratosCall(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c, _ := signedInOn(e, p, "tenant-a", "admin")
	target := e.tenant("tenant-b", "active")
	e.member(target, p.accountID, "member", "active", e.clock.Now())
	before := len(e.kratos.callsTo("/sessions/whoami"))
	e.kratos.setWhoamiStatus(http.StatusInternalServerError)

	rec := c.switchTo(target)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if after := len(e.kratos.callsTo("/sessions/whoami")); after != before {
		t.Errorf("whoami calls %d -> %d, switch must not call Kratos", before, after)
	}
}

func TestSwitchNeverOutlivesTheOldToken(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c, _ := signedInOn(e, p, "tenant-a", "admin")
	target := e.tenant("tenant-b", "active")
	e.member(target, p.accountID, "member", "active", e.clock.Now())
	oldExp := c.claims().ExpiresAt
	e.clock.advance(9 * time.Minute)

	rec := c.switchTo(target)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if got := c.claims().ExpiresAt; !got.Equal(oldExp) {
		t.Errorf("exp = %v, want the old exp %v (now + TTL would be %v)", got, oldExp, e.clock.Now().Add(10*time.Minute))
	}
}

func TestSwitchCheckAccessOrder(t *testing.T) {
	cases := []struct {
		name    string
		code    string
		degrade func(e *sessEnv, p person, target string)
	}{
		{"locked account", "ACCOUNT_LOCKED_OR_DISABLED", func(e *sessEnv, p person, _ string) {
			e.exec("UPDATE iam_accounts SET status = 'locked' WHERE id = $1::uuid", p.accountID)
		}},
		{"tenant suspended", "TENANT_SUSPENDED", func(e *sessEnv, _ person, target string) {
			e.exec("UPDATE ten_tenants SET status = 'suspended' WHERE id = $1::uuid", target)
		}},
		{"tenant suspended wins over a revoked membership", "TENANT_SUSPENDED", func(e *sessEnv, p person, target string) {
			e.exec("UPDATE ten_tenants SET status = 'suspended' WHERE id = $1::uuid", target)
			e.exec("UPDATE ten_memberships SET status = 'revoked' WHERE tenant_id = $1::uuid AND account_id = $2::uuid", target, p.accountID)
		}},
		{"membership revoked", "MEMBERSHIP_REVOKED", func(e *sessEnv, p person, target string) {
			e.exec("UPDATE ten_memberships SET status = 'revoked' WHERE tenant_id = $1::uuid AND account_id = $2::uuid", target, p.accountID)
		}},
		{"membership suspended", "MEMBERSHIP_REVOKED", func(e *sessEnv, p person, target string) {
			e.exec("UPDATE ten_memberships SET status = 'suspended' WHERE tenant_id = $1::uuid AND account_id = $2::uuid", target, p.accountID)
		}},
		{"not a member", "TENANT_ACCESS_DENIED", func(e *sessEnv, p person, target string) {
			e.exec("DELETE FROM ten_memberships WHERE tenant_id = $1::uuid AND account_id = $2::uuid", target, p.accountID)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newSessEnv(t, sessEnvOptions{})
			p := e.person()
			c, home := signedInOn(e, p, "tenant-a", "admin")
			target := e.tenant("tenant-b", "active")
			e.member(target, p.accountID, "member", "active", e.clock.Now())
			tc.degrade(e, p, target)
			cookiesBefore := map[string]string{}
			for name, value := range c.cookies {
				cookiesBefore[name] = value
			}

			rec := c.switchTo(target)

			expectError(t, rec, http.StatusForbidden, tc.code)
			expectNoCookies(t, rec)
			for name, value := range cookiesBefore {
				if c.cookies[name] != value {
					t.Errorf("cookie %s changed on a refused switch", name)
				}
			}
			last := e.scanNullable("SELECT last_active_tenant_id::text FROM iam_accounts WHERE id = $1::uuid", p.accountID)
			if last == target {
				t.Error("a refused switch must not record the tenant as last active")
			}
			if claims := c.claims(); claims.TenantID == nil || *claims.TenantID != home {
				t.Errorf("the session left its tenant: %+v", claims)
			}
		})
	}
}

func TestSwitchAnswersTheSameBytesForANonMemberAndAnUnknownTenant(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	c, _ := signedInOn(e, e.person(), "tenant-a", "admin")
	foreign := e.tenant("tenant-b", "active")

	foreignRec := c.switchTo(foreign)
	unknownRec := c.switchTo(randomUUID(t))

	expectError(t, foreignRec, http.StatusForbidden, "TENANT_ACCESS_DENIED")
	expectError(t, unknownRec, http.StatusForbidden, "TENANT_ACCESS_DENIED")
	if a, b := errorObject(t, foreignRec), errorObject(t, unknownRec); a != b {
		t.Errorf("error objects differ:\n%s\n%s", a, b)
	}
}

func TestSwitchValidatesTheBody(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		issue string
	}{
		{"empty object", `{}`, "FIELD_REQUIRED"},
		{"null", `{"tenantId":null}`, "FIELD_REQUIRED"},
		{"number", `{"tenantId":5}`, "FIELD_TYPE_INVALID"},
		{"object", `{"tenantId":{}}`, "FIELD_TYPE_INVALID"},
		{"not a uuid", `{"tenantId":"not-a-uuid"}`, "FIELD_INVALID_FORMAT"},
		{"empty string", `{"tenantId":""}`, "FIELD_INVALID_FORMAT"},
		{"braced uuid", `{"tenantId":"{0b9c4c6e-0000-4000-8000-0000000000aa}"}`, "FIELD_INVALID_FORMAT"},
		{"malformed json", `{"tenantId":`, "BODY_MALFORMED"},
		{"not an object", `[]`, "BODY_MALFORMED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newSessEnv(t, sessEnvOptions{})
			c, _ := signedInOn(e, e.person(), "tenant-a", "admin")

			rec := c.switchBody(tc.body, nil)

			expectError(t, rec, http.StatusUnprocessableEntity, "VALIDATION_FAILED")
			var envelope api.Envelope[json.RawMessage]
			_ = json.Unmarshal(rec.Body.Bytes(), &envelope)
			if len(envelope.Error.Issues) != 1 || string(envelope.Error.Issues[0].Code) != tc.issue {
				t.Errorf("issues = %+v, want one %s", envelope.Error.Issues, tc.issue)
			}
			expectNoCookies(t, rec)
		})
	}
}

func TestSwitchRefusesAWrongMediaTypeAndAnOversizedBody(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	c, _ := signedInOn(e, e.person(), "tenant-a", "admin")
	csrf := map[string]string{csrfHeader: c.cookies["XSRF-TOKEN"]}

	noType := httptest.NewRequest(http.MethodPost, switchPath, strings.NewReader(`{"tenantId":"x"}`))
	noType.AddCookie(&http.Cookie{Name: "gb_session", Value: c.cookies["gb_session"]})
	noType.Header.Set(csrfHeader, csrf[csrfHeader])
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, noType)
	expectError(t, rec, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE")

	huge := `{"tenantId":"` + strings.Repeat("a", api.MaxBodyBytes+1) + `"}`
	expectError(t, c.doBody(http.MethodPost, switchPath, csrf, huge), http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE")
}

func TestSwitchRefusalsComeFromTheGuard(t *testing.T) {
	t.Run("missing csrf header", func(t *testing.T) {
		e := newSessEnv(t, sessEnvOptions{})
		p := e.person()
		c, home := signedInOn(e, p, "tenant-a", "admin")
		target := e.tenant("tenant-b", "active")
		e.member(target, p.accountID, "member", "active", e.clock.Now())
		rec := c.doBody(http.MethodPost, switchPath, nil, `{"tenantId":"`+target+`"}`)
		expectError(t, rec, http.StatusForbidden, "CSRF_TOKEN_INVALID")
		expectNoCookies(t, rec)
		if claims := c.claims(); claims.TenantID == nil || *claims.TenantID != home {
			t.Error("the tenant changed")
		}
	})
	t.Run("another session's csrf value", func(t *testing.T) {
		e := newSessEnv(t, sessEnvOptions{})
		p := e.person()
		a, _ := signedInOn(e, p, "tenant-a", "admin")
		b := e.signIn(p)
		rec := b.doBody(http.MethodPost, switchPath, map[string]string{csrfHeader: a.cookies["XSRF-TOKEN"]}, `{"tenantId":"`+randomUUID(t)+`"}`)
		expectError(t, rec, http.StatusForbidden, "CSRF_TOKEN_INVALID")
	})
	t.Run("stale tab", func(t *testing.T) {
		e := newSessEnv(t, sessEnvOptions{})
		p := e.person()
		c, home := signedInOn(e, p, "tenant-a", "admin")
		target := e.tenant("tenant-b", "active")
		e.member(target, p.accountID, "member", "active", e.clock.Now())

		rec := c.switchBody(`{"tenantId":"`+target+`"}`, map[string]string{"X-Active-Tenant-Id": target})

		expectError(t, rec, http.StatusConflict, "ACTIVE_TENANT_CHANGED")
		expectNoCookies(t, rec)
		if claims := c.claims(); claims.TenantID == nil || *claims.TenantID != home {
			t.Error("the tenant changed")
		}
		okRec := c.switchBody(`{"tenantId":"`+target+`"}`, map[string]string{"X-Active-Tenant-Id": home})
		if okRec.Code != http.StatusOK {
			t.Errorf("with the matching header: status %d", okRec.Code)
		}
	})
	t.Run("expired token", func(t *testing.T) {
		e := newSessEnv(t, sessEnvOptions{})
		c, _ := signedInOn(e, e.person(), "tenant-a", "admin")
		e.clock.advance(11 * time.Minute)
		rec := c.switchTo(randomUUID(t))
		expectError(t, rec, http.StatusUnauthorized, "SESSION_INVALID")
		expectNoCookies(t, rec)
	})
}

func TestTheOldTokenKeepsItsTenantAfterASwitchInAnotherJar(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	a, _ := signedInOn(e, p, "tenant-a", "admin")
	target := e.tenant("tenant-b", "active")
	e.member(target, p.accountID, "member", "active", e.clock.Now())
	b := e.client()
	for name, value := range a.cookies {
		b.cookies[name] = value
	}

	if rec := a.switchTo(target); rec.Code != http.StatusOK {
		t.Fatalf("switch: status %d: %s", rec.Code, rec.Body.String())
	}

	aProbe := decodeData[map[string]any](t, a.do(http.MethodGet, "/api/v1/probe", nil))
	bProbe := decodeData[map[string]any](t, b.do(http.MethodGet, "/api/v1/probe", nil))
	if aProbe["tenantSlug"] != "tenant-b" || bProbe["tenantSlug"] != "tenant-a" {
		t.Errorf("tenants a=%v b=%v, want tenant-b and tenant-a", aProbe["tenantSlug"], bProbe["tenantSlug"])
	}
}

func TestASwitchedSessionRefreshesOnItsNewTenant(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c, _ := signedInOn(e, p, "tenant-a", "admin")
	target := e.tenant("tenant-b", "active")
	e.member(target, p.accountID, "member", "active", e.clock.Now())
	c.switchTo(target)

	view := decodeData[viewReply](t, c.mutate(http.MethodPost, refreshPath))

	if view.ActiveTenant == nil || view.ActiveTenant.ID != target {
		t.Errorf("activeTenant = %+v, want the switched tenant", view.ActiveTenant)
	}
}

func TestSwitchOutOfATenantlessSession(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c := e.signIn(p)
	target := e.tenant("tenant-b", "active")
	e.member(target, p.accountID, "member", "active", e.clock.Now())

	rec := c.switchTo(target)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if claims := c.claims(); claims.TenantID == nil || *claims.TenantID != target {
		t.Errorf("claims = %+v", claims)
	}
}

func FuzzParseSwitchTenantID(f *testing.F) {
	seeds := []string{
		`{"tenantId":"0b9c4c6e-0000-4000-8000-0000000000aa"}`, `{}`, `{"tenantId":5}`, `{"tenantId":null}`,
		`{"tenantId":"not-a-uuid"}`, `[`, ``, `{"tenantId":"` + strings.Repeat("a", 64) + `"}`, "\x00\xff", `{"tenantId":["x"]}`,
	}
	for _, seed := range seeds {
		f.Add(seed, true)
		f.Add(seed, false)
	}
	f.Fuzz(func(t *testing.T, body string, hasJSONType bool) {
		req := httptest.NewRequest(http.MethodPost, switchPath, strings.NewReader(body))
		if hasJSONType {
			req.Header.Set("Content-Type", "application/json")
		}
		id, err := parseSwitchTenantID(req)
		if err == nil {
			canonical, ok := api.ParseUUID(id)
			if !ok || canonical != id {
				t.Fatalf("accepted %q which is not a canonical UUID", id)
			}
			return
		}
		var apiErr *api.Error
		if !errors.As(err, &apiErr) {
			t.Fatalf("error %v is not an *api.Error", err)
		}
		allowed := apiErr.Status == http.StatusUnprocessableEntity || apiErr.Status == http.StatusUnsupportedMediaType ||
			apiErr.Status == http.StatusRequestEntityTooLarge
		if !allowed || id != "" {
			t.Fatalf("status %d with id %q", apiErr.Status, id)
		}
	})
}
