package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/emit"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/source"
)

// rebuildCatalogHint is what to do when the catalog does not match the sources.
const rebuildCatalogHint = "rebuild the catalog: initializer catalog build"

// codeStackUnknown is the code of a --stack that names no stack of the catalog.
const codeStackUnknown = "STACK_UNKNOWN"

// moduleListHint and stackListHint point at the commands that list what exists.
const (
	moduleListHint = "run 'initializer module ls'"
	stackListHint  = "run 'initializer stack ls'"
)

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
		return "use .yaml, .yml or .json"
	case plan.CodeComponentAmbiguous:
		return "set it per component: --component go-srv=<name>,db-name=<db>"
	case plan.CodeModulesUnused:
		return "drop the top-level modules, or remove modules from one component"
	case manifest.CodeComponentMergeAmbiguous:
		return "To add, remove or rename a component, edit the input file"
	case manifest.CodeNameInvalid:
		if strings.Contains(found.Message, "underscores") {
			return "example: acme_api"
		}
		return "example: acme-api"
	case plan.CodeNameReserved:
		return "choose another name"
	case plan.CodeModuleUnknown:
		closest := suggest(lastQuoted(found.Message), moduleIDs)
		if closest == "" {
			return moduleListHint
		}
		return fmt.Sprintf("%s; did you mean %q?", moduleListHint, closest)
	case codeStackUnknown:
		return stackListHint
	case source.CodeRootNotFound, source.CodeRootInvalid, source.CodeNotGit, source.CodeGitMissing,
		source.CodeCatalogUnreadable, source.CodeUnreadable, source.CodeGeneratorStale:
		return source.HintFor(found.Code)
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

// shortenedNote ends the hint of a suggested name cut to fit the name limit.
const shortenedNote = " (shortened to fit the 63-byte limit)"

// addNameHints sets the hint of every COMPONENT_NAME_REQUIRED issue: the
// command line with each unnamed --component head filled in, or the lines to
// add to the input file. Issue paths are still selection#<pointer>.
func addNameHints(args []string, in manifest.Input, issues []issue.Issue) []issue.Issue {
	hinted := append([]issue.Issue(nil), issues...)
	fills := map[int]string{}
	for _, found := range hinted {
		if found.Code != plan.CodeComponentNameRequired {
			continue
		}
		for _, entry := range unnamedEntries(in, found) {
			if in.Origins[entry.pointer].Source == manifest.FlagsSource {
				fills[entry.flagIndex] = entry.name
			}
		}
	}
	for i := range hinted {
		if hinted[i].Code != plan.CodeComponentNameRequired {
			continue
		}
		entries := unnamedEntries(in, hinted[i])
		hint := fileNameHint(in, entries)
		if hint == "" {
			hint = "initializer " + strings.Join(quoteArgs(fillComponentHeads(args, fills)), " ")
		}
		for _, entry := range entries {
			if entry.shortened {
				hint += shortenedNote
				break
			}
		}
		hinted[i].Hint = hint
	}
	return hinted
}

// unnamedEntry is a component entry that needs a name and the name suggested.
type unnamedEntry struct {
	pointer   string
	index     int
	flagIndex int
	name      string
	shortened bool
}

// unnamedEntries lists the unnamed entries of the stack a COMPONENT_NAME_REQUIRED
// issue is about, with the suggested name of each.
func unnamedEntries(in manifest.Input, found issue.Issue) []unnamedEntry {
	pointer := strings.TrimPrefix(found.Path, selectionSource+"#")
	first, err := strconv.Atoi(strings.TrimPrefix(pointer, "/components/"))
	if err != nil || first < 0 || first >= len(in.Selection.Components) {
		return nil
	}
	stack := in.Selection.Components[first].Stack
	var entries []unnamedEntry
	k := 0
	for i, component := range in.Selection.Components {
		if component.Stack != stack {
			continue
		}
		if component.Name == "" && k < len(found.SuggestedNames) {
			at := "/components/" + strconv.Itoa(i)
			flagIndex := -1
			if origin := in.Origins[at]; origin.Source == manifest.FlagsSource {
				flagIndex, _ = strconv.Atoi(strings.TrimPrefix(origin.Pointer, "/components/"))
			}
			name := found.SuggestedNames[k]
			entries = append(entries, unnamedEntry{
				pointer: at, index: i, flagIndex: flagIndex, name: name,
				shortened: !strings.HasPrefix(name, in.Selection.Project.Name+"-"),
			})
		}
		k++
	}
	return entries
}

// fileNameHint returns the lines to add to the input file for entries, or ""
// when an entry comes from the command line.
func fileNameHint(in manifest.Input, entries []unnamedEntry) string {
	var lines []string
	for _, entry := range entries {
		origin := in.Origins[entry.pointer]
		if origin.Source == manifest.FlagsSource || origin.Source == "" {
			return ""
		}
		member := "name: " + entry.name
		if strings.HasSuffix(strings.ToLower(origin.Source), ".json") {
			member = `"name": "` + entry.name + `"`
		}
		where := origin.Source
		if origin.Line > 0 {
			where += ":" + strconv.Itoa(origin.Line)
		}
		lines = append(lines, fmt.Sprintf("under components[%d] (%s) add %s", entry.index, where, member))
	}
	return strings.Join(lines, "; ")
}

// fillComponentHeads returns args with the head of the n-th --component value
// completed to STACK=NAME for every n in fills.
func fillComponentHeads(args []string, fills map[int]string) []string {
	filled := append([]string(nil), args...)
	count := 0
	fill := func(value string) string {
		name, has := fills[count]
		count++
		head, tail, hasTail := strings.Cut(value, ",")
		if !has || strings.Contains(head, "=") {
			return value
		}
		completed := head + "=" + name
		if hasTail {
			completed += "," + tail
		}
		return completed
	}
	for i := 0; i < len(filled); i++ {
		if value, isInline := strings.CutPrefix(filled[i], "--component="); isInline {
			filled[i] = "--component=" + fill(value)
		} else if filled[i] == "--component" && i+1 < len(filled) {
			filled[i+1] = fill(filled[i+1])
			i++
		}
	}
	return filled
}

// quoteArgs single-quotes the arguments a shell would split or expand, so the
// hint pastes into bash and PowerShell.
func quoteArgs(args []string) []string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = arg
		if arg == "" || strings.ContainsAny(arg, " \t\"'$&|;<>*?()[]{}\\") {
			quoted[i] = "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
		}
	}
	return quoted
}
