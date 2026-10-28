package apikeys

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/db"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

func TestCreateAPIKeyReturnsThePlainKeyOnceAndNeverStoresOrLogsIt(t *testing.T) {
	e := newEnv(t)
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)

	rec := admin.send(http.MethodPost, "/api/v1/api-keys", createKeyJSON("  billing-sync ", []string{"tenant:read"}, nil))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	created := decodeData[ApiKeyCreated](t, rec)
	if !keyPattern.MatchString(created.PlainTextKey) {
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

// sequenceKeys returns a generator that yields keys in order, then fails.
func sequenceKeys(keys ...string) func() (string, error) {
	next := 0
	return func() (string, error) {
		if next >= len(keys) {
			return "", errors.New("generator exhausted")
		}
		key := keys[next]
		next++
		return key, nil
	}
}

func TestCreateAPIKeyRetriesAfterAPrefixCollision(t *testing.T) {
	e := newEnv(t)
	tenant := e.tenant("tenant-a", "active")
	creator := e.person()
	taken := e.apiKey(tenant, []string{"tenant:read"}, nil)
	fresh, err := newAPIKey()
	if err != nil {
		t.Fatalf("new key: %v", err)
	}
	takenPrefix, _, _ := splitAPIKey(taken)
	collidingPlain := takenPrefix + "_" + strings.Repeat("a", keySecretChars)
	service := newAPIKeys(e.pool, e.clock)

	service.generate = sequenceKeys(collidingPlain, fresh)
	created, err := service.Create(context.Background(), tenantScope(tenant), reqctx.AccountID(creator.accountID), "retry", []string{"tenant:read"}, nil)

	if err != nil {
		t.Fatalf("create after one collision: %v", err)
	}
	if created.PlainTextKey != fresh {
		t.Fatalf("plain key = %q, want the second generated key", created.PlainTextKey)
	}
}

func TestCreateAPIKeyFailsWhenEveryAttemptCollides(t *testing.T) {
	e := newEnv(t)
	tenant := e.tenant("tenant-a", "active")
	creator := e.person()
	taken := e.apiKey(tenant, []string{"tenant:read"}, nil)
	takenPrefix, _, _ := splitAPIKey(taken)
	collidingPlain := takenPrefix + "_" + strings.Repeat("a", keySecretChars)
	service := newAPIKeys(e.pool, e.clock)

	service.generate = sequenceKeys(collidingPlain, collidingPlain, collidingPlain, collidingPlain)
	_, err := service.Create(context.Background(), tenantScope(tenant), reqctx.AccountID(creator.accountID), "retry", []string{"tenant:read"}, nil)

	if !db.IsUniqueViolation(err) {
		t.Fatalf("create error = %v, want a unique violation after every attempt collides", err)
	}
}

func TestCreateAPIKeyValidationAccumulates(t *testing.T) {
	e := newEnv(t)
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
		{"NUL in name", `{"name":"billing\u0000sync","scopes":["tenant:read"]}`,
			[]wantIssue{{"name", "FIELD_INVALID_FORMAT"}}},
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
	e := newEnv(t)
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
	e := newEnv(t)
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
		{http.MethodPost, "/api/v1/probe"},
		{http.MethodGet, "/api/v1/probe"},
	}
	for _, route := range routes {
		t.Run("key only "+route.method+" "+route.path, func(t *testing.T) {
			rec := e.mutateWithKey(route.method, route.path, key)
			expectError(t, rec, http.StatusUnauthorized, "SESSION_INVALID")
		})
	}
}

func TestAPIKeyAndSessionTogetherAreAConflict(t *testing.T) {
	e := newEnv(t)
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	key := e.apiKey(tenant, []string{"tenant:read"}, nil)

	rec := admin.do(http.MethodGet, "/api/v1/tenant", map[string]string{apiKeyHeader: key})

	expectError(t, rec, http.StatusConflict, "CREDENTIAL_CONFLICT")
}

