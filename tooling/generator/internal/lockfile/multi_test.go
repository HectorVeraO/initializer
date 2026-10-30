package lockfile_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

func intPtr(value int) *int {
	return &value
}

// validMulti returns a valid multi lock: two go-srv backends and one ng-web
// frontend bound to the second backend.
func validMulti() lockfile.Lock {
	return lockfile.Lock{
		Schema:        2,
		Generator:     "0.1.0",
		CatalogSHA256: hash,
		Selection:     manifest.Selection{Schema: 2, Project: manifest.Project{Name: "acme", GoModule: "example.com/acme"}, Stacks: []string{"go-srv", "ng-web"}},
		Ports:         &lockfile.Ports{Band: 101},
		Components: []lockfile.Component{
			{
				Stack: "go-srv", Name: "acme-srv-first", Index: intPtr(0), GoModule: "example.com/acme/acme-srv-first",
				Database: &lockfile.Database{Name: "acme_first", User: "acme_first"},
				Modules:  []string{"core"},
				Ports:    map[string]int{"srv": 10100, "postgres": 10132},
				Origin:   "http://localhost:10100",
				TCK:      &lockfile.TCK{Tags: []string{"@core"}, Grep: "@(core)"},
				Names:    map[string]string{"component.go-srv": "acme-srv-first", "database": "acme_first", "dbUser": "acme_first"},
			},
			{
				Stack: "go-srv", Name: "acme-srv-second", Index: intPtr(1), GoModule: "example.com/acme/acme-srv-second",
				Modules: []string{"core", "tenancy"},
				Ports:   map[string]int{"srv": 10101, "postgres": 10142},
				Origin:  "http://localhost:10120",
				Names:   map[string]string{"component.go-srv": "acme-srv-second"},
			},
			{
				Stack: "ng-web", Name: "acme-web", Index: intPtr(0), Modules: []string{"core"}, Backend: "acme-srv-second",
				Ports: map[string]int{"web": 10120},
				Names: map[string]string{"component.ng-web": "acme-web"},
			},
		},
		Names: map[string]string{"project": "acme"},
		Modules: []lockfile.Module{
			{Stack: "go-srv", Module: "core", Version: "0.1.0", Component: "acme-srv-first", Files: []lockfile.File{{Path: "acme-srv-first/go.mod", SHA256: hash}, {Path: ".deploy/local/README.md", SHA256: hash}}},
			{Stack: "go-srv", Module: "core", Version: "0.1.0", Component: "acme-srv-second", Files: []lockfile.File{{Path: "acme-srv-second/go.mod", SHA256: hash}}},
			{Stack: "go-srv", Module: "tenancy", Version: "0.1.0", Component: "acme-srv-second", Files: []lockfile.File{{Path: "acme-srv-second/internal/platform/tenancy/tenancy.go", SHA256: hash}}},
			{Stack: "ng-web", Module: "core", Version: "0.1.0", Component: "acme-web", Files: []lockfile.File{{Path: "acme-web/package.json", SHA256: hash}}},
		},
		Specs:     []lockfile.Spec{{Spec: "core", Version: "0.1.0", Files: []lockfile.File{{Path: "specs/core/a.yaml", SHA256: hash}}}},
		Generated: []lockfile.File{{Path: "README.md", SHA256: hash}},
	}
}

func encodeAndDecode(t *testing.T, lock lockfile.Lock) (lockfile.Lock, error) {
	t.Helper()
	encoded, err := lockfile.Encode(lock)
	if err != nil {
		t.Fatal(err)
	}
	return lockfile.Decode(encoded)
}

func TestAMultiLockRoundTrips(t *testing.T) {
	encoded, err := lockfile.Encode(validMulti())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := lockfile.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	again, err := lockfile.Encode(decoded)
	if err != nil || string(again) != string(encoded) {
		t.Fatalf("round trip differs: %v", err)
	}
	if !strings.Contains(string(encoded), `"index": 0`) {
		t.Errorf("index 0 did not encode:\n%s", encoded)
	}
	if !strings.Contains(string(encoded), `"env": {}`) {
		t.Errorf("a tck without env must encode an empty object:\n%s", encoded)
	}
}

