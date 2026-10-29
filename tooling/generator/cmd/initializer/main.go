// Command initializer generates a project from the module catalog.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/emit"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/verify"
)

// Exit codes.
const (
	exitOK       = 0
	exitIssues   = 1
	exitUsage    = 2
	exitInternal = 3
)

// maxInputBytes bounds every input file read from disk.
const maxInputBytes = 1 << 20

// maxFileListBytes bounds the tracked file list.
const maxFileListBytes = 32 << 20

// Context is what a command reads and writes; tests pass fakes.
type Context struct {
	Stdout, Stderr io.Writer
	Stdin          io.Reader
	// Args is the original command line.
	Args []string
	// Format is "text" or "json".
	Format string
	// Logger discards every event unless --log json is set.
	Logger *slog.Logger
	// Notes and Warnings are collected for the result document.
	Notes    []string
	Warnings []Warning
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one command line and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	ctx := &Context{
		Stdout: stdout,
		Stderr: stderr,
		Stdin:  os.Stdin,
		Format: formatText,
		Logger: discardLogger(),
	}
	return runWith(ctx, args)
}

// useOutputOptions applies --format and --log to ctx.
func useOutputOptions(ctx *Context, format string, logJSON bool) {
	ctx.Format = format
	if logJSON {
		ctx.Logger = slog.New(slog.NewJSONHandler(ctx.Stderr, nil))
	}
}

// runWith executes one command line against ctx and returns the exit code.
func runWith(ctx *Context, args []string) int {
	ctx.Args = args
	if ctx.Logger == nil {
		ctx.Logger = discardLogger()
	}
	if ctx.Format == "" {
		ctx.Format = formatText
	}
	if len(args) == 0 {
		writeOverview(ctx.Stderr)
		return exitUsage
	}
	if args[0] == "-h" || args[0] == "--help" {
		writeOverview(ctx.Stdout)
		return exitOK
	}
	found := resolve(args)
	switch {
	case found.problem != nil:
		format, logJSON := lookAhead(args)
		useOutputOptions(ctx, format, logJSON)
		return reportProblems(ctx, []Problem{*found.problem})
	case found.group != "":
		return runGroup(ctx, found.group, args[1:])
	}
	return runCommand(ctx, found.cmd, args[found.consumed:])
}

// runGroup answers a noun group named without a verb: its help on request,
// else a usage error that lists the verbs.
func runGroup(ctx *Context, group string, rest []string) int {
	if len(rest) > 0 && (rest[0] == "-h" || rest[0] == "--help") {
		writeGroupHelp(ctx.Stdout, group)
		return exitOK
	}
	var verbs []string
	for _, cmd := range groupCommands(group) {
		verbs = append(verbs, cmd.Path[1])
	}
	format, logJSON := lookAhead(rest)
	useOutputOptions(ctx, format, logJSON)
	if ctx.isJSON() {
		return reportProblems(ctx, []Problem{{Arg: group, Message: "needs a command: " + strings.Join(verbs, ", ")}})
	}
	fmt.Fprintf(ctx.Stderr, "error: %s needs a command: %s\n\nCommands:\n", group, strings.Join(verbs, ", "))
	writeCommandRows(ctx.Stderr, groupCommands(group))
	ctx.Logger.Error("usage_error", slog.Int("problem_count", 1))
	return exitUsage
}

// registerFlags registers the flags of cmd on set: its own, then --format and
// --log (every command but help). It returns the bound options and the output
// options (nil for help).
func registerFlags(set *flagSet, cmd *Command) (opts any, out *outputOptions) {
	if cmd.Flags != nil {
		opts = cmd.Flags(set)
	}
	if cmd.Path[0] != "help" {
		out = registerOutputFlags(set)
	}
	return opts, out
}

