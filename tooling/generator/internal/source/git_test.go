package source

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
)

// scriptedGit answers git calls by their first words; an unmatched call fails
// the test.
func scriptedGit(t *testing.T, answers map[string]func() ([]byte, error)) func(context.Context, string, ...string) ([]byte, error) {
	t.Helper()
	return func(_ context.Context, _ string, args ...string) ([]byte, error) {
		key := strings.Join(args, " ")
		for prefix, answer := range answers {
			if strings.HasPrefix(key, prefix) {
				return answer()
			}
		}
		t.Fatalf("unexpected git call: %s", key)
		return nil, nil
	}
}

func missingGit() error {
	return &exec.Error{Name: "git", Err: exec.ErrNotFound}
}

func TestTrackedFilesListsTheFilesOfAWorkTree(t *testing.T) {
	dir := newRepo(t)
	writeRepoFile(t, dir, "b.txt", "b")
	writeRepoFile(t, dir, "a/c.txt", "c")
	commitAll(t, dir, "files")
	writeRepoFile(t, dir, "untracked.txt", "u")

	files, err := TrackedFiles(background(), Root{Dir: dir}, realHost(dir))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a/c.txt", "b.txt"}; !reflect.DeepEqual(files, want) {
		t.Fatalf("files = %v, want %v (untracked files stay invisible)", files, want)
	}
}

func TestTrackedFilesRefusesWhatIsNotAWorkTreeTopLevel(t *testing.T) {
	repo := newRepo(t)
	writeRepoFile(t, repo, "sub/file.txt", "x")
	commitAll(t, repo, "files")
	plain := t.TempDir()

	for name, dir := range map[string]string{"a folder inside a work tree": filepath.Join(repo, "sub"), "a folder outside any repository": plain} {
		t.Run(name, func(t *testing.T) {
			_, err := TrackedFiles(background(), Root{Dir: dir}, realHost(dir))
			found := asSourceError(t, err)
			if found.Issue.Code != CodeNotGit || found.Exit != ExitUsage {
				t.Fatalf("error = %+v", found)
			}
			if want := "clone the repo, or pass --catalog with a catalog built elsewhere"; found.Issue.Hint != want {
				t.Fatalf("hint = %q", found.Issue.Hint)
			}
		})
	}
}

func TestTrackedFilesWithoutGit(t *testing.T) {
	dir := t.TempDir()
	for name, git := range map[string]func(context.Context, string, ...string) ([]byte, error){
		"git fails to start": func(context.Context, string, ...string) ([]byte, error) {
			return nil, missingGit()
		},
		"no git configured": nil,
	} {
		t.Run(name, func(t *testing.T) {
			host := fakeHost(dir, "", nil)
			host.Git = git
			_, err := TrackedFiles(background(), Root{Dir: dir}, host)
			found := asSourceError(t, err)
			if found.Issue.Code != CodeGitMissing || found.Exit != ExitUsage {
				t.Fatalf("error = %+v", found)
			}
			if want := "install git, or pass --catalog"; found.Issue.Hint != want {
				t.Fatalf("hint = %q", found.Issue.Hint)
			}
		})
	}
}

func TestTrackedFilesWhenGitFails(t *testing.T) {
	dir := t.TempDir()
	top := func() ([]byte, error) { return []byte(filepath.ToSlash(dir) + "\n"), nil }
	cases := map[string]map[string]func() ([]byte, error){
		"rev-parse crashes": {
			"rev-parse --show-toplevel": func() ([]byte, error) { return nil, errors.New("signal: killed") },
		},
		"ls-files exits non-zero": {
			"rev-parse --show-toplevel": top,
			"ls-files -z":               func() ([]byte, error) { return nil, &GitError{Code: 128, Stderr: "fatal: index file corrupt"} },
		},
		"ls-files prints an empty element": {
			"rev-parse --show-toplevel": top,
			"ls-files -z":               func() ([]byte, error) { return []byte("a\x00\x00b\x00"), nil },
		},
	}
	for name, answers := range cases {
		t.Run(name, func(t *testing.T) {
			host := fakeHost(dir, "", nil)
			host.Git = scriptedGit(t, answers)
			_, err := TrackedFiles(background(), Root{Dir: dir}, host)
			found := asSourceError(t, err)
			if found.Issue.Code != CodeUnreadable || found.Exit != ExitInternal {
				t.Fatalf("error = %+v", found)
			}
		})
	}
}

