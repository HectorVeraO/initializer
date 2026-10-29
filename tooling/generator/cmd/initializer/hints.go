package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/emit"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
)

// rebuildCatalogHint is what to do when the catalog does not match the sources.
const rebuildCatalogHint = "rebuild the catalog: initializer catalog build"

// addHints returns a copy of issues in which every issue without a Hint gets
// the one its code has in the hint table. moduleIDs are the catalog's module
// ids, for the MODULE_UNKNOWN close match.
func addHints(issues []issue.Issue, moduleIDs []string) []issue.Issue {
	hinted := make([]issue.Issue, len(issues))
	copy(hinted, issues)
	for i := range hinted {
		if hinted[i].Hint == "" {
			hinted[i].Hint = hintFor(hinted[i], moduleIDs)
		}
	}
	return hinted
}

// hintFor returns the table hint of one issue, or "".
func hintFor(found issue.Issue, moduleIDs []string) string {
	switch found.Code {
	case emit.CodeOutputNotEmpty:
		return "choose another folder: initializer generate <dir>"
	case plan.CodeGeneratorMismatch, plan.CodeFileMismatch:
		return rebuildCatalogHint
	case codeInputFormatUnknown:
		return "use .json"
	case manifest.CodeComponentMergeAmbiguous:
		return "To add, remove or rename a component, edit the input file"
	case manifest.CodeNameInvalid:
		return "example: acme-api"
	case plan.CodeNameReserved:
		return "choose another name"
	case plan.CodeModuleUnknown:
		closest := suggest(lastQuoted(found.Message), moduleIDs)
		if closest == "" {
			return ""
		}
		return fmt.Sprintf("did you mean %q?", closest)
	}
	return ""
}

// lastQuoted returns the last double-quoted Go string of text, unquoted; ""
// when there is none.
func lastQuoted(text string) string {
	end := strings.LastIndex(text, `"`)
	if end <= 0 {
		return ""
	}
	start := strings.LastIndex(text[:end], `"`)
	if start < 0 {
		return ""
	}
	unquoted, err := strconv.Unquote(text[start : end+1])
	if err != nil {
		return ""
	}
	return unquoted
}
