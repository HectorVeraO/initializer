package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The guide promises that -f <project>/initializer.selection.json regenerates
// the project. A project named after its folder must come back with that name.
func TestReplayWithoutAFolderRegeneratesAFolderNamedProject(t *testing.T) {
	e := newEnv(t)
	first := t.TempDir()
	if code, _, stderr := executeIn(t, first, "", e.inputArgs("acme", "--stacks", "go-srv,ng-web", "--modules", "identity")...); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	recorded := filepath.Join(first, "acme", "initializer.selection.json")

	second := t.TempDir()
	code, stdout, stderr := executeIn(t, second, "", e.inputArgs("-f", recorded)...)
	if code != exitOK {
		t.Fatalf("replay exit %d: %s %s", code, stdout, stderr)
	}
	replayed := filepath.Join(second, "acme")
	if _, err := os.Stat(filepath.Join(replayed, "initializer.lock.json")); err != nil {
		t.Fatalf("replay did not regenerate ./acme: %v", err)
	}
	if got := lockedName(t, replayed); got != "acme" {
		t.Errorf("project name = %q, want acme", got)
	}
	if got := goModuleOf(t, replayed); got != "example.com/acme" {
		t.Errorf("go module = %q, want example.com/acme", got)
	}
}

func TestReplayIntoAnotherFolderKeepsTheFolderDerivedName(t *testing.T) {
	root, catalogPath := realCatalog(t)
	e := env{root: root, catalog: catalogPath}
	wd := t.TempDir()
	if code, _, stderr := executeIn(t, wd, "", e.inputArgs("acme", "--stacks", "go-srv,ng-web", "--modules", "identity,parameters")...); code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	recorded := filepath.Join(wd, "acme", "initializer.selection.json")
	if code, _, stderr := executeIn(t, wd, "", e.inputArgs("-f", recorded, "regenerated")...); code != exitOK {
		t.Fatalf("replay exit %d: %s", code, stderr)
	}
	regenerated := filepath.Join(wd, "regenerated")
	if got := lockedName(t, regenerated); got != "acme" {
		t.Errorf("replayed project name = %q, want acme", got)
	}
	if got := goModuleOf(t, regenerated); got != "example.com/acme" {
		t.Errorf("replayed go module = %q, want example.com/acme", got)
	}
	if got := folderOf(t, regenerated, "go-srv"); got != "acme-srv" {
		t.Errorf("replayed backend folder = %q, want acme-srv", got)
	}
}

func TestReplayWithNameOverridesTheFolderDerivedName(t *testing.T) {
	e := newEnv(t)
	wd := t.TempDir()
	if code, _, stderr := executeIn(t, wd, "", e.inputArgs("acme", "--stacks", "go-srv,ng-web", "--modules", "parameters")...); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	recorded := filepath.Join(wd, "acme", "initializer.selection.json")
	if code, _, stderr := executeIn(t, wd, "", e.inputArgs("-f", recorded, "--name", "shop", "shop")...); code != exitOK {
		t.Fatalf("replay exit %d: %s", code, stderr)
	}
	shop := filepath.Join(wd, "shop")
	if got := lockedName(t, shop); got != "shop" {
		t.Errorf("project name = %q, want shop", got)
	}
	if got := goModuleOf(t, shop); got != "example.com/shop" {
		t.Errorf("go module = %q, want example.com/shop", got)
	}
	if got := folderOf(t, shop, "go-srv"); got != "shop-srv" {
		t.Errorf("backend folder = %q, want shop-srv", got)
	}
}

// A frontend bound with backend= to the single backend's derived name follows
// the backend when the replay renames the project.
func TestReplayWithNameKeepsAFrontendBoundToTheDerivedBackend(t *testing.T) {
	e := newEnv(t)
	wd := t.TempDir()
	first := []string{"first", "--name", "acme", "--stacks", "go-srv,ng-web", "--modules", "parameters",
		"--component", "ng-web=acme-console,backend=acme-srv"}
	if code, _, stderr := executeIn(t, wd, "", e.inputArgs(first...)...); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	recorded := filepath.Join(wd, "first", "initializer.selection.json")
	code, stdout, stderr := executeIn(t, wd, "", e.inputArgs("-f", recorded, "--name", "shop", "second")...)
	if code != exitOK {
		t.Fatalf("replay with --name exit %d: %s %s", code, stdout, stderr)
	}
	second := filepath.Join(wd, "second")
	if got := folderOf(t, second, "go-srv"); got != "shop-srv" {
		t.Errorf("backend folder = %q, want shop-srv", got)
	}
	if got := folderOf(t, second, "ng-web"); got != "acme-console" {
		t.Errorf("frontend folder = %q, want acme-console", got)
	}
}

// A backend the project owner named keeps its name on a replay with --name, so
// a frontend bound to it keeps that binding.
func TestReplayWithNameKeepsAFrontendBoundToANamedBackend(t *testing.T) {
	e := newEnv(t)
	wd := t.TempDir()
	first := []string{"first", "--name", "acme", "--stacks", "go-srv,ng-web", "--modules", "parameters",
		"--component", "go-srv=acme-api", "--component", "ng-web=acme-console,backend=acme-api"}
	if code, _, stderr := executeIn(t, wd, "", e.inputArgs(first...)...); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	recorded := filepath.Join(wd, "first", "initializer.selection.json")
	code, stdout, stderr := executeIn(t, wd, "", e.inputArgs("-f", recorded, "--name", "shop", "second")...)
	if code != exitOK {
		t.Fatalf("replay with --name exit %d: %s %s", code, stdout, stderr)
	}
	if got := folderOf(t, filepath.Join(wd, "second"), "go-srv"); got != "acme-api" {
		t.Errorf("backend folder = %q, want acme-api", got)
	}
}
