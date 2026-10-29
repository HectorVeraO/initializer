// Command initializer generates a project from the module catalog.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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
	Args   []string
	Logger *slog.Logger
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
		Logger: slog.New(slog.NewJSONHandler(stderr, nil)),
	}
	return runWith(ctx, args)
}

// runWith executes one command line against ctx and returns the exit code.
func runWith(ctx *Context, args []string) int {
	ctx.Args = args
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
	fmt.Fprintf(ctx.Stderr, "error: %s needs a command: %s\n\nCommands:\n", group, strings.Join(verbs, ", "))
	writeCommandRows(ctx.Stderr, groupCommands(group))
	ctx.Logger.Error("usage_error", slog.Int("problem_count", 1))
	return exitUsage
}

// runCommand scans args against cmd's flags, reports every command-line
// problem at once, warns about deprecated spellings and runs the command.
func runCommand(ctx *Context, cmd *Command, args []string) int {
	set := newFlagSet(strings.Join(cmd.Path, " "))
	opts := cmd.Flags(set)
	var wantsHelp bool
	set.boolean(&wantsHelp, flagSpec{Name: "help", Short: "h", Hidden: true})
	positionals, problems := scan(set, args)
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
		if spec.Deprecated != "" && set.given[spec.Name] > 0 {
			fmt.Fprintf(ctx.Stderr, "warning: --%s is deprecated; %s\n", spec.Name, spec.Deprecated)
		}
	}
	return cmd.Run(ctx, opts, positionals)
}

// reportProblems writes every problem, a count and the usage_error event; it
// returns the usage exit code.
func reportProblems(ctx *Context, problems []Problem) int {
	for _, problem := range problems {
		if problem.Arg == "" {
			fmt.Fprintf(ctx.Stderr, "error: %s\n", problem.Message)
		} else {
			fmt.Fprintf(ctx.Stderr, "error: %s: %s\n", problem.Arg, problem.Message)
		}
		if problem.Suggestion != "" {
			fmt.Fprintf(ctx.Stderr, "  hint: did you mean %q?\n", problem.Suggestion)
		}
	}
	noun := "problems"
	if len(problems) == 1 {
		noun = "problem"
	}
	fmt.Fprintf(ctx.Stderr, "%d %s.\n", len(problems), noun)
	ctx.Logger.Error("usage_error", slog.Int("problem_count", len(problems)))
	return exitUsage
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
	stdout, stderr, logger := ctx.Stdout, ctx.Stderr, ctx.Logger

	info, err := os.Stat(o.root)
	if err != nil || !info.IsDir() {
		logger.Error("input_unreadable", slog.String("error", "root is not a readable directory"))
		return exitInternal
	}
	list, err := readLimited(o.files, maxFileListBytes)
	if err != nil {
		logger.Error("input_unreadable", slog.String("error", err.Error()))
		return exitInternal
	}
	files, err := catalog.ParseFileList(list)
	if err != nil {
		logger.Error("input_unreadable", slog.String("error", err.Error()))
		return exitInternal
	}

	built, issues, err := catalog.Build(catalog.BuildOptions{
		Root:      os.DirFS(o.root),
		Files:     files,
		Generator: generator.Version,
	})
	if err != nil {
		logger.Error("catalog_build_failed", slog.String("error", err.Error()))
		return exitInternal
	}
	if len(issues) > 0 {
		logger.Warn("validation_failed", slog.Int("issue_count", len(issues)))
		if err := issue.Write(stderr, issues); err != nil {
			logger.Error("issues_unwritable", slog.String("error", err.Error()))
			return exitInternal
		}
		return exitIssues
	}
	encoded, err := catalog.Encode(built)
	if err != nil {
		logger.Error("catalog_build_failed", slog.String("error", err.Error()))
		return exitInternal
	}
	if err := writeOutput(o.out, encoded); err != nil {
		logger.Error("output_unwritable", slog.String("error", err.Error()))
		return exitInternal
	}
	sum := sha256.Sum256(encoded)
	result := struct {
		Catalog string `json:"catalog"`
		SHA256  string `json:"sha256"`
	}{Catalog: filepath.ToSlash(o.out), SHA256: hex.EncodeToString(sum[:])}
	line, err := json.Marshal(result)
	if err != nil {
		logger.Error("catalog_build_failed", slog.String("error", err.Error()))
		return exitInternal
	}
	if _, err := stdout.Write(append(line, '\n')); err != nil {
		logger.Error("result_unwritable", slog.String("error", err.Error()))
		return exitInternal
	}
	logger.Info("catalog_built",
		slog.Int("spec_count", len(built.Specs)),
		slog.Int("stack_count", len(built.Stacks)),
		slog.String("sha256", result.SHA256))
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
	stdout, stderr, logger := ctx.Stdout, ctx.Stderr, ctx.Logger
	dir := "."
	if o.project != "" {
		dir = o.project
	}
	if len(positionals) > 0 {
		dir = positionals[0]
	}
	differences, err := verify.Project(dir)
	if err != nil {
		logger.Error("verify_unreadable", slog.String("error", err.Error()))
		return exitInternal
	}
	result := struct {
		Project     string `json:"project"`
		Differences int    `json:"differences"`
	}{Project: filepath.ToSlash(dir), Differences: len(differences)}
	if code := writeResult(stdout, result, logger); code != exitOK {
		return code
	}
	if len(differences) > 0 {
		logger.Warn("verify_failed", slog.Int("difference_count", len(differences)))
		if err := issue.Write(stderr, differences); err != nil {
			logger.Error("issues_unwritable", slog.String("error", err.Error()))
			return exitInternal
		}
		return exitIssues
	}
	return exitOK
}

