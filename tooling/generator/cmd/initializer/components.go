package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
)

// componentKeys is the key table of the --component tail. It is empty until the
// tracks that add entry fields register their keys: every key is unknown.
var componentKeys []string

// componentEntry is one --component flag: the parsed head and the value as typed.
type componentEntry struct {
	manifest.Component
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

// parseComponent parses value = head *( "," pair ), head = stack [ "=" name ].
// Only the syntax is checked here: an unknown stack or a name that fails the
// label grammar is a value problem for the selection checks. Every key of keys
// is accepted by the grammar; with the key table empty, any tail is an unknown
// key.
func parseComponent(value string, keys []string) (manifest.Component, *componentProblem) {
	if value == "" {
		return manifest.Component{}, &componentProblem{message: "needs STACK=NAME, the value is empty"}
	}
	head, tail, hasTail := strings.Cut(value, ",")
	stack, name, hasName := strings.Cut(head, "=")
	switch {
	case stack == "":
		return manifest.Component{}, &componentProblem{message: fmt.Sprintf("%q needs a stack before =NAME", value)}
	case !hasName:
		return manifest.Component{}, &componentProblem{message: fmt.Sprintf("%q needs STACK=NAME", value)}
	case name == "":
		return manifest.Component{}, &componentProblem{message: fmt.Sprintf("%q needs a name after =", value)}
	}
	if hasTail {
		for _, pair := range strings.Split(tail, ",") {
			key, _, _ := strings.Cut(pair, "=")
			isKnown := false
			for _, known := range keys {
				isKnown = isKnown || known == key
			}
			if !isKnown {
				return manifest.Component{}, &componentProblem{
					message:    fmt.Sprintf("unknown key %q", key),
					suggestion: suggest(key, keys),
				}
			}
		}
	}
	return manifest.Component{Stack: stack, Name: name}, nil
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
	component, problem := parseComponent(value, componentKeys)
	if problem != nil {
		return problem
	}
	*v.target = append(*v.target, componentEntry{Component: component, raw: value})
	return nil
}

// Suggest returns the close key of an unknown key in value, or "".
func (v componentValue) Suggest(value string) string {
	_, problem := parseComponent(value, componentKeys)
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
		key := entry.Stack + "=" + entry.Name
		if seen[key] {
			problems = append(problems, Problem{Arg: "--component " + entry.raw, Message: "is given twice"})
		}
		seen[key] = true
	}
	return problems
}

// componentFlags returns the --component values as flag inputs, in flag order.
func componentFlags(entries []componentEntry) []manifest.ComponentFlag {
	flags := make([]manifest.ComponentFlag, 0, len(entries))
	for i, entry := range entries {
		flags = append(flags, manifest.ComponentFlag{Index: i, Arg: entry.raw, Entry: entry.Component})
	}
	return flags
}

// multiComponentIssues reports COMPONENT_MULTI_UNSUPPORTED for every entry
// after the first of a stack. An entry from a flag is reported at its flag; the
// entries of a file once per stack, at the file's components.
func multiComponentIssues(in manifest.Input) []issue.Issue {
	var issues []issue.Issue
	seen := map[string]bool{}
	reportedFile := map[string]bool{}
	for i, component := range in.Selection.Components {
		if !seen[component.Stack] {
			seen[component.Stack] = true
			continue
		}
		origin := in.Origins["/components/"+strconv.Itoa(i)]
		path := issue.Path(origin.Source, origin.Pointer)
		if origin.Source != manifest.FlagsSource {
			path = issue.Path(origin.Source, "/components")
			if reportedFile[component.Stack] {
				continue
			}
			reportedFile[component.Stack] = true
		}
		issues = append(issues, issue.Issue{
			Code:    plan.CodeComponentMultiUnsupported,
			Path:    path,
			Line:    origin.Line,
			Message: fmt.Sprintf("several components of stack %s are not supported by this generator yet", component.Stack),
		})
	}
	return issues
}
