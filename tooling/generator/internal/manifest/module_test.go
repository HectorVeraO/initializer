package manifest_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

const modulePath = "go-srv/internal/gatbase/parameters/module.manifest.json"

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

// mutateFixture decodes a fixture into a generic map, applies change and
// re-encodes it.
func mutateFixture(t *testing.T, name string, change func(map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(readFixture(t, name), &doc); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	change(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return out
}

func hasIssue(issues []issue.Issue, code, path string) bool {
	for _, found := range issues {
		if found.Code == code && found.Path == path {
			return true
		}
	}
	return false
}

func TestDecodeModuleValidFixture(t *testing.T) {
	module, issues := manifest.DecodeModule(modulePath, readFixture(t, "module.valid.json"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if module.Module != "parameters" || module.Stack != "go-srv" || module.Version.String() != "0.1.0" {
		t.Errorf("identity not decoded: %+v", module)
	}
	if module.RequiresCore == nil || module.RequiresCore.String() != "^0.1.0" {
		t.Errorf("requiresCore not decoded: %v", module.RequiresCore)
	}
	if len(module.Files) != 2 || module.Files[1].Target != "api/parameters.yaml" {
		t.Errorf("files not decoded: %+v", module.Files)
	}
}

func TestDecodeModuleReportsEveryProblem(t *testing.T) {
	_, issues := manifest.DecodeModule(modulePath, readFixture(t, "module.many-errors.json"))
	want := map[string]string{
		modulePath + "#/schema":         manifest.CodeManifestInvalid,
		modulePath + "#/stack":          manifest.CodeStackUnknown,
		modulePath + "#/module":         manifest.CodeManifestInvalid,
		modulePath + "#/version":        manifest.CodeManifestInvalid,
		modulePath + "#/implied":        manifest.CodeManifestInvalid,
		modulePath + "#/implementsSpec": manifest.CodeManifestInvalid,
		modulePath + "#/requiresCore":   manifest.CodeManifestInvalid,
		modulePath + "#/files/0":        manifest.CodeManifestInvalid,
	}
	if len(issues) != len(want) {
		t.Fatalf("got %d issues, want %d: %v", len(issues), len(want), issues)
	}
	for path, code := range want {
		if !hasIssue(issues, code, path) {
			t.Errorf("missing %s at %s", code, path)
		}
	}
}

func TestDecodeModuleIssuesAreSorted(t *testing.T) {
	_, issues := manifest.DecodeModule(modulePath, readFixture(t, "module.many-errors.json"))
	for i := 1; i < len(issues); i++ {
		if issues[i-1].Path > issues[i].Path {
			t.Fatalf("issues not sorted by path: %v", issues)
		}
	}
}

func TestDecodeModuleSyntaxErrors(t *testing.T) {
	cases := map[string]string{
		"empty":        "",
		"truncated":    `{"schema": 1`,
		"trailing":     `{} {}`,
		"not an obj":   `[]`,
		"null":         `null`,
		"bom":          "\ufeff{}",
		"unterminated": `{"a": "b`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, issues := manifest.DecodeModule(modulePath, []byte(input))
			if len(issues) != 1 || issues[0].Code != manifest.CodeManifestInvalid || issues[0].Path != modulePath+"#" {
				t.Fatalf("want one whole-file MANIFEST_INVALID, got %v", issues)
			}
		})
	}
}

func TestDecodeModuleSyntaxErrorCarriesOffset(t *testing.T) {
	_, issues := manifest.DecodeModule(modulePath, []byte(`{"schema": x}`))
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "offset") {
		t.Fatalf("want an offset in the message, got %v", issues)
	}
}

func TestDecodeModuleRejectsUnknownAndDuplicateKeys(t *testing.T) {
	cases := map[string]struct {
		change func(map[string]any)
		path   string
	}{
		"top level": {func(m map[string]any) { m["extra"] = 1 }, "#/extra"},
		"requiresModules element": {func(m map[string]any) {
			m["requiresModules"] = []any{map[string]any{"module": "a", "range": "1.0.0", "extra": true}}
		}, "#/requiresModules/0/extra"},
		"file entry": {func(m map[string]any) {
			m["files"] = []any{map[string]any{"path": "a/b", "target": "c", "extra": 1}}
		}, "#/files/0/extra"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			data := mutateFixture(t, "module.valid.json", tc.change)
			_, issues := manifest.DecodeModule(modulePath, data)
			if !hasIssue(issues, manifest.CodeManifestInvalid, modulePath+tc.path) {
				t.Fatalf("want unknown key at %s, got %v", tc.path, issues)
			}
		})
	}
	t.Run("duplicate key", func(t *testing.T) {
		data := strings.Replace(string(readFixture(t, "module.valid.json")), `"implied": false,`, `"implied": false, "implied": true,`, 1)
		_, issues := manifest.DecodeModule(modulePath, []byte(data))
		if !hasIssue(issues, manifest.CodeManifestInvalid, modulePath+"#/implied") {
			t.Fatalf("want duplicate key issue, got %v", issues)
		}
	})
}

