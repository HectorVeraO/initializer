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
	"strings"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
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
	// WorkDir is the working directory DIR is relative to; "" means the
	// process's.
	WorkDir string
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
		Stdout:  stdout,
		Stderr:  stderr,
		Stdin:   os.Stdin,
		WorkDir: workDir(),
		Format:  formatText,
		Logger:  discardLogger(),
	}
	return runWith(ctx, args)
}

// workDir returns the process's working directory, or "" when it is unknown.
func workDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
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
	if cmd.Bind != nil {
		cmd.Bind(set, opts)
	}
	if len(problems) > 0 {
		var issues []issue.Issue
		var view issueView
		if cmd.Issues != nil {
			issues, view = cmd.Issues(ctx, opts, positionals)
		}
		return reportProblemsAndIssues(ctx, problems, issues, view)
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
	return readLimitedFrom(file, limit)
}
