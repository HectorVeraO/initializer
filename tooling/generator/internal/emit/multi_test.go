package emit_test

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/emit"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/verify"
)

const (
	firstModule  = "example.com/acme/acme-srv-first"
	secondModule = "example.com/acme/acme-srv-second"
)

// backend returns a go-srv entry that installs exactly modules (plus what they require).
func backend(name string, modules ...string) manifest.Component {
	return manifest.Component{Stack: "go-srv", Name: name, Modules: modules, HasModules: true}
}

// multiSelection selects two backends of the synthetic repo and one frontend
// bound to the second. The first installs identity; the second tenancy.
func multiSelection() manifest.Selection {
	return manifest.Selection{
		Schema:  2,
		Project: manifest.Project{Name: "acme", GoModule: "example.com/acme"},
		Stacks:  []string{"go-srv", "ng-web"},
		Components: []manifest.Component{
			backend("acme-srv-first", "identity"),
			backend("acme-srv-second", "tenancy"),
			{Stack: "ng-web", Name: "acme-web", Backend: "acme-srv-second"},
		},
	}
}

func resolveOrFail(t *testing.T, repo fixture.Repo, sel manifest.Selection) plan.Plan {
	t.Helper()
	resolved, issues := plan.Resolve(repo.Catalog, sel)
	if len(issues) > 0 {
		t.Fatalf("resolve: %v", issues)
	}
	return resolved
}

func mustBuildMulti(t *testing.T, repo fixture.Repo) emit.Project {
	t.Helper()
	return mustBuild(t, repo, multiSelection())
}

func TestMultiBuildWritesOneFolderPerComponentWithItsOwnModule(t *testing.T) {
	project := mustBuildMulti(t, fixture.Load(t))
	paths := projectPaths(project)
	for _, want := range []string{
		"acme-srv-first/go.mod", "acme-srv-first/internal/platform/identity/identity.go",
		"acme-srv-second/go.mod", "acme-srv-second/internal/platform/tenancy/tenancy.go",
		"acme-web/src/app/platform/modules.ts", "acme-web/package.json", "specs/core/openapi.yaml",
	} {
		if !paths[want] {
			t.Errorf("%s was not delivered", want)
		}
	}
	for path := range paths {
		if strings.HasPrefix(path, "go-srv/") || strings.HasPrefix(path, "ng-web/") {
			t.Errorf("%s stayed under a stack id", path)
		}
	}
	if paths["acme-srv-first/internal/platform/tenancy/tenancy.go"] {
		t.Error("the first component carries tenancy, which only the second installs")
	}
	for name, module := range map[string]string{"acme-srv-first": firstModule, "acme-srv-second": secondModule} {
		other := firstModule
		if name == "acme-srv-first" {
			other = secondModule
		}
		if gomod := projectFile(t, project, name+"/go.mod"); !strings.HasPrefix(gomod, "module "+module+"\n") {
			t.Errorf("%s/go.mod = %q", name, gomod)
		}
		root := projectFile(t, project, name+"/cmd/server/modules.go")
		if !strings.Contains(root, module+"/internal/platform/") || strings.Contains(root, other) {
			t.Errorf("%s composition root imports the wrong module:\n%s", name, root)
		}
	}
	if first := projectFile(t, project, "acme-srv-first/cmd/server/modules.go"); strings.Contains(first, "/tenancy") {
		t.Errorf("the first composition root lists tenancy:\n%s", first)
	}
	if second := projectFile(t, project, "acme-srv-second/cmd/server/modules.go"); !strings.Contains(second, "/tenancy") {
		t.Errorf("the second composition root misses tenancy:\n%s", second)
	}
}

