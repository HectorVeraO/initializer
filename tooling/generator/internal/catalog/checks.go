package catalog

import (
	"fmt"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/gorewrite"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

const coreModuleFolder = "core"

type parsedGo struct {
	failed  bool
	pkg     string
	imports []goImport
}

type goImport struct {
	path string
	line int
}

// derivedFolder is the module folder name the id implies in the stack.
func derivedFolder(stack, id string) string {
	if stack == manifest.StackGoSrv {
		return strings.ReplaceAll(id, "-", "")
	}
	return id
}

func (b *builder) specByName() map[string]*specDoc {
	out := map[string]*specDoc{}
	for _, spec := range b.specs {
		out[spec.folder] = spec
	}
	return out
}

// checkModuleSet runs the checks that compare manifests with each other.
func (b *builder) checkModuleSet() {
	byStack := map[string]map[string]*moduleDoc{}
	for _, doc := range b.modules {
		id := doc.module.Module
		stackField := doc.module.Stack
		_, known := b.stackByName(stackField)
		if known && stackField != doc.stack.name {
			b.add(manifest.CodeStackUnknown, doc.path, "/stack", "stack does not match the modules root that holds the manifest")
		}
		if id == "" {
			doc.duplicate = true
			continue
		}
		if doc.folder != derivedFolder(doc.stack.name, id) {
			b.add(CodeModuleDirMismatch, doc.path, "/module", "folder is not the one derived from the module id")
		}
		ids := byStack[doc.stack.name]
		if ids == nil {
			ids = map[string]*moduleDoc{}
			byStack[doc.stack.name] = ids
		}
		if first, exists := ids[id]; exists {
			doc.duplicate = true
			b.add(CodeModuleDuplicate, doc.path, "/module", "module id is also declared by "+first.path)
			continue
		}
		ids[id] = doc
	}
	b.checkFrontend(byStack)
	b.checkImplied(byStack)
	b.checkCoreAndRequirements(byStack)
	b.checkImplementations()
	b.checkCycles(byStack)
}

func (b *builder) checkFrontend(byStack map[string]map[string]*moduleDoc) {
	for _, doc := range b.modules {
		if doc.duplicate || !manifest.IsFrontendStack(doc.stack.name) {
			continue
		}
		hasBackend := false
		for _, stack := range stackTable {
			if manifest.IsBackendStack(stack.name) && byStack[stack.name][doc.module.Module] != nil {
				hasBackend = true
			}
		}
		if !hasBackend {
			b.add(CodeModuleNotInBackend, doc.path, "/module", "no backend stack has a module with this id")
		}
	}
}

func (b *builder) checkImplied(byStack map[string]map[string]*moduleDoc) {
	for _, doc := range b.modules {
		if doc.duplicate {
			continue
		}
		id := doc.module.Module
		switch {
		case id == manifest.CoreModule && !doc.module.Implied:
			b.add(CodeImpliedMismatch, doc.path, "/implied", "core must be implied")
		case doc.module.Implied && id != manifest.CoreModule:
			b.add(CodeImpliedMismatch, doc.path, "/implied", "only core may be implied; select the module or require it through requiresModules")
		}
	}
	// An id present in several stacks must agree with the first stack's value.
	first := map[string]*moduleDoc{}
	for _, stack := range stackTable {
		ids := byStack[stack.name]
		names := make([]string, 0, len(ids))
		for id := range ids {
			names = append(names, id)
		}
		sort.Strings(names)
		for _, id := range names {
			doc := ids[id]
			head, seen := first[id]
			if !seen {
				first[id] = doc
				continue
			}
			if head.module.Implied != doc.module.Implied {
				b.add(CodeImpliedMismatch, doc.path, "/implied", "implied differs from the same module in "+head.stack.name)
			}
		}
	}
}

func (b *builder) checkCoreAndRequirements(byStack map[string]map[string]*moduleDoc) {
	for _, stack := range stackTable {
		ids := byStack[stack.name]
		if len(ids) == 0 {
			continue
		}
		core := ids[manifest.CoreModule]
		if core == nil {
			b.addPlain(CodeCoreMissing, stack.modulesRoot, "stack has modules but no core")
		}
		for _, doc := range b.modules {
			if doc.stack.name != stack.name || doc.duplicate {
				continue
			}
			if doc.module.Module == manifest.CoreModule && len(doc.module.RequiresModules) > 0 {
				b.add(CodeRequiresUnsatisfied, doc.path, "/requiresModules", "core must not require another module")
			}
			if core != nil && doc.module.RequiresCore != nil && !doc.module.RequiresCore.Contains(core.module.Version) {
				b.add(CodeRequiresUnsatisfied, doc.path, "/requiresCore",
					"core "+core.module.Version.String()+" is outside "+doc.module.RequiresCore.String())
			}
			for i, requirement := range doc.module.RequiresModules {
				pointer := fmt.Sprintf("/requiresModules/%d", i)
				target := ids[requirement.Module]
				switch {
				case target == nil:
					b.add(CodeRequiresUnsatisfied, doc.path, pointer, "module "+requirement.Module+" is absent from the stack")
				case !requirement.Range.Contains(target.module.Version):
					b.add(CodeRequiresUnsatisfied, doc.path, pointer,
						requirement.Module+" "+target.module.Version.String()+" is outside "+requirement.Range.String())
				}
			}
		}
	}
}

// checkImplementations compares every implementsSpec entry with the spec set.
func (b *builder) checkImplementations() {
	specs := b.specByName()
	implementedBy := map[string]map[string]*moduleDoc{}
	backendImplemented := map[string]bool{}
	for _, doc := range b.modules {
		if doc.duplicate {
			continue
		}
		names := make([]string, 0, len(doc.module.ImplementsSpec))
		for name := range doc.module.ImplementsSpec {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			version := doc.module.ImplementsSpec[name]
			pointer := "/implementsSpec/" + name
			spec := specs[name]
			if spec == nil {
				b.add(CodeSpecUnknown, doc.path, pointer, "no specs/"+name+"/spec.manifest.json")
				continue
			}
			perStack := implementedBy[doc.stack.name]
			if perStack == nil {
				perStack = map[string]*moduleDoc{}
				implementedBy[doc.stack.name] = perStack
			}
			if first, exists := perStack[name]; exists {
				b.add(CodeSpecImplementedTwice, doc.path, pointer, "spec is also implemented by "+first.path)
			} else {
				perStack[name] = doc
			}
			if manifest.IsBackendStack(doc.stack.name) {
				backendImplemented[name] = true
				if version != spec.spec.Version {
					b.add(CodeSpecVersionMismatch, doc.path, pointer,
						"implements "+version.String()+", the spec is at "+spec.spec.Version.String())
				}
				continue
			}
			switch {
			case version.Major != spec.spec.Version.Major:
				b.add(CodeSpecMajorMismatch, doc.path, pointer,
					"implements "+version.String()+", the spec is at "+spec.spec.Version.String())
			case version.Compare(spec.spec.Version) > 0:
				b.add(CodeSpecVersionAhead, doc.path, pointer,
					"implements "+version.String()+", the spec is at "+spec.spec.Version.String())
			}
		}
	}
	b.checkSpecSet(backendImplemented)
}

func (b *builder) checkSpecSet(backendImplemented map[string]bool) {
	tagOwner := map[string]*specDoc{}
	for _, spec := range b.specs {
		if !backendImplemented[spec.folder] {
			b.add(CodeSpecUnimplemented, spec.path, "/spec", "no backend module implements this spec")
		}
		for i, tag := range spec.spec.TCKTags {
			if other, exists := tagOwner[tag]; exists && other != spec {
				b.add(CodeTCKTagDuplicate, spec.path, fmt.Sprintf("/tckTags/%d", i), "tag is also declared by "+other.path)
				continue
			}
			tagOwner[tag] = spec
		}
	}
	for _, spec := range b.specs {
		for i, tag := range spec.spec.CooperatesWith {
			if _, owned := tagOwner[tag]; !owned {
				b.add(CodeTCKTagUnknown, spec.path, fmt.Sprintf("/cooperatesWith/%d", i),
					fmt.Sprintf("cooperatesWith names %s, which no spec declares in tckTags", tag))
			}
		}
	}
}

// checkCycles reports each group of modules that require each other, once.
func (b *builder) checkCycles(byStack map[string]map[string]*moduleDoc) {
	for _, stack := range stackTable {
		ids := byStack[stack.name]
		names := make([]string, 0, len(ids))
		for id := range ids {
			names = append(names, id)
		}
		sort.Strings(names)
		reach := map[string]map[string]bool{}
		for _, id := range names {
			reach[id] = reachable(ids, id)
		}
		reported := map[string]bool{}
		for _, id := range names {
			if !reach[id][id] || reported[id] {
				continue
			}
			group := []string{id}
			for _, other := range names {
				if other != id && reach[id][other] && reach[other][id] {
					group = append(group, other)
				}
			}
			for _, member := range group {
				reported[member] = true
			}
			b.add(CodeDependencyCycle, ids[id].path, "/requiresModules",
				"modules require each other in a cycle: "+strings.Join(group, ", "))
		}
	}
}

// reachable returns every module id reachable from start through requiresModules.
func reachable(ids map[string]*moduleDoc, start string) map[string]bool {
	seen := map[string]bool{}
	queue := []string{start}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		doc := ids[current]
		if doc == nil {
			continue
		}
		for _, requirement := range doc.module.RequiresModules {
			if !seen[requirement.Module] {
				seen[requirement.Module] = true
				queue = append(queue, requirement.Module)
			}
		}
	}
	return seen
}

