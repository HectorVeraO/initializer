package emit_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/emit"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

const (
	runsCompose = ".deploy/local/docker-compose.yml"
	runsReadme  = ".deploy/local/README.md"
	runsMise    = "mise.toml"
)

// runsComposeText lays the compose file out in runs: one go-srv run holds the
// instance (database, identity services, backend), one ng-web run holds the
// frontend and its dependency on the backend.
const runsComposeText = `name: fixture

services:
  # initializer:stack go-srv
  postgres:
    image: postgres:18
  # initializer:end go-srv
  # initializer:module identity

  kratos:
    image: oryd/kratos:v26.2.0
    depends_on:
      - postgres
  # initializer:end identity
  # initializer:stack go-srv

  go-srv:
    image: golang
    env_file:
      - path: ./.example.env
  # initializer:end go-srv
  # initializer:module tenancy
    labels: [tenancy]
  # initializer:end tenancy

  # initializer:stack ng-web
  ng-web:
    image: nginx
  # initializer:end ng-web
  # initializer:stack go-srv
    depends_on:
      - go-srv
  # initializer:end go-srv
`

// runsReadmeText has a table whose rows sit in a run directly under the
// header, so the copies of the run extend the table, and a module block
// outside every run.
const runsReadmeText = `# Local

| Port | Service |
|---|---|
<!-- initializer:stack go-srv -->
| 10100 | ` + "`go-srv`" + ` HTTP API |
<!-- initializer:end go-srv -->
<!-- initializer:stack ng-web -->
| 10120 | ` + "`ng-web`" + ` console |
<!-- initializer:end ng-web -->
<!-- initializer:module identity -->
| 10133 | ` + "`kratos`" + ` public API |
<!-- initializer:end identity -->

<!-- initializer:module tenancy -->
Tenancy note.
<!-- initializer:end tenancy -->
`

const runsMiseText = `[tasks.up]
run = "up"

# initializer:stack go-srv
[tasks."dev:srv"]
dir = "{{config_root}}/go-srv"
env = { _.file = ["{{config_root}}/.deploy/local/.example.env", "{{config_root}}/.deploy/local/.env"] }
run = "go run ./cmd/server"
# initializer:end go-srv

# initializer:stack ng-web
[tasks."dev:web"]
dir = "{{config_root}}/ng-web"
run = "npm start"
# initializer:end ng-web

# initializer:stack go-srv
[tasks.tck]
run = "npm --prefix specs run tck"
# initializer:end go-srv
`

// runsRules are the name rules of the overlay.
var runsRules = []struct{ path, text string }{
	{runsCompose, "name: %{project}"},
	{runsCompose, "\n  %{service.postgres}:\n"},
	{runsCompose, "\n  %{service.kratos}:\n"},
	{runsCompose, "\n      - %{service.postgres}\n"},
	{runsCompose, "\n  %{component.go-srv}:\n"},
	{runsCompose, "\n      - %{component.go-srv}\n"},
	{runsCompose, "\n  %{component.ng-web}:\n"},
	{runsCompose, "- path: ./%{instanceDir.go-srv}.example.env"},
	{runsReadme, "`%{component.go-srv}` HTTP API"},
	{runsReadme, "`%{component.ng-web}` console"},
	{runsReadme, "`%{service.kratos}` public API"},
	{runsMise, `[tasks."dev:%{task.go-srv}"]`},
	{runsMise, `[tasks."dev:%{task.ng-web}"]`},
	{runsMise, `dir = "{{config_root}}/%{component.go-srv}"`},
	{runsMise, `dir = "{{config_root}}/%{component.ng-web}"`},
	{runsMise, "local/%{instanceDir.go-srv}.example.env"},
	{runsMise, `local/%{instanceDir.go-srv}.env"`},
	{runsMise, "[tasks.%{tckTask.go-srv}]"},
	{runsMise, "npm --prefix specs run tck%{tckArgs.go-srv}"},
}

