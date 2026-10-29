package lockfile_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

var hash = strings.Repeat("a", 64)

func validLock() lockfile.Lock {
	return lockfile.Lock{
		Schema:        2,
		Generator:     "0.1.0",
		CatalogSHA256: hash,
		Selection:     manifest.Selection{Schema: 1, Project: manifest.Project{Name: "acme", GoModule: "example.com/acme"}, Stacks: []string{"go-srv"}, Modules: []string{"core"}},
		Components:    []lockfile.Component{{Stack: "go-srv", Name: "go-srv"}},
		Names:         map[string]string{"project": "acme", "component.go-srv": "go-srv"},
		Specs:         []lockfile.Spec{{Spec: "core", Version: "0.1.0", Files: []lockfile.File{{Path: "specs/core/a.yaml", SHA256: hash}}}},
		Modules:       []lockfile.Module{{Stack: "go-srv", Module: "core", Version: "0.1.0", Files: []lockfile.File{{Path: "go-srv/go.mod", SHA256: hash}}}},
		Generated:     []lockfile.File{{Path: "README.md", SHA256: hash}},
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	encoded, err := lockfile.Encode(validLock())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(encoded), "}\n") || strings.Contains(string(encoded), "null") {
		t.Fatalf("encoding: %s", encoded)
	}
	decoded, err := lockfile.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	again, err := lockfile.Encode(decoded)
	if err != nil || string(again) != string(encoded) {
		t.Fatalf("round trip differs: %v", err)
	}
}

func TestTCKGrepInvertRoundTripsAndIsOmittedWhenEmpty(t *testing.T) {
	plain, err := lockfile.Encode(validLock())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "tckGrepInvert") {
		t.Fatalf("empty tckGrepInvert is encoded: %s", plain)
	}
	withInvert := validLock()
	withInvert.TCKGrep = "@(core)(?![A-Za-z0-9_-])"
	withInvert.TCKGrepInvert = "@(tenancy)(?![A-Za-z0-9_-])"
	encoded, err := lockfile.Encode(withInvert)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	decoded, err := lockfile.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.TCKGrepInvert != withInvert.TCKGrepInvert {
		t.Fatalf("TCKGrepInvert = %q", decoded.TCKGrepInvert)
	}
	again, err := lockfile.Encode(decoded)
	if err != nil || string(again) != text {
		t.Fatalf("round trip differs: %v", err)
	}
}

func TestDecodeRefusals(t *testing.T) {
	cases := map[string]struct {
		mutate func(*lockfile.Lock)
		want   string
	}{
		"schema 3":                {func(l *lockfile.Lock) { l.Schema = 3 }, "schema must be the number 1 or 2"},
		"stack":                   {func(l *lockfile.Lock) { l.Modules[0].Stack = "rust-srv" }, "has no component"},
		"module id":               {func(l *lockfile.Lock) { l.Modules[0].Module = "../x" }, "malformed"},
		"spec id":                 {func(l *lockfile.Lock) { l.Specs[0].Spec = "A B" }, "malformed"},
		"escaping path":           {func(l *lockfile.Lock) { l.Generated[0].Path = "../x" }, "project-relative"},
		"absolute path":           {func(l *lockfile.Lock) { l.Generated[0].Path = "/etc/x" }, "project-relative"},
		"backslash":               {func(l *lockfile.Lock) { l.Generated[0].Path = "a\\b" }, "project-relative"},
		"bad hash":                {func(l *lockfile.Lock) { l.Generated[0].SHA256 = "zz" }, "sha256"},
		"lists itself":            {func(l *lockfile.Lock) { l.Generated[0].Path = lockfile.Name }, "itself"},
		"duplicate":               {func(l *lockfile.Lock) { l.Generated[0].Path = "go-srv/go.mod" }, "twice"},
		"component unknown stack": {func(l *lockfile.Lock) { l.Components[0].Stack = "rust-srv" }, "components"},
		"component stack twice":   {func(l *lockfile.Lock) { l.Components = append(l.Components, l.Components[0]) }, "listed twice"},
		"component bad name":      {func(l *lockfile.Lock) { l.Components[0].Name = "Acme_Srv" }, "not a label"},
		"component parent path":   {func(l *lockfile.Lock) { l.Components[0].Name = "../x" }, "not a label"},
		"component nested path":   {func(l *lockfile.Lock) { l.Components[0].Name = "a/b" }, "not a label"},
		"component five digits":   {func(l *lockfile.Lock) { l.Components[0].Name = "app10132" }, "not a label"},
		"component name twice": {func(l *lockfile.Lock) {
			l.Components = append(l.Components, lockfile.Component{Stack: "ng-web", Name: "go-srv"})
		}, "used twice"},
		"module without component": {func(l *lockfile.Lock) {
			l.Components = nil
			l.Names = map[string]string{"project": "acme"}
		}, "has no component"},
		"file in other component": {func(l *lockfile.Lock) {
			l.Components = append(l.Components, lockfile.Component{Stack: "ng-web", Name: "acme-web"})
			l.Names["component.ng-web"] = "acme-web"
			l.Modules[0].Files[0].Path = "acme-web/x.go"
		}, "inside the folder"},
		"names unknown key":        {func(l *lockfile.Lock) { l.Names["bogus"] = "x" }, "not a slot name"},
		"names unknown stack slot": {func(l *lockfile.Lock) { l.Names["component.rust-srv"] = "x" }, "not a slot name"},
		"names empty value":        {func(l *lockfile.Lock) { l.Names["project"] = "" }, "empty value"},
		"names component differs":  {func(l *lockfile.Lock) { l.Names["component.go-srv"] = "other" }, "must equal"},
		"names component missing":  {func(l *lockfile.Lock) { delete(l.Names, "component.go-srv") }, "is missing"},
		"source commit":            {func(l *lockfile.Lock) { l.Source = &lockfile.Source{Commit: "abc"} }, "source.commit"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			lock := validLock()
			tc.mutate(&lock)
			encoded, err := lockfile.Encode(lock)
			if err != nil {
				t.Fatal(err)
			}
			_, err = lockfile.Decode(encoded)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want one containing %q", err, tc.want)
			}
		})
	}
	for name, raw := range map[string]string{"unknown key": `{"schema":1,"extra":1}`, "trailing": "{\"schema\":1} {}", "empty": ""} {
		if _, err := lockfile.Decode([]byte(raw)); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
	}
}

