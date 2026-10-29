package source

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
)

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestResolveSourceBuildsTheCatalogInMemoryFromAGitCheckout(t *testing.T) {
	dir, commit := fixtureRepo(t)
	resolved, issues, err := ResolveSource(background(), Options{FlagRoot: dir, Host: realHost(dir)})
	if err != nil || len(issues) > 0 {
		t.Fatalf("err = %v issues = %v", err, issues)
	}
	want := fixture.Load(t)
	if !bytes.Equal(resolved.CatalogBytes, want.CatalogBytes) {
		t.Fatal("the in-memory catalog differs from the catalog built from the file list")
	}
	if resolved.CatalogSHA256 != sha(want.CatalogBytes) || resolved.CatalogFile != "" {
		t.Fatalf("sha = %s file = %q", resolved.CatalogSHA256, resolved.CatalogFile)
	}
	if got := (Revision{Commit: commit, Known: true}); resolved.Revision != got {
		t.Fatalf("revision = %+v, want %+v", resolved.Revision, got)
	}
	if resolved.Root.How != FromFlag || len(resolved.Catalog.Stacks) == 0 {
		t.Fatalf("resolved = %+v", resolved.Root)
	}
}

func TestResolveSourceMarksAModifiedCataloguedFile(t *testing.T) {
	dir, _ := fixtureRepo(t)
	writeRepoFile(t, dir, "go-srv/internal/platform/core/core.go", "package core\n\n// changed\n")
	resolved, issues, err := ResolveSource(background(), Options{FlagRoot: dir, Host: realHost(dir)})
	if err != nil || len(issues) > 0 {
		t.Fatalf("err = %v issues = %v", err, issues)
	}
	if !resolved.Revision.Known || !resolved.Revision.Modified {
		t.Fatalf("revision = %+v", resolved.Revision)
	}
}

func TestResolveSourceKeepsAnUntrackedFileInvisible(t *testing.T) {
	dir, _ := fixtureRepo(t)
	before, _, err := ResolveSource(background(), Options{FlagRoot: dir, Host: realHost(dir)})
	if err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, dir, "go-srv/internal/platform/core/untracked.go", "package core\n")
	after, _, err := ResolveSource(background(), Options{FlagRoot: dir, Host: realHost(dir)})
	if err != nil {
		t.Fatal(err)
	}
	if before.CatalogSHA256 != after.CatalogSHA256 {
		t.Fatal("an untracked file changed the catalog")
	}
}

func TestResolveSourceNamesATrackedFileDeletedFromTheWorkingTree(t *testing.T) {
	dir, _ := fixtureRepo(t)
	if err := os.Remove(filepath.Join(dir, "go-srv", "internal", "platform", "core", "core.go")); err != nil {
		t.Fatal(err)
	}
	_, _, err := ResolveSource(background(), Options{FlagRoot: dir, Host: realHost(dir)})
	found := asSourceError(t, err)
	if found.Exit != ExitInternal || found.Issue.Code != CodeUnreadable {
		t.Fatalf("error = %+v", found)
	}
	name := "go-srv/internal/platform/core/core.go"
	if !strings.Contains(found.Issue.Message, name) {
		t.Fatalf("message %q does not name the file", found.Issue.Message)
	}
	for _, want := range []string{"git rm " + name, "git restore " + name} {
		if !strings.Contains(found.Issue.Hint, want) {
			t.Fatalf("hint %q lacks %q", found.Issue.Hint, want)
		}
	}
}

func TestResolveSourceReturnsBuildIssuesWithAHint(t *testing.T) {
	dir, _ := fixtureRepo(t)
	writeRepoFile(t, dir, "go-srv/internal/platform/core/module.manifest.json", "{not json")
	commitAll(t, dir, "break a manifest")
	resolved, issues, err := ResolveSource(background(), Options{FlagRoot: dir, Host: realHost(dir)})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) == 0 || resolved.Root.Dir == "" {
		t.Fatalf("issues = %v root = %+v", issues, resolved.Root)
	}
	if !resolved.Revision.Known {
		t.Fatalf("a git checkout with build issues must keep its revision: %+v", resolved.Revision)
	}
	for _, found := range issues {
		if found.Hint != "fix the module sources, or pass --catalog" {
			t.Fatalf("issue %+v has no source hint", found)
		}
	}
}

