package plan_test

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
)

// multiSelection selects both stacks with the given entries and no top-level modules.
func multiSelection(entries ...manifest.Component) manifest.Selection {
	return manifest.Selection{
		Schema:     2,
		Project:    manifest.Project{Name: "acme", GoModule: "example.com/acme"},
		Stacks:     []string{"go-srv", "ng-web"},
		Components: entries,
	}
}

func backendEntry(name string, modules ...string) manifest.Component {
	entry := manifest.Component{Stack: "go-srv", Name: name}
	if modules != nil {
		entry.Modules, entry.HasModules = modules, true
	}
	return entry
}

func webEntry(name, backend string) manifest.Component {
	return manifest.Component{Stack: "ng-web", Name: name, Backend: backend}
}

// byName returns the component called name, failing the test when absent.
func byName(t *testing.T, p plan.Plan, name string) plan.ComponentPlan {
	t.Helper()
	for _, component := range p.Components {
		if component.Name == name {
			return component
		}
	}
	t.Fatalf("no component %s in %+v", name, nameOnly(p))
	return plan.ComponentPlan{}
}

// withInfraRules gives the fixture catalog the name rules of a database and
// its services: postgres always, kratos with identity.
func withInfraRules(t *testing.T) catalog.Catalog {
	t.Helper()
	cat := load(t, "fullstack.json")
	for _, slot := range []string{"database", "dbUser", "service.postgres", "service.kratos", "service.kratos-migrate"} {
		cat.Reference[slot] = "ref-" + slot
	}
	core := []catalog.NameRule{{Path: "a", Text: "db: %{database} %{dbUser} %{service.postgres}", Blocks: []string{""}}}
	identity := []catalog.NameRule{
		{Path: "b", Text: "k: %{service.kratos}", Blocks: []string{"module:identity"}},
		{Path: "c", Text: "m: %{service.kratos-migrate}", Blocks: []string{"module:identity"}},
	}
	return withRules(t, cat, core, identity)
}

func TestEachBackendPlansWithItsOwnFrontendInstalledFromItsClosure(t *testing.T) {
	sel := multiSelection(
		backendEntry("acme-srv-first", "identity"),
		backendEntry("acme-srv-second", "parameters", "feature-flags"),
		webEntry("acme-web", "acme-srv-second"))
	p := mustResolve(t, load(t, "fullstack.json"), sel)
	if !p.Multi() {
		t.Fatal("two backend components are a multi plan")
	}
	got := []string{}
	for _, component := range p.Components {
		got = append(got, component.Stack+"/"+component.Name)
	}
	want := []string{"go-srv/acme-srv-first", "go-srv/acme-srv-second", "ng-web/acme-web"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("components = %v, want %v", got, want)
	}
	wantIDs(t, byName(t, p, "acme-srv-first"), "core", "identity")
	wantIDs(t, byName(t, p, "acme-srv-second"), "core", "identity", "feature-flags", "parameters")
	web := byName(t, p, "acme-web")
	wantIDs(t, web, "core", "feature-flags", "parameters")
	if web.Partner != "acme-srv-second" || byName(t, p, "acme-srv-second").Partner != "acme-web" || byName(t, p, "acme-srv-first").Partner != "" {
		t.Fatalf("partners = %q %q", web.Partner, byName(t, p, "acme-srv-second").Partner)
	}
	if web.Index != 0 || byName(t, p, "acme-srv-second").Index != 1 {
		t.Fatalf("indexes = %d %d", web.Index, byName(t, p, "acme-srv-second").Index)
	}
}

func TestABackendWithoutItsOwnListStartsFromTheTopLevelList(t *testing.T) {
	sel := multiSelection(backendEntry("acme-srv-first", "identity"), backendEntry("acme-srv-second"))
	sel.Stacks = []string{"go-srv"}
	sel.Modules = []string{"parameters"}
	p := mustResolve(t, load(t, "fullstack.json"), sel)
	wantIDs(t, byName(t, p, "acme-srv-first"), "core", "identity")
	wantIDs(t, byName(t, p, "acme-srv-second"), "core", "identity", "parameters")
	sel.AllModules = true
	p = mustResolve(t, load(t, "fullstack.json"), sel)
	wantIDs(t, byName(t, p, "acme-srv-first"), "core", "identity")
	wantIDs(t, byName(t, p, "acme-srv-second"), "core", "identity", "feature-flags", "parameters")
}

