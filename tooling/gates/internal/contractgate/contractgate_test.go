package contractgate_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/gates/internal/contractgate"
	"github.com/HectorVeraO/initializer/tooling/gates/internal/report"
)

const fakeExitEnv = "CONTRACTGATE_FAKE_OASDIFF_EXIT"

// TestMain lets the test binary stand in for oasdiff: a boundary fake whose
// exit code the test picks through the environment.
func TestMain(m *testing.M) {
	code := os.Getenv(fakeExitEnv)
	if code != "" {
		var exit int
		_, err := fmt.Sscanf(code, "%d", &exit)
		if err != nil {
			os.Exit(99)
		}
		fmt.Fprintln(os.Stderr, "fake oasdiff failure")
		os.Exit(exit)
	}
	os.Exit(m.Run())
}

func specYAML(version, extraProperty, requiredList, extra string) string {
	return fmt.Sprintf(`openapi: 3.0.3
info:
  title: Example
  version: %s
paths:
  /api/v1/examples:
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                required: %s
                properties:
                  id:
                    type: string
                  name:
                    type: string%s
%s`, version, requiredList, extraProperty, extra)
}

const baseSpec = "1.0.0"

func manifestJSON(spec, version string, tags ...string) string {
	quoted := make([]string, 0, len(tags))
	for _, tag := range tags {
		quoted = append(quoted, fmt.Sprintf("%q", tag))
	}
	return fmt.Sprintf(`{"schema":1,"spec":%q,"version":%q,"tckTags":[%s]}`, spec, version, strings.Join(quoted, ","))
}

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

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	err := os.MkdirAll(filepath.Dir(full), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(full, []byte(content), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

func writeSpec(t *testing.T, root, name, openapi, manifest string) {
	t.Helper()
	write(t, root, "specs/"+name+"/openapi.yaml", openapi)
	write(t, root, "specs/"+name+"/spec.manifest.json", manifest)
	write(t, root, "specs/"+name+"/tck/example.spec.ts", "// synthetic\n")
}

func newRepo(t *testing.T) string {
	t.Helper()
	_, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	write(t, dir, "README.txt", "x\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "first")
	return dir
}

func commitAll(t *testing.T, dir, message string) {
	t.Helper()
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", message)
}

func requireOasdiff(t *testing.T) {
	t.Helper()
	_, err := exec.LookPath("oasdiff")
	if err != nil {
		t.Skip("oasdiff is not on PATH; run under mise exec --")
	}
}

func check(t *testing.T, dir string) []report.Violation {
	t.Helper()
	vs, err := contractgate.Check(context.Background(), contractgate.Options{Root: dir, BaseRev: "main"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	for _, v := range vs {
		if strings.Contains(v.Path, `\`) {
			t.Errorf("backslash in path %q", v.Path)
		}
	}
	return vs
}

func ruleSet(vs []report.Violation) []string {
	out := []string{}
	for _, v := range vs {
		out = append(out, v.Rule)
	}
	slices.Sort(out)
	return out
}

func wantRules(t *testing.T, vs []report.Violation, want ...string) {
	t.Helper()
	slices.Sort(want)
	got := ruleSet(vs)
	if !slices.Equal(got, want) {
		t.Fatalf("rules = %v, want %v\n%v", got, want, vs)
	}
}

func goodRepo(t *testing.T) string {
	t.Helper()
	dir := newRepo(t)
	writeSpec(t, dir, "alpha", specYAML(baseSpec, "", "[id, name]", ""), manifestJSON("alpha", baseSpec, "@core"))
	commitAll(t, dir, "base specs")
	return dir
}

func TestNoSpecsDirectoryPasses(t *testing.T) {
	dir := newRepo(t)
	wantRules(t, check(t, dir))
}

func TestIgnoredDirectoriesAreNotSpecs(t *testing.T) {
	dir := newRepo(t)
	for _, name := range []string{"tck-support", "scripts", "node_modules", "test-results", "playwright-report"} {
		write(t, dir, "specs/"+name+"/file.txt", "x\n")
	}
	commitAll(t, dir, "tooling dirs")
	wantRules(t, check(t, dir))
}

func TestValidSpecPasses(t *testing.T) {
	dir := goodRepo(t)
	wantRules(t, check(t, dir))
}

func TestDirectoryWithoutOpenapiIsMissing(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "specs/beta/notes.txt", "x\n")
	commitAll(t, dir, "beta")
	vs := check(t, dir)
	wantRules(t, vs, contractgate.RuleMissing)
	if vs[0].Path != "specs/beta" {
		t.Errorf("path = %q", vs[0].Path)
	}
}

func TestManifestMissing(t *testing.T) {
	dir := newRepo(t)
	writeSpec(t, dir, "alpha", specYAML(baseSpec, "", "[id]", ""), "")
	if err := os.Remove(filepath.Join(dir, "specs", "alpha", "spec.manifest.json")); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "alpha")
	wantRules(t, check(t, dir), contractgate.RuleManifestMissing)
}

func TestManifestSpecDiffersFromFolder(t *testing.T) {
	dir := newRepo(t)
	writeSpec(t, dir, "alpha", specYAML(baseSpec, "", "[id]", ""), manifestJSON("other", baseSpec, "@core"))
	commitAll(t, dir, "alpha")
	wantRules(t, check(t, dir), contractgate.RuleManifestMissing)
}

func TestManifestNotJSON(t *testing.T) {
	dir := newRepo(t)
	writeSpec(t, dir, "alpha", specYAML(baseSpec, "", "[id]", ""), "{not json")
	commitAll(t, dir, "alpha")
	wantRules(t, check(t, dir), contractgate.RuleManifestMissing)
}

func TestTCKMissingWithoutSpecFile(t *testing.T) {
	dir := newRepo(t)
	writeSpec(t, dir, "alpha", specYAML(baseSpec, "", "[id]", ""), manifestJSON("alpha", baseSpec, "@core"))
	if err := os.Remove(filepath.Join(dir, "specs", "alpha", "tck", "example.spec.ts")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "specs/alpha/tck/readme.md", "x\n")
	commitAll(t, dir, "alpha")
	wantRules(t, check(t, dir), contractgate.RuleTCKMissing)
}

func TestTCKMissingWithoutDirectory(t *testing.T) {
	dir := newRepo(t)
	writeSpec(t, dir, "alpha", specYAML(baseSpec, "", "[id]", ""), manifestJSON("alpha", baseSpec, "@core"))
	if err := os.RemoveAll(filepath.Join(dir, "specs", "alpha", "tck")); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "alpha")
	wantRules(t, check(t, dir), contractgate.RuleTCKMissing)
}

func TestTCKSpecInNestedDirectoryCounts(t *testing.T) {
	dir := newRepo(t)
	writeSpec(t, dir, "alpha", specYAML(baseSpec, "", "[id]", ""), manifestJSON("alpha", baseSpec, "@core"))
	if err := os.Remove(filepath.Join(dir, "specs", "alpha", "tck", "example.spec.ts")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "specs/alpha/tck/sub/deep.spec.ts", "// synthetic\n")
	commitAll(t, dir, "alpha")
	wantRules(t, check(t, dir))
}

func TestExternalRefs(t *testing.T) {
	cases := []struct {
		name string
		ref  string
		bad  bool
	}{
		{"file ref", "'./other.yaml#/x'", true},
		{"url ref", "'https://example.com/spec.yaml#/x'", true},
		{"bare hash", "'#'", false},
		{"local pointer", "'#/components/schemas/X'", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newRepo(t)
			extra := "components:\n  schemas:\n    X:\n      $ref: " + tc.ref + "\n"
			writeSpec(t, dir, "alpha", specYAML(baseSpec, "", "[id]", extra), manifestJSON("alpha", baseSpec, "@core"))
			commitAll(t, dir, "alpha")
			vs := check(t, dir)
			if tc.bad {
				wantRules(t, vs, contractgate.RuleExternalRef)
				if vs[0].Line == 0 {
					t.Error("external ref should carry a line")
				}
				return
			}
			wantRules(t, vs)
		})
	}
}

func TestStabilityLevels(t *testing.T) {
	cases := map[string]bool{"draft": true, "alpha": true, "beta": false, "stable": false}
	for level, bad := range cases {
		t.Run(level, func(t *testing.T) {
			dir := newRepo(t)
			extra := "x-stability-level: " + level + "\n"
			writeSpec(t, dir, "alpha", specYAML(baseSpec, "", "[id]", extra), manifestJSON("alpha", baseSpec, "@core"))
			commitAll(t, dir, "alpha")
			vs := check(t, dir)
			if bad {
				wantRules(t, vs, contractgate.RuleStabilityExcluded)
				return
			}
			wantRules(t, vs)
		})
	}
}

func TestVersionDrift(t *testing.T) {
	dir := newRepo(t)
	writeSpec(t, dir, "alpha", specYAML("1.1.0", "", "[id]", ""), manifestJSON("alpha", "1.0.0", "@core"))
	commitAll(t, dir, "alpha")
	wantRules(t, check(t, dir), contractgate.RuleVersionDrift)
}

func TestTCKTags(t *testing.T) {
	cases := []struct {
		name string
		tags []string
		bad  bool
	}{
		{"empty", nil, true},
		{"unknown", []string{"@nope"}, true},
		{"missing at sign", []string{"core"}, true},
		{"two valid", []string{"@core", "@otel"}, false},
		{"same tag twice in one spec", []string{"@core", "@core"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newRepo(t)
			writeSpec(t, dir, "alpha", specYAML(baseSpec, "", "[id]", ""), manifestJSON("alpha", baseSpec, tc.tags...))
			commitAll(t, dir, "alpha")
			vs := check(t, dir)
			if tc.bad {
				wantRules(t, vs, contractgate.RuleTCKTagInvalid)
				return
			}
			wantRules(t, vs)
		})
	}
}

func TestDuplicateTagAcrossSpecsIsReportedOnTheSecond(t *testing.T) {
	dir := newRepo(t)
	writeSpec(t, dir, "alpha", specYAML(baseSpec, "", "[id]", ""), manifestJSON("alpha", baseSpec, "@core"))
	writeSpec(t, dir, "beta", specYAML(baseSpec, "", "[id]", ""), manifestJSON("beta", baseSpec, "@core"))
	commitAll(t, dir, "two specs")
	vs := check(t, dir)
	wantRules(t, vs, contractgate.RuleTCKTagInvalid)
	if vs[0].Path != "specs/beta/spec.manifest.json" {
		t.Errorf("path = %q", vs[0].Path)
	}
}

func TestNonBreakingChangePasses(t *testing.T) {
	requireOasdiff(t)
	dir := goodRepo(t)
	runGit(t, dir, "switch", "-c", "topic")
	optional := "\n                  nickname:\n                    type: string"
	write(t, dir, "specs/alpha/openapi.yaml", specYAML("1.1.0", optional, "[id, name]", ""))
	write(t, dir, "specs/alpha/spec.manifest.json", manifestJSON("alpha", "1.1.0", "@core"))
	commitAll(t, dir, "feature(alpha): Add optional field")
	wantRules(t, check(t, dir))
}

func breakingBranch(t *testing.T, newVersion, message string) string {
	t.Helper()
	return breakingBranchWithManifest(t, newVersion, manifestJSON("alpha", newVersion, "@core"), message)
}

// breakingBranchWithManifest commits a breaking change to alpha on a topic
// branch; manifest is the new spec.manifest.json content, or "" to delete it.
func breakingBranchWithManifest(t *testing.T, newVersion, manifest, message string) string {
	t.Helper()
	requireOasdiff(t)
	dir := goodRepo(t)
	runGit(t, dir, "switch", "-c", "topic")
	// "name" disappears from the response: a breaking change.
	broken := strings.Replace(specYAML(newVersion, "", "[id]", ""), "                  name:\n                    type: string\n", "", 1)
	write(t, dir, "specs/alpha/openapi.yaml", broken)
	if manifest == "" {
		err := os.Remove(filepath.Join(dir, "specs", "alpha", "spec.manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
	} else {
		write(t, dir, "specs/alpha/spec.manifest.json", manifest)
	}
	commitAll(t, dir, message)
	return dir
}

func TestBreakingChangeWithoutMarkerOrMajorFailsBoth(t *testing.T) {
	dir := breakingBranch(t, "1.1.0", "feature(alpha): Drop name")
	vs := check(t, dir)
	wantRules(t, vs, contractgate.RuleBreakingUnmarked, contractgate.RuleBreakingNotMajor)
	for _, v := range vs {
		if !strings.Contains(v.Message, "removed the required property") {
			t.Errorf("message lacks oasdiff output: %q", v.Message)
		}
	}
}

func TestBreakingChangeWithMarkerAndMajorPasses(t *testing.T) {
	dir := breakingBranch(t, "2.0.0", "feature(alpha): Drop name\n\nWhy.\n\nBREAKING CHANGE: name is gone from the response")
	wantRules(t, check(t, dir))
}

func TestBreakingChangeWithMarkerButSameMajorFailsNotMajor(t *testing.T) {
	dir := breakingBranch(t, "1.1.0", "feature(alpha): Drop name\n\nWhy.\n\nBREAKING CHANGE: name is gone")
	wantRules(t, check(t, dir), contractgate.RuleBreakingNotMajor)
}

func TestBreakingChangeWithMajorButNoMarkerFailsUnmarked(t *testing.T) {
	dir := breakingBranch(t, "2.0.0", "feature(alpha): Drop name")
	wantRules(t, check(t, dir), contractgate.RuleBreakingUnmarked)
}

const markedMessage = "feature(alpha): Drop name\n\nWhy.\n\nBREAKING CHANGE: name is gone"

func TestBreakingChangeWithoutAUsableManifestFailsNotMajor(t *testing.T) {
	cases := []struct {
		name     string
		manifest string
		version  string
		want     []string
	}{
		{"manifest deleted", "", "2.0.0", []string{contractgate.RuleManifestMissing, contractgate.RuleBreakingNotMajor}},
		{"manifest not JSON", "{not json", "2.0.0", []string{contractgate.RuleManifestMissing, contractgate.RuleBreakingNotMajor}},
		{"version not MAJOR.MINOR.PATCH", manifestJSON("alpha", "2-beta", "@core"), "2-beta", []string{contractgate.RuleBreakingNotMajor}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := breakingBranchWithManifest(t, tc.version, tc.manifest, markedMessage)
			wantRules(t, check(t, dir), tc.want...)
		})
	}
}

// baseWithoutManifest builds a main branch whose alpha spec has no manifest,
// then a topic branch that adds the manifest with a breaking change.
func baseWithoutManifest(t *testing.T, newVersion string) string {
	t.Helper()
	requireOasdiff(t)
	dir := newRepo(t)
	write(t, dir, "specs/alpha/openapi.yaml", specYAML(baseSpec, "", "[id, name]", ""))
	write(t, dir, "specs/alpha/tck/example.spec.ts", "// synthetic\n")
	commitAll(t, dir, "base without manifest")
	runGit(t, dir, "switch", "-c", "topic")
	broken := strings.Replace(specYAML(newVersion, "", "[id]", ""), "                  name:\n                    type: string\n", "", 1)
	write(t, dir, "specs/alpha/openapi.yaml", broken)
	write(t, dir, "specs/alpha/spec.manifest.json", manifestJSON("alpha", newVersion, "@core"))
	commitAll(t, dir, markedMessage)
	return dir
}

func TestBaseWithoutManifestFallsBackToContractVersion(t *testing.T) {
	wantRules(t, check(t, baseWithoutManifest(t, "1.1.0")), contractgate.RuleBreakingNotMajor)
	wantRules(t, check(t, baseWithoutManifest(t, "2.0.0")))
}

func TestBangTypeIsNotAMarker(t *testing.T) {
	dir := breakingBranch(t, "2.0.0", "feat(alpha)!: Drop name")
	wantRules(t, check(t, dir), contractgate.RuleBreakingUnmarked)
}

func TestMarkerInBodyWithoutBlankLineIsNotATrailer(t *testing.T) {
	dir := breakingBranch(t, "2.0.0", "feature(alpha): Drop name\n\nWhy it happened.\nBREAKING CHANGE: name is gone")
	wantRules(t, check(t, dir), contractgate.RuleBreakingUnmarked)
}

func TestMarkerOnCommitOutsideTheSpecDoesNotCount(t *testing.T) {
	dir := breakingBranch(t, "2.0.0", "feature(alpha): Drop name")
	write(t, dir, "other.txt", "x\n")
	commitAll(t, dir, "feature: Other\n\nWhy.\n\nBREAKING CHANGE: unrelated")
	wantRules(t, check(t, dir), contractgate.RuleBreakingUnmarked)
}

func TestHyphenatedMarkerCounts(t *testing.T) {
	dir := breakingBranch(t, "2.0.0", "feature(alpha): Drop name\n\nWhy.\n\nBREAKING-CHANGE: name is gone")
	wantRules(t, check(t, dir))
}

func TestSpecAbsentAtMergeBaseIsSkipped(t *testing.T) {
	requireOasdiff(t)
	dir := newRepo(t)
	runGit(t, dir, "switch", "-c", "topic")
	writeSpec(t, dir, "alpha", specYAML(baseSpec, "", "[id]", ""), manifestJSON("alpha", baseSpec, "@core"))
	commitAll(t, dir, "feature(alpha): New spec")
	wantRules(t, check(t, dir))
}

func TestUnchangedSpecIsNotDiffed(t *testing.T) {
	dir := goodRepo(t)
	runGit(t, dir, "switch", "-c", "topic")
	write(t, dir, "other.txt", "x\n")
	commitAll(t, dir, "chore: Other")
	// A missing binary would be an error if the spec were diffed.
	vs, err := contractgate.Check(context.Background(), contractgate.Options{Root: dir, BaseRev: "main", OasdiffBin: "no-such-oasdiff-binary"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	wantRules(t, vs)
}

func TestOasdiffOperationalFailureIsAnError(t *testing.T) {
	dir := goodRepo(t)
	runGit(t, dir, "switch", "-c", "topic")
	write(t, dir, "specs/alpha/openapi.yaml", specYAML("1.0.1", "", "[id, name]", ""))
	write(t, dir, "specs/alpha/spec.manifest.json", manifestJSON("alpha", "1.0.1", "@core"))
	commitAll(t, dir, "fix(alpha): Text")

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeExitEnv, "3")
	_, err = contractgate.Check(context.Background(), contractgate.Options{Root: dir, BaseRev: "main", OasdiffBin: self})
	if err == nil {
		t.Fatal("want an error for oasdiff exit 3")
	}
	if !strings.Contains(err.Error(), "fake oasdiff failure") {
		t.Errorf("error lacks oasdiff stderr: %v", err)
	}
}

func TestMissingOasdiffBinaryIsAnError(t *testing.T) {
	dir := goodRepo(t)
	runGit(t, dir, "switch", "-c", "topic")
	write(t, dir, "specs/alpha/openapi.yaml", specYAML("1.0.1", "", "[id, name]", ""))
	write(t, dir, "specs/alpha/spec.manifest.json", manifestJSON("alpha", "1.0.1", "@core"))
	commitAll(t, dir, "fix(alpha): Text")
	_, err := contractgate.Check(context.Background(), contractgate.Options{Root: dir, BaseRev: "main", OasdiffBin: "no-such-oasdiff-binary"})
	if err == nil {
		t.Fatal("want an error")
	}
}

func TestOptionsAreValidated(t *testing.T) {
	_, err := contractgate.Check(context.Background(), contractgate.Options{BaseRev: "main"})
	if err == nil {
		t.Error("empty root should fail")
	}
	_, err = contractgate.Check(context.Background(), contractgate.Options{Root: t.TempDir()})
	if err == nil {
		t.Error("empty base revision should fail")
	}
}

func TestUnparseableContractIsAnError(t *testing.T) {
	dir := newRepo(t)
	writeSpec(t, dir, "alpha", "a: [unclosed\n", manifestJSON("alpha", baseSpec, "@core"))
	commitAll(t, dir, "alpha")
	_, err := contractgate.Check(context.Background(), contractgate.Options{Root: dir, BaseRev: "main"})
	if err == nil {
		t.Fatal("want an error")
	}
}

func FuzzCheckStructure(f *testing.F) {
	f.Add(specYAML(baseSpec, "", "[id]", ""))
	f.Add("$ref: ./x.yaml\nx-stability-level: alpha\n")
	f.Add("&a [*a]\n")
	f.Add("")
	_, err := exec.LookPath("git")
	if err != nil {
		f.Skip("git is not on PATH")
	}
	dir := f.TempDir()
	cmds := [][]string{
		{"init", "-b", "main"},
		{"-c", "user.name=Example Tester", "-c", "user.email=tester@example.com", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "first"},
	}
	for _, args := range cmds {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			f.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	manifest := manifestJSON("alpha", baseSpec, "@core")
	f.Fuzz(func(t *testing.T, content string) {
		writeSpec(t, dir, "alpha", content, manifest)
		vs, err := contractgate.Check(context.Background(), contractgate.Options{Root: dir, BaseRev: "main"})
		// Unparseable YAML is a designed error; any other error is a defect.
		if err != nil && strings.HasPrefix(err.Error(), "parse ") {
			return
		}
		if err != nil {
			t.Fatalf("Check failed on spec %q: %v", content, err)
		}
		for _, v := range vs {
			if strings.Contains(v.Path, `\`) || v.Rule == "" {
				t.Fatalf("bad violation %+v", v)
			}
		}
	})
}

func checkPreRelease(t *testing.T, dir string) []report.Violation {
	t.Helper()
	vs, err := contractgate.Check(context.Background(), contractgate.Options{Root: dir, BaseRev: "main", PreRelease: true})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return vs
}

func TestPreReleaseBreakingChangeWithMarkerAndSameVersionPasses(t *testing.T) {
	dir := breakingBranch(t, "1.0.0", "feature(alpha): Drop name\n\nWhy.\n\nBREAKING CHANGE: name is gone")
	wantRules(t, checkPreRelease(t, dir))
}

func TestPreReleaseBreakingChangeWithoutMarkerFailsUnmarkedOnly(t *testing.T) {
	dir := breakingBranch(t, "1.0.0", "feature(alpha): Drop name")
	wantRules(t, checkPreRelease(t, dir), contractgate.RuleBreakingUnmarked)
}

func TestPreReleaseVersionResetPasses(t *testing.T) {
	dir := goodRepo(t)
	runGit(t, dir, "switch", "-c", "topic")
	write(t, dir, "specs/alpha/openapi.yaml", specYAML("0.1.0", "", "[id, name]", ""))
	write(t, dir, "specs/alpha/spec.manifest.json", manifestJSON("alpha", "0.1.0", "@core"))
	commitAll(t, dir, "reset")
	wantRules(t, checkPreRelease(t, dir))
}

func TestPreReleaseKeepsVersionDrift(t *testing.T) {
	dir := newRepo(t)
	writeSpec(t, dir, "alpha", specYAML("0.1.0", "", "[id]", ""), manifestJSON("alpha", "0.2.0", "@core"))
	commitAll(t, dir, "alpha")
	wantRules(t, checkPreRelease(t, dir), contractgate.RuleVersionDrift)
}

func TestNotPreReleaseBreakingChangeStillNeedsMajor(t *testing.T) {
	dir := breakingBranch(t, "1.1.0", "feature(alpha): Drop name\n\nWhy.\n\nBREAKING CHANGE: name is gone")
	wantRules(t, check(t, dir), contractgate.RuleBreakingNotMajor)
}
