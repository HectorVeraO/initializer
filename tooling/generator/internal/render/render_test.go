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

// readmeRun returns the run data of project name: a backend, and a console
// when withWeb is set.
func readmeRun(name string, withWeb, withIdentity bool) render.ReadmeRun {
	instance := render.ReadmeInstance{
		Backend:     name + "-srv",
		OriginURL:   "http://localhost:10100",
		HasIdentity: withIdentity,
		DevSrvTask:  "dev:srv",
		TCKTask:     "tck",
	}
	if withWeb {
		instance.Web = name + "-web"
		instance.OriginURL = "http://localhost:10120"
		instance.DevWebTask = "dev:web"
	}
	return render.ReadmeRun{PortLow: 10100, PortHigh: 10199, ComposeProject: name + "-101", Instances: []render.ReadmeInstance{instance}}
}

// readmeInput returns the README data of an acme project with the identity
// and feature-flags modules.
func readmeInput(withWeb, withIdentity, withTCKEnv bool) render.ReadmeInput {
	in := render.ReadmeInput{
		Name:             "acme",
		GeneratorVersion: "0.1.0",
		Modules: []render.ReadmeModule{
			{Stack: "go-srv", Module: "core", Version: "0.1.0"},
			{Stack: "go-srv", Module: "identity", Version: "0.1.0"},
			{Stack: "go-srv", Module: "feature-flags", Version: "0.1.0"},
		},
		TCKTags: []string{"@core", "@feature-flags", "@identity", "@otel"},
		Run:     readmeRun("acme", withWeb, withIdentity),
	}
	if withTCKEnv {
		in.TCKEnv = []render.EnvPair{{Key: "APP_FLAGS_FILE", Value: "specs/feature-flags/tck/fixtures/flags.yaml"}}
	}
	return in
}

func TestReadmeGoldens(t *testing.T) {
	cases := []struct {
		golden string
		in     render.ReadmeInput
	}{
		{"README.all.md.golden", readmeInput(true, true, true)},
		{"README.backend-only.md.golden", readmeInput(false, true, true)},
		{"README.no-identity.md.golden", readmeInput(true, false, false)},
	}
	for _, tc := range cases {
		t.Run(tc.golden, func(t *testing.T) {
			got, err := render.Readme(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			assertGolden(t, tc.golden, got)
			if !bytes.HasSuffix(got, []byte("mise run tck\n")) || bytes.HasSuffix(got, []byte("\n\n")) {
				t.Fatal("README must end with exactly one newline")
			}
		})
	}
}

func TestReadmeVariantsOmitWhatTheProjectLacks(t *testing.T) {
	cases := []struct {
		name    string
		in      render.ReadmeInput
		want    []string
		missing []string
	}{
		{
			name: "console, identity and tckEnv",
			in:   readmeInput(true, true, true),
			want: []string{
				"The console opens on http://localhost:10120; the API is behind it at `/api`. Host ports 10100-10199, bound to `127.0.0.1`.",
				"cookies are per host, not per port.",
				"stop acme-web\n    mise run dev:web",
				"stop acme-srv acme-web\n    mise run dev:srv\n    mise run dev:web",
				"Once per clone: `npm --prefix acme-web ci`.",
				"Add `APP_FLAGS_FILE=../specs/feature-flags/tck/fixtures/flags.yaml` to `.deploy/local/.env` (acme-srv reads it in both modes)",
			},
		},
		{
			name: "no console",
			in:   readmeInput(false, true, true),
			want: []string{"The API opens on http://localhost:10100. Host ports 10100-10199", "stop acme-srv\n    mise run dev:srv\n\n## TCK"},
			missing: []string{
				"console", "acme-web", "dev:web", "npm --prefix acme-web",
			},
		},
		{
			name:    "no identity",
			in:      readmeInput(true, false, true),
			missing: []string{"cookies are per host"},
		},
		{
			name:    "no tckEnv",
			in:      readmeInput(true, true, false),
			want:    []string{"@otel\n\nOnce per clone: `npm --prefix specs ci`. Then:\n\n    mise run tck\n"},
			missing: []string{"Add `", "APP_FLAGS_FILE"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := render.Readme(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			text := string(got)
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("README lacks %q:\n%s", want, text)
				}
			}
			for _, banned := range tc.missing {
				if strings.Contains(text, banned) {
					t.Errorf("README holds %q:\n%s", banned, text)
				}
			}
		})
	}
}

