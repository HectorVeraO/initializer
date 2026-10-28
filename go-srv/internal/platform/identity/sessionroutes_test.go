package identity

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestIssueMintsASessionFromTheKratosCookie(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	acme := e.tenant("acme", "active")
	e.member(acme, p.accountID, "admin", "active", e.clock.Now())
	c := e.client()
	c.cookies[kratosCookieName] = p.kratosCookie

	rec := c.do(http.MethodGet, "/api/v1/auth/session", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	view := decodeData[viewReply](t, rec)
	if view.Account.ID != p.accountID || view.Account.Email != p.email {
		t.Errorf("account = %+v", view.Account)
	}
	if view.Account.Role == nil || *view.Account.Role != "admin" {
		t.Errorf("account.role = %v, want admin", view.Account.Role)
	}
	if view.ActiveTenant == nil || view.ActiveTenant.Slug != "acme" {
		t.Errorf("activeTenant = %+v", view.ActiveTenant)
	}
	if len(view.Memberships) != 1 || view.Memberships[0].Tenant.Slug != "acme" || view.Memberships[0].Role != "admin" {
		t.Errorf("memberships = %+v", view.Memberships)
	}
	if view.Flags == nil {
		t.Error("flags must be an object, got null")
	}
	claims := c.claims()
	if claims.KratosSessionID == "" || claims.CSRF != c.cookies["XSRF-TOKEN"] {
		t.Errorf("claims do not bind the kratos session and the csrf cookie: %+v", claims)
	}
	if claims.ExpiresAt.Sub(e.clock.Now()) != 10*time.Minute {
		t.Errorf("exp = now + %v, want the 10m TTL", claims.ExpiresAt.Sub(e.clock.Now()))
	}
}

func TestIssueCookieAttributes(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c := e.client()
	c.cookies[kratosCookieName] = p.kratosCookie

	rec := c.do(http.MethodGet, "/api/v1/auth/session", nil)

	byName := map[string]*http.Cookie{}
	for _, set := range rec.Result().Cookies() {
		byName[set.Name] = set
	}
	sessionCookie, csrfCookie := byName["app_session"], byName["XSRF-TOKEN"]
	if sessionCookie == nil || csrfCookie == nil {
		t.Fatalf("cookies set: %v", setCookieNames(rec))
	}
	if !sessionCookie.HttpOnly || csrfCookie.HttpOnly {
		t.Errorf("HttpOnly session=%v csrf=%v, want true/false", sessionCookie.HttpOnly, csrfCookie.HttpOnly)
	}
	for _, set := range []*http.Cookie{sessionCookie, csrfCookie} {
		if set.Path != "/" || set.SameSite != http.SameSiteLaxMode || set.Domain != "" || set.MaxAge != 86400 {
			t.Errorf("%s attributes = %+v", set.Name, set)
		}
	}
	if cache := rec.Header().Get("Cache-Control"); cache != "no-store" {
		t.Errorf("Cache-Control = %q", cache)
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Cookie") {
		t.Errorf("Vary = %q", rec.Header().Get("Vary"))
	}
}

func TestIssueForwardsOnlyTheKratosCookie(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c := e.client()
	c.cookies[kratosCookieName] = p.kratosCookie
	c.cookies["unrelated"] = "x"

	c.do(http.MethodGet, "/api/v1/auth/session", nil)

	calls := e.kratos.callsTo("/sessions/whoami")
	if len(calls) != 1 || calls[0].cookies != kratosCookieName+"="+p.kratosCookie {
		t.Fatalf("whoami calls = %s", describeCalls(calls))
	}
}

func TestIssueReturnsFlagsFromTheEvaluator(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{flags: mapFlags{"new_settings_ui": true}})
	p := e.person()
	c := e.client()
	c.cookies[kratosCookieName] = p.kratosCookie

	view := decodeData[viewReply](t, c.do(http.MethodGet, "/api/v1/auth/session", nil))

	if !view.Flags["new_settings_ui"] {
		t.Errorf("flags = %v", view.Flags)
	}
}

