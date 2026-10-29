// Package plan resolves a selection against a catalog.
package plan

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/semver"
)

// Issue codes raised by this package. Codes shared with the catalog build or
// the manifest decoders are reused from those packages.
const (
	CodeModuleUnknown     = "MODULE_UNKNOWN"
	CodeSpecNotInBackend  = "SPEC_NOT_IN_BACKEND"
	CodeGeneratorMismatch = "CATALOG_GENERATOR_MISMATCH"
	CodeFileMismatch      = "CATALOG_FILE_MISMATCH"
	CodeTCKEnvConflict    = "TCK_ENV_CONFLICT"
)

// Issue path sources: the selection has no file name here, so its issues use
// "selection"; catalog issues use the catalog file name.
const (
	selectionSource = "selection"
	catalogSource   = "catalog.lock.json"
	coreID          = "core"
	roleBackend     = "backend"
	roleFrontend    = "frontend"
)

var (
	envKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

// Plan is the resolved selection: what to copy and how to certify it.
type Plan struct {
	// Selection is the recorded form: stacks sorted, modules in backend
	// catalog order with implied modules included.
	Selection manifest.Selection
	// Stacks are the selected stacks, sorted by stack name.
	Stacks []StackPlan
	// Components holds one component per selected stack, sorted by Stack.
	Components []Component
	// Specs are the specs to copy, sorted by spec.
	Specs []catalog.SpecEntry
	// TCKTags is the sorted union of the copied specs' tags.
	TCKTags []string
	// TCKGrep is the anchored Playwright --grep pattern built from TCKTags.
	TCKGrep string
	// TCKGrepInvert is the anchored Playwright --grep-invert pattern built from
	// the cooperatesWith tags of the copied specs that are not in TCKTags,
	// sorted; "" when every cooperating tag is installed.
	TCKGrepInvert string
	// TCKEnv is the union of the copied specs' tckEnv.
	TCKEnv      map[string]string
	GoModule    string
	ComposeName string
}

// Component is one delivered stack and the name of its project folder,
// compose service and %{component.<stack>} slot.
type Component struct {
	Stack string
	Name  string
}

// Component returns the single component of a stack, and false when the stack
// is not selected or has several components.
func (p Plan) Component(stack string) (Component, bool) {
	var found Component
	count := 0
	for _, component := range p.Components {
		if component.Stack == stack {
			found = component
			count++
		}
	}
	if count != 1 {
		return Component{}, false
	}
	return found, true
}

// StackPlan is the modules of one selected stack in catalog order, core first.
type StackPlan struct {
	Stack, Role string
	Modules     []catalog.ModuleEntry
}

// origin is where an id of the resolved set came from, for issue paths.
type origin struct {
	path string
}

type resolver struct {
	cat    catalog.Catalog
	sel    manifest.Selection
	issues []issue.Issue
}

// Resolve returns the modules per stack in catalog order, the specs, TCK tags and TCK env,
// or every issue found. With any issue the returned Plan is the zero value.
func Resolve(cat catalog.Catalog, sel manifest.Selection) (Plan, []issue.Issue) {
	r := &resolver{cat: cat, sel: sel}
	result := r.run()
	if len(r.issues) > 0 {
		issue.Sort(r.issues)
		return Plan{}, r.issues
	}
	return result, nil
}

func (r *resolver) add(code, source, pointer, message string) {
	r.issues = append(r.issues, issue.Issue{Code: code, Path: issue.Path(source, pointer), Message: message})
}

func (r *resolver) run() Plan {
	if r.cat.Generator != generator.Version {
		r.add(CodeGeneratorMismatch, catalogSource, "/generator",
			fmt.Sprintf("catalog was built by generator %q, this is %q", r.cat.Generator, generator.Version))
	}
	selected := r.selectedStacks()
	if selected == nil {
		return Plan{}
	}
	backend := -1
	for i, entry := range selected {
		if r.cat.Stacks[entry].Role == roleBackend {
			backend = i
		}
	}
	afterStacks := len(r.issues)
	for _, entry := range selected {
		if findModule(r.cat.Stacks[entry], coreID) < 0 {
			r.add(catalog.CodeCoreMissing, catalogSource, fmt.Sprintf("/stacks/%d", entry), "stack has no core module")
		}
	}
	if len(r.issues) > afterStacks {
		return Plan{}
	}
	closure := r.closure(selected)
	r.checkPresence(selected[backend], closure)
	stackPlans := make([]StackPlan, 0, len(selected))
	for _, entry := range selected {
		stackPlans = append(stackPlans, r.planStack(entry, closure))
	}
	r.checkFrontendSpecs(selected, stackPlans)
	if len(r.issues) > 0 {
		return Plan{}
	}
	sort.Slice(stackPlans, func(i, j int) bool { return stackPlans[i].Stack < stackPlans[j].Stack })
	return r.assemble(stackPlans, backendPlan(stackPlans))
}

// selectedStacks returns the catalog indexes of the selected stacks, or nil
// after reporting why the stack choice is unusable.
func (r *resolver) selectedStacks() []int {
	start := len(r.issues)
	var selected []int
	seen := map[string]bool{}
	backends, frontends := 0, 0
	for i, name := range r.sel.Stacks {
		pointer := fmt.Sprintf("/stacks/%d", i)
		if seen[name] {
			r.add(manifest.CodeSelectionInvalid, selectionSource, pointer, "stack is listed twice")
			continue
		}
		seen[name] = true
		index := -1
		for j, stack := range r.cat.Stacks {
			if stack.Stack == name {
				index = j
			}
		}
		if index < 0 || len(r.cat.Stacks[index].Modules) == 0 {
			r.add(manifest.CodeSelectionInvalid, selectionSource, pointer, "stack has no modules in the catalog")
			continue
		}
		switch r.cat.Stacks[index].Role {
		case roleBackend:
			backends++
		case roleFrontend:
			frontends++
		}
		selected = append(selected, index)
	}
	if len(r.issues) > start {
		return nil
	}
	if backends != 1 {
		r.add(manifest.CodeSelectionInvalid, selectionSource, "/stacks", "must contain exactly one backend stack")
	}
	if frontends > 1 {
		r.add(manifest.CodeSelectionInvalid, selectionSource, "/stacks", "must contain at most one frontend stack")
	}
	if len(r.issues) > start {
		return nil
	}
	return selected
}

// closure computes step 1 and 2: implied ids and the selection, closed under
// requiresModules in every selected stack. The map value records where the id
// first came from.
func (r *resolver) closure(selected []int) map[string]origin {
	set := map[string]origin{coreID: {}}
	var queue []string
	push := func(id string, from origin) {
		if _, found := set[id]; found {
			return
		}
		set[id] = from
		queue = append(queue, id)
	}
	for _, entry := range selected {
		for _, module := range r.cat.Stacks[entry].Modules {
			if module.Implied {
				push(module.Module, origin{})
			}
		}
	}
	queue = append(queue, coreID)
	if r.sel.AllModules {
		for _, entry := range selected {
			if r.cat.Stacks[entry].Role != roleBackend {
				continue
			}
			for _, module := range r.cat.Stacks[entry].Modules {
				push(module.Module, origin{})
			}
		}
	}
	for i, id := range r.sel.Modules {
		push(id, origin{path: issue.Path(selectionSource, fmt.Sprintf("/modules/%d", i))})
	}
	for head := 0; head < len(queue); head++ {
		id := queue[head]
		for _, entry := range selected {
			stack := r.cat.Stacks[entry]
			position := findModule(stack, id)
			if position < 0 {
				continue
			}
			for k, requirement := range stack.Modules[position].RequiresModules {
				pointer := fmt.Sprintf("/stacks/%d/modules/%d/requiresModules/%d/module", entry, position, k)
				push(requirement.Module, origin{path: issue.Path(catalogSource, pointer)})
			}
		}
	}
	return set
}

// checkPresence reports step 3 for every id of the closure.
func (r *resolver) checkPresence(backendEntry int, closure map[string]origin) {
	backend := r.cat.Stacks[backendEntry]
	for _, id := range sortedKeys(closure) {
		if findModule(backend, id) >= 0 {
			continue
		}
		code, message := CodeModuleUnknown, "module is not in the catalog"
		for _, stack := range r.cat.Stacks {
			if findModule(stack, id) >= 0 {
				code, message = catalog.CodeModuleNotInBackend, "module has no manifest in the backend stack"
			}
		}
		path := closure[id].path
		if path == "" {
			path = issue.Path(catalogSource, fmt.Sprintf("/stacks/%d", backendEntry))
		}
		r.issues = append(r.issues, issue.Issue{Code: code, Path: path, Message: fmt.Sprintf("%s: %q", message, id)})
	}
}

// planStack applies steps 4 and 5 to one stack.
func (r *resolver) planStack(entry int, closure map[string]origin) StackPlan {
	stack := r.cat.Stacks[entry]
	var present []int
	for position, module := range stack.Modules {
		if _, found := closure[module.Module]; found {
			present = append(present, position)
		}
	}
	r.checkRequirements(entry, present)
	ordered := r.order(entry, present)
	modules := make([]catalog.ModuleEntry, 0, len(ordered))
	for _, position := range ordered {
		modules = append(modules, stack.Modules[position])
	}
	return StackPlan{Stack: stack.Stack, Role: stack.Role, Modules: modules}
}

func (r *resolver) checkRequirements(entry int, present []int) {
	stack := r.cat.Stacks[entry]
	coreVersion, coreOK := r.version(entry, findModule(stack, coreID), "/version", stack.Modules[findModule(stack, coreID)].Version)
	for _, position := range present {
		module := stack.Modules[position]
		base := fmt.Sprintf("/stacks/%d/modules/%d", entry, position)
		if module.Module != coreID && module.RequiresCore != "" {
			r.checkRange(base+"/requiresCore", module.RequiresCore, coreVersion, coreOK, "core", stack.Modules[findModule(stack, coreID)].Version)
		}
		for k, requirement := range module.RequiresModules {
			pointer := fmt.Sprintf("%s/requiresModules/%d", base, k)
			target := findModule(stack, requirement.Module)
			if target < 0 {
				if stack.Role != roleBackend {
					r.add(catalog.CodeRequiresUnsatisfied, catalogSource, pointer+"/module",
						fmt.Sprintf("required module %q is not in stack %s", requirement.Module, stack.Stack))
				}
				continue
			}
			version, ok := r.version(entry, target, "/version", stack.Modules[target].Version)
			r.checkRange(pointer+"/range", requirement.Range, version, ok, requirement.Module, stack.Modules[target].Version)
		}
	}
}

func (r *resolver) version(entry, position int, suffix, text string) (semver.Version, bool) {
	parsed, err := semver.ParseVersion(text)
	if err != nil {
		pointer := fmt.Sprintf("/stacks/%d/modules/%d%s", entry, position, suffix)
		r.add(catalog.CodeCatalogInvalid, catalogSource, pointer, err.Error())
		return semver.Version{}, false
	}
	return parsed, true
}

func (r *resolver) checkRange(pointer, rangeText string, version semver.Version, versionOK bool, name, versionText string) {
	parsed, err := semver.ParseRange(rangeText)
	if err != nil {
		r.add(catalog.CodeCatalogInvalid, catalogSource, pointer, err.Error())
		return
	}
	if versionOK && !parsed.Contains(version) {
		r.add(catalog.CodeRequiresUnsatisfied, catalogSource, pointer,
			fmt.Sprintf("range %s excludes %s %s", rangeText, name, versionText))
	}
}

// order is step 5: core first, then Kahn's algorithm over requiresModules among
// the present modules, implied ones first, then by id bytewise.
func (r *resolver) order(entry int, present []int) []int {
	stack := r.cat.Stacks[entry]
	ids := map[string]int{}
	for _, position := range present {
		ids[stack.Modules[position].Module] = position
	}
	pending := map[int]map[string]bool{}
	for _, position := range present {
		module := stack.Modules[position]
		if module.Module == coreID {
			continue
		}
		deps := map[string]bool{}
		for _, requirement := range module.RequiresModules {
			if _, found := ids[requirement.Module]; found && requirement.Module != coreID {
				deps[requirement.Module] = true
			}
		}
		pending[position] = deps
	}
	ordered := []int{ids[coreID]}
	for len(pending) > 0 {
		var ready []int
		for position, deps := range pending {
			if len(deps) == 0 {
				ready = append(ready, position)
			}
		}
		if len(ready) == 0 {
			r.add(catalog.CodeDependencyCycle, catalogSource, fmt.Sprintf("/stacks/%d", entry),
				"requiresModules has a cycle among the selected modules")
			return ordered
		}
		sort.Slice(ready, func(i, j int) bool {
			a, b := stack.Modules[ready[i]], stack.Modules[ready[j]]
			if a.Implied != b.Implied {
				return a.Implied
			}
			return a.Module < b.Module
		})
		next := ready[0]
		ordered = append(ordered, next)
		delete(pending, next)
		for _, deps := range pending {
			delete(deps, stack.Modules[next].Module)
		}
	}
	return ordered
}

func backendPlan(stacks []StackPlan) StackPlan {
	for _, stack := range stacks {
		if stack.Role == roleBackend {
			return stack
		}
	}
	return StackPlan{}
}

// checkFrontendSpecs reports SPEC_NOT_IN_BACKEND.
func (r *resolver) checkFrontendSpecs(selected []int, stacks []StackPlan) {
	backendSpecs := map[string]bool{}
	for _, module := range backendPlan(stacks).Modules {
		for spec := range module.ImplementsSpec {
			backendSpecs[spec] = true
		}
	}
	for _, entry := range selected {
		stack := r.cat.Stacks[entry]
		if stack.Role != roleFrontend {
			continue
		}
		for _, planned := range stacks {
			if planned.Stack != stack.Stack {
				continue
			}
			for _, module := range planned.Modules {
				for _, spec := range sortedKeys(module.ImplementsSpec) {
					if !backendSpecs[spec] {
						position := findModule(stack, module.Module)
						pointer := fmt.Sprintf("/stacks/%d/modules/%d/implementsSpec/%s", entry, position, strings.NewReplacer("~", "~0", "/", "~1").Replace(spec))
						r.add(CodeSpecNotInBackend, catalogSource, pointer, r.specNotInBackendMessage(spec, planned.Stack, module.Module))
					}
				}
			}
		}
	}
}

// specNotInBackendMessage is the SPEC_NOT_IN_BACKEND text: it names the
// backend catalog modules that implement spec, when there are any.
func (r *resolver) specNotInBackendMessage(spec, stack, module string) string {
	message := fmt.Sprintf("no selected backend module implements spec %q (needed by %s %s)", spec, stack, module)
	var implementers []string
	for _, entry := range r.cat.Stacks {
		if entry.Role != roleBackend {
			continue
		}
		for _, candidate := range entry.Modules {
			if _, found := candidate.ImplementsSpec[spec]; found {
				implementers = append(implementers, candidate.Module)
			}
		}
	}
	if len(implementers) == 0 {
		return message
	}
	sort.Strings(implementers)
	return fmt.Sprintf("%s; select module %q or drop stack %s", message, strings.Join(implementers, ","), stack)
}

// assemble applies step 6 and builds the Plan.
func (r *resolver) assemble(stacks []StackPlan, backend StackPlan) Plan {
	wanted := map[string]bool{}
	for _, module := range backend.Modules {
		for spec := range module.ImplementsSpec {
			wanted[spec] = true
		}
	}
	var specs []catalog.SpecEntry
	tagSet := map[string]bool{}
	env := map[string]string{}
	envFrom := map[string]string{}
	for i, spec := range r.cat.Specs {
		if !wanted[spec.Spec] {
			continue
		}
		delete(wanted, spec.Spec)
		specs = append(specs, spec)
		for _, tag := range spec.TCKTags {
			if !manifest.TagPattern.MatchString(tag) {
				r.add(catalog.CodeCatalogInvalid, catalogSource, fmt.Sprintf("/specs/%d/tckTags", i), fmt.Sprintf("tag %q must match ^@[a-z][a-z0-9-]*$", tag))
				continue
			}
			tagSet[tag] = true
		}
		for _, key := range sortedKeys(spec.TCKEnv) {
			value := spec.TCKEnv[key]
			pointer := fmt.Sprintf("/specs/%d/tckEnv/%s", i, key)
			if !envKeyPattern.MatchString(key) || strings.ContainsAny(value, "`\r\n\x00") {
				r.add(catalog.CodeCatalogInvalid, catalogSource, pointer, "tckEnv needs an upper-case key and a value without backtick or line break")
				continue
			}
			previous, found := env[key]
			if found && previous != value {
				r.add(CodeTCKEnvConflict, catalogSource, pointer,
					fmt.Sprintf("spec %q sets %s differently from spec %q", spec.Spec, key, envFrom[key]))
				continue
			}
			env[key] = value
			envFrom[key] = spec.Spec
		}
	}
	for i := range specs {
		specs[i].Files = installedFiles(specs[i].Files, tagSet)
	}
	for _, spec := range sortedKeys(wanted) {
		r.add(catalog.CodeSpecUnknown, catalogSource, "/specs", fmt.Sprintf("spec %q is implemented by a module but not in the catalog", spec))
	}
	tags := sortedKeys(tagSet)
	invertSet := map[string]bool{}
	for i, spec := range specs {
		for _, tag := range spec.CooperatesWith {
			if !manifest.TagPattern.MatchString(tag) {
				r.add(catalog.CodeCatalogInvalid, catalogSource, fmt.Sprintf("/specs/%d/cooperatesWith", i), fmt.Sprintf("tag %q must match ^@[a-z][a-z0-9-]*$", tag))
				continue
			}
			if !tagSet[tag] {
				invertSet[tag] = true
			}
		}
	}
	recorded := make([]string, 0, len(backend.Modules))
	for _, module := range backend.Modules {
		recorded = append(recorded, module.Module)
	}
	stackNames := make([]string, 0, len(stacks))
	for _, stack := range stacks {
		stackNames = append(stackNames, stack.Stack)
	}
	components := make([]Component, 0, len(stacks))
	for _, stack := range stacks {
		components = append(components, Component{Stack: stack.Stack, Name: stack.Stack})
	}
	return Plan{
		Selection: manifest.Selection{
			Schema:  r.sel.Schema,
			Project: r.sel.Project,
			Stacks:  stackNames,
			Modules: recorded,
		},
		Stacks:        stacks,
		Components:    components,
		Specs:         specs,
		TCKTags:       tags,
		TCKGrep:       Grep(tags),
		TCKGrepInvert: Grep(sortedKeys(invertSet)),
		TCKEnv:        env,
		GoModule:      r.cat.GoModule,
		ComposeName:   r.cat.ComposeName,
	}
}

// installedFiles drops the files of a with-<tag> cooperation folder whose tag no
// copied spec installs. It returns a new slice and leaves files untouched.
func installedFiles(files []catalog.File, installed map[string]bool) []catalog.File {
	kept := make([]catalog.File, 0, len(files))
	for _, file := range files {
		tag, gated := catalog.CooperationTag(file.Path)
		if gated && !installed[tag] {
			continue
		}
		kept = append(kept, file)
	}
	return kept
}

// Grep builds the anchored Playwright --grep pattern for sorted tags. The
// negative lookahead keeps @core from matching a longer tag such as @core-x.
// It returns "" for no tags.
func Grep(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	names := make([]string, len(tags))
	for i, tag := range tags {
		names[i] = strings.TrimPrefix(tag, "@")
	}
	return "@(" + strings.Join(names, "|") + ")(?![A-Za-z0-9_-])"
}

func findModule(stack catalog.StackEntry, id string) int {
	for i, module := range stack.Modules {
		if module.Module == id {
			return i
		}
	}
	return -1
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Pull is a module the requirement closure added to one stack.
type Pull struct {
	// Module is the pulled module id.
	Module string
	// RequiredBy lists the present modules of the stack whose requiresModules
	// names Module, sorted bytewise.
	RequiredBy []string
	// Stack is the stack name.
	Stack string
}

// Pulls returns the modules of p that the requirement closure added, ordered
// by stack then module. It returns nil when sel is --all-modules.
func Pulls(p Plan, sel manifest.Selection) []Pull {
	if sel.AllModules {
		return nil
	}
	typed := map[string]bool{coreID: true}
	for _, id := range sel.Modules {
		typed[id] = true
	}
	var pulls []Pull
	for _, stack := range p.Stacks {
		requirers := map[string][]string{}
		for _, module := range stack.Modules {
			for _, requirement := range module.RequiresModules {
				requirers[requirement.Module] = append(requirers[requirement.Module], module.Module)
			}
		}
		var found []Pull
		for _, module := range stack.Modules {
			by := requirers[module.Module]
			if typed[module.Module] || len(by) == 0 {
				continue
			}
			sort.Strings(by)
			found = append(found, Pull{Module: module.Module, RequiredBy: by, Stack: stack.Stack})
		}
		sort.Slice(found, func(i, j int) bool { return found[i].Module < found[j].Module })
		pulls = append(pulls, found...)
	}
	return pulls
}
