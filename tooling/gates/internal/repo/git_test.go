package repo_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestTrailerValuesFindsOnlyRealTrailersOnTouchingCommits(t *testing.T) {
	dir := newRepo(t)
	keys := []string{"BREAKING CHANGE", "BREAKING-CHANGE"}

	// Body line without a blank line before it is not a trailer.
	writeFile(t, filepath.Join(dir, "specs", "a", "f.txt"), "1\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "feature(a): Change a\n\nBody text\nBREAKING CHANGE: not a trailer")
	got, err := repo.TrailerValues(dir, "main", "HEAD", "specs/a/", keys)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("body line returned as trailer: %v", got)
	}

	// Real trailer on a commit touching another path is not counted.
	writeFile(t, filepath.Join(dir, "specs", "b", "f.txt"), "1\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "feature(b): Change b\n\nWhy.\n\nBREAKING CHANGE: b wire change")
	got, err = repo.TrailerValues(dir, "main", "HEAD", "specs/a/", keys)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("trailer of another path counted: %v", got)
	}

	writeFile(t, filepath.Join(dir, "specs", "a", "f.txt"), "2\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "feature(a): Break a\n\nWhy.\n\nBREAKING CHANGE: a wire change")
	got, err = repo.TrailerValues(dir, "main", "HEAD", "specs/a/", keys)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "a wire change" {
		t.Fatalf("got %v", got)
	}
}

func TestTrailerValuesAcceptsIndentedContinuationLines(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, filepath.Join(dir, "specs", "a", "f.txt"), "1\n")
	runGit(t, dir, "add", "-A")
	message := "feature(a): Break a\n\nWhy.\n\nBREAKING CHANGE: a wire change\n  that spans two lines\nRefs: #1"
	runGit(t, dir, "commit", "-m", message)
	got, err := repo.TrailerValues(dir, "main", "HEAD", "specs/a/", []string{"BREAKING CHANGE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "a wire change" {
		t.Fatalf("got %v", got)
	}
}

func TestTrailerValuesAcceptsUnindentedWrapOfBreakingChange(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, filepath.Join(dir, "specs", "a", "f.txt"), "1\n")
	runGit(t, dir, "add", "-A")
	message := "feature(a): Break a\n\nWhy.\n\nBREAKING CHANGE: a wire change\nthat spans two lines\nRefs: #1\nRefs: #2"
	runGit(t, dir, "commit", "-m", message)
	got, err := repo.TrailerValues(dir, "main", "HEAD", "specs/a/", []string{"BREAKING CHANGE", "Refs"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a wire change", "#1", "#2"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTrailerValuesAcceptsUnindentedWrapOfHyphenatedBreakingChange(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, filepath.Join(dir, "specs", "a", "f.txt"), "1\n")
	runGit(t, dir, "add", "-A")
	message := "feature(a): Break a\n\nWhy.\n\nBREAKING-CHANGE: a wire change\nthat spans two lines\nRefs: #1"
	runGit(t, dir, "commit", "-m", message)
	got, err := repo.TrailerValues(dir, "main", "HEAD", "specs/a/", []string{"BREAKING-CHANGE", "Refs"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a wire change", "#1"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTrailerValuesDoesNotWrapOtherTrailers(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, filepath.Join(dir, "specs", "a", "f.txt"), "1\n")
	runGit(t, dir, "add", "-A")
	message := "feature(a): Break a\n\nWhy.\n\nRefs: #1\nstray prose\nBREAKING CHANGE: a wire change"
	runGit(t, dir, "commit", "-m", message)
	got, err := repo.TrailerValues(dir, "main", "HEAD", "specs/a/", []string{"BREAKING CHANGE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("prose after a non-breaking trailer made the footer valid: %v", got)
	}
}

func TestTrailerValuesRejectsBadInput(t *testing.T) {
	dir := newRepo(t)
	cases := []struct {
		name, from, path string
		keys             []string
	}{
		{"option rev", "--all", "specs/", []string{"X"}},
		{"option path", "main", "--x", []string{"X"}},
		{"no keys", "main", "specs/", nil},
		{"bad key", "main", "specs/", []string{"a:b"}},
	}
	for _, tc := range cases {
		_, err := repo.TrailerValues(dir, tc.from, "HEAD", tc.path, tc.keys)
		if err == nil {
			t.Errorf("%s: want error", tc.name)
		}
	}
}

func TestListTags(t *testing.T) {
	dir := newRepo(t)
	tags, err := repo.ListTags(dir)
	if err != nil || len(tags) != 0 {
		t.Fatalf("fresh repo: tags = %v, err = %v", tags, err)
	}
	runGit(t, dir, "tag", "spec-b-v0.1.0")
	runGit(t, dir, "tag", "spec-a-v0.1.0")
	tags, err = repo.ListTags(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 || tags[0] != "spec-a-v0.1.0" || tags[1] != "spec-b-v0.1.0" {
		t.Fatalf("tags = %v", tags)
	}
}

func TestListTagsOutsideRepositoryIsAnError(t *testing.T) {
	_, err := repo.ListTags(t.TempDir())
	if err == nil {
		t.Fatal("want an error")
	}
}

func TestListBlobsMapsPathsToObjectIds(t *testing.T) {
	dir := newRepo(t)
	blobs, err := repo.ListBlobs(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "main:flags.yaml").Output()
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(string(out))
	if blobs["flags.yaml"] != want {
		t.Fatalf("flags.yaml blob = %q, want %q", blobs["flags.yaml"], want)
	}
	_, err = repo.ListBlobs(dir, "-bad")
	if err == nil || !strings.Contains(err.Error(), "invalid revision") {
		t.Fatalf("option-like revision: got %v, want an invalid revision error", err)
	}
}
