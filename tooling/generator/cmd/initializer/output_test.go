package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// generatedDoc is the exit-0 document of generate.
type generatedDoc struct {
	Schema  int
	OK      bool
	Project string
	Path    string
	Files   int
	Catalog struct {
		SHA256 string
		File   string
	}
	Modules []struct{ Stack, Module, Version string }
	Added   []struct {
		Module     string
		RequiredBy []string
		Stack      string
	}
	Notes    []string
	Warnings []struct{ Code, Message, Hint string }
}

func TestGenerateTextResultNamesPulledModulesAndTheNextStep(t *testing.T) {
	root, catalogPath := realCatalog(t)
	dir := inFreshDir(t)
	code, stdout, stderr := execute(t, "generate", "acme", "--root", root, "--catalog", catalogPath,
		"--go-module", "example.com/acme", "--stacks", "go-srv", "--modules", "parameters")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	files := len(tree(t, filepath.Join(dir, "acme")))
	want := "Added identity (required by parameters).\n" +
		"Project acme generated in ./acme (" + itoa(files) + " files).\n" +
		"Next: cd acme, then read README.md\n"
	if stdout != want {
		t.Fatalf("stdout\n%q\nwant\n%q", stdout, want)
	}
	if stderr != "note: project name \"acme\" comes from the folder; pass --name to set it\n" {
		t.Fatalf("stderr = %q", stderr)
	}
}

func itoa(n int) string {
	encoded, _ := json.Marshal(n)
	return string(encoded)
}

func TestGenerateJSONDocument(t *testing.T) {
	root, catalogPath := realCatalog(t)
	dir := inFreshDir(t)
	code, stdout, stderr := executeJSON(t, "generate", "acme", "--root", root, "--catalog", catalogPath,
		"--go-module", "example.com/acme", "--stacks", "go-srv", "--modules", "parameters")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var doc generatedDoc
	decodeDocument(t, stdout, &doc)
	if doc.Schema != 1 || !doc.OK || doc.Project != "acme" || doc.Path != "acme" || doc.Files != len(tree(t, filepath.Join(dir, "acme"))) {
		t.Fatalf("doc = %+v", doc)
	}
	if len(doc.Catalog.SHA256) != 64 || doc.Catalog.File != filepath.ToSlash(catalogPath) {
		t.Fatalf("catalog = %+v", doc.Catalog)
	}
	if len(doc.Added) != 1 || doc.Added[0].Module != "identity" || doc.Added[0].Stack != "go-srv" ||
		strings.Join(doc.Added[0].RequiredBy, ",") != "parameters" {
		t.Fatalf("added = %+v", doc.Added)
	}
	gotModules := []string{}
	for _, module := range doc.Modules {
		if module.Version == "" {
			t.Fatalf("module without a version: %+v", module)
		}
		gotModules = append(gotModules, module.Stack+"/"+module.Module)
	}
	wantModules := "go-srv/core,go-srv/identity,go-srv/parameters"
	if strings.Join(gotModules, ",") != wantModules {
		t.Fatalf("modules = %v, want %s", gotModules, wantModules)
	}
	if len(doc.Notes) != 1 || !strings.HasPrefix(doc.Notes[0], "project name") {
		t.Fatalf("notes = %v", doc.Notes)
	}
	if !strings.Contains(stdout, `"warnings":[]`) {
		t.Fatalf("an empty list encodes as []: %s", stdout)
	}
}

