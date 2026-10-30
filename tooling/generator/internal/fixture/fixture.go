// Package fixture loads the golden fixture repository for tests of the
// packages that generate and verify projects.
package fixture

import (
	"os"
	"os/exec"
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

// LoadCheckout builds the catalog of the repository checkout the tests run in,
// from its tracked files. The test is skipped when git cannot list them.
func LoadCheckout(t testing.TB) Repo {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the checkout")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
	listed, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Skipf("git ls-files: %v", err)
	}
	files, err := catalog.ParseFileList(listed)
	if err != nil {
		t.Fatalf("parse the file list: %v", err)
	}
	built, issues, err := catalog.Build(catalog.BuildOptions{Root: os.DirFS(root), Files: files, Generator: generator.Version})
	if err != nil || len(issues) > 0 {
		t.Fatalf("build the checkout catalog: %v %v", err, issues)
	}
	encoded, err := catalog.Encode(built)
	if err != nil {
		t.Fatalf("encode the checkout catalog: %v", err)
	}
	return Repo{Root: root, Catalog: built, CatalogBytes: encoded}
}

// Clone copies the golden fixture checkout into a new git repository that
// holds the generator marker (tooling/generator/go.mod), commits it as Jane Doe
// and returns the directory and the commit. A developer's git configuration is
// kept out and INITIALIZER_ROOT is cleared for the test.
func Clone(t testing.TB) (dir, commit string) {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"GIT_CONFIG_GLOBAL": empty, "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "Jane Doe", "GIT_AUTHOR_EMAIL": "jane@example.com",
		"GIT_COMMITTER_NAME": "Jane Doe", "GIT_COMMITTER_EMAIL": "jane@example.com",
		"INITIALIZER_ROOT": "",
	} {
		t.Setenv(key, value)
	}
	dir = t.TempDir()
	source := filepath.Join(Dir(t), "repo")
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return WriteFile(dir, filepath.ToSlash(relative), data)
	})
	if err != nil {
		t.Fatal(err)
	}
	marker := "module github.com/HectorVeraO/initializer/tooling/generator\n\ngo 1.26\n"
	if err := WriteFile(dir, "tooling/generator/go.mod", []byte(marker)); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	git("config", "commit.gpgsign", "false")
	git("config", "core.autocrlf", "false")
	git("add", "-A", "-f")
	git("commit", "-q", "-m", "fixture")
	return dir, git("rev-parse", "HEAD")
}

// WriteFile writes data at the slash path name under dir, creating folders.
func WriteFile(dir, name string, data []byte) error {
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
