package featureflags_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/featureflags"
)

type wantProblem struct {
	code string
	line int
}

func parseProblems(t *testing.T, content string) []featureflags.Problem {
	t.Helper()
	_, err := featureflags.ParseManifest(strings.NewReader(content))
	if err == nil {
		t.Fatalf("manifest accepted:\n%s", content)
	}
	var manifestErr *featureflags.ManifestError
	if !errors.As(err, &manifestErr) {
		t.Fatalf("error is %T, want *ManifestError: %v", err, err)
	}
	if len(manifestErr.Problems) == 0 {
		t.Fatal("ManifestError without problems")
	}
	return manifestErr.Problems
}

func TestParseManifestRejectsEachRuleWithCodeAndLine(t *testing.T) {
	const head = "flags:\n"
	cases := []struct {
		name    string
		content string
		want    wantProblem
	}{
		{"not a mapping", "- a\n", wantProblem{featureflags.CodeParseError, 1}},
		{"flags not a sequence", "flags: {a: b}\n", wantProblem{featureflags.CodeParseError, 1}},
		{"flags null", "flags:\n", wantProblem{featureflags.CodeParseError, 1}},
		{"empty file", "", wantProblem{featureflags.CodeParseError, 0}},
		{"broken yaml", "flags: [\n", wantProblem{featureflags.CodeParseError, 1}},
		{"entry not a mapping", head + "  - nope\n", wantProblem{featureflags.CodeParseError, 2}},
		{"unknown top-level field", "flags: []\nextra: 1\n", wantProblem{featureflags.CodeUnknownField, 2}},
		{"unknown entry field", head + "  - key: a_flag\n    type: ops\n    default: true\n    description: d\n    owner: x\n", wantProblem{featureflags.CodeUnknownField, 6}},
		{"key missing", head + "  - type: ops\n    default: true\n    description: d\n", wantProblem{featureflags.CodeKeyInvalid, 2}},
		{"key uppercase", head + "  - key: Bad_Key\n    type: ops\n    default: true\n    description: d\n", wantProblem{featureflags.CodeKeyInvalid, 2}},
		{"key with dash", head + "  - key: bad-key\n    type: ops\n    default: true\n    description: d\n", wantProblem{featureflags.CodeKeyInvalid, 2}},
		{"key starts with digit", head + "  - key: 1abc\n    type: ops\n    default: true\n    description: d\n", wantProblem{featureflags.CodeKeyInvalid, 2}},
		{"key 65 characters", head + "  - key: a" + strings.Repeat("b", 64) + "\n    type: ops\n    default: true\n    description: d\n", wantProblem{featureflags.CodeKeyInvalid, 2}},
		{"duplicate key", head + "  - key: a_flag\n    type: ops\n    default: true\n    description: d\n  - key: a_flag\n    type: ops\n    default: true\n    description: d\n", wantProblem{featureflags.CodeKeyDuplicate, 6}},
		{"type missing", head + "  - key: a_flag\n    default: true\n    description: d\n", wantProblem{featureflags.CodeTypeInvalid, 2}},
		{"type unknown", head + "  - key: a_flag\n    type: temp\n    default: true\n    description: d\n", wantProblem{featureflags.CodeTypeInvalid, 3}},
		{"default missing", head + "  - key: a_flag\n    type: ops\n    description: d\n", wantProblem{featureflags.CodeDefaultMissing, 2}},
		{"default is a string", head + "  - key: a_flag\n    type: ops\n    default: \"false\"\n    description: d\n", wantProblem{featureflags.CodeDefaultMissing, 4}},
		{"description missing", head + "  - key: a_flag\n    type: ops\n    default: true\n", wantProblem{featureflags.CodeDescriptionMissing, 2}},
		{"description blank", head + "  - key: a_flag\n    type: ops\n    default: true\n    description: \"   \"\n", wantProblem{featureflags.CodeDescriptionMissing, 5}},
		{"release default true", head + "  - key: a_flag\n    type: release\n    default: true\n    expires_at: \"2099-12-31\"\n    description: d\n", wantProblem{featureflags.CodeReleaseDefaultTrue, 4}},
		{"release without expiry", head + "  - key: a_flag\n    type: release\n    default: false\n    description: d\n", wantProblem{featureflags.CodeExpiresAtMissing, 2}},
		{"expiry impossible date", head + "  - key: a_flag\n    type: release\n    default: false\n    expires_at: 2026-02-30\n    description: d\n", wantProblem{featureflags.CodeExpiresAtInvalid, 5}},
		{"expiry wrong format", head + "  - key: a_flag\n    type: release\n    default: false\n    expires_at: 31/12/2099\n    description: d\n", wantProblem{featureflags.CodeExpiresAtInvalid, 5}},
		{"expiry null", head + "  - key: a_flag\n    type: release\n    default: false\n    expires_at:\n    description: d\n", wantProblem{featureflags.CodeExpiresAtInvalid, 5}},
		{"ops with expiry", head + "  - key: a_flag\n    type: ops\n    default: true\n    expires_at: 2099-12-31\n    description: d\n", wantProblem{featureflags.CodeOpsHasExpiry, 5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := parseProblems(t, tc.content)
			found := false
			for _, p := range problems {
				if p.Code == tc.want.code && p.Line == tc.want.line {
					found = true
				}
			}
			if !found {
				t.Fatalf("want %+v among %+v", tc.want, problems)
			}
		})
	}
}

