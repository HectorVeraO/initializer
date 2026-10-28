package api_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
)

func jsonRequest(contentType string, body []byte) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/things", bytes.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	return r
}

func asAPIError(t *testing.T, err error) *api.Error {
	t.Helper()
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v is not *api.Error", err)
	}
	return apiErr
}

func TestDecodeBodyMediaType(t *testing.T) {
	for _, ok := range []string{"application/json", "application/json; charset=utf-8", "Application/JSON"} {
		if _, err := api.DecodeBody(jsonRequest(ok, []byte(`{}`))); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "text/plain", "application/jsonx", "application/x-www-form-urlencoded", "application/json;;", "multipart/form-data; boundary=x"} {
		_, err := api.DecodeBody(jsonRequest(bad, []byte(`{}`)))
		apiErr := asAPIError(t, err)
		if apiErr.Status != http.StatusUnsupportedMediaType || apiErr.Code != api.CodeUnsupportedMediaType {
			t.Errorf("%q -> %+v", bad, apiErr)
		}
	}
}

func TestDecodeBodyMalformedInputsGiveOneBodyMalformedIssue(t *testing.T) {
	cases := map[string]string{
		"empty":         "",
		"whitespace":    "  \n",
		"not json":      "{nope",
		"trailing data": `{"a":1} {"b":2}`,
		"trailing junk": `{"a":1}x`,
		"array":         `[1,2]`,
		"string":        `"x"`,
		"null":          `null`,
		"number":        `1`,
		"bom":           "\xef\xbb\xbf{}",
	}
	for name, body := range cases {
		_, err := api.DecodeBody(jsonRequest("application/json", []byte(body)))
		apiErr := asAPIError(t, err)
		if apiErr.Status != http.StatusUnprocessableEntity || apiErr.Code != api.CodeValidationFailed ||
			len(apiErr.Issues) != 1 || apiErr.Issues[0].Field != "" || apiErr.Issues[0].Code != api.CodeBodyMalformed {
			t.Errorf("%s -> %+v", name, apiErr)
		}
	}
}