func TestResolveSourceReadsAnExplicitCatalogWithoutGit(t *testing.T) {
	repo := fixture.Load(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "catalog.lock.json")
	if err := os.WriteFile(file, repo.CatalogBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	host := fakeHost(dir, "", nil)
	resolved, issues, err := ResolveSource(background(), Options{FlagRoot: repo.Root, CatalogFile: file, Host: host})
	if err != nil || len(issues) > 0 {
		t.Fatalf("err = %v issues = %v", err, issues)
	}
	if resolved.CatalogSHA256 != sha(repo.CatalogBytes) || resolved.CatalogFile != file {
		t.Fatalf("sha = %s file = %q", resolved.CatalogSHA256, resolved.CatalogFile)
	}
	if resolved.Revision != (Revision{}) {
		t.Fatalf("revision = %+v, want unknown without git", resolved.Revision)
	}
}

func TestResolveSourceExplicitCatalogOnANonGitRootIsKnownNothing(t *testing.T) {
	isolateGit(t)
	repo := fixture.Load(t)
	file := filepath.Join(t.TempDir(), "catalog.lock.json")
	if err := os.WriteFile(file, repo.CatalogBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, _, err := ResolveSource(background(), Options{FlagRoot: repo.Root, CatalogFile: file, Host: realHost(repo.Root)})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Revision.Known {
		t.Fatalf("revision = %+v: the golden folder is not a work tree top level", resolved.Revision)
	}
}

func TestResolveSourceRefusesAnUnreadableCatalog(t *testing.T) {
	repo := fixture.Load(t)
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.json")
	if err := os.WriteFile(garbage, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	wrongSchema := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(wrongSchema, bytes.Replace(repo.CatalogBytes, []byte(`"schema": 1`), []byte(`"schema": 9`), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	oversized := filepath.Join(dir, "oversized.json")
	if err := os.WriteFile(oversized, bytes.Repeat([]byte(" "), MaxCatalogBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, file := range map[string]string{"missing": filepath.Join(dir, "missing.json"), "not JSON": garbage, "wrong schema": wrongSchema, "a directory": dir, "over the size limit": oversized} {
		t.Run(name, func(t *testing.T) {
			_, _, err := ResolveSource(background(), Options{FlagRoot: repo.Root, CatalogFile: file, Host: fakeHost(dir, "", nil)})
			found := asSourceError(t, err)
			if found.Issue.Code != CodeCatalogUnreadable || found.Exit != ExitInternal || found.Issue.Hint == "" {
				t.Fatalf("error = %+v", found)
			}
		})
	}
}

func TestResolveSourceFailuresWithoutACatalogFile(t *testing.T) {
	repo := fixture.Load(t)
	t.Run("a root that is not a work tree", func(t *testing.T) {
		isolateGit(t)
		_, _, err := ResolveSource(background(), Options{FlagRoot: repo.Root, Host: realHost(repo.Root)})
		if found := asSourceError(t, err); found.Issue.Code != CodeNotGit || found.Exit != ExitUsage {
			t.Fatalf("error = %+v", found)
		}
	})
	t.Run("no git", func(t *testing.T) {
		host := fakeHost(repo.Root, "", nil)
		_, _, err := ResolveSource(background(), Options{FlagRoot: repo.Root, Host: host})
		if found := asSourceError(t, err); found.Issue.Code != CodeGitMissing || found.Exit != ExitUsage {
			t.Fatalf("error = %+v", found)
		}
	})
	t.Run("no checkout", func(t *testing.T) {
		host := fakeHost(t.TempDir(), "", nil)
		_, _, err := ResolveSource(background(), Options{Host: host})
		if found := asSourceError(t, err); found.Issue.Code != CodeRootNotFound {
			t.Fatalf("error = %+v", found)
		}
	})
}

func TestResolveSourceReportsAStaleBinary(t *testing.T) {
	dir, built := fixtureRepo(t)
	markTree(t, dir, MainModule)
	writeRepoFile(t, dir, "tooling/generator/main.go", "package main\n")
	commitAll(t, dir, "add the generator")
	built = runGitCmd(t, dir, "rev-parse", "HEAD")
	writeRepoFile(t, dir, "tooling/generator/main.go", "package main\n// changed\n")
	commitAll(t, dir, "change the generator")

	resolved, issues, err := ResolveSource(background(), Options{
		FlagRoot: dir, Host: realHost(dir), Build: generator.Build{Version: generator.Version, Commit: built},
	})
	if err != nil || len(issues) > 0 {
		t.Fatalf("err = %v issues = %v", err, issues)
	}
	if len(resolved.Warnings) != 1 || resolved.Warnings[0].Code != CodeGeneratorStale {
		t.Fatalf("warnings = %+v", resolved.Warnings)
	}
}

// The catalog built in memory from a git checkout is byte for byte the golden
// catalog, and the --catalog form records the same hash.
func TestResolveSourceMatchesTheGoldenCatalogInBothForms(t *testing.T) {
	isolateGit(t)
	dir, _ := fixtureRepo(t)
	wantBytes := fixture.Load(t).CatalogBytes

	resolved, issues, err := ResolveSource(background(), Options{FlagRoot: dir, Host: realHost(dir)})
	if err != nil || len(issues) > 0 {
		t.Fatalf("err = %v issues = %v", err, issues)
	}
	if !bytes.Equal(resolved.CatalogBytes, wantBytes) || resolved.CatalogSHA256 != sha(wantBytes) {
		t.Fatal("the in-memory catalog differs from the golden catalog")
	}
	if !resolved.Revision.Known {
		t.Fatalf("root = %+v revision = %+v", resolved.Root, resolved.Revision)
	}

	file := filepath.Join(t.TempDir(), "catalog.lock.json")
	if err := os.WriteFile(file, wantBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	fromFile, _, err := ResolveSource(background(), Options{FlagRoot: dir, CatalogFile: file, Host: realHost(dir)})
	if err != nil {
		t.Fatal(err)
	}
	if fromFile.CatalogSHA256 != resolved.CatalogSHA256 {
		t.Fatal("the --catalog form records another catalog hash")
	}
}

func TestDefaultHostReportsTheRealProcess(t *testing.T) {
	host := DefaultHost()
	wantDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	gotDir, err := host.Getwd()
	if err != nil || gotDir != wantDir {
		t.Fatalf("Getwd = %q, %v; want %q", gotDir, err, wantDir)
	}
	wantExe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	gotExe, err := host.Executable()
	if err != nil || gotExe != wantExe {
		t.Fatalf("Executable = %q, %v; want %q", gotExe, err, wantExe)
	}
	t.Setenv("INITIALIZER_DEFAULT_HOST_PROBE", "probe-value")
	if got := host.Getenv("INITIALIZER_DEFAULT_HOST_PROBE"); got != "probe-value" {
		t.Fatalf("Getenv = %q", got)
	}
	if host.MainModule == "" {
		t.Fatal("MainModule is empty")
	}
	out, err := host.Git(background(), t.TempDir(), "--version")
	if err != nil {
		t.Skipf("git is not available: %v", err)
	}
	if !strings.HasPrefix(string(out), "git version") {
		t.Fatalf("git --version = %q", out)
	}
}
