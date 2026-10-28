package versions_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/gates/internal/versions"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{
		"-C", dir,
		"-c", "user.name=Example Tester",
		"-c", "user.email=tester@example.com",
		"-c", "commit.gpgsign=false",
	}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeAt(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	err := os.MkdirAll(filepath.Dir(full), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(full, []byte(content), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, dir, message string) {
	t.Helper()
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "--allow-empty", "-m", message)
}

func requireGit(t *testing.T) {
	t.Helper()
	_, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH")
	}
}

func specManifest(version string) string {
	return `{"schema":1,"spec":"alpha","version":"` + version + `","tckTags":["@core"]}`
}

func moduleManifest(module, version, files, exclude string) string {
	return `{"schema":1,"stack":"go-srv","module":"` + module + `","version":"` + version +
		`","files":` + files + `,"exclude":` + exclude + `}`
}

const (
	coreFiles   = `["go-srv/", "mise.toml"]`
	coreExclude = `["go-srv/AGENTS.md", "go-srv/cookbooks/"]`
)

// baseRepo builds main with spec alpha 0.1.0, module core 0.1.0 (owning go-srv/
// minus docs) and module parameters 0.1.0 (owning its own folder), then
// switches to branch topic.
func baseRepo(t *testing.T) string {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	writeAt(t, dir, "specs/alpha/spec.manifest.json", specManifest("0.1.0"))
	writeAt(t, dir, "specs/alpha/openapi.yaml", "openapi: 3.0.3\n")
	writeAt(t, dir, "go-srv/internal/platform/core/module.manifest.json", moduleManifest("core", "0.1.0", coreFiles, coreExclude))
	writeAt(t, dir, "go-srv/internal/platform/parameters/module.manifest.json",
		moduleManifest("parameters", "0.1.0", `["go-srv/internal/platform/parameters/"]`, `[]`))
	writeAt(t, dir, "go-srv/internal/platform/parameters/params.go", "package parameters\n")
	writeAt(t, dir, "go-srv/cmd/server/modules.go", "package main\n")
	writeAt(t, dir, "go-srv/main.go", "package main\n")
	writeAt(t, dir, "go-srv/AGENTS.md", "docs\n")
	writeAt(t, dir, "go-srv/cookbooks/a.md", "docs\n")
	writeAt(t, dir, "mise.toml", "x\n")
	commitAll(t, dir, "base")
	runGit(t, dir, "switch", "-c", "topic")
	return dir
}

func rules(vs []string) string {
	return strings.Join(vs, ",")
}

func check(t *testing.T, dir string) (rulesFound []string, paths []string) {
	t.Helper()
	return checkWith(t, dir, false)
}

