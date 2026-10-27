package verify_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/emit"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/verify"
)

// generated writes a core + parameters project and returns its directory.
func generated(t *testing.T) string {
	t.Helper()
	repo := fixture.Load(t)
	sel := manifest.Selection{
		Schema:  1,
		Project: manifest.Project{Name: "acme-backend", GoModule: "example.com/acme/backend"},
		Stacks:  []string{"go-srv"},
		Modules: []string{"parameters"},
	}
	resolved, issues := plan.Resolve(repo.Catalog, sel)
	if len(issues) > 0 {
		t.Fatalf("resolve: %v", issues)
	}
	sum := sha256.Sum256(repo.CatalogBytes)
	project, err := emit.Build(emit.BuildOptions{Root: os.DirFS(repo.Root), Plan: resolved, CatalogSHA256: hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "project")
	if err := emit.Write(dir, project); err != nil {
		t.Fatal(err)
	}
	return dir
}

func codes(differences []issue.Issue) []string {
	out := make([]string, len(differences))
	for i, d := range differences {
		out[i] = d.Code + " " + d.Path
	}
	return out
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFreshProjectHasNoDifferences(t *testing.T) {
	differences, err := verify.Project(generated(t))
	if err != nil || len(differences) != 0 {
		t.Fatalf("differences %v, error %v", differences, err)
	}
}

func TestModifiedMissingAndUntrackedAreReported(t *testing.T) {
	dir := generated(t)
	write(t, dir, "go-srv/internal/gatbase/parameters/parameters.go", "package parameters\n")
	if err := os.Remove(filepath.Join(dir, "go-srv", "internal", "gatbase", "core", "core.go")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "go-srv/internal/gatbase/coreauth/extra.go", "package coreauth\n")
	write(t, dir, "specs/parameters/extra.yaml", "x: 1\n")
	// The project's own code outside the delivered folders is never reported.
	write(t, dir, "go-srv/internal/app/app.go", "package app\n")

	differences, err := verify.Project(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"MISSING go-srv/internal/gatbase/core/core.go",
		"UNTRACKED go-srv/internal/gatbase/coreauth/extra.go",
		"MODIFIED go-srv/internal/gatbase/parameters/parameters.go",
		"UNTRACKED specs/parameters/extra.yaml",
	}
	if strings.Join(codes(differences), "\n") != strings.Join(want, "\n") {
		t.Fatalf("differences:\n%s", strings.Join(codes(differences), "\n"))
	}
}

func TestGeneratedFilesAreChecked(t *testing.T) {
	dir := generated(t)
	write(t, dir, "README.md", "edited\n")
	differences, err := verify.Project(dir)
	if err != nil || strings.Join(codes(differences), ",") != "MODIFIED README.md" {
		t.Fatalf("differences %v, error %v", codes(differences), err)
	}
}

func TestDirectoryInPlaceOfAFileIsModified(t *testing.T) {
	dir := generated(t)
	target := filepath.Join(dir, "README.md")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	differences, err := verify.Project(dir)
	if err != nil || strings.Join(codes(differences), ",") != "MODIFIED README.md" {
		t.Fatalf("differences %v, error %v", codes(differences), err)
	}
}

func TestMissingOrBrokenLockIsAnError(t *testing.T) {
	missing := generated(t)
	if err := os.Remove(filepath.Join(missing, lockfile.Name)); err != nil {
		t.Fatal(err)
	}
	broken := generated(t)
	write(t, broken, lockfile.Name, "{not json")
	escaping := generated(t)
	write(t, escaping, lockfile.Name, `{"schema":1,"generator":"0.1.0","catalogSha256":"","selection":{"schema":1,"project":{"name":"a"},"stacks":[],"modules":[]},"tckTags":[],"tckGrep":"","tckEnv":{},"specs":[],"modules":[],"generated":[{"path":"../x","sha256":"`+strings.Repeat("a", 64)+`"}]}`)
	for name, dir := range map[string]string{"missing": missing, "broken": broken, "escaping path": escaping, "absent project": filepath.Join(missing, "nope")} {
		if _, err := verify.Project(dir); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
