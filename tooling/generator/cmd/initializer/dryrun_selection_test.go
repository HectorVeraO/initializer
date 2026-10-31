package main

import (
	"os"
	"path/filepath"
	"testing"
)

// dryRunSelection runs a flag line as a dry run and returns the document and
// its selection member written to a starter file.
func dryRunSelection(t *testing.T, e env, wd string, flags ...string) (dryRunDocument, string) {
	t.Helper()
	line := append(append([]string{}, flags...), "--dry-run", "--format", "json")
	code, stdout, stderr := executeIn(t, wd, "", e.inputArgs(line...)...)
	if code != exitOK {
		t.Fatalf("flag dry run exit %d: %s %s", code, stdout, stderr)
	}
	var doc dryRunDocument
	decodeDocument(t, stdout, &doc)
	starter := filepath.Join(t.TempDir(), "starter.json")
	if err := os.WriteFile(starter, doc.Selection, 0o600); err != nil {
		t.Fatal(err)
	}
	return doc, starter
}

func TestTheDryRunSelectionOfAMultiBackendProjectPlansTheSameProject(t *testing.T) {
	e := newEnv(t)
	wd := t.TempDir()
	flags := []string{"starter", "--name", "acme", "--stacks", "go-srv,ng-web", "--modules", "identity",
		"--component", "go-srv=acme-one", "--component", "go-srv=acme-two,modules=tenancy",
		"--component", "ng-web=acme-web,backend=acme-two"}
	fromFlags, starter := dryRunSelection(t, e, wd, flags...)

	code, stdout, stderr := executeIn(t, wd, "", e.inputArgs("starter", "-f", starter, "--dry-run", "--format", "json")...)
	if code != exitOK {
		t.Fatalf("the dry run's selection member is not valid -f input: exit %d\nselection: %s\n%s%s",
			code, fromFlags.Selection, stdout, stderr)
	}
	var fromFile dryRunDocument
	decodeDocument(t, stdout, &fromFile)
	if fromFile.Files != fromFlags.Files {
		t.Errorf("flags plan %d files, the starter file plans %d", fromFlags.Files, fromFile.Files)
	}
	if string(fromFile.Selection) != string(fromFlags.Selection) {
		t.Errorf("the starter file resolves to another selection:\n%s\nwant\n%s", fromFile.Selection, fromFlags.Selection)
	}
}

func TestGeneratingFromTheDryRunSelectionOfAMultiBackendProjectMatchesTheFlagLine(t *testing.T) {
	root, catalogPath := realCatalog(t)
	e := env{root: root, catalog: catalogPath}
	wd := t.TempDir()
	flags := []string{"--name", "acme", "--go-module", "example.com/acme", "--stacks", "go-srv,ng-web",
		"--modules", "identity", "--service", "postgres=shared-pg",
		"--component", "go-srv=acme-srv-first,db-name=first_db,service.kratos=first-id",
		"--component", "go-srv=acme-srv-second,modules=identity+tenancy,go-module=example.com/acme/second",
		"--component", "ng-web=acme-web,backend=acme-srv-second"}
	_, starter := dryRunSelection(t, e, wd, append([]string{"dry"}, flags...)...)

	direct := filepath.Join(wd, "direct")
	if code, _, stderr := executeIn(t, wd, "", e.inputArgs(append([]string{direct}, flags...)...)...); code != exitOK {
		t.Fatalf("direct generate exit %d: %s", code, stderr)
	}
	fromFile := filepath.Join(wd, "from-file")
	if code, _, stderr := executeIn(t, wd, "", e.inputArgs("-f", starter, fromFile)...); code != exitOK {
		t.Fatalf("generate -f <dry-run selection> exit %d:\n%s", code, stderr)
	}
	a, b := tree(t, direct), tree(t, fromFile)
	for _, recorded := range []string{"initializer.lock.json", "initializer.selection.json"} {
		delete(a, recorded)
		delete(b, recorded)
	}
	if len(a) != len(b) {
		t.Fatalf("%d files from flags, %d from the dry-run selection", len(a), len(b))
	}
	for name, content := range a {
		if b[name] != content {
			t.Errorf("%s differs between the two generations", name)
		}
	}
}
