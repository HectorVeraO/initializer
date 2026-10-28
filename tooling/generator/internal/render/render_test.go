package render_test

import (
	"bytes"
	"flag"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/render"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("output differs from %s (run with -update to rewrite)\n%s", path, got)
	}
}

func TestModulesGoGoldens(t *testing.T) {
	cases := []struct {
		golden  string
		folders []string
	}{
		{"modules.core-parameters.go.golden", []string{"identity", "parameters"}},
		{"modules.core-only.go.golden", []string{}},
		{"modules.all.go.golden", []string{"identity", "featureflags", "parameters"}},
	}
	for _, c := range cases {
		t.Run(c.golden, func(t *testing.T) {
			got, err := render.ModulesGo("example.com/acme/backend", c.folders)
			if err != nil {
				t.Fatal(err)
			}
			assertGolden(t, c.golden, got)
			again, err := format.Source(got)
			if err != nil || !bytes.Equal(again, got) {
				t.Fatalf("output is not format.Source-stable: %v", err)
			}
			second, err := render.ModulesGo("example.com/acme/backend", c.folders)
			if err != nil || !bytes.Equal(second, got) {
				t.Fatal("rendering twice gave different bytes")
			}
		})
	}
}

func TestModulesGoKeepsOneImportGroup(t *testing.T) {
	got, err := render.ModulesGo("example.com/acme/backend", []string{"identity", "parameters"})
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(got), "import (")
	end := strings.Index(string(got)[start:], ")\n")
	if strings.Contains(string(got)[start:start+end], "\n\n") {
		t.Fatalf("imports must form one group:\n%s", got)
	}
}

func TestModulesGoRefusesBadInput(t *testing.T) {
	cases := map[string]struct {
		module  string
		folders []string
	}{
		"empty module":     {"", nil},
		"quote in module":  {`example.com/a"b`, nil},
		"space in module":  {"example.com/a b", nil},
		"bad folder":       {"example.com/a", []string{"Bad"}},
		"injection folder": {"example.com/a", []string{"x\"\n}"}},
		"core folder":      {"example.com/a", []string{"core"}},
		"duplicate folder": {"example.com/a", []string{"parameters", "parameters"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if out, err := render.ModulesGo(c.module, c.folders); err == nil {
				t.Fatalf("want an error, got:\n%s", out)
			}
		})
	}
}

func TestReadmeGoldens(t *testing.T) {
	coreParams := render.ReadmeInput{
		Name:             "acme-backend",
		GeneratorVersion: "0.1.0",
		Modules: []render.ReadmeModule{
			{Stack: "go-srv", Module: "core", Version: "0.1.0"},
			{Stack: "go-srv", Module: "identity", Version: "0.1.0"},
			{Stack: "go-srv", Module: "parameters", Version: "0.1.0"},
		},
		TCKTags: []string{"@core", "@identity", "@otel", "@parameters"},
	}
	all := coreParams
	all.Modules = []render.ReadmeModule{
		{Stack: "go-srv", Module: "core", Version: "0.1.0"},
		{Stack: "go-srv", Module: "identity", Version: "0.1.0"},
		{Stack: "go-srv", Module: "feature-flags", Version: "0.1.0"},
		{Stack: "go-srv", Module: "parameters", Version: "0.1.0"},
	}
	all.TCKTags = []string{"@core", "@identity", "@feature-flags", "@otel", "@parameters"}
	all.TCKEnv = []render.EnvPair{{Key: "APP_FLAGS_FILE", Value: "specs/feature-flags/tck/fixtures/flags.yaml"}}
	cases := map[string]render.ReadmeInput{
		"README.core-parameters.md.golden": coreParams,
		"README.all.md.golden":             all,
	}
	for golden, in := range cases {
		t.Run(golden, func(t *testing.T) {
			got, err := render.Readme(in)
			if err != nil {
				t.Fatal(err)
			}
			assertGolden(t, golden, got)
			if !bytes.HasSuffix(got, []byte("run tck\n")) || bytes.HasSuffix(got, []byte("\n\n")) {
				t.Fatal("README must end with exactly one newline")
			}
		})
	}
}

func TestReadmeRefusesUnsafeFields(t *testing.T) {
	base := render.ReadmeInput{Name: "acme-backend", GeneratorVersion: "0.1.0"}
	bad := []render.ReadmeInput{base, base, base, base, base}
	bad[0].Name = "x\n# injected"
	bad[1].TCKEnv = []render.EnvPair{{Key: "K", Value: "a`b"}}
	bad[2].Modules = []render.ReadmeModule{{Stack: "go-srv", Module: "a\r", Version: "0.1.0"}}
	bad[3].TCKTags = []string{"@core\n# injected"}
	bad[4].TCKEnv = []render.EnvPair{{Key: "K`", Value: "v"}}
	for i, in := range bad {
		if out, err := render.Readme(in); err == nil {
			t.Fatalf("case %d: want an error, got:\n%s", i, out)
		}
	}
}