// runsFixture copies the fixture repository, lays its compose file, deploy
// README and a mise.toml out in runs, adds the rules and returns the root with
// the catalog built from the changed tree.
func runsFixture(t *testing.T) (string, catalog.Catalog, []byte) {
	t.Helper()
	repo := fixture.Load(t)
	root := t.TempDir()
	copyTree(t, repo.Root, root)
	write := func(rel, text string) {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(rel string) string {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	write(runsCompose, runsComposeText)
	write(runsReadme, runsReadmeText)
	write(runsMise, runsMiseText)

	var rules []string
	for _, rule := range runsRules {
		encoded, err := json.Marshal(map[string]string{"path": rule.path, "text": rule.text})
		if err != nil {
			t.Fatal(err)
		}
		rules = append(rules, string(encoded))
	}
	manifestText := read(coreManifestPath)
	changed := strings.Replace(manifestText, `"names": [`+projectRuleJSON+`]`, `"names": [`+strings.Join(rules, ", ")+`]`, 1)
	changed = strings.Replace(changed, `"go-srv/", "specs/package.json"`, `"go-srv/", "mise.toml", "specs/package.json"`, 1)
	if changed == manifestText || !strings.Contains(changed, `"mise.toml"`) {
		t.Fatal("the core manifest was not changed")
	}
	write(coreManifestPath, changed)

	referencePath := ".initializer/reference.json"
	reference := read(referencePath)
	withServices := strings.Replace(reference, `"ports": {`, `"services": {"kratos": "kratos", "postgres": "postgres"},`+"\n  "+`"ports": {`, 1)
	if withServices == reference {
		t.Fatal("the reference file was not changed")
	}
	write(referencePath, withServices)

	list, err := os.ReadFile(filepath.Join(fixture.Dir(t), "files.txt"))
	if err != nil {
		t.Fatal(err)
	}
	files := append(strings.Fields(string(list)), runsMise)
	built, issues, err := catalog.Build(catalog.BuildOptions{Root: os.DirFS(root), Files: files, Generator: generator.Version})
	if err != nil || len(issues) > 0 {
		t.Fatalf("catalog build: %v %v", err, issues)
	}
	encoded, err := catalog.Encode(built)
	if err != nil {
		t.Fatal(err)
	}
	return root, built, encoded
}

func buildRuns(t *testing.T, sel manifest.Selection) emit.Project {
	t.Helper()
	root, cat, encoded := runsFixture(t)
	project, err := build(t, os.DirFS(root), cat, encoded, sel)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return project
}

func assertContainsAll(t *testing.T, name, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("%s lacks %q:\n%s", name, want, text)
		}
	}
}

func assertCount(t *testing.T, name, text, needle string, want int) {
	t.Helper()
	if got := strings.Count(text, needle); got != want {
		t.Errorf("%s holds %q %d times, want %d:\n%s", name, needle, got, want, text)
	}
}

func TestRunsRepeatTheComposeInstanceOncePerBackend(t *testing.T) {
	project := buildRuns(t, multiSelection())
	compose := projectFile(t, project, runsCompose)
	assertContainsAll(t, "compose", compose,
		"\n  acme-srv-first-postgres:\n", "\n  acme-srv-second-postgres:\n",
		"\n  acme-srv-first-kratos:\n", "\n  acme-srv-second-kratos:\n",
		"\n  acme-srv-first:\n", "\n  acme-srv-second:\n",
		"- acme-srv-first-postgres\n", "- acme-srv-second-postgres\n",
		"- path: ./acme-srv-first/.example.env", "- path: ./acme-srv-second/.example.env")
	if strings.Contains(compose, "\n  postgres:\n") || strings.Contains(compose, "\n  go-srv:\n") {
		t.Errorf("a reference service key survived:\n%s", compose)
	}
	assertCount(t, "compose", compose, "name: acme\n", 1)
	assertCount(t, "compose", compose, "\nservices:\n", 1)
	// The frontend run has one copy, bound to the second backend.
	assertCount(t, "compose", compose, "\n  acme-web:\n", 1)
	assertCount(t, "compose", compose, "      - acme-srv-second\n", 1)
	assertCount(t, "compose", compose, "      - acme-srv-first\n", 0)
	// Only the second backend installs tenancy: inside a copy a module block
	// follows that component's modules.
	assertCount(t, "compose", compose, "labels: [tenancy]", 1)
	if first, second := strings.Index(compose, "\n  acme-srv-first:\n"), strings.Index(compose, "\n  acme-srv-second:\n"); first > second {
		t.Errorf("copies are not in index order:\n%s", compose)
	}
}