func TestReadRevisionOfACleanAndAModifiedTree(t *testing.T) {
	dir := newRepo(t)
	writeRepoFile(t, dir, "owned.txt", "one")
	writeRepoFile(t, dir, "other.txt", "one")
	commit := commitAll(t, dir, "files")
	host := realHost(dir)
	owned := []string{"owned.txt"}

	clean, err := ReadRevision(background(), Root{Dir: dir}, host, owned)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Revision{Commit: commit, Known: true}); clean != want {
		t.Fatalf("clean = %+v, want %+v", clean, want)
	}

	writeRepoFile(t, dir, "other.txt", "two")
	writeRepoFile(t, dir, "untracked.txt", "u")
	uncatalogued, err := ReadRevision(background(), Root{Dir: dir}, host, owned)
	if err != nil {
		t.Fatal(err)
	}
	if uncatalogued.Modified {
		t.Fatal("a modified file outside the catalog must not count")
	}

	writeRepoFile(t, dir, "owned.txt", "two")
	modified, err := ReadRevision(background(), Root{Dir: dir}, host, owned)
	if err != nil {
		t.Fatal(err)
	}
	if !modified.Modified || modified.Commit != commit {
		t.Fatalf("modified = %+v", modified)
	}

	runGitCmd(t, dir, "add", "owned.txt")
	staged, err := ReadRevision(background(), Root{Dir: dir}, host, owned)
	if err != nil {
		t.Fatal(err)
	}
	if !staged.Modified {
		t.Fatal("a staged change to a catalogued file must count")
	}
}

func TestReadRevisionCountsAnUntrackedCataloguedFile(t *testing.T) {
	dir := newRepo(t)
	writeRepoFile(t, dir, "tracked.txt", "one")
	commitAll(t, dir, "files")
	writeRepoFile(t, dir, "new.txt", "u")
	got, err := ReadRevision(background(), Root{Dir: dir}, realHost(dir), []string{"new.txt"})
	if err != nil || !got.Modified {
		t.Fatalf("revision = %+v err = %v", got, err)
	}
}

func TestReadRevisionCountsADeletedCataloguedFile(t *testing.T) {
	dir := newRepo(t)
	writeRepoFile(t, dir, "owned.txt", "one")
	commitAll(t, dir, "files")
	if err := os.Remove(filepath.Join(dir, "owned.txt")); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRevision(background(), Root{Dir: dir}, realHost(dir), []string{"owned.txt"})
	if err != nil || !got.Modified {
		t.Fatalf("revision = %+v err = %v", got, err)
	}
}

func TestReadRevisionIsUnknownWithoutAWorkTreeTopLevel(t *testing.T) {
	repo := newRepo(t)
	writeRepoFile(t, repo, "sub/file.txt", "x")
	commitAll(t, repo, "files")
	unborn := newRepo(t)

	cases := map[string]Root{
		"a folder inside a work tree":   {Dir: filepath.Join(repo, "sub")},
		"a folder outside any repo":     {Dir: t.TempDir()},
		"a repository without a commit": {Dir: unborn},
	}
	for name, root := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ReadRevision(background(), root, realHost(root.Dir), []string{"sub/file.txt"})
			if err != nil || got != (Revision{}) {
				t.Fatalf("revision = %+v err = %v", got, err)
			}
		})
	}
}

func TestReadRevisionIsUnknownWithoutGit(t *testing.T) {
	dir := t.TempDir()
	host := fakeHost(dir, "", nil)
	host.Git = func(context.Context, string, ...string) ([]byte, error) {
		return nil, missingGit()
	}
	got, err := ReadRevision(background(), Root{Dir: dir}, host, nil)
	if err != nil || got != (Revision{}) {
		t.Fatalf("revision = %+v err = %v", got, err)
	}
}

func TestReadRevisionReportsGitFailuresAfterTheTopLevelIsConfirmed(t *testing.T) {
	dir := t.TempDir()
	top := func() ([]byte, error) { return []byte(dir), nil }
	head := func() ([]byte, error) { return []byte(strings.Repeat("a", 40) + "\n"), nil }
	cases := map[string]map[string]func() ([]byte, error){
		"rev-parse crashes": {
			"rev-parse --show-toplevel": func() ([]byte, error) { return nil, errors.New("signal: killed") },
		},
		"HEAD cannot be read": {
			"rev-parse --show-toplevel": top,
			"rev-parse HEAD":            func() ([]byte, error) { return nil, errors.New("signal: killed") },
		},
		"diff fails": {
			"rev-parse --show-toplevel": top,
			"rev-parse HEAD":            head,
			"diff":                      func() ([]byte, error) { return nil, &GitError{Code: 128, Stderr: "fatal: bad"} },
		},
	}
	for name, answers := range cases {
		t.Run(name, func(t *testing.T) {
			host := fakeHost(dir, "", nil)
			host.Git = scriptedGit(t, answers)
			_, err := ReadRevision(background(), Root{Dir: dir}, host, []string{"a"})
			if found := asSourceError(t, err); found.Issue.Code != CodeUnreadable || found.Exit != ExitInternal {
				t.Fatalf("error = %+v", found)
			}
		})
	}
}

