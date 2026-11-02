package dispatch_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/fixture/srv/internal/dispatch"
	"example.com/fixture/srv/internal/platform/core"
	"example.com/fixture/srv/internal/platform/core/api"
	"example.com/fixture/srv/internal/platform/core/coretest"
	"example.com/fixture/srv/internal/platform/core/reqctx"
)

// accountID is the account every request of this file acts as.
const accountID = "3f2b6f6e-2a5b-4c2e-9d1a-6f0c2b7d8e01"

// principal is an account actor with no active tenant: the queue is
// project-wide.
func principal() reqctx.Principal {
	return reqctx.Principal{
		Kind:      reqctx.ActorAccount,
		ActorID:   accountID,
		AccountID: reqctx.AccountID(accountID),
	}
}

// newHandler serves the vertical through coretest over a fresh database.
func newHandler(t *testing.T) http.Handler {
	t.Helper()
	return coretest.New(t, []core.Module{dispatch.Module()}, coretest.Options{
		Database:        true,
		SessionResolver: coretest.Session(principal()),
	})
}

// call sends one request as the signed-in account. A body is JSON.
func call(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return callWith(t, handler, method, path, body, nil)
}

// callWith is call with extra request headers; a header given as "" is sent
// empty, which the session fixture reads as an invalid CSRF token.
func callWith(t *testing.T, handler http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "ok"})
	request.Header.Set("X-XSRF-TOKEN", "ok")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// decode reads the envelope of a response and checks the trace header.
func decode(t *testing.T, recorder *httptest.ResponseRecorder) api.Envelope[json.RawMessage] {
	t.Helper()
	var envelope api.Envelope[json.RawMessage]
	err := json.Unmarshal(recorder.Body.Bytes(), &envelope)
	if err != nil {
		t.Fatalf("not an envelope (status %d): %q", recorder.Code, recorder.Body.String())
	}
	traceID := recorder.Header().Get("X-Trace-Id")
	if traceID != envelope.Meta.TraceID {
		t.Fatalf("X-Trace-Id %q differs from meta.traceId %q", traceID, envelope.Meta.TraceID)
	}
	return envelope
}

// data decodes the data of a successful response.
func data[T any](t *testing.T, recorder *httptest.ResponseRecorder, status int) T {
	t.Helper()
	envelope := decode(t, recorder)
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, status, recorder.Body.String())
	}
	if envelope.Data == nil || envelope.Error != nil {
		t.Fatalf("data = %v, error = %+v", envelope.Data, envelope.Error)
	}
	var value T
	err := json.Unmarshal(*envelope.Data, &value)
	if err != nil {
		t.Fatalf("data: %v: %s", err, *envelope.Data)
	}
	return value
}

// requireError asserts the status and the wire code of a failure.
func requireError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code api.Code) {
	t.Helper()
	envelope := decode(t, recorder)
	if recorder.Code != status || envelope.Error == nil || envelope.Error.Code != code {
		t.Fatalf("status = %d, error = %+v, want %d %s: %s", recorder.Code, envelope.Error, status, code, recorder.Body.String())
	}
	if envelope.Data != nil {
		t.Fatalf("a failure carries data: %s", recorder.Body.String())
	}
}

// requireIssues asserts a 422 VALIDATION_FAILED whose issues are exactly
// "field:CODE", in order.
func requireIssues(t *testing.T, recorder *httptest.ResponseRecorder, want []string) {
	t.Helper()
	envelope := decode(t, recorder)
	if recorder.Code != http.StatusUnprocessableEntity ||
		envelope.Error == nil || envelope.Error.Code != api.CodeValidationFailed {
		t.Fatalf("status = %d, error = %+v, want 422 VALIDATION_FAILED: %s", recorder.Code, envelope.Error, recorder.Body.String())
	}
	got := make([]string, 0, len(envelope.Error.Issues))
	for _, issue := range envelope.Error.Issues {
		got = append(got, issue.Field+":"+string(issue.Code))
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("issues:\n got  %v\n want %v", got, want)
	}
}

func TestModuleDeclaresItsIdentityAndRoutes(t *testing.T) {
	module := dispatch.Module()
	if module.ID != "dispatch" {
		t.Fatalf("module id = %q", module.ID)
	}
	if module.Migrations == nil || module.Provide == nil || module.Mount == nil {
		t.Fatalf("module = %+v", module)
	}
	if module.Start != nil {
		t.Fatal("this vertical mounts routes and starts nothing")
	}
}

func TestCreateThenListServesBothRoutes(t *testing.T) {
	handler := newHandler(t)
	created := data[dispatch.JobView](t, call(t, handler, http.MethodPost, "/api/v1/dispatch", `{"reference":"job-a"}`), http.StatusCreated)
	if created.Reference != "job-a" || created.Status != "queued" {
		t.Fatalf("created = %+v", created)
	}
	if created.CreatedBy == nil || *created.CreatedBy != accountID {
		t.Fatalf("createdBy = %v", created.CreatedBy)
	}

	page := data[api.Page[dispatch.JobView]](t, call(t, handler, http.MethodGet, "/api/v1/dispatch", ""), http.StatusOK)
	if page.TotalItems != 1 || len(page.Items) != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("page = %+v", page)
	}

	requireError(t, call(t, handler, http.MethodPost, "/api/v1/dispatch", `{"reference":"job-a"}`),
		http.StatusConflict, dispatch.CodeReferenceTaken)
}