func TestMultiAndComponentOfTellTheShapes(t *testing.T) {
	multi, err := encodeAndDecode(t, validMulti())
	if err != nil {
		t.Fatal(err)
	}
	if !multi.Multi() {
		t.Error("two go-srv components make a multi lock")
	}
	for _, module := range multi.Modules {
		holder, found := multi.ComponentOf(module)
		if !found || holder.Name != module.Component || holder.Stack != module.Stack {
			t.Errorf("ComponentOf(%s/%s) = %q, %v", module.Component, module.Module, holder.Name, found)
		}
	}
	for _, orphan := range []lockfile.Module{{Stack: "go-srv", Module: "core", Component: "acme-web"}, {Stack: "go-srv", Module: "core"}} {
		if _, found := multi.ComponentOf(orphan); found {
			t.Errorf("ComponentOf(%+v) must not find a holder", orphan)
		}
	}
	single, err := encodeAndDecode(t, validLock())
	if err != nil {
		t.Fatal(err)
	}
	holder, found := single.ComponentOf(single.Modules[0])
	_, foundWeb := single.ComponentOf(lockfile.Module{Stack: "ng-web", Module: "core"})
	if single.Multi() || !found || holder.Name != "go-srv" || foundWeb {
		t.Errorf("single lock: Multi %v, holder %q (%v), web holder %v", single.Multi(), holder.Name, found, foundWeb)
	}
}

