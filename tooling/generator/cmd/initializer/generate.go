package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/emit"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/source"
)

// selectionSource is the source of the issue paths the plan reports for the
// selection; relocate replaces it with the origin of the value.
const selectionSource = "selection"

// stdinSource names standard input in issue paths.
const stdinSource = "stdin"

// stdinPath is the -f value that reads standard input.
const stdinPath = "-"

// roleBackend is the catalog role of a backend stack.
const roleBackend = "backend"

// inputRun is the result of reading the selection: the file, the flags, their
// merge and the issues of those steps.
type inputRun struct {
	merged manifest.Input
	issues []issue.Issue
	notes  []string
	view   issueView
}

// readFailure is an input file that could not be read (exit 3).
type readFailure struct{ err error }

// flagsOf builds the selection flags of the line.
func (o *generateOptions) flagsOf() manifest.Flags {
	given := func(name string) bool {
		return o.given[name] > 0
	}
	return manifest.Flags{
		Name:        o.name,
		GoModule:    o.goModule,
		DisplayName: o.displayName,
		Has: manifest.FlagPresence{
			Name: given("name"), GoModule: given("go-module"), Stacks: given("stacks"), Modules: given("modules"),
			DBName: given("db-name"), DBUser: given("db-user"), DisplayName: given("display-name"),
			PortBand: given("port-band"),
		},
		Stacks:     splitList(o.stacks),
		Modules:    splitList(o.modules),
		AllModules: o.allModules,
		Components: componentFlags(o.components),
		PortBand:   o.portBand,
		DBName:     o.dbName,
		DBUser:     o.dbUser,
		Services:   serviceFlags(o.services),
	}
}

// readSelection runs steps 2 to 5 of generate: the input file, the flags, the
// merge and the catalog-free checks. A file that cannot be read is a
// *readFailure.
func (o *generateOptions) readSelection(ctx *Context, dir string) (inputRun, *readFailure) {
	run := inputRun{view: issueView{dir: dir, components: typedComponents(o.components)}}
	var file *manifest.Input
	if path := o.file; path != "" {
		data, source, err := readSelectionFile(ctx, path)
		if err != nil {
			return run, &readFailure{err}
		}
		format, _ := pathFormat(path)
		if path == stdinPath {
			format = sniffFormat(data)
		}
		var tree *manifest.Node
		var parseIssues []issue.Issue
		if format == manifest.FormatYAML {
			tree, parseIssues = manifest.ParseYAML(source, data)
		} else {
			tree, parseIssues = manifest.ParseJSON(source, data)
		}
		run.issues = append(run.issues, parseIssues...)
		if tree != nil {
			decoded, decodeIssues := manifest.DecodeInput(source, tree)
			run.issues = append(run.issues, decodeIssues...)
			file = &decoded
		}
	}
	flags, flagIssues := manifest.FlagsInput(o.flagsOf())
	run.issues = append(run.issues, flagIssues...)
	merged, mergeIssues := manifest.Merge(file, flags)
	run.issues = append(run.issues, mergeIssues...)
	run.issues = append(run.issues, manifest.CheckMerged(merged)...)
	run.merged = merged
	if file != nil && overridesNamedComponents(file, flags) {
		source := file.Origins[""].Source
		run.notes = append(run.notes,
			fmt.Sprintf("--name changes derived names only; components named in %s keep their names", source))
	}
	return run, nil
}

// overridesNamedComponents reports that the flags replace a project name the
// file gave while the file names components.
func overridesNamedComponents(file *manifest.Input, flags manifest.Input) bool {
	if !file.Selection.Has.Name || !flags.Selection.Has.Name {
		return false
	}
	for _, component := range file.Selection.Components {
		if component.Name != "" {
			return true
		}
	}
	return false
}

// readSelectionFile reads the -f value: a file or standard input, at most
// maxInputBytes. It returns the data and the source name of its issue paths.
func readSelectionFile(ctx *Context, path string) (data []byte, source string, err error) {
	if path == stdinPath {
		if ctx.Stdin == nil {
			return nil, stdinSource, errors.New("standard input is not available")
		}
		data, err = readLimitedFrom(ctx.Stdin, maxInputBytes)
		return data, stdinSource, err
	}
	data, err = readInput(path)
	return data, filepath.ToSlash(path), err
}

