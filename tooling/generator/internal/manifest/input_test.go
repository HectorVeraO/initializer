package manifest_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

// decode parses and decodes a selection document.
func decode(t *testing.T, source, document string) (manifest.Input, []issue.Issue) {
	t.Helper()
	tree, issues := manifest.ParseJSON(source, []byte(document))
	if len(issues) > 0 {
		t.Fatalf("parse: %v", issues)
	}
	return manifest.DecodeInput(source, tree)
}

func issueAt(issues []issue.Issue, path, messagePart string) *issue.Issue {
	for i := range issues {
		if issues[i].Path == path && strings.Contains(issues[i].Message, messagePart) {
			return &issues[i]
		}
	}
	return nil
}

func TestDecodeInputRules(t *testing.T) {
	cases := []struct {
		name     string
		document string
		path     string
		message  string
		hint     string
	}{
		{"1 schema missing", `{"project":{"name":"acme"},"stacks":["go-srv"],"modules":[]}`, "#/schema", "must be 1 or 2", ""},
		{"1 schema is a string", `{"schema":"1"}`, "#/schema", "must be 1 or 2", ""},
		{"1 schema is a float", `{"schema":1.0}`, "#/schema", "must be 1 or 2", ""},
		{"1 schema zero", `{"schema":0}`, "#/schema", "must be 1 or 2", ""},
		{"1 schema three", `{"schema":3}`, "#/schema", "must be 1 or 2", ""},
		{"2 key needs schema 2", `{"schema":1,"components":[]}`, "#/components", "components needs schema 2", `set "schema": 2`},
		{"3 unknown key with a close match", `{"schema":2,"stakcs":[]}`, "#/stakcs", "unknown key", `did you mean "stacks"?`},
		{"3 unknown nested key", `{"schema":2,"project":{"nmae":"x"}}`, "#/project/nmae", "unknown key", `did you mean "name"?`},
		{"3 unknown key without a match", `{"schema":2,"zzzzzzzz":1}`, "#/zzzzzzzz", "unknown key", ""},
		{"4 schema 1 requires project", `{"schema":1,"stacks":["go-srv"],"modules":[]}`, "#/project", "required", ""},
		{"4 schema 1 requires the name", `{"schema":1,"project":{},"stacks":["go-srv"],"modules":[]}`, "#/project/name", "required", ""},
		{"4 schema 1 requires stacks", `{"schema":1,"project":{"name":"acme"},"modules":[]}`, "#/stacks", "required", ""},
		{"4 schema 1 requires modules", `{"schema":1,"project":{"name":"acme"},"stacks":["go-srv"]}`, "#/modules", "required", ""},
		{"4 an entry requires its stack", `{"schema":2,"components":[{"name":"acme-srv"}]}`, "#/components/0/stack", "required", ""},
		{"5 project is a list", `{"schema":2,"project":[]}`, "#/project", "must be an object", ""},
		{"5 name is an int", `{"schema":2,"project":{"name":7}}`, "#/project/name", "must be a string", ""},
		{"5 name is null", `{"schema":2,"project":{"name":null}}`, "#/project/name", "must be a string", ""},
		{"5 stacks is a string", `{"schema":2,"stacks":"go-srv"}`, "#/stacks", "must be a list of strings", ""},
		{"5 module is a bool", `{"schema":2,"modules":[true]}`, "#/modules/0", "must be a string", ""},
		{"5 components is a map", `{"schema":2,"components":{}}`, "#/components", "must be a list of objects", ""},
		{"5 entry is a string", `{"schema":2,"components":["go-srv"]}`, "#/components/0", "must be an object", ""},
		{"6 name grammar", `{"schema":2,"project":{"name":"Acme"}}`, "#/project/name", "must be lowercase", ""},
		{"6 go module grammar", `{"schema":2,"project":{"goModule":"Example.com/x"}}`, "#/project/goModule", "Go module path", ""},
		{"6 stack id", `{"schema":2,"stacks":["rust-srv"]}`, "#/stacks/0", "not in the stack table", ""},
		{"6 module id", `{"schema":2,"modules":["Bad"]}`, "#/modules/0", "module id", ""},
		{"6 component name", `{"schema":2,"components":[{"stack":"go-srv","name":"a/b"}]}`, "#/components/0/name", "must be lowercase", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, issues := decode(t, "acme.json", tc.document)
			found := issueAt(issues, "acme.json"+tc.path, tc.message)
			if found == nil {
				t.Fatalf("want %q at %s, got %v", tc.message, tc.path, issues)
			}
			if found.Hint != tc.hint {
				t.Errorf("hint = %q, want %q", found.Hint, tc.hint)
			}
		})
	}
}

