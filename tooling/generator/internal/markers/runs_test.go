package markers_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/markers"
)

func counts(byStack map[string]int) func(string) int {
	return func(stack string) int {
		return byStack[stack]
	}
}

// tag marks every copy with its stack and index, so a test reads the order.
func tag(r markers.Run, index int, seg []byte) ([]byte, error) {
	return []byte(fmt.Sprintf("[%s %d]\n%s", r.Stack, index, seg)), nil
}

func identityOutside(seg []byte) ([]byte, error) {
	return seg, nil
}

func parse(t *testing.T, p, src string) []markers.Block {
	t.Helper()
	blocks, issues := markers.Parse(p, []byte(src))
	if len(issues) > 0 {
		t.Fatalf("Parse: %+v", issues)
	}
	return blocks
}

func TestRunsGroupDirectlyAdjacentBlocksThatStartWithAStackBlock(t *testing.T) {
	src := strings.Join([]string{
		"head",
		"# initializer:module m1",
		"x",
		"# initializer:end m1",
		"",
		"",
		"# initializer:stack go-srv",
		"a",
		"# initializer:end go-srv",
		"# initializer:module m2",
		"b",
		"# initializer:end m2",
		"# initializer:stack ng-web",
		"c",
		"# initializer:end ng-web",
		"",
		"# initializer:stack ng-web",
		"d",
		"# initializer:end ng-web",
		"",
	}, "\n")
	runs := markers.Runs([]byte(src), parse(t, "a.yml", src))
	if len(runs) != 2 {
		t.Fatalf("runs = %+v, want 2", runs)
	}
	first, second := runs[0], runs[1]
	if first.Stack != "go-srv" || first.Start != 6 || first.End != 14 || first.Blank != 2 || len(first.Blocks) != 3 {
		t.Errorf("first run = %+v", first)
	}
	if second.Stack != "ng-web" || second.Start != 16 || second.End != 18 || second.Blank != 1 || len(second.Blocks) != 1 {
		t.Errorf("second run = %+v", second)
	}
}

func TestRunsIgnoreAModuleBlockThatIsNotInsideARun(t *testing.T) {
	src := "# initializer:module m1\nx\n# initializer:end m1\n# initializer:stack go-srv\na\n# initializer:end go-srv\n"
	runs := markers.Runs([]byte(src), parse(t, "a.yml", src))
	if len(runs) != 1 || runs[0].Start != 3 || runs[0].Blank != 0 {
		t.Fatalf("runs = %+v, want the one run that starts at the stack block", runs)
	}
}

