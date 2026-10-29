package main

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

func TestParseComponent(t *testing.T) {
	good := map[string]manifest.Component{
		"go-srv=acme-srv":    {Stack: "go-srv", Name: "acme-srv"},
		"ng-web=acme-web":    {Stack: "ng-web", Name: "acme-web"},
		"rust-srv=x":         {Stack: "rust-srv", Name: "x"},
		"go-srv=Not_A_Label": {Stack: "go-srv", Name: "Not_A_Label"},
		"go-srv=a=b":         {Stack: "go-srv", Name: "a=b"},
	}
	for value, want := range good {
		got, problem := parseComponent(value, nil)
		if problem != nil || got != want {
			t.Errorf("parseComponent(%q) = %+v %v, want %+v", value, got, problem, want)
		}
	}
	bad := map[string]string{
		"":                      "the value is empty",
		"=acme":                 "needs a stack before =NAME",
		",x=y":                  "needs a stack before =NAME",
		"go-srv":                `"go-srv" needs STACK=NAME`,
		"go-srv=":               `"go-srv=" needs a name after =`,
		"go-srv=acme,modules=x": `unknown key "modules"`,
		"go-srv=acme,loose":     `unknown key "loose"`,
		"go-srv=acme,":          `unknown key ""`,
	}
	for value, message := range bad {
		_, problem := parseComponent(value, nil)
		if problem == nil || !strings.Contains(problem.message, message) {
			t.Errorf("parseComponent(%q) problem = %v, want %q", value, problem, message)
		}
	}
}

func TestParseComponentSuggestsAClosedKey(t *testing.T) {
	keys := []string{"modules", "go-module"}
	_, problem := parseComponent("go-srv=acme,modueles=x", keys)
	if problem == nil || problem.suggestion != "modules" {
		t.Fatalf("problem = %+v", problem)
	}
	got, problem := parseComponent("go-srv=acme,modules=x", keys)
	if problem != nil || got.Name != "acme" {
		t.Fatalf("a registered key is valid: %+v %v", got, problem)
	}
}

func FuzzComponentFlag(f *testing.F) {
	for _, seed := range []string{"go-srv=acme-srv", "ng-web=x", "", "=", "go-srv", "go-srv=", "a=b,c=d", ",", "=a,b", "go-srv=a=b", "\x00=\xff"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		component, problem := parseComponent(value, componentKeys)
		if problem != nil {
			return
		}
		if component.Stack == "" || component.Name == "" {
			t.Fatalf("accepted %q without a stack and a name: %+v", value, component)
		}
		if got := component.Stack + "=" + component.Name; got != value {
			t.Fatalf("accepted %q does not round-trip: %q", value, got)
		}
	})
}

