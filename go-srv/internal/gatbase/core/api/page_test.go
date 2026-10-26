package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
)

var testSpec = api.PageSpec{Sorts: []string{"name", "createdAt"}, DefaultSort: "-createdAt"}

func pageRequest(rawQuery string) *http.Request {
	return httptest.NewRequest(http.MethodGet, "/api/v1/things?"+rawQuery, nil)
}

func TestParsePageQueryDefaults(t *testing.T) {
	var p api.Problems
	q := api.ParsePageQuery(pageRequest(""), &p, testSpec)
	if !p.Empty() || q.Page != 1 || q.Size != 20 || q.SortField != "createdAt" || !q.SortDesc || q.Offset() != 0 {
		t.Fatalf("q = %+v issues %v", q, issuesOf(&p))
	}
}

func TestParsePageQueryValidValues(t *testing.T) {
	var p api.Problems
	q := api.ParsePageQuery(pageRequest("page=3&size=50&sort=name"), &p, testSpec)
	if !p.Empty() || q.Page != 3 || q.Size != 50 || q.SortField != "name" || q.SortDesc || q.Offset() != 100 {
		t.Fatalf("q = %+v issues %v", q, issuesOf(&p))
	}
	q = api.ParsePageQuery(pageRequest("size=100&sort=-name"), &p, testSpec)
	if !p.Empty() || q.Size != 100 || !q.SortDesc || q.SortField != "name" {
		t.Fatalf("q = %+v", q)
	}
}

func TestParsePageQueryAcceptsMaxPage(t *testing.T) {
	var p api.Problems
	q := api.ParsePageQuery(pageRequest("page=2147483647"), &p, testSpec)
	if !p.Empty() || q.Page != 2147483647 {
		t.Fatalf("q = %+v issues %v", q, issuesOf(&p))
	}
}

func TestParsePageQueryProblemsAccumulate(t *testing.T) {
	cases := []struct {
		query string
		want  []issueView
	}{
		{"page=0", []issueView{{"page", api.CodeFieldOutOfRange}}},
		{"page=-1", []issueView{{"page", api.CodeFieldOutOfRange}}},
		{"size=0", []issueView{{"size", api.CodeFieldOutOfRange}}},
		{"size=101", []issueView{{"size", api.CodeFieldOutOfRange}}},
		{"page=abc", []issueView{{"page", api.CodeFieldTypeInvalid}}},
		{"size=1.5", []issueView{{"size", api.CodeFieldTypeInvalid}}},
		{"page=99999999999999999999", []issueView{{"page", api.CodeFieldOutOfRange}}},
		{"page=2147483648", []issueView{{"page", api.CodeFieldOutOfRange}}},
		{"sort=-unknown", []issueView{{"sort", api.CodeFieldNotAllowed}}},
		{"sort=--name", []issueView{{"sort", api.CodeFieldNotAllowed}}},
		{"page=x&size=0&sort=nope", []issueView{{"page", api.CodeFieldTypeInvalid}, {"size", api.CodeFieldOutOfRange}, {"sort", api.CodeFieldNotAllowed}}},
	}
	for _, tc := range cases {
		var p api.Problems
		q := api.ParsePageQuery(pageRequest(tc.query), &p, testSpec)
		got := issuesOf(&p)
		if len(got) != len(tc.want) {
			t.Errorf("%s -> %v", tc.query, got)
			continue
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("%s issue %d = %v", tc.query, i, got[i])
			}
		}
		if q.Page < 1 || q.Size < 1 {
			t.Errorf("%s returned unusable query %+v", tc.query, q)
		}
	}
}

func TestParsePageQueryCustomLimits(t *testing.T) {
	var p api.Problems
	spec := api.PageSpec{DefaultSize: 5, MaxSize: 10}
	q := api.ParsePageQuery(pageRequest(""), &p, spec)
	if q.Size != 5 || q.SortField != "" {
		t.Fatalf("q = %+v", q)
	}
	api.ParsePageQuery(pageRequest("size=11&sort=name"), &p, spec)
	got := issuesOf(&p)
	if len(got) != 2 || got[0].field != "size" || got[1].field != "sort" {
		t.Fatalf("issues = %v", got)
	}
}

func TestParsePageQueryPanicsOnBadSpec(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	var p api.Problems
	api.ParsePageQuery(pageRequest(""), &p, api.PageSpec{Sorts: []string{"a"}, DefaultSort: "b"})
}

func TestNewPageSerialisesItemsNeverNull(t *testing.T) {
	page := api.NewPage[string](nil, api.PageQuery{Page: 2, Size: 10}, 11)
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"items":[],"page":2,"size":10,"totalItems":11}` {
		t.Fatalf("json = %s", raw)
	}
}

func FuzzParsePageQuery(f *testing.F) {
	for _, seed := range []string{"", "page=1&size=2", "page=-1", "sort=-name", "page=%00", "size=99999999999999999999", "sort=%ff"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, rawQuery string) {
		if _, err := url.ParseQuery(rawQuery); err != nil || strings.ContainsAny(rawQuery, " \r\n\t") {
			t.Skip()
		}
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		r.URL.RawQuery = rawQuery
		var p api.Problems
		q := api.ParsePageQuery(r, &p, testSpec)
		if q.Page < 1 || q.Size < 1 || q.Size > 100 || q.Offset() < 0 {
			t.Fatalf("unusable query %+v for %q", q, rawQuery)
		}
		if q.SortField != "name" && q.SortField != "createdAt" {
			t.Fatalf("sort field %q escaped the allow-list", q.SortField)
		}
	})
}
