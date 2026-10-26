package featureflags_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	pathpkg "path"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/auth"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/coretest"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/featureflag"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/featureflags"
)

// actorSessions is a SessionResolver fake at the session boundary: the
// test_session cookie names the principal; X-XSRF-TOKEN "ok" passes CSRF.
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

// fakeDirectory is the TenantDirectory boundary: known ids exist, failWith
// makes every lookup fail.
type fakeDirectory struct {
	known    map[string]bool
	failWith error
}

func (d fakeDirectory) TenantExists(_ context.Context, id reqctx.TenantID) (bool, error) {
	if d.failWith != nil {
		return false, d.failWith
	}
	return d.known[string(id)], nil
}

// routeEnv is the module behind its real chain with an operator and a tenant admin.
type routeEnv struct {
	handler http.Handler
	tenantA string
	tenantB string
	probe   *env
}

func newRouteEnv(t testing.TB, directoryErr error) *routeEnv {
	t.Helper()
	logger, _ := newLogger()
	return buildRouteEnv(t, directoryErr, logger)
}

func newRouteEnvWithLogger(t testing.TB, logger *slog.Logger) *routeEnv {
	t.Helper()
	return buildRouteEnv(t, nil, logger)
}

func buildRouteEnv(t testing.TB, directoryErr error, logger *slog.Logger) *routeEnv {
	t.Helper()
	e := &routeEnv{tenantA: randomUUID(t), tenantB: randomUUID(t)}
	adminTenant := reqctx.TenantID(e.tenantA)
	operator := randomUUID(t)
	admin := randomUUID(t)
	sessions := &actorSessions{principals: map[string]reqctx.Principal{
		"operator": {Kind: reqctx.ActorAccount, ActorID: operator, AccountID: reqctx.AccountID(operator), IsPlatformOperator: true},
		"admin":    {Kind: reqctx.ActorAccount, ActorID: admin, AccountID: reqctx.AccountID(admin), TenantID: &adminTenant, Role: reqctx.RoleAdmin},
	}}
	e.probe = &env{}
	capture := core.Module{ID: "capture", Mount: func(_ context.Context, h *core.Host) error {
		e.probe.Evaluator = h.FlagEvaluator()
		e.probe.Pool = h.Pool()
		return nil
	}}
	e.handler = coretest.New(t, []core.Module{featureflags.Module(), capture}, coretest.Options{
		Database:        true,
		Config:          map[string]string{"GATBASE_FLAGS_FILE": writeManifest(t, twoFlagManifest)},
		SessionResolver: sessions,
		TenantDirectory: fakeDirectory{known: map[string]bool{e.tenantA: true, e.tenantB: true}, failWith: directoryErr},
		Logger:          logger,
	})
	return e
}

