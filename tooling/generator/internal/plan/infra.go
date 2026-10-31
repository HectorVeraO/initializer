package plan

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/markers"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
)

const serviceSlotPrefix = "service."

// Database returns the database and owner role names of the plan, set for the
// slots that are live; nil when neither is live.
func (p Plan) Database() *manifest.Database {
	database := databaseFrom(p.Slots, p.Live)
	if database == (manifest.Database{}) {
		return nil
	}
	return &database
}

// Services returns the live compose service names by service key; nil when no
// service slot is live.
func (p Plan) Services() map[string]string {
	return servicesFrom(p.Slots, p.Live)
}

// servicesFrom returns the live service names by key; nil when none is live.
func servicesFrom(values map[string]string, live map[string]bool) map[string]string {
	var services map[string]string
	for slot := range live {
		key, isService := strings.CutPrefix(slot, serviceSlotPrefix)
		if !isService {
			continue
		}
		if services == nil {
			services = map[string]string{}
		}
		services[key] = values[slot]
	}
	return services
}

// infraSource is where the database and service names of one backend instance
// come from: the given values and the defaults derived from the project (a
// single project) or from the component (a multi project).
type infraSource struct {
	database manifest.Database
	services map[string]string
	// base prefixes a derived service name; dbDefault is the derived database
	// name and user.
	base, dbDefault string
	// dbPointer and servicesPointer locate the given values; derivedAt locates
	// the input a derived value comes from.
	dbPointer, servicesPointer, derivedAt string
}

// infraOf returns the infra source of a backend component. A single project
// reads the top-level values; a multi project reads the entry's values, with
// the top-level database for the one backend that has no database of its own.
func (r *resolver) infraOf(backend componentInput) infraSource {
	if !r.multi {
		src := infraSource{
			services: r.sel.Services, base: r.sel.Project.Name, dbDefault: names.Snake(r.sel.Project.Name),
			dbPointer: "/database", servicesPointer: "/services", derivedAt: "/project/name",
		}
		if r.sel.Database != nil {
			src.database = *r.sel.Database
		}
		return src
	}
	entry := r.sel.Components[backend.entry]
	src := infraSource{
		services: entry.Services, base: backend.name, dbDefault: names.Snake(backend.name),
		dbPointer:       fmt.Sprintf("/components/%d/database", backend.entry),
		servicesPointer: fmt.Sprintf("/components/%d/services", backend.entry),
		derivedAt:       backend.namePointer,
	}
	switch {
	case entry.Database != nil:
		src.database = *entry.Database
	case r.sel.Database != nil && r.databaseInheritors() == 1:
		src.database, src.dbPointer = *r.sel.Database, "/database"
	}
	return src
}

