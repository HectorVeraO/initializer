package repo_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/gates/internal/repo"
)

func TestDiscoverModulesWithoutPlatformDir(t *testing.T) {
	modules, err := repo.DiscoverModules(t.TempDir())
	if err != nil || len(modules) != 0 {
		t.Fatalf("got %v, %v", modules, err)
	}
}

func TestDiscoverModulesReadsOnlyModuleAndPrefixes(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "go-srv", "internal", "platform")
	writeFile(t, filepath.Join(base, "b_mod", "module.manifest.json"), `{"module":"parameters","tablePrefixes":["sys"],"files":[1,2],"x":{}}`)
	writeFile(t, filepath.Join(base, "a_mod", "keep.go"), "package a")
	writeFile(t, filepath.Join(base, "c_mod", "module.manifest.json"), "{")
	writeFile(t, filepath.Join(base, "loose.txt"), "not a module")

	modules, err := repo.DiscoverModules(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(modules) != 3 || modules[0].Dir != "a_mod" || modules[1].Dir != "b_mod" || modules[2].Dir != "c_mod" {
		t.Fatalf("modules = %+v", modules)
	}
	if modules[0].HasManifest {
		t.Fatal("a_mod has no manifest")
	}
	b := modules[1]
	if !b.HasManifest || b.Module != "parameters" || !reflect.DeepEqual(b.TablePrefixes, []string{"sys"}) ||
		b.ManifestPath != "go-srv/internal/platform/b_mod/module.manifest.json" {
		t.Fatalf("b_mod = %+v", b)
	}
	if modules[2].ManifestError == "" {
		t.Fatal("broken JSON must set ManifestError")
	}
}

func TestDiffNameStatusAndListFiles(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	writeFile(t, filepath.Join(root, "g", "keep.txt"), "keep keep keep keep\n")
	writeFile(t, filepath.Join(root, "g", "edit.txt"), "one\n")
	writeFile(t, filepath.Join(root, "g", "gone.txt"), "bye bye bye bye bye\n")
	writeFile(t, filepath.Join(root, "g", "move.txt"), "some long stable content for rename detection\nsecond line\n")
	writeFile(t, filepath.Join(root, "other", "x.txt"), "x\n")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-m", "base")
	runGit(t, root, "switch", "-c", "topic")
	writeFile(t, filepath.Join(root, "g", "edit.txt"), "two\n")
	writeFile(t, filepath.Join(root, "g", "new file.txt"), "new\n")
	writeFile(t, filepath.Join(root, "other", "x.txt"), "changed\n")
	runGit(t, root, "rm", "-q", "g/gone.txt")
	runGit(t, root, "mv", "g/move.txt", "g/moved.txt")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-m", "change")

	changes, err := repo.DiffNameStatus(root, "main", "HEAD", "g")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]repo.Change{}
	for _, c := range changes {
		got[c.Path] = c
	}
	want := map[string]repo.Change{
		"g/edit.txt":     {Kind: repo.ChangeModified, Path: "g/edit.txt"},
		"g/new file.txt": {Kind: repo.ChangeAdded, Path: "g/new file.txt"},
		"g/gone.txt":     {Kind: repo.ChangeDeleted, Path: "g/gone.txt"},
		"g/moved.txt":    {Kind: repo.ChangeRenamed, Path: "g/moved.txt", OldPath: "g/move.txt"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}

	files, err := repo.ListFiles(root, "main", "g")
	if err != nil || !reflect.DeepEqual(files, []string{"g/edit.txt", "g/gone.txt", "g/keep.txt", "g/move.txt"}) {
		t.Fatalf("files = %v, %v", files, err)
	}
	none, err := repo.ListFiles(root, "main", "missing")
	if err != nil || len(none) != 0 {
		t.Fatalf("none = %v, %v", none, err)
	}
}

func TestDiffNameStatusRejectsBadArguments(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	writeFile(t, filepath.Join(root, "g", "a.txt"), "one\n")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-m", "base")

	// With a real commit, a missing guard lets git accept the option and write
	// the output file, which the test observes.
	outputFile := filepath.Join(root, "out.txt")
	_, err := repo.DiffNameStatus(root, "--output="+outputFile, "HEAD", "g")
	if err == nil {
		t.Fatal("option-like revision accepted")
	}
	_, statErr := os.Stat(outputFile)
	if statErr == nil {
		t.Fatal("option-like revision reached git and wrote a file")
	}
	_, err = repo.DiffNameStatus(root, "HEAD", "HEAD", "-g")
	if err == nil {
		t.Fatal("option-like path accepted")
	}
	_, err = repo.ListFiles(root, "HEAD", "")
	if err == nil {
		t.Fatal("empty path accepted")
	}
}
