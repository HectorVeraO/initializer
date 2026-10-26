package flagscheck_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/tooling/gates/internal/flagscheck"
	"github.com/HectorVeraO/initializer/tooling/gates/internal/report"
)

var today = time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)

func checkText(t *testing.T, content, base string) []report.Violation {
	t.Helper()
	path := filepath.Join(t.TempDir(), "flags.yaml")
	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	opts := flagscheck.Options{FilePath: path, Today: today, WarnDays: 7}
	if base != "" {
		opts.Base = []byte(base)
	}
	vs, err := flagscheck.Check(opts)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return vs
}

func rules(vs []report.Violation) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.Rule)
	}
	return out
}

func assertRules(t *testing.T, vs []report.Violation, want ...string) {
	t.Helper()
	got := strings.Join(rules(vs), ",")
	if got != strings.Join(want, ",") {
		t.Fatalf("rules = [%s], want [%s]; violations: %+v", got, strings.Join(want, ","), vs)
	}
}

func release(key, expires string) string {
	return "flags:\n  - key: " + key + "\n    type: release\n    default: false\n    expires_at: " + expires + "\n    description: d\n"
}

func ops(key string) string {
	return "flags:\n  - key: " + key + "\n    type: ops\n    default: false\n    description: d\n"
}

func TestValidFileHasNoViolations(t *testing.T) {
	content, err := os.ReadFile("testdata/valid.yaml")
	if err != nil {
		t.Fatal(err)
	}
	assertRules(t, checkText(t, string(content), ""))
}

func TestEmptyFlagsSequenceIsValid(t *testing.T) {
	assertRules(t, checkText(t, "# no flags yet\nflags: []\n", ""))
}

func TestMissingFile(t *testing.T) {
	vs, err := flagscheck.Check(flagscheck.Options{FilePath: filepath.Join(t.TempDir(), "flags.yaml"), Today: today})
	if err != nil {
		t.Fatal(err)
	}
	assertRules(t, vs, flagscheck.RuleFileMissing)
}

func TestUnreadableFileIsAnOperationalError(t *testing.T) {
	_, err := flagscheck.Check(flagscheck.Options{FilePath: t.TempDir(), Today: today})
	if err == nil {
		t.Fatal("reading a directory must be an error, not a violation")
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"empty file":         "",
		"flags null":         "flags:\n",
		"top level sequence": "- key: a\n",
		"not yaml":           "flags: [unclosed\n",
		"flags is mapping":   "flags:\n  key: a\n",
		"entry not mapping":  "flags:\n  - just_a_string\n",
		"scalar top level":   "hello\n",
		"missing flags key":  "other: []\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			vs := checkText(t, content, "")
			if len(vs) == 0 {
				t.Fatal("expected a violation")
			}
			t.Logf("RULES %s: %v", name, vs[0].Rule)
		})
	}
	assertRules(t, checkText(t, "flags:\n", ""), flagscheck.RuleParseError)
}

func TestUnknownFields(t *testing.T) {
	entry := ops("example_beta_banner") + "    owner: nobody\n"
	vs := checkText(t, entry, "")
	assertRules(t, vs, flagscheck.RuleUnknownField)
	if vs[0].Line != 6 {
		t.Fatalf("line = %d, want 6", vs[0].Line)
	}
	assertRules(t, checkText(t, "flags: []\nextra: 1\n", ""), flagscheck.RuleUnknownField)
}

func TestKeyValidation(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		valid bool
	}{
		{"64 characters", strings.Repeat("a", 64), true},
		{"65 characters", strings.Repeat("a", 65), false},
		{"uppercase", "Example", false},
		{"leading digit", "1example", false},
		{"hyphen", "example-flag", false},
		{"underscore and digits", "a_1_b", true},
		{"leading underscore", "_a", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vs := checkText(t, ops(tc.key), "")
			if tc.valid {
				assertRules(t, vs)
				return
			}
			assertRules(t, vs, flagscheck.RuleKeyInvalid)
		})
	}
}

func TestMissingKey(t *testing.T) {
	assertRules(t, checkText(t, "flags:\n  - type: ops\n    default: false\n    description: d\n", ""), flagscheck.RuleKeyInvalid)
}

func TestDuplicateKeyIsReportedOnTheSecondAndCitesTheFirst(t *testing.T) {
	one := "  - key: example_beta_banner\n    type: ops\n    default: false\n    description: d\n"
	vs := checkText(t, "flags:\n"+one+one, "")
	assertRules(t, vs, flagscheck.RuleKeyDuplicate)
	if vs[0].Line != 6 || !strings.Contains(vs[0].Message, "line 2") {
		t.Fatalf("got line %d message %q", vs[0].Line, vs[0].Message)
	}
}