// runCommand scans args against cmd's flags, reports every command-line
// problem at once, warns about deprecated spellings and runs the command.
func runCommand(ctx *Context, cmd *Command, args []string) int {
	set := newFlagSet(strings.Join(cmd.Path, " "))
	opts, out := registerFlags(set, cmd)
	var wantsHelp bool
	set.boolean(&wantsHelp, flagSpec{Name: "help", Short: "h", Hidden: true})
	positionals, problems := scan(set, args)
	if out != nil {
		useOutputOptions(ctx, out.format, out.log == formatJSON)
	}
	if set.given["help"] > 0 {
		writeCommandHelp(ctx.Stdout, cmd)
		return exitOK
	}
	if cmd.MaxArgs >= 0 && len(positionals) > cmd.MaxArgs {
		for _, extra := range positionals[cmd.MaxArgs:] {
			problems = append(problems, Problem{Arg: extra, Message: "unexpected argument"})
		}
	}
	if cmd.Check != nil {
		problems = append(problems, cmd.Check(set, opts, positionals)...)
	}
	if len(problems) > 0 {
		return reportProblems(ctx, problems)
	}
	for _, spec := range set.specs {
		if spec.Deprecated == "" || set.given[spec.Name] == 0 {
			continue
		}
		message := fmt.Sprintf("--%s is deprecated", spec.Name)
		fmt.Fprintf(ctx.Stderr, "warning: %s; %s\n", message, spec.Deprecated)
		ctx.Warnings = append(ctx.Warnings, Warning{Code: codeFlagDeprecated, Message: message, Hint: spec.Deprecated})
		ctx.Logger.Warn("flag_deprecated", slog.String("flag", "--"+spec.Name))
	}
	return cmd.Run(ctx, opts, positionals)
}

// runHelp prints the overview, a group's command list or one command's help.
func runHelp(ctx *Context, _ any, words []string) int {
	if len(words) == 0 {
		writeOverview(ctx.Stdout)
		return exitOK
	}
	found := resolve(words)
	switch {
	case found.problem != nil:
		return reportProblems(ctx, []Problem{*found.problem})
	case found.group != "":
		writeGroupHelp(ctx.Stdout, found.group)
		return exitOK
	case found.consumed < len(words):
		return reportProblems(ctx, []Problem{{Arg: words[found.consumed], Message: "unexpected argument"}})
	}
	writeCommandHelp(ctx.Stdout, found.cmd)
	return exitOK
}

func runCatalogBuild(ctx *Context, opts any, _ []string) int {
	o := opts.(*catalogBuildOptions)

	info, err := os.Stat(o.root)
	if err != nil || !info.IsDir() {
		return fail(ctx, "input_unreadable", codeInputUnreadable, errors.New("root is not a readable directory"))
	}
	fileList, err := readLimited(o.files, maxFileListBytes)
	if err != nil {
		return fail(ctx, "input_unreadable", codeInputUnreadable, err)
	}
	files, err := catalog.ParseFileList(fileList)
	if err != nil {
		return fail(ctx, "input_unreadable", codeInputUnreadable, err)
	}

	built, issues, err := catalog.Build(catalog.BuildOptions{
		Root:      os.DirFS(o.root),
		Files:     files,
		Generator: generator.Version,
	})
	if err != nil {
		return fail(ctx, "catalog_build_failed", codeInternalError, err)
	}
	if len(issues) > 0 {
		return reportIssues(ctx, issues, issueView{})
	}
	encoded, err := catalog.Encode(built)
	if err != nil {
		return fail(ctx, "catalog_build_failed", codeInternalError, err)
	}
	if err := writeOutput(o.out, encoded); err != nil {
		return fail(ctx, "output_unwritable", codeOutputUnwritable, err)
	}
	sum := sha256.Sum256(encoded)
	digest := hex.EncodeToString(sum[:])
	if code := writeCatalogBuilt(ctx, o.out, digest); code != exitOK {
		return code
	}
	ctx.Logger.Info("catalog_built",
		slog.Int("spec_count", len(built.Specs)),
		slog.Int("stack_count", len(built.Stacks)),
		slog.String("sha256", digest))
	return exitOK
}

