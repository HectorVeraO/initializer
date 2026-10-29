package manifest

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
)

// CodeComponentMergeAmbiguous reports a --component flag that names no entry
// of the input file.
const CodeComponentMergeAmbiguous = "COMPONENT_MERGE_AMBIGUOUS"

// Merge applies flags over file (nil when there is no file) and returns the
// merged input as given: a flag replaces the file's value, and --component
// flags merge per stack. It reports COMPONENT_MERGE_AMBIGUOUS.
func Merge(file *Input, flags Input) (Input, []issue.Issue) {
	merged := Input{Origins: map[string]Origin{}}
	if file != nil {
		merged.Selection = file.Selection
		merged.Selection.Components = append([]Component(nil), file.Selection.Components...)
		merged.Selection.AllModules = false
		for pointer, origin := range file.Origins {
			merged.Origins[pointer] = origin
		}
	}
	if merged.Selection.Schema == 0 {
		merged.Selection.Schema = 1
	}
	mergeScalars(&merged, flags)
	mergeLists(&merged, flags)
	issues := mergeComponents(&merged, file, flags)
	if len(merged.Selection.Components) > 0 {
		merged.Selection.Schema = MaxSchema
	}
	issue.Sort(issues)
	return merged, issues
}

func mergeScalars(merged *Input, flags Input) {
	if flags.Selection.Has.Name {
		merged.Selection.Project.Name, merged.Selection.Has.Name = flags.Selection.Project.Name, true
		merged.Origins[patternName] = flags.Origins[patternName]
	}
	if flags.Selection.Has.GoModule {
		merged.Selection.Project.GoModule, merged.Selection.Has.GoModule = flags.Selection.Project.GoModule, true
		merged.Origins[patternGoModule] = flags.Origins[patternGoModule]
	}
}

// mergeLists replaces the stacks and modules lists the flags gave. --all-modules
// replaces the file's modules with every module of the backend stack.
func mergeLists(merged *Input, flags Input) {
	if flags.Selection.Has.Stacks {
		replaceList(merged, flags, "/stacks")
		merged.Selection.Stacks = append([]string(nil), flags.Selection.Stacks...)
		merged.Selection.Has.Stacks = true
	}
	switch {
	case flags.Selection.Has.Modules:
		replaceList(merged, flags, "/modules")
		merged.Selection.Modules = append([]string(nil), flags.Selection.Modules...)
		merged.Selection.Has.Modules = true
	case flags.Selection.AllModules:
		dropOrigins(merged, "/modules")
		merged.Selection.Modules = nil
		merged.Selection.Has.Modules = false
	}
	if flags.Selection.AllModules {
		merged.Selection.AllModules = true
		merged.Origins["/modules"] = flags.Origins["/modules"]
	}
}

// replaceList moves the origins of the list at prefix from flags into merged.
func replaceList(merged *Input, flags Input, prefix string) {
	dropOrigins(merged, prefix)
	for pointer, origin := range flags.Origins {
		if pointer == prefix || strings.HasPrefix(pointer, prefix+"/") {
			merged.Origins[pointer] = origin
		}
	}
}

func dropOrigins(merged *Input, prefix string) {
	for pointer := range merged.Origins {
		if pointer == prefix || strings.HasPrefix(pointer, prefix+"/") {
			delete(merged.Origins, pointer)
		}
	}
}

// mergeComponents applies the per-stack rule: with no file entry of a stack the
// flags define its entries; with file entries every flag must name exactly one
// of them, and the entries stay as the file gave them.
func mergeComponents(merged *Input, file *Input, flags Input) []issue.Issue {
	fromFlags := flags.Selection.Components
	if len(fromFlags) == 0 {
		return nil
	}
	fileEntries := map[string][]Component{}
	if file != nil {
		for _, entry := range file.Selection.Components {
			fileEntries[entry.Stack] = append(fileEntries[entry.Stack], entry)
		}
	}
	var issues []issue.Issue
	for i, entry := range fromFlags {
		existing := fileEntries[entry.Stack]
		if len(existing) == 0 {
			appendFlagEntry(merged, flags, i)
			continue
		}
		if matches(existing, entry.Name) != 1 {
			issues = append(issues, ambiguousIssue(file, flags, i, existing))
		}
	}
	return issues
}

// matches counts the entries that carry name; an empty name matches none.
func matches(entries []Component, name string) int {
	count := 0
	for _, entry := range entries {
		if name != "" && entry.Name == name {
			count++
		}
	}
	return count
}

// appendFlagEntry adds the flag's entry at the end of the merged components,
// with the origins of its values.
func appendFlagEntry(merged *Input, flags Input, flagIndex int) {
	at := strconv.Itoa(len(merged.Selection.Components))
	from := "/components/" + strconv.Itoa(flagIndex)
	merged.Selection.Components = append(merged.Selection.Components, flags.Selection.Components[flagIndex])
	for _, suffix := range []string{"", "/stack", "/name"} {
		if origin, has := flags.Origins[from+suffix]; has {
			merged.Origins["/components/"+at+suffix] = origin
		}
	}
	if _, has := merged.Origins["/components"]; !has {
		merged.Origins["/components"] = flags.Origins["/components"]
	}
}

// ambiguousIssue builds COMPONENT_MERGE_AMBIGUOUS for flag i, which names no
// single entry of its stack in the file.
func ambiguousIssue(file *Input, flags Input, i int, existing []Component) issue.Issue {
	flag := flags.Selection.Components[i]
	arg := flags.Origins["/components/"+strconv.Itoa(i)].Arg
	fileName := "the input file"
	if origin, has := file.Origins[""]; has && origin.Source != "" {
		fileName = origin.Source
	}
	var existingNames []string
	for _, entry := range existing {
		if entry.Name != "" {
			existingNames = append(existingNames, entry.Name)
		}
	}
	listed := "none is named"
	if len(existingNames) > 0 {
		listed = strings.Join(existingNames, ", ")
	}
	message := fmt.Sprintf("%s names no %s component of %s (%s)", arg, flag.Stack, fileName, listed)
	if flag.Name == "" {
		message = fmt.Sprintf("%s names no component; %s has %s components (%s)", arg, fileName, flag.Stack, listed)
	}
	hint := "To add, remove or rename a component, edit " + fileName
	if closest := names.Closest(flag.Name, existingNames); closest != "" {
		hint = fmt.Sprintf("did you mean %q? %s", closest, hint)
	}
	return issue.Issue{
		Code:    CodeComponentMergeAmbiguous,
		Path:    issue.Path(FlagsSource, "/components/"+strconv.Itoa(i)),
		Message: message,
		Hint:    hint,
	}
}
