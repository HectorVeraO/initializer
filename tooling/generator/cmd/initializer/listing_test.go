package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
)

const moduleTable = `MODULE          STACKS          REQUIRES   SPEC                  VERSION
core            go-srv,ng-web   -          core 0.1.0            0.1.0   (always installed)
api-keys        go-srv          identity   api-keys 0.1.0        0.1.0
feature-flags   go-srv,ng-web   identity   feature-flags 0.1.0   0.1.0
identity        go-srv,ng-web   -          identity 0.1.0        0.1.0
parameters      go-srv,ng-web   identity   parameters 0.1.0      0.1.0
tenancy         go-srv          identity   tenancy 0.1.0         0.1.0
`

func TestModuleLsPrintsTheTable(t *testing.T) {
	c := newClone(t)
	code, stdout, stderr := c.run(t, "module", "ls")
	if code != exitOK || stdout != moduleTable || stderr != c.sourcesLine()+"\n" {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr: %q", code, stdout, stderr)
	}
}

func TestModuleLsFiltersByStack(t *testing.T) {
	c := newClone(t)
	code, stdout, _ := c.run(t, "module", "ls", "--stack", "ng-web")
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n")[1:] {
		ids = append(ids, strings.Fields(line)[0])
	}
	if got := strings.Join(ids, ","); got != "core,feature-flags,identity,parameters" {
		t.Fatalf("modules of ng-web = %s", got)
	}
}