func TestComponentFlagProblemsAreAllListed(t *testing.T) {
	e := newEnv(t)
	args := e.args("--stacks", "go-srv", "--modules", "parameters", "--go-module", "example.com/acme",
		"--component", "go-srv=", "--component", "go-srv", "--component", "=x", "--component", "go-srv=a,k=v",
		"--component", "go-srv=same", "--component", "go-srv=same")
	code, _, stderr := execute(t, args...)
	if code != exitUsage {
		t.Fatalf("exit %d, want 2: %s", code, stderr)
	}
	for _, want := range []string{
		`--component: invalid value: "go-srv=" needs a name after =`,
		`--component: invalid value: "go-srv" needs STACK=NAME`,
		`--component: invalid value: "=x" needs a stack before =NAME`,
		`unknown key "k"`,
		"--component go-srv=same: is given twice",
		"5 problems.",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if _, err := os.Stat(e.out); err == nil {
		t.Error("a usage error wrote the output")
	}
}

func TestComponentFlagWithoutAValueIsAProblem(t *testing.T) {
	e := newEnv(t)
	code, _, stderr := execute(t, e.args("--stacks", "go-srv", "--modules", "parameters", "--go-module", "example.com/acme", "--component")...)
	if code != exitUsage || !strings.Contains(stderr, "--component: needs a value") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

// fullstackArgs selects both stacks of the fixture with every module.
func (e env) fullstackArgs(extra ...string) []string {
	base := e.args("--name", "acme-backend", "--go-module", "example.com/acme/backend", "--stacks", "go-srv,ng-web", "--all-modules")
	return append(base, extra...)
}

// generateDocument is the part of the generate document these tests read.
type generateDocument struct {
	Project    string
	Files      int
	Components []manifest.Component
}

func TestGenerateDeliversTheStackUnderItsComponentFolder(t *testing.T) {
	e := newEnv(t)
	code, stdout, stderr := executeJSON(t, e.fullstackArgs("--component", "go-srv=acme-srv", "--component", "ng-web=acme-web")...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var doc generateDocument
	decodeDocument(t, stdout, &doc)
	want := []manifest.Component{{Stack: "go-srv", Name: "acme-srv"}, {Stack: "ng-web", Name: "acme-web"}}
	if !reflect.DeepEqual(doc.Components, want) {
		t.Fatalf("components = %+v, want %+v", doc.Components, want)
	}

	files := tree(t, e.out)
	for _, path := range []string{"acme-srv/go.mod", "acme-srv/cmd/server/modules.go", "acme-srv/internal/platform/identity/identity.go",
		"acme-web/src/app/platform/modules.ts", "acme-web/package.json", "specs/core/openapi.yaml", ".deploy/local/docker-compose.yml"} {
		if _, found := files[path]; !found {
			t.Errorf("%s was not delivered", path)
		}
	}
	for path := range files {
		if strings.HasPrefix(path, "go-srv/") || strings.HasPrefix(path, "ng-web/") {
			t.Errorf("%s stayed under a stack id", path)
		}
	}
	if !strings.HasPrefix(files["acme-srv/go.mod"], "module example.com/acme/backend\n") {
		t.Errorf("go.mod module line not rewritten: %q", files["acme-srv/go.mod"])
	}
	for _, want := range []string{"stop acme-srv acme-web\n", "Editing acme-srv runs the console", "`npm --prefix acme-web ci`"} {
		if !strings.Contains(files["README.md"], want) {
			t.Errorf("README lacks %q:\n%s", want, files["README.md"])
		}
	}
}

func TestGeneratedComponentLockDecodesAndVerifies(t *testing.T) {
	e := newEnv(t)
	if code, _, stderr := execute(t, e.fullstackArgs("--component", "go-srv=acme-srv", "--component", "ng-web=acme-web")...); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	data, err := os.ReadFile(filepath.Join(e.out, lockfile.Name))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lockfile.Decode(data)
	if err != nil {
		t.Fatalf("the lock does not decode: %v", err)
	}
	wantComponents := []lockfile.Component{{Stack: "go-srv", Name: "acme-srv"}, {Stack: "ng-web", Name: "acme-web"}}
	if !reflect.DeepEqual(lock.Components, wantComponents) || lock.Names["component.go-srv"] != "acme-srv" {
		t.Fatalf("lock components %+v names %v", lock.Components, lock.Names)
	}
	code, stdout, _ := executeJSON(t, "verify", e.out)
	if code != exitOK {
		t.Fatalf("verify exit %d: %s", code, stdout)
	}

	stray := filepath.Join(e.out, "acme-srv", "internal", "platform", "identity", "stray.go")
	if err := os.WriteFile(stray, []byte("package identity\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ = executeJSON(t, "verify", e.out)
	var doc struct {
		Differences []struct{ Code, Path string }
	}
	decodeDocument(t, stdout, &doc)
	if code != exitIssues || len(doc.Differences) != 1 || doc.Differences[0].Code != "UNTRACKED" ||
		doc.Differences[0].Path != "acme-srv/internal/platform/identity/stray.go" {
		t.Fatalf("exit %d differences %+v", code, doc.Differences)
	}
}

func TestLockEntriesKeepTheSourcePathOfAMovedFile(t *testing.T) {
	e := newEnv(t)
	if code, _, stderr := execute(t, e.fullstackArgs("--component", "go-srv=acme-srv", "--component", "ng-web=ng-web")...); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	data, err := os.ReadFile(filepath.Join(e.out, lockfile.Name))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lockfile.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	moved, kept := 0, 0
	for _, module := range lock.Modules {
		for _, file := range module.Files {
			if module.Stack == "go-srv" && file.Path == "acme-srv/internal/platform/core/core.go" && file.SourcePath == "go-srv/internal/platform/core/core.go" {
				moved++
			}
			if module.Stack == "ng-web" && file.SourcePath == "" && strings.HasPrefix(file.Path, "ng-web/") {
				kept++
			}
		}
	}
	if moved != 1 || kept == 0 {
		t.Fatalf("moved core.go entries %d, unmoved ng-web entries %d", moved, kept)
	}
}

func TestWithoutComponentsTheFoldersFollowTheProjectName(t *testing.T) {
	e := newEnv(t)
	code, stdout, stderr := executeJSON(t, e.fullstackArgs()...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var doc generateDocument
	decodeDocument(t, stdout, &doc)
	want := []manifest.Component{{Stack: "go-srv", Name: "acme-backend-srv"}, {Stack: "ng-web", Name: "acme-backend-web"}}
	if !reflect.DeepEqual(doc.Components, want) {
		t.Fatalf("components = %+v", doc.Components)
	}
	files := tree(t, e.out)
	if _, found := files["acme-backend-srv/cmd/server/modules.go"]; !found {
		t.Error("the default composition root is not under the project-named folder")
	}
	for path := range files {
		if strings.HasPrefix(path, "go-srv/") || strings.HasPrefix(path, "ng-web/") {
			t.Errorf("%s stayed under a stack id", path)
		}
	}
	if !strings.Contains(files["README.md"], "stop acme-backend-srv acme-backend-web\n") {
		t.Errorf("README does not name the default folder:\n%s", files["README.md"])
	}
	var recorded map[string]any
	if err := json.Unmarshal([]byte(files["initializer.selection.json"]), &recorded); err != nil {
		t.Fatal(err)
	}
	_, hasComponents := recorded["components"]
	if recorded["schema"] != float64(1) || hasComponents {
		t.Errorf("a run without components must record schema 1 and no components: %v", recorded)
	}
}

func TestTheRecordedSelectionHoldsTheComponentsAsGivenAndReplays(t *testing.T) {
	first := newEnv(t)
	if code, _, stderr := execute(t, first.fullstackArgs("--component", "go-srv=acme-srv", "--component", "ng-web=acme-web")...); code != exitOK {
		t.Fatalf("first: %d %s", code, stderr)
	}
	recorded := tree(t, first.out)["initializer.selection.json"]
	var doc struct {
		Schema     int
		Components []manifest.Component
	}
	if err := json.Unmarshal([]byte(recorded), &doc); err != nil {
		t.Fatal(err)
	}
	want := []manifest.Component{{Stack: "go-srv", Name: "acme-srv"}, {Stack: "ng-web", Name: "acme-web"}}
	if doc.Schema != 2 || !reflect.DeepEqual(doc.Components, want) {
		t.Fatalf("recorded selection:\n%s", recorded)
	}

	second := first
	second.out = filepath.Join(filepath.Dir(first.out), "again")
	if code, _, stderr := execute(t, second.args("--selection", filepath.Join(first.out, "initializer.selection.json"))...); code != exitOK {
		t.Fatalf("replay: %d %s", code, stderr)
	}
	left, right := tree(t, first.out), tree(t, second.out)
	if len(left) == 0 || len(left) != len(right) {
		t.Fatalf("file sets differ: %d vs %d", len(left), len(right))
	}
	for path, content := range left {
		if right[path] != content {
			t.Errorf("%s differs after the replay", path)
		}
	}
}

func TestSeveralComponentsOfOneStackAreRefused(t *testing.T) {
	e := newEnv(t)
	code, stdout, _ := executeJSON(t, e.fullstackArgs("--component", "go-srv=acme-srv", "--component", "go-srv=other-srv")...)
	found := issueArray(t, stdout)
	if code != exitIssues || len(found) != 1 {
		t.Fatalf("exit %d issues %v", code, found)
	}
	if found[0]["code"] != "COMPONENT_MULTI_UNSUPPORTED" || found[0]["path"] != "flags#/components/1" ||
		found[0]["message"] != "several components of stack go-srv are not supported by this generator yet" {
		t.Fatalf("issue %v", found[0])
	}
	if _, err := os.Stat(e.out); err == nil {
		t.Error("a refused run wrote the output")
	}
}

func TestSeveralComponentsOfOneStackInAFileAreRefused(t *testing.T) {
	e := newEnv(t)
	body := `{"schema":2,"project":{"name":"acme-backend","goModule":"example.com/acme/backend"},"stacks":["go-srv"],"modules":["parameters"],
"components":[{"stack":"go-srv","name":"acme-srv"},{"stack":"go-srv","name":"other-srv"}]}`
	file := writeSelection(t, body)
	code, stdout, _ := executeJSON(t, e.args("--selection", file)...)
	found := issueArray(t, stdout)
	if code != exitIssues || len(found) != 1 || found[0]["code"] != "COMPONENT_MULTI_UNSUPPORTED" ||
		found[0]["path"] != filepath.ToSlash(file)+"#/components" {
		t.Fatalf("exit %d issues %v", code, found)
	}
}

func TestComponentNameProblemsAreCollectedInOneRun(t *testing.T) {
	e := newEnv(t)
	code, stdout, _ := executeJSON(t, e.fullstackArgs(
		"--component", "go-srv=con", "--component", "ng-web=specs", "--component", "ng-wb=acme-web")...)
	found := issueArray(t, stdout)
	if code != exitIssues {
		t.Fatalf("exit %d", code)
	}
	var codes, paths []string
	for _, issue := range found {
		codes = append(codes, issue["code"])
		paths = append(paths, issue["path"])
	}
	wantCodes := []string{"NAME_RESERVED", "NAME_RESERVED", "NAME_OVERRIDE_UNUSED"}
	wantPaths := []string{"flags#/components/0", "flags#/components/1", "flags#/components/2"}
	if !reflect.DeepEqual(codes, wantCodes) || !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("codes %v (want %v) paths %v", codes, wantCodes, paths)
	}
	if found[2]["hint"] != `did you mean "ng-web"?` {
		t.Errorf("unused override hint = %q", found[2]["hint"])
	}
}

func TestComponentNameProblemsRenderTheFlagAsTyped(t *testing.T) {
	e := newEnv(t)
	code, _, stderr := execute(t, e.fullstackArgs("--component", "go-srv=acme", "--component", "ng-web=acme")...)
	if code != exitIssues {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{
		`error: --component go-srv=acme: component go-srv and component ng-web both resolve to "acme" [NAME_CONFLICT]`,
		`error: --component ng-web=acme: component go-srv and component ng-web both resolve to "acme" [NAME_CONFLICT]`,
		"  hint: set one of them to another name",
		"2 problems; nothing was written.",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
}

func TestAConflictWithADefaultedFolderRendersTheStacksFlag(t *testing.T) {
	e := newEnv(t)
	code, _, stderr := execute(t, e.fullstackArgs("--component", "go-srv=acme-backend-web")...)
	if code != exitIssues {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{
		`error: --component go-srv=acme-backend-web: component go-srv and component ng-web both resolve to "acme-backend-web" [NAME_CONFLICT]`,
		`error: --stacks: component go-srv and component ng-web both resolve to "acme-backend-web" [NAME_CONFLICT]`,
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
}

func TestAnInvalidComponentNameIsNameInvalidWithTheExampleHint(t *testing.T) {
	e := newEnv(t)
	code, stdout, stderr := executeJSON(t, e.fullstackArgs("--component", "go-srv=Acme_API", "--component", "ng-web=abc-")...)
	found := issueArray(t, stdout)
	if code != exitIssues || len(found) != 2 {
		t.Fatalf("exit %d issues %v %s", code, found, stderr)
	}
	for i, issue := range found {
		wantPath := "flags#/components/" + string(rune('0'+i))
		if issue["code"] != "NAME_INVALID" || issue["path"] != wantPath || issue["hint"] != "example: acme-api" {
			t.Errorf("issue %d = %v", i, issue)
		}
	}
	if !strings.HasPrefix(found[0]["message"], `"Acme_API" must be lowercase letters, digits and inner hyphens, 2-63 characters`) {
		t.Errorf("message %q", found[0]["message"])
	}
}

func TestANameProblemAndAFileProblemAreReportedTogether(t *testing.T) {
	e := newEnv(t)
	body := `{"schema":2,"project":{"name":"Bad_Name","goModule":"example.com/acme/backend"},"stacks":["go-srv"],"modules":["parameters"],
"components":[{"stack":"go-srv","name":"Bad_Srv"},{"stack":"ng-web","extra":1}]}`
	code, stdout, _ := executeJSON(t, e.args("--selection", writeSelection(t, body))...)
	var codes []string
	for _, issue := range issueArray(t, stdout) {
		codes = append(codes, issue["code"]+" "+issue["path"][strings.Index(issue["path"], "#"):])
	}
	want := []string{"NAME_INVALID #/components/0/name", "SELECTION_INVALID #/components/1/extra", "NAME_INVALID #/project/name"}
	sort.Strings(want)
	sort.Strings(codes)
	if code != exitIssues || !reflect.DeepEqual(codes, want) {
		t.Fatalf("exit %d codes %v, want %v", code, codes, want)
	}
}

func TestComponentsUnderSchema1AreRefused(t *testing.T) {
	e := newEnv(t)
	body := strings.Replace(validSelection, `"schema": 1,`, `"schema": 1, "components": [],`, 1)
	code, stdout, _ := executeJSON(t, e.args("--selection", writeSelection(t, body))...)
	found := issueArray(t, stdout)
	if code != exitIssues || len(found) != 1 || found[0]["message"] != "components needs schema 2" {
		t.Fatalf("exit %d issues %v", code, found)
	}
}

func TestAFileWithComponentsGenerates(t *testing.T) {
	e := newEnv(t)
	body := `{"schema":2,"project":{"name":"acme-backend","goModule":"example.com/acme/backend"},"stacks":["go-srv","ng-web"],"modules":["parameters"],
"components":[{"stack":"go-srv","name":"acme-srv"},{"stack":"ng-web"}]}`
	code, _, stderr := execute(t, e.args("--selection", writeSelection(t, body))...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	files := tree(t, e.out)
	if _, found := files["acme-srv/go.mod"]; !found {
		t.Error("the named component did not move")
	}
	if _, found := files["acme-backend-web/package.json"]; !found {
		t.Error("the unnamed component must take the default name")
	}
	recorded := files["initializer.selection.json"]
	var recordedSelection struct {
		Components []struct {
			Stack string `json:"stack"`
			Name  string `json:"name"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(recorded), &recordedSelection); err != nil {
		t.Fatalf("recorded selection is not JSON: %v\n%s", err, recorded)
	}
	wantEntries := []struct{ Stack, Name string }{{"go-srv", "acme-srv"}, {"ng-web", ""}}
	if len(recordedSelection.Components) != len(wantEntries) {
		t.Fatalf("the recorded selection must hold the entries as given: %s", recorded)
	}
	for i, want := range wantEntries {
		got := recordedSelection.Components[i]
		if got.Stack != want.Stack || got.Name != want.Name {
			t.Errorf("recorded entry %d = %+v, want %+v", i, got, want)
		}
	}
}

// ciSelections lists the CI selection files of the repository.
func ciSelections(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(realCheckoutRoot(t), "tooling", "generator", "testdata", "ci", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no CI selections: %v", err)
	}
	return paths
}

// randomLabel returns a valid label that cannot collide with a delivered folder.
func randomLabel(random *rand.Rand, suffix string) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	length := 2 + random.Intn(8)
	name := make([]byte, 0, length+len(suffix)+1)
	name = append(name, 'p')
	for i := 0; i < length; i++ {
		name = append(name, letters[random.Intn(len(letters))])
	}
	return string(name) + "-" + suffix
}

// Property: for every CI selection and random valid component names, every
// delivered path is outside the stack roots or under its component folder, and
// verify reports no difference.
func TestEveryCISelectionDeliversUnderItsComponentFolders(t *testing.T) {
	root, catalogPath := realCatalog(t)
	random := rand.New(rand.NewSource(184))
	for _, path := range ciSelections(t) {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			folders := map[string]string{}
			var entries []map[string]string
			for _, stack := range doc["stacks"].([]any) {
				id := stack.(string)
				folders[id] = randomLabel(random, strings.TrimPrefix(id, "go-"))
				entries = append(entries, map[string]string{"stack": id, "name": folders[id]})
			}
			doc["schema"] = 2
			doc["components"] = entries
			selection, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "out")
			code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, "--selection", writeSelection(t, string(selection)), out)
			if code != exitOK {
				t.Fatalf("generate exit %d: %s", code, stderr)
			}
			for delivered := range tree(t, out) {
				first, _, _ := strings.Cut(delivered, "/")
				isStackRoot := first == "go-srv" || first == "ng-web"
				isComponent := first == folders["go-srv"] || first == folders["ng-web"]
				if isStackRoot {
					t.Errorf("%s stayed under a stack root", delivered)
				}
				isRendered := delivered == "README.md" || strings.HasPrefix(delivered, "initializer.") ||
					delivered == "mise.toml" || delivered == ".gitignore" || delivered == ".gitattributes" || delivered == "flags.yaml"
				if !isComponent && !isRendered && !strings.HasPrefix(delivered, "specs/") && !strings.HasPrefix(delivered, ".deploy/") {
					t.Errorf("%s is neither in a component folder nor project scope", delivered)
				}
			}
			code, stdout, _ := executeJSON(t, "verify", out)
			if code != exitOK {
				t.Fatalf("verify exit %d: %s", code, stdout)
			}
		})
	}
}
