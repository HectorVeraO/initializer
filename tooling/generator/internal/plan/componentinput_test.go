package plan_test

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
)

// inputOf builds a resolved input: project acme, both stacks, the given entries.
func inputOf(components ...manifest.Component) manifest.Input {
	sel := manifest.Selection{
		Schema:     2,
		Project:    manifest.Project{Name: "acme", GoModule: "example.com/acme"},
		Stacks:     []string{"go-srv", "ng-web"},
		Modules:    []string{},
		Components: components,
	}
	return manifest.Input{Selection: sel, Origins: map[string]manifest.Origin{}}
}

func resolveComponents(t *testing.T, in manifest.Input) []issue.Issue {
	t.Helper()
	return plan.ResolveComponents(fixture.Load(t).Catalog, in)
}

// codesAt returns "CODE path" for every issue.
func codesAt(issues []issue.Issue) []string {
	var found []string
	for _, got := range issues {
		found = append(found, got.Code+" "+strings.TrimPrefix(got.Path, "selection#"))
	}
	return found
}

func backend(name string) manifest.Component {
	return manifest.Component{Stack: "go-srv", Name: name}
}

func frontend(name, backendName string) manifest.Component {
	return manifest.Component{Stack: "ng-web", Name: name, Backend: backendName}
}

func TestResolveComponentsAcceptsNamedBackendsBoundToOneFrontend(t *testing.T) {
	in := inputOf(backend("acme-srv-first"), backend("acme-srv-second"), frontend("acme-web", "acme-srv-second"))
	if issues := resolveComponents(t, in); len(issues) != 0 {
		t.Fatalf("issues: %+v", issues)
	}
}

func TestResolveComponentsAcceptsASingleProjectWithoutEntries(t *testing.T) {
	if issues := resolveComponents(t, inputOf()); len(issues) != 0 {
		t.Fatalf("issues: %+v", issues)
	}
}

