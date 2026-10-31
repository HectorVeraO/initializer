package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// executeIn runs args in-process with the working directory wd and stdin.
func executeIn(t *testing.T, wd, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	ctx := &Context{
		Stdout: &out, Stderr: &errOut, Stdin: strings.NewReader(stdin), WorkDir: wd,
		Format: formatText, Logger: discardLogger(),
	}
	code = runWith(ctx, args)
	return code, out.String(), errOut.String()
}

// sources returns the --root and --catalog flags of e.
func (e env) sources() []string {
	return []string{"--root", e.root, "--catalog", e.catalog}
}

// inputArgs builds a generate line with the sources and extra arguments.
func (e env) inputArgs(extra ...string) []string {
	return append(append([]string{"generate"}, e.sources()...), extra...)
}

// writeNamed writes a file called name in a fresh directory and returns its path.
func writeNamed(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// projectLock returns the generic content of a generated project's lock.
func projectLock(t *testing.T, dir string) map[string]any {
	t.Helper()
	return readJSONFile(t, filepath.Join(dir, "initializer.lock.json"))
}

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return value
}

// lockedName returns names.project of a generated project's lock.
func lockedName(t *testing.T, dir string) string {
	t.Helper()
	names, _ := projectLock(t, dir)["names"].(map[string]any)
	name, _ := names["project"].(string)
	return name
}

// folderOf returns the component folder that the project's lock records for
// stack.
func folderOf(t *testing.T, dir, stack string) string {
	t.Helper()
	list, _ := projectLock(t, dir)["components"].([]any)
	for _, raw := range list {
		entry, _ := raw.(map[string]any)
		if entry["stack"] == stack {
			return entry["name"].(string)
		}
	}
	t.Fatalf("the lock of %s has no component of stack %s", dir, stack)
	return ""
}

// goModuleOf returns the module line of the project's backend go.mod.
func goModuleOf(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, folderOf(t, dir, "go-srv"), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	first, _, _ := strings.Cut(string(data), "\n")
	return strings.TrimSpace(strings.TrimPrefix(first, "module "))
}

// lockedModules returns the modules of one stack in the project's lock.
func lockedModules(t *testing.T, dir, stack string) []string {
	t.Helper()
	var modules []string
	list, _ := projectLock(t, dir)["modules"].([]any)
	for _, raw := range list {
		entry, _ := raw.(map[string]any)
		if entry["stack"] == stack {
			modules = append(modules, entry["module"].(string))
		}
	}
	return modules
}

func TestGenerateWithABrokenFileReportsTheFileNotAMissingName(t *testing.T) {
	e := newEnv(t)
	wd := t.TempDir()
	file := writeNamed(t, "broken.json", `{"schema":2,"stacks":[`)
	code, _, stderr := executeIn(t, wd, "", e.inputArgs("-f", file)...)
	if code == exitOK {
		t.Fatalf("exit %d, want a refusal: %s", code, stderr)
	}
	if strings.Contains(stderr, "NAME_REQUIRED") {
		t.Errorf("a file that fails to parse was reported as a missing name: %s", stderr)
	}
	if !strings.Contains(stderr, "broken.json") {
		t.Errorf("stderr does not name the broken file: %s", stderr)
	}
	entries, err := os.ReadDir(wd)
	if err != nil || len(entries) != 0 {
		t.Fatalf("the working directory holds %v (%v)", entries, err)
	}
}

func TestGenerateWithAFileThatNamesNoProjectAndNoFolderIsRefused(t *testing.T) {
	e := newEnv(t)
	wd := t.TempDir()
	file := writeNamed(t, "starter.json", `{"schema":2,"stacks":["go-srv"]}`)
	code, _, stderr := executeIn(t, wd, "", e.inputArgs("-f", file)...)
	if code != exitIssues {
		t.Fatalf("exit %d, want %d: %s", code, exitIssues, stderr)
	}
	for _, want := range []string{"error: --name: no project name", "[NAME_REQUIRED]", "  hint: name the project: initializer generate <name>", "1 problem; nothing was written.\n"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q: %s", want, stderr)
		}
	}
	entries, err := os.ReadDir(wd)
	if err != nil || len(entries) != 0 {
		t.Fatalf("the working directory holds %v (%v)", entries, err)
	}
}

