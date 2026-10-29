package main

import (
	"strings"
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
		{Code: "INPUT_FORMAT_UNKNOWN", Path: "a", Message: "m"},
		{Code: "COMPONENT_MERGE_AMBIGUOUS", Path: "a", Message: "m"},
	}
	got := addHints(in, nil)
	want := []string{
		"producer says so",
		"choose another folder: initializer generate <dir>",
		"rebuild the catalog: initializer catalog build",
		"rebuild the catalog: initializer catalog build",
		"",
		"use .yaml, .yml or .json",
		"To add, remove or rename a component, edit the input file",
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

func TestModuleUnknownHintPointsAtModuleLsAndNamesTheCloseMatch(t *testing.T) {
	ids := []string{"core", "identity", "tenancy"}
	cases := map[string]string{
		"tenacy":    `run 'initializer module ls'; did you mean "tenancy"?`,
		"identiti":  `run 'initializer module ls'; did you mean "identity"?`,
		"zzzzzzzzz": `run 'initializer module ls'`,
	}
	for unknown, want := range cases {
		found := issue.Issue{Code: "MODULE_UNKNOWN", Path: "flags#/modules/0", Message: `module is not in the catalog: "` + unknown + `"`}
		if got := addHints([]issue.Issue{found}, ids)[0].Hint; got != want {
			t.Errorf("%s: hint %q, want %q", unknown, got, want)
		}
	}
	found := issue.Issue{Code: "MODULE_UNKNOWN", Message: "no quoted id here"}
	if got := addHints([]issue.Issue{found}, ids)[0].Hint; got != `run 'initializer module ls'` {
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

func TestLastQuotedReturnsTheLastQuotedString(t *testing.T) {
	cases := map[string]string{
		`module is not in the catalog: "tenacy"`: "tenacy",
		`"first" and "second"`:                   "second",
		`no quotes here`:                         "",
		`"`:                                      "",
		`""`:                                     "",
	}
	for text, want := range cases {
		if got := lastQuoted(text); got != want {
			t.Errorf("lastQuoted(%q) = %q, want %q", text, got, want)
		}
	}
}

func FuzzLastQuotedNeverPanics(f *testing.F) {
	f.Add(`module is not in the catalog: "tenacy"`)
	f.Add(`"`)
	f.Add(`""`)
	f.Add(`"\`)
	f.Fuzz(func(t *testing.T, text string) {
		got := lastQuoted(text)
		hasQuotePair := strings.Count(text, `"`) >= 2
		if got != "" && !hasQuotePair {
			t.Fatalf("lastQuoted(%q) = %q without a quoted string", text, got)
		}
		if len(got) > len(text) {
			t.Fatalf("unquoted %q is longer than %q", got, text)
		}
	})
}

func TestEveryNewSourceCodeHasAHintRow(t *testing.T) {
	cases := map[string]string{
		"SOURCE_ROOT_NOT_FOUND": "run inside a clone, pass --root <clone>, or set INITIALIZER_ROOT",
		"SOURCE_ROOT_INVALID":   "pass --root <clone>, or set INITIALIZER_ROOT to a clone",
		"SOURCE_NOT_GIT":        "clone the repo, or pass --catalog with a catalog built elsewhere",
		"GIT_NOT_FOUND":         "install git, or pass --catalog",
		"CATALOG_UNREADABLE":    "rebuild it: initializer catalog build --out <file>",
		"SOURCE_UNREADABLE":     "run 'git status' in the checkout to see why git fails",
		"GENERATOR_STALE":       "run 'mise run cli'",
		"STACK_UNKNOWN":         "run 'initializer stack ls'",
	}
	for code, want := range cases {
		if got := addHints([]issue.Issue{{Code: code}}, nil)[0].Hint; got != want {
			t.Errorf("%s: hint %q, want %q", code, got, want)
		}
	}
}