// writeOutput writes data to path, creating the parent directory.
func writeOutput(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func runVerify(ctx *Context, opts any, positionals []string) int {
	o := opts.(*verifyOptions)
	dir := "."
	if o.project != "" {
		dir = o.project
	}
	if len(positionals) > 0 {
		dir = positionals[0]
	}
	report, err := verify.Check(dir)
	if err != nil {
		return fail(ctx, "verify_unreadable", codeVerifyUnreadable, err)
	}
	if len(report.Differences) > 0 {
		ctx.Logger.Warn("verify_failed", slog.Int("difference_count", len(report.Differences)))
	}
	return writeVerify(ctx, report.Project, report.Differences)
}

func runGenerate(ctx *Context, opts any, positionals []string) int {
	o := opts.(*generateOptions)
	dir := o.out
	if len(positionals) > 0 {
		dir = positionals[0]
	}

	var selection manifest.Selection
	var issues []issue.Issue
	var notes []string
	fromFolder := false
	if filePath := o.filePath(); filePath != "" {
		data, err := readInput(filePath)
		if err != nil {
			return fail(ctx, "input_unreadable", codeInputUnreadable, err)
		}
		selection, issues = manifest.DecodeSelection(filepath.ToSlash(filePath), data)
		issues = append(issues, multiComponentIssues(selection.Components, filepath.ToSlash(filePath))...)
	} else {
		name := o.name
		switch {
		case name != "":
		case dir != "":
			name, fromFolder = folderName(dir), true
			notes = append(notes, fmt.Sprintf("project name %q comes from the folder; pass --name to set it", name))
		default:
			name = defaultProjectName
			notes = append(notes, fmt.Sprintf("no name given; using %q", name))
		}
		selection, issues = manifest.SelectionFromFlags(manifest.SelectionFlags{
			Name:       name,
			GoModule:   o.goModule,
			Stacks:     splitList(o.stacks),
			Modules:    splitList(o.modules),
			AllModules: o.allModules,
			Components: componentSelection(o.components),
		})
		issues = append(issues, multiComponentIssues(selection.Components, "")...)
		if fromFolder {
			issues = folderNameIssues(issues, name, dir)
		}
	}
	if len(issues) > 0 {
		return reportIssues(ctx, issues, issueView{dir: dir, selectionFile: o.filePath(), components: typedComponents(o.components)})
	}
	for _, note := range notes {
		fmt.Fprintf(ctx.Stderr, "note: %s\n", note)
	}
	ctx.Notes = notes
	if dir == "" {
		dir = selection.Project.Name
	}
	ctx.Logger.Info("selection_decoded",
		slog.Int("stack_count", len(selection.Stacks)),
		slog.Int("module_count", len(selection.Modules)),
		slog.Bool("all_modules", selection.AllModules))
	return generateProject(ctx, generateInput{
		root: o.root, catalogPath: o.catalog, out: dir, selection: selection, selectionFile: o.filePath(),
		typedComponents: typedComponents(o.components), nameFromFolder: fromFolder,
	})
}

// defaultProjectName names a project when neither a flag nor a folder does.
const defaultProjectName = "demo"

// folderName returns the base name of dir as an absolute, cleaned path; for
// "." that is the working directory's name. It never lowercases or repairs it.
func folderName(dir string) string {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	return filepath.Base(absolute)
}

// folderNameIssues rewrites the issue about a name taken from a folder so it
// points at the DIR argument instead of a flag the person never typed.
func folderNameIssues(issues []issue.Issue, name, dir string) []issue.Issue {
	pointer := issue.Pointer("project", "name")
	nameIssues := map[string]bool{
		issue.Path(manifest.FlagsSource, pointer): true,
		issue.Path("selection", pointer):          true,
	}
	rewritten := make([]issue.Issue, 0, len(issues))
	for _, found := range issues {
		if nameIssues[found.Path] {
			message := fmt.Sprintf("name %q comes from the folder %q and is not a project name", name, dir)
			if found.Code == plan.CodeNameReserved {
				message = fmt.Sprintf("name %q comes from the folder %q and is a reserved device name on Windows", name, dir)
			}
			found = issue.Issue{
				Code:    found.Code,
				Path:    issue.Path("args", issue.Pointer("dir")),
				Message: message,
			}
		}
		rewritten = append(rewritten, found)
	}
	return rewritten
}

type generateInput struct {
	root, catalogPath, out, selectionFile string
	selection                             manifest.Selection
	// typedComponents are the --component values as typed, in flag order.
	typedComponents []string
	// nameFromFolder reports that the project name is the base name of out.
	nameFromFolder bool
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

// generateProject runs everything after the selection is decoded: catalog,
// plan, source and output checks, the in-memory build and the write.
func generateProject(ctx *Context, in generateInput) int {
	logger := ctx.Logger
	info, err := os.Stat(in.root)
	if err != nil || !info.IsDir() {
		return fail(ctx, "input_unreadable", codeInputUnreadable, errors.New("root is not a readable directory"))
	}
	catalogBytes, err := readLimited(in.catalogPath, maxFileListBytes)
	if err != nil {
		return fail(ctx, "input_unreadable", codeCatalogUnreadable, err)
	}
	built, decodeIssues := catalog.Decode(catalogBytes)
	if len(decodeIssues) > 0 {
		cause := errors.New(decodeIssues[0].Message)
		logger.Error("catalog_undecodable", slog.Int("issue_count", len(decodeIssues)), slog.String("error", cause.Error()))
		return reportFailure(ctx, codeCatalogUnreadable, cause.Error())
	}
	resolved, issues := plan.Resolve(built, in.selection)
	root := os.DirFS(in.root)
	if len(issues) == 0 {
		sourceIssues, err := plan.CheckSources(root, resolved)
		if err != nil {
			return fail(ctx, "sources_unreadable", codeInternalError, err)
		}
		issues = append(issues, sourceIssues...)
	}
	outputIssues, err := emit.CheckOutput(in.out)
	if err != nil {
		return fail(ctx, "output_unreadable", codeInternalError, err)
	}
	issues = append(issues, outputIssues...)
	if len(issues) > 0 {
		if in.nameFromFolder {
			issues = folderNameIssues(issues, in.selection.Project.Name, in.out)
		}
		if in.selectionFile == "" {
			issues = flagOrigin(issues)
		}
		return reportIssues(ctx, issues, issueView{
			dir: in.out, moduleIDs: moduleIDs(built), selectionFile: in.selectionFile, components: in.typedComponents,
		})
	}

	var added []moduleAdded
	for _, pull := range plan.Pulls(resolved, in.selection) {
		logger.Info("module_required",
			slog.String("module", pull.Module),
			slog.String("required_by", strings.Join(pull.RequiredBy, ",")),
			slog.String("stack", pull.Stack))
		added = append(added, moduleAdded{Module: pull.Module, RequiredBy: pull.RequiredBy, Stack: pull.Stack})
	}

	sum := sha256.Sum256(catalogBytes)
	digest := hex.EncodeToString(sum[:])
	project, err := emit.Build(emit.BuildOptions{Root: root, Plan: resolved, CatalogSHA256: digest})
	if err != nil {
		return fail(ctx, "project_build_failed", codeInternalError, err)
	}
	if err := emit.Write(in.out, project); err != nil {
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
		path:       in.out,
		files:      len(project.Files),
		catalog:    catalogRef{SHA256: digest, File: filepath.ToSlash(in.catalogPath)},
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

func splitList(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, ",")
}

// readInput reads a file of at most maxInputBytes.
func readInput(path string) ([]byte, error) {
	return readLimited(path, maxInputBytes)
}

// readLimited reads a file of at most limit bytes.
func readLimited(path string, limit int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("input exceeds %d bytes", limit)
	}
	return data, nil
}
