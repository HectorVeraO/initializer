package render_test

import (
	"bytes"
	"math/rand"
	"regexp"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/render"
)

func TestModulesTSGoldens(t *testing.T) {
	cases := []struct {
		golden string
		ids    []string
	}{
		{"modules.core-only.ts.golden", []string{"core"}},
		{"modules.core-parameters.ts.golden", []string{"core", "parameters"}},
		{"modules.all.ts.golden", []string{"core", "feature-flags", "parameters"}},
	}
	for _, c := range cases {
		t.Run(c.golden, func(t *testing.T) {
			got, err := render.ModulesTS(c.ids)
			if err != nil {
				t.Fatal(err)
			}
			assertGolden(t, c.golden, got)
			second, err := render.ModulesTS(c.ids)
			if err != nil || !bytes.Equal(second, got) {
				t.Fatal("rendering twice gave different bytes")
			}
			hasCR := bytes.Contains(got, []byte("\r"))
			endsOnce := bytes.HasSuffix(got, []byte("];\n")) && !bytes.HasSuffix(got, []byte("\n\n"))
			if hasCR || !endsOnce {
				t.Fatalf("output must use LF and end with one newline:\n%q", got)
			}
		})
	}
}

func TestModulesTSKeepsTheGivenOrder(t *testing.T) {
	got, err := render.ModulesTS([]string{"core", "zeta", "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "[coreModule, zetaModule, alphaModule]") {
		t.Fatalf("order was not kept:\n%s", got)
	}
}

func TestModulesTSRefusesBadInput(t *testing.T) {
	cases := map[string][]string{
		"empty":            nil,
		"core not first":   {"parameters", "core"},
		"duplicate":        {"core", "parameters", "parameters"},
		"uppercase id":     {"core", "Params"},
		"quote in id":      {"core", "a'b"},
		"path in id":       {"core", "../x"},
		"trailing hyphen":  {"core", "a-"},
		"double hyphen":    {"core", "a--b"},
		"newline in id":    {"core", "a\nb"},
		"leading digit id": {"core", "1a"},
		"same symbol":      {"core", "a-1b", "a1b"},
	}
	for name, ids := range cases {
		t.Run(name, func(t *testing.T) {
			if got, err := render.ModulesTS(ids); err == nil {
				t.Fatalf("expected an error, got:\n%s", got)
			}
		})
	}
}

func TestWebSymbolExamples(t *testing.T) {
	cases := map[string]string{
		"core":          "coreModule",
		"core-auth":     "coreAuthModule",
		"feature-flags": "featureFlagsModule",
		"a1-b2-c3":      "a1B2C3Module",
	}
	for id, want := range cases {
		got, err := render.WebSymbol(id)
		if err != nil || got != want {
			t.Errorf("WebSymbol(%q) = %q, %v; want %q", id, got, err, want)
		}
	}
}

// For generated ids whose parts start with a letter, the symbol is a valid
// TypeScript identifier and the id is recoverable from it.
func TestWebSymbolIsAnIdentifierThatRoundTrips(t *testing.T) {
	identifier := regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)
	humps := regexp.MustCompile(`[A-Z]`)
	random := rand.New(rand.NewSource(7))
	const letters = "abcdefghijklmnopqrstuvwxyz"
	const alphanumerics = letters + "0123456789"
	for i := 0; i < 500; i++ {
		var parts []string
		for p := 0; p < 1+random.Intn(4); p++ {
			var part strings.Builder
			part.WriteByte(letters[random.Intn(len(letters))])
			for c := 0; c < random.Intn(6); c++ {
				part.WriteByte(alphanumerics[random.Intn(len(alphanumerics))])
			}
			parts = append(parts, part.String())
		}
		id := strings.Join(parts, "-")
		symbol, err := render.WebSymbol(id)
		if err != nil {
			t.Fatalf("WebSymbol(%q): %v", id, err)
		}
		if !identifier.MatchString(symbol) {
			t.Fatalf("%q -> %q is not an identifier", id, symbol)
		}
		stem := strings.TrimSuffix(symbol, "Module")
		recovered := strings.ToLower(humps.ReplaceAllStringFunc(stem, func(s string) string { return "-" + s }))
		if recovered != id {
			t.Fatalf("%q -> %q does not round-trip (%q)", id, symbol, recovered)
		}
	}
}

func FuzzWebSymbol(f *testing.F) {
	for _, seed := range []string{"core", "feature-flags", "", "-", "A", "a--b", "a\x00"} {
		f.Add(seed)
	}
	identifier := regexp.MustCompile(`^[a-z][A-Za-z0-9]*Module$`)
	f.Fuzz(func(t *testing.T, id string) {
		symbol, err := render.WebSymbol(id)
		if err != nil {
			return
		}
		if !identifier.MatchString(symbol) {
			t.Fatalf("%q -> %q is not an identifier", id, symbol)
		}
	})
}
