package plan

import (
	"fmt"
	"sort"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
)

// startList is the module list a backend component starts from.
type startList struct {
	ids []string
	// pointer is the selection pointer of the list.
	pointer string
	// all is true when the backend starts from every module of its stack.
	all bool
	// inherited is true when the list is the top-level one.
	inherited bool
}

// pairInput is a backend component and the frontend bound to it, if any.
type pairInput struct {
	backend  componentInput
	frontend *componentInput
}

// pairPlan is the plan of one backend component and its bound frontend.
type pairPlan struct {
	input    pairInput
	backend  ComponentPlan
	frontend *ComponentPlan
	src      infraSource
	// values and live are the slots of the pair; nil while a component has no name.
	values map[string]string
	live   map[string]bool
	named  bool
}

// pairInputs returns the backend components in index order, each with its
// bound frontend. A component with no name, a frontend with no resolvable
// backend and a second frontend for one backend set r.undetermined: the
// component checks report them, and the plan skips what they leave open.
func (r *resolver) pairInputs() []pairInput {
	var backends, frontends []componentInput
	for _, input := range r.componentInputs() {
		if input.name == "" {
			r.undetermined = true
		}
		switch input.role {
		case roleBackend:
			backends = append(backends, input)
		case roleFrontend:
			frontends = append(frontends, input)
		}
	}
	pairs := make([]pairInput, len(backends))
	for i := range backends {
		pairs[i].backend = backends[i]
	}
	for i := range frontends {
		given := ""
		if frontends[i].entry >= 0 {
			given = r.sel.Components[frontends[i].entry].Backend
		}
		target := -1
		for j, backend := range backends {
			isNamed := given != "" && backend.name == given
			isOnly := given == "" && len(backends) == 1
			if isNamed || isOnly {
				target = j
			}
		}
		if target < 0 || pairs[target].frontend != nil {
			r.undetermined = true
			continue
		}
		pairs[target].frontend = &frontends[i]
	}
	return pairs
}

// startOf returns the list a backend component starts from: its own in a multi
// project when it gave one, else the top-level list.
func (r *resolver) startOf(backend componentInput) startList {
	if r.multi && backend.entry >= 0 && r.sel.Components[backend.entry].HasModules {
		return startList{
			ids:     r.sel.Components[backend.entry].Modules,
			pointer: fmt.Sprintf("/components/%d/modules", backend.entry),
		}
	}
	return startList{ids: r.sel.Modules, pointer: "/modules", all: r.sel.AllModules, inherited: true}
}

// planPair runs steps 1 to 5 of the plan for a backend component and its
// frontend, then checks the frontend's specs against that backend.
func (r *resolver) planPair(selected []int, input pairInput) pairPlan {
	backendEntry := r.catalogEntry(selected, input.backend.stack)
	frontendEntry := -1
	if input.frontend != nil {
		frontendEntry = r.catalogEntry(selected, input.frontend.stack)
	}
	var stacks []int
	for _, entry := range selected {
		if entry == backendEntry || entry == frontendEntry {
			stacks = append(stacks, entry)
		}
	}
	closure := r.closure(stacks, r.startOf(input.backend), input.backend.name)
	r.checkPresence(backendEntry, closure)
	pair := pairPlan{input: input, backend: r.planStack(backendEntry, closure)}
	pair.backend.Name, pair.backend.Index = input.backend.name, input.backend.index
	pair.backend.GoModule = r.goModuleOf(input.backend)
	if input.frontend != nil {
		frontend := r.planStack(frontendEntry, closure)
		frontend.Name, frontend.Index = input.frontend.name, input.frontend.index
		frontend.Partner, pair.backend.Partner = pair.backend.Name, frontend.Name
		pair.frontend = &frontend
		r.checkFrontendSpecs(pair.backend, frontend, frontendEntry)
	}
	return pair
}

// catalogEntry returns the catalog index of stack among the selected entries.
func (r *resolver) catalogEntry(selected []int, stack string) int {
	for _, entry := range selected {
		if r.cat.Stacks[entry].Stack == stack {
			return entry
		}
	}
	return -1
}

// flatten returns the components of the pairs sorted by (Stack, Index).
func flatten(pairs []pairPlan) []ComponentPlan {
	var components []ComponentPlan
	for _, pair := range pairs {
		components = append(components, pair.backend)
		if pair.frontend != nil {
			components = append(components, *pair.frontend)
		}
	}
	sort.Slice(components, func(i, j int) bool {
		if components[i].Stack != components[j].Stack {
			return components[i].Stack < components[j].Stack
		}
		return components[i].Index < components[j].Index
	})
	return components
}

// goModuleBase is the path a derived Go module path hangs under.
func (r *resolver) goModuleBase() string {
	if r.sel.Project.GoModule != "" {
		return r.sel.Project.GoModule
	}
	return "example.com/" + r.sel.Project.Name
}

// hasGivenGoModule reports whether the component's entry sets its Go module.
func (r *resolver) hasGivenGoModule(component componentInput) bool {
	return component.entry >= 0 && r.sel.Components[component.entry].GoModule != ""
}

// goModuleOf returns the target Go module path of a component: the project's
// in a single project, the entry's or <base>/<name> in a multi project.
func (r *resolver) goModuleOf(component componentInput) string {
	info, _ := manifest.LookupStack(component.stack)
	switch {
	case !info.GoModule:
		return ""
	case !r.multi:
		return r.sel.Project.GoModule
	case r.hasGivenGoModule(component):
		return r.sel.Components[component.entry].GoModule
	case component.name == "":
		return ""
	}
	return r.goModuleBase() + "/" + component.name
}

// derivedNotes returns the note for the Go module paths a multi project derived.
func (r *resolver) derivedNotes(pairs []pairPlan) []string {
	if !r.multi {
		return nil
	}
	for _, pair := range pairs {
		if !r.hasGivenGoModule(pair.input.backend) {
			return []string{fmt.Sprintf("go modules derived from %s; pass go-module= per component to change them", r.goModuleBase())}
		}
	}
	return nil
}

// nameSlots computes the slots of a pair and the database and service names of
// its backend. A pair with an unnamed component is left for the caller.
func (r *resolver) nameSlots(pair *pairPlan) {
	members := []ComponentPlan{pair.backend}
	if pair.frontend != nil {
		members = append(members, *pair.frontend)
	}
	for _, member := range members {
		if member.Name == "" {
			return
		}
	}
	pair.src = r.infraOf(pair.input.backend)
	pair.values, pair.live = r.slots(members, pair.src)
	pair.named = true
	pair.backend.Database = databaseFrom(pair.values, pair.live)
	pair.backend.Services = servicesFrom(pair.values, pair.live)
}

// databaseFrom returns the database and owner role names of the slots that are
// live.
func databaseFrom(values map[string]string, live map[string]bool) manifest.Database {
	var database manifest.Database
	if live[names.SlotDatabase] {
		database.Name = values[names.SlotDatabase]
	}
	if live[names.SlotDBUser] {
		database.User = values[names.SlotDBUser]
	}
	return database
}
