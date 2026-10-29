package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
)

// componentKeys is the key table of the --component tail: the keys a pair may
// name, besides service.<key>.
var componentKeys = []string{"modules", "go-module", "db-name", "db-user", "backend", "service.<key>"}

// serviceKeyPrefix starts the key of a service pair.
const serviceKeyPrefix = "service."

// componentEntry is one --component flag: the parsed head and keys, the keys
// that were given and the value as typed.
type componentEntry struct {
	manifest.Component
	set map[string]bool
	raw string
}

// componentProblem is why a --component value is not valid command-line syntax.
type componentProblem struct {
	message    string
	suggestion string
}

func (p *componentProblem) Error() string {
	return p.message
}

// ParseComponentFlag parses value = head *( "," pair ), head = stack [ "=" name ],
// pair = key "=" val. Only the syntax is checked here: an unknown stack, a name
// that fails the label grammar or a key the role does not take is a value
// problem for the selection checks. Index is the caller's.
func ParseComponentFlag(value string) (manifest.ComponentFlag, *componentProblem) {
	flag := manifest.ComponentFlag{Arg: value, Set: map[string]bool{}}
	if value == "" {
		return flag, &componentProblem{message: "needs STACK[=NAME], the value is empty"}
	}
	head, tail, hasTail := strings.Cut(value, ",")
	stack, name, hasName := strings.Cut(head, "=")
	switch {
	case stack == "":
		return flag, &componentProblem{message: fmt.Sprintf("%q needs a stack before =NAME", value)}
	case hasName && name == "":
		return flag, &componentProblem{message: fmt.Sprintf("%q needs a name after =", value)}
	}
	flag.Entry = manifest.Component{Stack: stack, Name: name}
	if !hasTail {
		return flag, nil
	}
	for _, pair := range strings.Split(tail, ",") {
		if problem := applyComponentPair(&flag, pair); problem != nil {
			return flag, problem
		}
	}
	return flag, nil
}

// applyComponentPair sets one key=value pair on the flag.
func applyComponentPair(flag *manifest.ComponentFlag, pair string) *componentProblem {
	key, val, hasValue := strings.Cut(pair, "=")
	if !hasValue {
		return &componentProblem{message: fmt.Sprintf("%q needs KEY=VALUE", pair)}
	}
	serviceKey, isService := strings.CutPrefix(key, serviceKeyPrefix)
	if !isService && !slices.Contains(componentKeys, key) {
		return &componentProblem{message: fmt.Sprintf("unknown key %q", key), suggestion: suggest(key, componentKeys)}
	}
	if isService && serviceKey == "" {
		return &componentProblem{message: fmt.Sprintf("%q needs a service key after service.", pair)}
	}
	if flag.Set[key] {
		return &componentProblem{message: fmt.Sprintf("key %q is given twice", key)}
	}
	if val == "" && key != "modules" {
		return &componentProblem{message: fmt.Sprintf("key %q needs a value", key)}
	}
	flag.Set[key] = true
	entry := &flag.Entry
	switch key {
	case "modules":
		entry.HasModules = true
		entry.Modules = []string{}
		if val == "" {
			return nil
		}
		for _, id := range strings.Split(val, "+") {
			if id == "" {
				return &componentProblem{message: fmt.Sprintf("modules %q has an empty module id", val)}
			}
			entry.Modules = append(entry.Modules, id)
		}
	case "go-module":
		entry.GoModule = val
	case "backend":
		entry.Backend = val
	case "db-name":
		entry.Database = databaseOf(entry.Database)
		entry.Database.Name = val
	case "db-user":
		entry.Database = databaseOf(entry.Database)
		entry.Database.User = val
	default:
		if entry.Services == nil {
			entry.Services = map[string]string{}
		}
		entry.Services[serviceKey] = val
	}
	return nil
}

// databaseOf returns database, or a new one when it is nil.
func databaseOf(database *manifest.Database) *manifest.Database {
	if database == nil {
		return &manifest.Database{}
	}
	return database
}

