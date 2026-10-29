package plan_test

import (
	"reflect"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
)

// withRules gives the core and identity modules of the backend a name rule each.
func withRules(t *testing.T, cat catalog.Catalog, core, identity []catalog.NameRule) catalog.Catalog {
	t.Helper()
	backendModule(t, &cat, "core").Names = core
	backendModule(t, &cat, "identity").Names = identity
	return cat
}

func fullSelection(modules ...string) manifest.Selection {
	sel := selection(modules...)
	sel.Stacks = []string{"go-srv", "ng-web"}
	return sel
}

func TestLockNamesAreProjectAndSelectedComponents(t *testing.T) {
	p := mustResolve(t, load(t, "fullstack.json"), fullSelection())
	want := map[string]string{
		"project":          "acme-backend",
		"component.go-srv": "acme-backend-srv",
		"component.ng-web": "acme-backend-web",
	}
	if got := p.LockNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("LockNames = %v, want %v", got, want)
	}
}

func TestLockNamesOmitAnUnselectedStack(t *testing.T) {
	p := mustResolve(t, load(t, "fullstack.json"), selection())
	got := p.LockNames()
	if _, found := got["component.ng-web"]; found {
		t.Fatalf("LockNames records an unselected stack: %v", got)
	}
	if p.Slots["component.ng-web"] != "" {
		t.Fatalf("Slots holds a value for an unselected stack without a rule using it: %v", p.Slots)
	}
}

func TestLiveSlotsFollowTheKeptBlocksOfTheRules(t *testing.T) {
	core := []catalog.NameRule{
		{Path: "a", Text: "x: %{project}", Blocks: []string{""}},
		{Path: "b", Text: "y: %{envPrefix}", Blocks: []string{"module:identity"}},
		{Path: "c", Text: "z: %{component.ng-web}", Blocks: []string{"stack:ng-web"}},
	}
	cat := withRules(t, load(t, "fullstack.json"), core, nil)

	withoutIdentity := mustResolve(t, cat, selection())
	if withoutIdentity.Live["envPrefix"] || withoutIdentity.Live["component.ng-web"] {
		t.Fatalf("slots of dropped blocks are live: %v", withoutIdentity.Live)
	}
	if withoutIdentity.Slots["envPrefix"] != "ACME_BACKEND" || withoutIdentity.Slots["component.ng-web"] != "acme-backend-web" {
		t.Fatalf("a slot of a dropped block has no value: %v", withoutIdentity.Slots)
	}

	withIdentity := mustResolve(t, cat, fullSelection("identity"))
	got := withIdentity.LockNames()
	want := map[string]string{
		"project": "acme-backend", "envPrefix": "ACME_BACKEND",
		"component.go-srv": "acme-backend-srv", "component.ng-web": "acme-backend-web",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LockNames = %v, want %v", got, want)
	}
}

func TestRuleOfAnUninstalledModuleIsNotLive(t *testing.T) {
	identity := []catalog.NameRule{{Path: "a", Text: "x: %{envPrefix}", Blocks: []string{""}}}
	cat := withRules(t, load(t, "fullstack.json"), nil, identity)
	p := mustResolve(t, cat, selection())
	if _, found := p.LockNames()["envPrefix"]; found {
		t.Fatalf("a rule of an uninstalled module made envPrefix live: %v", p.LockNames())
	}
	installed := mustResolve(t, cat, selection("identity"))
	if installed.LockNames()["envPrefix"] != "ACME_BACKEND" {
		t.Fatalf("LockNames = %v", installed.LockNames())
	}
}

func TestRuleUsingASlotWithoutAValueSourceIsAnInternalError(t *testing.T) {
	core := []catalog.NameRule{{Path: "a", Text: "x: %{displayName}", Blocks: []string{""}}}
	cat := withRules(t, load(t, "fullstack.json"), core, nil)
	wantCodes(t, cat, selection(), "INTERNAL_ERROR")
}

func TestRuleOutsideBlocksDoesNotMakeAnUnselectedComponentSlotLive(t *testing.T) {
	core := []catalog.NameRule{{Path: "a", Text: "z: %{component.ng-web}", Blocks: []string{""}}}
	cat := withRules(t, load(t, "fullstack.json"), core, nil)
	p := mustResolve(t, cat, selection())
	if _, found := p.LockNames()["component.ng-web"]; found {
		t.Fatalf("an unselected stack's component slot is live: %v", p.LockNames())
	}
	if p.Slots["component.ng-web"] != "acme-backend-web" {
		t.Fatalf("the slot has no default value: %v", p.Slots)
	}
}

func TestUnusedOverrideNamesEveryModuleThatWouldDeliverTheSlot(t *testing.T) {
	core := []catalog.NameRule{
		{Path: "a", Text: "db: %{database}", Blocks: []string{"module:identity"}},
		{Path: "b", Text: "db: %{database}", Blocks: []string{"module:tenancy"}},
	}
	cat := withRules(t, load(t, "fullstack.json"), core, nil)
	sel := selection()
	sel.Database = &manifest.Database{Name: "ledger"}
	issues := wantCodes(t, cat, sel, plan.CodeNameOverrideUnused)
	want := "/database/name: no selected module delivers a database name (none of identity, tenancy is selected)"
	if issues[0].Message != want {
		t.Fatalf("message = %q, want %q", issues[0].Message, want)
	}
}
