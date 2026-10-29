package plan

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
)

// MaxBackendComponents is the most backend components (and frontends) one
// project may have.
const MaxBackendComponents = 7

// Issue codes of the component input checks.
const (
	CodeComponentNameRequired    = "COMPONENT_NAME_REQUIRED"
	CodeComponentLimit           = "COMPONENT_LIMIT"
	CodeComponentFieldNotAllowed = "COMPONENT_FIELD_NOT_ALLOWED"
	CodeComponentBackendRequired = "COMPONENT_BACKEND_REQUIRED"
	CodeComponentBackendUnknown  = "COMPONENT_BACKEND_UNKNOWN"
	CodeComponentBackendTaken    = "COMPONENT_BACKEND_TAKEN"
	CodeComponentAmbiguous       = "COMPONENT_AMBIGUOUS"
	CodeModulesUnused            = "MODULES_UNUSED"
)

// ordinals name the k-th component of a stack in a suggested name.
var ordinals = []string{"first", "second", "third", "fourth", "fifth", "sixth", "seventh"}

// instance is one component of the project: an entry of components, or the
// implicit component of a stack no entry names.
type instance struct {
	stack, role string
	// entry is the index in components, -1 for an implicit component.
	entry     int
	component manifest.Component
	// name is the effective name; empty when the stack has several components
	// and this one is unnamed.
	name string
	// pointer is the input that stands for the component.
	pointer string
}

// componentCheck holds the state of one ResolveComponents run.
type componentCheck struct {
	sel       manifest.Selection
	in        manifest.Input
	cat       catalog.Catalog
	instances []instance
	byStack   map[string][]int
	issues    []issue.Issue
}

// ResolveComponents runs the checks of the component input on a resolved input
// (defaults filled): the shape and default checks, which need no catalog but
// its service keys, then the module ids against the catalog. It collects every
// issue; paths use the selection source and are relocated by the caller.
func ResolveComponents(cat catalog.Catalog, in manifest.Input) []issue.Issue {
	c := &componentCheck{sel: in.Selection, in: in, cat: cat, byStack: map[string][]int{}}
	c.collect()
	c.nameRequired()
	c.limits()
	c.fieldsNotAllowed()
	c.binding()
	c.ambiguousDatabase()
	c.modulesUnused()
	c.overrides()
	c.givenNames()
	c.issues = append(c.issues, checkStackRoles(c.sel, tableRole)...)
	c.issues = append(c.issues, moduleIssues(cat, c.sel)...)
	issue.Sort(c.issues)
	return c.issues
}

func (c *componentCheck) add(code, pointer, message, hint string) {
	c.issues = append(c.issues, issue.Issue{Code: code, Path: issue.Path(selectionSource, pointer), Message: message, Hint: hint})
}

// collect lists the components: the entries of every selected known stack in
// entry order, else the implicit one.
func (c *componentCheck) collect() {
	seen := map[string]bool{}
	for position, stack := range c.sel.Stacks {
		info, known := manifest.LookupStack(stack)
		if !known || seen[stack] {
			continue
		}
		seen[stack] = true
		var entries []int
		for i, component := range c.sel.Components {
			if component.Stack == stack {
				entries = append(entries, i)
			}
		}
		if len(entries) == 0 {
			c.push(instance{stack: stack, role: info.Role, entry: -1, pointer: fmt.Sprintf("/stacks/%d", position)})
			continue
		}
		for _, i := range entries {
			c.push(instance{stack: stack, role: info.Role, entry: i, component: c.sel.Components[i], pointer: fmt.Sprintf("/components/%d", i)})
		}
	}
	for stack, indexes := range c.byStack {
		for _, at := range indexes {
			inst := &c.instances[at]
			inst.name = inst.component.Name
			if inst.name == "" && len(indexes) == 1 {
				inst.name = defaultComponentName(c.sel.Project.Name, stack)
			}
		}
	}
}