func TestReadRevisionIgnoresAHeadItCannotRecord(t *testing.T) {
	dir := t.TempDir()
	host := fakeHost(dir, "", nil)
	host.Git = scriptedGit(t, map[string]func() ([]byte, error){
		"rev-parse --show-toplevel": func() ([]byte, error) { return []byte(dir), nil },
		"rev-parse HEAD":            func() ([]byte, error) { return []byte("--not-a-commit\n"), nil },
	})
	got, err := ReadRevision(background(), Root{Dir: dir}, host, nil)
	if err != nil || got.Known {
		t.Fatalf("revision = %+v err = %v", got, err)
	}
}

func TestParseDiffNames(t *testing.T) {
	paths := []string{"a.txt", "dir/b.txt", "c d.txt"}
	cases := map[string]struct {
		diff string
		want []string
	}{
		"empty":              {"", nil},
		"one catalogued":     {"a.txt\x00", []string{"a.txt"}},
		"sorted and unique":  {"dir/b.txt\x00a.txt\x00a.txt\x00", []string{"a.txt", "dir/b.txt"}},
		"uncatalogued only":  {"z.txt\x00", nil},
		"names with spaces":  {"c d.txt\x00", []string{"c d.txt"}},
		"no trailing NUL":    {"a.txt", []string{"a.txt"}},
		"a prefix is not it": {"a.tx\x00a.txt2\x00", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := parseDiffNames([]byte(tc.diff), paths); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseDiffNames = %v, want %v", got, tc.want)
			}
		})
	}
}

func FuzzParseDiffNames(f *testing.F) {
	f.Add([]byte("a.txt\x00dir/b.txt\x00"), "a.txt\x00dir/b.txt\x00zzz")
	f.Add([]byte("\x00\x00"), "")
	f.Add([]byte("x"), "x")
	f.Fuzz(func(t *testing.T, diff []byte, joinedPaths string) {
		paths := strings.Split(joinedPaths, "\x00")
		got := parseDiffNames(diff, paths)
		allowed := map[string]bool{}
		for _, path := range paths {
			allowed[path] = true
		}
		for i, name := range got {
			if !allowed[name] {
				t.Fatalf("%q is not one of the given paths", name)
			}
			if i > 0 && got[i-1] >= name {
				t.Fatalf("result is not sorted and unique: %q", got)
			}
		}
	})
}

// staleRepo is a marked repository with a commit that built binaries can name.
func staleRepo(t *testing.T) (dir, built string) {
	t.Helper()
	dir = newRepo(t)
	markTree(t, dir, MainModule)
	writeRepoFile(t, dir, "tooling/generator/main.go", "package main\n")
	writeRepoFile(t, dir, "go-srv/main.go", "package main\n")
	return dir, commitAll(t, dir, "base")
}

func staleCheck(t *testing.T, dir, builtCommit string) ([]string, []string) {
	t.Helper()
	host := realHost(dir)
	root, err := Locate(dir, host)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := ReadRevision(background(), root, host, nil)
	if err != nil {
		t.Fatal(err)
	}
	warnings, notes, err := CheckStale(background(), root, host, generator.Build{Commit: builtCommit}, revision)
	if err != nil {
		t.Fatal(err)
	}
	var messages []string
	for _, warning := range warnings {
		if warning.Code != CodeGeneratorStale || warning.Hint != "run 'mise run cli'" {
			t.Fatalf("warning = %+v", warning)
		}
		messages = append(messages, warning.Message)
	}
	return messages, notes
}

func TestCheckStaleWarnsWhenTheGeneratorChangedSinceTheBuild(t *testing.T) {
	dir, built := staleRepo(t)
	writeRepoFile(t, dir, "tooling/generator/main.go", "package main\n// changed\n")
	head := commitAll(t, dir, "generator change")

	warnings, notes := staleCheck(t, dir, built)
	want := "this initializer was built from " + built[:7] + "; tooling/generator changed since (HEAD " + head[:7] + ")"
	if !reflect.DeepEqual(warnings, []string{want}) || len(notes) != 0 {
		t.Fatalf("warnings = %q notes = %q, want %q", warnings, notes, want)
	}
}

