package plan_test

import (
	"crypto/sha256"
	"encoding/hex"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"testing/quick"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
)

func load(t *testing.T, name string) catalog.Catalog {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	cat, issues := catalog.Decode(data)
	if len(issues) > 0 {
		t.Fatalf("fixture %s: %v", name, issues)
	}
	return cat
}

func selection(modules ...string) manifest.Selection {
	return manifest.Selection{
		Schema:  1,
		Project: manifest.Project{Name: "acme-backend", GoModule: "example.com/acme/backend"},
		Stacks:  []string{"go-srv"},
		Modules: modules,
	}
}

func mustResolve(t *testing.T, cat catalog.Catalog, sel manifest.Selection) plan.Plan {
	t.Helper()
	p, issues := plan.Resolve(cat, sel)
	if len(issues) > 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	return p
}

func ids(stack plan.StackPlan) []string {
	out := make([]string, len(stack.Modules))
	for i, module := range stack.Modules {
		out[i] = module.Module
	}
	return out
}

func wantIDs(t *testing.T, stack plan.StackPlan, want ...string) {
	t.Helper()
	if got := ids(stack); !reflect.DeepEqual(got, want) {
		t.Fatalf("modules = %v, want %v", got, want)
	}
}

// wantCodes asserts the exact list of issue codes and a zero Plan.
func wantCodes(t *testing.T, cat catalog.Catalog, sel manifest.Selection, want ...string) []issue.Issue {
	t.Helper()
	p, issues := plan.Resolve(cat, sel)
	if !reflect.DeepEqual(p, plan.Plan{}) {
		t.Fatalf("plan must be zero when issues exist: %+v", p)
	}
	got := make([]string, len(issues))
	for i, found := range issues {
		got[i] = found.Code
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("codes = %v, want %v (%v)", got, want, issues)
	}
	return issues
}

func backendModule(t *testing.T, cat *catalog.Catalog, id string) *catalog.ModuleEntry {
	t.Helper()
	for s := range cat.Stacks {
		for m := range cat.Stacks[s].Modules {
			if cat.Stacks[s].Role == "backend" && cat.Stacks[s].Modules[m].Module == id {
				return &cat.Stacks[s].Modules[m]
			}
		}
	}
	t.Fatalf("no backend module %s", id)
	return nil
}

func TestEmptySelectionResolvesCore(t *testing.T) {
	p := mustResolve(t, load(t, "backend.json"), selection())
	wantIDs(t, p.Stacks[0], "core")
	if !reflect.DeepEqual(p.Selection.Modules, []string{"core"}) {
		t.Fatalf("recorded modules = %v", p.Selection.Modules)
	}
	if p.GoModule != "example.com/fixture/srv" || p.Reference["project"] != "fixture-app" {
		t.Fatalf("catalog identity not carried: %q %q", p.GoModule, p.Reference["project"])
	}
}

func TestMilestoneOrderForBothSelectionForms(t *testing.T) {
	cat := load(t, "backend.json")
	all := selection()
	all.AllModules = true
	for name, sel := range map[string]manifest.Selection{
		"all":      all,
		"explicit": selection("parameters", "feature-flags"),
	} {
		t.Run(name, func(t *testing.T) {
			p := mustResolve(t, cat, sel)
			want := []string{"core", "identity", "feature-flags", "parameters"}
			wantIDs(t, p.Stacks[0], want...)
			if !reflect.DeepEqual(p.Selection.Modules, want) || p.Selection.AllModules {
				t.Fatalf("recorded selection = %+v", p.Selection)
			}
			if p.Selection.Project.Name != "acme-backend" || !reflect.DeepEqual(p.Selection.Stacks, []string{"go-srv"}) {
				t.Fatalf("recorded selection = %+v", p.Selection)
			}
			if p.TCKGrep != "@(core|feature-flags|identity|otel|parameters)(?![A-Za-z0-9_-])" {
				t.Fatalf("grep = %q", p.TCKGrep)
			}
			wantEnv := map[string]string{"APP_FLAGS_FILE": "specs/feature-flags/tck/fixtures/flags.yaml"}
			if !reflect.DeepEqual(p.TCKEnv, wantEnv) {
				t.Fatalf("env = %v", p.TCKEnv)
			}
		})
	}
}