func TestMultiLockRecordsEveryComponentAndItsModules(t *testing.T) {
	lock := decodeLock(t, mustBuildMulti(t, fixture.Load(t)))
	if !lock.Multi() {
		t.Fatal("Multi() is false for two backends")
	}
	var order []string
	for _, component := range lock.Components {
		order = append(order, component.Stack+"/"+component.Name)
	}
	if want := "go-srv/acme-srv-first,go-srv/acme-srv-second,ng-web/acme-web"; strings.Join(order, ",") != want {
		t.Fatalf("components = %v, want %s", order, want)
	}
	first, second, web := lock.Components[0], lock.Components[1], lock.Components[2]
	if *first.Index != 0 || *second.Index != 1 || *web.Index != 0 {
		t.Errorf("indexes = %d %d %d", *first.Index, *second.Index, *web.Index)
	}
	if first.GoModule != firstModule || second.GoModule != secondModule || web.GoModule != "" {
		t.Errorf("go modules = %q %q %q", first.GoModule, second.GoModule, web.GoModule)
	}
	if web.Backend != "acme-srv-second" {
		t.Errorf("web backend = %q, want acme-srv-second", web.Backend)
	}
	if first.Backend != "" {
		t.Errorf("first backend = %q, want none", first.Backend)
	}
	if first.TCK == nil {
		t.Fatal("the first backend has no tck member")
	}
	wantFirst := map[string]int{"srv": 10100, "postgres": 10132, "kratos-public": 10133}
	wantSecond := map[string]int{"srv": 10101, "postgres": 10142, "kratos-public": 10143}
	if !reflect.DeepEqual(first.Ports, wantFirst) || !reflect.DeepEqual(second.Ports, wantSecond) || !reflect.DeepEqual(web.Ports, map[string]int{"web": 10120}) {
		t.Errorf("ports = %v %v %v", first.Ports, second.Ports, web.Ports)
	}
	if first.Origin != "http://localhost:10100" || second.Origin != "http://localhost:10120" || web.Origin != "" {
		t.Errorf("origins = %q %q %q", first.Origin, second.Origin, web.Origin)
	}
	if strings.Join(lock.TCKTags, ",") != strings.Join(first.TCK.Tags, ",") || lock.TCKGrep != first.TCK.Grep {
		t.Error("the top-level tck is not the first backend's")
	}
	if len(lock.Names) != 1 || lock.Names["project"] != "acme" || first.Names["component.go-srv"] != "acme-srv-first" || web.Names["component.ng-web"] != "acme-web" {
		t.Errorf("names: top level %v, first %v, web %v", lock.Names, first.Names, web.Names)
	}
	listed := map[string][]string{}
	for _, module := range lock.Modules {
		holder, found := lock.ComponentOf(module)
		if !found || holder.Name != module.Component {
			t.Errorf("ComponentOf(%s/%s) = %q, %v", module.Component, module.Module, holder.Name, found)
		}
		listed[module.Component] = append(listed[module.Component], module.Module)
	}
	for _, component := range lock.Components {
		if strings.Join(listed[component.Name], ",") != strings.Join(component.Modules, ",") {
			t.Errorf("%s: modules entries %v, entry modules %v", component.Name, listed[component.Name], component.Modules)
		}
	}
}

func TestProjectScopeFilesAreDeliveredOnceUnderTheFirstComponent(t *testing.T) {
	lock := decodeLock(t, mustBuildMulti(t, fixture.Load(t)))
	owners := map[string][]string{}
	for _, module := range lock.Modules {
		for _, file := range module.Files {
			owners[file.Path] = append(owners[file.Path], module.Component+"/"+module.Module)
		}
	}
	for _, path := range []string{".deploy/local/docker-compose.yml", ".deploy/local/README.md"} {
		if got := owners[path]; len(got) != 1 || !strings.HasPrefix(got[0], "acme-srv-first/") {
			t.Errorf("%s is listed by %v, want once under acme-srv-first", path, got)
		}
	}
}

func TestProjectScopeCollisionBetweenModulesIsRefused(t *testing.T) {
	repo := fixture.Load(t)
	crafted := repo.Catalog
	crafted.Specs = append([]catalog.SpecEntry(nil), crafted.Specs...)
	for i := range crafted.Specs {
		if crafted.Specs[i].Spec == "tenancy" {
			crafted.Specs[i].Files = append(append([]catalog.File(nil), crafted.Specs[i].Files...), catalog.File{
				Path:   "specs/tenancy/openapi.yaml",
				SHA256: hashOfFile(t, repo.Root, "specs/tenancy/openapi.yaml"),
				Target: "specs/core/openapi.yaml",
			})
		}
	}
	_, err := build(t, os.DirFS(repo.Root), crafted, repo.CatalogBytes, multiSelection())
	if err == nil || !strings.Contains(err.Error(), "is produced twice") || !strings.Contains(err.Error(), "specs/core/openapi.yaml") {
		t.Fatalf("want a produced-twice error naming specs/core/openapi.yaml, got %v", err)
	}
}

