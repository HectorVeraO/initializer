package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

// issueDocument is the issues document with the members these tests read.
type issueDocument struct {
	Issues []struct {
		Code           string   `json:"code"`
		Path           string   `json:"path"`
		Message        string   `json:"message"`
		Hint           string   `json:"hint"`
		SuggestedNames []string `json:"suggestedNames"`
	} `json:"issues"`
}

func issuesOf(t *testing.T, stdout string) issueDocument {
	t.Helper()
	var doc issueDocument
	decodeDocument(t, stdout, &doc)
	return doc
}

func (d issueDocument) codes() []string {
	var codes []string
	for _, found := range d.Issues {
		codes = append(codes, found.Code)
	}
	sort.Strings(codes)
	return codes
}

func TestAValidTwoBackendSelectionStopsAtTheGuard(t *testing.T) {
	e := newEnv(t)
	args := e.fullstackArgs("--component", "go-srv=acme-srv-first", "--component", "go-srv=acme-srv-second,modules=parameters",
		"--component", "ng-web=acme-web,backend=acme-srv-second")
	for _, extra := range [][]string{nil, {"--dry-run"}} {
		code, stdout, stderr := executeJSON(t, append(append([]string{}, args...), extra...)...)
		doc := issuesOf(t, stdout)
		if code != exitIssues || !reflect.DeepEqual(doc.codes(), []string{"COMPONENT_MULTI_UNSUPPORTED"}) {
			t.Fatalf("extra %v: exit %d codes %v %s", extra, code, doc.codes(), stderr)
		}
	}
}

func TestComponentChecksAreCollectedInOneRun(t *testing.T) {
	e := newEnv(t)
	code, stdout, _ := executeJSON(t, e.args("--stacks", "go-srv,ng-web", "--modules", "parameters", "--db-name", "shared",
		"--service", "postgres=shared-pg", "--component", "go-srv", "--component", "go-srv", "--component", "ng-web=acme-web")...)
	doc := issuesOf(t, stdout)
	want := []string{"COMPONENT_AMBIGUOUS", "COMPONENT_BACKEND_REQUIRED", "COMPONENT_NAME_REQUIRED", "NAME_OVERRIDE_UNUSED"}
	if code != exitIssues || !reflect.DeepEqual(doc.codes(), want) {
		t.Fatalf("exit %d codes %v, want %v", code, doc.codes(), want)
	}
}

func TestANameRequiredIssueCarriesSuggestedNamesAndAPasteableHint(t *testing.T) {
	e := newEnv(t)
	args := e.args("--stacks", "go-srv,ng-web", "--modules", "parameters", "--component", "go-srv",
		"--component", "go-srv,modules=parameters", "--component", "ng-web=acme-web,backend=acme-srv-second")
	code, stdout, _ := executeJSON(t, args...)
	doc := issuesOf(t, stdout)
	var required []int
	for i, found := range doc.Issues {
		if found.Code == "COMPONENT_NAME_REQUIRED" {
			required = append(required, i)
		}
	}
	if code != exitIssues || len(required) != 1 {
		t.Fatalf("exit %d issues %+v", code, doc.Issues)
	}
	found := doc.Issues[required[0]]
	if len(found.SuggestedNames) != 2 || !strings.HasSuffix(found.SuggestedNames[0], "-srv-first") || !strings.HasSuffix(found.SuggestedNames[1], "-srv-second") {
		t.Fatalf("suggested %v", found.SuggestedNames)
	}
	if found.Path != "flags#/components/0" || found.Message != "stack go-srv has 2 components; name each one" {
		t.Fatalf("issue %+v", found)
	}
	pasted, hasPrefix := strings.CutPrefix(found.Hint, "initializer ")
	if !hasPrefix || !strings.Contains(pasted, "--component go-srv="+found.SuggestedNames[0]+" ") ||
		!strings.Contains(pasted, "--component go-srv="+found.SuggestedNames[1]+",modules=parameters") {
		t.Fatalf("hint %q", found.Hint)
	}
}

func TestThePastedNameHintClearsTheNameRequiredIssue(t *testing.T) {
	e := newEnv(t)
	args := e.args("--stacks", "go-srv,ng-web", "--modules", "parameters", "--component", "go-srv",
		"--component", "go-srv", "--component", "ng-web=acme-web,backend=x")
	_, stdout, _ := executeJSON(t, args...)
	var hint string
	for _, found := range issuesOf(t, stdout).Issues {
		if found.Code == "COMPONENT_NAME_REQUIRED" {
			hint = found.Hint
		}
	}
	if hint == "" {
		t.Fatalf("no hint in %s", stdout)
	}
	pasted := splitQuoted(strings.TrimPrefix(hint, "initializer "))
	// Bind the console to the first backend so only the guard is left.
	firstBackend := ""
	for _, arg := range pasted {
		if name, isBackend := strings.CutPrefix(arg, "go-srv="); isBackend && firstBackend == "" {
			firstBackend = name
		}
	}
	for i, arg := range pasted {
		if arg == "ng-web=acme-web,backend=x" {
			pasted[i] = "ng-web=acme-web,backend=" + firstBackend
		}
	}
	code, stdout, stderr := execute(t, pasted...)
	if stdout == "" {
		t.Fatalf("pasted %v: exit %d stderr %s", pasted, code, stderr)
	}
	if got := issuesOf(t, stdout).codes(); code != exitIssues || !reflect.DeepEqual(got, []string{"COMPONENT_MULTI_UNSUPPORTED"}) {
		t.Fatalf("pasted %v: exit %d codes %v", pasted, code, got)
	}
}

