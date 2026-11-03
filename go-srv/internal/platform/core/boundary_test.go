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
	// platformDirName is the one directory under internal/ that is not a product
	// vertical.
	platformDirName = "platform"
	internalDir     = "internal"
	platformDir     = internalDir + "/" + platformDirName
	coreDir         = platformDir + "/core"
	// testkitDirName holds test support only: the module test host testcore and
	// the database helper testpg. It is neither a module nor a product vertical.
	testkitDirName = "testkit"
	testkitDir     = internalDir + "/" + testkitDirName
	serverDir      = "cmd/server"
	// modulesGoFile is the generated module list of the server; every other
	// file of the package may import the module roots it names.
	modulesGoFile = "modules.go"
	// productFile is the project-owned registration seam of the server.
	productFile = "product.go"
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

// productOwner returns the vertical of a product package: the first path
// element below internal/ when it is not the platform tree nor the test kit, or
// false.
func productOwner(rel string) (string, bool) {
	below, found := strings.CutPrefix(rel, internalDir+"/")
	if !found {
		return "", false
	}
	owner, _, _ := strings.Cut(below, "/")
	if owner == "" || owner == platformDirName || owner == testkitDirName {
		return "", false
	}
	return owner, true
}

// moduleDirOf returns the directory a package belongs to: the root of its
// platform module or of its product vertical, "" for everything else, core
// included.
func moduleDirOf(rel string) string {
	if owner, isPlatform := platformOwner(rel); isPlatform {
		if owner == "core" {
			return ""
		}
		return platformDir + "/" + owner
	}
	if owner, isProduct := productOwner(rel); isProduct {
		return internalDir + "/" + owner
	}
	return ""
}

func isUnder(rel, dir string) bool {
	return rel == dir || strings.HasPrefix(rel, dir+"/")
}

// ruleCoreImportsNoModule is rule 1: a core package imports nothing under
// internal/ outside the core tree. The test kit is not a dependency of the
// code: only a _test.go file may import it, which rule 7 enforces.
func ruleCoreImportsNoModule(modulePath string, packages []listedPackage) []string {
	var problems []string
	for _, p := range packages {
		rel, _ := relImport(modulePath, p.ImportPath)
		if !isUnder(rel, coreDir) {
			continue
		}
		for _, imp := range p.allImports() {
			target, internal := relImport(modulePath, imp)
			if internal && isUnder(target, internalDir) && !isUnder(target, coreDir) && !isUnder(target, testkitDir) {
				problems = append(problems, fmt.Sprintf("rule 1: %s imports %s", p.ImportPath, imp))
			}
		}
	}
	return problems
}

// ruleModuleImports is rule 2: a platform module package imports only core and
// its own subtree; a product package imports only non-internal core packages
// and its own subtree. A _test.go file may import the test kit; rule 7 keeps it
// out of every other file.
func ruleModuleImports(modulePath string, packages []listedPackage) []string {
	var problems []string
	for _, p := range packages {
		rel, _ := relImport(modulePath, p.ImportPath)
		dir := moduleDirOf(rel)
		if dir == "" {
			continue
		}
		for _, imp := range p.allImports() {
			target, internal := relImport(modulePath, imp)
			if !internal {
				continue
			}
			if isUnder(target, testkitDir) {
				continue
			}
			if !isUnder(target, dir) && !isPublicCore(target) {
				problems = append(problems, fmt.Sprintf("rule 2: %s imports %s", p.ImportPath, imp))
			}
		}
	}
	return problems
}

// isPublicCore reports whether target is a core package another module may
// import: core itself or one of its packages outside core/internal.
func isPublicCore(target string) bool {
	isCore := isUnder(target, coreDir)
	hasInternal := strings.Contains(target, "/internal/") || strings.HasSuffix(target, "/internal")
	return isCore && !hasInternal
}

