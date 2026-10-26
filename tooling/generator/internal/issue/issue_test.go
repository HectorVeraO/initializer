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
