package emit_test

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/emit"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
)

var update = flag.Bool("update", false, "rewrite testdata/golden/expected")

const (
	fixtureModule = "example.com/fixture/srv"
	targetModule  = "example.com/acme/backend"
)

func selection(goModule string, modules ...string) manifest.Selection {
	return manifest.Selection{
		Schema:  1,
		Project: manifest.Project{Name: "acme-backend", GoModule: goModule},
		Stacks:  []string{"go-srv"},
		Modules: modules,
	}
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// build resolves sel against cat and builds the project from root.
func build(t *testing.T, root fs.FS, cat catalog.Catalog, catalogBytes []byte, sel manifest.Selection) (emit.Project, error) {
	t.Helper()
	resolved, issues := plan.Resolve(cat, sel)
	if len(issues) > 0 {
		t.Fatalf("resolve: %v", issues)
	}
	return emit.Build(emit.BuildOptions{Root: root, Plan: resolved, CatalogSHA256: hashOf(catalogBytes)})
}

func mustBuild(t *testing.T, repo fixture.Repo, sel manifest.Selection) emit.Project {
	t.Helper()
	project, err := build(t, os.DirFS(repo.Root), repo.Catalog, repo.CatalogBytes, sel)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return project
}

func mustWrite(t *testing.T, project emit.Project) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "project")
	if err := emit.Write(out, project); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return out
}

// readTree returns every file under dir keyed by slash path.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		tree[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("read tree: %v", err)
	}
	return tree
}

func compareTrees(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(want) == 0 {
		t.Fatal("expected tree is empty")
	}
	for p, content := range want {
		actual, found := got[p]
		if !found {
			t.Errorf("missing file %s", p)
			continue
		}
		if actual != content {
			t.Errorf("file %s differs", p)
		}
	}
	for p := range got {
		if _, found := want[p]; !found {
			t.Errorf("unexpected file %s", p)
		}
	}
}

func projectFile(t *testing.T, project emit.Project, path string) string {
	t.Helper()
	for _, file := range project.Files {
		if file.Path == path {
			return string(file.Data)
		}
	}
	t.Fatalf("project has no %s", path)
	return ""
}

func decodeLock(t *testing.T, project emit.Project) lockfile.Lock {
	t.Helper()
	lock, err := lockfile.Decode([]byte(projectFile(t, project, lockfile.Name)))
	if err != nil {
		t.Fatalf("decode lock: %v", err)
	}
	return lock
}

func TestGolden(t *testing.T) {
	repo := fixture.Load(t)
	sel := selection(targetModule, "parameters")
	first := mustWrite(t, mustBuild(t, repo, sel))
	expected := filepath.Join(fixture.Dir(t), "expected")
	if *update {
		if err := os.RemoveAll(expected); err != nil {
			t.Fatal(err)
		}
		if err := emit.Write(expected, mustBuild(t, repo, sel)); err != nil {
			t.Fatal(err)
		}
	}
	got := readTree(t, first)
	compareTrees(t, got, readTree(t, expected))

	second := mustWrite(t, mustBuild(t, repo, sel))
	compareTrees(t, readTree(t, second), got)
}

func TestRegenerationFromTheRecordedSelectionIsByteIdentical(t *testing.T) {
	repo := fixture.Load(t)
	first := mustBuild(t, repo, selection(targetModule, "feature-flags"))
	recorded, issues := manifest.DecodeSelection("gatbase.selection.json", []byte(projectFile(t, first, lockfile.SelectionName)))
	if len(issues) > 0 {
		t.Fatalf("decode recorded selection: %v", issues)
	}
	second := mustBuild(t, repo, recorded)
	compareTrees(t, readTree(t, mustWrite(t, second)), readTree(t, mustWrite(t, first)))
}