func TestTCKGrepIsAnchored(t *testing.T) {
	cat := load(t, "backend.json")
	p := mustResolve(t, cat, selection("parameters"))
	if p.TCKGrep != "@(core|identity|otel|parameters)(?![A-Za-z0-9_-])" {
		t.Fatalf("grep = %q", p.TCKGrep)
	}
	if len(p.TCKEnv) != 0 {
		t.Fatalf("env = %v", p.TCKEnv)
	}
	specNames := []string{}
	for _, spec := range p.Specs {
		specNames = append(specNames, spec.Spec)
	}
	if !reflect.DeepEqual(specNames, []string{"core", "identity", "parameters"}) {
		t.Fatalf("specs = %v", specNames)
	}

	coreOnly := load(t, "backend.json")
	coreOnly.Specs[0].TCKTags = []string{"@core"}
	p = mustResolve(t, coreOnly, selection())
	if p.TCKGrep != "@(core)(?![A-Za-z0-9_-])" {
		t.Fatalf("grep = %q", p.TCKGrep)
	}
}

func TestRequiresChainPullsAndOrdersDependencies(t *testing.T) {
	cat := load(t, "chain.json")
	p := mustResolve(t, cat, selection("b"))
	wantIDs(t, p.Stacks[0], "core", "a", "b")
	// Diamond: d needs b and c, both need a. Ready ties break by id.
	p = mustResolve(t, cat, selection("d"))
	wantIDs(t, p.Stacks[0], "core", "a", "b", "c", "d")
}

func TestImpliedModulesComeFirstAmongReady(t *testing.T) {
	// "a" sorts before "core" bytewise; the implied module still wins.
	p := mustResolve(t, load(t, "chain.json"), selection("a"))
	wantIDs(t, p.Stacks[0], "core", "a")
}

func TestDuplicatesAndImpliedIdsListedExplicitly(t *testing.T) {
	cat := load(t, "backend.json")
	clean := mustResolve(t, cat, selection("parameters"))
	noisy := mustResolve(t, cat, selection("parameters", "core", "identity", "parameters", "identity"))
	if !reflect.DeepEqual(clean, noisy) {
		t.Fatalf("plans differ:\n%+v\n%+v", clean, noisy)
	}
}

func TestResolveIgnoresOrderAndDuplicationOfModules(t *testing.T) {
	cat := load(t, "chain.json")
	base := mustResolve(t, cat, selection("a", "b", "c", "d"))
	rng := rand.New(rand.NewSource(1))
	pool := []string{"a", "b", "c", "d", "core"}
	for i := 0; i < 50; i++ {
		modules := []string{"d"}
		for j := rng.Intn(8); j > 0; j-- {
			modules = append(modules, pool[rng.Intn(len(pool))])
		}
		rng.Shuffle(len(modules), func(a, b int) { modules[a], modules[b] = modules[b], modules[a] })
		got := mustResolve(t, cat, selection(modules...))
		if !reflect.DeepEqual(got, base) {
			t.Fatalf("modules %v gave a different plan", modules)
		}
	}
}

func TestModuleUnknown(t *testing.T) {
	issues := wantCodes(t, load(t, "backend.json"), selection("ghost"), plan.CodeModuleUnknown)
	if !strings.HasSuffix(issues[0].Path, "#/modules/0") {
		t.Fatalf("path = %s", issues[0].Path)
	}
}

func TestModuleUnknownThroughRequirement(t *testing.T) {
	cat := load(t, "chain.json")
	module := backendModule(t, &cat, "b")
	module.RequiresModules = append(module.RequiresModules, catalog.Requirement{Module: "ghost", Range: "^0.1.0"})
	wantCodes(t, cat, selection("b"), plan.CodeModuleUnknown)
}

func TestFrontendOnlyModuleIsNotInBackend(t *testing.T) {
	cat := load(t, "fullstack.json")
	sel := selection("console")
	sel.Stacks = []string{"ng-web", "go-srv"}
	wantCodes(t, cat, sel, catalog.CodeModuleNotInBackend)
}