func TestDecodeInputReportsEveryProblemWithItsOrigin(t *testing.T) {
	_, issues := decode(t, "acme.json", `{
  "schema": 1,
  "project": {"name": "Acme", "goModule": "x"},
  "stacks": ["go-srv", "rust-srv"],
  "modules": ["Bad", 7],
  "extra": true
}`)
	want := []string{
		"acme.json#/extra", "acme.json#/modules/0", "acme.json#/modules/1", "acme.json#/project/goModule",
		"acme.json#/project/name", "acme.json#/stacks/1",
	}
	var got []string
	for _, found := range issues {
		got = append(got, found.Path)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
}

func TestDecodeInputKeepsTheInputAsGiven(t *testing.T) {
	in, issues := decode(t, "acme.json", `{"schema":2,"project":{"name":"acme"},"stacks":[],"components":[{"stack":"go-srv"},{"stack":"ng-web","name":"acme-web"}]}`)
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	s := in.Selection
	wantComponents := []manifest.Component{{Stack: "go-srv"}, {Stack: "ng-web", Name: "acme-web"}}
	if s.Schema != 2 || s.Project.Name != "acme" || !s.Has.Name || s.Has.GoModule || !s.Has.Stacks || s.Has.Modules ||
		!reflect.DeepEqual(s.Components, wantComponents) {
		t.Fatalf("selection = %+v", s)
	}
	if origin := in.Origins["/components/1/name"]; origin.Source != "acme.json" || origin.Pointer != "/components/1/name" {
		t.Errorf("origin = %+v", origin)
	}
}

func TestDecodeInputAcceptsASchema1FileWithoutGoModule(t *testing.T) {
	in, issues := decode(t, "acme.json", `{"schema":1,"project":{"name":"acme"},"stacks":["go-srv"],"modules":[]}`)
	if len(issues) != 0 || in.Selection.Has.GoModule {
		t.Fatalf("issues %v selection %+v", issues, in.Selection)
	}
}

func TestDecodeInputNeverAcceptsNull(t *testing.T) {
	for _, document := range []string{
		`{"schema":null}`,
		`{"schema":2,"project":null}`,
		`{"schema":2,"stacks":[null]}`,
		`{"schema":2,"components":[null]}`,
	} {
		if _, issues := decode(t, "acme.json", document); len(issues) == 0 {
			t.Errorf("%s was accepted", document)
		}
	}
}

func TestDecodeInputOfANonObjectRoot(t *testing.T) {
	for _, document := range []string{`[]`, `"x"`, `7`, `null`} {
		_, issues := decode(t, "acme.json", document)
		if issueAt(issues, "acme.json#", "must be an object") == nil {
			t.Errorf("%s: %v", document, issues)
		}
	}
}

func TestCheckMergedCountsStacksOnlyWhenGiven(t *testing.T) {
	cases := map[string]struct {
		stacks  string
		message string
	}{
		"no backend":       {`["ng-web"]`, "exactly one backend"},
		"two backends":     {`["go-srv","go-srv"]`, "exactly one backend"},
		"two frontends":    {`["go-srv","ng-web","ng-web"]`, "at most one frontend"},
		"empty list given": {`[]`, "exactly one backend"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			in, issues := decode(t, "acme.json", `{"schema":2,"stacks":`+tc.stacks+`}`)
			if len(issues) != 0 {
				t.Fatalf("issues: %v", issues)
			}
			if got := manifest.CheckMerged(in); issueAt(got, "acme.json#/stacks", tc.message) == nil {
				t.Fatalf("want %q, got %v", tc.message, got)
			}
		})
	}
	in, _ := decode(t, "acme.json", `{"schema":2}`)
	if got := manifest.CheckMerged(in); len(got) != 0 {
		t.Fatalf("absent stacks are defaulted later: %v", got)
	}
}