func checkWith(t *testing.T, dir string, preRelease bool) (rulesFound []string, paths []string) {
	t.Helper()
	violations, err := versions.CheckBumps(context.Background(), dir, "main", preRelease)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		rulesFound = append(rulesFound, v.Rule)
		paths = append(paths, v.Path)
		if strings.Contains(v.Path, `\`) || strings.Contains(v.Message, `\`) {
			t.Errorf("backslash in %+v", v)
		}
	}
	return rulesFound, paths
}

func TestChangeWithoutBumpFails(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "specs/alpha/openapi.yaml", "openapi: 3.0.3\ninfo: {}\n")
	commitAll(t, dir, "change")
	found, paths := check(t, dir)
	if rules(found) != versions.RuleVersionNotBumped || paths[0] != "specs/alpha/spec.manifest.json" {
		t.Fatalf("got %v %v", found, paths)
	}
}

func TestChangeWithBumpPasses(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "specs/alpha/openapi.yaml", "openapi: 3.0.3\ninfo: {}\n")
	writeAt(t, dir, "specs/alpha/spec.manifest.json", specManifest("0.10.0"))
	commitAll(t, dir, "change")
	found, _ := check(t, dir)
	if len(found) != 0 {
		t.Fatalf("got %v", found)
	}
}

func TestUnchangedComponentNeedsNoBump(t *testing.T) {
	dir := baseRepo(t)
	commitAll(t, dir, "empty")
	found, _ := check(t, dir)
	if len(found) != 0 {
		t.Fatalf("got %v", found)
	}
}

func TestVersionDecreasedFails(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "specs/alpha/spec.manifest.json", specManifest("0.0.9"))
	commitAll(t, dir, "downgrade")
	found, _ := check(t, dir)
	if rules(found) != versions.RuleVersionDecreased {
		t.Fatalf("got %v", found)
	}
}

func TestInvalidVersionsFail(t *testing.T) {
	for _, bad := range []string{"01.0.0", "1.0", "v1.0.0", "1.0.0-rc1", "", "99999999999999999999.0.0"} {
		dir := baseRepo(t)
		writeAt(t, dir, "specs/alpha/spec.manifest.json", specManifest(bad))
		commitAll(t, dir, "bad")
		found, _ := check(t, dir)
		if rules(found) != versions.RuleVersionInvalid {
			t.Errorf("version %q: got %v", bad, found)
		}
	}
}

func TestNewComponentStartsWithoutComparison(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "go-srv/internal/platform/flags/module.manifest.json",
		moduleManifest("flags", "0.1.0", `["go-srv/internal/platform/flags/"]`, `[]`))
	writeAt(t, dir, "go-srv/internal/platform/core/module.manifest.json", moduleManifest("core", "0.1.1", coreFiles, coreExclude))
	commitAll(t, dir, "new module")
	found, _ := check(t, dir)
	if len(found) != 0 {
		t.Fatalf("got %v", found)
	}
}

func TestNestedModuleFilesGoToTheirLongestPrefixOwner(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "go-srv/internal/platform/parameters/params.go", "package parameters // changed\n")
	commitAll(t, dir, "change nested")
	found, paths := check(t, dir)
	if rules(found) != versions.RuleVersionNotBumped || !strings.Contains(paths[0], "/parameters/") {
		t.Fatalf("only parameters should need a bump, got %v %v", found, paths)
	}
}

func TestCoreChangeOutsideNestedModuleBumpsCoreOnly(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "go-srv/main.go", "package main // changed\n")
	commitAll(t, dir, "change core")
	found, paths := check(t, dir)
	if rules(found) != versions.RuleVersionNotBumped || !strings.Contains(paths[0], "/core/") {
		t.Fatalf("got %v %v", found, paths)
	}
}

func TestCompositionRootAndExcludedFilesBelongToNoComponent(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "go-srv/cmd/server/modules.go", "package main // changed\n")
	writeAt(t, dir, "go-srv/AGENTS.md", "docs changed\n")
	writeAt(t, dir, "go-srv/cookbooks/a.md", "docs changed\n")
	writeAt(t, dir, "unowned.txt", "x\n")
	commitAll(t, dir, "docs only")
	found, _ := check(t, dir)
	if len(found) != 0 {
		t.Fatalf("got %v", found)
	}
}

func TestDeletedFileCountsAsChange(t *testing.T) {
	dir := baseRepo(t)
	runGit(t, dir, "rm", "-q", "mise.toml")
	commitAll(t, dir, "delete")
	found, paths := check(t, dir)
	if rules(found) != versions.RuleVersionNotBumped || !strings.Contains(paths[0], "/core/") {
		t.Fatalf("got %v %v", found, paths)
	}
}

func TestExactFileEntryBeatsDirectory(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "go-srv/internal/platform/other/module.manifest.json",
		moduleManifest("other", "0.1.0", `["go-srv/main.go"]`, `[]`))
	commitAll(t, dir, "other module")
	runGit(t, dir, "branch", "-f", "main", "topic")
	writeAt(t, dir, "go-srv/main.go", "package main // changed\n")
	commitAll(t, dir, "change")
	found, paths := check(t, dir)
	if rules(found) != versions.RuleVersionNotBumped || !strings.Contains(paths[0], "/other/") {
		t.Fatalf("got %v %v", found, paths)
	}
}

func TestPathTargetFilesEntriesAreRead(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "go-srv/internal/platform/other/module.manifest.json",
		moduleManifest("other", "0.1.0", `[{"path":"go-srv/main.go","target":"app/main.go"}]`, `[]`))
	commitAll(t, dir, "other module")
	components, violations, err := versions.Discover(dir)
	if err != nil || len(violations) != 0 {
		t.Fatalf("err = %v, violations = %v", err, violations)
	}
	var paths []string
	for _, c := range components {
		if c.Name == "go-srv-other" {
			paths = c.Paths
		}
	}
	if len(paths) != 1 || paths[0] != "go-srv/main.go" {
		t.Fatalf("paths = %v", paths)
	}
}

func TestOverlapOfEqualEntriesIsReported(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "go-srv/internal/platform/other/module.manifest.json",
		moduleManifest("other", "0.1.0", `["mise.toml"]`, `[]`))
	writeAt(t, dir, "go-srv/internal/platform/core/module.manifest.json", moduleManifest("core", "0.1.1", coreFiles, coreExclude))
	commitAll(t, dir, "overlap")
	found, _ := check(t, dir)
	if rules(found) != versions.RuleComponentPathOverlap {
		t.Fatalf("got %v", found)
	}
}

func TestModuleEntryInsideSpecFolderIsAnOverlap(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "go-srv/internal/platform/other/module.manifest.json",
		moduleManifest("other", "0.1.0", `["specs/alpha/openapi.yaml"]`, `[]`))
	writeAt(t, dir, "go-srv/internal/platform/core/module.manifest.json", moduleManifest("core", "0.1.1", coreFiles, coreExclude))
	commitAll(t, dir, "overlap")
	found, _ := check(t, dir)
	if rules(found) != versions.RuleComponentPathOverlap {
		t.Fatalf("got %v", found)
	}
}

func TestDiscoverReportsUnsafeAndBrokenManifests(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	writeAt(t, dir, "specs/alpha/spec.manifest.json", "{not json")
	writeAt(t, dir, "specs/Bad_Name/spec.manifest.json", specManifest("0.1.0"))
	writeAt(t, dir, "go-srv/internal/platform/x/module.manifest.json",
		moduleManifest("a b", "0.1.0", `[]`, `[]`))
	writeAt(t, dir, "go-srv/internal/platform/y/module.manifest.json",
		moduleManifest("y", "0.1.0", `["../outside", "/abs", "a//b", 5]`, `[]`))
	components, violations, err := versions.Discover(dir)
	if err != nil {
		t.Fatalf("Discover error: %v", err)
	}
	if len(violations) != 4 {
		t.Fatalf("violations = %v", violations)
	}
	var rejectedPaths []string
	for _, v := range violations {
		rejectedPaths = append(rejectedPaths, v.Path)
	}
	sort.Strings(rejectedPaths)
	wantPaths := []string{
		"go-srv/internal/platform/x/module.manifest.json",
		"go-srv/internal/platform/y/module.manifest.json",
		"specs/Bad_Name/spec.manifest.json",
		"specs/alpha/spec.manifest.json",
	}
	sort.Strings(wantPaths)
	if !slices.Equal(rejectedPaths, wantPaths) {
		t.Fatalf("violation paths = %v, want %v", rejectedPaths, wantPaths)
	}
	for _, v := range violations {
		if v.Rule != versions.RuleVersionInvalid {
			t.Errorf("unexpected rule %+v", v)
		}
	}
	for _, c := range components {
		if strings.ContainsAny(c.Name, " _") {
			t.Errorf("unsafe component name %q", c.Name)
		}
	}
}

func TestDiscoverRejectsUnsafePathEntriesIndividually(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	writeAt(t, dir, "go-srv/internal/platform/y/module.manifest.json",
		moduleManifest("y", "0.1.0", `["../outside", "/abs", "a//b", "ok.go"]`, `["../x", "C:/y"]`))
	components, violations, err := versions.Discover(dir)
	if err != nil {
		t.Fatalf("Discover error: %v", err)
	}
	if len(violations) != 5 {
		t.Fatalf("violations = %v", violations)
	}
	if len(components) != 1 || !slices.Equal(components[0].Paths, []string{"ok.go"}) || len(components[0].Exclude) != 0 {
		t.Fatalf("components = %+v", components)
	}
}

func TestFolderEntryWithoutTrailingSlashIsInvalid(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	writeAt(t, dir, "go-srv/cookbooks/a.md", "docs\n")
	writeAt(t, dir, "go-srv/internal/platform/y/module.manifest.json",
		moduleManifest("y", "0.1.0", `["go-srv/cookbooks", "go-srv/cookbooks/a.md"]`, `[]`))
	_, violations, err := versions.Discover(dir)
	if err != nil {
		t.Fatalf("Discover error: %v", err)
	}
	if len(violations) != 1 || violations[0].Rule != versions.RuleVersionInvalid {
		t.Fatalf("violations = %v", violations)
	}
}

func TestMissingTagsListsUntaggedVersionsSorted(t *testing.T) {
	dir := baseRepo(t)
	tags, err := versions.MissingTags(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := "go-srv-core-v0.1.0,go-srv-parameters-v0.1.0,spec-alpha-v0.1.0"
	if rules(tags) != want {
		t.Fatalf("got %v", tags)
	}
	runGit(t, dir, "tag", "spec-alpha-v0.1.0")
	runGit(t, dir, "tag", "go-srv-core-v0.1.0")
	tags, err = versions.MissingTags(context.Background(), dir)
	if err != nil || rules(tags) != "go-srv-parameters-v0.1.0" {
		t.Fatalf("got %v, %v", tags, err)
	}
	runGit(t, dir, "tag", "go-srv-parameters-v0.1.0")
	tags, err = versions.MissingTags(context.Background(), dir)
	if err != nil || len(tags) != 0 {
		t.Fatalf("got %v, %v", tags, err)
	}
}

func TestMissingTagsSkipsInvalidVersions(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "specs/alpha/spec.manifest.json", specManifest("1.0"))
	tags, err := versions.MissingTags(context.Background(), dir)
	want := "go-srv-core-v0.1.0,go-srv-parameters-v0.1.0"
	if err != nil || rules(tags) != want {
		t.Fatalf("got %v, %v, want %s", tags, err, want)
	}
}

func TestMissingTagsOutsideRepositoryIsAnError(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	writeAt(t, dir, "specs/alpha/spec.manifest.json", specManifest("0.1.0"))
	_, err := versions.MissingTags(context.Background(), dir)
	if err == nil {
		t.Fatal("want an error")
	}
}

func TestCheckBumpsUnknownBaseIsAnError(t *testing.T) {
	dir := baseRepo(t)
	_, err := versions.CheckBumps(context.Background(), dir, "no-such-rev", false)
	if err == nil {
		t.Fatal("want an error")
	}
}

func TestCancelledContextStopsWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := versions.MissingTags(ctx, t.TempDir())
	if err == nil {
		t.Fatal("want an error")
	}
	_, err = versions.CheckBumps(ctx, t.TempDir(), "main", false)
	if err == nil {
		t.Fatal("want an error")
	}
}

func TestDuplicateComponentNameKeepsFirstAndReportsSecond(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	writeAt(t, dir, "go-srv/internal/platform/a/module.manifest.json",
		moduleManifest("dup", "0.1.0", `["a.go"]`, `[]`))
	writeAt(t, dir, "go-srv/internal/platform/b/module.manifest.json",
		moduleManifest("dup", "0.1.0", `["b.go"]`, `[]`))
	components, violations, err := versions.Discover(dir)
	if err != nil {
		t.Fatalf("Discover error: %v", err)
	}
	if len(components) != 1 || !slices.Equal(components[0].Paths, []string{"a.go"}) {
		t.Fatalf("components = %+v", components)
	}
	if len(violations) != 1 || violations[0].Rule != versions.RuleVersionInvalid ||
		violations[0].Path != "go-srv/internal/platform/b/module.manifest.json" {
		t.Fatalf("violations = %v", violations)
	}
}

func TestManifestEntriesAreNormalizedOrRejected(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	writeAt(t, dir, "go-srv/internal/platform/y/module.manifest.json",
		moduleManifest("y", "0.1.0", `["dir\\file.go", "", ".", "a/./b", "ok.go"]`, `["x\\y/", ""]`))
	components, violations, err := versions.Discover(dir)
	if err != nil {
		t.Fatalf("Discover error: %v", err)
	}
	if len(violations) != 4 {
		t.Fatalf("violations = %v", violations)
	}
	if len(components) != 1 ||
		!slices.Equal(components[0].Paths, []string{"dir/file.go", "ok.go"}) ||
		!slices.Equal(components[0].Exclude, []string{"x/y/"}) {
		t.Fatalf("components = %+v", components)
	}
}

func TestRootLevelFolderEntryWithoutSlashIsInvalid(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "go-srv/internal/platform/other/module.manifest.json",
		moduleManifest("other", "0.1.0", `["go-srv"]`, `[]`))
	commitAll(t, dir, "root folder entry")
	found, _ := check(t, dir)
	if !slices.Contains(found, versions.RuleVersionInvalid) {
		t.Fatalf("got %v", found)
	}
	_, violations, err := versions.Discover(dir)
	if err != nil || len(violations) != 1 || violations[0].Rule != versions.RuleVersionInvalid {
		t.Fatalf("violations = %v, err = %v", violations, err)
	}
}

func TestNgWebModulesAreDiscoveredAndBumpChecked(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "ng-web/src/app/platform/widget/module.manifest.json",
		`{"schema":1,"stack":"ng-web","module":"widget","version":"0.1.0","files":["ng-web/src/app/platform/widget/"],"exclude":[]}`)
	writeAt(t, dir, "ng-web/src/app/platform/widget/widget.ts", "export {}\n")
	commitAll(t, dir, "ng-web module")
	components, violations, err := versions.Discover(dir)
	if err != nil || len(violations) != 0 {
		t.Fatalf("err = %v, violations = %v", err, violations)
	}
	var names []string
	for _, c := range components {
		names = append(names, c.Name)
	}
	if !slices.Contains(names, "ng-web-widget") {
		t.Fatalf("names = %v", names)
	}
	runGit(t, dir, "branch", "-f", "main", "topic")
	writeAt(t, dir, "ng-web/src/app/platform/widget/widget.ts", "export const a = 1\n")
	commitAll(t, dir, "change")
	found, paths := check(t, dir)
	if rules(found) != versions.RuleVersionNotBumped || !strings.Contains(paths[0], "/widget/") {
		t.Fatalf("got %v %v", found, paths)
	}
}

func TestCheckBumpsComparesAgainstTheMergeBaseNotTheMainTip(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "specs/alpha/openapi.yaml", "openapi: 3.0.3\ninfo: {}\n")
	writeAt(t, dir, "specs/alpha/spec.manifest.json", specManifest("0.2.0"))
	commitAll(t, dir, "topic bump")
	runGit(t, dir, "switch", "main")
	writeAt(t, dir, "specs/alpha/openapi.yaml", "openapi: 3.0.3\ninfo: {title: x}\n")
	writeAt(t, dir, "specs/alpha/spec.manifest.json", specManifest("0.3.0"))
	commitAll(t, dir, "main advanced")
	runGit(t, dir, "switch", "topic")
	found, _ := check(t, dir)
	if len(found) != 0 {
		t.Fatalf("got %v", found)
	}
}

func TestPreReleaseChangeKeptAtInitialVersionPasses(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "specs/alpha/openapi.yaml", "openapi: 3.0.3\ninfo: {}\n")
	commitAll(t, dir, "change")
	found, _ := checkWith(t, dir, true)
	if len(found) != 0 {
		t.Fatalf("got %v", found)
	}
}

func TestPreReleaseResetFromHigherVersionPasses(t *testing.T) {
	dir := baseRepo(t)
	runGit(t, dir, "switch", "main")
	writeAt(t, dir, "specs/alpha/spec.manifest.json", specManifest("1.1.1"))
	commitAll(t, dir, "main at 1.1.1")
	runGit(t, dir, "switch", "topic")
	runGit(t, dir, "merge", "--no-edit", "main")
	writeAt(t, dir, "specs/alpha/spec.manifest.json", specManifest("0.1.0"))
	writeAt(t, dir, "specs/alpha/openapi.yaml", "openapi: 3.0.3\ninfo: {}\n")
	commitAll(t, dir, "reset")
	found, _ := checkWith(t, dir, true)
	if len(found) != 0 {
		t.Fatalf("got %v", found)
	}
}

func TestPreReleaseVersionOtherThanInitialFailsOnItsManifest(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "specs/alpha/openapi.yaml", "openapi: 3.0.3\ninfo: {}\n")
	writeAt(t, dir, "specs/alpha/spec.manifest.json", specManifest("0.2.0"))
	commitAll(t, dir, "bump")
	found, paths := checkWith(t, dir, true)
	if rules(found) != versions.RuleVersionNotPrerelease || paths[0] != "specs/alpha/spec.manifest.json" {
		t.Fatalf("got %v %v", found, paths)
	}
}

func TestPreReleaseNewComponentAboveInitialVersionFails(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "go-srv/internal/platform/flags/module.manifest.json",
		moduleManifest("flags", "0.2.0", `["go-srv/internal/platform/flags/"]`, `[]`))
	commitAll(t, dir, "new module")
	found, paths := checkWith(t, dir, true)
	if rules(found) != versions.RuleVersionNotPrerelease || paths[0] != "go-srv/internal/platform/flags/module.manifest.json" {
		t.Fatalf("got %v %v", found, paths)
	}
}

func TestPreReleaseUnparseableVersionIsOnlyInvalid(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "specs/alpha/spec.manifest.json", specManifest("1.0"))
	commitAll(t, dir, "bad")
	found, _ := checkWith(t, dir, true)
	if rules(found) != versions.RuleVersionInvalid {
		t.Fatalf("got %v", found)
	}
}

func TestPreReleaseKeepsPathOverlapRule(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "go-srv/internal/platform/other/module.manifest.json",
		moduleManifest("other", "0.1.0", `["mise.toml"]`, `[]`))
	commitAll(t, dir, "overlap")
	found, _ := checkWith(t, dir, true)
	if rules(found) != versions.RuleComponentPathOverlap {
		t.Fatalf("got %v", found)
	}
}

func TestPreReleaseOffComponentAboveInitialVersionPasses(t *testing.T) {
	dir := baseRepo(t)
	runGit(t, dir, "switch", "main")
	writeAt(t, dir, "specs/alpha/spec.manifest.json", specManifest("1.1.1"))
	commitAll(t, dir, "main at 1.1.1")
	runGit(t, dir, "switch", "topic")
	runGit(t, dir, "merge", "--no-edit", "main")
	commitAll(t, dir, "empty")
	found, _ := checkWith(t, dir, false)
	if len(found) != 0 {
		t.Fatalf("got %v", found)
	}
}

func TestPreReleaseViolationCarriesRefL76(t *testing.T) {
	dir := baseRepo(t)
	writeAt(t, dir, "specs/alpha/spec.manifest.json", specManifest("0.2.0"))
	commitAll(t, dir, "bump")
	violations, err := versions.CheckBumps(context.Background(), dir, "main", true)
	if err != nil || len(violations) != 1 || violations[0].Ref != "L76" {
		t.Fatalf("got %+v %v", violations, err)
	}
}

func tagContent(t *testing.T, dir string) []string {
	t.Helper()
	violations, err := versions.CheckTagContent(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, v := range violations {
		found = append(found, v.Rule+" "+v.Path)
	}
	return found
}

func TestTagAtMatchingContentPasses(t *testing.T) {
	dir := baseRepo(t)
	runGit(t, dir, "tag", "spec-alpha-v0.1.0")
	runGit(t, dir, "tag", "go-srv-core-v0.1.0")
	// Unrelated changes after the tag do not make it foreign.
	writeAt(t, dir, "go-srv/AGENTS.md", "other docs\n")
	writeAt(t, dir, "go-srv/cmd/server/modules.go", "package main // changed\n")
	writeAt(t, dir, "go-srv/internal/platform/parameters/params.go", "package parameters // changed\n")
	commitAll(t, dir, "unrelated")
	if found := tagContent(t, dir); len(found) != 0 {
		t.Fatalf("got %v", found)
	}
}

func TestTagAtOtherContentFails(t *testing.T) {
	dir := baseRepo(t)
	runGit(t, dir, "tag", "spec-alpha-v0.1.0")
	runGit(t, dir, "tag", "go-srv-core-v0.1.0")
	runGit(t, dir, "tag", "go-srv-parameters-v0.1.0")
	writeAt(t, dir, "specs/alpha/openapi.yaml", "openapi: 3.1.0\n")
	writeAt(t, dir, "go-srv/main.go", "package main // changed\n")
	commitAll(t, dir, "change")
	want := "TAG_CONTENT_MISMATCH go-srv/internal/platform/core/module.manifest.json," +
		"TAG_CONTENT_MISMATCH specs/alpha/spec.manifest.json"
	found := tagContent(t, dir)
	if strings.Join(found, ",") != want {
		t.Fatalf("got %v", found)
	}
}

func TestTagOnDeletedOrAddedComponentFileFails(t *testing.T) {
	dir := baseRepo(t)
	runGit(t, dir, "tag", "go-srv-parameters-v0.1.0")
	writeAt(t, dir, "go-srv/internal/platform/parameters/extra.go", "package parameters\n")
	commitAll(t, dir, "add file")
	found := tagContent(t, dir)
	if len(found) != 1 || !strings.HasPrefix(found[0], "TAG_CONTENT_MISMATCH ") {
		t.Fatalf("added file: got %v", found)
	}
	runGit(t, dir, "reset", "--hard", "HEAD~1")
	runGit(t, dir, "rm", "-q", "go-srv/internal/platform/parameters/params.go")
	runGit(t, dir, "commit", "-m", "delete file")
	found = tagContent(t, dir)
	if len(found) != 1 || !strings.HasPrefix(found[0], "TAG_CONTENT_MISMATCH ") {
		t.Fatalf("deleted file: got %v", found)
	}
}

func TestTagOfAnotherVersionOrNoTagIsNotChecked(t *testing.T) {
	dir := baseRepo(t)
	runGit(t, dir, "tag", "spec-alpha-v1.0.0")
	runGit(t, dir, "tag", "spec-alpha-v0.1.0")
	writeAt(t, dir, "specs/alpha/openapi.yaml", "openapi: 3.1.0\n")
	writeAt(t, dir, "specs/alpha/spec.manifest.json", specManifest("0.2.0"))
	commitAll(t, dir, "release 0.2.0")
	if found := tagContent(t, dir); len(found) != 0 {
		t.Fatalf("got %v", found)
	}
}

func TestAnnotatedTagAtOtherContentFails(t *testing.T) {
	dir := baseRepo(t)
	runGit(t, dir, "tag", "-a", "-m", "release", "spec-alpha-v0.1.0")
	writeAt(t, dir, "specs/alpha/openapi.yaml", "openapi: 3.1.0\n")
	commitAll(t, dir, "change")
	found := tagContent(t, dir)
	want := "TAG_CONTENT_MISMATCH specs/alpha/spec.manifest.json"
	if strings.Join(found, ",") != want {
		t.Fatalf("got %v", found)
	}
}

func TestCheckTagContentOutsideRepositoryIsAnError(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	writeAt(t, dir, "specs/alpha/spec.manifest.json", specManifest("0.1.0"))
	_, err := versions.CheckTagContent(context.Background(), dir)
	if err == nil {
		t.Fatal("want an error")
	}
}
