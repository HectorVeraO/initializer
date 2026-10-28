package main

import (
	"os/exec"
	"strings"
	"testing"
)

const (
	alphaManifest = `{"schema":1,"spec":"alpha","version":"VERSION","tckTags":["@core"]}`
	betaManifest  = `{"schema":1,"spec":"beta","version":"0.1.0","tckTags":["@beta"]}`
)

// versionsRepo returns a repo whose main holds specs alpha and beta at 0.1.0
// and whose branch topic changes alpha's contract and sets alpha's version.
func versionsRepo(t *testing.T, alphaVersion string) string {
	t.Helper()
	_, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	writeAt(t, dir, "specs/alpha/spec.manifest.json", strings.Replace(alphaManifest, "VERSION", "0.1.0", 1))
	writeAt(t, dir, "specs/alpha/openapi.yaml", "one\n")
	writeAt(t, dir, "specs/beta/spec.manifest.json", betaManifest)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")
	gitRun(t, dir, "switch", "-c", "topic")
	writeAt(t, dir, "specs/alpha/openapi.yaml", "two\n")
	writeAt(t, dir, "specs/alpha/spec.manifest.json", strings.Replace(alphaManifest, "VERSION", alphaVersion, 1))
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "change")
	return dir
}

func TestVersionsBumpedExitsZero(t *testing.T) {
	dir := versionsRepo(t, "0.2.0")
	r := invoke([]string{"versions", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 0 || r.stdout != "" || r.stderr != "" {
		t.Fatalf("got %+v", r)
	}
}

func TestVersionsNotBumpedExitsOne(t *testing.T) {
	dir := versionsRepo(t, "0.1.0")
	r := invoke([]string{"versions", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 1 || !strings.HasPrefix(r.stdout, "specs/alpha/spec.manifest.json:0: VERSION_NOT_BUMPED [L23]: ") || !strings.Contains(r.stderr, "1 violations") {
		t.Fatalf("got %+v", r)
	}
	if strings.Contains(r.stdout, `\`) {
		t.Errorf("backslash in output %q", r.stdout)
	}
}

func TestVersionsUnknownBaseExitsTwo(t *testing.T) {
	dir := versionsRepo(t, "0.2.0")
	r := invoke([]string{"versions", "--root", dir, "--base-rev", "nope"}, fixedNow, nil)
	if r.code != 2 || !strings.Contains(r.stderr, "base revision nope not found") {
		t.Fatalf("got %+v", r)
	}
}

func TestVersionsRejectsExtraArguments(t *testing.T) {
	r := invoke([]string{"versions", "extra"}, fixedNow, nil)
	if r.code != 2 {
		t.Fatalf("got %+v", r)
	}
}

func TestTagsPrintsOnlyUntaggedSorted(t *testing.T) {
	dir := versionsRepo(t, "0.2.0")
	gitRun(t, dir, "tag", "spec-beta-v0.1.0")
	r := invoke([]string{"tags", "--root", dir}, fixedNow, nil)
	if r.code != 0 || r.stdout != "spec-alpha-v0.2.0\n" || r.stderr != "" {
		t.Fatalf("got %+v", r)
	}
}

func TestTagsPrintsNothingWhenAllTagged(t *testing.T) {
	dir := versionsRepo(t, "0.2.0")
	gitRun(t, dir, "tag", "spec-beta-v0.1.0")
	gitRun(t, dir, "tag", "spec-alpha-v0.2.0")
	r := invoke([]string{"tags", "--root", dir}, fixedNow, nil)
	if r.code != 0 || r.stdout != "" || r.stderr != "" {
		t.Fatalf("got %+v", r)
	}
}

func TestTagsOutsideRepositoryExitsTwo(t *testing.T) {
	r := invoke([]string{"tags", "--root", t.TempDir()}, fixedNow, nil)
	if r.code != 2 {
		t.Fatalf("got %+v", r)
	}
}

func TestTagsFailsOnTagThatNamesOtherContent(t *testing.T) {
	for _, preRelease := range []string{preReleaseOff, preReleaseOn} {
		dir := versionsRepo(t, "0.2.0")
		writeAt(t, dir, "versioning.json", preRelease)
		gitRun(t, dir, "tag", "spec-alpha-v0.2.0", "main")
		r := invoke([]string{"tags", "--root", dir}, fixedNow, nil)
		if r.code != 1 || r.stdout != "" || !strings.Contains(r.stderr, "TAG_CONTENT_MISMATCH") || !strings.Contains(r.stderr, "spec-alpha-v0.2.0") {
			t.Fatalf("got %+v", r)
		}
	}
}

func TestTagsPassesOnTagAtCurrentContent(t *testing.T) {
	dir := versionsRepo(t, "0.2.0")
	gitRun(t, dir, "tag", "spec-alpha-v0.2.0")
	gitRun(t, dir, "tag", "spec-beta-v0.1.0")
	r := invoke([]string{"tags", "--root", dir}, fixedNow, nil)
	if r.code != 0 || r.stdout != "" || r.stderr != "" {
		t.Fatalf("got %+v", r)
	}
}
