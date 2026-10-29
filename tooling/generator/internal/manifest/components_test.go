package manifest_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
)

// withComponents turns the valid fixture into a selection with the given
// schema and components value.
func withComponents(t *testing.T, schema int, components any) []byte {
	t.Helper()
	return mutateFixture(t, "selection.valid.json", func(m map[string]any) {
		m["schema"] = schema
		m["components"] = components
	})
}

func TestDecodeSelectionSchema2ReadsComponents(t *testing.T) {
	data := withComponents(t, 2, []any{
		map[string]any{"stack": "go-srv", "name": "acme-srv"},
		map[string]any{"stack": "ng-web"},
	})
	selection, issues := decodeFile(t, selectionPath, data)
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	want := []manifest.Component{{Stack: "go-srv", Name: "acme-srv"}, {Stack: "ng-web"}}
	if selection.Schema != 2 || !reflect.DeepEqual(selection.Components, want) {
		t.Fatalf("selection %+v, want schema 2 and components %v", selection, want)
	}
}

func TestDecodeSelectionSchema2WithoutComponentsIsValid(t *testing.T) {
	data := mutateFixture(t, "selection.valid.json", func(m map[string]any) { m["schema"] = 2 })
	selection, issues := decodeFile(t, selectionPath, data)
	if len(issues) != 0 || selection.Schema != 2 || len(selection.Components) != 0 {
		t.Fatalf("selection %+v issues %v", selection, issues)
	}
}

func TestDecodeSelectionComponentProblems(t *testing.T) {
	cases := []struct {
		name       string
		schema     int
		components any
		code, path string
		message    string
	}{
		{"schema 1 refuses components", 1, []any{map[string]any{"stack": "go-srv"}},
			manifest.CodeSelectionInvalid, "#/components", "components needs schema 2"},
		{"components is not an array", 2, "go-srv",
			manifest.CodeSelectionInvalid, "#/components", "must be a list of objects"},
		{"entry is not an object", 2, []any{"go-srv"},
			manifest.CodeSelectionInvalid, "#/components/0", "must be an object"},
		{"entry without stack", 2, []any{map[string]any{"name": "acme-srv"}},
			manifest.CodeSelectionInvalid, "#/components/0/stack", "required"},
		{"stack is not a string", 2, []any{map[string]any{"stack": 7}},
			manifest.CodeSelectionInvalid, "#/components/0/stack", "must be a string"},
		{"name is not a string", 2, []any{map[string]any{"stack": "go-srv", "name": 7}},
			manifest.CodeSelectionInvalid, "#/components/0/name", "must be a string"},
		{"unknown entry key", 2, []any{map[string]any{"stack": "go-srv", "modules": []any{}}},
			manifest.CodeSelectionInvalid, "#/components/0/modules", "unknown key"},
		{"name with upper case", 2, []any{map[string]any{"stack": "go-srv", "name": "Acme_API"}},
			manifest.CodeNameInvalid, "#/components/0/name", `"Acme_API" must be lowercase letters`},
		{"name with a trailing hyphen", 2, []any{map[string]any{"stack": "go-srv", "name": "abc-"}},
			manifest.CodeNameInvalid, "#/components/0/name", `"abc-" must be`},
		{"empty name", 2, []any{map[string]any{"stack": "go-srv", "name": ""}},
			manifest.CodeNameInvalid, "#/components/0/name", `"" must be`},
		{"name with a slash", 2, []any{map[string]any{"stack": "go-srv", "name": "a/b"}},
			manifest.CodeNameInvalid, "#/components/0/name", `"a/b" must be`},
		{"name of dot-dot", 2, []any{map[string]any{"stack": "go-srv", "name": ".."}},
			manifest.CodeNameInvalid, "#/components/0/name", `".." must be`},
		{"name escaping upward", 2, []any{map[string]any{"stack": "go-srv", "name": "../etc"}},
			manifest.CodeNameInvalid, "#/components/0/name", `"../etc" must be`},
		{"name of five digits", 2, []any{map[string]any{"stack": "go-srv", "name": "app10132"}},
			manifest.CodeNameInvalid, "#/components/0/name", "five digits"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, issues := decodeFile(t, selectionPath, withComponents(t, tc.schema, tc.components))
			found := false
			for _, got := range issues {
				isMatch := got.Code == tc.code && got.Path == selectionPath+tc.path && strings.Contains(got.Message, tc.message)
				found = found || isMatch
			}
			if !found {
				t.Fatalf("want %s at %s containing %q, got %v", tc.code, tc.path, tc.message, issues)
			}
		})
	}
}

func TestDecodeSelectionCollectsEveryBadComponentName(t *testing.T) {
	data := withComponents(t, 2, []any{
		map[string]any{"stack": "go-srv", "name": "Bad"},
		map[string]any{"stack": "ng-web", "name": "worse_"},
	})
	_, issues := decodeFile(t, selectionPath, data)
	if !hasIssue(issues, manifest.CodeNameInvalid, selectionPath+"#/components/0/name") ||
		!hasIssue(issues, manifest.CodeNameInvalid, selectionPath+"#/components/1/name") {
		t.Fatalf("want both names reported, got %v", issues)
	}
}

func TestDecodeSelectionKeepsAnUnknownStackForThePlan(t *testing.T) {
	data := withComponents(t, 2, []any{map[string]any{"stack": "rust-srv", "name": "acme-srv"}})
	selection, issues := decodeFile(t, selectionPath, data)
	if len(issues) != 0 || len(selection.Components) != 1 || selection.Components[0].Stack != "rust-srv" {
		t.Fatalf("selection %+v issues %v", selection, issues)
	}
}

func FuzzDecodeSelectionComponents(f *testing.F) {
	f.Add([]byte(`{"schema":2,"project":{"name":"acme-backend","goModule":"example.com/acme/backend"},"stacks":["go-srv"],"modules":[],"components":[{"stack":"go-srv","name":"acme-srv"}]}`))
	f.Add([]byte(`{"schema":2,"project":{"name":"acme-backend","goModule":"example.com/acme/backend"},"stacks":["go-srv"],"modules":[],"components":[{"stack":"go-srv","name":"../x"}]}`))
	f.Add([]byte(`{"schema":2,"project":{"name":"acme-backend","goModule":"example.com/acme/backend"},"stacks":["go-srv"],"modules":[],"components":[{"stack":"go-srv","name":"a/b"}]}`))
	f.Add([]byte(`{"schema":1,"components":[{}]}`))
	f.Add([]byte(`{"schema":2,"components":[{"stack":1,"name":[]},"x",null]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		selection, issues := decodeFile(t, selectionPath, data)
		if len(issues) == 0 && selection.Schema == 1 && len(selection.Components) > 0 {
			t.Fatalf("schema 1 decoded components: %+v", selection)
		}
		if len(issues) == 0 {
			for _, component := range selection.Components {
				if component.Name != "" && !names.ValidLabel(component.Name) {
					t.Fatalf("accepted an invalid component name: %+v", component)
				}
			}
		}
	})
}