func TestCheckStaleAcceptsATwelveHexBuildCommit(t *testing.T) {
	dir, built := staleRepo(t)
	writeRepoFile(t, dir, "tooling/generator/main.go", "package main\n// changed\n")
	commitAll(t, dir, "generator change")
	if warnings, _ := staleCheck(t, dir, built[:12]); len(warnings) != 1 {
		t.Fatalf("warnings = %q", warnings)
	}
}

func TestCheckStaleIsQuietWhenOnlyOtherFoldersChanged(t *testing.T) {
	dir, built := staleRepo(t)
	writeRepoFile(t, dir, "go-srv/main.go", "package main\n// changed\n")
	commitAll(t, dir, "elsewhere")
	if warnings, notes := staleCheck(t, dir, built); len(warnings) != 0 || len(notes) != 0 {
		t.Fatalf("warnings = %q notes = %q", warnings, notes)
	}
}

func TestCheckStaleIsQuietAtTheBuiltCommit(t *testing.T) {
	dir, built := staleRepo(t)
	if warnings, notes := staleCheck(t, dir, built); len(warnings) != 0 || len(notes) != 0 {
		t.Fatalf("warnings = %q notes = %q", warnings, notes)
	}
}

func TestCheckStaleNotesACommitThatIsNotInTheCheckout(t *testing.T) {
	dir, _ := staleRepo(t)
	unknown := "0123456789abcdef0123456789abcdef01234567"
	warnings, notes := staleCheck(t, dir, unknown)
	want := "this initializer was built from 0123456, which is not in " + filepath.ToSlash(dir) + "; cannot tell whether it is current"
	if len(warnings) != 0 || !reflect.DeepEqual(notes, []string{want}) {
		t.Fatalf("warnings = %q notes = %q, want note %q", warnings, notes, want)
	}
}

func TestCheckStaleIsSkippedWhenItCannotApply(t *testing.T) {
	dir, built := staleRepo(t)
	writeRepoFile(t, dir, "tooling/generator/main.go", "package main\n// changed\n")
	commitAll(t, dir, "generator change")
	host := realHost(dir)
	marked, err := Locate(dir, host)
	if err != nil {
		t.Fatal(err)
	}
	known := Revision{Commit: strings.Repeat("a", 40), Known: true}
	unmarked := marked
	unmarked.Marked = false

	cases := map[string]struct {
		root     Root
		build    generator.Build
		revision Revision
	}{
		"the binary has no commit":   {marked, generator.Build{}, known},
		"the root is not marked":     {unmarked, generator.Build{Commit: built}, known},
		"the revision is unknown":    {marked, generator.Build{Commit: built}, Revision{}},
		"the commit is not a hex id": {marked, generator.Build{Commit: "--output=x"}, known},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			host.Git = func(context.Context, string, ...string) ([]byte, error) {
				t.Fatal("git must not run")
				return nil, nil
			}
			warnings, notes, err := CheckStale(background(), tc.root, host, tc.build, tc.revision)
			if err != nil || len(warnings) != 0 || len(notes) != 0 {
				t.Fatalf("warnings = %v notes = %v err = %v", warnings, notes, err)
			}
		})
	}
}

func TestCheckStaleReportsGitFailures(t *testing.T) {
	dir := t.TempDir()
	root := Root{Dir: dir, Marked: true}
	known := Revision{Commit: strings.Repeat("a", 40), Known: true}
	build := generator.Build{Commit: strings.Repeat("b", 40)}
	cases := map[string]map[string]func() ([]byte, error){
		"cat-file crashes": {
			"cat-file": func() ([]byte, error) { return nil, errors.New("signal: killed") },
		},
		"diff exits with another status": {
			"cat-file": func() ([]byte, error) { return nil, nil },
			"diff":     func() ([]byte, error) { return nil, &GitError{Code: 128, Stderr: "fatal"} },
		},
	}
	for name, answers := range cases {
		t.Run(name, func(t *testing.T) {
			host := fakeHost(dir, "", nil)
			host.Git = scriptedGit(t, answers)
			_, _, err := CheckStale(background(), root, host, build, known)
			if found := asSourceError(t, err); found.Issue.Code != CodeUnreadable || found.Exit != ExitInternal {
				t.Fatalf("error = %+v", found)
			}
		})
	}
}
