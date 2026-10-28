package lockfile_test

import (
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

var hash = strings.Repeat("a", 64)

func validLock() lockfile.Lock {
	return lockfile.Lock{
		Schema:        1,
		Generator:     "0.1.0",
		CatalogSHA256: hash,
		Selection:     manifest.Selection{Schema: 1, Project: manifest.Project{Name: "acme", GoModule: "example.com/acme"}, Stacks: []string{"go-srv"}, Modules: []string{"core"}},
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
	cases := map[string]func(*lockfile.Lock){
		"schema":        func(l *lockfile.Lock) { l.Schema = 2 },
		"stack":         func(l *lockfile.Lock) { l.Modules[0].Stack = "rust-srv" },
		"module id":     func(l *lockfile.Lock) { l.Modules[0].Module = "../x" },
		"spec id":       func(l *lockfile.Lock) { l.Specs[0].Spec = "A B" },
		"escaping path": func(l *lockfile.Lock) { l.Generated[0].Path = "../x" },
		"absolute path": func(l *lockfile.Lock) { l.Generated[0].Path = "/etc/x" },
		"backslash":     func(l *lockfile.Lock) { l.Generated[0].Path = "a\\b" },
		"bad hash":      func(l *lockfile.Lock) { l.Generated[0].SHA256 = "zz" },
		"lists itself":  func(l *lockfile.Lock) { l.Generated[0].Path = lockfile.Name },
		"duplicate":     func(l *lockfile.Lock) { l.Generated[0].Path = "go-srv/go.mod" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			lock := validLock()
			mutate(&lock)
			encoded, err := lockfile.Encode(lock)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := lockfile.Decode(encoded); err == nil {
				t.Fatal("want a refusal")
			}
		})
	}
	for name, raw := range map[string]string{"unknown key": `{"schema":1,"extra":1}`, "trailing": "{\"schema\":1} {}", "empty": ""} {
		if _, err := lockfile.Decode([]byte(raw)); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
	}
}

func TestModuleDirDropsHyphensOnlyForGo(t *testing.T) {
	if dir, ok := lockfile.ModuleDir("go-srv", "feature-flags"); !ok || dir != "go-srv/internal/platform/featureflags/" {
		t.Errorf("go dir = %q %v", dir, ok)
	}
	if dir, ok := lockfile.ModuleDir("ng-web", "feature-flags"); !ok || dir != "ng-web/src/app/platform/feature-flags/" {
		t.Errorf("ng dir = %q %v", dir, ok)
	}
	if _, ok := lockfile.ModuleDir("other", "x"); ok {
		t.Error("unknown stack accepted")
	}
}

func FuzzDecode(f *testing.F) {
	valid, err := lockfile.Encode(validLock())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte(`{"schema":1}`))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) {
		lock, err := lockfile.Decode(data)
		if err != nil {
			return
		}
		// An accepted lock only names safe paths and re-encodes.
		for _, module := range lock.Modules {
			for _, file := range module.Files {
				if !lockfile.ValidPath(file.Path) {
					t.Fatalf("accepted path %q", file.Path)
				}
			}
			if _, ok := lockfile.ModuleDir(module.Stack, module.Module); !ok {
				t.Fatalf("accepted stack %q", module.Stack)
			}
		}
		if _, err := lockfile.Encode(lock); err != nil {
			t.Fatal(err)
		}
	})
}
