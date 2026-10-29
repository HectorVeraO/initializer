package plan_test

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
)

// fullstackSelection selects both stacks of the fixture repository.
func fullstackSelection(components ...manifest.Component) manifest.Selection {
	sel := manifest.Selection{
		Schema:     1,
		Project:    manifest.Project{Name: "acme-backend", GoModule: "example.com/acme/backend"},
		Stacks:     []string{"go-srv", "ng-web"},
		Modules:    []string{"parameters"},
		Components: components,
	}
	if len(components) > 0 {
		sel.Schema = 2
	}
	return sel
}

func issuesAt(issues []issue.Issue, code string) []issue.Issue {
	var found []issue.Issue
	for _, got := range issues {
		if got.Code == code {
			found = append(found, got)
		}
	}
	return found
}

func TestComponentNamesComeFromTheEntriesElseTheProjectName(t *testing.T) {
	repo := fixture.Load(t)
	p := mustResolve(t, repo.Catalog, fullstackSelection(manifest.Component{Stack: "go-srv", Name: "acme-srv"}))
	want := []plan.Component{{Stack: "go-srv", Name: "acme-srv"}, {Stack: "ng-web", Name: "acme-backend-web"}}
	if !reflect.DeepEqual(p.Components, want) {
		t.Fatalf("components = %+v, want %+v", p.Components, want)
	}
}

func TestProjectPathMovesOnlyThePathsUnderASelectedStackRoot(t *testing.T) {
	repo := fixture.Load(t)
	p := mustResolve(t, repo.Catalog, fullstackSelection(
		manifest.Component{Stack: "go-srv", Name: "acme-srv"},
		manifest.Component{Stack: "ng-web", Name: "acme-web"}))
	cases := map[string]string{
		"go-srv/internal/platform/core/core.go": "acme-srv/internal/platform/core/core.go",
		"go-srv/go.mod":                         "acme-srv/go.mod",
		"ng-web/src/app/platform/modules.ts":    "acme-web/src/app/platform/modules.ts",
		"specs/core/openapi.yaml":               "specs/core/openapi.yaml",
		".deploy/local/docker-compose.yml":      ".deploy/local/docker-compose.yml",
		"mise.toml":                             "mise.toml",
		"flags.yaml":                            "flags.yaml",
		"go-srvx/file.go":                       "go-srvx/file.go",
		"go-srv":                                "go-srv",
	}
	for source, want := range cases {
		if got := p.ProjectPath(source); got != want {
			t.Errorf("ProjectPath(%q) = %q, want %q", source, got, want)
		}
	}
}

func TestProjectPathLeavesAnUnselectedStackRootAlone(t *testing.T) {
	repo := fixture.Load(t)
	p := mustResolve(t, repo.Catalog, selection("parameters"))
	if got := p.ProjectPath("ng-web/package.json"); got != "ng-web/package.json" {
		t.Errorf("a path under an unselected stack root moved to %q", got)
	}
	if got := p.ProjectPath("go-srv/go.mod"); got != "acme-backend-srv/go.mod" {
		t.Errorf("the default name must follow the project, got %q", got)
	}
}

func TestRecordedSelectionKeepsTheComponentsAsGiven(t *testing.T) {
	repo := fixture.Load(t)
	given := []manifest.Component{{Stack: "ng-web"}, {Stack: "go-srv", Name: "acme-srv"}}
	p := mustResolve(t, repo.Catalog, fullstackSelection(given...))
	if p.Selection.Schema != 2 || !reflect.DeepEqual(p.Selection.Components, given) {
		t.Fatalf("recorded selection %+v", p.Selection)
	}
}

func TestRecordedSelectionStaysSchema1WithoutComponents(t *testing.T) {
	repo := fixture.Load(t)
	sel := fullstackSelection()
	sel.Schema = 2
	p := mustResolve(t, repo.Catalog, sel)
	if p.Selection.Schema != 1 || p.Selection.Components != nil {
		t.Fatalf("a schema 2 selection without components must record schema 1: %+v", p.Selection)
	}
}

