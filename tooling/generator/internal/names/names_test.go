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

func TestIsDeviceName(t *testing.T) {
	devices := []string{"con", "prn", "aux", "nul", "com0", "com9", "lpt0", "lpt5", "lpt9"}
	others := []string{"", "co", "cons", "com", "comx", "com10", "lpt", "lpt10", "CON", "acme", "con-x", "xcom1", "com1 ", "nul.txt"}
	for _, s := range devices {
		if !names.IsDeviceName(s) {
			t.Errorf("%q should be a device name", s)
		}
	}
	for _, s := range others {
		if names.IsDeviceName(s) {
			t.Errorf("%q should not be a device name", s)
		}
	}
}

func TestLabelMessageNamesTheValueAndTheRule(t *testing.T) {
	message := names.LabelMessage("Acme_API")
	if !strings.HasPrefix(message, `"Acme_API" must be lowercase letters, digits and inner hyphens, 2-63 characters`) {
		t.Fatalf("message %q", message)
	}
}

func TestClosest(t *testing.T) {
	stacks := []string{"go-srv", "ng-web"}
	cases := map[string]string{
		"ng-wb":    "ng-web",
		"go-svr":   "go-srv",
		"ng-webs":  "ng-web",
		"ng-web":   "",
		"rust-srv": "",
		"":         "",
	}
	for word, want := range cases {
		if got := names.Closest(word, stacks); got != want {
			t.Errorf("Closest(%q) = %q, want %q", word, got, want)
		}
	}
	if got := names.Closest("ng-wb", nil); got != "" {
		t.Errorf("no candidates: %q", got)
	}
}

func TestValidPGIdent(t *testing.T) {
	valid := []string{"a", "_", "acme", "acme_backend", "_x1", "a1234", "a1234b5678", strings.Repeat("a", 63)}
	invalid := []string{"", "1a", "Acme", "acme-backend", "a b", "a.b", "app10132", "a12345", strings.Repeat("a", 64), "ab\n", "é"}
	for _, s := range valid {
		if !names.ValidPGIdent(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range invalid {
		if names.ValidPGIdent(s) {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestPGIdentMessageNamesTheValueAndTheRule(t *testing.T) {
	message := names.PGIdentMessage("Bad-Name")
	for _, want := range []string{`"Bad-Name"`, "underscores", "1-63"} {
		if !strings.Contains(message, want) {
			t.Errorf("message %q lacks %q", message, want)
		}
	}
}

var referencePGIdent = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

func FuzzValidPGIdent(f *testing.F) {
	for _, seed := range []string{"acme", "acme_backend", "_", "1a", "a-b", "", "a12345", "Ab"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !names.ValidPGIdent(s) {
			return
		}
		if !referencePGIdent.MatchString(s) || referenceDigitRun.MatchString(s) {
			t.Fatalf("ValidPGIdent(%q) accepted a value outside the grammar", s)
		}
		if len(s) < 1 || len(s) > 63 {
			t.Fatalf("ValidPGIdent(%q) accepted length %d", s, len(s))
		}
	})
}

func TestSnakeOfALabelIsAPGIdent(t *testing.T) {
	for _, label := range []string{"ab", "acme-backend", "a-b-c", "app1013"} {
		if !names.ValidPGIdent(names.Snake(label)) {
			t.Errorf("Snake(%q) = %q is not a pgident", label, names.Snake(label))
		}
	}
}

func TestValidDisplay(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{"one byte", "A", true},
		{"64 bytes", strings.Repeat("a", 64), true},
		{"65 bytes", strings.Repeat("a", 65), false},
		{"leading space", " Acme", false},
		{"trailing space", "Acme ", false},
		{"inner space", "Acme Shop", true},
		{"digits", "Shop 12345", true},
		{"dots underscores hyphens", "a.b_c-d", true},
		{"trailing dot", "Acme.", true},
		{"quote", "Acme's", false},
		{"placeholder", "%{x}", false},
		{"angle bracket", "a<b", false},
		{"ampersand", "a&b", false},
		{"backslash", `a\b`, false},
		{"empty", "", false},
		{"non ASCII", "Café", false},
	}
	for _, c := range cases {
		if got := names.ValidDisplay(c.value); got != c.want {
			t.Errorf("%s: names.ValidDisplay(%q) = %v, want %v", c.name, c.value, got, c.want)
		}
	}
}

func FuzzValidDisplay(f *testing.F) {
	for _, seed := range []string{"", "A", "Acme Shop", " x", "x ", "%{x}", "a\x00b", strings.Repeat("z", 70)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if !names.ValidDisplay(value) {
			return
		}
		// An accepted value is plain ASCII text that needs no escaping and
		// has no edge whitespace.
		if len(value) < 1 || len(value) > 64 || strings.TrimSpace(value) != value {
			t.Fatalf("accepted %q", value)
		}
		if strings.ContainsAny(value, "\"'<>&%{}\\n\t\x00") {
			t.Fatalf("accepted %q with a character that needs escaping", value)
		}
		for i := 0; i < len(value); i++ {
			if value[i] > 127 {
				t.Fatalf("accepted non-ASCII %q", value)
			}
		}
	})
}
