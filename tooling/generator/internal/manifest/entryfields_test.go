package manifest_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

const entryFieldsJSON = `{"schema":2,"project":{"name":"acme"},"stacks":["go-srv","ng-web"],"components":[
 {"stack":"go-srv","name":"acme-srv","modules":["tenancy","api-keys"],"goModule":"example.com/acme/srv",
  "database":{"name":"acme_db","user":"acme_u"},"services":{"postgres":"acme-pg"}},
 {"stack":"ng-web","name":"acme-web","backend":"acme-srv"}]}`

const entryFieldsYAML = `schema: 2
project:
  name: acme
stacks: [go-srv, ng-web]
components:
  - stack: go-srv
    name: acme-srv
    modules: [tenancy, api-keys]
    goModule: example.com/acme/srv
    database:
      name: acme_db
      user: acme_u
    services:
      postgres: acme-pg
  - stack: ng-web
    name: acme-web
    backend: acme-srv
`

func wantEntries() []manifest.Component {
	return []manifest.Component{
		{Stack: "go-srv", Name: "acme-srv", Modules: []string{"tenancy", "api-keys"}, HasModules: true,
			GoModule: "example.com/acme/srv", Database: &manifest.Database{Name: "acme_db", User: "acme_u"},
			Services: map[string]string{"postgres": "acme-pg"}},
		{Stack: "ng-web", Name: "acme-web", Backend: "acme-srv"},
	}
}

func TestDecodeInputReadsEntryFieldsFromJSON(t *testing.T) {
	in, issues := decode(t, "acme.json", entryFieldsJSON)
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	if !reflect.DeepEqual(in.Selection.Components, wantEntries()) {
		t.Fatalf("components: %+v", in.Selection.Components)
	}
	if origin := in.Origins["/components/0/modules/1"]; origin.Pointer != "/components/0/modules/1" {
		t.Errorf("origin of a module id: %+v", origin)
	}
}

func TestDecodeInputReadsEntryFieldsFromYAML(t *testing.T) {
	tree, parseIssues := manifest.ParseYAML("acme.yaml", []byte(entryFieldsYAML))
	if len(parseIssues) != 0 {
		t.Fatalf("parse: %v", parseIssues)
	}
	in, issues := manifest.DecodeInput("acme.yaml", tree)
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	if !reflect.DeepEqual(in.Selection.Components, wantEntries()) {
		t.Fatalf("components: %+v", in.Selection.Components)
	}
}

func TestDecodeInputEntryValuesFollowTheGrammar(t *testing.T) {
	cases := map[string]struct {
		entry   string
		pointer string
	}{
		"module id":        {`{"stack":"go-srv","modules":["Bad"]}`, "#/components/0/modules/0"},
		"go module":        {`{"stack":"go-srv","goModule":"x"}`, "#/components/0/goModule"},
		"database name":    {`{"stack":"go-srv","database":{"name":"Bad-Name"}}`, "#/components/0/database/name"},
		"database user":    {`{"stack":"go-srv","database":{"user":"Bad"}}`, "#/components/0/database/user"},
		"service value":    {`{"stack":"go-srv","services":{"postgres":"Bad_Name"}}`, "#/components/0/services/postgres"},
		"backend":          {`{"stack":"ng-web","backend":"Bad_Name"}`, "#/components/0/backend"},
		"modules a map":    {`{"stack":"go-srv","modules":{}}`, "#/components/0/modules"},
		"unknown subkey":   {`{"stack":"go-srv","database":{"host":"x"}}`, "#/components/0/database/host"},
		"modules a scalar": {`{"stack":"go-srv","modules":"core"}`, "#/components/0/modules"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, issues := decode(t, "acme.json", `{"schema":2,"components":[`+tc.entry+`]}`)
			found := false
			for _, got := range issues {
				found = found || got.Path == "acme.json"+tc.pointer
			}
			if !found {
				t.Fatalf("want an issue at %s, got %v", tc.pointer, issues)
			}
		})
	}
}

func TestDecodeInputSchema1RefusesEntryFields(t *testing.T) {
	_, issues := decode(t, "acme.json", `{"schema":1,"project":{"name":"acme"},"stacks":["go-srv"],"modules":[],"components":[{"stack":"go-srv","modules":[]}]}`)
	if issueAt(issues, "acme.json#/components", "needs schema 2") == nil {
		t.Fatalf("issues: %v", issues)
	}
}