func TestNameProblemsAreCollectedInOneRun(t *testing.T) {
	repo := fixture.Load(t)
	sel := fullstackSelection(
		manifest.Component{Stack: "go-srv", Name: "con"},
		manifest.Component{Stack: "ng-web", Name: "specs"},
		manifest.Component{Stack: "ng-wb", Name: "acme-web"},
	)
	sel.Project.Name = "nul"
	_, issues := plan.Resolve(repo.Catalog, sel)

	reserved := issuesAt(issues, plan.CodeNameReserved)
	wantReserved := map[string]string{
		"selection#/project/name":      `"nul" is a reserved device name on Windows`,
		"selection#/components/0/name": `"con" is a reserved device name on Windows`,
		"selection#/components/1/name": `"specs" is already a folder of the project`,
	}
	if len(reserved) != len(wantReserved) {
		t.Fatalf("NAME_RESERVED issues = %v", reserved)
	}
	for _, got := range reserved {
		if want, ok := wantReserved[got.Path]; !ok || got.Message != want {
			t.Errorf("unexpected NAME_RESERVED %+v", got)
		}
	}
	unused := issuesAt(issues, plan.CodeNameOverrideUnused)
	if len(unused) != 1 || unused[0].Path != "selection#/components/2/stack" ||
		unused[0].Message != "/components/2: stack ng-wb is not selected" || unused[0].Hint != `did you mean "ng-web"?` {
		t.Fatalf("NAME_OVERRIDE_UNUSED issues = %+v", unused)
	}
}

func TestAnUnselectedStackIsAnUnusedOverride(t *testing.T) {
	repo := fixture.Load(t)
	sel := selection("parameters")
	sel.Schema = 2
	sel.Components = []manifest.Component{{Stack: "ng-web", Name: "acme-web"}, {Stack: "", Name: "acme-srv"}}
	_, issues := plan.Resolve(repo.Catalog, sel)
	unused := issuesAt(issues, plan.CodeNameOverrideUnused)
	if len(unused) != 2 {
		t.Fatalf("issues = %+v", issues)
	}
	if unused[0].Message != "/components/0: stack ng-web is not selected" || unused[0].Hint != "" {
		t.Errorf("unselected stack: %+v", unused[0])
	}
	if unused[1].Message != `/components/1: stack "" is not selected` {
		t.Errorf("empty stack: %+v", unused[1])
	}
}

func TestTwoComponentsWithOneNameConflictOnBothFields(t *testing.T) {
	repo := fixture.Load(t)
	sel := fullstackSelection(
		manifest.Component{Stack: "go-srv", Name: "acme"},
		manifest.Component{Stack: "ng-web", Name: "acme"},
	)
	_, issues := plan.Resolve(repo.Catalog, sel)
	conflicts := issuesAt(issues, plan.CodeNameConflict)
	if len(conflicts) != 2 {
		t.Fatalf("issues = %+v", issues)
	}
	for i, pointer := range []string{"selection#/components/0/name", "selection#/components/1/name"} {
		if conflicts[i].Path != pointer || conflicts[i].Message != `component go-srv and component ng-web both resolve to "acme"` {
			t.Errorf("conflict %d = %+v", i, conflicts[i])
		}
	}
}

func TestANameEqualToAnotherDefaultedFolderConflicts(t *testing.T) {
	repo := fixture.Load(t)
	sel := fullstackSelection(manifest.Component{Stack: "go-srv", Name: "acme-backend-web"})
	_, issues := plan.Resolve(repo.Catalog, sel)
	conflicts := issuesAt(issues, plan.CodeNameConflict)
	paths := []string{}
	for _, found := range conflicts {
		paths = append(paths, found.Path)
	}
	want := []string{"selection#/components/0/name", "selection#/stacks/1"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("conflict paths = %v, want %v (%v)", paths, want, issues)
	}
}

