package manifest

import (
	"encoding/json"
	"fmt"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
)

// FlagsSource is the issue path source of a selection built from flags.
const FlagsSource = "flags"

// Project names the generated project.
type Project struct {
	Name     string `json:"name"`
	GoModule string `json:"goModule,omitempty"`
}

// Component is one components[] entry as given. Name is empty when the entry
// does not name the component.
type Component struct {
	Stack string `json:"stack"`
	Name  string `json:"name,omitempty"`
}

// Selection is the recorded choice of stacks and modules.
type Selection struct {
	Schema  int      `json:"schema"`
	Project Project  `json:"project"`
	Stacks  []string `json:"stacks"`
	Modules []string `json:"modules"`
	// Components holds the component entries as given; schema 2 only.
	Components []Component `json:"components,omitempty"`
	// AllModules selects every module of the backend stack; the plan expands
	// it and it is never written to a recorded selection.
	AllModules bool `json:"-"`
}

// SelectionFlags are the selection values given as command-line flags.
type SelectionFlags struct {
	Name       string
	GoModule   string
	Stacks     []string
	Modules    []string
	AllModules bool
	Components []Component
}

var (
	selectionKeys = []string{"schema", "project", "stacks", "modules", "components"}
	projectKeys   = []string{"name", "goModule"}
	componentKeys = []string{"stack", "name"}
)

// DecodeSelection decodes and validates an initializer.selection.json file.
func DecodeSelection(path string, data []byte) (Selection, []issue.Issue) {
	d := newDecoder(path, CodeSelectionInvalid)
	var selection Selection
	root, ok := d.syntax(data)
	if !ok {
		return selection, d.result()
	}
	members, ok := d.object(root, "", selectionKeys)
	if !ok {
		return selection, d.result()
	}
	selection.Schema = d.selectionSchema(members)
	if selection.Schema == 0 {
		selection.Schema = 1
	}
	if raw, found := members["components"]; found {
		if selection.Schema == 1 {
			d.add("/components", "components needs schema 2")
		} else {
			selection.Components = d.components(raw)
		}
	}
	projectOK := false
	if raw, ok := d.required(members, "", "project"); ok {
		selection.Project, projectOK = d.project(raw)
	}
	stacksOK := false
	if raw, ok := d.required(members, "", "stacks"); ok {
		values, valid, ok := d.strings(raw, "/stacks")
		selection.Stacks, stacksOK = values, ok && allTrue(valid)
	}
	if raw, ok := d.required(members, "", "modules"); ok {
		values, valid, ok := d.strings(raw, "/modules")
		selection.Modules = values
		if ok {
			d.checkModules(values, valid)
		}
	}
	d.checkProject(selection, projectOK, stacksOK)
	d.checkStacks(selection.Stacks, stacksOK)
	return selection, d.result()
}

// SelectionFromFlags builds and validates a selection from flag values. Issue
// paths use the source "flags" and the selection field pointers.
func SelectionFromFlags(f SelectionFlags) (Selection, []issue.Issue) {
	d := newDecoder(FlagsSource, CodeSelectionInvalid)
	selection := Selection{
		Schema:     1,
		Project:    Project{Name: f.Name, GoModule: f.GoModule},
		Stacks:     f.Stacks,
		Modules:    f.Modules,
		AllModules: f.AllModules,
		Components: f.Components,
	}
	if len(f.Components) > 0 {
		selection.Schema = 2
	}
	for i, component := range f.Components {
		hasName := component.Name != ""
		if hasName && !names.ValidLabel(component.Name) {
			d.addCode(CodeNameInvalid, fmt.Sprintf("/components/%d", i), names.LabelMessage(component.Name))
		}
	}
	valid := make([]bool, len(f.Modules))
	for i := range valid {
		valid[i] = true
	}
	d.checkModules(f.Modules, valid)
	d.checkProject(selection, true, true)
	d.checkStacks(f.Stacks, true)
	return selection, d.result()
}

func (d *decoder) project(raw json.RawMessage) (Project, bool) {
	members, ok := d.object(raw, "/project", projectKeys)
	if !ok {
		return Project{}, false
	}
	var project Project
	ok = true
	if rawName, found := d.required(members, "/project", "name"); found {
		name, isString := d.str(rawName, "/project/name")
		project.Name = name
		ok = ok && isString
	} else {
		ok = false
	}
	if rawModule, found := members["goModule"]; found {
		module, isString := d.str(rawModule, "/project/goModule")
		project.GoModule = module
		ok = ok && isString
	}
	return project, ok
}

// components decodes the components member: an array of objects with the keys
// stack (required) and name (optional). A name that fails the label grammar is
// NAME_INVALID at the name.
func (d *decoder) components(raw json.RawMessage) []Component {
	items, ok := d.array(raw, "/components")
	if !ok {
		return nil
	}
	found := make([]Component, 0, len(items))
	for i, item := range items {
		pointer := fmt.Sprintf("/components/%d", i)
		members, ok := d.object(item, pointer, componentKeys)
		if !ok {
			continue
		}
		var component Component
		if rawStack, present := d.required(members, pointer, "stack"); present {
			component.Stack, _ = d.str(rawStack, pointer+"/stack")
		}
		if rawName, present := members["name"]; present {
			name, isString := d.str(rawName, pointer+"/name")
			component.Name = name
			if isString && !names.ValidLabel(name) {
				d.addCode(CodeNameInvalid, pointer+"/name", names.LabelMessage(name))
			}
		}
		found = append(found, component)
	}
	return found
}

func (d *decoder) checkProject(selection Selection, projectOK, stacksOK bool) {
	if !projectOK {
		return
	}
	if !names.ValidLabel(selection.Project.Name) {
		d.addCode(CodeNameInvalid, "/project/name", names.LabelMessage(selection.Project.Name))
	}
	selectsGo := false
	for _, stack := range selection.Stacks {
		info, known := LookupStack(stack)
		needsGoModule := known && info.GoModule
		selectsGo = selectsGo || needsGoModule
	}
	goModule := selection.Project.GoModule
	switch {
	case goModule == "" && selectsGo && stacksOK:
		d.add("/project/goModule", "required when go-srv is selected")
	case goModule != "" && !validGoModulePath(goModule):
		d.add("/project/goModule", "must be a Go module path with a lowercase dotted first segment")
	}
}

func (d *decoder) checkStacks(stacks []string, stacksOK bool) {
	if !stacksOK {
		return
	}
	backends, frontends := 0, 0
	for i, stack := range stacks {
		switch {
		case IsBackendStack(stack):
			backends++
		case IsFrontendStack(stack):
			frontends++
		default:
			d.add(fmt.Sprintf("/stacks/%d", i), "stack is not in the stack table")
		}
	}
	if backends != 1 {
		d.add("/stacks", "must contain exactly one backend stack")
	}
	if frontends > 1 {
		d.add("/stacks", "must contain at most one frontend stack")
	}
}

func (d *decoder) checkModules(modules []string, valid []bool) {
	for i, module := range modules {
		if valid[i] && !validID(module) {
			d.add(fmt.Sprintf("/modules/%d", i), "must be a module id of at most 31 characters")
		}
	}
}

func allTrue(values []bool) bool {
	for _, value := range values {
		if !value {
			return false
		}
	}
	return true
}
