package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/source"
)

// withoutSources drops the Sources: line from stderr text, for tests that
// assert the rest of it.
func withoutSources(stderr string) string {
	var kept []string
	for _, line := range strings.SplitAfter(stderr, "\n") {
		if !strings.HasPrefix(line, "Sources: ") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "")
}

// hostAt returns a Host of a person standing in cwd with the given binary and
// environment; git is the real one.
func hostAt(cwd, exe string, env map[string]string) source.Host {
	return source.Host{
		Getwd:      func() (string, error) { return cwd, nil },
		Executable: func() (string, error) { return exe, nil },
		Getenv:     func(name string) string { return env[name] },
		MainModule: source.MainModule,
	}
}

// executeAs runs args in-process with host, build and a working directory.
func executeAs(t *testing.T, host source.Host, build generator.Build, workDir string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	ctx := &Context{
		Stdout: &out, Stderr: &errOut, Stdin: strings.NewReader(""), WorkDir: workDir, Host: host, Build: build,
		Format: formatText, Logger: discardLogger(),
	}
	code = runWith(ctx, args)
	return code, out.String(), errOut.String()
}

// clone is a git checkout of the fixture with the generator marker.
type clone struct{ dir, commit string }

func newClone(t *testing.T) clone {
	t.Helper()
	dir, commit := fixture.Clone(t)
	return clone{dir: dir, commit: commit}
}

// run executes args as a person whose working directory is the clone's.
func (c clone) run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	return executeAs(t, hostAt(c.dir, filepath.Join(t.TempDir(), "initializer"), nil), generator.Build{}, t.TempDir(), args...)
}

// sourcesLineOf returns the Sources: line the clone prints.
func (c clone) sourcesLine() string {
	return "Sources: " + filepath.ToSlash(c.dir) + " at " + c.commit[:7]
}

func sha256Of(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}

func TestGenerateFindsItsSourcesFromAClone(t *testing.T) {
	c := newClone(t)
	out := filepath.Join(t.TempDir(), "acme")
	code, stdout, stderr := c.run(t, "generate", out, "--stacks", "go-srv", "--modules", "identity", "--go-module", "example.com/acme")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if firstLine(stderr) != c.sourcesLine() {
		t.Fatalf("first stderr line = %q, want %q", firstLine(stderr), c.sourcesLine())
	}
	if !strings.HasPrefix(stdout, "Project acme generated in ") {
		t.Fatalf("stdout = %q", stdout)
	}

	lock := projectLock(t, out)
	if lock["catalogSha256"] != catalogHashOfBuild(t, c) {
		t.Fatalf("catalogSha256 = %v, want the hash of catalog build over the same tree", lock["catalogSha256"])
	}
	recorded, _ := lock["source"].(map[string]any)
	if recorded["commit"] != c.commit || recorded["modified"] != false {
		t.Fatalf("lock source = %v, want commit %s", lock["source"], c.commit)
	}
}

// catalogHashOfBuild returns the sha256 of `catalog build --out` over the clone.
func catalogHashOfBuild(t *testing.T, c clone) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "catalog.lock.json")
	if code, _, stderr := c.run(t, "catalog", "build", "--out", out); code != exitOK {
		t.Fatalf("catalog build exit %d: %s", code, stderr)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return sha256Of(data)
}