func TestANameRequiredHintForAFileNamesTheLinesToAdd(t *testing.T) {
	e := newEnv(t)
	yamlBody := "schema: 2\nproject:\n  name: acme\n  goModule: example.com/acme\nstacks: [go-srv]\ncomponents:\n  - stack: go-srv\n  - stack: go-srv\n"
	yamlPath := filepath.Join(t.TempDir(), "acme.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlBody), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stdout, _ := executeJSON(t, e.args("-f", yamlPath)...)
	doc := issuesOf(t, stdout)
	if len(doc.Issues) != 1 || doc.Issues[0].Code != "COMPONENT_NAME_REQUIRED" {
		t.Fatalf("issues %+v", doc.Issues)
	}
	hint := doc.Issues[0].Hint
	for _, want := range []string{"components[0]", "components[1]", ":7)", ":8)", "name: acme-srv-first", "name: acme-srv-second"} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint lacks %q: %s", want, hint)
		}
	}

	jsonBody := `{"schema":2,"project":{"name":"acme","goModule":"example.com/acme"},"stacks":["go-srv"],"components":[{"stack":"go-srv"},{"stack":"go-srv","name":"acme-srv-first"}]}`
	_, stdout, _ = executeJSON(t, e.args("-f", writeSelection(t, jsonBody))...)
	doc = issuesOf(t, stdout)
	if len(doc.Issues) != 1 || !strings.Contains(doc.Issues[0].Hint, `"name": "acme-srv-second"`) ||
		!reflect.DeepEqual(doc.Issues[0].SuggestedNames, []string{"acme-srv-second", "acme-srv-first"}) {
		t.Fatalf("issues %+v", doc.Issues)
	}
}

func TestListingAStackTwiceGainsTheComponentHint(t *testing.T) {
	e := newEnv(t)
	code, stdout, _ := executeJSON(t, e.args("--stacks", "go-srv,go-srv")...)
	doc := issuesOf(t, stdout)
	if code != exitIssues || len(doc.Issues) != 1 || doc.Issues[0].Message != "stack is listed twice" ||
		doc.Issues[0].Hint != "to ask for two go-srv components, repeat --component go-srv=NAME" {
		t.Fatalf("exit %d issues %+v", code, doc.Issues)
	}
}

