package plan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
)

// Issue codes of the component names.
const (
	CodeNameReserved       = "NAME_RESERVED"
	CodeNameConflict       = "NAME_CONFLICT"
	CodeNameTooLong        = "NAME_TOO_LONG"
	CodeNameOverrideUnused = "NAME_OVERRIDE_UNUSED"
	// CodeComponentMultiUnsupported is raised by the command before Resolve;
	// Resolve never receives two entries of one stack.
	CodeComponentMultiUnsupported = "COMPONENT_MULTI_UNSUPPORTED"
	// CodeInternalError reports an input that the command should have refused.
	CodeInternalError = "INTERNAL_ERROR"
)

// maxNameLength is the longest derived name any live value may have.
const maxNameLength = 63

// ProjectPath maps a delivered source-form path to its project path: a path
// under the source root of a selected stack moves under that stack's
// component folder, every other path is returned unchanged.
func (p Plan) ProjectPath(sourceForm string) string {
	for _, stack := range p.Stacks {
		info, known := manifest.LookupStack(stack.Stack)
		component, hasComponent := p.Component(stack.Stack)
		isUnder := known && strings.HasPrefix(sourceForm, info.Root)
		if isUnder && hasComponent {
			return component.Name + "/" + strings.TrimPrefix(sourceForm, info.Root)
		}
	}
	return sourceForm
}

// NameTooLong reports NAME_TOO_LONG when a derived value passes 63 bytes. what
// names the value ("component folder"), pointer is the input it derives from.
func NameTooLong(source, pointer, what, value string) (issue.Issue, bool) {
	if len(value) <= maxNameLength {
		return issue.Issue{}, false
	}
	found := issue.Issue{
		Code:    CodeNameTooLong,
		Path:    issue.Path(source, pointer),
		Message: fmt.Sprintf("derived %s %q is %d characters (max %d)", what, value, len(value), maxNameLength),
		Hint:    "shorten the name",
	}
	return found, true
}

// defaultComponentName returns the name of the component of stack when no
// entry names it: "<project>-<suffix>" with the suffix of the stack table.
func defaultComponentName(project, stack string) string {
	info, known := manifest.LookupStack(stack)
	if !known {
		return stack
	}
	return project + "-" + info.Suffix
}

// derivedNameTooLong reports NAME_TOO_LONG at /project/name for a default
// component name that passes the limit.
func derivedNameTooLong(source, stack, value string) (issue.Issue, bool) {
	found, isLong := NameTooLong(source, "/project/name", "component folder", value)
	if !isLong {
		return found, false
	}
	info, _ := manifest.LookupStack(stack)
	limit := maxNameLength - len("-"+info.Suffix)
	found.Hint = fmt.Sprintf("shorten the name to %d characters or fewer, or pass --component %s=NAME", limit, stack)
	return found, true
}

// componentInput is the effective component of one selected stack and the
// pointer of the input the name comes from.
type componentInput struct {
	stack string
	name  string
	// given reports whether an entry named the component.
	given bool
	// namePointer is the pointer of the input that holds the name: the name of
	// the entry, else the entry, else the stack in the stacks list.
	namePointer string
}

// entryError reports two entries of one stack, which the command refuses first.
func (r *resolver) entryError() bool {
	seen := map[string]bool{}
	failed := false
	for i, component := range r.sel.Components {
		if seen[component.Stack] {
			failed = true
			r.add(CodeInternalError, selectionSource, fmt.Sprintf("/components/%d", i),
				fmt.Sprintf("internal error: several components of stack %s reached the planner", component.Stack))
		}
		seen[component.Stack] = true
	}
	return failed
}

