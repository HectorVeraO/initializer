package legs_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/legs"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

func selection(stacks []string, modules ...string) manifest.Selection {
	return manifest.Selection{
		Schema:  1,
		Project: manifest.Project{Name: "acme-backend", GoModule: "example.com/acme/backend"},
		Stacks:  stacks,
		Modules: modules,
	}
}

func describe(found []legs.Leg) []string {
	var out []string
	for _, leg := range found {
		if len(leg.Components) > 0 {
			continue
		}
		out = append(out, strings.Join(leg.Stacks, ",")+" "+strings.Join(leg.Modules, ","))
	}
	return out
}

// multiLegs returns the legs that hold components.
func multiLegs(found []legs.Leg) []legs.Leg {
	var out []legs.Leg
	for _, leg := range found {
		if len(leg.Components) > 0 {
			out = append(out, leg)
		}
	}
	return out
}

// multiSelection is the selection file of the multi leg.
func multiSelection(stacks []string) manifest.Selection {
	sel := selection(stacks, "identity")
	sel.Schema = 2
	sel.Components = []manifest.Component{
		{Stack: "go-srv", Name: "acme-backend-srv-first"},
		{Stack: "go-srv", Name: "acme-backend-srv-second", Modules: []string{"api-keys", "feature-flags", "identity", "parameters", "tenancy"}, HasModules: true},
	}
	if len(stacks) > 1 {
		sel.Components = append(sel.Components, manifest.Component{Stack: "ng-web", Name: "acme-backend-web", Backend: "acme-backend-srv-second"})
	}
	return sel
}

// The fixture catalog has the modules core, identity, parameters, feature-flags,
// tenancy and api-keys; parameters cooperates with tenancy; ng-web holds core,
// identity, feature-flags and parameters.
func TestDeriveReturnsTheCoveringSetInOrder(t *testing.T) {
	got := describe(legs.Derive(fixture.Load(t).Catalog))
	want := []string{
		"go-srv core",
		"go-srv core,identity",
		"go-srv core,identity,api-keys",
		"go-srv core,identity,api-keys,feature-flags,parameters,tenancy",
		"go-srv core,identity,feature-flags",
		"go-srv core,identity,parameters",
		"go-srv core,identity,parameters,tenancy",
		"go-srv core,identity,tenancy",
	}
	filtered := make([]string, 0, len(got))
	for _, leg := range got {
		if !strings.HasPrefix(leg, "go-srv,ng-web") {
			filtered = append(filtered, leg)
		}
	}
	if !reflect.DeepEqual(sortedCopy(filtered), sortedCopy(want)) {
		t.Fatalf("backend legs = %v, want %v", filtered, want)
	}
	isOrdered := true
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			isOrdered = false
		}
	}
	if !isOrdered {
		t.Fatalf("legs are not ordered: %v", got)
	}
}

