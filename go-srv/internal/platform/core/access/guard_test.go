package access_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/internal/ctxkey"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

const (
	tenantA = "0b6f2a4e-7d1c-4c52-9a38-1f5e6d7c8b90"
	tenantB = "a1b2c3d4-e5f6-4a78-8b9c-0d1e2f3a4b5c"
)

type fakeSession struct {
	present bool
	result  access.SessionResult
	err     error
	failOn  *testing.T
}

func (f *fakeSession) Present(*http.Request) bool {
	if f.failOn != nil {
		f.failOn.Error("session resolver was consulted")
	}
	return f.present
}

func (f *fakeSession) Resolve(*http.Request) (access.SessionResult, error) {
	if f.failOn != nil {
		f.failOn.Error("session resolver was consulted")
	}
	return f.result, f.err
}

type fakeKey struct {
	present   bool
	principal reqctx.Principal
	err       error
	failOn    *testing.T
}

func (f *fakeKey) Present(*http.Request) bool {
	if f.failOn != nil {
		f.failOn.Error("api key resolver was consulted")
	}
	return f.present
}

func (f *fakeKey) Resolve(context.Context, *http.Request) (reqctx.Principal, error) {
	if f.failOn != nil {
		f.failOn.Error("api key resolver was consulted")
	}
	return f.principal, f.err
}

func tenantPtr(id string) *reqctx.TenantID {
	tenant := reqctx.TenantID(id)
	return &tenant
}

func memberPrincipal(role reqctx.Role) reqctx.Principal {
	return reqctx.Principal{
		Kind: reqctx.ActorAccount, ActorID: "acct-1", AccountID: "acct-1",
		TenantID: tenantPtr(tenantA), TenantSlug: "tenant-a", Role: role,
	}
}

func validSession(p reqctx.Principal) *fakeSession {
	return &fakeSession{present: true, result: access.SessionResult{State: access.SessionValid, Principal: p, CSRFValid: true}}
}

func newScopes(t *testing.T) *access.ScopeRegistry {
	t.Helper()
	scopes := access.NewScopeRegistry()
	levels := map[string]access.ScopeLevel{
		"tenant:read":   access.ScopeTenant,
		"api-keys:read": access.ScopeTenant,
		"platform:read": access.ScopeAny,
	}
	for name, level := range levels {
		if err := scopes.Register(name, "test scope", level); err != nil {
			t.Fatal(err)
		}
	}
	scopes.Freeze()
	return scopes
}

type harness struct {
	guard  *access.Guard
	logs   *bytes.Buffer
	called bool
	seen   reqctx.Principal
	hasP   bool
}

func newHarness(t *testing.T, s access.SessionResolver, k access.APIKeyResolver) *harness {
	t.Helper()
	logs := &bytes.Buffer{}
	writer := api.NewWriter(clock.Fixed(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), slog.New(slog.NewJSONHandler(logs, nil)))
	return &harness{guard: access.NewGuard(s, k, newScopes(t), writer), logs: logs}
}

func (h *harness) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.called = true
		h.seen, h.hasP = reqctx.PrincipalFrom(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h *harness) do(p access.Policy, method string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1/things", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.guard.Require(p, h.handler()).ServeHTTP(rec, req)
	return rec
}

func (h *harness) expect(t *testing.T, rec *httptest.ResponseRecorder, status int, code api.Code) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, status, rec.Body)
	}
	if h.called {
		t.Fatal("handler ran")
	}
	var env api.Envelope[any]
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error == nil || env.Error.Code != code {
		t.Fatalf("error = %+v, want code %s", env.Error, code)
	}
}

var (
	sessionPolicy = access.Policy{Kind: access.KindSession}
	keyPolicy     = access.Policy{Kind: access.KindAPIKey, Scope: "tenant:read"}
	eitherPolicy  = access.Policy{Kind: access.KindSessionOrAPIKey, Scope: "tenant:read", MinRole: reqctx.RoleViewer}
)

func TestPublicNeverTouchesResolvers(t *testing.T) {
	h := newHarness(t, &fakeSession{failOn: t}, &fakeKey{failOn: t})
	rec := h.do(access.Public, http.MethodPost, nil)
	if rec.Code != http.StatusNoContent || !h.called || h.hasP {
		t.Fatalf("status %d called %v principal %v", rec.Code, h.called, h.hasP)
	}
}

