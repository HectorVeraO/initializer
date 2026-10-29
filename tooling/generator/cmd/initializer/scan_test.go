package main

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// scanTarget holds the values bound to the flag set newScanSet builds.
type scanTarget struct {
	name, file string
	tags       []string
	all        bool
	count      string
}

// listValue appends every value it is given.
type listValue struct{ target *[]string }

func (v listValue) String() string { return "" }

func (v listValue) Set(value string) error {
	*v.target = append(*v.target, value)
	return nil
}

// evenValue accepts only even-length strings.
type evenValue struct{ target *string }

func (v evenValue) String() string { return "" }

func (v evenValue) Set(value string) error {
	if len(value)%2 != 0 {
		return errors.New("length must be even")
	}
	*v.target = value
	return nil
}

func newScanSet() (*flagSet, *scanTarget) {
	target := &scanTarget{}
	set := newFlagSet("test")
	set.str(&target.name, flagSpec{Name: "name"})
	set.str(&target.file, flagSpec{Name: "file", Short: "f"})
	set.add(flagSpec{Name: "tag", Repeatable: true, Value: listValue{&target.tags}})
	set.add(flagSpec{Name: "count", Value: evenValue{&target.count}})
	set.boolean(&target.all, flagSpec{Name: "all-modules"})
	set.str(new(string), flagSpec{Name: "secret", Hidden: true})
	return set, target
}

func TestScanSeparatesPositionalsFromFlagsAnywhere(t *testing.T) {
	set, target := newScanSet()
	positionals, problems := scan(set, []string{"first", "--name", "acme", "second", "--all-modules", "third"})
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	if !reflect.DeepEqual(positionals, []string{"first", "second", "third"}) {
		t.Fatalf("positionals = %v", positionals)
	}
	if target.name != "acme" || !target.all {
		t.Fatalf("target = %+v", target)
	}
}

func TestScanTreatsEverythingAfterDoubleDashAsPositional(t *testing.T) {
	set, target := newScanSet()
	positionals, problems := scan(set, []string{"--name", "a", "--", "--name", "-x", "--"})
	if len(problems) != 0 || target.name != "a" {
		t.Fatalf("problems %v, name %q", problems, target.name)
	}
	if !reflect.DeepEqual(positionals, []string{"--name", "-x", "--"}) {
		t.Fatalf("positionals = %v", positionals)
	}
}

func TestScanAcceptsEveryFlagSpelling(t *testing.T) {
	cases := map[string][]string{
		"space":         {"--name", "acme"},
		"equals":        {"--name=acme"},
		"single dash":   {"-name", "acme"},
		"single equals": {"-name=acme"},
	}
	for label, args := range cases {
		t.Run(label, func(t *testing.T) {
			set, target := newScanSet()
			_, problems := scan(set, args)
			if len(problems) != 0 || target.name != "acme" {
				t.Fatalf("problems %v, name %q", problems, target.name)
			}
		})
	}
}

func TestScanShortNameAndEmptyValue(t *testing.T) {
	set, target := newScanSet()
	_, problems := scan(set, []string{"-f", "in.json", "--name="})
	if len(problems) != 0 || target.file != "in.json" || set.given["name"] != 1 {
		t.Fatalf("problems %v, target %+v", problems, target)
	}
}

func TestScanBoolFlagsTakeNoSeparateValue(t *testing.T) {
	cases := map[string]struct {
		args        []string
		all         bool
		positionals []string
	}{
		"bare":          {[]string{"--all-modules"}, true, nil},
		"explicit true": {[]string{"--all-modules=true"}, true, nil},
		"explicit off":  {[]string{"--all-modules=false"}, false, nil},
		"next word":     {[]string{"--all-modules", "false"}, true, []string{"false"}},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			set, target := newScanSet()
			positionals, problems := scan(set, tc.args)
			if len(problems) != 0 || target.all != tc.all || !slices.Equal(positionals, tc.positionals) {
				t.Fatalf("all %v positionals %v problems %v", target.all, positionals, problems)
			}
		})
	}
}

func TestScanRejectsABoolValueThatIsNotABool(t *testing.T) {
	set, _ := newScanSet()
	_, problems := scan(set, []string{"--all-modules=maybe"})
	if len(problems) != 1 || problems[0].Arg != "--all-modules=maybe" || !strings.Contains(problems[0].Message, "invalid value") {
		t.Fatalf("problems = %v", problems)
	}
}

func TestScanRepeatableAndNonRepeatableFlags(t *testing.T) {
	set, target := newScanSet()
	_, problems := scan(set, []string{"--tag", "a", "--tag=b", "--name", "x", "--name", "y", "-f", "1", "--file", "2"})
	if !slices.Equal(target.tags, []string{"a", "b"}) {
		t.Fatalf("tags = %v", target.tags)
	}
	if target.name != "x" || target.file != "1" {
		t.Fatalf("a repeated flag must keep its first value: %+v", target)
	}
	want := []Problem{
		{Arg: "--name", Message: "is given twice"},
		{Arg: "--file", Message: "is given twice"},
	}
	if !reflect.DeepEqual(problems, want) {
		t.Fatalf("problems = %v, want %v", problems, want)
	}
}

func TestScanMissingValue(t *testing.T) {
	cases := map[string][]string{
		"at the end":         {"--name"},
		"before a flag":      {"--name", "--all-modules"},
		"before a short one": {"--name", "-f", "x"},
	}
	for label, args := range cases {
		t.Run(label, func(t *testing.T) {
			set, target := newScanSet()
			_, problems := scan(set, args)
			if len(problems) == 0 || problems[0].Arg != "--name" || problems[0].Message != "needs a value" {
				t.Fatalf("problems = %v", problems)
			}
			if target.name != "" {
				t.Fatalf("name must stay unset, got %q", target.name)
			}
		})
	}
}

