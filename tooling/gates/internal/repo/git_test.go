package repo_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/gates/internal/repo"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{
		"-C", dir,
		"-c", "user.name=Example Tester",
		"-c", "user.email=tester@example.com",
		"-c", "commit.gpgsign=false",
	}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

// newRepo builds main with flags.yaml "one", then branch topic with "two".
func newRepo(t *testing.T) string {
	t.Helper()
	_, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	writeFile(t, filepath.Join(dir, "flags.yaml"), "one\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "first")
	runGit(t, dir, "switch", "-c", "topic")
	writeFile(t, filepath.Join(dir, "flags.yaml"), "two\n")
	writeFile(t, filepath.Join(dir, "other.txt"), "x\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "second")
	return dir
}

func TestResolveRev(t *testing.T) {
	dir := newRepo(t)
	sha, err := repo.ResolveRev(dir, "main")
	if err != nil || len(sha) != 40 {
		t.Fatalf("ResolveRev(main) = %q, %v", sha, err)
	}
	_, err = repo.ResolveRev(dir, "origin/main")
	if !errors.Is(err, repo.ErrRevNotFound) {
		t.Fatalf("ResolveRev(origin/main) error = %v, want ErrRevNotFound", err)
	}
}

func TestResolveRevRejectsOptionLookingRevisions(t *testing.T) {
	dir := newRepo(t)
	for _, rev := range []string{"", "--output=x", "-h"} {
		_, err := repo.ResolveRev(dir, rev)
		if err == nil || errors.Is(err, repo.ErrRevNotFound) {
			t.Errorf("ResolveRev(%q) error = %v, want a validation error", rev, err)
		}
	}
}

func TestResolveRevOutsideRepositoryIsAnError(t *testing.T) {
	_ = newRepo(t)
	_, err := repo.ResolveRev(t.TempDir(), "main")
	if err == nil || errors.Is(err, repo.ErrRevNotFound) {
		t.Fatalf("error = %v, want an operational error", err)
	}
}

func TestMergeBaseIsTheBranchPoint(t *testing.T) {
	dir := newRepo(t)
	mainSha, _ := repo.ResolveRev(dir, "main")
	got, err := repo.MergeBase(dir, "main", "HEAD")
	if err != nil || got != mainSha {
		t.Fatalf("MergeBase = %q, %v; want %q", got, err, mainSha)
	}
}

func TestMergeBaseIgnoresCommitsAddedToBaseAfterBranching(t *testing.T) {
	dir := newRepo(t)
	branchPoint, _ := repo.ResolveRev(dir, "main")
	runGit(t, dir, "switch", "main")
	writeFile(t, filepath.Join(dir, "later.txt"), "later\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "main moves on")
	runGit(t, dir, "switch", "topic")
	got, err := repo.MergeBase(dir, "main", "HEAD")
	if err != nil || got != branchPoint {
		t.Fatalf("MergeBase = %q, %v; want the branch point %q", got, err, branchPoint)
	}
}

func TestShowFile(t *testing.T) {
	dir := newRepo(t)
	content, found, err := repo.ShowFile(dir, "main", "flags.yaml")
	if err != nil || !found || string(content) != "one\n" {
		t.Fatalf("ShowFile(main) = %q, %v, %v", content, found, err)
	}
	_, found, err = repo.ShowFile(dir, "main", "other.txt")
	if err != nil || found {
		t.Fatalf("ShowFile of an absent file = found %v, err %v; want absent without error", found, err)
	}
	_, _, err = repo.ShowFile(dir, "no-such-rev", "flags.yaml")
	if err == nil {
		t.Fatal("ShowFile with a bad revision must fail, not report absent")
	}
}

func TestShowFileIsRelativeToDir(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, filepath.Join(dir, "sub", "flags.yaml"), "nested\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "nested")
	content, found, err := repo.ShowFile(filepath.Join(dir, "sub"), "HEAD", "flags.yaml")
	if err != nil || !found || string(content) != "nested\n" {
		t.Fatalf("ShowFile(sub) = %q, %v, %v", content, found, err)
	}
}