func TestModuleLsRefusesAnUnknownStackWithAClosestMatch(t *testing.T) {
	c := newClone(t)
	code, stdout, stderr := c.run(t, "module", "ls", "--stack", "ng-wbe")
	if code != exitIssues || stdout != "" {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
	for _, want := range []string{
		`error: --stack: stack "ng-wbe" is not in the catalog [STACK_UNKNOWN]`,
		`  hint: run 'initializer stack ls'; did you mean "ng-web"?`,
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	_, stdout, _ = c.run(t, "module", "ls", "--stack", "zzzzzzzz", "--format", "json")
	issues := issueArray(t, stdout)
	if len(issues) != 1 || issues[0]["code"] != "STACK_UNKNOWN" || issues[0]["hint"] != "run 'initializer stack ls'" {
		t.Fatalf("issues = %v", issues)
	}
}

func TestModuleLsJSONDocument(t *testing.T) {
	c := newClone(t)
	code, stdout, stderr := c.run(t, "module", "ls", "--format", "json")
	if code != exitOK || stderr != c.sourcesLine()+"\n" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	var doc struct {
		Schema  int
		OK      bool
		Source  sourceRef
		Catalog catalogRef
		Modules []struct {
			Module   string
			Implied  bool
			Requires []string
			Stacks   []struct {
				Stack          string
				Version        string
				ImplementsSpec map[string]string
			}
		}
		Warnings []Warning
	}
	decodeDocument(t, stdout, &doc)
	if doc.Schema != 1 || !doc.OK || doc.Source.Commit != c.commit || len(doc.Catalog.SHA256) != 64 || doc.Catalog.File != "" {
		t.Fatalf("document = %+v", doc)
	}
	if len(doc.Modules) != 6 || doc.Modules[0].Module != "core" || !doc.Modules[0].Implied {
		t.Fatalf("modules = %+v", doc.Modules)
	}
	tenancy := doc.Modules[5]
	if tenancy.Module != "tenancy" || len(tenancy.Requires) != 1 || tenancy.Requires[0] != "identity" ||
		len(tenancy.Stacks) != 1 || tenancy.Stacks[0].Stack != "go-srv" || tenancy.Stacks[0].ImplementsSpec["tenancy"] != "0.1.0" {
		t.Fatalf("tenancy = %+v", tenancy)
	}
	if doc.Warnings == nil {
		t.Fatal("warnings must be an empty list, never null")
	}
}

// catalogWith writes the fixture catalog after change has edited it.
func catalogWith(t *testing.T, change func(*catalog.Catalog)) string {
	t.Helper()
	built := fixture.Load(t).Catalog
	change(&built)
	encoded, err := catalog.Encode(built)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "catalog.lock.json")
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestModuleLsNamesTheVersionOfEachStackWhenTheyDisagree(t *testing.T) {
	file := catalogWith(t, func(c *catalog.Catalog) {
		for i := range c.Stacks {
			for j := range c.Stacks[i].Modules {
				if c.Stacks[i].Stack == "ng-web" && c.Stacks[i].Modules[j].Module == "identity" {
					c.Stacks[i].Modules[j].Version = "0.2.0"
				}
			}
		}
	})
	e := newEnv(t)
	code, stdout, stderr := executeIn(t, t.TempDir(), "", "module", "ls", "--root", e.root, "--catalog", file)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "go-srv 0.1.0, ng-web 0.2.0") {
		t.Fatalf("stdout:\n%s", stdout)
	}
	if strings.Contains(stderr, "Sources: "+filepath.ToSlash(e.root)+" at") {
		t.Fatalf("the fixture folder is not a git checkout: %s", stderr)
	}
}

func TestModuleLsOnAnEmptyCatalogSaysSo(t *testing.T) {
	file := catalogWith(t, func(c *catalog.Catalog) { c.Stacks = nil })
	e := newEnv(t)
	code, stdout, stderr := executeIn(t, t.TempDir(), "", "module", "ls", "--root", e.root, "--catalog", file)
	if code != exitOK || stdout != "no modules in the catalog\n" {
		t.Fatalf("exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	code, stdout, _ = executeIn(t, t.TempDir(), "", "module", "ls", "--root", e.root, "--catalog", file, "--format", "json")
	var doc struct{ Modules []any }
	decodeDocument(t, stdout, &doc)
	if code != exitOK || doc.Modules == nil || len(doc.Modules) != 0 {
		t.Fatalf("exit %d, modules = %v", code, doc.Modules)
	}
}

func TestModuleLsShowsAnImpliedModuleByItsFlagNotByTheIDCore(t *testing.T) {
	file := catalogWith(t, func(c *catalog.Catalog) {
		for i := range c.Stacks {
			for j := range c.Stacks[i].Modules {
				switch c.Stacks[i].Modules[j].Module {
				case "core":
					c.Stacks[i].Modules[j].Implied = false
				case "identity":
					c.Stacks[i].Modules[j].Implied = true
				}
			}
		}
	})
	e := newEnv(t)
	_, stdout, _ := executeIn(t, t.TempDir(), "", "module", "ls", "--root", e.root, "--catalog", file)
	lines := strings.Split(stdout, "\n")
	if !strings.HasPrefix(lines[1], "identity") || !strings.Contains(lines[1], "(always installed)") {
		t.Fatalf("the implied module comes first:\n%s", stdout)
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "core") && strings.Contains(line, "always installed") {
			t.Fatalf("core is not implied here:\n%s", stdout)
		}
	}
}

const stackTable = `STACK    ROLE       SUFFIX   GO MODULE   MODULES
go-srv   backend    srv      yes         6
ng-web   frontend   web      no          4
Several components of one stack: repeat --component STACK=NAME (see 'initializer help generate').
`

func TestStackLsPrintsTheTableAndTheJSONDocument(t *testing.T) {
	c := newClone(t)
	code, stdout, stderr := c.run(t, "stack", "ls")
	if code != exitOK || stdout != stackTable || stderr != c.sourcesLine()+"\n" {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr: %q", code, stdout, stderr)
	}
	code, stdout, _ = c.run(t, "stack", "ls", "--format", "json")
	var doc struct {
		OK     bool
		Source sourceRef
		Stacks []struct {
			Stack    string
			Role     string
			Suffix   string
			GoModule bool
			Modules  int
		}
	}
	decodeDocument(t, stdout, &doc)
	if code != exitOK || !doc.OK || len(doc.Stacks) != 2 || doc.Source.Commit != c.commit {
		t.Fatalf("document = %+v", doc)
	}
	first, second := doc.Stacks[0], doc.Stacks[1]
	if first.Stack != "go-srv" || first.Role != "backend" || first.Suffix != "srv" || !first.GoModule || first.Modules != 6 ||
		second.Stack != "ng-web" || second.Role != "frontend" || second.Suffix != "web" || second.GoModule || second.Modules != 4 {
		t.Fatalf("stacks = %+v", doc.Stacks)
	}
}

func TestListIsNotAVerbButSuggestsLs(t *testing.T) {
	for _, group := range []string{"module", "stack"} {
		code, stdout, stderr := execute(t, group, "list")
		want := `error: unknown command "` + group + ` list"` + "\n" + `  hint: did you mean "` + group + ` ls"?`
		if code != exitUsage || stdout != "" || !strings.Contains(stderr, want) {
			t.Fatalf("%s list: exit %d, stdout %q, stderr %q", group, code, stdout, stderr)
		}
	}
}

func TestModuleAndStackGroupsNameTheirVerb(t *testing.T) {
	for _, group := range []string{"module", "stack"} {
		code, stdout, stderr := execute(t, group)
		if code != exitUsage || stdout != "" || !strings.Contains(stderr, "error: "+group+" needs a command: ls") ||
			!strings.Contains(stderr, group+" ls ") {
			t.Fatalf("%s: exit %d, stdout %q, stderr %q", group, code, stdout, stderr)
		}
		for _, args := range [][]string{{group, "--help"}, {"help", group}} {
			code, stdout, stderr := execute(t, args...)
			if code != exitOK || stderr != "" || !strings.Contains(stdout, group+" ls ") {
				t.Fatalf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
			}
		}
	}
}

func TestListingHelpHasItsFlagsAndExamples(t *testing.T) {
	cases := map[string][]string{
		"module ls": {"--stack ID", "--root DIR", "--catalog FILE", "initializer module ls --stack ng-web", "initializer module ls --format json"},
		"stack ls":  {"--root DIR", "--catalog FILE", "initializer stack ls --format json"},
		"version":   {"initializer version --format json", "initializer --version"},
	}
	for name, wants := range cases {
		_, stdout, _ := execute(t, append([]string{"help"}, strings.Fields(name)...)...)
		for _, want := range wants {
			if !strings.Contains(stdout, want) {
				t.Errorf("help %s lacks %q:\n%s", name, want, stdout)
			}
		}
	}
}

func TestSourcesNoteAboutAnUnknownBinaryCommitReachesTheNotes(t *testing.T) {
	c := newClone(t)
	build := generator.Build{Version: generator.Version, Commit: "0123456789abcdef0123456789abcdef01234567", Origin: generator.OriginDevel}
	host := hostAt(c.dir, filepath.Join(t.TempDir(), "initializer"), nil)
	code, stdout, stderr := executeAs(t, host, build, t.TempDir(), "stack", "ls", "--format", "json")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var doc struct{ Notes []string }
	decodeDocument(t, stdout, &doc)
	want := "this initializer was built from 0123456, which is not in " + filepath.ToSlash(c.dir) + "; cannot tell whether it is current"
	if len(doc.Notes) != 1 || doc.Notes[0] != want {
		t.Fatalf("notes = %q", doc.Notes)
	}
	if !strings.Contains(stderr, "note: "+want) {
		t.Fatalf("stderr = %q", stderr)
	}
}
