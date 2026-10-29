package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const overviewText = `initializer creates projects from the module catalog.

Usage:
  initializer <command> [flags]

Commands:
  generate [DIR]   Create a project in DIR (default ./<name>)
  verify [DIR]     Compare a generated project with its initializer.lock.json
  module ls        List the catalog's modules
  stack ls         List the catalog's stacks
  catalog build    Build catalog.lock.json from a checkout (CI, module authors)
  version          Print the generator version, build commit and source checkout
  help [COMMAND]   Show help for a command

Run 'initializer help <command>' for details.
`

const generateHelpText = `Usage:
  initializer generate [DIR] [flags]

Create a project in DIR from the module catalog. DIR is relative to the
working directory and must be absent or empty. Values come from flags, then
from the input file (-f), then from defaults.

Flags:
  -f, --file PATH
        Input file: .yaml, .yml or .json; - reads stdin (default: none)
  --name NAME
        Project name (default: the folder's name, else "demo")
  --display-name TEXT
        Brand text of the web console (default: the project name)
  --go-module PATH
        Go module path (default: example.com/<name> when a stack needs one)
  --stacks ID[,ID...]
        Stacks to generate; replaces the file's list (default: the catalog's backend stack)
  --modules ID[,ID...]
        Modules to install; replaces the file's list; required modules are added (default: none)
  --all-modules
        Install every module of the backend stack (default: false)
  --component STACK=NAME
        Name the folder of STACK's component (repeatable) (default: <name>-<suffix>)
  --db-name NAME
        Database name (default: <name> with - replaced by _)
  --db-user NAME
        Database user (default: <name> with - replaced by _)
  --service KEY=NAME
        Rename one compose service, repeatable (default: <name>-<key>)
  --port-band NNN
        Host port band 1NNxx, from 100 to 654 (default: 101)
  --dry-run
        Print what would be generated and write nothing (default: false)
  --root DIR
        Source checkout (default: INITIALIZER_ROOT, else the clone above the working directory, else the clone above this binary)
  --catalog FILE
        Catalog file to use instead of building one from the checkout
  --format text|json
        Result format (default: text)
  --log json
        Write structured log events to stderr (default: off)

Examples:
  # a backend with identity, in ./acme
  initializer generate acme --modules identity
  # backend and web console with every module
  initializer generate acme --stacks go-srv,ng-web --all-modules
  # from a file, overriding one value
  initializer generate -f acme.yaml --name acme-shop
  # set the brand text the web console shows
  initializer generate acme --stacks go-srv,ng-web --display-name "Acme Shop"
  # run a second project beside the first on another port band
  initializer generate acme --port-band 103
  # see the resolved values without writing anything
  initializer generate acme --modules tenancy --dry-run
`

// linesWithPrefix returns the lines of text that start with prefix.
func linesWithPrefix(text, prefix string) []string {
	var found []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			found = append(found, line)
		}
	}
	return found
}

func TestBareInvocationPrintsTheOverviewOnStderrAndExitsTwo(t *testing.T) {
	code, stdout, stderr := execute(t)
	if code != exitUsage || stdout != "" || stderr != overviewText {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestOverviewOnRequestGoesToStdoutAndExitsZero(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, stdout, stderr := execute(t, args...)
			if code != exitOK || stdout != overviewText || stderr != "" {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
			}
		})
	}
}

func TestEveryCommandHelpIsTheSameFromEverySpelling(t *testing.T) {
	commands := [][]string{{"generate"}, {"verify"}, {"module", "ls"}, {"stack", "ls"}, {"catalog", "build"}, {"version"}, {"help"}}
	for _, words := range commands {
		name := strings.Join(words, " ")
		t.Run(name, func(t *testing.T) {
			_, want, _ := execute(t, append([]string{"help"}, words...)...)
			if want == "" || !strings.Contains(want, "Usage:\n  initializer "+words[0]) {
				t.Fatalf("help %s printed %q", name, want)
			}
			spellings := [][]string{append(append([]string{}, words...), "--help"), append(append([]string{}, words...), "-h")}
			for _, args := range spellings {
				code, stdout, stderr := execute(t, args...)
				if code != exitOK || stdout != want || stderr != "" {
					t.Fatalf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
				}
			}
		})
	}
}