func TestASingleLockEncodesNoMultiFields(t *testing.T) {
	encoded, err := lockfile.Encode(validLock())
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components []map[string]any `json:"components"`
		Modules    []map[string]any `json:"modules"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	for _, entry := range document.Components {
		if len(entry) != 2 {
			t.Errorf("single component entry holds %v, want stack and name only", entry)
		}
	}
	for _, entry := range document.Modules {
		if _, has := entry["component"]; has {
			t.Errorf("single module entry holds component: %v", entry)
		}
	}
}

func TestDecodeRefusesAMultiLockThatBreaksAStrictRule(t *testing.T) {
	cases := map[string]struct {
		mutate func(*lockfile.Lock)
		want   string
	}{
		"entry without index":       {func(l *lockfile.Lock) { l.Components[1].Index = nil }, "index must be 1"},
		"duplicated index":          {func(l *lockfile.Lock) { l.Components[1].Index = intPtr(0) }, "index must be 1"},
		"entry module id malformed": {func(l *lockfile.Lock) { l.Components[0].Modules = []string{"core", "../x"} }, "malformed"},
		"entry without modules":     {func(l *lockfile.Lock) { l.Components[0].Modules = nil }, "modules is missing"},
		"go entry no goModule":      {func(l *lockfile.Lock) { l.Components[1].GoModule = "" }, "goModule is missing"},
		"goModule twice":            {func(l *lockfile.Lock) { l.Components[1].GoModule = l.Components[0].GoModule }, "also the one of"},
		"goModule on a frontend":    {func(l *lockfile.Lock) { l.Components[2].GoModule = "example.com/acme/web" }, "only for stacks with a Go module"},
		"frontend no backend":       {func(l *lockfile.Lock) { l.Components[2].Backend = "" }, "does not name a backend"},
		"frontend unknown":          {func(l *lockfile.Lock) { l.Components[2].Backend = "acme-srv-third" }, "does not name a backend"},
		"backend named twice": {func(l *lockfile.Lock) {
			l.Components = append(l.Components, lockfile.Component{
				Stack: "ng-web", Name: "acme-admin", Index: intPtr(1), Modules: []string{"core"}, Backend: "acme-srv-second",
				Ports: map[string]int{"web": 10121},
				Names: map[string]string{"component.ng-web": "acme-admin"},
			})
		}, "is also the backend of"},
		"backend on a backend":   {func(l *lockfile.Lock) { l.Components[0].Backend = "acme-srv-second" }, "only for frontend"},
		"tck without tags":       {func(l *lockfile.Lock) { l.Components[0].TCK.Tags = nil }, "tck needs tags and grep"},
		"entry without ports":    {func(l *lockfile.Lock) { l.Components[1].Ports = nil }, "ports is missing"},
		"frontend without ports": {func(l *lockfile.Lock) { l.Components[2].Ports = nil }, "ports is missing"},
		"backend without origin": {func(l *lockfile.Lock) { l.Components[1].Origin = "" }, "origin is missing"},
		"port out of range":      {func(l *lockfile.Lock) { l.Components[0].Ports["srv"] = 70000 }, "TCP port"},
		"port name not a label":  {func(l *lockfile.Lock) { l.Components[0].Ports["Srv"] = 10100 }, "TCP port"},
		"module without component": {func(l *lockfile.Lock) {
			l.Modules[1].Component = ""
		}, "is not a go-srv component"},
		"module of another stack's component": {func(l *lockfile.Lock) { l.Modules[1].Component = "acme-web" }, "is not a go-srv component"},
		"module not listed by the component":  {func(l *lockfile.Lock) { l.Components[1].Modules = []string{"core"} }, "does not list it"},
		"listed module without entry": {func(l *lockfile.Lock) {
			l.Components[0].Modules = []string{"core", "identity"}
		}, "has no modules entry"},
		"module entry twice": {func(l *lockfile.Lock) {
			l.Modules = append(l.Modules, lockfile.Module{Stack: "go-srv", Module: "tenancy", Version: "0.1.0", Component: "acme-srv-second"})
		}, "has two entries"},
		"component slot at the top":  {func(l *lockfile.Lock) { l.Names["component.go-srv"] = "acme-srv-first" }, "belongs to the component entries"},
		"entry without its slot":     {func(l *lockfile.Lock) { delete(l.Components[0].Names, "component.go-srv") }, "must equal the name"},
		"entry holds a foreign slot": {func(l *lockfile.Lock) { l.Components[0].Names["component.ng-web"] = "acme-web" }, "not the slot of this component"},
		"file in another component's folder": {func(l *lockfile.Lock) {
			l.Modules[1].Files[0].Path = "acme-srv-first/other.go"
		}, "inside the folder"},
		"one module file listed twice": {func(l *lockfile.Lock) {
			l.Modules[2].Files[0].Path = "acme-srv-second/go.mod"
		}, "listed twice"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			lock := validMulti()
			tc.mutate(&lock)
			_, err := encodeAndDecode(t, lock)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestAMultiLockWithoutAPortBandNeedsNoPortsOrOrigin(t *testing.T) {
	lock := validMulti()
	lock.Ports = nil
	for i := range lock.Components {
		lock.Components[i].Ports, lock.Components[i].Origin = nil, ""
	}
	if _, err := encodeAndDecode(t, lock); err != nil {
		t.Fatalf("a catalog without ports records none: %v", err)
	}
}

func TestDecodeRefusesAMultiFieldOnASingleLock(t *testing.T) {
	cases := map[string]func(*lockfile.Lock){
		"index":     func(l *lockfile.Lock) { l.Components[0].Index = intPtr(0) },
		"goModule":  func(l *lockfile.Lock) { l.Components[0].GoModule = "example.com/acme" },
		"database":  func(l *lockfile.Lock) { l.Components[0].Database = &lockfile.Database{Name: "acme"} },
		"modules":   func(l *lockfile.Lock) { l.Components[0].Modules = []string{"core"} },
		"ports":     func(l *lockfile.Lock) { l.Components[0].Ports = map[string]int{"srv": 10100} },
		"origin":    func(l *lockfile.Lock) { l.Components[0].Origin = "http://localhost:10100" },
		"tck":       func(l *lockfile.Lock) { l.Components[0].TCK = &lockfile.TCK{Tags: []string{"@core"}, Grep: "@core"} },
		"backend":   func(l *lockfile.Lock) { l.Components[0].Backend = "go-srv" },
		"names":     func(l *lockfile.Lock) { l.Components[0].Names = map[string]string{"component.go-srv": "go-srv"} },
		"component": func(l *lockfile.Lock) { l.Modules[0].Component = "go-srv" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			lock := validLock()
			mutate(&lock)
			_, err := encodeAndDecode(t, lock)
			if err == nil || !strings.Contains(err.Error(), "no stack has two") {
				t.Fatalf("error = %v, want a refusal naming the single shape", err)
			}
		})
	}
}