// goChecks parses a catalogued .go file and applies the Go checks to it.
func (b *builder) goChecks(p string, data []byte, owner *moduleDoc) {
	isGo := strings.HasSuffix(p, ".go") && gorewrite.ShouldRewrite(p)
	if isGo {
		pg, ok := parseImports(p, data)
		b.parsed[p] = pg
		if !ok {
			b.addPlain(CodeGoParseFailed, p, "go/parser cannot read the file")
			return
		}
	}
	if b.goModule != "" {
		for _, finding := range gorewrite.CheckFile(p, data, b.goModule) {
			code := CodeGoModulePathLeak
			if finding.Rule == gorewrite.RuleImportGroup {
				code = CodeGoImportGroup
			}
			b.addPlain(code, fmt.Sprintf("%s:%d", p, finding.Line), finding.Detail)
		}
	}
	if isGo {
		b.checkBoundary(p, b.parsed[p], owner)
	}
}

func parseImports(p string, data []byte) (*parsedGo, bool) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, p, data, parser.ImportsOnly)
	if err != nil || file == nil {
		return &parsedGo{failed: true}, false
	}
	pg := &parsedGo{pkg: file.Name.Name}
	for _, spec := range file.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		pg.imports = append(pg.imports, goImport{path: value, line: fset.Position(spec.Pos()).Line})
	}
	return pg, true
}