func TestFlagsInputReportsGrammarAtTheFlags(t *testing.T) {
	_, issues := manifest.FlagsInput(manifest.Flags{
		Name: "A", GoModule: "x", Stacks: []string{"rust-srv"}, Modules: []string{"Bad"},
		Has: manifest.FlagPresence{Name: true, GoModule: true, Stacks: true, Modules: true},
		Components: []manifest.ComponentFlag{
			{Index: 0, Arg: "go-srv=Web_", Entry: manifest.Component{Stack: "go-srv", Name: "Web_"}},
		},
	})
	for _, path := range []string{
		"flags#/project/name", "flags#/project/goModule", "flags#/stacks/0", "flags#/modules/0", "flags#/components/0",
	} {
		if issueAt(issues, path, "") == nil {
			t.Errorf("missing issue at %s: %v", path, issues)
		}
	}
	if len(issues) != 5 {
		t.Errorf("want 5 issues, got %v", issues)
	}
}

func TestFlagsInputDistinguishesAnEmptyListFromAnAbsentOne(t *testing.T) {
	absent, _ := manifest.FlagsInput(manifest.Flags{})
	empty, _ := manifest.FlagsInput(manifest.Flags{Has: manifest.FlagPresence{Modules: true}})
	if absent.Selection.Has.Modules || !empty.Selection.Has.Modules || len(empty.Selection.Modules) != 0 {
		t.Fatalf("absent %+v empty %+v", absent.Selection.Has, empty.Selection.Has)
	}
}

// merge merges a file document with flags.
func merge(t *testing.T, document string, flags manifest.Flags) (manifest.Input, []issue.Issue) {
	t.Helper()
	var file *manifest.Input
	if document != "" {
		in, issues := decode(t, "acme.json", document)
		if len(issues) > 0 {
			t.Fatalf("file issues: %v", issues)
		}
		file = &in
	}
	fromFlags, issues := manifest.FlagsInput(flags)
	if len(issues) > 0 {
		t.Fatalf("flag issues: %v", issues)
	}
	return manifest.Merge(file, fromFlags)
}

const fullFile = `{"schema":1,"project":{"name":"acme","goModule":"example.com/acme"},"stacks":["go-srv"],"modules":["identity","tenancy"]}`

