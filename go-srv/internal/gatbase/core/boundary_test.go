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

// loadPackages runs `go list -json ./...` from the go-srv module root.
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
	gatbaseDir = "internal/gatbase"
	coreDir    = "internal/gatbase/core"
	serverDir  = "cmd/server"
)

// gatbaseOwner returns the first path element below internal/gatbase ("core",
// "coreauth", ...) or false when rel is not under it.
func gatbaseOwner(rel string) (string, bool) {
	below, found := strings.CutPrefix(rel, gatbaseDir+"/")
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
			owner, isGatbase := gatbaseOwner(target)
			if internal && isGatbase && owner != "core" {
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
		owner, isGatbase := gatbaseOwner(rel)
		if !isGatbase || owner == "core" {
			continue
		}
		own := gatbaseDir + "/" + owner
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
			owner, isGatbase := gatbaseOwner(target)
			isCoreRoot := target == coreDir
			isModuleRoot := isGatbase && owner != "core" && target == gatbaseDir+"/"+owner && named[imp]
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
				"internal/gatbase/core/core.go":   "package core\n\nimport _ \"example.com/boundary/internal/gatbase/other\"\n",
				"internal/gatbase/other/other.go": "package other\n",
				"cmd/server/main.go":              "package main\n\nfunc main() {}\n",
			},
			want: "rule 1",
		},
		{
			name: "rule 2 module imports another module",
			files: map[string]string{
				"internal/gatbase/core/core.go": "package core\n",
				"internal/gatbase/a/a.go":       "package a\n\nimport _ \"example.com/boundary/internal/gatbase/b\"\n",
				"internal/gatbase/b/b.go":       "package b\n",
			},
			want: "rule 2",
		},
		{
			name: "rule 2 module imports a core internal package",
			files: map[string]string{
				"internal/gatbase/core/core.go":         "package core\n",
				"internal/gatbase/core/internal/x/x.go": "package x\n",
				"internal/gatbase/a/a.go":               "package a\n\nimport _ \"example.com/boundary/internal/gatbase/core/internal/x\"\n",
			},
			want: "rule 2",
		},
		{
			name: "rule 3 server imports a module root outside modules.go",
			files: map[string]string{
				"internal/gatbase/core/core.go": "package core\n",
				"internal/gatbase/a/a.go":       "package a\n",
				"cmd/server/main.go":            "package main\n\nimport _ \"example.com/boundary/internal/gatbase/a\"\n\nfunc main() {}\n",
			},
			want: "rule 3",
		},
		{
			name: "rule 3 server imports a module subpackage",
			files: map[string]string{
				"internal/gatbase/core/core.go": "package core\n",
				"internal/gatbase/a/sub/sub.go": "package sub\n",
				"cmd/server/main.go":            "package main\n\nimport _ \"example.com/boundary/internal/gatbase/a/sub\"\n\nfunc main() {}\n",
			},
			want: "rule 3",
		},
		{
			name: "rule 4 string literal",
			files: map[string]string{
				"internal/gatbase/core/core.go": "package core\n\nvar path = \"example.com/boundary/x\"\n",
			},
			want: "rule 4",
		},
		{
			name: "rule 4 go generate line",
			files: map[string]string{
				"internal/gatbase/core/core.go": "package core\n\n//go:generate tool example.com/boundary/x\n",
			},
			want: "rule 4",
		},
		{
			name: "rule 4 struct tag",
			files: map[string]string{
				"internal/gatbase/core/core.go": "package core\n\ntype T struct {\n\tA string `json:\"example.com/boundary\"`\n}\n",
			},
			want: "rule 4",
		},
		{
			name: "rule 4 embedded file",
			files: map[string]string{
				"internal/gatbase/core/core.go":  "package core\n\nimport _ \"embed\"\n\n//go:embed note.txt\nvar note string\n",
				"internal/gatbase/core/note.txt": "see example.com/boundary\n",
			},
			want: "rule 4",
		},
		{
			name: "rule 4 own import grouped with another import",
			files: map[string]string{
				"internal/gatbase/core/core.go":    "package core\n\nimport (\n\t\"fmt\"\n\t\"example.com/boundary/internal/gatbase/core/sub\"\n)\n\nvar _ = fmt.Sprint\nvar _ = sub.X\n",
				"internal/gatbase/core/sub/sub.go": "package sub\n\nvar X = 1\n",
			},
			want: "rule 4",
		},
		{
			name: "rule 5 core subpackage imports the root",
			files: map[string]string{
				"internal/gatbase/core/core.go":    "package core\n",
				"internal/gatbase/core/sub/sub.go": "package sub\n\nimport _ \"example.com/boundary/internal/gatbase/core\"\n",
			},
			want: "rule 5",
		},
		{
			name: "clean module reports nothing",
			files: map[string]string{
				"internal/gatbase/core/core.go":    "package core\n",
				"internal/gatbase/core/sub/sub.go": "package sub\n",
				"internal/gatbase/a/a.go":          "package a\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/boundary/internal/gatbase/core\"\n)\n\nvar _ = fmt.Sprint\nvar _ core.T\n",
				"cmd/server/main.go":               "package main\n\nimport _ \"example.com/boundary/internal/gatbase/core\"\n\nfunc main() {}\n",
				"cmd/server/modules.go":            "package main\n\nimport _ \"example.com/boundary/internal/gatbase/a\"\n",
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
				files["internal/gatbase/core/core.go"] = "package core\n\ntype T struct{}\n"
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