func TestKeyLengthBoundary(t *testing.T) {
	key64 := "a" + strings.Repeat("b", 63)
	content := "flags:\n  - key: " + key64 + "\n    type: ops\n    default: true\n    description: d\n"
	manifest, err := featureflags.ParseManifest(strings.NewReader(content))
	if err != nil {
		t.Fatalf("64-character key rejected: %v", err)
	}
	if len(manifest.Flags()) != 1 {
		t.Fatalf("flags = %v", manifest.Flags())
	}
}

func TestParseManifestReportsEveryProblemTogether(t *testing.T) {
	content := `flags:
  - key: Bad_Key
    type: temp
    default: "no"
    description: " "
  - key: ok_flag
    type: release
    default: true
    description: d
  - key: ok_flag
    type: ops
    default: false
    expires_at: 2099-12-31
    description: d
`
	problems := parseProblems(t, content)
	codes := map[string]bool{}
	for _, p := range problems {
		codes[p.Code] = true
	}
	for _, code := range []string{
		featureflags.CodeKeyInvalid, featureflags.CodeTypeInvalid, featureflags.CodeDefaultMissing,
		featureflags.CodeDescriptionMissing, featureflags.CodeReleaseDefaultTrue,
		featureflags.CodeExpiresAtMissing, featureflags.CodeKeyDuplicate, featureflags.CodeOpsHasExpiry,
	} {
		if !codes[code] {
			t.Errorf("missing %s in %+v", code, problems)
		}
	}
	for i := 1; i < len(problems); i++ {
		if problems[i].Line < problems[i-1].Line {
			t.Fatalf("problems not ordered by line: %+v", problems)
		}
	}
	message := (&featureflags.ManifestError{Problems: problems}).Error()
	if !strings.Contains(message, "FLAG_KEY_INVALID") || !strings.Contains(message, "FLAG_OPS_HAS_EXPIRY") {
		t.Fatalf("Error() must list every problem: %s", message)
	}
}

func TestParseManifestAcceptsValidFiles(t *testing.T) {
	t.Run("empty sequence", func(t *testing.T) {
		manifest, err := featureflags.ParseManifest(strings.NewReader("flags: []\n"))
		if err != nil {
			t.Fatalf("flags: [] rejected: %v", err)
		}
		if len(manifest.Flags()) != 0 {
			t.Fatalf("flags = %v", manifest.Flags())
		}
	})
	t.Run("quoted and unquoted dates", func(t *testing.T) {
		content := `flags:
  - key: quoted_date
    type: release
    default: false
    expires_at: "2099-12-31"
    description: d
  - key: plain_date
    type: release
    default: false
    expires_at: 2099-12-30
    description: d
`
		manifest, err := featureflags.ParseManifest(strings.NewReader(content))
		if err != nil {
			t.Fatalf("dates rejected: %v", err)
		}
		want := []time.Time{time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC), time.Date(2099, 12, 30, 0, 0, 0, 0, time.UTC)}
		for i, flag := range manifest.Flags() {
			if flag.ExpiresAt == nil || !flag.ExpiresAt.Equal(want[i]) {
				t.Fatalf("flag %s expires_at = %v, want %v", flag.Key, flag.ExpiresAt, want[i])
			}
		}
	})
	t.Run("expired release flag loads", func(t *testing.T) {
		content := "flags:\n  - key: old_flag\n    type: release\n    default: false\n    expires_at: 2001-01-01\n    description: d\n"
		_, err := featureflags.ParseManifest(strings.NewReader(content))
		if err != nil {
			t.Fatalf("expired flag rejected: %v", err)
		}
	})
	t.Run("order lookup and copies", func(t *testing.T) {
		manifest, err := featureflags.ParseManifest(strings.NewReader(twoFlagManifest))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		flags := manifest.Flags()
		if len(flags) != 2 || flags[0].Key != releaseKey || flags[1].Key != opsKey {
			t.Fatalf("flags = %+v", flags)
		}
		flags[0].Default = true
		flags[0].ExpiresAt = nil
		again, ok := manifest.Lookup(releaseKey)
		if !ok || again.Default || again.ExpiresAt == nil || again.Type != featureflags.TypeRelease {
			t.Fatalf("manifest mutated through a returned copy: %+v", again)
		}
		ops, _ := manifest.Lookup(opsKey)
		if !ops.Default || ops.ExpiresAt != nil || ops.Line != 7 {
			t.Fatalf("ops flag = %+v", ops)
		}
		_, found := manifest.Lookup("nope")
		if found {
			t.Fatal("Lookup found an undeclared key")
		}
	})
}

