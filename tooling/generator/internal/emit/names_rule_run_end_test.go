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

// A name rule whose occurrence starts with the newline that ends a stack run's
// end marker is either refused at catalog build or applied by generate; it is
// never accepted and then delivered with the reference project name.
func TestARuleStartingAtTheNewlineAfterARunEndIsAppliedOrRefused(t *testing.T) {
	repo := fixture.Load(t)
	root := t.TempDir()
	copyTree(t, repo.Root, root)
	write := func(rel, text string) {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	compose := "name: fixture\n\nservices:\n  # initializer:stack go-srv\n  db:\n    image: postgres:18\n  # initializer:end go-srv\nx-label: fixture\n"
	write(".deploy/local/docker-compose.yml", compose)

	manifestData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(coreManifestPath)))
	if err != nil {
		t.Fatal(err)
	}
	rule := `{"path": ".deploy/local/docker-compose.yml", "text": "\nx-label: %{project}"}`
	changed := strings.Replace(string(manifestData), `"names": [`+projectRuleJSON+`]`, `"names": [`+projectRuleJSON+`, `+rule+`]`, 1)
	if changed == string(manifestData) {
		t.Fatal("the core manifest was not changed")
	}
	write(coreManifestPath, changed)

	list, err := os.ReadFile(filepath.Join(fixture.Dir(t), "files.txt"))
	if err != nil {
		t.Fatal(err)
	}
	built, issues, err := catalog.Build(catalog.BuildOptions{Root: os.DirFS(root), Files: strings.Fields(string(list)), Generator: generator.Version})
	if err != nil {
		t.Fatalf("catalog build: %v", err)
	}
	if len(issues) > 0 {
		for _, found := range issues {
			if found.Code != catalog.CodeNameRuleOverlap {
				t.Errorf("catalog build refused the rule for another reason: %+v", found)
			}
		}
		return
	}
	encoded, err := catalog.Encode(built)
	if err != nil {
		t.Fatal(err)
	}
	project, err := build(t, os.DirFS(root), built, encoded, selection("example.com/acme"))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	delivered := projectFile(t, project, ".deploy/local/docker-compose.yml")
	if !strings.Contains(delivered, "x-label: acme-backend\n") {
		t.Errorf("catalog build accepted the rule but generate did not apply it; delivered compose:\n%s", delivered)
	}
	if strings.Contains(delivered, "x-label: fixture\n") {
		t.Errorf("the reference project name leaks into the delivered compose:\n%s", delivered)
	}
}