// generateIssues returns the value problems of a line that also has
// command-line problems; it skips an input file it cannot read.
func generateIssues(ctx *Context, opts any, positionals []string) ([]issue.Issue, issueView) {
	o := opts.(*generateOptions)
	dir := ""
	if len(positionals) > 0 {
		dir = positionals[0]
	}
	if o.given == nil {
		o.given = map[string]int{}
	}
	if path := o.file; path != "" && !isKnownPath(path) {
		o.file = ""
	}
	run, failure := o.readSelection(ctx, dir)
	if failure != nil {
		return nil, run.view
	}
	return run.issues, run.view
}

func runGenerate(ctx *Context, opts any, positionals []string) int {
	o := opts.(*generateOptions)
	dir := ""
	if len(positionals) > 0 {
		dir = positionals[0]
	}
	run, failure := o.readSelection(ctx, dir)
	if failure != nil {
		return fail(ctx, "input_unreadable", codeInputUnreadable, failure.err)
	}
	selection := run.merged.Selection
	ctx.Logger.Info("selection_decoded",
		slog.Int("stack_count", len(selection.Stacks)),
		slog.Int("module_count", len(selection.Modules)),
		slog.Bool("all_modules", selection.AllModules))
	return generateProject(ctx, generateInput{
		root: o.root, catalogPath: o.catalog, dir: dir, run: run, dryRun: o.dryRun,
	})
}

type generateInput struct {
	root, catalogPath, dir string
	run                    inputRun
	// dryRun resolves and checks everything and writes nothing.
	dryRun bool
}

// lockSource returns the lock's source member for a revision, nil when the
// revision is unknown.
func lockSource(revision source.Revision) *lockfile.Source {
	if !revision.Known {
		return nil
	}
	return &lockfile.Source{Commit: revision.Commit, Modified: revision.Modified}
}

// backendStackIDs returns the sorted ids of the catalog's backend stacks.
func backendStackIDs(c catalog.Catalog) []string {
	var ids []string
	for _, stack := range c.Stacks {
		if stack.Role == roleBackend {
			ids = append(ids, stack.Stack)
		}
	}
	sort.Strings(ids)
	return ids
}

// moduleIDs returns the sorted, distinct module ids of every stack of c.
func moduleIDs(c catalog.Catalog) []string {
	seen := map[string]bool{}
	var ids []string
	for _, stack := range c.Stacks {
		for _, module := range stack.Modules {
			if !seen[module.Module] {
				seen[module.Module] = true
				ids = append(ids, module.Module)
			}
		}
	}
	sort.Strings(ids)
	return ids
}

// relocate rewrites the plan issues at selection#<pointer> to the origin of
// the value: the file, the flag or the folder argument that gave it.
func relocate(issues []issue.Issue, in manifest.Input) []issue.Issue {
	rewritten := make([]issue.Issue, len(issues))
	for i, found := range issues {
		pointer, isSelection := strings.CutPrefix(found.Path, selectionSource+"#")
		if isSelection {
			if path, line, ok := in.OriginPath(pointer); ok {
				found.Path, found.Line = path, line
			}
		}
		rewritten[i] = found
	}
	return rewritten
}

// folderNameIssues rewrites the message of a device-name issue about a name
// taken from the folder.
func folderNameIssues(issues []issue.Issue, name, dir string) []issue.Issue {
	argsDir := issue.Path(manifest.ArgsSource, "/dir")
	rewritten := make([]issue.Issue, len(issues))
	for i, found := range issues {
		if found.Path == argsDir && found.Code == plan.CodeNameReserved {
			found.Message = fmt.Sprintf("name %q comes from the folder %q and is a reserved device name on Windows", name, dir)
		}
		rewritten[i] = found
	}
	return rewritten
}

