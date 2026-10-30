// Package legs derives the CI selections that certify a catalog: the minimal
// set of generated projects whose TCK and builds together cover every module
// and every cooperation between specs.
package legs

import (
	"fmt"
	"sort"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
)

// CodeDrift is the issue code of a selection file set that differs from the
// covering set of the catalog.
const CodeDrift = "LEGS_DRIFT"

const (
	roleBackend  = "backend"
	roleFrontend = "frontend"
	// filesDir is the directory of the selection files, used in issue paths.
	filesDir = "testdata/ci"
)

// Leg is one CI selection after resolution.
type Leg struct {
	// Stacks are the selected stacks, sorted.
	Stacks []string
	// Modules are the resolved backend modules in plan order, core first.
	Modules []string
	// Components lists the delivered components of a project with several
	// backends, sorted by (Stack, Index); nil for every single leg.
	Components []LegComponent
}

// LegComponent is one component of a multi leg after resolution.
type LegComponent struct {
	Stack string
	// Index is the position among the components of Stack.
	Index int
	// Modules are the resolved modules of the component in plan order.
	Modules []string
	// Backend is the index of the bound backend of a frontend, -1 for a backend.
	Backend int
}

// key identifies a leg: its stacks and resolved modules, then the components
// of a multi leg as stack#index:modules>backend. Names are not part of it.
func (l Leg) key() string {
	base := strings.Join(l.Stacks, ",") + "|" + strings.Join(l.Modules, ",")
	if len(l.Components) == 0 {
		return base
	}
	parts := make([]string, len(l.Components))
	for i, component := range l.Components {
		parts[i] = fmt.Sprintf("%s#%d:%s>%d", component.Stack, component.Index, strings.Join(component.Modules, ","), component.Backend)
	}
	return base + "|" + strings.Join(parts, ";")
}

// baseLeg is a resolved backend leg with the typed modules that produced it.
type baseLeg struct {
	typed []string
	leg   Leg
}

// Derive returns the legs of the covering set of cat in order: by stacks
// joined with ",", then by resolved modules joined with ",". Candidates the
// plan refuses are dropped. Everything derives from catalog data.
//
// Backend candidates are: no module; each non-core module alone; for every
// spec that cooperates with the tag of another spec, the modules implementing
// both; and every module. Each is resolved against the backend stack and
// de-duplicated by resolved modules. A frontend twin of a backend leg is kept
// when the plan accepts it and the leg is the full set or the frontend stack
// implements every spec the leg's backend modules implement.
func Derive(cat catalog.Catalog) []Leg {
	backend, frontends := splitStacks(cat)
	if backend == nil {
		return nil
	}
	var legs []Leg
	seen := map[string]bool{}
	var bases []baseLeg
	for _, typed := range backendCandidates(cat, *backend) {
		resolved, ok := resolve(cat, []string{backend.Stack}, typed)
		if !ok || seen[resolved.key()] {
			continue
		}
		seen[resolved.key()] = true
		legs = append(legs, resolved)
		bases = append(bases, baseLeg{typed: typed, leg: resolved})
	}
	for _, base := range bases {
		isFull := len(base.leg.Modules) == len(backend.Modules)
		for _, frontend := range frontends {
			resolved, ok := resolve(cat, []string{backend.Stack, frontend.Stack}, base.typed)
			if !ok || seen[resolved.key()] {
				continue
			}
			if !isFull && !implementsAll(*backend, base.leg.Modules, frontend) {
				continue
			}
			seen[resolved.key()] = true
			legs = append(legs, resolved)
		}
	}
	if multi, ok := multiLeg(cat, *backend, frontends); ok && !seen[multi.key()] {
		legs = append(legs, multi)
	}
	sort.Slice(legs, func(i, j int) bool {
		left, right := strings.Join(legs[i].Stacks, ","), strings.Join(legs[j].Stacks, ",")
		if left != right {
			return left < right
		}
		if modules := strings.Compare(strings.Join(legs[i].Modules, ","), strings.Join(legs[j].Modules, ",")); modules != 0 {
			return modules < 0
		}
		return legs[i].key() < legs[j].key()
	})
	return legs
}