// Per-pair planning is the single-project algorithm: any split of modules over
// two backends gives each backend the plan of a project that lists those modules.
func TestEachBackendEqualsTheSingleProjectPlanOfItsList(t *testing.T) {
	cat := load(t, "fullstack.json")
	pool := []string{"identity", "parameters", "feature-flags", "core"}
	rng := rand.New(rand.NewSource(7))
	pick := func() []string {
		picked := []string{}
		for _, id := range pool {
			if rng.Intn(2) == 0 {
				picked = append(picked, id)
			}
		}
		return picked
	}
	for i := 0; i < 40; i++ {
		first, second := pick(), pick()
		sel := multiSelection(backendEntry("acme-srv-first", first...), backendEntry("acme-srv-second", second...))
		sel.Components[0].HasModules, sel.Components[1].HasModules = true, true
		sel.Stacks = []string{"go-srv"}
		multi := mustResolve(t, cat, sel)
		for name, list := range map[string][]string{"acme-srv-first": first, "acme-srv-second": second} {
			alone := mustResolve(t, cat, selection(list...))
			if got, want := ids(byName(t, multi, name)), ids(alone.Components[0]); !reflect.DeepEqual(got, want) {
				t.Fatalf("%s from %v = %v, the single plan gives %v", name, list, got, want)
			}
		}
	}
}

func frontendModule(t *testing.T, cat *catalog.Catalog, id string) *catalog.ModuleEntry {
	t.Helper()
	for s := range cat.Stacks {
		for m := range cat.Stacks[s].Modules {
			if cat.Stacks[s].Role == "frontend" && cat.Stacks[s].Modules[m].Module == id {
				return &cat.Stacks[s].Modules[m]
			}
		}
	}
	t.Fatalf("no frontend module %s", id)
	return nil
}

func TestAFrontendNeedsItsOwnBackendToImplementItsSpecs(t *testing.T) {
	cat := load(t, "fullstack.json")
	frontendModule(t, &cat, "parameters").ImplementsSpec = map[string]string{"parameters": "0.1.0", "feature-flags": "0.1.0"}
	sel := multiSelection(
		backendEntry("acme-srv-first", "parameters"),
		backendEntry("acme-srv-second", "parameters", "feature-flags"),
		webEntry("acme-web", "acme-srv-first"))
	issues := wantCodes(t, cat, sel, plan.CodeSpecNotInBackend)
	want := `no module of backend component acme-srv-first implements spec "feature-flags" (needed by ng-web parameters in acme-web)`
	if issues[0].Message != want {
		t.Fatalf("message = %q, want %q", issues[0].Message, want)
	}
	sel.Components[2].Backend = "acme-srv-second"
	mustResolve(t, cat, sel)
}

func TestMissingModulesAreReportedAtTheListThatGaveThem(t *testing.T) {
	cat := load(t, "fullstack.json")
	own := multiSelection(backendEntry("acme-srv-first"), backendEntry("acme-srv-second", "ghost"))
	own.Stacks = []string{"go-srv"}
	issues := wantCodes(t, cat, own, plan.CodeModuleUnknown)
	if !strings.HasSuffix(issues[0].Path, "#/components/1/modules/0") {
		t.Fatalf("path = %s", issues[0].Path)
	}
	inherited := multiSelection(backendEntry("acme-srv-first"), backendEntry("acme-srv-second"))
	inherited.Stacks = []string{"go-srv"}
	inherited.Modules = []string{"ghost"}
	issues = wantCodes(t, cat, inherited, plan.CodeModuleUnknown, plan.CodeModuleUnknown)
	for i, component := range []string{"acme-srv-first", "acme-srv-second"} {
		if !strings.HasSuffix(issues[i].Path, "#/modules/0") || !strings.HasSuffix(issues[i].Message, "(backend component "+component+")") {
			t.Fatalf("issue %d = %+v", i, issues[i])
		}
	}
}