func TestIssueWithoutAMembershipIsTenantless(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	revoked := e.tenant("gone", "active")
	e.member(revoked, p.accountID, "member", "revoked", e.clock.Now())
	c := e.client()
	c.cookies[kratosCookieName] = p.kratosCookie

	rec := c.do(http.MethodGet, "/api/v1/auth/session", nil)

	view := decodeData[viewReply](t, rec)
	if view.ActiveTenant != nil || view.Account.Role != nil || len(view.Memberships) != 0 {
		t.Errorf("view = %+v", view)
	}
	if c.claims().TenantID != nil {
		t.Error("token must be tenantless")
	}
}

func TestIssueFallsBackToAnotherActiveMembership(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	suspended := e.tenant("suspended-co", "suspended")
	healthy := e.tenant("healthy-co", "active")
	e.member(suspended, p.accountID, "admin", "active", e.clock.Now().Add(-2*time.Hour))
	e.member(healthy, p.accountID, "viewer", "active", e.clock.Now().Add(-time.Hour))
	e.remember(p.accountID, suspended)
	c := e.client()
	c.cookies[kratosCookieName] = p.kratosCookie

	view := decodeData[viewReply](t, c.do(http.MethodGet, "/api/v1/auth/session", nil))

	if view.ActiveTenant == nil || view.ActiveTenant.Slug != "healthy-co" {
		t.Fatalf("activeTenant = %+v", view.ActiveTenant)
	}
	if view.Account.Role == nil || *view.Account.Role != "viewer" {
		t.Errorf("role = %v", view.Account.Role)
	}
	if len(view.Memberships) != 2 || view.Memberships[0].Tenant.Slug != "healthy-co" ||
		view.Memberships[1].Tenant.Slug != "suspended-co" {
		t.Errorf("memberships must list every active membership ordered by slug: %+v", view.Memberships)
	}
}

func TestIssueWithoutAKratosCookieIsSessionInvalid(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	c := e.client()

	rec := c.do(http.MethodGet, "/api/v1/auth/session", nil)

	expectError(t, rec, http.StatusUnauthorized, "SESSION_INVALID")
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("cookies set: %v", setCookieNames(rec))
	}
	if calls := e.kratos.callsTo("/sessions/whoami"); len(calls) != 0 {
		t.Errorf("Kratos was called without a cookie: %s", describeCalls(calls))
	}
}

func TestIssueWithAnUnknownKratosSessionIsSessionInvalid(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	c := e.client()
	c.cookies[kratosCookieName] = "not-a-session"

	expectError(t, c.do(http.MethodGet, "/api/v1/auth/session", nil), http.StatusUnauthorized, "SESSION_INVALID")
}

func TestIssueTreatsATamperedBFFCookieAsNoSession(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c := e.signIn(p)
	token := c.cookies["app_session"]
	flipped := token[:len(token)-2] + "AA"
	if flipped == token {
		flipped = token[:len(token)-2] + "BB"
	}
	fresh := e.client()
	fresh.cookies["app_session"] = flipped

	expectError(t, fresh.do(http.MethodGet, "/api/v1/auth/session", nil), http.StatusUnauthorized, "SESSION_INVALID")
}

func TestIssueAnswers503WhenKratosIsUnavailable(t *testing.T) {
	cases := map[string]func(*sessEnv){
		"server error": func(e *sessEnv) { e.kratos.setWhoamiStatus(http.StatusInternalServerError) },
		"unreachable":  func(e *sessEnv) { e.kratos.server.Close() },
	}
	for name, breakKratos := range cases {
		t.Run(name, func(t *testing.T) {
			e := newSessEnv(t, sessEnvOptions{})
			p := e.person()
			breakKratos(e)
			c := e.client()
			c.cookies[kratosCookieName] = p.kratosCookie

			rec := c.do(http.MethodGet, "/api/v1/auth/session", nil)

			expectError(t, rec, http.StatusServiceUnavailable, "IDENTITY_PROVIDER_UNAVAILABLE")
			if len(rec.Result().Cookies()) != 0 {
				t.Errorf("cookies set: %v", setCookieNames(rec))
			}
		})
	}
}