func TestCreateAnswersTheGuardRejections(t *testing.T) {
	handler := newHandler(t)
	cases := []struct {
		name    string
		headers map[string]string
		status  int
		code    api.Code
	}{
		{"a mutation without the CSRF token", map[string]string{"X-XSRF-TOKEN": ""},
			http.StatusForbidden, api.CodeCSRFTokenInvalid},
		{"a mutation whose active tenant differs", map[string]string{"X-Active-Tenant-Id": "9d4b6c1e-2f3a-4b5c-8d7e-0a1b2c3d4e5f"},
			http.StatusConflict, api.CodeActiveTenantChanged},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := callWith(t, handler, http.MethodPost, "/api/v1/dispatch", `{"reference":"job-a"}`, testCase.headers)
			requireError(t, recorder, testCase.status, testCase.code)
		})
	}
}

func TestCreateRejectsAMissingReference(t *testing.T) {
	handler := newHandler(t)
	requireIssues(t, call(t, handler, http.MethodPost, "/api/v1/dispatch", `{}`),
		[]string{"reference:FIELD_REQUIRED"})
}

func TestCreateAcceptsAReferenceOfSixtyFourCharacters(t *testing.T) {
	handler := newHandler(t)
	reference := strings.Repeat("a", 64)
	body, err := json.Marshal(map[string]string{"reference": reference})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	created := data[dispatch.JobView](t, call(t, handler, http.MethodPost, "/api/v1/dispatch", string(body)), http.StatusCreated)
	if created.Reference != reference {
		t.Fatalf("reference = %q", created.Reference)
	}
}

func TestCreateAccumulatesEveryProblemOfAReference(t *testing.T) {
	handler := newHandler(t)
	body, err := json.Marshal(map[string]string{"reference": strings.Repeat("a", 61) + " bad"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	requireIssues(t, call(t, handler, http.MethodPost, "/api/v1/dispatch", string(body)),
		[]string{"reference:FIELD_TOO_LONG", "reference:FIELD_INVALID_FORMAT"})
}

func TestListDefaultsToAscendingOrder(t *testing.T) {
	handler := newHandler(t)
	// Queued in reverse, so the order is the reference and not the insert.
	for _, reference := range []string{"job-b", "job-a"} {
		data[dispatch.JobView](t, call(t, handler, http.MethodPost, "/api/v1/dispatch", `{"reference":"`+reference+`"}`), http.StatusCreated)
	}

	page := data[api.Page[dispatch.JobView]](t, call(t, handler, http.MethodGet, "/api/v1/dispatch", ""), http.StatusOK)
	if len(page.Items) != 2 || page.Items[0].Reference != "job-a" || page.Items[1].Reference != "job-b" {
		t.Fatalf("items = %+v", page.Items)
	}
}

func TestListSortsDescendingAndPages(t *testing.T) {
	handler := newHandler(t)
	for _, reference := range []string{"job-a", "job-b"} {
		data[dispatch.JobView](t, call(t, handler, http.MethodPost, "/api/v1/dispatch", `{"reference":"`+reference+`"}`), http.StatusCreated)
	}

	// Descending order puts job-b first, so page 2 of size 1 is job-a: an
	// ascending order or an offset off by one answers another job or nothing.
	page := data[api.Page[dispatch.JobView]](t, call(t, handler, http.MethodGet,
		"/api/v1/dispatch?sort=-reference&size=1&page=2", ""), http.StatusOK)
	if page.Page != 2 || page.Size != 1 || page.TotalItems != 2 {
		t.Fatalf("page = %+v", page)
	}
	if len(page.Items) != 1 || page.Items[0].Reference != "job-a" {
		t.Fatalf("items = %+v", page.Items)
	}
}

func TestListRejectsInvalidPageParameters(t *testing.T) {
	handler := newHandler(t)
	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"a page that is not an integer", "?page=x", []string{"page:FIELD_TYPE_INVALID"}},
		{"a page below one", "?page=0", []string{"page:FIELD_OUT_OF_RANGE"}},
		{"a size above one hundred", "?size=101", []string{"size:FIELD_OUT_OF_RANGE"}},
		{"a size below one", "?size=0", []string{"size:FIELD_OUT_OF_RANGE"}},
		{"an unknown sort field", "?sort=status", []string{"sort:FIELD_NOT_ALLOWED"}},
		{"every parameter at once", "?page=x&size=101&sort=status",
			[]string{"page:FIELD_TYPE_INVALID", "size:FIELD_OUT_OF_RANGE", "sort:FIELD_NOT_ALLOWED"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			requireIssues(t, call(t, handler, http.MethodGet, "/api/v1/dispatch"+testCase.query, ""), testCase.want)
		})
	}
}
