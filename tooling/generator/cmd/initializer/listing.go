package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/source"
)

// rootDefault is how --root is found when it is absent, as help prints it.
const rootDefault = "INITIALIZER_ROOT, else the clone above the working directory, else the clone above this binary"

// registerSourceFlags registers --root and --catalog, shared by every command
// that resolves the sources.
func registerSourceFlags(set *flagSet, root, catalogFile *string) {
	set.str(root, flagSpec{Name: "root", Placeholder: "DIR", Usage: "Source checkout", Default: rootDefault})
	set.str(catalogFile, flagSpec{Name: "catalog", Placeholder: "FILE",
		Usage: "Catalog file to use instead of building one from the checkout"})
}

type moduleLsOptions struct{ root, catalog, stack string }

func moduleLsCommand() *Command {
	return &Command{
		Path:    []string{"module", "ls"},
		Label:   "module ls",
		Summary: "List the catalog's modules",
		Usage:   "module ls [flags]",
		Description: "List the modules of the catalog: the stacks that have each one, the modules\n" +
			"it requires, the spec it implements and its version.",
		MaxArgs: 0,
		Flags: func(set *flagSet) any {
			o := &moduleLsOptions{}
			set.str(&o.stack, flagSpec{Name: "stack", Placeholder: "ID", Usage: "Only the modules of this stack", Default: "all"})
			registerSourceFlags(set, &o.root, &o.catalog)
			return o
		},
		Examples: []Example{
			{"every module", "initializer module ls"},
			{"the modules of the web console", "initializer module ls --stack ng-web"},
			{"as JSON", "initializer module ls --format json"},
		},
		Run: runModuleLs,
	}
}

type stackLsOptions struct{ root, catalog string }

func stackLsCommand() *Command {
	return &Command{
		Path:        []string{"stack", "ls"},
		Label:       "stack ls",
		Summary:     "List the catalog's stacks",
		Usage:       "stack ls [flags]",
		Description: "List the stacks of the catalog with their role, folder suffix and module count.",
		MaxArgs:     0,
		Flags: func(set *flagSet) any {
			o := &stackLsOptions{}
			registerSourceFlags(set, &o.root, &o.catalog)
			return o
		},
		Examples: []Example{
			{"every stack", "initializer stack ls"},
			{"as JSON", "initializer stack ls --format json"},
		},
		Run: runStackLs,
	}
}

// moduleStack is one stack's view of a module in the module ls document.
type moduleStack struct {
	Stack          string            `json:"stack"`
	Version        string            `json:"version"`
	ImplementsSpec map[string]string `json:"implementsSpec"`
}

// moduleListed is one module of the module ls document.
type moduleListed struct {
	Module   string        `json:"module"`
	Implied  bool          `json:"implied"`
	Requires []string      `json:"requires"`
	Stacks   []moduleStack `json:"stacks"`
}

// listModules returns one entry per module id of c: the implied ones first,
// then by id. Stacks follow the catalog order.
func listModules(c catalog.Catalog) []moduleListed {
	byID := map[string]*moduleListed{}
	var order []string
	requires := map[string]map[string]bool{}
	for _, stack := range c.Stacks {
		for _, module := range stack.Modules {
			entry, seen := byID[module.Module]
			if !seen {
				entry = &moduleListed{Module: module.Module}
				byID[module.Module] = entry
				requires[module.Module] = map[string]bool{}
				order = append(order, module.Module)
			}
			entry.Implied = entry.Implied || module.Implied
			specs := module.ImplementsSpec
			if specs == nil {
				specs = map[string]string{}
			}
			entry.Stacks = append(entry.Stacks, moduleStack{Stack: stack.Stack, Version: module.Version, ImplementsSpec: specs})
			for _, requirement := range module.RequiresModules {
				requires[module.Module][requirement.Module] = true
			}
		}
	}
	listed := make([]moduleListed, 0, len(order))
	for _, id := range order {
		entry := byID[id]
		entry.Requires = make([]string, 0, len(requires[id]))
		for required := range requires[id] {
			entry.Requires = append(entry.Requires, required)
		}
		sort.Strings(entry.Requires)
		listed = append(listed, *entry)
	}
	sort.SliceStable(listed, func(i, j int) bool {
		if listed[i].Implied != listed[j].Implied {
			return listed[i].Implied
		}
		return listed[i].Module < listed[j].Module
	})
	return listed
}

