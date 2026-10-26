package coreauth

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/auth"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
)

var plainKeyPattern = regexp.MustCompile(`^gbk_live_[a-z2-7]{16}_[a-z2-7]{52}$`)

// apiKey inserts a key of tenantID built from fresh random parts and returns
// its plain text. The creator account is a new synthetic person.
func (e *sessEnv) apiKey(tenantID string, scopes []string, expiresAt *time.Time) string {
	e.t.Helper()
	plain, err := newAPIKey()
	if err != nil {
		e.t.Fatalf("new key: %v", err)
	}
	prefix, secret, ok := splitAPIKey(plain)
	if !ok {
		e.t.Fatal("generated key does not split")
	}
	creator := e.person()
	e.exec(`INSERT INTO iam_api_keys (tenant_id, name, key_prefix, secret_hash, hash_scheme, scopes, expires_at, created_by_account_id, created_at)
VALUES ($1::uuid, 'seeded', $2, $3, 'sha256', $4, $5, $6::uuid, $7)`,
		tenantID, prefix, hashAPIKeySecret(secret), scopes, expiresAt, creator.accountID, e.clock.Now())
	return plain
}

// withKey sends a GET with only the X-API-Key header.
func (e *sessEnv) withKey(path, key string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.client().do(http.MethodGet, path, map[string]string{apiKeyHeader: key})
}

// errorJSON returns the raw "error" member of an envelope.
func errorJSON(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope map[string]json.RawMessage
	err := json.Unmarshal(rec.Body.Bytes(), &envelope)
	if err != nil || len(envelope["error"]) == 0 {
		t.Fatalf("no error member (status %d): %s", rec.Code, rec.Body.String())
	}
	return string(envelope["error"])
}

// mutateWithKey sends an unsafe request with only the X-API-Key header.
func (e *sessEnv) mutateWithKey(method, path, key string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.client().doBody(method, path, map[string]string{apiKeyHeader: key}, `{}`)
}

func createKeyJSON(name string, scopes []string, expiresAt any) string {
	body, _ := json.Marshal(map[string]any{"name": name, "scopes": scopes, "expiresAt": expiresAt})
	return string(body)
}

func TestNewAPIKeyAlwaysMatchesTheFormatAndSplits(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 300; i++ {
		plain, err := newAPIKey()
		if err != nil {
			t.Fatalf("newAPIKey: %v", err)
		}
		if !plainKeyPattern.MatchString(plain) || len(plain) != 78 {
			t.Fatalf("key %q does not match the format", plain)
		}
		prefix, secret, ok := splitAPIKey(plain)
		if !ok || len(prefix) != 25 || len(secret) != 52 || prefix+"_"+secret != plain {
			t.Fatalf("split = %q %q %v", prefix, secret, ok)
		}
		if seen[plain] {
			t.Fatal("duplicate key generated")
		}
		seen[plain] = true
	}
}

func TestSplitAPIKeyRejectsMalformedValues(t *testing.T) {
	good, err := newAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"empty":           "",
		"trailing nl":     good + "\n",
		"upper case":      strings.ToUpper(good),
		"short":           good[:77],
		"long":            good + "a",
		"bad alphabet":    good[:30] + "1" + good[31:],
		"wrong label":     "gbk_test_" + good[9:],
		"missing divider": good[:25] + "a" + good[26:],
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, ok := splitAPIKey(value); ok {
				t.Fatalf("%q was accepted", value)
			}
		})
	}
}