func TestScanLoneDashIsAValue(t *testing.T) {
	set, target := newScanSet()
	positionals, problems := scan(set, []string{"-", "--file", "-"})
	if len(problems) != 0 || target.file != "-" || !slices.Equal(positionals, []string{"-"}) {
		t.Fatalf("problems %v, file %q, positionals %v", problems, target.file, positionals)
	}
}

func TestScanSetErrorIsAProblem(t *testing.T) {
	set, _ := newScanSet()
	_, problems := scan(set, []string{"--count", "odd"})
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "length must be even") {
		t.Fatalf("problems = %v", problems)
	}
}

func TestScanCollectsEveryProblemEvenAfterAPositional(t *testing.T) {
	set, _ := newScanSet()
	_, problems := scan(set, []string{"dir", "--nmae", "x", "--bogus", "--count", "ab", "--count", "cd", "--file"})
	args := make([]string, 0, len(problems))
	for _, problem := range problems {
		args = append(args, problem.Arg)
	}
	want := []string{"--nmae", "--bogus", "--count", "--file"}
	if !slices.Equal(args, want) {
		t.Fatalf("problem args = %v, want %v", args, want)
	}
	if problems[0].Suggestion != "--name" {
		t.Fatalf("--nmae must suggest --name, got %q", problems[0].Suggestion)
	}
}

func TestScanUnknownFlagSuggestsOnlyVisibleFlags(t *testing.T) {
	set, _ := newScanSet()
	_, problems := scan(set, []string{"--secre"})
	if len(problems) != 1 || problems[0].Suggestion != "" {
		t.Fatalf("a hidden flag must not be suggested: %v", problems)
	}
}

func TestScanRecountsOnEveryCall(t *testing.T) {
	set, _ := newScanSet()
	scan(set, []string{"--name", "a"})
	_, problems := scan(set, []string{"--name", "b"})
	if len(problems) != 0 {
		t.Fatalf("a second scan must start from zero: %v", problems)
	}
}

func TestSuggest(t *testing.T) {
	flags := []string{"--name", "--modules", "--stacks", "--go-module"}
	cases := map[string]struct {
		word       string
		candidates []string
		want       string
	}{
		"unique prefix":         {"--mod", flags, "--modules"},
		"ambiguous prefix":      {"--", flags, ""},
		"transposition":         {"--nmae", flags, "--name"},
		"one edit on a command": {"genrate", []string{"generate", "verify"}, "generate"},
		"at the bound":          {"verfy", []string{"verify"}, "verify"},
		"over the bound":        {"vrfy", []string{"verify", "help"}, ""},
		"far over the bound":    {"xyz", []string{"verify", "generate"}, ""},
		"short word, one edit":  {"hep", []string{"help", "verify"}, "help"},
		"short word, two edits": {"hp", []string{"help"}, ""},
		"tie goes to earlier":   {"ab", []string{"ac", "ad"}, "ac"},
		"empty word":            {"", flags, ""},
		"no candidates":         {"name", nil, ""},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			if got := suggest(tc.word, tc.candidates); got != tc.want {
				t.Fatalf("suggest(%q) = %q, want %q", tc.word, got, tc.want)
			}
		})
	}
}

func TestSuggestDistanceBoundGrowsWithTheWord(t *testing.T) {
	candidates := []string{"abcdefghi"}
	if got := suggest("abcdefxyz", candidates); got != "abcdefghi" {
		t.Fatalf("three edits on nine letters is within max(1, 9/3) = 3, got %q", got)
	}
	if got := suggest("abcdxxxxx", candidates); got != "" {
		t.Fatalf("five edits must not qualify, got %q", got)
	}
}

func FuzzScan(f *testing.F) {
	for _, seed := range []string{
		"", "a", "--name x", "--name=x --all-modules", "-- --name", "--bogus -f", "- -- -", "--tag a --tag b --count odd",
		"---x", "-=", "--=", "--all-modules=maybe",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, line string) {
		args := strings.Split(line, " ")
		set, _ := newScanSet()
		positionals, problems := scan(set, args)

		counts := map[string]int{}
		for _, arg := range args {
			counts[arg]++
		}
		for _, positional := range positionals {
			counts[positional]--
			if counts[positional] < 0 {
				t.Fatalf("positional %q is not an argument of %q", positional, args)
			}
		}
		for _, problem := range problems {
			if !slices.Contains(args, problem.Arg) {
				t.Fatalf("problem %+v does not name an argument of %q", problem, args)
			}
			if problem.Message == "" {
				t.Fatalf("problem without a message: %+v", problem)
			}
		}
		for i, arg := range args {
			isFlagToken := looksLikeFlag(arg)
			isPositional := slices.Contains(positionals, arg)
			isValue := i > 0 && looksLikeFlag(args[i-1]) && !strings.Contains(args[i-1], "=") && !isFlagToken
			if !isFlagToken && !isPositional && !isValue {
				t.Fatalf("argument %d (%q) of %q is neither a positional, a value nor a flag", i, arg, args)
			}
		}
	})
}

func FuzzSuggest(f *testing.F) {
	f.Add("--nmae", "--name,--modules")
	f.Add("", "a")
	f.Add("genrate", "generate,verify,catalog,help")
	f.Fuzz(func(t *testing.T, word, joined string) {
		candidates := strings.Split(joined, ",")
		got := suggest(word, candidates)
		if got != "" && !slices.Contains(candidates, got) {
			t.Fatalf("suggest(%q, %q) = %q, not a candidate", word, candidates, got)
		}
	})
}
