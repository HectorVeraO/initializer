package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/legs"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

// realCheckoutRoot returns the checkout root that holds tooling/generator.
func realCheckoutRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the test file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
}

// realCatalog builds catalog.lock.json from the real checkout.
func realCatalog(t *testing.T) (root, catalogPath string) {
	t.Helper()
	root = realCheckoutRoot(t)
	listed, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	dir := t.TempDir()
	filesPath := filepath.Join(dir, "files.bin")
	if err := os.WriteFile(filesPath, listed, 0o600); err != nil {
		t.Fatal(err)
	}
	catalogPath = filepath.Join(dir, "catalog.lock.json")
	code, _, stderr := execute(t, "catalog", "build", "--root", root, "--files", filesPath, "--out", catalogPath)
	if code != exitOK {
		t.Fatalf("catalog build exit %d: %s", code, stderr)
	}
	return root, catalogPath
}

// In a project generated with every module, the README sets
// APP_FLAGS_FILE for go-srv started from go-srv/, so the path must
// resolve from there.
func TestGenerateReadmeFlagsFileResolvesFromGoSrv(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, "--out", out,
		"--name", "acme-backend", "--go-module", "example.com/acme/backend", "--stacks", "go-srv", "--all-modules")
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	readme, err := os.ReadFile(filepath.Join(out, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	const key = "APP_FLAGS_FILE="
	at := strings.Index(string(readme), key)
	if at < 0 {
		t.Fatalf("README names no %s", key)
	}
	value := string(readme)[at+len(key):]
	value = value[:strings.IndexAny(value, "`\n ")]
	fromGoSrv := filepath.Join(out, "go-srv", filepath.FromSlash(value))
	if _, err := os.Stat(fromGoSrv); err != nil {
		t.Fatalf("README sets %s%s, which does not exist from the documented go-srv/ working directory: %v", key, value, err)
	}
}

// A project generated without feature-flags ships none of its configuration.
func TestGenerateWithoutFeatureFlagsShipsNoFlagsConfig(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, "--out", out,
		"--name", "acme-backend", "--go-module", "example.com/acme/backend", "--stacks", "go-srv", "--modules", "parameters")
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(out, "flags.yaml")); err == nil {
		t.Fatal("flags.yaml delivered without feature-flags")
	}
	survivors := map[string][]string{
		".deploy/local/.example.env": {"INITIALIZER_PG_PASSWORD=", "OTEL_SERVICE_NAME=", "APP_SESSION_KEYS=", "APP_KRATOS_ADMIN_URL="},
		".deploy/local/README.md":    {"## Start and stop", "## Session keys"},
	}
	for p, wanted := range survivors {
		data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		for _, survivor := range wanted {
			if !strings.Contains(string(data), survivor) {
				t.Errorf("%s lost unrelated core content %q", p, survivor)
			}
		}
		for _, banned := range []string{"APP_FLAGS_FILE", "feature-flags", "Feature flags"} {
			if strings.Contains(string(data), banned) {
				t.Errorf("%s mentions %q in a project without the feature-flags module", p, banned)
			}
		}
	}
}

// A project with feature-flags keeps its configuration lines and sections.
// The repository copy also carries the blocks of the other modules, so the
// byte-for-byte comparison lives in the test below.
func TestGenerateWithFeatureFlagsOnlyKeepsFlagsConfig(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, "--out", out,
		"--name", "acme-backend", "--go-module", "example.com/acme/backend", "--stacks", "go-srv", "--modules", "feature-flags")
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	data, err := os.ReadFile(filepath.Join(out, ".deploy", "local", ".example.env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "APP_FLAGS_FILE") {
		t.Error(".example.env lost the feature-flags configuration")
	}
	if strings.Contains(string(data), "APP_BOOTSTRAP_TENANT_SLUG") {
		t.Error(".example.env carries tenancy configuration without the tenancy module")
	}
}