func TestParseManifestSizeCap(t *testing.T) {
	const capBytes = 1 << 20
	const header = "flags: []\n# "
	padded := func(total int) string {
		return header + strings.Repeat("x", total-len(header)-1) + "\n"
	}
	t.Run("accepts input of exactly the cap", func(t *testing.T) {
		_, err := featureflags.ParseManifest(strings.NewReader(padded(capBytes)))
		if err != nil {
			t.Fatalf("manifest at the cap rejected: %v", err)
		}
	})
	t.Run("rejects input one byte over the cap", func(t *testing.T) {
		problems := parseProblems(t, padded(capBytes+1))
		if problems[0].Code != featureflags.CodeParseError {
			t.Fatalf("problems = %+v", problems)
		}
	})
}

func TestLoadManifestFile(t *testing.T) {
	t.Run("loads a file", func(t *testing.T) {
		path := writeManifest(t, twoFlagManifest)
		manifest, err := featureflags.LoadManifestFile(path)
		if err != nil || len(manifest.Flags()) != 2 {
			t.Fatalf("load = %v, %v", manifest.Flags(), err)
		}
	})
	t.Run("resolves a relative path against the working directory", func(t *testing.T) {
		dir := t.TempDir()
		err := os.WriteFile(filepath.Join(dir, "flags.yaml"), []byte("flags: []\n"), 0o600)
		if err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)
		_, err = featureflags.LoadManifestFile("flags.yaml")
		if err != nil {
			t.Fatalf("relative load: %v", err)
		}
	})
	t.Run("file not found names the path", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "absent.yaml")
		_, err := featureflags.LoadManifestFile(missing)
		if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), missing) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("invalid file keeps every problem", func(t *testing.T) {
		path := writeManifest(t, "flags:\n  - key: Bad\n")
		_, err := featureflags.LoadManifestFile(path)
		var manifestErr *featureflags.ManifestError
		if !errors.As(err, &manifestErr) || len(manifestErr.Problems) < 2 {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("empty path", func(t *testing.T) {
		_, err := featureflags.LoadManifestFile("")
		if err == nil {
			t.Fatal("empty path accepted")
		}
	})
}

func FuzzParseManifest(f *testing.F) {
	f.Add([]byte(twoFlagManifest))
	f.Add([]byte("flags: []\n"))
	f.Add([]byte("flags: [\n"))
	f.Add([]byte("flags:\n  - &a {key: x}\n  - *a\n"))
	f.Add([]byte("- a\n"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) {
		manifest, err := featureflags.ParseManifest(strings.NewReader(string(data)))
		if err == nil {
			for _, flag := range manifest.Flags() {
				_, found := manifest.Lookup(flag.Key)
				if !found || flag.Key == "" {
					t.Fatalf("accepted manifest has an unusable flag %+v", flag)
				}
			}
			return
		}
		var manifestErr *featureflags.ManifestError
		if !errors.As(err, &manifestErr) || len(manifestErr.Problems) == 0 {
			t.Fatalf("error without problems: %v", err)
		}
	})
}
