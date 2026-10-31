package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAGoModuleThatExtendsTheSourcePathIsAccepted(t *testing.T) {
	root, catalogPath := realCatalog(t)
	source := repoGoModule(t, root)
	for _, goModule := range []string{source + "-fork", source + "/v2", source + "x/api"} {
		t.Run(goModule, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, out,
				"--name", "shop", "--stacks", "go-srv", "--go-module", goModule)
			if code != exitOK {
				t.Fatalf("generate exit %d: %s", code, stderr)
			}
			gomod, err := os.ReadFile(filepath.Join(out, "shop-srv", "go.mod"))
			if err != nil {
				t.Fatal(err)
			}
			if want := "module " + goModule + "\n"; !strings.HasPrefix(string(gomod), want) {
				t.Fatalf("go.mod starts with %q, want %q", firstLine(string(gomod)), want)
			}
		})
	}
}

func TestTheSourceModulePathKeptNextToAnotherModuleIsAReportedProblem(t *testing.T) {
	root, catalogPath := realCatalog(t)
	source := repoGoModule(t, root)
	cases := map[string][]string{
		"top level": {"--go-module", source,
			"--component", "go-srv=acme-srv,go-module=example.com/acme/srv", "--component", "go-srv=acme-api,go-module=example.com/acme/api"},
		"one component": {"--go-module", "example.com/acme",
			"--component", "go-srv=acme-srv,go-module=" + source, "--component", "go-srv=acme-api,go-module=example.com/acme/api"},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			args := append([]string{"generate", out, "--root", root, "--catalog", catalogPath, "--name", "acme"}, extra...)
			code, _, stderr := execute(t, args...)
			if code != exitIssues {
				t.Fatalf("exit %d, want %d: %s", code, exitIssues, stderr)
			}
			for _, want := range []string{"GO_MODULE_PATH_LEAK", "hint: use your own Go module path for the project and for every component: --go-module <path>", "nothing was written"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr lacks %q: %s", want, stderr)
				}
			}
			if strings.Contains(stderr, codeInternalError) {
				t.Errorf("a selection problem was reported as an internal error: %s", stderr)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("the output folder exists (%v)", err)
			}
		})
	}
}
