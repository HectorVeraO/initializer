// Package plan resolves a selection against a catalog.
package plan

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
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
	// Selection is the resolved selection: the effective project, stacks
	// sorted and the modules of the index-0 backend in plan order with implied
	// modules included. A multi project records every backend's list on its
	// entry and no top-level modules.
	Selection manifest.Selection
	// Components holds every delivered component, sorted by (Stack, Index).
	Components []ComponentPlan
	// Specs are the specs to copy, the union over the backends, sorted by spec.
	Specs []catalog.SpecEntry
	// TCKTags is the sorted tags of the specs of the index-0 backend (L131).
	TCKTags []string
	// TCKGrep is the anchored Playwright --grep pattern built from TCKTags.
	TCKGrep string
	// TCKGrepInvert is the anchored Playwright --grep-invert pattern built from
	// the cooperatesWith tags of the index-0 backend's specs that are not in
	// TCKTags, sorted; "" when every cooperating tag is installed.
	TCKGrepInvert string
	// TCKEnv is the union of the index-0 backend's specs' tckEnv.
	TCKEnv   map[string]string
	GoModule string
	// Reference is the slot map of the catalog: the value every name rule
	// matches in the source files.
	Reference map[string]string
	// Slots holds a value for every slot a rule of a delivered file uses, live
	// or not, plus the project, component and derived slots. A multi project
	// holds the slots of the index-0 backend and its frontend.
	Slots map[string]string
	// Live is the set of live slots: the project, the component slot of every
	// selected stack and the slots of every live rule (index-0 instance).
	Live map[string]bool
	// Ports is the effective port layout.
	Ports PortPlan
	// Notes lists the defaults a multi project derived, one line per kind,
	// without the "note: " prefix.
	Notes []string
}

// ComponentPlan is one delivered component: a stack copied into one folder
// with its own modules, Go module, names and ports.
type ComponentPlan struct {
	// Stack is the stack id, "go-srv".
	Stack string
	// Role is "backend" or "frontend", from the stack table.
	Role string
	// Name is the component and folder name, "acme-srv-second".
	Name string
	// Index is the position among the components of Stack.
	Index int
	// Partner is the bound backend of a frontend, the bound frontend of a
	// backend, or "".
	Partner string
	// Modules are the installed modules in plan order, core first while core exists.
	Modules []catalog.ModuleEntry
	// GoModule is the target Go module path; "" for a stack without one.
	GoModule string
	// Database is the effective {Name, User} of a backend; zero when no database
	// slot is live.
	Database manifest.Database
	// Services maps the live service keys of a backend to their effective names.
	Services map[string]string
	// Slots holds the slot values of the component's instance: the values of
	// the backend and its bound frontend, the instance slots in their single
	// or multi form, and a value for every slot a rule of the project uses.
	Slots map[string]string
	// Ports maps each declared port of the installed modules to its host port.
	Ports map[string]int
	// Origin is the public base URL of a backend.
	Origin string
	// TCK is the TCK selection of a backend with at least one spec, else nil.
	TCK *TCKPlan
}

// TCKPlan is the TCK selection of one backend component.
type TCKPlan struct {
	// Tags are the tags of the specs its modules implement, sorted.
	Tags []string
	// Grep is Grep(Tags).
	Grep string
	// GrepInvert holds the cooperation tags of its specs that it does not
	// install; "" when none.
	GrepInvert string
	// Env is the union of its specs' tckEnv.
	Env map[string]string
}

// Multi reports whether the plan has two or more backend components.
func (p Plan) Multi() bool {
	backends := 0
	for _, component := range p.Components {
		if component.Role == roleBackend {
			backends++
		}
	}
	return backends > 1
}

// Component returns the single component of a stack, and false when the stack
// is not selected or has several components.
func (p Plan) Component(stack string) (ComponentPlan, bool) {
	var found ComponentPlan
	count := 0
	for _, component := range p.Components {
		if component.Stack == stack {
			found = component
			count++
		}
	}
	if count != 1 {
		return ComponentPlan{}, false
	}
	return found, true
}

// origin is where an id of the resolved set came from, for issue paths.
type origin struct {
	path string
	// component names the backend component of an id that a top-level list
	// gave in a multi project.
	component string
}

type resolver struct {
	cat    catalog.Catalog
	sel    manifest.Selection
	issues []issue.Issue
	// multi is true with two or more backend components.
	multi bool
	// undetermined is true when a component has no name or no resolvable
	// backend: the component checks report it and the plan skips that part.
	undetermined bool
}