func TestResolveComponentsReportsEachCode(t *testing.T) {
	cases := []struct {
		name  string
		input manifest.Input
		want  []string
	}{
		{"unnamed entries of a repeated stack",
			inputOf(backend(""), backend(""), frontend("acme-web", "")),
			[]string{"COMPONENT_BACKEND_REQUIRED /components/2", "COMPONENT_NAME_REQUIRED /components/0"}},
		{"frontend needs its backend among several",
			inputOf(backend("a-one"), backend("a-two"), frontend("acme-web", "")),
			[]string{"COMPONENT_BACKEND_REQUIRED /components/2"}},
		{"frontend names an unknown backend",
			inputOf(backend("a-one"), backend("a-two"), frontend("acme-web", "a-thre")),
			[]string{"COMPONENT_BACKEND_UNKNOWN /components/2/backend"}},
		{"single project frontend names another backend",
			inputOf(backend("acme-srv"), frontend("acme-web", "other")),
			[]string{"COMPONENT_BACKEND_UNKNOWN /components/1/backend"}},
		{"single project frontend names its only backend",
			inputOf(backend("acme-srv"), frontend("acme-web", "acme-srv")),
			nil},
		{"modules on a frontend",
			inputOf(backend("acme-srv"), manifest.Component{Stack: "ng-web", Modules: []string{"x"}, HasModules: true}),
			[]string{"COMPONENT_FIELD_NOT_ALLOWED /components/1/modules"}},
		{"database and services on a frontend",
			inputOf(backend("acme-srv"), manifest.Component{Stack: "ng-web", Database: &manifest.Database{Name: "d"}, Services: map[string]string{"k": "v"}}),
			[]string{"COMPONENT_FIELD_NOT_ALLOWED /components/1/database", "COMPONENT_FIELD_NOT_ALLOWED /components/1/services"}},
		{"backend on a backend",
			inputOf(manifest.Component{Stack: "go-srv", Backend: "x"}),
			[]string{"COMPONENT_FIELD_NOT_ALLOWED /components/0/backend"}},
		{"goModule on a stack that needs none",
			inputOf(backend("acme-srv"), manifest.Component{Stack: "ng-web", GoModule: "example.com/x"}),
			[]string{"COMPONENT_FIELD_NOT_ALLOWED /components/1/goModule"}},
		{"two frontends name one backend",
			inputOf(backend("a-one"), backend("a-two"), frontend("w-one", "a-one"), frontend("w-two", "a-one")),
			[]string{"COMPONENT_BACKEND_TAKEN /components/3"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := append([]string(nil), codesAt(resolveComponents(t, tc.input))...)
			want := append([]string(nil), tc.want...)
			sort.Strings(got)
			sort.Strings(want)
			if len(got) != len(want) {
				t.Fatalf("codes %v, want %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("codes %v, want %v", got, want)
				}
			}
		})
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

func TestResolveComponentsHasNoCountLimit(t *testing.T) {
	var entries []manifest.Component
	for i := 0; i < 40; i++ {
		entries = append(entries, backend(fmt.Sprintf("acme-srv-%02d", i)))
	}
	in := inputOf(entries...)
	in.Selection.Stacks = []string{"go-srv"}
	if got := codesAt(resolveComponents(t, in)); len(got) != 0 {
		t.Fatalf("40 backends: %v", got)
	}
}

func TestResolveComponentsSuggestsNames(t *testing.T) {
	cases := []struct {
		name  string
		input manifest.Input
		want  []string
	}{
		{"all unnamed", inputOf(backend(""), backend("")), []string{"acme-srv-first", "acme-srv-second"}},
		{"a named one keeps its name", inputOf(backend("acme-api"), backend("")), []string{"acme-api", "acme-srv-second"}},
		{"a taken suggestion takes the next ordinal", inputOf(backend(""), backend("acme-srv-first")), []string{"acme-srv-second", "acme-srv-first"}},
		{"a taken second takes the third", inputOf(backend(""), backend(""), backend("acme-srv-second")),
			[]string{"acme-srv-first", "acme-srv-third", "acme-srv-second"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.input.Selection.Stacks = []string{"go-srv"}
			required := issuesAt(resolveComponents(t, tc.input), plan.CodeComponentNameRequired)
			if len(required) != 1 {
				t.Fatalf("issues: %+v", required)
			}
			if !reflect.DeepEqual(required[0].SuggestedNames, tc.want) {
				t.Fatalf("suggested %v, want %v", required[0].SuggestedNames, tc.want)
			}
			wantMessage := fmt.Sprintf("stack go-srv has %d components; name each one", len(tc.want))
			if required[0].Message != wantMessage {
				t.Fatalf("message %q", required[0].Message)
			}
		})
	}
}

func TestResolveComponentsShortensASuggestionToSixtyThreeBytes(t *testing.T) {
	in := inputOf(backend(""), backend(""))
	in.Selection.Stacks = []string{"go-srv"}
	in.Selection.Project.Name = strings.Repeat("a", 60) + "-b"
	required := issuesAt(resolveComponents(t, in), plan.CodeComponentNameRequired)
	if len(required) != 1 {
		t.Fatalf("issues: %+v", required)
	}
	for k, name := range required[0].SuggestedNames {
		if len(name) > 63 || !strings.HasSuffix(name, []string{"-srv-first", "-srv-second"}[k]) {
			t.Errorf("suggestion %d = %q (%d bytes)", k, name, len(name))
		}
	}
}

func TestResolveComponentsPointsAtTheFirstUnnamedEntry(t *testing.T) {
	in := inputOf(backend("acme-api"), backend(""), backend(""))
	in.Selection.Stacks = []string{"go-srv"}
	required := issuesAt(resolveComponents(t, in), plan.CodeComponentNameRequired)
	if len(required) != 1 || required[0].Path != "selection#/components/1" {
		t.Fatalf("issues: %+v", required)
	}
}

func TestResolveComponentsAmbiguousDatabase(t *testing.T) {
	top := &manifest.Database{Name: "shared"}
	own := &manifest.Database{Name: "own"}
	cases := []struct {
		name string
		in   manifest.Input
		want bool
	}{
		{"two backends inherit", inputOf(backend("a-one"), backend("a-two")), true},
		{"one inherits, one has its own", inputOf(backend("a-one"), manifest.Component{Stack: "go-srv", Name: "a-two", Database: own}), false},
		{"both have their own", inputOf(manifest.Component{Stack: "go-srv", Name: "a-one", Database: own}, manifest.Component{Stack: "go-srv", Name: "a-two", Database: own}), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.Selection.Stacks = []string{"go-srv"}
			tc.in.Selection.Database = top
			got := len(issuesAt(resolveComponents(t, tc.in), plan.CodeComponentAmbiguous)) == 1
			if got != tc.want {
				t.Fatalf("ambiguous = %v, want %v", got, tc.want)
			}
		})
	}
	single := inputOf(backend("acme-srv"))
	single.Selection.Database = top
	if got := issuesAt(resolveComponents(t, single), plan.CodeComponentAmbiguous); len(got) != 0 {
		t.Fatalf("one backend takes the top-level database: %+v", got)
	}
}

func TestResolveComponentsModulesUnused(t *testing.T) {
	own := func(name string) manifest.Component {
		return manifest.Component{Stack: "go-srv", Name: name, Modules: []string{"parameters"}, HasModules: true}
	}
	cases := []struct {
		name    string
		backend []manifest.Component
		top     []string
		all     bool
		want    string
	}{
		{"every backend has its own list", []manifest.Component{own("a-one"), own("a-two")}, []string{"parameters"}, false, "selection#/modules"},
		{"all-modules with own lists", []manifest.Component{own("a-one")}, nil, true, "flags#/all-modules"},
		{"one backend inherits", []manifest.Component{own("a-one"), backend("a-two")}, []string{"parameters"}, false, ""},
		{"no top-level list", []manifest.Component{own("a-one")}, nil, false, ""},
		{"empty own list still counts", []manifest.Component{{Stack: "go-srv", Name: "a-one", HasModules: true}}, []string{"parameters"}, false, "selection#/modules"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := inputOf(tc.backend...)
			in.Selection.Stacks = []string{"go-srv"}
			in.Selection.Modules, in.Selection.AllModules = tc.top, tc.all
			unused := issuesAt(resolveComponents(t, in), plan.CodeModulesUnused)
			switch {
			case tc.want == "" && len(unused) != 0:
				t.Fatalf("unexpected: %+v", unused)
			case tc.want != "" && (len(unused) != 1 || unused[0].Path != tc.want):
				t.Fatalf("want one at %s, got %+v", tc.want, unused)
			}
		})
	}
}

