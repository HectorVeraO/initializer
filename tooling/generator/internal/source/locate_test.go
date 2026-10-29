package source

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// markTree writes the marker of an initializer checkout into dir.
func markTree(t *testing.T, dir, modulePath string) {
	t.Helper()
	path := filepath.Join(dir, "tooling", "generator")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "module " + modulePath + "\n\ngo 1.26\n"
	if err := os.WriteFile(filepath.Join(path, "go.mod"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeHost returns a Host with no git over the given working directory,
// binary and environment.
func fakeHost(cwd, exe string, env map[string]string) Host {
	return Host{
		Getwd:      func() (string, error) { return cwd, nil },
		Executable: func() (string, error) { return exe, nil },
		Getenv:     func(name string) string { return env[name] },
		MainModule: MainModule,
	}
}

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	dir := filepath.Join(parts...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func sameDir(t *testing.T, got, want string) {
	t.Helper()
	if !samePath(got, want) {
		t.Fatalf("dir = %s, want %s", got, want)
	}
}

func asSourceError(t *testing.T, err error) *Error {
	t.Helper()
	var found *Error
	if !errors.As(err, &found) {
		t.Fatalf("error %v is not a *source.Error", err)
	}
	return found
}

func TestLocateFlag(t *testing.T) {
	base := t.TempDir()
	cwd := mkdir(t, base, "cwd")
	plain := mkdir(t, base, "cwd", "plain")
	file := filepath.Join(base, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := fakeHost(cwd, "", map[string]string{EnvRoot: "ignored"})

	t.Run("an absolute directory needs no marker", func(t *testing.T) {
		root, err := Locate(plain, host)
		if err != nil {
			t.Fatal(err)
		}
		sameDir(t, root.Dir, plain)
		if root.How != FromFlag || root.Marked || root.FS == nil {
			t.Fatalf("root = %+v", root)
		}
	})
	t.Run("a relative directory resolves against the working directory", func(t *testing.T) {
		root, err := Locate("plain", host)
		if err != nil {
			t.Fatal(err)
		}
		sameDir(t, root.Dir, plain)
	})
	t.Run("a marked directory is marked", func(t *testing.T) {
		marked := mkdir(t, base, "marked")
		markTree(t, marked, MainModule)
		root, err := Locate(marked, host)
		if err != nil || !root.Marked {
			t.Fatalf("root = %+v err = %v", root, err)
		}
	})
	for name, value := range map[string]string{"a file": file, "a missing path": filepath.Join(base, "missing")} {
		t.Run(name+" is invalid", func(t *testing.T) {
			_, err := Locate(value, host)
			found := asSourceError(t, err)
			if found.Issue.Code != CodeRootInvalid || found.Exit != ExitUsage {
				t.Fatalf("error = %+v", found)
			}
			if want := "--root " + value + " is not a directory"; found.Issue.Message != want {
				t.Fatalf("message = %q, want %q", found.Issue.Message, want)
			}
			if found.Issue.Hint == "" {
				t.Fatal("no hint")
			}
		})
	}
}

func TestLocateEnv(t *testing.T) {
	base := t.TempDir()
	cwd := mkdir(t, base, "cwd")
	marked := mkdir(t, base, "cwd", "clone")
	markTree(t, marked, MainModule)
	unmarked := mkdir(t, base, "cwd", "bare")
	other := mkdir(t, base, "cwd", "other")
	markTree(t, other, "example.com/not/the/generator")
	walkable := mkdir(t, base, "walk")
	markTree(t, walkable, MainModule)

	t.Run("a marked absolute path wins over the working directory walk", func(t *testing.T) {
		host := fakeHost(walkable, "", map[string]string{EnvRoot: marked})
		root, err := Locate("", host)
		if err != nil {
			t.Fatal(err)
		}
		sameDir(t, root.Dir, marked)
		if root.How != FromEnv || !root.Marked {
			t.Fatalf("root = %+v", root)
		}
	})
	t.Run("a relative path resolves against the working directory", func(t *testing.T) {
		host := fakeHost(cwd, "", map[string]string{EnvRoot: "clone"})
		root, err := Locate("", host)
		if err != nil {
			t.Fatal(err)
		}
		sameDir(t, root.Dir, marked)
	})
	for name, value := range map[string]string{"an unmarked directory": unmarked, "another module path": other, "a missing path": filepath.Join(base, "missing")} {
		t.Run(name+" is invalid and does not fall through", func(t *testing.T) {
			host := fakeHost(walkable, "", map[string]string{EnvRoot: value})
			_, err := Locate("", host)
			found := asSourceError(t, err)
			if found.Issue.Code != CodeRootInvalid || found.Exit != ExitUsage {
				t.Fatalf("error = %+v", found)
			}
			want := "INITIALIZER_ROOT=" + value + " is not an initializer checkout (no tooling/generator/go.mod declaring " + MainModule + ")"
			if found.Issue.Message != want {
				t.Fatalf("message = %q, want %q", found.Issue.Message, want)
			}
		})
	}
	t.Run("the flag beats the environment", func(t *testing.T) {
		host := fakeHost(cwd, "", map[string]string{EnvRoot: marked})
		root, err := Locate(unmarked, host)
		if err != nil {
			t.Fatal(err)
		}
		if root.How != FromFlag {
			t.Fatalf("how = %s", root.How)
		}
	})
}

func TestLocateWorkingDirectoryWalk(t *testing.T) {
	base := t.TempDir()
	clone := mkdir(t, base, "clone")
	markTree(t, clone, MainModule)
	deep := mkdir(t, clone, "go-srv", "internal")
	exeElsewhere := filepath.Join(mkdir(t, base, "elsewhere"), "initializer")

	t.Run("the clone above a nested folder", func(t *testing.T) {
		root, err := Locate("", fakeHost(deep, exeElsewhere, nil))
		if err != nil {
			t.Fatal(err)
		}
		sameDir(t, root.Dir, clone)
		if root.How != FromWorkingDir || !root.Marked {
			t.Fatalf("root = %+v", root)
		}
	})
	t.Run("the nearest marker wins: a worktree inside a clone finds itself", func(t *testing.T) {
		worktree := mkdir(t, clone, ".worktrees", "feature")
		markTree(t, worktree, MainModule)
		root, err := Locate("", fakeHost(mkdir(t, worktree, "go-srv"), exeElsewhere, nil))
		if err != nil {
			t.Fatal(err)
		}
		sameDir(t, root.Dir, worktree)
	})
	t.Run("a project generated inside the clone is walked through", func(t *testing.T) {
		project := mkdir(t, clone, "demo", "demo-srv")
		root, err := Locate("", fakeHost(project, exeElsewhere, nil))
		if err != nil {
			t.Fatal(err)
		}
		sameDir(t, root.Dir, clone)
	})
	t.Run("a marker with another module path is not a checkout", func(t *testing.T) {
		foreign := mkdir(t, base, "foreign")
		markTree(t, foreign, "example.com/other/tool")
		_, err := Locate("", fakeHost(foreign, exeElsewhere, nil))
		if found := asSourceError(t, err); found.Issue.Code != CodeRootNotFound {
			t.Fatalf("error = %+v", found)
		}
	})
	t.Run("the host's main module names the marker", func(t *testing.T) {
		custom := mkdir(t, base, "custom")
		markTree(t, custom, "example.com/fork/tooling/generator")
		host := fakeHost(custom, exeElsewhere, nil)
		host.MainModule = "example.com/fork/tooling/generator"
		root, err := Locate("", host)
		if err != nil || !root.Marked {
			t.Fatalf("root = %+v err = %v", root, err)
		}
	})
	t.Run("a CRLF go.mod line still matches", func(t *testing.T) {
		crlf := mkdir(t, base, "crlf")
		path := mkdir(t, crlf, "tooling", "generator")
		if err := os.WriteFile(filepath.Join(path, "go.mod"), []byte("module "+MainModule+"\r\ngo 1.26\r\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Locate("", fakeHost(crlf, exeElsewhere, nil)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a marker whose first line is not the module line does not match", func(t *testing.T) {
		late := mkdir(t, base, "late")
		path := mkdir(t, late, "tooling", "generator")
		if err := os.WriteFile(filepath.Join(path, "go.mod"), []byte("// comment\nmodule "+MainModule+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Locate("", fakeHost(late, exeElsewhere, nil)); err == nil {
			t.Fatal("expected not found")
		}
	})
}

func TestLocateExecutableWalk(t *testing.T) {
	base := t.TempDir()
	clone := mkdir(t, base, "clone")
	markTree(t, clone, MainModule)
	binDir := mkdir(t, clone, "tooling", "generator", "bin")
	exe := filepath.Join(binDir, "initializer")
	if err := os.WriteFile(exe, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	away := mkdir(t, base, "away")

	t.Run("a binary inside the clone finds it from anywhere", func(t *testing.T) {
		root, err := Locate("", fakeHost(away, exe, nil))
		if err != nil {
			t.Fatal(err)
		}
		sameDir(t, root.Dir, clone)
		if root.How != FromExecutable {
			t.Fatalf("how = %s", root.How)
		}
	})
	t.Run("a symlink to the binary is resolved", func(t *testing.T) {
		link := filepath.Join(mkdir(t, base, "links"), "initializer")
		if err := os.Symlink(exe, link); err != nil {
			t.Skipf("symlinks are not available: %v", err)
		}
		root, err := Locate("", fakeHost(away, link, nil))
		if err != nil {
			t.Fatal(err)
		}
		sameDir(t, root.Dir, clone)
		if root.How != FromExecutable {
			t.Fatalf("how = %s", root.How)
		}
	})
	t.Run("the working directory walk beats the binary", func(t *testing.T) {
		nearer := mkdir(t, base, "nearer")
		markTree(t, nearer, MainModule)
		root, err := Locate("", fakeHost(nearer, exe, nil))
		if err != nil {
			t.Fatal(err)
		}
		sameDir(t, root.Dir, nearer)
		if root.How != FromWorkingDir {
			t.Fatalf("how = %s", root.How)
		}
	})
}

func TestLocateNothingFound(t *testing.T) {
	base := t.TempDir()
	cwd := mkdir(t, base, "cwd")
	exeDir := mkdir(t, base, "bin")
	_, err := Locate("", fakeHost(cwd, filepath.Join(exeDir, "initializer"), nil))
	found := asSourceError(t, err)
	if found.Issue.Code != CodeRootNotFound || found.Exit != ExitUsage {
		t.Fatalf("error = %+v", found)
	}
	for _, want := range []string{"no initializer checkout found above ", filepath.ToSlash(cwd), " or above ", filepath.ToSlash(exeDir)} {
		if !strings.Contains(found.Issue.Message, want) {
			t.Fatalf("message %q lacks %q", found.Issue.Message, want)
		}
	}
	if want := "run inside a clone, pass --root <clone>, or set INITIALIZER_ROOT"; found.Issue.Hint != want {
		t.Fatalf("hint = %q", found.Issue.Hint)
	}
}

func TestLocateWithoutAWorkingDirectoryIsAnInternalError(t *testing.T) {
	host := fakeHost("", "", nil)
	host.Getwd = func() (string, error) { return "", errors.New("gone") }
	_, err := Locate("", host)
	found := asSourceError(t, err)
	if found.Exit != ExitInternal || found.Issue.Code != CodeUnreadable {
		t.Fatalf("error = %+v", found)
	}
}