func TestMergePrecedencePerField(t *testing.T) {
	cases := []struct {
		name  string
		flags manifest.Flags
		check func(t *testing.T, s manifest.Selection)
	}{
		{"no flags keeps the file", manifest.Flags{}, func(t *testing.T, s manifest.Selection) {
			if s.Project.Name != "acme" || s.Project.GoModule != "example.com/acme" ||
				!reflect.DeepEqual(s.Stacks, []string{"go-srv"}) || !reflect.DeepEqual(s.Modules, []string{"identity", "tenancy"}) {
				t.Errorf("selection = %+v", s)
			}
		}},
		{"name flag wins", manifest.Flags{Name: "other", Has: manifest.FlagPresence{Name: true}}, func(t *testing.T, s manifest.Selection) {
			if s.Project.Name != "other" || s.Project.GoModule != "example.com/acme" {
				t.Errorf("project = %+v", s.Project)
			}
		}},
		{"go module flag wins", manifest.Flags{GoModule: "example.com/other", Has: manifest.FlagPresence{GoModule: true}}, func(t *testing.T, s manifest.Selection) {
			if s.Project.GoModule != "example.com/other" || s.Project.Name != "acme" {
				t.Errorf("project = %+v", s.Project)
			}
		}},
		{"stacks flag replaces the list", manifest.Flags{Stacks: []string{"go-srv", "ng-web"}, Has: manifest.FlagPresence{Stacks: true}}, func(t *testing.T, s manifest.Selection) {
			if !reflect.DeepEqual(s.Stacks, []string{"go-srv", "ng-web"}) {
				t.Errorf("stacks = %v", s.Stacks)
			}
		}},
		{"modules flag replaces the list", manifest.Flags{Modules: []string{"parameters"}, Has: manifest.FlagPresence{Modules: true}}, func(t *testing.T, s manifest.Selection) {
			if !reflect.DeepEqual(s.Modules, []string{"parameters"}) {
				t.Errorf("modules = %v", s.Modules)
			}
		}},
		{"an empty modules flag gives the empty list", manifest.Flags{Has: manifest.FlagPresence{Modules: true}}, func(t *testing.T, s manifest.Selection) {
			if len(s.Modules) != 0 || !s.Has.Modules {
				t.Errorf("modules = %v given %v", s.Modules, s.Has.Modules)
			}
		}},
		{"all-modules replaces the file's list", manifest.Flags{AllModules: true}, func(t *testing.T, s manifest.Selection) {
			if !s.AllModules || len(s.Modules) != 0 {
				t.Errorf("selection = %+v", s)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			merged, issues := merge(t, fullFile, tc.flags)
			if len(issues) != 0 {
				t.Fatalf("issues: %v", issues)
			}
			tc.check(t, merged.Selection)
		})
	}
}

func TestMergeOriginsFollowTheValue(t *testing.T) {
	merged, _ := merge(t, fullFile, manifest.Flags{
		Stacks: []string{"go-srv", "ng-web"}, Has: manifest.FlagPresence{Stacks: true},
	})
	cases := map[string]string{
		"/project/name": "acme.json#/project/name",
		"/modules/1":    "acme.json#/modules/1",
		"/stacks/1":     "flags#/stacks/1",
	}
	for pointer, want := range cases {
		if got, _, found := merged.OriginPath(pointer); !found || got != want {
			t.Errorf("origin of %s = %q (%v), want %q", pointer, got, found, want)
		}
	}
}

func TestMergeWithoutAFileIsTheFlags(t *testing.T) {
	merged, issues := merge(t, "", manifest.Flags{Name: "acme", Has: manifest.FlagPresence{Name: true}})
	if len(issues) != 0 || merged.Selection.Project.Name != "acme" || merged.Selection.Schema != 1 {
		t.Fatalf("merged %+v issues %v", merged.Selection, issues)
	}
}

func componentFlags(args ...string) []manifest.ComponentFlag {
	var flags []manifest.ComponentFlag
	for i, arg := range args {
		stack, name, _ := strings.Cut(arg, "=")
		flags = append(flags, manifest.ComponentFlag{Index: i, Arg: arg, Entry: manifest.Component{Stack: stack, Name: name}})
	}
	return flags
}

const fileWithComponents = `{"schema":2,"stacks":["go-srv","ng-web"],"components":[` +
	`{"stack":"go-srv","name":"acme-billing"},{"stack":"go-srv","name":"acme-catalog"},{"stack":"ng-web"}]}`

func TestMergeComponentsRules(t *testing.T) {
	t.Run("no flags keeps the file entries", func(t *testing.T) {
		merged, issues := merge(t, fileWithComponents, manifest.Flags{})
		want := []manifest.Component{{Stack: "go-srv", Name: "acme-billing"}, {Stack: "go-srv", Name: "acme-catalog"}, {Stack: "ng-web"}}
		if len(issues) != 0 || !reflect.DeepEqual(merged.Selection.Components, want) {
			t.Fatalf("merged %+v issues %v", merged.Selection.Components, issues)
		}
	})
	t.Run("a stack without file entries takes the flags in flag order", func(t *testing.T) {
		merged, issues := merge(t, `{"schema":2,"stacks":["go-srv","ng-web"]}`, manifest.Flags{Components: componentFlags("ng-web=acme-web", "go-srv=acme-srv")})
		want := []manifest.Component{{Stack: "ng-web", Name: "acme-web"}, {Stack: "go-srv", Name: "acme-srv"}}
		if len(issues) != 0 || !reflect.DeepEqual(merged.Selection.Components, want) {
			t.Fatalf("merged %+v issues %v", merged.Selection.Components, issues)
		}
		if got, _, _ := merged.OriginPath("/components/0/name"); got != "flags#/components/0" {
			t.Errorf("origin = %s", got)
		}
	})
	t.Run("a flag for a stack the file does not list is added", func(t *testing.T) {
		merged, issues := merge(t, fileWithComponents, manifest.Flags{Components: componentFlags("rust-srv=acme-rs")})
		if len(issues) != 0 || len(merged.Selection.Components) != 4 {
			t.Fatalf("merged %+v issues %v", merged.Selection.Components, issues)
		}
	})
	t.Run("flags naming file entries leave them as given", func(t *testing.T) {
		merged, issues := merge(t, fileWithComponents, manifest.Flags{Components: componentFlags("go-srv=acme-catalog")})
		if len(issues) != 0 || len(merged.Selection.Components) != 3 || merged.Selection.Components[0].Name != "acme-billing" {
			t.Fatalf("merged %+v issues %v", merged.Selection.Components, issues)
		}
	})
	t.Run("a flag that names no entry is ambiguous", func(t *testing.T) {
		merged, issues := merge(t, fileWithComponents, manifest.Flags{Components: componentFlags("go-srv=acme-catalog", "go-srv=acme-catlog")})
		if len(issues) != 1 {
			t.Fatalf("issues = %v", issues)
		}
		found := issues[0]
		wantMessage := "--component go-srv=acme-catlog names no go-srv component of acme.json (acme-billing, acme-catalog)"
		wantHint := `did you mean "acme-catalog"? To add, remove or rename a component, edit acme.json`
		if found.Code != "COMPONENT_MERGE_AMBIGUOUS" || found.Path != "flags#/components/1" || found.Message != wantMessage || found.Hint != wantHint {
			t.Fatalf("issue = %+v", found)
		}
		if len(merged.Selection.Components) != 3 {
			t.Errorf("the merge must never append silently: %+v", merged.Selection.Components)
		}
	})
	t.Run("no close match leaves the second sentence alone", func(t *testing.T) {
		_, issues := merge(t, fileWithComponents, manifest.Flags{Components: componentFlags("go-srv=zzzzzzzz")})
		if len(issues) != 1 || issues[0].Hint != "To add, remove or rename a component, edit acme.json" {
			t.Fatalf("issues = %v", issues)
		}
	})
	t.Run("a schema 1 file takes components from flags", func(t *testing.T) {
		merged, issues := merge(t, fullFile, manifest.Flags{Components: componentFlags("go-srv=acme-srv")})
		if len(issues) != 0 || merged.Selection.Schema != 2 {
			t.Fatalf("merged %+v issues %v", merged.Selection, issues)
		}
	})
}

func TestResolveDefaultsName(t *testing.T) {
	wd := filepath.Join(t.TempDir(), "workspace")
	given, _ := manifest.FlagsInput(manifest.Flags{Name: "flagged", Has: manifest.FlagPresence{Name: true}})
	cases := []struct {
		name      string
		input     manifest.Input
		dir       string
		wantName  string
		wantNotes []string
		wantOut   string
	}{
		{"flag beats the folder", given, "folder", "flagged", []string{"Go module defaults to example.com/flagged; pass --go-module to set yours"}, filepath.Join(wd, "folder")},
		{"folder names the project", manifest.Input{}, "acme", "acme",
			[]string{`project name "acme" comes from the folder; pass --name to set it`, "Go module defaults to example.com/acme; pass --go-module to set yours"},
			filepath.Join(wd, "acme")},
		{"dot is the working directory", manifest.Input{}, ".", "workspace",
			[]string{`project name "workspace" comes from the folder; pass --name to set it`, "Go module defaults to example.com/workspace; pass --go-module to set yours"},
			wd},
		{"demo without name or folder", manifest.Input{}, "", "demo",
			[]string{`no name given; using "demo"`, "Go module defaults to example.com/demo; pass --go-module to set yours"},
			filepath.Join(wd, "demo")},
		{"absolute folder", manifest.Input{}, filepath.Join(wd, "abs", "shop"), "shop",
			[]string{`project name "shop" comes from the folder; pass --name to set it`, "Go module defaults to example.com/shop; pass --go-module to set yours"},
			filepath.Join(wd, "abs", "shop")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolved, notes, out, issues := manifest.ResolveDefaults(tc.input, tc.dir, wd, []string{"go-srv"})
			if len(issues) != 0 {
				t.Fatalf("issues: %v", issues)
			}
			if resolved.Selection.Project.Name != tc.wantName || !reflect.DeepEqual(notes, tc.wantNotes) || out != tc.wantOut {
				t.Fatalf("name %q notes %q out %q", resolved.Selection.Project.Name, notes, out)
			}
		})
	}
}