func TestDecodeModuleRequiredKeys(t *testing.T) {
	for _, key := range []string{"schema", "stack", "module", "version", "implied", "implementsSpec", "requiresCore", "requiresModules", "tablePrefixes", "files", "exclude"} {
		t.Run(key, func(t *testing.T) {
			data := mutateFixture(t, "module.valid.json", func(m map[string]any) { delete(m, key) })
			_, issues := manifest.DecodeModule(modulePath, data)
			if !hasIssue(issues, manifest.CodeManifestInvalid, modulePath+"#/"+key) {
				t.Fatalf("want missing %s, got %v", key, issues)
			}
		})
	}
}

func TestDecodeModuleIDLength(t *testing.T) {
	cases := map[string]bool{
		strings.Repeat("a", 31):   true,
		strings.Repeat("a", 32):   false,
		"a":                       true,
		"feature-flags":           true,
		"Feature":                 false,
		"1abc":                    false,
		"a--b":                    false,
		"a-":                      false,
		"-a":                      false,
		"a_b":                     false,
		"a\n":                     false,
		"":                        false,
		"core":                    true,
		strings.Repeat("a-b", 10): true,
	}
	for id, ok := range cases {
		data := mutateFixture(t, "module.valid.json", func(m map[string]any) { m["module"] = id })
		_, issues := manifest.DecodeModule(modulePath, data)
		if got := !hasIssue(issues, manifest.CodeManifestInvalid, modulePath+"#/module"); got != ok {
			t.Errorf("module id %q: valid=%v, want %v", id, got, ok)
		}
	}
}

func TestDecodeModuleCoreRules(t *testing.T) {
	core := func(m map[string]any) {
		m["module"] = "core"
		m["implementsSpec"] = map[string]any{"core": "0.1.0"}
		delete(m, "requiresCore")
		m["requiresModules"] = []any{}
	}
	t.Run("core without requiresCore is valid", func(t *testing.T) {
		_, issues := manifest.DecodeModule(modulePath, mutateFixture(t, "module.valid.json", core))
		if len(issues) != 0 {
			t.Fatalf("unexpected issues: %v", issues)
		}
	})
	t.Run("core with requiresCore", func(t *testing.T) {
		data := mutateFixture(t, "module.valid.json", func(m map[string]any) {
			core(m)
			m["requiresCore"] = "^0.1.0"
		})
		_, issues := manifest.DecodeModule(modulePath, data)
		if !hasIssue(issues, manifest.CodeManifestInvalid, modulePath+"#/requiresCore") {
			t.Fatalf("want requiresCore rejected on core, got %v", issues)
		}
	})
	t.Run("requiresModules naming core", func(t *testing.T) {
		data := mutateFixture(t, "module.valid.json", func(m map[string]any) {
			m["requiresModules"] = []any{map[string]any{"module": "core", "range": "^0.1.0"}}
		})
		_, issues := manifest.DecodeModule(modulePath, data)
		if !hasIssue(issues, manifest.CodeManifestInvalid, modulePath+"#/requiresModules/0/module") {
			t.Fatalf("want core rejected, got %v", issues)
		}
	})
}

