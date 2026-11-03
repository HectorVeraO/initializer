package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
)

// fixtureSelection is the selection every upgrade test generates from: one
// backend with the parameters module and a name no folder gives.
var fixtureSelection = []string{"--name", "acme", "--stacks", "go-srv", "--modules", "parameters"}

// newerCatalog copies the fixture checkout, changes one delivered file, adds a
// file the old catalog did not deliver, and returns the checkout and the
// catalog built from it. It is the "current" side of an upgrade.
func newerCatalog(t *testing.T) (root, catalogPath string) {
	t.Helper()
	repo := fixture.Load(t)
	root = t.TempDir()
	if err := os.CopyFS(root, os.DirFS(repo.Root)); err != nil {
		t.Fatalf("copy the fixture: %v", err)
	}
	changed := filepath.Join(root, "specs", "parameters", "openapi.yaml")
	data, err := os.ReadFile(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(changed, append(data, []byte("# upgraded\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	const added = "specs/parameters/upgrade.md"
	if err := fixture.WriteFile(root, added, []byte("# Upgrade\n")); err != nil {
		t.Fatal(err)
	}
	listed, err := os.ReadFile(filepath.Join(fixture.Dir(t), "files.txt"))
	if err != nil {
		t.Fatalf("read files.txt: %v", err)
	}
	files := append(strings.Fields(string(listed)), added)
	sort.Strings(files)
	built, issues, err := catalog.Build(catalog.BuildOptions{Root: os.DirFS(root), Files: files, Generator: generator.Version})
	if err != nil || len(issues) > 0 {
		t.Fatalf("build the newer catalog: %v %v", err, issues)
	}
	encoded, err := catalog.Encode(built)
	if err != nil {
		t.Fatalf("encode the newer catalog: %v", err)
	}
	catalogPath = filepath.Join(t.TempDir(), "catalog.lock.json")
	if err := os.WriteFile(catalogPath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	return root, catalogPath
}

// upgradeReport is the members of the upgrade document every test reads.
type upgradeReport struct {
	OK     bool `json:"ok"`
	DryRun bool `json:"dryRun"`
	Files  []struct{ Path, Status string }
	Counts struct {
		Added           int `json:"added"`
		Upgraded        int `json:"upgraded"`
		Unchanged       int `json:"unchanged"`
		SkippedModified int `json:"skippedModified"`
		SkippedSeed     int `json:"skippedSeed"`
		SkippedNotFile  int `json:"skippedNotFile"`
	}
	Written int      `json:"written"`
	Stale   []string `json:"stale"`
}

// statusesOf returns the status of every reported path.
func statusesOf(report upgradeReport) map[string]string {
	found := map[string]string{}
	for _, file := range report.Files {
		found[file.Path] = file.Status
	}
	return found
}

// upgradeArgs is the upgrade line of one test: DIR with the given source.
func upgradeArgs(dir, root, catalogPath string, extra ...string) []string {
	args := []string{"upgrade", dir, "--root", root, "--catalog", catalogPath}
	return append(args, extra...)
}

// generateFromFixture generates fixtureSelection into e.out.
func generateFromFixture(t *testing.T, e env) {
	t.Helper()
	if code, _, stderr := execute(t, e.args(fixtureSelection...)...); code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
}

// The tree and the lock an upgrade leaves are the ones a fresh generation from
// the same catalog delivers.
func TestUpgradeFromAFixtureCatalogMatchesAFreshGeneration(t *testing.T) {
	e := newEnv(t)
	generateFromFixture(t, e)
	srv := folderOf(t, e.out, "go-srv")

	root, catalogPath := newerCatalog(t)
	fresh := env{root: root, catalog: catalogPath, out: filepath.Join(t.TempDir(), "fresh")}
	recorded := filepath.Join(e.out, lockfile.SelectionName)
	if code, _, stderr := execute(t, fresh.args("-f", recorded)...); code != exitOK {
		t.Fatalf("fresh generation exit %d: %s", code, stderr)
	}

	code, stdout, stderr := executeJSON(t, upgradeArgs(e.out, root, catalogPath)...)
	if code != exitOK {
		t.Fatalf("upgrade exit %d: %s %s", code, stdout, stderr)
	}
	var report upgradeReport
	decodeDocument(t, stdout, &report)
	if !report.OK {
		t.Fatalf("upgrade reported ok=false: %s", stdout)
	}
	status := statusesOf(report)
	cases := map[string]string{
		"specs/parameters/openapi.yaml":         "upgraded",
		"specs/parameters/upgrade.md":           "added",
		"initializer.lock.json":                 "upgraded",
		srv + "/cmd/server/product.go":          "unchanged",
		srv + "/internal/platform/core/core.go": "unchanged",
	}
	for path, want := range cases {
		if status[path] != want {
			t.Errorf("%s: status %q, want %q", path, status[path], want)
		}
	}
	if report.Counts.SkippedModified != 0 || report.Counts.SkippedSeed != 0 {
		t.Errorf("a clean project must upgrade everything: %+v", report.Counts)
	}
	if len(report.Files) != len(tree(t, fresh.out)) {
		t.Errorf("the report names %d files, the fresh tree holds %d", len(report.Files), len(tree(t, fresh.out)))
	}

	got, want := tree(t, e.out), tree(t, fresh.out)
	if len(got) != len(want) {
		t.Fatalf("file sets differ: %d after the upgrade, %d after a generation", len(got), len(want))
	}
	for path, content := range want {
		if got[path] != content {
			t.Errorf("%s differs from a fresh generation", path)
		}
	}
	if got[lockfile.Name] != want[lockfile.Name] {
		t.Errorf("the lock differs from a fresh generation:\n%s\nwant:\n%s", got[lockfile.Name], want[lockfile.Name])
	}
}

// A file the project edited survives the upgrade, is reported, and is replaced
// only by --force.
func TestUpgradeLeavesAnEditedFileAndForceReplacesIt(t *testing.T) {
	e := newEnv(t)
	generateFromFixture(t, e)
	srv := folderOf(t, e.out, "go-srv")
	edited := srv + "/internal/platform/core/core.go"
	path := filepath.Join(e.out, filepath.FromSlash(edited))
	generated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mine := []byte("package core\n\n// the project's own change\n")
	if err := os.WriteFile(path, mine, 0o644); err != nil {
		t.Fatal(err)
	}
	lockBefore, err := os.ReadFile(filepath.Join(e.out, lockfile.Name))
	if err != nil {
		t.Fatal(err)
	}

	root, catalogPath := newerCatalog(t)
	code, stdout, stderr := executeJSON(t, upgradeArgs(e.out, root, catalogPath, "--dry-run")...)
	if code != exitIssues {
		t.Fatalf("dry run exit %d, want %d: %s %s", code, exitIssues, stdout, stderr)
	}
	var dry upgradeReport
	decodeDocument(t, stdout, &dry)
	if !dry.DryRun || dry.Written != 0 || statusesOf(dry)[edited] != "skipped-modified" {
		t.Fatalf("dry run report: %s", stdout)
	}
	if kept, err := os.ReadFile(path); err != nil || string(kept) != string(mine) {
		t.Fatalf("a dry run wrote the edited file: %v", err)
	}
	if lock, err := os.ReadFile(filepath.Join(e.out, lockfile.Name)); err != nil || string(lock) != string(lockBefore) {
		t.Fatalf("a dry run wrote the lock: %v", err)
	}

	code, stdout, stderr = executeJSON(t, upgradeArgs(e.out, root, catalogPath)...)
	if code != exitIssues {
		t.Fatalf("upgrade exit %d, want %d: %s %s", code, exitIssues, stdout, stderr)
	}
	var report upgradeReport
	decodeDocument(t, stdout, &report)
	if report.OK || report.Counts.SkippedModified != 1 || statusesOf(report)[edited] != "skipped-modified" {
		t.Fatalf("upgrade report: %s", stdout)
	}
	if kept, err := os.ReadFile(path); err != nil || string(kept) != string(mine) {
		t.Fatalf("the upgrade wrote the edited file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.out, "specs", "parameters", "upgrade.md")); err != nil {
		t.Fatalf("the upgrade must still deliver the other files: %v", err)
	}
	if !strings.Contains(stdout, `"skippedModified":1`) {
		t.Fatalf("the report does not name the skipped file: %s", stdout)
	}

	code, stdout, stderr = executeJSON(t, upgradeArgs(e.out, root, catalogPath, "--force")...)
	if code != exitOK {
		t.Fatalf("forced upgrade exit %d: %s %s", code, stdout, stderr)
	}
	decodeDocument(t, stdout, &report)
	if report.Counts.SkippedModified != 0 {
		t.Fatalf("a forced run must skip nothing as modified: %+v", report.Counts)
	}
	if statusesOf(report)[edited] != "upgraded" {
		t.Fatalf("the forced upgrade did not report the file as upgraded: %s", stdout)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(generated) {
		t.Fatalf("--force did not replace the edited file: %v", err)
	}

	code, stdout, _ = executeJSON(t, upgradeArgs(e.out, root, catalogPath)...)
	if code != exitOK {
		t.Fatalf("a replaced file must upgrade cleanly: exit %d %s", code, stdout)
	}
}

// A seed belongs to the project: the upgrade reports it and never writes it,
// whatever its state.
func TestUpgradeNeverWritesASeed(t *testing.T) {
	e := newEnv(t)
	generateFromFixture(t, e)
	srv := folderOf(t, e.out, "go-srv")
	seed := srv + "/cmd/server/product.go"
	path := filepath.Join(e.out, filepath.FromSlash(seed))
	mine := []byte("package main\n\n// the project's own product\n")
	if err := os.WriteFile(path, mine, 0o644); err != nil {
		t.Fatal(err)
	}

	root, catalogPath := newerCatalog(t)
	code, stdout, stderr := executeJSON(t, upgradeArgs(e.out, root, catalogPath)...)
	if code != exitOK {
		t.Fatalf("a seed must not fail the upgrade: exit %d: %s %s", code, stdout, stderr)
	}
	var report upgradeReport
	decodeDocument(t, stdout, &report)
	if report.Counts.SkippedSeed != 1 || statusesOf(report)[seed] != "skipped-seed" {
		t.Fatalf("the seed is not reported as skipped-seed: %s", stdout)
	}
	if kept, err := os.ReadFile(path); err != nil || string(kept) != string(mine) {
		t.Fatalf("the upgrade wrote the seed: %v", err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ = executeJSON(t, upgradeArgs(e.out, root, catalogPath)...)
	if code != exitOK {
		t.Fatalf("a missing seed must not fail the upgrade: exit %d %s", code, stdout)
	}
	decodeDocument(t, stdout, &report)
	if statusesOf(report)[seed] != "skipped-seed" {
		t.Fatalf("a missing seed is not reported as skipped-seed: %s", stdout)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the upgrade recreated the seed")
	}
}

// A second upgrade reports every file unchanged and writes nothing.
func TestASecondUpgradeChangesNothing(t *testing.T) {
	e := newEnv(t)
	generateFromFixture(t, e)
	root, catalogPath := newerCatalog(t)
	if code, _, stderr := execute(t, upgradeArgs(e.out, root, catalogPath)...); code != exitOK {
		t.Fatalf("first upgrade exit %d: %s", code, stderr)
	}
	before := tree(t, e.out)
	lock := filepath.Join(e.out, lockfile.Name)
	lockInfo, err := os.Stat(lock)
	if err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := executeJSON(t, upgradeArgs(e.out, root, catalogPath)...)
	if code != exitOK {
		t.Fatalf("second upgrade exit %d: %s %s", code, stdout, stderr)
	}
	var report upgradeReport
	decodeDocument(t, stdout, &report)
	if report.Written != 0 || report.Counts.Unchanged != len(before) {
		t.Fatalf("the second run wrote files: %+v of %d", report, len(before))
	}
	for _, file := range report.Files {
		if file.Status != "unchanged" {
			t.Errorf("%s: status %q, want unchanged", file.Path, file.Status)
		}
	}
	after := tree(t, e.out)
	if len(after) != len(before) {
		t.Fatalf("file sets differ: %d then %d", len(before), len(after))
	}
	for path, content := range before {
		if after[path] != content {
			t.Errorf("%s changed on the second run", path)
		}
	}
	lockInfoAfter, err := os.Stat(lock)
	if err != nil {
		t.Fatal(err)
	}
	if !lockInfoAfter.ModTime().Equal(lockInfo.ModTime()) {
		t.Errorf("the second run rewrote the lock")
	}
}

// The text report names the counts and tells the reader how to replace a
// skipped file.
func TestUpgradeTextReportCountsAndForceHint(t *testing.T) {
	e := newEnv(t)
	generateFromFixture(t, e)
	srv := folderOf(t, e.out, "go-srv")
	edited := srv + "/internal/platform/core/core.go"
	if err := os.WriteFile(filepath.Join(e.out, filepath.FromSlash(edited)), []byte("package core\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	planned := len(tree(t, e.out))

	code, stdout, stderr := execute(t, upgradeArgs(e.out, e.root, e.catalog)...)
	if code != exitIssues {
		t.Fatalf("exit %d, want %d: %s %s", code, exitIssues, stdout, stderr)
	}
	if !strings.HasPrefix(stdout, "Project acme in "+filepath.ToSlash(e.out)+"\n") {
		t.Fatalf("stdout starts %q", stdout)
	}
	if !strings.Contains(stdout, "skipped-modified "+edited+"\n") {
		t.Fatalf("stdout does not report the edited file: %s", stdout)
	}
	wantTail := fmt.Sprintf("%d unchanged, 1 skipped-modified.\n1 file skipped as modified; pass --force to replace it.\n", planned-1)
	if !strings.HasSuffix(stdout, wantTail) {
		t.Fatalf("stdout ends %q, want %q", stdout, wantTail)
	}
	if got := strings.Count(stdout, "\n"); got != planned+3 {
		t.Fatalf("stdout has %d lines, want one header, %d files and two summaries", got, planned)
	}
}

// A locked file the project deleted is a local change: reported, left absent,
// and restored only by --force.
func TestUpgradeDoesNotRestoreADeletedFileWithoutForce(t *testing.T) {
	e := newEnv(t)
	generateFromFixture(t, e)
	srv := folderOf(t, e.out, "go-srv")
	removed := srv + "/internal/platform/core/core.go"
	path := filepath.Join(e.out, filepath.FromSlash(removed))
	generated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := executeJSON(t, upgradeArgs(e.out, e.root, e.catalog)...)
	if code != exitIssues {
		t.Fatalf("exit %d, want %d: %s %s", code, exitIssues, stdout, stderr)
	}
	var report upgradeReport
	decodeDocument(t, stdout, &report)
	if report.Counts.SkippedModified != 1 || statusesOf(report)[removed] != "skipped-modified" {
		t.Fatalf("a deleted file must be reported as modified: %s", stdout)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the upgrade restored a file the project deleted")
	}

	code, stdout, stderr = executeJSON(t, upgradeArgs(e.out, e.root, e.catalog, "--force")...)
	if code != exitOK {
		t.Fatalf("forced exit %d: %s %s", code, stdout, stderr)
	}
	decodeDocument(t, stdout, &report)
	if report.Counts.SkippedModified != 0 || statusesOf(report)[removed] != "upgraded" {
		t.Fatalf("--force did not restore the deleted file: %s", stdout)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != string(generated) {
		t.Fatalf("the restored file differs: %v", err)
	}
}

// A path that is not a regular file is reported and never written, whatever
// --force says.
func TestUpgradeRefusesANonRegularFileAtADeliveredPath(t *testing.T) {
	e := newEnv(t)
	generateFromFixture(t, e)
	srv := folderOf(t, e.out, "go-srv")
	blocked := srv + "/internal/platform/core/core.go"
	path := filepath.Join(e.out, filepath.FromSlash(blocked))
	planned := len(tree(t, e.out))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := execute(t, upgradeArgs(e.out, e.root, e.catalog, "--force")...)
	if code != exitIssues {
		t.Fatalf("exit %d, want %d: %s %s", code, exitIssues, stdout, stderr)
	}
	if !strings.Contains(stdout, "skipped-not-file "+blocked+"\n") {
		t.Fatalf("stdout does not report the path: %s", stdout)
	}
	wantTail := fmt.Sprintf("%d unchanged, 1 skipped-not-file.\n1 file skipped because a symlink or another non-regular file is in the way; move it away and run again.\n", planned-1)
	if !strings.HasSuffix(stdout, wantTail) {
		t.Fatalf("stdout ends %q, want %q", stdout, wantTail)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatalf("the folder at the delivered path was replaced: %v", err)
	}
}
