package core_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// listedPackage is the part of `go list -json` the boundary rules read.
type listedPackage struct {
	ImportPath     string
	Dir            string
	Module         *struct{ Path, Dir string }
	GoFiles        []string
	CgoFiles       []string
	TestGoFiles    []string
	XTestGoFiles   []string
	EmbedFiles     []string
	TestEmbedFiles []string
	Imports        []string
	TestImports    []string
	XTestImports   []string
}

// allImports is every import of the package, tests included.
func (p listedPackage) allImports() []string {
	var all []string
	all = append(all, p.Imports...)
	all = append(all, p.TestImports...)
	all = append(all, p.XTestImports...)
	return all
}

func (p listedPackage) sourceFiles() []string {
	var files []string
	files = append(files, p.GoFiles...)
	files = append(files, p.CgoFiles...)
	files = append(files, p.TestGoFiles...)
	files = append(files, p.XTestGoFiles...)
	return files
}

// loadPackages runs `go list -json ./...` from the module root.
func loadPackages(t *testing.T) []listedPackage {
	t.Helper()
	return listPackagesIn(t, moduleRoot(t))
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		_, statErr := os.Stat(filepath.Join(dir, "go.mod"))
		if statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above the test directory")
		}
		dir = parent
	}
}

func listPackagesIn(t *testing.T, root string) []listedPackage {
	t.Helper()
	cmd := exec.Command("go", "list", "-e", "-json", "./...")
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, stderr.String())
	}
	var packages []listedPackage
	decoder := json.NewDecoder(bytes.NewReader(out))
	for decoder.More() {
		var p listedPackage
		decodeErr := decoder.Decode(&p)
		if decodeErr != nil {
			t.Fatalf("decode go list output: %v", decodeErr)
		}
		packages = append(packages, p)
	}
	if len(packages) == 0 {
		t.Fatalf("go list returned no packages")
	}
	return packages
}

// modulePathOf reads the Go module path from the listing.
func modulePathOf(t *testing.T, packages []listedPackage) string {
	t.Helper()
	for _, p := range packages {
		if p.Module != nil && p.Module.Path != "" {
			return p.Module.Path
		}
	}
	t.Fatalf("go list reported no module path")
	return ""
}

// relImport returns the path of imp relative to the module, or false when imp
// is not part of this module.
func relImport(modulePath, imp string) (string, bool) {
	if imp == modulePath {
		return "", true
	}
	rel, found := strings.CutPrefix(imp, modulePath+"/")
	return rel, found
}

const (
	platformDir = "internal/platform"
	coreDir     = "internal/platform/core"
	serverDir   = "cmd/server"
)

// platformOwner returns the first path element below internal/platform ("core",
// "identity", ...) or false when rel is not under it.
func platformOwner(rel string) (string, bool) {
	below, found := strings.CutPrefix(rel, platformDir+"/")
	if !found {
		return "", false
	}
	owner, _, _ := strings.Cut(below, "/")
	return owner, true
}

func isUnder(rel, dir string) bool {
	return rel == dir || strings.HasPrefix(rel, dir+"/")
}

// ruleCoreImportsNoModule is rule 1.
func ruleCoreImportsNoModule(modulePath string, packages []listedPackage) []string {
	var problems []string
	for _, p := range packages {
		rel, _ := relImport(modulePath, p.ImportPath)
		if !isUnder(rel, coreDir) {
			continue
		}
		for _, imp := range p.allImports() {
			target, internal := relImport(modulePath, imp)
			owner, isPlatform := platformOwner(target)
			if internal && isPlatform && owner != "core" {
				problems = append(problems, fmt.Sprintf("rule 1: %s imports %s", p.ImportPath, imp))
			}
		}
	}
	return problems
}

// ruleModuleImports is rule 2: module packages import only core and their own subtree.
func ruleModuleImports(modulePath string, packages []listedPackage) []string {
	var problems []string
	for _, p := range packages {
		rel, _ := relImport(modulePath, p.ImportPath)
		owner, isPlatform := platformOwner(rel)
		if !isPlatform || owner == "core" {
			continue
		}
		own := platformDir + "/" + owner
		for _, imp := range p.allImports() {
			target, internal := relImport(modulePath, imp)
			if !internal {
				continue
			}
			allowed := isUnder(target, own) || (isUnder(target, coreDir) && !strings.Contains(target, "/internal/") && !strings.HasSuffix(target, "/internal"))
			if !allowed {
				problems = append(problems, fmt.Sprintf("rule 2: %s imports %s", p.ImportPath, imp))
			}
		}
	}
	return problems
}