// A project with feature-flags keeps the configuration, byte for byte. The
// repository copy carries the blocks of every module that has one, so the
// project selects tenancy too.
func TestGenerateWithFeatureFlagsAndTenancyKeepsConfigByteForByte(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, "--out", out,
		"--name", "acme-backend", "--go-module", "example.com/acme/backend", "--stacks", "go-srv", "--modules", "feature-flags,tenancy")
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	for _, p := range []string{".deploy/local/.example.env", ".deploy/local/README.md"} {
		want, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s differs from the repository copy", p)
		}
	}
}

// A core-only project ships nothing of identity.
func TestGenerateCoreOnlyShipsNoIdentity(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	selection := writeSelection(t, `{"schema":1,"project":{"name":"acme-backend","goModule":"example.com/acme/backend"},"stacks":["go-srv"],"modules":[]}`)
	code, _, stderr := executeLogged(t, "generate", "--root", root, "--catalog", catalogPath, "--out", out, "--selection", selection)
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	for _, absent := range []string{"go-srv/internal/platform/identity", ".deploy/local/kratos", ".deploy/local/postgres-init", "specs/tck-support/actors"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(absent))); err == nil {
			t.Errorf("core-only project ships %s", absent)
		}
	}
	for _, p := range []string{".deploy/local/docker-compose.yml", ".deploy/local/.example.env", ".deploy/local/README.md"} {
		data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"KRATOS_PG_PASSWORD", "APP_SESSION_KEYS", "kratos", "postgres-init"} {
			if strings.Contains(string(data), banned) {
				t.Errorf("%s mentions %q in a core-only project", p, banned)
			}
		}
	}
	if strings.Contains(stderr, "module_required") {
		t.Errorf("core alone pulls nothing: %s", stderr)
	}
}

// moduleRequiredEvents returns the module_required records on stderr.
func moduleRequiredEvents(t *testing.T, stderr string) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		if record["msg"] == "module_required" {
			events = append(events, record)
		}
	}
	return events
}

func TestGenerateLogsEachPulledModuleOnStderrOnly(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, stdout, stderr := executeLogged(t, "generate", "--root", root, "--catalog", catalogPath, "--out", out,
		"--name", "acme-backend", "--go-module", "example.com/acme/backend", "--stacks", "go-srv", "--modules", "parameters")
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	events := moduleRequiredEvents(t, stderr)
	if len(events) != 1 {
		t.Fatalf("want one module_required event, got %v", events)
	}
	event := events[0]
	if event["level"] != "INFO" || event["module"] != "identity" || event["required_by"] != "parameters" || event["stack"] != "go-srv" {
		t.Fatalf("event = %v", event)
	}
	if strings.Contains(stdout, "module_required") {
		t.Fatalf("stdout carries the event: %s", stdout)
	}
	files := tree(t, out)
	for p, content := range files {
		if strings.Contains(content, "module_required") {
			t.Errorf("%s carries the event", p)
		}
	}
}

func TestGenerateLogsNothingWhenTheSelectionNamesTheRequirement(t *testing.T) {
	root, catalogPath := realCatalog(t)
	for name, modules := range map[string]string{"named": "identity,parameters", "all": ""} {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			args := []string{"generate", "--root", root, "--catalog", catalogPath, "--out", out,
				"--name", "acme-backend", "--go-module", "example.com/acme/backend", "--stacks", "go-srv"}
			if modules == "" {
				args = append(args, "--all-modules")
			} else {
				args = append(args, "--modules", modules)
			}
			code, _, stderr := executeLogged(t, args...)
			if code != exitOK {
				t.Fatalf("generate exit %d: %s", code, stderr)
			}
			if events := moduleRequiredEvents(t, stderr); len(events) != 0 {
				t.Fatalf("unexpected events %v", events)
			}
		})
	}
}

