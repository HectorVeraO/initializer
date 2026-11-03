package upgrade

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/emit"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
)

// projectDir writes files under a new folder: the project an upgrade reads.
func projectDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for path, data := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// read returns the content at path, or "" when it is absent.
func read(t *testing.T, dir, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(data)
}

// statuses returns the outcome of every planned path.
func statuses(result Result) map[string]Status {
	found := map[string]Status{}
	for _, file := range result.Files {
		found[file.Path] = file.Status
	}
	return found
}

func planned(pairs ...string) []emit.File {
	files := make([]emit.File, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		files = append(files, emit.File{Path: pairs[i], Data: []byte(pairs[i+1])})
	}
	return files
}

func locked(path, data string) lockfile.File {
	return lockfile.File{Path: path, SHA256: hashHex([]byte(data))}
}

func TestApplyReplacesOnlyTheFilesTheLockVouchesFor(t *testing.T) {
	dir := projectDir(t, map[string]string{
		"a.go":        "old a",
		"b.go":        "mine",
		"c.go":        "same",
		"seed":        "left",
		"other":       "not in the plan",
		"nested/e.go": "mine too",
	})
	project := emit.Project{Files: append(planned(
		"a.go", "new a",
		"b.go", "theirs",
		"c.go", "same",
		"d.go", "new d",
	), emit.File{Path: lockfile.Name, Data: []byte("new lock")})}
	lock := lockfile.Lock{
		Specs: []lockfile.Spec{{Spec: "core", Files: []lockfile.File{
			locked("a.go", "old a"), locked("b.go", "old b"), locked("c.go", "same"),
		}}},
	}

	result, err := Apply(Options{Dir: dir, Project: project, Lock: lock})
	if err != nil {
		t.Fatal(err)
	}
	got := statuses(result)
	want := map[string]Status{
		"a.go": Upgraded, "b.go": SkippedModified, "c.go": Unchanged, "d.go": Added,
		lockfile.Name: Upgraded,
	}
	for path, status := range want {
		if got[path] != status {
			t.Errorf("%s: status %q, want %q", path, got[path], status)
		}
	}
	if len(result.Written) != 3 {
		t.Errorf("written %v, want a.go, d.go and the lock", result.Written)
	}
	if content := read(t, dir, "a.go"); content != "new a" {
		t.Errorf("a.go = %q, want the planned content", content)
	}
	if content := read(t, dir, "b.go"); content != "mine" {
		t.Errorf("b.go = %q, want the project's content", content)
	}
	if content := read(t, dir, "d.go"); content != "new d" {
		t.Errorf("d.go = %q, want the planned content", content)
	}
	if content := read(t, dir, lockfile.Name); content != "new lock" {
		t.Errorf("the lock = %q, want the new one", content)
	}
	if content := read(t, dir, "other"); content != "not in the plan" {
		t.Errorf("other = %q, the upgrade touched a file it does not deliver", content)
	}

	result, err = Apply(Options{Dir: dir, Project: project, Lock: lock, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses(result)["b.go"]; got != Upgraded {
		t.Errorf("with --force b.go: status %q, want %q", got, Upgraded)
	}
	if content := read(t, dir, "b.go"); content != "theirs" {
		t.Errorf("with --force b.go = %q, want the planned content", content)
	}
}

func TestApplyNeverWritesASeed(t *testing.T) {
	dir := projectDir(t, map[string]string{"equal.go": "planned", "edited.go": "mine"})
	project := emit.Project{Files: []emit.File{
		{Path: "equal.go", Data: []byte("planned"), Seed: true},
		{Path: "edited.go", Data: []byte("planned"), Seed: true},
		{Path: "absent.go", Data: []byte("planned"), Seed: true},
		{Path: lockfile.Name, Data: []byte("lock")},
	}}
	result, err := Apply(Options{Dir: dir, Project: project, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	got := statuses(result)
	want := map[string]Status{
		"equal.go": Unchanged, "edited.go": SkippedSeed, "absent.go": SkippedSeed, lockfile.Name: Upgraded,
	}
	for path, status := range want {
		if got[path] != status {
			t.Errorf("%s: status %q, want %q", path, got[path], status)
		}
	}
	if content := read(t, dir, "edited.go"); content != "mine" {
		t.Errorf("edited.go = %q, the upgrade wrote a seed", content)
	}
	if content := read(t, dir, "absent.go"); content != "" {
		t.Errorf("absent.go = %q, the upgrade created a seed", content)
	}
	for _, path := range result.Written {
		if path != lockfile.Name {
			t.Errorf("written %q, only the lock may be written", path)
		}
	}
}

func TestApplyTreatsAFileOutsideTheLockAsEdited(t *testing.T) {
	dir := projectDir(t, map[string]string{"same.go": "planned", "mine.go": "mine"})
	project := emit.Project{Files: planned(
		"same.go", "planned",
		"mine.go", "theirs",
		"theirs.go", "theirs",
	)}
	result, err := Apply(Options{Dir: dir, Project: project})
	if err != nil {
		t.Fatal(err)
	}
	got := statuses(result)
	if got["same.go"] != Unchanged || got["mine.go"] != SkippedModified || got["theirs.go"] != Added {
		t.Fatalf("statuses %v", got)
	}
	if content := read(t, dir, "mine.go"); content != "mine" {
		t.Errorf("mine.go = %q, want the project's content", content)
	}
}

// A path the plan delivers and the lock records that the plan still delivers
// is not stale; a locked path the plan dropped is, and it stays on disk.
func TestApplyListsOnlyTheLockedPathsThePlanDropped(t *testing.T) {
	dir := projectDir(t, map[string]string{"kept.go": "here", "stale.go": "mine", lockfile.Name: "lock"})
	project := emit.Project{Files: []emit.File{
		{Path: "kept.go", Data: []byte("here")},
		{Path: lockfile.Name, Data: []byte("lock")},
	}}
	lock := lockfile.Lock{
		Modules: []lockfile.Module{{Stack: "go-srv", Module: "core", Files: []lockfile.File{
			locked("kept.go", "here"), locked("stale.go", "old"),
		}}},
	}
	result, err := Apply(Options{Dir: dir, Project: project, Lock: lock})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Stale) != 1 || result.Stale[0] != "stale.go" {
		t.Fatalf("stale = %v, want only stale.go", result.Stale)
	}
	if got := statuses(result)["kept.go"]; got != Unchanged {
		t.Errorf("kept.go: status %q, want unchanged", got)
	}
	if content := read(t, dir, "stale.go"); content != "mine" {
		t.Errorf("stale.go = %q, the upgrade touched a file it no longer delivers", content)
	}
	if len(result.Written) != 0 {
		t.Errorf("written %v, want nothing", result.Written)
	}
}

func TestApplyDoesNotRestoreADeletedFileWithoutForce(t *testing.T) {
	dir := projectDir(t, nil)
	project := emit.Project{Files: planned("gone.go", "planned")}
	lock := lockfile.Lock{Generated: []lockfile.File{locked("gone.go", "planned")}}

	result, err := Apply(Options{Dir: dir, Project: project, Lock: lock})
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses(result)["gone.go"]; got != SkippedModified {
		t.Fatalf("gone.go: status %q, want %q", got, SkippedModified)
	}
	if content := read(t, dir, "gone.go"); content != "" {
		t.Fatalf("gone.go = %q, the upgrade restored a deleted file", content)
	}

	result, err = Apply(Options{Dir: dir, Project: project, Lock: lock, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses(result)["gone.go"]; got != Upgraded {
		t.Fatalf("with --force gone.go: status %q, want %q", got, Upgraded)
	}
	if content := read(t, dir, "gone.go"); content != "planned" {
		t.Errorf("with --force gone.go = %q, want the planned content", content)
	}
}

func TestApplyRefusesASymlinkAtADeliveredPath(t *testing.T) {
	dir := projectDir(t, nil)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "file.go")); err != nil {
		t.Skipf("this machine cannot create a symlink: %v", err)
	}
	project := emit.Project{Files: planned("file.go", "planned")}
	result, err := Apply(Options{Dir: dir, Project: project, Lock: lockfile.Lock{}, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses(result)["file.go"]; got != SkippedNotFile {
		t.Fatalf("file.go: status %q, want %q", got, SkippedNotFile)
	}
	if len(result.Written) != 0 || !result.Skipped() {
		t.Fatalf("written %v, skipped %v", result.Written, result.Skipped())
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "keep me" {
		t.Fatalf("the symlink target is %q (%v), want it untouched", data, err)
	}
}

func TestApplyRefusesADirectoryAtADeliveredPath(t *testing.T) {
	dir := projectDir(t, nil)
	if err := os.MkdirAll(filepath.Join(dir, "file.go"), 0o755); err != nil {
		t.Fatal(err)
	}
	project := emit.Project{Files: planned("file.go", "planned")}
	result, err := Apply(Options{Dir: dir, Project: project, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses(result)["file.go"]; got != SkippedNotFile {
		t.Fatalf("file.go: status %q, want %q", got, SkippedNotFile)
	}
	info, err := os.Stat(filepath.Join(dir, "file.go"))
	if err != nil || !info.IsDir() {
		t.Fatalf("the folder at the delivered path was replaced: %v", err)
	}
}

func TestApplyRefusesAFolderThatLeavesTheProject(t *testing.T) {
	dir := projectDir(t, nil)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "linked")); err != nil {
		t.Skipf("this machine cannot create a symlink: %v", err)
	}
	project := emit.Project{Files: planned("linked/x.go", "planned")}
	result, err := Apply(Options{Dir: dir, Project: project, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses(result)["linked/x.go"]; got != SkippedNotFile {
		t.Fatalf("linked/x.go: status %q, want %q", got, SkippedNotFile)
	}
	if len(result.Written) != 0 {
		t.Fatalf("written %v, want nothing", result.Written)
	}
	if _, err := os.Stat(filepath.Join(outside, "x.go")); err == nil {
		t.Fatal("the upgrade wrote through a folder that leaves the project")
	}
}

func TestApplyRefusesAFolderThatIsASymlinkEvenWhenItStaysInside(t *testing.T) {
	dir := projectDir(t, map[string]string{"real/keep.txt": "keep"})
	if err := os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "linked")); err != nil {
		t.Skipf("this machine cannot create a symlink: %v", err)
	}
	project := emit.Project{Files: planned("linked/x.go", "planned")}
	result, err := Apply(Options{Dir: dir, Project: project})
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses(result)["linked/x.go"]; got != SkippedNotFile {
		t.Fatalf("linked/x.go: status %q, want %q", got, SkippedNotFile)
	}
	if content := read(t, dir, "real/x.go"); content != "" {
		t.Errorf("real/x.go = %q, want the target untouched", content)
	}
}

func TestApplyDryRunWritesNothing(t *testing.T) {
	dir := projectDir(t, map[string]string{"a.go": "old a", lockfile.Name: "old lock"})
	project := emit.Project{Files: append(planned("a.go", "new a", "b.go", "new b"),
		emit.File{Path: lockfile.Name, Data: []byte("new lock")})}
	lock := lockfile.Lock{Generated: []lockfile.File{locked("a.go", "old a")}}
	result, err := Apply(Options{Dir: dir, Project: project, Lock: lock, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	got := statuses(result)
	if got["a.go"] != Upgraded || got["b.go"] != Added || got[lockfile.Name] != Upgraded {
		t.Fatalf("statuses %v", got)
	}
	if len(result.Written) != 0 {
		t.Errorf("a dry run wrote %v", result.Written)
	}
	if content := read(t, dir, "a.go"); content != "old a" {
		t.Errorf("a.go = %q, a dry run wrote it", content)
	}
	if content := read(t, dir, "b.go"); content != "" {
		t.Errorf("b.go = %q, a dry run created it", content)
	}
	if content := read(t, dir, lockfile.Name); content != "old lock" {
		t.Errorf("the lock = %q, a dry run wrote it", content)
	}
}