func (c *componentCheck) push(inst instance) {
	c.byStack[inst.stack] = append(c.byStack[inst.stack], len(c.instances))
	c.instances = append(c.instances, inst)
}

// ofRole returns the components of a role in order.
func (c *componentCheck) ofRole(role string) []instance {
	var found []instance
	for _, inst := range c.instances {
		if inst.role == role {
			found = append(found, inst)
		}
	}
	return found
}

// isMulti reports two or more components of one stack.
func (c *componentCheck) isMulti() bool {
	for _, indexes := range c.byStack {
		if len(indexes) > 1 {
			return true
		}
	}
	return false
}

// nameRequired reports a stack with several components and an unnamed one.
func (c *componentCheck) nameRequired() {
	for _, stack := range c.stackOrder() {
		indexes := c.byStack[stack]
		if len(indexes) < 2 {
			continue
		}
		first := -1
		for _, at := range indexes {
			if c.instances[at].component.Name == "" && first < 0 {
				first = at
			}
		}
		if first < 0 {
			continue
		}
		found := issue.Issue{
			Code:           CodeComponentNameRequired,
			Path:           issue.Path(selectionSource, c.instances[first].pointer),
			Message:        fmt.Sprintf("stack %s has %d components; name each one", stack, len(indexes)),
			SuggestedNames: c.suggestNames(stack),
		}
		c.issues = append(c.issues, found)
	}
}

// stackOrder returns the stacks of the components in first-appearance order.
func (c *componentCheck) stackOrder() []string {
	var order []string
	seen := map[string]bool{}
	for _, inst := range c.instances {
		if !seen[inst.stack] {
			seen[inst.stack] = true
			order = append(order, inst.stack)
		}
	}
	return order
}

// suggestNames returns one name per component of stack: the given name, else
// <project>-<suffix>-<ordinal> with the next free ordinal.
func (c *componentCheck) suggestNames(stack string) []string {
	used := map[string]bool{}
	for _, component := range c.sel.Components {
		if component.Name != "" {
			used[component.Name] = true
		}
	}
	info, _ := manifest.LookupStack(stack)
	var suggested []string
	for k, at := range c.byStack[stack] {
		given := c.instances[at].component.Name
		if given != "" {
			suggested = append(suggested, given)
			continue
		}
		ordinal := k
		candidate := suggestedName(c.sel.Project.Name, info.Suffix, ordinal)
		for used[candidate] {
			ordinal++
			candidate = suggestedName(c.sel.Project.Name, info.Suffix, ordinal)
		}
		used[candidate] = true
		suggested = append(suggested, candidate)
	}
	return suggested
}

// suggestedName builds <project>-<suffix>-<ordinal> for the 0-based ordinal,
// cutting the project part when the name would pass 63 bytes.
func suggestedName(project, suffix string, ordinal int) string {
	word := strconv.Itoa(ordinal + 1)
	if ordinal < len(ordinals) {
		word = ordinals[ordinal]
	}
	tail := "-" + suffix + "-" + word
	if len(project)+len(tail) <= maxNameLength {
		return project + tail
	}
	room := max(0, maxNameLength-len(tail))
	cut := strings.TrimRight(project[:min(room, len(project))], "-")
	if cut == "" {
		return strings.TrimPrefix(tail, "-")
	}
	return cut + tail
}

// limits reports more than MaxBackendComponents components of a role.
func (c *componentCheck) limits() {
	for _, role := range []string{roleBackend, roleFrontend} {
		found := c.ofRole(role)
		if len(found) > MaxBackendComponents {
			c.add(CodeComponentLimit, found[MaxBackendComponents].pointer,
				fmt.Sprintf("%d %s components; a project has at most %d", len(found), role, MaxBackendComponents), "")
		}
	}
}

