package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	migrationsDir = "go-srv/internal/gatbase/parameters/"
	sysManifest   = `{"module":"parameters","tablePrefixes":["sys"]}`
	migrationFile = migrationsDir + "migrations/20260101120000_sys_create_parameters.sql"
)

func writeAt(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	err := os.MkdirAll(filepath.Dir(full), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(full, []byte(content), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

// migrationsRepo returns a repo whose main holds only a README and whose
// branch topic adds the sys module with the given migration ("" adds nothing).
func migrationsRepo(t *testing.T, migration string) string {
	t.Helper()
	_, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	writeAt(t, dir, "README.md", "x\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")
	gitRun(t, dir, "switch", "-c", "topic")
	if migration != "" {
		writeAt(t, dir, migrationsDir+"module.manifest.json", sysManifest)
		writeAt(t, dir, migrationFile, migration)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "change")
	return dir
}

func TestMigrationsWithoutFilesPrintsZero(t *testing.T) {
	dir := migrationsRepo(t, "")
	r := invoke([]string{"migrations", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 0 || r.stdout != "0 migration files checked\n" || r.stderr != "" {
		t.Fatalf("got %+v", r)
	}
}

func TestMigrationsCleanFileExitsZero(t *testing.T) {
	dir := migrationsRepo(t, "-- +goose Up\nCREATE TABLE sys_parameters (id int);\n")
	r := invoke([]string{"migrations", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 0 || r.stdout != "1 migration files checked\n" {
		t.Fatalf("got %+v", r)
	}
}

func TestMigrationsViolationsPrintBeforeTheCount(t *testing.T) {
	dir := migrationsRepo(t, "-- +goose Up\nCREATE TABLE parameters (id int);\n")
	r := invoke([]string{"migrations", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	want := migrationFile + ":2: TABLE_NAME_INVALID [RA.3]: "
	if r.code != 1 || !strings.HasPrefix(r.stdout, want) || !strings.HasSuffix(r.stdout, "\n1 migration files checked\n") ||
		r.stderr != "1 violations\n" {
		t.Fatalf("got %+v", r)
	}
}

func TestMigrationsModifiedFileIsCaughtAgainstTheBase(t *testing.T) {
	dir := migrationsRepo(t, "-- +goose Up\n")
	gitRun(t, dir, "switch", "main")
	gitRun(t, dir, "merge", "--ff-only", "topic")
	gitRun(t, dir, "switch", "-c", "next")
	writeAt(t, dir, migrationFile, "-- +goose Up\nSELECT 1;\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "edit")
	r := invoke([]string{"migrations", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 1 || !strings.Contains(r.stdout, "MIGRATION_MODIFIED [RA.3]") {
		t.Fatalf("got %+v", r)
	}
}

func TestMigrationsGitHubActionsEmitsWorkflowCommands(t *testing.T) {
	dir := migrationsRepo(t, "CREATE TABLE sys_x (id int);\n")
	r := invoke([]string{"migrations", "--root", dir, "--base-rev", "main"}, fixedNow, map[string]string{"GITHUB_ACTIONS": "true"})
	if r.code != 1 || !strings.Contains(r.stdout, "::error file="+migrationFile+"::MIGRATION_GOOSE_UP_MISSING: ") {
		t.Fatalf("got %+v", r)
	}
}

func TestMigrationsUnresolvableBaseExitsTwo(t *testing.T) {
	dir := migrationsRepo(t, "")
	r := invoke([]string{"migrations", "--root", dir, "--base-rev", "nope"}, fixedNow, nil)
	if r.code != 2 || r.stdout != "" || !strings.Contains(r.stderr, "base revision nope not found; fetch with fetch-depth: 0") {
		t.Fatalf("got %+v", r)
	}
}

func TestMigrationsUsageErrorsExitTwo(t *testing.T) {
	for name, args := range map[string][]string{
		"unknown flag": {"migrations", "--nope"},
		"extra arg":    {"migrations", "stray"},
	} {
		t.Run(name, func(t *testing.T) {
			r := invoke(args, fixedNow, nil)
			if r.code != 2 || r.stdout != "" || r.stderr == "" {
				t.Fatalf("got %+v", r)
			}
		})
	}
}