func TestUnnamedComponentsStillPlanAndAreLeftToTheComponentChecks(t *testing.T) {
	cat := load(t, "fullstack.json")
	unnamed := multiSelection(backendEntry("", "ghost"), backendEntry(""))
	unnamed.Stacks = []string{"go-srv"}
	wantCodes(t, cat, unnamed, plan.CodeModuleUnknown)

	clean := multiSelection(backendEntry(""), backendEntry(""))
	clean.Stacks = []string{"go-srv"}
	issues := wantCodes(t, cat, clean, plan.CodeInternalError)
	if issues[0].Message != plan.UndeterminedMessage {
		t.Fatalf("message = %q", issues[0].Message)
	}
}

func TestMultiDefaultsAreDerivedPerComponent(t *testing.T) {
	cat := withInfraRules(t)
	sel := multiSelection(backendEntry("acme-srv-first"), backendEntry("acme-srv-second", "identity"))
	sel.Stacks = []string{"go-srv"}
	p := mustResolve(t, cat, sel)
	first, second := byName(t, p, "acme-srv-first"), byName(t, p, "acme-srv-second")
	if first.GoModule != "example.com/acme/acme-srv-first" || second.GoModule != "example.com/acme/acme-srv-second" {
		t.Fatalf("go modules = %q %q", first.GoModule, second.GoModule)
	}
	if first.Database != (manifest.Database{Name: "acme_srv_first", User: "acme_srv_first"}) {
		t.Fatalf("database = %+v", first.Database)
	}
	if want := map[string]string{"postgres": "acme-srv-first-postgres"}; !reflect.DeepEqual(first.Services, want) {
		t.Fatalf("services = %v, want %v", first.Services, want)
	}
	wantSecond := map[string]string{
		"postgres": "acme-srv-second-postgres", "kratos": "acme-srv-second-kratos", "kratos-migrate": "acme-srv-second-kratos-migrate",
	}
	if !reflect.DeepEqual(second.Services, wantSecond) {
		t.Fatalf("services = %v, want %v", second.Services, wantSecond)
	}
	wantNote := "go modules derived from example.com/acme; pass go-module= per component to change them"
	if !reflect.DeepEqual(p.Notes, []string{wantNote}) {
		t.Fatalf("notes = %v", p.Notes)
	}
}

func TestEntryValuesOverrideTheDerivedDefaults(t *testing.T) {
	cat := withInfraRules(t)
	first := backendEntry("acme-srv-first")
	first.GoModule = "example.org/billing"
	first.Database = &manifest.Database{Name: "ledger", User: "ledger_owner"}
	first.Services = map[string]string{"postgres": "ledger-db"}
	second := backendEntry("acme-srv-second", "identity")
	second.GoModule = "example.org/orders"
	sel := multiSelection(first, second)
	sel.Stacks = []string{"go-srv"}
	sel.Database = &manifest.Database{Name: "shared", User: "shared_owner"}
	p := mustResolve(t, cat, sel)
	gotFirst, gotSecond := byName(t, p, "acme-srv-first"), byName(t, p, "acme-srv-second")
	if gotFirst.GoModule != "example.org/billing" || gotFirst.Database.Name != "ledger" || gotFirst.Services["postgres"] != "ledger-db" {
		t.Fatalf("first = %+v", gotFirst)
	}
	if gotSecond.GoModule != "example.org/orders" || gotSecond.Database != (manifest.Database{Name: "shared", User: "shared_owner"}) {
		t.Fatalf("second inherits the top-level database it alone lacks: %+v", gotSecond)
	}
	if len(p.Notes) != 0 {
		t.Fatalf("every Go module was given, notes = %v", p.Notes)
	}
}