func TestTypeInvalid(t *testing.T) {
	for _, typ := range []string{"sometimes", "Release", `""`} {
		content := "flags:\n  - key: example_beta_banner\n    type: " + typ + "\n    default: false\n    description: d\n"
		assertRules(t, checkText(t, content, ""), flagscheck.RuleTypeInvalid)
	}
	assertRules(t, checkText(t, "flags:\n  - key: example_beta_banner\n    default: false\n    description: d\n", ""), flagscheck.RuleTypeInvalid)
}

func TestDefaultMustBeABoolean(t *testing.T) {
	for _, def := range []string{`"false"`, "0", "maybe"} {
		content := "flags:\n  - key: example_beta_banner\n    type: ops\n    default: " + def + "\n    description: d\n"
		assertRules(t, checkText(t, content, ""), flagscheck.RuleDefaultMissing)
	}
	assertRules(t, checkText(t, "flags:\n  - key: example_beta_banner\n    type: ops\n    description: d\n", ""), flagscheck.RuleDefaultMissing)
}

func TestDescriptionRequired(t *testing.T) {
	for _, desc := range []string{"    description: \"   \"\n", ""} {
		content := "flags:\n  - key: example_beta_banner\n    type: ops\n    default: false\n" + desc
		assertRules(t, checkText(t, content, ""), flagscheck.RuleDescriptionMiss)
	}
}

func TestReleaseFlagCannotDefaultToTrue(t *testing.T) {
	content := "flags:\n  - key: example_beta_banner\n    type: release\n    default: true\n    expires_at: 2026-11-10\n    description: d\n"
	assertRules(t, checkText(t, content, ""), flagscheck.RuleReleaseDefault)
}

func TestReleaseFlagNeedsExpiry(t *testing.T) {
	content := "flags:\n  - key: example_beta_banner\n    type: release\n    default: false\n    description: d\n"
	assertRules(t, checkText(t, content, ""), flagscheck.RuleExpiresAtMissing)
}

func TestExpiresAtMustBeARealDate(t *testing.T) {
	for _, value := range []string{"2026-02-30", `"2026-13-01"`, "tomorrow", "2026-1-5", "2026-11-10T00:00:00Z", "null"} {
		t.Run(value, func(t *testing.T) {
			assertRules(t, checkText(t, release("example_beta_banner", value), ""), flagscheck.RuleExpiresAtInvalid)
		})
	}
}

func TestQuotedAndUnquotedDatesAreEquivalent(t *testing.T) {
	for _, value := range []string{"2026-11-10", `"2026-11-10"`, "'2026-11-10'"} {
		assertRules(t, checkText(t, release("example_beta_banner", value), ""))
	}
}

func TestOpsFlagCannotExpire(t *testing.T) {
	content := ops("example_kill_switch") + "    expires_at: 2027-01-01\n"
	assertRules(t, checkText(t, content, ""), flagscheck.RuleOpsHasExpiry)
}

func TestExpiryBoundaries(t *testing.T) {
	day := func(offset int) string { return today.AddDate(0, 0, offset).Format("2006-01-02") }
	cases := []struct {
		name   string
		offset int
		want   []string
	}{
		{"yesterday", -1, []string{flagscheck.RuleExpired}},
		{"today", 0, []string{flagscheck.RuleExpired}},
		{"tomorrow warns", 1, []string{flagscheck.RuleExpiresSoon}},
		{"7 days warns", 7, []string{flagscheck.RuleExpiresSoon}},
		{"8 days is quiet", 8, nil},
		{"30 days passes", 30, nil},
		{"31 days fails", 31, []string{flagscheck.RuleExpiresTooFar}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertRules(t, checkText(t, release("example_beta_banner", day(tc.offset)), ""), tc.want...)
		})
	}
}

func TestSoonIsAWarningAndExpiredIsAnError(t *testing.T) {
	soon := checkText(t, release("example_beta_banner", today.AddDate(0, 0, 3).Format("2006-01-02")), "")
	if soon[0].Severity != report.SeverityWarning {
		t.Fatal("FLAG_EXPIRES_SOON must be a warning")
	}
	expired := checkText(t, release("example_beta_banner", "2026-10-20"), "")
	if expired[0].Severity != report.SeverityError || !strings.Contains(expired[0].Message, "TBD rule 5") {
		t.Fatalf("unexpected expired violation %+v", expired[0])
	}
}