// writeResult writes v as one JSON line on stdout.
func writeResult(stdout io.Writer, v any, logger *slog.Logger) int {
	line, err := json.Marshal(v)
	if err != nil {
		logger.Error("result_unencodable", slog.String("error", err.Error()))
		return exitInternal
	}
	if _, err := stdout.Write(append(line, 0x0a)); err != nil {
		logger.Error("result_unwritable", slog.String("error", err.Error()))
		return exitInternal
	}
	return exitOK
}

func runGenerate(ctx *Context, opts any, positionals []string) int {
	o := opts.(*generateOptions)
	stdout, stderr, logger := ctx.Stdout, ctx.Stderr, ctx.Logger
	dir := o.out
	if len(positionals) > 0 {
		dir = positionals[0]
	}

	var selection manifest.Selection
	var issues []issue.Issue
	var notes []string
	if filePath := o.filePath(); filePath != "" {
		data, err := readInput(filePath)
		if err != nil {
			logger.Error("input_unreadable", slog.String("error", err.Error()))
			return exitInternal
		}
		selection, issues = manifest.DecodeSelection(filepath.ToSlash(filePath), data)
	} else {
		name := o.name
		fromFolder := false
		switch {
		case name != "":
		case dir != "":
			name, fromFolder = folderName(dir), true
			notes = append(notes, fmt.Sprintf("note: project name %q comes from the folder; pass --name to set it", name))
		default:
			name = defaultProjectName
			notes = append(notes, fmt.Sprintf("note: no name given; using %q", name))
		}
		selection, issues = manifest.SelectionFromFlags(manifest.SelectionFlags{
			Name:       name,
			GoModule:   o.goModule,
			Stacks:     splitList(o.stacks),
			Modules:    splitList(o.modules),
			AllModules: o.allModules,
		})
		if fromFolder {
			issues = folderNameIssues(issues, name, dir)
		}
	}
	if len(issues) > 0 {
		logger.Warn("validation_failed", slog.Int("issue_count", len(issues)))
		if err := issue.Write(stderr, issues); err != nil {
			logger.Error("issues_unwritable", slog.String("error", err.Error()))
			return exitInternal
		}
		return exitIssues
	}
	for _, note := range notes {
		fmt.Fprintln(stderr, note)
	}
	if dir == "" {
		dir = selection.Project.Name
	}
	logger.Info("selection_decoded",
		slog.Int("stack_count", len(selection.Stacks)),
		slog.Int("module_count", len(selection.Modules)),
		slog.Bool("all_modules", selection.AllModules))
	return generateProject(generateInput{
		root: o.root, catalogPath: o.catalog, out: dir, selection: selection,
	}, stdout, stderr, logger)
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
	nameIssue := issue.Path(manifest.FlagsSource, issue.Pointer("project", "name"))
	rewritten := make([]issue.Issue, 0, len(issues))
	for _, found := range issues {
		if found.Path == nameIssue {
			found = issue.Issue{
				Code:    manifest.CodeSelectionInvalid,
				Path:    issue.Path("args", issue.Pointer("dir")),
				Message: fmt.Sprintf("name %q comes from the folder %q and is not a project name", name, dir),
			}
		}
		rewritten = append(rewritten, found)
	}
	return rewritten
}