func TestListAPIKeysFiltersPagesAndSortsWithoutSecrets(t *testing.T) {
	e := newEnv(t)
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
	e := newEnv(t)
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
	e := newEnv(t)
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
	err := e.pool.QueryRow(context.Background(), "SELECT revoked_at FROM apk_keys WHERE id = $1::uuid", created.ApiKey.ID).Scan(&revokedAt)
	if err != nil || revokedAt != nil {
		t.Fatalf("the foreign key was touched: %v %v", revokedAt, err)
	}
}

func TestListAPIKeyScopesIsOrderedByNameAndCarriesTheLevel(t *testing.T) {
	e := newEnv(t)
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
		wantLevel := "tenant"
		if scope.Name == whoamiScope {
			wantLevel = "any"
		}
		if scope.Level != wantLevel {
			t.Fatalf("scope %q has level %q, want %s", scope.Name, scope.Level, wantLevel)
		}
		names = append(names, scope.Name)
	}
	if !reflect.DeepEqual(names, []string{"api-keys:read", whoamiScope, "tenant:read"}) {
		t.Fatalf("scopes = %v", names)
	}
}

func TestCreateAPIKeyRefusesAChangedActiveTenant(t *testing.T) {
	e := newEnv(t)
	tenantA := e.tenant("tenant-a", "active")
	tenantC := e.tenant("tenant-c", "active")
	_, admin := e.actor(tenantA, "admin", false)
	body := createKeyJSON("a", []string{"tenant:read"}, nil)

	rec := admin.doBody(http.MethodPost, "/api/v1/api-keys",
		map[string]string{csrfHeader: admin.cookies["XSRF-TOKEN"], "X-Active-Tenant-Id": tenantC}, body)
	expectError(t, rec, http.StatusConflict, "ACTIVE_TENANT_CHANGED")
	count := e.scan("SELECT count(*)::text FROM apk_keys")
	if count != "0" {
		t.Fatalf("keys stored = %s", count)
	}

	rec = admin.doBody(http.MethodPost, "/api/v1/api-keys",
		map[string]string{csrfHeader: admin.cookies["XSRF-TOKEN"], "X-Active-Tenant-Id": tenantA}, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAPIKeyRoutesFollowTheCallerTable(t *testing.T) {
	e := newEnv(t)
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	_, viewer := e.actor(tenant, "viewer", false)
	_, viewerOperator := e.actor(tenant, "viewer", true)
	_, tenantlessOperator := e.actor("", "", true)
	_, tenantlessPlain := e.actor("", "", false)
	createBody := createKeyJSON("routes", []string{"tenant:read"}, nil)
	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/api-keys", ""},
		{http.MethodGet, "/api/v1/api-key-scopes", ""},
		{http.MethodPost, "/api/v1/api-keys", createBody},
		{http.MethodDelete, "/api/v1/api-keys/" + randomUUID(t), ""},
	}

	t.Run("admin with a tenant passes every route in tenant scope", func(t *testing.T) {
		wantStatus := []int{http.StatusOK, http.StatusOK, http.StatusCreated, http.StatusNotFound}
		for i, route := range routes {
			rec := admin.send(route.method, route.path, route.body)
			if rec.Code != wantStatus[i] {
				t.Errorf("%s %s answered %d: %s", route.method, route.path, rec.Code, rec.Body.String())
			}
		}
		stored := e.scan("SELECT count(*)::text FROM apk_keys WHERE tenant_id = $1::uuid", tenant)
		if stored != "1" {
			t.Errorf("keys stored in the tenant = %s, want 1", stored)
		}
	})
	t.Run("a role below admin is refused, operator or not", func(t *testing.T) {
		for name, client := range map[string]*sessClient{"viewer": viewer, "viewer operator": viewerOperator} {
			for _, route := range routes {
				expectError(t, client.send(route.method, route.path, route.body), http.StatusForbidden, "INSUFFICIENT_ROLE")
			}
			if t.Failed() {
				t.Fatalf("%s was not refused on every route", name)
			}
		}
	})
	t.Run("an operator without a tenant works in global scope", func(t *testing.T) {
		list := tenantlessOperator.send(http.MethodGet, "/api/v1/api-keys", "")
		if list.Code != http.StatusOK {
			t.Fatalf("list answered %d: %s", list.Code, list.Body.String())
		}
		page := decodeData[struct {
			Items      []ApiKeyView `json:"items"`
			TotalItems int64        `json:"totalItems"`
		}](t, list)
		if len(page.Items) != 0 || page.TotalItems != 0 {
			t.Errorf("the tenant's key leaked into the global list: %+v", page)
		}
		scopes := tenantlessOperator.send(http.MethodGet, "/api/v1/api-key-scopes", "")
		if scopes.Code != http.StatusOK {
			t.Errorf("scopes answered %d", scopes.Code)
		}
		expectError(t, tenantlessOperator.send(http.MethodDelete, routes[3].path, ""), http.StatusNotFound, "RESOURCE_NOT_FOUND")
	})
	t.Run("a session without a tenant that is not an operator is refused", func(t *testing.T) {
		for _, route := range routes {
			expectError(t, tenantlessPlain.send(route.method, route.path, route.body), http.StatusForbidden, "ACTIVE_TENANT_REQUIRED")
		}
	})
	t.Run("a key with a tenant lists in tenant scope and nothing else", func(t *testing.T) {
		key := e.apiKey(tenant, []string{"api-keys:read"}, nil)
		global := e.apiKey("", []string{"api-keys:read"}, nil)
		globalPrefix, _, _ := splitAPIKey(global)
		tenantPrefix, _, _ := splitAPIKey(key)

		list := e.withKey("/api/v1/api-keys", key)
		hasOwnRow := strings.Contains(list.Body.String(), tenantPrefix)
		if list.Code != http.StatusOK || !hasOwnRow || strings.Contains(list.Body.String(), globalPrefix) {
			t.Fatalf("list answered %d (own row listed: %t): %s", list.Code, hasOwnRow, list.Body.String())
		}
		for _, route := range routes[1:] {
			expectError(t, e.mutateWithKey(route.method, route.path, key), http.StatusUnauthorized, "SESSION_INVALID")
		}
	})
	t.Run("a key without a tenant cannot list and cannot use the admin routes", func(t *testing.T) {
		global := e.apiKey("", []string{"api-keys:read"}, nil)

		expectError(t, e.withKey("/api/v1/api-keys", global), http.StatusForbidden, "ACTIVE_TENANT_REQUIRED")
		for _, route := range routes[1:] {
			expectError(t, e.mutateWithKey(route.method, route.path, global), http.StatusUnauthorized, "SESSION_INVALID")
		}
	})
}

func TestAnOperatorWithoutATenantMintsNoTenantLevelScope(t *testing.T) {
	e := newEnv(t)
	_, operator := e.actor("", "", true)
	tooLong := strings.Repeat("n", 101)

	rec := operator.send(http.MethodPost, "/api/v1/api-keys", createKeyJSON(tooLong, []string{"tenant:read", "api-keys:read"}, nil))

	expectIssues(t, rec, []wantIssue{
		{"name", "FIELD_TOO_LONG"}, {"scopes[0]", "SCOPE_LEVEL_MISMATCH"}, {"scopes[1]", "SCOPE_LEVEL_MISMATCH"},
	})
	if stored := e.scan("SELECT count(*)::text FROM apk_keys"); stored != "0" {
		t.Fatalf("keys stored = %s", stored)
	}
}

func TestAnOperatorWithoutATenantManagesGlobalKeysOnly(t *testing.T) {
	e := newEnv(t)
	tenant := e.tenant("tenant-a", "active")
	_, admin := e.actor(tenant, "admin", false)
	operatorPerson, operator := e.actor("", "", true)
	service := newAPIKeys(e.pool, e.clock)
	global, err := service.Create(context.Background(), nil, reqctx.AccountID(operatorPerson.accountID), "global-sync", []string{"tenant:read"}, nil)
	if err != nil {
		t.Fatalf("create global key: %v", err)
	}
	if !keyPattern.MatchString(global.PlainTextKey) {
		t.Fatalf("global key %q does not match the format", global.PlainTextKey)
	}
	if stored := e.scan("SELECT count(*)::text FROM apk_keys WHERE tenant_id IS NULL"); stored != "1" {
		t.Fatalf("global rows = %s, want 1", stored)
	}
	own := decodeData[ApiKeyCreated](t, admin.send(http.MethodPost, "/api/v1/api-keys", createKeyJSON("own", []string{"tenant:read"}, nil)))

	namesOf := func(client *sessClient, query string) []string {
		rec := client.send(http.MethodGet, "/api/v1/api-keys"+query, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("list %s: %d %s", query, rec.Code, rec.Body.String())
		}
		page := decodeData[struct {
			Items []ApiKeyView `json:"items"`
		}](t, rec)
		names := []string{}
		for _, item := range page.Items {
			names = append(names, item.Name)
		}
		return names
	}
	if got := namesOf(operator, "?sort=name&size=5"); !reflect.DeepEqual(got, []string{"global-sync"}) {
		t.Fatalf("operator list = %v", got)
	}
	if got := namesOf(admin, ""); !reflect.DeepEqual(got, []string{"own"}) {
		t.Fatalf("admin list = %v", got)
	}
	if got := namesOf(operator, "?status=revoked"); len(got) != 0 {
		t.Fatalf("operator revoked list = %v", got)
	}

	expectError(t, admin.send(http.MethodDelete, "/api/v1/api-keys/"+global.ApiKey.ID, ""), http.StatusNotFound, "RESOURCE_NOT_FOUND")
	expectError(t, operator.send(http.MethodDelete, "/api/v1/api-keys/"+own.ApiKey.ID, ""), http.StatusNotFound, "RESOURCE_NOT_FOUND")
	if untouched := e.scan("SELECT count(*)::text FROM apk_keys WHERE revoked_at IS NOT NULL"); untouched != "0" {
		t.Fatalf("a key outside the scope was revoked (%s rows)", untouched)
	}

	revoked := operator.send(http.MethodDelete, "/api/v1/api-keys/"+global.ApiKey.ID, "")
	if revoked.Code != http.StatusOK || decodeData[ApiKeyView](t, revoked).Status != "revoked" {
		t.Fatalf("revoke global: %d %s", revoked.Code, revoked.Body.String())
	}
	if got := namesOf(operator, "?status=revoked"); !reflect.DeepEqual(got, []string{"global-sync"}) {
		t.Fatalf("operator revoked list = %v", got)
	}
}

func TestWithoutTenancyAnOperatorMintsAGlobalKeyThatCallsAnAnyLevelRoute(t *testing.T) {
	e := newEnvWithoutTenancy(t)
	_, operator := e.actor("", "", true)

	refused := operator.send(http.MethodPost, "/api/v1/api-keys", createKeyJSON("tenant-level", []string{"tenant:read"}, nil))
	expectIssues(t, refused, []wantIssue{{"scopes[0]", "SCOPE_LEVEL_MISMATCH"}})

	created := operator.send(http.MethodPost, "/api/v1/api-keys", createKeyJSON("global-probe", []string{whoamiScope}, nil))
	if created.Code != http.StatusCreated {
		t.Fatalf("mint answered %d: %s", created.Code, created.Body.String())
	}
	key := decodeData[ApiKeyCreated](t, created).PlainTextKey

	rec := e.withKey(whoamiPath, key)
	if rec.Code != http.StatusOK {
		t.Fatalf("global key on the any-level route answered %d: %s", rec.Code, rec.Body.String())
	}
	principal := decodeData[whoami](t, rec)
	if principal.Kind != string(reqctx.ActorAPIKey) || principal.TenantID != "" || !reflect.DeepEqual(principal.Scopes, []string{whoamiScope}) {
		t.Fatalf("principal = %+v", principal)
	}

	expectError(t, e.withKey("/api/v1/api-keys", key), http.StatusForbidden, "ACTIVE_TENANT_REQUIRED")
	if leaked := strings.Contains(e.logs.String(), key); leaked {
		t.Fatal("the plain key reached the logs")
	}
}