func TestFrontendStackMissingModuleIsNotCopied(t *testing.T) {
	cat := load(t, "fullstack.json")
	sel := selection("parameters")
	sel.Stacks = []string{"ng-web", "go-srv"}
	p := mustResolve(t, cat, sel)
	if p.Stacks[0].Stack != "go-srv" || p.Stacks[1].Stack != "ng-web" {
		t.Fatalf("stacks not sorted: %s %s", p.Stacks[0].Stack, p.Stacks[1].Stack)
	}
	wantIDs(t, p.Stacks[0], "core", "identity", "parameters")
	wantIDs(t, p.Stacks[1], "core", "parameters")
	if !reflect.DeepEqual(p.Selection.Stacks, []string{"go-srv", "ng-web"}) {
		t.Fatalf("recorded stacks = %v", p.Selection.Stacks)
	}
}

func TestSelectedStackWithoutModulesIsInvalid(t *testing.T) {
	sel := selection()
	sel.Stacks = []string{"go-srv", "ng-web"}
	issues := wantCodes(t, load(t, "backend.json"), sel, manifest.CodeSelectionInvalid)
	if !strings.HasSuffix(issues[0].Path, "#/stacks/1") || !strings.Contains(issues[0].Message, "no modules in the catalog") {
		t.Fatalf("issue = %+v", issues[0])
	}
}

func TestStackChoiceIsValidated(t *testing.T) {
	cat := load(t, "fullstack.json")
	frontendOnly := selection()
	frontendOnly.Stacks = []string{"ng-web"}
	wantCodes(t, cat, frontendOnly, manifest.CodeSelectionInvalid)
	twice := selection()
	twice.Stacks = []string{"go-srv", "go-srv"}
	wantCodes(t, cat, twice, manifest.CodeSelectionInvalid)
	both := selection()
	both.Stacks = []string{"go-srv", "ng-web"}
	twoBackends := load(t, "fullstack.json")
	twoBackends.Stacks[1].Role = "backend"
	issues := wantCodes(t, twoBackends, both, manifest.CodeSelectionInvalid)
	if !strings.Contains(issues[0].Message, "exactly one backend") {
		t.Fatalf("issue = %+v", issues[0])
	}
	twoFrontends := load(t, "fullstack.json")
	twoFrontends.Stacks[0].Role = "frontend"
	issues = wantCodes(t, twoFrontends, both, manifest.CodeSelectionInvalid, manifest.CodeSelectionInvalid)
	if !strings.Contains(issues[0].Message, "at most one frontend") {
		t.Fatalf("issue = %+v", issues[0])
	}
}

func TestRequiresUnsatisfied(t *testing.T) {
	t.Run("range excludes core", func(t *testing.T) {
		cat := load(t, "backend.json")
		backendModule(t, &cat, "parameters").RequiresCore = "^0.2.0"
		wantCodes(t, cat, selection("parameters"), catalog.CodeRequiresUnsatisfied)
	})
	t.Run("module range excludes dependency", func(t *testing.T) {
		cat := load(t, "chain.json")
		backendModule(t, &cat, "b").RequiresModules[0].Range = "^0.2.0"
		wantCodes(t, cat, selection("b"), catalog.CodeRequiresUnsatisfied)
	})
	t.Run("0.0.Z exact range", func(t *testing.T) {
		cat := load(t, "backend.json")
		backendModule(t, &cat, "core").Version = "0.0.5"
		backendModule(t, &cat, "identity").RequiresCore = "0.0.5"
		backendModule(t, &cat, "parameters").RequiresCore = "0.0.5"
		mustResolve(t, cat, selection("parameters"))
		backendModule(t, &cat, "parameters").RequiresCore = "^0.0.6"
		wantCodes(t, cat, selection("parameters"), catalog.CodeRequiresUnsatisfied)
	})
	t.Run("frontend requirement missing from its stack", func(t *testing.T) {
		cat := load(t, "fullstack.json")
		cat.Stacks[1].Modules[3].RequiresModules = []catalog.Requirement{{Module: "identity", Range: "^0.1.0"}}
		sel := selection("parameters")
		sel.Stacks = []string{"go-srv", "ng-web"}
		wantCodes(t, cat, sel, catalog.CodeRequiresUnsatisfied)
	})
}

func TestDependencyCycle(t *testing.T) {
	cat := load(t, "chain.json")
	backendModule(t, &cat, "a").RequiresModules = []catalog.Requirement{{Module: "b", Range: "^0.1.0"}}
	wantCodes(t, cat, selection("b"), catalog.CodeDependencyCycle)
}