func TestDecodeAcceptsSourceAndPorts(t *testing.T) {
	lock := validLock()
	lock.Source = &lockfile.Source{Commit: strings.Repeat("a", 40), Modified: true}
	lock.Ports = &lockfile.Ports{Band: 10100}
	encoded, err := lockfile.Encode(lock)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := lockfile.Decode(encoded)
	if err != nil || decoded.Source == nil || decoded.Ports == nil || decoded.Ports.Band != 10100 {
		t.Fatalf("decoded = %+v, error %v", decoded, err)
	}
	if decoded.Source.Commit != lock.Source.Commit || decoded.Source.Modified != lock.Source.Modified {
		t.Fatalf("source = %+v, want %+v", *decoded.Source, *lock.Source)
	}
}

func TestDecodeAcceptsEveryNamesSlot(t *testing.T) {
	lock := validLock()
	lock.Names = map[string]string{
		"project":          "acme",
		"displayName":      "Acme",
		"database":         "acme",
		"dbUser":           "acme",
		"envPrefix":        "ACME",
		"service.postgres": "acme-postgres",
		"component.go-srv": "go-srv",
	}
	encoded, err := lockfile.Encode(lock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockfile.Decode(encoded); err != nil {
		t.Fatalf("a lock with every names slot kind: %v", err)
	}
}

func TestSchema2OmitsSourceAndPortsWhenNil(t *testing.T) {
	encoded, err := lockfile.Encode(validLock())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"source"`) || strings.Contains(string(encoded), `"ports"`) {
		t.Fatalf("nil source or ports encoded: %s", encoded)
	}
}

func schema1Lock(extra string) string {
	return `{"schema":1,"generator":"0.1.0","catalogSha256":"` + hash + `",
"selection":{"schema":1,"project":{"name":"acme","goModule":"example.com/acme"},"stacks":["go-srv","ng-web"],"modules":["core"]},
"tckTags":[],"tckGrep":"","tckEnv":{},"specs":[],
"modules":[{"stack":"ng-web","module":"core","version":"0.1.0","implementsSpec":{},"files":[]},{"stack":"go-srv","module":"core","version":"0.1.0","implementsSpec":{},"files":[]}],
"generated":[]` + extra + `}`
}

func TestDecodeSchema1MapsEveryStackToItsId(t *testing.T) {
	lock, err := lockfile.Decode([]byte(schema1Lock("")))
	if err != nil {
		t.Fatal(err)
	}
	if lock.Schema != 1 || len(lock.Components) != 2 || lock.Components[0].Stack != "go-srv" || lock.Components[1].Stack != "ng-web" {
		t.Fatalf("components = %+v schema %d", lock.Components, lock.Schema)
	}
	for _, stack := range []string{"go-srv", "ng-web"} {
		if folder, ok := lock.FolderOf(stack); !ok || folder != stack {
			t.Errorf("FolderOf(%s) = %q %v", stack, folder, ok)
		}
	}
	if len(lock.Names) != 0 || lock.Source != nil || lock.Ports != nil {
		t.Errorf("schema 1 gained schema 2 members: %+v", lock)
	}
}

func TestDecodeSchema1RefusesSchema2Members(t *testing.T) {
	members := map[string]string{
		"components": `,"components":[]`,
		"names":      `,"names":{}`,
		"source":     `,"source":{"commit":"` + strings.Repeat("a", 40) + `","modified":false}`,
		"ports":      `,"ports":{"band":10100}`,
	}
	for name, member := range members {
		_, err := lockfile.Decode([]byte(schema1Lock(member)))
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
}

func TestDecodeSchema1RefusesSelectionSchema(t *testing.T) {
	raw := strings.Replace(schema1Lock(""), `"selection":{"schema":1`, `"selection":{"schema":2`, 1)
	_, err := lockfile.Decode([]byte(raw))
	if err == nil || !strings.Contains(err.Error(), "selection.schema") {
		t.Fatalf("want a selection.schema refusal, got %v", err)
	}
}

func TestModuleDirDropsHyphensOnlyForGo(t *testing.T) {
	if dir, ok := lockfile.ModuleDir("go-srv", "go-srv", "feature-flags"); !ok || dir != "go-srv/internal/platform/featureflags/" {
		t.Errorf("go dir = %q %v", dir, ok)
	}
	if dir, ok := lockfile.ModuleDir("acme-srv", "go-srv", "api-keys"); !ok || dir != "acme-srv/internal/platform/apikeys/" {
		t.Errorf("renamed go dir = %q %v", dir, ok)
	}
	if dir, ok := lockfile.ModuleDir("ng-web", "ng-web", "feature-flags"); !ok || dir != "ng-web/src/app/platform/feature-flags/" {
		t.Errorf("ng dir = %q %v", dir, ok)
	}
	if _, ok := lockfile.ModuleDir("x", "other", "x"); ok {
		t.Error("unknown stack accepted")
	}
}

func TestFolderOf(t *testing.T) {
	lock := lockfile.Lock{Components: []lockfile.Component{{Stack: "go-srv", Name: "acme-srv"}, {Stack: "ng-web", Name: "web-a"}, {Stack: "ng-web", Name: "web-b"}}}
	if folder, ok := lock.FolderOf("go-srv"); !ok || folder != "acme-srv" {
		t.Errorf("go-srv = %q %v", folder, ok)
	}
	if _, ok := lock.FolderOf("ng-web"); ok {
		t.Error("two components must give false")
	}
	if _, ok := lock.FolderOf("rust-srv"); ok {
		t.Error("absent stack must give false")
	}
}

func FuzzDecode(f *testing.F) {
	valid, err := lockfile.Encode(validLock())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte(`{"schema":1}`))
	f.Add([]byte(schema1Lock("")))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) {
		lock, err := lockfile.Decode(data)
		if err != nil {
			return
		}
		// An accepted lock only names safe paths, finds every module folder and re-encodes.
		for _, module := range lock.Modules {
			for _, file := range module.Files {
				if !lockfile.ValidPath(file.Path) {
					t.Fatalf("accepted path %q", file.Path)
				}
			}
			folder, hasFolder := lock.FolderOf(module.Stack)
			if !hasFolder {
				t.Fatalf("accepted stack %q without a component", module.Stack)
			}
			if _, ok := lockfile.ModuleDir(folder, module.Stack, module.Module); !ok {
				t.Fatalf("accepted stack %q", module.Stack)
			}
		}
		if lock.Schema != lockfile.SchemaVersion {
			return
		}
		encoded, err := lockfile.Encode(lock)
		if err != nil {
			t.Fatal(err)
		}
		again, err := lockfile.Decode(encoded)
		if err != nil {
			t.Fatalf("re-decode: %v", err)
		}
		if !reflect.DeepEqual(again, lock) {
			t.Fatalf("round trip differs: %+v vs %+v", again, lock)
		}
	})
}
