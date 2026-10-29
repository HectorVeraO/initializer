package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/ports"
)

// Issue codes emitted by Build, besides the ones the manifest package emits.
const (
	CodeModuleDirMismatch     = "MODULE_DIR_MISMATCH"
	CodeModuleDuplicate       = "MODULE_DUPLICATE"
	CodeModuleNotInBackend    = "MODULE_NOT_IN_BACKEND"
	CodeImpliedMismatch       = "IMPLIED_MISMATCH"
	CodeCoreMissing           = "CORE_MISSING"
	CodeSpecUnknown           = "SPEC_UNKNOWN"
	CodeSpecVersionMismatch   = "SPEC_VERSION_MISMATCH"
	CodeSpecVersionAhead      = "SPEC_VERSION_AHEAD"
	CodeSpecMajorMismatch     = "SPEC_MAJOR_MISMATCH"
	CodeSpecImplementedTwice  = "SPEC_IMPLEMENTED_TWICE"
	CodeSpecUnimplemented     = "SPEC_UNIMPLEMENTED"
	CodeTCKTagDuplicate       = "TCK_TAG_DUPLICATE"
	CodeTCKTagUnknown         = "TCK_TAG_UNKNOWN"
	CodeRequiresUnsatisfied   = "REQUIRES_UNSATISFIED"
	CodeDependencyCycle       = "DEPENDENCY_CYCLE"
	CodeRequiresStackMismatch = "REQUIRES_STACK_MISMATCH"
	CodeFileUnowned           = "FILE_UNOWNED"
	CodeFileOwnershipConflict = "FILE_OWNERSHIP_CONFLICT"
	CodeTargetConflict        = "TARGET_CONFLICT"
	CodeSymlink               = "SYMLINK"
	CodeNonLFText             = "NON_LF_TEXT"
	CodeGoModuleLineInvalid   = "GO_MODULE_LINE_INVALID"
	CodeGoModulePathLeak      = "GO_MODULE_PATH_LEAK"
	CodeGoImportGroup         = "GO_IMPORT_GROUP"
	CodeGoParseFailed         = "GO_PARSE_FAILED"
	CodeImportBoundary        = "IMPORT_BOUNDARY"
	CodeEntrypointMissing     = "ENTRYPOINT_MISSING"
	CodeNameRuleInvalid       = "NAME_RULE_INVALID"
	CodeNameRuleForeign       = "NAME_RULE_FOREIGN"
	CodeNameRuleUnmatched     = "NAME_RULE_UNMATCHED"
	CodeNameRuleOverlap       = "NAME_RULE_OVERLAP"
	CodeReferenceInvalid      = names.CodeReferenceInvalid
	CodeReferenceIncomplete   = "REFERENCE_INCOMPLETE"
)

const (
	goModPath        = "go-srv/go.mod"
	maxManifestBytes = 1 << 20
	maxFileBytes     = 64 << 20
	textProbeBytes   = 8000
)

// BuildOptions are the inputs of one catalog build.
type BuildOptions struct {
	// Root is the repository checkout.
	Root fs.FS
	// Files are the tracked repo-relative paths, as git ls-files lists them.
	Files []string
	// Generator is the version written into the catalog.
	Generator string
}

type stackInfo struct {
	name            string
	root            string
	modulesRoot     string
	compositionRoot string
}

// stackTable is the built-in stack table of the manifest package, as source
// paths, sorted by stack name.
var stackTable = sourceStackTable()

func sourceStackTable() []stackInfo {
	rows := manifest.Stacks()
	table := make([]stackInfo, len(rows))
	for i, row := range rows {
		table[i] = stackInfo{
			name:            row.ID,
			root:            row.Root,
			modulesRoot:     row.Root + row.ModulesRoot,
			compositionRoot: row.Root + row.CompositionRoot,
		}
	}
	return table
}

// staticRoots are the managed roots that do not depend on a stack manifest.
var staticRoots = []string{"specs/", ".deploy/local/", ".initializer/"}

// ReferencePath is the file that holds the reference names the name rules
// match. The catalog reads it and never delivers it.
const ReferencePath = ".initializer/reference.json"

// renderedFiles are project-level files the generator renders; no copy may land on them.
var renderedFiles = []string{"README.md", "initializer.selection.json", "initializer.lock.json"}

type moduleDoc struct {
	path      string
	folder    string
	stack     stackInfo
	module    manifest.Module
	duplicate bool
	files     []File
	// recorded are the name rules with their blocks, sorted by (path, text).
	recorded []NameRule
}

type specDoc struct {
	path   string
	folder string
	spec   manifest.Spec
	files  []File
}