func TestSpecNotInBackend(t *testing.T) {
	cat := load(t, "fullstack.json")
	cat.Stacks[1].Modules[3].ImplementsSpec = map[string]string{"parameters": "0.1.0", "feature-flags": "0.1.0"}
	sel := selection("parameters")
	sel.Stacks = []string{"go-srv", "ng-web"}
	issues := wantCodes(t, cat, sel, plan.CodeSpecNotInBackend)
	if !strings.Contains(issues[0].Path, "implementsSpec/feature-flags") {
		t.Fatalf("path = %s", issues[0].Path)
	}
	// Selecting the backend module that implements the spec clears the issue.
	sel.Modules = []string{"parameters", "feature-flags"}
	mustResolve(t, cat, sel)
}

func TestSpecNotInBackendMessage(t *testing.T) {
	const prefix = `no selected backend module implements spec "identity" (needed by ng-web core)`
	cases := []struct {
		name   string
		mutate func(t *testing.T, cat *catalog.Catalog)
		want   string
	}{
		{"one module implements the spec", func(t *testing.T, cat *catalog.Catalog) {},
			prefix + `; select module "identity" or drop stack ng-web`},
		{"several modules implement the spec", func(t *testing.T, cat *catalog.Catalog) {
			backendModule(t, cat, "parameters").ImplementsSpec["identity"] = "0.1.0"
			backendModule(t, cat, "feature-flags").ImplementsSpec["identity"] = "0.1.0"
		}, prefix + `; select module "feature-flags,identity,parameters" or drop stack ng-web`},
		{"no module implements the spec", func(t *testing.T, cat *catalog.Catalog) {
			backendModule(t, cat, "identity").ImplementsSpec = map[string]string{}
		}, prefix},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cat := load(t, "fullstack.json")
			cat.Stacks[1].Modules[1].ImplementsSpec = map[string]string{"core": "0.1.0", "identity": "0.1.0"}
			tc.mutate(t, &cat)
			sel := selection()
			sel.Stacks = []string{"go-srv", "ng-web"}
			issues := wantCodes(t, cat, sel, plan.CodeSpecNotInBackend)
			if issues[0].Message != tc.want {
				t.Fatalf("message = %q, want %q", issues[0].Message, tc.want)
			}
			if want := "catalog.lock.json#/stacks/1/modules/1/implementsSpec/identity"; issues[0].Path != want {
				t.Fatalf("path = %s, want %s", issues[0].Path, want)
			}
		})
	}
}

func TestGeneratorMismatch(t *testing.T) {
	cat := load(t, "backend.json")
	cat.Generator = "9.9.9"
	wantCodes(t, cat, selection(), plan.CodeGeneratorMismatch)
}

func TestTCKEnvConflictAndAgreement(t *testing.T) {
	cat := load(t, "backend.json")
	cat.Specs[3].TCKEnv = map[string]string{"APP_FLAGS_FILE": "other.yaml"}
	cat.Specs[1].TCKEnv = map[string]string{"APP_FLAGS_FILE": "flags.yaml"}
	wantCodes(t, cat, selection("parameters", "feature-flags"), plan.CodeTCKEnvConflict)
	// A conflicting spec that is not copied does not count.
	mustResolve(t, cat, selection("feature-flags"))
	cat.Specs[3].TCKEnv["APP_FLAGS_FILE"] = "flags.yaml"
	p := mustResolve(t, cat, selection("parameters", "feature-flags"))
	if p.TCKEnv["APP_FLAGS_FILE"] != "flags.yaml" || len(p.TCKEnv) != 1 {
		t.Fatalf("env = %v", p.TCKEnv)
	}
}

