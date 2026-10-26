package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validSelection = `{
  "schema": 1,
  "project": { "name": "acme-backend", "goModule": "example.com/acme/backend" },
  "stacks": ["go-srv"],
  "modules": ["parameters"]
}`

func execute(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func writeSelection(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gatbase.selection.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write selection: %v", err)
	}
	return path
}

// issueArray returns the issue array printed on stderr, or nil when none.
func issueArray(t *testing.T, stderr string) []map[string]string {
	t.Helper()
	var found []map[string]string
	arrays := 0
	for _, line := range strings.Split(stderr, "\n") {
		if !strings.HasPrefix(line, "[") {
			continue
		}
		arrays++
		if err := json.Unmarshal([]byte(line), &found); err != nil {
			t.Fatalf("stderr array is not JSON: %v", err)
		}
	}
	if arrays > 1 {
		t.Fatalf("want at most one array on stderr, got %d", arrays)
	}
	return found
}

func TestUsageErrorsExitTwo(t *testing.T) {
	sel := writeSelection(t, validSelection)
	common := []string{"generate", "--root", ".", "--catalog", "c.json", "--out", "o"}
	cases := map[string]struct {
		args    []string
		message string
	}{
		"no args":                  {[]string{}, "a subcommand is required"},
		"unknown subcommand":       {[]string{"frobnicate"}, "unknown subcommand"},
		"catalog without build":    {[]string{"catalog"}, "requires the build subcommand"},
		"unknown flag":             {[]string{"verify", "--project", "p", "--nope"}, "not defined"},
		"unexpected argument":      {[]string{"verify", "--project", "p", "extra"}, "unexpected argument"},
		"missing project":          {[]string{"verify"}, "missing required flag"},
		"missing catalog flags":    {[]string{"catalog", "build", "--root", "."}, "missing required flag"},
		"generate missing out":     {[]string{"generate", "--root", ".", "--catalog", "c.json", "--selection", sel}, "missing required flag"},
		"no selection":             {common, "a selection is required"},
		"file plus flag":           {append(append([]string{}, common...), "--selection", sel, "--name", "acme-backend"), "cannot be combined"},
		"file plus all-modules":    {append(append([]string{}, common...), "--selection", sel, "--all-modules"), "cannot be combined"},
		"flags without modules":    {append(append([]string{}, common...), "--name", "n", "--stacks", "go-srv"), "requires --modules or --all-modules"},
		"all-modules plus modules": {append(append([]string{}, common...), "--all-modules", "--modules", "a", "--name", "n"), "cannot be combined"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			code, _, stderr := execute(t, tc.args...)
			if code != 2 {
				t.Fatalf("exit %d, want 2", code)
			}
			if !strings.Contains(stderr, tc.message) {
				t.Fatalf("stderr %q does not mention %q", stderr, tc.message)
			}
		})
	}
}

func TestUnreadableSelectionExitsThree(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.json")
	code, _, stderr := execute(t, "generate", "--root", ".", "--catalog", "c.json", "--out", "o", "--selection", missing)
	if code != 3 {
		t.Fatalf("exit %d, want 3", code)
	}
	if issueArray(t, stderr) != nil {
		t.Fatalf("no issue array expected for an I/O error: %s", stderr)
	}
}

func TestSelectionSizeLimit(t *testing.T) {
	cases := map[string]struct {
		padding int
		code    int
	}{
		"exactly at the limit":    {maxInputBytes - len(validSelection), 0},
		"one byte over the limit": {maxInputBytes - len(validSelection) + 1, 3},
		"far over the limit":      {maxInputBytes, 3},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeSelection(t, validSelection+strings.Repeat(" ", tc.padding))
			code, _, stderr := execute(t, "generate", "--root", ".", "--catalog", "c.json", "--out", "o", "--selection", path)
			if tc.code == 3 && code != 3 {
				t.Fatalf("exit %d, want 3", code)
			}
			if tc.code == 3 && issueArray(t, stderr) != nil {
				t.Fatalf("no issue array expected for an I/O error: %s", stderr)
			}
			if tc.code == 0 && !strings.Contains(stderr, "selection_decoded") {
				t.Fatalf("input at the limit must be decoded, got exit %d: %s", code, stderr)
			}
		})
	}
}

