package names_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
)

func TestValidLabel(t *testing.T) {
	valid := []string{"ab", "acme-backend", "app1013", "a1", strings.Repeat("a", 63), "a-b-c", "app1234x"}
	invalid := []string{"", "a", "abc-", "-abc", "Acme", "a_b", "app10132", "1ab", "../x", "a/b", "..", "a..b", "a!b", strings.Repeat("a", 64), "a b", "ab\n"}
	for _, s := range valid {
		if !names.ValidLabel(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range invalid {
		if names.ValidLabel(s) {
			t.Errorf("%q should be invalid", s)
		}
	}
}

var referenceLabel = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)
var referenceDigitRun = regexp.MustCompile(`[0-9]{5}`)

func FuzzValidLabel(f *testing.F) {
	for _, seed := range []string{"ab", "acme-backend", "app10132", "-a", "a-", "", "Ab"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		// Must not panic; a label is also stable under a second check.
		got := names.ValidLabel(s)
		if got != names.ValidLabel(s) {
			t.Fatalf("ValidLabel(%q) is not deterministic", s)
		}
		if !got {
			return
		}
		// The reference grammar must agree on every accepted label.
		if !referenceLabel.MatchString(s) || referenceDigitRun.MatchString(s) {
			t.Fatalf("ValidLabel(%q) accepted a label outside the grammar", s)
		}
		// Invariants of every accepted label, independent of the rule's encoding.
		if strings.ContainsAny(s, " _./ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			t.Fatalf("ValidLabel(%q) accepted a forbidden character", s)
		}
		if strings.HasPrefix(s, "-") || strings.HasSuffix(s, "-") {
			t.Fatalf("ValidLabel(%q) accepted a leading or trailing hyphen", s)
		}
		if len(s) < 2 || len(s) > 63 {
			t.Fatalf("ValidLabel(%q) accepted length %d", s, len(s))
		}
	})
}
