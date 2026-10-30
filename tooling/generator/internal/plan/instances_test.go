package plan_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
)

// repoCatalog builds the catalog of the checkout this test runs in.
func repoCatalog(t *testing.T) catalog.Catalog {
	t.Helper()
	return fixture.LoadCheckout(t).Catalog
}

// backendsSelection selects n backends named b1..bn, every module, and a
// console bound to the last one.
func backendsSelection(n int) manifest.Selection {
	sel := manifest.Selection{
		Schema:     2,
		Project:    manifest.Project{Name: "acme", GoModule: "example.com/acme"},
		Stacks:     []string{"go-srv", "ng-web"},
		AllModules: true,
	}
	for i := 1; i <= n; i++ {
		sel.Components = append(sel.Components, manifest.Component{Stack: "go-srv", Name: fmt.Sprintf("b%d", i)})
	}
	sel.Components = append(sel.Components, manifest.Component{Stack: "ng-web", Name: "web", Backend: fmt.Sprintf("b%d", n)})
	return sel
}

// offsetsOf returns every host offset of the plan, with its owner.
func offsetsOf(p plan.Plan) map[int][]string {
	used := map[int][]string{}
	for _, component := range p.Components {
		for name, port := range component.Ports {
			offset := port - p.Ports.Band*100
			used[offset] = append(used[offset], component.Name+"/"+name)
		}
	}
	return used
}

func TestTwoBackendsGetTheirOwnPortsAndOrigins(t *testing.T) {
	cat := fixture.Load(t).Catalog
	sel := manifest.Selection{
		Schema:  2,
		Project: manifest.Project{Name: "acme", GoModule: "example.com/acme"},
		Stacks:  []string{"go-srv", "ng-web"},
		Components: []manifest.Component{
			backendEntry("acme-srv-first", "identity"),
			backendEntry("acme-srv-second", "identity"),
			webEntry("acme-web", "acme-srv-second"),
		},
	}
	p := mustResolve(t, cat, sel)
	first, second, web := byName(t, p, "acme-srv-first"), byName(t, p, "acme-srv-second"), byName(t, p, "acme-web")
	wantFirst := map[string]int{"srv": 10100, "postgres": 10132, "kratos-public": 10133}
	wantSecond := map[string]int{"srv": 10101, "postgres": 10142, "kratos-public": 10143}
	for _, tc := range []struct {
		name      string
		got, want map[string]int
	}{{"first", first.Ports, wantFirst}, {"second", second.Ports, wantSecond}, {"web", web.Ports, map[string]int{"web": 10120}}} {
		if len(tc.got) != len(tc.want) {
			t.Errorf("%s ports = %v, want %v", tc.name, tc.got, tc.want)
			continue
		}
		for name, port := range tc.want {
			if tc.got[name] != port {
				t.Errorf("%s port %s = %d, want %d", tc.name, name, tc.got[name], port)
			}
		}
	}
	if first.Origin != "http://localhost:10100" || second.Origin != "http://localhost:10120" || web.Origin != "" {
		t.Errorf("origins = %q %q %q", first.Origin, second.Origin, web.Origin)
	}
}

func TestATokenMeansThePortOfTheComponentOrItsPartner(t *testing.T) {
	cat := fixture.Load(t).Catalog
	sel := manifest.Selection{
		Schema:  2,
		Project: manifest.Project{Name: "acme", GoModule: "example.com/acme"},
		Stacks:  []string{"go-srv", "ng-web"},
		Components: []manifest.Component{
			backendEntry("acme-srv-first", "identity"),
			backendEntry("acme-srv-second", "identity"),
			webEntry("acme-web", "acme-srv-second"),
		},
	}
	p := mustResolve(t, cat, sel)
	cases := []struct {
		component string
		// offsets maps a token offset to the offset it carries in that context.
		offsets map[int]int
	}{
		// An unbound backend: its own ports; the console's origin is its own srv.
		{"acme-srv-first", map[int]int{0: 0, 32: 32, 33: 33, 20: 0}},
		// A bound backend: its own ports; the console's port is the partner's.
		{"acme-srv-second", map[int]int{0: 1, 32: 42, 33: 43, 20: 20}},
		// A frontend: the backend's ports at the backend's index.
		{"acme-web", map[int]int{0: 1, 32: 42, 33: 43, 20: 20}},
	}
	for _, tc := range cases {
		got := byName(t, p, tc.component).TokenOffsets
		for token, want := range tc.offsets {
			if got[token] != want {
				t.Errorf("%s: token offset %d = %d, want %d", tc.component, token, got[token], want)
			}
		}
	}
}

func TestMaxBackendComponentsIsTheLargestCountWithUniquePorts(t *testing.T) {
	cat := repoCatalog(t)
	// The planner's own output gives each port's host offset at index 0 and
	// at index 1; the next count fits only while its last index stays in the band.
	p := mustResolve(t, cat, backendsSelection(2))
	first, second := byName(t, p, "b1"), byName(t, p, "b2")
	highestNext := 0
	for name, port := range first.Ports {
		stride := second.Ports[name] - port
		last := port - p.Ports.Band*100 + plan.MaxBackendComponents*stride
		if last > highestNext {
			highestNext = last
		}
	}
	if highestNext <= 98 {
		t.Errorf("%d backends would still fit the band (highest offset %d); plan.MaxBackendComponents is too low", plan.MaxBackendComponents+1, highestNext)
	}
	atLimit := mustResolve(t, cat, backendsSelection(plan.MaxBackendComponents))
	for offset, owners := range offsetsOf(atLimit) {
		if len(owners) != 1 || offset > 98 {
			t.Errorf("%d backends: offset %d is used by %v", plan.MaxBackendComponents, offset, owners)
		}
	}
}