func TestGoModulePathsMayNotBeEqualOrNested(t *testing.T) {
	cat := load(t, "fullstack.json")
	for name, paths := range map[string][2]string{
		"equal":  {"example.org/acme", "example.org/acme"},
		"nested": {"example.org/acme", "example.org/acme/billing"},
	} {
		t.Run(name, func(t *testing.T) {
			first, second := backendEntry("acme-srv-first"), backendEntry("acme-srv-second")
			first.GoModule, second.GoModule = paths[0], paths[1]
			sel := multiSelection(first, second)
			sel.Stacks = []string{"go-srv"}
			issues := wantCodes(t, cat, sel, plan.CodeNameConflict, plan.CodeNameConflict)
			if !strings.HasSuffix(issues[0].Path, "#/components/0/goModule") || !strings.HasSuffix(issues[1].Path, "#/components/1/goModule") {
				t.Fatalf("paths = %s %s", issues[0].Path, issues[1].Path)
			}
		})
	}
	first, second := backendEntry("acme-srv-first"), backendEntry("acme-srv-second")
	first.GoModule, second.GoModule = "example.org/acme", "example.org/acme-billing"
	sel := multiSelection(first, second)
	sel.Stacks = []string{"go-srv"}
	mustResolve(t, cat, sel)
}

func TestServiceNamesMayNotRepeatAcrossInstancesOrMatchAComponent(t *testing.T) {
	cat := withInfraRules(t)
	first, second := backendEntry("acme-srv-first"), backendEntry("acme-srv-second")
	first.Services = map[string]string{"postgres": "shared-db"}
	second.Services = map[string]string{"postgres": "shared-db"}
	sel := multiSelection(first, second)
	sel.Stacks = []string{"go-srv"}
	issues := wantCodes(t, cat, sel, plan.CodeNameConflict, plan.CodeNameConflict)
	if !strings.HasSuffix(issues[0].Path, "#/components/0/services/postgres") {
		t.Fatalf("path = %s", issues[0].Path)
	}
	derived := multiSelection(backendEntry("acme-srv-first"), backendEntry("acme-srv-first-postgres"))
	derived.Stacks = []string{"go-srv"}
	wantCodes(t, cat, derived, plan.CodeNameConflict, plan.CodeNameConflict)
}

func TestDerivedServiceNamesAreCheckedAgainst63BytesPerInstance(t *testing.T) {
	cat := withInfraRules(t)
	long := strings.Repeat("a", 49)
	sel := multiSelection(backendEntry("acme-srv-first"), backendEntry(long, "identity"))
	sel.Stacks = []string{"go-srv"}
	issues := wantCodes(t, cat, sel, plan.CodeNameTooLong)
	if !strings.HasSuffix(issues[0].Path, "#/components/1/name") || !strings.Contains(issues[0].Message, "kratos-migrate") {
		t.Fatalf("issue = %+v", issues[0])
	}
	sel.Components[1].Name = strings.Repeat("a", 48)
	mustResolve(t, cat, sel)
	withoutIdentity := multiSelection(backendEntry("acme-srv-first"), backendEntry(long))
	withoutIdentity.Stacks = []string{"go-srv"}
	mustResolve(t, cat, withoutIdentity)
}

func TestAnEntryOverrideOfAServiceTheInstanceLacksIsUnused(t *testing.T) {
	cat := withInfraRules(t)
	first := backendEntry("acme-srv-first")
	first.Services = map[string]string{"kratos": "auth"}
	second := backendEntry("acme-srv-second", "identity")
	second.Services = map[string]string{"kratos": "auth-two"}
	sel := multiSelection(first, second)
	sel.Stacks = []string{"go-srv"}
	issues := wantCodes(t, cat, sel, plan.CodeNameOverrideUnused)
	if !strings.HasSuffix(issues[0].Path, "#/components/0/services/kratos") {
		t.Fatalf("path = %s", issues[0].Path)
	}
}