// inStack reports whether the module is in the given stack.
func (m moduleListed) inStack(stack string) bool {
	for _, found := range m.Stacks {
		if found.Stack == stack {
			return true
		}
	}
	return false
}

// specText renders the implemented specs of a module over its stacks as
// "spec version", sorted, comma-joined.
func (m moduleListed) specText() string {
	seen := map[string]bool{}
	var specs []string
	for _, found := range m.Stacks {
		for spec, version := range found.ImplementsSpec {
			text := spec + " " + version
			if !seen[text] {
				seen[text] = true
				specs = append(specs, text)
			}
		}
	}
	if len(specs) == 0 {
		return "-"
	}
	sort.Strings(specs)
	return strings.Join(specs, ", ")
}

// versionText is the version when every stack agrees, else one "stack version"
// per stack.
func (m moduleListed) versionText() string {
	agreed := true
	for _, found := range m.Stacks {
		if found.Version != m.Stacks[0].Version {
			agreed = false
		}
	}
	if agreed {
		return m.Stacks[0].Version
	}
	parts := make([]string, 0, len(m.Stacks))
	for _, found := range m.Stacks {
		parts = append(parts, found.Stack+" "+found.Version)
	}
	return strings.Join(parts, ", ")
}

// writeTable renders rows as columns separated by three spaces; the last
// column is not padded.
func writeTable(text *strings.Builder, rows [][]string) {
	var widths []int
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], len(cell))
		}
	}
	for _, row := range rows {
		for i, cell := range row {
			if i == len(row)-1 {
				text.WriteString(cell)
				continue
			}
			text.WriteString(cell)
			text.WriteString(strings.Repeat(" ", widths[i]-len(cell)+3))
		}
		text.WriteString("\n")
	}
}

// announce prints the Sources: line and the resolution warnings and notes of a
// command that read a catalog.
func announce(ctx *Context, resolved source.Resolved) sourceRef {
	ref, notes := announceSources(ctx, resolved)
	for _, note := range notes {
		fmt.Fprintf(ctx.diagnostics(), "note: %s\n", note)
	}
	ctx.Notes = notes
	return ref
}

func runModuleLs(ctx *Context, opts any, _ []string) int {
	o := opts.(*moduleLsOptions)
	resolved, buildIssues, err := ctx.resolveSources(o.root, o.catalog)
	if err != nil {
		return reportSourceError(ctx, err, nil, issueView{})
	}
	ref := announce(ctx, resolved)
	if len(buildIssues) > 0 {
		return reportIssues(ctx, buildIssues, issueView{})
	}
	if o.stack != "" {
		if unknown := unknownStack(resolved.Catalog, o.stack); unknown != nil {
			return reportIssues(ctx, []issue.Issue{*unknown}, issueView{})
		}
	}
	var modules []moduleListed
	for _, module := range listModules(resolved.Catalog) {
		if o.stack == "" || module.inStack(o.stack) {
			modules = append(modules, module)
		}
	}
	return writeModuleList(ctx, ref, resolved, modules)
}

// stackIDs returns the stack ids of c in catalog order.
func stackIDs(c catalog.Catalog) []string {
	ids := make([]string, 0, len(c.Stacks))
	for _, stack := range c.Stacks {
		ids = append(ids, stack.Stack)
	}
	return ids
}

// unknownStack returns the STACK_UNKNOWN issue of a --stack value that names
// no stack of c, or nil.
func unknownStack(c catalog.Catalog, stack string) *issue.Issue {
	ids := stackIDs(c)
	for _, id := range ids {
		if id == stack {
			return nil
		}
	}
	hint := stackListHint
	if closest := suggest(stack, ids); closest != "" {
		hint = fmt.Sprintf("%s; did you mean %q?", stackListHint, closest)
	}
	return &issue.Issue{
		Code:    codeStackUnknown,
		Path:    issue.Path(manifest.FlagsSource, "/stack"),
		Message: fmt.Sprintf("stack %q is not in the catalog", stack),
		Hint:    hint,
	}
}