func TestImportsModuleLineAndComposeNameAreRewritten(t *testing.T) {
	repo := fixture.Load(t)
	project := mustBuild(t, repo, selection(targetModule, "parameters"))

	if got := projectFile(t, project, "go-srv/go.mod"); !strings.HasPrefix(got, "module "+targetModule+"\n") {
		t.Fatalf("go.mod line 1: %q", got)
	}
	if got := projectFile(t, project, ".deploy/local/docker-compose.yml"); !strings.HasPrefix(got, "name: acme-backend\n") {
		t.Fatalf("compose name: %q", got)
	}
	params := projectFile(t, project, "go-srv/internal/gatbase/parameters/parameters.go")
	if !strings.Contains(params, "\""+targetModule+"/internal/gatbase/core\"") {
		t.Fatalf("imports not rewritten: %s", params)
	}
	modules := projectFile(t, project, "go-srv/cmd/server/modules.go")
	if !strings.Contains(modules, "Code generated by initializer generate. DO NOT EDIT.") ||
		!strings.Contains(modules, targetModule+"/internal/gatbase/parameters") ||
		!strings.Contains(modules, "coreauth.Module()") {
		t.Fatalf("modules.go: %s", modules)
	}
	if strings.Contains(modules, "featureflags") {
		t.Fatalf("modules.go lists an unselected module: %s", modules)
	}
	for _, file := range project.Files {
		if strings.Contains(string(file.Data), fixtureModule) {
			t.Errorf("%s still holds the source module path", file.Path)
		}
	}
}

func TestLockRecordsDeliveredAndSourceHashes(t *testing.T) {
	repo := fixture.Load(t)
	project := mustBuild(t, repo, selection(targetModule, "feature-flags", "parameters"))
	lock := decodeLock(t, project)

	if lock.CatalogSHA256 != hashOf(repo.CatalogBytes) {
		t.Errorf("catalogSha256 = %s", lock.CatalogSHA256)
	}
	if lock.TCKGrep != "@(core|core-auth|feature-flags|otel|parameters)(?![A-Za-z0-9_-])" {
		t.Errorf("tckGrep = %s", lock.TCKGrep)
	}
	if lock.TCKEnv["GATBASE_FLAGS_FILE"] != "specs/feature-flags/tck/fixtures/flags.yaml" {
		t.Errorf("tckEnv = %v", lock.TCKEnv)
	}
	wantModules := []string{"core", "core-auth", "feature-flags", "parameters"}
	var gotModules []string
	rewritten := map[string]bool{}
	for _, module := range lock.Modules {
		gotModules = append(gotModules, module.Module)
		for _, file := range module.Files {
			if file.SourceSHA256 != "" {
				rewritten[file.Path] = true
			}
			if file.SHA256 != hashOf([]byte(projectFile(t, project, file.Path))) {
				t.Errorf("%s: sha256 does not match the delivered bytes", file.Path)
			}
			if file.SourcePath != "" && file.Path != "flags.yaml" {
				t.Errorf("%s: unexpected sourcePath", file.Path)
			}
			if file.Path == "flags.yaml" && file.SourcePath != ".gatbase/project/flags.yaml" {
				t.Errorf("flags.yaml sourcePath = %q", file.SourcePath)
			}
		}
	}
	if strings.Join(gotModules, ",") != strings.Join(wantModules, ",") {
		t.Errorf("modules = %v", gotModules)
	}
	wantRewritten := []string{
		".deploy/local/docker-compose.yml",
		"go-srv/cmd/server/main.go",
		"go-srv/go.mod",
		"go-srv/internal/gatbase/coreauth/coreauth.go",
		"go-srv/internal/gatbase/parameters/parameters.go",
		"go-srv/internal/gatbase/parameters/parameters_test.go",
	}
	for _, p := range wantRewritten {
		if !rewritten[p] {
			t.Errorf("%s has no sourceSha256", p)
		}
	}
	if len(rewritten) != len(wantRewritten) {
		t.Errorf("rewritten files = %v", rewritten)
	}
	for _, entry := range lock.Generated {
		if entry.Path == lockfile.Name {
			t.Error("the lock lists itself")
		}
	}
	last := project.Files[len(project.Files)-1]
	if last.Path != lockfile.Name {
		t.Errorf("last file = %s", last.Path)
	}
}

func TestIdentityTargetOmitsSourceHashes(t *testing.T) {
	repo := fixture.Load(t)
	sel := selection(fixtureModule, "parameters")
	sel.Project.Name = "fixture"
	project := mustBuild(t, repo, sel)
	for _, module := range decodeLock(t, project).Modules {
		for _, file := range module.Files {
			if file.SourceSHA256 != "" {
				t.Errorf("%s: sourceSha256 on an identity copy", file.Path)
			}
		}
	}
}

