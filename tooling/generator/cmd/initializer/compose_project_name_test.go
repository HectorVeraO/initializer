package main

import (
	"path/filepath"
	"regexp"
	"testing"
)

var composeNamePattern = regexp.MustCompile(`(?m)^name: (\S+)$`)

// composeProjectOf generates the selection into a fresh folder and returns the
// top-level name of the generated local compose file.
func composeProjectOf(t *testing.T, root, catalogPath, selection, folder string, extra ...string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), folder)
	args := append([]string{"generate", out, "--root", root, "--catalog", catalogPath, "-f", selection}, extra...)
	code, stdout, stderr := execute(t, args...)
	if code != exitOK {
		t.Fatalf("generate %s exit %d: %s%s", folder, code, stdout, stderr)
	}
	compose := readProjectFile(t, out, ".deploy/local/docker-compose.yml")
	match := composeNamePattern.FindStringSubmatch(compose)
	if match == nil {
		t.Fatalf("%s: compose file has no top-level name", folder)
	}
	return match[1]
}

func TestSameSelectionOnTwoBandsGetsTwoComposeProjects(t *testing.T) {
	root, catalogPath := realCatalog(t)
	selection := writeSelection(t, `{"schema": 2, "project": {"name": "acme", "goModule": "example.com/acme"},
"stacks": ["go-srv", "ng-web"], "modules": ["identity", "tenancy"]}`)
	onDefaultBand := composeProjectOf(t, root, catalogPath, selection, "acme")
	onBand103 := composeProjectOf(t, root, catalogPath, selection, "acme-103", "--port-band", "103")
	if onDefaultBand != "acme-101" {
		t.Errorf("default band compose project = %q, want acme-101", onDefaultBand)
	}
	if onBand103 != "acme-103" {
		t.Errorf("band 103 compose project = %q, want acme-103", onBand103)
	}
}

func TestTwoNamesOnOneBandGetTwoComposeProjects(t *testing.T) {
	root, catalogPath := realCatalog(t)
	first := writeSelection(t, `{"schema": 2, "project": {"name": "acme", "goModule": "example.com/acme"},
"stacks": ["go-srv"], "modules": ["identity"]}`)
	second := writeSelection(t, `{"schema": 2, "project": {"name": "globex", "goModule": "example.com/globex"},
"stacks": ["go-srv"], "modules": ["identity"]}`)
	acme := composeProjectOf(t, root, catalogPath, first, "acme")
	globex := composeProjectOf(t, root, catalogPath, second, "globex")
	if acme == globex {
		t.Errorf("projects acme and globex on band 101 share compose project %q", acme)
	}
}

func TestMultiComponentProjectComposeNameCarriesTheBand(t *testing.T) {
	root, catalogPath := realCatalog(t)
	selection := writeSelection(t, `{"schema": 2, "project": {"name": "acme", "goModule": "example.com/acme"},
"stacks": ["go-srv", "ng-web"], "modules": ["identity"],
"components": [{"stack": "go-srv", "name": "acme-first"}, {"stack": "go-srv", "name": "acme-second"}, {"stack": "ng-web", "name": "acme-web", "backend": "acme-second"}]}`)
	got := composeProjectOf(t, root, catalogPath, selection, "acme", "--port-band", "104")
	if got != "acme-104" {
		t.Errorf("multi-component compose project = %q, want acme-104", got)
	}
}
