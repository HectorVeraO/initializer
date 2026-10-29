package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
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
	path := filepath.Join(t.TempDir(), "initializer.selection.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write selection: %v", err)
	}
	return path
}

// executeJSON runs args with --format json.
func executeJSON(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	return execute(t, append(append([]string{}, args...), "--format", "json")...)
}

// executeLogged runs args with --log json.
func executeLogged(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	return execute(t, append(append([]string{}, args...), "--log", "json")...)
}

// decodeDocument decodes text, which must hold exactly one JSON value, into target.
func decodeDocument(t *testing.T, text string, target any) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(text))
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, text)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		t.Fatalf("more than one JSON value: %s", text)
	}
}

// issueArray returns the issues of the exit-1 document in stdout, or nil when
// stdout is empty. Values are the string members only.
func issueArray(t *testing.T, stdout string) []map[string]string {
	t.Helper()
	if stdout == "" {
		return nil
	}
	var doc struct {
		Schema int              `json:"schema"`
		OK     bool             `json:"ok"`
		Issues []map[string]any `json:"issues"`
	}
	decodeDocument(t, stdout, &doc)
	if doc.Schema != 1 || doc.OK {
		t.Fatalf("not an issues document: %s", stdout)
	}
	var found []map[string]string
	for _, raw := range doc.Issues {
		flat := map[string]string{}
		for key, value := range raw {
			if text, ok := value.(string); ok {
				flat[key] = text
			}
		}
		found = append(found, flat)
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
		"no args":                  {[]string{}, "Commands:"},
		"unknown command":          {[]string{"frobnicate"}, `unknown command "frobnicate"`},
		"catalog without build":    {[]string{"catalog"}, "catalog needs a command: build"},
		"unknown flag":             {[]string{"verify", "--nope"}, "unknown flag"},
		"unexpected argument":      {[]string{"verify", "p", "extra"}, "unexpected argument"},
		"folder twice":             {[]string{"verify", "p", "--project", "p"}, "pass the folder once"},
		"missing catalog flags":    {[]string{"catalog", "build", "--root", "."}, "--files: is required"},
		"generate missing root":    {[]string{"generate", "--catalog", "c.json", "--selection", sel}, "--root: is required"},
		"generate missing catalog": {[]string{"generate", "--root", ".", "--selection", sel}, "--catalog: is required"},
		"no selection":             {common, "a selection is required"},
		"file plus flag":           {append(append([]string{}, common...), "--selection", sel, "--name", "acme-backend"), "cannot be combined"},
		"file plus all-modules":    {append(append([]string{}, common...), "--selection", sel, "--all-modules"), "cannot be combined"},
		"flags without modules":    {append(append([]string{}, common...), "--name", "n", "--stacks", "go-srv"), "requires --modules or --all-modules"},
		"all-modules plus modules": {append(append([]string{}, common...), "--all-modules", "--modules", "a", "--name", "n"), "cannot be combined"},
		"selection plus file":      {append(append([]string{}, common...), "--selection", sel, "-f", sel), "pass the file once"},
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

func TestVerifyWithoutADirectoryChecksTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	code, _, stderr := execute(t, "verify")
	if code != exitInternal || !strings.HasSuffix(stderr, "[VERIFY_UNREADABLE]\n") {
		t.Fatalf("an empty working directory is not a project: exit %d %s", code, stderr)
	}

	lock, err := lockfile.Encode(lockfile.Lock{Schema: lockfile.SchemaVersion})
	if err != nil {
		t.Fatalf("encode lock: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, lockfile.Name), lock, 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	code, stdout, stderr := execute(t, "verify")
	if code != exitOK {
		t.Fatalf("a working directory holding a lock verifies: exit %d %s", code, stderr)
	}
	if stdout != "No differences.\n" {
		t.Fatalf("stdout %q does not report zero differences", stdout)
	}
}

func TestUnreadableSelectionExitsThree(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.json")
	code, stdout, stderr := executeJSON(t, "generate", "o", "--root", ".", "--catalog", "c.json", "-f", missing)
	if code != 3 || stdout != "" {
		t.Fatalf("exit %d, stdout %q, want 3 and empty", code, stdout)
	}
	var doc struct {
		Schema int  `json:"schema"`
		OK     bool `json:"ok"`
		Error  struct{ Code, Message string }
	}
	decodeDocument(t, stderr, &doc)
	if doc.Schema != 1 || doc.OK || doc.Error.Code != "INPUT_UNREADABLE" || doc.Error.Message == "" {
		t.Fatalf("exit-3 document = %+v", doc)
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
			code, stdout, stderr := executeLogged(t, "generate", "--root", ".", "--catalog", "c.json", "--out", "o", "--selection", path)
			if tc.code == 3 && code != 3 {
				t.Fatalf("exit %d, want 3", code)
			}
			if tc.code == 3 && (stdout != "" || !strings.Contains(stderr, "[INPUT_UNREADABLE]")) {
				t.Fatalf("want an I/O error on stderr only: %q %s", stdout, stderr)
			}
			if tc.code == 0 && !strings.Contains(stderr, "selection_decoded") {
				t.Fatalf("input at the limit must be decoded, got exit %d: %s", code, stderr)
			}
		})
	}
}

func TestSelectionIssuesExitOneWithSortedArray(t *testing.T) {
	bad := `{"schema":1,"project":{"name":"A","goModule":"x"},"stacks":["ng-web"],"modules":["Bad"]}`
	code, stdout, _ := executeJSON(t, "generate", "--root", ".", "--catalog", "c.json", "--out", "o", "--selection", writeSelection(t, bad))
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	issues := issueArray(t, stdout)
	if len(issues) < 3 {
		t.Fatalf("want every problem reported, got %v", issues)
	}
	for i := 1; i < len(issues); i++ {
		if issues[i-1]["path"] > issues[i]["path"] {
			t.Fatalf("issues not sorted by path: %v", issues)
		}
	}
	wantCodes := map[string]string{
		"#/modules/0":        "SELECTION_INVALID",
		"#/project/goModule": "SELECTION_INVALID",
		"#/project/name":     "NAME_INVALID",
		"#/stacks":           "SELECTION_INVALID",
	}
	if len(issues) != len(wantCodes) {
		t.Fatalf("want %d issues, got %v", len(wantCodes), issues)
	}
	for _, found := range issues {
		_, suffix, _ := strings.Cut(found["path"], "#")
		wantCode, known := wantCodes["#"+suffix]
		if !known || found["code"] != wantCode || found["message"] == "" {
			t.Fatalf("malformed issue: %v", found)
		}
	}
}

func TestFlagSelectionIssuesUseFlagsSource(t *testing.T) {
	code, stdout, _ := executeJSON(t, "generate", "--root", ".", "--catalog", "c.json", "--out", "o",
		"--name", "A", "--stacks", "go-srv", "--go-module", "example.com/acme/backend", "--modules", "parameters")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	issues := issueArray(t, stdout)
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
	code, stdout, stderr := executeLogged(t, "generate", "o", "--root", ".", "--catalog", "c.json", "-f", writeSelection(t, validSelection))
	if code != exitInternal || !strings.Contains(stderr, "input_unreadable") || !strings.Contains(stderr, "[CATALOG_UNREADABLE]") {
		t.Fatalf("want exit %d with an unreadable catalog: %d %s", exitInternal, code, stderr)
	}
	if stdout != "" {
		t.Fatalf("want empty stdout: %q", stdout)
	}
	event := decodedEvent(t, stderr)
	if event["stack_count"] != float64(1) || event["module_count"] != float64(1) || event["all_modules"] != false {
		t.Fatalf("decoded selection mismatch: %v", event)
	}
}

func TestValidFlagSelectionAndAllModules(t *testing.T) {
	_, _, stderr := executeLogged(t, "generate", "o", "--root", ".", "--catalog", "c.json",
		"--name", "acme-backend", "--go-module", "example.com/acme/backend", "--stacks", "go-srv,ng-web", "--all-modules")
	event := decodedEvent(t, stderr)
	if event["stack_count"] != float64(2) || event["module_count"] != float64(0) || event["all_modules"] != true {
		t.Fatalf("decoded selection mismatch: %v", event)
	}
}

func TestProgressLinesAreStructuredJSON(t *testing.T) {
	_, _, stderr := executeLogged(t, "generate", "o", "--root", ".", "--catalog", "c.json", "-f", writeSelection(t, validSelection))
	events := 0
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		if strings.HasPrefix(line, "error:") {
			continue
		}
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
		code, stdout, stderr := executeJSON(t, "catalog", "build", "--root", root, "--files", list, "--out", out)
		if code != exitOK {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		var result map[string]any
		decodeDocument(t, stdout, &result)
		if result["schema"] != float64(1) || result["ok"] != true || result["catalog"] == "" {
			t.Fatalf("stdout = %q", stdout)
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
	code, stdout, stderr := executeJSON(t, "catalog", "build", "--root", repo, "--files", list, "--out", out)
	if code != exitIssues || stderr != "" {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	hasUnowned := false
	for _, issue := range issueArray(t, stdout) {
		if issue["code"] == "FILE_UNOWNED" && issue["path"] == "specs/stray/x.md" {
			hasUnowned = true
		}
	}
	if !hasUnowned {
		t.Fatalf("want a FILE_UNOWNED issue for specs/stray/x.md on stdout: %q", stdout)
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
