package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// angularIDFiles are the files of a frontend component that carry the Angular
// project id, with the text each holds once the id is the component name.
func angularIDFiles(name string) map[string][]string {
	return map[string][]string{
		"angular.json": {`"` + name + `": {`, `"outputPath": "dist/` + name + `"`,
			`"buildTarget": "` + name + `:build:production"`, `"buildTarget": "` + name + `:build:development"`},
		"package.json":      {`"name": "` + name + `"`},
		"package-lock.json": {`"name": "` + name + `"`},
		"Dockerfile":        {"/dist/" + name + "/browser"},
	}
}

func requireAngularID(t *testing.T, out, name string) {
	t.Helper()
	for file, wants := range angularIDFiles(name) {
		data, err := os.ReadFile(filepath.Join(out, name, file))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(string(data), want) {
				t.Errorf("%s/%s lacks %q", name, file, want)
			}
		}
		if strings.Contains(string(data), "ng-web") {
			t.Errorf("%s/%s keeps the reference Angular project id", name, file)
		}
	}
}

func TestTheAngularProjectIdIsTheFrontendComponentName(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, out,
		"--name", "shop", "--go-module", "example.com/shop", "--stacks", "go-srv,ng-web", "--component", "ng-web=shop-console")
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	requireAngularID(t, out, "shop-console")
}

func TestEveryFrontendOfAMultiProjectGetsItsOwnAngularProjectId(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, out, "--name", "shop",
		"--component", "go-srv=shop-first,go-module=example.com/shop/first", "--component", "go-srv=shop-second,go-module=example.com/shop/second",
		"--component", "ng-web=shop-admin,backend=shop-first", "--component", "ng-web=shop-portal,backend=shop-second")
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	requireAngularID(t, out, "shop-admin")
	requireAngularID(t, out, "shop-portal")
}