func TestHostileCatalogValuesAreRefused(t *testing.T) {
	tag := load(t, "backend.json")
	tag.Specs[0].TCKTags = []string{"@core|.*"}
	wantCodes(t, tag, selection(), catalog.CodeCatalogInvalid)
	env := load(t, "backend.json")
	env.Specs[1].TCKEnv = map[string]string{"APP_FLAGS_FILE": "a`b"}
	wantCodes(t, env, selection("feature-flags"), catalog.CodeCatalogInvalid)
	for _, hostile := range []string{"a\nb", "a\rb", "a\x00b"} {
		bad := load(t, "backend.json")
		bad.Specs[1].TCKEnv = map[string]string{"APP_FLAGS_FILE": hostile}
		wantCodes(t, bad, selection("feature-flags"), catalog.CodeCatalogInvalid)
	}
	key := load(t, "backend.json")
	key.Specs[1].TCKEnv = map[string]string{"lower": "x"}
	wantCodes(t, key, selection("feature-flags"), catalog.CodeCatalogInvalid)
	version := load(t, "backend.json")
	backendModule(t, &version, "core").Version = "not-a-version"
	wantCodes(t, version, selection("parameters"), catalog.CodeCatalogInvalid)
	rng := load(t, "backend.json")
	backendModule(t, &rng, "parameters").RequiresCore = "not a range"
	wantCodes(t, rng, selection("parameters"), catalog.CodeCatalogInvalid)
}

func TestCoreMissing(t *testing.T) {
	cat := load(t, "chain.json")
	module := backendModule(t, &cat, "core")
	module.Module = "zcore"
	module.Implied = false
	wantCodes(t, cat, selection(), catalog.CodeCoreMissing)
}

func TestCoreIsPlannedWhenNotImplied(t *testing.T) {
	cat := load(t, "backend.json")
	backendModule(t, &cat, "core").Implied = false
	p := mustResolve(t, cat, selection())
	wantIDs(t, p.Stacks[0], "core")
}

func TestSpecUnknown(t *testing.T) {
	cat := load(t, "backend.json")
	kept := cat.Specs[:0:0]
	for _, spec := range cat.Specs {
		if spec.Spec != "feature-flags" {
			kept = append(kept, spec)
		}
	}
	cat.Specs = kept
	issues := wantCodes(t, cat, selection("feature-flags"), catalog.CodeSpecUnknown)
	if !strings.Contains(issues[0].Message, "feature-flags") {
		t.Fatalf("message = %s", issues[0].Message)
	}
	// A spec no selected module implements is not required.
	mustResolve(t, cat, selection())
}

func TestIssuesAreReportedTogether(t *testing.T) {
	cat := load(t, "backend.json")
	cat.Generator = "9.9.9"
	wantCodes(t, cat, selection("ghost", "phantom"), plan.CodeGeneratorMismatch, plan.CodeModuleUnknown, plan.CodeModuleUnknown)
}