// UndeterminedMessage is the INTERNAL_ERROR message of a selection whose
// components have no name or no backend: the component checks report those, so
// a caller that ran them drops this issue.
const UndeterminedMessage = "internal error: a component has no name or no backend"

// Resolve returns the modules per component, the specs, TCK tags and TCK env,
// or every issue found. With any issue the returned Plan is the zero value.
func Resolve(cat catalog.Catalog, sel manifest.Selection) (Plan, []issue.Issue) {
	r := &resolver{cat: cat, sel: sel}
	result := r.run()
	if len(r.issues) > 0 {
		r.issues = uniqueIssues(r.issues)
		issue.Sort(r.issues)
		return Plan{}, r.issues
	}
	return result, nil
}

// uniqueIssues drops an issue equal to an earlier one: the pairs of a multi
// project share catalog issues.
func uniqueIssues(issues []issue.Issue) []issue.Issue {
	seen := map[string]bool{}
	kept := make([]issue.Issue, 0, len(issues))
	for _, found := range issues {
		key := found.Code + "\x00" + found.Path + "\x00" + found.Message + "\x00" + found.Hint
		if seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, found)
	}
	return kept
}

func (r *resolver) add(code, source, pointer, message string) {
	r.issues = append(r.issues, issue.Issue{Code: code, Path: issue.Path(source, pointer), Message: message})
}

