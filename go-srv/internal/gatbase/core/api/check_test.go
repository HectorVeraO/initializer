package api_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
)

type issueView struct {
	field string
	code  api.Code
}

func issuesOf(p *api.Problems) []issueView {
	var out []issueView
	var apiErr *api.Error
	if !errors.As(p.Err(), &apiErr) {
		return nil
	}
	for _, issue := range apiErr.Issues {
		out = append(out, issueView{issue.Field, issue.Code})
	}
	return out
}

func expectOne(t *testing.T, p *api.Problems, field string, code api.Code) {
	t.Helper()
	got := issuesOf(p)
	if len(got) != 1 || got[0].field != field || got[0].code != code {
		t.Fatalf("issues = %v, want %s %s", got, field, code)
	}
}

func TestCheckRequired(t *testing.T) {
	var p api.Problems
	if !api.CheckRequired(&p, "name", "x") || !p.Empty() {
		t.Fatal("non-empty failed")
	}
	if api.CheckRequired(&p, "name", " \t\n") {
		t.Fatal("blank passed")
	}
	expectOne(t, &p, "name", api.CodeFieldRequired)
}

func TestCheckLengthCountsRunesAfterTrim(t *testing.T) {
	var p api.Problems
	// Three runes, nine bytes.
	if !api.CheckLength(&p, "n", "  é世\U0001f600  ", 3, 3) {
		t.Fatalf("multi-byte runes miscounted: %v", issuesOf(&p))
	}
	if api.CheckLength(&p, "n", "ab", 3, 5) {
		t.Fatal("short passed")
	}
	if api.CheckLength(&p, "n2", "abcdef", 3, 5) {
		t.Fatal("long passed")
	}
	got := issuesOf(&p)
	if len(got) != 2 || got[0].code != api.CodeFieldTooShort || got[1].code != api.CodeFieldTooLong {
		t.Fatalf("issues = %v", got)
	}
}

func TestCheckIntRangeBoundaries(t *testing.T) {
	var p api.Problems
	for _, v := range []int64{1, 10} {
		if !api.CheckIntRange(&p, "n", v, 1, 10) {
			t.Fatalf("%d rejected", v)
		}
	}
	if api.CheckIntRange(&p, "low", 0, 1, 10) || api.CheckIntRange(&p, "high", 11, 1, 10) {
		t.Fatal("out-of-range value accepted")
	}
	got := issuesOf(&p)
	want := []issueView{{"low", api.CodeFieldOutOfRange}, {"high", api.CodeFieldOutOfRange}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("issues = %v, want %v", got, want)
	}
}

func TestCheckOneOfMinItemsUnique(t *testing.T) {
	var p api.Problems
	if !api.CheckOneOf(&p, "role", "admin", "admin", "member") || api.CheckOneOf(&p, "role", "root", "admin") {
		t.Fatal("CheckOneOf")
	}
	if !api.CheckMinItems(&p, "s", 1, 1) || api.CheckMinItems(&p, "s", 0, 1) {
		t.Fatal("CheckMinItems")
	}
	if api.CheckUnique(&p, "scopes", []string{"a", "b", "a", "a"}) {
		t.Fatal("duplicates passed")
	}
	if !api.CheckUnique(&p, "scopes", []string{"a", "b"}) {
		t.Fatal("unique failed")
	}
	got := issuesOf(&p)
	want := []issueView{
		{"role", api.CodeFieldNotAllowed}, {"s", api.CodeFieldMinItems},
		{"scopes[2]", api.CodeFieldDuplicate}, {"scopes[3]", api.CodeFieldDuplicate},
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

func TestCheckUUIDCanonicalises(t *testing.T) {
	var p api.Problems
	got, ok := api.CheckUUID(&p, "id", "0A1B2C3D-0000-4000-8000-ABCDEF012345")
	if !ok || got != "0a1b2c3d-0000-4000-8000-abcdef012345" {
		t.Fatalf("got %q %v", got, ok)
	}
	bad := []string{"", "0a1b2c3d000040008000abcdef012345", "0a1b2c3d00004000800abcdef012345abcd",
		"{0a1b2c3d-0000-4000-8000-abcdef012345}", "urn:uuid:0a1b2c3d-0000-4000-8000-abcdef012345",
		"0a1b2c3d-0000-4000-8000-abcdef01234g", "0a1b2c3d_0000-4000-8000-abcdef012345",
		" 0a1b2c3d-0000-4000-8000-abcdef012345", "%30a1b2c3d-0000-4000-8000-abcdef012345"}
	for _, v := range bad {
		if _, ok := api.CheckUUID(&p, "id", v); ok {
			t.Errorf("%q accepted", v)
		}
	}
	if p.Len() != len(bad) {
		t.Errorf("issues = %d", p.Len())
	}
}

func TestParseUUIDRecordsNothing(t *testing.T) {
	if _, ok := api.ParseUUID("nope"); ok {
		t.Fatal("accepted")
	}
	if v, ok := api.ParseUUID("0A1B2C3D-0000-4000-8000-ABCDEF012345"); !ok || v != "0a1b2c3d-0000-4000-8000-abcdef012345" {
		t.Fatalf("not canonical: %q", v)
	}
}

func TestCheckEmail(t *testing.T) {
	var p api.Problems
	got, ok := api.CheckEmail(&p, "email", "  Admin@example.com ")
	if !ok || got != "admin@example.com" {
		t.Fatalf("got %q %v", got, ok)
	}
	bad := []string{"", "Jane Doe <jane@example.com>", "<jane@example.com>", "jane@example.com, joe@example.com",
		"jane", "@example.com", "jane@", "jane@example.com (note)", `"j d"@example.com`,
		strings.Repeat("a", 250) + "@example.com",
		strings.Repeat("a", 243) + "@example.com"}
	for _, v := range bad {
		if _, ok := api.CheckEmail(&p, "email", v); ok {
			t.Errorf("%q accepted", v)
		}
	}
	atLimit := strings.Repeat("a", 242) + "@example.com"
	if len(atLimit) != 254 {
		t.Fatalf("fixture length = %d", len(atLimit))
	}
	if _, ok := api.CheckEmail(&p, "email", atLimit); !ok {
		t.Errorf("254-character address rejected")
	}
}

func TestCheckTimestamp(t *testing.T) {
	var p api.Problems
	got, ok := api.CheckTimestamp(&p, "at", "2026-01-01T01:00:00+01:00")
	if !ok || !got.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) || got.Location() != time.UTC {
		t.Fatalf("got %v %v", got, ok)
	}
	for _, v := range []string{"", "2026-01-01", "2026-01-01 00:00:00", "2026-13-01T00:00:00Z", "yesterday"} {
		if _, ok := api.CheckTimestamp(&p, "at", v); ok {
			t.Errorf("%q accepted", v)
		}
	}
}

func TestInjectionLookingStringsStayPlainData(t *testing.T) {
	for _, v := range []string{"%253Cscript%253E", "//evil.example.com/x", "'; DROP TABLE x;--", "../../etc/passwd"} {
		var p api.Problems
		isRequiredOK := api.CheckRequired(&p, "f", v)
		isLengthOK := api.CheckLength(&p, "f", v, len(v), len(v))
		if !isRequiredOK || !isLengthOK || p.Len() != 0 {
			t.Errorf("%q: required=%v length=%v problems=%d", v, isRequiredOK, isLengthOK, p.Len())
		}
	}
}