// fieldsNotAllowed reports the entry keys the component's role does not take.
func (c *componentCheck) fieldsNotAllowed() {
	for _, inst := range c.instances {
		if inst.entry < 0 {
			continue
		}
		info, _ := manifest.LookupStack(inst.stack)
		component := inst.component
		report := func(field, message string) {
			c.add(CodeComponentFieldNotAllowed, inst.pointer+"/"+field, message, "")
		}
		if inst.role == roleFrontend {
			if component.HasModules {
				report("modules", fmt.Sprintf("%s components take no modules; they install what their backend installs", inst.stack))
			}
			if component.Database != nil {
				report("database", fmt.Sprintf("%s components take no database", inst.stack))
			}
			if len(component.Services) > 0 {
				report("services", fmt.Sprintf("%s components take no services", inst.stack))
			}
		}
		if inst.role == roleBackend && component.Backend != "" {
			report("backend", fmt.Sprintf("%s components take no backend; only a frontend names its backend", inst.stack))
		}
		if component.GoModule != "" && !info.GoModule {
			report("goModule", fmt.Sprintf("%s components need no Go module", inst.stack))
		}
	}
}

// binding reports the frontends' backend: required, unknown and taken.
func (c *componentCheck) binding() {
	backends := c.ofRole(roleBackend)
	byName := map[string]bool{}
	var backendNames []string
	for _, backend := range backends {
		if backend.name != "" {
			byName[backend.name] = true
			backendNames = append(backendNames, backend.name)
		}
	}
	taken := map[string]string{}
	for _, frontend := range c.ofRole(roleFrontend) {
		given := frontend.component.Backend
		bound := ""
		switch {
		case given != "" && !byName[given]:
			hint := "backend components: " + strings.Join(backendNames, ", ")
			if closest := names.Closest(given, backendNames); closest != "" {
				hint = fmt.Sprintf("did you mean %q?", closest)
			}
			c.add(CodeComponentBackendUnknown, frontend.pointer+"/backend",
				fmt.Sprintf("%q names no backend component", given), hint)
			continue
		case given != "":
			bound = given
		case len(backends) == 1:
			bound = backends[0].name
		case len(backends) > 1:
			c.add(CodeComponentBackendRequired, frontend.pointer,
				fmt.Sprintf("%s has %d backend components; name the one it uses", frontend.stack, len(backends)),
				"set backend to one of: "+strings.Join(backendNames, ", "))
			continue
		}
		if bound == "" {
			continue
		}
		first, isTaken := taken[bound]
		if isTaken {
			c.add(CodeComponentBackendTaken, frontend.pointer,
				fmt.Sprintf("backend component %q already has the frontend %q", bound, first), "")
			continue
		}
		label := frontend.name
		if label == "" {
			label = frontend.stack
		}
		taken[bound] = label
	}
}

// ambiguousDatabase reports a top-level database that several backends inherit.
func (c *componentCheck) ambiguousDatabase() {
	if c.sel.Database == nil {
		return
	}
	inheriting := 0
	for _, backend := range c.ofRole(roleBackend) {
		if backend.entry < 0 || backend.component.Database == nil {
			inheriting++
		}
	}
	if inheriting < 2 {
		return
	}
	pointer := "/database"
	for _, candidate := range []string{"/database/name", "/database/user"} {
		if _, has := c.in.Origins[candidate]; has {
			pointer = candidate
			break
		}
	}
	c.add(CodeComponentAmbiguous, pointer,
		fmt.Sprintf("the top-level database would be inherited by %d backend components", inheriting), "")
}

// modulesUnused reports a top-level module list that no backend starts from.
func (c *componentCheck) modulesUnused() {
	hasTop := len(c.sel.Modules) > 0 || c.sel.AllModules
	backends := c.ofRole(roleBackend)
	if !hasTop || len(backends) == 0 {
		return
	}
	for _, backend := range backends {
		if backend.entry < 0 || !backend.component.HasModules {
			return
		}
	}
	message := "every backend component has its own modules; the top-level modules start no component"
	if c.sel.AllModules {
		c.issues = append(c.issues, issue.Issue{
			Code: CodeModulesUnused, Path: issue.Path(manifest.FlagsSource, "/all-modules"), Message: message,
		})
		return
	}
	c.add(CodeModulesUnused, "/modules", message, "")
}