func TestGenerateHelpListsEveryFlagAndNoHiddenAlias(t *testing.T) {
	code, stdout, stderr := execute(t, "help", "generate")
	if code != exitOK || stderr != "" || stdout != generateHelpText {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s", code, stderr, stdout)
	}
	for _, hidden := range []string{"--out", "--selection", "--project", "--help"} {
		if strings.Contains(stdout, hidden) {
			t.Errorf("help lists the hidden flag %s", hidden)
		}
	}
}

func TestOtherCommandHelpListsItsFlags(t *testing.T) {
	cases := map[string][]string{
		"verify":        {"verify"},
		"catalog build": {"catalog", "build"},
	}
	wants := map[string][]string{
		"verify": {"initializer verify [DIR] [flags]", "# in ./acme", "initializer verify acme"},
		"catalog build": {"--root DIR", "--files PATH", "--out PATH", "(required)", "initializer catalog build --out catalog.lock.json",
			"initializer catalog build --root . --files files.bin --out catalog.lock.json"},
	}
	for name, words := range cases {
		t.Run(name, func(t *testing.T) {
			_, stdout, _ := execute(t, append([]string{"help"}, words...)...)
			for _, want := range wants[name] {
				if !strings.Contains(stdout, want) {
					t.Errorf("help %s misses %q:\n%s", name, want, stdout)
				}
			}
			if strings.Contains(stdout, "--project") {
				t.Errorf("help lists the hidden alias --project")
			}
		})
	}
}

func TestGroupHelpListsItsCommands(t *testing.T) {
	for _, args := range [][]string{{"catalog", "--help"}, {"help", "catalog"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, stdout, stderr := execute(t, args...)
			if code != exitOK || stderr != "" {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
			if !strings.Contains(stdout, "catalog build   Build catalog.lock.json from a checkout") {
				t.Fatalf("group help misses the build row:\n%s", stdout)
			}
		})
	}
}