func TestDecodeInputAcceptsEmptyComponentsAndStacks(t *testing.T) {
	in, issues := decode(t, "acme.json", `{"schema":2,"stacks":[],"components":[]}`)
	if len(issues) != 0 || !in.Selection.Has.Stacks || len(in.Selection.Components) != 0 {
		t.Fatalf("issues %v input %+v", issues, in.Selection)
	}
}

func TestEmptyModulesListDiffersFromAbsent(t *testing.T) {
	in, issues := decode(t, "acme.json", `{"schema":2,"components":[{"stack":"go-srv","modules":[]},{"stack":"ng-web"}]}`)
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	if !in.Selection.Components[0].HasModules || in.Selection.Components[1].HasModules {
		t.Fatalf("components: %+v", in.Selection.Components)
	}
	encoded, err := json.Marshal(in.Selection.Components)
	if err != nil || string(encoded) != `[{"stack":"go-srv","modules":[]},{"stack":"ng-web"}]` {
		t.Fatalf("encoded %s (%v)", encoded, err)
	}
	var back []manifest.Component
	if err := json.Unmarshal(encoded, &back); err != nil || !reflect.DeepEqual(back, in.Selection.Components) {
		t.Fatalf("decoded %+v (%v)", back, err)
	}
}

func TestComponentUnmarshalIsStrict(t *testing.T) {
	var component manifest.Component
	if err := json.Unmarshal([]byte(`{"stack":"go-srv","other":1}`), &component); err == nil {
		t.Fatal("an unknown member must be refused")
	}
}

func flagEntry(index int, arg string, entry manifest.Component) manifest.ComponentFlag {
	return manifest.ComponentFlag{Index: index, Arg: arg, Entry: entry}
}

func TestFlagsInputChecksEntryValuesAtTheFlag(t *testing.T) {
	_, issues := manifest.FlagsInput(manifest.Flags{Components: []manifest.ComponentFlag{
		flagEntry(0, "go-srv=acme,modules=Bad,go-module=x,db-name=Bad-Db,service.postgres=Bad_Svc", manifest.Component{
			Stack: "go-srv", Name: "acme", Modules: []string{"Bad"}, HasModules: true, GoModule: "x",
			Database: &manifest.Database{Name: "Bad-Db"}, Services: map[string]string{"postgres": "Bad_Svc"}}),
	}})
	want := map[string]bool{
		"flags#/components/0/modules/0": false, "flags#/components/0/goModule": false,
		"flags#/components/0/database/name": false, "flags#/components/0/services/postgres": false,
	}
	for _, got := range issues {
		if _, expected := want[got.Path]; expected {
			want[got.Path] = true
		}
	}
	for path, seen := range want {
		if !seen {
			t.Errorf("no issue at %s: %v", path, issues)
		}
	}
}

func fileWithEntries(entries ...manifest.Component) *manifest.Input {
	in := manifest.Input{
		Selection: manifest.Selection{Schema: 2, Components: entries},
		Origins:   map[string]manifest.Origin{"": {Source: "acme.yaml"}},
	}
	for i := range entries {
		pointer := "/components/" + string(rune('0'+i))
		in.Origins[pointer] = manifest.Origin{Source: "acme.yaml", Pointer: pointer, Line: 4 + i}
	}
	return &in
}

func flagsOf(entries ...manifest.Component) manifest.Input {
	var flags []manifest.ComponentFlag
	for i, entry := range entries {
		flags = append(flags, flagEntry(i, entry.Stack+"="+entry.Name, entry))
	}
	in, _ := manifest.FlagsInput(manifest.Flags{Components: flags})
	return in
}

