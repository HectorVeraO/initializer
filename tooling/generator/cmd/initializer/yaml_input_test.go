package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const validYAMLSelection = `schema: 1
project:
  name: acme-backend
  goModule: example.com/acme/backend
stacks: [go-srv]
modules: [parameters]
`

func TestYAMLFilesAreReadByExtensionInAnyCase(t *testing.T) {
	e := newEnv(t)
	for _, name := range []string{"acme.yaml", "ACME.YAML", "acme.yml"} {
		t.Run(name, func(t *testing.T) {
			wd := t.TempDir()
			file := writeNamed(t, name, validYAMLSelection)
			code, _, stderr := executeIn(t, wd, "", e.inputArgs("-f", file)...)
			if code != exitOK {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if lockedName(t, filepath.Join(wd, "acme-backend")) != "acme-backend" {
				t.Fatal("YAML selection not used")
			}
		})
	}
}

func TestAFlagOverridesAValueOfTheYAMLFile(t *testing.T) {
	e := newEnv(t)
	wd := t.TempDir()
	file := writeNamed(t, "acme.yaml", validYAMLSelection)
	code, _, stderr := executeIn(t, wd, "", e.inputArgs("-f", file, "--name", "acme-shop")...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if lockedName(t, filepath.Join(wd, "acme-shop")) != "acme-shop" {
		t.Fatal("--name did not override the file")
	}
}

func TestTheSelectionAliasLoadsAYAMLFile(t *testing.T) {
	e := newEnv(t)
	wd := t.TempDir()
	file := writeNamed(t, "acme.yaml", validYAMLSelection)
	code, _, stderr := executeIn(t, wd, "", e.inputArgs("-f", file)...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if lockedName(t, filepath.Join(wd, "acme-backend")) != "acme-backend" {
		t.Fatal("YAML selection not used through --selection")
	}
}

func TestStdinIsJSONWhenItStartsWithABraceElseYAML(t *testing.T) {
	e := newEnv(t)
	cases := map[string]string{
		"yaml":                     validYAMLSelection,
		"json":                     validSelection,
		"json after blank space":   " \t\r\n" + validSelection,
		"yaml with a flow mapping": "schema: 1\nproject: {name: acme-backend, goModule: example.com/acme/backend}\nstacks: [go-srv]\nmodules: []\n",
	}
	for name, stdin := range cases {
		t.Run(name, func(t *testing.T) {
			wd := t.TempDir()
			code, _, stderr := executeIn(t, wd, stdin, e.inputArgs("-f", "-")...)
			if code != exitOK {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if lockedName(t, filepath.Join(wd, "acme-backend")) != "acme-backend" {
				t.Fatal("stdin selection not used")
			}
		})
	}
	t.Run("YAML that is not an object names stdin", func(t *testing.T) {
		code, stdout, _ := executeIn(t, t.TempDir(), "- a\n", e.inputArgs("-f", "-", "--format", "json")...)
		if code != exitIssues || !strings.Contains(stdout, `"path":"stdin#"`) || !strings.Contains(stdout, `"line":1`) {
			t.Fatalf("exit %d stdout %s", code, stdout)
		}
	})
}

func TestYAMLIssuesCarryTheirLine(t *testing.T) {
	e := newEnv(t)
	file := writeNamed(t, "acme.yaml", "schema: 1\nproject:\n  name: 123\nstacks: [go-srv]\nmodules: []\n")
	slashed := filepath.ToSlash(file)

	code, stdout, _ := executeIn(t, t.TempDir(), "", e.inputArgs("-f", file, "--format", "json")...)
	if code != exitIssues {
		t.Fatalf("exit %d", code)
	}
	wantName := `"path":"` + slashed + `#/project/name","line":3,"message":"must be a string (quote it)"`
	if !strings.Contains(stdout, wantName) {
		t.Fatalf("stdout = %s", stdout)
	}

	twice := writeNamed(t, "twice.yaml", "schema: 1\nstacks: [go-srv]\nmodules: []\nstacks: []\n")
	twiceSlashed := filepath.ToSlash(twice)
	_, stdout, _ = executeIn(t, t.TempDir(), "", e.inputArgs("-f", twice, "--format", "json")...)
	wantStacks := `"path":"` + twiceSlashed + `#/stacks","line":4,"message":"stacks is given twice (first at line 2)"`
	if !strings.Contains(stdout, wantStacks) {
		t.Fatalf("stdout = %s", stdout)
	}

	code, _, text := executeIn(t, t.TempDir(), "", e.inputArgs("-f", twice)...)
	if code != exitIssues || !strings.Contains(text, twiceSlashed+":4 /stacks") {
		t.Fatalf("exit %d: %s", code, text)
	}
}
func TestJSONIssuesStayWithoutALine(t *testing.T) {
	e := newEnv(t)
	file := writeNamed(t, "acme.json", `{"schema":1,"project":{"name":123},"stacks":["go-srv"],"modules":[]}`)
	code, stdout, text := executeIn(t, t.TempDir(), "", e.inputArgs("-f", file, "--format", "json")...)
	if code != exitIssues || strings.Contains(stdout, `"line"`) || !strings.Contains(stdout, `"message":"must be a string"`) {
		t.Fatalf("exit %d stdout %s", code, stdout)
	}
	_, _, text = executeIn(t, t.TempDir(), "", e.inputArgs("-f", file)...)
	if strings.Contains(text, ".json:") {
		t.Fatalf("a JSON issue must not show a line: %s", text)
	}
}

func TestEveryCISelectionGeneratesTheSameFromItsYAMLTwin(t *testing.T) {
	e := newEnv(t)
	paths, err := filepath.Glob(filepath.Join(realCheckoutRoot(t), "tooling", "generator", "testdata", "ci", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no CI selections: %v", err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(filepath.Base(path), func(t *testing.T) {
			var value any
			if err := yaml.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			twin, err := yaml.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			fromJSON, fromYAML := e, e
			fromJSON.out = filepath.Join(t.TempDir(), "json")
			fromYAML.out = filepath.Join(t.TempDir(), "yaml")
			if code, _, stderr := executeIn(t, t.TempDir(), "", fromJSON.args("-f", path)...); code != exitOK {
				t.Fatalf("json: exit %d: %s", code, stderr)
			}
			if code, _, stderr := executeIn(t, t.TempDir(), "", fromYAML.args("-f", writeNamed(t, "twin.yaml", string(twin)))...); code != exitOK {
				t.Fatalf("yaml: exit %d: %s", code, stderr)
			}
			left, right := tree(t, fromJSON.out), tree(t, fromYAML.out)
			if len(left) == 0 || len(left) != len(right) {
				t.Fatalf("file sets differ: %d vs %d", len(left), len(right))
			}
			for name, content := range left {
				if right[name] != content {
					t.Errorf("%s differs", name)
				}
			}
		})
	}
}
