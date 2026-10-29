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
	// CodeComponentMultiUnsupported is raised by the command after Resolve for
	// a plan with several backend components; Resolve never raises it.
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
	for _, planned := range p.Components {
		info, known := manifest.LookupStack(planned.Stack)
		component, hasComponent := p.Component(planned.Stack)
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

// componentInput is one component of the selection: an entry, or the implicit
// component of a stack no entry names.
type componentInput struct {
	stack string
	role  string
	// index is the position among the components of the stack; entry is the
	// index in components, -1 for an implicit component.
	index, entry int
	// name is the effective name; empty when the stack has several components
	// and this one is unnamed.
	name string
	// given reports whether an entry named the component.
	given bool
	// namePointer is the pointer of the input that holds the name: the name of
	// the entry, else the entry, else the stack in the stacks list.
	namePointer string
	// label names the component in a message.
	label string
}

// componentInputs returns the components of every distinct selected stack of
// the stack table, sorted by (stack, index).
func (r *resolver) componentInputs() []componentInput {
	seen := map[string]bool{}
	var inputs []componentInput
	for position, stack := range r.sel.Stacks {
		info, known := manifest.LookupStack(stack)
		if !known || seen[stack] {
			continue
		}
		seen[stack] = true
		var entries []int
		for i, component := range r.sel.Components {
			if component.Stack == stack {
				entries = append(entries, i)
			}
		}
		if len(entries) == 0 {
			inputs = append(inputs, componentInput{
				stack: stack, role: info.Role, entry: -1, name: defaultComponentName(r.sel.Project.Name, stack),
				namePointer: fmt.Sprintf("/stacks/%d", position), label: "component " + stack,
			})
			continue
		}
		for index, entry := range entries {
			input := componentInput{
				stack: stack, role: info.Role, index: index, entry: entry,
				namePointer: fmt.Sprintf("/components/%d", entry), label: "component " + stack,
			}
			if len(entries) > 1 {
				input.label = fmt.Sprintf("component %s #%d", stack, index+1)
			}
			switch given := r.sel.Components[entry].Name; {
			case given != "":
				input.name, input.given = given, true
				input.namePointer += "/name"
			case len(entries) == 1:
				input.name = defaultComponentName(r.sel.Project.Name, stack)
			}
			inputs = append(inputs, input)
		}
	}
	sort.SliceStable(inputs, func(i, j int) bool { return inputs[i].stack < inputs[j].stack })
	return inputs
}

// checkNames reports the name problems that need only the selection:
// NAME_OVERRIDE_UNUSED, NAME_RESERVED for device names, NAME_CONFLICT and
// NAME_TOO_LONG. A value that fails the label grammar was reported by the
// decoder and is skipped here, and so is a component with no name. In a multi
// project a conflict between given names of one stack is reported by the component checks.
func (r *resolver) checkNames() {
	project := r.sel.Project.Name
	if names.ValidLabel(project) && names.IsDeviceName(project) {
		r.add(CodeNameReserved, selectionSource, "/project/name", deviceMessage(project))
	}
	r.checkOverrides()

	byName := map[string][]componentInput{}
	for _, input := range r.componentInputs() {
		if input.name == "" || (input.given && !names.ValidLabel(input.name)) {
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
		allGiven, sameStack := true, true
		labels := make([]string, 0, len(group))
		for _, input := range group {
			allGiven = allGiven && input.given
			sameStack = sameStack && input.stack == group[0].stack
			labels = append(labels, input.label)
		}
		if r.multi && allGiven && sameStack {
			continue
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
	r.issues = append(r.issues, componentStackIssues(r.sel)...)
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
func projectScopeFolders(stacks []ComponentPlan, specs []catalog.SpecEntry) map[string]bool {
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

// wantedSpecs returns the catalog specs that a module of a backend component
// implements.
func (r *resolver) wantedSpecs(components []ComponentPlan) []catalog.SpecEntry {
	wanted := map[string]bool{}
	for _, component := range components {
		if component.Role != roleBackend {
			continue
		}
		for spec := range specsOf(component.Modules) {
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
func (r *resolver) checkFolders(components []ComponentPlan, specs []catalog.SpecEntry) {
	folders := projectScopeFolders(components, specs)
	for _, input := range r.componentInputs() {
		isChecked := input.given && names.ValidLabel(input.name)
		if isChecked && folders[input.name] {
			r.add(CodeNameReserved, selectionSource, input.namePointer,
				fmt.Sprintf("%q is already a folder of the project", input.name))
		}
	}
}

// recordedSelection builds the resolved selection the plan carries: the
// effective project, the sorted stacks, the installed modules of the index-0
// backend and the components as given. Every list is present; schema 2 only
// when components are given. A multi project records each backend's installed
// list on its entry and no top-level list. The recorded document of a run is
// built by the caller.
func (r *resolver) recordedSelection(stackNames []string, installed [][]string) manifest.Selection {
	recorded := manifest.Selection{
		Schema:  1,
		Project: r.sel.Project,
		Stacks:  stackNames,
		Modules: installed[0],
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
	if r.multi {
		recorded.Modules, recorded.Has.Modules = nil, false
		k := 0
		for i, entry := range recorded.Components {
			if manifest.IsBackendStack(entry.Stack) && k < len(installed) {
				recorded.Components[i].Modules, recorded.Components[i].HasModules = installed[k], true
				k++
			}
		}
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