// componentFlagArg renders a parsed flag as a --component value that parses
// back to an equal flag: the head, then the keys in the order of the key table
// and the service keys sorted.
func componentFlagArg(flag manifest.ComponentFlag) string {
	entry := flag.Entry
	head := entry.Stack
	if entry.Name != "" {
		head += "=" + entry.Name
	}
	parts := []string{head}
	if entry.HasModules {
		parts = append(parts, "modules="+strings.Join(entry.Modules, "+"))
	}
	if entry.GoModule != "" {
		parts = append(parts, "go-module="+entry.GoModule)
	}
	if entry.Database != nil && entry.Database.Name != "" {
		parts = append(parts, "db-name="+entry.Database.Name)
	}
	if entry.Database != nil && entry.Database.User != "" {
		parts = append(parts, "db-user="+entry.Database.User)
	}
	if entry.Backend != "" {
		parts = append(parts, "backend="+entry.Backend)
	}
	keys := make([]string, 0, len(entry.Services))
	for key := range entry.Services {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		parts = append(parts, serviceKeyPrefix+key+"="+entry.Services[key])
	}
	return strings.Join(parts, ",")
}

// componentValue is the repeatable flag value of --component.
type componentValue struct{ target *[]componentEntry }

func (v componentValue) String() string {
	if v.target == nil {
		return ""
	}
	typed := make([]string, 0, len(*v.target))
	for _, entry := range *v.target {
		typed = append(typed, entry.raw)
	}
	return strings.Join(typed, " ")
}

func (v componentValue) Set(value string) error {
	flag, problem := ParseComponentFlag(value)
	if problem != nil {
		return problem
	}
	*v.target = append(*v.target, componentEntry{Component: flag.Entry, set: flag.Set, raw: value})
	return nil
}

// Suggest returns the close key of an unknown key in value, or "".
func (v componentValue) Suggest(value string) string {
	_, problem := ParseComponentFlag(value)
	if problem == nil {
		return ""
	}
	return problem.suggestion
}

// components registers --component on set, bound to target.
func (s *flagSet) components(target *[]componentEntry, spec flagSpec) {
	spec.Value = componentValue{target}
	spec.Repeatable = true
	s.add(spec)
}

// duplicateComponentProblems reports each --component whose STACK=NAME repeats
// an earlier one.
func duplicateComponentProblems(entries []componentEntry) []Problem {
	var problems []Problem
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.Name == "" {
			continue
		}
		key := entry.Stack + "=" + entry.Name
		if seen[key] {
			problems = append(problems, Problem{
				Arg:     "--component " + entry.raw,
				Message: "is given twice",
				Hint:    fmt.Sprintf("give every key of %s in one --component", entry.Name),
			})
		}
		seen[key] = true
	}
	return problems
}

// componentFlags returns the --component values as flag inputs, in flag order.
func componentFlags(entries []componentEntry) []manifest.ComponentFlag {
	flags := make([]manifest.ComponentFlag, 0, len(entries))
	for i, entry := range entries {
		flags = append(flags, manifest.ComponentFlag{Index: i, Arg: entry.raw, Entry: entry.Component, Set: entry.set})
	}
	return flags
}

// multiComponentIssues reports COMPONENT_MULTI_UNSUPPORTED once when a stack
// has several components.
func multiComponentIssues(in manifest.Input) []issue.Issue {
	if !hasSeveralOfStack(in.Selection) {
		return nil
	}
	found := issue.Issue{
		Code:    plan.CodeComponentMultiUnsupported,
		Path:    issue.Path(selectionSource, "/components"),
		Message: "several backend components are not supported by this generator yet",
	}
	return []issue.Issue{found}
}

// hasSeveralOfStack reports two components of one stack in the entries.
func hasSeveralOfStack(sel manifest.Selection) bool {
	seen := map[string]bool{}
	for _, component := range sel.Components {
		if seen[component.Stack] {
			return true
		}
		seen[component.Stack] = true
	}
	return false
}