// modulesGoImports returns the import paths declared by modules.go in dir.
func modulesGoImports(dir string) map[string]bool {
	named := map[string]bool{}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(dir, "modules.go"), nil, parser.ImportsOnly)
	if err != nil {
		return named
	}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err == nil {
			named[path] = true
		}
	}
	return named
}

// ruleServerImports is rule 3: cmd/server imports core and the module roots named in modules.go.
func ruleServerImports(modulePath string, packages []listedPackage) []string {
	var problems []string
	for _, p := range packages {
		rel, _ := relImport(modulePath, p.ImportPath)
		if !isUnder(rel, serverDir) {
			continue
		}
		named := modulesGoImports(p.Dir)
		for _, imp := range p.allImports() {
			target, internal := relImport(modulePath, imp)
			if !internal {
				continue
			}
			owner, isPlatform := platformOwner(target)
			isCoreRoot := target == coreDir
			isModuleRoot := isPlatform && owner != "core" && target == platformDir+"/"+owner && named[imp]
			if !isCoreRoot && !isModuleRoot {
				problems = append(problems, fmt.Sprintf("rule 3: %s imports %s", p.ImportPath, imp))
			}
		}
	}
	return problems
}

// ruleCoreSubpackagesNotRoot is rule 5. core/coretest is the one exception: it
// is the test host and runs core.Build.
func ruleCoreSubpackagesNotRoot(modulePath string, packages []listedPackage) []string {
	var problems []string
	root := modulePath + "/" + coreDir
	for _, p := range packages {
		rel, _ := relImport(modulePath, p.ImportPath)
		isTestHost := rel == coreDir+"/coretest"
		if !isUnder(rel, coreDir) || rel == coreDir || isTestHost {
			continue
		}
		for _, imp := range p.allImports() {
			if imp == root {
				problems = append(problems, fmt.Sprintf("rule 5: %s imports the core root", p.ImportPath))
			}
		}
	}
	return problems
}

// ruleModulePathLeaks is rule 4: the module path appears only in go.mod and
// import specs, in an import group of its own.
func ruleModulePathLeaks(modulePath string, packages []listedPackage) []string {
	var problems []string
	for _, p := range packages {
		for _, name := range p.sourceFiles() {
			problems = append(problems, scanGoFile(modulePath, filepath.Join(p.Dir, name))...)
		}
		embedded := append(append([]string{}, p.EmbedFiles...), p.TestEmbedFiles...)
		for _, name := range embedded {
			path := filepath.Join(p.Dir, name)
			data, err := os.ReadFile(path)
			if err == nil && bytes.Contains(data, []byte(modulePath)) {
				problems = append(problems, fmt.Sprintf("rule 4: embedded file %s contains the module path", path))
			}
		}
	}
	return problems
}

func scanGoFile(modulePath, path string) []string {
	src, err := os.ReadFile(path)
	if err != nil {
		return []string{fmt.Sprintf("rule 4: cannot read %s: %v", path, err)}
	}
	var problems []string
	fset := token.NewFileSet()
	file := fset.AddFile(path, fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, src, nil, scanner.ScanComments)

	// importState: 0 outside, 1 after "import", 2 inside "import (", 3 in a single spec.
	state := 0
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		line := fset.Position(pos).Line
		switch {
		case tok == token.IMPORT:
			state = 1
		case state == 1 && tok == token.LPAREN:
			state = 2
		case state == 1:
			state = 3
		case state == 2 && tok == token.RPAREN:
			state = 0
		case state == 3 && tok == token.SEMICOLON:
			state = 0
		}
		inImport := state == 2 || state == 3
		switch {
		case tok == token.STRING && strings.Contains(lit, modulePath) && !inImport:
			problems = append(problems, fmt.Sprintf("rule 4: %s:%d string literal contains the module path", path, line))
		case tok == token.COMMENT && strings.HasPrefix(lit, "//go:generate") && strings.Contains(lit, modulePath):
			problems = append(problems, fmt.Sprintf("rule 4: %s:%d go:generate line contains the module path", path, line))
		}
	}
	problems = append(problems, importGroupProblems(modulePath, path, src)...)
	return problems
}

// importGroupProblems checks that imports of this module sit in groups
// that hold no other import.
func importGroupProblems(modulePath, path string, src []byte) []string {
	var problems []string
	lines := strings.Split(string(src), "\n")
	inBlock := false
	var group []string
	flush := func() {
		var own, other int
		for _, imp := range group {
			if strings.Contains(imp, `"`+modulePath) {
				own++
			} else {
				other++
			}
		}
		if own > 0 && other > 0 {
			problems = append(problems, fmt.Sprintf("rule 4: %s mixes module imports with other imports in one group", path))
		}
		group = nil
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "import (":
			inBlock = true
		case inBlock && trimmed == ")":
			flush()
			inBlock = false
		case inBlock && trimmed == "":
			flush()
		case inBlock && !strings.HasPrefix(trimmed, "//"):
			group = append(group, trimmed)
		}
	}
	return problems
}

