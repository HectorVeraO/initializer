package identity

import (
	"net/http"
	"strings"
	"testing"
)

// aloneEnv is identity with no tenant provider: the core No* defaults answer.
func aloneEnv(t *testing.T, opts sessEnvOptions) *sessEnv {
	t.Helper()
	opts.noTenancy = true
	return newSessEnv(t, opts)
}

// operatorPerson is a person whose account carries the platform operator flag.
func operatorPerson(e *sessEnv) person {
	e.t.Helper()
	p := e.person()
	e.exec("UPDATE iam_accounts SET is_platform_operator = true WHERE id = $1::uuid", p.accountID)
	return p
}

func TestAloneLoginMintsATenantlessToken(t *testing.T) {
	cases := []struct {
		name         string
		isOperator   bool
		wantOperator bool
	}{
		{"regular account", false, false},
		{"operator account", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := aloneEnv(t, sessEnvOptions{})
			p := e.person()
			if tc.isOperator {
				p = operatorPerson(e)
			}
			c := e.client()
			c.cookies[kratosCookieName] = p.kratosCookie

			rec := c.do(http.MethodGet, "/api/v1/auth/session", nil)

			view := decodeData[viewReply](t, rec)
			claims := c.claims()
			if claims.TenantID != nil || claims.TenantSlug != nil || claims.Role != nil {
				t.Errorf("claims = %+v, want tid, tslug and role null", claims)
			}
			if claims.AccountID != p.accountID || claims.PlatformOperator != tc.wantOperator {
				t.Errorf("claims sub=%q plat=%v, want %q and %v", claims.AccountID, claims.PlatformOperator, p.accountID, tc.wantOperator)
			}
			if view.Account.ID != p.accountID || view.Account.Email != p.email {
				t.Errorf("account = %+v, want the stored account", view.Account)
			}
			if view.Account.Role != nil || view.Account.IsPlatformOperator != tc.wantOperator {
				t.Errorf("account role=%v operator=%v, want null and %v", view.Account.Role, view.Account.IsPlatformOperator, tc.wantOperator)
			}
			if view.ActiveTenant != nil {
				t.Errorf("activeTenant = %+v, want null", view.ActiveTenant)
			}
			if view.Memberships == nil || len(view.Memberships) != 0 {
				t.Errorf("memberships = %+v, want a non-nil empty list", view.Memberships)
			}
			if !strings.Contains(rec.Body.String(), `"memberships":[]`) {
				t.Errorf("body does not carry memberships as []: %s", rec.Body.String())
			}
		})
	}
}

func TestAloneSessionViewFlagsFollowTheEvaluator(t *testing.T) {
	cases := []struct {
		name      string
		opts      sessEnvOptions
		wantFlags map[string]bool
	}{
		{"no feature-flags module", sessEnvOptions{}, map[string]bool{}},
		{"global values with feature-flags", sessEnvOptions{flags: mapFlags{"new_settings_ui": true}}, map[string]bool{"new_settings_ui": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := aloneEnv(t, tc.opts)
			c := e.client()
			c.cookies[kratosCookieName] = e.person().kratosCookie

			rec := c.do(http.MethodGet, "/api/v1/auth/session", nil)

			view := decodeData[viewReply](t, rec)
			if view.Flags == nil || len(view.Flags) != len(tc.wantFlags) {
				t.Fatalf("flags = %v, want %v", view.Flags, tc.wantFlags)
			}
			for key, want := range tc.wantFlags {
				if view.Flags[key] != want {
					t.Errorf("flags[%s] = %v, want %v", key, view.Flags[key], want)
				}
			}
		})
	}
}

func TestAloneSwitchTenantIsDeniedForEveryWellFormedID(t *testing.T) {
	cases := []struct {
		name string
		id   string
	}{
		{"random id", randomUUID(t)},
		{"nil uuid", "00000000-0000-0000-0000-000000000000"},
		{"upper-case id", strings.ToUpper(randomUUID(t))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := aloneEnv(t, sessEnvOptions{})
			c := e.signIn(e.person())
			before := c.cookies[c.sessionCookieName()]

			rec := c.switchTo(tc.id)

			expectError(t, rec, http.StatusForbidden, "TENANT_ACCESS_DENIED")
			expectNoCookies(t, rec)
			if c.cookies[c.sessionCookieName()] != before {
				t.Error("a refused switch changed the session cookie")
			}
		})
	}
}

func TestAloneSwitchTenantRejectsAMalformedBody(t *testing.T) {
	bodies := []string{`{}`, `{"tenantId":null}`, `{"tenantId":5}`, `{"tenantId":"not-a-uuid"}`, `{"tenantId":""}`, `{"tenantId":`, `[]`}
	e := aloneEnv(t, sessEnvOptions{})
	c := e.signIn(e.person())
	for _, body := range bodies {
		t.Run(body, func(t *testing.T) {
			rec := c.switchBody(body, nil)

			expectError(t, rec, http.StatusUnprocessableEntity, "VALIDATION_FAILED")
			expectNoCookies(t, rec)
		})
	}
}

func TestAloneRefreshStaysTenantless(t *testing.T) {
	e := aloneEnv(t, sessEnvOptions{})
	p := operatorPerson(e)
	c := e.signIn(p)
	before := c.claims()

	rec := c.mutate(http.MethodPost, "/api/v1/auth/refresh")

	if rec.Code != http.StatusOK {
		t.Fatalf("refresh answered %d: %s", rec.Code, rec.Body.String())
	}
	after := c.claims()
	if after.TenantID != nil || after.TenantSlug != nil || after.Role != nil {
		t.Errorf("claims = %+v, want tid, tslug and role null", after)
	}
	if after.SessionID != before.SessionID || !after.PlatformOperator {
		t.Errorf("refresh changed the chain or lost plat: before %+v after %+v", before, after)
	}
	view := decodeData[viewReply](t, rec)
	if view.ActiveTenant != nil || len(view.Memberships) != 0 {
		t.Errorf("view = %+v, want no active tenant and no memberships", view)
	}
}