func TestResolveDefaultsNameFromFile(t *testing.T) {
	in, _ := decode(t, "acme.json", `{"schema":2,"project":{"name":"filed"}}`)
	resolved, _, out, issues := manifest.ResolveDefaults(in, "", "/w", []string{"go-srv"})
	if len(issues) != 0 || resolved.Selection.Project.Name != "filed" || filepath.Base(out) != "filed" {
		t.Fatalf("resolved %+v out %s issues %v", resolved.Selection.Project, out, issues)
	}
}

func TestResolveDefaultsRefusesAFolderThatIsNotAProjectName(t *testing.T) {
	_, _, _, issues := manifest.ResolveDefaults(manifest.Input{}, "My App", "/w", []string{"go-srv"})
	if len(issues) != 1 {
		t.Fatalf("issues = %v", issues)
	}
	found := issues[0]
	wantMessage := `name "My App" comes from the folder "My App" and is not a project name`
	if found.Code != manifest.CodeNameInvalid || found.Path != "args#/dir" || found.Message != wantMessage || found.Hint != "pass --name <name>" {
		t.Fatalf("issue = %+v", found)
	}
}

func TestResolveDefaultsStacks(t *testing.T) {
	t.Run("from the components in first-appearance order", func(t *testing.T) {
		in, _ := decode(t, "acme.json", `{"schema":2,"components":[{"stack":"ng-web","name":"acme-web"},{"stack":"go-srv"}]}`)
		resolved, _, _, issues := manifest.ResolveDefaults(in, "acme", "/w", []string{"go-srv"})
		if len(issues) != 0 || !reflect.DeepEqual(resolved.Selection.Stacks, []string{"ng-web", "go-srv"}) {
			t.Fatalf("stacks %v issues %v", resolved.Selection.Stacks, issues)
		}
		if got, _, _ := resolved.OriginPath("/stacks/0"); got != "acme.json#/components/0" {
			t.Errorf("origin = %s", got)
		}
	})
	t.Run("the one backend stack", func(t *testing.T) {
		resolved, _, _, issues := manifest.ResolveDefaults(manifest.Input{}, "acme", "/w", []string{"go-srv"})
		if len(issues) != 0 || !reflect.DeepEqual(resolved.Selection.Stacks, []string{"go-srv"}) {
			t.Fatalf("stacks %v issues %v", resolved.Selection.Stacks, issues)
		}
	})
	for name, backends := range map[string][]string{"no backend stack": nil, "several backend stacks": {"go-srv", "py-srv"}} {
		t.Run(name, func(t *testing.T) {
			_, _, _, issues := manifest.ResolveDefaults(manifest.Input{}, "acme", "/w", backends)
			want := "no default stack: pass --stacks (backend stacks: " + strings.Join(backends, ", ") + ")"
			if len(issues) != 1 || issues[0].Code != manifest.CodeSelectionInvalid || issues[0].Message != want {
				t.Fatalf("issues = %v", issues)
			}
		})
	}
	t.Run("a given list is kept", func(t *testing.T) {
		in, _ := decode(t, "acme.json", `{"schema":2,"stacks":["ng-web"]}`)
		resolved, _, _, _ := manifest.ResolveDefaults(in, "acme", "/w", []string{"go-srv"})
		if !reflect.DeepEqual(resolved.Selection.Stacks, []string{"ng-web"}) {
			t.Fatalf("stacks = %v", resolved.Selection.Stacks)
		}
	})
}