type entryRef struct {
	doc     *moduleDoc
	entry   manifest.FileEntry
	exclude bool
	index   int
}

type builder struct {
	root    fs.FS
	tracked []string
	set     map[string]bool
	issues  []issue.Issue

	modules      []*moduleDoc
	specs        []*specDoc
	managedStack map[string]bool
	goModule     string
	reference    map[string]string
	// referenceReady is true when the reference file decoded without issues.
	referenceReady bool
	// referenceDecoded is true when the reference file decoded; portBand is
	// its ports.band, 0 when it has none.
	referenceDecoded bool
	portBand         int
	parsed           map[string]*parsedGo
}

// Build validates every manifest and returns the catalog, or every issue found.
// The error is for I/O failures and malformed inputs only.
func Build(opts BuildOptions) (Catalog, []issue.Issue, error) {
	if opts.Root == nil {
		return Catalog{}, nil, errors.New("catalog: root is nil")
	}
	if opts.Generator == "" {
		return Catalog{}, nil, errors.New("catalog: generator version is empty")
	}
	tracked, err := cleanFileList(opts.Files)
	if err != nil {
		return Catalog{}, nil, err
	}
	b := &builder{
		root:         opts.Root,
		tracked:      tracked,
		set:          map[string]bool{},
		managedStack: map[string]bool{},
		parsed:       map[string]*parsedGo{},
	}
	for _, p := range tracked {
		b.set[p] = true
	}
	if err := b.run(); err != nil {
		return Catalog{}, nil, err
	}
	issue.Sort(b.issues)
	return b.assemble(opts.Generator), b.issues, nil
}

