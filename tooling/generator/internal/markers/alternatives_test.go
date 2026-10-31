package markers_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/markers"
)

func TestAModuleMarkerMayListAlternativeIds(t *testing.T) {
	src := "a: 1\n# initializer:module identity|multi\nb: 2\n# initializer:end identity|multi\n"
	blocks, issues := markers.Parse("a.yml", []byte(src))
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	if len(blocks) != 1 || !reflect.DeepEqual(blocks[0].IDs(), []string{"identity", "multi"}) {
		t.Fatalf("blocks = %+v", blocks)
	}
	if blocks[0].Ref() != "module:identity|multi" {
		t.Errorf("ref = %q", blocks[0].Ref())
	}
	kind, ids := markers.SplitRef(blocks[0].Ref())
	if kind != "module" || !reflect.DeepEqual(ids, []string{"identity", "multi"}) {
		t.Errorf("SplitRef = %q %v", kind, ids)
	}
}

func TestABlockWithAlternativesIsKeptWhenAnyIsInstalled(t *testing.T) {
	src := "a: 1\n# initializer:module identity|multi\nb: 2\n# initializer:end identity|multi\nc: 3\n"
	cases := []struct {
		name      string
		installed []string
		want      string
	}{
		{"first alone", []string{"identity"}, src},
		{"second alone", []string{markers.MultiID}, src},
		{"both", []string{"identity", markers.MultiID}, src},
		{"neither", []string{"tenancy"}, "a: 1\nc: 3\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := markers.Drop("a.yml", []byte(src), keepOnly(tc.installed, nil))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAlternativeIdsFollowTheIdGrammar(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"empty alternative", "# initializer:module identity|\n# initializer:end identity|\n", `marker must be exactly "# initializer:module <id>" (line 1)`},
		{"uppercase alternative", "# initializer:module identity|Multi\n", `marker must be exactly "# initializer:module <id>" (line 1)`},
		{"alternatives on a stack", "# initializer:stack go-srv|ng-web\n# initializer:end go-srv|ng-web\n", `marker must be exactly "# initializer:stack <id>" (line 1)`},
		{"core among alternatives", "# initializer:module identity|core\n# initializer:end identity|core\n", `a marker may not name core (line 1)`},
		{"close names another list", "# initializer:module identity|multi\n# initializer:end identity\n", `closing marker names identity, the open block is identity|multi (line 2)`},
		{"id list over 63 characters", "# initializer:module " + strings.Repeat("a", 31) + "|" + strings.Repeat("b", 31) + "|c\n", `marker must be exactly "# initializer:module <id>" (line 1)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, issues := markers.Parse("a.yml", []byte(tc.src))
			found := false
			for _, got := range issues {
				found = found || got.Message == tc.want
			}
			if !found {
				t.Errorf("want message %q in %+v", tc.want, issues)
			}
		})
	}
}

// A run keeps its place when a block with alternatives sits inside it: the
// module blocks of a run copy follow that component's modules, and multi is
// true in every copy of a multi project.
func TestABlockWithAlternativesInsideARunFollowsTheComponent(t *testing.T) {
	src := "# initializer:stack go-srv\n# initializer:end go-srv\n# initializer:module identity|multi\nrole\n# initializer:end identity|multi\n"
	blocks, issues := markers.Parse("a.yml", []byte(src))
	if len(issues) != 0 {
		t.Fatal(issues)
	}
	runs := markers.Runs([]byte(src), blocks)
	if len(runs) != 1 || len(runs[0].Blocks) != 2 {
		t.Fatalf("runs = %+v", runs)
	}
	copyOf := func(modules ...string) (roles, copies int) {
		out, err := markers.Expand("a.yml", []byte(src), func(string) int { return 2 },
			func(_ markers.Run, _ int, seg []byte) ([]byte, error) {
				return markers.Drop("a.yml", seg, keepOnly(modules, []string{"go-srv"}))
			},
			func(seg []byte) ([]byte, error) { return seg, nil })
		if err != nil {
			t.Fatal(err)
		}
		return strings.Count(string(out), "role\n"), strings.Count(string(out), "# initializer:stack go-srv\n")
	}
	if roles, copies := copyOf(); roles != 0 || copies != 2 {
		t.Errorf("no module installed: %d roles in %d copies, want two empty copies and no role", roles, copies)
	}
	if roles, copies := copyOf(markers.MultiID); roles != 2 || copies != 2 {
		t.Errorf("multi: %d roles in %d copies, want two copies with a role each", roles, copies)
	}
}