func TestReadmeNamesTheComponentsOfTheRun(t *testing.T) {
	in := readmeInput(true, true, true)
	in.Run.Instances[0].Backend = "ledger-api"
	in.Run.Instances[0].Web = "ledger-console"
	got, err := render.Readme(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"stop ledger-console\n", "stop ledger-api ledger-console\n", "`npm --prefix ledger-console ci`", "(ledger-api reads it in both modes)", "Editing ledger-api runs the console"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("README lacks %q:\n%s", want, got)
		}
	}
}

func TestReadmeRefusesAnInvalidRun(t *testing.T) {
	cases := map[string]func(run *render.ReadmeRun){
		"port range reversed":     func(run *render.ReadmeRun) { run.PortLow, run.PortHigh = 10199, 10100 },
		"port range empty":        func(run *render.ReadmeRun) { run.PortHigh = run.PortLow },
		"no compose project":      func(run *render.ReadmeRun) { run.ComposeProject = "" },
		"backtick in compose":     func(run *render.ReadmeRun) { run.ComposeProject = "acme`-101" },
		"line break in compose":   func(run *render.ReadmeRun) { run.ComposeProject = "acme\n# injected-101" },
		"no instance":             func(run *render.ReadmeRun) { run.Instances = nil },
		"empty backend":           func(run *render.ReadmeRun) { run.Instances[0].Backend = "" },
		"web without a web task":  func(run *render.ReadmeRun) { run.Instances[0].DevWebTask = "" },
		"web task without a web":  func(run *render.ReadmeRun) { run.Instances[0].Web = "" },
		"empty origin":            func(run *render.ReadmeRun) { run.Instances[0].OriginURL = "" },
		"empty dev task":          func(run *render.ReadmeRun) { run.Instances[0].DevSrvTask = "" },
		"empty tck task":          func(run *render.ReadmeRun) { run.Instances[0].TCKTask = "" },
		"line break in a backend": func(run *render.ReadmeRun) { run.Instances[0].Backend = "acme\n# injected" },
		"line break in the web":   func(run *render.ReadmeRun) { run.Instances[0].Web = "acme\r" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := readmeInput(true, true, true)
			mutate(&in.Run)
			if out, err := render.Readme(in); err == nil {
				t.Fatalf("want an error, got:\n%s", out)
			}
		})
	}
}