func (r *resolver) run() Plan {
	if r.cat.Generator != generator.Version {
		r.add(CodeGeneratorMismatch, catalogSource, "/generator",
			fmt.Sprintf("catalog was built by generator %q, this is %q", r.cat.Generator, generator.Version))
	}
	r.multi = r.backendCount() > 1
	r.checkNames()
	selected := r.selectedStacks()
	if selected == nil {
		return Plan{}
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
	var pairs []pairPlan
	for _, input := range r.pairInputs() {
		pairs = append(pairs, r.planPair(selected, input))
	}
	if len(pairs) == 0 {
		return Plan{}
	}
	components := flatten(pairs)
	r.checkFolders(components, r.wantedSpecs(components))
	for i := range pairs {
		r.nameSlots(&pairs[i], components)
	}
	r.checkInfraNames(pairs)
	if len(r.issues) > 0 {
		return Plan{}
	}
	if r.undetermined {
		r.add(CodeInternalError, selectionSource, "/components", UndeterminedMessage)
		return Plan{}
	}
	return r.assemble(pairs)
}

// backendCount returns the number of backend components of the input.
func (r *resolver) backendCount() int {
	count := 0
	for _, input := range r.componentInputs() {
		if input.role == roleBackend {
			count++
		}
	}
	return count
}

// selectedStacks returns the catalog indexes of the selected stacks, or nil
// after reporting why the stack choice is unusable.
func (r *resolver) selectedStacks() []int {
	start := len(r.issues)
	var selected []int
	seen := map[string]bool{}
	for i, name := range r.sel.Stacks {
		pointer := fmt.Sprintf("/stacks/%d", i)
		if seen[name] {
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
		selected = append(selected, index)
	}
	if len(r.issues) > start {
		return nil
	}
	r.issues = append(r.issues, checkStackRoles(r.sel, r.catalogRole)...)
	if len(r.issues) > start {
		return nil
	}
	return selected
}

// closure computes step 1 and 2 for one pair: implied ids and the starting
// list, closed under requiresModules in the pair's stacks. The map value
// records where the id first came from. component names the backend.
func (r *resolver) closure(selected []int, start startList, component string) map[string]origin {
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
	if start.all {
		for _, entry := range selected {
			if r.cat.Stacks[entry].Role != roleBackend {
				continue
			}
			for _, module := range r.cat.Stacks[entry].Modules {
				push(module.Module, origin{})
			}
		}
	}
	for i, id := range start.ids {
		from := origin{path: issue.Path(selectionSource, fmt.Sprintf("%s/%d", start.pointer, i))}
		if start.inherited && r.multi {
			from.component = component
		}
		push(id, from)
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
		code, message, _ := moduleMissing(r.cat, backend, id)
		path := closure[id].path
		if path == "" {
			path = issue.Path(catalogSource, fmt.Sprintf("/stacks/%d", backendEntry))
		}
		if component := closure[id].component; component != "" {
			message = fmt.Sprintf("%s (backend component %s)", message, component)
		}
		r.issues = append(r.issues, issue.Issue{Code: code, Path: path, Message: message})
	}
}

// planStack applies steps 4 and 5 to one stack.
func (r *resolver) planStack(entry int, closure map[string]origin) ComponentPlan {
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
	role := stack.Role
	if info, known := manifest.LookupStack(stack.Stack); known {
		role = info.Role
	}
	return ComponentPlan{Stack: stack.Stack, Role: role, Modules: modules}
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

// specsOf returns the specs the modules implement.
func specsOf(modules []catalog.ModuleEntry) map[string]bool {
	specs := map[string]bool{}
	for _, module := range modules {
		for spec := range module.ImplementsSpec {
			specs[spec] = true
		}
	}
	return specs
}

// checkFrontendSpecs reports SPEC_NOT_IN_BACKEND for a spec that a module of
// the frontend implements and the backend of its pair does not.
func (r *resolver) checkFrontendSpecs(backend, frontend ComponentPlan, entry int) {
	backendSpecs := specsOf(backend.Modules)
	stack := r.cat.Stacks[entry]
	for _, module := range frontend.Modules {
		for _, spec := range sortedKeys(module.ImplementsSpec) {
			if backendSpecs[spec] {
				continue
			}
			position := findModule(stack, module.Module)
			pointer := fmt.Sprintf("/stacks/%d/modules/%d/implementsSpec/%s", entry, position, strings.NewReplacer("~", "~0", "/", "~1").Replace(spec))
			found := issue.Issue{Code: CodeSpecNotInBackend, Path: issue.Path(catalogSource, pointer)}
			found.Message, found.Hint = r.specNotInBackendMessage(spec, backend, frontend, module.Module)
			r.issues = append(r.issues, found)
		}
	}
}

// specNotInBackendMessage is the SPEC_NOT_IN_BACKEND text and hint. A single
// project names the backend catalog modules that implement spec, when there are
// any; a multi project names both components.
func (r *resolver) specNotInBackendMessage(spec string, backend, frontend ComponentPlan, module string) (message, hint string) {
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
	sort.Strings(implementers)
	list := strings.Join(implementers, ",")
	if r.multi {
		message = fmt.Sprintf("no module of backend component %s implements spec %q (needed by %s %s in %s)",
			backend.Name, spec, frontend.Stack, module, frontend.Name)
		if len(implementers) > 0 {
			hint = fmt.Sprintf("select module %q in %s or bind %s to another backend", list, backend.Name, frontend.Name)
		}
		return message, hint
	}
	message = fmt.Sprintf("no selected backend module implements spec %q (needed by %s %s)", spec, frontend.Stack, module)
	if len(implementers) > 0 {
		message = fmt.Sprintf("%s; select module %q or drop stack %s", message, list, frontend.Stack)
	}
	return message, ""
}

// tckOf returns the TCK selection of the specs wanted by one backend, the
// catalog entries of those specs in catalog order and their tags. It reports
// the catalog problems of those specs; the entries still carry every file.
func (r *resolver) tckOf(wanted map[string]bool) (tck *TCKPlan, specs []catalog.SpecEntry, tagSet map[string]bool) {
	tagSet = map[string]bool{}
	env := map[string]string{}
	envFrom := map[string]string{}
	remaining := map[string]bool{}
	for spec := range wanted {
		remaining[spec] = true
	}
	for i, spec := range r.cat.Specs {
		if !wanted[spec.Spec] {
			continue
		}
		delete(remaining, spec.Spec)
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
	for _, spec := range sortedKeys(remaining) {
		r.add(catalog.CodeSpecUnknown, catalogSource, "/specs", fmt.Sprintf("spec %q is implemented by a module but not in the catalog", spec))
	}
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
	tags := sortedKeys(tagSet)
	tck = &TCKPlan{Tags: tags, Grep: Grep(tags), GrepInvert: Grep(sortedKeys(invertSet)), Env: env}
	return tck, specs, tagSet
}

// assemble applies step 6 and builds the Plan.
func (r *resolver) assemble(pairs []pairPlan) Plan {
	unionWanted := map[string]bool{}
	unionTags := map[string]bool{}
	var firstTCK *TCKPlan
	lists := make([][]string, 0, len(pairs))
	for i := range pairs {
		backend := &pairs[i].backend
		wanted := specsOf(backend.Modules)
		for spec := range wanted {
			unionWanted[spec] = true
		}
		tck, specs, tags := r.tckOf(wanted)
		for tag := range tags {
			unionTags[tag] = true
		}
		if len(specs) > 0 {
			backend.TCK = tck
		}
		if i == 0 {
			firstTCK = tck
		}
		installed := make([]string, 0, len(backend.Modules))
		for _, module := range backend.Modules {
			installed = append(installed, module.Module)
		}
		lists = append(lists, installed)
	}
	var specs []catalog.SpecEntry
	for _, spec := range r.cat.Specs {
		if unionWanted[spec.Spec] {
			spec.Files = installedFiles(spec.Files, unionTags)
			specs = append(specs, spec)
		}
	}
	components := flatten(pairs)
	portPlan := r.portPlan(components)
	r.fillPorts(components, portPlan)
	tags, grep, invert, env := []string{}, "", "", map[string]string{}
	if firstTCK != nil {
		tags, grep, invert, env = firstTCK.Tags, firstTCK.Grep, firstTCK.GrepInvert, firstTCK.Env
	}
	stackNames := make([]string, 0, len(components))
	for _, component := range components {
		if len(stackNames) == 0 || stackNames[len(stackNames)-1] != component.Stack {
			stackNames = append(stackNames, component.Stack)
		}
	}
	return Plan{
		Selection:     r.recordedSelection(stackNames, lists),
		Components:    components,
		Specs:         specs,
		TCKTags:       tags,
		TCKGrep:       grep,
		TCKGrepInvert: invert,
		TCKEnv:        env,
		GoModule:      r.cat.GoModule,
		Reference:     r.cat.Reference,
		Slots:         pairs[0].values,
		Live:          pairs[0].live,
		Ports:         portPlan,
		Notes:         r.derivedNotes(pairs),
	}
}

// fillPorts sets the host ports and the public origin of every component from
// the effective band.
func (r *resolver) fillPorts(components []ComponentPlan, layout PortPlan) {
	for i := range components {
		component := &components[i]
		component.Ports = map[string]int{}
		for _, module := range component.Modules {
			for _, port := range module.Ports {
				component.Ports[port.Name] = layout.Band*100 + port.Offset
			}
		}
		if component.Role == roleBackend {
			component.Origin = "http://localhost:" + strconv.Itoa(layout.Origin)
		}
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

// Pull is a module the requirement closure added to one component.
type Pull struct {
	// Module is the pulled module id.
	Module string
	// RequiredBy lists the present modules of the component whose requiresModules
	// names Module, sorted bytewise.
	RequiredBy []string
	// Stack is the stack name.
	Stack string
	// Component is the component name in a multi project, "" in a single one.
	Component string
}

// Pulls returns the modules of p that the requirement closure added, ordered
// by component then module. A module is typed when the list its backend
// started from names it (a frontend follows its backend). It returns nil for
// a backend that started from --all-modules.
func Pulls(p Plan, sel manifest.Selection) []Pull {
	var pulls []Pull
	multi := p.Multi()
	for _, component := range p.Components {
		typed, expanded := typedModules(p, sel, component, multi)
		if expanded {
			continue
		}
		requirers := map[string][]string{}
		for _, module := range component.Modules {
			for _, requirement := range module.RequiresModules {
				requirers[requirement.Module] = append(requirers[requirement.Module], module.Module)
			}
		}
		var found []Pull
		for _, module := range component.Modules {
			by := requirers[module.Module]
			if typed[module.Module] || len(by) == 0 {
				continue
			}
			sort.Strings(by)
			pull := Pull{Module: module.Module, RequiredBy: by, Stack: component.Stack}
			if multi {
				pull.Component = component.Name
			}
			found = append(found, pull)
		}
		sort.Slice(found, func(i, j int) bool { return found[i].Module < found[j].Module })
		pulls = append(pulls, found...)
	}
	return pulls
}

// typedModules returns the ids the person listed for the backend of
// component, core included, and whether the backend started from
// --all-modules.
func typedModules(p Plan, sel manifest.Selection, component ComponentPlan, multi bool) (typed map[string]bool, expanded bool) {
	typed = map[string]bool{coreID: true}
	ids, all := sel.Modules, sel.AllModules
	if multi {
		backend := component
		if component.Role != roleBackend {
			for _, candidate := range p.Components {
				if candidate.Name == component.Partner {
					backend = candidate
				}
			}
		}
		if entry, has := backendEntry(sel, backend.Index); has && entry.HasModules {
			ids, all = entry.Modules, false
		}
	}
	for _, id := range ids {
		typed[id] = true
	}
	return typed, all
}

// backendEntry returns the k-th entry of a backend stack.
func backendEntry(sel manifest.Selection, k int) (manifest.Component, bool) {
	seen := 0
	for _, entry := range sel.Components {
		if !manifest.IsBackendStack(entry.Stack) {
			continue
		}
		if seen == k {
			return entry, true
		}
		seen++
	}
	return manifest.Component{}, false
}

// catalogRole returns the role the catalog gives a stack, "" when it has none.
func (r *resolver) catalogRole(stack string) string {
	for _, entry := range r.cat.Stacks {
		if entry.Stack == stack {
			return entry.Role
		}
	}
	return ""
}
