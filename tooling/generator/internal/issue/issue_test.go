package issue_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
)

func TestWriteSortsByPathThenCode(t *testing.T) {
	in := []issue.Issue{
		{Code: "B", Path: "f#/b", Message: "m"},
		{Code: "B", Path: "f#/a", Message: "m"},
		{Code: "A", Path: "f#/a", Message: "m"},
	}
	var out bytes.Buffer
	if err := issue.Write(&out, in); err != nil {
		t.Fatal(err)
	}
	var got []issue.Issue
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not one JSON array: %v", err)
	}
	want := []string{"A f#/a", "B f#/a", "B f#/b"}
	for i, w := range want {
		if got[i].Code+" "+got[i].Path != w {
			t.Fatalf("position %d = %s %s, want %s", i, got[i].Code, got[i].Path, w)
		}
	}
	if in[0].Code != "B" || in[0].Path != "f#/b" {
		t.Fatal("Write must not reorder the caller's slice")
	}
}

func TestPointerEscapes(t *testing.T) {
	if got := issue.Pointer("a/b", "c~d", "0"); got != "/a~1b/c~0d/0" {
		t.Fatalf("got %q", got)
	}
	if got := issue.Path("m.json", ""); got != "m.json#" {
		t.Fatalf("got %q", got)
	}
}

func TestWriteKeepsTheHintAndOmitsAnEmptyOne(t *testing.T) {
	in := []issue.Issue{
		{Code: "A", Path: "f#/a", Message: "m", Hint: "do this"},
		{Code: "B", Path: "f#/b", Message: "m"},
	}
	var out bytes.Buffer
	if err := issue.Write(&out, in); err != nil {
		t.Fatal(err)
	}
	var raw []map[string]string
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if raw[0]["hint"] != "do this" {
		t.Fatalf("hint = %q", raw[0]["hint"])
	}
	if _, present := raw[1]["hint"]; present {
		t.Fatalf("an empty hint must not be written: %v", raw[1])
	}
}

func TestIssueEncodesLineHintAndNamesOnlyWhenSet(t *testing.T) {
	plain, err := json.Marshal(issue.Issue{Code: "C", Path: "p", Message: "m"})
	if err != nil || string(plain) != `{"code":"C","path":"p","message":"m"}` {
		t.Fatalf("plain = %s (%v)", plain, err)
	}
	full, err := json.Marshal(issue.Issue{Code: "C", Path: "p", Line: 3, Message: "m", Hint: "h", SuggestedNames: []string{"a-srv"}})
	want := `{"code":"C","path":"p","line":3,"message":"m","hint":"h","suggestedNames":["a-srv"]}`
	if err != nil || string(full) != want {
		t.Fatalf("full = %s (%v)", full, err)
	}
}