func TestCreateAPIKeyReturnsThePlainKeyOnceAndNeverStoresOrLogsIt(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)

	rec := admin.send(http.MethodPost, "/api/v1/api-keys", createKeyJSON("  billing-sync ", []string{"tenant:read"}, nil))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	created := decodeData[ApiKeyCreated](t, rec)
	if !plainKeyPattern.MatchString(created.PlainTextKey) {
		t.Fatalf("plain key %q does not match", created.PlainTextKey)
	}
	prefix, secret, _ := splitAPIKey(created.PlainTextKey)
	view := created.ApiKey
	if view.Name != "billing-sync" || view.KeyPrefix != prefix || view.Status != "active" ||
		!reflect.DeepEqual(view.Scopes, []string{"tenant:read"}) || view.ExpiresAt != nil || view.RevokedAt != nil {
		t.Fatalf("view = %+v", view)
	}

	list := admin.send(http.MethodGet, "/api/v1/api-keys", "")
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), secret) || strings.Contains(list.Body.String(), "plainTextKey") {
		t.Fatalf("listing leaks the key: %d %s", list.Code, list.Body.String())
	}
	if !strings.Contains(list.Body.String(), prefix) {
		t.Fatalf("listing misses the prefix: %s", list.Body.String())
	}

	// Use the key so the resolver's logs are covered too, then scan every table and log.
	e.withKey("/api/v1/tenant", created.PlainTextKey)
	if strings.Contains(e.logs.String(), secret) {
		t.Fatal("the secret reached the logs")
	}
	if strings.Contains(e.logs.String(), created.PlainTextKey) {
		t.Fatal("the plain key reached the logs")
	}
	rows, err := e.pool.Query(context.Background(), "SELECT table_name FROM information_schema.tables WHERE table_schema = 'public'")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	rows.Close()
	for _, name := range names {
		var dump string
		err = e.pool.QueryRow(context.Background(),
			`SELECT coalesce(string_agg(row_to_json(x)::text, ' '), '') FROM "`+name+`" x`).Scan(&dump)
		if err != nil {
			t.Fatalf("dump %s: %v", name, err)
		}
		if strings.Contains(dump, secret) {
			t.Fatalf("table %s holds the secret", name)
		}
	}
}

