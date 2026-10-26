package parameters_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/auth"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/coretest"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/parameters"
)

// actorSessions is a SessionResolver fake at the session boundary: the value
// of the test_session cookie names the principal; X-XSRF-TOKEN "ok" passes CSRF.
type actorSessions struct {
	principals map[string]reqctx.Principal
}

func (*actorSessions) Present(r *http.Request) bool {
	_, err := r.Cookie("test_session")
	hasCookie := err == nil
	return hasCookie
}

func (s *actorSessions) Resolve(r *http.Request) (auth.SessionResult, error) {
	cookie, err := r.Cookie("test_session")
	if err != nil {
		return auth.SessionResult{State: auth.SessionInvalid}, nil
	}
	principal, known := s.principals[cookie.Value]
	if !known {
		return auth.SessionResult{State: auth.SessionInvalid}, nil
	}
	result := auth.SessionResult{State: auth.SessionValid, Principal: principal, CSRFValid: r.Header.Get("X-XSRF-TOKEN") == "ok"}
	return result, nil
}

// env is the module behind its real chain with one actor of each kind.
type env struct {
	handler http.Handler
	tenantA string
	tenantB string
}

func accountPrincipal(t testing.TB, tenant string, role reqctx.Role, operator bool) reqctx.Principal {
	t.Helper()
	account := randomUUID(t)
	p := reqctx.Principal{
		Kind:               reqctx.ActorAccount,
		ActorID:            account,
		AccountID:          reqctx.AccountID(account),
		IsPlatformOperator: operator,
	}
	if tenant != "" {
		tenantID := reqctx.TenantID(tenant)
		p.TenantID = &tenantID
		p.Role = role
	}
	return p
}

func newEnv(t testing.TB, logger *slog.Logger) *env {
	t.Helper()
	e := &env{tenantA: randomUUID(t), tenantB: randomUUID(t)}
	sessions := &actorSessions{principals: map[string]reqctx.Principal{
		"operator":   accountPrincipal(t, "", "", true),
		"adminA":     accountPrincipal(t, e.tenantA, reqctx.RoleAdmin, false),
		"memberA":    accountPrincipal(t, e.tenantA, reqctx.RoleMember, false),
		"adminB":     accountPrincipal(t, e.tenantB, reqctx.RoleAdmin, false),
		"tenantless": accountPrincipal(t, "", "", false),
	}}
	e.handler = coretest.New(t, []core.Module{parameters.Module()}, coretest.Options{
		Database:        true,
		SessionResolver: sessions,
		Logger:          logger,
	})
	return e
}

// call sends one request as actor ("" sends no cookie).
func (e *env) call(actor, method, path, contentType, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if actor != "" {
		request.AddCookie(&http.Cookie{Name: "test_session", Value: actor})
		request.Header.Set("X-XSRF-TOKEN", "ok")
	}
	recorder := httptest.NewRecorder()
	e.handler.ServeHTTP(recorder, request)
	return recorder
}

func (e *env) json(actor, method, path, body string) *httptest.ResponseRecorder {
	return e.call(actor, method, path, "application/json", body)
}

func decodeEnvelope(t testing.TB, rec *httptest.ResponseRecorder) api.Envelope[json.RawMessage] {
	t.Helper()
	var envelope api.Envelope[json.RawMessage]
	err := json.Unmarshal(rec.Body.Bytes(), &envelope)
	if err != nil {
		t.Fatalf("not an envelope (status %d): %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Trace-Id") != envelope.Meta.TraceID {
		t.Fatalf("X-Trace-Id %q differs from meta.traceId %q", rec.Header().Get("X-Trace-Id"), envelope.Meta.TraceID)
	}
	return envelope
}

func wantData[T any](t testing.TB, rec *httptest.ResponseRecorder, status int) T {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
	envelope := decodeEnvelope(t, rec)
	if envelope.Error != nil {
		t.Fatalf("unexpected error: %+v", envelope.Error)
	}
	var data T
	if envelope.Data == nil {
		t.Fatalf("data is null: %s", rec.Body.String())
	}
	err := json.Unmarshal(*envelope.Data, &data)
	if err != nil {
		t.Fatalf("data: %v: %s", err, *envelope.Data)
	}
	return data
}

func wantError(t testing.TB, rec *httptest.ResponseRecorder, status int, code api.Code) *api.Error {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
	envelope := decodeEnvelope(t, rec)
	if envelope.Error == nil || envelope.Error.Code != code || envelope.Data != nil {
		t.Fatalf("want error %s, got %s", code, rec.Body.String())
	}
	return envelope.Error
}

func wantIssues(t testing.TB, apiErr *api.Error, want ...string) {
	t.Helper()
	got := issueList(apiErr)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("issues:\n got  %v\n want %v", got, want)
	}
}

