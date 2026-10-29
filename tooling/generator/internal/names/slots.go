package names

import (
	"sort"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
)

// Slot names of the closed vocabulary.
const (
	SlotProject     = "project"
	SlotDisplayName = "displayName"
	SlotDatabase    = "database"
	SlotDBUser      = "dbUser"
	SlotEnvPrefix   = "envPrefix"
)

const (
	componentPrefix = "component."
	servicePrefix   = "service."
)

// CodeReferenceInvalid is the issue code of a reference value that breaks its
// grammar or conflicts with another one.
const CodeReferenceInvalid = "REFERENCE_INVALID"

// ReferenceSource is the issue path source of the reference file.
const ReferenceSource = ".initializer/reference.json"

// ComponentSlot returns "component.<stack>".
func ComponentSlot(stack string) string {
	return componentPrefix + stack
}

// ServiceSlot returns "service.<key>".
func ServiceSlot(key string) string {
	return servicePrefix + key
}

// Resolved holds the effective names of one project with one component per stack.
type Resolved struct {
	Project     string
	DisplayName string
	Database    string
	DBUser      string
	// Components maps a stack id to its component name.
	Components map[string]string
	// Services maps a service key to its compose service name.
	Services map[string]string
}

// DerivedSlot is a slot no input sets. Its reference value is Value applied
// to the reference input, which catalog build stores in reference.
type DerivedSlot struct {
	// Name is the slot name, or a prefix ending in "." for one slot per stack
	// ("task." gives "task.go-srv").
	Name string
	// Value returns the slot value; stack is "" for a project slot.
	Value func(r Resolved, stack string) string
}

// Derived is the registry of derived slots, sorted by Name.
var Derived = []DerivedSlot{
	{Name: SlotEnvPrefix, Value: func(r Resolved, _ string) string { return EnvPrefix(r.Project) }},
}

// Snake returns s with every "-" replaced by "_".
func Snake(s string) string {
	return strings.ReplaceAll(s, "-", "_")
}

// EnvPrefix returns upper(snake(project)): "acme-backend" gives "ACME_BACKEND".
func EnvPrefix(project string) string {
	return strings.ToUpper(Snake(project))
}

// IsSlot reports whether name is a slot of the vocabulary or of Derived,
// for the given stack ids. The caller passes the stack table's ids
// (manifest.Stacks), so names never imports manifest.
func IsSlot(name string, stacks []string) bool {
	isStack := func(id string) bool {
		for _, stack := range stacks {
			if stack == id {
				return true
			}
		}
		return false
	}
	return isSlotName(name, isStack)
}

// isSlotName reports whether name is a slot, with stackKnown deciding which
// stack ids a per-stack slot accepts.
func isSlotName(name string, stackKnown func(string) bool) bool {
	switch name {
	case SlotProject, SlotDisplayName, SlotDatabase, SlotDBUser:
		return true
	}
	if key, isService := strings.CutPrefix(name, servicePrefix); isService {
		return ValidLabel(key)
	}
	if stack, isComponent := strings.CutPrefix(name, componentPrefix); isComponent {
		return stackKnown(stack)
	}
	for _, derived := range Derived {
		if strings.HasSuffix(derived.Name, ".") {
			stack, isPerStack := strings.CutPrefix(name, derived.Name)
			if isPerStack && stackKnown(stack) {
				return true
			}
			continue
		}
		if name == derived.Name {
			return true
		}
	}
	return false
}

// Slots returns every slot value of r, derived slots included. A slot without
// a value is omitted.
func (r Resolved) Slots() map[string]string {
	slots := map[string]string{}
	set := func(name, value string) {
		if value != "" {
			slots[name] = value
		}
	}
	set(SlotProject, r.Project)
	set(SlotDisplayName, r.DisplayName)
	set(SlotDatabase, r.Database)
	set(SlotDBUser, r.DBUser)
	stacks := make([]string, 0, len(r.Components))
	for stack, name := range r.Components {
		set(ComponentSlot(stack), name)
		stacks = append(stacks, stack)
	}
	for key, name := range r.Services {
		set(ServiceSlot(key), name)
	}
	sort.Strings(stacks)
	for _, derived := range Derived {
		if !strings.HasSuffix(derived.Name, ".") {
			set(derived.Name, derived.Value(r, ""))
			continue
		}
		for _, stack := range stacks {
			set(derived.Name+stack, derived.Value(r, stack))
		}
	}
	return slots
}

// Reference resolves the reference values into their slot map and returns
// REFERENCE_INVALID issues. Values are taken as given; only derived slots
// are computed. The caller (catalog build) fills r from the Selection that
// manifest.DecodeReference returns: names never imports manifest, because
// manifest imports names.
func Reference(r Resolved) (map[string]string, []issue.Issue) {
	var issues []issue.Issue
	add := func(pointer, message string) {
		issues = append(issues, issue.Issue{
			Code:    CodeReferenceInvalid,
			Path:    issue.Path(ReferenceSource, pointer),
			Message: message,
		})
	}
	if !ValidLabel(r.Project) {
		add("/project/name", LabelMessage(r.Project))
	}
	stacks := make([]string, 0, len(r.Components))
	for stack := range r.Components {
		stacks = append(stacks, stack)
	}
	sort.Strings(stacks)
	owners := map[string][]string{}
	for _, stack := range stacks {
		name := r.Components[stack]
		if !ValidLabel(name) {
			add("/components", "component "+stack+": "+LabelMessage(name))
			continue
		}
		owners[name] = append(owners[name], "component "+stack)
	}
	keys := make([]string, 0, len(r.Services))
	for key := range r.Services {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		name := r.Services[key]
		if !ValidLabel(key) || !ValidLabel(name) {
			add("/services", "service "+key+": must be a label")
			continue
		}
		owners[name] = append(owners[name], "service "+key)
	}
	conflicts := make([]string, 0, len(owners))
	for name := range owners {
		conflicts = append(conflicts, name)
	}
	sort.Strings(conflicts)
	for _, name := range conflicts {
		group := owners[name]
		if len(group) > 1 {
			add("/components", strings.Join(group, " and ")+` both resolve to "`+name+`"`)
		}
	}
	if len(issues) > 0 {
		issue.Sort(issues)
		return nil, issues
	}
	return r.Slots(), nil
}
