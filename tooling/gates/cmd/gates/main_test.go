package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const emptyFlags = "# flags\nflags: []\n"

func releaseFlag(expires string) string {
	return "flags:\n  - key: example_beta_banner\n    type: release\n    default: false\n    expires_at: " + expires + "\n    description: d\n"
}

func gitRun(t *testing.T, dir string, args ...string) {
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

func writeFlags(t *testing.T, dir, content string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(dir, "flags.yaml"), []byte(content), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

// newRepo returns a repository whose main branch holds baseContent and whose
// working tree on branch topic holds content.
func newRepo(t *testing.T, baseContent, content string) string {
	t.Helper()
	_, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	writeFlags(t, dir, baseContent)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")
	gitRun(t, dir, "switch", "-c", "topic")
	writeFlags(t, dir, content)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "change")
	return dir
}

type result struct {
	code           int
	stdout, stderr string
}

func invoke(args []string, now time.Time, env map[string]string) result {
	var out, errOut bytes.Buffer
	clock := func() time.Time { return now }
	getenv := func(name string) string { return env[name] }
	code := runWith(args, &out, &errOut, clock, getenv)
	return result{code: code, stdout: out.String(), stderr: errOut.String()}
}

var fixedNow = time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC)

func TestCleanFileExitsZeroWithNoOutput(t *testing.T) {
	dir := newRepo(t, emptyFlags, emptyFlags)
	r := invoke([]string{"flags", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 0 || r.stdout != "" || r.stderr != "" {
		t.Fatalf("got %+v", r)
	}
}

func TestViolationsGoToStdoutAndTheCountToStderr(t *testing.T) {
	dir := newRepo(t, emptyFlags, releaseFlag("2026-10-20"))
	r := invoke([]string{"flags", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 1 {
		t.Fatalf("code = %d, want 1", r.code)
	}
	if !strings.HasPrefix(r.stdout, "flags.yaml:2: FLAG_EXPIRED [RA.2]: ") {
		t.Fatalf("stdout = %q", r.stdout)
	}
	if r.stderr != "1 violations\n" {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

func TestWarningsDoNotFailTheRun(t *testing.T) {
	dir := newRepo(t, emptyFlags, releaseFlag("2026-10-30"))
	r := invoke([]string{"flags", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 0 || !strings.Contains(r.stdout, "FLAG_EXPIRES_SOON") || r.stderr != "" {
		t.Fatalf("got %+v", r)
	}
}

func TestWarnDaysFlagSetsTheWarningWindow(t *testing.T) {
	// The flag expires in 5 days from fixedNow (2026-10-26).
	dir := newRepo(t, emptyFlags, releaseFlag("2026-10-31"))
	narrow := invoke([]string{"flags", "--root", dir, "--base-rev", "main", "--warn-days", "3"}, fixedNow, nil)
	if narrow.code != 0 || narrow.stdout != "" {
		t.Fatalf("warn-days 3: got %+v", narrow)
	}
	wide := invoke([]string{"flags", "--root", dir, "--base-rev", "main", "--warn-days", "5"}, fixedNow, nil)
	if wide.code != 0 || !strings.Contains(wide.stdout, "FLAG_EXPIRES_SOON") {
		t.Fatalf("warn-days 5: got %+v", wide)
	}
}

func TestTodayFlagOverridesTheClock(t *testing.T) {
	dir := newRepo(t, emptyFlags, releaseFlag("2026-12-01"))
	args := []string{"flags", "--root", dir, "--base-rev", "main", "--today", "2026-12-01"}
	r := invoke(args, fixedNow, nil)
	if r.code != 1 || !strings.Contains(r.stdout, "FLAG_EXPIRED") {
		t.Fatalf("got %+v", r)
	}
}

func TestTodayDefaultsToTheSantiagoDate(t *testing.T) {
	// 01:30 UTC on the 27th is still the 26th in Santiago, so a flag expiring
	// on the 27th has not expired yet.
	dir := newRepo(t, emptyFlags, releaseFlag("2026-10-27"))
	now := time.Date(2026, 10, 27, 1, 30, 0, 0, time.UTC)
	r := invoke([]string{"flags", "--root", dir, "--base-rev", "main"}, now, nil)
	if r.code != 0 || !strings.Contains(r.stdout, "FLAG_EXPIRES_SOON") {
		t.Fatalf("got %+v", r)
	}
}

func TestExtendedExpiryIsComparedAgainstTheMergeBase(t *testing.T) {
	dir := newRepo(t, releaseFlag("2026-11-05"), releaseFlag("2026-11-20"))
	r := invoke([]string{"flags", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 1 || !strings.Contains(r.stdout, "FLAG_EXPIRY_EXTENDED") {
		t.Fatalf("got %+v", r)
	}
}

func TestExtendedExpiryIgnoresBaseCommitsAddedAfterBranching(t *testing.T) {
	// main later gains the same date as the topic; only the merge base
	// (2026-11-05) shows the extension.
	dir := newRepo(t, releaseFlag("2026-11-05"), releaseFlag("2026-11-20"))
	gitRun(t, dir, "switch", "main")
	writeFlags(t, dir, releaseFlag("2026-11-20"))
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "main moves on")
	gitRun(t, dir, "switch", "topic")
	r := invoke([]string{"flags", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 1 || !strings.Contains(r.stdout, "FLAG_EXPIRY_EXTENDED") {
		t.Fatalf("got %+v", r)
	}
}

func TestMissingFlagsFileIsAViolation(t *testing.T) {
	dir := newRepo(t, emptyFlags, emptyFlags)
	err := os.Remove(filepath.Join(dir, "flags.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	r := invoke([]string{"flags", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 1 || !strings.Contains(r.stdout, "FLAGS_FILE_MISSING") {
		t.Fatalf("got %+v", r)
	}
}

func TestUnresolvableBaseRevisionExitsTwo(t *testing.T) {
	dir := newRepo(t, emptyFlags, emptyFlags)
	r := invoke([]string{"flags", "--root", dir, "--base-rev", "origin/main"}, fixedNow, nil)
	want := "base revision origin/main not found; fetch with fetch-depth: 0\n"
	if r.code != 2 || r.stderr != want || r.stdout != "" {
		t.Fatalf("got %+v", r)
	}
}

func TestBaseRevisionDefaultsToOriginMain(t *testing.T) {
	dir := newRepo(t, emptyFlags, emptyFlags)
	r := invoke([]string{"flags", "--root", dir}, fixedNow, nil)
	if r.code != 2 || !strings.Contains(r.stderr, "base revision origin/main not found") {
		t.Fatalf("got %+v", r)
	}
}

func TestGitHubActionsEnablesWorkflowCommands(t *testing.T) {
	dir := newRepo(t, emptyFlags, releaseFlag("2026-10-20"))
	env := map[string]string{"GITHUB_ACTIONS": "true"}
	r := invoke([]string{"flags", "--root", dir, "--base-rev", "main"}, fixedNow, env)
	if !strings.Contains(r.stdout, "::error file=flags.yaml,line=2::FLAG_EXPIRED: ") {
		t.Fatalf("stdout = %q", r.stdout)
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	cases := map[string][]string{
		"no subcommand":      {},
		"unknown subcommand": {"migrations"},
		"unknown flag":       {"flags", "--nope"},
		"bad today":          {"flags", "--today", "26-10-2026"},
		"negative warn days": {"flags", "--warn-days", "-1"},
		"extra argument":     {"flags", "stray"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			r := invoke(args, fixedNow, nil)
			if r.code != 2 || r.stderr == "" || r.stdout != "" {
				t.Fatalf("got %+v", r)
			}
		})
	}
}

func TestRunUsesTheRealEntryPoint(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"bogus"}, &out, &errOut)
	if code != 2 {
		t.Fatalf("code = %d", code)
	}
}
