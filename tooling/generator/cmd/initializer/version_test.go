package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/source"
)

func TestVersionFlagPrintsOneLine(t *testing.T) {
	build := generator.Build{Version: generator.Version, Commit: "22e304a0dde9db4d9aa809008e6a8372241c2116", Origin: generator.OriginDevel, Modified: true}
	cases := map[string]struct {
		build generator.Build
		want  string
	}{
		"commit and changes": {build, "initializer 0.1.0 (22e304a0dde9, devel, modified)\n"},
		"clean release":      {generator.Build{Version: generator.Version, Commit: "22e304a0dde9db4d9aa809008e6a8372241c2116", Origin: generator.OriginRelease}, "initializer 0.1.0 (22e304a0dde9, release)\n"},
		"no commit":          {generator.Build{Version: generator.Version, Origin: generator.OriginDevel}, "initializer 0.1.0 (devel)\n"},
		"zero build":         {generator.Build{}, "initializer 0.1.0 (devel)\n"},
	}
	away := t.TempDir()
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			code, stdout, stderr := executeAs(t, hostAt(away, "", nil), tc.build, away, "--version")
			if code != exitOK || stdout != tc.want || stderr != "" {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
			}
		})
	}
}

func TestVersionPrintsTheBuildAndTheCheckout(t *testing.T) {
	c := newClone(t)
	build := generator.Build{Version: generator.Version, Commit: c.commit, Origin: generator.OriginDevel, Modified: true}
	code, stdout, stderr := executeAs(t, hostAt(c.dir, filepath.Join(t.TempDir(), "initializer"), nil), build, t.TempDir(), "version")
	want := "initializer 0.1.0\n" +
		"  build     " + c.commit[:12] + " (devel, modified)\n" +
		"  sources   " + filepath.ToSlash(c.dir) + " at " + c.commit[:7] + " (found from the working directory)\n"
	if code != exitOK || stdout != want || stderr != "" {
		t.Fatalf("exit %d\nstdout %q\nwant   %q\nstderr %q", code, stdout, want, stderr)
	}
}

func TestVersionNamesWhichRuleFoundTheCheckout(t *testing.T) {
	c := newClone(t)
	away := t.TempDir()
	cases := map[string]struct {
		host source.Host
		want string
	}{
		"env":        {hostAt(away, filepath.Join(away, "x"), map[string]string{"INITIALIZER_ROOT": c.dir}), "(found from INITIALIZER_ROOT)"},
		"executable": {hostAt(away, filepath.Join(c.dir, "tooling", "generator", "bin", "initializer"), nil), "(found from the binary's folder)"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, stdout, _ := executeAs(t, tc.host, generator.Build{}, away, "version")
			if !strings.Contains(stdout, tc.want) {
				t.Fatalf("stdout = %q", stdout)
			}
		})
	}
}

