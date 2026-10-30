package catalog

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	envFile    = ".deploy/local/.example.env"
	kratosFile = ".deploy/local/kratos/kratos.yml"
)

// instanceRepo returns the ports repo with a per-component env file owned by
// core and the kratos folder owned per component by identity.
func instanceRepo(t *testing.T) fstest.MapFS {
	t.Helper()
	repo := portsRepo(t)
	put(repo, envFile, "APP_PUBLIC_BASE_URL=http://localhost:10100\nAPP_NAME=fixture-app\n")
	edit(t, repo, coreManifest, `"portFiles": ["`+composeFile+`"],`, `"portFiles": ["`+composeFile+`", "`+envFile+`"], "perComponent": ["`+envFile+`"],`)
	return repo
}

func TestPerComponentPathsAreRecordedSorted(t *testing.T) {
	built, issues := buildRepo(t, instanceRepo(t))
	assertCodes(t, issues)
	if got := findModule(t, built, "go-srv", "core").PerComponent; !reflect.DeepEqual(got, []string{envFile}) {
		t.Errorf("core perComponent = %v", got)
	}
	if got := findModule(t, built, "go-srv", "identity").PerComponent; !reflect.DeepEqual(got, []string{".deploy/local/kratos/"}) {
		t.Errorf("identity perComponent = %v", got)
	}
	encoded, err := Encode(built)
	if err != nil {
		t.Fatal(err)
	}
	if _, decodeIssues := Decode(encoded); len(decodeIssues) > 0 {
		t.Fatalf("decode: %v", decodeIssues)
	}
}

func TestPerComponentInvalid(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, repo fstest.MapFS)
	}{
		{"a file the module does not own", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, identityManifest, `"perComponent": [".deploy/local/kratos/"]`, `"perComponent": [".deploy/local/kratos/", "`+envFile+`"]`)
		}},
		{"a folder the module does not own", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, coreManifest, `"perComponent": ["`+envFile+`"]`, `"perComponent": [".deploy/local/postgres-init/"]`)
		}},
		{"a path outside .deploy/local", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, coreManifest, `"perComponent": ["`+envFile+`"]`, `"perComponent": ["specs/package.json"]`)
		}},
		{".deploy/local itself", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, coreManifest, `"perComponent": ["`+envFile+`"]`, `"perComponent": [".deploy/local/"]`)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := instanceRepo(t)
			tc.mutate(t, repo)
			_, issues := buildRepo(t, repo)
			want := []string{"PER_COMPONENT_INVALID"}
			if tc.name == ".deploy/local itself" {
				// The folder makes the compose file per-component too, so its
				// band range now sits in a per-component file.
				want = append(want, "PORT_RANGE_IN_RUN")
			}
			assertCodes(t, issues, want...)
		})
	}
}