func TestFlagInBaseKeepsItsFarDate(t *testing.T) {
	content := release("example_beta_banner", "2026-12-31")
	assertRules(t, checkText(t, content, content))
}

func TestExtendingExpiryFails(t *testing.T) {
	base := release("example_beta_banner", "2026-11-12")
	assertRules(t, checkText(t, release("example_beta_banner", "2026-11-24"), base), flagscheck.RuleExpiryExtended)
}

func TestShorteningExpiryFails(t *testing.T) {
	base := release("example_beta_banner", "2026-11-24")
	assertRules(t, checkText(t, release("example_beta_banner", "2026-11-12"), base), flagscheck.RuleExpiryExtended)
}

func TestChangingTypeFails(t *testing.T) {
	vs := checkText(t, release("example_beta_banner", "2026-11-24"), ops("example_beta_banner"))
	assertRules(t, vs, flagscheck.RuleTypeChanged)
}

func TestNewFlagAgainstAnEmptyBaseIsStillLimited(t *testing.T) {
	assertRules(t, checkText(t, release("example_beta_banner", "2026-12-31"), "flags: []\n"), flagscheck.RuleExpiresTooFar)
}

func TestBrokenBaseDoesNotHideCurrentViolations(t *testing.T) {
	assertRules(t, checkText(t, release("example_beta_banner", "2026-12-31"), "not: [valid"), flagscheck.RuleExpiresTooFar)
}

func TestEveryViolationOfOneFileIsReported(t *testing.T) {
	content, err := os.ReadFile("testdata/many_violations.yaml")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, v := range checkText(t, string(content), "") {
		got[v.Rule] = true
	}
	want := []string{
		flagscheck.RuleKeyInvalid, flagscheck.RuleTypeInvalid, flagscheck.RuleDefaultMissing,
		flagscheck.RuleDescriptionMiss, flagscheck.RuleReleaseDefault, flagscheck.RuleExpiresAtInvalid,
		flagscheck.RuleUnknownField, flagscheck.RuleOpsHasExpiry, flagscheck.RuleKeyDuplicate,
	}
	for _, rule := range want {
		if !got[rule] {
			t.Errorf("missing %s in %v", rule, got)
		}
	}
}

func TestViolationsCarryPathRefAndSortedLines(t *testing.T) {
	content, err := os.ReadFile("testdata/many_violations.yaml")
	if err != nil {
		t.Fatal(err)
	}
	prev := 0
	vs := checkText(t, string(content), "")
	if len(vs) == 0 {
		t.Fatal("many_violations.yaml must produce violations")
	}
	for _, v := range vs {
		if v.Path != "flags.yaml" || v.Ref != "RA.2" || v.Line < prev {
			t.Fatalf("bad violation %+v after line %d", v, prev)
		}
		prev = v.Line
	}
}

func TestSantiagoDateDiffersFromUTCNearMidnight(t *testing.T) {
	now := time.Date(2026, 10, 27, 1, 30, 0, 0, time.UTC)
	got, err := flagscheck.SantiagoDate(now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format("2006-01-02") != "2026-10-26" {
		t.Fatalf("got %s, want 2026-10-26", got.Format("2006-01-02"))
	}
}

var knownRules = map[string]bool{
	flagscheck.RuleFileMissing: true, flagscheck.RuleParseError: true, flagscheck.RuleUnknownField: true,
	flagscheck.RuleKeyInvalid: true, flagscheck.RuleKeyDuplicate: true, flagscheck.RuleTypeInvalid: true,
	flagscheck.RuleDefaultMissing: true, flagscheck.RuleDescriptionMiss: true, flagscheck.RuleReleaseDefault: true,
	flagscheck.RuleExpiresAtMissing: true, flagscheck.RuleExpiresAtInvalid: true, flagscheck.RuleOpsHasExpiry: true,
	flagscheck.RuleExpired: true, flagscheck.RuleExpiresTooFar: true, flagscheck.RuleExpiryExtended: true,
	flagscheck.RuleTypeChanged: true, flagscheck.RuleExpiresSoon: true,
}

func FuzzCheck(f *testing.F) {
	seeds := []string{"", "flags: []\n", release("example_beta_banner", "2026-11-10"), "- a\n", "flags: [", "&a [*a]", "flags:\n  - &x {key: *x}\n"}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(dir, "flags.yaml")
		err := os.WriteFile(path, data, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		vs, err := flagscheck.Check(flagscheck.Options{FilePath: path, Base: data, Today: today, WarnDays: 7})
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range vs {
			if v.Line < 0 || !knownRules[v.Rule] {
				t.Fatalf("bad violation %+v", v)
			}
		}
	})
}