const numberDefinition = `{"key":"export.max_rows","valueType":"number","defaultValue":50,"minValue":1,"maxValue":500}`

func TestOperatorDefinesAndTenantsOverride(t *testing.T) {
	e := newEnv(t, nil)

	created := wantData[parameters.View](t, e.json("operator", http.MethodPost, "/api/v1/parameters", numberDefinition), http.StatusCreated)
	if created.Key != "export.max_rows" || created.Source != "global" || string(created.EffectiveValue) != "50" {
		t.Fatalf("created = %+v", created)
	}

	set := wantData[parameters.View](t, e.json("adminA", http.MethodPut, "/api/v1/parameters/export.max_rows/tenant-override", `{"value":200}`), http.StatusOK)
	if set.Source != "tenant" || string(set.EffectiveValue) != "200" || string(set.DefaultValue) != "50" {
		t.Fatalf("override = %+v", set)
	}

	pageA := wantData[api.Page[parameters.View]](t, e.json("adminA", http.MethodGet, "/api/v1/parameters", ""), http.StatusOK)
	pageB := wantData[api.Page[parameters.View]](t, e.json("adminB", http.MethodGet, "/api/v1/parameters", ""), http.StatusOK)
	pageNone := wantData[api.Page[parameters.View]](t, e.json("tenantless", http.MethodGet, "/api/v1/parameters", ""), http.StatusOK)
	if pageA.Items[0].Source != "tenant" || pageB.Items[0].Source != "global" || pageNone.Items[0].Source != "global" {
		t.Fatalf("sources = %s %s %s", pageA.Items[0].Source, pageB.Items[0].Source, pageNone.Items[0].Source)
	}

	for attempt := 0; attempt < 2; attempt++ {
		cleared := wantData[parameters.View](t, e.json("adminA", http.MethodDelete, "/api/v1/parameters/export.max_rows/tenant-override", ""), http.StatusOK)
		if cleared.Source != "global" || string(cleared.EffectiveValue) != "50" {
			t.Fatalf("clear %d = %+v", attempt, cleared)
		}
	}
}

func TestReplaceAndDeleteAnswerTheirContractShapes(t *testing.T) {
	e := newEnv(t, nil)
	wantData[parameters.View](t, e.json("operator", http.MethodPost, "/api/v1/parameters", `{"key":"custom.banner","valueType":"string","defaultValue":"Hello"}`), http.StatusCreated)
	wantData[parameters.View](t, e.json("adminA", http.MethodPut, "/api/v1/parameters/custom.banner/tenant-override", `{"value":"Hi"}`), http.StatusOK)

	replaced := wantData[parameters.View](t, e.json("operator", http.MethodPut, "/api/v1/parameters/custom.banner", `{"defaultValue":"Welcome","description":"Banner"}`), http.StatusOK)
	if replaced.Key != "custom.banner" || string(replaced.DefaultValue) != "\"Welcome\"" || replaced.Source != "global" {
		t.Fatalf("replaced = %+v", replaced)
	}

	deleted := wantData[parameters.Deleted](t, e.json("operator", http.MethodDelete, "/api/v1/parameters/custom.banner", ""), http.StatusOK)
	if deleted.Key != "custom.banner" || deleted.DeletedOverrideCount != 1 {
		t.Fatalf("deleted = %+v", deleted)
	}
	wantError(t, e.json("operator", http.MethodDelete, "/api/v1/parameters/custom.banner", ""), http.StatusNotFound, api.CodeResourceNotFound)
}

