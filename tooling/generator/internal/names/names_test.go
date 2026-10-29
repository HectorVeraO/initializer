package names_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
)

func TestValidLabel(t *testing.T) {
	valid := []string{"ab", "acme-backend", "app1013", "a1", strings.Repeat("a", 63), "a-b-c", "app1234x"}
	invalid := []string{"", "a", "abc-", "-abc", "Acme", "a_b", "app10132", "1ab", strings.Repeat("a", 64), "a b", "ab\n"}
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

var (
	labelPattern  = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)
	digitsPattern = regexp.MustCompile(`[0-9]{5}`)
)

func FuzzValidLabel(f *testing.F) {
	for _, seed := range []string{"ab", "acme-backend", "app10132", "-a", "a-", "", "Ab"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		want := len(s) >= 2 && len(s) <= 63 && labelPattern.MatchString(s) && !digitsPattern.MatchString(s)
		if got := names.ValidLabel(s); got != want {
			t.Fatalf("ValidLabel(%q) = %v, want %v", s, got, want)
		}
	})
}
