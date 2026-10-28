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
	out := make([]string, len(found))
	for i, leg := range found {
		out[i] = strings.Join(leg.Stacks, ",") + " " + strings.Join(leg.Modules, ",")
	}
	return out
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
				cat.Stacks[s].Modules[m].ImplementsSpec = map[string]string{"core": "0.1.0", "identity": "0.1.0"}
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
	if len(issues) != len(legs.Derive(cat)) {
		t.Fatalf("issues = %d, legs = %d", len(issues), len(legs.Derive(cat)))
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
