package main

import (
	"os/exec"
	"strings"
	"testing"
)

const (
	preReleaseOn  = `{ "schema": 1, "preRelease": true }` + "\n"
	preReleaseOff = `{ "schema": 1, "preRelease": false }` + "\n"

	versionsNotice   = "pre-release (versioning.json): VERSION_NOT_BUMPED and VERSION_DECREASED are off; every version must be 0.1.0\n"
	contractsNotice  = "pre-release (versioning.json): CONTRACT_BREAKING_NOT_MAJOR is off\n"
	migrationsNotice = "pre-release (versioning.json): MIGRATION_MODIFIED and MIGRATION_OUT_OF_ORDER are off\n"
	tagsNotice       = "pre-release (versioning.json): no tags\n"
)

func TestPreReleaseVersionsChangeWithoutBumpExitsZeroWithNotice(t *testing.T) {
	dir := versionsRepo(t, "0.1.0")
	writeAt(t, dir, "versioning.json", preReleaseOn)
	r := invoke([]string{"versions", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 0 || r.stdout != "" || r.stderr != versionsNotice {
		t.Fatalf("got %+v", r)
	}
}

func TestPreReleaseVersionsAboveInitialVersionExitsOne(t *testing.T) {
	dir := versionsRepo(t, "0.2.0")
	writeAt(t, dir, "versioning.json", preReleaseOn)
	r := invoke([]string{"versions", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 1 || !strings.HasPrefix(r.stdout, "specs/alpha/spec.manifest.json:0: VERSION_NOT_PRERELEASE [L76]: ") {
		t.Fatalf("got %+v", r)
	}
}

func TestPreReleaseVersionsOffAndAbsentKeepTheBumpRule(t *testing.T) {
	for name, setting := range map[string]string{"false": preReleaseOff, "absent": ""} {
		t.Run(name, func(t *testing.T) {
			dir := versionsRepo(t, "0.1.0")
			if setting != "" {
				writeAt(t, dir, "versioning.json", setting)
			}
			r := invoke([]string{"versions", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
			if r.code != 1 || !strings.HasPrefix(r.stdout, "specs/alpha/spec.manifest.json:0: VERSION_NOT_BUMPED [L23]: ") ||
				strings.Contains(r.stderr, "pre-release") {
				t.Fatalf("got %+v", r)
			}
		})
	}
}

func TestPreReleaseContractsBreakingWithoutMajorExitsZeroWithNotice(t *testing.T) {
	_, err := exec.LookPath("oasdiff")
	if err != nil {
		t.Skip("oasdiff is not on PATH; run under mise exec --")
	}
	broken := strings.Replace(contractV1, "                  name:\n                    type: string\n", "", 1)
	broken = strings.Replace(broken, "[id, name]", "[id]", 1)
	dir := contractsRepo(t, broken, manifestV1, "feature(alpha): Drop name\n\nWhy.\n\nBREAKING CHANGE: name is gone")
	writeAt(t, dir, "versioning.json", preReleaseOn)
	r := invoke([]string{"contracts", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 0 || r.stdout != "" || r.stderr != contractsNotice {
		t.Fatalf("got %+v", r)
	}
}

func TestPreReleaseMigrationsEditedBaseFileExitsZeroWithNotice(t *testing.T) {
	dir := migrationsRepo(t, "-- +goose Up\n")
	gitRun(t, dir, "switch", "main")
	gitRun(t, dir, "merge", "--ff-only", "topic")
	gitRun(t, dir, "switch", "-c", "next")
	writeAt(t, dir, migrationFile, "-- +goose Up\nSELECT 1;\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "edit")
	writeAt(t, dir, "versioning.json", preReleaseOn)
	r := invoke([]string{"migrations", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 0 || r.stdout != "1 migration files checked\n" || r.stderr != migrationsNotice {
		t.Fatalf("got %+v", r)
	}
	writeAt(t, dir, "versioning.json", preReleaseOff)
	r = invoke([]string{"migrations", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if r.code != 1 || !strings.Contains(r.stdout, "MIGRATION_MODIFIED") {
		t.Fatalf("setting off: got %+v", r)
	}
}

func TestPreReleaseMigrationsStillResolvesTheBase(t *testing.T) {
	dir := migrationsRepo(t, "-- +goose Up\n")
	writeAt(t, dir, "versioning.json", preReleaseOn)
	r := invoke([]string{"migrations", "--root", dir, "--base-rev", "nope"}, fixedNow, nil)
	if r.code != 2 {
		t.Fatalf("got %+v", r)
	}
}

func TestPreReleaseTagsPrintsNothingWithNotice(t *testing.T) {
	dir := versionsRepo(t, "0.2.0")
	writeAt(t, dir, "versioning.json", preReleaseOn)
	r := invoke([]string{"tags", "--root", dir}, fixedNow, nil)
	if r.code != 0 || r.stdout != "" || r.stderr != tagsNotice {
		t.Fatalf("got %+v", r)
	}
	writeAt(t, dir, "versioning.json", preReleaseOff)
	r = invoke([]string{"tags", "--root", dir}, fixedNow, nil)
	if r.code != 0 || r.stdout != "spec-alpha-v0.2.0\nspec-beta-v0.1.0\n" || r.stderr != "" {
		t.Fatalf("setting off: got %+v", r)
	}
}

func TestPreReleaseMalformedFileExitsTwoNamingTheFile(t *testing.T) {
	for _, command := range []string{"versions", "contracts", "migrations", "tags"} {
		t.Run(command, func(t *testing.T) {
			dir := versionsRepo(t, "0.2.0")
			writeAt(t, dir, "versioning.json", "not json\n")
			r := invoke([]string{command, "--root", dir, "--base-rev", "main"}[:argCount(command)], fixedNow, nil)
			if r.code != 2 || !strings.Contains(r.stderr, "versioning.json") {
				t.Fatalf("got %+v", r)
			}
		})
	}
}

// argCount is how many of the shared arguments the command accepts: tags takes no --base-rev.
func argCount(command string) int {
	if command == "tags" {
		return 3
	}
	return 5
}

func TestPreReleaseMalformedFileDoesNotAffectFlags(t *testing.T) {
	dir := newRepo(t, "", "")
	without := invoke([]string{"flags", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	writeAt(t, dir, "versioning.json", "not json\n")
	with := invoke([]string{"flags", "--root", dir, "--base-rev", "main"}, fixedNow, nil)
	if with != without {
		t.Fatalf("with %+v, without %+v", with, without)
	}
}