func TestStep1BothCredentialsConflict(t *testing.T) {
	for name, p := range map[string]access.Policy{"session": sessionPolicy, "key": keyPolicy, "either": eitherPolicy} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, &fakeSession{present: true}, &fakeKey{present: true})
			h.expect(t, h.do(p, http.MethodGet, nil), 409, api.CodeCredentialConflict)
		})
	}
}

func TestStep2MissingSession(t *testing.T) {
	cases := map[string]struct {
		policy access.Policy
		key    bool
	}{
		"session none":         {sessionPolicy, false},
		"session with key":     {sessionPolicy, true},
		"allow-expired no one": {access.Policy{Kind: access.KindSessionAllowExpired}, false},
		"allow-expired key":    {access.Policy{Kind: access.KindSessionAllowExpired}, true},
		"either none":          {eitherPolicy, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, &fakeSession{}, &fakeKey{present: c.key})
			h.expect(t, h.do(c.policy, http.MethodGet, nil), 401, api.CodeSessionInvalid)
		})
	}
}

func TestStep2bMissingAPIKey(t *testing.T) {
	for name, sessionPresent := range map[string]bool{"nothing": false, "session only": true} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, &fakeSession{present: sessionPresent}, &fakeKey{})
			h.expect(t, h.do(keyPolicy, http.MethodGet, nil), 401, api.CodeAPIKeyInvalidOrRevoked)
		})
	}
}

func TestStep3SessionStates(t *testing.T) {
	expired := &fakeSession{present: true, result: access.SessionResult{State: access.SessionExpired, Principal: memberPrincipal(reqctx.RoleAdmin), CSRFValid: true}}
	invalid := &fakeSession{present: true, result: access.SessionResult{State: access.SessionInvalid}}
	unknown := &fakeSession{present: true, result: access.SessionResult{State: 0}}

	h := newHarness(t, expired, &fakeKey{})
	h.expect(t, h.do(sessionPolicy, http.MethodGet, nil), 401, api.CodeSessionInvalid)
	h = newHarness(t, invalid, &fakeKey{})
	h.expect(t, h.do(access.Policy{Kind: access.KindSessionAllowExpired}, http.MethodGet, nil), 401, api.CodeSessionInvalid)
	h = newHarness(t, unknown, &fakeKey{})
	h.expect(t, h.do(sessionPolicy, http.MethodGet, nil), 500, api.CodeInternalError)
}

func TestExpiredSessionAllowedMarksPrincipal(t *testing.T) {
	expired := &fakeSession{present: true, result: access.SessionResult{State: access.SessionExpired, Principal: memberPrincipal(reqctx.RoleAdmin), CSRFValid: true}}
	h := newHarness(t, expired, &fakeKey{})
	rec := h.do(access.Policy{Kind: access.KindSessionAllowExpired}, http.MethodPost, nil)
	if rec.Code != http.StatusNoContent || !h.seen.SessionExpired {
		t.Fatalf("status %d expired %v", rec.Code, h.seen.SessionExpired)
	}
}

func TestStep3bAPIKeyRejections(t *testing.T) {
	rejections := []*api.Error{
		api.NewError(401, api.CodeAPIKeyInvalidOrRevoked, "bad key"),
		api.NewError(403, "TENANT_SUSPENDED", "suspended"),
	}
	for _, rejection := range rejections {
		h := newHarness(t, &fakeSession{}, &fakeKey{present: true, err: rejection})
		h.expect(t, h.do(keyPolicy, http.MethodGet, nil), rejection.Status, rejection.Code)
	}
}