func TestMergeKeyMergeGivesAMatchedEntryTheFlagKeys(t *testing.T) {
	file := fileWithEntries(
		manifest.Component{Stack: "go-srv", Name: "acme-api", Modules: []string{"tenancy"}, HasModules: true,
			Database: &manifest.Database{Name: "file_db", User: "file_u"}, Services: map[string]string{"postgres": "file-pg", "kratos": "file-kr"}},
		manifest.Component{Stack: "go-srv", Name: "acme-billing"},
	)
	flag := manifest.Component{Stack: "go-srv", Name: "acme-api", Modules: []string{}, HasModules: true,
		GoModule: "example.com/acme/api", Database: &manifest.Database{Name: "flag_db"}, Services: map[string]string{"postgres": "flag-pg"}}
	merged, issues := manifest.Merge(file, flagsOf(flag))
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	want := []manifest.Component{
		{Stack: "go-srv", Name: "acme-api", Modules: []string{}, HasModules: true, GoModule: "example.com/acme/api",
			Database: &manifest.Database{Name: "flag_db", User: "file_u"},
			Services: map[string]string{"postgres": "flag-pg", "kratos": "file-kr"}},
		{Stack: "go-srv", Name: "acme-billing"},
	}
	if !reflect.DeepEqual(merged.Selection.Components, want) {
		t.Fatalf("merged: %+v", merged.Selection.Components)
	}
	if origin := merged.Origins["/components/0/database/name"]; origin.Source != manifest.FlagsSource {
		t.Errorf("a flag key keeps the flag origin: %+v", origin)
	}
	if file.Selection.Components[0].Database.Name != "file_db" || file.Selection.Components[0].Services["postgres"] != "file-pg" {
		t.Error("the merge changed the file input")
	}
}

func TestMergeNamesAnUnnamedFlagThatMatchesNothing(t *testing.T) {
	file := fileWithEntries(manifest.Component{Stack: "go-srv", Name: "acme-api"}, manifest.Component{Stack: "go-srv", Name: "acme-billing"})
	_, issues := manifest.Merge(file, flagsOf(manifest.Component{Stack: "go-srv"}))
	if len(issues) != 1 || issues[0].Code != manifest.CodeComponentMergeAmbiguous ||
		!strings.Contains(issues[0].Message, "names no component; acme.yaml has go-srv components (acme-api, acme-billing)") {
		t.Fatalf("issues: %+v", issues)
	}
}

func TestMergeWithNoFlagsKeepsTheFileAndOrder(t *testing.T) {
	entries := []manifest.Component{
		{Stack: "ng-web", Name: "acme-web", Backend: "acme-b"},
		{Stack: "go-srv", Name: "acme-b", Modules: []string{"x"}, HasModules: true},
		{Stack: "go-srv", Name: "acme-a"},
	}
	merged, issues := manifest.Merge(fileWithEntries(entries...), manifest.Input{Origins: map[string]manifest.Origin{}})
	if len(issues) != 0 || !reflect.DeepEqual(merged.Selection.Components, entries) {
		t.Fatalf("merged %+v issues %v", merged.Selection.Components, issues)
	}
}

func TestMergeFlagsDefineTheEntriesOfAStackTheFileLacks(t *testing.T) {
	file := fileWithEntries(manifest.Component{Stack: "ng-web", Name: "acme-web"})
	merged, issues := manifest.Merge(file, flagsOf(
		manifest.Component{Stack: "go-srv", Name: "acme-b"}, manifest.Component{Stack: "go-srv", Name: "acme-a", Modules: []string{"x"}, HasModules: true}))
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	var got []string
	for _, entry := range merged.Selection.Components {
		got = append(got, entry.Name)
	}
	if !reflect.DeepEqual(got, []string{"acme-web", "acme-b", "acme-a"}) {
		t.Fatalf("order: %v", got)
	}
	if origin := merged.Origins["/components/2/modules/0"]; origin.Source != manifest.FlagsSource || origin.Pointer != "/components/1/modules/0" {
		t.Errorf("module origin: %+v", origin)
	}
}