// componentInputs returns the effective component of every distinct selected
// stack of the stack table, sorted by stack.
func (r *resolver) componentInputs() []componentInput {
	entries := map[string]int{}
	for i, component := range r.sel.Components {
		entries[component.Stack] = i
	}
	seen := map[string]bool{}
	var inputs []componentInput
	for position, stack := range r.sel.Stacks {
		if !isKnownStack(stack) || seen[stack] {
			continue
		}
		seen[stack] = true
		input := componentInput{stack: stack, name: defaultComponentName(r.sel.Project.Name, stack), namePointer: fmt.Sprintf("/stacks/%d", position)}
		if index, hasEntry := entries[stack]; hasEntry {
			input.namePointer = fmt.Sprintf("/components/%d", index)
			if name := r.sel.Components[index].Name; name != "" {
				input.name, input.given = name, true
				input.namePointer += "/name"
			}
		}
		inputs = append(inputs, input)
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].stack < inputs[j].stack })
	return inputs
}

// checkNames reports the name problems that need only the selection:
// NAME_OVERRIDE_UNUSED, NAME_RESERVED for device names, NAME_CONFLICT and
// NAME_TOO_LONG. A value that fails the label grammar was reported by the
// decoder and is skipped here.
func (r *resolver) checkNames() {
	project := r.sel.Project.Name
	if names.ValidLabel(project) && names.IsDeviceName(project) {
		r.add(CodeNameReserved, selectionSource, "/project/name", deviceMessage(project))
	}
	r.checkOverrides()

	byName := map[string][]componentInput{}
	for _, input := range r.componentInputs() {
		if input.given && !names.ValidLabel(input.name) {
			continue
		}
		if input.given && names.IsDeviceName(input.name) {
			r.add(CodeNameReserved, selectionSource, input.namePointer, deviceMessage(input.name))
		}
		if !input.given {
			if !names.ValidLabel(project) {
				continue
			}
			if tooLong, isLong := derivedNameTooLong(selectionSource, input.stack, input.name); isLong {
				r.issues = append(r.issues, tooLong)
				continue
			}
		}
		if tooLong, isLong := NameTooLong(selectionSource, input.namePointer, "component folder", input.name); isLong {
			r.issues = append(r.issues, tooLong)
			continue
		}
		byName[input.name] = append(byName[input.name], input)
	}
	for _, name := range sortedKeys(byName) {
		group := byName[name]
		if len(group) < 2 {
			continue
		}
		labels := make([]string, 0, len(group))
		for _, input := range group {
			labels = append(labels, "component "+input.stack)
		}
		joiner := " and "
		if len(group) > 2 {
			joiner = ", "
		}
		message := fmt.Sprintf("%s all resolve to %q", strings.Join(labels, joiner), name)
		if len(group) == 2 {
			message = fmt.Sprintf("%s both resolve to %q", strings.Join(labels, joiner), name)
		}
		for _, input := range group {
			r.issues = append(r.issues, issue.Issue{
				Code:    CodeNameConflict,
				Path:    issue.Path(selectionSource, input.namePointer),
				Message: message,
				Hint:    "set one of them to another name",
			})
		}
	}
}

// checkOverrides reports NAME_OVERRIDE_UNUSED for a component of a stack that
// is unknown or not selected.
func (r *resolver) checkOverrides() {
	selected := map[string]bool{}
	for _, stack := range r.sel.Stacks {
		selected[stack] = true
	}
	var tableIDs []string
	for _, info := range manifest.Stacks() {
		tableIDs = append(tableIDs, info.ID)
	}
	for i, component := range r.sel.Components {
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
		}
		r.issues = append(r.issues, found)
	}
}

func isKnownStack(stack string) bool {
	_, known := manifest.LookupStack(stack)
	return known
}

// quoteStack renders a stack id for a message; an empty id shows as "".
func quoteStack(stack string) string {
	if stack == "" {
		return `""`
	}
	return stack
}

func deviceMessage(name string) string {
	return fmt.Sprintf("%q is a reserved device name on Windows", name)
}