func TestResolveDefaultsGoModule(t *testing.T) {
	t.Run("a stack that needs one gets the default", func(t *testing.T) {
		resolved, _, _, _ := manifest.ResolveDefaults(manifest.Input{}, "acme", "/w", []string{"go-srv"})
		if resolved.Selection.Project.GoModule != "example.com/acme" {
			t.Fatalf("go module = %q", resolved.Selection.Project.GoModule)
		}
	})
	t.Run("a given module is kept", func(t *testing.T) {
		in, _ := decode(t, "acme.json", `{"schema":2,"project":{"goModule":"example.com/mine"}}`)
		resolved, notes, _, _ := manifest.ResolveDefaults(in, "acme", "/w", []string{"go-srv"})
		if resolved.Selection.Project.GoModule != "example.com/mine" || len(notes) != 1 {
			t.Fatalf("go module %q notes %q", resolved.Selection.Project.GoModule, notes)
		}
	})
	t.Run("a project without such a stack gets none and no note", func(t *testing.T) {
		in, _ := decode(t, "acme.json", `{"schema":2,"stacks":["ng-web"]}`)
		resolved, notes, _, _ := manifest.ResolveDefaults(in, "acme", "/w", []string{"go-srv"})
		if resolved.Selection.Project.GoModule != "" || len(notes) != 1 {
			t.Fatalf("go module %q notes %q", resolved.Selection.Project.GoModule, notes)
		}
	})
}

