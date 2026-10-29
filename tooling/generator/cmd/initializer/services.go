package main

import (
	"fmt"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

// serviceEntry is one --service flag: the parsed KEY=NAME and the value as typed.
type serviceEntry struct {
	manifest.KeyValue
	raw string
}

// parseService parses value = key "=" name. Only the syntax is checked here: a
// name that fails the label grammar or a key outside the catalog is a value
// problem for the selection checks.
func parseService(value string) (manifest.KeyValue, error) {
	if value == "" {
		return manifest.KeyValue{}, fmt.Errorf("needs KEY=NAME, the value is empty")
	}
	key, name, hasName := strings.Cut(value, "=")
	switch {
	case !hasName:
		return manifest.KeyValue{}, fmt.Errorf("%q needs KEY=NAME", value)
	case key == "":
		return manifest.KeyValue{}, fmt.Errorf("%q needs a key before =NAME", value)
	case name == "":
		return manifest.KeyValue{}, fmt.Errorf("%q needs a name after =", value)
	}
	return manifest.KeyValue{Key: key, Value: name}, nil
}

// serviceValue is the repeatable flag value of --service.
type serviceValue struct{ target *[]serviceEntry }

func (v serviceValue) String() string {
	if v.target == nil {
		return ""
	}
	typed := make([]string, 0, len(*v.target))
	for _, entry := range *v.target {
		typed = append(typed, entry.raw)
	}
	return strings.Join(typed, " ")
}

func (v serviceValue) Set(value string) error {
	pair, err := parseService(value)
	if err != nil {
		return err
	}
	*v.target = append(*v.target, serviceEntry{KeyValue: pair, raw: value})
	return nil
}

// services registers --service on set, bound to target.
func (s *flagSet) services(target *[]serviceEntry, spec flagSpec) {
	spec.Value = serviceValue{target}
	spec.Repeatable = true
	s.add(spec)
}

// duplicateServiceProblems reports each --service whose KEY repeats an earlier
// one.
func duplicateServiceProblems(entries []serviceEntry) []Problem {
	var problems []Problem
	seen := map[string]bool{}
	for _, entry := range entries {
		if seen[entry.Key] {
			problems = append(problems, Problem{
				Arg:     "--service " + entry.raw,
				Message: fmt.Sprintf("service %q is given twice", entry.Key),
			})
		}
		seen[entry.Key] = true
	}
	return problems
}

// serviceFlags returns the --service values as flag inputs, in flag order.
func serviceFlags(entries []serviceEntry) []manifest.KeyValue {
	pairs := make([]manifest.KeyValue, 0, len(entries))
	for _, entry := range entries {
		pairs = append(pairs, entry.KeyValue)
	}
	return pairs
}

// preferredServiceOrder is the order the dry run lists the services in: the
// compose start order of the reference services. Other keys follow sorted.
var preferredServiceOrder = []string{"postgres", "postgres-init", "kratos-migrate", "kratos"}
