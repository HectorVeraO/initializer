package manifest_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

func TestDecodeModuleReadsNameRules(t *testing.T) {
	data := mutateFixture(t, "module.valid.json", func(doc map[string]any) {
		doc["names"] = []any{
			map[string]any{"path": ".deploy/local/docker-compose.yml", "text": "name: %{project}"},
			map[string]any{"path": "go-srv/x.go", "text": `x = "%{component.go-srv}"`},
		}
	})
	module, issues := manifest.DecodeModule(modulePath, data)
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	want := []manifest.NameRule{
		{Path: ".deploy/local/docker-compose.yml", Text: "name: %{project}"},
		{Path: "go-srv/x.go", Text: `x = "%{component.go-srv}"`},
	}
	if !reflect.DeepEqual(module.Names, want) {
		t.Fatalf("Names = %+v, want %+v", module.Names, want)
	}
}

func TestDecodeModuleWithoutNamesHasNoRules(t *testing.T) {
	module, issues := manifest.DecodeModule(modulePath, readFixture(t, "module.valid.json"))
	if len(issues) != 0 || len(module.Names) != 0 {
		t.Fatalf("Names = %+v, issues = %v", module.Names, issues)
	}
}

func TestDecodeModuleRefusesBadNameRules(t *testing.T) {
	good := map[string]any{"path": "a/b.yml", "text": "x: %{project}"}
	with := func(rule map[string]any) func(map[string]any) {
		return func(doc map[string]any) { doc["names"] = []any{rule} }
	}
	cases := map[string]struct {
		change func(map[string]any)
		path   string
	}{
		"not an array":     {func(doc map[string]any) { doc["names"] = "x" }, modulePath + "#/names"},
		"not an object":    {func(doc map[string]any) { doc["names"] = []any{"x"} }, modulePath + "#/names/0"},
		"unknown key":      {with(map[string]any{"path": "a", "text": "x", "target": "y"}), modulePath + "#/names/0/target"},
		"missing path":     {with(map[string]any{"text": "x"}), modulePath + "#/names/0/path"},
		"missing text":     {with(map[string]any{"path": "a"}), modulePath + "#/names/0/text"},
		"path not string":  {with(map[string]any{"path": 1, "text": "x"}), modulePath + "#/names/0/path"},
		"text not string":  {with(map[string]any{"path": "a", "text": 1}), modulePath + "#/names/0/text"},
		"directory path":   {with(map[string]any{"path": "a/", "text": "x"}), modulePath + "#/names/0/path"},
		"traversal path":   {with(map[string]any{"path": "../a", "text": "x"}), modulePath + "#/names/0/path"},
		"absolute path":    {with(map[string]any{"path": "/a", "text": "x"}), modulePath + "#/names/0/path"},
		"backslash path":   {with(map[string]any{"path": `a\b`, "text": "x"}), modulePath + "#/names/0/path"},
		"good rule beside": {func(doc map[string]any) { doc["names"] = []any{good, "x"} }, modulePath + "#/names/1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, issues := manifest.DecodeModule(modulePath, mutateFixture(t, "module.valid.json", tc.change))
			if !hasIssue(issues, manifest.CodeManifestInvalid, tc.path) {
				t.Fatalf("want %s at %s, got %v", manifest.CodeManifestInvalid, tc.path, issues)
			}
		})
	}
}

const referenceSource = ".initializer/reference.json"

const validReference = `{
  "schema": 2,
  "project": {"name": "initializer"},
  "stacks": ["go-srv", "ng-web"],
  "modules": [],
  "components": [
    {"stack": "go-srv", "name": "go-srv"},
    {"stack": "ng-web", "name": "ng-web"}
  ]
}`

func TestDecodeReferenceReadsASchema2Selection(t *testing.T) {
	selection, issues := manifest.DecodeReference([]byte(validReference))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if selection.Project.Name != "initializer" || len(selection.Components) != 2 || selection.Components[1].Name != "ng-web" {
		t.Fatalf("selection = %+v", selection)
	}
	if selection.Project.GoModule != "" {
		t.Fatalf("the reference must carry no goModule: %+v", selection)
	}
}

func TestDecodeReferenceRefusesBadDocuments(t *testing.T) {
	cases := map[string]struct {
		text string
		path string
	}{
		"empty":                 {"", referenceSource + "#"},
		"not JSON":              {"{", referenceSource + "#"},
		"trailing data":         {validReference + "{}", referenceSource + "#"},
		"duplicate key":         {strings.Replace(validReference, `"schema": 2,`, `"schema": 2, "schema": 2,`, 1), referenceSource + "#/schema"},
		"schema 1":              {strings.Replace(validReference, `"schema": 2`, `"schema": 1`, 1), referenceSource + "#/schema"},
		"no schema":             {strings.Replace(validReference, `"schema": 2,`, ``, 1), referenceSource + "#/schema"},
		"unknown key":           {strings.Replace(validReference, `"schema": 2,`, `"schema": 2, "bogus": 1,`, 1), referenceSource + "#/bogus"},
		"no project name":       {strings.Replace(validReference, `"project": {"name": "initializer"},`, ``, 1), referenceSource + "#/project/name"},
		"project not a label":   {strings.Replace(validReference, `"name": "initializer"}`, `"name": "Bad_Name"}`, 1), referenceSource + "#/project/name"},
		"component not a label": {strings.Replace(validReference, `"name": "ng-web"`, `"name": "-x"`, 1), referenceSource + "#/components/1/name"},
		"stack not in table":    {strings.Replace(validReference, `"stack": "ng-web"`, `"stack": "rust-srv"`, 1), referenceSource + "#/components/1/stack"},
		"listed stack unknown":  {strings.Replace(validReference, `"stacks": ["go-srv", "ng-web"]`, `"stacks": ["rust-srv"]`, 1), referenceSource + "#/stacks/0"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, issues := manifest.DecodeReference([]byte(tc.text))
			if !hasIssue(issues, "REFERENCE_INVALID", tc.path) {
				t.Fatalf("want REFERENCE_INVALID at %s, got %v", tc.path, issues)
			}
			for _, found := range issues {
				if found.Code != "REFERENCE_INVALID" {
					t.Errorf("issue code %s, want REFERENCE_INVALID", found.Code)
				}
			}
		})
	}
}

func FuzzDecodeReference(f *testing.F) {
	f.Add([]byte(validReference))
	f.Add([]byte(`{"schema": 2}`))
	f.Add([]byte(`{"schema": 2, "project": {"name": "a"}, "components": [{"stack": 1}]}`))
	f.Add([]byte(`[`))
	f.Fuzz(func(t *testing.T, data []byte) {
		selection, issues := manifest.DecodeReference(data)
		if len(issues) == 0 && selection.Project.Name == "" {
			t.Fatalf("accepted a reference without a project name: %q", data)
		}
		for _, found := range issues {
			if found.Code != "REFERENCE_INVALID" {
				t.Fatalf("issue code %s for %q", found.Code, data)
			}
		}
	})
}
