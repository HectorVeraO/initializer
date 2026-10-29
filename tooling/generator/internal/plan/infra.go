package plan

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
)

const serviceSlotPrefix = "service."

// Database returns the database and owner role names of the plan, set for the
// slots that are live; nil when neither is live.
func (p Plan) Database() *manifest.Database {
	var database manifest.Database
	if p.Live[names.SlotDatabase] {
		database.Name = p.Slots[names.SlotDatabase]
	}
	if p.Live[names.SlotDBUser] {
		database.User = p.Slots[names.SlotDBUser]
	}
	if database == (manifest.Database{}) {
		return nil
	}
	return &database
}

// Services returns the live compose service names by service key; nil when no
// service slot is live.
func (p Plan) Services() map[string]string {
	var services map[string]string
	for slot := range p.Live {
		key, isService := strings.CutPrefix(slot, serviceSlotPrefix)
		if !isService {
			continue
		}
		if services == nil {
			services = map[string]string{}
		}
		services[key] = p.Slots[slot]
	}
	return services
}

// serviceKeys returns the sorted service keys of the catalog reference: the
// closed set of keys a selection may rename.
func (r *resolver) serviceKeys() []string {
	var keys []string
	for slot := range r.cat.Reference {
		if key, isService := strings.CutPrefix(slot, serviceSlotPrefix); isService {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// slotProviders returns the sorted ids of the catalog modules whose name rules
// use slot inside a module block.
func (r *resolver) slotProviders(slot string) []string {
	found := map[string]bool{}
	for _, stack := range r.cat.Stacks {
		for _, module := range stack.Modules {
			for _, rule := range module.Names {
				used, err := names.Slots(rule.Text)
				if err != nil || !slices.Contains(used, slot) {
					continue
				}
				for _, block := range rule.Blocks {
					kind, id, _ := strings.Cut(block, ":")
					if kind == "module" {
						found[id] = true
					}
				}
			}
		}
	}
	return sortedKeys(found)
}

// notLiveMessage explains why no selected module delivers what: it names the
// modules that would, when the catalog has any.
func (r *resolver) notLiveMessage(pointer, what, slot string) string {
	message := fmt.Sprintf("%s: no selected module delivers %s", pointer, what)
	providers := r.slotProviders(slot)
	switch len(providers) {
	case 0:
		return message
	case 1:
		return fmt.Sprintf("%s (%s is not selected)", message, providers[0])
	}
	return fmt.Sprintf("%s (none of %s is selected)", message, strings.Join(providers, ", "))
}

// checkInfraNames reports the name problems of the database, its user and the
// compose services: overrides without a live slot, unknown service keys,
// derived values past the limit and conflicts. A grammar-invalid value was
// reported by the decoder and is skipped here.
func (r *resolver) checkInfraNames() {
	r.checkInfraOverrides()
	if !names.ValidLabel(r.sel.Project.Name) {
		return
	}
	r.checkServiceLengths()
	r.checkServiceConflicts()
}

// checkInfraOverrides reports NAME_OVERRIDE_UNUSED for a database name, user
// or service that no live rule uses, and for a service key the catalog does
// not know.
func (r *resolver) checkInfraOverrides() {
	if database := r.sel.Database; database != nil {
		if database.Name != "" && !r.live[names.SlotDatabase] {
			r.addUnused("/database/name", r.notLiveMessage("/database/name", "a database name", names.SlotDatabase), "")
		}
		if database.User != "" && !r.live[names.SlotDBUser] {
			r.addUnused("/database/user", r.notLiveMessage("/database/user", "a database user", names.SlotDBUser), "")
		}
	}
	keys := r.serviceKeys()
	for _, key := range sortedKeys(r.sel.Services) {
		pointer := "/services" + issue.Pointer(key)
		slot := names.ServiceSlot(key)
		_, isKnown := r.cat.Reference[slot]
		unknown := fmt.Sprintf("%s: no service key %q; keys: %s", pointer, key, strings.Join(keys, ", "))
		switch {
		case isKnown && !r.live[slot]:
			r.addUnused(pointer, r.notLiveMessage(pointer, fmt.Sprintf("a %q service", key), slot), "")
		case isKnown:
			continue
		case isKnownStack(key):
			hint := fmt.Sprintf("a component's service is its component name: --component %s=%s", key, r.sel.Services[key])
			r.addUnused(pointer, unknown, hint)
		default:
			hint := ""
			if closest := names.Closest(key, keys); closest != "" {
				hint = fmt.Sprintf("did you mean %q?", closest)
			}
			r.addUnused(pointer, unknown, hint)
		}
	}
}

func (r *resolver) addUnused(pointer, message, hint string) {
	r.issues = append(r.issues, issue.Issue{
		Code:    CodeNameOverrideUnused,
		Path:    issue.Path(selectionSource, pointer),
		Message: message,
		Hint:    hint,
	})
}

// liveServiceKeys returns the sorted keys of the live service slots.
func (r *resolver) liveServiceKeys() []string {
	var keys []string
	for slot := range r.live {
		if key, isService := strings.CutPrefix(slot, serviceSlotPrefix); isService {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// checkServiceLengths reports NAME_TOO_LONG at /project/name for a live
// service name derived from the project that passes the limit. The advised
// project length comes from the longest live derived suffix.
func (r *resolver) checkServiceLengths() {
	longestSuffix := 0
	var derived []string
	for _, key := range r.liveServiceKeys() {
		if _, isGiven := r.sel.Services[key]; isGiven {
			continue
		}
		derived = append(derived, key)
		longestSuffix = max(longestSuffix, len("-"+key))
	}
	limit := maxNameLength - longestSuffix
	for _, key := range derived {
		value := r.values[names.ServiceSlot(key)]
		found, isLong := NameTooLong(selectionSource, "/project/name", "compose service", value)
		if !isLong {
			continue
		}
		found.Hint = fmt.Sprintf("shorten the name to %d characters or fewer, or set services.%s", limit, key)
		r.issues = append(r.issues, found)
	}
}

// infraOwner is one live value that a conflict can name.
type infraOwner struct {
	label     string
	pointer   string
	isService bool
}

// checkServiceConflicts reports NAME_CONFLICT for live values that are equal
// when at least one is a service: two services, or a service and a component.
// Component-only conflicts are reported by checkNames.
func (r *resolver) checkServiceConflicts() {
	groups := map[string][]infraOwner{}
	componentCount := map[string]int{}
	for _, input := range r.componentInputs() {
		slot := names.ComponentSlot(input.stack)
		if !r.live[slot] || (input.given && !names.ValidLabel(input.name)) {
			continue
		}
		value := r.values[slot]
		groups[value] = append(groups[value], infraOwner{label: "component " + input.stack, pointer: input.namePointer})
		componentCount[value]++
	}
	for _, key := range r.liveServiceKeys() {
		value := r.values[names.ServiceSlot(key)]
		pointer := "/project/name"
		if given, isGiven := r.sel.Services[key]; isGiven {
			if !names.ValidLabel(given) {
				continue
			}
			pointer = "/services" + issue.Pointer(key)
		}
		groups[value] = append(groups[value], infraOwner{label: "service " + key, pointer: pointer, isService: true})
	}
	for _, value := range sortedKeys(groups) {
		group := groups[value]
		hasService := false
		labels := make([]string, 0, len(group))
		for _, owner := range group {
			hasService = hasService || owner.isService
			labels = append(labels, owner.label)
		}
		if len(group) < 2 || !hasService {
			continue
		}
		message := fmt.Sprintf("%s all resolve to %q", strings.Join(labels, ", "), value)
		if len(group) == 2 {
			message = fmt.Sprintf("%s both resolve to %q", strings.Join(labels, " and "), value)
		}
		for _, owner := range group {
			isReportedByComponents := !owner.isService && componentCount[value] > 1
			if isReportedByComponents {
				continue
			}
			r.issues = append(r.issues, issue.Issue{
				Code:    CodeNameConflict,
				Path:    issue.Path(selectionSource, owner.pointer),
				Message: message,
				Hint:    "set one of them to another name",
			})
		}
	}
}

// defaultInfraSlot returns the value of a database, database user or service
// slot: the selection's value, else the default derived from the project.
func (r *resolver) defaultInfraSlot(slot string) (string, bool) {
	project := r.sel.Project.Name
	switch slot {
	case names.SlotDatabase:
		if database := r.sel.Database; database != nil && database.Name != "" {
			return database.Name, true
		}
		return names.Snake(project), true
	case names.SlotDBUser:
		if database := r.sel.Database; database != nil && database.User != "" {
			return database.User, true
		}
		return names.Snake(project), true
	}
	key, isService := strings.CutPrefix(slot, serviceSlotPrefix)
	if !isService || !names.ValidLabel(key) {
		return "", false
	}
	if given, isGiven := r.sel.Services[key]; isGiven {
		return given, true
	}
	return project + "-" + key, true
}