// Names of the synthetic components of the multi candidate.
const (
	multiProject = "legs"
	multiFirst   = multiProject + "-srv-first"
	multiSecond  = multiProject + "-srv-second"
	multiWeb     = multiProject + "-web"
)

// multiLeg resolves the multi candidate: backend index 0 starts from the
// modules that own per-instance infrastructure, backend index 1 from every
// module, and the first frontend stack binds to index 1. It reports false when
// the plan refuses the candidate.
func multiLeg(cat catalog.Catalog, backend catalog.StackEntry, frontends []catalog.StackEntry) (Leg, bool) {
	var owners, all []string
	for _, module := range backend.Modules {
		if module.Module == manifest.CoreModule {
			continue
		}
		all = append(all, module.Module)
		hasInfrastructure := len(module.Ports) > 0 || len(module.PerComponent) > 0
		if !module.Implied && hasInfrastructure {
			owners = append(owners, module.Module)
		}
	}
	sort.Strings(owners)
	sort.Strings(all)
	sel := manifest.Selection{
		Schema:  2,
		Project: manifest.Project{Name: multiProject, GoModule: "example.com/legs"},
		Stacks:  []string{backend.Stack},
		Modules: owners,
		Components: []manifest.Component{
			{Stack: backend.Stack, Name: multiFirst},
			{Stack: backend.Stack, Name: multiSecond, Modules: all, HasModules: true},
		},
	}
	if len(frontends) > 0 {
		sel.Stacks = append(sel.Stacks, frontends[0].Stack)
		sel.Components = append(sel.Components, manifest.Component{Stack: frontends[0].Stack, Name: multiWeb, Backend: multiSecond})
	}
	sort.Strings(sel.Stacks)
	resolved, issues := plan.Resolve(cat, sel)
	if len(issues) > 0 || !resolved.Multi() {
		return Leg{}, false
	}
	return legOf(resolved), true
}

// legOf builds the leg of a resolved plan; a multi plan adds its components.
func legOf(resolved plan.Plan) Leg {
	leg := Leg{Stacks: resolved.Selection.Stacks, Modules: resolved.Selection.Modules}
	if !resolved.Multi() {
		return leg
	}
	backendIndex := map[string]int{}
	for _, component := range resolved.Components {
		if component.Role == roleBackend {
			backendIndex[component.Name] = component.Index
		}
	}
	for _, component := range resolved.Components {
		entry := LegComponent{Stack: component.Stack, Index: component.Index, Backend: -1}
		for _, module := range component.Modules {
			entry.Modules = append(entry.Modules, module.Module)
		}
		if component.Role == roleFrontend {
			entry.Backend = backendIndex[component.Partner]
		}
		leg.Components = append(leg.Components, entry)
	}
	return leg
}

// splitStacks returns the backend stack and the frontend stacks of cat.
func splitStacks(cat catalog.Catalog) (*catalog.StackEntry, []catalog.StackEntry) {
	var backend *catalog.StackEntry
	var frontends []catalog.StackEntry
	for i, stack := range cat.Stacks {
		switch stack.Role {
		case roleBackend:
			if backend == nil {
				backend = &cat.Stacks[i]
			}
		case roleFrontend:
			frontends = append(frontends, stack)
		}
	}
	sort.Slice(frontends, func(i, j int) bool { return frontends[i].Stack < frontends[j].Stack })
	return backend, frontends
}

