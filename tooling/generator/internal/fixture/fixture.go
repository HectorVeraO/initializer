// Package fixture loads the golden fixture repository for tests of the
// packages that generate and verify projects.
package fixture

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
)

// Dir returns the absolute path of tooling/generator/testdata/golden.
func Dir(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the fixture")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "golden")
}

// Repo is the golden fixture repository and the catalog built from it.
type Repo struct {
	// Root is the fixture checkout directory.
	Root string
	// Catalog is the decoded catalog; CatalogBytes its encoded form.
	Catalog      catalog.Catalog
	CatalogBytes []byte
}

// Load builds the catalog of the fixture repository from files.txt.
func Load(t testing.TB) Repo {
	t.Helper()
	dir := Dir(t)
	root := filepath.Join(dir, "repo")
	list, err := os.ReadFile(filepath.Join(dir, "files.txt"))
	if err != nil {
		t.Fatalf("read files.txt: %v", err)
	}
	files := strings.Fields(string(list))
	built, issues, err := catalog.Build(catalog.BuildOptions{Root: os.DirFS(root), Files: files, Generator: generator.Version})
	if err != nil || len(issues) > 0 {
		t.Fatalf("build fixture catalog: %v %v", err, issues)
	}
	encoded, err := catalog.Encode(built)
	if err != nil {
		t.Fatalf("encode fixture catalog: %v", err)
	}
	return Repo{Root: root, Catalog: built, CatalogBytes: encoded}
}
