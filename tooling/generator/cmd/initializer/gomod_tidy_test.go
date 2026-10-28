package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// yamlRequirement is the module only the feature-flags manifest parser imports.
const yamlRequirement = "go.yaml.in/yaml/v3"

// networkFailure matches the go command errors of an offline module fetch.
var networkFailure = regexp.MustCompile(`dial tcp|no such host|i/o timeout|connection refused|proxy\.golang\.org|TLS handshake|network is unreachable`)

// tidyDiff runs "go mod tidy -diff" in dir and returns its output and whether it exited 0.
// It skips the test only when the toolchain cannot fetch modules (no go binary, no module
// cache and no network); any other failure without a tidy diff fails the test.
func tidyDiff(t *testing.T, dir string) (string, bool) {
	t.Helper()
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBinary, "mod", "tidy", "-diff")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	out, runErr := cmd.CombinedOutput()
	if runErr == nil {
		return string(out), true
	}
	isTidyDiff := strings.Contains(string(out), "diff current/")
	isNetworkFailure := networkFailure.MatchString(string(out))
	if !isTidyDiff && !isNetworkFailure {
		t.Fatalf("go mod tidy -diff failed: %v: %s", runErr, out)
	}
	if !isTidyDiff {
		t.Skipf("go mod tidy cannot run here: %v: %s", runErr, out)
	}
	return string(out), false
}

// A generated go-srv go.mod lists only the requirements its modules import.
func TestGeneratedGoModIsTidyPerSelection(t *testing.T) {
	cases := []struct {
		name            string
		modules         string
		wantYamlRequire bool
	}{
		{"core only", "", false},
		{"identity without feature-flags", "identity", false},
		{"feature-flags with identity", "feature-flags,identity", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := generateSelection(t, tc.modules)
			goSrv := filepath.Join(out, "go-srv")
			gomod, err := os.ReadFile(filepath.Join(goSrv, "go.mod"))
			if err != nil {
				t.Fatal(err)
			}
			if has := strings.Contains(string(gomod), yamlRequirement); has != tc.wantYamlRequire {
				t.Errorf("go.mod requires %s = %v, want %v", yamlRequirement, has, tc.wantYamlRequire)
			}
			if diff, tidy := tidyDiff(t, goSrv); !tidy {
				t.Errorf("go mod tidy -diff reports changes:\n%s", diff)
			}
		})
	}
}

// go.sum cannot carry markers, so a selection without feature-flags keeps the lines
// tidy keeps and the whole file stays byte-identical to the repo copy.
func TestGeneratedGoSumIsTheRepoCopy(t *testing.T) {
	root, _ := realCatalog(t)
	want, err := os.ReadFile(filepath.Join(root, "go-srv", "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	out := generateSelection(t, "")
	got, err := os.ReadFile(filepath.Join(out, "go-srv", "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Error("core-only go.sum differs from the repo go.sum")
	}
}

// generateSelection generates a go-srv project from the real checkout; "" selects core only.
func generateSelection(t *testing.T, modules string) string {
	t.Helper()
	if modules != "" {
		return generateFromCheckout(t, modules)
	}
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	selection := filepath.Join(root, "tooling", "generator", "testdata", "ci", "core.json")
	code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, "--out", out, "--selection", selection)
	if code != exitOK {
		t.Fatalf("generate core only exit %d: %s", code, stderr)
	}
	return out
}
