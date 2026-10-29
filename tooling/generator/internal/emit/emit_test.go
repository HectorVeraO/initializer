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

	generator "github.com/HectorVeraO/initializer/tooling/generator"
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
	cases := []struct {
		tree string
		sel  manifest.Selection
	}{
		{"expected", selection(targetModule, "parameters")},
		{"expected-web", webSelection("parameters")},
		{"expected-core", selection(targetModule)},
		{"expected-core-web", webSelection()},
		{"expected-tenancy", selection(targetModule, "parameters", "tenancy")},
	}
	for _, tc := range cases {
		t.Run(tc.tree, func(t *testing.T) {
			first := mustWrite(t, mustBuild(t, repo, tc.sel))
			expected := filepath.Join(fixture.Dir(t), tc.tree)
			if *update {
				if err := os.RemoveAll(expected); err != nil {
					t.Fatal(err)
				}
				if err := emit.Write(expected, mustBuild(t, repo, tc.sel)); err != nil {
					t.Fatal(err)
				}
			}
			got := readTree(t, first)
			compareTrees(t, got, readTree(t, expected))

			second := mustWrite(t, mustBuild(t, repo, tc.sel))
			compareTrees(t, readTree(t, second), got)
		})
	}
}

func TestLockRecordsGrepInvertOfAnUninstalledCooperatingSpec(t *testing.T) {
	repo := fixture.Load(t)
	without := decodeLock(t, mustBuild(t, repo, selection(targetModule, "parameters")))
	if want := "@(tenancy)(?![A-Za-z0-9_-])"; without.TCKGrepInvert != want {
		t.Fatalf("TCKGrepInvert = %q, want %q", without.TCKGrepInvert, want)
	}
	with := decodeLock(t, mustBuild(t, repo, selection(targetModule, "parameters", "tenancy")))
	if with.TCKGrepInvert != "" {
		t.Fatalf("TCKGrepInvert = %q with tenancy installed, want empty", with.TCKGrepInvert)
	}
}

