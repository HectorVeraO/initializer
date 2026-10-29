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

// multiComponentIssues reports COMPONENT_MULTI_UNSUPPORTED for every entry
// after the first of a stack. source is the selection file as typed, or "" when
// the entries come from flags.
func multiComponentIssues(components []manifest.Component, source string) []issue.Issue {
	var issues []issue.Issue
	seen := map[string]bool{}
	reportedFile := map[string]bool{}
	for i, component := range components {
		if !seen[component.Stack] {
			seen[component.Stack] = true
			continue
		}
		path := issue.Path(manifest.FlagsSource, "/components/"+strconv.Itoa(i))
		if source != "" {
			path = issue.Path(source, "/components")
			if reportedFile[component.Stack] {
				continue
			}
			reportedFile[component.Stack] = true
		}
		issues = append(issues, issue.Issue{
			Code:    plan.CodeComponentMultiUnsupported,
			Path:    path,
			Message: fmt.Sprintf("several components of stack %s are not supported by this generator yet", component.Stack),
		})
	}
	return issues
}

// componentSelection returns the entries as selection components.
func componentSelection(entries []componentEntry) []manifest.Component {
	if len(entries) == 0 {
		return nil
	}
	components := make([]manifest.Component, 0, len(entries))
	for _, entry := range entries {
		components = append(components, entry.Component)
	}
	return components
}

// flagOrigin rewrites the plan issues about component entries so they point at
// the flag that gave the entry: selection#/components/1/name becomes
// flags#/components/1.
func flagOrigin(issues []issue.Issue) []issue.Issue {
	const prefix = "selection#/components/"
	rewritten := make([]issue.Issue, len(issues))
	for i, found := range issues {
		if strings.HasPrefix(found.Path, prefix) {
			index, _, _ := strings.Cut(strings.TrimPrefix(found.Path, prefix), "/")
			found.Path = issue.Path(manifest.FlagsSource, "/components/"+index)
		}
		rewritten[i] = found
	}
	return rewritten
}
