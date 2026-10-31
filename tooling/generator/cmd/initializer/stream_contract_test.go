package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Section 10 of docs/generate-a-project.md: --format json prints one JSON
// document, on stderr for exit 2 and 3 with stdout empty. An exit-3 failure
// after the sources were announced still leaves stderr as that one document.
func TestJSONFormatExitThreeLeavesStderrAsOneDocument(t *testing.T) {
	e := newEnv(t)
	parent := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(parent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.out = filepath.Join(parent, "sub")

	code, stdout, stderr := executeJSON(t, e.args("-f", writeSelection(t, validSelection))...)
	if code != exitInternal {
		t.Fatalf("exit %d, want %d\nstderr:\n%s", code, exitInternal, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	var doc struct {
		OK    bool    `json:"ok"`
		Error failure `json:"error"`
	}
	decodeDocument(t, stderr, &doc)
	if doc.OK || doc.Error.Code != codeOutputUnwritable {
		t.Fatalf("document = %+v, want an %s failure", doc, codeOutputUnwritable)
	}
}

// The Sources: line is a diagnostic of the run, not of the failure: a JSON run
// that ends with exit 0 still prints it on stderr next to the stdout document.
func TestJSONFormatSuccessKeepsTheSourcesLineOnStderr(t *testing.T) {
	e := newEnv(t)
	code, stdout, stderr := executeJSON(t, e.args("-f", writeSelection(t, validSelection), "--dry-run")...)
	if code != exitOK {
		t.Fatalf("exit %d\nstderr:\n%s", code, stderr)
	}
	var doc map[string]any
	decodeDocument(t, stdout, &doc)
	if !strings.HasPrefix(stderr, "Sources: ") {
		t.Fatalf("stderr = %q, want it to start with the Sources: line", stderr)
	}
}

// The same holds when the run ends with exit 1: the issues document is the one
// JSON document on stdout and the Sources: line stays on stderr.
func TestJSONFormatIssuesKeepTheSourcesLineOnStderr(t *testing.T) {
	e := newEnv(t)
	code, stdout, stderr := executeJSON(t, e.args("-f", writeSelection(t, validSelection), "--name", "Not_A_Label", "--dry-run")...)
	if code != exitIssues {
		t.Fatalf("exit %d, want %d\nstderr:\n%s", code, exitIssues, stderr)
	}
	var doc map[string]any
	decodeDocument(t, stdout, &doc)
	if !strings.HasPrefix(stderr, "Sources: ") {
		t.Fatalf("stderr = %q, want it to start with the Sources: line", stderr)
	}
	if strings.Contains(stderr, "{") {
		t.Errorf("stderr carries a JSON document, want stdout only: %q", stderr)
	}
}

// Section 10: note: lines go to stderr and stdout carries the result only,
// also for the text dry run of a project with several backends.
func TestMultiBackendDryRunKeepsItsNoteOffStdout(t *testing.T) {
	root, catalogPath := realCatalog(t)
	selection := filepath.Join(root, "tooling", "generator", "testdata", "ci", "multi-srv-web.json")
	args := []string{"generate", "--root", root, "--catalog", catalogPath, "-f", selection, "acme-backend", "--dry-run"}
	code, stdout, stderr := executeIn(t, t.TempDir(), "", args...)
	if code != exitOK {
		t.Fatalf("dry run exit %d: %s", code, stderr)
	}
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "note:") {
			t.Errorf("stdout carries a note: line, want it on stderr only: %q", line)
		}
	}
	if !strings.Contains(stderr, "note: go modules derived from example.com/acme/backend; pass go-module= per component to change them\n") {
		t.Errorf("stderr = %q, want the derived go modules note", stderr)
	}
}