func TestRouteErrorsFollowTheContract(t *testing.T) {
	e := newEnv(t, nil)
	wantData[parameters.View](t, e.json("operator", http.MethodPost, "/api/v1/parameters", numberDefinition), http.StatusCreated)
	wantData[parameters.View](t, e.json("operator", http.MethodPost, "/api/v1/parameters", `{"key":"sys.locked","valueType":"string","defaultValue":"x","isSystemRequired":true}`), http.StatusCreated)
	wantData[parameters.View](t, e.json("adminA", http.MethodPut, "/api/v1/parameters/export.max_rows/tenant-override", `{"value":400}`), http.StatusOK)

	cases := []struct {
		name   string
		actor  string
		method string
		path   string
		body   string
		status int
		code   api.Code
	}{
		{"duplicate key", "operator", http.MethodPost, "/api/v1/parameters", numberDefinition, 409, "PARAMETER_ALREADY_EXISTS"},
		{"system parameter", "operator", http.MethodDelete, "/api/v1/parameters/sys.locked", "", 409, "SYSTEM_PARAMETER_PROTECTED"},
		{"overrides violate", "operator", http.MethodPut, "/api/v1/parameters/export.max_rows", `{"defaultValue":50,"minValue":1,"maxValue":100}`, 409, "PARAMETER_OVERRIDES_VIOLATE_CONSTRAINTS"},
		{"member cannot override", "memberA", http.MethodPut, "/api/v1/parameters/export.max_rows/tenant-override", `{"value":5}`, 403, "INSUFFICIENT_ROLE"},
		{"member cannot clear", "memberA", http.MethodDelete, "/api/v1/parameters/export.max_rows/tenant-override", "", 403, "INSUFFICIENT_ROLE"},
		{"operator is not a tenant admin", "operator", http.MethodPut, "/api/v1/parameters/export.max_rows/tenant-override", `{"value":5}`, 403, "ACTIVE_TENANT_REQUIRED"},
		{"tenantless cannot clear", "tenantless", http.MethodDelete, "/api/v1/parameters/export.max_rows/tenant-override", "", 403, "ACTIVE_TENANT_REQUIRED"},
		{"admin cannot define", "adminA", http.MethodPost, "/api/v1/parameters", numberDefinition, 403, "PLATFORM_OPERATOR_REQUIRED"},
		{"admin cannot replace", "adminA", http.MethodPut, "/api/v1/parameters/export.max_rows", `{"defaultValue":5}`, 403, "PLATFORM_OPERATOR_REQUIRED"},
		{"admin cannot delete", "adminA", http.MethodDelete, "/api/v1/parameters/export.max_rows", "", 403, "PLATFORM_OPERATOR_REQUIRED"},
		{"anonymous list", "", http.MethodGet, "/api/v1/parameters", "", 401, "SESSION_INVALID"},
		{"anonymous mutation", "", http.MethodPost, "/api/v1/parameters", numberDefinition, 401, "SESSION_INVALID"},
		{"unknown key replace", "operator", http.MethodPut, "/api/v1/parameters/no.such", `{"defaultValue":1}`, 404, "RESOURCE_NOT_FOUND"},
		{"unknown key delete", "operator", http.MethodDelete, "/api/v1/parameters/no.such", "", 404, "RESOURCE_NOT_FOUND"},
		{"unknown key override", "adminA", http.MethodPut, "/api/v1/parameters/no.such/tenant-override", `{"value":1}`, 404, "RESOURCE_NOT_FOUND"},
		{"unknown key clear", "adminA", http.MethodDelete, "/api/v1/parameters/no.such/tenant-override", "", 404, "RESOURCE_NOT_FOUND"},
		{"malformed key", "adminA", http.MethodPut, "/api/v1/parameters/Bad%20Key/tenant-override", `{"value":1}`, 404, "RESOURCE_NOT_FOUND"},
		{"malformed key delete", "operator", http.MethodDelete, "/api/v1/parameters/.x..", "", 404, "RESOURCE_NOT_FOUND"},
		{"wrong method", "operator", http.MethodPatch, "/api/v1/parameters", `{}`, 404, "ROUTE_NOT_FOUND"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantError(t, e.json(tc.actor, tc.method, tc.path, tc.body), tc.status, tc.code)
		})
	}
}

func TestMutationsNeedTheCSRFHeader(t *testing.T) {
	e := newEnv(t, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/parameters", strings.NewReader(numberDefinition))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "operator"})
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, request)
	wantError(t, rec, http.StatusForbidden, api.CodeCSRFTokenInvalid)
}

func TestActiveTenantChangedStopsAMutation(t *testing.T) {
	e := newEnv(t, nil)
	wantData[parameters.View](t, e.json("operator", http.MethodPost, "/api/v1/parameters", numberDefinition), http.StatusCreated)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/parameters/export.max_rows/tenant-override", strings.NewReader(`{"value":5}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-XSRF-TOKEN", "ok")
	request.Header.Set("X-Active-Tenant-Id", e.tenantB)
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "adminA"})
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, request)
	wantError(t, rec, http.StatusConflict, api.CodeActiveTenantChanged)
	page := wantData[api.Page[parameters.View]](t, e.json("adminA", http.MethodGet, "/api/v1/parameters", ""), http.StatusOK)
	if page.Items[0].Source != "global" {
		t.Fatal("the refused mutation must not write")
	}
}