// databaseInheritors counts the backend entries that give no database.
func (r *resolver) databaseInheritors() int {
	count := 0
	for _, entry := range r.sel.Components {
		if manifest.IsBackendStack(entry.Stack) && entry.Database == nil {
			count++
		}
	}
	return count
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
					kind, ids := markers.SplitRef(block)
					if kind != "module" {
						continue
					}
					for _, id := range ids {
						if id != markers.MultiID {
							found[id] = true
						}
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

// checkInfraNames reports the name problems of the display name, the database,
// its user, the compose services and the Go modules: overrides without a live
// slot, unknown service keys, derived values past the limit and conflicts. A
// grammar-invalid value was reported by the decoder and is skipped here.
func (r *resolver) checkInfraNames(pairs []pairPlan) {
	r.checkDisplayName(pairs)
	for i := range pairs {
		if pairs[i].named {
			r.checkInfraOverrides(pairs[i])
		}
	}
	if !names.ValidLabel(r.sel.Project.Name) {
		return
	}
	for i := range pairs {
		if pairs[i].named {
			r.checkServiceLengths(pairs[i])
		}
	}
	r.checkServiceConflicts(pairs)
	r.checkSharedServices(pairs)
	r.checkDatabaseNames(pairs)
	r.checkGoModules(pairs)
}

// checkDisplayName reports NAME_OVERRIDE_UNUSED for a display name no live
// rule of any pair uses.
func (r *resolver) checkDisplayName(pairs []pairPlan) {
	if r.sel.DisplayName == "" {
		return
	}
	hasNamed := false
	for _, pair := range pairs {
		if !pair.named {
			continue
		}
		hasNamed = true
		if pair.live[names.SlotDisplayName] {
			return
		}
	}
	if !hasNamed {
		return
	}
	r.addUnused("/displayName", r.notLiveMessage("/displayName", "a display name", names.SlotDisplayName), "")
}

// checkInfraOverrides reports NAME_OVERRIDE_UNUSED for a database name, user
// or service that no live rule of the pair uses, and for a service key the
// catalog does not know. A multi project's entries are checked for unknown keys
// by the component checks.
func (r *resolver) checkInfraOverrides(pair pairPlan) {
	src, live := pair.src, pair.live
	if src.database.Name != "" && !live[names.SlotDatabase] {
		pointer := src.dbPointer + "/name"
		r.addUnused(pointer, r.notLiveMessage(pointer, "a database name", names.SlotDatabase), "")
	}
	if src.database.User != "" && !live[names.SlotDBUser] {
		pointer := src.dbPointer + "/user"
		r.addUnused(pointer, r.notLiveMessage(pointer, "a database user", names.SlotDBUser), "")
	}
	keys := r.serviceKeys()
	for _, key := range sortedKeys(src.services) {
		pointer := src.servicesPointer + issue.Pointer(key)
		slot := names.ServiceSlot(key)
		_, isKnown := r.cat.Reference[slot]
		unknown := fmt.Sprintf("%s: no service key %q; keys: %s", pointer, key, strings.Join(keys, ", "))
		switch {
		case r.multi && names.IsSharedService(key):
			r.addUnused(pointer, fmt.Sprintf("%s: the %q service is shared by every backend", pointer, key),
				fmt.Sprintf("set it for the project: --service %s=<name>", key))
		case isKnown && !live[slot]:
			r.addUnused(pointer, r.notLiveMessage(pointer, fmt.Sprintf("a %q service", key), slot), "")
		case isKnown, r.multi:
			continue
		case isKnownStack(key):
			hint := fmt.Sprintf("a component's service is its component name: --component %s=%s", key, src.services[key])
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
func liveServiceKeys(live map[string]bool) []string {
	var keys []string
	for slot := range live {
		if key, isService := strings.CutPrefix(slot, serviceSlotPrefix); isService {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// checkServiceLengths reports NAME_TOO_LONG for a live service name derived
// from the project (single) or the component (multi) that passes the limit. The
// advised length comes from the longest live derived suffix.
func (r *resolver) checkServiceLengths(pair pairPlan) {
	longestSuffix := 0
	var derived []string
	for _, key := range liveServiceKeys(pair.live) {
		if r.isGivenService(pair, key) {
			continue
		}
		derived = append(derived, key)
		longestSuffix = max(longestSuffix, len("-"+key))
	}
	limit := maxNameLength - longestSuffix
	for _, key := range derived {
		value := pair.values[names.ServiceSlot(key)]
		found, isLong := NameTooLong(selectionSource, pair.src.derivedAt, "compose service", value)
		if !isLong {
			continue
		}
		found.Hint = fmt.Sprintf("shorten the name to %d characters or fewer, or set services.%s", limit, key)
		if r.multi {
			found.Hint = fmt.Sprintf("shorten the component name to %d characters or fewer, or set service.%s on it", limit, key)
		}
		if r.multi && names.IsSharedService(key) {
			found, _ = NameTooLong(selectionSource, "/project/name", "compose service", value)
			found.Hint = fmt.Sprintf("shorten the project name to %d characters or fewer, or set services.%s", limit, key)
		}
		r.issues = append(r.issues, found)
	}
}

// checkSharedServices reports NAME_OVERRIDE_UNUSED for a project-level service
// of a multi project that no live rule uses, or that the catalog does not know.
// The other top-level services of a multi project are reported by the
// component checks.
func (r *resolver) checkSharedServices(pairs []pairPlan) {
	if !r.multi {
		return
	}
	keys := r.serviceKeys()
	for _, key := range sortedKeys(r.sel.Services) {
		if !names.IsSharedService(key) {
			continue
		}
		pointer := "/services" + issue.Pointer(key)
		slot := names.ServiceSlot(key)
		if _, isKnown := r.cat.Reference[slot]; !isKnown {
			r.addUnused(pointer, fmt.Sprintf("%s: no service key %q; keys: %s", pointer, key, strings.Join(keys, ", ")), "")
			continue
		}
		isLive := false
		for _, pair := range pairs {
			isLive = isLive || pair.live[slot]
		}
		if !isLive {
			r.addUnused(pointer, r.notLiveMessage(pointer, fmt.Sprintf("a %q service", key), slot), "")
		}
	}
}

// pairUses reports whether a live name rule of the pair's backend modules uses
// slot. A slot no lock records has no live flag, so it is read from the rules.
func (r *resolver) pairUses(pair pairPlan, slot string) bool {
	installed := map[string]bool{markers.MultiID: r.multi}
	for _, module := range pair.backend.Modules {
		installed[module.Module] = true
	}
	selected := map[string]bool{pair.backend.Stack: true}
	for _, module := range pair.backend.Modules {
		for _, rule := range module.Names {
			used, err := names.Slots(rule.Text)
			if err == nil && slices.Contains(used, slot) && ruleIsLive(rule, installed, selected) {
				return true
			}
		}
	}
	return false
}

// nameHolder is one database or role name of the shared Postgres server.
type nameHolder struct {
	label   string
	pointer string
}

// checkDatabaseNames reports, in a multi project whose backends share one
// Postgres server, NAME_CONFLICT for two databases or two roles that resolve to
// one name (the server's own "postgres" included) and NAME_TOO_LONG for a Kratos
// name derived past the identifier limit.
func (r *resolver) checkDatabaseNames(pairs []pairPlan) {
	if !r.multi {
		return
	}
	databases := map[string][]nameHolder{"postgres": {{label: "the Postgres server's database"}}}
	roles := map[string][]nameHolder{"postgres": {{label: "the Postgres server's superuser"}}}
	for _, pair := range pairs {
		if !pair.named {
			continue
		}
		name := pair.backend.Name
		namePointer, userPointer := pair.src.derivedAt, pair.src.derivedAt
		if pair.src.database.Name != "" {
			namePointer = pair.src.dbPointer + "/name"
		}
		if pair.src.database.User != "" {
			userPointer = pair.src.dbPointer + "/user"
		}
		database, user := databaseNamesOf(pair.src)
		databases[database] = append(databases[database], nameHolder{label: "database of " + name, pointer: namePointer})
		roles[user] = append(roles[user], nameHolder{label: "database user of " + name, pointer: userPointer})
		if !r.pairUses(pair, names.SlotKratosDatabase) {
			continue
		}
		for _, derived := range []struct {
			slot, label, pointer string
			into                 map[string][]nameHolder
		}{
			{names.SlotKratosDatabase, "Kratos database of " + name, namePointer, databases},
			{names.SlotKratosUser, "Kratos database user of " + name, userPointer, roles},
		} {
			value := pair.values[derived.slot]
			if found, isLong := NameTooLong(selectionSource, derived.pointer, derived.label, value); isLong {
				found.Hint = "shorten the database name and user to 56 characters or fewer"
				r.issues = append(r.issues, found)
				continue
			}
			derived.into[value] = append(derived.into[value], nameHolder{label: derived.label, pointer: derived.pointer})
		}
	}
	for _, group := range []map[string][]nameHolder{databases, roles} {
		for _, value := range sortedKeys(group) {
			holders := group[value]
			if len(holders) < 2 {
				continue
			}
			labels := make([]string, 0, len(holders))
			for _, holder := range holders {
				labels = append(labels, holder.label)
			}
			message := fmt.Sprintf("%s all resolve to %q on the shared Postgres server", strings.Join(labels, ", "), value)
			if len(holders) == 2 {
				message = fmt.Sprintf("%s both resolve to %q on the shared Postgres server", strings.Join(labels, " and "), value)
			}
			for _, holder := range holders {
				if holder.pointer == "" {
					continue
				}
				r.issues = append(r.issues, issue.Issue{
					Code: CodeNameConflict, Path: issue.Path(selectionSource, holder.pointer), Message: message,
					Hint: "give each backend its own database name and user (db-name, db-user)",
				})
			}
		}
	}
}

// givenService returns the name the selection gives to a service key of the
// pair: the pair's entry, and for a shared service of a multi project the
// project's services.
func (r *resolver) givenService(pair pairPlan, key string) (string, bool) {
	if r.multi && names.IsSharedService(key) {
		given, isGiven := r.sel.Services[key]
		return given, isGiven
	}
	given, isGiven := pair.src.services[key]
	return given, isGiven
}

// givenServicePointer locates the input givenService read.
func (r *resolver) givenServicePointer(pair pairPlan, key string) string {
	if r.multi && names.IsSharedService(key) {
		return "/services" + issue.Pointer(key)
	}
	return pair.src.servicesPointer + issue.Pointer(key)
}

// isGivenService reports whether the selection names the service key of pair.
func (r *resolver) isGivenService(pair pairPlan, key string) bool {
	_, isGiven := r.givenService(pair, key)
	return isGiven
}

// infraOwner is one live value that a conflict can name.
type infraOwner struct {
	label     string
	pointer   string
	isService bool
	isGiven   bool
}

// checkServiceConflicts reports NAME_CONFLICT for live values that are equal
// when at least one is a service: two services, or a service and a component.
// Component-only conflicts are reported by checkNames. In a multi project a
// group whose values were all given and holds at most one service is reported
// by the component checks.
func (r *resolver) checkServiceConflicts(pairs []pairPlan) {
	groups := map[string][]infraOwner{}
	componentCount := map[string]int{}
	for _, input := range r.componentInputs() {
		if input.name == "" || (input.given && !names.ValidLabel(input.name)) {
			continue
		}
		groups[input.name] = append(groups[input.name], infraOwner{label: input.label, pointer: input.namePointer, isGiven: input.given})
		componentCount[input.name]++
	}
	sharedSeen := map[string]bool{}
	for _, pair := range pairs {
		if !pair.named {
			continue
		}
		for _, key := range liveServiceKeys(pair.live) {
			value := pair.values[names.ServiceSlot(key)]
			owner := infraOwner{label: "service " + key, pointer: pair.src.derivedAt, isService: true}
			isShared := r.multi && names.IsSharedService(key)
			if isShared {
				if sharedSeen[key] {
					continue
				}
				sharedSeen[key] = true
				owner.pointer = "/project/name"
			}
			if given, isGiven := r.givenService(pair, key); isGiven {
				if !names.ValidLabel(given) {
					continue
				}
				owner.pointer, owner.isGiven = r.givenServicePointer(pair, key), true
			}
			if r.multi && !isShared {
				owner.label += " of " + pair.backend.Name
			}
			groups[value] = append(groups[value], owner)
		}
	}
	for _, value := range sortedKeys(groups) {
		group := groups[value]
		services, allGiven := 0, true
		labels := make([]string, 0, len(group))
		for _, owner := range group {
			if owner.isService {
				services++
			}
			allGiven = allGiven && owner.isGiven
			labels = append(labels, owner.label)
		}
		if len(group) < 2 || services == 0 || (r.multi && allGiven && services < 2) {
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

// checkGoModules reports NAME_CONFLICT for two Go components whose module
// paths are equal or nested: one is a path-segment prefix of the other.
func (r *resolver) checkGoModules(pairs []pairPlan) {
	if !r.multi {
		return
	}
	type owner struct{ path, pointer, name string }
	var owners []owner
	for _, pair := range pairs {
		backend := pair.input.backend
		if pair.backend.GoModule == "" {
			continue
		}
		pointer := backend.namePointer
		if r.hasGivenGoModule(backend) {
			pointer = fmt.Sprintf("/components/%d/goModule", backend.entry)
		}
		owners = append(owners, owner{path: pair.backend.GoModule, pointer: pointer, name: pair.backend.Name})
	}
	for i := range owners {
		for j := i + 1; j < len(owners); j++ {
			a, b := owners[i], owners[j]
			overlaps := a.path == b.path || strings.HasPrefix(b.path, a.path+"/") || strings.HasPrefix(a.path, b.path+"/")
			if !overlaps {
				continue
			}
			message := fmt.Sprintf("go module %q of %s and go module %q of %s are equal or nested", a.path, a.name, b.path, b.name)
			for _, o := range []owner{a, b} {
				r.issues = append(r.issues, issue.Issue{
					Code: CodeNameConflict, Path: issue.Path(selectionSource, o.pointer), Message: message,
					Hint: "give each component a Go module path that is not inside another",
				})
			}
		}
	}
}

// defaultInfraSlot returns the value of a database, database user or service
// slot: the instance's value, else the default derived from the project or the
// component.
func (r *resolver) defaultInfraSlot(slot string, src infraSource) (string, bool) {
	switch slot {
	case names.SlotDisplayName:
		if r.sel.DisplayName != "" {
			return r.sel.DisplayName, true
		}
		return r.sel.Project.Name, true
	case names.SlotDatabase:
		if src.database.Name != "" {
			return src.database.Name, true
		}
		return src.dbDefault, true
	case names.SlotDBUser:
		if src.database.User != "" {
			return src.database.User, true
		}
		return src.dbDefault, true
	}
	key, isService := strings.CutPrefix(slot, serviceSlotPrefix)
	if !isService || !names.ValidLabel(key) {
		return "", false
	}
	if r.multi && names.IsSharedService(key) {
		if given, isGiven := r.sel.Services[key]; isGiven {
			return given, true
		}
		return r.sel.Project.Name + "-" + key, true
	}
	if given, isGiven := src.services[key]; isGiven {
		return given, true
	}
	return src.base + "-" + key, true
}
