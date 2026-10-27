package main

import (
	"os/exec"
	"strings"
	"testing"
)

const (
	contractV1 = `openapi: 3.0.3
info:
  title: Example
  version: 1.0.0
paths:
  /api/v1/examples:
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                required: [id, name]
                properties:
                  id:
                    type: string
                  name:
                    type: string
`
	manifestV1 = `{"schema":1,"spec":"alpha","version":"1.0.0","tckTags":["@core"]}`
)

// contractsRepo returns a repo whose main holds spec alpha 1.0.0 and whose
// branch topic holds the given contract and manifest.
func contractsRepo(t *testing.T, contract, manifest, message string) string {
	t.Helper()
	_, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	writeAt(t, dir, "specs/alpha/openapi.yaml", contractV1)
	writeAt(t, dir, "specs/alpha/spec.manifest.json", manifestV1)
	writeAt(t, dir, "specs/alpha/tck/example.spec.ts", "// synthetic\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")
	gitRun(t, dir, "switch", "-c", "topic")
	writeAt(t, dir, "specs/alpha/openapi.yaml", contract)
	writeAt(t, dir, "specs/alpha/spec.manifest.json", manifest)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "--allow-empty", "-m", message)
	return dir
}

func TestContractsCleanRepoExitsZero(t *testing.T) {
	dir := contractsRepo(t, contractV1, manifestV1, "change")
	r := invoke([]string{"contracts", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 0 || r.stdout != "" || r.stderr != "" {
		t.Fatalf("got %+v", r)
	}
}

func TestContractsStructuralViolationExitsOne(t *testing.T) {
	dir := contractsRepo(t, contractV1+"x-stability-level: alpha\n", manifestV1, "change")
	r := invoke([]string{"contracts", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 1 || !strings.Contains(r.stdout, "CONTRACT_STABILITY_EXCLUDED [L16]") || !strings.Contains(r.stderr, "1 violations") {
		t.Fatalf("got %+v", r)
	}
	if strings.Contains(r.stdout, `\`) {
		t.Errorf("backslash in output %q", r.stdout)
	}
}

func TestContractsBreakingChangeExitsOneWithBothRules(t *testing.T) {
	_, err := exec.LookPath("oasdiff")
	if err != nil {
		t.Skip("oasdiff is not on PATH; run under mise exec --")
	}
	broken := strings.Replace(contractV1, "                  name:\n                    type: string\n", "", 1)
	broken = strings.Replace(broken, "[id, name]", "[id]", 1)
	dir := contractsRepo(t, broken, manifestV1, "feature(alpha): Drop name")
	r := invoke([]string{"contracts", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 1 {
		t.Fatalf("got %+v", r)
	}
	for _, want := range []string{"CONTRACT_BREAKING_UNMARKED [RA.4]", "CONTRACT_BREAKING_NOT_MAJOR [L23]"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout lacks %q: %q", want, r.stdout)
		}
	}
}

func TestContractsOasdiffFailureExitsTwo(t *testing.T) {
	changed := strings.Replace(contractV1, "title: Example", "title: Example API", 1)
	dir := contractsRepo(t, changed, manifestV1, "fix(alpha): Text")
	r := invoke([]string{"contracts", "--root", dir, "--base-rev", "main", "--oasdiff", "no-such-oasdiff-binary"}, fixedNow, nil)
	if r.code != 2 || r.stderr == "" || r.stdout != "" {
		t.Fatalf("got %+v", r)
	}
}

func TestContractsBaseRevisionErrorsExitTwo(t *testing.T) {
	dir := contractsRepo(t, contractV1, manifestV1, "change")
	r := invoke([]string{"contracts", "--root", dir, "--base-rev", "no-such-branch"}, fixedNow, nil)
	if r.code != 2 || !strings.Contains(r.stderr, "no-such-branch") {
		t.Fatalf("got %+v", r)
	}
	r = invoke([]string{"contracts", "--root", dir}, fixedNow, nil)
	if r.code != 2 || !strings.Contains(r.stderr, "origin/main") {
		t.Fatalf("default base: got %+v", r)
	}
}

func TestContractsUsageErrorsAndHelp(t *testing.T) {
	r := invoke([]string{"contracts", "--nope"}, fixedNow, nil)
	if r.code != 2 {
		t.Fatalf("unknown flag: %+v", r)
	}
	r = invoke([]string{"contracts", "stray"}, fixedNow, nil)
	if r.code != 2 || !strings.Contains(r.stderr, "gates contracts") {
		t.Fatalf("stray argument: %+v", r)
	}
	r = invoke([]string{"contracts", "-h"}, fixedNow, nil)
	if r.code != 0 || r.stdout != "" {
		t.Fatalf("help: %+v", r)
	}
}

func TestContractsGitHubActionsEmitsWorkflowCommands(t *testing.T) {
	dir := contractsRepo(t, contractV1+"x-stability-level: draft\n", manifestV1, "change")
	r := invoke([]string{"contracts", "--root", dir, "--base-rev", "main"}, fixedNow, map[string]string{"GITHUB_ACTIONS": "true"})
	if r.code != 1 || !strings.Contains(r.stdout, "::error file=specs/alpha/openapi.yaml") {
		t.Fatalf("got %+v", r)
	}
}

// TestMigrationsLintSkipsTestdataUnderGoSrv keeps the lead fix for fixture
// trees: a migration under a testdata directory is never linted.
func TestMigrationsLintSkipsTestdataUnderGoSrv(t *testing.T) {
	dir := migrationsRepo(t, "")
	writeAt(t, dir, migrationsDir+"testdata/migrations/not_a_migration_name.sql", "select 1;\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "fixture")
	r := invoke([]string{"migrations", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 0 {
		t.Fatalf("got %+v", r)
	}
}
