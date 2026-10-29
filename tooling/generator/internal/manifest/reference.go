package manifest

import (
	"fmt"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
)

// referenceSchema is the only schema of .initializer/reference.json.
const referenceSchema = 2

// DecodeReference decodes .initializer/reference.json with the selection's
// strict JSON walk and the schema-2 field set, and checks shape and the name
// grammars only: schema 2, known keys, stacks of the table, label values and
// a project name. It applies no role rule, no goModule requirement and no
// catalog check. Issues use the source name ".initializer/reference.json" and
// the code REFERENCE_INVALID.
func DecodeReference(data []byte) (Selection, []issue.Issue) {
	root, issues := ParseJSON(names.ReferenceSource, data)
	if len(issues) > 0 {
		return Selection{}, recode(issues)
	}
	input, issues := DecodeInput(names.ReferenceSource, root)
	selection := input.Selection
	d := newDecoder(names.ReferenceSource, names.CodeReferenceInvalid)
	if selection.Schema != 0 && selection.Schema != referenceSchema {
		d.add("/schema", fmt.Sprintf("must be %d", referenceSchema))
	}
	if !selection.Has.Name {
		d.add("/project/name", "required")
	}
	for i, component := range selection.Components {
		if !isKnownStack(component.Stack) {
			d.add(fmt.Sprintf("/components/%d/stack", i), "stack is not in the stack table")
		}
	}
	issues = append(recode(issues), d.issues...)
	issue.Sort(issues)
	if len(issues) > 0 {
		return Selection{}, dedupe(issues)
	}
	return selection, nil
}

// recode returns issues with the REFERENCE_INVALID code.
func recode(issues []issue.Issue) []issue.Issue {
	out := make([]issue.Issue, len(issues))
	for i, found := range issues {
		found.Code = names.CodeReferenceInvalid
		out[i] = found
	}
	return out
}

// dedupe keeps the first issue of each path of the sorted issues.
func dedupe(issues []issue.Issue) []issue.Issue {
	var out []issue.Issue
	seen := map[string]bool{}
	for _, found := range issues {
		if seen[found.Path] {
			continue
		}
		seen[found.Path] = true
		out = append(out, found)
	}
	return out
}
