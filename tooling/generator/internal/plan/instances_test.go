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

// sharedPorts are the ports every backend uses: one Postgres server.
var sharedPorts = map[string]bool{"postgres": true}

// assertUniqueOffsets fails unless every offset of the plan is inside the band
// and used by one port; a shared port has one owner per component, all naming
// the same port.
func assertUniqueOffsets(t *testing.T, label string, p plan.Plan) {
	t.Helper()
	for offset, owners := range offsetsOf(p) {
		portNames := map[string]bool{}
		for _, owner := range owners {
			_, port, _ := strings.Cut(owner, "/")
			portNames[port] = true
		}
		isOnePort := len(portNames) == 1
		isShared := false
		for name := range portNames {
			isShared = sharedPorts[name]
		}
		if offset > 98 || !isOnePort || (len(owners) > 1 && !isShared) {
			t.Errorf("%s: offset %d is used by %v", label, offset, owners)
		}
	}
}

// modulesSelection selects n backends b1..bn with the given modules, and a
// console bound to each of the first webs backends.
func modulesSelection(n, webs int, modules ...string) manifest.Selection {
	sel := manifest.Selection{
		Schema:  2,
		Project: manifest.Project{Name: "acme", GoModule: "example.com/acme"},
		Stacks:  []string{"go-srv"},
	}
	for i := 1; i <= n; i++ {
		sel.Components = append(sel.Components, manifest.Component{Stack: "go-srv", Name: fmt.Sprintf("b%d", i), Modules: modules, HasModules: true})
	}
	for i := 1; i <= webs; i++ {
		if i == 1 {
			sel.Stacks = append(sel.Stacks, "ng-web")
		}
		sel.Components = append(sel.Components, manifest.Component{Stack: "ng-web", Name: fmt.Sprintf("w%d", i), Backend: fmt.Sprintf("b%d", i)})
	}
	return sel
}

// largestBackendCount returns the most backends the band holds for the shape,
// found by planning one more backend until the plan reports PORT_BAND_EXHAUSTED.
func largestBackendCount(t *testing.T, cat catalog.Catalog, webs func(n int) int, modules ...string) int {
	t.Helper()
	largest := 0
	for n := 1; n <= 60; n++ {
		p, issues := plan.Resolve(cat, modulesSelection(n, webs(n), modules...))
		if len(issues) > 0 {
			for _, found := range issues {
				if found.Code != plan.CodePortBandExhausted {
					t.Fatalf("%d backends: %+v", n, found)
				}
			}
			return largest
		}
		assertUniqueOffsets(t, fmt.Sprintf("%d backends", n), p)
		largest = n
	}
	t.Fatalf("the band still fits 60 backends")
	return largest
}

func TestTheBandLimitsBackendsOnlyByTheOffsetsItHolds(t *testing.T) {
	cat := repoCatalog(t)
	identityAndMore := []string{"identity", "api-keys", "tenancy"}
	cases := []struct {
		name    string
		webs    func(n int) int
		modules []string
		want    int
	}{
		// srv takes offsets 0..n-1 and stops before the shared Postgres at 32.
		{"core only, no console", func(int) int { return 0 }, nil, 32},
		{"identity and more, no console", func(int) int { return 0 }, identityAndMore, 32},
		// srv runs into the console at offset 20 as soon as one console exists.
		{"identity and more, one console", func(int) int { return 1 }, identityAndMore, 20},
		// consoles take offsets 20..19+n and stop before the shared Postgres at 32.
		{"identity and more, a console per backend", func(n int) int { return n }, identityAndMore, 12},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := largestBackendCount(t, cat, tc.webs, tc.modules...); got != tc.want {
				t.Errorf("the band holds %d backends, want %d", got, tc.want)
			}
		})
	}
}

func TestOneMoreBackendThanTheBandHoldsIsPortBandExhausted(t *testing.T) {
	_, issues := plan.Resolve(repoCatalog(t), modulesSelection(33, 0, "identity"))
	if len(issues) == 0 {
		t.Fatal("33 backends planned")
	}
	for _, found := range issues {
		if found.Code != plan.CodePortBandExhausted {
			t.Errorf("unexpected issue %+v", found)
		}
	}
	first := issues[0]
	if !strings.Contains(first.Message, "b33 (index 32)") || !strings.Contains(first.Message, "offset 32") {
		t.Errorf("message = %q", first.Message)
	}
	wantHint := "the band holds offsets 0-98 and each component takes its own: with these modules and the other components of this selection at most 32 go-srv components fit"
	if !strings.Contains(first.Hint, wantHint) {
		t.Errorf("hint = %q", first.Hint)
	}
}

func TestBackendsShareOnePostgresPort(t *testing.T) {
	cat := repoCatalog(t)
	for n := 2; n <= 6; n++ {
		p := mustResolve(t, cat, modulesSelection(n, 0, "identity"))
		for _, component := range p.Components {
			if component.Ports["postgres"] != 10132 {
				t.Errorf("%d backends: %s postgres port = %d, want the shared 10132", n, component.Name, component.Ports["postgres"])
			}
		}
		first, last := byName(t, p, "b1"), byName(t, p, fmt.Sprintf("b%d", n))
		if first.Ports["srv"] != 10100 || first.Ports["kratos-public"] != 10133 || first.Ports["kratos-admin"] != 10134 {
			t.Errorf("%d backends: index 0 moved: %v", n, first.Ports)
		}
		wantPublic := 10133 + 2*(n-1)
		if last.Ports["kratos-public"] != wantPublic || last.Ports["kratos-admin"] != wantPublic+1 {
			t.Errorf("%d backends: last kratos ports = %v, want %d and %d", n, last.Ports, wantPublic, wantPublic+1)
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
