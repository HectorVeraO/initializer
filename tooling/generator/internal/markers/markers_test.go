package markers_test

import (
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/markers"
)

func only(ids ...string) func(string) bool {
	return func(id string) bool {
		for _, want := range ids {
			if id == want {
				return true
			}
		}
		return false
	}
}

func TestDropRemovesAbsentBlocks(t *testing.T) {
	cases := []struct {
		name      string
		path      string
		src       string
		installed []string
		want      string
	}{
		{
			name: "yaml block with preceding blank line",
			path: "a/compose.yml",
			src:  "a: 1\n\n# initializer:module x\nb: 2\n# initializer:end x\nc: 3\n",
			want: "a: 1\nc: 3\n",
		},
		{
			name: "non-blank preceding line is kept",
			path: "a.env",
			src:  "A=1\n# initializer:module x\nB=2\n# initializer:end x\n\nC=3\n",
			want: "A=1\n\nC=3\n",
		},
		{
			name:      "installed block keeps every byte, markers included",
			path:      "a.yaml",
			src:       "a: 1\n\n# initializer:module x\nb: 2\n# initializer:end x\n",
			installed: []string{"x"},
			want:      "a: 1\n\n# initializer:module x\nb: 2\n# initializer:end x\n",
		},
		{
			name: "jsonc block",
			path: "ng-web/proxy.conf.json",
			src:  "{\n  \"/a\": {},\n  // initializer:module x\n  \"/b\": {},\n  // initializer:end x\n  \"/c\": {}\n}\n",
			want: "{\n  \"/a\": {},\n  \"/c\": {}\n}\n",
		},
		{
			name: "markdown block",
			path: "README.md",
			src:  "# T\n\n<!-- initializer:module x -->\n## X\n\ntext\n<!-- initializer:end x -->\n\n## Y\n",
			want: "# T\n\n## Y\n",
		},
		{
			name: "indented marker and last line without newline",
			path: "nginx.conf.template",
			src:  "a\n\n    # initializer:module x\n    b\n    # initializer:end x",
			want: "a\n",
		},
		{
			name:      "only the absent module is dropped",
			path:      "a.sh",
			src:       "# initializer:module x\nx\n# initializer:end x\n# initializer:module y\ny\n# initializer:end y\n",
			installed: []string{"y"},
			want:      "# initializer:module y\ny\n# initializer:end y\n",
		},
		{
			name: "file without markers is unchanged",
			path: "a.yml",
			src:  "a: 1\n",
			want: "a: 1\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := markers.Drop(tc.path, []byte(tc.src), only(tc.installed...))
			if err != nil {
				t.Fatalf("Drop: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseReportsInvalidMarkers(t *testing.T) {
	cases := []struct {
		name string
		path string
		src  string
		want string
	}{
		{"unclosed", "a.yml", "# initializer:module x\na: 1\n", `opening marker for x has no closing marker (line 1)`},
		{"unopened", "a.yml", "a: 1\n# initializer:end x\n", `closing marker for x has no opening marker (line 2)`},
		{"nested", "a.yml", "# initializer:module x\n# initializer:module y\n", `markers do not nest: y opens inside x (line 2)`},
		{"mismatched close", "a.yml", "# initializer:module x\n# initializer:end y\n# initializer:end x\n", `closing marker names y, the open block is x (line 2)`},
		{"two spaces", "a.yml", "# initializer:module  x\n# initializer:end x\n", `marker must be exactly "# initializer:module <id>" (line 1)`},
		{"wrong comment token", "a.yml", "// initializer:module x\n# initializer:end x\n", `marker must be exactly "# initializer:module <id>" (line 1)`},
		{"hash in jsonc", "ng-web/proxy.conf.json", "# initializer:module x\n", `marker must be exactly "// initializer:module <id>" (line 1)`},
		{"bad markdown close", "a.md", "<!-- initializer:end x-->\n", `marker must be exactly "<!-- initializer:end <id> -->" (line 1)`},
		{"uppercase id", "a.yml", "# initializer:module X\n", `marker must be exactly "# initializer:module <id>" (line 1)`},
		{"id over 31 characters", "a.yml", "# initializer:module " + strings.Repeat("a", 32) + "\n", `marker must be exactly "# initializer:module <id>" (line 1)`},
		{"core", "a.yml", "# initializer:module core\n# initializer:end core\n", `a marker may not name core (line 1)`},
		{"disallowed kind", "a.json", "# initializer:module x\n", `markers are not allowed in this file kind (line 1)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, issues := markers.Parse(tc.path, []byte(tc.src))
			found := false
			for _, got := range issues {
				if got.Code != markers.CodeMarkerInvalid {
					t.Errorf("code %q, want %q", got.Code, markers.CodeMarkerInvalid)
				}
				if got.Path != tc.path+"#" {
					t.Errorf("path %q, want %q", got.Path, tc.path+"#")
				}
				if got.Message == tc.want {
					found = true
				}
			}
			if !found {
				t.Errorf("want message %q in %+v", tc.want, issues)
			}
			if _, err := markers.Drop(tc.path, []byte(tc.src), only()); err == nil {
				t.Error("Drop must fail on a malformed file")
			}
		})
	}
}

func TestParseReturnsBlocksInFileOrder(t *testing.T) {
	src := "a\n# initializer:module x\nb\n# initializer:end x\n# initializer:module y-z\n# initializer:end y-z\n"
	blocks, issues := markers.Parse("a.env", []byte(src))
	if len(issues) != 0 {
		t.Fatalf("issues: %+v", issues)
	}
	want := []markers.Block{{Module: "x", Start: 1, End: 3}, {Module: "y-z", Start: 4, End: 5}}
	if len(blocks) != len(want) || blocks[0] != want[0] || blocks[1] != want[1] {
		t.Errorf("got %+v, want %+v", blocks, want)
	}
}

func TestHasCandidateIgnoresProseMentioningTheGrammar(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"mid-sentence mention", "Blocks use `# initializer:module <id>` lines.\n", false},
		{"comment mentioning it later", "# see the initializer:module grammar\n", false},
		{"word boundary", "# initializer:modules\n", false},
		{"valid marker", "x\n  # initializer:module a\n", true},
		{"wrong form still a candidate", "#   initializer:end\n", true},
		{"markdown form", "<!-- initializer:end a -->\n", true},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := markers.HasCandidate([]byte(tc.src)); got != tc.want {
				t.Errorf("HasCandidate = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSyntaxForCoversTheMarkerKinds(t *testing.T) {
	cases := map[string]string{
		".deploy/local/docker-compose.yml": "# initializer:module a",
		".deploy/local/.example.env":       "# initializer:module a",
		"ng-web/nginx.conf.template":       "# initializer:module a",
		"ng-web/proxy.conf.json":           "// initializer:module a",
		".deploy/local/README.md":          "<!-- initializer:module a -->",
		"x/y.sh":                           "# initializer:module a",
	}
	for p, want := range cases {
		syntax, ok := markers.SyntaxFor(p)
		if !ok || syntax.Open("a") != want {
			t.Errorf("SyntaxFor(%q) ok=%v, want open %q", p, ok, want)
		}
	}
	for _, p := range []string{"a.json", "a.go", "Makefile", "ng-web/package.json"} {
		if _, ok := markers.SyntaxFor(p); ok {
			t.Errorf("SyntaxFor(%q) must be false", p)
		}
	}
}

func FuzzDrop(f *testing.F) {
	seeds := []string{
		"",
		"a\n\n# initializer:module x\nb\n# initializer:end x\n",
		"# initializer:module x\n",
		"# initializer:module core\n# initializer:end core\n",
		"  # initializer:module a-b\n\n# initializer:end a-b",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	paths := []string{"a.yml", "ng-web/proxy.conf.json", "a.md"}
	f.Fuzz(func(t *testing.T, text string) {
		for _, p := range paths {
			src := []byte(text)
			kept, err := markers.Drop(p, src, func(string) bool { return true })
			if err == nil && string(kept) != text {
				t.Fatalf("%s: installed=true changed the bytes", p)
			}
			dropped, err := markers.Drop(p, src, func(string) bool { return false })
			if err != nil {
				continue
			}
			if markers.HasCandidate(dropped) {
				t.Fatalf("%s: installed=false left a candidate line in %q", p, dropped)
			}
		}
	})
}