// generateProject runs everything after the selection is merged: catalog,
// defaults, plan, source and output checks, the in-memory build and the write.
func generateProject(ctx *Context, in generateInput) int {
	logger := ctx.Logger
	found, buildIssues, err := ctx.resolveSources(in.root, in.catalogPath)
	if err != nil {
		var failure *source.Error
		isFailure := errors.As(err, &failure)
		if isFailure && failure.Exit == source.ExitInternal && len(in.run.issues) > 0 {
			// The input problems are reportable without a catalog.
			return reportIssues(ctx, in.run.issues, in.run.view)
		}
		return reportSourceError(ctx, err, in.run.issues, in.run.view)
	}
	ref, sourceNotes := announceSources(ctx, found)
	if len(buildIssues) > 0 {
		return reportIssues(ctx, append(append([]issue.Issue(nil), in.run.issues...), buildIssues...), in.run.view)
	}
	built := found.Catalog

	workDir := ctx.WorkDir
	if workDir == "" {
		workDir = workDirOrDot()
	}
	resolvedInput, defaultNotes, outDir, defaultIssues := manifest.ResolveDefaults(in.run.merged, in.dir, workDir, backendStackIDs(built))
	issues := append(append([]issue.Issue(nil), in.run.issues...), defaultIssues...)
	notes := append(append(append([]string(nil), in.run.notes...), defaultNotes...), sourceNotes...)
	selection := resolvedInput.Selection
	issues = append(issues, multiComponentIssues(resolvedInput)...)

	root := found.Root.FS
	var resolved plan.Plan
	if !hasCode(issues, manifest.CodeSelectionInvalid, plan.CodeComponentMultiUnsupported) {
		var planIssues []issue.Issue
		resolved, planIssues = plan.Resolve(built, selection)
		issues = append(issues, planIssues...)
		if len(issues) == 0 {
			sourceIssues, err := plan.CheckSources(root, resolved)
			if err != nil {
				return fail(ctx, "sources_unreadable", codeInternalError, err)
			}
			issues = append(issues, sourceIssues...)
		}
	}
	displayDir := in.dir
	if displayDir == "" {
		displayDir = selection.Project.Name
	}
	outputIssues, err := emit.CheckOutput(outDir)
	if err != nil {
		return fail(ctx, "output_unreadable", codeInternalError, err)
	}
	for i := range outputIssues {
		outputIssues[i].Path = filepath.ToSlash(displayDir)
	}
	issues = append(issues, outputIssues...)
	for _, note := range notes {
		fmt.Fprintf(ctx.Stderr, "note: %s\n", note)
	}
	if len(issues) > 0 {
		issues = folderNameIssues(relocate(issues, resolvedInput), selection.Project.Name, in.dir)
		view := in.run.view
		view.dir = in.dir
		view.moduleIDs = moduleIDs(built)
		return reportIssues(ctx, issues, view)
	}
	ctx.Notes = notes

	var added []moduleAdded
	for _, pull := range plan.Pulls(resolved, selection) {
		logger.Info("module_required",
			slog.String("module", pull.Module),
			slog.String("required_by", strings.Join(pull.RequiredBy, ",")),
			slog.String("stack", pull.Stack))
		added = append(added, moduleAdded{Module: pull.Module, RequiredBy: pull.RequiredBy, Stack: pull.Stack})
	}

	digest := found.CatalogSHA256
	recorded := manifest.RecordedSelection(in.run.merged, resolved.Selection.Modules)
	project, err := emit.Build(emit.BuildOptions{
		Root:          root,
		Plan:          resolved,
		CatalogSHA256: digest,
		Source:        lockSource(found.Revision),
		Recorded:      recorded,
	})
	if err != nil {
		return fail(ctx, "project_build_failed", codeInternalError, err)
	}
	if in.dryRun {
		return writeDryRun(ctx, dryRun{
			path: displayDir, project: project, plan: resolved, resolvedInput: resolvedInput, recorded: recorded,
			source: ref, catalog: catalogOf(found), added: added,
		})
	}
	if err := emit.Write(outDir, project); err != nil {
		return fail(ctx, "project_write_failed", codeOutputUnwritable, err)
	}
	components := make([]componentInstalled, 0, len(resolved.Components))
	for _, component := range resolved.Components {
		components = append(components, componentInstalled{Stack: component.Stack, Name: component.Name})
	}
	var installed []moduleInstalled
	for _, stack := range resolved.Stacks {
		for _, module := range stack.Modules {
			installed = append(installed, moduleInstalled{Stack: stack.Stack, Module: module.Module, Version: module.Version})
		}
	}
	code := writeGenerate(ctx, generateResult{
		project:    resolved.Selection.Project.Name,
		path:       displayDir,
		files:      len(project.Files),
		source:     ref,
		catalog:    catalogOf(found),
		components: components,
		modules:    installed,
		added:      added,
	})
	if code != exitOK {
		return code
	}
	logger.Info("project_generated",
		slog.Int("file_count", len(project.Files)),
		slog.Int("module_count", len(resolved.Selection.Modules)))
	return exitOK
}

// hasCode reports whether an issue carries one of codes.
func hasCode(issues []issue.Issue, codes ...string) bool {
	for _, found := range issues {
		if slices.Contains(codes, found.Code) {
			return true
		}
	}
	return false
}

// workDirOrDot returns the process's working directory, or "." when unknown.
func workDirOrDot() string {
	if wd := workDir(); wd != "" {
		return wd
	}
	return "."
}

// readLimitedFrom reads at most limit bytes from r.
func readLimitedFrom(r io.Reader, limit int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("input exceeds %d bytes", limit)
	}
	return data, nil
}
