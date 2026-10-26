// Command initializer generates a project from the Gatbase catalog.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
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

const usageText = `usage:
  initializer catalog build --root <repo> --files <file> --out <file>
  initializer generate --root <repo> --catalog <file> --out <dir> (--selection <file> | --name <n> --go-module <m> --stacks <a,b> (--modules <a,b> | --all-modules))
  initializer verify --project <dir>
`

var errNotImplemented = errors.New("not implemented")

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one command line and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	if len(args) == 0 {
		return usageError(logger, "a subcommand is required")
	}
	switch args[0] {
	case "-h", "--help", "help":
		_, _ = io.WriteString(stdout, usageText)
		return exitOK
	case "catalog":
		if len(args) < 2 || args[1] != "build" {
			return usageError(logger, "catalog requires the build subcommand")
		}
		return runCatalogBuild(args[2:], stdout, stderr, logger)
	case "generate":
		return runGenerate(args[1:], stdout, stderr, logger)
	case "verify":
		return runVerify(args[1:], stdout, logger)
	}
	return usageError(logger, "unknown subcommand")
}

func usageError(logger *slog.Logger, message string) int {
	logger.Error("usage_error", slog.String("error", message))
	return exitUsage
}

func notImplemented(logger *slog.Logger, command string) int {
	logger.Error("command_failed",
		slog.String("command", command),
		slog.String("error", errNotImplemented.Error()))
	return exitInternal
}

// newFlagSet returns a flag set that reports errors through run, not the process.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// parseFlags parses args and reports the first problem as a usage error.
func parseFlags(fs *flag.FlagSet, args []string, stdout io.Writer, logger *slog.Logger) (code int, done bool) {
	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		_, _ = io.WriteString(stdout, usageText)
		return exitOK, true
	}
	if err != nil {
		return usageError(logger, err.Error()), true
	}
	if fs.NArg() > 0 {
		return usageError(logger, "unexpected argument"), true
	}
	return exitOK, false
}

func missingFlag(fs *flag.FlagSet, names ...string) string {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	for _, name := range names {
		if !set[name] {
			return name
		}
	}
	return ""
}

func runCatalogBuild(args []string, stdout, stderr io.Writer, logger *slog.Logger) int {
	fs := newFlagSet("catalog build")
	root := fs.String("root", "", "repository root")
	filesPath := fs.String("files", "", "tracked file list")
	outPath := fs.String("out", "", "catalog output file")
	if code, done := parseFlags(fs, args, stdout, logger); done {
		return code
	}
	if name := missingFlag(fs, "root", "files", "out"); name != "" {
		return usageError(logger, "missing required flag: "+name)
	}

	info, err := os.Stat(*root)
	if err != nil || !info.IsDir() {
		logger.Error("input_unreadable", slog.String("error", "root is not a readable directory"))
		return exitInternal
	}
	list, err := readLimited(*filesPath, maxFileListBytes)
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
		Root:      os.DirFS(*root),
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
	if err := writeOutput(*outPath, encoded); err != nil {
		logger.Error("output_unwritable", slog.String("error", err.Error()))
		return exitInternal
	}
	sum := sha256.Sum256(encoded)
	result := struct {
		Catalog string `json:"catalog"`
		SHA256  string `json:"sha256"`
	}{Catalog: filepath.ToSlash(*outPath), SHA256: hex.EncodeToString(sum[:])}
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

func runVerify(args []string, stdout io.Writer, logger *slog.Logger) int {
	fs := newFlagSet("verify")
	fs.String("project", "", "generated project directory")
	if code, done := parseFlags(fs, args, stdout, logger); done {
		return code
	}
	if name := missingFlag(fs, "project"); name != "" {
		return usageError(logger, "missing required flag: "+name)
	}
	return notImplemented(logger, "verify")
}

func runGenerate(args []string, stdout, stderr io.Writer, logger *slog.Logger) int {
	fs := newFlagSet("generate")
	fs.String("root", "", "repository root")
	fs.String("catalog", "", "catalog file")
	fs.String("out", "", "output directory")
	selectionFile := fs.String("selection", "", "selection file")
	name := fs.String("name", "", "project name")
	goModule := fs.String("go-module", "", "Go module path")
	stacks := fs.String("stacks", "", "comma-separated stacks")
	modules := fs.String("modules", "", "comma-separated modules")
	allModules := fs.Bool("all-modules", false, "select every module of the backend stack")
	if code, done := parseFlags(fs, args, stdout, logger); done {
		return code
	}
	if missing := missingFlag(fs, "root", "catalog", "out"); missing != "" {
		return usageError(logger, "missing required flag: "+missing)
	}

	hasFile := missingFlag(fs, "selection") == ""
	hasFlags := false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "name", "go-module", "stacks", "modules", "all-modules":
			hasFlags = true
		}
	})
	hasModules := missingFlag(fs, "modules") == ""
	switch {
	case hasFile && hasFlags:
		return usageError(logger, "--selection cannot be combined with selection flags")
	case !hasFile && !hasFlags:
		return usageError(logger, "a selection is required: --selection or selection flags")
	case *allModules && hasModules:
		return usageError(logger, "--all-modules cannot be combined with --modules")
	case !hasFile && !*allModules && !hasModules:
		return usageError(logger, "flag selection requires --modules or --all-modules")
	}

	var selection manifest.Selection
	var issues []issue.Issue
	if hasFile {
		data, err := readInput(*selectionFile)
		if err != nil {
			logger.Error("input_unreadable", slog.String("error", err.Error()))
			return exitInternal
		}
		selection, issues = manifest.DecodeSelection(filepath.ToSlash(*selectionFile), data)
	} else {
		selection, issues = manifest.SelectionFromFlags(manifest.SelectionFlags{
			Name:       *name,
			GoModule:   *goModule,
			Stacks:     splitList(*stacks),
			Modules:    splitList(*modules),
			AllModules: *allModules,
		})
	}
	if len(issues) > 0 {
		logger.Warn("validation_failed", slog.Int("issue_count", len(issues)))
		if err := issue.Write(stderr, issues); err != nil {
			logger.Error("issues_unwritable", slog.String("error", err.Error()))
			return exitInternal
		}
		return exitIssues
	}
	logger.Info("selection_decoded",
		slog.Int("stack_count", len(selection.Stacks)),
		slog.Int("module_count", len(selection.Modules)),
		slog.Bool("all_modules", selection.AllModules))
	return notImplemented(logger, "generate")
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