func TestCreateAPIKeyValidationAccumulates(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	cases := []struct {
		name string
		body string
		want []wantIssue
	}{
		{"empty name and scopes", `{"name":"","scopes":[]}`,
			[]wantIssue{{"name", "FIELD_REQUIRED"}, {"scopes", "FIELD_REQUIRED"}}},
		{"blank name", `{"name":"   ","scopes":["tenant:read"]}`, []wantIssue{{"name", "FIELD_REQUIRED"}}},
		{"all missing", `{}`, []wantIssue{{"name", "FIELD_REQUIRED"}, {"scopes", "FIELD_REQUIRED"}}},
		{"all null", `{"name":null,"scopes":null,"expiresAt":null}`,
			[]wantIssue{{"name", "FIELD_REQUIRED"}, {"scopes", "FIELD_REQUIRED"}}},
		{"wrong types", `{"name":5,"scopes":"x","expiresAt":"soon"}`,
			[]wantIssue{{"name", "FIELD_TYPE_INVALID"}, {"scopes", "FIELD_TYPE_INVALID"}, {"expiresAt", "FIELD_INVALID_FORMAT"}}},
		{"expiresAt not a string", `{"name":"a","scopes":["tenant:read"],"expiresAt":5}`,
			[]wantIssue{{"expiresAt", "FIELD_TYPE_INVALID"}}},
		{"too long", `{"name":"` + strings.Repeat("a", 101) + `","scopes":["tenant:read"]}`,
			[]wantIssue{{"name", "FIELD_TOO_LONG"}}},
		// A non-string entry is a shape error: it stops scope-name checks, so only it is reported.
		{"non-string scope entry", `{"name":"a","scopes":["tenant:read",7,"tenant:read","nope:nope"]}`,
			[]wantIssue{{"scopes[1]", "FIELD_TYPE_INVALID"}}},
		{"unknown and duplicate", `{"name":"a","scopes":["tenant:read","nope:nope","tenant:read"]}`,
			[]wantIssue{{"scopes[1]", "FIELD_NOT_ALLOWED"}, {"scopes[2]", "FIELD_DUPLICATE"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectIssues(t, admin.send(http.MethodPost, "/api/v1/api-keys", tc.body), tc.want)
		})
	}

	t.Run("malformed body", func(t *testing.T) {
		expectIssues(t, admin.send(http.MethodPost, "/api/v1/api-keys", `{"name":`), []wantIssue{{"", "BODY_MALFORMED"}})
	})
	t.Run("name at the limit", func(t *testing.T) {
		rec := admin.send(http.MethodPost, "/api/v1/api-keys", createKeyJSON(strings.Repeat("a", 100), []string{"tenant:read"}, nil))
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestCreateAPIKeyAcceptsAPastExpiryAndBornExpired(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	past := e.clock.Now().Add(-time.Hour).Format(time.RFC3339)

	rec := admin.send(http.MethodPost, "/api/v1/api-keys", createKeyJSON("old", []string{"tenant:read"}, past))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	created := decodeData[ApiKeyCreated](t, rec)
	if created.ApiKey.Status != "expired" || created.ApiKey.ExpiresAt == nil {
		t.Fatalf("view = %+v", created.ApiKey)
	}
	expectError(t, e.withKey("/api/v1/tenant", created.PlainTextKey), http.StatusUnauthorized, "API_KEY_INVALID_OR_REVOKED")
}

func TestAPIKeyWriteRoutesRefuseViewersAndKeys(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, viewer := e.actor(tenant, "viewer", false)
	key := e.apiKey(tenant, []string{"tenant:read", "api-keys:read"}, nil)

	t.Run("viewer cannot create", func(t *testing.T) {
		rec := viewer.send(http.MethodPost, "/api/v1/api-keys", createKeyJSON("a", []string{"tenant:read"}, nil))
		expectError(t, rec, http.StatusForbidden, "INSUFFICIENT_ROLE")
	})
	t.Run("viewer cannot list", func(t *testing.T) {
		expectError(t, viewer.send(http.MethodGet, "/api/v1/api-keys", ""), http.StatusForbidden, "INSUFFICIENT_ROLE")
	})
	t.Run("viewer cannot read scopes", func(t *testing.T) {
		expectError(t, viewer.send(http.MethodGet, "/api/v1/api-key-scopes", ""), http.StatusForbidden, "INSUFFICIENT_ROLE")
	})
	t.Run("viewer cannot revoke", func(t *testing.T) {
		rec := viewer.send(http.MethodDelete, "/api/v1/api-keys/"+randomUUID(t), "")
		expectError(t, rec, http.StatusForbidden, "INSUFFICIENT_ROLE")
	})
	routes := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/api-keys"},
		{http.MethodDelete, "/api/v1/api-keys/" + randomUUID(t)},
		{http.MethodGet, "/api/v1/api-key-scopes"},
		{http.MethodPost, "/api/v1/tenants"},
		{http.MethodPatch, "/api/v1/tenants/" + randomUUID(t) + "/status"},
		{http.MethodPatch, "/api/v1/memberships/" + randomUUID(t)},
		{http.MethodPost, "/api/v1/invitations"},
		{http.MethodDelete, "/api/v1/invitations/" + randomUUID(t)},
		{http.MethodPost, "/api/v1/invitations/accept"},
		{http.MethodGet, "/api/v1/memberships"},
		{http.MethodGet, "/api/v1/invitations"},
	}
	for _, route := range routes {
		t.Run("key only "+route.method+" "+route.path, func(t *testing.T) {
			rec := e.mutateWithKey(route.method, route.path, key)
			expectError(t, rec, http.StatusUnauthorized, "SESSION_INVALID")
		})
	}
}

func TestAPIKeyAndSessionTogetherAreAConflict(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	key := e.apiKey(tenant, []string{"tenant:read"}, nil)

	rec := admin.do(http.MethodGet, "/api/v1/tenant", map[string]string{apiKeyHeader: key})

	expectError(t, rec, http.StatusConflict, "CREDENTIAL_CONFLICT")
}

func TestListAPIKeysFiltersPagesAndSortsWithoutSecrets(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	other := e.tenant("tenant-b", "active")
	_, admin := e.actor(tenant, "admin", false)
	now := e.clock.Now()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	create := func(name string, expires *time.Time) ApiKeyCreated {
		var raw any
		if expires != nil {
			raw = expires.Format(time.RFC3339)
		}
		e.clock.advance(time.Second)
		rec := admin.send(http.MethodPost, "/api/v1/api-keys", createKeyJSON(name, []string{"tenant:read"}, raw))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", name, rec.Code, rec.Body.String())
		}
		return decodeData[ApiKeyCreated](t, rec)
	}
	active := create("b-active", &future)
	expired := create("a-expired", &past)
	revoked := create("c-revoked", nil)
	e.apiKey(other, []string{"tenant:read"}, nil)
	rec := admin.send(http.MethodDelete, "/api/v1/api-keys/"+revoked.ApiKey.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d", rec.Code)
	}

	namesOf := func(query string) []string {
		rec := admin.send(http.MethodGet, "/api/v1/api-keys"+query, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("list %s: %d %s", query, rec.Code, rec.Body.String())
		}
		page := decodeData[struct {
			Items      []ApiKeyView `json:"items"`
			TotalItems int64        `json:"totalItems"`
		}](t, rec)
		var names []string
		for _, item := range page.Items {
			names = append(names, item.Name+":"+item.Status)
		}
		return names
	}
	cases := map[string][]string{
		"":                           {"c-revoked:revoked", "a-expired:expired", "b-active:active"},
		"?status=active":             {"b-active:active"},
		"?status=expired":            {"a-expired:expired"},
		"?status=revoked":            {"c-revoked:revoked"},
		"?sort=name":                 {"a-expired:expired", "b-active:active", "c-revoked:revoked"},
		"?sort=-name&size=2":         {"c-revoked:revoked", "b-active:active"},
		"?sort=name&size=2&page=2":   {"c-revoked:revoked"},
		"?sort=createdAt":            {"b-active:active", "a-expired:expired", "c-revoked:revoked"},
		"?status=active&sort=-name":  {"b-active:active"},
		"?status=revoked&sort=-name": {"c-revoked:revoked"},
	}
	for query, want := range cases {
		got := namesOf(query)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("list %q = %v, want %v", query, got, want)
		}
	}
	expectIssues(t, admin.send(http.MethodGet, "/api/v1/api-keys?status=nope&sort=secret", ""),
		[]wantIssue{{"status", "FIELD_NOT_ALLOWED"}, {"sort", "FIELD_NOT_ALLOWED"}})
	for _, created := range []ApiKeyCreated{active, expired, revoked} {
		_, secret, _ := splitAPIKey(created.PlainTextKey)
		body := admin.send(http.MethodGet, "/api/v1/api-keys", "").Body.String()
		if strings.Contains(body, secret) || strings.Contains(body, "secret") {
			t.Fatalf("listing leaks secret material: %s", body)
		}
	}
}

func TestListAPIKeysAcceptsAKeyHoldingTheScope(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	both := e.apiKey(tenant, []string{"tenant:read", "api-keys:read"}, nil)
	narrow := e.apiKey(tenant, []string{"tenant:read"}, nil)

	ok := e.withKey("/api/v1/api-keys", both)
	if ok.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", ok.Code, ok.Body.String())
	}
	expectError(t, e.withKey("/api/v1/api-keys", narrow), http.StatusForbidden, "INSUFFICIENT_SCOPE")
}

