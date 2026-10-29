package emit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
)

const (
	coreManifestPath = "go-srv/internal/platform/core/module.manifest.json"
	projectRuleJSON  = `{"path": ".deploy/local/docker-compose.yml", "text": "name: %{project}"}`
)

// ruleFixture copies the fixture repository, appends a parameters block to its
// compose file, adds the rule texts to the core manifest and returns the root
// with the catalog built from the changed tree.
func ruleFixture(t *testing.T, block string, rules ...string) (string, catalog.Catalog, []byte) {
	t.Helper()
	repo := fixture.Load(t)
	root := t.TempDir()
	copyTree(t, repo.Root, root)
	composePath := filepath.Join(root, ".deploy", "local", "docker-compose.yml")
	compose, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(composePath, append(compose, block...), 0o644); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, filepath.FromSlash(coreManifestPath))
	manifestText, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	all := append([]string{projectRuleJSON}, rules...)
	changed := strings.Replace(string(manifestText), `"names": [`+projectRuleJSON+`]`, `"names": [`+strings.Join(all, ", ")+`]`, 1)
	if changed == string(manifestText) {
		t.Fatal("the core manifest rule was not found")
	}
	if err := os.WriteFile(manifestPath, []byte(changed), 0o644); err != nil {
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
	return root, built, encoded
}

func TestRulesRenameOccurrencesInsideKeptBlocksAndDisappearWithDroppedBlocks(t *testing.T) {
	block := "\n# initializer:module parameters\nsvc: fixture\n# initializer:end parameters\n"
	root, cat, encoded := ruleFixture(t, block, `{"path": ".deploy/local/docker-compose.yml", "text": "svc: %{project}"}`)

	kept, err := build(t, os.DirFS(root), cat, encoded, selection(targetModule, "parameters"))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	text := projectFile(t, kept, ".deploy/local/docker-compose.yml")
	if !strings.HasPrefix(text, "name: acme-backend\n") || !strings.Contains(text, "svc: acme-backend\n") {
		t.Fatalf("kept block not renamed:\n%s", text)
	}
	if strings.Contains(text, "fixture") {
		t.Fatalf("the reference name survived:\n%s", text)
	}

	dropped, err := build(t, os.DirFS(root), cat, encoded, selection(targetModule))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	text = projectFile(t, dropped, ".deploy/local/docker-compose.yml")
	if strings.Contains(text, "svc:") || strings.Contains(text, "fixture") {
		t.Fatalf("dropped block left a trace:\n%s", text)
	}
}

func TestRuleWithTheReferenceValueLeavesTheCopyUnchanged(t *testing.T) {
	repo := fixture.Load(t)
	project := mustBuild(t, repo, selection(targetModule, "parameters"))
	if got := projectFile(t, project, ".deploy/local/docker-compose.yml"); strings.Contains(got, "name: fixture") {
		t.Fatalf("compose keeps the reference name:\n%s", got)
	}
	reference := selection(targetModule, "parameters")
	reference.Project.Name = repo.Catalog.Reference["project"]
	same := mustBuild(t, repo, reference)
	source, err := os.ReadFile(filepath.Join(repo.Root, ".deploy", "local", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := projectFile(t, same, ".deploy/local/docker-compose.yml"); got != string(source) {
		t.Fatalf("generating at the reference name changed the compose file:\n%s", got)
	}
}
