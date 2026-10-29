package main

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Problem is one command-line problem (exit 2).
type Problem struct {
	// Arg is the argument as typed; empty when the problem is not about one argument.
	Arg     string `json:"arg"`
	Message string `json:"message"`
	// Suggestion is a close flag, command or key, or "".
	Suggestion string `json:"suggestion,omitempty"`
}

// flagSpec is the help metadata of one flag; the Value is registered on a flag.FlagSet.
type flagSpec struct {
	Name, Short, Placeholder, Usage, Default string
	Hidden, Repeatable, Bool                 bool
	// Deprecated is the warning text of a hidden alias; empty for a real flag.
	Deprecated string
	Value      flag.Value
}

// flagSet wraps flag.FlagSet with help metadata and the count of times scan
// saw each flag (even with a missing or invalid value), keyed by long name.
type flagSet struct {
	fs    *flag.FlagSet
	specs []flagSpec
	given map[string]int
}

func newFlagSet(name string) *flagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return &flagSet{fs: fs, given: map[string]int{}}
}

// add registers spec under its long name and its short name.
func (s *flagSet) add(spec flagSpec) {
	if spec.Name == "" || spec.Value == nil {
		panic("flag spec needs a name and a value")
	}
	s.fs.Var(spec.Value, spec.Name, spec.Usage)
	if spec.Short != "" {
		s.fs.Var(spec.Value, spec.Short, spec.Usage)
	}
	s.specs = append(s.specs, spec)
}

// str registers a string flag bound to target.
func (s *flagSet) str(target *string, spec flagSpec) {
	spec.Value = stringValue{target}
	s.add(spec)
}

// boolean registers a bool flag bound to target.
func (s *flagSet) boolean(target *bool, spec flagSpec) {
	spec.Value = boolValue{target}
	spec.Bool = true
	s.add(spec)
}

// lookup returns the spec registered under name, a long or a short name.
func (s *flagSet) lookup(name string) *flagSpec {
	for i := range s.specs {
		if name != "" && (s.specs[i].Name == name || s.specs[i].Short == name) {
			return &s.specs[i]
		}
	}
	return nil
}

// visibleNames returns the long names of the flags help lists, as typed ("--name").
func (s *flagSet) visibleNames() []string {
	var names []string
	for _, spec := range s.specs {
		if !spec.Hidden {
			names = append(names, "--"+spec.Name)
		}
	}
	return names
}

type stringValue struct{ target *string }

func (v stringValue) String() string {
	if v.target == nil {
		return ""
	}
	return *v.target
}

func (v stringValue) Set(value string) error {
	*v.target = value
	return nil
}

type boolValue struct{ target *bool }

func (v boolValue) String() string {
	if v.target == nil {
		return "false"
	}
	return strconv.FormatBool(*v.target)
}

func (v boolValue) Set(value string) error {
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("%q is not true or false", value)
	}
	*v.target = parsed
	return nil
}

func (v boolValue) IsBoolFlag() bool { return true }

// looksLikeFlag reports whether arg starts a flag; a lone "-" is a value.
func looksLikeFlag(arg string) bool {
	return len(arg) > 1 && arg[0] == '-'
}

// scan walks args once against set: it separates positionals (anywhere, and
// everything after "--"), resolves --name=value and --name value, and collects
// every unknown flag, missing value, repeated non-repeatable flag and Set error
// instead of stopping at the first. A flag's value is the next argument unless
// that one looks like a flag; write --name=-x for a value that starts with a dash.
func scan(set *flagSet, args []string) (positionals []string, problems []Problem) {
	set.given = map[string]int{}
	flagsEnded := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if flagsEnded || !looksLikeFlag(arg) {
			positionals = append(positionals, arg)
			continue
		}
		if arg == "--" {
			flagsEnded = true
			continue
		}
		name := strings.TrimPrefix(strings.TrimPrefix(arg, "-"), "-")
		name, value, hasValue := strings.Cut(name, "=")
		spec := set.lookup(name)
		if spec == nil {
			problems = append(problems, Problem{
				Arg:        arg,
				Message:    "unknown flag",
				Suggestion: suggest("--"+name, set.visibleNames()),
			})
			continue
		}
		set.given[spec.Name]++
		if !hasValue && spec.Bool {
			value = "true"
		} else if !hasValue {
			hasNext := i+1 < len(args) && !looksLikeFlag(args[i+1])
			if !hasNext {
				problems = append(problems, Problem{Arg: arg, Message: "needs a value"})
				continue
			}
			i++
			value = args[i]
		}
		if set.given[spec.Name] > 1 && !spec.Repeatable {
			problems = append(problems, Problem{Arg: arg, Message: "is given twice"})
			continue
		}
		if err := spec.Value.Set(value); err != nil {
			problems = append(problems, Problem{Arg: arg, Message: "invalid value: " + err.Error()})
		}
	}
	return positionals, problems
}

// suggest returns the candidate closest to word: a unique prefix match, else
// the smallest Levenshtein distance not above max(1, len(word)/3); ties go to
// the earlier candidate; "" when none qualifies.
func suggest(word string, candidates []string) string {
	if word == "" {
		return ""
	}
	prefixed, matches := "", 0
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, word) {
			prefixed = candidate
			matches++
		}
	}
	if matches == 1 {
		return prefixed
	}
	bound := max(1, len([]rune(word))/3)
	best, bestDistance := "", bound+1
	for _, candidate := range candidates {
		distance := levenshtein(word, candidate)
		if distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	return best
}

// levenshtein returns the edit distance between a and b, counted in runes.
func levenshtein(a, b string) int {
	left, right := []rune(a), []rune(b)
	previous := make([]int, len(right)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(left); i++ {
		current := make([]int, len(right)+1)
		current[0] = i
		for j := 1; j <= len(right); j++ {
			cost := 1
			if left[i-1] == right[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(right)]
}
