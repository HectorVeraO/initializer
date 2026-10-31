package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// git runs git in dir and fails the test on error; it returns trimmed stdout.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// initRepo creates a git repository with a fixed configuration.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "commit.gpgsign", "false")
	git(t, dir, "config", "core.autocrlf", "false")
}

// copyGenerator copies the generator module (no tests, testdata or bin) to dest.
func copyGenerator(t *testing.T, dest string) {
	t.Helper()
	source, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == "testdata" || entry.Name() == "bin") {
			return filepath.SkipDir
		}
		if entry.IsDir() || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The commit stamped into the CLI feeds `initializer version` and the
// GENERATOR_STALE check: a CLI built by build-cli in a git worktree that sits
// inside another repository reports the worktree's own HEAD, not the
// enclosing repository's.
func TestBuiltCLIInNestedWorktreeReportsTheWorktreeCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the CLI")
	}
	base := t.TempDir()
	gitConfig := filepath.Join(base, "gitconfig")
	if err := os.WriteFile(gitConfig, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Jane Doe")
	t.Setenv("GIT_AUTHOR_EMAIL", "jane@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Jane Doe")
	t.Setenv("GIT_COMMITTER_EMAIL", "jane@example.com")
	t.Setenv("INITIALIZER_ROOT", "")

	clone := filepath.Join(base, "clone")
	initRepo(t, clone)
	copyGenerator(t, filepath.Join(clone, "tooling", "generator"))
	git(t, clone, "add", "-A")
	git(t, clone, "commit", "-q", "-m", "generator")
	built := git(t, clone, "rev-parse", "HEAD")

	outer := filepath.Join(base, "outer")
	initRepo(t, outer)
	if err := os.WriteFile(filepath.Join(outer, "README.md"), []byte("outer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, outer, "add", "-A")
	git(t, outer, "commit", "-q", "-m", "outer")
	outerHead := git(t, outer, "rev-parse", "HEAD")

	worktree := filepath.Join(outer, "wt")
	git(t, clone, "worktree", "add", "-q", "--detach", worktree, built)

	generatorDir := filepath.Join(worktree, "tooling", "generator")
	buildAndReport := func() (commit string, modified bool, notes []string) {
		t.Helper()
		buildCmd := exec.Command("go", "run", "./cmd/build-cli")
		buildCmd.Dir = generatorDir
		if out, err := buildCmd.CombinedOutput(); err != nil {
			t.Fatalf("go run ./cmd/build-cli: %v\n%s", err, out)
		}
		binary := filepath.Join(generatorDir, "bin", "initializer")
		if runtime.GOOS == "windows" {
			binary += ".exe"
		}
		versionCmd := exec.Command(binary, "version", "--format", "json")
		versionCmd.Dir = worktree
		out, err := versionCmd.Output()
		if err != nil {
			t.Fatalf("initializer version: %v\n%s", err, out)
		}
		var doc struct {
			Build struct {
				Commit   string `json:"commit"`
				Modified bool   `json:"modified"`
			} `json:"build"`
			Notes []string `json:"notes"`
		}
		if err := json.Unmarshal(out, &doc); err != nil {
			t.Fatalf("decode %s: %v", out, err)
		}
		return doc.Build.Commit, doc.Build.Modified, doc.Notes
	}

	commit, modified, notes := buildAndReport()
	if commit != built || modified {
		t.Fatalf("clean worktree: build.commit = %q modified = %v, want %q and false (enclosing repo HEAD is %q)\nnotes: %q",
			commit, modified, built, outerHead, notes)
	}

	// The enclosing repository stays clean; only the worktree changes.
	changed := filepath.Join(generatorDir, "version.go")
	source, err := os.ReadFile(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(changed, append(source, []byte("\n// edited\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	commit, modified, notes = buildAndReport()
	if commit != built || !modified {
		t.Fatalf("edited worktree: build.commit = %q modified = %v, want %q and true\nnotes: %q", commit, modified, built, notes)
	}
}
