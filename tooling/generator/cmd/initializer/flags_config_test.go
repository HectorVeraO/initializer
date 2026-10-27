package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// realCheckoutRoot returns the checkout root that holds tooling/generator.
func realCheckoutRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the test file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
}

// realCatalog builds catalog.lock.json from the real checkout.
func realCatalog(t *testing.T) (root, catalogPath string) {
	t.Helper()
	root = realCheckoutRoot(t)
	listed, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	dir := t.TempDir()
	filesPath := filepath.Join(dir, "files.bin")
	if err := os.WriteFile(filesPath, listed, 0o600); err != nil {
		t.Fatal(err)
	}
	catalogPath = filepath.Join(dir, "catalog.lock.json")
	code, _, stderr := execute(t, "catalog", "build", "--root", root, "--files", filesPath, "--out", catalogPath)
	if code != exitOK {
		t.Fatalf("catalog build exit %d: %s", code, stderr)
	}
	return root, catalogPath
}

// In a project generated with every module, the README sets
// GATBASE_FLAGS_FILE for go-srv started from go-srv/, so the path must
// resolve from there.
func TestGenerateReadmeFlagsFileResolvesFromGoSrv(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, "--out", out,
		"--name", "acme-backend", "--go-module", "example.com/acme/backend", "--stacks", "go-srv", "--all-modules")
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	readme, err := os.ReadFile(filepath.Join(out, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	const key = "GATBASE_FLAGS_FILE="
	at := strings.Index(string(readme), key)
	if at < 0 {
		t.Fatalf("README names no %s", key)
	}
	value := string(readme)[at+len(key):]
	value = value[:strings.IndexAny(value, "`\n ")]
	fromGoSrv := filepath.Join(out, "go-srv", filepath.FromSlash(value))
	if _, err := os.Stat(fromGoSrv); err != nil {
		t.Fatalf("README sets %s%s, which does not exist from the documented go-srv/ working directory: %v", key, value, err)
	}
}

// A project generated without feature-flags ships none of its configuration.
func TestGenerateWithoutFeatureFlagsShipsNoFlagsConfig(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, "--out", out,
		"--name", "acme-backend", "--go-module", "example.com/acme/backend", "--stacks", "go-srv", "--modules", "parameters")
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(out, "flags.yaml")); err == nil {
		t.Fatal("flags.yaml delivered without feature-flags")
	}
	survivors := map[string]string{
		".deploy/local/.example.env": "INITIALIZER_PG_PASSWORD=",
		".deploy/local/README.md":    "## Start and stop",
	}
	for p, survivor := range survivors {
		data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), survivor) {
			t.Errorf("%s lost unrelated core content %q", p, survivor)
		}
		for _, banned := range []string{"GATBASE_FLAGS_FILE", "feature-flags", "Feature flags"} {
			if strings.Contains(string(data), banned) {
				t.Errorf("%s mentions %q in a project without the feature-flags module", p, banned)
			}
		}
	}
}

// A project with feature-flags keeps the configuration, byte for byte.
func TestGenerateWithFeatureFlagsKeepsFlagsConfig(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, "--out", out,
		"--name", "acme-backend", "--go-module", "example.com/acme/backend", "--stacks", "go-srv", "--modules", "feature-flags")
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	for _, p := range []string{".deploy/local/.example.env", ".deploy/local/README.md"} {
		want, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s differs from the repository copy", p)
		}
	}
}
