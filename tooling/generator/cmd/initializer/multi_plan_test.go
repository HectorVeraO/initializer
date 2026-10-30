package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/emit"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
)

// equivalentForm returns the schema 2 form of a CI selection: the top-level
// modules move into the backend entry, a listed frontend gets an unnamed entry
// and every other field stays.
func equivalentForm(t *testing.T, data []byte) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	var components []any
	stacks, _ := document["stacks"].([]any)
	for _, stack := range stacks {
		id, _ := stack.(string)
		entry := map[string]any{"stack": id}
		if manifest.IsBackendStack(id) {
			entry["modules"] = document["modules"]
		}
		components = append(components, entry)
	}
	document["schema"] = 2
	document["components"] = components
	delete(document, "modules")
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// lockWithoutTheRecordedInput decodes a lock and blanks what records the input
// as given: the selection and the file that holds it.
func lockWithoutTheRecordedInput(t *testing.T, data string) lockfile.Lock {
	t.Helper()
	lock, err := lockfile.Decode([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	lock.Selection = manifest.Selection{}
	kept := lock.Generated[:0:0]
	for _, file := range lock.Generated {
		if file.Path != lockfile.SelectionName {
			kept = append(kept, file)
		}
	}
	lock.Generated = kept
	return lock
}

// A project that is not multi generates the same bytes whether its modules are
// listed at the top level or on its backend entry.
func TestSingleFormEquivalence(t *testing.T) {
	e := newEnv(t)
	paths, err := filepath.Glob(filepath.Join(realCheckoutRoot(t), "tooling", "generator", "testdata", "ci", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no CI selections: %v", err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(filepath.Base(path), func(t *testing.T) {
			if bytes.Contains(data, []byte(`"components"`)) {
				t.Skip("a multi selection lists its modules on its entries by definition")
			}
			asWritten, asEntry := e, e
			asWritten.out = filepath.Join(t.TempDir(), "written")
			asEntry.out = filepath.Join(t.TempDir(), "entry")
			if code, _, stderr := executeIn(t, t.TempDir(), "", asWritten.args("-f", path)...); code != exitOK {
				t.Fatalf("as written: exit %d: %s", code, stderr)
			}
			twin := writeNamed(t, "twin.json", string(equivalentForm(t, data)))
			if code, _, stderr := executeIn(t, t.TempDir(), "", asEntry.args("-f", twin)...); code != exitOK {
				t.Fatalf("equivalent form: exit %d: %s", code, stderr)
			}
			left, right := tree(t, asWritten.out), tree(t, asEntry.out)
			leftLock, rightLock := left[lockfile.Name], right[lockfile.Name]
			for _, files := range []map[string]string{left, right} {
				delete(files, lockfile.SelectionName)
				delete(files, lockfile.Name)
			}
			if len(left) == 0 || len(left) != len(right) {
				t.Fatalf("file sets differ: %d vs %d", len(left), len(right))
			}
			for name, content := range left {
				if right[name] != content {
					t.Errorf("%s differs", name)
				}
			}
			if !reflect.DeepEqual(lockWithoutTheRecordedInput(t, leftLock), lockWithoutTheRecordedInput(t, rightLock)) {
				t.Error("the locks differ beyond the recorded selection")
			}
		})
	}
}

// multiCatalog is the fixture catalog with name rules for a database and the
// postgres and kratos services.
func multiCatalog(t *testing.T) catalog.Catalog {
	t.Helper()
	cat := fixture.Load(t).Catalog
	for _, slot := range []string{"database", "dbUser", "service.postgres", "service.kratos"} {
		cat.Reference[slot] = "ref-" + slot
	}
	for s := range cat.Stacks {
		for m := range cat.Stacks[s].Modules {
			module := &cat.Stacks[s].Modules[m]
			isBackend := cat.Stacks[s].Role == "backend"
			switch {
			case isBackend && module.Module == "core":
				module.Names = []catalog.NameRule{{Path: "a", Text: "db: %{database} %{dbUser} %{service.postgres}", Blocks: []string{""}}}
			case isBackend && module.Module == "identity":
				module.Names = []catalog.NameRule{{Path: "b", Text: "k: %{service.kratos}", Blocks: []string{"module:identity"}}}
			}
		}
	}
	return cat
}

func multiPlan(t *testing.T) (plan.Plan, manifest.Selection) {
	t.Helper()
	sel := manifest.Selection{
		Schema:  2,
		Project: manifest.Project{Name: "acme", GoModule: "example.com/acme"},
		Stacks:  []string{"go-srv", "ng-web"},
		Components: []manifest.Component{
			{Stack: "go-srv", Name: "acme-srv-first", Modules: []string{"parameters"}, HasModules: true},
			{Stack: "go-srv", Name: "acme-srv-second", Modules: []string{"tenancy"}, HasModules: true},
			{Stack: "ng-web", Name: "acme-web", Backend: "acme-srv-first"},
		},
	}
	resolved, issues := plan.Resolve(multiCatalog(t), sel)
	if len(issues) > 0 {
		t.Fatalf("multi plan: %+v", issues)
	}
	return resolved, sel
}

// dryRunOf wraps a plan in the dry-run state; the project holds only a lock,
// because the builder still writes one folder per stack.
func dryRunOf(t *testing.T, resolved plan.Plan, added []moduleAdded) dryRun {
	t.Helper()
	lock := lockfile.Lock{
		Schema: lockfile.SchemaVersion, Generator: generator.Version, CatalogSHA256: strings.Repeat("a", 64),
		Selection: resolved.Selection, Names: resolved.LockNames(),
		Components: []lockfile.Component{{Stack: "go-srv", Name: "acme-srv-first"}, {Stack: "ng-web", Name: "acme-web"}},
	}
	encoded, err := lockfile.Encode(lock)
	if err != nil {
		t.Fatal(err)
	}
	return dryRun{
		path: "acme", plan: resolved, recorded: resolved.Selection, added: added, notes: resolved.Notes,
		project: emit.Project{Files: []emit.File{{Path: lockfile.Name, Data: encoded}}},
	}
}

func TestADryRunOfAMultiPlanListsEveryComponentWithItsBackend(t *testing.T) {
	resolved, _ := multiPlan(t)
	var out, errOut bytes.Buffer
	ctx := &Context{Stdout: &out, Stderr: &errOut, Format: formatText, Logger: discardLogger()}
	if code := writeDryRun(ctx, dryRunOf(t, resolved, nil)); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	want := "Project acme in ./acme (dry run, nothing written)\n" +
		"COMPONENT         STACK    INDEX   BACKEND          GO MODULE                          DATABASE                          PORTS                                            MODULES\n" +
		"acme-srv-first    go-srv   0                        example.com/acme/acme-srv-first    acme_srv_first/acme_srv_first     srv 10100, postgres 10132, kratos-public 10133   core, identity, parameters\n" +
		"acme-srv-second   go-srv   1                        example.com/acme/acme-srv-second   acme_srv_second/acme_srv_second   srv 10101, postgres 10142, kratos-public 10143   core, identity, tenancy\n" +
		"acme-web          ng-web   0       acme-srv-first                                                                        web 10120                                        core, identity, parameters\n" +
		"note: go modules derived from example.com/acme; pass go-module= per component to change them\n" +
		"1 file\n"
	if out.String() != want {
		t.Fatalf("stdout\n%s\nwant\n%s", out.String(), want)
	}
}

func TestADryRunDocumentOfAMultiPlanHasComponentsAndTheComponentOfEachAddition(t *testing.T) {
	resolved, sel := multiPlan(t)
	var added []moduleAdded
	for _, pull := range plan.Pulls(resolved, sel) {
		added = append(added, moduleAdded{Module: pull.Module, RequiredBy: pull.RequiredBy, Stack: pull.Stack, Component: pull.Component})
	}
	var out bytes.Buffer
	ctx := &Context{Stdout: &out, Stderr: &bytes.Buffer{}, Format: formatJSON, Logger: discardLogger(), Notes: resolved.Notes}
	if code := writeDryRun(ctx, dryRunOf(t, resolved, added)); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	var doc struct {
		Components []map[string]any `json:"components"`
		Added      []map[string]any `json:"added"`
		Notes      []string         `json:"notes"`
	}
	decodeDocument(t, out.String(), &doc)
	if len(doc.Components) != 3 {
		t.Fatalf("components = %v", doc.Components)
	}
	second := doc.Components[1]
	if second["name"] != "acme-srv-second" || second["index"] != float64(1) || second["goModule"] != "example.com/acme/acme-srv-second" {
		t.Fatalf("second = %v", second)
	}
	if !reflect.DeepEqual(second["modules"], []any{"core", "identity", "tenancy"}) || second["database"].(map[string]any)["name"] != "acme_srv_second" {
		t.Fatalf("second = %v", second)
	}
	names := second["names"].(map[string]any)
	if names["component.go-srv"] != "acme-srv-second" || names["service.postgres"] != "acme-srv-second-postgres" || names["service.kratos"] != "acme-srv-second-kratos" {
		t.Fatalf("names = %v", names)
	}
	if grep, _ := second["tck"].(map[string]any)["grep"].(string); !strings.HasPrefix(grep, "@(") || !strings.Contains(grep, "identity") {
		t.Fatalf("a backend carries its tck grep: %v", second)
	}
	ports, _ := second["ports"].(map[string]any)
	if ports["srv"] != float64(10101) || ports["postgres"] != float64(10142) || second["origin"] != "http://localhost:10101" {
		t.Fatalf("a backend carries its ports and origin: %v", second)
	}
	if doc.Components[2]["backend"] != "acme-srv-first" || doc.Components[0]["backend"] != nil {
		t.Fatalf("backend bindings = %v %v", doc.Components[2]["backend"], doc.Components[0]["backend"])
	}
	if len(doc.Added) != 3 || doc.Added[1]["component"] != "acme-srv-second" {
		t.Fatalf("added = %v", doc.Added)
	}
	if len(doc.Notes) != 1 {
		t.Fatalf("notes = %v", doc.Notes)
	}
}

func TestAMultiGenerateTextNamesTheComponentOfEachAddition(t *testing.T) {
	var out bytes.Buffer
	ctx := &Context{Stdout: &out, Stderr: &bytes.Buffer{}, Format: formatText, Logger: discardLogger()}
	result := generateResult{
		project: "acme", path: "acme", files: 3,
		added: []moduleAdded{{Module: "identity", RequiredBy: []string{"tenancy", "api-keys"}, Stack: "go-srv", Component: "acme-srv-second"}},
	}
	if code := writeGenerate(ctx, result); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.HasPrefix(out.String(), "Added identity to acme-srv-second (required by tenancy, api-keys).\n") {
		t.Fatalf("stdout = %q", out.String())
	}
	out.Reset()
	result.added[0].Component = ""
	if code := writeGenerate(ctx, result); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.HasPrefix(out.String(), "Added identity (required by tenancy, api-keys).\n") {
		t.Fatalf("a single project names no component: %q", out.String())
	}
}

// The guard fires after the plan and only when the plan is clean.
func TestTheMultiGuardWaitsForAnotherIssueToBeFixed(t *testing.T) {
	e := newEnv(t)
	args := e.fullstackArgs("--component", "go-srv=acme-srv-first,modules=ghost", "--component", "go-srv=acme-srv-second",
		"--component", "ng-web=acme-web,backend=acme-srv-second")
	code, stdout, _ := executeJSON(t, args...)
	if got := issuesOf(t, stdout).codes(); code != exitIssues || !reflect.DeepEqual(got, []string{"MODULE_UNKNOWN"}) {
		t.Fatalf("exit %d codes %v", code, got)
	}
}

// A module the top-level list names and the catalog lacks is reported once for
// every backend that inherits the list, each naming its component.
func TestAnInheritedMissingModuleIsReportedPerBackendComponent(t *testing.T) {
	e := newEnv(t)
	args := e.args("--stacks", "go-srv", "--modules", "ghost", "--component", "go-srv=acme-srv-first", "--component", "go-srv=acme-srv-second")
	code, stdout, _ := executeJSON(t, args...)
	doc := issuesOf(t, stdout)
	if code != exitIssues || len(doc.Issues) != 2 {
		t.Fatalf("exit %d issues %+v", code, doc.Issues)
	}
	for i, component := range []string{"acme-srv-first", "acme-srv-second"} {
		found := doc.Issues[i]
		if found.Code != "MODULE_UNKNOWN" || !strings.HasSuffix(found.Path, "#/modules/0") || !strings.HasSuffix(found.Message, "(backend component "+component+")") {
			t.Fatalf("issue %d = %+v", i, found)
		}
	}
}

// Two backends with distinct go modules generate through the command; the
// per-entry values themselves are covered by the plan tests
// (TestEntryValuesOverrideTheDerivedDefaults).
func TestSeveralBackendsWithOwnGoModulesGenerate(t *testing.T) {
	e := newEnv(t)
	args := e.fullstackArgs(
		"--component", "go-srv=acme-srv-first,go-module=example.org/first",
		"--component", "go-srv=acme-srv-second,go-module=example.org/second",
		"--component", "ng-web=acme-web,backend=acme-srv-first")
	code, stdout, _ := executeJSON(t, args...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	files := tree(t, e.out)
	if !strings.HasPrefix(files["acme-srv-first/go.mod"], "module example.org/first\n") || !strings.HasPrefix(files["acme-srv-second/go.mod"], "module example.org/second\n") {
		t.Errorf("go.mod lines: %q %q", files["acme-srv-first/go.mod"], files["acme-srv-second/go.mod"])
	}
}

// A backend's go-module stays its own: the other backend derives its module
// from the project base, not from the first backend's value. Folding the
// first value into the project would make the two collide and be reported
// as NAME_CONFLICT instead of generating.
func TestABackendGoModuleDoesNotBecomeTheBaseOfTheOthers(t *testing.T) {
	e := newEnv(t)
	args := e.fullstackArgs(
		"--component", "go-srv=acme-srv-first,go-module=example.com/acme/acme-srv-second",
		"--component", "go-srv=acme-srv-second",
		"--component", "ng-web=acme-web,backend=acme-srv-first")
	code, stdout, _ := executeJSON(t, args...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	files := tree(t, e.out)
	firstLine := strings.SplitN(files["acme-srv-first/go.mod"], "\n", 2)[0]
	secondLine := strings.SplitN(files["acme-srv-second/go.mod"], "\n", 2)[0]
	if firstLine != "module example.com/acme/acme-srv-second" || secondLine == firstLine || !strings.HasPrefix(secondLine, "module ") {
		t.Errorf("go.mod lines: %q %q", firstLine, secondLine)
	}
}