func TestRevokeAPIKeyIsIdempotentAndScopedToTheTenant(t *testing.T) {
	e := newInvEnv(t, "")
	tenantA := e.tenant("tenant-a", "active")
	tenantB := e.tenant("tenant-b", "active")
	_, adminA := e.actor(tenantA, "admin", false)
	_, adminB := e.actor(tenantB, "admin", false)
	created := decodeData[ApiKeyCreated](t, adminB.send(http.MethodPost, "/api/v1/api-keys", createKeyJSON("b", []string{"tenant:read"}, nil)))
	own := decodeData[ApiKeyCreated](t, adminA.send(http.MethodPost, "/api/v1/api-keys", createKeyJSON("a", []string{"tenant:read"}, nil)))

	first := adminA.send(http.MethodDelete, "/api/v1/api-keys/"+own.ApiKey.ID, "")
	e.clock.advance(time.Minute)
	second := adminA.send(http.MethodDelete, "/api/v1/api-keys/"+own.ApiKey.ID, "")

	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("statuses = %d %d", first.Code, second.Code)
	}
	one, two := decodeData[ApiKeyView](t, first), decodeData[ApiKeyView](t, second)
	if one.Status != "revoked" || one.RevokedAt == nil || two.RevokedAt == nil || !one.RevokedAt.Equal(*two.RevokedAt) {
		t.Fatalf("revoke views = %+v %+v", one, two)
	}

	foreign := adminA.send(http.MethodDelete, "/api/v1/api-keys/"+created.ApiKey.ID, "")
	unknown := adminA.send(http.MethodDelete, "/api/v1/api-keys/"+randomUUID(t), "")
	malformed := adminA.send(http.MethodDelete, "/api/v1/api-keys/not-a-uuid", "")
	expectError(t, foreign, http.StatusNotFound, "RESOURCE_NOT_FOUND")
	if errorJSON(t, foreign) != errorJSON(t, unknown) || errorJSON(t, foreign) != errorJSON(t, malformed) {
		t.Fatal("404 bodies differ between foreign, unknown and malformed ids")
	}
	var revokedAt *time.Time
	err := e.pool.QueryRow(context.Background(), "SELECT revoked_at FROM iam_api_keys WHERE id = $1::uuid", created.ApiKey.ID).Scan(&revokedAt)
	if err != nil || revokedAt != nil {
		t.Fatalf("the foreign key was touched: %v %v", revokedAt, err)
	}
}