func TestOperationalErrorsAnswer500WithoutLeaking(t *testing.T) {
	secret := errors.New("dial tcp 10.0.0.1: connection refused")
	h := newHarness(t, &fakeSession{present: true, err: secret}, &fakeKey{})
	rec := h.do(sessionPolicy, http.MethodGet, nil)
	h.expect(t, rec, 500, api.CodeInternalError)
	if strings.Contains(rec.Body.String(), "10.0.0.1") {
		t.Fatalf("leaked: %s", rec.Body)
	}

	wrongStatus := api.NewError(404, api.CodeResourceNotFound, "nope")
	h = newHarness(t, &fakeSession{}, &fakeKey{present: true, err: wrongStatus})
	h.expect(t, h.do(keyPolicy, http.MethodGet, nil), 500, api.CodeInternalError)
	h = newHarness(t, &fakeSession{present: true, err: wrongStatus}, &fakeKey{})
	h.expect(t, h.do(sessionPolicy, http.MethodGet, nil), 500, api.CodeInternalError)
	h = newHarness(t, &fakeSession{}, &fakeKey{present: true, err: secret})
	rec = h.do(keyPolicy, http.MethodGet, nil)
	h.expect(t, rec, 500, api.CodeInternalError)
	if strings.Contains(rec.Body.String(), "10.0.0.1") {
		t.Fatalf("key path leaked: %s", rec.Body)
	}
}

func TestStep4CSRFOnUnsafeMethods(t *testing.T) {
	noCSRF := func() *fakeSession {
		s := validSession(memberPrincipal(reqctx.RoleAdmin))
		s.result.CSRFValid = false
		return s
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		h := newHarness(t, noCSRF(), &fakeKey{})
		h.expect(t, h.do(sessionPolicy, method, nil), 403, api.CodeCSRFTokenInvalid)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		h := newHarness(t, noCSRF(), &fakeKey{})
		if rec := h.do(sessionPolicy, method, nil); rec.Code != http.StatusNoContent {
			t.Fatalf("%s status = %d", method, rec.Code)
		}
	}
	expired := &fakeSession{present: true, result: access.SessionResult{State: access.SessionExpired, Principal: memberPrincipal(reqctx.RoleAdmin)}}
	h := newHarness(t, expired, &fakeKey{})
	h.expect(t, h.do(access.Policy{Kind: access.KindSessionAllowExpired}, http.MethodPost, nil), 403, api.CodeCSRFTokenInvalid)
}

func TestStep4BeforeStep6(t *testing.T) {
	operator := reqctx.Principal{Kind: reqctx.ActorAccount, ActorID: "op"}
	s := &fakeSession{present: true, result: access.SessionResult{State: access.SessionValid, Principal: operator}}
	h := newHarness(t, s, &fakeKey{})
	h.expect(t, h.do(access.Policy{Kind: access.KindSession, PlatformOperator: true}, http.MethodPost, nil), 403, api.CodeCSRFTokenInvalid)
}

func TestStep5ActiveTenantHeader(t *testing.T) {
	member := memberPrincipal(reqctx.RoleAdmin)
	tenantless := reqctx.Principal{Kind: reqctx.ActorAccount, ActorID: "acct-2"}
	allowExpired := access.Policy{Kind: access.KindSessionAllowExpired}
	cases := []struct {
		name   string
		p      reqctx.Principal
		method string
		policy access.Policy
		header *string
		want   int
	}{
		{"absent passes", member, "POST", sessionPolicy, nil, 204},
		{"same passes", member, "POST", sessionPolicy, ptr(tenantA), 204},
		{"upper case passes", member, "POST", sessionPolicy, ptr(strings.ToUpper(tenantA)), 204},
		{"other tenant", member, "POST", sessionPolicy, ptr(tenantB), 409},
		{"garbage", member, "POST", sessionPolicy, ptr("tenant-a"), 409},
		{"empty with tenant", member, "POST", sessionPolicy, ptr(""), 409},
		{"empty without tenant", tenantless, "POST", sessionPolicy, ptr(""), 204},
		{"value without tenant", tenantless, "POST", sessionPolicy, ptr(tenantA), 409},
		{"mismatch on GET passes", member, "GET", sessionPolicy, ptr(tenantB), 204},
		{"mismatch on allow-expired passes", member, "POST", allowExpired, ptr(tenantB), 204},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, validSession(c.p), &fakeKey{})
			headers := map[string]string{}
			if c.header != nil {
				headers["X-Active-Tenant-Id"] = *c.header
			}
			rec := h.do(c.policy, c.method, headers)
			if c.want == 409 {
				h.expect(t, rec, 409, api.CodeActiveTenantChanged)
			} else if rec.Code != c.want {
				t.Fatalf("status = %d, want %d", rec.Code, c.want)
			}
		})
	}
}

func ptr(s string) *string { return &s }

