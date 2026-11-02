package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixturePath is the product vertical the generated-project-product-module CI
// job copies into a freshly generated project.
const fixturePath = "tooling/generator/testdata/product-vertical"

// A fixture under tooling/ is a repository artifact: the catalog lists none of
// its files and a project generated with every module receives none of them.
func TestProductVerticalFixtureShipsNothing(t *testing.T) {
	root, catalogPath := realCatalog(t)
	catalog, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(catalog), fixturePath) {
		t.Fatalf("the catalog lists a file of %s", fixturePath)
	}

	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, out,
		"--name", "acme-backend", "--go-module", "example.com/acme/backend", "--stacks", "go-srv", "--all-modules")
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	walkErr := filepath.WalkDir(out, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, relErr := filepath.Rel(out, path)
		if relErr != nil {
			return relErr
		}
		slashed := filepath.ToSlash(relative)
		for _, forbidden := range []string{"internal/dispatch", "specs/dispatch"} {
			if strings.Contains(slashed, forbidden) {
				t.Errorf("the generated project holds %s", slashed)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
}