// projectScopeFolders returns the first segment of every delivered path that
// stays outside the component folders: the paths of the specs and modules that
// are not under the source root of a selected stack.
func projectScopeFolders(stacks []StackPlan, specs []catalog.SpecEntry) map[string]bool {
	var roots []string
	var files []catalog.File
	for _, stack := range stacks {
		if info, known := manifest.LookupStack(stack.Stack); known {
			roots = append(roots, info.Root)
		}
		for _, module := range stack.Modules {
			files = append(files, module.Files...)
		}
	}
	for _, spec := range specs {
		files = append(files, spec.Files...)
	}
	folders := map[string]bool{}
	for _, file := range files {
		dest := file.Path
		if file.Target != "" {
			dest = file.Target
		}
		underRoot := false
		for _, root := range roots {
			underRoot = underRoot || strings.HasPrefix(dest, root)
		}
		first, _, nested := strings.Cut(dest, "/")
		if !underRoot && nested {
			folders[first] = true
		}
	}
	return folders
}

// wantedSpecs returns the catalog specs that a module of the backend plan
// implements.
func (r *resolver) wantedSpecs(backend StackPlan) []catalog.SpecEntry {
	wanted := map[string]bool{}
	for _, module := range backend.Modules {
		for spec := range module.ImplementsSpec {
			wanted[spec] = true
		}
	}
	var specs []catalog.SpecEntry
	for _, spec := range r.cat.Specs {
		if wanted[spec.Spec] {
			specs = append(specs, spec)
		}
	}
	return specs
}

// checkFolders reports NAME_RESERVED for a component name that equals the first
// segment of a path delivered outside the component folders.
func (r *resolver) checkFolders(stacks []StackPlan, specs []catalog.SpecEntry) {
	folders := projectScopeFolders(stacks, specs)
	for _, input := range r.componentInputs() {
		isChecked := input.given && names.ValidLabel(input.name)
		if isChecked && folders[input.name] {
			r.add(CodeNameReserved, selectionSource, input.namePointer,
				fmt.Sprintf("%q is already a folder of the project", input.name))
		}
	}
}

// components returns the plan's components: the given name or the default name,
// one per stack, sorted by stack.
func (r *resolver) components(stacks []StackPlan) []Component {
	given := map[string]string{}
	for _, component := range r.sel.Components {
		given[component.Stack] = component.Name
	}
	components := make([]Component, 0, len(stacks))
	for _, stack := range stacks {
		name := given[stack.Stack]
		if name == "" {
			name = defaultComponentName(r.sel.Project.Name, stack.Stack)
		}
		components = append(components, Component{Stack: stack.Stack, Name: name})
	}
	return components
}

// recordedSelection builds the resolved selection the plan carries: the
// effective project, the sorted stacks, the installed backend modules and the
// components as given. Every list is present; schema 2 only when components
// are given. The recorded document of a run is built by the caller.
func (r *resolver) recordedSelection(stackNames, modules []string) manifest.Selection {
	recorded := manifest.Selection{
		Schema:  1,
		Project: r.sel.Project,
		Stacks:  stackNames,
		Modules: modules,
		Has: manifest.Presence{
			Name:     true,
			GoModule: r.sel.Project.GoModule != "",
			Stacks:   true,
			Modules:  true,
		},
	}
	if len(r.sel.Components) > 0 {
		recorded.Schema = 2
		recorded.Components = append([]manifest.Component(nil), r.sel.Components...)
	}
	if database := r.sel.Database; database != nil {
		copied := *database
		recorded.Database = &copied
		recorded.Schema = 2
	}
	if len(r.sel.Services) > 0 {
		recorded.Services = make(map[string]string, len(r.sel.Services))
		for key, value := range r.sel.Services {
			recorded.Services[key] = value
		}
		recorded.Schema = 2
	}
	if r.sel.Ports != nil {
		copied := *r.sel.Ports
		recorded.Ports = &copied
		recorded.Schema = 2
	}
	return recorded
}