func TestResolveDefaultsFillsModules(t *testing.T) {
	resolved, _, _, _ := manifest.ResolveDefaults(manifest.Input{}, "acme", "/w", []string{"go-srv"})
	if resolved.Selection.Modules == nil || len(resolved.Selection.Modules) != 0 {
		t.Fatalf("modules = %#v", resolved.Selection.Modules)
	}
}

// canonical returns the document as a generic value for key-order-free comparison.
func canonical(t *testing.T, data []byte) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, data)
	}
	return value
}

func encodeIndented(t *testing.T, s manifest.Selection) []byte {
	t.Helper()
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(s); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// Property: every CI selection decodes and records back to the same document,
// and the flags-only input with the same values records the same document.
func TestRecordedSelectionOfEveryCISelection(t *testing.T) {
	for _, path := range ciSelectionPaths(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			in, issues := decodeInputFile(filepath.Base(path), data)
			if len(issues) != 0 {
				t.Fatalf("issues: %v", issues)
			}
			fromFile := manifest.RecordedSelection(in, in.Selection.Modules)
			if !reflect.DeepEqual(canonical(t, encodeIndented(t, fromFile)), canonical(t, data)) {
				t.Fatalf("recorded form differs from the file:\n%s\n%s", encodeIndented(t, fromFile), data)
			}

			s := in.Selection
			flags, flagIssues := manifest.FlagsInput(manifest.Flags{
				Name: s.Project.Name, GoModule: s.Project.GoModule, Stacks: s.Stacks, Modules: s.Modules,
				Has: manifest.FlagPresence{Name: true, GoModule: s.Has.GoModule, Stacks: true, Modules: true},
			})
			if len(flagIssues) != 0 {
				t.Fatalf("flag issues: %v", flagIssues)
			}
			merged, _ := manifest.Merge(nil, flags)
			fromFlags := manifest.RecordedSelection(merged, s.Modules)
			if !reflect.DeepEqual(fromFile, fromFlags) {
				t.Fatalf("file-only %+v differs from flags-only %+v", fromFile, fromFlags)
			}
		})
	}
}