func TestEachBackendHasItsOwnTCKSelectionAndTheTopLevelOnesAreIndexZeros(t *testing.T) {
	cat := load(t, "fullstack.json")
	for i := range cat.Specs {
		if cat.Specs[i].Spec == "feature-flags" {
			cat.Specs[i].TCKEnv = map[string]string{"APP_FLAGS_FILE": "specs/feature-flags/tck/flags.yaml"}
		}
	}
	sel := multiSelection(
		backendEntry("acme-srv-first", "identity"),
		backendEntry("acme-srv-second", "feature-flags"),
		webEntry("acme-web", "acme-srv-second"))
	p := mustResolve(t, cat, sel)
	first, second := byName(t, p, "acme-srv-first"), byName(t, p, "acme-srv-second")
	if first.TCK == nil || second.TCK == nil || byName(t, p, "acme-web").TCK != nil {
		t.Fatalf("tck = %+v %+v", first.TCK, second.TCK)
	}
	if len(first.TCK.Env) != 0 || second.TCK.Env["APP_FLAGS_FILE"] == "" {
		t.Fatalf("env = %v %v", first.TCK.Env, second.TCK.Env)
	}
	if !reflect.DeepEqual(first.TCK.Tags, []string{"@core", "@identity", "@otel"}) || p.TCKGrep != first.TCK.Grep || !reflect.DeepEqual(p.TCKTags, first.TCK.Tags) {
		t.Fatalf("top-level tck = %v %q, index 0 = %+v", p.TCKTags, p.TCKGrep, first.TCK)
	}
	if len(p.TCKEnv) != 0 {
		t.Fatalf("the top-level env is index 0's, not the union: %v", p.TCKEnv)
	}
	specs := []string{}
	for _, spec := range p.Specs {
		specs = append(specs, spec.Spec)
	}
	if !reflect.DeepEqual(specs, []string{"core", "feature-flags", "identity"}) {
		t.Fatalf("specs = %v, want the union over both backends", specs)
	}
}

func TestACooperationTagIsInvertedOnlyForTheBackendThatLacksIt(t *testing.T) {
	cat := load(t, "fullstack.json")
	for i := range cat.Specs {
		if cat.Specs[i].Spec == "parameters" {
			cat.Specs[i].CooperatesWith = []string{"@feature-flags"}
		}
	}
	sel := multiSelection(backendEntry("acme-srv-first", "parameters"), backendEntry("acme-srv-second", "parameters", "feature-flags"))
	sel.Stacks = []string{"go-srv"}
	p := mustResolve(t, cat, sel)
	if got, want := byName(t, p, "acme-srv-first").TCK.GrepInvert, plan.Grep([]string{"@feature-flags"}); got != want {
		t.Fatalf("first lacks feature-flags, invert = %q, want %q", got, want)
	}
	if got := byName(t, p, "acme-srv-second").TCK.GrepInvert; got != "" {
		t.Fatalf("second installs feature-flags, invert = %q", got)
	}
	if p.TCKGrepInvert != byName(t, p, "acme-srv-first").TCK.GrepInvert {
		t.Fatalf("the top-level invert is index 0's: %q", p.TCKGrepInvert)
	}
}

func TestAMultiPlanRecordsEachBackendsInstalledListAndReplays(t *testing.T) {
	cat := load(t, "fullstack.json")
	sel := multiSelection(
		backendEntry("acme-srv-first", "parameters"),
		backendEntry("acme-srv-second"),
		webEntry("acme-web", "acme-srv-first"))
	sel.Modules = []string{"identity"}
	p := mustResolve(t, cat, sel)
	if p.Selection.Modules != nil || p.Selection.Has.Modules {
		t.Fatalf("a multi record has no top-level modules: %+v", p.Selection)
	}
	recorded := map[string][]string{}
	for _, entry := range p.Selection.Components {
		recorded[entry.Name] = entry.Modules
	}
	want := map[string][]string{
		"acme-srv-first": {"core", "identity", "parameters"}, "acme-srv-second": {"core", "identity"}, "acme-web": nil,
	}
	if !reflect.DeepEqual(recorded, want) {
		t.Fatalf("recorded = %v, want %v", recorded, want)
	}
	if p.Selection.Components[2].HasModules {
		t.Fatal("a frontend entry takes no modules")
	}
	replayed := mustResolve(t, cat, p.Selection)
	if !reflect.DeepEqual(replayed, p) {
		t.Fatalf("the recorded selection must reproduce the plan:\n%+v\n%+v", replayed.Components, p.Components)
	}
}