func TestIssuesRenderWithTheFlagAndAHintThenOneSummary(t *testing.T) {
	root, catalogPath := realCatalog(t)
	dir := inFreshDir(t)
	if err := os.MkdirAll(filepath.Join(dir, "acme"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "acme", "keep.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"generate", "acme", "--name", "acme", "--root", root, "--catalog", catalogPath,
		"--go-module", "example.com/acme", "--stacks", "go-srv", "--modules", "identity,tenacy"}
	code, stdout, stderr := execute(t, args...)
	if code != exitIssues || stdout != "" {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
	want := "error: ./acme: output directory is not empty [OUTPUT_NOT_EMPTY]\n" +
		"  hint: choose another folder: initializer generate <dir>\n" +
		"error: --modules: module is not in the catalog: \"tenacy\" [MODULE_UNKNOWN]\n" +
		"  hint: did you mean \"tenancy\"?\n" +
		"2 problems; nothing was written.\n"
	if stderr != want {
		t.Fatalf("stderr\n%s\nwant\n%s", stderr, want)
	}
}

func TestBadFormatValueSuggestsTheClosestOne(t *testing.T) {
	code, stdout, stderr := execute(t, "verify", "--format", "jsno")
	want := "error: --format: invalid value: \"jsno\" must be text or json\n  hint: did you mean \"json\"?\n1 problem.\n"
	if code != exitUsage || stdout != "" || stderr != want {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	code, _, stderr = execute(t, "verify", "--log", "yaml")
	if code != exitUsage || !strings.Contains(stderr, `error: --log: invalid value: "yaml" must be json`) {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = execute(t, "verify", "--format", "xml")
	if code != exitUsage || strings.Contains(stderr, "hint:") {
		t.Fatalf("nothing is close to xml: exit %d, stderr %q", code, stderr)
	}
}

func TestHelpTakesNeitherFormatNorLog(t *testing.T) {
	_, stdout, _ := execute(t, "help", "verify")
	if !strings.Contains(stdout, "--format text|json\n        Result format (default: text)") ||
		!strings.Contains(stdout, "--log json\n        Write structured log events to stderr (default: off)") {
		t.Fatalf("verify help misses the shared flags:\n%s", stdout)
	}
	_, stdout, _ = execute(t, "help", "help")
	if strings.Contains(stdout, "--format") {
		t.Fatalf("help takes no --format:\n%s", stdout)
	}
}

func TestUsageErrorDocumentGoesToStderrWithEmptyStdout(t *testing.T) {
	cases := map[string][]string{
		"unknown flag":    {"verify", "--nope", "--format", "json"},
		"unknown command": {"frobnicate", "--format=json"},
		"group no verb":   {"catalog", "--format", "json"},
		"after the value": {"generate", "--root", "r", "--catalog", "c", "--format", "json", "--name", "a", "--name", "b"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			code, stdout, stderr := execute(t, args...)
			if code != exitUsage || stdout != "" {
				t.Fatalf("exit %d, stdout %q", code, stdout)
			}
			var doc struct {
				Schema   int
				OK       bool
				Problems []struct{ Arg, Message, Suggestion string }
				Issues   []map[string]any
			}
			decodeDocument(t, stderr, &doc)
			if doc.Schema != 1 || doc.OK || len(doc.Problems) == 0 || doc.Issues == nil {
				t.Fatalf("doc = %+v\n%s", doc, stderr)
			}
		})
	}
	_, _, stderr := execute(t, "verify", "--nmae", "--format", "json")
	if !strings.Contains(stderr, `"problems":[{"arg":"--nmae","message":"unknown flag"`) {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestExitThreeDocumentsCarryTheirCode(t *testing.T) {
	root := filepath.Join(cleanFixture, "repo")
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	cases := map[string]struct {
		args []string
		code string
	}{
		"file list":     {[]string{"catalog", "build", "--root", root, "--files", filepath.Join(t.TempDir(), "none"), "--out", "o"}, "INPUT_UNREADABLE"},
		"root":          {[]string{"catalog", "build", "--root", filepath.Join(t.TempDir(), "none"), "--files", writeFileList(t), "--out", "o"}, "INPUT_UNREADABLE"},
		"catalog":       {[]string{"generate", "o", "--root", e.root, "--catalog", filepath.Join(t.TempDir(), "none"), "-f", writeSelection(t, validSelection)}, "CATALOG_UNREADABLE"},
		"verify":        {[]string{"verify", t.TempDir()}, "VERIFY_UNREADABLE"},
		"project write": {[]string{"generate", filepath.Join(blocker, "proj"), "--root", e.root, "--catalog", e.catalog, "-f", writeSelection(t, validSelection)}, "OUTPUT_UNWRITABLE"},
		"catalog write": {[]string{"catalog", "build", "--root", root, "--files", writeFileList(t), "--out", filepath.Join(blocker, "c.json")}, "OUTPUT_UNWRITABLE"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			code, stdout, stderr := executeJSON(t, tc.args...)
			if code != exitInternal || stdout != "" {
				t.Fatalf("exit %d, stdout %q, stderr %s", code, stdout, stderr)
			}
			var doc struct {
				Schema int
				OK     bool
				Error  struct{ Code, Message string }
			}
			decodeDocument(t, lastLines(stderr), &doc)
			if doc.Schema != 1 || doc.OK || doc.Error.Code != tc.code || doc.Error.Message == "" {
				t.Fatalf("doc = %+v", doc)
			}
			_, _, text := execute(t, tc.args...)
			if !strings.HasPrefix(text, "error: ") || !strings.HasSuffix(text, "["+tc.code+"]\n") {
				t.Fatalf("text = %q", text)
			}
		})
	}
}

// lastLines drops the warning lines that precede the document on stderr.
func lastLines(stderr string) string {
	var kept []string
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		if strings.HasPrefix(line, "{") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func TestAliasWarningTravelsInTextAndInTheDocument(t *testing.T) {
	e := newEnv(t)
	sel := writeSelection(t, validSelection)
	code, stdout, stderr := executeJSON(t, "generate", "--root", e.root, "--catalog", e.catalog, "--out", e.out, "--selection", sel)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var doc generatedDoc
	decodeDocument(t, stdout, &doc)
	if len(doc.Warnings) != 2 || doc.Warnings[0].Code != "FLAG_DEPRECATED" ||
		doc.Warnings[0].Message != "--out is deprecated" || doc.Warnings[0].Hint != "pass the folder as an argument: initializer generate <dir>" ||
		doc.Warnings[1].Message != "--selection is deprecated" {
		t.Fatalf("warnings = %+v", doc.Warnings)
	}
	if got := linesWithPrefix(stderr, "warning:"); len(got) != 2 {
		t.Fatalf("stderr keeps one warning line per alias: %q", stderr)
	}
}

func TestLogEventsAppearOnlyWithLogJSON(t *testing.T) {
	e := newEnv(t)
	sel := writeSelection(t, validSelection)
	code, _, stderr := execute(t, "generate", e.out, "--root", e.root, "--catalog", e.catalog, "-f", sel)
	if code != exitOK || strings.Contains(stderr, "{") {
		t.Fatalf("default run: exit %d, stderr %q", code, stderr)
	}
	second := newEnv(t)
	code, _, stderr = executeLogged(t, "generate", "--root", second.root, "--catalog", second.catalog, "--out", second.out, "--selection", sel)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, event := range []string{"selection_decoded", "project_generated", "flag_deprecated"} {
		if !strings.Contains(stderr, `"msg":"`+event+`"`) {
			t.Errorf("no %s event: %s", event, stderr)
		}
	}
	if !strings.Contains(stderr, `"flag":"--out"`) {
		t.Errorf("flag_deprecated names the flag: %s", stderr)
	}
}

func TestCatalogBuildTextResult(t *testing.T) {
	out := filepath.Join(t.TempDir(), "catalog.lock.json")
	code, stdout, stderr := execute(t, "catalog", "build", "--root", filepath.Join(cleanFixture, "repo"), "--files", writeFileList(t), "--out", out)
	if code != exitOK || stderr != "" {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ SHA256 string }
	_, jsonOut, _ := executeJSON(t, "catalog", "build", "--root", filepath.Join(cleanFixture, "repo"), "--files", writeFileList(t), "--out", out)
	decodeDocument(t, jsonOut, &doc)
	want := "Catalog written to " + filepath.ToSlash(out) + " (sha256 " + doc.SHA256[:12] + ").\n"
	if stdout != want || len(written) == 0 {
		t.Fatalf("stdout %q, want %q", stdout, want)
	}
}

func TestVerifyTextAndJSONReportDifferences(t *testing.T) {
	e := newEnv(t)
	if code, _, stderr := execute(t, e.args("--selection", writeSelection(t, validSelection))...); code != exitOK {
		t.Fatalf("generate: %d %s", code, stderr)
	}
	srv := folderOf(t, e.out, "go-srv")
	if err := os.Remove(filepath.Join(e.out, srv, "internal", "platform", "parameters", "parameters.go")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := execute(t, "verify", e.out)
	want := "MISSING " + srv + "/internal/platform/parameters/parameters.go: listed file does not exist\n1 difference\n"
	if code != exitIssues || stdout != want || stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	code, stdout, _ = executeJSON(t, "verify", e.out)
	wantJSON := `{"schema":1,"ok":false,"project":"acme-backend","differences":[{"code":"MISSING","path":"` + srv + `/internal/platform/parameters/parameters.go","message":"listed file does not exist"}],"warnings":[]}` + "\n"
	if code != exitIssues || stdout != wantJSON {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
}

// Under --format json every exit path leaves stdout empty or holding one
// JSON value, and exit 2 and 3 leave their one document on stderr.
func TestJSONRunsWriteOneDocumentOnTheRightStream(t *testing.T) {
	e := newEnv(t)
	sel := writeSelection(t, validSelection)
	generated := newEnv(t)
	if code, _, stderr := execute(t, generated.args("--selection", sel)...); code != exitOK {
		t.Fatalf("generate: %d %s", code, stderr)
	}
	cases := map[string]struct {
		args []string
		code int
	}{
		"ok":          {e.args("--selection", sel), exitOK},
		"issues":      {e.args("--selection", writeSelection(t, strings.Replace(validSelection, `"parameters"`, `"nope"`, 1))), exitIssues},
		"usage":       {[]string{"generate", "--bogus"}, exitUsage},
		"internal":    {[]string{"verify", t.TempDir()}, exitInternal},
		"verify ok":   {[]string{"verify", generated.out}, exitOK},
		"no such cmd": {[]string{"nosuch"}, exitUsage},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			code, stdout, stderr := executeJSON(t, tc.args...)
			if code != tc.code {
				t.Fatalf("exit %d, want %d: %s", code, tc.code, stderr)
			}
			document := stdout
			if tc.code == exitUsage || tc.code == exitInternal {
				if stdout != "" {
					t.Fatalf("stdout %q must be empty", stdout)
				}
				document = lastLines(stderr)
			}
			var anything map[string]any
			decodeDocument(t, document, &anything)
			if anything["schema"] != float64(1) || anything["ok"] != (tc.code == exitOK) {
				t.Fatalf("document = %v", anything)
			}
		})
	}
}

func FuzzLookAheadNeverPanicsAndOnlyAcceptsKnownValues(f *testing.F) {
	f.Add("--format", "json")
	f.Add("--format=json", "")
	f.Add("--log", "json")
	f.Add("-", "--format")
	f.Fuzz(func(t *testing.T, first, second string) {
		format, logJSON := lookAhead([]string{first, second})
		if format != formatText && format != formatJSON {
			t.Fatalf("format %q", format)
		}
		namesJSON := first == "--format=json" || second == "--format=json" ||
			(first == "--format" && second == "json")
		if namesJSON && format != formatJSON {
			t.Fatalf("args %q %q: format %q, want json", first, second, format)
		}
		mentionsJSON := strings.Contains(first, "json") || strings.Contains(second, "json")
		if (format == formatJSON || logJSON) && !mentionsJSON {
			t.Fatalf("args %q %q: json without a json value", first, second)
		}
	})
}

// failingWriter rejects every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("closed pipe")
}

func TestJSONResultThatCannotBeWrittenExitsInternalAndLogsTheEvent(t *testing.T) {
	root, catalogPath := realCatalog(t)
	inFreshDir(t)
	var errOut bytes.Buffer
	args := []string{"generate", "acme", "--root", root, "--catalog", catalogPath,
		"--go-module", "example.com/acme", "--stacks", "go-srv", "--modules", "parameters", "--format", "json", "--log", "json"}
	code := run(args, failingWriter{}, &errOut)
	if code != exitInternal {
		t.Fatalf("exit %d, want %d: %s", code, exitInternal, errOut.String())
	}
	if !strings.Contains(errOut.String(), "result_unwritable") {
		t.Fatalf("stderr lacks the result_unwritable event: %s", errOut.String())
	}
}

func TestInternalFailureIsExitThreeInBothFormats(t *testing.T) {
	var out, errOut bytes.Buffer
	textCtx := &Context{Stdout: &out, Stderr: &errOut, Format: formatText}
	if code := reportFailure(textCtx, codeInternalError, "disk full"); code != exitInternal {
		t.Fatalf("text exit %d", code)
	}
	if out.Len() != 0 || errOut.String() != "error: disk full [INTERNAL_ERROR]\n" {
		t.Fatalf("stdout %q, stderr %q", out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	jsonCtx := &Context{Stdout: &out, Stderr: &errOut, Format: formatJSON}
	if code := reportFailure(jsonCtx, codeInternalError, "disk full"); code != exitInternal {
		t.Fatalf("json exit %d", code)
	}
	var doc struct {
		Schema int
		OK     bool
		Error  struct{ Code, Message string }
	}
	decodeDocument(t, errOut.String(), &doc)
	if out.Len() != 0 || doc.Schema != 1 || doc.OK || doc.Error.Code != "INTERNAL_ERROR" || doc.Error.Message != "disk full" {
		t.Fatalf("stdout %q, doc %+v", out.String(), doc)
	}
}