func TestVersionWithoutACheckoutStillExitsZero(t *testing.T) {
	away := t.TempDir()
	host := hostAt(away, filepath.Join(away, "initializer"), nil)
	code, stdout, stderr := executeAs(t, host, generator.Build{}, away, "version")
	want := "initializer 0.1.0\n  build     none (devel)\n  sources   none found (SOURCE_ROOT_NOT_FOUND)\n"
	if code != exitOK || stdout != want || stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	bad := hostAt(away, "", map[string]string{"INITIALIZER_ROOT": away})
	code, stdout, _ = executeAs(t, bad, generator.Build{}, away, "version")
	if code != exitOK || !strings.Contains(stdout, "none found (SOURCE_ROOT_INVALID)") {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
}

func TestVersionJSON(t *testing.T) {
	c := newClone(t)
	build := generator.Build{Version: generator.Version, Commit: c.commit, Origin: generator.OriginRelease}
	code, stdout, _ := executeAs(t, hostAt(c.dir, "", nil), build, t.TempDir(), "version", "--format", "json")
	var doc struct {
		Schema    int
		OK        bool
		Generator string
		Build     struct {
			Commit   string
			Origin   string
			Modified bool
		}
		Source   *sourceRef
		Notes    []string
		Warnings []Warning
	}
	decodeDocument(t, stdout, &doc)
	if code != exitOK || doc.Schema != 1 || !doc.OK || doc.Generator != "0.1.0" ||
		doc.Build.Commit != c.commit || doc.Build.Origin != "release" || doc.Build.Modified {
		t.Fatalf("document = %+v", doc)
	}
	if doc.Source == nil || doc.Source.How != "working-dir" || doc.Source.Commit != c.commit || doc.Source.Modified != nil {
		t.Fatalf("source = %+v", doc.Source)
	}
	if doc.Notes == nil || doc.Warnings == nil {
		t.Fatal("notes and warnings must be lists, never null")
	}

	away := t.TempDir()
	_, stdout, _ = executeAs(t, hostAt(away, filepath.Join(away, "x"), nil), generator.Build{}, away, "version", "--format", "json")
	var none map[string]any
	decodeDocument(t, stdout, &none)
	if _, has := none["source"]; has {
		t.Fatalf("source must be omitted when none is found: %s", stdout)
	}
}

func TestVersionTakesNoFlagsButTheCommonOnes(t *testing.T) {
	code, _, stderr := execute(t, "version", "--root", ".")
	if code != exitUsage || !strings.Contains(stderr, "error: --root: unknown flag") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

// staleClone returns a clone and the commit a binary was built from, after
// which tooling/generator changed.
func staleClone(t *testing.T) (c clone, built string) {
	t.Helper()
	c = newClone(t)
	built = c.commit
	if err := os.WriteFile(filepath.Join(c.dir, "tooling", "generator", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, c.dir, "add", "-A")
	gitIn(t, c.dir, "commit", "-q", "-m", "change the generator")
	c.commit = gitIn(t, c.dir, "rev-parse", "HEAD")
	return c, built
}

func TestAStampedBinaryOlderThanTheGeneratorWarnsAndStillWorks(t *testing.T) {
	c, built := staleClone(t)
	build := generator.Build{Version: generator.Version, Commit: built, Origin: generator.OriginDevel}
	host := hostAt(c.dir, filepath.Join(t.TempDir(), "initializer"), nil)
	message := "this initializer was built from " + built[:7] + "; tooling/generator changed since (HEAD " + c.commit[:7] + ")"

	for _, args := range [][]string{{"version"}, {"stack", "ls"}, {"module", "ls"}} {
		code, _, stderr := executeAs(t, host, build, t.TempDir(), args...)
		want := "warning: " + message + " [GENERATOR_STALE]\n  hint: run 'mise run cli'\n"
		if code != exitOK || !strings.Contains(stderr, want) {
			t.Fatalf("%v: exit %d, stderr %q", args, code, stderr)
		}
	}

	out := filepath.Join(t.TempDir(), "acme")
	code, stdout, stderr := executeAs(t, host, build, t.TempDir(), "generate", out, "--stacks", "go-srv", "--go-module", "example.com/acme", "--format", "json")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var doc struct{ Warnings []Warning }
	decodeDocument(t, stdout, &doc)
	if len(doc.Warnings) != 1 || doc.Warnings[0].Code != "GENERATOR_STALE" || doc.Warnings[0].Message != message || doc.Warnings[0].Hint != "run 'mise run cli'" {
		t.Fatalf("warnings = %+v", doc.Warnings)
	}
	event := logEvent(t, runLogged(t, host, build, "stack", "ls"), "generator_stale")
	if event["built_from"] != built || event["head"] != c.commit {
		t.Fatalf("event = %v", event)
	}
}

func runLogged(t *testing.T, host source.Host, build generator.Build, args ...string) string {
	t.Helper()
	_, _, stderr := executeAs(t, host, build, t.TempDir(), append(args, "--log", "json")...)
	return stderr
}

func TestABinaryAtTheCurrentGeneratorOrWithoutACommitDoesNotWarn(t *testing.T) {
	c, _ := staleClone(t)
	away := t.TempDir()
	host := hostAt(c.dir, filepath.Join(away, "initializer"), nil)
	for name, commit := range map[string]string{"built from HEAD": c.commit, "no commit": ""} {
		t.Run(name, func(t *testing.T) {
			build := generator.Build{Version: generator.Version, Commit: commit, Origin: generator.OriginDevel}
			_, _, stderr := executeAs(t, host, build, away, "stack", "ls")
			if strings.Contains(stderr, "GENERATOR_STALE") {
				t.Fatalf("stderr = %q", stderr)
			}
		})
	}
	// A commit elsewhere in the repository does not make the binary stale.
	if err := os.WriteFile(filepath.Join(c.dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, c.dir, "add", "-A")
	gitIn(t, c.dir, "commit", "-q", "-m", "docs")
	build := generator.Build{Version: generator.Version, Commit: c.commit, Origin: generator.OriginDevel}
	if _, _, stderr := executeAs(t, host, build, away, "stack", "ls"); strings.Contains(stderr, "GENERATOR_STALE") {
		t.Fatalf("a commit outside tooling/generator made the binary stale: %q", stderr)
	}
}

func TestVersionNotesAFailedStaleCheckInsteadOfStayingSilent(t *testing.T) {
	c, built := staleClone(t)
	build := generator.Build{Version: generator.Version, Commit: built, Origin: generator.OriginDevel}
	host := hostAt(c.dir, filepath.Join(t.TempDir(), "initializer"), nil)
	realGit := source.DefaultHost().Git
	host.Git = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "cat-file" {
			return nil, errors.New("signal: killed")
		}
		return realGit(ctx, dir, args...)
	}

	code, _, stderr := executeAs(t, host, build, t.TempDir(), "version")
	if code != exitOK || !strings.Contains(stderr, "note: could not check whether this initializer is current") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}
