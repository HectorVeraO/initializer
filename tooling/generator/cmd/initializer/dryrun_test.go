package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
)

func TestDryRunPrintsTheResolvedProjectAndWritesNothing(t *testing.T) {
	c := newClone(t)
	work := t.TempDir()
	host := hostAt(c.dir, filepath.Join(t.TempDir(), "initializer"), nil)
	code, stdout, stderr := executeAs(t, host, generator.Build{}, work, "generate", "acme",
		"--stacks", "go-srv,ng-web", "--modules", "parameters", "--dry-run")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	files := writtenFileCount(t, c, "--stacks", "go-srv,ng-web", "--modules", "parameters")
	want := "Project acme in ./acme (dry run, nothing written)\n" +
		"COMPONENT   STACK    FOLDER      MODULES\n" +
		"acme-srv    go-srv   acme-srv/   core, identity (required by parameters), parameters\n" +
		"acme-web    ng-web   acme-web/   core, identity (required by parameters), parameters\n" +
		"Go module   example.com/acme (default)\n" +
		"Ports       band 101\n" +
		strconv.Itoa(files) + " files\n"
	if stdout != want {
		t.Fatalf("stdout\n%s\nwant\n%s", stdout, want)
	}
	if firstLine(stderr) != c.sourcesLine() {
		t.Fatalf("first stderr line = %q", firstLine(stderr))
	}
	if _, err := os.Stat(filepath.Join(work, "acme")); !os.IsNotExist(err) {
		t.Fatalf("a dry run must not create the folder: %v", err)
	}
}

