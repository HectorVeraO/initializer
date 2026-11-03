//go:build windows

package upgrade

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/emit"
)

// A junction is the symlinked folder a Windows user can create without a
// privilege, so it covers the escaping folder on this platform.
func TestApplyRefusesAJunctionThatLeavesTheProject(t *testing.T) {
	dir := projectDir(t, map[string]string{"real/keep.txt": "keep"})
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "x.go")
	if err := os.WriteFile(outsideFile, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Skipf("this machine cannot create a junction: %v %s", err, out)
	}
	project := emit.Project{Files: planned("linked/x.go", "planned")}
	result, err := Apply(Options{Dir: dir, Project: project, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses(result)["linked/x.go"]; got != SkippedNotFile {
		t.Fatalf("linked/x.go: status %q, want %q", got, SkippedNotFile)
	}
	if len(result.Written) != 0 {
		t.Fatalf("written %v, want nothing", result.Written)
	}
	if data, err := os.ReadFile(outsideFile); err != nil || string(data) != "mine" {
		t.Fatalf("the junction target is %q (%v), want it untouched", data, err)
	}
}