func TestMultiBuildVerifiesCleanAndFlagsOneStrayFilePerComponent(t *testing.T) {
	dir := mustWrite(t, mustBuildMulti(t, fixture.Load(t)))
	if differences, err := verify.Project(dir); err != nil || len(differences) != 0 {
		t.Fatalf("fresh multi project: differences %v, error %v", differences, err)
	}
	for _, stray := range []string{
		"acme-srv-second/internal/platform/tenancy/stray.go",
		"acme-srv-first/internal/platform/core/stray.go",
	} {
		path := filepath.Join(dir, filepath.FromSlash(stray))
		if err := os.WriteFile(path, []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		differences, err := verify.Project(dir)
		if err != nil || len(differences) != 1 || differences[0].Code != verify.CodeUntracked || differences[0].Path != stray {
			t.Fatalf("stray %s: differences %v, error %v", stray, differences, err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
}

// withTenancyFile adds a file to the module "tenancy" of the synthetic repo and
// returns the root, the catalog and its encoded bytes.
func withTenancyFile(t *testing.T, repo fixture.Repo, name string, content []byte) (string, catalog.Catalog, []byte) {
	t.Helper()
	root := t.TempDir()
	copyTree(t, repo.Root, root)
	if err := os.WriteFile(filepath.Join(root, "go-srv", "internal", "platform", "tenancy", name), content, 0o644); err != nil {
		t.Fatal(err)
	}
	crafted := repo.Catalog
	crafted.Stacks = append([]catalog.StackEntry(nil), crafted.Stacks...)
	for i := range crafted.Stacks {
		crafted.Stacks[i].Modules = append([]catalog.ModuleEntry(nil), crafted.Stacks[i].Modules...)
		for j := range crafted.Stacks[i].Modules {
			module := &crafted.Stacks[i].Modules[j]
			if crafted.Stacks[i].Stack == "go-srv" && module.Module == "tenancy" {
				added := catalog.File{Path: "go-srv/internal/platform/tenancy/" + name, SHA256: hashOf(content)}
				module.Files = append(append([]catalog.File(nil), module.Files...), added)
			}
		}
	}
	encoded, err := catalog.Encode(crafted)
	if err != nil {
		t.Fatal(err)
	}
	return root, crafted, encoded
}

func TestCrossReferencesBetweenGoComponentsAreRefusedAndLookalikesAreNot(t *testing.T) {
	repo := fixture.Load(t)
	cases := map[string]struct {
		content string
		want    error
	}{
		"slash":        {"see " + firstModule + "/internal/platform/identity\n", emit.ErrGoModuleCrossReference},
		"quote":        {"var path = \"" + firstModule + "\"\n", emit.ErrGoModuleCrossReference},
		"source path":  {"see " + fixtureModule + " for details\n", emit.ErrGoModulePathLeak},
		"own path":     {"see " + secondModule + "/internal/platform/tenancy\n", nil},
		"longer name":  {"see " + firstModule + "-extra is unrelated\n", nil},
		"other prefix": {"see example.com/acme/acme-srv for details\n", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root, crafted, encoded := withTenancyFile(t, repo, "note.txt", []byte(tc.content))
			_, err := build(t, os.DirFS(root), crafted, encoded, multiSelection())
			isWanted := errors.Is(err, tc.want)
			if tc.want == nil {
				isWanted = err == nil
			}
			if !isWanted || (tc.want != nil && !strings.Contains(err.Error(), "acme-srv-second/internal/platform/tenancy/note.txt")) {
				t.Fatalf("error = %v, want %v naming the file", err, tc.want)
			}
		})
	}
}

func TestAComponentKeepingTheSourceModulePathIsRefusedThroughTheLock(t *testing.T) {
	selection := multiSelection()
	selection.Project.GoModule = "example.com/fixture"
	selection.Components[0].Name = "srv"
	resolved := resolveOrFail(t, fixture.Load(t), selection)
	repo := fixture.Load(t)
	_, err := emit.Build(emit.BuildOptions{Root: os.DirFS(repo.Root), Plan: resolved, CatalogSHA256: hashOf(repo.CatalogBytes)})
	if !errors.Is(err, emit.ErrGoModulePathLeak) || !strings.Contains(err.Error(), "initializer.lock.json") {
		t.Fatalf("error = %v, want a leak found in the lock", err)
	}
}

func TestMultiReadmeRunSectionNamesEveryBackendAndOnlyTheBoundFrontend(t *testing.T) {
	readme := projectFile(t, mustBuildMulti(t, fixture.Load(t)), "README.md")
	for _, want := range []string{
		"stop acme-srv-first\n", "stop acme-srv-second acme-web\n", "mise run dev:acme-srv-first\n", "mise run dev:acme-web\n",
		"- acme-srv-first: http://localhost:10100;", "- acme-srv-second: http://localhost:10120 (console acme-web);",
		"mise run tck:acme-srv-first\n", "mise run tck:acme-srv-second\n",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README lacks %q:\n%s", want, readme)
		}
	}
	if strings.Contains(readme, "stop acme-srv-first acme-web") {
		t.Errorf("README binds the frontend to the first backend:\n%s", readme)
	}
}

func TestAGoComponentWithoutAModulePathIsRefused(t *testing.T) {
	repo := fixture.Load(t)
	resolved := resolveOrFail(t, repo, multiSelection())
	for i := range resolved.Components {
		if resolved.Components[i].Name == "acme-srv-second" {
			resolved.Components[i].GoModule = ""
		}
	}
	_, err := emit.Build(emit.BuildOptions{Root: os.DirFS(repo.Root), Plan: resolved, CatalogSHA256: hashOf(repo.CatalogBytes)})
	if err == nil || !strings.Contains(err.Error(), "acme-srv-second") {
		t.Fatalf("want an error naming the component, got %v", err)
	}
}

func TestEntryNamesHoldTheInstanceSlotsAndTheTopLevelOnlyTheProjectOnes(t *testing.T) {
	repo := fixture.Load(t)
	resolved := resolveOrFail(t, repo, multiSelection())
	for i := range resolved.Components {
		if resolved.Components[i].Name == "acme-srv-second" {
			resolved.Components[i].Database = manifest.Database{Name: "acme_second", User: "acme_second_owner"}
			resolved.Components[i].Services = map[string]string{"postgres": "acme-srv-second-postgres"}
		}
	}
	for slot, value := range map[string]string{"database": "acme_first", "dbUser": "acme_first", "service.postgres": "acme-srv-first-postgres"} {
		resolved.Live[slot] = true
		resolved.Slots[slot] = value
	}
	project, err := emit.Build(emit.BuildOptions{Root: os.DirFS(repo.Root), Plan: resolved, CatalogSHA256: hashOf(repo.CatalogBytes)})
	if err != nil {
		t.Fatal(err)
	}
	lock := decodeLock(t, project)
	second := lock.Components[1]
	if second.Database == nil || second.Database.Name != "acme_second" || second.Database.User != "acme_second_owner" || lock.Components[0].Database != nil {
		t.Fatalf("databases: first %+v, second %+v", lock.Components[0].Database, second.Database)
	}
	want := map[string]string{"component.go-srv": "acme-srv-second", "database": "acme_second", "dbUser": "acme_second_owner", "service.postgres": "acme-srv-second-postgres"}
	for key, value := range want {
		if second.Names[key] != value {
			t.Errorf("names[%s] = %q, want %q", key, second.Names[key], value)
		}
	}
	if len(lock.Names) != 1 || lock.Names["project"] != "acme" {
		t.Errorf("top-level names = %v, want only the project", lock.Names)
	}
}

// Every delivered path of a generated multi project sits under at most one
// component folder, none stays under a stack id, and no path repeats.
func TestEveryDeliveredPathHasOneScope(t *testing.T) {
	repo := fixture.Load(t)
	pool := []string{"identity", "tenancy", "api-keys", "parameters", "feature-flags"}
	rng := rand.New(rand.NewSource(11))
	for round := 0; round < 12; round++ {
		sel := manifest.Selection{
			Schema:  2,
			Project: manifest.Project{Name: "acme", GoModule: "example.com/acme"},
			Stacks:  []string{"go-srv", "ng-web"},
		}
		folders := []string{"acme-web"}
		for i := 0; i < 2+round%2; i++ {
			var picked []string
			for _, id := range pool {
				if rng.Intn(2) == 0 {
					picked = append(picked, id)
				}
			}
			name := fmt.Sprintf("acme-srv-%d", i)
			folders = append(folders, name)
			sel.Components = append(sel.Components, backend(name, picked...))
		}
		sel.Components = append(sel.Components, manifest.Component{Stack: "ng-web", Name: "acme-web", Backend: "acme-srv-0"})
		project := mustBuild(t, repo, sel)
		seen := map[string]bool{}
		for _, file := range project.Files {
			scopes := 0
			for _, folder := range folders {
				if strings.HasPrefix(file.Path, folder+"/") {
					scopes++
				}
			}
			isStackRoot := strings.HasPrefix(file.Path, "go-srv/") || strings.HasPrefix(file.Path, "ng-web/")
			if seen[file.Path] || scopes > 1 || isStackRoot {
				t.Errorf("round %d: %s is repeated, in several folders or under a stack id", round, file.Path)
			}
			seen[file.Path] = true
		}
		if !decodeLock(t, project).Multi() {
			t.Fatalf("round %d: the lock is not multi", round)
		}
	}
}

func TestMultiProjectsAreByteIdenticalAcrossBuilds(t *testing.T) {
	repo := fixture.Load(t)
	compareTrees(t, readTree(t, mustWrite(t, mustBuildMulti(t, repo))), readTree(t, mustWrite(t, mustBuildMulti(t, repo))))
}
