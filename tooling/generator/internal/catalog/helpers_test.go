package catalog

import (
	"io/fs"
	"os"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
)

const fixtureModule = "example.com/fixture/srv"

// symlinkMode is the file mode of a symlink in an in-memory file system.
const symlinkMode = fs.ModeSymlink | 0o777

const ngRoot = "ng-web/src/app/gatbase/"

// cleanRepo loads testdata/clean/repo into memory so a case can edit it.
func cleanRepo(t *testing.T) fstest.MapFS {
	t.Helper()
	repo, err := loadCleanRepo()
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	return repo
}

func loadCleanRepo() (fstest.MapFS, error) {
	root := os.DirFS("testdata/clean/repo")
	repo := fstest.MapFS{}
	err := fs.WalkDir(root, ".", func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(root, p)
		if err != nil {
			return err
		}
		repo[p] = &fstest.MapFile{Data: data}
		return nil
	})
	return repo, err
}

func put(repo fstest.MapFS, p, content string) {
	repo[p] = &fstest.MapFile{Data: []byte(content)}
}

func edit(t *testing.T, repo fstest.MapFS, p, old, replacement string) {
	t.Helper()
	file := repo[p]
	if file == nil {
		t.Fatalf("fixture file %s is missing", p)
	}
	text := string(file.Data)
	if !strings.Contains(text, old) {
		t.Fatalf("fixture file %s does not contain %q", p, old)
	}
	put(repo, p, strings.Replace(text, old, replacement, 1))
}

func filesOf(repo fstest.MapFS) []string {
	files := make([]string, 0, len(repo))
	for p := range repo {
		files = append(files, p)
	}
	sort.Strings(files)
	return files
}

func buildRepo(t *testing.T, repo fstest.MapFS) (Catalog, []issue.Issue) {
	t.Helper()
	built, issues, err := Build(BuildOptions{Root: repo, Files: filesOf(repo), Generator: "0.1.0"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return built, issues
}

func codesOf(issues []issue.Issue) []string {
	seen := map[string]bool{}
	for _, found := range issues {
		seen[found.Code] = true
	}
	codes := make([]string, 0, len(seen))
	for code := range seen {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

func assertCodes(t *testing.T, issues []issue.Issue, want ...string) {
	t.Helper()
	sort.Strings(want)
	got := codesOf(issues)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("issue codes = %v, want %v; issues: %+v", got, want, issues)
	}
}

func findModule(t *testing.T, built Catalog, stack, id string) ModuleEntry {
	t.Helper()
	for _, s := range built.Stacks {
		if s.Stack != stack {
			continue
		}
		for _, m := range s.Modules {
			if m.Module == id {
				return m
			}
		}
	}
	t.Fatalf("module %s/%s not in the catalog", stack, id)
	return ModuleEntry{}
}

func hasFile(files []File, p string) bool {
	for _, f := range files {
		if f.Path == p {
			return true
		}
	}
	return false
}

// ngManifest renders an ng-web module manifest.
func ngManifest(id string, implied bool, implements string) string {
	impliedText := "false"
	if implied {
		impliedText = "true"
	}
	core := ""
	files := `["ng-web/src/app/gatbase/` + id + `/"]`
	if id == "core" {
		files = `["ng-web/"]`
	} else {
		core = "\n  \"requiresCore\": \"^0.1.0\","
	}
	return `{
  "schema": 1,
  "stack": "ng-web",
  "module": "` + id + `",
  "version": "0.1.0",
  "implied": ` + impliedText + `,
  "implementsSpec": ` + implements + `,` + core + `
  "requiresModules": [],
  "files": ` + files + `,
  "exclude": []
}
`
}

// addNgModule adds an ng-web module with its manifest and entry point.
func addNgModule(repo fstest.MapFS, id string, implied bool, implements string) {
	put(repo, ngRoot+id+"/module.manifest.json", ngManifest(id, implied, implements))
	put(repo, ngRoot+id+"/module.ts", "export const placeholder = true;\n")
}

func addNgCore(repo fstest.MapFS) {
	addNgModule(repo, "core", true, `{"core": "1.0.0"}`)
}