func TestStep5RepeatedHeaderChecksEveryValue(t *testing.T) {
	h := newHarness(t, validSession(memberPrincipal(reqctx.RoleAdmin)), &fakeKey{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/things", nil)
	req.Header.Add("X-Active-Tenant-Id", tenantA)
	req.Header.Add("X-Active-Tenant-Id", tenantB)
	rec := httptest.NewRecorder()
	h.guard.Require(sessionPolicy, h.handler()).ServeHTTP(rec, req)
	h.expect(t, rec, 409, api.CodeActiveTenantChanged)
}

func TestStep5IgnoredForAPIKeys(t *testing.T) {
	key := reqctx.Principal{Kind: reqctx.ActorAPIKey, ActorID: "apikey:abc", TenantID: tenantPtr(tenantA), Scopes: []string{"tenant:read"}}
	h := newHarness(t, &fakeSession{}, &fakeKey{present: true, principal: key})
	rec := h.do(keyPolicy, http.MethodPost, map[string]string{"X-Active-Tenant-Id": tenantB})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestStep6PlatformOperator(t *testing.T) {
	policy := access.Policy{Kind: access.KindSession, PlatformOperator: true}
	h := newHarness(t, validSession(memberPrincipal(reqctx.RoleAdmin)), &fakeKey{})
	h.expect(t, h.do(policy, http.MethodGet, nil), 403, api.CodePlatformOperatorNeeded)

	operator := reqctx.Principal{Kind: reqctx.ActorAccount, ActorID: "op", IsPlatformOperator: true}
	h = newHarness(t, validSession(operator), &fakeKey{})
	if rec := h.do(policy, http.MethodPost, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestStep7TenantAndRole(t *testing.T) {
	member := access.Policy{Kind: access.KindSession, MinRole: reqctx.RoleMember}
	tenantless := reqctx.Principal{Kind: reqctx.ActorAccount, ActorID: "acct-2"}
	h := newHarness(t, validSession(tenantless), &fakeKey{})
	h.expect(t, h.do(member, http.MethodGet, nil), 403, api.CodeActiveTenantRequired)

	h = newHarness(t, validSession(memberPrincipal(reqctx.RoleViewer)), &fakeKey{})
	h.expect(t, h.do(member, http.MethodGet, nil), 403, api.CodeInsufficientRole)

	h = newHarness(t, validSession(memberPrincipal("")), &fakeKey{})
	h.expect(t, h.do(member, http.MethodGet, nil), 403, api.CodeInsufficientRole)

	for _, role := range []reqctx.Role{reqctx.RoleMember, reqctx.RoleAdmin} {
		h = newHarness(t, validSession(memberPrincipal(role)), &fakeKey{})
		if rec := h.do(member, http.MethodGet, nil); rec.Code != http.StatusNoContent {
			t.Fatalf("%s status = %d", role, rec.Code)
		}
	}
}

func TestStep7cScopes(t *testing.T) {
	for name, scopes := range map[string][]string{"empty": {}, "nil": nil, "other": {"api-keys:read"}} {
		t.Run(name, func(t *testing.T) {
			key := reqctx.Principal{Kind: reqctx.ActorAPIKey, ActorID: "apikey:abc", Scopes: scopes}
			h := newHarness(t, &fakeSession{}, &fakeKey{present: true, principal: key})
			h.expect(t, h.do(eitherPolicy, http.MethodGet, nil), 403, api.CodeInsufficientScope)
		})
	}
}

func TestAPIKeySkipsRoleChecks(t *testing.T) {
	key := reqctx.Principal{Kind: reqctx.ActorAPIKey, ActorID: "apikey:abc", TenantID: tenantPtr(tenantA), Scopes: []string{"tenant:read"}}
	h := newHarness(t, &fakeSession{}, &fakeKey{present: true, principal: key})
	rec := h.do(eitherPolicy, http.MethodPost, nil)
	if rec.Code != http.StatusNoContent || h.seen.ActorID != "apikey:abc" {
		t.Fatalf("status %d actor %q", rec.Code, h.seen.ActorID)
	}
}

func tenantlessKey(scopes ...string) *fakeKey {
	principal := reqctx.Principal{Kind: reqctx.ActorAPIKey, ActorID: "apikey:abc", Scopes: scopes}
	return &fakeKey{present: true, principal: principal}
}

func TestAPIKeyWithoutTenantFailsTheTenantRequirement(t *testing.T) {
	cases := map[string]struct {
		policy access.Policy
		scope  string
		want   int
	}{
		"min role route": {eitherPolicy, "tenant:read", 403},
		"tenant-level scope without min role on either": {
			access.Policy{Kind: access.KindSessionOrAPIKey, Scope: "tenant:read"}, "tenant:read", 403},
		"tenant-level scope on a key route": {keyPolicy, "tenant:read", 403},
		"any-level scope on a key route": {
			access.Policy{Kind: access.KindAPIKey, Scope: "platform:read"}, "platform:read", 204},
		"any-level scope with min role": {
			access.Policy{Kind: access.KindSessionOrAPIKey, Scope: "platform:read", MinRole: reqctx.RoleViewer}, "platform:read", 403},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, &fakeSession{}, tenantlessKey(c.scope))
			rec := h.do(c.policy, http.MethodGet, nil)
			if c.want == 204 {
				if rec.Code != http.StatusNoContent {
					t.Fatalf("status = %d, want 204; body %s", rec.Code, rec.Body)
				}
				return
			}
			h.expect(t, rec, 403, api.CodeActiveTenantRequired)
		})
	}
}

func TestAPIKeyWithTenantPassesTheTenantRequirement(t *testing.T) {
	for name, policy := range map[string]access.Policy{"min role": eitherPolicy, "tenant scope": keyPolicy} {
		t.Run(name, func(t *testing.T) {
			key := reqctx.Principal{Kind: reqctx.ActorAPIKey, ActorID: "apikey:abc", TenantID: tenantPtr(tenantA), Scopes: []string{"tenant:read"}}
			h := newHarness(t, &fakeSession{}, &fakeKey{present: true, principal: key})
			if rec := h.do(policy, http.MethodGet, nil); rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d; body %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestTenantlessOperatorNeverAppliesToKeys(t *testing.T) {
	policy := access.Policy{
		Kind: access.KindSessionOrAPIKey, Scope: "tenant:read",
		MinRole: reqctx.RoleAdmin, TenantlessOperator: true,
	}
	h := newHarness(t, &fakeSession{}, tenantlessKey("tenant:read"))
	h.expect(t, h.do(policy, http.MethodGet, nil), 403, api.CodeActiveTenantRequired)
}

func TestTenantlessOperatorOnSessions(t *testing.T) {
	plain := access.Policy{Kind: access.KindSession, MinRole: reqctx.RoleAdmin}
	relaxed := access.Policy{Kind: access.KindSession, MinRole: reqctx.RoleAdmin, TenantlessOperator: true}
	tenantless := func(operator bool) reqctx.Principal {
		return reqctx.Principal{Kind: reqctx.ActorAccount, ActorID: "acct-2", AccountID: "acct-2", IsPlatformOperator: operator}
	}
	withTenant := func(role reqctx.Role, operator bool) reqctx.Principal {
		p := memberPrincipal(role)
		p.IsPlatformOperator = operator
		return p
	}
	cases := []struct {
		name      string
		principal reqctx.Principal
		plain     api.Code
		relaxed   api.Code
	}{
		{"admin with tenant", withTenant(reqctx.RoleAdmin, false), "", ""},
		{"member operator with tenant", withTenant(reqctx.RoleMember, true), api.CodeInsufficientRole, api.CodeInsufficientRole},
		{"operator without tenant", tenantless(true), api.CodeActiveTenantRequired, ""},
		{"non-operator without tenant", tenantless(false), api.CodeActiveTenantRequired, api.CodeActiveTenantRequired},
	}
	for _, c := range cases {
		for policyName, p := range map[string]access.Policy{"plain": plain, "relaxed": relaxed} {
			want := c.plain
			if policyName == "relaxed" {
				want = c.relaxed
			}
			t.Run(c.name+"/"+policyName, func(t *testing.T) {
				h := newHarness(t, validSession(c.principal), &fakeKey{})
				rec := h.do(p, http.MethodGet, nil)
				if want == "" {
					if rec.Code != http.StatusNoContent {
						t.Fatalf("status = %d, want 204; body %s", rec.Code, rec.Body)
					}
					return
				}
				h.expect(t, rec, 403, want)
			})
		}
	}
}

func TestHappyPathInstallsPrincipal(t *testing.T) {
	h := newHarness(t, validSession(memberPrincipal(reqctx.RoleAdmin)), &fakeKey{})
	rec := h.do(eitherPolicy, http.MethodPost, map[string]string{"X-Active-Tenant-Id": tenantA})
	if rec.Code != http.StatusNoContent || !h.hasP {
		t.Fatalf("status %d hasPrincipal %v", rec.Code, h.hasP)
	}
	if tenant, ok := reqctx.TenantFrom(withPrincipal(h.seen)); !ok || string(tenant) != tenantA {
		t.Fatalf("tenant = %q %v", tenant, ok)
	}
	if h.seen.Role != reqctx.RoleAdmin || h.seen.SessionExpired {
		t.Fatalf("principal = %+v", h.seen)
	}
}

func withPrincipal(p reqctx.Principal) context.Context {
	// Round-trip through a guarded handler so the context key stays private.
	var out context.Context
	logs := &bytes.Buffer{}
	writer := api.NewWriter(clock.System{}, slog.New(slog.NewJSONHandler(logs, nil)))
	scopes := access.NewScopeRegistry()
	guard := access.NewGuard(&fakeSession{present: true, result: access.SessionResult{State: access.SessionValid, Principal: p, CSRFValid: true}}, &fakeKey{}, scopes, writer)
	handler := guard.Require(access.Policy{Kind: access.KindSession}, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		out = r.Context()
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	return out
}

func TestRequireRejectsInvalidPolicies(t *testing.T) {
	h := newHarness(t, &fakeSession{}, &fakeKey{})
	invalid := map[string]access.Policy{
		"zero kind":              {},
		"unknown kind":           {Kind: 99},
		"scope on session":       {Kind: access.KindSession, Scope: "tenant:read"},
		"missing scope":          {Kind: access.KindAPIKey},
		"unregistered scope":     {Kind: access.KindAPIKey, Scope: "nope:read"},
		"role on api key":        {Kind: access.KindAPIKey, Scope: "tenant:read", MinRole: reqctx.RoleViewer},
		"role on public":         {Kind: access.KindNone, MinRole: reqctx.RoleViewer},
		"role on allow-expired":  {Kind: access.KindSessionAllowExpired, MinRole: reqctx.RoleViewer},
		"invalid role":           {Kind: access.KindSession, MinRole: "owner"},
		"operator with role":     {Kind: access.KindSession, PlatformOperator: true, MinRole: reqctx.RoleViewer},
		"operator on api key":    {Kind: access.KindAPIKey, Scope: "tenant:read", PlatformOperator: true},
		"operator on either":     {Kind: access.KindSessionOrAPIKey, Scope: "tenant:read", PlatformOperator: true},
		"operator on public":     {Kind: access.KindNone, PlatformOperator: true},
		"operator allow-expired": {Kind: access.KindSessionAllowExpired, PlatformOperator: true},

		"tenantless operator without min role":                {Kind: access.KindSession, TenantlessOperator: true},
		"tenantless operator on api key":                      {Kind: access.KindAPIKey, Scope: "tenant:read", MinRole: reqctx.RoleAdmin, TenantlessOperator: true},
		"tenantless operator on public":                       {Kind: access.KindNone, TenantlessOperator: true},
		"tenantless operator on allow-expired":                {Kind: access.KindSessionAllowExpired, TenantlessOperator: true},
		"tenantless operator with platform only":              {Kind: access.KindSession, PlatformOperator: true, TenantlessOperator: true},
		"tenantless operator with platform operator and role": {Kind: access.KindSession, MinRole: reqctx.RoleAdmin, PlatformOperator: true, TenantlessOperator: true},
	}
	for name, p := range invalid {
		t.Run(name, func(t *testing.T) {
			if err := p.Validate(newScopes(t)); err == nil {
				t.Fatal("Validate accepted the policy")
			}
			defer func() {
				if recover() == nil {
					t.Fatal("Require did not panic")
				}
			}()
			h.guard.Require(p, h.handler())
		})
	}
}

func TestRequirePanicsOnNilHandler(t *testing.T) {
	h := newHarness(t, &fakeSession{}, &fakeKey{})
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	h.guard.Require(access.Public, nil)
}

func TestNewGuardPanicsOnNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	access.NewGuard(nil, access.NoAPIKeys, access.NewScopeRegistry(), nil)
}

func TestMilestonePolicyShapesAreValid(t *testing.T) {
	scopes := newScopes(t)
	shapes := []access.Policy{
		access.Public,
		{Kind: access.KindSessionAllowExpired},
		{Kind: access.KindSessionOrAPIKey, Scope: "tenant:read", MinRole: reqctx.RoleViewer},
		{Kind: access.KindSessionOrAPIKey, Scope: "api-keys:read", MinRole: reqctx.RoleAdmin},
		{Kind: access.KindSession, PlatformOperator: true},
		{Kind: access.KindSession, MinRole: reqctx.RoleAdmin, TenantlessOperator: true},
		{Kind: access.KindSessionOrAPIKey, Scope: "api-keys:read", MinRole: reqctx.RoleAdmin, TenantlessOperator: true},
	}
	for _, p := range shapes {
		if err := p.Validate(scopes); err != nil {
			t.Fatalf("%+v: %v", p, err)
		}
	}
}

// TestValidateRejectsInvalidPolicies pins concrete rejected combinations.
func TestValidateRejectsInvalidPolicies(t *testing.T) {
	scopes := newScopes(t)
	cases := map[string]access.Policy{
		"zero kind":                  {},
		"unknown kind":               {Kind: 6},
		"none with scope":            {Kind: access.KindNone, Scope: "tenant:read"},
		"session with scope":         {Kind: access.KindSession, Scope: "tenant:read"},
		"api key without scope":      {Kind: access.KindAPIKey},
		"api key unknown scope":      {Kind: access.KindAPIKey, Scope: "unknown:scope"},
		"api key with role":          {Kind: access.KindAPIKey, Scope: "tenant:read", MinRole: reqctx.RoleViewer},
		"session unknown role":       {Kind: access.KindSession, MinRole: "owner"},
		"allow-expired with role":    {Kind: access.KindSessionAllowExpired, MinRole: reqctx.RoleViewer},
		"operator with role":         {Kind: access.KindSession, PlatformOperator: true, MinRole: reqctx.RoleAdmin},
		"operator on api key":        {Kind: access.KindAPIKey, Scope: "tenant:read", PlatformOperator: true},
		"operator on session-or-key": {Kind: access.KindSessionOrAPIKey, Scope: "tenant:read", PlatformOperator: true},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			if err := p.Validate(scopes); err == nil {
				t.Fatalf("%+v accepted", p)
			}
		})
	}
}

func TestGuardFillsRequestInfoWhenPresent(t *testing.T) {
	h := newHarness(t, validSession(memberPrincipal(reqctx.RoleAdmin)), &fakeKey{})
	info := &ctxkey.RequestInfo{Route: "GET /api/v1/things"}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/things", nil)
	req = req.WithContext(ctxkey.WithRequestInfo(req.Context(), info))
	h.guard.Require(sessionPolicy, h.handler()).ServeHTTP(httptest.NewRecorder(), req)
	if info.ActorID != "acct-1" || info.TenantID != tenantA || info.Route != "GET /api/v1/things" {
		t.Fatalf("info = %+v", info)
	}
}

func TestValidateNamesEveryTenantlessOperatorProblem(t *testing.T) {
	scopes := newScopes(t)
	cases := map[string]struct {
		policy access.Policy
		want   []string
	}{
		"without min role": {
			access.Policy{Kind: access.KindSession, TenantlessOperator: true},
			[]string{"access: tenantless operator requires min role"},
		},
		"on a key route": {
			access.Policy{Kind: access.KindAPIKey, Scope: "tenant:read", MinRole: reqctx.RoleAdmin, TenantlessOperator: true},
			[]string{"access: tenantless operator requires KindSession or KindSessionOrAPIKey"},
		},
		"both problems are reported together": {
			access.Policy{Kind: access.KindNone, TenantlessOperator: true},
			[]string{
				"access: tenantless operator requires min role",
				"access: tenantless operator requires KindSession or KindSessionOrAPIKey",
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := c.policy.Validate(scopes)
			if err == nil {
				t.Fatal("Validate accepted the policy")
			}
			for _, want := range c.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}