func TestExpandEmitsEachRunOncePerComponentInIndexOrder(t *testing.T) {
	const yaml = "# initializer:stack go-srv\nsvc\n# initializer:end go-srv\n"
	cases := []struct {
		name  string
		src   string
		count int
		want  string
	}{
		{"zero copies, no blank", "top\n" + yaml + "tail\n", 0, "top\ntail\n"},
		{"zero copies drop one preceding blank", "top\n\n" + yaml + "tail\n", 0, "top\ntail\n"},
		{"zero copies keep the second blank", "top\n\n\n" + yaml + "tail\n", 0, "top\n\ntail\n"},
		{"one copy stays in place", "top\n\n" + yaml + "tail\n", 1, "top\n\n[go-srv 0]\n" + yaml + "tail\n"},
		{"two copies, no blank above", "top\n" + yaml + "tail\n", 2, "top\n[go-srv 0]\n" + yaml + "[go-srv 1]\n" + yaml + "tail\n"},
		{"two copies, one blank above", "top\n\n" + yaml, 2, "top\n\n[go-srv 0]\n" + yaml + "\n[go-srv 1]\n" + yaml},
		{"three copies, two blanks above", "top\n\n\n" + yaml, 3,
			"top\n\n\n[go-srv 0]\n" + yaml + "\n\n[go-srv 1]\n" + yaml + "\n\n[go-srv 2]\n" + yaml},
		{"a run at the start of the file", yaml + "tail\n", 2, "[go-srv 0]\n" + yaml + "[go-srv 1]\n" + yaml + "tail\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := markers.Expand("a.yml", []byte(tc.src), counts(map[string]int{"go-srv": tc.count}), tag, identityOutside)
			if err != nil {
				t.Fatalf("Expand: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

func TestExpandSeparatesCopiesOfARunAtTheEndOfAFileWithoutANewline(t *testing.T) {
	src := "top\n# initializer:stack go-srv\nsvc\n# initializer:end go-srv"
	got, err := markers.Expand("a.yml", []byte(src), counts(map[string]int{"go-srv": 2}), func(r markers.Run, _ int, seg []byte) ([]byte, error) {
		return seg, nil
	}, identityOutside)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	want := "top\n# initializer:stack go-srv\nsvc\n# initializer:end go-srv\n# initializer:stack go-srv\nsvc\n# initializer:end go-srv"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExpandTomlAndMarkdownSources(t *testing.T) {
	toml := "[tasks.up]\nrun = \"a\"\n\n# initializer:stack go-srv\n[tasks.dev]\n# initializer:end go-srv\n"
	got, err := markers.Expand("mise.toml", []byte(toml), counts(map[string]int{"go-srv": 2}), tag, identityOutside)
	if err != nil {
		t.Fatalf("toml: %v", err)
	}
	want := "[tasks.up]\nrun = \"a\"\n\n[go-srv 0]\n# initializer:stack go-srv\n[tasks.dev]\n# initializer:end go-srv\n\n[go-srv 1]\n# initializer:stack go-srv\n[tasks.dev]\n# initializer:end go-srv\n"
	if string(got) != want {
		t.Errorf("toml got %q", got)
	}
	table := "| Port | Service |\n|---|---|\n<!-- initializer:stack go-srv -->\n| 1 | a |\n<!-- initializer:end go-srv -->\n"
	got, err = markers.Expand("README.md", []byte(table), counts(map[string]int{"go-srv": 3}), func(_ markers.Run, _ int, seg []byte) ([]byte, error) {
		return seg, nil
	}, identityOutside)
	if err != nil {
		t.Fatalf("markdown: %v", err)
	}
	if strings.Count(string(got), "| 1 | a |") != 3 || strings.Count(string(got), "| Port | Service |") != 1 {
		t.Errorf("a table run with no blank above must extend one table:\n%s", got)
	}
}

func TestExpandPassesEverySegmentThroughItsCallback(t *testing.T) {
	src := "a\n# initializer:module m\nb\n# initializer:end m\n\n# initializer:stack go-srv\nc\n# initializer:end go-srv\n\nz\n"
	got, err := markers.Expand("a.yml", []byte(src), counts(map[string]int{"go-srv": 2}),
		func(r markers.Run, index int, seg []byte) ([]byte, error) {
			return []byte(fmt.Sprintf("<%s/%d>", r.Stack, index)), nil
		},
		func(seg []byte) ([]byte, error) {
			return []byte(strings.ToUpper(string(seg))), nil
		})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	out := string(got)
	if strings.Count(out, "<go-srv/") != 2 || !strings.Contains(out, "<go-srv/0>") || !strings.Contains(out, "<go-srv/1>") {
		t.Errorf("the run must be copied once per index:\n%s", out)
	}
	if !strings.Contains(out, "A\n# INITIALIZER:MODULE M\nB") || !strings.HasSuffix(out, "Z\n") {
		t.Errorf("text outside the run must pass through the outside callback:\n%s", out)
	}
	if strings.Contains(out, "c\n") {
		t.Errorf("the run body must be replaced by the copy callback:\n%s", out)
	}
}

func TestExpandRefusesBadInput(t *testing.T) {
	good := counts(map[string]int{"go-srv": 1})
	if _, err := markers.Expand("a.yml", []byte("# initializer:stack go-srv\n"), good, tag, identityOutside); err == nil {
		t.Error("an unclosed block must fail")
	}
	src := []byte("# initializer:stack go-srv\n# initializer:end go-srv\n")
	if _, err := markers.Expand("a.yml", src, counts(map[string]int{"go-srv": -1}), tag, identityOutside); err == nil {
		t.Error("a negative count must fail")
	}
	if _, err := markers.Expand("a.yml", src, nil, tag, identityOutside); err == nil {
		t.Error("a nil count must fail")
	}
	failing := func(markers.Run, int, []byte) ([]byte, error) { return nil, fmt.Errorf("boom") }
	if _, err := markers.Expand("a.yml", src, good, failing, identityOutside); err == nil {
		t.Error("a failing callback must fail")
	}
}

func FuzzRuns(f *testing.F) {
	seeds := []string{
		"",
		"a\n\n# initializer:stack go-srv\nb\n# initializer:end go-srv\n",
		"# initializer:stack go-srv\n# initializer:module m\n# initializer:end m\n# initializer:end go-srv\n",
		"# initializer:stack go-srv\nx\n# initializer:end go-srv\n# initializer:module m\ny\n# initializer:end m\n# initializer:stack ng-web\nz\n# initializer:end ng-web",
		"\n\n\n# initializer:stack ng-web\n# initializer:end ng-web\n\n\n# initializer:stack ng-web\n# initializer:end ng-web\n",
		"# initializer:module m\n# initializer:stack go-srv\n",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	paths := []string{"a.yml", "a.md", "mise.toml"}
	f.Fuzz(func(t *testing.T, text string) {
		for _, p := range paths {
			src := []byte(text)
			blocks, issues := markers.Parse(p, src)
			if len(issues) > 0 {
				continue
			}
			for _, run := range markers.Runs(src, blocks) {
				if run.Start > run.End || run.Blank < 0 || run.Blank > run.Start || len(run.Blocks) == 0 {
					t.Fatalf("%s: malformed run %+v in %q", p, run, text)
				}
			}
			// One copy of every run and identity callbacks must give src back.
			got, err := markers.Expand(p, src, func(string) int { return 1 },
				func(_ markers.Run, _ int, seg []byte) ([]byte, error) { return seg, nil }, identityOutside)
			if err != nil {
				t.Fatalf("%s: Expand failed on a file Parse accepts: %v", p, err)
			}
			if string(got) != text {
				t.Fatalf("%s: one copy per run changed %q into %q", p, text, got)
			}
			// No copy at all must never grow the file.
			none, err := markers.Expand(p, src, func(string) int { return 0 },
				func(_ markers.Run, _ int, seg []byte) ([]byte, error) { return seg, nil }, identityOutside)
			if err != nil || len(none) > len(src) {
				t.Fatalf("%s: zero copies grew %q into %q (%v)", p, text, none, err)
			}
		}
	})
}