// cleanFileList validates, sorts and deduplicates the tracked paths.
func cleanFileList(files []string) ([]string, error) {
	seen := make(map[string]bool, len(files))
	out := make([]string, 0, len(files))
	for _, p := range files {
		if !validTrackedPath(p) || !fs.ValidPath(p) {
			return nil, fmt.Errorf("catalog: invalid tracked path %q", p)
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func (b *builder) add(code, p, pointer, message string) {
	b.issues = append(b.issues, issue.Issue{Code: code, Path: issue.Path(p, pointer), Message: message})
}

func (b *builder) addPlain(code, p, message string) {
	b.issues = append(b.issues, issue.Issue{Code: code, Path: p, Message: message})
}

func (b *builder) run() error {
	if err := b.loadManifests(); err != nil {
		return err
	}
	b.checkModuleSet()
	b.checkEntriesMatch()
	if err := b.readGoModule(); err != nil {
		return err
	}
	if err := b.readReference(); err != nil {
		return err
	}
	if err := b.assignOwnership(); err != nil {
		return err
	}
	if err := b.checkNameRules(); err != nil {
		return err
	}
	if err := b.checkPorts(); err != nil {
		return err
	}
	b.checkTargets()
	b.checkEntrypoints()
	b.checkCooperationFolders()
	return nil
}

func (b *builder) stackByName(name string) (stackInfo, bool) {
	for _, s := range stackTable {
		if s.name == name {
			return s, true
		}
	}
	return stackInfo{}, false
}

// readFile reads one tracked file without following a symlink. It returns
// symlink=true and no data for a symlink.
func (b *builder) readFile(p string, limit int64) (data []byte, symlink bool, err error) {
	info, err := fs.Lstat(b.root, p)
	if err != nil {
		return nil, false, fmt.Errorf("catalog: tracked file %s: %w", p, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, true, nil
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("catalog: tracked path %s is not a regular file", p)
	}
	file, err := b.root.Open(p)
	if err != nil {
		return nil, false, fmt.Errorf("catalog: tracked file %s: %w", p, err)
	}
	defer file.Close()
	data, err = io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, false, fmt.Errorf("catalog: tracked file %s: %w", p, err)
	}
	if int64(len(data)) > limit {
		return nil, false, fmt.Errorf("catalog: tracked file %s exceeds %d bytes", p, limit)
	}
	return data, false, nil
}

func (b *builder) loadManifests() error {
	for _, p := range b.tracked {
		for _, stack := range stackTable {
			if !strings.HasPrefix(p, stack.modulesRoot) {
				continue
			}
			parts := strings.Split(p[len(stack.modulesRoot):], "/")
			if len(parts) == 2 && parts[1] == "module.manifest.json" {
				if err := b.loadModule(p, parts[0], stack); err != nil {
					return err
				}
			}
		}
		parts := strings.Split(p, "/")
		if len(parts) == 3 && parts[0] == "specs" && parts[2] == "spec.manifest.json" {
			if err := b.loadSpec(p, parts[1]); err != nil {
				return err
			}
		}
	}
	for _, doc := range b.modules {
		b.managedStack[doc.stack.name] = true
	}
	return nil
}

func (b *builder) loadModule(p, folder string, stack stackInfo) error {
	data, symlink, err := b.readFile(p, maxManifestBytes)
	if err != nil {
		return err
	}
	if symlink {
		b.addPlain(CodeSymlink, p, "tracked path is a symlink")
		return nil
	}
	module, issues := manifest.DecodeModule(p, data)
	b.issues = append(b.issues, issues...)
	b.modules = append(b.modules, &moduleDoc{path: p, folder: folder, stack: stack, module: module})
	return nil
}

func (b *builder) loadSpec(p, folder string) error {
	data, symlink, err := b.readFile(p, maxManifestBytes)
	if err != nil {
		return err
	}
	if symlink {
		b.addPlain(CodeSymlink, p, "tracked path is a symlink")
		return nil
	}
	spec, issues := manifest.DecodeSpec(p, folder, data)
	b.issues = append(b.issues, issues...)
	b.specs = append(b.specs, &specDoc{path: p, folder: folder, spec: spec})
	return nil
}

// underManagedRoot reports whether p sits under a managed root.
func (b *builder) underManagedRoot(p string) bool {
	for _, root := range staticRoots {
		if strings.HasPrefix(p, root) {
			return true
		}
	}
	for _, stack := range stackTable {
		if b.managedStack[stack.name] && strings.HasPrefix(p, stack.root) {
			return true
		}
	}
	return false
}

// underUnmanagedStack reports whether p sits under a stack root whose stack has no manifest.
func (b *builder) underUnmanagedStack(p string) bool {
	for _, stack := range stackTable {
		if !b.managedStack[stack.name] && strings.HasPrefix(p, stack.root) {
			return true
		}
	}
	return false
}

// unmanifestedModuleFolder returns the folder prefix (with trailing slash) when p sits
// in a direct child folder of a managed stack's modules root that has no module
// manifest, and "" otherwise. Core's broader claim on the stack root never covers
// such a folder; an entry at least as specific as the folder still does.
func (b *builder) unmanifestedModuleFolder(p string) string {
	for _, stack := range stackTable {
		if !b.managedStack[stack.name] || !strings.HasPrefix(p, stack.modulesRoot) {
			continue
		}
		folder, _, nested := strings.Cut(p[len(stack.modulesRoot):], "/")
		if !nested {
			return ""
		}
		for _, doc := range b.modules {
			if doc.stack.name == stack.name && doc.folder == folder {
				return ""
			}
		}
		return stack.modulesRoot + folder + "/"
	}
	return ""
}

func isCompositionRoot(p string) bool {
	for _, stack := range stackTable {
		if p == stack.compositionRoot {
			return true
		}
	}
	return false
}

func entryMatches(entry manifest.FileEntry, p string) bool {
	if strings.HasSuffix(entry.Path, "/") {
		return strings.HasPrefix(p, entry.Path)
	}
	return p == entry.Path
}

// checkEntriesMatch reports a files or exclude entry that matches no tracked file.
func (b *builder) checkEntriesMatch() {
	for _, doc := range b.modules {
		check := func(section string, entries []manifest.FileEntry) {
			for i, entry := range entries {
				if !b.anyTracked(entry) {
					b.add(manifest.CodeManifestInvalid, doc.path, fmt.Sprintf("/%s/%d", section, i),
						"entry matches no tracked file")
				}
			}
		}
		check("files", doc.module.Files)
		check("exclude", doc.module.Exclude)
	}
}

func (b *builder) anyTracked(entry manifest.FileEntry) bool {
	if !strings.HasSuffix(entry.Path, "/") {
		return b.set[entry.Path]
	}
	at := sort.SearchStrings(b.tracked, entry.Path)
	return at < len(b.tracked) && strings.HasPrefix(b.tracked[at], entry.Path)
}

// resolve finds the winning entry of p. conflict lists the manifests tied on the longest entry.
func (b *builder) resolve(p string) (winner *entryRef, conflict []*moduleDoc) {
	best := -1
	var candidates []entryRef
	consider := func(doc *moduleDoc, entries []manifest.FileEntry, exclude bool) {
		for i, entry := range entries {
			if !entryMatches(entry, p) {
				continue
			}
			length := len(entry.Path)
			if length < best {
				continue
			}
			if length > best {
				best = length
				candidates = candidates[:0]
			}
			candidates = append(candidates, entryRef{doc: doc, entry: entry, exclude: exclude, index: i})
		}
	}
	for _, doc := range b.modules {
		consider(doc, doc.module.Files, false)
		consider(doc, doc.module.Exclude, true)
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	docs := map[*moduleDoc]bool{}
	for _, c := range candidates {
		docs[c.doc] = true
	}
	if len(docs) > 1 {
		for _, doc := range b.modules {
			if docs[doc] {
				conflict = append(conflict, doc)
			}
		}
		return nil, conflict
	}
	// Within one manifest an exclude entry beats a files entry of equal length.
	chosen := candidates[0]
	for _, c := range candidates {
		if c.exclude {
			chosen = c
		}
	}
	return &chosen, nil
}

func (b *builder) specOf(p string) *specDoc {
	parts := strings.SplitN(p, "/", 3)
	if len(parts) < 3 || parts[0] != "specs" {
		return nil
	}
	for _, spec := range b.specs {
		if spec.folder == parts[1] {
			return spec
		}
	}
	return nil
}

func (b *builder) assignOwnership() error {
	for _, p := range b.tracked {
		if p == ReferencePath || isCompositionRoot(p) || b.underUnmanagedStack(p) {
			continue
		}
		if spec := b.specOf(p); spec != nil {
			file, ok, err := b.contentChecks(p, "", nil)
			if err != nil {
				return err
			}
			if ok {
				spec.files = append(spec.files, file)
			}
			continue
		}
		winner, conflict := b.resolve(p)
		if folder := b.unmanifestedModuleFolder(p); folder != "" && len(conflict) == 0 && (winner == nil || len(winner.entry.Path) < len(folder)) {
			b.addPlain(CodeFileUnowned, p, "module folder has no module.manifest.json")
			continue
		}
		switch {
		case len(conflict) > 0:
			names := make([]string, len(conflict))
			for i, doc := range conflict {
				names[i] = doc.stack.name + "/" + doc.module.Module
			}
			b.addPlain(CodeFileOwnershipConflict, p, "owned equally by "+strings.Join(names, " and "))
		case winner == nil:
			if b.underManagedRoot(p) {
				b.addPlain(CodeFileUnowned, p, "no manifest lists this file")
			}
		case winner.exclude:
		default:
			file, ok, err := b.contentChecks(p, winner.entry.Target, winner.doc)
			if err != nil {
				return err
			}
			if ok {
				winner.doc.files = append(winner.doc.files, file)
			}
		}
	}
	return nil
}

// contentChecks reads a catalogued file, hashes it and applies the per-file checks.
// ok is false when the file cannot be listed (a symlink).
func (b *builder) contentChecks(p, target string, owner *moduleDoc) (File, bool, error) {
	data, symlink, err := b.readFile(p, maxFileBytes)
	if err != nil {
		return File{}, false, err
	}
	if symlink {
		b.addPlain(CodeSymlink, p, "tracked path is a symlink")
		return File{}, false, nil
	}
	if isText(data) && strings.Contains(string(data), "\r\n") {
		b.addPlain(CodeNonLFText, p, "text file contains CRLF line ends")
	}
	b.goChecks(p, data, owner)
	if isText(data) {
		b.markerChecks(p, data, owner)
	}
	sum := sha256.Sum256(data)
	return File{Path: p, SHA256: hex.EncodeToString(sum[:]), Target: target}, true, nil
}

func isText(data []byte) bool {
	probe := data
	if len(probe) > textProbeBytes {
		probe = probe[:textProbeBytes]
	}
	for _, c := range probe {
		if c == 0 {
			return false
		}
	}
	return true
}

func (b *builder) readGoModule() error {
	if !b.managedStack[manifest.StackGoSrv] {
		return nil
	}
	if !b.set[goModPath] {
		b.addPlain(CodeGoModuleLineInvalid, goModPath, "file is not tracked")
		return nil
	}
	data, symlink, err := b.readFile(goModPath, maxFileBytes)
	if err != nil {
		return err
	}
	if symlink {
		return nil
	}
	firstLine := string(data)
	if end := strings.IndexByte(firstLine, '\n'); end >= 0 {
		firstLine = firstLine[:end]
	}
	module, found := strings.CutPrefix(firstLine, "module ")
	if !found || module == "" || strings.ContainsAny(module, " \t\r\"`") || path.Clean(module) != module {
		b.addPlain(CodeGoModuleLineInvalid, goModPath, "line 1 must be \"module <path>\"")
		return nil
	}
	b.goModule = module
	return nil
}

// checkTargets applies TARGET_CONFLICT to every copy with a target.
func (b *builder) checkTargets() {
	destinations := map[string][]string{}
	for _, doc := range b.modules {
		for _, file := range doc.files {
			dest := file.Path
			if file.Target != "" {
				dest = file.Target
			}
			destinations[dest] = append(destinations[dest], file.Path)
		}
	}
	for _, spec := range b.specs {
		for _, file := range spec.files {
			destinations[file.Path] = append(destinations[file.Path], file.Path)
		}
	}
	for _, doc := range b.modules {
		for _, file := range doc.files {
			if file.Target == "" {
				continue
			}
			pointer := b.targetPointer(doc, file.Path)
			if len(destinations[file.Target]) > 1 {
				b.add(CodeTargetConflict, doc.path, pointer, "target "+file.Target+" is also the destination of another file")
				continue
			}
			if reason := b.reservedTarget(doc, file.Target); reason != "" {
				b.add(CodeTargetConflict, doc.path, pointer, "target "+file.Target+" "+reason)
			}
		}
	}
}

func (b *builder) targetPointer(doc *moduleDoc, source string) string {
	for i, entry := range doc.module.Files {
		if entry.Path == source && entry.Target != "" {
			return fmt.Sprintf("/files/%d/target", i)
		}
	}
	return "/files"
}

func (b *builder) reservedTarget(doc *moduleDoc, target string) string {
	if isCompositionRoot(target) {
		return "is a composition root"
	}
	for _, name := range renderedFiles {
		if target == name {
			return "is a rendered project file"
		}
	}
	if !b.underManagedRoot(target) {
		return ""
	}
	if b.specOf(target) != nil {
		return "is inside a spec folder"
	}
	winner, conflict := b.resolve(target)
	if len(conflict) > 0 || (winner != nil && winner.doc != doc) {
		return "is under the managed root of another owner"
	}
	return ""
}

// assemble builds the catalog value from the loaded documents.
func (b *builder) assemble(generator string) Catalog {
	c := Catalog{
		Schema:    SchemaVersion,
		Generator: generator,
		GoModule:  b.goModule,
		Reference: b.reference,
		PortBand:  b.portBand,
		Specs:     []SpecEntry{},
		Stacks:    []StackEntry{},
	}
	for _, spec := range b.specs {
		tags := append([]string{}, spec.spec.TCKTags...)
		env := map[string]string{}
		for k, v := range spec.spec.TCKEnv {
			env[k] = v
		}
		var cooperates []string
		if len(spec.spec.CooperatesWith) > 0 {
			cooperates = append([]string{}, spec.spec.CooperatesWith...)
		}
		c.Specs = append(c.Specs, SpecEntry{
			Spec:           spec.folder,
			Version:        spec.spec.Version.String(),
			TCKTags:        tags,
			TCKEnv:         env,
			CooperatesWith: cooperates,
			Files:          sortedFiles(spec.files),
		})
	}
	sort.SliceStable(c.Specs, func(i, j int) bool { return c.Specs[i].Spec < c.Specs[j].Spec })
	for _, stack := range stackTable {
		var entries []ModuleEntry
		for _, doc := range b.modules {
			if doc.stack.name == stack.name && !doc.duplicate {
				entries = append(entries, b.moduleEntry(doc))
			}
		}
		if len(entries) == 0 {
			continue
		}
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Module < entries[j].Module })
		role := "frontend"
		if manifest.IsBackendStack(stack.name) {
			role = "backend"
		}
		c.Stacks = append(c.Stacks, StackEntry{Stack: stack.name, Role: role, Modules: entries})
	}
	return c
}

func (b *builder) moduleEntry(doc *moduleDoc) ModuleEntry {
	m := doc.module
	entry := ModuleEntry{
		Module:          m.Module,
		Version:         m.Version.String(),
		Implied:         m.Implied,
		ImplementsSpec:  map[string]string{},
		RequiresModules: []Requirement{},
		TablePrefixes:   append([]string(nil), m.TablePrefixes...),
		Files:           sortedFiles(doc.files),
		Names:           doc.recorded,
		Ports:           append([]ports.Port(nil), m.Ports...),
		PortFiles:       recordedPortFiles(m.PortFiles),
	}
	for spec, version := range m.ImplementsSpec {
		entry.ImplementsSpec[spec] = version.String()
	}
	if m.RequiresCore != nil {
		entry.RequiresCore = m.RequiresCore.String()
	}
	for _, requirement := range m.RequiresModules {
		entry.RequiresModules = append(entry.RequiresModules, Requirement{
			Module: requirement.Module,
			Range:  requirement.Range.String(),
		})
	}
	return entry
}

func sortedFiles(files []File) []File {
	out := append([]File{}, files...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