func TestSourceModulePathLeakIsRefused(t *testing.T) {
	repo := fixture.Load(t)
	root := t.TempDir()
	copyTree(t, repo.Root, root)
	leak := []byte("see " + fixtureModule + " for details\n")
	if err := os.WriteFile(filepath.Join(root, "specs", "parameters", "leak.txt"), leak, 0o644); err != nil {
		t.Fatal(err)
	}
	crafted := repo.Catalog
	crafted.Specs = append([]catalog.SpecEntry(nil), crafted.Specs...)
	for i := range crafted.Specs {
		if crafted.Specs[i].Spec != "parameters" {
			continue
		}
		files := append([]catalog.File(nil), crafted.Specs[i].Files...)
		files = append(files, catalog.File{Path: "specs/parameters/leak.txt", SHA256: hashOf(leak)})
		crafted.Specs[i].Files = files
	}
	encoded, err := catalog.Encode(crafted)
	if err != nil {
		t.Fatal(err)
	}
	_, err = build(t, os.DirFS(root), crafted, encoded, selection(targetModule, "parameters"))
	if err == nil || !strings.Contains(err.Error(), "specs/parameters/leak.txt") {
		t.Fatalf("want a leak error naming the file, got %v", err)
	}
}

func TestChangedSourceIsRefused(t *testing.T) {
	repo := fixture.Load(t)
	root := t.TempDir()
	copyTree(t, repo.Root, root)
	path := filepath.Join(root, "specs", "parameters", "openapi.yaml")
	if err := os.WriteFile(path, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := build(t, os.DirFS(root), repo.Catalog, repo.CatalogBytes, selection(targetModule, "parameters")); err == nil {
		t.Fatal("want an error for a source that no longer matches the catalog")
	}
}

func TestCatalogPathsThatEscapeAreRefused(t *testing.T) {
	repo := fixture.Load(t)
	for _, bad := range []string{"../x", "/etc/passwd", "a/../../x", "a\\b"} {
		crafted := repo.Catalog
		crafted.Specs = append([]catalog.SpecEntry(nil), crafted.Specs...)
		for i := range crafted.Specs {
			if crafted.Specs[i].Spec == "parameters" {
				crafted.Specs[i].Files = []catalog.File{{Path: bad, SHA256: hashOf(nil)}}
			}
		}
		_, err := build(t, os.DirFS(repo.Root), crafted, repo.CatalogBytes, selection(targetModule, "parameters"))
		if err == nil || !strings.Contains(err.Error(), "is not a repo-relative path") {
			t.Errorf("%q: want a repo-relative path error, got %v", bad, err)
		}
	}
}

func TestCatalogFileOnARenderedPathIsRefused(t *testing.T) {
	repo := fixture.Load(t)
	crafted := repo.Catalog
	crafted.Specs = append([]catalog.SpecEntry(nil), crafted.Specs...)
	for i := range crafted.Specs {
		if crafted.Specs[i].Spec == "parameters" {
			crafted.Specs[i].Files = []catalog.File{{Path: "specs/parameters/openapi.yaml", SHA256: hashOfFile(t, repo.Root, "specs/parameters/openapi.yaml"), Target: "README.md"}}
		}
	}
	_, err := build(t, os.DirFS(repo.Root), crafted, repo.CatalogBytes, selection(targetModule, "parameters"))
	if err == nil || !strings.Contains(err.Error(), "lands on the rendered path") || !strings.Contains(err.Error(), "README.md") {
		t.Fatalf("want a rendered-path collision error naming README.md, got %v", err)
	}
}

func TestWriteStaysUnderTheOutputDirectory(t *testing.T) {
	parent := t.TempDir()
	out := filepath.Join(parent, "out")
	project := emit.Project{Files: []emit.File{
		{Path: "../escape.txt", Data: []byte("x")},
		{Path: lockfile.Name, Data: []byte("{}")},
	}}
	if err := emit.Write(out, project); err == nil {
		t.Fatal("want an error for a path outside the output directory")
	}
	if _, err := os.Stat(filepath.Join(parent, "escape.txt")); err == nil {
		t.Fatal("a file was written outside the output directory")
	}
}

func TestCheckOutput(t *testing.T) {
	dir := t.TempDir()
	filled := filepath.Join(dir, "filled")
	if err := os.MkdirAll(filled, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filled, "a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	asFile := filepath.Join(dir, "file")
	if err := os.WriteFile(asFile, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		path    string
		blocked bool
	}{
		"absent":    {filepath.Join(dir, "absent"), false},
		"empty dir": {empty, false},
		"filled":    {filled, true},
		"file":      {asFile, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			issues, err := emit.CheckOutput(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if tc.blocked != (len(issues) == 1 && issues[0].Code == emit.CodeOutputNotEmpty) {
				t.Fatalf("issues = %v", issues)
			}
		})
	}
}

func TestBuildRejectsIncompleteOptions(t *testing.T) {
	if _, err := emit.Build(emit.BuildOptions{}); err == nil {
		t.Fatal("want an error for empty options")
	}
}

func hashOfFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return hashOf(data)
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(to, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy tree: %v", err)
	}
}

func TestComposeFileWhoseNameLineDiffersFromTheCatalogIsRefused(t *testing.T) {
	repo := fixture.Load(t)
	crafted := repo.Catalog
	crafted.ComposeName = "other-name"
	encoded, err := catalog.Encode(crafted)
	if err != nil {
		t.Fatal(err)
	}
	_, err = build(t, os.DirFS(repo.Root), crafted, encoded, selection(targetModule, "parameters"))
	if err == nil || !strings.Contains(err.Error(), "line 1 must be exactly") {
		t.Fatalf("want a compose name error, got %v", err)
	}
}

func webSelection(modules ...string) manifest.Selection {
	sel := selection(targetModule, modules...)
	sel.Stacks = []string{"go-srv", "ng-web"}
	return sel
}

func TestGoldenWeb(t *testing.T) {
	repo := fixture.Load(t)
	sel := webSelection("parameters")
	first := mustWrite(t, mustBuild(t, repo, sel))
	expected := filepath.Join(fixture.Dir(t), "expected-web")
	if *update {
		if err := os.RemoveAll(expected); err != nil {
			t.Fatal(err)
		}
		if err := emit.Write(expected, mustBuild(t, repo, sel)); err != nil {
			t.Fatal(err)
		}
	}
	got := readTree(t, first)
	compareTrees(t, got, readTree(t, expected))

	second := mustWrite(t, mustBuild(t, repo, sel))
	compareTrees(t, readTree(t, second), got)
}

func TestWebCompositionRootPerSelection(t *testing.T) {
	const header = "// Code generated by initializer generate. DO NOT EDIT.\nimport type { GatbaseWebModule } from './core';\n"
	cases := []struct {
		name    string
		modules []string
		want    string
	}{
		{"core only", nil, header +
			"import { coreModule } from './core/module';\n\n" +
			"/** The console modules of this project, in catalog order. */\n" +
			"export const gatbaseModules: readonly GatbaseWebModule[] = [coreModule];\n"},
		{"core and parameters", []string{"parameters"}, header +
			"import { coreModule } from './core/module';\n" +
			"import { parametersModule } from './parameters/module';\n\n" +
			"/** The console modules of this project, in catalog order. */\n" +
			"export const gatbaseModules: readonly GatbaseWebModule[] = [coreModule, parametersModule];\n"},
		{"all modules", []string{"feature-flags", "parameters"}, header +
			"import { coreModule } from './core/module';\n" +
			"import { featureFlagsModule } from './feature-flags/module';\n" +
			"import { parametersModule } from './parameters/module';\n\n" +
			"/** The console modules of this project, in catalog order. */\n" +
			"export const gatbaseModules: readonly GatbaseWebModule[] = [coreModule, featureFlagsModule, parametersModule];\n"},
	}
	repo := fixture.Load(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project := mustBuild(t, repo, webSelection(tc.modules...))
			got := projectFile(t, project, "ng-web/src/app/gatbase/modules.ts")
			if got != tc.want {
				t.Fatalf("modules.ts:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestWebProjectCarriesOnlySelectedModulesAndNoRepoOnlyFiles(t *testing.T) {
	repo := fixture.Load(t)
	project := mustBuild(t, repo, webSelection("parameters"))
	present := map[string]bool{}
	for _, file := range project.Files {
		present[file.Path] = true
		if strings.Contains(file.Path, "ng-web/src/app/gatbase/feature-flags/") {
			t.Errorf("unselected module file %s was copied", file.Path)
		}
		if strings.Contains(string(file.Data), "featureFlags") || strings.Contains(string(file.Data), "feature-flags") {
			if file.Path != "gatbase.lock.json" && !strings.HasPrefix(file.Path, "specs/feature-flags/") {
				t.Errorf("%s references the unselected module", file.Path)
			}
		}
	}
	for _, path := range []string{"ng-web/package.json", "ng-web/src/app/gatbase/core/module.ts", "ng-web/src/app/gatbase/parameters/i18n/en.json", "ng-web/src/app/gatbase/modules.ts"} {
		if !present[path] {
			t.Errorf("project is missing %s", path)
		}
	}
	for _, path := range []string{"ng-web/AGENTS.md", "ng-web/cookbooks/notes.md", "ng-web/.vscode/settings.json"} {
		if present[path] {
			t.Errorf("repo-only file %s was copied", path)
		}
	}
	// core-auth has no Angular folder: the stack plans only the modules it has.
	if present["ng-web/src/app/gatbase/core-auth/module.ts"] {
		t.Error("core-auth must have no Angular folder")
	}
}

func TestWebProjectsAreByteIdenticalAcrossBuilds(t *testing.T) {
	repo := fixture.Load(t)
	first := mustBuild(t, repo, webSelection("feature-flags", "parameters"))
	second := mustBuild(t, repo, webSelection("parameters", "feature-flags"))
	compareTrees(t, readTree(t, mustWrite(t, second)), readTree(t, mustWrite(t, first)))
}

func TestWebStackLockListsAngularModules(t *testing.T) {
	repo := fixture.Load(t)
	lock := decodeLock(t, mustBuild(t, repo, webSelection("parameters")))
	var web []string
	for _, module := range lock.Modules {
		if module.Stack == "ng-web" {
			web = append(web, module.Module)
		}
	}
	if strings.Join(web, ",") != "core,parameters" {
		t.Fatalf("ng-web modules = %v", web)
	}
}

func TestFlagsConfigSourceWithoutTheFeatureFlagsBlockIsRefusedWhenTheModuleIsNotSelected(t *testing.T) {
	cases := []struct {
		name         string
		templatePath string
		content      string
	}{
		{"env template", ".deploy/local/.example.env", "INITIALIZER_PG_PASSWORD=change-me\n"},
		{"deploy README", ".deploy/local/README.md", "# Local\n\n## Ports\n\nnone\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertFlagsConfigRefused(t, tc.templatePath, []byte(tc.content))
		})
	}
}

func assertFlagsConfigRefused(t *testing.T, templatePath string, template []byte) {
	t.Helper()
	repo := fixture.Load(t)
	root := t.TempDir()
	copyTree(t, repo.Root, root)
	if err := os.MkdirAll(filepath.Join(root, ".deploy", "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(templatePath)), template, 0o644); err != nil {
		t.Fatal(err)
	}
	crafted := repo.Catalog
	crafted.Specs = append([]catalog.SpecEntry(nil), crafted.Specs...)
	for i := range crafted.Specs {
		if crafted.Specs[i].Spec != "parameters" {
			continue
		}
		files := append([]catalog.File(nil), crafted.Specs[i].Files...)
		files = append(files, catalog.File{Path: templatePath, SHA256: hashOf(template)})
		crafted.Specs[i].Files = files
	}
	encoded, err := catalog.Encode(crafted)
	if err != nil {
		t.Fatal(err)
	}
	_, err = build(t, os.DirFS(root), crafted, encoded, selection(targetModule, "parameters"))
	isRefusal := err != nil && strings.Contains(err.Error(), templatePath) && strings.Contains(err.Error(), "the feature-flags block is missing")
	if !isRefusal {
		t.Fatalf("want a missing feature-flags block error naming %s, got %v", templatePath, err)
	}
}