func TestRecordedSelectionRecordsOnlyWhatWasGiven(t *testing.T) {
	cases := []struct {
		name  string
		flags manifest.Flags
		want  string
	}{
		{"nothing given", manifest.Flags{}, `{"schema":2,"project":{},"modules":["core"]}`},
		{"name and stacks make schema 1", manifest.Flags{Name: "acme", Stacks: []string{"ng-web", "go-srv"},
			Has: manifest.FlagPresence{Name: true, Stacks: true}},
			`{"schema":1,"project":{"name":"acme"},"stacks":["go-srv","ng-web"],"modules":["core"]}`},
		{"a component makes schema 2", manifest.Flags{Name: "acme", Stacks: []string{"go-srv"},
			Has:        manifest.FlagPresence{Name: true, Stacks: true},
			Components: componentFlags("go-srv=acme-srv")},
			`{"schema":2,"project":{"name":"acme"},"stacks":["go-srv"],"modules":["core"],"components":[{"stack":"go-srv","name":"acme-srv"}]}`},
		{"stacks without a name make schema 2", manifest.Flags{Stacks: []string{"go-srv"}, Has: manifest.FlagPresence{Stacks: true}},
			`{"schema":2,"project":{},"stacks":["go-srv"],"modules":["core"]}`},
		{"a go module alone", manifest.Flags{GoModule: "example.com/acme", Has: manifest.FlagPresence{GoModule: true}},
			`{"schema":2,"project":{"goModule":"example.com/acme"},"modules":["core"]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flags, issues := manifest.FlagsInput(tc.flags)
			if len(issues) != 0 {
				t.Fatalf("issues: %v", issues)
			}
			merged, _ := manifest.Merge(nil, flags)
			encoded, err := json.Marshal(manifest.RecordedSelection(merged, []string{"core"}))
			if err != nil || string(encoded) != tc.want {
				t.Fatalf("recorded = %s (%v), want %s", encoded, err, tc.want)
			}
		})
	}
}

func TestSelectionMarshalKeepsASchema1ListsAndTheDecodedLockLists(t *testing.T) {
	cases := map[string]manifest.Selection{
		"schema 1 empty lists":                 {Schema: 1, Project: manifest.Project{Name: "acme"}},
		"schema 2 with values and no presence": {Schema: 2, Stacks: []string{"go-srv"}, Modules: []string{"core"}},
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			encoded, err := json.Marshal(s)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatalf("encoded = %s (%v)", encoded, err)
			}
			for key, want := range map[string]int{"stacks": len(s.Stacks), "modules": len(s.Modules)} {
				list, isList := decoded[key].([]any)
				if !isList || len(list) != want {
					t.Errorf("%s = %v in %s, want a list of %d", key, decoded[key], encoded, want)
				}
			}
		})
	}
}

func FuzzDecodeInput(f *testing.F) {
	for _, path := range ciSelectionPaths(f) {
		if data, err := os.ReadFile(path); err == nil {
			f.Add(data)
		}
	}
	f.Add([]byte(`{"schema":2,"components":[{"stack":"go-srv","name":"acme-srv"},null,{"x":1}],"stacks":[1,"go-srv"],"project":[]}`))
	f.Add([]byte(`{"schema":1,"components":[],"unknown":{"a":1},"modules":{}}`))
	f.Add([]byte(`{"schema":"2","project":{"name":null,"goModule":7}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		tree, parseIssues := manifest.ParseJSON("fuzz.json", data)
		if len(parseIssues) > 0 {
			return
		}
		in, issues := manifest.DecodeInput("fuzz.json", tree)
		paths := map[string]bool{}
		for _, found := range issues {
			if found.Code == "" || found.Message == "" || !strings.HasPrefix(found.Path, "fuzz.json#") {
				t.Fatalf("malformed issue: %+v", found)
			}
			if paths[found.Path] {
				t.Fatalf("two issues at %s: %v", found.Path, issues)
			}
			paths[found.Path] = true
		}
		if !sort.SliceIsSorted(issues, func(i, j int) bool { return issues[i].Path < issues[j].Path }) {
			t.Fatalf("issues are not sorted by path: %v", issues)
		}
		if len(issues) == 0 && (in.Selection.Schema < 1 || in.Selection.Schema > manifest.MaxSchema) {
			t.Fatalf("accepted schema %d", in.Selection.Schema)
		}
	})
}