func sortedCopy(values []string) []string {
	out := append([]string(nil), values...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestDeriveKeepsAWebTwinOnlyWhenTheFrontendImplementsTheSpecs(t *testing.T) {
	var web []string
	for _, leg := range describe(legs.Derive(fixture.Load(t).Catalog)) {
		if strings.HasPrefix(leg, "go-srv,ng-web ") {
			web = append(web, strings.TrimPrefix(leg, "go-srv,ng-web "))
		}
	}
	// tenancy and api-keys have no ng-web module, so only the full set keeps
	// a twin among the legs that include them.
	want := []string{
		"core",
		"core,identity",
		"core,identity,api-keys,feature-flags,parameters,tenancy",
		"core,identity,feature-flags",
		"core,identity,parameters",
	}
	if !reflect.DeepEqual(web, want) {
		t.Fatalf("web legs = %v, want %v", web, want)
	}
}

func TestDeriveDropsAWebCandidateThePlanRefuses(t *testing.T) {
	cat := fixture.Load(t).Catalog
	// ng-web core implements the identity spec: without a backend identity
	// module the plan refuses the twin of the core-only leg.
	for s := range cat.Stacks {
		if cat.Stacks[s].Stack != "ng-web" {
			continue
		}
		for m := range cat.Stacks[s].Modules {
			if cat.Stacks[s].Modules[m].Module == "core" {
				cat.Stacks[s].Modules[m].ImplementsSpec = map[string]string{"core": "0.1.0", "ghost-spec": "0.1.0"}
			}
		}
	}
	for _, leg := range describe(legs.Derive(cat)) {
		if leg == "go-srv,ng-web core" {
			t.Fatalf("the refused candidate %q was kept", leg)
		}
	}
}

func TestDeriveIsDeterministic(t *testing.T) {
	cat := fixture.Load(t).Catalog
	first := legs.Derive(cat)
	for i := 0; i < 5; i++ {
		if again := legs.Derive(cat); !reflect.DeepEqual(first, again) {
			t.Fatalf("run %d differs:\n%v\n%v", i, describe(first), describe(again))
		}
	}
}

func TestDeriveAddsACooperationPair(t *testing.T) {
	cat := fixture.Load(t).Catalog
	with := describe(legs.Derive(cat))
	if !contains(with, "go-srv core,identity,parameters,tenancy") {
		t.Fatalf("no leg for the parameters and tenancy cooperation: %v", with)
	}
	for i := range cat.Specs {
		cat.Specs[i].CooperatesWith = nil
	}
	without := describe(legs.Derive(cat))
	if contains(without, "go-srv core,identity,parameters,tenancy") {
		t.Fatalf("the pair stays without cooperatesWith: %v", without)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestDeriveWithoutABackendStackIsEmpty(t *testing.T) {
	if got := legs.Derive(catalog.Catalog{}); len(got) != 0 {
		t.Fatalf("legs = %v", got)
	}
}

// completeFiles builds one selection file per derived leg.
func completeFiles(t *testing.T, cat catalog.Catalog) map[string]manifest.Selection {
	t.Helper()
	files := map[string]manifest.Selection{}
	for _, leg := range legs.Derive(cat) {
		if len(leg.Components) > 0 {
			files["multi.json"] = multiSelection(leg.Stacks)
			continue
		}
		name := strings.ReplaceAll(strings.Join(leg.Stacks, "+")+"-"+strings.Join(leg.Modules, "+"), ",", "+") + ".json"
		files[name] = selection(leg.Stacks, leg.Modules...)
	}
	return files
}

func driftMessages(issues []issue.Issue) map[string]string {
	out := map[string]string{}
	for _, found := range issues {
		out[found.Path+" "+found.Message] = found.Code
	}
	return out
}

func TestCheckAcceptsTheDerivedSet(t *testing.T) {
	cat := fixture.Load(t).Catalog
	if issues := legs.Check(cat, completeFiles(t, cat)); len(issues) != 0 {
		t.Fatalf("issues = %v", issues)
	}
}

func TestCheckReportsEveryKindOfDrift(t *testing.T) {
	cat := fixture.Load(t).Catalog
	files := completeFiles(t, cat)

	// A file outside the covering set.
	files["extra.json"] = selection([]string{"go-srv"}, "api-keys", "parameters")
	// A refused selection.
	files["refused.json"] = selection([]string{"go-srv"}, "ghost")
	// A second file for a leg that already has one.
	files["twin.json"] = selection([]string{"go-srv"}, "identity")
	// A derived leg without a file.
	delete(files, "go-srv-core+identity+feature-flags.json")

	issues := legs.Check(cat, files)
	got := driftMessages(issues)
	wants := []string{
		"testdata/ci/extra.json leg is not in the covering set",
		"testdata/ci no selection file for stacks go-srv modules core,identity,feature-flags",
	}
	for _, want := range wants {
		if got[want] != legs.CodeDrift {
			t.Errorf("missing %q in %v", want, got)
		}
	}
	hasRefused, hasTwin := false, false
	for _, found := range issues {
		if found.Code != legs.CodeDrift {
			t.Errorf("code = %s", found.Code)
		}
		if found.Path == "testdata/ci/refused.json" && strings.HasPrefix(found.Message, "selection is refused: ") {
			hasRefused = true
		}
		if strings.HasPrefix(found.Path, "testdata/ci/") && strings.HasPrefix(found.Message, "resolves to the same leg as ") {
			hasTwin = true
		}
	}
	if !hasRefused || !hasTwin {
		t.Errorf("refused=%v twin=%v in %v", hasRefused, hasTwin, issues)
	}
	for i := 1; i < len(issues); i++ {
		if issues[i-1].Path > issues[i].Path {
			t.Fatalf("issues are not sorted: %v", issues)
		}
	}
}

func TestCheckEqualSelectionsWithDifferentTypedModulesAreOneLeg(t *testing.T) {
	cat := fixture.Load(t).Catalog
	files := completeFiles(t, cat)
	// {parameters} resolves to core, identity, parameters like the typed pair.
	files["a-typed-pair.json"] = selection([]string{"go-srv"}, "identity", "parameters")
	issues := legs.Check(cat, files)
	if len(issues) != 1 || !strings.HasPrefix(issues[0].Message, "resolves to the same leg as ") {
		t.Fatalf("issues = %v", issues)
	}
}

func TestCheckOfNoFilesNamesEveryLeg(t *testing.T) {
	cat := fixture.Load(t).Catalog
	issues := legs.Check(cat, nil)
	got := driftMessages(issues)
	derived := legs.Derive(cat)
	if len(issues) != len(derived) {
		t.Fatalf("issues = %d, legs = %d", len(issues), len(derived))
	}
	for _, leg := range derived {
		want := "testdata/ci no selection file for stacks " + strings.Join(leg.Stacks, ",") + " modules " + strings.Join(leg.Modules, ",")
		if got[want] != legs.CodeDrift {
			t.Errorf("missing %q in %v", want, got)
		}
	}
}

func TestDeriveListsALegOnceWhenTwoCandidatesResolveToIt(t *testing.T) {
	cat := fixture.Load(t).Catalog
	// api-keys cooperating with identity gives the pair {api-keys, identity},
	// which resolves like api-keys alone.
	for i := range cat.Specs {
		if cat.Specs[i].Spec == "api-keys" {
			cat.Specs[i].CooperatesWith = []string{"@identity"}
		}
	}
	count := 0
	for _, leg := range describe(legs.Derive(cat)) {
		if leg == "go-srv core,identity,api-keys" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("leg listed %d times", count)
	}
}

func TestDeriveAddsOneMultiLegWithABoundFrontend(t *testing.T) {
	found := multiLegs(legs.Derive(fixture.Load(t).Catalog))
	if len(found) != 1 {
		t.Fatalf("multi legs = %d", len(found))
	}
	got := found[0]
	if !reflect.DeepEqual(got.Stacks, []string{"go-srv", "ng-web"}) {
		t.Fatalf("stacks = %v", got.Stacks)
	}
	want := []legs.LegComponent{
		{Stack: "go-srv", Index: 0, Modules: []string{"core", "identity"}, Backend: -1},
		{Stack: "go-srv", Index: 1, Modules: []string{"core", "identity", "api-keys", "feature-flags", "parameters", "tenancy"}, Backend: -1},
		{Stack: "ng-web", Index: 0, Modules: []string{"core", "identity", "feature-flags", "parameters"}, Backend: 1},
	}
	if !reflect.DeepEqual(got.Components, want) {
		t.Fatalf("components = %+v, want %+v", got.Components, want)
	}
}

func TestDeriveAddsTheMultiLegWithoutAFrontendStack(t *testing.T) {
	cat := fixture.Load(t).Catalog
	backendOnly := cat
	backendOnly.Stacks = nil
	for _, stack := range cat.Stacks {
		if stack.Role == "backend" {
			backendOnly.Stacks = append(backendOnly.Stacks, stack)
		}
	}
	found := multiLegs(legs.Derive(backendOnly))
	if len(found) != 1 || !reflect.DeepEqual(found[0].Stacks, []string{"go-srv"}) || len(found[0].Components) != 2 {
		t.Fatalf("multi legs = %+v", found)
	}
}

func TestDeriveStartsIndexZeroFromTheModulesThatOwnInstanceFiles(t *testing.T) {
	cat := fixture.Load(t).Catalog
	for s := range cat.Stacks {
		if cat.Stacks[s].Role != "backend" {
			continue
		}
		for m := range cat.Stacks[s].Modules {
			if cat.Stacks[s].Modules[m].Module == "parameters" {
				cat.Stacks[s].Modules[m].PerComponent = []string{"cfg/parameters.yaml"}
			}
		}
	}
	found := multiLegs(legs.Derive(cat))
	if len(found) != 1 {
		t.Fatalf("multi legs = %d", len(found))
	}
	first := found[0].Components[0].Modules
	wantFirst := []string{"core", "identity", "parameters"}
	if !reflect.DeepEqual(first, wantFirst) {
		t.Fatalf("index 0 modules = %v, want %v", first, wantFirst)
	}
}

func TestDeriveWithoutInstanceOwnersGivesNoMultiLegBeyondCore(t *testing.T) {
	cat := fixture.Load(t).Catalog
	for s := range cat.Stacks {
		for m := range cat.Stacks[s].Modules {
			cat.Stacks[s].Modules[m].Ports = nil
			cat.Stacks[s].Modules[m].PerComponent = nil
		}
	}
	found := multiLegs(legs.Derive(cat))
	if len(found) != 1 || !reflect.DeepEqual(found[0].Components[0].Modules, []string{"core"}) {
		t.Fatalf("multi legs = %+v", found)
	}
}

func TestCheckMatchesTheMultiFileAndReportsItsDrift(t *testing.T) {
	cat := fixture.Load(t).Catalog
	files := completeFiles(t, cat)
	if issues := legs.Check(cat, files); len(issues) != 0 {
		t.Fatalf("issues = %v", issues)
	}

	// A different module list on the second backend is another leg.
	drifted := multiSelection([]string{"go-srv", "ng-web"})
	drifted.Components[1].Modules = []string{"api-keys", "identity"}
	files["multi.json"] = drifted
	got := driftMessages(legs.Check(cat, files))
	if got["testdata/ci/multi.json leg is not in the covering set"] != legs.CodeDrift {
		t.Fatalf("drift = %v", got)
	}

	// Without the file the multi leg is named as missing.
	delete(files, "multi.json")
	missing := false
	for _, found := range legs.Check(cat, files) {
		if found.Path == "testdata/ci" && strings.HasPrefix(found.Message, "no selection file for stacks go-srv,ng-web") {
			missing = true
		}
	}
	if !missing {
		t.Fatal("the missing multi file is not reported")
	}
}

func TestCheckKeepsNamesOutOfTheLegIdentity(t *testing.T) {
	cat := fixture.Load(t).Catalog
	files := completeFiles(t, cat)
	renamed := multiSelection([]string{"go-srv", "ng-web"})
	renamed.Components[0].Name = "acme-backend-api"
	renamed.Components[1].Name = "acme-backend-core"
	renamed.Components[2].Backend = "acme-backend-core"
	files["multi.json"] = renamed
	if issues := legs.Check(cat, files); len(issues) != 0 {
		t.Fatalf("issues = %v", issues)
	}
}

func TestDeriveDropsTheMultiLegThePlanRefuses(t *testing.T) {
	breakFrontend := func(cat *catalog.Catalog) {
		for s := range cat.Stacks {
			if cat.Stacks[s].Role != "frontend" {
				continue
			}
			for m := range cat.Stacks[s].Modules {
				if cat.Stacks[s].Modules[m].Module == "core" {
					cat.Stacks[s].Modules[m].ImplementsSpec = map[string]string{"core": "0.1.0", "ghost-spec": "0.1.0"}
				}
			}
		}
	}
	breakBackend := func(cat *catalog.Catalog) {
		for s := range cat.Stacks {
			if cat.Stacks[s].Role != "backend" {
				continue
			}
			var kept []catalog.ModuleEntry
			for _, module := range cat.Stacks[s].Modules {
				if module.Module != "identity" {
					kept = append(kept, module)
				}
			}
			cat.Stacks[s].Modules = kept
		}
	}
	cases := map[string][]func(*catalog.Catalog){
		"backend only":  {breakBackend},
		"frontend only": {breakFrontend},
		"both":          {breakBackend, breakFrontend},
	}
	for name, breakers := range cases {
		t.Run(name, func(t *testing.T) {
			cat := fixture.Load(t).Catalog
			for _, apply := range breakers {
				apply(&cat)
			}
			if found := multiLegs(legs.Derive(cat)); len(found) != 0 {
				t.Fatalf("the refused multi leg was kept: %+v", found)
			}
		})
	}
}

func TestDeriveListsTheMultiLegAfterTheSingleLegsOfItsStacks(t *testing.T) {
	found := legs.Derive(fixture.Load(t).Catalog)
	position := -1
	for i, leg := range found {
		if len(leg.Components) > 0 {
			position = i
		}
	}
	if position < 0 {
		t.Fatal("no multi leg")
	}
	for i, leg := range found {
		if i > position && strings.Join(leg.Stacks, ",") == "go-srv,ng-web" && strings.Join(leg.Modules, ",") < strings.Join(found[position].Modules, ",") {
			t.Fatalf("leg %d (%v) sorts before the multi leg at %d", i, leg.Modules, position)
		}
	}
	for i := 1; i < len(found); i++ {
		left, right := strings.Join(found[i-1].Stacks, ","), strings.Join(found[i].Stacks, ",")
		if left > right {
			t.Fatalf("legs not ordered by stacks at %d: %q > %q", i, left, right)
		}
	}
}