func TestPortStrideInvalid(t *testing.T) {
	cases := []struct {
		name string
		port string
		want []string
	}{
		{"no stride", `{"name": "postgres", "offset": 32}`, []string{"PORT_STRIDE_INVALID"}},
		{"zero stride", `{"name": "postgres", "offset": 32, "stride": 0}`, []string{"PORT_STRIDE_INVALID"}},
		{"negative stride", `{"name": "postgres", "offset": 32, "stride": -3}`, []string{"PORT_STRIDE_INVALID"}},
		{"stride of one", `{"name": "postgres", "offset": 32, "stride": 1}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := instanceRepo(t)
			edit(t, repo, coreManifest, `{"name": "postgres", "offset": 32, "stride": 10}`, tc.port)
			_, issues := buildRepo(t, repo)
			assertCodes(t, issues, tc.want...)
			for _, found := range issues {
				if !strings.HasSuffix(found.Path, "#/ports/1/stride") {
					t.Errorf("path = %s", found.Path)
				}
			}
		})
	}
}

func TestNameRuleUnscoped(t *testing.T) {
	const instanceRule = `{"path": "` + composeFile + `", "text": "svc: %{component.go-srv}"}`
	const dirRule = `{"path": "` + composeFile + `", "text": "svc: ./%{instanceDir.go-srv}.example.env"}`
	cases := []struct {
		name string
		text string
		rule string
		want []string
	}{
		{"instance slot outside every run", "name: fixture-app\nsvc: go-srv\n", instanceRule, []string{"NAME_RULE_UNSCOPED"}},
		{"instance slot in a module block outside a run", "name: fixture-app\n# initializer:module identity\nsvc: go-srv\n# initializer:end identity\n", instanceRule, []string{"NAME_RULE_UNSCOPED"}},
		{"instance slot inside a run", "name: fixture-app\n# initializer:stack go-srv\nsvc: go-srv\n# initializer:end go-srv\n", instanceRule, nil},
		{"instance slot in the second block of a run", "name: fixture-app\n# initializer:stack go-srv\na: 1\n# initializer:end go-srv\n# initializer:module identity\nsvc: go-srv\n# initializer:end identity\n", instanceRule, nil},
		{"a derived instance slot outside every run", "name: fixture-app\nsvc: ./.example.env\n", dirRule, []string{"NAME_RULE_UNSCOPED"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := instanceRepo(t)
			put(repo, composeFile, tc.text)
			setRules(t, repo, projectRule, tc.rule)
			_, issues := buildRepo(t, repo)
			assertCodes(t, issues, tc.want...)
		})
	}
}

func TestProjectSlotsAreAllowedOutsideEveryRun(t *testing.T) {
	repo := instanceRepo(t)
	setRules(t, repo, projectRule)
	_, issues := buildRepo(t, repo)
	assertCodes(t, issues)
}

func TestNameRuleInAPerComponentFileIsNotProjectScope(t *testing.T) {
	repo := instanceRepo(t)
	setRules(t, repo, projectRule, `{"path": "`+envFile+`", "text": "APP_NAME=%{component.go-srv}"}`)
	put(repo, envFile, "APP_PUBLIC_BASE_URL=http://localhost:10100\nAPP_NAME=go-srv\n")
	_, issues := buildRepo(t, repo)
	assertCodes(t, issues)
}

func TestPortUnscoped(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"instance port outside every run", "name: fixture-app\napi: 10132\n", []string{"PORT_UNSCOPED"}},
		{"instance port in a module block outside a run", "name: fixture-app\n# initializer:module identity\napi: 10133\n# initializer:end identity\n", []string{"PORT_UNSCOPED"}},
		{"instance port inside a run", "name: fixture-app\n# initializer:stack go-srv\napi: 10132\n# initializer:end go-srv\n", nil},
		{"srv offset is the band bound outside a run", "name: fixture-app\napi: 10100\n", nil},
		{"band bound", "name: fixture-app\nrange: 10199\n", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := instanceRepo(t)
			put(repo, composeFile, tc.text)
			_, issues := buildRepo(t, repo)
			assertCodes(t, issues, tc.want...)
		})
	}
}

func TestPortTokensOfAPerComponentFileNeedNoRun(t *testing.T) {
	repo := instanceRepo(t)
	put(repo, kratosFile, "public: http://localhost:10133\nweb: http://localhost:10132\n")
	_, issues := buildRepo(t, repo)
	assertCodes(t, issues)
}

func TestPortRangeInAPerComponentFile(t *testing.T) {
	repo := instanceRepo(t)
	put(repo, envFile, "RANGE=10100-10199\n")
	_, issues := buildRepo(t, repo)
	assertCodes(t, issues, "PORT_RANGE_IN_RUN")
}

func TestDecodeRefusesBadStrideAndPerComponent(t *testing.T) {
	built, issues := buildRepo(t, instanceRepo(t))
	assertCodes(t, issues)
	cases := map[string]func(m *ModuleEntry){
		"zero stride":           func(m *ModuleEntry) { m.Ports[0].Stride = 0 },
		"unsorted perComponent": func(m *ModuleEntry) { m.PerComponent = []string{"b", "a"} },
		"escaping path":         func(m *ModuleEntry) { m.PerComponent = []string{"../a"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			copied := built
			copied.Stacks = append([]StackEntry(nil), built.Stacks...)
			copied.Stacks[0].Modules = append([]ModuleEntry(nil), built.Stacks[0].Modules...)
			module := copied.Stacks[0].Modules[0]
			module.Ports = append(module.Ports[:0:0], module.Ports...)
			module.PerComponent = append([]string(nil), module.PerComponent...)
			mutate(&module)
			copied.Stacks[0].Modules[0] = module
			encoded, err := Encode(copied)
			if err != nil {
				t.Fatal(err)
			}
			if _, decodeIssues := Decode(encoded); len(decodeIssues) == 0 {
				t.Fatal("Decode accepted the catalog")
			}
		})
	}
}