func TestEveryBackendCountUpToTheLimitPlansUniquePorts(t *testing.T) {
	cat := repoCatalog(t)
	for n := 1; n <= plan.MaxBackendComponents; n++ {
		p := mustResolve(t, cat, backendsSelection(n))
		for offset, owners := range offsetsOf(p) {
			if len(owners) != 1 || offset > 98 {
				t.Errorf("%d backends: offset %d is used by %v", n, offset, owners)
			}
		}
		if n == 1 {
			continue
		}
		first := byName(t, p, "b1")
		if first.Ports["srv"] != 10100 || first.Ports["postgres"] != 10132 || first.Ports["kratos-public"] != 10133 || first.Ports["kratos-admin"] != 10134 {
			t.Errorf("%d backends: index 0 moved: %v", n, first.Ports)
		}
	}
}

func TestOneBackendKeepsTheReferencePorts(t *testing.T) {
	p := mustResolve(t, repoCatalog(t), backendsSelection(1))
	got := byName(t, p, "b1")
	want := map[string]int{"srv": 10100, "postgres": 10132, "kratos-public": 10133, "kratos-admin": 10134}
	for name, port := range want {
		if got.Ports[name] != port {
			t.Errorf("port %s = %d, want %d", name, got.Ports[name], port)
		}
	}
	if got.Origin != "http://localhost:10120" {
		t.Errorf("origin = %s", got.Origin)
	}
}

func TestPortBandExhaustedNamesThePortTheComponentAndTheIndex(t *testing.T) {
	cat := fixture.Load(t).Catalog
	for s := range cat.Stacks {
		for m := range cat.Stacks[s].Modules {
			for i := range cat.Stacks[s].Modules[m].Ports {
				if cat.Stacks[s].Modules[m].Ports[i].Name == "postgres" {
					cat.Stacks[s].Modules[m].Ports[i].Stride = 50
				}
			}
		}
	}
	sel := backendsSelection(3)
	_, issues := plan.Resolve(cat, sel)
	if len(issues) != 1 || issues[0].Code != plan.CodePortBandExhausted {
		t.Fatalf("issues = %+v", issues)
	}
	want := "port postgres of b3 (index 2) would use offset 132; the band has 0-98"
	if issues[0].Message != want {
		t.Errorf("message = %q, want %q", issues[0].Message, want)
	}
}

func TestPortBandExhaustedReportsAClashOnce(t *testing.T) {
	cat := fixture.Load(t).Catalog
	for s := range cat.Stacks {
		for m := range cat.Stacks[s].Modules {
			for i := range cat.Stacks[s].Modules[m].Ports {
				if cat.Stacks[s].Modules[m].Ports[i].Name == "postgres" {
					// 32+1 collides with kratos-public of the first instance.
					cat.Stacks[s].Modules[m].Ports[i].Stride = 1
				}
			}
		}
	}
	sel := manifest.Selection{
		Schema:  2,
		Project: manifest.Project{Name: "acme", GoModule: "example.com/acme"},
		Stacks:  []string{"go-srv"},
		Components: []manifest.Component{
			backendEntry("acme-srv-first", "identity"),
			backendEntry("acme-srv-second", "identity"),
		},
	}
	_, issues := plan.Resolve(cat, sel)
	if len(issues) != 1 || issues[0].Code != plan.CodePortBandExhausted || !strings.Contains(issues[0].Message, "offset 33") {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestPortBandIsNotCheckedPastTheComponentLimit(t *testing.T) {
	_, issues := plan.Resolve(repoCatalog(t), backendsSelection(plan.MaxBackendComponents+1))
	for _, found := range issues {
		if found.Code == plan.CodePortBandExhausted {
			t.Fatalf("the band check ran past the limit: %+v", found)
		}
	}
}

func TestAComponentCannotTakeAFolderUnderDeployLocalInAMultiProject(t *testing.T) {
	cat := fixture.Load(t).Catalog
	for _, name := range []string{"kratos", "postgres-init"} {
		t.Run(name, func(t *testing.T) {
			sel := manifest.Selection{
				Schema:     2,
				Project:    manifest.Project{Name: "acme", GoModule: "example.com/acme"},
				Stacks:     []string{"go-srv"},
				Components: []manifest.Component{backendEntry(name, "identity"), backendEntry("acme-srv-second", "identity")},
			}
			_, issues := plan.Resolve(cat, sel)
			var codes []string
			for _, found := range issues {
				codes = append(codes, found.Code+" "+found.Path)
			}
			sort.Strings(codes)
			if len(codes) != 1 || codes[0] != "NAME_RESERVED selection#/components/0/name" {
				t.Fatalf("issues = %v", codes)
			}
			single := manifest.Selection{
				Schema:     2,
				Project:    manifest.Project{Name: "acme", GoModule: "example.com/acme"},
				Stacks:     []string{"go-srv"},
				Components: []manifest.Component{backendEntry(name, "identity")},
			}
			if _, issues := plan.Resolve(cat, single); len(issues) > 0 {
				t.Errorf("a single project keeps its per-component files at the source path: %+v", issues)
			}
		})
	}
}