func TestIssueRefusesLockedAndDisabledAccountsWithoutCookies(t *testing.T) {
	for _, status := range []string{"locked", "disabled"} {
		t.Run(status, func(t *testing.T) {
			e := newSessEnv(t, sessEnvOptions{})
			p := e.person()
			e.exec("UPDATE iam_accounts SET status = $2 WHERE id = $1::uuid", p.accountID, status)
			c := e.client()
			c.cookies[kratosCookieName] = p.kratosCookie

			rec := c.do(http.MethodGet, "/api/v1/auth/session", nil)

			expectError(t, rec, http.StatusForbidden, "ACCOUNT_LOCKED_OR_DISABLED")
			if len(rec.Result().Cookies()) != 0 {
				t.Errorf("cookies set: %v", setCookieNames(rec))
			}
		})
	}
}

func TestIssueLinksANewIdentityOnFirstSight(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	identity := randomUUID(t)
	email := "Fresh-" + identity[:8] + "@Example.com"
	cookie := e.kratos.login(t, identity, email)
	c := e.client()
	c.cookies[kratosCookieName] = cookie

	view := decodeData[viewReply](t, c.do(http.MethodGet, "/api/v1/auth/session", nil))

	if view.Account.Email != strings.ToLower(email) {
		t.Errorf("email = %q", view.Account.Email)
	}
}

func TestAVerifyingTokenIsAnsweredWithoutKratosOrNewCookies(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	acme := e.tenant("acme", "active")
	e.member(acme, p.accountID, "member", "active", e.clock.Now())
	c := e.signIn(p)
	delete(c.cookies, kratosCookieName)
	e.kratos.setWhoamiStatus(http.StatusInternalServerError)

	rec := c.do(http.MethodGet, "/api/v1/auth/session", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("cookies set: %v", setCookieNames(rec))
	}
	if calls := e.kratos.callsTo("/sessions/whoami"); len(calls) != 1 {
		t.Errorf("whoami calls = %d, want 1 (the issue)", len(calls))
	}
}

func TestATenantlessTokenMovesOntoATenantKeepingTheChain(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c := e.signIn(p)
	before := c.claims()
	if before.TenantID != nil {
		t.Fatal("setup: token must start tenantless")
	}
	acme := e.tenant("acme", "active")
	e.member(acme, p.accountID, "admin", "active", e.clock.Now())
	e.clock.advance(4 * time.Minute)

	rec := c.do(http.MethodGet, "/api/v1/auth/session", nil)

	view := decodeData[viewReply](t, rec)
	if view.ActiveTenant == nil || view.ActiveTenant.Slug != "acme" {
		t.Fatalf("activeTenant = %+v", view.ActiveTenant)
	}
	after := c.claims()
	if after.SessionID != before.SessionID || after.KratosSessionID != before.KratosSessionID || after.CSRF != before.CSRF {
		t.Errorf("sid, ksid and csrf must be kept: before %+v after %+v", before, after)
	}
	if after.TenantID == nil || *after.Role != "admin" || *after.TenantSlug != "acme" {
		t.Errorf("claims = %+v", after)
	}
	// now + TTL would be 14 minutes after the first issue; the old exp (10) caps it.
	if !after.ExpiresAt.Equal(before.ExpiresAt) {
		t.Errorf("exp = %v, want the old exp %v", after.ExpiresAt, before.ExpiresAt)
	}
	if len(setCookieNames(rec)) != 2 {
		t.Errorf("cookies set: %v", setCookieNames(rec))
	}
}

