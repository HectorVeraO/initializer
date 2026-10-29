package manifest_test

import (
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

const (
	specPath      = "specs/feature-flags/spec.manifest.json"
	selectionPath = "initializer.selection.json"
)

func TestDecodeSpecValidFixture(t *testing.T) {
	spec, issues := manifest.DecodeSpec(specPath, "feature-flags", readFixture(t, "spec.valid.json"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if spec.Spec != "feature-flags" || len(spec.TCKTags) != 2 || spec.TCKEnv["APP_FLAGS_FILE"] == "" {
		t.Errorf("spec not decoded: %+v", spec)
	}
}

func TestDecodeSpecRules(t *testing.T) {
	cases := []struct {
		name   string
		folder string
		change func(map[string]any)
		path   string
	}{
		{"folder mismatch", "other", func(map[string]any) {}, "#/spec"},
		{"bad id", "feature-flags", func(m map[string]any) { m["spec"] = "Feature" }, "#/spec"},
		{"bad version", "feature-flags", func(m map[string]any) { m["version"] = "1.0" }, "#/version"},
		{"malformed tag", "feature-flags", func(m map[string]any) { m["tckTags"] = []any{"@Nope"} }, "#/tckTags/0"},
		{"tag without at sign", "feature-flags", func(m map[string]any) { m["tckTags"] = []any{"tenancy"} }, "#/tckTags/0"},
		{"bare at sign", "feature-flags", func(m map[string]any) { m["tckTags"] = []any{"@"} }, "#/tckTags/0"},
		{"empty tags", "feature-flags", func(m map[string]any) { m["tckTags"] = []any{} }, "#/tckTags"},
		{"cooperatesWith not an array", "feature-flags", func(m map[string]any) { m["cooperatesWith"] = "@tenancy" }, "#/cooperatesWith"},
		{"cooperatesWith malformed item", "feature-flags", func(m map[string]any) { m["cooperatesWith"] = []any{"Tenancy"} }, "#/cooperatesWith/0"},
		{"cooperatesWith unsorted", "feature-flags", func(m map[string]any) { m["cooperatesWith"] = []any{"@tenancy", "@api-keys"} }, "#/cooperatesWith/1"},
		{"cooperatesWith duplicate", "feature-flags", func(m map[string]any) { m["cooperatesWith"] = []any{"@tenancy", "@tenancy"} }, "#/cooperatesWith/1"},
		{"cooperatesWith own tag", "feature-flags", func(m map[string]any) { m["cooperatesWith"] = []any{"@core"} }, "#/cooperatesWith/0"},
		{"unsorted tags", "feature-flags", func(m map[string]any) { m["tckTags"] = []any{"@otel", "@core"} }, "#/tckTags/1"},
		{"duplicate tags", "feature-flags", func(m map[string]any) { m["tckTags"] = []any{"@core", "@core"} }, "#/tckTags/1"},
		{"lowercase env key", "feature-flags", func(m map[string]any) { m["tckEnv"] = map[string]any{"abc": "x"} }, "#/tckEnv/abc"},
		{"non-string env value", "feature-flags", func(m map[string]any) { m["tckEnv"] = map[string]any{"ABC": 1} }, "#/tckEnv/ABC"},
		{"unknown key", "feature-flags", func(m map[string]any) { m["extra"] = 1 }, "#/extra"},
		{"missing tckEnv", "feature-flags", func(m map[string]any) { delete(m, "tckEnv") }, "#/tckEnv"},
		{"missing tckTags", "feature-flags", func(m map[string]any) { delete(m, "tckTags") }, "#/tckTags"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := mutateFixture(t, "spec.valid.json", tc.change)
			_, issues := manifest.DecodeSpec(specPath, tc.folder, data)
			if !hasIssue(issues, manifest.CodeManifestInvalid, specPath+tc.path) {
				t.Fatalf("want issue at %s, got %v", tc.path, issues)
			}
		})
	}
}

func TestDecodeSpecEmptyEnv(t *testing.T) {
	data := mutateFixture(t, "spec.valid.json", func(m map[string]any) {
		m["tckEnv"] = map[string]any{}
	})
	if _, issues := manifest.DecodeSpec(specPath, "feature-flags", data); len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
}

func TestDecodeSpecAcceptsAnyWellFormedTagAndCooperation(t *testing.T) {
	cases := []struct {
		name string
		tags []any
		coop any
		want []string
	}{
		{"unlisted tag", []any{"@tenancy"}, nil, []string{}},
		{"hyphenated tag", []any{"@api-keys", "@tenancy"}, nil, []string{}},
		{"cooperation sorted", []any{"@core"}, []any{"@api-keys", "@tenancy"}, []string{"@api-keys", "@tenancy"}},
		{"cooperation empty", []any{"@core"}, []any{}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := mutateFixture(t, "spec.valid.json", func(m map[string]any) {
				m["tckTags"] = tc.tags
				if tc.coop != nil {
					m["cooperatesWith"] = tc.coop
				}
			})
			spec, issues := manifest.DecodeSpec(specPath, "feature-flags", data)
			if len(issues) != 0 {
				t.Fatalf("unexpected issues: %v", issues)
			}
			if strings.Join(spec.CooperatesWith, ",") != strings.Join(tc.want, ",") || spec.CooperatesWith == nil {
				t.Errorf("CooperatesWith = %#v, want %#v", spec.CooperatesWith, tc.want)
			}
		})
	}
}

func TestDecodeSelectionValidFixture(t *testing.T) {
	selection, issues := decodeFile(t, selectionPath, readFixture(t, "selection.valid.json"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if selection.Project.Name != "acme-backend" || len(selection.Stacks) != 2 {
		t.Errorf("selection not decoded: %+v", selection)
	}
}

func TestDecodeSelectionReportsEveryProblem(t *testing.T) {
	_, issues := decodeFile(t, selectionPath, readFixture(t, "selection.invalid.json"))
	for _, path := range []string{"#/project/goModule", "#/stacks", "#/modules/0"} {
		if !hasIssue(issues, manifest.CodeSelectionInvalid, selectionPath+path) {
			t.Errorf("missing SELECTION_INVALID at %s: %v", path, issues)
		}
	}
	if !hasIssue(issues, manifest.CodeNameInvalid, selectionPath+"#/project/name") {
		t.Errorf("missing NAME_INVALID at #/project/name: %v", issues)
	}
}

func TestDecodeSelectionRules(t *testing.T) {
	cases := []struct {
		name   string
		change func(map[string]any)
		path   string
		code   string
	}{
		{"name too short", func(m map[string]any) { m["project"].(map[string]any)["name"] = "a" }, "#/project/name", manifest.CodeNameInvalid},
		{"name 64 chars", func(m map[string]any) { m["project"].(map[string]any)["name"] = "a" + strings.Repeat("b", 63) }, "#/project/name", manifest.CodeNameInvalid},
		{"name uppercase", func(m map[string]any) { m["project"].(map[string]any)["name"] = "Acme" }, "#/project/name", manifest.CodeNameInvalid},
		{"name starts with digit", func(m map[string]any) { m["project"].(map[string]any)["name"] = "1ab" }, "#/project/name", manifest.CodeNameInvalid},
		{"goModule uppercase first segment", func(m map[string]any) { m["project"].(map[string]any)["goModule"] = "Example.com/acme" }, "#/project/goModule", manifest.CodeSelectionInvalid},
		{"goModule dotdot segment", func(m map[string]any) { m["project"].(map[string]any)["goModule"] = "example.com/../x" }, "#/project/goModule", manifest.CodeSelectionInvalid},
		{"goModule dot segment", func(m map[string]any) { m["project"].(map[string]any)["goModule"] = "example.com/./x" }, "#/project/goModule", manifest.CodeSelectionInvalid},
		{"goModule empty segment", func(m map[string]any) { m["project"].(map[string]any)["goModule"] = "example.com//x" }, "#/project/goModule", manifest.CodeSelectionInvalid},
		{"goModule leading slash", func(m map[string]any) { m["project"].(map[string]any)["goModule"] = "/example.com/x" }, "#/project/goModule", manifest.CodeSelectionInvalid},
		{"goModule trailing slash", func(m map[string]any) { m["project"].(map[string]any)["goModule"] = "example.com/x/" }, "#/project/goModule", manifest.CodeSelectionInvalid},
		{"goModule first segment no dot", func(m map[string]any) { m["project"].(map[string]any)["goModule"] = "example/x" }, "#/project/goModule", manifest.CodeSelectionInvalid},
		{"goModule bad character", func(m map[string]any) { m["project"].(map[string]any)["goModule"] = "example.com/a b" }, "#/project/goModule", manifest.CodeSelectionInvalid},
		{"two backend stacks", func(m map[string]any) { m["stacks"] = []any{"go-srv", "go-srv"} }, "#/stacks", manifest.CodeSelectionInvalid},
		{"no backend stack", func(m map[string]any) { m["stacks"] = []any{"ng-web"} }, "#/stacks", manifest.CodeSelectionInvalid},
		{"two frontend stacks", func(m map[string]any) { m["stacks"] = []any{"go-srv", "ng-web", "ng-web"} }, "#/stacks", manifest.CodeSelectionInvalid},
		{"unknown stack", func(m map[string]any) { m["stacks"] = []any{"go-srv", "rust-srv"} }, "#/stacks/1", manifest.CodeSelectionInvalid},
		{"bad module id", func(m map[string]any) { m["modules"] = []any{"Bad"} }, "#/modules/0", manifest.CodeSelectionInvalid},
		{"unknown key", func(m map[string]any) { m["extra"] = 1 }, "#/extra", manifest.CodeSelectionInvalid},
		{"unknown project key", func(m map[string]any) { m["project"].(map[string]any)["extra"] = 1 }, "#/project/extra", manifest.CodeSelectionInvalid},
		{"missing modules", func(m map[string]any) { delete(m, "modules") }, "#/modules", manifest.CodeSelectionInvalid},
		{"schema 3", func(m map[string]any) { m["schema"] = 3 }, "#/schema", manifest.CodeSelectionInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := mutateFixture(t, "selection.valid.json", tc.change)
			_, issues := decodeFile(t, selectionPath, data)
			if !hasIssue(issues, tc.code, selectionPath+tc.path) {
				t.Fatalf("want %s at %s, got %v", tc.code, tc.path, issues)
			}
		})
	}
}

func TestDecodeSelectionAcceptsBoundariesAndIgnoresOrder(t *testing.T) {
	cases := map[string]func(map[string]any){
		"name 2 chars":      func(m map[string]any) { m["project"].(map[string]any)["name"] = "ab" },
		"name 63 chars":     func(m map[string]any) { m["project"].(map[string]any)["name"] = "a" + strings.Repeat("b", 62) },
		"duplicate modules": func(m map[string]any) { m["modules"] = []any{"parameters", "parameters", "feature-flags"} },
		"frontend first":    func(m map[string]any) { m["stacks"] = []any{"ng-web", "go-srv"} },
		"empty modules":     func(m map[string]any) { m["modules"] = []any{} },
		"nested module":     func(m map[string]any) { m["project"].(map[string]any)["goModule"] = "github.com/acme/back_end.v2~x" },
		"goModule absent":   func(m map[string]any) { delete(m["project"].(map[string]any), "goModule") },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			_, issues := decodeFile(t, selectionPath, mutateFixture(t, "selection.valid.json", change))
			if len(issues) != 0 {
				t.Fatalf("unexpected issues: %v", issues)
			}
		})
	}
}

func TestDecodeSelectionSyntaxError(t *testing.T) {
	_, issues := decodeFile(t, selectionPath, []byte(`{`))
	if len(issues) != 1 || issues[0].Code != manifest.CodeSelectionInvalid || issues[0].Path != selectionPath+"#" {
		t.Fatalf("want one whole-file SELECTION_INVALID, got %v", issues)
	}
}