func TestDecodeModuleStacks(t *testing.T) {
	t.Run("unknown stack", func(t *testing.T) {
		data := mutateFixture(t, "module.valid.json", func(m map[string]any) { m["stack"] = "rust-srv" })
		_, issues := manifest.DecodeModule(modulePath, data)
		if !hasIssue(issues, manifest.CodeStackUnknown, modulePath+"#/stack") {
			t.Fatalf("want STACK_UNKNOWN, got %v", issues)
		}
	})
	t.Run("unknown stack still validates tablePrefixes", func(t *testing.T) {
		data := mutateFixture(t, "module.valid.json", func(m map[string]any) {
			m["stack"] = "rust-srv"
			m["tablePrefixes"] = []any{"X"}
		})
		_, issues := manifest.DecodeModule(modulePath, data)
		if !hasIssue(issues, manifest.CodeStackUnknown, modulePath+"#/stack") {
			t.Fatalf("want STACK_UNKNOWN, got %v", issues)
		}
		if !hasIssue(issues, manifest.CodeManifestInvalid, modulePath+"#/tablePrefixes/0") {
			t.Fatalf("want tablePrefixes/0 rejected, got %v", issues)
		}
	})
	t.Run("missing module id does not demand requiresCore", func(t *testing.T) {
		data := mutateFixture(t, "module.valid.json", func(m map[string]any) {
			delete(m, "module")
			delete(m, "requiresCore")
		})
		_, issues := manifest.DecodeModule(modulePath, data)
		if !hasIssue(issues, manifest.CodeManifestInvalid, modulePath+"#/module") {
			t.Fatalf("want module rejected, got %v", issues)
		}
		if hasIssue(issues, manifest.CodeManifestInvalid, modulePath+"#/requiresCore") {
			t.Fatalf("requiresCore must not be reported without a module id, got %v", issues)
		}
	})
	t.Run("ng-web forbids tablePrefixes", func(t *testing.T) {
		data := mutateFixture(t, "module.valid.json", func(m map[string]any) { m["stack"] = "ng-web" })
		_, issues := manifest.DecodeModule(modulePath, data)
		if !hasIssue(issues, manifest.CodeManifestInvalid, modulePath+"#/tablePrefixes") {
			t.Fatalf("want tablePrefixes rejected, got %v", issues)
		}
	})
	t.Run("ng-web without tablePrefixes is valid", func(t *testing.T) {
		data := mutateFixture(t, "module.valid.json", func(m map[string]any) {
			m["stack"] = "ng-web"
			delete(m, "tablePrefixes")
		})
		_, issues := manifest.DecodeModule(modulePath, data)
		if len(issues) != 0 {
			t.Fatalf("unexpected issues: %v", issues)
		}
	})
	t.Run("go-srv accepts empty tablePrefixes", func(t *testing.T) {
		data := mutateFixture(t, "module.valid.json", func(m map[string]any) { m["tablePrefixes"] = []any{} })
		_, issues := manifest.DecodeModule(modulePath, data)
		if len(issues) != 0 {
			t.Fatalf("unexpected issues: %v", issues)
		}
	})
}

func TestDecodeModuleTablePrefixes(t *testing.T) {
	cases := []struct {
		name     string
		prefixes []any
		ok       bool
		path     string
	}{
		{"one letter", []any{"a"}, false, "#/tablePrefixes/0"},
		{"two letters", []any{"ab"}, true, ""},
		{"four letters", []any{"abcd"}, true, ""},
		{"five letters", []any{"abcde"}, false, "#/tablePrefixes/0"},
		{"uppercase", []any{"AB"}, false, "#/tablePrefixes/0"},
		{"digit", []any{"a1"}, false, "#/tablePrefixes/0"},
		{"unsorted", []any{"sys", "prm"}, false, "#/tablePrefixes/1"},
		{"duplicate", []any{"sys", "sys"}, false, "#/tablePrefixes/1"},
		{"sorted pair", []any{"prm", "sys"}, true, ""},
		{"not a string", []any{1}, false, "#/tablePrefixes/0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := mutateFixture(t, "module.valid.json", func(m map[string]any) { m["tablePrefixes"] = tc.prefixes })
			_, issues := manifest.DecodeModule(modulePath, data)
			if got := len(issues) == 0; got != tc.ok {
				t.Fatalf("valid=%v, want %v: %v", got, tc.ok, issues)
			}
			if !tc.ok && !hasIssue(issues, manifest.CodeManifestInvalid, modulePath+tc.path) {
				t.Fatalf("want MANIFEST_INVALID at %s, got %v", tc.path, issues)
			}
		})
	}
}