// overrides reports NAME_OVERRIDE_UNUSED for a component of an unselected
// stack and, in a multi project, for top-level services and unknown entry
// service keys.
func (c *componentCheck) overrides() {
	c.issues = append(c.issues, componentStackIssues(c.sel)...)
	backends := len(c.ofRole(roleBackend))
	if backends < 2 {
		return
	}
	for _, key := range sortedKeys(c.sel.Services) {
		pointer := "/services" + issue.Pointer(key)
		c.add(CodeNameOverrideUnused, pointer,
			fmt.Sprintf("%s: services apply to one backend and this project has %d", pointer, backends),
			fmt.Sprintf("per component: --component go-srv=<name>,service.%s=<value>", key))
	}
	keys := serviceKeysOf(c.cat)
	for _, inst := range c.instances {
		for _, key := range sortedKeys(inst.component.Services) {
			if _, isKnown := c.cat.Reference[names.ServiceSlot(key)]; isKnown {
				continue
			}
			pointer := inst.pointer + "/services" + issue.Pointer(key)
			hint := ""
			if closest := names.Closest(key, keys); closest != "" {
				hint = fmt.Sprintf("did you mean %q?", closest)
			}
			c.add(CodeNameOverrideUnused, pointer,
				fmt.Sprintf("%s: no service key %q; keys: %s", pointer, key, strings.Join(keys, ", ")), hint)
		}
	}
}

// givenNames reports, in a multi project, two components with one given name
// and a component name that a service already uses.
func (c *componentCheck) givenNames() {
	if !c.isMulti() {
		return
	}
	byName := map[string][]instance{}
	for _, inst := range c.instances {
		if inst.component.Name != "" {
			byName[inst.component.Name] = append(byName[inst.component.Name], inst)
		}
	}
	serviceNames := map[string]bool{}
	for _, value := range c.sel.Services {
		serviceNames[value] = true
	}
	for _, inst := range c.instances {
		for _, value := range inst.component.Services {
			serviceNames[value] = true
		}
	}
	for _, name := range sortedKeys(byName) {
		group := byName[name]
		if len(group) > 1 {
			for _, inst := range group {
				c.add(CodeNameConflict, inst.pointer+"/name",
					fmt.Sprintf("%d components are named %q", len(group), name), "set one of them to another name")
			}
		}
		if serviceNames[name] {
			for _, inst := range group {
				c.add(CodeNameConflict, inst.pointer+"/name",
					fmt.Sprintf("%q is also the name given to a compose service", name), "set one of them to another name")
			}
		}
	}
}

// componentStackIssues reports NAME_OVERRIDE_UNUSED for a component of a stack
// that is unknown or not selected.
func componentStackIssues(sel manifest.Selection) []issue.Issue {
	selected := map[string]bool{}
	for _, stack := range sel.Stacks {
		selected[stack] = true
	}
	var tableIDs []string
	for _, info := range manifest.Stacks() {
		tableIDs = append(tableIDs, info.ID)
	}
	var issues []issue.Issue
	for i, component := range sel.Components {
		if selected[component.Stack] && isKnownStack(component.Stack) {
			continue
		}
		pointer := fmt.Sprintf("/components/%d", i)
		found := issue.Issue{
			Code:    CodeNameOverrideUnused,
			Path:    issue.Path(selectionSource, pointer+"/stack"),
			Message: fmt.Sprintf("%s: stack %s is not selected", pointer, quoteStack(component.Stack)),
		}
		if closest := names.Closest(component.Stack, tableIDs); closest != "" {
			found.Hint = fmt.Sprintf("did you mean %q?", closest)
		} else if isKnownStack(component.Stack) {
			found.Hint = fmt.Sprintf("add %s to stacks", component.Stack)
		}
		issues = append(issues, found)
	}
	return issues
}