// manifestFile is the module manifest each module directory carries.
const manifestFile = "module.manifest.json"

// tableTokenPattern matches a token shaped like a table name: a 2 to 4 letter
// prefix, an underscore and a snake_case name.
var tableTokenPattern = regexp.MustCompile(`\b([a-z]{2,4})_[a-z0-9_]+\b`)

// corePrefix is core's migration ledger prefix; every module may name it.
const corePrefix = "cor"

// moduleRootOf returns the directory of the Go module the listing describes.
func moduleRootOf(packages []listedPackage) string {
	for _, p := range packages {
		if p.Module != nil && p.Module.Dir != "" {
			return p.Module.Dir
		}
	}
	return ""
}

// readTablePrefixes returns, for every non-core module directory under
// internal/platform, the table prefixes its manifest claims. A directory
// without a readable manifest claims none.
func readTablePrefixes(root string) map[string][]string {
	claims := map[string][]string{}
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(platformDir)))
	if err != nil {
		return claims
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "core" {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(platformDir), entry.Name(), manifestFile))
		if readErr != nil {
			continue
		}
		var manifest struct {
			TablePrefixes []string `json:"tablePrefixes"`
		}
		if json.Unmarshal(data, &manifest) == nil {
			claims[entry.Name()] = manifest.TablePrefixes
		}
	}
	return claims
}

// ruleNoForeignTables is rule 6: no Go file of a module (tests included,
// testdata excluded) names a table whose prefix another module claims.
func ruleNoForeignTables(packages []listedPackage) []string {
	root := moduleRootOf(packages)
	claims := readTablePrefixes(root)
	ownerOfPrefix := map[string]string{}
	for dir, prefixes := range claims {
		for _, prefix := range prefixes {
			ownerOfPrefix[prefix] = dir
		}
	}
	var problems []string
	for dir := range claims {
		moduleDir := filepath.Join(root, filepath.FromSlash(platformDir), dir)
		walkErr := filepath.WalkDir(moduleDir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				return nil
			}
			problems = append(problems, foreignTablesIn(root, path, dir, ownerOfPrefix)...)
			return nil
		})
		if walkErr != nil {
			problems = append(problems, fmt.Sprintf("rule 6: cannot scan %s: %v", moduleDir, walkErr))
		}
	}
	return problems
}

// foreignTablesIn reports every token of path whose prefix is claimed by a
// module other than self.
func foreignTablesIn(root, path, self string, ownerOfPrefix map[string]string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{fmt.Sprintf("rule 6: cannot read %s: %v", path, err)}
	}
	rel, relErr := filepath.Rel(root, path)
	if relErr != nil {
		rel = path
	}
	var problems []string
	for i, line := range strings.Split(string(data), "\n") {
		for _, match := range tableTokenPattern.FindAllStringSubmatch(line, -1) {
			prefix := match[1]
			owner, claimed := ownerOfPrefix[prefix]
			if prefix == corePrefix || !claimed || owner == self {
				continue
			}
			problems = append(problems, fmt.Sprintf("rule 6: %s:%d names %s, owned by %s", filepath.ToSlash(rel), i+1, match[0], owner))
		}
	}
	return problems
}

// allProblems applies every rule to the listing.
func allProblems(t *testing.T, packages []listedPackage) []string {
	t.Helper()
	modulePath := modulePathOf(t, packages)
	var problems []string
	problems = append(problems, ruleCoreImportsNoModule(modulePath, packages)...)
	problems = append(problems, ruleModuleImports(modulePath, packages)...)
	problems = append(problems, ruleServerImports(modulePath, packages)...)
	problems = append(problems, ruleModulePathLeaks(modulePath, packages)...)
	problems = append(problems, ruleCoreSubpackagesNotRoot(modulePath, packages)...)
	problems = append(problems, ruleNoForeignTables(packages)...)
	sort.Strings(problems)
	return problems
}

func TestImportBoundaryHolds(t *testing.T) {
	packages := loadPackages(t)
	problems := allProblems(t, packages)
	if len(problems) > 0 {
		t.Fatalf("import boundary violations:\n%s", strings.Join(problems, "\n"))
	}
}