func TestComponentGrammarProblemsExitTwoWithKeySuggestions(t *testing.T) {
	e := newEnv(t)
	code, _, stderr := execute(t, e.args("--stacks", "go-srv",
		"--component", "go-srv=a,modueles=x", "--component", "go-srv=b,modules=x,modules=y",
		"--component", "go-srv=c,db-name=", "--component", "go-srv=d,service.=x", "--component", "go-srv=e,backend")...)
	if code != exitUsage {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{
		`unknown key "modueles"`, `hint: did you mean "modules"?`, `key "modules" is given twice`,
		`key "db-name" needs a value`, `needs a service key after service.`, `"backend" needs KEY=VALUE`, "5 problems.",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
}

func TestTwoFlagsForOneNamedComponentAreRefused(t *testing.T) {
	e := newEnv(t)
	code, _, stderr := execute(t, e.args("--component", "go-srv=acme-api,modules=", "--component", "go-srv=acme-api,db-name=x")...)
	if code != exitUsage || !strings.Contains(stderr, "give every key of acme-api in one --component") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

func TestAnEntryDatabaseWinsOverTheTopLevelOneInASingleProject(t *testing.T) {
	doc := infraDryRunJSON(t, "--name", "acme", "--db-name", "flag_db", "--db-user", "flag_user",
		"--component", "go-srv=acme-srv,db-name=entry_db")
	if doc.Names["database"] != "entry_db" || doc.Names["dbUser"] != "flag_user" {
		t.Fatalf("names %v", doc.Names)
	}
}

func TestAnEntryServiceAndGoModuleWinInASingleProject(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", out, "--root", root, "--catalog", catalogPath, "--name", "acme", "--go-module", "example.com/top",
		"--service", "postgres=top-pg", "--component", "go-srv=acme-srv,go-module=example.com/entry,service.postgres=entry-pg")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	data, err := os.ReadFile(filepath.Join(out, "initializer.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lockfile.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Names["service.postgres"] != "entry-pg" {
		t.Errorf("names %v", lock.Names)
	}
	gomod, err := os.ReadFile(filepath.Join(out, "acme-srv", "go.mod"))
	if err != nil || !strings.HasPrefix(string(gomod), "module example.com/entry\n") {
		t.Errorf("go.mod %q (%v)", gomod, err)
	}
}

func TestASingleBackendEntryListIsTheStartingListAndIsRecordedInTheEntry(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", out, "--root", root, "--catalog", catalogPath, "--name", "acme",
		"--component", "go-srv=acme-srv,modules=parameters")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	data, err := os.ReadFile(filepath.Join(out, "initializer.selection.json"))
	if err != nil {
		t.Fatal(err)
	}
	var recorded manifest.Selection
	decodeDocument(t, string(data), &recorded)
	if recorded.Has.Modules || len(recorded.Modules) != 0 || len(recorded.Components) != 1 || !recorded.Components[0].HasModules {
		t.Fatalf("recorded:\n%s", data)
	}
	if !contains(recorded.Components[0].Modules, "parameters") || !contains(recorded.Components[0].Modules, "core") {
		t.Fatalf("entry list %v", recorded.Components[0].Modules)
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestTopLevelModulesWithAnEntryListAreUnused(t *testing.T) {
	e := newEnv(t)
	code, stdout, _ := executeJSON(t, e.args("--modules", "parameters", "--component", "go-srv=acme-srv,modules=parameters")...)
	doc := issuesOf(t, stdout)
	if code != exitIssues || !reflect.DeepEqual(doc.codes(), []string{"MODULES_UNUSED"}) ||
		doc.Issues[0].Path != "flags#/modules" || doc.Issues[0].Hint != "drop the top-level modules, or remove modules from one component" {
		t.Fatalf("exit %d issues %+v", code, doc.Issues)
	}
	code, stdout, _ = executeJSON(t, e.args("--all-modules", "--component", "go-srv=acme-srv,modules=")...)
	doc = issuesOf(t, stdout)
	if code != exitIssues || len(doc.Issues) != 1 || doc.Issues[0].Path != "flags#/all-modules" {
		t.Fatalf("all-modules: exit %d issues %+v", code, doc.Issues)
	}
}

func TestAnEntryModuleTheCatalogLacksIsReportedAtTheEntry(t *testing.T) {
	e := newEnv(t)
	code, stdout, _ := executeJSON(t, e.args("--component", "go-srv=acme-srv,modules=parameters+nothing")...)
	doc := issuesOf(t, stdout)
	if code != exitIssues || len(doc.Issues) != 1 || doc.Issues[0].Code != "MODULE_UNKNOWN" ||
		doc.Issues[0].Path != "flags#/components/0/modules/1" {
		t.Fatalf("exit %d issues %+v", code, doc.Issues)
	}
}

func TestAFrontendWithModulesIsNotAllowed(t *testing.T) {
	e := newEnv(t)
	code, stdout, _ := executeJSON(t, e.fullstackArgs("--component", "ng-web=acme-web,modules=parameters")...)
	doc := issuesOf(t, stdout)
	if code != exitIssues || len(doc.Issues) != 1 || doc.Issues[0].Code != "COMPONENT_FIELD_NOT_ALLOWED" ||
		doc.Issues[0].Message != "ng-web components take no modules; they install what their backend installs" {
		t.Fatalf("exit %d issues %+v", code, doc.Issues)
	}
}

func TestAFileEntryIsOverriddenByAKeyOfItsFlag(t *testing.T) {
	root, catalogPath := realCatalog(t)
	body := `{"schema":2,"project":{"name":"acme","goModule":"example.com/acme"},"stacks":["go-srv"],"components":[{"stack":"go-srv","name":"acme-srv","database":{"name":"file_db"}}]}`
	out := filepath.Join(t.TempDir(), "out")
	code, stdout, stderr := execute(t, "generate", out, "--root", root, "--catalog", catalogPath, "--dry-run", "--format", "json",
		"-f", writeSelection(t, body), "--component", "go-srv=acme-srv,db-name=flag_db")
	if code != exitOK {
		t.Fatalf("exit %d: %s%s", code, stdout, stderr)
	}
	var doc dryRunDocument
	decodeDocument(t, stdout, &doc)
	if doc.Names["database"] != "flag_db" {
		t.Fatalf("names %v", doc.Names)
	}
}

// splitQuoted splits a pasted command line on spaces, honoring single quotes.
func splitQuoted(line string) []string {
	var words []string
	var current strings.Builder
	inQuote, hasWord := false, false
	for _, char := range line {
		switch {
		case char == '\'':
			inQuote, hasWord = !inQuote, true
		case char == ' ' && !inQuote:
			if hasWord {
				words = append(words, current.String())
			}
			current.Reset()
			hasWord = false
		default:
			current.WriteRune(char)
			hasWord = true
		}
	}
	if hasWord {
		words = append(words, current.String())
	}
	return words
}