func TestResolveComponentsTopLevelServicesOfAMultiProjectAreTheSharedOnes(t *testing.T) {
	multi := inputOf(backend("a-one"), backend("a-two"))
	multi.Selection.Stacks = []string{"go-srv"}
	multi.Selection.Services = map[string]string{"postgres": "shared-pg", "postgres-init": "shared-init", "kratos": "one-kratos"}
	unused := issuesAt(resolveComponents(t, multi), plan.CodeNameOverrideUnused)
	if len(unused) != 1 || unused[0].Path != "selection#/services/kratos" || !strings.Contains(unused[0].Hint, "service.kratos=") {
		t.Fatalf("multi: %+v", unused)
	}
	single := inputOf(backend("acme-srv"))
	single.Selection.Services = map[string]string{"postgres": "shared-pg", "kratos": "acme-kratos"}
	if got := issuesAt(resolveComponents(t, single), plan.CodeNameOverrideUnused); len(got) != 0 {
		t.Fatalf("single: %+v", got)
	}
}

func TestResolveComponentsNameConflictsOverGivenNames(t *testing.T) {
	twice := inputOf(backend("acme-api"), backend("acme-api"))
	twice.Selection.Stacks = []string{"go-srv"}
	conflicts := issuesAt(resolveComponents(t, twice), plan.CodeNameConflict)
	if len(conflicts) != 2 {
		t.Fatalf("twice: %+v", conflicts)
	}
	service := inputOf(backend("acme-api"), manifest.Component{Stack: "go-srv", Name: "acme-other", Services: map[string]string{"postgres": "acme-api"}})
	service.Selection.Stacks = []string{"go-srv"}
	if got := issuesAt(resolveComponents(t, service), plan.CodeNameConflict); len(got) != 1 || got[0].Path != "selection#/components/0/name" {
		t.Fatalf("service: %+v", got)
	}
}

func TestResolveComponentsRefusesUnknownEntryServiceKeysInAMultiProject(t *testing.T) {
	in := inputOf(backend("a-one"), manifest.Component{Stack: "go-srv", Name: "a-two", Services: map[string]string{"nokey": "x-svc"}})
	in.Selection.Stacks = []string{"go-srv"}
	unused := issuesAt(resolveComponents(t, in), plan.CodeNameOverrideUnused)
	if len(unused) != 1 || unused[0].Path != "selection#/components/1/services/nokey" {
		t.Fatalf("issues: %+v", unused)
	}
}

func TestResolveComponentsReportsModulesTheCatalogLacks(t *testing.T) {
	in := inputOf(manifest.Component{Stack: "go-srv", Name: "acme-srv", Modules: []string{"parameter"}, HasModules: true})
	in.Selection.Stacks = []string{"go-srv"}
	in.Selection.Modules = []string{"nothing"}
	got := codesAt(resolveComponents(t, in))
	for _, want := range []string{"MODULE_UNKNOWN /components/0/modules/0", "MODULE_UNKNOWN /modules/0"} {
		if !contains(got, want) {
			t.Errorf("missing %q in %v", want, got)
		}
	}
}

func TestResolveComponentsRoleRefusals(t *testing.T) {
	cases := []struct {
		name   string
		stacks []string
		want   []string
	}{
		{"no stack at all", []string{}, []string{"SELECTION_INVALID /stacks"}},
		{"frontend only", []string{"ng-web"}, []string{"SELECTION_INVALID /stacks", "SELECTION_INVALID /stacks/0"}},
		{"stack listed twice", []string{"go-srv", "go-srv"}, []string{"SELECTION_INVALID /stacks/1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := inputOf()
			in.Selection.Stacks = tc.stacks
			got := codesAt(resolveComponents(t, in))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	twice := inputOf()
	twice.Selection.Stacks = []string{"go-srv", "go-srv"}
	listed := resolveComponents(t, twice)
	if listed[0].Message != "stack is listed twice" || listed[0].Hint != "to ask for two go-srv components, repeat --component go-srv=NAME" {
		t.Fatalf("listed twice: %+v", listed[0])
	}
}