// backendCandidates lists the typed module selections of the backend stack:
// none, each optional module, each cooperating pair, all optional modules.
func backendCandidates(cat catalog.Catalog, backend catalog.StackEntry) [][]string {
	var optional []string
	implementer := map[string]string{}
	for _, module := range backend.Modules {
		for spec := range module.ImplementsSpec {
			implementer[spec] = module.Module
		}
		if module.Module != manifest.CoreModule {
			optional = append(optional, module.Module)
		}
	}
	sort.Strings(optional)
	candidates := [][]string{nil}
	for _, id := range optional {
		candidates = append(candidates, []string{id})
	}
	owner := map[string]string{}
	for _, spec := range cat.Specs {
		for _, tag := range spec.TCKTags {
			owner[tag] = spec.Spec
		}
	}
	specs := append([]catalog.SpecEntry(nil), cat.Specs...)
	sort.Slice(specs, func(i, j int) bool { return specs[i].Spec < specs[j].Spec })
	for _, spec := range specs {
		tags := append([]string(nil), spec.CooperatesWith...)
		sort.Strings(tags)
		for _, tag := range tags {
			pair := distinctOptional(implementer[spec.Spec], implementer[owner[tag]])
			if len(pair) > 0 {
				candidates = append(candidates, pair)
			}
		}
	}
	candidates = append(candidates, optional)
	return candidates
}

// distinctOptional returns the sorted, distinct, non-empty ids other than core.
func distinctOptional(ids ...string) []string {
	set := map[string]bool{}
	for _, id := range ids {
		if id != "" && id != manifest.CoreModule {
			set[id] = true
		}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// resolve runs the plan for stacks and typed modules and returns its leg.
func resolve(cat catalog.Catalog, stacks, modules []string) (Leg, bool) {
	sel := manifest.Selection{
		Schema:  1,
		Project: manifest.Project{Name: "legs", GoModule: "example.com/legs"},
		Stacks:  stacks,
		Modules: modules,
	}
	resolved, issues := plan.Resolve(cat, sel)
	if len(issues) > 0 {
		return Leg{}, false
	}
	return Leg{Stacks: resolved.Selection.Stacks, Modules: resolved.Selection.Modules}, true
}

// implementsAll reports whether every spec implemented by the named backend
// modules is implemented by some module of the frontend stack.
func implementsAll(backend catalog.StackEntry, moduleIDs []string, frontend catalog.StackEntry) bool {
	provided := map[string]bool{}
	for _, module := range frontend.Modules {
		for spec := range module.ImplementsSpec {
			provided[spec] = true
		}
	}
	chosen := map[string]bool{}
	for _, id := range moduleIDs {
		chosen[id] = true
	}
	for _, module := range backend.Modules {
		if !chosen[module.Module] {
			continue
		}
		for spec := range module.ImplementsSpec {
			if !provided[spec] {
				return false
			}
		}
	}
	return true
}

// Check resolves every selection of files, keyed by file name, against cat
// and compares the legs with Derive. It reports one CodeDrift issue per file
// that cannot be resolved, per file whose leg is not derived, per file that
// resolves to the leg of an earlier file (by name), and per derived leg that
// has no file. Issue paths are "testdata/ci/<name>", or "testdata/ci" for a
// missing file. The issues are sorted.
func Check(cat catalog.Catalog, files map[string]manifest.Selection) []issue.Issue {
	derived := Derive(cat)
	inSet := map[string]bool{}
	for _, leg := range derived {
		inSet[leg.key()] = true
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var issues []issue.Issue
	covered := map[string]string{}
	for _, name := range names {
		path := filesDir + "/" + name
		resolved, found := plan.Resolve(cat, files[name])
		if len(found) > 0 {
			first := found[0]
			issues = append(issues, issue.Issue{Code: CodeDrift, Path: path,
				Message: fmt.Sprintf("selection is refused: %s %s: %s", first.Code, first.Path, first.Message)})
			continue
		}
		leg := legOf(resolved)
		if !inSet[leg.key()] {
			issues = append(issues, issue.Issue{Code: CodeDrift, Path: path, Message: "leg is not in the covering set"})
			continue
		}
		if other, taken := covered[leg.key()]; taken {
			issues = append(issues, issue.Issue{Code: CodeDrift, Path: path, Message: "resolves to the same leg as " + other})
			continue
		}
		covered[leg.key()] = name
	}
	for _, leg := range derived {
		if _, has := covered[leg.key()]; has {
			continue
		}
		issues = append(issues, issue.Issue{Code: CodeDrift, Path: filesDir,
			Message: fmt.Sprintf("no selection file for stacks %s modules %s", strings.Join(leg.Stacks, ","), strings.Join(leg.Modules, ","))})
	}
	issue.Sort(issues)
	return issues
}