func sha(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func TestCheckSources(t *testing.T) {
	cat := load(t, "backend.json")
	repo := fstest.MapFS{
		"go-srv/a.go":    {Data: []byte("package a\n")},
		"specs/core/x.y": {Data: []byte("spec\n")},
	}
	// Hashes are computed from the in-memory files, never pasted.
	backendModule(t, &cat, "core").Files = []catalog.File{{Path: "go-srv/a.go", SHA256: sha("package a\n")}}
	cat.Specs[0].Files = []catalog.File{{Path: "specs/core/x.y", SHA256: sha("spec\n")}}
	p := mustResolve(t, cat, selection())
	issues, err := plan.CheckSources(repo, p)
	if err != nil || len(issues) != 0 {
		t.Fatalf("clean tree: issues=%v err=%v", issues, err)
	}

	repo["go-srv/a.go"] = &fstest.MapFile{Data: []byte("package b\n")}
	delete(repo, "specs/core/x.y")
	issues, err = plan.CheckSources(repo, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 || issues[0].Code != plan.CodeFileMismatch || issues[0].Path != "go-srv/a.go" ||
		issues[1].Code != plan.CodeFileMismatch || issues[1].Path != "specs/core/x.y" {
		t.Fatalf("issues = %v", issues)
	}
}

func TestCheckSourcesIgnoresFilesOfUnplannedModules(t *testing.T) {
	cat := load(t, "backend.json")
	backendModule(t, &cat, "parameters").Files = []catalog.File{{Path: "go-srv/p.go", SHA256: sha("x")}}
	p := mustResolve(t, cat, selection())
	issues, err := plan.CheckSources(fstest.MapFS{}, p)
	if err != nil || len(issues) != 0 {
		t.Fatalf("issues=%v err=%v", issues, err)
	}
}

func TestCheckSourcesReportsReadFailureAsError(t *testing.T) {
	cat := load(t, "backend.json")
	backendModule(t, &cat, "core").Files = []catalog.File{{Path: "go-srv/dir", SHA256: strings.Repeat("0", 64)}}
	p := mustResolve(t, cat, selection())
	repo := fstest.MapFS{"go-srv/dir/file": {Data: []byte("x")}}
	if _, err := plan.CheckSources(repo, p); err == nil {
		t.Fatal("reading a directory as a file must be an I/O error")
	}
}

func TestCheckSourcesRefusesPathsOutsideTheRepo(t *testing.T) {
	for _, hostile := range []string{"../outside.go", "/etc/passwd", "go-srv/../../x.go", ""} {
		cat := load(t, "backend.json")
		backendModule(t, &cat, "core").Files = []catalog.File{{Path: hostile, SHA256: sha("x")}}
		p := mustResolve(t, cat, selection())
		repo := fstest.MapFS{"outside.go": {Data: []byte("x")}}
		issues, err := plan.CheckSources(repo, p)
		if err != nil {
			t.Fatalf("%q: %v", hostile, err)
		}
		if len(issues) != 1 || issues[0].Code != plan.CodeFileMismatch {
			t.Fatalf("%q: issues = %v", hostile, issues)
		}
	}
}

func FuzzResolveSelection(f *testing.F) {
	f.Add("parameters", "go-srv")
	f.Add("", "ng-web")
	f.Add("../../x\x00", "go-srv")
	data, err := os.ReadFile(filepath.Join("testdata", "fullstack.json"))
	if err != nil {
		f.Fatal(err)
	}
	cat, issues := catalog.Decode(data)
	if len(issues) > 0 {
		f.Fatal(issues)
	}
	f.Fuzz(func(t *testing.T, module, stack string) {
		sel := selection(module)
		sel.Stacks = []string{"go-srv", stack}
		p, found := plan.Resolve(cat, sel)
		hasIssues := len(found) > 0
		isZero := reflect.DeepEqual(p, plan.Plan{})
		if hasIssues != isZero {
			t.Fatalf("issues=%v zero=%v", found, isZero)
		}
	})
}

func TestGrepOfNoTagsIsEmpty(t *testing.T) {
	if got := plan.Grep(nil); got != "" {
		t.Fatalf("grep = %q", got)
	}
}

// withCooperation adds a tenancy spec and module to the backend fixture and
// makes the named specs cooperate with the given tags.
func withCooperation(t *testing.T, cooperation map[string][]string) catalog.Catalog {
	t.Helper()
	cat := load(t, "backend.json")
	cat.Specs = append(cat.Specs, catalog.SpecEntry{Spec: "tenancy", Version: "0.1.0", TCKTags: []string{"@tenancy"}, TCKEnv: map[string]string{}, Files: []catalog.File{}})
	for i := range cat.Specs {
		cat.Specs[i].CooperatesWith = cooperation[cat.Specs[i].Spec]
	}
	for s := range cat.Stacks {
		if cat.Stacks[s].Role == "backend" {
			cat.Stacks[s].Modules = append(cat.Stacks[s].Modules, catalog.ModuleEntry{
				Module: "tenancy", Version: "0.1.0", ImplementsSpec: map[string]string{"tenancy": "0.1.0"},
				RequiresCore: "^0.1.0", RequiresModules: []catalog.Requirement{}, Files: []catalog.File{},
			})
		}
	}
	return cat
}

func TestMalformedCooperatesWithIsCatalogInvalid(t *testing.T) {
	cat := withCooperation(t, map[string][]string{"parameters": {"@tenancy|.*"}})
	wantCodes(t, cat, selection("parameters"), catalog.CodeCatalogInvalid)
}

func TestTCKGrepInvert(t *testing.T) {
	cases := []struct {
		name        string
		cooperation map[string][]string
		modules     []string
		want        string
	}{
		{"cooperating tag not installed", map[string][]string{"parameters": {"@tenancy"}}, []string{"parameters"}, "@(tenancy)(?![A-Za-z0-9_-])"},
		{"cooperating tag installed", map[string][]string{"parameters": {"@tenancy"}}, []string{"parameters", "tenancy"}, ""},
		{"no copied spec cooperates", nil, []string{"parameters"}, ""},
		{"cooperation of an uncopied spec is ignored", map[string][]string{"tenancy": {"@parameters"}}, []string{"feature-flags"}, ""},
		{"two specs naming one tag list it once", map[string][]string{"parameters": {"@tenancy"}, "feature-flags": {"@tenancy"}}, []string{"parameters", "feature-flags"}, "@(tenancy)(?![A-Za-z0-9_-])"},
		{"several tags are sorted", map[string][]string{"parameters": {"@tenancy"}, "feature-flags": {"@otel-x", "@api-keys"}}, []string{"parameters", "feature-flags"}, "@(api-keys|otel-x|tenancy)(?![A-Za-z0-9_-])"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cat := withCooperation(t, tc.cooperation)
			p := mustResolve(t, cat, selection(tc.modules...))
			if p.TCKGrepInvert != tc.want {
				t.Fatalf("TCKGrepInvert = %q, want %q", p.TCKGrepInvert, tc.want)
			}
		})
	}
}

func TestGrepAgreesWithTagPattern(t *testing.T) {
	const head = "abcdefghijklmnopqrstuvwxyz"
	const tail = "abcdefghijklmnopqrstuvwxyz0123456789-"
	property := func(first uint8, rest []uint8) bool {
		tag := "@" + string(head[int(first)%len(head)])
		for _, b := range rest {
			tag += string(tail[int(b)%len(tail)])
		}
		if !manifest.TagPattern.MatchString(tag) {
			return false
		}
		// RE2 has no lookahead: express the trailing (?!...) as a consuming
		// class or the end of the text.
		grep := plan.Grep([]string{tag})
		if !strings.HasSuffix(grep, "(?![A-Za-z0-9_-])") {
			return false
		}
		pattern, err := regexp.Compile(strings.TrimSuffix(grep, "(?![A-Za-z0-9_-])") + "([^A-Za-z0-9_-]|$)")
		if err != nil {
			return false
		}
		matchesTag := pattern.MatchString("x " + tag)
		matchesLonger := pattern.MatchString("x " + tag + "-y")
		return matchesTag && !matchesLonger
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 2000}); err != nil {
		t.Fatal(err)
	}
}