func TestATenantlessTokenStaysWhenNoTenantIsUsable(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c := e.signIn(p)

	rec := c.do(http.MethodGet, "/api/v1/auth/session", nil)

	if rec.Code != http.StatusOK || len(rec.Result().Cookies()) != 0 {
		t.Errorf("status %d, cookies %v", rec.Code, setCookieNames(rec))
	}
}

func TestAnExpiredTokenStartsANewChain(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c := e.signIn(p)
	before := c.claims()
	e.clock.advance(11 * time.Minute)

	rec := c.do(http.MethodGet, "/api/v1/auth/session", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	after := c.claims()
	if after.SessionID == before.SessionID || after.CSRF == before.CSRF {
		t.Error("an expired token must start a new chain (new sid and csrf)")
	}
	if calls := e.kratos.callsTo("/sessions/whoami"); len(calls) != 2 {
		t.Errorf("whoami calls = %d, want 2", len(calls))
	}
}

func TestAnExpiredTokenWithoutAKratosSessionIsSessionInvalid(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	c := e.signIn(e.person())
	delete(c.cookies, kratosCookieName)
	e.clock.advance(11 * time.Minute)

	expectError(t, c.do(http.MethodGet, "/api/v1/auth/session", nil), http.StatusUnauthorized, "SESSION_INVALID")
}

func TestWrongMethodOnTheSessionPathFallsBackToAnEnvelope404(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	c := e.client()

	expectError(t, c.do(http.MethodDelete, "/api/v1/auth/session", nil), http.StatusNotFound, "ROUTE_NOT_FOUND")
	expectError(t, c.do(http.MethodGet, "/api/v1/auth/logout", nil), http.StatusNotFound, "ROUTE_NOT_FOUND")
}

func TestLogoutRevokesKratosAndClearsEveryCookie(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c := e.signIn(p)

	rec := c.mutate(http.MethodPost, "/api/v1/auth/logout")

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeData[map[string]string](t, rec)
	if result["logoutUrl"] != sessTestPublicURL+"/login" {
		t.Errorf("logoutUrl = %q", result["logoutUrl"])
	}
	cleared := map[string]bool{}
	for _, set := range rec.Result().Cookies() {
		if set.MaxAge < 0 && set.Value == "" && set.Path == "/" {
			cleared[set.Name] = true
		}
	}
	for _, name := range []string{"app_session", "XSRF-TOKEN", kratosCookieName} {
		if !cleared[name] {
			t.Errorf("cookie %s not cleared; Set-Cookie: %v", name, rec.Header().Values("Set-Cookie"))
		}
	}
	if calls := e.kratos.callsTo("/self-service/logout"); len(calls) != 1 ||
		calls[0].cookies != kratosCookieName+"="+p.kratosCookie {
		t.Errorf("kratos logout calls = %s", describeCalls(calls))
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
}

func TestLogoutWithoutAKratosCookieSkipsKratos(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	c := e.signIn(e.person())
	delete(c.cookies, kratosCookieName)

	rec := c.mutate(http.MethodPost, "/api/v1/auth/logout")

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if calls := e.kratos.callsTo("/self-service/logout/browser"); len(calls) != 0 {
		t.Errorf("kratos called: %s", describeCalls(calls))
	}
}

func TestLogoutContinuesWhenKratosFailsOrKnowsNoSession(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusUnauthorized} {
		e := newSessEnv(t, sessEnvOptions{})
		c := e.signIn(e.person())
		e.kratos.setLogoutStatus(status)

		rec := c.mutate(http.MethodPost, "/api/v1/auth/logout")

		if rec.Code != http.StatusOK {
			t.Errorf("kratos logout %d: status %d", status, rec.Code)
		}
		warned := strings.Contains(e.logs.String(), "kratos_logout_failed")
		if warned != (status == http.StatusInternalServerError) {
			t.Errorf("kratos logout %d: kratos_logout_failed logged = %v", status, warned)
		}
	}
}

func TestLogoutRefusalsComeFromTheGuardAndClearNothing(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()

	t.Run("no cookie", func(t *testing.T) {
		rec := e.client().do(http.MethodPost, "/api/v1/auth/logout", nil)
		expectError(t, rec, http.StatusUnauthorized, "SESSION_INVALID")
		if len(rec.Result().Cookies()) != 0 {
			t.Errorf("cookies set: %v", setCookieNames(rec))
		}
	})
	t.Run("tampered cookie", func(t *testing.T) {
		c := e.client()
		c.cookies["app_session"] = e.signIn(p).cookies["app_session"] + "x"
		c.cookies["XSRF-TOKEN"] = "whatever"
		rec := c.do(http.MethodPost, "/api/v1/auth/logout", map[string]string{csrfHeader: "whatever"})
		expectError(t, rec, http.StatusUnauthorized, "SESSION_INVALID")
		if len(rec.Result().Cookies()) != 0 {
			t.Errorf("cookies set: %v", setCookieNames(rec))
		}
	})
	t.Run("missing and wrong csrf header", func(t *testing.T) {
		c := e.signIn(p)
		for _, headers := range []map[string]string{nil, {csrfHeader: "wrong"}} {
			rec := c.do(http.MethodPost, "/api/v1/auth/logout", headers)
			expectError(t, rec, http.StatusForbidden, "CSRF_TOKEN_INVALID")
			if len(rec.Result().Cookies()) != 0 {
				t.Errorf("cookies set: %v", setCookieNames(rec))
			}
		}
		if calls := e.kratos.callsTo("/self-service/logout"); len(calls) != 0 {
			t.Errorf("the Kratos session was revoked despite the refusal: %s", describeCalls(calls))
		}
		if c.do(http.MethodGet, "/api/v1/auth/session", nil).Code != http.StatusOK {
			t.Error("the session must survive a refused logout")
		}
	})
}

func TestLogoutAcceptsAnExpiredSessionOnlyWithinTheRefreshMaxAge(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()

	within := e.signIn(p)
	e.clock.advance(2 * time.Hour)
	if rec := within.mutate(http.MethodPost, "/api/v1/auth/logout"); rec.Code != http.StatusOK {
		t.Errorf("expired within max age: status %d", rec.Code)
	}

	beyond := e.signIn(e.person())
	e.clock.advance(25 * time.Hour)
	rec := beyond.mutate(http.MethodPost, "/api/v1/auth/logout")
	expectError(t, rec, http.StatusUnauthorized, "SESSION_INVALID")
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("cookies set: %v", setCookieNames(rec))
	}
}

