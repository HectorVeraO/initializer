package apikeys

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

func TestMalformedAPIKeyValuesAnswerTheUniform401(t *testing.T) {
	e := newEnv(t)
	tenant := e.tenant("tenant-a", "active")
	good := e.apiKey(tenant, []string{"tenant:read"}, nil)
	cases := map[string]string{
		"trailing nl":     good + "\n",
		"upper case":      strings.ToUpper(good),
		"short":           good[:76],
		"long":            good + "a",
		"bad alphabet":    good[:30] + "1" + good[31:],
		"wrong label":     "ak_test_" + good[8:],
		"missing divider": good[:24] + "a" + good[25:],
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			expectError(t, e.withKey("/api/v1/tenant", value), http.StatusUnauthorized, "API_KEY_INVALID_OR_REVOKED")
		})
	}
	rec := e.withKey("/api/v1/tenant", good)
	if rec.Code != http.StatusOK {
		t.Fatalf("the well-formed key answered %d: %s", rec.Code, rec.Body.String())
	}
}

func TestResolverRejectionsAreIdenticalAndStatusIsReadAfterTheSecret(t *testing.T) {
	e := newEnv(t)
	tenant := e.tenant("tenant-a", "active")
	now := e.clock.Now()
	valid := e.apiKey(tenant, []string{"tenant:read"}, nil)
	revoked := e.apiKey(tenant, []string{"tenant:read"}, nil)
	expired := e.apiKey(tenant, []string{"tenant:read"}, ptr(now.Add(-time.Minute)))
	boundary := e.apiKey(tenant, []string{"tenant:read"}, &now)
	lastSecond := e.apiKey(tenant, []string{"tenant:read"}, ptr(now.Add(time.Second)))
	prefix, _, _ := splitAPIKey(revoked)
	e.exec("UPDATE apk_keys SET revoked_at = $2 WHERE key_prefix = $1", prefix, now)

	validPrefix, _, _ := splitAPIKey(valid)
	other, _ := newAPIKey()
	_, otherSecret, _ := splitAPIKey(other)
	wrongSecret := validPrefix + "_" + otherSecret
	unknown, _ := newAPIKey()
	// The tenant of this key exists in no table: the resolver asks the directory.
	unknownTenant := e.apiKey(randomUUID(t), []string{"tenant:read"}, nil)
	rejected := map[string]string{
		"revoked": revoked, "expired": expired, "expires at now": boundary,
		"wrong secret": wrongSecret, "unknown prefix": unknown, "unknown tenant": unknownTenant,
		"malformed": valid[:60], "empty": "", "upper": strings.ToUpper(valid),
	}
	var reference string
	for name, key := range rejected {
		rec := e.withKey("/api/v1/tenant", key)
		expectError(t, rec, http.StatusUnauthorized, "API_KEY_INVALID_OR_REVOKED")
		got := errorJSON(t, rec)
		if reference == "" {
			reference = got
		}
		if got != reference {
			t.Fatalf("%s: error = %s, want %s", name, got, reference)
		}
	}
	if rec := e.withKey("/api/v1/tenant", valid); rec.Code != http.StatusOK {
		t.Fatalf("valid key answered %d: %s", rec.Code, rec.Body.String())
	}
	if rec := e.withKey("/api/v1/tenant", lastSecond); rec.Code != http.StatusOK {
		t.Fatalf("a key expiring in one second answered %d", rec.Code)
	}

	// A suspended tenant answers 403 only to a verified, usable key.
	e.tenants.SetTenantStatus(reqctx.TenantID(tenant), coreaccess.TenantSuspended)
	expectError(t, e.withKey("/api/v1/tenant", valid), http.StatusForbidden, "TENANT_SUSPENDED")
	for name, key := range rejected {
		rec := e.withKey("/api/v1/tenant", key)
		expectError(t, rec, http.StatusUnauthorized, "API_KEY_INVALID_OR_REVOKED")
		if errorJSON(t, rec) != reference {
			t.Fatalf("%s: suspension changed the rejection", name)
		}
	}
	e.tenants.SetTenantStatus(reqctx.TenantID(tenant), coreaccess.TenantActive)
	if rec := e.withKey("/api/v1/tenant", valid); rec.Code != http.StatusOK {
		t.Fatalf("reactivated tenant answered %d", rec.Code)
	}
}

