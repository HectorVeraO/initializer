package manifest

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
)

// DefaultProjectName names a project when neither a flag, the file nor the
// folder does.
const DefaultProjectName = "demo"

// Origin sources of a value the generator filled in.
const (
	ArgsSource    = "args"
	DefaultSource = "default"
)

// ResolveDefaults fills the project-level defaults on a merged input: the name
// from DIR or "demo", the stacks from the components or the one backend stack,
// the modules as an empty list, and the Go module when a selected stack needs
// one. dir is the DIR argument as given ("" when absent); wd is the working
// directory; backendStacks are the catalog's stack ids with role backend. It
// returns the resolved document (every value present, origins "default" or
// "args" for the filled ones), one note per default taken, the effective output
// folder (absolute), and the issues.
func ResolveDefaults(in Input, dir, wd string, backendStacks []string) (resolved Input, notes []string, outDir string, issues []issue.Issue) {
	resolved = Input{Selection: in.Selection, Origins: map[string]Origin{}}
	resolved.Selection.Components = append([]Component(nil), in.Selection.Components...)
	for pointer, origin := range in.Origins {
		resolved.Origins[pointer] = origin
	}
	s := &resolved.Selection

	name, nameNotes, nameIssues := resolveName(&resolved, dir, wd)
	notes = append(notes, nameNotes...)
	issues = append(issues, nameIssues...)

	if !s.Has.Stacks {
		stacks, stackIssues := defaultStacks(&resolved, backendStacks)
		s.Stacks = stacks
		issues = append(issues, stackIssues...)
	}
	s.Has.Stacks = true
	if !s.Has.Modules {
		s.Modules = []string{}
	}
	s.Has.Modules = true

	if !s.Has.GoModule && !entryGivesGoModule(s.Components) && names.ValidLabel(name) && selectsGoStack(s.Stacks) {
		s.Project.GoModule = "example.com/" + name
		notes = append(notes, fmt.Sprintf("Go module defaults to %s; pass --go-module to set yours", s.Project.GoModule))
		resolved.Origins[patternGoModule] = Origin{Source: DefaultSource, Pointer: patternGoModule}
	}
	s.Has.GoModule = s.Project.GoModule != ""

	outDir = filepath.Join(wd, name)
	if dir != "" {
		outDir = dir
		if !filepath.IsAbs(dir) {
			outDir = filepath.Join(wd, dir)
		}
	}
	outDir = filepath.Clean(outDir)
	return resolved, notes, outDir, issues
}

// resolveName applies flag > file > DIR's base name > "demo" and reports the
// notes and the NAME_INVALID of a folder name that is not a project name.
func resolveName(resolved *Input, dir, wd string) (name string, notes []string, issues []issue.Issue) {
	s := &resolved.Selection
	if s.Has.Name {
		return s.Project.Name, nil, nil
	}
	s.Has.Name = true
	if dir == "" {
		s.Project.Name = DefaultProjectName
		resolved.Origins[patternName] = Origin{Source: DefaultSource, Pointer: patternName}
		return DefaultProjectName, []string{fmt.Sprintf("no name given; using %q", DefaultProjectName)}, nil
	}
	name = folderBase(dir, wd)
	s.Project.Name = name
	resolved.Origins[patternName] = Origin{Source: ArgsSource, Pointer: "/dir"}
	if !names.ValidLabel(name) {
		issues = append(issues, issue.Issue{
			Code:    CodeNameInvalid,
			Path:    issue.Path(ArgsSource, "/dir"),
			Message: fmt.Sprintf("name %q comes from the folder %q and is not a project name", name, dir),
			Hint:    "pass --name <name>",
		})
		return name, nil, issues
	}
	return name, []string{fmt.Sprintf("project name %q comes from the folder; pass --name to set it", name)}, nil
}

// folderBase returns the base name of dir as an absolute, cleaned path; for
// "." that is the name of wd. It never lowercases or repairs the name.
func folderBase(dir, wd string) string {
	absolute := dir
	if !filepath.IsAbs(dir) {
		absolute = filepath.Join(wd, dir)
	}
	return filepath.Base(filepath.Clean(absolute))
}

// defaultStacks returns the stacks of the components in first-appearance order,
// else the one backend stack; the issue says why there is none.
func defaultStacks(resolved *Input, backendStacks []string) ([]string, []issue.Issue) {
	var stacks []string
	seen := map[string]bool{}
	for i, component := range resolved.Selection.Components {
		if seen[component.Stack] {
			continue
		}
		seen[component.Stack] = true
		pointer := "/stacks/" + strconv.Itoa(len(stacks))
		resolved.Origins[pointer] = resolved.Origins["/components/"+strconv.Itoa(i)]
		stacks = append(stacks, component.Stack)
	}
	if len(stacks) > 0 {
		return stacks, nil
	}
	if len(backendStacks) == 1 {
		resolved.Origins["/stacks/0"] = Origin{Source: DefaultSource, Pointer: "/stacks/0"}
		return []string{backendStacks[0]}, nil
	}
	found := issue.Issue{
		Code:    CodeSelectionInvalid,
		Path:    issue.Path(FlagsSource, "/stacks"),
		Message: fmt.Sprintf("no default stack: pass --stacks (backend stacks: %s)", strings.Join(backendStacks, ", ")),
	}
	return []string{}, []issue.Issue{found}
}

// selectsGoStack reports whether a stack of the list needs a Go module path.
func selectsGoStack(stacks []string) bool {
	for _, stack := range stacks {
		info, known := LookupStack(stack)
		if known && info.GoModule {
			return true
		}
	}
	return false
}

// entryGivesGoModule reports whether a component entry sets its own Go module.
func entryGivesGoModule(components []Component) bool {
	for _, component := range components {
		if component.GoModule != "" {
			return true
		}
	}
	return false
}