// serviceKeysOf returns the sorted service keys of the catalog reference.
func serviceKeysOf(cat catalog.Catalog) []string {
	var keys []string
	for slot := range cat.Reference {
		if key, isService := strings.CutPrefix(slot, serviceSlotPrefix); isService {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// checkStackRoles owns every refusal of the stack roles: a stack listed twice,
// no backend component, a frontend with no backend and more than one stack of
// a role. Paths use the selection source.
func checkStackRoles(sel manifest.Selection, roleOf func(stack string) string) []issue.Issue {
	var found []issue.Issue
	add := func(pointer, message, hint string) {
		found = append(found, issue.Issue{
			Code: manifest.CodeSelectionInvalid, Path: issue.Path(selectionSource, pointer), Message: message, Hint: hint,
		})
	}
	seen := map[string]bool{}
	backends, frontends := 0, 0
	var frontendPointers []string
	for i, stack := range sel.Stacks {
		pointer := fmt.Sprintf("/stacks/%d", i)
		if seen[stack] {
			add(pointer, "stack is listed twice", fmt.Sprintf("to ask for two %s components, repeat --component %s=NAME", stack, stack))
			continue
		}
		seen[stack] = true
		switch roleOf(stack) {
		case roleBackend:
			backends++
		case roleFrontend:
			frontends++
			frontendPointers = append(frontendPointers, pointer)
		}
	}
	if backends == 0 {
		add("/stacks", "must contain at least one backend component (a project without one arrives with gap 4)", "")
		for _, pointer := range frontendPointers {
			add(pointer, "frontend component has no backend to bind to", "")
		}
	}
	if backends > 1 {
		add("/stacks", "must contain exactly one backend stack", "")
	}
	if frontends > 1 {
		add("/stacks", "must contain at most one frontend stack", "")
	}
	return found
}

// tableRole returns the role of a stack of the stack table, "" when unknown.
func tableRole(stack string) string {
	info, _ := manifest.LookupStack(stack)
	return info.Role
}

// moduleMissing returns the code and message of a module id the backend stack
// does not have, and false when it has it.
func moduleMissing(cat catalog.Catalog, backend catalog.StackEntry, id string) (code, message string, missing bool) {
	if findModule(backend, id) >= 0 {
		return "", "", false
	}
	code, message = CodeModuleUnknown, "module is not in the catalog"
	for _, stack := range cat.Stacks {
		if findModule(stack, id) >= 0 {
			code, message = catalog.CodeModuleNotInBackend, "module has no manifest in the backend stack"
		}
	}
	return code, fmt.Sprintf("%s: %q", message, id), true
}

// moduleIssues reports MODULE_UNKNOWN for the ids of the top-level list and of
// every backend entry's list.
func moduleIssues(cat catalog.Catalog, sel manifest.Selection) []issue.Issue {
	var backend *catalog.StackEntry
	for _, stack := range sel.Stacks {
		for i := range cat.Stacks {
			if cat.Stacks[i].Stack == stack && cat.Stacks[i].Role == roleBackend {
				backend = &cat.Stacks[i]
			}
		}
	}
	if backend == nil {
		return nil
	}
	var found []issue.Issue
	check := func(pointer, id string) {
		if code, message, missing := moduleMissing(cat, *backend, id); missing {
			found = append(found, issue.Issue{Code: code, Path: issue.Path(selectionSource, pointer), Message: message})
		}
	}
	for i, id := range sel.Modules {
		check(fmt.Sprintf("/modules/%d", i), id)
	}
	for i, component := range sel.Components {
		if !manifest.IsBackendStack(component.Stack) {
			continue
		}
		for j, id := range component.Modules {
			check(fmt.Sprintf("/components/%d/modules/%d", i, j), id)
		}
	}
	return found
}