func TestListAPIKeyScopesIsOrderedByName(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)

	rec := admin.send(http.MethodGet, "/api/v1/api-key-scopes", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	scopes := decodeData[[]ScopeInfo](t, rec)
	var names []string
	for _, scope := range scopes {
		if scope.Description == "" {
			t.Fatalf("scope %q has no description", scope.Name)
		}
		names = append(names, scope.Name)
	}
	if !reflect.DeepEqual(names, []string{"api-keys:read", "tenant:read"}) {
		t.Fatalf("scopes = %v", names)
	}
}

func TestResolverRejectionsAreIdenticalAndStatusIsReadAfterTheSecret(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	now := e.clock.Now()
	valid := e.apiKey(tenant, []string{"tenant:read"}, nil)
	revoked := e.apiKey(tenant, []string{"tenant:read"}, nil)
	expired := e.apiKey(tenant, []string{"tenant:read"}, ptr(now.Add(-time.Minute)))
	boundary := e.apiKey(tenant, []string{"tenant:read"}, &now)
	lastSecond := e.apiKey(tenant, []string{"tenant:read"}, ptr(now.Add(time.Second)))
	prefix, _, _ := splitAPIKey(revoked)
	e.exec("UPDATE iam_api_keys SET revoked_at = $2 WHERE key_prefix = $1", prefix, now)

	validPrefix, _, _ := splitAPIKey(valid)
	other, _ := newAPIKey()
	_, otherSecret, _ := splitAPIKey(other)
	wrongSecret := validPrefix + "_" + otherSecret
	unknown, _ := newAPIKey()
	rejected := map[string]string{
		"revoked": revoked, "expired": expired, "expires at now": boundary,
		"wrong secret": wrongSecret, "unknown prefix": unknown,
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
	e.exec("UPDATE ten_tenants SET status = 'suspended' WHERE id = $1::uuid", tenant)
	expectError(t, e.withKey("/api/v1/tenant", valid), http.StatusForbidden, "TENANT_SUSPENDED")
	for name, key := range rejected {
		rec := e.withKey("/api/v1/tenant", key)
		expectError(t, rec, http.StatusUnauthorized, "API_KEY_INVALID_OR_REVOKED")
		if errorJSON(t, rec) != reference {
			t.Fatalf("%s: suspension changed the rejection", name)
		}
	}
	e.exec("UPDATE ten_tenants SET status = 'active' WHERE id = $1::uuid", tenant)
	if rec := e.withKey("/api/v1/tenant", valid); rec.Code != http.StatusOK {
		t.Fatalf("reactivated tenant answered %d", rec.Code)
	}
}

func TestResolverRejectsRepeatedHeadersAndUnknownHashSchemes(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	valid := e.apiKey(tenant, []string{"tenant:read"}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenant", nil)
	req.Header.Add(apiKeyHeader, valid)
	req.Header.Add(apiKeyHeader, valid)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	expectError(t, rec, http.StatusUnauthorized, "API_KEY_INVALID_OR_REVOKED")

	prefix, _, _ := splitAPIKey(valid)
	e.exec("UPDATE iam_api_keys SET hash_scheme = 'argon2' WHERE key_prefix = $1", prefix)
	expectError(t, e.withKey("/api/v1/tenant", valid), http.StatusUnauthorized, "API_KEY_INVALID_OR_REVOKED")
}

func TestResolverBuildsThePrincipalAndDropsUnregisteredScopes(t *testing.T) {
	e := newInvEnv(t, "")
	tenant := e.tenant("tenant-a", "active")
	plain := e.apiKey(tenant, []string{"tenant:read", "removed:scope"}, nil)
	prefix, _, _ := splitAPIKey(plain)
	registry := auth.NewScopeRegistry()
	if err := registry.Register("tenant:read", "x"); err != nil {
		t.Fatal(err)
	}
	resolver := newAPIKeyResolver(e.pool, e.clock, registry, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenant", nil)
	req.Header.Set(apiKeyHeader, plain)

	if !resolver.Present(req) {
		t.Fatal("Present = false")
	}
	principal, err := resolver.Resolve(context.Background(), req)

	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := reqctx.TenantID(tenant)
	if principal.Kind != reqctx.ActorAPIKey || principal.ActorID != "apikey:"+prefix || principal.TenantID == nil ||
		*principal.TenantID != want || principal.TenantSlug != "tenant-a" || principal.Role != "" ||
		principal.IsPlatformOperator || !reflect.DeepEqual(principal.Scopes, []string{"tenant:read"}) {
		t.Fatalf("principal = %+v", principal)
	}
	if resolver.Present(httptest.NewRequest(http.MethodGet, "/", nil)) {
		t.Fatal("Present = true without the header")
	}
}

func TestAPIKeyTenantRefIsCheckedAgainstTheKeysTenant(t *testing.T) {
	e := newInvEnv(t, "")
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

func TestCreateAPIKeyRefusesAChangedActiveTenant(t *testing.T) {
	e := newInvEnv(t, "")
	tenantA := e.tenant("tenant-a", "active")
	tenantC := e.tenant("tenant-c", "active")
	_, admin := e.actor(tenantA, "admin", false)
	body := createKeyJSON("a", []string{"tenant:read"}, nil)

	rec := admin.doBody(http.MethodPost, "/api/v1/api-keys",
		map[string]string{csrfHeader: admin.cookies["XSRF-TOKEN"], "X-Active-Tenant-Id": tenantC}, body)
	expectError(t, rec, http.StatusConflict, "ACTIVE_TENANT_CHANGED")
	count := e.scan("SELECT count(*)::text FROM iam_api_keys")
	if count != "0" {
		t.Fatalf("keys stored = %s", count)
	}

	rec = admin.doBody(http.MethodPost, "/api/v1/api-keys",
		map[string]string{csrfHeader: admin.cookies["XSRF-TOKEN"], "X-Active-Tenant-Id": tenantA}, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}

func ptr[T any](v T) *T {
	return &v
}