func TestAfterLogoutTheSameJarCannotReissue(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c := e.signIn(p)
	if rec := c.mutate(http.MethodPost, "/api/v1/auth/logout"); rec.Code != http.StatusOK {
		t.Fatalf("logout status %d", rec.Code)
	}

	// Replaying the pre-logout Kratos cookie in a fresh jar only fails when
	// logout revoked the Kratos session.
	replay := e.client()
	replay.cookies[kratosCookieName] = p.kratosCookie
	expectError(t, replay.do(http.MethodGet, "/api/v1/auth/session", nil), http.StatusUnauthorized, "SESSION_INVALID")
}

func TestKratosCallsCarryTheTraceparent(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c := e.client()
	c.cookies[kratosCookieName] = p.kratosCookie

	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	incoming := map[string]string{"traceparent": "00-" + traceID + "-00f067aa0ba902b7-01"}
	c.do(http.MethodGet, "/api/v1/auth/session", incoming)
	c.do(http.MethodGet, "/kratos/self-service/login/browser", incoming)

	whoami := e.kratos.callsTo("/sessions/whoami")
	if len(whoami) != 1 || !strings.HasPrefix(whoami[0].traceparent, "00-"+traceID+"-") {
		t.Errorf("whoami must continue the incoming trace: %s", describeCalls(whoami))
	}
	proxied := e.kratos.callsTo("/self-service/login/browser")
	if len(proxied) != 1 || !strings.HasPrefix(proxied[0].traceparent, "00-"+traceID+"-") {
		t.Errorf("proxied call must continue the incoming trace: %s", describeCalls(proxied))
	}
}