func TestBodyErrorsComeBeforeLookups(t *testing.T) {
	e := newEnv(t, nil)
	malformed := wantError(t, e.json("operator", http.MethodPost, "/api/v1/parameters", "{"), http.StatusUnprocessableEntity, api.CodeValidationFailed)
	wantIssues(t, malformed, ":BODY_MALFORMED")
	wantError(t, e.call("operator", http.MethodPost, "/api/v1/parameters", "text/plain", "{}"), http.StatusUnsupportedMediaType, api.CodeUnsupportedMediaType)
	big := `{"description":"` + strings.Repeat("a", 2<<20) + `"}`
	wantError(t, e.json("operator", http.MethodPost, "/api/v1/parameters", big), http.StatusRequestEntityTooLarge, api.CodePayloadTooLarge)

	// A body problem on an unknown key answers before the 404.
	missing := wantError(t, e.json("operator", http.MethodPut, "/api/v1/parameters/no.such", "{"), http.StatusUnprocessableEntity, api.CodeValidationFailed)
	wantIssues(t, missing, ":BODY_MALFORMED")
	wantError(t, e.call("adminA", http.MethodPut, "/api/v1/parameters/no.such/tenant-override", "text/plain", "x"), http.StatusUnsupportedMediaType, api.CodeUnsupportedMediaType)
}

func TestCreateAccumulatesEveryProblem(t *testing.T) {
	e := newEnv(t, nil)
	rec := e.json("operator", http.MethodPost, "/api/v1/parameters", `{"key":"Bad Key","valueType":"number","defaultValue":"x","minValue":5,"maxValue":1}`)
	apiErr := wantError(t, rec, http.StatusUnprocessableEntity, api.CodeValidationFailed)
	wantIssues(t, apiErr, "key:PARAMETER_KEY_INVALID", "maxValue:PARAMETER_CONSTRAINT_INVALID", "defaultValue:PARAMETER_TYPE_MISMATCH")
}

func TestOverrideValuesAreValidated(t *testing.T) {
	e := newEnv(t, nil)
	wantData[parameters.View](t, e.json("operator", http.MethodPost, "/api/v1/parameters", numberDefinition), http.StatusCreated)
	cases := []struct {
		body string
		want string
	}{
		{`{"value":"five"}`, "value:PARAMETER_TYPE_MISMATCH"},
		{`{"value":0}`, "value:PARAMETER_OUT_OF_RANGE"},
		{`{"value":501}`, "value:PARAMETER_OUT_OF_RANGE"},
		{`{}`, "value:FIELD_REQUIRED"},
	}
	for _, tc := range cases {
		rec := e.json("adminA", http.MethodPut, "/api/v1/parameters/export.max_rows/tenant-override", tc.body)
		wantIssues(t, wantError(t, rec, http.StatusUnprocessableEntity, api.CodeValidationFailed), tc.want)
	}
}

func TestListPagesAndSorts(t *testing.T) {
	e := newEnv(t, nil)
	for index := 0; index < 25; index++ {
		body := fmt.Sprintf(`{"key":"page.k%02d","valueType":"number","defaultValue":%d}`, index, index)
		wantData[parameters.View](t, e.json("operator", http.MethodPost, "/api/v1/parameters", body), http.StatusCreated)
	}
	first := wantData[api.Page[parameters.View]](t, e.json("adminA", http.MethodGet, "/api/v1/parameters", ""), http.StatusOK)
	if first.Page != 1 || first.Size != 20 || first.TotalItems != 25 || len(first.Items) != 20 || first.Items[0].Key != "page.k00" {
		t.Fatalf("first page = %d/%d total %d items %d", first.Page, first.Size, first.TotalItems, len(first.Items))
	}
	desc := wantData[api.Page[parameters.View]](t, e.json("adminA", http.MethodGet, "/api/v1/parameters?sort=-key&size=5&page=2", ""), http.StatusOK)
	if len(desc.Items) != 5 || desc.Items[0].Key != "page.k19" {
		t.Fatalf("desc page 2 = %+v", desc.Items)
	}
	bad := []struct {
		query string
		want  string
	}{
		{"page=x", "page:FIELD_TYPE_INVALID"},
		{"page=0", "page:FIELD_OUT_OF_RANGE"},
		{"size=101", "size:FIELD_OUT_OF_RANGE"},
		{"sort=name", "sort:FIELD_NOT_ALLOWED"},
	}
	for _, tc := range bad {
		rec := e.json("adminA", http.MethodGet, "/api/v1/parameters?"+tc.query, "")
		wantIssues(t, wantError(t, rec, http.StatusUnprocessableEntity, api.CodeValidationFailed), tc.want)
	}
}

