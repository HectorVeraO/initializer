package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

func TestValidSelectionFileIsDecodedBeforeTheCatalogIsRead(t *testing.T) {
	code, stdout, stderr := execute(t, "generate", "--root", ".", "--catalog", "c.json", "--out", "o", "--selection", writeSelection(t, validSelection))
	if code != exitInternal || !strings.Contains(stderr, "input_unreadable") {
		t.Fatalf("want exit %d with an unreadable catalog: %d %s", exitInternal, code, stderr)
	}
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
	for _, arg := range []string{"--help", "-h", "help"} {
		t.Run(arg, func(t *testing.T) {
			code, stdout, _ := execute(t, arg)
			if code != 0 || !strings.Contains(stdout, "usage") {
				t.Fatalf("exit %d stdout %q", code, stdout)
			}
		})
	}
}

const cleanFixture = "../../internal/catalog/testdata/clean"

// writeFileList writes the NUL-separated file list of the clean fixture.
func writeFileList(t *testing.T, extra ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cleanFixture, "files.txt"))
	if err != nil {
		t.Fatalf("read files.txt: %v", err)
	}
	names := append(strings.Split(strings.TrimSpace(string(data)), "\n"), extra...)
	path := filepath.Join(t.TempDir(), "files.bin")
	if err := os.WriteFile(path, []byte(strings.Join(names, "\x00")+"\x00"), 0o600); err != nil {
		t.Fatalf("write list: %v", err)
	}
	return path
}

func TestCatalogBuildWritesTheCatalogTwiceIdentically(t *testing.T) {
	list := writeFileList(t)
	dir := t.TempDir()
	outA, outB := filepath.Join(dir, "a", "catalog.lock.json"), filepath.Join(dir, "b.json")
	root := filepath.Join(cleanFixture, "repo")
	for _, out := range []string{outA, outB} {
		code, stdout, stderr := execute(t, "catalog", "build", "--root", root, "--files", list, "--out", out)
		if code != exitOK {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		var result map[string]string
		if err := json.Unmarshal([]byte(stdout), &result); err != nil || result["catalog"] == "" {
			t.Fatalf("stdout = %q (%v)", stdout, err)
		}
		written, readErr := os.ReadFile(out)
		if readErr != nil {
			t.Fatalf("read %s: %v", out, readErr)
		}
		digest := sha256.Sum256(written)
		if result["sha256"] != hex.EncodeToString(digest[:]) {
			t.Fatalf("stdout sha256 %q does not match the written catalog", result["sha256"])
		}
	}
	a, errA := os.ReadFile(outA)
	b, errB := os.ReadFile(outB)
	if errA != nil || errB != nil || !bytes.Equal(a, b) {
		t.Fatalf("catalogs differ or are unreadable: %v %v", errA, errB)
	}
	golden, err := os.ReadFile(filepath.Join(cleanFixture, "catalog.lock.json"))
	if err != nil || !bytes.Equal(a, golden) {
		t.Fatalf("catalog differs from the golden file (%v)", err)
	}
}

func TestCatalogBuildIssueExit(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "specs", "stray"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "specs", "stray", "x.md"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	list := filepath.Join(t.TempDir(), "files.bin")
	if err := os.WriteFile(list, []byte("specs/stray/x.md\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "c.json")
	code, stdout, stderr := execute(t, "catalog", "build", "--root", repo, "--files", list, "--out", out)
	if code != exitIssues || stdout != "" {
		t.Fatalf("exit %d stdout %q", code, stdout)
	}
	hasUnowned := false
	for _, issue := range issueArray(t, stderr) {
		if issue["code"] == "FILE_UNOWNED" && issue["path"] == "specs/stray/x.md" {
			hasUnowned = true
		}
	}
	if !hasUnowned {
		t.Fatalf("want a FILE_UNOWNED issue for specs/stray/x.md on stderr: %q", stderr)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatalf("no catalog may be written when issues exist")
	}
}

func TestCatalogBuildInputErrorsExitThree(t *testing.T) {
	root := filepath.Join(cleanFixture, "repo")
	out := filepath.Join(t.TempDir(), "c.json")
	badList := filepath.Join(t.TempDir(), "bad.bin")
	if err := os.WriteFile(badList, []byte("../escape\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	emptyElement := filepath.Join(t.TempDir(), "empty.bin")
	if err := os.WriteFile(emptyElement, []byte("a\x00\x00b\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]string{
		"unreadable list": {"--root", root, "--files", filepath.Join(t.TempDir(), "missing.bin"), "--out", out},
		"escaping path":   {"--root", root, "--files", badList, "--out", out},
		"empty element":   {"--root", root, "--files", emptyElement, "--out", out},
		"root not a dir":  {"--root", filepath.Join(t.TempDir(), "none"), "--files", writeFileList(t), "--out", out},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			code, _, _ := execute(t, append([]string{"catalog", "build"}, args...)...)
			if code != exitInternal {
				t.Fatalf("exit %d, want %d", code, exitInternal)
			}
		})
	}
}

func TestCatalogBuildFileListSizeLimit(t *testing.T) {
	root := filepath.Join(cleanFixture, "repo")
	out := filepath.Join(t.TempDir(), "c.json")
	list := filepath.Join(t.TempDir(), "big.bin")
	if err := os.WriteFile(list, bytes.Repeat([]byte("a"), maxFileListBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, _ := execute(t, "catalog", "build", "--root", root, "--files", list, "--out", out)
	if code != exitInternal {
		t.Fatalf("exit %d, want %d", code, exitInternal)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatalf("no catalog may be written for an oversized list")
	}
}

func TestCatalogBuildRequiresFilesFlag(t *testing.T) {
	code, _, stderr := execute(t, "catalog", "build", "--root", ".", "--out", "c.json")
	if code != exitUsage || !strings.Contains(stderr, "files") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}
