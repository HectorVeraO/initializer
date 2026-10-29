package source

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
)

// isolateGit keeps a developer's git configuration (signing, hooks, templates,
// autocrlf) out of the tests that run real git, and clears INITIALIZER_ROOT.
func isolateGit(t *testing.T) {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Jane Doe")
	t.Setenv("GIT_AUTHOR_EMAIL", "jane@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Jane Doe")
	t.Setenv("GIT_COMMITTER_EMAIL", "jane@example.com")
	t.Setenv(EnvRoot, "")
}

// runGitCmd runs git in dir and fails the test on error; it returns stdout.
func runGitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo creates an empty git repository with a fixed identity and returns its
// directory.
func newRepo(t *testing.T) string {
	t.Helper()
	isolateGit(t)
	dir := t.TempDir()
	runGitCmd(t, dir, "init", "-q", "-b", "main")
	runGitCmd(t, dir, "config", "commit.gpgsign", "false")
	runGitCmd(t, dir, "config", "core.autocrlf", "false")
	return dir
}

// writeRepoFile writes content to the repo-relative slash path.
func writeRepoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commitAll stages everything, commits with message and returns the commit id.
func commitAll(t *testing.T, dir, message string) string {
	t.Helper()
	runGitCmd(t, dir, "add", "-A", "-f")
	runGitCmd(t, dir, "commit", "-q", "--allow-empty", "-m", message)
	return runGitCmd(t, dir, "rev-parse", "HEAD")
}

// fixtureRepo copies the golden fixture checkout into a new git repository,
// commits it and returns the directory and the commit.
func fixtureRepo(t *testing.T) (dir, commit string) {
	t.Helper()
	dir = newRepo(t)
	source := filepath.Join(fixture.Dir(t), "repo")
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		writeRepoFile(t, dir, filepath.ToSlash(relative), string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir, commitAll(t, dir, "fixture")
}

// realHost is the Host of the running process with git on PATH.
func realHost(dir string) Host {
	host := DefaultHost()
	host.Getwd = func() (string, error) { return dir, nil }
	host.Executable = func() (string, error) { return filepath.Join(dir, "initializer"), nil }
	return host
}

func background() context.Context {
	return context.Background()
}