func TestATenantCannotReadOrClearAnotherTenantsOverride(t *testing.T) {
	e := newEnv(t, nil)
	wantData[parameters.View](t, e.json("operator", http.MethodPost, "/api/v1/parameters", numberDefinition), http.StatusCreated)
	wantData[parameters.View](t, e.json("adminA", http.MethodPut, "/api/v1/parameters/export.max_rows/tenant-override", `{"value":7}`), http.StatusOK)

	cleared := wantData[parameters.View](t, e.json("adminB", http.MethodDelete, "/api/v1/parameters/export.max_rows/tenant-override", ""), http.StatusOK)
	if cleared.Source != "global" || string(cleared.EffectiveValue) != "50" {
		t.Fatalf("tenant B view = %+v", cleared)
	}
	pageA := wantData[api.Page[parameters.View]](t, e.json("adminA", http.MethodGet, "/api/v1/parameters", ""), http.StatusOK)
	if pageA.Items[0].Source != "tenant" || string(pageA.Items[0].EffectiveValue) != "7" {
		t.Fatalf("tenant A lost its override: %+v", pageA.Items[0])
	}
}

func TestOverrideWriteLogCarriesTheCorrelationFields(t *testing.T) {
	var buffer bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	e := newEnv(t, logger)
	wantData[parameters.View](t, e.json("operator", http.MethodPost, "/api/v1/parameters", numberDefinition), http.StatusCreated)
	wantData[parameters.View](t, e.json("adminA", http.MethodPut, "/api/v1/parameters/export.max_rows/tenant-override", `{"value":9}`), http.StatusOK)

	var found map[string]any
	for _, line := range strings.Split(buffer.String(), "\n") {
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) == nil && record["msg"] == "parameter_override_set" {
			found = record
		}
	}
	if found == nil {
		t.Fatalf("no parameter_override_set record in:\n%s", buffer.String())
	}
	for _, field := range []string{"trace_id", "span_id", "tenant_id", "actor_id"} {
		value, _ := found[field].(string)
		if value == "" {
			t.Errorf("record lacks %s: %v", field, found)
		}
	}
	if found["tenant_id"] != e.tenantA {
		t.Errorf("tenant_id = %v, want %s", found["tenant_id"], e.tenantA)
	}
}

func FuzzHandlers(f *testing.F) {
	f.Add([]byte(numberDefinition), true)
	f.Add([]byte(`{"value":1}`), false)
	f.Add([]byte(`{"value":null}`), false)
	f.Add([]byte(`{"key":1,"valueType":[],"defaultValue":{}}`), true)
	f.Add([]byte("{"), false)
	f.Add([]byte(`{"value":1e999}`), false)
	f.Add([]byte{0xff, 0xfe, '{', '}'}, true)
	e := newEnv(f, nil)
	wantData[parameters.View](f, e.json("operator", http.MethodPost, "/api/v1/parameters", `{"key":"fuzz.k","valueType":"number","defaultValue":1}`), http.StatusCreated)
	f.Fuzz(func(t *testing.T, payload []byte, isCreate bool) {
		actor, method, path := "adminA", http.MethodPut, "/api/v1/parameters/fuzz.k/tenant-override"
		if isCreate {
			actor, method, path = "operator", http.MethodPost, "/api/v1/parameters"
		}
		rec := e.call(actor, method, path, "application/json", string(payload))
		if rec.Code == http.StatusInternalServerError {
			t.Fatalf("500 for %q: %s", payload, rec.Body.String())
		}
		var envelope api.Envelope[json.RawMessage]
		err := json.Unmarshal(rec.Body.Bytes(), &envelope)
		if err != nil || rec.Header().Get("X-Trace-Id") == "" {
			t.Fatalf("not an envelope for %q (status %d): %q", payload, rec.Code, rec.Body.String())
		}
	})
}