func (e *routeEnv) call(actor, method, path, contentType, body string) *httptest.ResponseRecorder {
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

func (e *routeEnv) json(actor, method, path, body string) *httptest.ResponseRecorder {
	return e.call(actor, method, path, "application/json", body)
}

func decode(t testing.TB, rec *httptest.ResponseRecorder) api.Envelope[json.RawMessage] {
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
	envelope := decode(t, rec)
	if envelope.Error != nil || envelope.Data == nil {
		t.Fatalf("want data, got %s", rec.Body.String())
	}
	var data T
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
	envelope := decode(t, rec)
	if envelope.Error == nil || envelope.Error.Code != code || envelope.Data != nil {
		t.Fatalf("want error %s, got %s", code, rec.Body.String())
	}
	return envelope.Error
}

func wantIssue(t testing.TB, apiErr *api.Error, field string, code api.Code) {
	t.Helper()
	if len(apiErr.Issues) != 1 || apiErr.Issues[0].Field != field || apiErr.Issues[0].Code != code {
		t.Fatalf("issues = %+v, want %s %s", apiErr.Issues, field, code)
	}
}

const (
	flagsPath  = "/api/v1/feature-flags"
	notDefined = api.Code("FLAG_NOT_DEFINED_IN_MANIFEST")
	enabledOn  = `{"enabled": true}`
)

func TestEveryRouteRequiresAPlatformOperator(t *testing.T) {
	e := newRouteEnv(t, nil)
	tenantPath := flagsPath + "/" + releaseKey + "/tenants/" + e.tenantA
	calls := [][3]string{
		{http.MethodGet, flagsPath, ""},
		{http.MethodPut, flagsPath + "/" + releaseKey, enabledOn},
		{http.MethodDelete, flagsPath + "/" + releaseKey, ""},
		{http.MethodPut, tenantPath, enabledOn},
		{http.MethodDelete, tenantPath, ""},
	}
	for _, c := range calls {
		wantError(t, e.json("admin", c[0], c[1], c[2]), http.StatusForbidden, "PLATFORM_OPERATOR_REQUIRED")
		wantError(t, e.json("", c[0], c[1], c[2]), http.StatusUnauthorized, "SESSION_INVALID")
	}
	listing := wantData[api.Page[featureflags.FlagView]](t, e.json("operator", http.MethodGet, flagsPath, ""), http.StatusOK)
	for _, item := range listing.Items {
		if item.GlobalOverride != nil || len(item.TenantOverrides) != 0 {
			t.Fatalf("a rejected call changed %s: %+v", item.Key, item)
		}
	}
}

func TestGlobalOverrideLifecycle(t *testing.T) {
	e := newRouteEnv(t, nil)
	path := flagsPath + "/" + releaseKey

	set := wantData[featureflags.FlagView](t, e.json("operator", http.MethodPut, path, enabledOn), http.StatusOK)
	if set.GlobalOverride == nil || !set.GlobalOverride.Enabled || set.Key != releaseKey || set.Type != featureflags.TypeRelease ||
		set.Default || set.ExpiresAt == nil || *set.ExpiresAt != "2099-12-31" {
		t.Fatalf("set = %+v", set)
	}
	if !e.probe.Evaluator.Snapshot(context.Background(), nil)[featureflag.Key(releaseKey)] {
		t.Fatal("the evaluator does not see the global override")
	}
	for range 2 {
		cleared := wantData[featureflags.FlagView](t, e.json("operator", http.MethodDelete, path, ""), http.StatusOK)
		if cleared.GlobalOverride != nil {
			t.Fatalf("cleared = %+v", cleared)
		}
	}
	if e.probe.Evaluator.Snapshot(context.Background(), nil)[featureflag.Key(releaseKey)] {
		t.Fatal("the evaluator still sees the cleared override")
	}
}

func TestTenantOverrideLifecycleAndCanonicalId(t *testing.T) {
	e := newRouteEnv(t, nil)
	path := flagsPath + "/" + opsKey + "/tenants/" + strings.ToUpper(e.tenantA)

	set := wantData[featureflags.FlagView](t, e.json("operator", http.MethodPut, path, `{"enabled": false}`), http.StatusOK)
	if len(set.TenantOverrides) != 1 || set.TenantOverrides[0].TenantID != e.tenantA || set.TenantOverrides[0].Enabled || set.ExpiresAt != nil {
		t.Fatalf("set = %+v", set)
	}
	tenant := reqctx.TenantID(e.tenantA)
	if e.probe.Evaluator.Snapshot(context.Background(), &tenant)[featureflag.Key(opsKey)] {
		t.Fatal("the evaluator does not see the tenant override")
	}
	again := wantData[featureflags.FlagView](t, e.json("operator", http.MethodPut, path, enabledOn), http.StatusOK)
	if len(again.TenantOverrides) != 1 || !again.TenantOverrides[0].Enabled {
		t.Fatalf("upper-case id made a second row: %+v", again)
	}
	for range 2 {
		cleared := wantData[featureflags.FlagView](t, e.json("operator", http.MethodDelete, path, ""), http.StatusOK)
		if len(cleared.TenantOverrides) != 0 {
			t.Fatalf("cleared = %+v", cleared)
		}
	}
}

func TestCheckOrderKeyThenTenantThenBody(t *testing.T) {
	e := newRouteEnv(t, nil)
	unknown := "00000000-0000-4000-8000-0000000000ff"
	wantError(t, e.json("operator", http.MethodPut, flagsPath+"/flag_ghost", enabledOn), http.StatusNotFound, notDefined)
	wantError(t, e.json("operator", http.MethodDelete, flagsPath+"/flag_ghost", ""), http.StatusNotFound, notDefined)
	wantError(t, e.json("operator", http.MethodPut, flagsPath+"/flag_ghost/tenants/not-a-uuid", `[]`), http.StatusNotFound, notDefined)
	wantError(t, e.json("operator", http.MethodDelete, flagsPath+"/flag_ghost/tenants/"+unknown, ""), http.StatusNotFound, notDefined)

	base := flagsPath + "/" + releaseKey + "/tenants/"
	wantError(t, e.json("operator", http.MethodPut, base+"not-a-uuid", `[]`), http.StatusNotFound, api.CodeResourceNotFound)
	wantError(t, e.json("operator", http.MethodPut, base+unknown, `[]`), http.StatusNotFound, api.CodeResourceNotFound)
	wantError(t, e.json("operator", http.MethodDelete, base+"not-a-uuid", ""), http.StatusNotFound, api.CodeResourceNotFound)
	wantData[featureflags.FlagView](t, e.json("operator", http.MethodDelete, base+unknown, ""), http.StatusOK)
}

func TestBodyValidation(t *testing.T) {
	e := newRouteEnv(t, nil)
	path := flagsPath + "/" + releaseKey + "/tenants/" + e.tenantA
	wantIssue(t, wantError(t, e.json("operator", http.MethodPut, path, `{"enabled": "yes"}`), 422, api.CodeValidationFailed), "enabled", api.CodeFieldTypeInvalid)
	wantIssue(t, wantError(t, e.json("operator", http.MethodPut, path, `{}`), 422, api.CodeValidationFailed), "enabled", api.CodeFieldRequired)
	wantIssue(t, wantError(t, e.json("operator", http.MethodPut, path, `{"enabled": null}`), 422, api.CodeValidationFailed), "enabled", api.CodeFieldRequired)
	wantIssue(t, wantError(t, e.json("operator", http.MethodPut, path, `[]`), 422, api.CodeValidationFailed), "", api.CodeBodyMalformed)
	wantIssue(t, wantError(t, e.json("operator", http.MethodPut, flagsPath+"/"+releaseKey, `{"enabled": 1}`), 422, api.CodeValidationFailed), "enabled", api.CodeFieldTypeInvalid)
	wantError(t, e.call("operator", http.MethodPut, path, "text/plain", enabledOn), http.StatusUnsupportedMediaType, api.CodeUnsupportedMediaType)
	huge := `{"enabled": true, "pad": "` + strings.Repeat("a", api.MaxBodyBytes+1) + `"}`
	wantError(t, e.json("operator", http.MethodPut, path, huge), http.StatusRequestEntityTooLarge, api.CodePayloadTooLarge)
}

func TestTenantDirectoryFailureIs500(t *testing.T) {
	e := newRouteEnv(t, errors.New("directory down"))
	path := flagsPath + "/" + releaseKey + "/tenants/" + e.tenantA
	wantError(t, e.json("operator", http.MethodPut, path, enabledOn), http.StatusInternalServerError, api.CodeInternalError)
}

func TestListingOrderPagingAndOrphans(t *testing.T) {
	e := newRouteEnv(t, nil)
	e.probe.setGlobal(t, "flag_orphan", true)
	e.probe.setTenant(t, e.tenantB, "flag_orphan", true)
	e.probe.setTenant(t, e.tenantB, releaseKey, true)
	e.probe.setTenant(t, e.tenantA, releaseKey, false)
	wantData[featureflags.FlagView](t, e.json("operator", http.MethodPut, flagsPath+"/"+opsKey, `{"enabled": false}`), http.StatusOK)

	page := wantData[api.Page[featureflags.FlagView]](t, e.json("operator", http.MethodGet, flagsPath, ""), http.StatusOK)
	if page.TotalItems != 2 || page.Size != 100 || page.Page != 1 || len(page.Items) != 2 ||
		page.Items[0].Key != releaseKey || page.Items[1].Key != opsKey {
		t.Fatalf("page = %+v", page)
	}
	release := page.Items[0]
	if release.GlobalOverride != nil || len(release.TenantOverrides) != 2 {
		t.Fatalf("release = %+v", release)
	}
	first, second := release.TenantOverrides[0].TenantID, release.TenantOverrides[1].TenantID
	if first >= second {
		t.Fatalf("tenant overrides not ordered by tenantId: %s, %s", first, second)
	}
	wantEnabled := map[string]bool{e.tenantA: false, e.tenantB: true}
	for _, override := range release.TenantOverrides {
		if override.Enabled != wantEnabled[override.TenantID] {
			t.Fatalf("tenant override %s enabled = %v", override.TenantID, override.Enabled)
		}
	}
	if page.Items[1].GlobalOverride == nil || page.Items[1].GlobalOverride.Enabled {
		t.Fatalf("ops = %+v", page.Items[1])
	}

	paged := wantData[api.Page[featureflags.FlagView]](t, e.json("operator", http.MethodGet, flagsPath+"?size=1&page=2", ""), http.StatusOK)
	if len(paged.Items) != 1 || paged.Items[0].Key != opsKey || paged.TotalItems != 2 {
		t.Fatalf("page 2 = %+v", paged)
	}
	beyond := wantData[api.Page[featureflags.FlagView]](t, e.json("operator", http.MethodGet, flagsPath+"?page=9", ""), http.StatusOK)
	if len(beyond.Items) != 0 {
		t.Fatalf("beyond = %+v", beyond)
	}
}

func TestListingQueryBounds(t *testing.T) {
	e := newRouteEnv(t, nil)
	for _, query := range []string{"?size=0", "?size=101", "?page=0"} {
		apiErr := wantError(t, e.json("operator", http.MethodGet, flagsPath+query, ""), 422, api.CodeValidationFailed)
		if len(apiErr.Issues) != 1 || apiErr.Issues[0].Code != api.CodeFieldOutOfRange {
			t.Fatalf("%s issues = %+v", query, apiErr.Issues)
		}
	}
	apiErr := wantError(t, e.json("operator", http.MethodGet, flagsPath+"?sort=key", ""), 422, api.CodeValidationFailed)
	if len(apiErr.Issues) != 1 || apiErr.Issues[0].Field != "sort" || apiErr.Issues[0].Code != api.CodeFieldNotAllowed {
		t.Fatalf("sort issues = %+v", apiErr.Issues)
	}
}

func TestWritesLogTheOperation(t *testing.T) {
	logger, sink := newLogger()
	e := newRouteEnvWithLogger(t, logger)
	wantData[featureflags.FlagView](t, e.json("operator", http.MethodPut, flagsPath+"/"+releaseKey+"/tenants/"+e.tenantA, enabledOn), http.StatusOK)
	wantData[featureflags.FlagView](t, e.json("operator", http.MethodDelete, flagsPath+"/"+releaseKey, ""), http.StatusOK)

	set := sink.records(t, "flag_override_set")
	if len(set) != 1 || set[0]["flag_key"] != releaseKey || set[0]["flag_scope"] != "tenant" ||
		set[0]["target_tenant_id"] != e.tenantA || set[0]["enabled"] != true || set[0]["level"] != "INFO" {
		t.Fatalf("set log = %+v", set)
	}
	cleared := sink.records(t, "flag_override_cleared")
	if len(cleared) != 1 || cleared[0]["flag_scope"] != "global" {
		t.Fatalf("cleared log = %+v", cleared)
	}
	if _, has := cleared[0]["target_tenant_id"]; has {
		t.Fatalf("a global log carries a target tenant: %+v", cleared[0])
	}
}

func FuzzFlagRoutes(f *testing.F) {
	f.Add(releaseKey, "", `{"enabled": true}`)
	f.Add(opsKey, "00000000-0000-4000-8000-0000000000ff", `{"enabled": "x"}`)
	f.Add("flag_ghost", "not-a-uuid", `[]`)
	f.Add(releaseKey, "%00", "\x00")
	f.Add("%2e%2e", "", ``)
	e := newRouteEnv(f, nil)
	f.Fuzz(func(t *testing.T, key, tenant, body string) {
		path := flagsPath + "/" + escapeSegment(key)
		if tenant != "" {
			path += "/tenants/" + escapeSegment(tenant)
		}
		request, err := http.NewRequest(http.MethodPut, "http://example.com"+path, strings.NewReader(body))
		if err != nil {
			return
		}
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(&http.Cookie{Name: "test_session", Value: "operator"})
		request.Header.Set("X-XSRF-TOKEN", "ok")
		recorder := httptest.NewRecorder()
		e.handler.ServeHTTP(recorder, request)
		if recorder.Code >= http.StatusInternalServerError {
			t.Fatalf("%s %q -> %d", path, body, recorder.Code)
		}
		// Only the mux, for a path it must clean, answers with a bodiless redirect.
		escapedPath := request.URL.EscapedPath()
		isUncleanPath := pathpkg.Clean(escapedPath) != escapedPath
		isRedirect := recorder.Code >= http.StatusMultipleChoices && recorder.Code < http.StatusBadRequest
		if isRedirect && !isUncleanPath {
			t.Fatalf("%s answered %d for a clean path", path, recorder.Code)
		}
		if !isRedirect {
			decode(t, recorder)
		}
	})
}

// escapeSegment keeps the fuzzed text inside one path segment.
func escapeSegment(s string) string {
	replacer := strings.NewReplacer("%", "%25", "/", "%2F", "?", "%3F", "#", "%23", " ", "%20", "\x00", "%00", "\n", "%0A", "\r", "%0D")
	return replacer.Replace(s)
}
