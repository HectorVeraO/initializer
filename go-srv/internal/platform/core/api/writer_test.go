package api_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/internal/ctxkey"
)

const testTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"

var fixedNow = time.Date(2026, 1, 1, 0, 0, 0, 999, time.UTC)

func newTestWriter(logs *bytes.Buffer) *api.Writer {
	handler := slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})
	return api.NewWriter(clock.Fixed(fixedNow), slog.New(handler))
}

func tracedRequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/things", nil)
	return r.WithContext(ctxkey.WithTraceID(r.Context(), testTraceID))
}

func TestDataWritesTheThreeEnvelopeKeys(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestWriter(&bytes.Buffer{}).Data(rec, tracedRequest(), http.StatusCreated, map[string]string{"name": "acme"})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 3 || raw["data"] == nil || raw["meta"] == nil || string(raw["error"]) != "null" {
		t.Fatalf("keys = %v", raw)
	}
	var env api.Envelope[map[string]string]
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if (*env.Data)["name"] != "acme" || env.Meta.TraceID != testTraceID || env.Meta.Timestamp != "2026-01-01T00:00:00Z" {
		t.Fatalf("envelope = %+v", env)
	}
}

func TestDataNilWritesNullData(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestWriter(&bytes.Buffer{}).Data(rec, tracedRequest(), http.StatusOK, nil)

	if !strings.Contains(rec.Body.String(), `"data":null`) || !strings.Contains(rec.Body.String(), `"error":null`) {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestDataMarshalFailureBecomesGeneric500(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestWriter(&bytes.Buffer{}).Data(rec, tracedRequest(), http.StatusOK, make(chan int))

	var env api.Envelope[any]
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusInternalServerError || env.Error == nil || env.Error.Code != api.CodeInternalError || env.Data != nil {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestTraceIDHeaderEqualsMetaTraceID(t *testing.T) {
	var logs bytes.Buffer
	w := newTestWriter(&logs)
	cases := map[string]func(rec *httptest.ResponseRecorder, r *http.Request){
		"data": func(rec *httptest.ResponseRecorder, r *http.Request) {
			w.Data(rec, r, http.StatusOK, map[string]string{"a": "b"})
		},
		"fail": func(rec *httptest.ResponseRecorder, r *http.Request) {
			w.Fail(rec, r, errors.New("boom"))
		},
	}
	for name, write := range cases {
		rec := httptest.NewRecorder()
		write(rec, tracedRequest())
		var env api.Envelope[any]
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		if got := rec.Header().Get("X-Trace-Id"); got != testTraceID || got != env.Meta.TraceID {
			t.Errorf("%s: header %q, meta %q", name, got, env.Meta.TraceID)
		}
	}
	rec := httptest.NewRecorder()
	w.Data(rec, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK, nil)
	if got := rec.Header().Get("X-Trace-Id"); got != strings.Repeat("0", 32) {
		t.Errorf("missing context: header %q", got)
	}
}

func TestMissingTraceContextWritesZerosAndWarns(t *testing.T) {
	var logs bytes.Buffer
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/things", nil)
	newTestWriter(&logs).Data(rec, r, http.StatusOK, nil)

	var env api.Envelope[any]
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Meta.TraceID != strings.Repeat("0", 32) {
		t.Fatalf("traceId = %q", env.Meta.TraceID)
	}
	if !strings.Contains(logs.String(), `"msg":"trace_context_missing"`) || !strings.Contains(logs.String(), `"level":"WARN"`) {
		t.Fatalf("logs = %s", logs.String())
	}
}

func TestMalformedTraceIDIsTreatedAsMissing(t *testing.T) {
	for _, bad := range []string{"", "ABCDEF", strings.Repeat("0", 32), strings.ToUpper(testTraceID), testTraceID + "0"} {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r = r.WithContext(ctxkey.WithTraceID(r.Context(), bad))
		var logs bytes.Buffer
		newTestWriter(&logs).Data(rec, r, http.StatusOK, nil)
		if !strings.Contains(rec.Body.String(), strings.Repeat("0", 32)) {
			t.Errorf("trace %q not replaced: %s", bad, rec.Body)
		}
		if !strings.Contains(logs.String(), `"msg":"trace_context_missing"`) {
			t.Errorf("trace %q: no WARN logged: %s", bad, logs.String())
		}
	}
}

func TestFailPlainErrorNeverLeaksText(t *testing.T) {
	var logs bytes.Buffer
	rec := httptest.NewRecorder()
	internalText := `pq: relation "usr_accounts" does not exist`
	newTestWriter(&logs).Fail(rec, tracedRequest(), errors.New(internalText))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "usr_accounts") {
		t.Fatalf("leaked: %s", rec.Body)
	}
	var env api.Envelope[any]
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != api.CodeInternalError || env.Error.Message != "Internal error" || env.Error.Issues == nil || len(env.Error.Issues) != 0 {
		t.Fatalf("error = %+v", env.Error)
	}
	if !strings.Contains(logs.String(), "usr_accounts") || !strings.Contains(logs.String(), `"msg":"request_failed"`) || !strings.Contains(logs.String(), `"err":`) {
		t.Fatalf("cause not logged: %s", logs.String())
	}
}

func TestFailNilErrorIsGeneric500(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestWriter(&bytes.Buffer{}).Fail(rec, tracedRequest(), nil)
	assertGeneric500(t, rec)
}

func assertGeneric500(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var env api.Envelope[any]
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusInternalServerError || env.Error == nil ||
		env.Error.Code != api.CodeInternalError || env.Error.Message != "Internal error" ||
		env.Error.Issues == nil || len(env.Error.Issues) != 0 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestDataWithNon2xxStatusBecomesGeneric500(t *testing.T) {
	for _, status := range []int{199, 300, 404} {
		rec := httptest.NewRecorder()
		newTestWriter(&bytes.Buffer{}).Data(rec, tracedRequest(), status, map[string]string{"a": "b"})
		assertGeneric500(t, rec)
		if strings.Contains(rec.Body.String(), `"a":"b"`) {
			t.Fatalf("status %d leaked data: %s", status, rec.Body)
		}
	}
}

func TestFailWritesWrappedAPIErrorAsIs(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped := errors.Join(errors.New("context"), api.NotFound())
	newTestWriter(&bytes.Buffer{}).Fail(rec, tracedRequest(), wrapped)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	var env api.Envelope[any]
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound || env.Error.Code != api.CodeResourceNotFound || env.Error.Message != "Resource not found" {
		t.Fatalf("status %d error %+v", rec.Code, env.Error)
	}
	if string(raw["data"]) != "null" || !strings.Contains(string(raw["error"]), `"issues":[]`) {
		t.Fatalf("body = %s", rec.Body)
	}
	if strings.Contains(string(raw["error"]), "Status") || strings.Contains(string(raw["error"]), "cause") {
		t.Fatalf("internal fields serialized: %s", raw["error"])
	}
}

func TestFail5xxLogsCauseButWiresMessageOnly(t *testing.T) {
	var logs bytes.Buffer
	rec := httptest.NewRecorder()
	failure := api.Wrap(errors.New("dial tcp 10.0.0.1: refused"), http.StatusServiceUnavailable, api.CodeServiceUnavailable, "Try again later")
	newTestWriter(&logs).Fail(rec, tracedRequest(), failure)

	if rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), "dial tcp") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if !strings.Contains(logs.String(), "dial tcp") || !strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Fatalf("logs = %s", logs.String())
	}
}

func TestFail4xxDoesNotLogAtError(t *testing.T) {
	var logs bytes.Buffer
	newTestWriter(&logs).Fail(httptest.NewRecorder(), tracedRequest(), api.NotFound())
	if strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Fatalf("logs = %s", logs.String())
	}
}

func TestFailErrorWithInvalidStatusBecomesGeneric500(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestWriter(&bytes.Buffer{}).Fail(rec, tracedRequest(), &api.Error{Status: 0, Code: "X", Message: "x"})
	assertGeneric500(t, rec)
}

func TestErrorStringAndUnwrap(t *testing.T) {
	cause := errors.New("boom")
	e := api.Wrap(cause, 500, api.CodeInternalError, "Internal error")
	if e.Error() != "INTERNAL_ERROR: Internal error" || !errors.Is(e, cause) {
		t.Fatalf("error = %q", e.Error())
	}
	if api.NewError(400, "X", "y").Unwrap() != nil {
		t.Fatal("NewError has a cause")
	}
}

func TestNewWriterRejectsNilDependencies(t *testing.T) {
	cases := map[string]func(){
		"nil clock":  func() { api.NewWriter(nil, slog.Default()) },
		"nil logger": func() { api.NewWriter(clock.Fixed(fixedNow), nil) },
	}
	for name, build := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: expected panic", name)
				}
			}()
			build()
		}()
	}
}