// A refused selection announces no pull: the project is never generated.
func TestGenerateRefusedSelectionLogsNoPull(t *testing.T) {
	root, catalogPath := realCatalog(t)
	busy := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(busy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(busy, "existing.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]string{
		"unknown module":   {"--out", filepath.Join(t.TempDir(), "out"), "--modules", "parameters,no-such-module"},
		"non-empty output": {"--out", busy, "--modules", "parameters"},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			args := append([]string{"generate", "--root", root, "--catalog", catalogPath,
				"--name", "acme-backend", "--go-module", "example.com/acme/backend", "--stacks", "go-srv"}, extra...)
			code, _, stderr := executeLogged(t, args...)
			if code != exitIssues {
				t.Fatalf("generate exit %d, want %d: %s", code, exitIssues, stderr)
			}
			if events := moduleRequiredEvents(t, stderr); len(events) != 0 {
				t.Fatalf("refused selection logged %v", events)
			}
		})
	}
}

func TestGenerateWithTwoRequirersListsBothSorted(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := executeLogged(t, "generate", "--root", root, "--catalog", catalogPath, "--out", out,
		"--name", "acme-backend", "--go-module", "example.com/acme/backend", "--stacks", "go-srv", "--modules", "parameters,feature-flags")
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	events := moduleRequiredEvents(t, stderr)
	if len(events) != 1 || events[0]["required_by"] != "feature-flags,parameters" {
		t.Fatalf("events = %v", events)
	}
}

func decodeRealCatalog(t *testing.T, path string) catalog.Catalog {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	built, issues := catalog.Decode(data)
	if len(issues) > 0 {
		t.Fatalf("decode catalog: %v", issues)
	}
	return built
}

// The CI selection files are exactly the covering set of the catalog.
func TestCISelectionsAreTheCoveringSet(t *testing.T) {
	root, catalogPath := realCatalog(t)
	dir := filepath.Join(root, "tooling", "generator", "testdata", "ci")
	names, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no selection files under %s: %v", dir, err)
	}
	files := map[string]manifest.Selection{}
	for _, path := range names {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		selection, issues := manifest.DecodeSelection(filepath.Base(path), data)
		if len(issues) > 0 {
			t.Fatalf("%s: %v", path, issues)
		}
		files[filepath.Base(path)] = selection
	}
	for _, found := range legs.Check(decodeRealCatalog(t, catalogPath), files) {
		t.Errorf("%s %s: %s", found.Code, found.Path, found.Message)
	}
}

// Identity owns the Kratos deployment files and the shared TCK actors, core none;
// tenancy and api-keys own the actor helper of their own routes.
func TestIdentityOwnsTheKratosAndActorFiles(t *testing.T) {
	root, catalogPath := realCatalog(t)
	built := decodeRealCatalog(t, catalogPath)
	owned := map[string]map[string]bool{}
	for _, stack := range built.Stacks {
		if stack.Stack != "go-srv" {
			continue
		}
		for _, module := range stack.Modules {
			owned[module.Module] = map[string]bool{}
			for _, file := range module.Files {
				owned[module.Module][file.Path] = true
			}
		}
	}
	listed, err := exec.Command("git", "-C", root, "ls-files", "--",
		".deploy/local/kratos/", ".deploy/local/postgres-init/", "specs/tck-support/actors/").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	paths := strings.Fields(string(listed))
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatal("no tracked file under the Kratos and actors folders")
	}
	// An actor helper belongs to the module whose routes it calls.
	helperOwner := map[string]string{
		"specs/tck-support/actors/tenants.ts":  "tenancy",
		"specs/tck-support/actors/api-keys.ts": "api-keys",
	}
	for _, p := range paths {
		want := "identity"
		if module, found := helperOwner[p]; found {
			want = module
		}
		if !owned[want][p] {
			t.Errorf("%s is not owned by go-srv %s", p, want)
		}
		if owned["core"][p] {
			t.Errorf("%s is owned by go-srv core", p)
		}
	}
}