func within(imported, base string) bool {
	return imported == base || strings.HasPrefix(imported, base+"/")
}

// checkBoundary applies IMPORT_BOUNDARY to the imports of one file.
func (b *builder) checkBoundary(p string, pg *parsedGo, owner *moduleDoc) {
	module := b.goModule
	if module == "" || pg == nil || pg.failed {
		return
	}
	platform := module + "/internal/platform"
	core := platform + "/" + coreModuleFolder
	for _, imp := range pg.imports {
		if !within(imp.path, module) {
			continue
		}
		violation := ""
		switch {
		case p == "go-srv/cmd/server/main.go":
			if !within(imp.path, core) {
				violation = "main.go may import only the core module from the module path"
			}
		case owner == nil || owner.stack.name != manifest.StackGoSrv:
		case owner.module.Module == manifest.CoreModule:
			if within(imp.path, platform) && !within(imp.path, core) {
				violation = "core must not import another module"
			}
		default:
			own := platform + "/" + owner.folder
			if !within(imp.path, core) && !within(imp.path, own) {
				violation = "a module may import only core and its own folder"
			}
		}
		if violation != "" {
			b.addPlain(CodeImportBoundary, fmt.Sprintf("%s:%d", p, imp.line), violation+": "+imp.path)
		}
	}
}

// checkEntrypoints applies ENTRYPOINT_MISSING to every module.
func (b *builder) checkEntrypoints() {
	for _, doc := range b.modules {
		id := doc.module.Module
		if doc.duplicate || id == "" {
			continue
		}
		if manifest.IsFrontendStack(doc.stack.name) {
			entry := doc.stack.modulesRoot + id + "/module.ts"
			if !b.set[entry] {
				b.add(CodeEntrypointMissing, doc.path, "/module", entry+" is missing")
			}
			continue
		}
		b.checkGoEntrypoint(doc)
	}
}

func (b *builder) checkGoEntrypoint(doc *moduleDoc) {
	dir := doc.stack.modulesRoot + doc.folder + "/"
	count := 0
	for _, p := range b.tracked {
		rest, found := strings.CutPrefix(p, dir)
		isDirect := found && !strings.Contains(rest, "/")
		if !isDirect || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			continue
		}
		pg := b.parsed[p]
		if pg == nil {
			continue
		}
		count++
		if !pg.failed && pg.pkg != doc.folder {
			b.addPlain(CodeEntrypointMissing, p, "package clause is "+pg.pkg+", want "+doc.folder)
		}
	}
	if count == 0 {
		b.add(CodeEntrypointMissing, doc.path, "/module", "no non-test Go file in "+dir)
	}
}