func TestDecodeBodyNilBodyIsMalformed(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("Content-Type", "application/json")
	r.Body = nil
	_, err := api.DecodeBody(r)
	if asAPIError(t, err).Issues[0].Code != api.CodeBodyMalformed {
		t.Fatal("nil body not malformed")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("connection reset")
}

func TestDecodeBodyReadErrorIsMalformed(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", failingReader{})
	r.Header.Set("Content-Type", "application/json")
	_, err := api.DecodeBody(r)
	apiErr := asAPIError(t, err)
	if apiErr.Status != http.StatusUnprocessableEntity || len(apiErr.Issues) != 1 || apiErr.Issues[0].Code != api.CodeBodyMalformed {
		t.Fatalf("got %+v", apiErr)
	}
}

// objectOfSize builds {"k":"aaa..."} of exactly n bytes.
func objectOfSize(n int) []byte {
	const wrapper = len(`{"k":""}`)
	return []byte(`{"k":"` + strings.Repeat("a", n-wrapper) + `"}`)
}

func TestDecodeBodySizeBoundary(t *testing.T) {
	exact := objectOfSize(api.MaxBodyBytes)
	if _, err := api.DecodeBody(jsonRequest("application/json", exact)); err != nil {
		t.Fatalf("exactly MaxBodyBytes rejected: %v", err)
	}
	over := objectOfSize(api.MaxBodyBytes + 1)
	for _, declared := range []bool{true, false} {
		r := jsonRequest("application/json", over)
		if !declared {
			r.ContentLength = -1
		}
		_, err := api.DecodeBody(r)
		apiErr := asAPIError(t, err)
		if apiErr.Status != http.StatusRequestEntityTooLarge || apiErr.Code != api.CodePayloadTooLarge {
			t.Errorf("declared=%v -> %+v", declared, apiErr)
		}
	}
}

func TestDecodeBodyIgnoresUnknownFieldsAndReportsPresence(t *testing.T) {
	body, err := api.DecodeBody(jsonRequest("application/json", []byte(`{"name":"a","gone":null,"extra":[1]}`)))
	if err != nil {
		t.Fatal(err)
	}
	if !body.Has("name") || !body.Has("gone") || body.Has("missing") {
		t.Fatal("Has")
	}
	if !body.IsNull("gone") || body.IsNull("name") || body.IsNull("missing") {
		t.Fatal("IsNull")
	}
	if raw, ok := body.Raw("extra"); !ok || string(raw) != "[1]" {
		t.Fatalf("Raw = %s %v", raw, ok)
	}
	if _, ok := body.Raw("missing"); ok {
		t.Fatal("Raw of absent field")
	}
}

func readFixture(t *testing.T, name string) *api.Body {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	body, decodeErr := api.DecodeBody(jsonRequest("application/json", data))
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	return body
}

func TestTypeProblemsAccumulateInOne422(t *testing.T) {
	body := readFixture(t, "mixed-types.json")
	var p api.Problems
	body.String(&p, "name", api.Required)
	body.String(&p, "title", api.Required)
	body.Bool(&p, "active", api.Optional)
	body.Int(&p, "count", api.Required)
	body.StringList(&p, "scopes", api.Required)
	limits, _ := body.Object(&p, "limits", api.Required)
	limits.Int(&p, "maxValue", api.Required)
	limits.String(&p, "unit", api.Required)

	got := issuesOf(&p)
	want := []issueView{
		{"name", api.CodeFieldTypeInvalid}, {"title", api.CodeFieldRequired}, {"active", api.CodeFieldTypeInvalid},
		{"count", api.CodeFieldTypeInvalid}, {"scopes[1]", api.CodeFieldTypeInvalid},
		{"limits.maxValue", api.CodeFieldTypeInvalid}, {"limits.unit", api.CodeFieldRequired},
	}
	if len(got) != len(want) {
		t.Fatalf("issues = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("issue %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestOptionalAbsentOrNullRecordsNothing(t *testing.T) {
	body := readFixture(t, "optional.json")
	var p api.Problems
	if _, ok := body.String(&p, "nothing", api.Optional); ok {
		t.Fatal("absent ok")
	}
	if _, ok := body.Int(&p, "nulled", api.Optional); ok {
		t.Fatal("null ok")
	}
	if _, ok := body.Object(&p, "nulled", api.Optional); ok {
		t.Fatal("null object ok")
	}
	if !p.Empty() {
		t.Fatalf("issues = %v", issuesOf(&p))
	}
	body.Int(&p, "nulled", api.Required)
	expectOne(t, &p, "nulled", api.CodeFieldRequired)
}

func TestTypedGettersReadValidValues(t *testing.T) {
	body := readFixture(t, "valid.json")
	var p api.Problems
	s, sOK := body.String(&p, "s", api.Required)
	b, bOK := body.Bool(&p, "b", api.Required)
	i, iOK := body.Int(&p, "i", api.Required)
	n, nOK := body.Number(&p, "n", api.Required)
	l, lOK := body.StringList(&p, "l", api.Required)
	e, eOK := body.StringList(&p, "empty", api.Required)
	o, oOK := body.Object(&p, "o", api.Required)
	inner, innerOK := o.String(&p, "inner", api.Required)
	if !p.Empty() || !sOK || !bOK || !iOK || !nOK || !lOK || !eOK || !oOK || !innerOK {
		t.Fatalf("issues = %v", issuesOf(&p))
	}
	if s != "%2e%2e//host" || !b || i != 42 || n != 1.5 || strings.Join(l, "|") != "a|b" || e == nil || len(e) != 0 || inner != "x" {
		t.Fatalf("values: %q %v %d %v %v %v %q", s, b, i, n, l, e, inner)
	}
}

func TestIntBoundaries(t *testing.T) {
	cases := []struct {
		json string
		want int64
		ok   bool
	}{
		{`1`, 1, true}, {`-7`, -7, true}, {`1.0`, 1, true}, {`1.000`, 1, true}, {`1e2`, 100, true}, {`9e15`, 9000000000000000, true}, {`1e16`, 0, false}, {`1e19`, 0, false},
		{`9223372036854775807`, 9223372036854775807, true},
		{`-9223372036854775808`, -9223372036854775808, true},
		{`1.5`, 0, false}, {`1.0001`, 0, false}, {`9223372036854775808`, 0, false},
		{`-9223372036854775809`, 0, false}, {`9223372036854775807.0`, 9223372036854775807, true}, {`1e400`, 0, false}, {`9007199254740993e0`, 0, false}, {`5000000000000000.3e0`, 0, false}, {`9007199254740992e0`, 9007199254740992, true}, {`9007199254740992.0e0`, 9007199254740992, true},
		{`1e-400`, 0, false}, {`"1"`, 0, false}, {`true`, 0, false}, {`[]`, 0, false}, {`{}`, 0, false},
	}
	for _, tc := range cases {
		body, err := api.DecodeBody(jsonRequest("application/json", []byte(`{"n":`+tc.json+`}`)))
		if err != nil {
			t.Fatal(err)
		}
		var p api.Problems
		got, ok := body.Int(&p, "n", api.Required)
		if ok != tc.ok || got != tc.want || (ok != p.Empty()) {
			t.Errorf("%s -> %d %v (issues %v)", tc.json, got, ok, issuesOf(&p))
		}
	}
}

func TestNumberRejectsNonNumbers(t *testing.T) {
	for _, v := range []string{`"1"`, `true`, `[1]`, `1e999`} {
		body, err := api.DecodeBody(jsonRequest("application/json", []byte(`{"n":`+v+`}`)))
		if err != nil {
			t.Fatalf("%s: decode: %v", v, err)
		}
		var p api.Problems
		if _, ok := body.Number(&p, "n", api.Required); ok {
			t.Errorf("%s accepted", v)
		}
	}
}

func TestStringListReportsEveryBadElement(t *testing.T) {
	body, _ := api.DecodeBody(jsonRequest("application/json", []byte(`{"l":["a",1,null,"b",true],"notlist":"x"}`)))
	var p api.Problems
	if _, ok := body.StringList(&p, "l", api.Required); ok {
		t.Fatal("accepted")
	}
	body.StringList(&p, "notlist", api.Required)
	got := issuesOf(&p)
	want := []string{"l[1]", "l[2]", "l[4]", "notlist"}
	if len(got) != len(want) {
		t.Fatalf("issues = %v", got)
	}
	for i := range want {
		if got[i].field != want[i] || got[i].code != api.CodeFieldTypeInvalid {
			t.Errorf("issue %d = %v", i, got[i])
		}
	}
}

func TestObjectRejectsNonObjects(t *testing.T) {
	body, _ := api.DecodeBody(jsonRequest("application/json", []byte(`{"a":[],"b":"x","c":1}`)))
	var p api.Problems
	for _, name := range []string{"a", "b", "c"} {
		if _, ok := body.Object(&p, name, api.Required); ok {
			t.Errorf("%s accepted", name)
		}
	}
	if p.Len() != 3 {
		t.Fatalf("issues = %v", issuesOf(&p))
	}
}

func TestNestedObjectPrefixesDeepPaths(t *testing.T) {
	body, _ := api.DecodeBody(jsonRequest("application/json", []byte(`{"a":{"b":{"c":1}}}`)))
	var p api.Problems
	a, _ := body.Object(&p, "a", api.Required)
	b, _ := a.Object(&p, "b", api.Required)
	b.String(&p, "c", api.Required)
	expectOne(t, &p, "a.b.c", api.CodeFieldTypeInvalid)
}

func FuzzDecodeBody(f *testing.F) {
	seeds := []string{"", "{}", `{"a":1}`, "[", `{"a":`, "null", `{"a":{"b":[1,"x",null]}}`, "\xff\xfe", `{"a":1}{"b":2}`, `{"k":"\ud800"}`}
	contentTypes := []string{"application/json", "", "text/plain", "application/json; charset=utf-8"}
	for i, seed := range seeds {
		f.Add([]byte(seed), contentTypes[i%len(contentTypes)])
	}
	f.Fuzz(func(t *testing.T, data []byte, contentType string) {
		body, err := api.DecodeBody(jsonRequest(contentType, data))
		if err == nil {
			if body == nil {
				t.Fatal("nil body without error")
			}
			var p api.Problems
			body.String(&p, "a", api.Optional)
			body.Int(&p, "a", api.Optional)
			body.Number(&p, "a", api.Optional)
			body.Bool(&p, "a", api.Optional)
			body.StringList(&p, "a", api.Optional)
			body.Object(&p, "a", api.Optional)
			return
		}
		var apiErr *api.Error
		if !errors.As(err, &apiErr) {
			t.Fatalf("error %T is not *api.Error", err)
		}
		switch apiErr.Status {
		case http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		default:
			t.Fatalf("unexpected status %d", apiErr.Status)
		}
	})
}

func TestDecodeBodyOverDrainCapStillAnswers413(t *testing.T) {
	over := bytes.Repeat([]byte("a"), api.MaxDrainBytes+1024)
	for _, declared := range []bool{true, false} {
		r := jsonRequest("application/json", over)
		if !declared {
			r.ContentLength = -1
		}
		_, err := api.DecodeBody(r)
		apiErr := asAPIError(t, err)
		if apiErr.Status != http.StatusRequestEntityTooLarge || apiErr.Code != api.CodePayloadTooLarge {
			t.Errorf("declared=%v -> %+v", declared, apiErr)
		}
	}
}

// slowReader hands out the body in small steps, like a client still streaming
// when the server has already decided to answer.
type slowReader struct {
	data []byte
}

func (s *slowReader) Read(p []byte) (int, error) {
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	n := min(len(p), 32<<10, len(s.data))
	copy(p, s.data[:n])
	s.data = s.data[n:]
	time.Sleep(time.Millisecond)
	return n, nil
}

// streamingProxy forwards each request over a fresh connection while it is
// still being read and answers 502 when the upstream write fails, like the
// Angular dev server proxy. httputil.ReverseProxy is too forgiving here: it
// returns an upstream response that arrives before the body is fully sent.
func streamingProxy(upstream string) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		conn, err := net.Dial("tcp", upstream)
		if err != nil {
			rw.WriteHeader(http.StatusBadGateway)
			return
		}
		defer conn.Close()
		r.RequestURI = ""
		r.Close = true
		if err := r.Write(conn); err != nil {
			rw.WriteHeader(http.StatusBadGateway)
			return
		}
		resp, err := http.ReadResponse(bufio.NewReader(conn), r)
		if err != nil {
			rw.WriteHeader(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for key, values := range resp.Header {
			rw.Header()[key] = values
		}
		rw.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(rw, resp.Body)
	})
}

func TestDecodeBodyOversizedAnswers413ThroughStreamingProxy(t *testing.T) {
	writer := newTestWriter(&bytes.Buffer{})
	backend := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if _, err := api.DecodeBody(r); err != nil {
			writer.Fail(rw, r, err)
			return
		}
		rw.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()
	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(streamingProxy(target.Host))
	defer proxy.Close()

	payload := `{"k":"` + strings.Repeat("a", 2<<20) + `"}`
	req, err := http.NewRequest(http.MethodPost, proxy.URL, &slowReader{data: []byte(payload)})
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = int64(len(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("status %d, undecodable envelope: %v", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusRequestEntityTooLarge || envelope.Error.Code != string(api.CodePayloadTooLarge) {
		t.Errorf("status %d code %q, want 413 %s", resp.StatusCode, envelope.Error.Code, api.CodePayloadTooLarge)
	}
}