type generateInput struct {
	root, catalogPath, out string
	selection              manifest.Selection
}

// generateProject runs everything after the selection is decoded: catalog,
// plan, source and output checks, the in-memory build and the write.
func generateProject(in generateInput, stdout, stderr io.Writer, logger *slog.Logger) int {
	info, err := os.Stat(in.root)
	if err != nil || !info.IsDir() {
		logger.Error("input_unreadable", slog.String("error", "root is not a readable directory"))
		return exitInternal
	}
	catalogBytes, err := readLimited(in.catalogPath, maxFileListBytes)
	if err != nil {
		logger.Error("input_unreadable", slog.String("error", err.Error()))
		return exitInternal
	}
	built, decodeIssues := catalog.Decode(catalogBytes)
	if len(decodeIssues) > 0 {
		logger.Error("catalog_undecodable", slog.Int("issue_count", len(decodeIssues)),
			slog.String("error", decodeIssues[0].Message))
		return exitInternal
	}
	resolved, issues := plan.Resolve(built, in.selection)
	root := os.DirFS(in.root)
	if len(issues) == 0 {
		sourceIssues, err := plan.CheckSources(root, resolved)
		if err != nil {
			logger.Error("sources_unreadable", slog.String("error", err.Error()))
			return exitInternal
		}
		issues = append(issues, sourceIssues...)
	}
	outputIssues, err := emit.CheckOutput(in.out)
	if err != nil {
		logger.Error("output_unreadable", slog.String("error", err.Error()))
		return exitInternal
	}
	for i := range outputIssues {
		if outputIssues[i].Code == emit.CodeOutputNotEmpty {
			outputIssues[i].Hint = "choose another folder: initializer generate <dir>"
		}
	}
	issues = append(issues, outputIssues...)
	if len(issues) > 0 {
		logger.Warn("validation_failed", slog.Int("issue_count", len(issues)))
		if err := issue.Write(stderr, issues); err != nil {
			logger.Error("issues_unwritable", slog.String("error", err.Error()))
			return exitInternal
		}
		return exitIssues
	}

	for _, pull := range plan.Pulls(resolved, in.selection) {
		logger.Info("module_required",
			slog.String("module", pull.Module),
			slog.String("required_by", strings.Join(pull.RequiredBy, ",")),
			slog.String("stack", pull.Stack))
	}

	sum := sha256.Sum256(catalogBytes)
	project, err := emit.Build(emit.BuildOptions{Root: root, Plan: resolved, CatalogSHA256: hex.EncodeToString(sum[:])})
	if err != nil {
		logger.Error("project_build_failed", slog.String("error", err.Error()))
		return exitInternal
	}
	if err := emit.Write(in.out, project); err != nil {
		logger.Error("project_write_failed", slog.String("error", err.Error()))
		return exitInternal
	}
	result := struct {
		Project string `json:"project"`
		Files   int    `json:"files"`
	}{Project: filepath.ToSlash(in.out), Files: len(project.Files)}
	if code := writeResult(stdout, result, logger); code != exitOK {
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