// writtenFileCount generates the project for real and returns how many files
// land on disk, the count a dry run must announce.
func writtenFileCount(t *testing.T, c clone, args ...string) int {
	t.Helper()
	work := t.TempDir()
	host := hostAt(c.dir, filepath.Join(t.TempDir(), "initializer"), nil)
	code, _, stderr := executeAs(t, host, generator.Build{}, work, append([]string{"generate", "acme"}, args...)...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	count := 0
	walkErr := filepath.WalkDir(filepath.Join(work, "acme"), func(_ string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			count++
		}
		return err
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	return count
}

func TestDryRunLeavesAnEmptyFolderEmpty(t *testing.T) {
	c := newClone(t)
	work := t.TempDir()
	if err := os.Mkdir(filepath.Join(work, "acme"), 0o755); err != nil {
		t.Fatal(err)
	}
	host := hostAt(c.dir, filepath.Join(t.TempDir(), "initializer"), nil)
	code, stdout, stderr := executeAs(t, host, generator.Build{}, work, "generate", "acme", "--stacks", "go-srv", "--go-module", "example.com/acme", "--dry-run")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "acme-srv    go-srv   acme-srv/   core\n") {
		t.Fatalf("stdout:\n%s", stdout)
	}
	entries, err := os.ReadDir(filepath.Join(work, "acme"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries %v err %v: the empty folder must stay empty", entries, err)
	}
}

func TestDryRunMarksAGoModuleThatWasGivenAsNotDefault(t *testing.T) {
	c := newClone(t)
	code, stdout, stderr := c.run(t, "generate", "acme", "--stacks", "go-srv", "--go-module", "example.com/mine", "--dry-run")
	if code != exitOK || !strings.Contains(stdout, "Go module   example.com/mine\n") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestDryRunRefusesANonEmptyFolderLikeGenerate(t *testing.T) {
	c := newClone(t)
	work := t.TempDir()
	if err := os.MkdirAll(filepath.Join(work, "acme"), 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(work, "acme", "keep.txt")
	if err := os.WriteFile(keep, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := hostAt(c.dir, filepath.Join(t.TempDir(), "initializer"), nil)
	for _, format := range []string{formatText, formatJSON} {
		code, stdout, stderr := executeAs(t, host, generator.Build{}, work, "generate", "acme", "--stacks", "go-srv",
			"--go-module", "example.com/acme", "--dry-run", "--format", format)
		if code != exitIssues {
			t.Fatalf("%s: exit %d, stdout %q, stderr %q", format, code, stdout, stderr)
		}
		if format == formatText && !strings.Contains(stderr, "[OUTPUT_NOT_EMPTY]") {
			t.Fatalf("stderr = %q", stderr)
		}
		if format == formatJSON && issueCodes(t, stdout) != "OUTPUT_NOT_EMPTY" {
			t.Fatalf("stdout = %q", stdout)
		}
	}
	if data, err := os.ReadFile(keep); err != nil || string(data) != "mine" {
		t.Fatalf("the existing file changed: %q %v", data, err)
	}
}

func TestDryRunReportsTheSameIssuesAsGenerate(t *testing.T) {
	c := newClone(t)
	work := t.TempDir()
	code, _, stderr := c.run(t, "generate", filepath.Join(work, "acme"), "--stacks", "go-srv", "--modules", "tenacy", "--dry-run")
	if code != exitIssues || !strings.Contains(stderr, "[MODULE_UNKNOWN]") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(work, "acme")); !os.IsNotExist(err) {
		t.Fatalf("nothing may be written: %v", err)
	}
}

// dryRunDocument is the JSON of generate --dry-run.
type dryRunDocument struct {
	Schema     int
	OK         bool
	DryRun     bool
	Project    string
	Path       string
	Files      int
	Source     sourceRef
	Catalog    catalogRef
	Selection  json.RawMessage
	Recorded   json.RawMessage
	Components []struct{ Stack, Name string }
	Names      map[string]string
	Ports      struct{ Band int }
	Added      []moduleAdded
	Notes      []string
	Warnings   []Warning
}

func TestDryRunJSONDocument(t *testing.T) {
	c := newClone(t)
	code, stdout, stderr := c.run(t, "generate", "acme", "--stacks", "go-srv,ng-web", "--modules", "parameters",
		"--go-module", "example.com/acme", "--dry-run", "--format", "json")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var doc dryRunDocument
	decodeDocument(t, stdout, &doc)
	if doc.Schema != 1 || !doc.OK || !doc.DryRun || doc.Project != "acme" || doc.Path != "acme" || doc.Files == 0 ||
		doc.Source.Commit != c.commit || len(doc.Catalog.SHA256) != 64 {
		t.Fatalf("document = %+v", doc)
	}
	if len(doc.Components) != 2 || doc.Components[0].Name != "acme-srv" || doc.Components[1].Name != "acme-web" {
		t.Fatalf("components = %+v", doc.Components)
	}
	if doc.Names["project"] != "acme" || doc.Names["component.go-srv"] != "acme-srv" || doc.Names["component.ng-web"] != "acme-web" {
		t.Fatalf("names = %v", doc.Names)
	}
	if len(doc.Added) != 2 || doc.Added[0].Module != "identity" || doc.Added[0].RequiredBy[0] != "parameters" {
		t.Fatalf("added = %+v", doc.Added)
	}
	wantSelection := `{"schema":2,"project":{"name":"acme","goModule":"example.com/acme"},"stacks":["go-srv","ng-web"],` +
		`"modules":["core","identity","parameters"],"components":[{"stack":"go-srv","name":"acme-srv"},{"stack":"ng-web","name":"acme-web"}],"ports":{"band":101}}`
	if string(doc.Selection) != wantSelection {
		t.Fatalf("selection\n%s\nwant\n%s", doc.Selection, wantSelection)
	}
	if doc.Notes == nil || doc.Warnings == nil {
		t.Fatal("notes and warnings must be lists, never null")
	}
}

func TestDryRunRecordedIsTheSelectionFileTheRealRunWrites(t *testing.T) {
	c := newClone(t)
	args := []string{"--stacks", "go-srv,ng-web", "--modules", "parameters", "--go-module", "example.com/acme", "--name", "acme"}
	_, stdout, _ := c.run(t, append([]string{"generate", "acme", "--dry-run", "--format", "json"}, args...)...)
	var doc dryRunDocument
	decodeDocument(t, stdout, &doc)

	out := filepath.Join(t.TempDir(), "acme")
	if code, _, stderr := c.run(t, append([]string{"generate", out}, args...)...); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	written, err := os.ReadFile(filepath.Join(out, "initializer.selection.json"))
	if err != nil {
		t.Fatal(err)
	}
	var compact, want any
	if err := json.Unmarshal(doc.Recorded, &compact); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(written, &want); err != nil {
		t.Fatal(err)
	}
	gotText, _ := json.Marshal(compact)
	wantText, _ := json.Marshal(want)
	if string(gotText) != string(wantText) {
		t.Fatalf("recorded\n%s\nwritten\n%s", gotText, wantText)
	}
	if doc.Files != len(tree(t, out)) {
		t.Fatalf("dry run counted %d files, the real run wrote %d", doc.Files, len(tree(t, out)))
	}
}

func TestDryRunSelectionFedBackWithTheFileFlagGeneratesTheSameProject(t *testing.T) {
	c := newClone(t)
	_, stdout, _ := c.run(t, "generate", "acme", "--stacks", "go-srv,ng-web", "--modules", "parameters",
		"--go-module", "example.com/acme", "--dry-run", "--format", "json")
	var doc dryRunDocument
	decodeDocument(t, stdout, &doc)
	file := filepath.Join(t.TempDir(), "selection.json")
	if err := os.WriteFile(file, doc.Selection, 0o644); err != nil {
		t.Fatal(err)
	}

	direct := filepath.Join(t.TempDir(), "direct")
	fromFile := filepath.Join(t.TempDir(), "from-file")
	if code, _, stderr := c.run(t, "generate", direct, "--name", "acme", "--stacks", "go-srv,ng-web", "--modules", "parameters",
		"--go-module", "example.com/acme"); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if code, _, stderr := c.run(t, "generate", fromFile, "-f", file); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
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
			t.Fatalf("%s differs between the two generations", name)
		}
	}

	_, again, _ := c.run(t, "generate", "acme", "-f", file, "--dry-run", "--format", "json")
	var second dryRunDocument
	decodeDocument(t, again, &second)
	if string(second.Selection) != string(doc.Selection) || second.Files != doc.Files {
		t.Fatalf("the resolved selection is not a fixed point:\n%s\n%s", second.Selection, doc.Selection)
	}
}