// goFileImports returns the import paths one Go file in dir declares.
func goFileImports(dir, name string) map[string]bool {
	named := map[string]bool{}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
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

// modulesGoImports returns the import paths declared by modules.go in dir.
func modulesGoImports(dir string) map[string]bool {
	return goFileImports(dir, modulesGoFile)
}

// productGoImports returns the import paths declared by product.go in dir.
func productGoImports(dir string) map[string]bool {
	return goFileImports(dir, productFile)
}

// ruleServerImports is rule 3, per file: product.go imports the core root and
// product roots only; every other file of cmd/server imports the core root and
// the platform module roots modules.go names.
func ruleServerImports(modulePath string, packages []listedPackage) []string {
	var problems []string
	for _, p := range packages {
		rel, _ := relImport(modulePath, p.ImportPath)
		if !isUnder(rel, serverDir) {
			continue
		}
		named := modulesGoImports(p.Dir)
		for _, name := range p.sourceFiles() {
			imports := goFileImports(p.Dir, name)
			if name == productFile {
				imports = productGoImports(p.Dir)
			}
			for _, imp := range sortedKeys(imports) {
				if !serverImportAllowed(modulePath, imp, name, named) {
					problems = append(problems, fmt.Sprintf("rule 3: %s/%s imports %s", p.ImportPath, name, imp))
				}
			}
		}
	}
	return problems
}

// serverImportAllowed reports whether imp may appear in file name of a
// cmd/server package.
func serverImportAllowed(modulePath, imp, name string, named map[string]bool) bool {
	target, internal := relImport(modulePath, imp)
	if !internal {
		return true
	}
	if target == coreDir {
		return true
	}
	if name == productFile {
		owner, isProduct := productOwner(target)
		return isProduct && target == internalDir+"/"+owner
	}
	owner, isPlatform := platformOwner(target)
	return isPlatform && owner != "core" && target == platformDir+"/"+owner && named[imp]
}

// sortedKeys returns the keys of a set, sorted bytewise.
func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// ruleCoreSubpackagesNotRoot is rule 5: a package under core/ other than the
// root imports the core root from nowhere, so core.Build runs only through the
// root package a module receives in its Host. The test host that runs
// core.Build lives outside the core tree (internal/testkit), so it needs no
// exception here.
func ruleCoreSubpackagesNotRoot(modulePath string, packages []listedPackage) []string {
	var problems []string
	root := modulePath + "/" + coreDir
	for _, p := range packages {
		rel, _ := relImport(modulePath, p.ImportPath)
		if !isUnder(rel, coreDir) || rel == coreDir {
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

// manifestFile is the module manifest each platform module directory carries.
// A product vertical carries none: it is never upgraded from the catalog, so
// its table prefix comes from its migration filenames alone.
const manifestFile = "module.manifest.json"

// migrationsDir holds the migrations of a platform module and of a product
// vertical. A vertical's file names declare its table prefix.
const migrationsDir = "migrations"

// tableTokenPattern matches a token shaped like a table name: a 2 to 4 letter
// prefix, an underscore and a snake_case name.
var tableTokenPattern = regexp.MustCompile(`\b([a-z]{2,4})_[a-z0-9_]+\b`)

// tablePrefixPattern is the shape of a table prefix.
var tablePrefixPattern = regexp.MustCompile(`^[a-z]{2,4}$`)

// migrationTimestampPattern matches a migration file name without its .sql
// suffix and captures everything after the 14-digit timestamp.
var migrationTimestampPattern = regexp.MustCompile(`^\d{14}_(.+)$`)

// corePrefix is core's migration ledger prefix; every module may name it.
const corePrefix = "cor"

// reservedTablePrefixes belong to areas outside this catalog. The list is a
// twin of tooling/gates/internal/migrationlint/reserved-prefixes.json: this
// test cannot import it (a separate Go module, and a generated project does not
// ship tooling/), so Code Path Parity in the root AGENTS.md keeps them equal.
var reservedTablePrefixes = []string{"aud", "pln", "mtr", "bil", "mkc", "jbr", "stg"}

// isReservedPrefix reports whether prefix belongs to a reserved area.
func isReservedPrefix(prefix string) bool {
	for _, reserved := range reservedTablePrefixes {
		if prefix == reserved {
			return true
		}
	}
	return false
}

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
// internal/platform, the table prefixes its manifest claims, keyed relative to
// internal/ ("platform/<module>"). A directory without a readable manifest
// claims none.
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
			claims[platformDirName+"/"+entry.Name()] = manifest.TablePrefixes
		}
	}
	return claims
}

// productDirs returns the names of the product verticals under internal/:
// every directory there that is neither the platform tree nor the test kit,
// sorted.
func productDirs(root string) []string {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(internalDir)))
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == platformDirName || entry.Name() == testkitDirName {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

// tableClaims returns every table prefix claimed under internal/, keyed relative
// to internal/ ("platform/<module>" or "<vertical>"), plus one problem for
// every claim the rule refuses.
func tableClaims(root string) (map[string][]string, []string) {
	claims := readTablePrefixes(root)
	products, problems := productPrefixes(root, claims)
	for vertical, prefixes := range products {
		claims[vertical] = prefixes
	}
	return claims, problems
}

// productPrefixes returns the table prefix each product vertical declares
// through its migration filenames, keyed by the vertical, plus one problem per
// refused claim: a malformed migration file name, migrations that yield more
// than one prefix, a prefix that is not 2 to 4 lowercase letters, a reserved
// prefix, core's prefix, a prefix a platform module already claims, a prefix
// another vertical already claims, and a prefix the Go files of another
// directory already use as an ordinary identifier. A vertical without migrations
// claims nothing.
func productPrefixes(root string, platformClaims map[string][]string) (map[string][]string, []string) {
	ownerOfPrefix := map[string]string{}
	for dir, prefixes := range platformClaims {
		for _, prefix := range prefixes {
			ownerOfPrefix[prefix] = dir
		}
	}
	verticals := productDirs(root)
	identifiers, problems := identifierOwners(root, platformClaims, verticals)
	claims := map[string][]string{}
	for _, vertical := range verticals {
		relDir := internalDir + "/" + vertical + "/" + migrationsDir
		files := migrationFiles(filepath.Join(root, filepath.FromSlash(relDir)))
		if len(files) == 0 {
			continue
		}
		prefixes, found := distinctMigrationPrefixes(relDir, files)
		problems = append(problems, found...)
		if len(prefixes) == 0 {
			continue
		}
		if len(prefixes) > 1 {
			problems = append(problems, fmt.Sprintf("rule 6: %s: claims %d table prefixes (%s)", relDir, len(prefixes), strings.Join(prefixes, ", ")))
			continue
		}
		prefix := prefixes[0]
		if !tablePrefixPattern.MatchString(prefix) {
			problems = append(problems, fmt.Sprintf("rule 6: %s: prefix %q must be 2 to 4 lowercase letters", relDir, prefix))
			continue
		}
		refusal := refusedPrefix(relDir, prefix, ownerOfPrefix, usedOutside(vertical, identifiers))
		if refusal != "" {
			problems = append(problems, refusal)
			continue
		}
		claims[vertical] = prefixes
		ownerOfPrefix[prefix] = vertical
	}
	return claims, problems
}

// identifierOwners returns, per claim key, every table prefix the Go files of
// that directory already contain as a word-start token, testdata excluded. Rule
// 6 matches tokens rather than SQL, so a vertical claiming one of them would
// report identifiers such as app_session, max_conns or pay_status as its own
// tables in directories it never touched.
func identifierOwners(root string, platformClaims map[string][]string, verticals []string) (map[string]map[string]bool, []string) {
	keys := make([]string, 0, len(platformClaims)+len(verticals))
	for key := range platformClaims {
		keys = append(keys, key)
	}
	keys = append(keys, verticals...)
	sort.Strings(keys)
	owners := map[string]map[string]bool{}
	var problems []string
	for _, key := range keys {
		dir := filepath.Join(root, filepath.FromSlash(internalDir), filepath.FromSlash(key))
		identifiers, err := dirIdentifiers(dir)
		if err != nil {
			problems = append(problems, fmt.Sprintf("rule 6: cannot scan %s: %v", filepath.ToSlash(internalDir+"/"+key), err))
			continue
		}
		owners[key] = identifiers
	}
	return owners, problems
}

// dirIdentifiers returns every table prefix the Go files under dir contain as a
// word-start token, testdata excluded.
func dirIdentifiers(dir string) (map[string]bool, error) {
	identifiers := map[string]bool{}
	walkErr := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == "testdata" {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, line := range strings.Split(string(data), "\n") {
			for _, match := range tableTokenPattern.FindAllStringSubmatch(line, -1) {
				identifiers[match[1]] = true
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return identifiers, nil
}

// usedOutside returns every prefix the directories other than self contain as an
// identifier. The claimant's own directory is excluded: it holds its own table
// names, which are the point of the claim.
func usedOutside(self string, owners map[string]map[string]bool) map[string]bool {
	used := map[string]bool{}
	for key, identifiers := range owners {
		if key == self {
			continue
		}
		for prefix := range identifiers {
			used[prefix] = true
		}
	}
	return used
}

// migrationFiles returns the names of the .sql files directly in dir, sorted.
// A missing directory yields none.
func migrationFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

// distinctMigrationPrefixes returns the distinct table prefixes the migration
// file names declare, sorted, plus one problem per file that declares none.
func distinctMigrationPrefixes(relDir string, files []string) ([]string, []string) {
	var problems []string
	seen := map[string]bool{}
	var prefixes []string
	for _, file := range files {
		prefix, hasPrefix := migrationPrefix(file)
		if !hasPrefix {
			problems = append(problems, fmt.Sprintf("rule 6: %s/%s: name must be <timestamp>_<prefix>_<name>.sql", relDir, file))
			continue
		}
		if seen[prefix] {
			continue
		}
		seen[prefix] = true
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	return prefixes, problems
}

// migrationPrefix derives the table prefix a migration file name declares: the
// token after the leading 14-digit timestamp.
func migrationPrefix(name string) (string, bool) {
	base, hasSQLSuffix := strings.CutSuffix(name, ".sql")
	if !hasSQLSuffix {
		return "", false
	}
	match := migrationTimestampPattern.FindStringSubmatch(base)
	if match == nil {
		return "", false
	}
	prefix, _, _ := strings.Cut(match[1], "_")
	return prefix, prefix != ""
}

// refusedPrefix returns the problem that makes prefix unusable as the single
// claim of the vertical at relDir, or "" when the claim is allowed.
func refusedPrefix(relDir, prefix string, ownerOfPrefix map[string]string, identifiers map[string]bool) string {
	if isReservedPrefix(prefix) {
		return fmt.Sprintf("rule 6: %s: prefix %q is reserved", relDir, prefix)
	}
	if prefix == corePrefix {
		return fmt.Sprintf("rule 6: %s: prefix %q belongs to core", relDir, prefix)
	}
	owner, taken := ownerOfPrefix[prefix]
	if taken {
		return fmt.Sprintf("rule 6: %s: prefix %q is already claimed by %s", relDir, prefix, owner)
	}
	if identifiers[prefix] {
		return fmt.Sprintf("rule 6: %s: prefix %q already appears as an identifier outside this vertical, choose another prefix", relDir, prefix)
	}
	return ""
}

// ruleNoForeignTables is rule 6: no Go file under internal/ (tests included,
// testdata excluded) names a table whose prefix another module or product
// vertical claims.
//
// A platform module claims the tablePrefixes of its module.manifest.json; a
// product vertical claims the one prefix its migration file names yield, and
// carries no manifest because it is never upgraded from the catalog. Every
// product vertical is scanned whether or not it claims a prefix, so a vertical
// that declares no migrations still cannot name a table the platform owns. A
// prefix another directory already uses as an ordinary identifier is refused:
// the token matcher cannot tell app_session, max_conns or pay_status from a
// table name.
func ruleNoForeignTables(packages []listedPackage) []string {
	root := moduleRootOf(packages)
	claims, problems := tableClaims(root)
	ownerOfPrefix := map[string]string{}
	for dir, prefixes := range claims {
		for _, prefix := range prefixes {
			ownerOfPrefix[prefix] = dir
		}
	}
	dirs := map[string]bool{}
	for dir := range claims {
		dirs[dir] = true
	}
	for _, vertical := range productDirs(root) {
		dirs[vertical] = true
	}
	for _, dir := range sortedKeys(dirs) {
		problems = append(problems, foreignTablesInDir(root, dir, ownerOfPrefix)...)
	}
	sort.Strings(problems)
	return problems
}

// foreignTablesInDir returns every problem in the Go files of the directory at key,
// relative to internal/.
func foreignTablesInDir(root, key string, ownerOfPrefix map[string]string) []string {
	dir := filepath.Join(root, filepath.FromSlash(internalDir), filepath.FromSlash(key))
	var problems []string
	walkErr := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == "testdata" {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		problems = append(problems, foreignTablesIn(root, path, key, ownerOfPrefix)...)
		return nil
	})
	if walkErr != nil {
		problems = append(problems, fmt.Sprintf("rule 6: cannot scan %s: %v", dir, walkErr))
	}
	return problems
}

// foreignTablesIn reports every token of path whose prefix is claimed by a
// module or vertical other than self, the claim key of the directory holding it.
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

// ruleProductionImportsNoTestkit is rule 7: a file of the package imports
// nothing under internal/testkit. That tree holds test support only (the module
// test host testcore and the database helper testpg), so a _test.go file may
// import it and the package's other files may not: production code never
// reaches a test helper. A package inside the tree is test support itself and
// may import its siblings.
func ruleProductionImportsNoTestkit(modulePath string, packages []listedPackage) []string {
	var problems []string
	for _, p := range packages {
		rel, _ := relImport(modulePath, p.ImportPath)
		if isUnder(rel, testkitDir) {
			continue
		}
		for _, imp := range p.Imports {
			target, internal := relImport(modulePath, imp)
			if internal && isUnder(target, testkitDir) {
				problems = append(problems, fmt.Sprintf("rule 7: %s imports %s", p.ImportPath, imp))
			}
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
	problems = append(problems, ruleProductionImportsNoTestkit(modulePath, packages)...)
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
			name: "rule 1 core imports a product vertical",
			files: map[string]string{
				"internal/platform/core/core.go": "package core\n\nimport _ \"example.com/boundary/internal/dispatch\"\n",
				"internal/dispatch/dispatch.go":  "package dispatch\n",
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
			name: "rule 2 product vertical imports a platform module",
			files: map[string]string{
				"internal/platform/core/core.go":             "package core\n",
				"internal/platform/parameters/parameters.go": "package parameters\n",
				"internal/dispatch/dispatch.go":              "package dispatch\n\nimport _ \"example.com/boundary/internal/platform/parameters\"\n",
			},
			want: "rule 2",
		},
		{
			name: "rule 2 product vertical imports another vertical",
			files: map[string]string{
				"internal/platform/core/core.go": "package core\n",
				"internal/billing/billing.go":    "package billing\n",
				"internal/dispatch/dispatch.go":  "package dispatch\n\nimport _ \"example.com/boundary/internal/billing\"\n",
			},
			want: "rule 2",
		},
		{
			name: "rule 2 product vertical imports a core internal package",
			files: map[string]string{
				"internal/platform/core/core.go":         "package core\n",
				"internal/platform/core/internal/x/x.go": "package x\n",
				"internal/dispatch/dispatch.go":          "package dispatch\n\nimport _ \"example.com/boundary/internal/platform/core/internal/x\"\n",
			},
			want: "rule 2",
		},
		{
			name: "rule 2 product vertical imports the core root",
			files: map[string]string{
				"internal/platform/core/core.go": "package core\n",
				"internal/dispatch/dispatch.go":  "package dispatch\n\nimport _ \"example.com/boundary/internal/platform/core\"\n",
			},
			want: "",
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
			name: "rule 3 main.go imports a product vertical",
			files: map[string]string{
				"internal/platform/core/core.go": "package core\n",
				"internal/dispatch/dispatch.go":  "package dispatch\n",
				"cmd/server/main.go":             "package main\n\nimport _ \"example.com/boundary/internal/dispatch\"\n\nfunc main() {}\n",
			},
			want: "rule 3",
		},
		{
			name: "rule 3 product.go imports a vertical subpackage",
			files: map[string]string{
				"internal/platform/core/core.go": "package core\n",
				"internal/dispatch/dispatch.go":  "package dispatch\n",
				"internal/dispatch/sub/sub.go":   "package sub\n",
				"cmd/server/product.go":          "package main\n\nimport _ \"example.com/boundary/internal/dispatch/sub\"\n",
			},
			want: "rule 3",
		},
		{
			name: "rule 3 product.go imports a platform module",
			files: map[string]string{
				"internal/platform/core/core.go":             "package core\n",
				"internal/platform/parameters/parameters.go": "package parameters\n",
				"cmd/server/modules.go":                      "package main\n\nimport _ \"example.com/boundary/internal/platform/parameters\"\n",
				"cmd/server/product.go":                      "package main\n\nimport _ \"example.com/boundary/internal/platform/parameters\"\n",
			},
			want: "rule 3",
		},
		{
			name: "rule 3 product.go registers a vertical root",
			files: map[string]string{
				"internal/platform/core/core.go": "package core\n",
				"internal/dispatch/dispatch.go":  "package dispatch\n",
				"cmd/server/modules.go":          "package main\n\nimport _ \"example.com/boundary/internal/platform/core\"\n",
				"cmd/server/product.go":          "package main\n\nimport _ \"example.com/boundary/internal/dispatch\"\n",
				"cmd/server/main.go":             "package main\n\nimport _ \"example.com/boundary/internal/platform/core\"\n\nfunc main() {}\n",
			},
			want: "",
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
			name: "rule 5 a nested core subpackage imports the root",
			files: map[string]string{
				"internal/platform/core/core.go":         "package core\n",
				"internal/platform/core/sub/nested/x.go": "package nested\n\nimport _ \"example.com/boundary/internal/platform/core\"\n",
			},
			want: "rule 5",
		},
		{
			name: "rule 7 a production file imports the test kit",
			files: map[string]string{
				"internal/platform/core/core.go":        "package core\n",
				"internal/testkit/testcore/testcore.go": "package testcore\n",
				"cmd/worker/main.go":                    "package main\n\nimport _ \"example.com/boundary/internal/testkit/testcore\"\n\nfunc main() {}\n",
			},
			want: "rule 7",
		},
		{
			name: "rule 7 a test file imports the test kit",
			files: map[string]string{
				"internal/platform/a/a.go":              "package a\n",
				"internal/platform/a/a_test.go":         "package a\n\nimport _ \"example.com/boundary/internal/testkit/testcore\"\n",
				"internal/testkit/testcore/testcore.go": "package testcore\n",
			},
			want: "",
		},
		{
			name: "rule 1 and rule 2 allow a test file that imports the test kit",
			files: map[string]string{
				"internal/platform/core/core.go":        "package core\n",
				"internal/platform/core/core_test.go":   "package core_test\n\nimport _ \"example.com/boundary/internal/testkit/testcore\"\n",
				"internal/platform/a/a.go":              "package a\n",
				"internal/platform/a/helpers_test.go":   "package a_test\n\nimport _ \"example.com/boundary/internal/testkit/testcore\"\n",
				"internal/testkit/testcore/testcore.go": "package testcore\n",
			},
			want: "",
		},
		{
			name: "rule 7 a test kit package imports its sibling",
			files: map[string]string{
				"internal/platform/core/core.go":        "package core\n",
				"internal/testkit/testpg/testpg.go":     "package testpg\n",
				"internal/testkit/testcore/testcore.go": "package testcore\n\nimport _ \"example.com/boundary/internal/testkit/testpg\"\n",
			},
			want: "",
		},
		{
			name: "rule 7 a test kit package imports a module",
			files: map[string]string{
				"internal/platform/core/core.go":         "package core\n",
				"internal/platform/identity/identity.go": "package identity\n",
				"internal/testkit/testcore/testcore.go":  "package testcore\n\nimport _ \"example.com/boundary/internal/platform/identity\"\n",
			},
			want: "",
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
			want: "rule 6: internal/platform/tenancy/store.go:3 names iam_accounts, owned by platform/identity",
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
			want: "rule 6: internal/platform/apikeys/keys_test.go:3 names ten_tenants, owned by platform/tenancy",
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
			name: "rule 6 a vertical claims its own prefix and names a platform table",
			files: map[string]string{
				"internal/platform/core/core.go":                               "package core\n",
				"internal/platform/tenancy/tenancy.go":                         "package tenancy\n",
				"internal/platform/tenancy/module.manifest.json":               "{\"tablePrefixes\": [\"ten\"]}\n",
				"internal/pay/migrations/20260101000000_pay_create_orders.sql": "",
				"internal/pay/store.go":                                        "package pay\n\nvar query = \"SELECT id FROM ten_tenants\"\n",
			},
			want: "rule 6: internal/pay/store.go:3 names ten_tenants, owned by platform/tenancy",
		},
		{
			name: "rule 6 a vertical with no migrations is scanned too",
			files: map[string]string{
				"internal/platform/core/core.go":                 "package core\n",
				"internal/platform/tenancy/tenancy.go":           "package tenancy\n",
				"internal/platform/tenancy/module.manifest.json": "{\"tablePrefixes\": [\"ten\"]}\n",
				"internal/pay/store.go":                          "package pay\n\nvar query = \"SELECT id FROM ten_tenants\"\n",
			},
			want: "rule 6: internal/pay/store.go:3 names ten_tenants, owned by platform/tenancy",
		},
		{
			name: "rule 6 two verticals claim one prefix",
			files: map[string]string{
				"internal/platform/core/core.go":                                     "package core\n",
				"internal/billing/migrations/20260101000000_shp_create_invoices.sql": "",
				"internal/dispatch/migrations/20260101000000_shp_create_routes.sql":  "",
			},
			want: "rule 6: internal/dispatch/migrations: prefix \"shp\" is already claimed by billing",
		},
		{
			name: "rule 6 a vertical claims two prefixes",
			files: map[string]string{
				"internal/platform/core/core.go":                               "package core\n",
				"internal/pay/migrations/20260101000000_pay_create_orders.sql": "",
				"internal/pay/migrations/20260101000100_ord_create_items.sql":  "",
			},
			want: "rule 6: internal/pay/migrations: claims 2 table prefixes (ord, pay)",
		},
		{
			name: "rule 6 a vertical claims a reserved prefix",
			files: map[string]string{
				"internal/platform/core/core.go":                              "package core\n",
				"internal/audit/migrations/20260101000000_aud_create_log.sql": "",
			},
			want: "rule 6: internal/audit/migrations: prefix \"aud\" is reserved",
		},
		{
			name: "rule 6 a vertical claims core's prefix",
			files: map[string]string{
				"internal/platform/core/core.go":                               "package core\n",
				"internal/pay/migrations/20260101000000_cor_create_orders.sql": "",
			},
			want: "rule 6: internal/pay/migrations: prefix \"cor\" belongs to core",
		},
		{
			name: "rule 6 a vertical claims a platform prefix",
			files: map[string]string{
				"internal/platform/core/core.go":                               "package core\n",
				"internal/platform/tenancy/tenancy.go":                         "package tenancy\n",
				"internal/platform/tenancy/module.manifest.json":               "{\"tablePrefixes\": [\"ten\"]}\n",
				"internal/pay/migrations/20260101000000_ten_create_orders.sql": "",
			},
			want: "rule 6: internal/pay/migrations: prefix \"ten\" is already claimed by platform/tenancy",
		},
		{
			name: "rule 6 testdata tokens do not refuse a prefix",
			files: map[string]string{
				"internal/platform/core/core.go":                               "package core\n",
				"internal/platform/tenancy/tenancy.go":                         "package tenancy\n",
				"internal/platform/tenancy/module.manifest.json":               "{\"tablePrefixes\": [\"ten\"]}\n",
				"internal/platform/tenancy/testdata/fixtures/x.go":             "package fixtures\n\nvar s = \"pay_status\"\n",
				"internal/pay/migrations/20260101000000_pay_create_orders.sql": "",
				"internal/pay/store.go":                                        "package pay\n\nvar q = \"SELECT id FROM pay_orders\"\n",
			},
			want: "",
		},
		{
			name: "rule 6 a vertical claims a prefix of the wrong shape",
			files: map[string]string{
				"internal/platform/core/core.go":                                    "package core\n",
				"internal/pay/migrations/20260101000000_payments_create_orders.sql": "",
			},
			want: "rule 6: internal/pay/migrations: prefix \"payments\" must be 2 to 4 lowercase letters",
		},
		{
			name: "rule 6 a vertical migration without a prefix",
			files: map[string]string{
				"internal/platform/core/core.go":             "package core\n",
				"internal/pay/migrations/20260101000000.sql": "",
			},
			want: "rule 6: internal/pay/migrations/20260101000000.sql: name must be <timestamp>_<prefix>_<name>.sql",
		},
		{
			name: "rule 6 a stray file next to a valid migration",
			files: map[string]string{
				"internal/platform/core/core.go":                               "package core\n",
				"internal/pay/migrations/20260101000000_pay_create_orders.sql": "",
				"internal/pay/migrations/seed_data.sql":                        "",
			},
			want: "rule 6: internal/pay/migrations/seed_data.sql: name must be <timestamp>_<prefix>_<name>.sql",
		},
		{
			name: "rule 6 several migrations of one prefix",
			files: map[string]string{
				"internal/platform/core/core.go":                               "package core\n",
				"internal/pay/migrations/20260101000000_pay_create_orders.sql": "",
				"internal/pay/migrations/20260101000100_pay_add_items.sql":     "",
			},
			want: "",
		},
		{
			name: "rule 6 a platform module naming a vertical's table refuses the prefix",
			files: map[string]string{
				"internal/platform/core/core.go":                               "package core\n",
				"internal/platform/tenancy/tenancy.go":                         "package tenancy\n",
				"internal/platform/tenancy/module.manifest.json":               "{\"tablePrefixes\": [\"ten\"]}\n",
				"internal/platform/tenancy/store.go":                           "package tenancy\n\nvar q = \"SELECT id FROM pay_orders\"\n",
				"internal/pay/migrations/20260101000000_pay_create_orders.sql": "",
			},
			want: "rule 6: internal/pay/migrations: prefix \"pay\" already appears as an identifier outside this vertical, choose another prefix",
		},
		{
			name: "rule 6 a sibling vertical naming a vertical's table refuses the prefix",
			files: map[string]string{
				"internal/platform/core/core.go":                               "package core\n",
				"internal/couriers/module.go":                                  "package couriers\n\nvar status = \"pay_status\"\n",
				"internal/pay/migrations/20260101000000_pay_create_orders.sql": "",
			},
			want: "rule 6: internal/pay/migrations: prefix \"pay\" already appears as an identifier outside this vertical, choose another prefix",
		},
		{
			name: "rule 6 refuses a prefix the platform tree uses as an identifier",
			files: map[string]string{
				"internal/platform/core/core.go":                               "package core\n",
				"internal/platform/identity/identity.go":                       "package identity\n",
				"internal/platform/identity/module.manifest.json":              "{\"tablePrefixes\": [\"iam\"]}\n",
				"internal/platform/identity/session_test.go":                   "package identity\n\nvar k = \"app_session\"\n",
				"internal/app/migrations/20260101000000_app_create_orders.sql": "",
			},
			want: "rule 6: internal/app/migrations: prefix \"app\" already appears as an identifier outside this vertical, choose another prefix",
		},
		{
			name: "rule 6 accepts a prefix next to an unrelated identifier",
			files: map[string]string{
				"internal/platform/core/core.go":                               "package core\n",
				"internal/platform/identity/identity.go":                       "package identity\n",
				"internal/platform/identity/module.manifest.json":              "{\"tablePrefixes\": [\"iam\"]}\n",
				"internal/platform/identity/session_test.go":                   "package identity\n\nvar k = \"app_session\"\n",
				"internal/pay/migrations/20260101000000_pay_create_orders.sql": "",
				"internal/pay/store.go":                                        "package pay\n\nvar q = \"SELECT id FROM pay_orders\"\n",
			},
			want: "",
		},
		{
			name: "rule 6 a short vertical prefix does not match longer words",
			files: map[string]string{
				"internal/platform/core/core.go":                               "package core\n",
				"internal/platform/parameters/parameters.go":                   "package parameters\n",
				"internal/platform/parameters/module.manifest.json":            "{\"tablePrefixes\": [\"sys\"]}\n",
				"internal/platform/parameters/store.go":                        "package parameters\n\nvar a = \"SELECT payload_total FROM payment_events\"\n",
				"internal/pay/migrations/20260101000000_pay_create_orders.sql": "",
				"internal/pay/store.go":                                        "package pay\n\nvar a = \"pay_orders\"\nvar b = \"cor_schema_migrations\"\n",
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