// TestBoundaryRulesCatchViolations runs the rules over a synthetic module, one
// planted violation per rule, and expects exactly that rule to report.
func TestBoundaryRulesCatchViolations(t *testing.T) {
	const mod = "example.com/boundary"
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name: "rule 1 core imports a module",
			files: map[string]string{
				"internal/platform/core/core.go":   "package core\n\nimport _ \"example.com/boundary/internal/platform/other\"\n",
				"internal/platform/other/other.go": "package other\n",
				"cmd/server/main.go":               "package main\n\nfunc main() {}\n",
			},
			want: "rule 1",
		},
		{
			name: "rule 2 module imports another module",
			files: map[string]string{
				"internal/platform/core/core.go": "package core\n",
				"internal/platform/a/a.go":       "package a\n\nimport _ \"example.com/boundary/internal/platform/b\"\n",
				"internal/platform/b/b.go":       "package b\n",
			},
			want: "rule 2",
		},
		{
			name: "rule 2 module imports a core internal package",
			files: map[string]string{
				"internal/platform/core/core.go":         "package core\n",
				"internal/platform/core/internal/x/x.go": "package x\n",
				"internal/platform/a/a.go":               "package a\n\nimport _ \"example.com/boundary/internal/platform/core/internal/x\"\n",
			},
			want: "rule 2",
		},
		{
			name: "rule 3 server imports a module root outside modules.go",
			files: map[string]string{
				"internal/platform/core/core.go": "package core\n",
				"internal/platform/a/a.go":       "package a\n",
				"cmd/server/main.go":             "package main\n\nimport _ \"example.com/boundary/internal/platform/a\"\n\nfunc main() {}\n",
			},
			want: "rule 3",
		},
		{
			name: "rule 3 server imports a module subpackage",
			files: map[string]string{
				"internal/platform/core/core.go": "package core\n",
				"internal/platform/a/sub/sub.go": "package sub\n",
				"cmd/server/main.go":             "package main\n\nimport _ \"example.com/boundary/internal/platform/a/sub\"\n\nfunc main() {}\n",
			},
			want: "rule 3",
		},
		{
			name: "rule 4 string literal",
			files: map[string]string{
				"internal/platform/core/core.go": "package core\n\nvar path = \"example.com/boundary/x\"\n",
			},
			want: "rule 4",
		},
		{
			name: "rule 4 go generate line",
			files: map[string]string{
				"internal/platform/core/core.go": "package core\n\n//go:generate tool example.com/boundary/x\n",
			},
			want: "rule 4",
		},
		{
			name: "rule 4 struct tag",
			files: map[string]string{
				"internal/platform/core/core.go": "package core\n\ntype T struct {\n\tA string `json:\"example.com/boundary\"`\n}\n",
			},
			want: "rule 4",
		},
		{
			name: "rule 4 embedded file",
			files: map[string]string{
				"internal/platform/core/core.go":  "package core\n\nimport _ \"embed\"\n\n//go:embed note.txt\nvar note string\n",
				"internal/platform/core/note.txt": "see example.com/boundary\n",
			},
			want: "rule 4",
		},
		{
			name: "rule 4 own import grouped with another import",
			files: map[string]string{
				"internal/platform/core/core.go":    "package core\n\nimport (\n\t\"fmt\"\n\t\"example.com/boundary/internal/platform/core/sub\"\n)\n\nvar _ = fmt.Sprint\nvar _ = sub.X\n",
				"internal/platform/core/sub/sub.go": "package sub\n\nvar X = 1\n",
			},
			want: "rule 4",
		},
		{
			name: "rule 5 core subpackage imports the root",
			files: map[string]string{
				"internal/platform/core/core.go":    "package core\n",
				"internal/platform/core/sub/sub.go": "package sub\n\nimport _ \"example.com/boundary/internal/platform/core\"\n",
			},
			want: "rule 5",
		},
		{
			name: "rule 5 the exemption does not cover a coretest subpackage",
			files: map[string]string{
				"internal/platform/core/core.go":             "package core\n",
				"internal/platform/core/coretest/sub/sub.go": "package sub\n\nimport _ \"example.com/boundary/internal/platform/core\"\n",
			},
			want: "rule 5",
		},
		{
			name: "rule 5 the exemption does not cover a coretest-prefixed sibling",
			files: map[string]string{
				"internal/platform/core/core.go":        "package core\n",
				"internal/platform/core/coretestx/x.go": "package coretestx\n\nimport _ \"example.com/boundary/internal/platform/core\"\n",
			},
			want: "rule 5",
		},
		{
			name: "rule 2 tenancy imports identity",
			files: map[string]string{
				"internal/platform/core/core.go":         "package core\n",
				"internal/platform/identity/identity.go": "package identity\n",
				"internal/platform/tenancy/tenancy.go":   "package tenancy\n\nimport _ \"example.com/boundary/internal/platform/identity\"\n",
			},
			want: "rule 2",
		},
		{
			name: "rule 2 apikeys imports tenancy",
			files: map[string]string{
				"internal/platform/core/core.go":       "package core\n",
				"internal/platform/tenancy/tenancy.go": "package tenancy\n",
				"internal/platform/apikeys/apikeys.go": "package apikeys\n\nimport _ \"example.com/boundary/internal/platform/tenancy\"\n",
			},
			want: "rule 2",
		},
		{
			name: "rule 6 tenancy file names an identity table",
			files: map[string]string{
				"internal/platform/core/core.go":                  "package core\n",
				"internal/platform/identity/identity.go":          "package identity\n",
				"internal/platform/identity/module.manifest.json": "{\"tablePrefixes\": [\"iam\"]}\n",
				"internal/platform/tenancy/store.go":              "package tenancy\n\nvar query = \"SELECT id FROM iam_accounts\"\n",
				"internal/platform/tenancy/module.manifest.json":  "{\"tablePrefixes\": [\"ten\"]}\n",
			},
			want: "rule 6: internal/platform/tenancy/store.go:3 names iam_accounts, owned by identity",
		},
		{
			name: "rule 6 test files are scanned too",
			files: map[string]string{
				"internal/platform/core/core.go":                 "package core\n",
				"internal/platform/tenancy/tenancy.go":           "package tenancy\n",
				"internal/platform/tenancy/module.manifest.json": "{\"tablePrefixes\": [\"ten\"]}\n",
				"internal/platform/apikeys/apikeys.go":           "package apikeys\n",
				"internal/platform/apikeys/keys_test.go":         "package apikeys\n\nvar fixture = \"INSERT INTO ten_tenants (id) VALUES (1)\"\n",
				"internal/platform/apikeys/module.manifest.json": "{\"tablePrefixes\": [\"apk\"]}\n",
			},
			want: "rule 6: internal/platform/apikeys/keys_test.go:3 names ten_tenants, owned by tenancy",
		},
		{
			name: "rule 6 allows own tables, core ledgers, unclaimed prefixes and testdata",
			files: map[string]string{
				"internal/platform/core/core.go":                  "package core\n",
				"internal/platform/identity/identity.go":          "package identity\n",
				"internal/platform/identity/module.manifest.json": "{\"tablePrefixes\": [\"iam\"]}\n",
				"internal/platform/tenancy/store.go":              "package tenancy\n\nvar a = \"ten_tenants\"\nvar b = \"cor_migrations_tenancy\"\nvar c = \"ak_live_x\"\n",
				"internal/platform/tenancy/module.manifest.json":  "{\"tablePrefixes\": [\"ten\"]}\n",
				"internal/platform/tenancy/testdata/x/x.go":       "package x\n\nvar d = \"iam_accounts\"\n",
			},
			want: "",
		},
		{
			name: "clean module reports nothing",
			files: map[string]string{
				"internal/platform/core/core.go":    "package core\n",
				"internal/platform/core/sub/sub.go": "package sub\n",
				"internal/platform/a/a.go":          "package a\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/boundary/internal/platform/core\"\n)\n\nvar _ = fmt.Sprint\nvar _ core.T\n",
				"cmd/server/main.go":                "package main\n\nimport _ \"example.com/boundary/internal/platform/core\"\n\nfunc main() {}\n",
				"cmd/server/modules.go":             "package main\n\nimport _ \"example.com/boundary/internal/platform/a\"\n",
			},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{"go.mod": "module " + mod + "\n\ngo 1.26\n"}
			for name, content := range tc.files {
				files[name] = content
			}
			if tc.name == "clean module reports nothing" {
				files["internal/platform/core/core.go"] = "package core\n\ntype T struct{}\n"
			}
			for name, content := range files {
				path := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatalf("write: %v", err)
				}
			}
			packages := listPackagesIn(t, root)
			problems := allProblems(t, packages)
			if tc.want == "" {
				if len(problems) > 0 {
					t.Fatalf("clean module reported: %v", problems)
				}
				return
			}
			if len(problems) == 0 {
				t.Fatalf("want a %q problem, got none", tc.want)
			}
			for _, problem := range problems {
				if !strings.HasPrefix(problem, tc.want) {
					t.Fatalf("want only %q problems, got %v", tc.want, problems)
				}
			}
		})
	}
}