// lockedStacks returns the stacks of the project's components.
func lockedStacks(t *testing.T, dir string) []string {
	t.Helper()
	var stacks []string
	list, _ := projectLock(t, dir)["components"].([]any)
	for _, raw := range list {
		entry, _ := raw.(map[string]any)
		stacks = append(stacks, entry["stack"].(string))
	}
	return stacks
}

func TestGenerateRecordsTheNameFromTheFolderAndNoOtherDefault(t *testing.T) {
	e := newEnv(t)
	wd := t.TempDir()
	if code, _, stderr := executeIn(t, wd, "", e.inputArgs("acme", "--modules", "parameters")...); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	recorded := readJSONFile(t, filepath.Join(wd, "acme", "initializer.selection.json"))
	project, _ := recorded["project"].(map[string]any)
	if project["name"] != "acme" {
		t.Errorf("the name taken from the folder is not recorded: %v", recorded)
	}
	if _, hasModule := project["goModule"]; hasModule || recorded["stacks"] != nil {
		t.Errorf("a defaulted Go module and stack list are not recorded: %v", recorded)
	}
}

func TestReplayReDerivesADefaultedGoModuleAndKeepsAGivenOne(t *testing.T) {
	e := newEnv(t)
	cases := map[string]struct {
		extra []string
		want  string
	}{
		"defaulted": {nil, "example.com/other"},
		"given":     {[]string{"--go-module", "example.com/keep"}, "example.com/keep"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			wd := t.TempDir()
			first := append([]string{"first", "--name", "acme", "--stacks", "go-srv", "--modules", "parameters"}, tc.extra...)
			if code, _, stderr := executeIn(t, wd, "", e.inputArgs(first...)...); code != exitOK {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			recorded := filepath.Join(wd, "first", "initializer.selection.json")
			if code, _, stderr := executeIn(t, wd, "", e.inputArgs("-f", recorded, "--name", "other", "second")...); code != exitOK {
				t.Fatalf("replay exit %d: %s", code, stderr)
			}
			if got := goModuleOf(t, filepath.Join(wd, "second")); got != tc.want {
				t.Fatalf("go module = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPrecedenceFlagOverFileOverDefault(t *testing.T) {
	e := newEnv(t)
	file := writeSelection(t, validSelection)
	cases := []struct {
		name  string
		extra []string
		check func(t *testing.T, dir string)
	}{
		{"the file alone", nil, func(t *testing.T, dir string) {
			if lockedName(t, dir) != "acme-backend" || goModuleOf(t, dir) != "example.com/acme/backend" ||
				strings.Join(lockedModules(t, dir, "go-srv"), ",") != "core,identity,parameters" {
				t.Errorf("name %q module %q modules %v", lockedName(t, dir), goModuleOf(t, dir), lockedModules(t, dir, "go-srv"))
			}
		}},
		{"--name replaces the file's name", []string{"--name", "other"}, func(t *testing.T, dir string) {
			if lockedName(t, dir) != "other" || goModuleOf(t, dir) != "example.com/acme/backend" {
				t.Errorf("name %q module %q", lockedName(t, dir), goModuleOf(t, dir))
			}
		}},
		{"--go-module replaces the file's module", []string{"--go-module", "example.com/other"}, func(t *testing.T, dir string) {
			if goModuleOf(t, dir) != "example.com/other" || lockedName(t, dir) != "acme-backend" {
				t.Errorf("name %q module %q", lockedName(t, dir), goModuleOf(t, dir))
			}
		}},
		{"--modules replaces the list", []string{"--modules", "identity"}, func(t *testing.T, dir string) {
			if got := strings.Join(lockedModules(t, dir, "go-srv"), ","); got != "core,identity" {
				t.Errorf("modules = %s", got)
			}
		}},
		{"--modules= gives core only", []string{"--modules="}, func(t *testing.T, dir string) {
			if got := strings.Join(lockedModules(t, dir, "go-srv"), ","); got != "core" {
				t.Errorf("modules = %s", got)
			}
		}},
		{"--all-modules replaces the list", []string{"--all-modules"}, func(t *testing.T, dir string) {
			got := lockedModules(t, dir, "go-srv")
			sort.Strings(got)
			if joined := strings.Join(got, ","); joined != "api-keys,core,feature-flags,identity,parameters,tenancy" {
				t.Errorf("modules = %v", got)
			}
		}},
		{"--stacks replaces the list", []string{"--stacks", "go-srv,ng-web"}, func(t *testing.T, dir string) {
			if got := strings.Join(lockedStacks(t, dir), ","); got != "go-srv,ng-web" {
				t.Errorf("stacks = %s", got)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wd := t.TempDir()
			code, _, stderr := executeIn(t, wd, "", e.inputArgs(append([]string{"-f", file, "project"}, tc.extra...)...)...)
			if code != exitOK {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			tc.check(t, filepath.Join(wd, "project"))
		})
	}
}

func TestGenerateWithNoModuleFlagIsCoreOnly(t *testing.T) {
	e := newEnv(t)
	wd := t.TempDir()
	code, _, stderr := executeIn(t, wd, "", e.inputArgs("acme", "--name", "acme-backend")...)
	if code != exitOK {
		t.Fatalf("generate with no module flag is a core-only project: exit %d: %s", code, stderr)
	}
	if got := strings.Join(lockedModules(t, filepath.Join(wd, "acme"), "go-srv"), ","); got != "core" {
		t.Fatalf("modules = %s", got)
	}
}

func TestAllModulesWithModulesIsAUsageError(t *testing.T) {
	e := newEnv(t)
	code, _, stderr := executeIn(t, t.TempDir(), "", e.inputArgs("--all-modules", "--modules", "identity")...)
	if code != exitUsage || !strings.Contains(stderr, "--all-modules: cannot be combined with --modules") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

func TestASchema1FileWithoutGoModuleGetsTheDefault(t *testing.T) {
	e := newEnv(t)
	wd := t.TempDir()
	file := writeSelection(t, `{"schema":1,"project":{"name":"acme-backend"},"stacks":["go-srv"],"modules":[]}`)
	code, _, stderr := executeIn(t, wd, "", e.inputArgs("-f", file)...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got := goModuleOf(t, filepath.Join(wd, "acme-backend")); got != "example.com/acme-backend" {
		t.Fatalf("go module = %q", got)
	}
}

func TestInputFormatIsDecidedByTheExtension(t *testing.T) {
	e := newEnv(t)
	for _, name := range []string{"acme.txt", "acme", "acme.yaml.bak"} {
		t.Run(name, func(t *testing.T) {
			path := writeNamed(t, name, validSelection)
			code, stdout, stderr := executeIn(t, t.TempDir(), "", e.inputArgs("-f", path, "--format", "json")...)
			if code != exitUsage || stdout != "" {
				t.Fatalf("exit %d stdout %q", code, stdout)
			}
			var doc struct {
				Problems []Problem
				Issues   []map[string]any
			}
			decodeDocument(t, stderr, &doc)
			if len(doc.Problems) != 1 || doc.Problems[0].Code != "INPUT_FORMAT_UNKNOWN" || doc.Problems[0].Hint != "use .yaml, .yml or .json" {
				t.Fatalf("problems = %+v", doc.Problems)
			}
		})
	}
	t.Run("the alias is checked too", func(t *testing.T) {
		code, _, stderr := executeIn(t, t.TempDir(), "", e.inputArgs("-f", "acme.txt")...)
		if code != exitUsage || !strings.Contains(stderr, "[INPUT_FORMAT_UNKNOWN]") || !strings.Contains(stderr, "hint: use .yaml, .yml or .json") {
			t.Fatalf("exit %d: %s", code, stderr)
		}
	})
	t.Run("an upper-case extension is JSON", func(t *testing.T) {
		path := writeNamed(t, "ACME.JSON", validSelection)
		if code, _, stderr := executeIn(t, t.TempDir(), "", e.inputArgs("-f", path)...); code != exitOK {
			t.Fatalf("exit %d: %s", code, stderr)
		}
	})
}

func TestACommandLineProblemCarriesTheValueProblemsOfTheSameRun(t *testing.T) {
	e := newEnv(t)
	code, stdout, stderr := executeIn(t, t.TempDir(), "", e.inputArgs("-f", "acme.txt", "--name", "A", "--format", "json")...)
	if code != exitUsage || stdout != "" {
		t.Fatalf("exit %d stdout %q", code, stdout)
	}
	var doc struct {
		Problems []Problem
		Issues   []map[string]string
	}
	decodeDocument(t, stderr, &doc)
	if len(doc.Problems) != 1 || len(doc.Issues) != 1 || doc.Issues[0]["path"] != "flags#/project/name" {
		t.Fatalf("document = %s", stderr)
	}
	code, _, text := executeIn(t, t.TempDir(), "", e.inputArgs("-f", "acme.txt", "--name", "A")...)
	if code != exitUsage || !strings.Contains(text, "error: --name: ") || !strings.Contains(text, "2 problems.") {
		t.Fatalf("exit %d: %s", code, text)
	}
}

func TestStdinIsReadAsJSON(t *testing.T) {
	e := newEnv(t)
	wd := t.TempDir()
	code, _, stderr := executeIn(t, wd, validSelection, e.inputArgs("-f", "-")...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if lockedName(t, filepath.Join(wd, "acme-backend")) != "acme-backend" {
		t.Fatalf("stdin selection not used")
	}

	code, stdout, _ := executeIn(t, t.TempDir(), `{"schema":1,"project":{"name":"A"}}`, e.inputArgs("-f", "-", "--format", "json")...)
	paths := map[string]bool{}
	for _, found := range issueArray(t, stdout) {
		paths[found["path"]] = true
	}
	if code != exitIssues || !paths["stdin#/project/name"] || !paths["stdin#/stacks"] || !paths["stdin#/modules"] {
		t.Fatalf("exit %d paths %v", code, paths)
	}
}

func TestStdinBoundsAndFailures(t *testing.T) {
	e := newEnv(t)
	cases := map[string]string{
		"over the size limit": strings.Repeat(" ", maxInputBytes+1),
	}
	for name, stdin := range cases {
		t.Run(name, func(t *testing.T) {
			code, stdout, stderr := executeIn(t, t.TempDir(), stdin, e.inputArgs("-f", "-", "--format", "json")...)
			if code != exitInternal || stdout != "" || !strings.Contains(stderr, "INPUT_UNREADABLE") {
				t.Fatalf("exit %d stdout %q stderr %s", code, stdout, stderr)
			}
		})
	}
	t.Run("empty stdin is an input problem", func(t *testing.T) {
		code, stdout, _ := executeIn(t, t.TempDir(), "", e.inputArgs("-f", "-", "--format", "json")...)
		if code != exitIssues || len(issueArray(t, stdout)) != 1 {
			t.Fatalf("exit %d stdout %s", code, stdout)
		}
	})
	t.Run("deeply nested input is refused, not followed", func(t *testing.T) {
		code, stdout, _ := executeIn(t, t.TempDir(), strings.Repeat("[", 50000), e.inputArgs("-f", "-", "--format", "json")...)
		if code != exitIssues || len(issueArray(t, stdout)) == 0 {
			t.Fatalf("exit %d stdout %s", code, stdout)
		}
	})
}

func TestEveryProblemIsReportedWithItsOrigin(t *testing.T) {
	e := newEnv(t)
	file := writeNamed(t, "acme.json", `{
  "schema": 1,
  "project": {"name": "Acme", "goModule": "x"},
  "stacks": ["go-srv", "rust-srv"],
  "modules": ["Bad"],
  "extra": 1
}`)
	slashed := filepath.ToSlash(file)
	code, stdout, _ := executeIn(t, t.TempDir(), "", e.inputArgs("-f", file, "--component", "go-srv=Web_", "--format", "json")...)
	if code != exitIssues {
		t.Fatalf("exit %d", code)
	}
	got := map[string]bool{}
	for _, found := range issueArray(t, stdout) {
		got[found["path"]] = true
	}
	for _, want := range []string{
		slashed + "#/extra", slashed + "#/project/name", slashed + "#/project/goModule",
		slashed + "#/stacks/1", slashed + "#/modules/0", "flags#/components/0",
	} {
		if !got[want] {
			t.Errorf("missing an issue at %s; got %v", want, got)
		}
	}
}

func TestFileProblemsRenderAsFileAndPointer(t *testing.T) {
	e := newEnv(t)
	file := writeNamed(t, "acme.json", `{"schema":2,"stacks":["rust-srv"]}`)
	_, _, stderr := executeIn(t, t.TempDir(), "", e.inputArgs("-f", file)...)
	want := "error: " + filepath.ToSlash(file) + " /stacks/0: stack is not in the stack table [SELECTION_INVALID]"
	if !strings.Contains(stderr, want) {
		t.Fatalf("stderr = %s, want %s", stderr, want)
	}
}

func TestComponentMergeAmbiguousNamesTheCloseMatch(t *testing.T) {
	e := newEnv(t)
	file := writeNamed(t, "acme.json", `{"schema":2,"project":{"name":"acme-file"},"stacks":["go-srv"],"components":[{"stack":"go-srv","name":"acme-catalog"}]}`)
	code, stdout, _ := executeIn(t, t.TempDir(), "", e.inputArgs("-f", file, "--component", "go-srv=acme-catlog", "--format", "json")...)
	found := issueArray(t, stdout)
	if code != exitIssues || len(found) != 1 {
		t.Fatalf("exit %d issues %v", code, found)
	}
	wantMessage := "--component go-srv=acme-catlog names no go-srv component of " + filepath.ToSlash(file) + " (acme-catalog)"
	if found[0]["code"] != "COMPONENT_MERGE_AMBIGUOUS" || found[0]["path"] != "flags#/components/0" || found[0]["message"] != wantMessage ||
		!strings.HasPrefix(found[0]["hint"], `did you mean "acme-catalog"?`) {
		t.Fatalf("issue = %v", found[0])
	}

	wd := t.TempDir()
	code, _, stderr := executeIn(t, wd, "", e.inputArgs("-f", file, "--component", "go-srv=acme-catalog", "--name", "acme")...)
	if code != exitOK {
		t.Fatalf("a flag naming the file's entry merges: exit %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "note: --name changes derived names only; components named in "+filepath.ToSlash(file)+" keep their names") {
		t.Fatalf("missing the --name note: %s", stderr)
	}
}

func TestComponentFlagsDefineTheEntriesOfAStackTheFileLeavesUnnamed(t *testing.T) {
	e := newEnv(t)
	wd := t.TempDir()
	file := writeNamed(t, "acme.json", `{"schema":2,"project":{"name":"acme"},"stacks":["go-srv","ng-web"],"modules":[]}`)
	code, _, stderr := executeIn(t, wd, "", e.inputArgs("-f", file, "--component", "go-srv=acme-srv")...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	recorded := readJSONFile(t, filepath.Join(wd, "acme", "initializer.selection.json"))
	components, _ := recorded["components"].([]any)
	if len(components) != 1 {
		t.Fatalf("recorded components = %v", recorded["components"])
	}
	if _, err := os.Stat(filepath.Join(wd, "acme", "acme-srv", "go.mod")); err != nil {
		t.Fatalf("the go-srv component is not in acme-srv/: %v", err)
	}
}

func TestFolderNameFromADotMatchesTheWorkingDirectory(t *testing.T) {
	e := newEnv(t)
	wd := filepath.Join(t.TempDir(), "shop")
	if err := os.Mkdir(wd, 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := executeIn(t, wd, "", e.inputArgs(".")...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if lockedName(t, wd) != "shop" {
		t.Fatalf("name = %q", lockedName(t, wd))
	}
}

func TestGrammarAndResolverProblemsAreReportedInOneRun(t *testing.T) {
	e := newEnv(t)
	file := writeNamed(t, "acme.json", `{"schema":2,"project":{"name":"Acme"},"stacks":["go-srv"]}`)
	code, stdout, _ := executeIn(t, t.TempDir(), "", e.inputArgs("-f", file, "--component", "go-srv=con", "--format", "json")...)
	if code != exitIssues {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	codes := map[string]bool{}
	for _, found := range issueArray(t, stdout) {
		codes[found["code"]] = true
	}
	for _, want := range []string{"NAME_INVALID", "NAME_RESERVED"} {
		if !codes[want] {
			t.Errorf("missing %s; got %v", want, codes)
		}
	}
}