func TestReadmeRefusesUnsafeFields(t *testing.T) {
	bad := []render.ReadmeInput{}
	for i := 0; i < 5; i++ {
		bad = append(bad, readmeInput(true, true, false))
	}
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

// multiReadmeInput returns the README data of a project with two backends: the
// first installs identity and has no console, the second installs identity and
// feature-flags and is bound to the console acme-web.
func multiReadmeInput() render.ReadmeInput {
	return render.ReadmeInput{
		Name:             "acme",
		GeneratorVersion: "0.1.0",
		Modules: []render.ReadmeModule{
			{Component: "acme-srv-first", Stack: "go-srv", Module: "core", Version: "0.1.0"},
			{Component: "acme-srv-first", Stack: "go-srv", Module: "identity", Version: "0.1.0"},
			{Component: "acme-srv-second", Stack: "go-srv", Module: "core", Version: "0.1.0"},
			{Component: "acme-srv-second", Stack: "go-srv", Module: "identity", Version: "0.1.0"},
			{Component: "acme-srv-second", Stack: "go-srv", Module: "feature-flags", Version: "0.1.0"},
			{Component: "acme-web", Stack: "ng-web", Module: "core", Version: "0.1.0"},
		},
		Run: render.ReadmeRun{PortLow: 10100, PortHigh: 10199, ComposeProject: "acme-101", Instances: []render.ReadmeInstance{
			{Backend: "acme-srv-first", OriginURL: "http://localhost:10100", HasIdentity: true, DevSrvTask: "dev:acme-srv-first", TCKTask: "tck:acme-srv-first"},
			{Backend: "acme-srv-second", Web: "acme-web", OriginURL: "http://localhost:10120", HasIdentity: true,
				DevSrvTask: "dev:acme-srv-second", DevWebTask: "dev:acme-web", TCKTask: "tck:acme-srv-second"},
		}},
		Components: []render.ReadmeComponent{
			{Name: "acme-srv-first", Stack: "go-srv", Origin: "http://localhost:10100",
				Ports:   []render.ReadmePort{{Name: "srv", Port: 10100}, {Name: "postgres", Port: 10132}, {Name: "kratos-public", Port: 10133}},
				TCKTags: []string{"@core", "@identity", "@otel"}},
			{Name: "acme-srv-second", Stack: "go-srv", Origin: "http://localhost:10120",
				Ports:   []render.ReadmePort{{Name: "srv", Port: 10101}, {Name: "postgres", Port: 10142}, {Name: "kratos-public", Port: 10143}},
				TCKTags: []string{"@core", "@feature-flags", "@identity", "@otel"},
				TCKEnv:  []render.EnvPair{{Key: "APP_FLAGS_FILE", Value: "specs/feature-flags/tck/fixtures/flags.yaml"}}},
			{Name: "acme-web", Stack: "ng-web", Backend: "acme-srv-second", Ports: []render.ReadmePort{{Name: "web", Port: 10120}}},
		},
	}
}

func TestReadmeMultiGolden(t *testing.T) {
	got, err := render.Readme(multiReadmeInput())
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "README.multi.md.golden", got)
	if !bytes.HasSuffix(got, []byte("mise run tck:acme-srv-second\n")) || bytes.HasSuffix(got, []byte("\n\n")) {
		t.Fatal("README must end with exactly one newline")
	}
}

func TestReadmeMultiStatesTheOverridesAndTheCookieNote(t *testing.T) {
	got, err := render.Readme(multiReadmeInput())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"acme-srv-second: http://localhost:10120 (console acme-web)",
		"acme-srv-first: http://localhost:10100;",
		"Per-instance overrides go in `.deploy/local/<component>/.env`",
		"`.deploy/local/.env` is read only by compose interpolation and reaches every instance",
		"one browser profile is signed in to one identity instance at a time",
		"mise run dev:acme-srv-first", "mise run dev:acme-web", "mise run tck:acme-srv-first",
		"to `.deploy/local/acme-srv-second/.env`",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("README lacks %q:\n%s", want, got)
		}
	}
	in := multiReadmeInput()
	in.Run.Instances[0].HasIdentity = false
	got, err = render.Readme(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "browser profile") {
		t.Errorf("the cookie note needs two identity instances:\n%s", got)
	}
}

func TestReadmeMultiRefusesAnInstanceWithoutItsComponent(t *testing.T) {
	cases := map[string]func(in *render.ReadmeInput){
		"missing component":     func(in *render.ReadmeInput) { in.Components = in.Components[1:] },
		"line break":            func(in *render.ReadmeInput) { in.Components[0].Name = "acme-srv-first\n# injected" },
		"line break in stack":   func(in *render.ReadmeInput) { in.Components[0].Stack = "go-srv\n# injected" },
		"line break in backend": func(in *render.ReadmeInput) { in.Components[2].Backend = "acme-srv-second\n# injected" },
		"line break in origin":  func(in *render.ReadmeInput) { in.Components[0].Origin = "http://localhost:10100\n# injected" },
		"backtick in env":       func(in *render.ReadmeInput) { in.Components[1].TCKEnv[0].Value = "a`b" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := multiReadmeInput()
			mutate(&in)
			if out, err := render.Readme(in); err == nil {
				t.Fatalf("want an error, got:\n%s", out)
			}
		})
	}
}

func TestReadmeWithoutComponentsRendersTheSingleText(t *testing.T) {
	in := readmeInput(true, true, true)
	single, err := render.Readme(in)
	if err != nil {
		t.Fatal(err)
	}
	in.Components = []render.ReadmeComponent{}
	again, err := render.Readme(in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(single, again) {
		t.Error("an empty Components list changed the single README")
	}
}
