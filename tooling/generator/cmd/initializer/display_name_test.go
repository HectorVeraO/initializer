package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDisplayNameDefaultsToTheProjectName(t *testing.T) {
	doc := infraDryRunJSON(t, "--name", "acme", "--stacks", "go-srv,ng-web")
	if doc.Names["displayName"] != "acme" {
		t.Fatalf("names = %v", doc.Names)
	}
}

func TestDisplayNameFlagOverridesTheDefault(t *testing.T) {
	doc := infraDryRunJSON(t, "--name", "acme", "--stacks", "go-srv,ng-web", "--display-name", "Acme Shop")
	if doc.Names["displayName"] != "Acme Shop" {
		t.Fatalf("names = %v", doc.Names)
	}
}

func TestDisplayNameFileValueIsReadAndAFlagWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "in.json")
	body := `{"schema":2,"project":{"name":"acme"},"displayName":"From File","stacks":["go-srv","ng-web"],"modules":[]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if doc := infraDryRunJSON(t, "-f", path); doc.Names["displayName"] != "From File" {
		t.Fatalf("names = %v", doc.Names)
	}
	if doc := infraDryRunJSON(t, "-f", path, "--display-name", "From Flag"); doc.Names["displayName"] != "From Flag" {
		t.Fatalf("names = %v", doc.Names)
	}
}

func TestDisplayNameNeedsSchema2InAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "in.json")
	body := `{"schema":1,"project":{"name":"acme"},"displayName":"Acme","stacks":["go-srv"],"modules":[]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := infraDryRun(t, "-f", path, "--format", "json")
	found := issueArray(t, stdout)
	if code != exitIssues || len(found) != 1 || found[0]["message"] != "displayName needs schema 2" {
		t.Fatalf("exit %d issues %v", code, found)
	}
}

func TestDisplayNameGrammarIsEnforced(t *testing.T) {
	for _, bad := range []string{"Acme's", "<b>", " Acme", "%{x}", ""} {
		code, stdout, _ := infraDryRun(t, "--name", "acme", "--stacks", "go-srv,ng-web", "--display-name", bad, "--format", "json")
		found := issueArray(t, stdout)
		if code != exitIssues || len(found) != 1 || found[0]["code"] != "NAME_INVALID" {
			t.Errorf("%q: exit %d issues %v", bad, code, found)
		}
	}
}

func TestDisplayNameGrammarIsEnforcedInAFile(t *testing.T) {
	for _, bad := range []string{"Acme's", "<b>", " Acme", "%{x}", ""} {
		path := filepath.Join(t.TempDir(), "in.json")
		body := `{"schema":2,"project":{"name":"acme"},"displayName":` + strconv.Quote(bad) + `,"stacks":["go-srv","ng-web"],"modules":[]}`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		code, stdout, _ := infraDryRun(t, "-f", path, "--format", "json")
		found := issueArray(t, stdout)
		if code != exitIssues || len(found) != 1 || found[0]["code"] != "NAME_INVALID" {
			t.Errorf("%q: exit %d issues %v", bad, code, found)
		}
	}
}

func TestDisplayNameWithoutTheWebStackIsAnUnusedOverride(t *testing.T) {
	code, stdout, _ := infraDryRun(t, "--name", "acme", "--stacks", "go-srv", "--display-name", "Acme Shop", "--format", "json")
	found := issueArray(t, stdout)
	if code != exitIssues || len(found) != 1 || found[0]["code"] != "NAME_OVERRIDE_UNUSED" {
		t.Fatalf("exit %d issues %v", code, found)
	}
}

func TestDisplayNameIsRecordedOnlyWhenGiven(t *testing.T) {
	root, catalogPath := realCatalog(t)
	generate := func(extra ...string) map[string]any {
		out := filepath.Join(t.TempDir(), "out")
		args := []string{"generate", out, "--root", root, "--catalog", catalogPath, "--name", "acme", "--go-module", "example.com/acme", "--stacks", "go-srv,ng-web"}
		code, _, stderr := execute(t, append(args, extra...)...)
		if code != exitOK {
			t.Fatalf("generate exit %d: %s", code, stderr)
		}
		return readJSONFile(t, filepath.Join(out, "initializer.selection.json"))
	}
	if _, has := generate()["displayName"]; has {
		t.Error("displayName recorded without being given")
	}
	given := generate("--display-name", "Acme Shop")
	if given["displayName"] != "Acme Shop" || given["schema"] != float64(2) {
		t.Errorf("recorded selection = %v", given)
	}
}

func TestGenerateDeliversTheDisplayNameIntoTheBrandFiles(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", out, "--root", root, "--catalog", catalogPath, "--name", "acme",
		"--go-module", "example.com/acme", "--stacks", "go-srv,ng-web", "--display-name", "Acme Shop")
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	if got := projectLock(t, out)["names"].(map[string]any)["displayName"]; got != "Acme Shop" {
		t.Errorf("lock names.displayName = %v", got)
	}
	web := folderOf(t, out, "ng-web")
	wants := map[string]string{
		"src/app/core/config/brand.ts": "BRAND = 'Acme Shop'",
		"src/index.html":               "<title>Acme Shop</title>",
		"src/assets/i18n/en.json":      "{{year}} Acme Shop",
		"src/assets/i18n/es.json":      "{{year}} Acme Shop",
	}
	for relative, want := range wants {
		data, err := os.ReadFile(filepath.Join(out, web, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), want) {
			t.Errorf("%s lacks %q", relative, want)
		}
	}
}