func TestPullsNameTheComponentOnlyInAMultiProject(t *testing.T) {
	cat := load(t, "fullstack.json")
	sel := multiSelection(backendEntry("acme-srv-first", "identity"), backendEntry("acme-srv-second", "parameters"))
	sel.Stacks = []string{"go-srv"}
	pulls := plan.Pulls(mustResolve(t, cat, sel), sel)
	want := []plan.Pull{{Module: "identity", RequiredBy: []string{"parameters"}, Stack: "go-srv", Component: "acme-srv-second"}}
	if !reflect.DeepEqual(pulls, want) {
		t.Fatalf("pulls = %+v, want %+v", pulls, want)
	}
	single := selection("parameters")
	pulls = plan.Pulls(mustResolve(t, cat, single), single)
	if len(pulls) != 1 || pulls[0].Component != "" {
		t.Fatalf("a single project names no component: %+v", pulls)
	}
}

func TestATCKEnvConflictIsFoundPerBackend(t *testing.T) {
	cat := load(t, "fullstack.json")
	for i := range cat.Specs {
		switch cat.Specs[i].Spec {
		case "feature-flags":
			cat.Specs[i].TCKEnv = map[string]string{"APP_FLAGS_FILE": "a.yaml"}
		case "parameters":
			cat.Specs[i].TCKEnv = map[string]string{"APP_FLAGS_FILE": "b.yaml"}
		}
	}
	apart := multiSelection(backendEntry("acme-srv-first", "feature-flags"), backendEntry("acme-srv-second", "parameters"))
	apart.Stacks = []string{"go-srv"}
	mustResolve(t, cat, apart)
	together := multiSelection(backendEntry("acme-srv-first", "identity"), backendEntry("acme-srv-second", "feature-flags", "parameters"))
	together.Stacks = []string{"go-srv"}
	wantCodes(t, cat, together, plan.CodeTCKEnvConflict)
}

// The document a run writes is manifest.RecordedSelection: replaying it plans
// the same components.
func TestTheWrittenRecordedSelectionOfAMultiPlanReplaysToTheSamePlan(t *testing.T) {
	cat := load(t, "fullstack.json")
	sel := multiSelection(
		backendEntry("acme-srv-first", "parameters"),
		backendEntry("acme-srv-second"),
		webEntry("acme-web", "acme-srv-first"))
	sel.Modules = []string{"identity"}
	sel.Has = manifest.Presence{Name: true, GoModule: true, Stacks: true, Modules: true}
	p := mustResolve(t, cat, sel)
	var lists [][]string
	for _, component := range p.Components {
		if component.Role == "backend" {
			lists = append(lists, ids(component))
		}
	}
	recorded := manifest.RecordedSelection(manifest.Input{Selection: sel}, lists...)
	if recorded.Has.Modules || len(recorded.Modules) != 0 {
		t.Fatalf("a multi record has no top-level modules: %+v", recorded)
	}
	replayed := mustResolve(t, cat, recorded)
	if !reflect.DeepEqual(replayed.Components, p.Components) || !reflect.DeepEqual(replayed.Specs, p.Specs) {
		t.Fatalf("replayed plan differs:\n%+v\n%+v", replayed.Components, p.Components)
	}
}

func TestAGivenNameSharedByTwoStacksConflictsInAMultiProject(t *testing.T) {
	cat := load(t, "fullstack.json")
	sel := multiSelection(backendEntry("shared"), backendEntry("acme-srv-second"), webEntry("shared", "shared"))
	issues := wantCodes(t, cat, sel, plan.CodeNameConflict, plan.CodeNameConflict)
	if !strings.Contains(issues[0].Message, `"shared"`) {
		t.Fatalf("message = %q", issues[0].Message)
	}
}

