package plan_test

import (
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
)

func portSelection(stacks []string, band int) manifest.Selection {
	sel := selection()
	sel.Stacks = stacks
	if band != 0 {
		sel.Schema = 2
		sel.Ports = &manifest.Ports{Band: band}
	}
	return sel
}

func backendOf(t *testing.T, p plan.Plan) plan.ComponentPlan {
	t.Helper()
	for _, component := range p.Components {
		if component.Role == "backend" {
			return component
		}
	}
	t.Fatal("plan has no backend component")
	return plan.ComponentPlan{}
}

func TestPortPlanOfTheReferenceBandWithAConsole(t *testing.T) {
	cat := fixture.Load(t).Catalog
	resolved := mustResolve(t, cat, portSelection([]string{"go-srv", "ng-web"}, 0))
	if want := (plan.PortPlan{RefBand: 101, Band: 101}); resolved.Ports != want {
		t.Fatalf("ports = %+v, want %+v", resolved.Ports, want)
	}
	if got := backendOf(t, resolved).Origin; got != "http://localhost:10120" {
		t.Errorf("origin = %s, want the console port", got)
	}
}

func TestPortPlanMapsTheConsoleOriginToTheBackendWithoutAConsole(t *testing.T) {
	cat := fixture.Load(t).Catalog
	backend := backendOf(t, mustResolve(t, cat, portSelection([]string{"go-srv"}, 0)))
	if backend.Origin != "http://localhost:10100" {
		t.Errorf("origin = %s, want the backend's own port", backend.Origin)
	}
	if backend.TokenOffsets[20] != 0 || backend.TokenOffsets[0] != 0 || backend.TokenOffsets[32] != 32 {
		t.Errorf("token offsets = %v", backend.TokenOffsets)
	}
}

func TestPortPlanFollowsTheChosenBand(t *testing.T) {
	cat := fixture.Load(t).Catalog
	withWeb := mustResolve(t, cat, portSelection([]string{"go-srv", "ng-web"}, 103))
	if withWeb.Ports.Band != 103 || backendOf(t, withWeb).Origin != "http://localhost:10320" || withWeb.Ports.RefBand != 101 {
		t.Errorf("with a console: %+v origin %s", withWeb.Ports, backendOf(t, withWeb).Origin)
	}
	backendOnly := mustResolve(t, cat, portSelection([]string{"go-srv"}, 654))
	if backendOnly.Ports.Band != 654 || backendOf(t, backendOnly).Origin != "http://localhost:65400" {
		t.Errorf("backend only: %+v origin %s", backendOnly.Ports, backendOf(t, backendOnly).Origin)
	}
}

func TestPortPlanKeepsTheGivenBandInTheResolvedSelection(t *testing.T) {
	cat := fixture.Load(t).Catalog
	given := mustResolve(t, cat, portSelection([]string{"go-srv"}, 103))
	if given.Selection.Ports == nil || given.Selection.Ports.Band != 103 || given.Selection.Schema != 2 {
		t.Errorf("selection = %+v", given.Selection)
	}
	absent := mustResolve(t, cat, portSelection([]string{"go-srv"}, 0))
	if absent.Selection.Ports != nil {
		t.Errorf("a default band must not appear in the selection: %+v", absent.Selection.Ports)
	}
}

func TestPortPlanRefusesABandOutsideItsRange(t *testing.T) {
	cat := fixture.Load(t).Catalog
	for _, band := range []int{99, 655, -3} {
		sel := portSelection([]string{"go-srv"}, 0)
		sel.Ports = &manifest.Ports{Band: band}
		_, issues := plan.Resolve(cat, sel)
		if len(issues) != 1 || issues[0].Code != manifest.CodeSelectionInvalid || issues[0].Path != "selection#/ports/band" {
			t.Errorf("band %d: issues %v", band, issues)
		}
	}
}

func TestACatalogWithoutPortsPlansNoSubstitution(t *testing.T) {
	cat := load(t, "backend.json")
	got := mustResolve(t, cat, portSelection([]string{"go-srv"}, 0)).Ports
	if got.RefBand != 0 || got.Band != 101 {
		t.Fatalf("ports = %+v", got)
	}
}
