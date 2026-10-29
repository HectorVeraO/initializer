package plan_test

import (
	"reflect"
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

func TestPortPlanOfTheReferenceBandWithAConsole(t *testing.T) {
	cat := fixture.Load(t).Catalog
	got := mustResolve(t, cat, portSelection([]string{"go-srv", "ng-web"}, 0)).Ports
	want := plan.PortPlan{RefBand: 101, Band: 101, Origin: 10120}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ports = %+v, want %+v", got, want)
	}
}

func TestPortPlanMapsTheConsoleOriginToTheBackendWithoutAConsole(t *testing.T) {
	cat := fixture.Load(t).Catalog
	got := mustResolve(t, cat, portSelection([]string{"go-srv"}, 0)).Ports
	want := plan.PortPlan{RefBand: 101, Band: 101, Offsets: map[int]int{20: 0}, Origin: 10100}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ports = %+v, want %+v", got, want)
	}
}

func TestPortPlanFollowsTheChosenBand(t *testing.T) {
	cat := fixture.Load(t).Catalog
	withWeb := mustResolve(t, cat, portSelection([]string{"go-srv", "ng-web"}, 103)).Ports
	if withWeb.Band != 103 || withWeb.Origin != 10320 || len(withWeb.Offsets) != 0 || withWeb.RefBand != 101 {
		t.Errorf("with a console: %+v", withWeb)
	}
	backendOnly := mustResolve(t, cat, portSelection([]string{"go-srv"}, 654)).Ports
	if backendOnly.Band != 654 || backendOnly.Origin != 65400 || backendOnly.Offsets[20] != 0 {
		t.Errorf("backend only: %+v", backendOnly)
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
	if got.RefBand != 0 || got.Band != 101 || len(got.Offsets) != 0 {
		t.Fatalf("ports = %+v", got)
	}
}