// catalogOf names the catalog a run used.
func catalogOf(resolved source.Resolved) catalogRef {
	ref := catalogRef{SHA256: resolved.CatalogSHA256}
	if resolved.CatalogFile != "" {
		ref.File = filepath.ToSlash(resolved.CatalogFile)
	}
	return ref
}

// writeModuleList prints the module ls result and returns the exit code.
func writeModuleList(ctx *Context, ref sourceRef, resolved source.Resolved, modules []moduleListed) int {
	if ctx.isJSON() {
		doc := struct {
			Schema   int            `json:"schema"`
			OK       bool           `json:"ok"`
			Source   sourceRef      `json:"source"`
			Catalog  catalogRef     `json:"catalog"`
			Modules  []moduleListed `json:"modules"`
			Notes    []string       `json:"notes"`
			Warnings []Warning      `json:"warnings"`
		}{documentSchema, true, ref, catalogOf(resolved), list(modules), list(ctx.Notes), ctx.warnings()}
		return emitDocument(ctx, ctx.Stdout, doc, exitOK)
	}
	if len(modules) == 0 {
		return emitText(ctx, "no modules in the catalog\n")
	}
	rows := [][]string{{"MODULE", "STACKS", "REQUIRES", "SPEC", "VERSION"}}
	for _, module := range modules {
		required := "-"
		if len(module.Requires) > 0 {
			required = strings.Join(module.Requires, ",")
		}
		stacks := make([]string, 0, len(module.Stacks))
		for _, found := range module.Stacks {
			stacks = append(stacks, found.Stack)
		}
		version := module.versionText()
		if module.Implied {
			version += "   (always installed)"
		}
		rows = append(rows, []string{module.Module, strings.Join(stacks, ","), required, module.specText(), version})
	}
	var text strings.Builder
	writeTable(&text, rows)
	return emitText(ctx, text.String())
}

// stackListed is one stack of the stack ls document.
type stackListed struct {
	Stack    string `json:"stack"`
	Role     string `json:"role"`
	Suffix   string `json:"suffix"`
	GoModule bool   `json:"goModule"`
	Modules  int    `json:"modules"`
}

// listStacks returns the stacks of c in catalog order with their stack table row.
func listStacks(c catalog.Catalog) []stackListed {
	listed := make([]stackListed, 0, len(c.Stacks))
	for _, stack := range c.Stacks {
		entry := stackListed{Stack: stack.Stack, Role: stack.Role, Modules: len(stack.Modules)}
		if info, known := manifest.LookupStack(stack.Stack); known {
			entry.Suffix, entry.GoModule = info.Suffix, info.GoModule
		}
		listed = append(listed, entry)
	}
	return listed
}

func runStackLs(ctx *Context, opts any, _ []string) int {
	o := opts.(*stackLsOptions)
	resolved, buildIssues, err := ctx.resolveSources(o.root, o.catalog)
	if err != nil {
		return reportSourceError(ctx, err, nil, issueView{})
	}
	ref := announce(ctx, resolved)
	if len(buildIssues) > 0 {
		return reportIssues(ctx, buildIssues, issueView{})
	}
	stacks := listStacks(resolved.Catalog)
	if ctx.isJSON() {
		doc := struct {
			Schema   int           `json:"schema"`
			OK       bool          `json:"ok"`
			Source   sourceRef     `json:"source"`
			Catalog  catalogRef    `json:"catalog"`
			Stacks   []stackListed `json:"stacks"`
			Notes    []string      `json:"notes"`
			Warnings []Warning     `json:"warnings"`
		}{documentSchema, true, ref, catalogOf(resolved), list(stacks), list(ctx.Notes), ctx.warnings()}
		return emitDocument(ctx, ctx.Stdout, doc, exitOK)
	}
	rows := [][]string{{"STACK", "ROLE", "SUFFIX", "GO MODULE", "MODULES"}}
	for _, stack := range stacks {
		suffix, goModule := stack.Suffix, "no"
		if suffix == "" {
			suffix = "-"
		}
		if stack.GoModule {
			goModule = "yes"
		}
		rows = append(rows, []string{stack.Stack, stack.Role, suffix, goModule, strconv.Itoa(stack.Modules)})
	}
	var text strings.Builder
	writeTable(&text, rows)
	text.WriteString("Several components of one stack: repeat --component STACK=NAME (see 'initializer help generate').\n")
	return emitText(ctx, text.String())
}
