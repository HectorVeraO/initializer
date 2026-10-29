package main

import (
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
)

func TestAddHintsKeepsAProducerHintAndFillsTheRest(t *testing.T) {
	in := []issue.Issue{
		{Code: "OUTPUT_NOT_EMPTY", Path: "acme", Message: "m", Hint: "producer says so"},
		{Code: "OUTPUT_NOT_EMPTY", Path: "acme", Message: "m"},
		{Code: "CATALOG_FILE_MISMATCH", Path: "a", Message: "m"},
		{Code: "CATALOG_GENERATOR_MISMATCH", Path: "a", Message: "m"},
		{Code: "SELECTION_INVALID", Path: "a", Message: "m"},
	}
	got := addHints(in, nil)
	want := []string{
		"producer says so",
		"choose another folder: initializer generate <dir>",
		"rebuild the catalog: initializer catalog build",
		"rebuild the catalog: initializer catalog build",
		"",
	}
	for i, hint := range want {
		if got[i].Hint != hint {
			t.Errorf("issue %d hint = %q, want %q", i, got[i].Hint, hint)
		}
	}
	if in[1].Hint != "" {
		t.Error("addHints must not change the caller's slice")
	}
}

func TestModuleUnknownHintNamesTheCloseMatchOnly(t *testing.T) {
	ids := []string{"core", "identity", "tenancy"}
	cases := map[string]string{
		"tenacy":    `did you mean "tenancy"?`,
		"identiti":  `did you mean "identity"?`,
		"zzzzzzzzz": "",
	}
	for unknown, want := range cases {
		found := issue.Issue{Code: "MODULE_UNKNOWN", Path: "flags#/modules/0", Message: `module is not in the catalog: "` + unknown + `"`}
		if got := addHints([]issue.Issue{found}, ids)[0].Hint; got != want {
			t.Errorf("%s: hint %q, want %q", unknown, got, want)
		}
	}
	found := issue.Issue{Code: "MODULE_UNKNOWN", Message: "no quoted id here"}
	if got := addHints([]issue.Issue{found}, ids)[0].Hint; got != "" {
		t.Errorf("hint %q without an id", got)
	}
}

func TestWhereOfRendersTheRightPlace(t *testing.T) {
	cases := []struct {
		name  string
		found issue.Issue
		view  issueView
		want  string
	}{
		{"name flag", issue.Issue{Path: "flags#/project/name"}, issueView{}, "--name"},
		{"go module flag", issue.Issue{Path: "flags#/project/goModule"}, issueView{}, "--go-module"},
		{"stacks element", issue.Issue{Path: "flags#/stacks/1"}, issueView{}, "--stacks"},
		{"modules element", issue.Issue{Path: "flags#/modules/0"}, issueView{}, "--modules"},
		{"unknown flag pointer", issue.Issue{Path: "flags#/other"}, issueView{}, "flags#/other"},
		{"flag-built selection", issue.Issue{Path: "selection#/modules/1"}, issueView{}, "--modules"},
		{"file-built selection", issue.Issue{Path: "selection#/modules/1"}, issueView{selectionFile: "acme.json"}, "acme.json /modules/1"},
		{"file", issue.Issue{Path: "acme.json#/modules/0"}, issueView{}, "acme.json /modules/0"},
		{"file with line", issue.Issue{Path: "acme.yaml#/modules/0", Line: 9}, issueView{}, "acme.yaml:9 /modules/0"},
		{"folder argument", issue.Issue{Path: "args#/dir"}, issueView{dir: "My App"}, "./My App"},
		{"folder argument unknown", issue.Issue{Path: "args#/dir"}, issueView{}, "args#/dir"},
		{"output folder", issue.Issue{Code: "OUTPUT_NOT_EMPTY", Path: "acme"}, issueView{}, "./acme"},
		{"plain path", issue.Issue{Code: "FILE_UNOWNED", Path: "specs/x/y.md"}, issueView{}, "specs/x/y.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := whereOf(tc.found, tc.view); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDisplayPath(t *testing.T) {
	cases := map[string]string{
		"acme":      "./acme",
		"./acme":    "./acme",
		"../acme":   "../acme",
		".":         ".",
		"a/b":       "./a/b",
		"/abs/acme": "/abs/acme",
	}
	for in, want := range cases {
		if got := displayPath(in); got != want {
			t.Errorf("displayPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSuggestAcceptsAnAdjacentSwap(t *testing.T) {
	if got := suggest("jsno", []string{"text", "json"}); got != "json" {
		t.Fatalf("got %q", got)
	}
}

func FuzzLastQuotedNeverPanics(f *testing.F) {
	f.Add(`module is not in the catalog: "tenacy"`)
	f.Add(`"`)
	f.Add(`""`)
	f.Add(`"\`)
	f.Fuzz(func(t *testing.T, text string) {
		if got := lastQuoted(text); len(got) > len(text) {
			t.Fatalf("unquoted %q is longer than %q", got, text)
		}
	})
}