func TestADeviceNameIsReservedAndNeverAlsoAConflictOrFolder(t *testing.T) {
	repo := fixture.Load(t)
	_, issues := plan.Resolve(repo.Catalog, fullstackSelection(manifest.Component{Stack: "ng-web", Name: "com1"}))
	if len(issues) != 1 || issues[0].Code != plan.CodeNameReserved {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestAnInvalidComponentNameIsSkippedByThePlanChecks(t *testing.T) {
	repo := fixture.Load(t)
	_, issues := plan.Resolve(repo.Catalog, fullstackSelection(
		manifest.Component{Stack: "go-srv", Name: "Specs"},
		manifest.Component{Stack: "ng-web", Name: "Specs"}))
	if len(issues) != 0 {
		t.Fatalf("a grammar-invalid name is the decoder's issue, got %+v", issues)
	}
}

func TestTwoEntriesOfOneStackAreAnInternalError(t *testing.T) {
	repo := fixture.Load(t)
	sel := fullstackSelection(
		manifest.Component{Stack: "go-srv", Name: "acme-srv"},
		manifest.Component{Stack: "go-srv", Name: "other-srv"})
	p, issues := plan.Resolve(repo.Catalog, sel)
	if len(issues) != 1 || issues[0].Code != plan.CodeInternalError || issues[0].Path != "selection#/components/1" {
		t.Fatalf("issues = %+v", issues)
	}
	if !reflect.DeepEqual(p, plan.Plan{}) {
		t.Fatalf("plan must be zero: %+v", p)
	}
}

func TestNameTooLong(t *testing.T) {
	long := strings.Repeat("a", 64)
	found, isLong := plan.NameTooLong("selection", "/project/name", "component folder", long)
	if !isLong || found.Code != plan.CodeNameTooLong || found.Path != "selection#/project/name" ||
		found.Message != `derived component folder "`+long+`" is 64 characters (max 63)` || found.Hint != "shorten the name" {
		t.Fatalf("NameTooLong = %+v %v", found, isLong)
	}
	if _, isLong := plan.NameTooLong("selection", "/project/name", "component folder", strings.Repeat("a", 63)); isLong {
		t.Error("63 bytes is the limit and passes")
	}
}

func TestADerivedNameTooLongIsReportedAtTheProjectName(t *testing.T) {
	repo := fixture.Load(t)
	cases := []struct {
		length  int
		wantErr bool
	}{{2, false}, {59, false}, {60, true}, {63, true}}
	for _, tc := range cases {
		sel := fullstackSelection()
		sel.Project.Name = strings.Repeat("a", tc.length)
		p, issues := plan.Resolve(repo.Catalog, sel)
		tooLong := issuesAt(issues, plan.CodeNameTooLong)
		if !tc.wantErr {
			if len(issues) != 0 {
				t.Errorf("%d bytes: issues = %+v", tc.length, issues)
			}
			if got := p.Components[0].Name; got != sel.Project.Name+"-srv" {
				t.Errorf("%d bytes: backend component = %q", tc.length, got)
			}
			continue
		}
		if len(tooLong) != 2 {
			t.Fatalf("%d bytes: want one issue per derived name, got %+v", tc.length, issues)
		}
		for _, found := range tooLong {
			if found.Path != "selection#/project/name" || !strings.HasPrefix(found.Message, "derived component folder ") ||
				!strings.Contains(found.Hint, "shorten the name to 59 characters or fewer, or pass --component ") {
				t.Errorf("%d bytes: issue %+v", tc.length, found)
			}
		}
	}
}

func TestAGivenNameIsNotTooLongBecauseTheProjectIs(t *testing.T) {
	repo := fixture.Load(t)
	sel := fullstackSelection(
		manifest.Component{Stack: "go-srv", Name: "acme-api"},
		manifest.Component{Stack: "ng-web", Name: "acme-ui"})
	sel.Project.Name = strings.Repeat("a", 63)
	if _, issues := plan.Resolve(repo.Catalog, sel); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestAnOverrideEqualToTheOtherDefaultConflictsOnBothFields(t *testing.T) {
	repo := fixture.Load(t)
	sel := fullstackSelection(manifest.Component{Stack: "ng-web", Name: "acme-backend-srv"})
	_, issues := plan.Resolve(repo.Catalog, sel)
	paths := []string{}
	for _, found := range issuesAt(issues, plan.CodeNameConflict) {
		paths = append(paths, found.Path)
	}
	want := []string{"selection#/components/0/name", "selection#/stacks/0"}
	sort.Strings(want)
	sort.Strings(paths)
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("conflict paths = %v, want %v", paths, want)
	}
}

func FuzzDefaultComponentName(f *testing.F) {
	f.Add("acme")
	f.Add(strings.Repeat("a", 59))
	f.Add(strings.Repeat("a", 60))
	f.Fuzz(func(t *testing.T, raw string) {
		name := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' {
				return r
			}
			return -1
		}, raw)
		if len(name) < 2 || len(name) > 63 {
			t.Skip()
		}
		repo := fixture.Load(t)
		sel := fullstackSelection()
		sel.Project.Name = name
		p, issues := plan.Resolve(repo.Catalog, sel)
		if len(name)+4 > 63 {
			if len(issuesAt(issues, plan.CodeNameTooLong)) != 2 || len(issues) != 2 {
				t.Fatalf("name of %d bytes: issues = %+v", len(name), issues)
			}
			return
		}
		if len(issues) != 0 {
			t.Fatalf("name %q: issues = %+v", name, issues)
		}
		srv, web := p.Components[0].Name, p.Components[1].Name
		if srv != name+"-srv" || web != name+"-web" || srv == web || !names.ValidLabel(srv) || !names.ValidLabel(web) {
			t.Fatalf("components = %q %q", srv, web)
		}
	})
}