func TestRunsGiveEachBackendItsMiseTasks(t *testing.T) {
	project := buildRuns(t, multiSelection())
	mise := projectFile(t, project, runsMise)
	assertContainsAll(t, "mise.toml", mise,
		`[tasks."dev:acme-srv-first"]`, `[tasks."dev:acme-srv-second"]`, `[tasks."dev:acme-web"]`,
		`[tasks."tck:acme-srv-first"]`, `[tasks."tck:acme-srv-second"]`,
		`dir = "{{config_root}}/acme-srv-first"`, `dir = "{{config_root}}/acme-web"`,
		`"{{config_root}}/.deploy/local/acme-srv-first/.example.env", "{{config_root}}/.deploy/local/acme-srv-first/.env"]`,
		`run = "npm --prefix specs run tck -- --component acme-srv-first"`,
		`run = "npm --prefix specs run tck -- --component acme-srv-second"`)
	assertCount(t, "mise.toml", mise, "[tasks.up]", 1)
	if strings.Contains(mise, `"dev:srv"`) || strings.Contains(mise, "[tasks.tck]") {
		t.Errorf("a reference task key survived:\n%s", mise)
	}
}

func TestRunsExtendTheReadmeTableAndDropAFrontendRowWithoutPartner(t *testing.T) {
	project := buildRuns(t, multiSelection())
	readme := projectFile(t, project, runsReadme)
	assertContainsAll(t, "README", readme,
		"| 10100 | `acme-srv-first` HTTP API |\n", "| 10100 | `acme-srv-second` HTTP API |\n",
		"| 10133 | `acme-srv-first-kratos` public API |\n", "| 10133 | `acme-srv-second-kratos` public API |\n")
	// The first backend has no frontend: its copy drops the ng-web row; the
	// second backend's copy keeps it.
	assertCount(t, "README", readme, "| 10120 | `acme-web` console |\n", 1)
	assertCount(t, "README", readme, "| Port | Service |", 1)
	if strings.Contains(readme, "\n\n| 10100") {
		t.Errorf("a copy of the table run is separated from the table:\n%s", readme)
	}
	// A module block outside every run keeps the any-component predicate.
	assertCount(t, "README", readme, "Tenancy note.", 1)
}

func TestRunsKeepOneInstanceAtTheReference(t *testing.T) {
	project := buildRuns(t, webSelection("tenancy"))
	compose := projectFile(t, project, runsCompose)
	assertContainsAll(t, "compose", compose,
		"\n  acme-backend-postgres:\n", "\n  acme-backend-kratos:\n", "\n  acme-backend-srv:\n", "\n  acme-backend-web:\n",
		"- path: ./.example.env", "labels: [tenancy]", "      - acme-backend-srv\n")
	mise := projectFile(t, project, runsMise)
	assertContainsAll(t, "mise.toml", mise, `[tasks."dev:srv"]`, `[tasks."dev:web"]`, "[tasks.tck]",
		`run = "npm --prefix specs run tck"`, `/.deploy/local/.example.env"`)
	readme := projectFile(t, project, runsReadme)
	assertCount(t, "README", readme, "| 10120 | `acme-backend-web` console |\n", 1)
	for _, p := range []string{runsCompose, runsMise, runsReadme} {
		text := projectFile(t, project, p)
		for _, derived := range []string{"instanceDir", "task.", "tckTask", "tckArgs", "%{"} {
			if strings.Contains(text, derived) {
				t.Errorf("%s keeps the placeholder %q:\n%s", p, derived, text)
			}
		}
	}
}

func TestRunsDropTheFrontendRunAndItsDependencyWithoutAConsole(t *testing.T) {
	project := buildRuns(t, selection(targetModule, "tenancy"))
	compose := projectFile(t, project, runsCompose)
	assertCount(t, "compose", compose, "ng-web", 0)
	assertCount(t, "compose", compose, "depends_on:\n      - acme-backend-srv", 0)
	readme := projectFile(t, project, runsReadme)
	assertCount(t, "README", readme, "console", 0)
	mise := projectFile(t, project, runsMise)
	assertCount(t, "mise.toml", mise, "dev:web", 0)
}

func TestInstanceSlotsAreNeverRecordedInTheLock(t *testing.T) {
	for _, sel := range []manifest.Selection{webSelection("tenancy"), multiSelection()} {
		lock := decodeLock(t, buildRuns(t, sel))
		recorded := map[string]string{}
		for key, value := range lock.Names {
			recorded[key] = value
		}
		for _, component := range lock.Components {
			for key, value := range component.Names {
				recorded[key] = value
			}
		}
		for key := range recorded {
			for _, derived := range []string{"instanceDir", "instanceSuffix", "task.", "tckTask", "tckArgs"} {
				if strings.HasPrefix(key, derived) {
					t.Errorf("the lock records the derived slot %s", key)
				}
			}
		}
	}
}