func TestPulls(t *testing.T) {
	backend := load(t, "backend.json")
	all := selection()
	all.AllModules = true
	cases := []struct {
		name string
		cat  catalog.Catalog
		sel  manifest.Selection
		want []plan.Pull
	}{
		{"typed module pulls its requirement", backend, selection("parameters"),
			[]plan.Pull{{Module: "identity", RequiredBy: []string{"parameters"}, Stack: "go-srv"}}},
		{"requirement named in the selection is not a pull", backend, selection("identity", "parameters"), nil},
		{"two requirers are listed sorted", backend, selection("parameters", "feature-flags"),
			[]plan.Pull{{Module: "identity", RequiredBy: []string{"feature-flags", "parameters"}, Stack: "go-srv"}}},
		{"the same selection in another order reports the same pull", backend, selection("feature-flags", "parameters"),
			[]plan.Pull{{Module: "identity", RequiredBy: []string{"feature-flags", "parameters"}, Stack: "go-srv"}}},
		{"all modules reports nothing", backend, all, nil},
		{"core alone pulls nothing", backend, selection(), nil},
		{"a chain reports every pulled module", load(t, "chain.json"), selection("d"), []plan.Pull{
			{Module: "a", RequiredBy: []string{"b", "c"}, Stack: "go-srv"},
			{Module: "b", RequiredBy: []string{"d"}, Stack: "go-srv"},
			{Module: "c", RequiredBy: []string{"d"}, Stack: "go-srv"},
		}},
		{"an implied module no one requires is not a pull", load(t, "chain.json"), selection("a"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := plan.Pulls(mustResolve(t, tc.cat, tc.sel), tc.sel)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("pulls = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestPullsAreReportedPerStack(t *testing.T) {
	cat := load(t, "fullstack.json")
	cat.Stacks[1].Modules = append(cat.Stacks[1].Modules, catalog.ModuleEntry{
		Module: "identity", Version: "0.1.0", ImplementsSpec: map[string]string{}, RequiresCore: "^0.1.0",
		RequiresModules: []catalog.Requirement{}, Files: []catalog.File{},
	})
	cat.Stacks[1].Modules[3].RequiresModules = []catalog.Requirement{{Module: "identity", Range: "^0.1.0"}}
	sel := selection("parameters")
	sel.Stacks = []string{"ng-web", "go-srv"}
	want := []plan.Pull{
		{Module: "identity", RequiredBy: []string{"parameters"}, Stack: "go-srv"},
		{Module: "identity", RequiredBy: []string{"parameters"}, Stack: "ng-web"},
	}
	if got := plan.Pulls(mustResolve(t, cat, sel), sel); !reflect.DeepEqual(got, want) {
		t.Fatalf("pulls = %+v, want %+v", got, want)
	}
}

// The order of the typed modules never changes the plan order (module order
// comes from the catalog and the requirement edges, not from the selection).
func TestSelectionOrderDoesNotChangeTheModuleOrder(t *testing.T) {
	cat := withCooperation(t, nil)
	backendModule(t, &cat, "tenancy").RequiresModules = []catalog.Requirement{{Module: "identity", Range: "^0.1.0"}}
	first := mustResolve(t, cat, selection("tenancy", "parameters"))
	second := mustResolve(t, cat, selection("parameters", "tenancy"))
	wantIDs(t, first.Stacks[0], "core", "identity", "parameters", "tenancy")
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("plans differ:\n%+v\n%+v", first, second)
	}
}

// paths lists the paths of files.
func paths(files []catalog.File) []string {
	out := make([]string, len(files))
	for i, file := range files {
		out[i] = file.Path
	}
	return out
}

func specFiles(t *testing.T, p plan.Plan, spec string) []string {
	t.Helper()
	for _, entry := range p.Specs {
		if entry.Spec == spec {
			return paths(entry.Files)
		}
	}
	t.Fatalf("spec %s is not in the plan", spec)
	return nil
}

func TestCooperationFilesShipOnlyWithTheirTag(t *testing.T) {
	const (
		always   = "specs/parameters/tck/validation.spec.ts"
		gated    = "specs/parameters/tck/with-tenancy/precedence.spec.ts"
		otherTag = "specs/parameters/tck/with-feature-flags/other.spec.ts"
	)
	cases := []struct {
		name    string
		modules []string
		want    []string
	}{
		{"tag not installed", []string{"parameters"}, []string{always}},
		{"tag installed", []string{"parameters", "tenancy"}, []string{always, gated}},
		{"another cooperating tag installed", []string{"parameters", "feature-flags"}, []string{always, otherTag}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cat := withCooperation(t, map[string][]string{"parameters": {"@feature-flags", "@tenancy"}})
			paramsIndex := -1
			for i := range cat.Specs {
				if cat.Specs[i].Spec != "parameters" {
					continue
				}
				cat.Specs[i].Files = []catalog.File{{Path: always}, {Path: gated}, {Path: otherTag}}
				paramsIndex = i
			}
			if paramsIndex < 0 {
				t.Fatalf("parameters spec is not in the catalog")
			}
			before := append([]catalog.File(nil), cat.Specs[paramsIndex].Files...)
			p := mustResolve(t, cat, selection(tc.modules...))
			if got := specFiles(t, p, "parameters"); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parameters files = %v, want %v", got, tc.want)
			}
			if !reflect.DeepEqual(cat.Specs[paramsIndex].Files, before) {
				t.Fatalf("Resolve changed the catalog it was given")
			}
		})
	}
}

func TestComponentsAreOnePerSelectedStackNamedAfterTheProject(t *testing.T) {
	cat := load(t, "fullstack.json")
	both := selection()
	both.Stacks = []string{"ng-web", "go-srv"}
	p := mustResolve(t, cat, both)
	want := []plan.Component{{Stack: "go-srv", Name: "acme-backend-srv"}, {Stack: "ng-web", Name: "acme-backend-web"}}
	if !reflect.DeepEqual(p.Components, want) {
		t.Fatalf("components = %+v", p.Components)
	}
	if component, ok := p.Component("ng-web"); !ok || component.Name != "acme-backend-web" {
		t.Errorf("Component(ng-web) = %+v %v", component, ok)
	}
	backendOnly := mustResolve(t, cat, selection())
	if _, ok := backendOnly.Component("ng-web"); ok {
		t.Error("an unselected stack has no component")
	}
	several := plan.Plan{Components: []plan.Component{{Stack: "ng-web", Name: "a-web"}, {Stack: "ng-web", Name: "b-web"}}}
	if _, ok := several.Component("ng-web"); ok {
		t.Error("a stack with several components has no single component")
	}
}