func TestDecodeModuleImplementsSpec(t *testing.T) {
	cases := []struct {
		name  string
		value any
		ok    bool
		path  string
	}{
		{"one spec", map[string]any{"parameters": "0.1.0"}, true, ""},
		{"empty", map[string]any{}, false, "#/implementsSpec"},
		{"bad key", map[string]any{"Bad": "0.1.0"}, false, "#/implementsSpec/Bad"},
		{"bad version", map[string]any{"parameters": "v0.1.0"}, false, "#/implementsSpec/parameters"},
		{"not an object", []any{"parameters"}, false, "#/implementsSpec"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := mutateFixture(t, "module.valid.json", func(m map[string]any) { m["implementsSpec"] = tc.value })
			_, issues := manifest.DecodeModule(modulePath, data)
			if got := len(issues) == 0; got != tc.ok {
				t.Fatalf("valid=%v, want %v: %v", got, tc.ok, issues)
			}
			if !tc.ok && !hasIssue(issues, manifest.CodeManifestInvalid, modulePath+tc.path) {
				t.Fatalf("want MANIFEST_INVALID at %s, got %v", tc.path, issues)
			}
		})
	}
}

func TestDecodeModuleRequiresModulesOrder(t *testing.T) {
	entry := func(module string) map[string]any {
		return map[string]any{"module": module, "range": "^0.1.0"}
	}
	cases := []struct {
		name  string
		value []any
		ok    bool
		path  string
	}{
		{"sorted", []any{entry("a"), entry("b")}, true, ""},
		{"unsorted", []any{entry("b"), entry("a")}, false, "#/requiresModules/1/module"},
		{"duplicate", []any{entry("a"), entry("a")}, false, "#/requiresModules/1/module"},
		{"bad range", []any{map[string]any{"module": "a", "range": "~1.0.0"}}, false, "#/requiresModules/0/range"},
		{"missing range", []any{map[string]any{"module": "a"}}, false, "#/requiresModules/0/range"},
		{"not an object", []any{"a"}, false, "#/requiresModules/0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := mutateFixture(t, "module.valid.json", func(m map[string]any) { m["requiresModules"] = tc.value })
			_, issues := manifest.DecodeModule(modulePath, data)
			if got := len(issues) == 0; got != tc.ok {
				t.Fatalf("valid=%v, want %v: %v", got, tc.ok, issues)
			}
			if !tc.ok && !hasIssue(issues, manifest.CodeManifestInvalid, modulePath+tc.path) {
				t.Fatalf("want MANIFEST_INVALID at %s, got %v", tc.path, issues)
			}
		})
	}
}

func TestDecodeModuleFilesOrder(t *testing.T) {
	cases := []struct {
		name  string
		value []any
		ok    bool
		path  string
	}{
		{"sorted", []any{"a/", "b/c"}, true, ""},
		{"unsorted", []any{"b", "a"}, false, "#/files/1"},
		{"duplicate", []any{"a", "a"}, false, "#/files/1"},
		{"sorted by source path", []any{"a", map[string]any{"path": "b", "target": "a"}}, true, ""},
		{"unsorted by source path", []any{map[string]any{"path": "b", "target": "a"}, "a"}, false, "#/files/1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := mutateFixture(t, "module.valid.json", func(m map[string]any) { m["files"] = tc.value })
			_, issues := manifest.DecodeModule(modulePath, data)
			if got := len(issues) == 0; got != tc.ok {
				t.Fatalf("valid=%v, want %v: %v", got, tc.ok, issues)
			}
			if !tc.ok && !hasIssue(issues, manifest.CodeManifestInvalid, modulePath+tc.path) {
				t.Fatalf("want MANIFEST_INVALID at %s, got %v", tc.path, issues)
			}
		})
	}
}