func TestRegenerationFromTheRecordedSelectionIsByteIdentical(t *testing.T) {
	repo := fixture.Load(t)
	first := mustBuild(t, repo, selection(targetModule, "feature-flags"))
	recorded, issues := manifest.DecodeSelection("initializer.selection.json", []byte(projectFile(t, first, lockfile.SelectionName)))
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
	params := projectFile(t, project, "go-srv/internal/platform/parameters/parameters.go")
	if !strings.Contains(params, "\""+targetModule+"/internal/platform/core\"") {
		t.Fatalf("imports not rewritten: %s", params)
	}
	modules := projectFile(t, project, "go-srv/cmd/server/modules.go")
	if !strings.Contains(modules, "Code generated by initializer generate. DO NOT EDIT.") ||
		!strings.Contains(modules, targetModule+"/internal/platform/parameters") ||
		!strings.Contains(modules, "identity.Module()") {
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
	if lock.TCKGrep != "@(core|feature-flags|identity|otel|parameters)(?![A-Za-z0-9_-])" {
		t.Errorf("tckGrep = %s", lock.TCKGrep)
	}
	if lock.TCKEnv["APP_FLAGS_FILE"] != "specs/feature-flags/tck/fixtures/flags.yaml" {
		t.Errorf("tckEnv = %v", lock.TCKEnv)
	}
	wantModules := []string{"core", "identity", "feature-flags", "parameters"}
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
			if file.Path == "flags.yaml" && file.SourcePath != ".initializer/project/flags.yaml" {
				t.Errorf("flags.yaml sourcePath = %q", file.SourcePath)
			}
		}
	}
	if strings.Join(gotModules, ",") != strings.Join(wantModules, ",") {
		t.Errorf("modules = %v", gotModules)
	}
	wantRewritten := []string{
		".deploy/local/.example.env",
		".deploy/local/docker-compose.yml",
		"go-srv/cmd/server/main.go",
		"go-srv/go.mod",
		"go-srv/internal/platform/identity/identity.go",
		"go-srv/internal/platform/parameters/parameters.go",
		"go-srv/internal/platform/parameters/parameters_test.go",
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
	sel := selection(fixtureModule, "feature-flags", "parameters", "tenancy")
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

func TestWebCompositionRootPerSelection(t *testing.T) {
	const header = "// Code generated by initializer generate. DO NOT EDIT.\nimport type { WebModule } from './core';\n"
	cases := []struct {
		name    string
		modules []string
		want    string
	}{
		{"core only", nil, header +
			"import { coreModule } from './core/module';\n\n" +
			"/** The console modules of this project, in catalog order. */\n" +
			"export const installedModules: readonly WebModule[] = [coreModule];\n"},
		{"core and parameters", []string{"parameters"}, header +
			"import { coreModule } from './core/module';\n" +
			"import { identityModule } from './identity/module';\n" +
			"import { parametersModule } from './parameters/module';\n\n" +
			"/** The console modules of this project, in catalog order. */\n" +
			"export const installedModules: readonly WebModule[] = [coreModule, identityModule, parametersModule];\n"},
		{"all modules", []string{"feature-flags", "parameters"}, header +
			"import { coreModule } from './core/module';\n" +
			"import { identityModule } from './identity/module';\n" +
			"import { featureFlagsModule } from './feature-flags/module';\n" +
			"import { parametersModule } from './parameters/module';\n\n" +
			"/** The console modules of this project, in catalog order. */\n" +
			"export const installedModules: readonly WebModule[] = [coreModule, identityModule, featureFlagsModule, parametersModule];\n"},
	}
	repo := fixture.Load(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project := mustBuild(t, repo, webSelection(tc.modules...))
			got := projectFile(t, project, "ng-web/src/app/platform/modules.ts")
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
		if strings.Contains(file.Path, "ng-web/src/app/platform/feature-flags/") {
			t.Errorf("unselected module file %s was copied", file.Path)
		}
		if strings.Contains(string(file.Data), "featureFlags") || strings.Contains(string(file.Data), "feature-flags") {
			if file.Path != "initializer.lock.json" && !strings.HasPrefix(file.Path, "specs/feature-flags/") {
				t.Errorf("%s references the unselected module", file.Path)
			}
		}
	}
	for _, path := range []string{"ng-web/package.json", "ng-web/src/app/platform/core/module.ts", "ng-web/src/app/platform/parameters/i18n/en.json", "ng-web/src/app/platform/modules.ts"} {
		if !present[path] {
			t.Errorf("project is missing %s", path)
		}
	}
	for _, path := range []string{"ng-web/AGENTS.md", "ng-web/cookbooks/notes.md", "ng-web/.vscode/settings.json"} {
		if present[path] {
			t.Errorf("repo-only file %s was copied", path)
		}
	}
	// parameters requires identity in ng-web too, so the edge pulls its folder.
	if !present["ng-web/src/app/platform/identity/module.ts"] {
		t.Error("the parameters edge must pull the Angular identity folder")
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
	if strings.Join(web, ",") != "core,identity,parameters" {
		t.Fatalf("ng-web modules = %v", web)
	}
}

func projectPaths(project emit.Project) map[string]bool {
	paths := map[string]bool{}
	for _, file := range project.Files {
		paths[file.Path] = true
	}
	return paths
}

func TestCoreOnlyProjectShipsNothingOfIdentity(t *testing.T) {
	repo := fixture.Load(t)
	project := mustBuild(t, repo, selection(targetModule))
	for p := range projectPaths(project) {
		for _, banned := range []string{"/identity/", ".deploy/local/kratos/", ".deploy/local/postgres-init/", "specs/tck-support/actors/", "specs/identity/"} {
			if strings.Contains(p, banned) {
				t.Errorf("core-only project ships %s", p)
			}
		}
	}
	for _, p := range []string{".deploy/local/docker-compose.yml", ".deploy/local/.example.env", ".deploy/local/README.md"} {
		text := projectFile(t, project, p)
		for _, banned := range []string{"kratos", "KRATOS_PG_PASSWORD", "postgres-init"} {
			if strings.Contains(text, banned) {
				t.Errorf("%s mentions %q in a core-only project", p, banned)
			}
		}
	}
	if !strings.Contains(projectFile(t, project, ".deploy/local/docker-compose.yml"), "postgres:") {
		t.Error("core-only compose lost postgres")
	}
	lock := decodeLock(t, project)
	if lock.TCKGrep != "@(core|otel)(?![A-Za-z0-9_-])" || lock.TCKGrepInvert != "" {
		t.Errorf("tckGrep = %q, tckGrepInvert = %q", lock.TCKGrep, lock.TCKGrepInvert)
	}
	if strings.Contains(projectFile(t, project, lockfile.Name), "tckGrepInvert") {
		t.Error("the lock of a core-only project records a tckGrepInvert key")
	}
	if got := strings.Join(lock.Selection.Modules, ","); got != "core" {
		t.Errorf("recorded modules = %q", got)
	}
}

func TestWebProxyKeepsKratosOnlyWithIdentity(t *testing.T) {
	repo := fixture.Load(t)
	with := mustBuild(t, repo, webSelection("parameters"))
	for _, p := range []string{"ng-web/proxy.conf.json", "ng-web/nginx.conf.template"} {
		want, err := os.ReadFile(filepath.Join(repo.Root, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		if got := projectFile(t, with, p); got != string(want) {
			t.Errorf("%s differs from the repository copy when identity is pulled", p)
		}
	}
	without := mustBuild(t, repo, webSelection())
	for _, p := range []string{"ng-web/proxy.conf.json", "ng-web/nginx.conf.template"} {
		if text := projectFile(t, without, p); strings.Contains(text, "kratos") || !strings.Contains(text, "/api") {
			t.Errorf("%s without identity:\n%s", p, text)
		}
	}
	for p := range projectPaths(without) {
		if strings.Contains(p, "ng-web/src/app/platform/identity/") {
			t.Errorf("core-web project ships %s", p)
		}
	}
}

// stackBlockFixture copies the fixture repository, appends an ng-web stack block to the
// compose file and returns the root with a catalog built from the changed tree.
func stackBlockFixture(t *testing.T) (string, catalog.Catalog, []byte, string) {
	t.Helper()
	repo := fixture.Load(t)
	root := t.TempDir()
	copyTree(t, repo.Root, root)
	composePath := filepath.Join(root, ".deploy", "local", "docker-compose.yml")
	compose, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatal(err)
	}
	block := "\n# initializer:stack ng-web\n# web-only-line\n# initializer:end ng-web\n"
	if err := os.WriteFile(composePath, append(compose, block...), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := os.ReadFile(filepath.Join(fixture.Dir(t), "files.txt"))
	if err != nil {
		t.Fatal(err)
	}
	built, issues, err := catalog.Build(catalog.BuildOptions{Root: os.DirFS(root), Files: strings.Fields(string(list)), Generator: generator.Version})
	if err != nil || len(issues) > 0 {
		t.Fatalf("catalog build: %v %v", err, issues)
	}
	encoded, err := catalog.Encode(built)
	if err != nil {
		t.Fatal(err)
	}
	return root, built, encoded, string(compose)
}

func TestStackBlockIsKeptOnlyWhenTheStackIsSelected(t *testing.T) {
	root, cat, encoded, original := stackBlockFixture(t)
	cases := []struct {
		name string
		sel  manifest.Selection
		want bool
	}{
		{"go-srv only drops it", selection(targetModule, "parameters"), false},
		{"go-srv and ng-web keeps it", webSelection("parameters"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project, err := build(t, os.DirFS(root), cat, encoded, tc.sel)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			text := projectFile(t, project, ".deploy/local/docker-compose.yml")
			hasBlock := strings.Contains(text, "# web-only-line")
			if hasBlock != tc.want {
				t.Errorf("block present = %v, want %v:\n%s", hasBlock, tc.want, text)
			}
			wantDropped := strings.Replace(original, "name: fixture", "name: acme-backend", 1)
			if !tc.want && text != wantDropped {
				t.Errorf("dropped block did not restore the original compose file:\n%s", text)
			}
		})
	}
}