func TestResolverRejectsRepeatedHeadersAndUnknownHashSchemes(t *testing.T) {
	e := newEnv(t)
	tenant := e.tenant("tenant-a", "active")
	valid := e.apiKey(tenant, []string{"tenant:read"}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenant", nil)
	req.Header.Add(apiKeyHeader, valid)
	req.Header.Add(apiKeyHeader, valid)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	expectError(t, rec, http.StatusUnauthorized, "API_KEY_INVALID_OR_REVOKED")

	prefix, _, _ := splitAPIKey(valid)
	e.exec("UPDATE apk_keys SET hash_scheme = 'argon2' WHERE key_prefix = $1", prefix)
	expectError(t, e.withKey("/api/v1/tenant", valid), http.StatusUnauthorized, "API_KEY_INVALID_OR_REVOKED")
}

func TestKeyScopesNoLongerRegisteredDoNotGrantAccess(t *testing.T) {
	e := newEnv(t)
	tenant := e.tenant("tenant-a", "active")
	mixed := e.apiKey(tenant, []string{"tenant:read", "removed:scope"}, nil)
	stale := e.apiKey(tenant, []string{"removed:scope"}, nil)

	rec := e.withKey("/api/v1/tenant", mixed)
	if rec.Code != http.StatusOK {
		t.Fatalf("key with a registered scope answered %d: %s", rec.Code, rec.Body.String())
	}
	rec = e.withKey("/api/v1/tenant", stale)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("key with only an unregistered scope answered %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAPIKeyTenantRefIsCheckedAgainstTheKeysTenant(t *testing.T) {
	e := newEnv(t)
	tenantA := e.tenant("tenant-a", "active")
	e.tenant("tenant-b", "active")
	key := e.apiKey(tenantA, []string{"tenant:read"}, nil)

	for _, ref := range []string{"tenant-a", "TENANT-A", strings.ToUpper(tenantA)} {
		rec := e.withKey("/api/v1/tenant?tenantId="+ref, key)
		if rec.Code != http.StatusOK {
			t.Fatalf("tenantId=%s answered %d: %s", ref, rec.Code, rec.Body.String())
		}
	}
	expectError(t, e.withKey("/api/v1/tenant?tenantId=tenant-b", key), http.StatusForbidden, "TENANT_MISMATCH")
}

// resolverFixture is the resolver over a real database with a directory fake.
type resolverFixture struct {
	resolver  *apiKeyResolver
	directory *tenantDir
	e         *env
	logs      *logSink
}

func newResolverFixture(t *testing.T) *resolverFixture {
	t.Helper()
	e := newEnv(t)
	scopes := coreaccess.NewScopeRegistry()
	for _, name := range []string{"tenant:read", "api-keys:read"} {
		err := scopes.Register(name, "synthetic", coreaccess.ScopeTenant)
		if err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	logs := &logSink{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	resolver := newAPIKeyResolver(e.pool, e.clock, scopes, logger)
	resolver.tenants = e.tenants
	return &resolverFixture{resolver: resolver, directory: e.tenants, e: e, logs: logs}
}

func keyRequest(key string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/api-keys", nil)
	req.Header.Set(apiKeyHeader, key)
	return req
}

func TestResolverGivesAKeyWithoutATenantAPrincipalWithoutOne(t *testing.T) {
	f := newResolverFixture(t)
	key := f.e.apiKey("", []string{"tenant:read", "removed:scope"}, nil)
	prefix, _, _ := splitAPIKey(key)

	principal, err := f.resolver.Resolve(context.Background(), keyRequest(key))

	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if principal.Kind != reqctx.ActorAPIKey || principal.ActorID != "apikey:"+prefix ||
		principal.TenantID != nil || principal.TenantSlug != "" || principal.AccountID != "" ||
		principal.Role != "" || principal.IsPlatformOperator ||
		!reflect.DeepEqual(principal.Scopes, []string{"tenant:read"}) {
		t.Fatalf("principal = %+v", principal)
	}
}

func TestResolverReadsTheTenantOfAKeyThroughTheDirectory(t *testing.T) {
	f := newResolverFixture(t)
	tenantID := f.e.tenant("acme", "active")
	key := f.e.apiKey(tenantID, []string{"tenant:read"}, nil)

	principal, err := f.resolver.Resolve(context.Background(), keyRequest(key))

	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if principal.TenantID == nil || string(*principal.TenantID) != tenantID || principal.TenantSlug != "acme" ||
		!reflect.DeepEqual(principal.Scopes, []string{"tenant:read"}) {
		t.Fatalf("principal = %+v", principal)
	}
}

func TestResolverAnswersTheSame401ForAnUnknownTenantAndASuspendedOneOnlyAfterTheSecret(t *testing.T) {
	f := newResolverFixture(t)
	suspended := f.e.tenant("gone", "suspended")
	unknownTenantKey := f.e.apiKey(randomUUID(t), []string{"tenant:read"}, nil)
	suspendedKey := f.e.apiKey(suspended, []string{"tenant:read"}, nil)
	other, _ := newAPIKey()
	_, wrongSecret, _ := splitAPIKey(other)
	suspendedPrefix, _, _ := splitAPIKey(suspendedKey)
	suspendedWrongSecret := suspendedPrefix + "_" + wrongSecret

	_, unknownErr := f.resolver.Resolve(context.Background(), keyRequest(unknownTenantKey))
	_, suspendedErr := f.resolver.Resolve(context.Background(), keyRequest(suspendedKey))
	_, wrongSecretErr := f.resolver.Resolve(context.Background(), keyRequest(suspendedWrongSecret))

	var unknown, suspendedAPI, wrong *api.Error
	if !errors.As(unknownErr, &unknown) || unknown.Status != http.StatusUnauthorized || unknown.Code != api.CodeAPIKeyInvalidOrRevoked {
		t.Fatalf("unknown tenant: %v", unknownErr)
	}
	if !errors.As(suspendedErr, &suspendedAPI) || suspendedAPI.Status != http.StatusForbidden || suspendedAPI.Code != api.CodeTenantSuspended {
		t.Fatalf("suspended tenant: %v", suspendedErr)
	}
	if !errors.As(wrongSecretErr, &wrong) || wrong.Status != http.StatusUnauthorized || wrong.Code != api.CodeAPIKeyInvalidOrRevoked || wrong.Message != unknown.Message {
		t.Fatalf("wrong secret of a suspended tenant's key: %v", wrongSecretErr)
	}
	logged := f.logs.String()
	if !strings.Contains(logged, `"reason":"tenant_unknown"`) || !strings.Contains(logged, `"reason":"tenant_suspended"`) {
		t.Fatalf("rejection reasons are not logged: %s", logged)
	}
	if strings.Contains(logged, unknownTenantKey) || strings.Contains(logged, suspendedKey) {
		t.Fatal("a key reached the log")
	}
}

func TestResolverFailsOperationallyWithoutADirectoryOrWhenItFails(t *testing.T) {
	f := newResolverFixture(t)
	tenantID := f.e.tenant("acme", "active")
	key := f.e.apiKey(tenantID, []string{"tenant:read"}, nil)

	f.directory.fail(errors.New("directory down"))
	_, err := f.resolver.Resolve(context.Background(), keyRequest(key))
	var apiErr *api.Error
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("a failing directory must be operational, got %v", err)
	}

	f.resolver.tenants = nil
	_, err = f.resolver.Resolve(context.Background(), keyRequest(key))
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("a missing directory must be operational, got %v", err)
	}
	_, err = f.resolver.Resolve(context.Background(), keyRequest("not-a-key"))
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("a missing directory must be operational for any request, got %v", err)
	}
}

func TestWithoutTenancyAGlobalKeyResolvesAndATenantKeyIsRejected(t *testing.T) {
	e := newEnvWithoutTenancy(t)
	global := e.apiKey("", []string{"api-keys:read"}, nil)
	bound := e.apiKey(randomUUID(t), []string{"api-keys:read"}, nil)

	// The global key authenticates: the list route then refuses it for lacking a tenant.
	expectError(t, e.withKey("/api/v1/api-keys", global), http.StatusForbidden, "ACTIVE_TENANT_REQUIRED")
	expectError(t, e.withKey("/api/v1/api-keys", bound), http.StatusUnauthorized, "API_KEY_INVALID_OR_REVOKED")
}