func TestSelectionIssuesExitOneWithSortedArray(t *testing.T) {
	bad := `{"schema":1,"project":{"name":"A","goModule":"x"},"stacks":["ng-web"],"modules":["Bad"]}`
	code, _, stderr := execute(t, "generate", "--root", ".", "--catalog", "c.json", "--out", "o", "--selection", writeSelection(t, bad))
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	issues := issueArray(t, stderr)
	if len(issues) < 3 {
		t.Fatalf("want every problem reported, got %v", issues)
	}
	for i := 1; i < len(issues); i++ {
		if issues[i-1]["path"] > issues[i]["path"] {
			t.Fatalf("issues not sorted by path: %v", issues)
		}
	}
	for _, found := range issues {
		if found["code"] != "SELECTION_INVALID" || found["message"] == "" {
			t.Fatalf("malformed issue: %v", found)
		}
	}
}

func TestFlagSelectionIssuesUseFlagsSource(t *testing.T) {
	code, _, stderr := execute(t, "generate", "--root", ".", "--catalog", "c.json", "--out", "o",
		"--name", "A", "--stacks", "go-srv", "--go-module", "example.com/acme/backend", "--modules", "parameters")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	issues := issueArray(t, stderr)
	if len(issues) != 1 || issues[0]["path"] != "flags#/project/name" {
		t.Fatalf("want one issue at flags#/project/name, got %v", issues)
	}
}

// decodedEvent returns the selection_decoded log record on stderr.
func decodedEvent(t *testing.T, stderr string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		if record["msg"] == "selection_decoded" {
			return record
		}
	}
	t.Fatalf("no selection_decoded event on stderr: %s", stderr)
	return nil
}

func TestValidSelectionFileIsDecodedBeforeGenerate(t *testing.T) {
	_, stdout, stderr := execute(t, "generate", "--root", ".", "--catalog", "c.json", "--out", "o", "--selection", writeSelection(t, validSelection))
	if issueArray(t, stderr) != nil || stdout != "" {
		t.Fatalf("want no issues and empty stdout: %q %s", stdout, stderr)
	}
	event := decodedEvent(t, stderr)
	if event["stack_count"] != float64(1) || event["module_count"] != float64(1) || event["all_modules"] != false {
		t.Fatalf("decoded selection mismatch: %v", event)
	}
}

func TestValidFlagSelectionAndAllModules(t *testing.T) {
	_, _, stderr := execute(t, "generate", "--root", ".", "--catalog", "c.json", "--out", "o",
		"--name", "acme-backend", "--go-module", "example.com/acme/backend", "--stacks", "go-srv,ng-web", "--all-modules")
	event := decodedEvent(t, stderr)
	if event["stack_count"] != float64(2) || event["module_count"] != float64(0) || event["all_modules"] != true {
		t.Fatalf("decoded selection mismatch: %v", event)
	}
}

func TestUnimplementedSubcommandsExitThree(t *testing.T) {
	cases := map[string][]string{
		"catalog build": {"catalog", "build", "--root", ".", "--files", "f.txt", "--out", "c.json"},
		"verify":        {"verify", "--project", "p"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			code, _, stderr := execute(t, args...)
			if code != 3 || !strings.Contains(stderr, "not implemented") {
				t.Fatalf("exit %d, stderr %s", code, stderr)
			}
		})
	}
}

func TestProgressLinesAreStructuredJSON(t *testing.T) {
	_, _, stderr := execute(t, "generate", "--root", ".", "--catalog", "c.json", "--out", "o", "--selection", writeSelection(t, validSelection))
	events := 0
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("stderr line is not JSON: %q", line)
		}
		msg, _ := record["msg"].(string)
		if msg == "" || strings.ContainsAny(msg, " :=") {
			t.Fatalf("event name must be a constant token, got %q", msg)
		}
		events++
	}
	if events == 0 {
		t.Fatal("expected progress lines")
	}
}

func TestHelpExitsZero(t *testing.T) {
	code, stdout, _ := execute(t, "--help")
	if code != 0 || !strings.Contains(stdout, "usage") {
		t.Fatalf("exit %d stdout %q", code, stdout)
	}
}