func TestTheKratosProxyIsMountedAndStripsBFFCookies(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	c := e.client()
	c.cookies["app_session"] = "bff-token"
	c.cookies["XSRF-TOKEN"] = "csrf"
	c.cookies["other"] = "kept"

	rec := c.do(http.MethodGet, "/kratos/self-service/login/browser", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	calls := e.kratos.callsTo("/self-service/login/browser")
	if len(calls) != 1 || calls[0].cookies != "other=kept" {
		t.Errorf("forwarded cookies = %s", describeCalls(calls))
	}
	if rec2 := c.do(http.MethodGet, "/kratos/admin/identities", nil); rec2.Code != http.StatusNotFound {
		t.Errorf("non-allowlisted path answered %d", rec2.Code)
	}
}

func TestAnIssuedSessionStillAuthenticatesWhenThePoolIsClosed(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	acme := e.tenant("acme", "active")
	e.member(acme, p.accountID, "member", "active", e.clock.Now())
	c := e.signIn(p)

	e.pool.Close()
	rec := c.do(http.MethodGet, "/api/v1/probe", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("probe answered %d: %s", rec.Code, rec.Body.String())
	}
	got := decodeData[map[string]string](t, rec)
	if got["actorId"] != p.accountID || got["tenantSlug"] != "acme" || got["role"] != "member" {
		t.Errorf("principal = %v", got)
	}
}

func TestProbeRouteEnforcesCSRFOnTheSessionResolver(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	c := e.signIn(e.person())

	expectError(t, c.do(http.MethodPost, "/api/v1/probe", nil), http.StatusForbidden, "CSRF_TOKEN_INVALID")
	if rec := c.mutate(http.MethodPost, "/api/v1/probe"); rec.Code != http.StatusOK {
		t.Errorf("with the csrf header: status %d", rec.Code)
	}
}

func TestIssueLandsOnTheRememberedTenant(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	oldest := e.tenant("oldest-co", "active")
	remembered := e.tenant("remembered-co", "active")
	e.member(oldest, p.accountID, "admin", "active", e.clock.Now().Add(-2*time.Hour))
	e.member(remembered, p.accountID, "viewer", "active", e.clock.Now().Add(-time.Hour))
	e.remember(p.accountID, remembered)
	c := e.client()
	c.cookies[kratosCookieName] = p.kratosCookie

	view := decodeData[viewReply](t, c.do(http.MethodGet, "/api/v1/auth/session", nil))

	if view.ActiveTenant == nil || view.ActiveTenant.Slug != "remembered-co" {
		t.Fatalf("activeTenant = %+v, want the remembered tenant", view.ActiveTenant)
	}
}

func TestANewLoginLandsOnTheTenantTheLastSwitchChose(t *testing.T) {
	e := newSessEnv(t, sessEnvOptions{})
	p := e.person()
	c, _ := signedInOn(e, p, "tenant-a", "admin")
	target := e.tenant("tenant-b", "active")
	e.member(target, p.accountID, "member", "active", e.clock.Now())
	e.clock.advance(time.Minute)
	rec := c.switchTo(target)
	if rec.Code != http.StatusOK {
		t.Fatalf("switch status = %d: %s", rec.Code, rec.Body.String())
	}
	fresh := e.client()
	fresh.cookies[kratosCookieName] = p.kratosCookie

	view := decodeData[viewReply](t, fresh.do(http.MethodGet, "/api/v1/auth/session", nil))

	if view.ActiveTenant == nil || view.ActiveTenant.Slug != "tenant-b" {
		t.Fatalf("activeTenant = %+v, want the switched tenant", view.ActiveTenant)
	}
}
