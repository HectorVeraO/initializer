package migrationlint_test

import (
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/gates/internal/migrationlint"
)

func texts(src string) []string {
	var out []string
	for _, s := range migrationlint.Scan([]byte(src)) {
		out = append(out, s.Text)
	}
	return out
}

func TestScanSplitsAndStrips(t *testing.T) {
	cases := map[string]struct {
		src  string
		want []string
	}{
		"split":               {"A;\nB;", []string{"A", "B"}},
		"no trailing semi":    {"A", []string{"A"}},
		"empty statements":    {";;  ;\n", nil},
		"line comment":        {"A -- B;\nC;", []string{"A C"}},
		"block comment":       {"A /* ; B */ C;", []string{"A C"}},
		"nested block":        {"A /* x /* y */ ; */ C;", []string{"A C"}},
		"string":              {"A 'x;y -- z' B;", []string{"A '' B"}},
		"doubled quote":       {"A 'it''s;' B;", []string{"A '' B"}},
		"escape string":       {`A E'it\'s;' B;`, []string{"A E'' B"}},
		"quoted identifier":   {`A "x;--y" B;`, []string{`A "x;--y" B`}},
		"dollar body":         {"A $$ x; y $$ B;", []string{"A $$ B"}},
		"dollar tag":          {"A $body$ x; $$ y $body$ B;", []string{"A $$ B"}},
		"dollar param":        {"A $1 B;", []string{"A $1 B"}},
		"unterminated block":  {"A /* B;", []string{"A"}},
		"unterminated str":    {"A 'B;", []string{"A ''"}},
		"unterminated dollar": {"A $$ B;", []string{"A $$"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := texts(tc.src)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestScanLinesAndContractMarker(t *testing.T) {
	src := "-- initializer:contract-step why\nA;\n\nB; C;\n-- initializer:contract-step\nD;\n-- initializer:contract-stepx why\nE;\nF -- initializer:contract-step why\n;\n-- initializer:contract-step ok\n/* c */ G;"
	got := migrationlint.Scan([]byte(src))
	type row struct {
		line   int
		text   string
		marked bool
	}
	want := []row{{2, "A", true}, {4, "B", false}, {4, "C", false}, {6, "D", false}, {8, "E", false}, {9, "F", false}, {12, "G", true}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, w := range want {
		g := got[i]
		if g.Line != w.line || g.Text != w.text || g.ContractStep != w.marked {
			t.Fatalf("statement %d = %+v, want %+v", i, g, w)
		}
	}
}

func FuzzScan(f *testing.F) {
	seeds := []string{"", "A;", "'", "$$", "$a$ x", "/*", "--", "E'\\", "\"", "CREATE TABLE sys_x (a int);"}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	const commentPrefix = "/* CREATE TABLE zzzz_hidden (a int); */\n"
	const stringPrefix = "SELECT 'DROP TABLE zzzz_a; CREATE TABLE zzzz_b';\n"
	f.Fuzz(func(t *testing.T, src []byte) {
		plain := migrationlint.Scan(src)
		for _, s := range plain {
			if s.Line < 1 || s.Text == "" {
				t.Fatalf("bad statement %+v", s)
			}
		}
		withComment := migrationlint.Scan(append([]byte(commentPrefix), src...))
		if len(withComment) != len(plain) {
			t.Fatalf("a leading comment changed the statement count: %d vs %d", len(withComment), len(plain))
		}
		for i := range plain {
			if withComment[i].Text != plain[i].Text {
				t.Fatalf("comment leaked into statement %d: %q vs %q", i, withComment[i].Text, plain[i].Text)
			}
		}
		withString := migrationlint.Scan(append([]byte(stringPrefix), src...))
		if len(withString) != len(plain)+1 || withString[0].Text != "SELECT ''" {
			t.Fatalf("string leaked: %+v", withString)
		}
		for _, s := range withString {
			if strings.Contains(s.Text, "zzzz_") {
				t.Fatalf("stripped table name surfaced: %q", s.Text)
			}
		}
	})
}