func TestGroupWithoutAVerbExitsTwoNamingTheVerb(t *testing.T) {
	code, stdout, stderr := execute(t, "catalog")
	if code != exitUsage || stdout != "" {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
	if !strings.Contains(stderr, "error: catalog needs a command: build") || !strings.Contains(stderr, "catalog build   Build catalog.lock.json") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestUnknownCommandsExitTwoWithAClosestMatch(t *testing.T) {
	cases := map[string]struct {
		args []string
		want string
	}{
		"top level":    {[]string{"genrate"}, `error: unknown command "genrate"` + "\n" + `  hint: did you mean "generate"?`},
		"no match":     {[]string{"nosuch"}, `error: unknown command "nosuch"`},
		"through help": {[]string{"help", "nosuch"}, `error: unknown command "nosuch"`},
		"a verb":       {[]string{"catalog", "bild"}, `hint: did you mean "catalog build"?`},
		"a flag":       {[]string{"--bogus"}, `error: unknown command "--bogus"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			code, stdout, stderr := execute(t, tc.args...)
			if code != exitUsage || stdout != "" || !strings.Contains(stderr, tc.want) {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
			}
		})
	}
	_, _, stderr := execute(t, "nosuch")
	if strings.Contains(stderr, "hint:") {
		t.Errorf("no hint expected when nothing is close: %s", stderr)
	}
}

func TestOneInvocationReportsEveryCommandLineProblem(t *testing.T) {
	code, stdout, stderr := executeLogged(t, "generate", "acme", "extra", "--bogus", "--name", "a", "--name", "b", "--out", "o", "--modules")
	if code != exitUsage || stdout != "" {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
	wants := []string{
		"error: --bogus: unknown flag",
		"error: extra: unexpected argument",
		"error: --name: is given twice",
		"error: --modules: needs a value",
		"error: --out: pass the folder once",
	}
	for _, want := range wants {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr misses %q:\n%s", want, stderr)
		}
	}
	if !strings.Contains(stderr, "6 problems.") {
		t.Errorf("want the count of 6 problems (the five above and the invalid name):\n%s", stderr)
	}
	if event := usageEvent(t, stderr); event["problem_count"] != float64(5) {
		t.Errorf("usage_error event = %v", event)
	}
}

// usageEvent returns the usage_error log record on stderr.
func usageEvent(t *testing.T, stderr string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("stderr line is not JSON: %q", line)
		}
		if record["msg"] == "usage_error" {
			return record
		}
	}
	t.Fatalf("no usage_error event on stderr: %s", stderr)
	return nil
}

func TestMistypedFlagSuggestsTheRealOne(t *testing.T) {
	_, _, stderr := execute(t, "generate", "--nmae", "x")
	if !strings.Contains(stderr, "error: --nmae: unknown flag\n  hint: did you mean \"--name\"?") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestExtraArgumentsAreRefusedByCommandsWithoutPositionals(t *testing.T) {
	code, _, stderr := execute(t, "catalog", "build", "stray", "--root", "r", "--files", "f", "--out", "o")
	if code != exitUsage || !strings.Contains(stderr, "error: stray: unexpected argument") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

func TestHelpFlagWinsOverOtherProblems(t *testing.T) {
	code, stdout, _ := execute(t, "generate", "--bogus", "--help")
	if code != exitOK || stdout != generateHelpText {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
}

// inFreshDir moves the test into an empty temporary directory.
func inFreshDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	return dir
}

func (e env) flagSelection(extra ...string) []string {
	base := []string{"--root", e.root, "--catalog", e.catalog, "--go-module", "example.com/acme", "--stacks", "go-srv", "--modules", "parameters"}
	return append(base, extra...)
}

// recordedName returns the effective project name the lock holds; the
// recorded selection carries a name only when the input gave one.
func recordedName(t *testing.T, project string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(project, "initializer.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Names map[string]string `json:"names"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatal(err)
	}
	name, found := lock.Names["project"]
	if !found {
		t.Fatalf("no names.project in %s", data)
	}
	return name
}

func TestGenerateTakesTheFolderAsAnArgumentAndNamesTheProjectAfterIt(t *testing.T) {
	e := newEnv(t)
	dir := inFreshDir(t)
	code, stdout, stderr := execute(t, append([]string{"generate", "acme"}, e.flagSelection()...)...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if recordedName(t, filepath.Join(dir, "acme")) != "acme" {
		t.Fatalf("the project must be named after its folder")
	}
	if !strings.Contains(stdout, "Project acme generated in ./acme (") {
		t.Fatalf("stdout = %s", stdout)
	}
	notes := linesWithPrefix(stderr, "note:")
	if len(notes) != 1 || notes[0] != `note: project name "acme" comes from the folder; pass --name to set it` {
		t.Fatalf("notes = %q", notes)
	}
}

func TestGenerateWithoutAFolderUsesTheNameAndSaysSo(t *testing.T) {
	e := newEnv(t)
	dir := inFreshDir(t)
	code, _, stderr := execute(t, append([]string{"generate", "--name", "acme-backend"}, e.flagSelection()...)...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if recordedName(t, filepath.Join(dir, "acme-backend")) != "acme-backend" {
		t.Fatalf("./<name> must hold the project")
	}
	if notes := linesWithPrefix(stderr, "note:"); len(notes) != 0 {
		t.Fatalf("a given name needs no note: %q", notes)
	}
}

func TestGenerateWithNeitherNameNorFolderFallsBackToDemo(t *testing.T) {
	e := newEnv(t)
	dir := inFreshDir(t)
	code, _, stderr := execute(t, append([]string{"generate"}, e.flagSelection()...)...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if recordedName(t, filepath.Join(dir, "demo")) != "demo" {
		t.Fatalf("the fallback project must be ./demo")
	}
	if notes := linesWithPrefix(stderr, "note:"); len(notes) != 1 || notes[0] != `note: no name given; using "demo"` {
		t.Fatalf("notes = %q", notes)
	}
}

func TestGenerateNameFlagBeatsTheFolderName(t *testing.T) {
	e := newEnv(t)
	dir := inFreshDir(t)
	code, _, stderr := execute(t, append([]string{"generate", "folder", "--name", "acme"}, e.flagSelection()...)...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if recordedName(t, filepath.Join(dir, "folder")) != "acme" {
		t.Fatalf("--name must win")
	}
}

func TestGenerateWithAFileWritesToTheProjectNameFolder(t *testing.T) {
	e := newEnv(t)
	dir := inFreshDir(t)
	code, _, stderr := execute(t, "generate", "--root", e.root, "--catalog", e.catalog, "-f", writeSelection(t, validSelection))
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if recordedName(t, filepath.Join(dir, "acme-backend")) != "acme-backend" {
		t.Fatalf("a file's project name must name the folder")
	}
}

func TestGenerateIntoTheWorkingDirectoryNamesTheProjectAfterIt(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(inFreshDir(t), "acme")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	code, _, stderr := execute(t, append([]string{"generate", "."}, e.flagSelection()...)...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if recordedName(t, dir) != "acme" {
		t.Fatalf("the working directory's name must name the project")
	}
}

func TestGenerateIntoANonEmptyFolderIsRefusedWithAHint(t *testing.T) {
	e := newEnv(t)
	dir := inFreshDir(t)
	holder := filepath.Join(dir, "acme")
	if err := os.Mkdir(holder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(holder, "keep.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := execute(t, append([]string{"generate", "acme"}, e.flagSelection()...)...)
	stderr = withoutSources(stderr)
	wantText := "note: project name \"acme\" comes from the folder; pass --name to set it\n" +
		"error: ./acme: output directory is not empty [OUTPUT_NOT_EMPTY]\n" +
		"  hint: choose another folder: initializer generate <dir>\n" +
		"1 problem; nothing was written.\n"
	if code != exitIssues || stdout != "" || stderr != wantText {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	code, stdout, _ = executeJSON(t, append([]string{"generate", "acme"}, e.flagSelection()...)...)
	if code != exitIssues {
		t.Fatalf("exit %d", code)
	}
	issues := issueArray(t, stdout)
	if len(issues) != 1 || issues[0]["code"] != "OUTPUT_NOT_EMPTY" || issues[0]["hint"] != "choose another folder: initializer generate <dir>" {
		t.Fatalf("issues = %v", issues)
	}
	if got := tree(t, holder); len(got) != 1 || got["keep.txt"] != "mine" {
		t.Fatalf("the folder changed: %v", got)
	}
}

func TestGenerateRefusesAFolderNameThatIsNotAProjectName(t *testing.T) {
	e := newEnv(t)
	dir := inFreshDir(t)
	code, stdout, _ := executeJSON(t, append([]string{"generate", "My App"}, e.flagSelection()...)...)
	if code != exitIssues {
		t.Fatalf("exit %d", code)
	}
	issues := issueArray(t, stdout)
	want := `name "My App" comes from the folder "My App" and is not a project name`
	if len(issues) != 1 || issues[0]["code"] != "NAME_INVALID" || issues[0]["path"] != "args#/dir" || issues[0]["message"] != want {
		t.Fatalf("issues = %v", issues)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("nothing may be written: %v", entries)
	}
}

func TestFolderNameIssueKeepsTheOtherSelectionIssues(t *testing.T) {
	e := newEnv(t)
	inFreshDir(t)
	code, stdout, _ := executeJSON(t, "generate", "My App", "--root", e.root, "--catalog", e.catalog, "--stacks", "go-srv", "--modules", "nope")
	if code != exitIssues {
		t.Fatalf("exit %d", code)
	}
	paths := map[string]bool{}
	for _, found := range issueArray(t, stdout) {
		paths[found["path"]] = true
	}
	if !paths["args#/dir"] || !paths["flags#/modules/0"] {
		t.Fatalf("want the folder and the module issues, got %v", paths)
	}
}

func TestInterleavedFlagsEqualsFormsAndDoubleDash(t *testing.T) {
	cases := map[string]func(e env) []string{
		"flags before and after the folder": func(e env) []string {
			return []string{"generate", "--root", e.root, "acme", "--catalog", e.catalog, "--go-module", "example.com/acme", "--stacks", "go-srv", "--modules", "parameters"}
		},
		"equals forms": func(e env) []string {
			return []string{"generate", "acme", "--root=" + e.root, "--catalog=" + e.catalog, "--go-module=example.com/acme", "--stacks=go-srv", "--modules=parameters"}
		},
		"single dash": func(e env) []string {
			return []string{"generate", "-root", e.root, "-catalog", e.catalog, "-go-module", "example.com/acme", "-stacks", "go-srv", "-all-modules", "acme"}
		},
		"double dash ends the flags": func(e env) []string {
			return append(append([]string{"generate"}, e.flagSelection()...), "--", "acme")
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			dir := inFreshDir(t)
			code, _, stderr := execute(t, build(e)...)
			if code != exitOK {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if _, err := os.Stat(filepath.Join(dir, "acme", "initializer.lock.json")); err != nil {
				t.Fatalf("the project is not in ./acme: %v", err)
			}
		})
	}
}

func TestADashedFolderAfterDoubleDashIsAFolderNotAFlag(t *testing.T) {
	e := newEnv(t)
	dir := inFreshDir(t)
	code, _, stderr := execute(t, append(append([]string{"generate", "--name", "acme"}, e.flagSelection()...), "--", "--odd")...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "--odd", "initializer.lock.json")); err != nil {
		t.Fatalf("the project is not in ./--odd: %v", err)
	}
}

func TestDeprecatedSpellingsWarnOnceAndStillWork(t *testing.T) {
	e := newEnv(t)
	sel := writeSelection(t, validSelection)

	code, _, stderr := execute(t, "generate", "--root", e.root, "--catalog", e.catalog, "--out", e.out, "--selection", sel)
	if code != exitOK {
		t.Fatalf("generate: exit %d: %s", code, stderr)
	}
	warnings := linesWithPrefix(stderr, "warning:")
	want := []string{
		"warning: --out is deprecated; pass the folder as an argument: initializer generate <dir>",
		"warning: --selection is deprecated; use -f <file>",
	}
	if !slices.Equal(warnings, want) {
		t.Fatalf("warnings = %q", warnings)
	}

	code, _, stderr = execute(t, "verify", "--project", e.out)
	if code != exitOK {
		t.Fatalf("verify: exit %d: %s", code, stderr)
	}
	warnings = linesWithPrefix(stderr, "warning:")
	if len(warnings) != 1 || warnings[0] != "warning: --project is deprecated; pass the folder as an argument: initializer verify <dir>" {
		t.Fatalf("warnings = %q", warnings)
	}
}

func TestCurrentSpellingsPrintNoWarning(t *testing.T) {
	e := newEnv(t)
	list := writeFileList(t)
	catalogOut := filepath.Join(t.TempDir(), "catalog.lock.json")
	code, _, stderr := execute(t, "catalog", "build", "--root", filepath.Join(cleanFixture, "repo"), "--files", list, "--out", catalogOut)
	if code != exitOK || strings.Contains(stderr, "warning:") {
		t.Fatalf("catalog build: exit %d: %s", code, stderr)
	}
	code, _, stderr = execute(t, "generate", e.out, "--root", e.root, "--catalog", e.catalog, "--name", "acme-backend",
		"--go-module", "example.com/acme/backend", "--stacks", "go-srv", "--all-modules")
	if code != exitOK || strings.Contains(stderr, "warning:") {
		t.Fatalf("generate: exit %d: %s", code, stderr)
	}
	code, stdout, stderr := execute(t, "verify", e.out)
	if code != exitOK || strings.Contains(stderr, "warning:") || stdout != "No differences.\n" {
		t.Fatalf("verify: exit %d, stdout %s, stderr %s", code, stdout, stderr)
	}
}

func TestAnAliasWithItsNewSpellingIsAUsageError(t *testing.T) {
	e := newEnv(t)
	cases := map[string]struct {
		args []string
		want string
	}{
		"--out and a folder":     {append([]string{"generate", "acme", "--out", "o"}, e.flagSelection()...), "error: --out: pass the folder once"},
		"--selection and -f":     {[]string{"generate", "--root", e.root, "--catalog", e.catalog, "-f", "a.json", "--selection", "b.json"}, "error: --selection: pass the file once"},
		"--project and a folder": {[]string{"verify", "dir", "--project", "dir"}, "error: --project: pass the folder once"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			code, _, stderr := execute(t, tc.args...)
			if code != exitUsage || !strings.Contains(stderr, tc.want) {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if strings.Contains(stderr, "warning:") {
				t.Fatalf("no warning when the line is refused: %s", stderr)
			}
		})
	}
}

func TestAllModulesFalseDoesNotConflictWithModules(t *testing.T) {
	e := newEnv(t)
	inFreshDir(t)
	code, _, stderr := execute(t, append([]string{"generate", "acme", "--all-modules=false"}, e.flagSelection()...)...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}