func TestGeneratingTwiceFromTheSameTreeGivesIdenticalLocks(t *testing.T) {
	c := newClone(t)
	var locks []string
	for range 2 {
		out := filepath.Join(t.TempDir(), "acme")
		if code, _, stderr := c.run(t, "generate", out, "--stacks", "go-srv", "--modules", "identity", "--go-module", "example.com/acme"); code != exitOK {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		data, err := os.ReadFile(filepath.Join(out, "initializer.lock.json"))
		if err != nil {
			t.Fatal(err)
		}
		locks = append(locks, string(data))
	}
	if locks[0] != locks[1] {
		t.Fatal("two generations of the same tree differ")
	}
}

func TestLockRecordsAModifiedCataloguedFile(t *testing.T) {
	c := newClone(t)
	path := filepath.Join(c.dir, "go-srv", "internal", "platform", "core", "core.go")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(original, []byte("\n// local edit\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "acme")
	code, _, stderr := c.run(t, "generate", out, "--stacks", "go-srv", "--go-module", "example.com/acme")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if want := c.sourcesLine() + " (modified)"; firstLine(stderr) != want {
		t.Fatalf("first stderr line = %q, want %q", firstLine(stderr), want)
	}
	recorded, _ := projectLock(t, out)["source"].(map[string]any)
	if recorded["modified"] != true {
		t.Fatalf("lock source = %v", recorded)
	}
}

func TestLockOmitsTheSourceForARootThatIsNotAGitCheckout(t *testing.T) {
	e := newEnv(t)
	code, stdout, stderr := executeIn(t, t.TempDir(), "", e.args("--stacks", "go-srv", "--go-module", "example.com/acme", "--name", "acme")...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if want := "Sources: " + filepath.ToSlash(e.root) + " (not a git checkout)"; !strings.Contains(stderr, want) {
		t.Fatalf("stderr %q lacks %q", stderr, want)
	}
	if _, has := projectLock(t, e.out)["source"]; has {
		t.Fatal("a root that is not a git checkout must record no source")
	}
	if !strings.Contains(stdout, "generated") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestRootAloneBuildsTheCatalogInMemoryAndRootWithCatalogKeepsItsMeaning(t *testing.T) {
	c := newClone(t)
	catalogFile := filepath.Join(t.TempDir(), "catalog.lock.json")
	if code, _, stderr := c.run(t, "catalog", "build", "--root", c.dir, "--out", catalogFile); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	aloneOut, fileOut := filepath.Join(t.TempDir(), "alone"), filepath.Join(t.TempDir(), "file")
	elsewhere := hostAt(t.TempDir(), filepath.Join(t.TempDir(), "initializer"), nil)
	common := []string{"--stacks", "go-srv", "--modules", "identity", "--go-module", "example.com/acme", "--name", "acme"}
	for out, extra := range map[string][]string{aloneOut: {"--root", c.dir}, fileOut: {"--root", c.dir, "--catalog", catalogFile}} {
		args := append(append([]string{"generate", out}, common...), extra...)
		if code, _, stderr := executeAs(t, elsewhere, generator.Build{}, t.TempDir(), args...); code != exitOK {
			t.Fatalf("exit %d: %s", code, stderr)
		}
	}
	alone, viaFile := tree(t, aloneOut), tree(t, fileOut)
	if len(alone) != len(viaFile) {
		t.Fatalf("%d files in memory, %d with --catalog", len(alone), len(viaFile))
	}
	for name, content := range alone {
		if viaFile[name] != content {
			t.Fatalf("%s differs between the in-memory catalog and --catalog", name)
		}
	}
}

func TestSourcesAreFoundByEachRuleAndReportedInJSON(t *testing.T) {
	c := newClone(t)
	away := t.TempDir()
	insideBin := filepath.Join(c.dir, "tooling", "generator", "bin", "initializer")
	cases := map[string]struct {
		host source.Host
		args []string
		how  string
	}{
		"flag":        {hostAt(away, filepath.Join(away, "x"), map[string]string{source.EnvRoot: away}), []string{"--root", c.dir}, "flag"},
		"env":         {hostAt(away, filepath.Join(away, "x"), map[string]string{source.EnvRoot: c.dir}), nil, "env"},
		"working-dir": {hostAt(filepath.Join(c.dir, "go-srv"), filepath.Join(away, "x"), nil), nil, "working-dir"},
		"executable":  {hostAt(away, insideBin, nil), nil, "executable"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			args := append([]string{"stack", "ls", "--format", "json"}, tc.args...)
			code, stdout, stderr := executeAs(t, tc.host, generator.Build{}, away, args...)
			if code != exitOK {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			var doc struct{ Source sourceRef }
			decodeDocument(t, stdout, &doc)
			if doc.Source.How != tc.how || doc.Source.Dir != filepath.ToSlash(c.dir) || doc.Source.Commit != c.commit {
				t.Fatalf("source = %+v, want how %s", doc.Source, tc.how)
			}
		})
	}
}

func TestSourceFailuresExitWithTheirCodeAndHint(t *testing.T) {
	c := newClone(t)
	away := t.TempDir()
	env := newEnv(t)
	unmarked := t.TempDir()
	noGit := hostAt(away, "", nil)
	noGit.Git = func(context.Context, string, ...string) ([]byte, error) {
		return nil, &exec.Error{Name: "git", Err: exec.ErrNotFound}
	}
	brokenGit := hostAt(away, "", nil)
	brokenGit.Git = func(context.Context, string, ...string) ([]byte, error) { return nil, os.ErrPermission }

	cases := map[string]struct {
		host     source.Host
		args     []string
		exit     int
		code     string
		hintPart string
	}{
		"no checkout":       {hostAt(away, filepath.Join(away, "x"), nil), nil, exitUsage, "SOURCE_ROOT_NOT_FOUND", "run inside a clone"},
		"root not a folder": {hostAt(away, "", nil), []string{"--root", filepath.Join(away, "missing")}, exitUsage, "SOURCE_ROOT_INVALID", "--root <clone>"},
		"env not a clone":   {hostAt(away, "", map[string]string{source.EnvRoot: unmarked}), nil, exitUsage, "SOURCE_ROOT_INVALID", "set INITIALIZER_ROOT"},
		"not a git root":    {hostAt(away, "", nil), []string{"--root", env.root}, exitUsage, "SOURCE_NOT_GIT", "clone the repo, or pass --catalog"},
		"no git":            {noGit, []string{"--root", env.root}, exitUsage, "GIT_NOT_FOUND", "install git, or pass --catalog"},
		"catalog missing":   {hostAt(away, "", nil), []string{"--root", c.dir, "--catalog", filepath.Join(away, "none.json")}, exitInternal, "CATALOG_UNREADABLE", "catalog build"},
		"git fails":         {brokenGit, []string{"--root", env.root}, exitInternal, "SOURCE_UNREADABLE", "git status"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			args := append([]string{"module", "ls"}, tc.args...)
			code, stdout, stderr := executeAs(t, tc.host, generator.Build{}, away, args...)
			if code != tc.exit || stdout != "" {
				t.Fatalf("exit %d, stdout %q, stderr %s", code, stdout, stderr)
			}
			if !strings.Contains(stderr, "["+tc.code+"]") || !strings.Contains(stderr, "  hint: ") || !strings.Contains(stderr, tc.hintPart) {
				t.Fatalf("text stderr = %q", stderr)
			}

			code, stdout, stderr = executeAs(t, tc.host, generator.Build{}, away, append(args, "--format", "json")...)
			if code != tc.exit || stdout != "" {
				t.Fatalf("json: exit %d, stdout %q, stderr %s", code, stdout, stderr)
			}
			var doc struct {
				OK       bool
				Problems []struct{ Code, Message, Hint string }
				Error    struct{ Code, Message, Hint string }
			}
			decodeDocument(t, stderr, &doc)
			got := doc.Error
			if tc.exit == exitUsage {
				if len(doc.Problems) != 1 {
					t.Fatalf("document = %s", stderr)
				}
				got = doc.Problems[0]
			}
			if doc.OK || got.Code != tc.code || got.Message == "" || !strings.Contains(got.Hint, tc.hintPart) {
				t.Fatalf("document = %s", stderr)
			}
		})
	}
}

func TestSourceProblemsTravelWithTheValueProblemsOfTheSameRun(t *testing.T) {
	away := t.TempDir()
	host := hostAt(away, filepath.Join(away, "x"), nil)
	code, stdout, stderr := executeAs(t, host, generator.Build{}, away, "generate", "acme", "--name", "A", "--format", "json")
	if code != exitUsage || stdout != "" {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
	var doc struct {
		Problems []struct{ Code string }
		Issues   []struct{ Code string }
	}
	decodeDocument(t, stderr, &doc)
	if len(doc.Problems) != 1 || doc.Problems[0].Code != "SOURCE_ROOT_NOT_FOUND" || len(doc.Issues) != 1 || doc.Issues[0].Code != "NAME_INVALID" {
		t.Fatalf("document = %s", stderr)
	}
}

func TestAnUnreadableCatalogStillReportsTheInputProblems(t *testing.T) {
	c := newClone(t)
	code, stdout, stderr := c.run(t, "generate", "acme", "--name", "A", "--catalog", filepath.Join(t.TempDir(), "none.json"), "--format", "json")
	if code != exitIssues {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if got := issueCodes(t, stdout); got != "NAME_INVALID" {
		t.Fatalf("issue codes = %s", got)
	}
}

func TestATrackedFileDeletedFromTheWorkingTreeIsNamedWithGitHints(t *testing.T) {
	c := newClone(t)
	if err := os.Remove(filepath.Join(c.dir, "go-srv", "internal", "platform", "core", "core.go")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := c.run(t, "module", "ls")
	name := "go-srv/internal/platform/core/core.go"
	if code != exitInternal || stdout != "" || !strings.Contains(stderr, name) || !strings.Contains(stderr, "git restore "+name) ||
		!strings.Contains(stderr, "[SOURCE_UNREADABLE]") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestModuleUnknownHintsRunModuleLs(t *testing.T) {
	c := newClone(t)
	cases := map[string]string{
		"tenacy": `  hint: run 'initializer module ls'; did you mean "tenancy"?`,
		"zzz":    `  hint: run 'initializer module ls'` + "\n",
	}
	for module, want := range cases {
		t.Run(module, func(t *testing.T) {
			code, _, stderr := c.run(t, "generate", "acme", "--modules", module, "--stacks", "go-srv", "--go-module", "example.com/acme")
			if code != exitIssues || !strings.Contains(stderr, "[MODULE_UNKNOWN]") || !strings.Contains(stderr, want) {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
		})
	}
}

func TestCatalogBuildWithoutRootOrFilesMatchesTheThreeFlagForm(t *testing.T) {
	c := newClone(t)
	implicit := filepath.Join(t.TempDir(), "implicit.json")
	code, stdout, stderr := c.run(t, "catalog", "build", "--out", implicit)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if firstLine(stderr) != c.sourcesLine() {
		t.Fatalf("first stderr line = %q", firstLine(stderr))
	}

	list := []byte(gitIn(t, c.dir, "ls-files") + "\n")
	files := filepath.Join(t.TempDir(), "files.bin")
	if err := os.WriteFile(files, bytes.ReplaceAll(list, []byte("\n"), []byte{0}), 0o644); err != nil {
		t.Fatal(err)
	}
	explicit := filepath.Join(t.TempDir(), "explicit.json")
	if code, _, stderr := c.run(t, "catalog", "build", "--root", c.dir, "--files", files, "--out", explicit); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	a, err := os.ReadFile(implicit)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(explicit)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("the catalog built from git ls-files differs from the one built from the file list")
	}
	if want := "Catalog written to " + filepath.ToSlash(implicit) + " (sha256 " + sha256Of(a)[:12] + ").\n"; stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

func TestCatalogBuildJSONNamesTheSource(t *testing.T) {
	c := newClone(t)
	out := filepath.Join(t.TempDir(), "catalog.lock.json")
	code, stdout, stderr := c.run(t, "catalog", "build", "--out", out, "--format", "json")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var doc struct {
		OK      bool
		Catalog string
		SHA256  string
		Source  sourceRef
	}
	decodeDocument(t, stdout, &doc)
	if !doc.OK || doc.Catalog != filepath.ToSlash(out) || len(doc.SHA256) != 64 ||
		doc.Source.How != "working-dir" || doc.Source.Commit != c.commit || doc.Source.Modified == nil || *doc.Source.Modified {
		t.Fatalf("document = %+v", doc)
	}
}

func TestCatalogBuildReportsTheNonGitRootAndTheMissingGitRoot(t *testing.T) {
	e := newEnv(t)
	away := t.TempDir()
	out := filepath.Join(t.TempDir(), "c.json")
	code, _, stderr := executeAs(t, hostAt(away, "", nil), generator.Build{}, away, "catalog", "build", "--root", e.root, "--out", out)
	if code != exitUsage || !strings.Contains(stderr, "[SOURCE_NOT_GIT]") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	code, _, stderr = executeAs(t, hostAt(away, "", nil), generator.Build{}, away, "catalog", "build", "--root", filepath.Join(away, "none"), "--out", out)
	if code != exitUsage || !strings.Contains(stderr, "[SOURCE_ROOT_INVALID]") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

func TestSourcesLineForms(t *testing.T) {
	modified := true
	cases := map[string]struct {
		ref  sourceRef
		want string
	}{
		"clean":    {sourceRef{Dir: "D:/src/initializer", Commit: "22e304a0dde9db4d9aa809008e6a8372241c2116", Modified: new(bool)}, "Sources: D:/src/initializer at 22e304a"},
		"modified": {sourceRef{Dir: "D:/src/initializer", Commit: "22e304a0dde9db4d9aa809008e6a8372241c2116", Modified: &modified}, "Sources: D:/src/initializer at 22e304a (modified)"},
		"not git":  {sourceRef{Dir: "/tmp/tree"}, "Sources: /tmp/tree (not a git checkout)"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := sourcesLine(tc.ref); got != tc.want {
				t.Fatalf("sourcesLine = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSourcesResolvedIsLogged(t *testing.T) {
	c := newClone(t)
	code, _, stderr := c.run(t, "stack", "ls", "--log", "json")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	event := logEvent(t, stderr, "sources_resolved")
	if event["how"] != "working-dir" || event["commit"] != c.commit || event["catalog_from"] != "checkout" ||
		event["dir"] != filepath.ToSlash(c.dir) {
		t.Fatalf("event = %v", event)
	}
}

// logEvent returns the first log record named msg on stderr.
func logEvent(t *testing.T, stderr, msg string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(stderr, "\n") {
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) == nil && record["msg"] == msg {
			return record
		}
	}
	t.Fatalf("no %s event on stderr: %s", msg, stderr)
	return nil
}

func TestWholeFileCatalogHashIsTheHashOfTheFileBytes(t *testing.T) {
	e := newEnv(t)
	away := t.TempDir()
	code, stdout, stderr := executeAs(t, hostAt(away, "", nil), generator.Build{}, away,
		"stack", "ls", "--root", e.root, "--catalog", e.catalog, "--format", "json")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var doc struct{ Catalog catalogRef }
	decodeDocument(t, stdout, &doc)
	data, err := os.ReadFile(e.catalog)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Catalog.SHA256 != sha256Of(data) || doc.Catalog.File != filepath.ToSlash(e.catalog) {
		t.Fatalf("catalog = %+v", doc.Catalog)
	}
}