func TestApplyEntryOverridesLetsTheEntryWin(t *testing.T) {
	in := manifest.Input{
		Selection: manifest.Selection{
			Schema:   2,
			Project:  manifest.Project{Name: "acme", GoModule: "example.com/top"},
			Stacks:   []string{"go-srv"},
			Modules:  []string{"top"},
			Database: &manifest.Database{Name: "top_db", User: "top_u"},
			Services: map[string]string{"postgres": "top-pg", "kratos": "top-kr"},
			Components: []manifest.Component{{
				Stack: "go-srv", Name: "acme-srv", Modules: []string{"entry"}, HasModules: true,
				GoModule: "example.com/entry", Database: &manifest.Database{Name: "entry_db"},
				Services: map[string]string{"postgres": "entry-pg"},
			}},
			Has: manifest.Presence{Modules: true, GoModule: true},
		},
		Origins: map[string]manifest.Origin{
			"/modules/0":                  {Source: "top.json"},
			"/components/0/modules/0":     {Source: "entry.json", Pointer: "/components/0/modules/0"},
			"/components/0/database/name": {Source: "entry.json", Pointer: "/components/0/database/name"},
		},
	}
	out := manifest.ApplyEntryOverrides(in)
	s := out.Selection
	if !reflect.DeepEqual(s.Modules, []string{"entry"}) || s.Project.GoModule != "example.com/entry" ||
		s.Database.Name != "entry_db" || s.Database.User != "top_u" ||
		!reflect.DeepEqual(s.Services, map[string]string{"postgres": "entry-pg", "kratos": "top-kr"}) {
		t.Fatalf("effective: %+v database %+v", s, s.Database)
	}
	if out.Origins["/modules/0"].Source != "entry.json" || out.Origins["/database/name"].Source != "entry.json" {
		t.Errorf("origins: %+v", out.Origins)
	}
	if in.Selection.Database.Name != "top_db" || in.Selection.Services["postgres"] != "top-pg" || in.Selection.Modules[0] != "top" {
		t.Error("the override changed the input")
	}
}

func TestApplyEntryOverridesWithoutEntryFieldsChangesNothing(t *testing.T) {
	in := manifest.Input{
		Selection: manifest.Selection{
			Schema: 2, Project: manifest.Project{Name: "acme"}, Stacks: []string{"go-srv"}, Modules: []string{"top"},
			Database:   &manifest.Database{Name: "top_db"},
			Components: []manifest.Component{{Stack: "go-srv", Name: "acme-srv"}},
		},
		Origins: map[string]manifest.Origin{},
	}
	out := manifest.ApplyEntryOverrides(in)
	if !reflect.DeepEqual(out.Selection, in.Selection) {
		t.Fatalf("changed: %+v", out.Selection)
	}
}

func TestRecordedSelectionMovesTheInstalledListIntoTheEntry(t *testing.T) {
	in := manifest.Input{Selection: manifest.Selection{
		Schema: 2, Project: manifest.Project{Name: "acme"}, Stacks: []string{"go-srv"},
		Has:        manifest.Presence{Name: true, Stacks: true},
		Components: []manifest.Component{{Stack: "go-srv", Modules: []string{"tenancy"}, HasModules: true}},
	}}
	recorded := manifest.RecordedSelection(in, []string{"core", "tenancy"})
	if recorded.Has.Modules || len(recorded.Modules) != 0 {
		t.Fatalf("top-level list kept: %+v", recorded.Modules)
	}
	if !reflect.DeepEqual(recorded.Components[0].Modules, []string{"core", "tenancy"}) {
		t.Fatalf("entry list: %+v", recorded.Components[0])
	}
	encoded, err := json.Marshal(recorded)
	var top map[string]json.RawMessage
	if err != nil || json.Unmarshal(encoded, &top) != nil {
		t.Fatalf("encoded %s (%v)", encoded, err)
	}
	if _, has := top["modules"]; has {
		t.Fatalf("top-level modules key emitted: %s", encoded)
	}
	if !strings.Contains(string(top["components"]), `"modules":["core","tenancy"]`) {
		t.Fatalf("entry list missing: %s", encoded)
	}
	if in.Selection.Components[0].Modules[0] != "tenancy" {
		t.Error("the recording changed the input entry")
	}
}

func TestRecordedSelectionKeepsTheTopLevelListWhenNoEntryHasOne(t *testing.T) {
	in := manifest.Input{Selection: manifest.Selection{
		Schema: 2, Project: manifest.Project{Name: "acme"}, Stacks: []string{"go-srv"},
		Has:        manifest.Presence{Name: true, Stacks: true},
		Components: []manifest.Component{{Stack: "go-srv", Name: "acme-srv"}},
	}}
	recorded := manifest.RecordedSelection(in, []string{"core"})
	if !recorded.Has.Modules || !reflect.DeepEqual(recorded.Modules, []string{"core"}) || recorded.Components[0].HasModules {
		t.Fatalf("recorded: %+v", recorded)
	}
}