// withInstanceRules gives the fixture catalog one rule that uses the derived
// instance slots and one of a module only some backends install.
func withInstanceRules(t *testing.T) catalog.Catalog {
	t.Helper()
	cat := load(t, "fullstack.json")
	for _, slot := range []string{"database", "dbUser", "service.postgres", "service.kratos", "service.kratos-migrate"} {
		cat.Reference[slot] = "ref-" + slot
	}
	core := []catalog.NameRule{
		{Path: "a", Text: "db: %{database} %{dbUser} %{service.postgres}", Blocks: []string{""}},
		{Path: "d", Text: "dir: %{instanceDir.go-srv} %{task.go-srv} %{task.ng-web} %{tckTask.go-srv}%{tckArgs.go-srv}", Blocks: []string{""}},
	}
	identity := []catalog.NameRule{
		{Path: "b", Text: "k: %{service.kratos}", Blocks: []string{"module:identity"}},
		{Path: "c", Text: "m: %{service.kratos-migrate}", Blocks: []string{"module:identity"}},
	}
	return withRules(t, cat, core, identity)
}

func TestMultiPlanGivesEachComponentTheSlotsOfItsInstance(t *testing.T) {
	sel := multiSelection(
		backendEntry("acme-srv-first", "identity"),
		backendEntry("acme-srv-second", "parameters"),
		webEntry("acme-web", "acme-srv-second"))
	p := mustResolve(t, withInstanceRules(t), sel)
	first, second, web := byName(t, p, "acme-srv-first"), byName(t, p, "acme-srv-second"), byName(t, p, "acme-web")
	if first.Slots["instanceDir.go-srv"] != "acme-srv-first/" || second.Slots["instanceDir.go-srv"] != "acme-srv-second/" {
		t.Errorf("instanceDir = %q %q", first.Slots["instanceDir.go-srv"], second.Slots["instanceDir.go-srv"])
	}
	if web.Slots["instanceDir.go-srv"] != "acme-srv-second/" || web.Slots["component.go-srv"] != "acme-srv-second" {
		t.Errorf("a frontend resolves the *.go-srv slots to its bound backend: %v", web.Slots)
	}
	if second.Slots["task.ng-web"] != "acme-web" || second.Slots["task.go-srv"] != "acme-srv-second" {
		t.Errorf("task slots of the second pair: %v", second.Slots)
	}
	if got := first.Slots["tckTask.go-srv"]; got != `"tck:acme-srv-first"` {
		t.Errorf("tckTask = %s", got)
	}
	if got := first.Slots["tckArgs.go-srv"]; got != " -- --component acme-srv-first" {
		t.Errorf("tckArgs = %q", got)
	}
	// The first pair has no frontend: its task.ng-web takes the single form so
	// the rule of the project still expands.
	if got := first.Slots["task.ng-web"]; got != "web" {
		t.Errorf("task.ng-web without a frontend = %q, want web", got)
	}
	for slot := range p.Live {
		for _, derived := range []string{"instanceDir", "instanceSuffix", "task.", "tckTask", "tckArgs"} {
			if strings.HasPrefix(slot, derived) {
				t.Errorf("live slot %s would reach the lock", slot)
			}
		}
	}
}

func TestSingleProjectKeepsTheReferenceFormOfTheInstanceSlots(t *testing.T) {
	sel := multiSelection(backendEntry("acme-srv", "identity"), webEntry("acme-web", ""))
	p := mustResolve(t, withInstanceRules(t), sel)
	for _, name := range []string{"acme-srv", "acme-web"} {
		slots := byName(t, p, name).Slots
		want := map[string]string{
			"instanceDir.go-srv": "", "instanceSuffix.go-srv": "", "task.go-srv": "srv", "task.ng-web": "web",
			"tckTask.go-srv": "tck", "tckArgs.go-srv": "",
		}
		for slot, value := range want {
			if got, ok := slots[slot]; !ok || got != value {
				t.Errorf("%s: %s = %q (present %v), want %q", name, slot, got, ok, value)
			}
		}
	}
	if _, found := p.LockNames()["task.go-srv"]; found {
		t.Error("the lock names hold a derived slot")
	}
}
