package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
)

const fixtureModule = "example.com/fixture/srv"

// env is a fixture repository, its catalog file and a fresh output path.
type env struct {
	root    string
	catalog string
	out     string
}

func newEnv(t *testing.T) env {
	t.Helper()
	repo := fixture.Load(t)
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.lock.json")
	if err := os.WriteFile(catalogPath, repo.CatalogBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	return env{root: repo.Root, catalog: catalogPath, out: filepath.Join(dir, "out")}
}

func (e env) args(extra ...string) []string {
	base := []string{"generate", "--root", e.root, "--catalog", e.catalog, "--out", e.out}
	return append(base, extra...)
}

func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		files[filepath.ToSlash(rel)] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func issueCodes(t *testing.T, stderr string) string {
	t.Helper()
	var codes []string
	for _, found := range issueArray(t, stderr) {
		codes = append(codes, found["code"])
	}
	return strings.Join(codes, ",")
}

func TestGenerateWritesTheProjectAndReportsTheCount(t *testing.T) {
	e := newEnv(t)
	code, stdout, stderr := execute(t, e.args("--selection", writeSelection(t, validSelection))...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	files := tree(t, e.out)
	var result struct {
		Project string
		Files   int
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout %q: %v", stdout, err)
	}
	if result.Files != len(files) || result.Project != filepath.ToSlash(e.out) {
		t.Fatalf("result %+v, files on disk %d", result, len(files))
	}
	if !strings.Contains(stderr, `"msg":"project_generated"`) {
		t.Fatalf("no project_generated event: %s", stderr)
	}
	for p, content := range files {
		if strings.Contains(content, fixtureModule) {
			t.Errorf("%s holds the source module path", p)
		}
	}
}

func TestGenerateFromFlagsEqualsGenerateFromTheFile(t *testing.T) {
	a, b := newEnv(t), newEnv(t)
	if code, _, stderr := execute(t, a.args("--selection", writeSelection(t, validSelection))...); code != exitOK {
		t.Fatalf("selection file: %d %s", code, stderr)
	}
	flags := b.args("--name", "acme-backend", "--go-module", "example.com/acme/backend", "--stacks", "go-srv", "--modules", "parameters")
	if code, _, stderr := execute(t, flags...); code != exitOK {
		t.Fatalf("flags: %d %s", code, stderr)
	}
	left, right := tree(t, a.out), tree(t, b.out)
	if len(left) != len(right) {
		t.Fatalf("file sets differ: %d vs %d", len(left), len(right))
	}
	for p, content := range left {
		if right[p] != content {
			t.Errorf("%s differs", p)
		}
	}
}

func TestGenerateFromTheRecordedSelectionReproducesTheProject(t *testing.T) {
	first := newEnv(t)
	if code, _, stderr := execute(t, first.args("--all-modules", "--name", "acme-backend", "--go-module", "example.com/acme/backend", "--stacks", "go-srv")...); code != exitOK {
		t.Fatalf("first: %d %s", code, stderr)
	}
	second := first
	second.out = filepath.Join(filepath.Dir(first.out), "again")
	recorded := filepath.Join(first.out, "gatbase.selection.json")
	if code, _, stderr := execute(t, second.args("--selection", recorded)...); code != exitOK {
		t.Fatalf("second: %d %s", code, stderr)
	}
	left, right := tree(t, first.out), tree(t, second.out)
	if len(left) == 0 || len(left) != len(right) {
		t.Fatalf("file sets differ: %d vs %d", len(left), len(right))
	}
	for p, content := range left {
		if right[p] != content {
			t.Errorf("%s differs", p)
		}
	}
}

func TestNonEmptyOutputIsRefusedAndNothingIsWritten(t *testing.T) {
	e := newEnv(t)
	if err := os.MkdirAll(e.out, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(e.out, "keep.txt")
	if err := os.WriteFile(keep, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := execute(t, e.args("--selection", writeSelection(t, validSelection))...)
	if code != exitIssues || issueCodes(t, stderr) != "OUTPUT_NOT_EMPTY" || stdout != "" {
		t.Fatalf("exit %d, codes %q, stdout %q", code, issueCodes(t, stderr), stdout)
	}
	if got := tree(t, e.out); len(got) != 1 || got["keep.txt"] != "mine" {
		t.Fatalf("output changed: %v", got)
	}
}

func TestOutputThatIsAFileIsRefused(t *testing.T) {
	e := newEnv(t)
	if err := os.WriteFile(e.out, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := execute(t, e.args("--selection", writeSelection(t, validSelection))...)
	if code != exitIssues || issueCodes(t, stderr) != "OUTPUT_NOT_EMPTY" {
		t.Fatalf("exit %d, codes %q", code, issueCodes(t, stderr))
	}
}

func TestPlanIssuesWriteNothing(t *testing.T) {
	e := newEnv(t)
	unknown := strings.Replace(validSelection, `"parameters"`, `"nonexistent"`, 1)
	code, _, stderr := execute(t, e.args("--selection", writeSelection(t, unknown))...)
	if code != exitIssues || issueCodes(t, stderr) != "MODULE_UNKNOWN" {
		t.Fatalf("exit %d, codes %q", code, issueCodes(t, stderr))
	}
	if _, err := os.Stat(e.out); err == nil {
		t.Fatal("the output directory was created")
	}
}

func TestCatalogFromAnotherTreeIsRefused(t *testing.T) {
	e := newEnv(t)
	other := t.TempDir()
	err := filepath.WalkDir(e.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(e.root, path)
		dest := filepath.Join(other, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(rel) == "specs/parameters/openapi.yaml" {
			data = append(data, "# edited\n"...)
		}
		return os.WriteFile(dest, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	e.root = other
	code, _, stderr := execute(t, e.args("--selection", writeSelection(t, validSelection))...)
	if code != exitIssues || issueCodes(t, stderr) != "CATALOG_FILE_MISMATCH" {
		t.Fatalf("exit %d, codes %q", code, issueCodes(t, stderr))
	}
	if _, err := os.Stat(e.out); err == nil {
		t.Fatal("the output directory was created")
	}
}

func TestCatalogFromAnotherGeneratorVersionIsRefused(t *testing.T) {
	e := newEnv(t)
	data, err := os.ReadFile(e.catalog)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(data), `"generator": "0.1.0"`, `"generator": "9.9.9"`, 1)
	if changed == string(data) {
		t.Fatal("fixture catalog has no generator line")
	}
	if err := os.WriteFile(e.catalog, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := execute(t, e.args("--selection", writeSelection(t, validSelection))...)
	if code != exitIssues || issueCodes(t, stderr) != "CATALOG_GENERATOR_MISMATCH" {
		t.Fatalf("exit %d, codes %q", code, issueCodes(t, stderr))
	}
}

func TestUnreadableOrUndecodableCatalogExitsThree(t *testing.T) {
	e := newEnv(t)
	sel := writeSelection(t, validSelection)
	missing := e
	missing.catalog = filepath.Join(t.TempDir(), "nope.json")
	if code, _, _ := execute(t, missing.args("--selection", sel)...); code != exitInternal {
		t.Errorf("missing catalog: exit %d", code)
	}
	if err := os.WriteFile(e.catalog, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := execute(t, e.args("--selection", sel)...); code != exitInternal {
		t.Errorf("undecodable catalog: exit %d", code)
	}
}

func TestCatalogPathOutsideTheRepositoryExitsThreeBeforeAnyWrite(t *testing.T) {
	e := newEnv(t)
	data, err := os.ReadFile(e.catalog)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(data), `"path": "specs/parameters/openapi.yaml"`, `"path": "../x"`, 1)
	if err := os.WriteFile(e.catalog, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, _ := execute(t, e.args("--selection", writeSelection(t, validSelection))...)
	if code != exitInternal {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(e.out); err == nil {
		t.Fatal("the output directory was created")
	}
}

func TestLeakedSourceModulePathExitsThreeAndWritesNothing(t *testing.T) {
	e := newEnv(t)
	other := t.TempDir()
	if err := os.CopyFS(other, os.DirFS(e.root)); err != nil {
		t.Fatal(err)
	}
	leak := []byte("see " + fixtureModule + "\n")
	if err := os.WriteFile(filepath.Join(other, "specs", "parameters", "leak.txt"), leak, 0o644); err != nil {
		t.Fatal(err)
	}
	repo := fixture.Load(t)
	crafted := repo.Catalog
	crafted.Specs = append([]catalog.SpecEntry(nil), crafted.Specs...)
	sum := sha256.Sum256(leak)
	for i := range crafted.Specs {
		if crafted.Specs[i].Spec == "parameters" {
			files := append([]catalog.File(nil), crafted.Specs[i].Files...)
			files = append(files, catalog.File{Path: "specs/parameters/leak.txt", SHA256: hex.EncodeToString(sum[:])})
			sort.Slice(files, func(a, b int) bool { return files[a].Path < files[b].Path })
			crafted.Specs[i].Files = files
		}
	}
	encoded, err := catalog.Encode(crafted)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.catalog, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	e.root = other
	code, _, stderr := execute(t, e.args("--selection", writeSelection(t, validSelection))...)
	if code != exitInternal || !strings.Contains(stderr, "leak.txt") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(e.out); err == nil {
		t.Fatal("the output directory was created")
	}
}

func TestVerifyExitCodes(t *testing.T) {
	e := newEnv(t)
	if code, _, stderr := execute(t, e.args("--selection", writeSelection(t, validSelection))...); code != exitOK {
		t.Fatalf("generate: %d %s", code, stderr)
	}
	code, stdout, stderr := execute(t, "verify", "--project", e.out)
	if code != exitOK || strings.TrimSpace(stdout) != `{"project":"`+filepath.ToSlash(e.out)+`","differences":0}` {
		t.Fatalf("fresh project: exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}

	edited := filepath.Join(e.out, "go-srv", "internal", "gatbase", "core", "core.go")
	if err := os.WriteFile(edited, []byte("package core\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(e.out, "go-srv", "internal", "gatbase", "parameters", "parameters.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.out, "go-srv", "internal", "gatbase", "coreauth", "new.go"), []byte("package coreauth\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = execute(t, "verify", "--project", e.out)
	if code != exitIssues || !strings.Contains(stdout, `"differences":3`) {
		t.Fatalf("edited project: exit %d, stdout %q", code, stdout)
	}
	if got := issueCodes(t, stderr); got != "UNTRACKED,MODIFIED,MISSING" && got != "MODIFIED,UNTRACKED,MISSING" {
		t.Fatalf("codes %q", got)
	}
	if !strings.Contains(stderr, `"msg":"verify_failed"`) {
		t.Fatalf("no verify_failed event: %s", stderr)
	}

	if err := os.Remove(filepath.Join(e.out, "gatbase.lock.json")); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := execute(t, "verify", "--project", e.out); code != exitInternal {
		t.Fatalf("missing lock: exit %d", code)
	}
}