func TestDecodeModulePathSecurity(t *testing.T) {
	bad := []string{
		"../x", "a/../b", "/etc/x", "C:/x", "a\\b", "a//b", "./a", "a/./b",
		"a\x00b", "a\nb", "a\x7fb", "", "/", "a/..", "a:b", "a/b//",
	}
	places := map[string]func(m map[string]any, path string){
		"files string":   func(m map[string]any, path string) { m["files"] = []any{path} },
		"exclude string": func(m map[string]any, path string) { m["exclude"] = []any{path} },
		"object path": func(m map[string]any, path string) {
			m["files"] = []any{map[string]any{"path": path, "target": "ok/file"}}
		},
		"object target": func(m map[string]any, path string) {
			m["files"] = []any{map[string]any{"path": "ok/file", "target": path}}
		},
	}
	for place, set := range places {
		for _, path := range bad {
			data := mutateFixture(t, "module.valid.json", func(m map[string]any) { set(m, path) })
			_, issues := manifest.DecodeModule(modulePath, data)
			if len(issues) == 0 {
				t.Errorf("%s accepted %q", place, path)
			}
		}
	}
	t.Run("target ending in slash", func(t *testing.T) {
		data := mutateFixture(t, "module.valid.json", func(m map[string]any) {
			m["files"] = []any{map[string]any{"path": "a/b", "target": "c/"}}
		})
		_, issues := manifest.DecodeModule(modulePath, data)
		if !hasIssue(issues, manifest.CodeManifestInvalid, modulePath+"#/files/0/target") {
			t.Fatalf("want target rejected, got %v", issues)
		}
	})
	t.Run("object entry needs both keys", func(t *testing.T) {
		data := mutateFixture(t, "module.valid.json", func(m map[string]any) {
			m["files"] = []any{map[string]any{"path": "a/b"}}
		})
		_, issues := manifest.DecodeModule(modulePath, data)
		if !hasIssue(issues, manifest.CodeManifestInvalid, modulePath+"#/files/0/target") {
			t.Fatalf("want missing target, got %v", issues)
		}
	})
}

func TestDecodeModuleVersions(t *testing.T) {
	for _, version := range []string{"01.0.0", "1.0", "v1.0.0", "1.0.0-rc.1", " 1.0.0", "99999999999999999999.0.0"} {
		data := mutateFixture(t, "module.valid.json", func(m map[string]any) { m["version"] = version })
		_, issues := manifest.DecodeModule(modulePath, data)
		if !hasIssue(issues, manifest.CodeManifestInvalid, modulePath+"#/version") {
			t.Errorf("version %q accepted", version)
		}
	}
}

func TestDecodeModuleSchemaMustBeOne(t *testing.T) {
	for _, value := range []any{2, 0, "1", 1.5, nil, true} {
		data := mutateFixture(t, "module.valid.json", func(m map[string]any) { m["schema"] = value })
		_, issues := manifest.DecodeModule(modulePath, data)
		if !hasIssue(issues, manifest.CodeManifestInvalid, modulePath+"#/schema") {
			t.Errorf("schema %v accepted", value)
		}
	}
}

func FuzzDecodeModule(f *testing.F) {
	f.Add(readFixtureFuzz(f, "module.valid.json"))
	f.Add(readFixtureFuzz(f, "module.many-errors.json"))
	f.Add([]byte(`{"files":[{"path":"a","target":"b"}]}`))
	f.Add([]byte(`{"schema":1,"schema":1}`))
	f.Add([]byte(`[`))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, issues := manifest.DecodeModule("m.json", data)
		for _, found := range issues {
			if found.Code == "" || !strings.HasPrefix(found.Path, "m.json#") {
				t.Fatalf("malformed issue: %+v", found)
			}
		}
	})
}

func readFixtureFuzz(tb testing.TB, name string) []byte {
	tb.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		tb.Fatalf("read fuzz seed %s: %v", name, err)
	}
	return data
}
