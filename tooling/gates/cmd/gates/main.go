// Command gates runs the repository's rule checks.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/HectorVeraO/initializer/tooling/gates/internal/contractgate"
	"github.com/HectorVeraO/initializer/tooling/gates/internal/flagscheck"
	"github.com/HectorVeraO/initializer/tooling/gates/internal/migrationlint"
	"github.com/HectorVeraO/initializer/tooling/gates/internal/repo"
	"github.com/HectorVeraO/initializer/tooling/gates/internal/report"
	"github.com/HectorVeraO/initializer/tooling/gates/internal/versions"
)

const (
	exitOK       = 0
	exitViolated = 1
	exitUsage    = 2

	defaultBaseRev  = "origin/main"
	defaultWarnDays = 7
	flagsFileName   = "flags.yaml"
	usage           = "usage: gates flags --root <dir> [--base-rev <rev>] [--today YYYY-MM-DD] [--warn-days N]\n       gates migrations --root <dir> [--base-rev <rev>]\n       gates contracts --root <dir> [--base-rev <rev>] [--oasdiff <path>]\n       gates versions --root <dir> [--base-rev <rev>]\n       gates tags --root <dir>"
)

func main() {
	code := run(os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(code)
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWith(args, stdout, stderr, time.Now, os.Getenv)
}

// runWith is run with the clock and the environment injected.
func runWith(args []string, stdout, stderr io.Writer, now func() time.Time, getenv func(string) string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return exitUsage
	}
	switch args[0] {
	case "flags":
		return runFlags(args[1:], stdout, stderr, now, getenv)
	case "migrations":
		return runMigrations(args[1:], stdout, stderr, getenv)
	case "contracts":
		return runContracts(args[1:], stdout, stderr, getenv)
	case "versions":
		return runVersions(args[1:], stdout, stderr, getenv)
	case "tags":
		return runTags(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n%s\n", args[0], usage)
		return exitUsage
	}
}

func runFlags(args []string, stdout, stderr io.Writer, now func() time.Time, getenv func(string) string) int {
	fs := flag.NewFlagSet("gates flags", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root")
	baseRev := fs.String("base-rev", "", "base revision (default origin/main)")
	todayText := fs.String("today", "", "override today's date, YYYY-MM-DD")
	warnDays := fs.Int("warn-days", defaultWarnDays, "warn when a release flag expires within this many days")
	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	if err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n%s\n", fs.Arg(0), usage)
		return exitUsage
	}
	if *warnDays < 0 {
		fmt.Fprintln(stderr, "--warn-days must not be negative")
		return exitUsage
	}

	today, err := resolveToday(*todayText, now)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	rootDir, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintf(stderr, "resolve root: %v\n", err)
		return exitUsage
	}
	rev := *baseRev
	if rev == "" {
		rev = defaultBaseRev
	}
	base, err := loadBase(rootDir, rev)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}

	violations, err := flagscheck.Check(flagscheck.Options{
		FilePath: filepath.Join(rootDir, flagsFileName),
		Base:     base,
		Today:    today,
		WarnDays: *warnDays,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}

	return finish(violations, stdout, stderr, getenv)
}

func runMigrations(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("gates migrations", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root")
	baseRev := fs.String("base-rev", "", "base revision (default origin/main)")
	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	if err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n%s\n", fs.Arg(0), usage)
		return exitUsage
	}
	rootDir, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintf(stderr, "resolve root: %v\n", err)
		return exitUsage
	}
	rev := *baseRev
	if rev == "" {
		rev = defaultBaseRev
	}
	sha, err := resolveBase(rootDir, rev)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	violations, checked, err := migrationlint.Lint(migrationlint.Options{Root: rootDir, BaseRev: sha})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	code := finish(violations, stdout, stderr, getenv)
	if code == exitUsage {
		return code
	}
	fmt.Fprintf(stdout, "%d migration files checked\n", checked)
	return code
}

func runContracts(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("gates contracts", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root")
	baseRev := fs.String("base-rev", "", "base revision (default origin/main)")
	oasdiff := fs.String("oasdiff", "", "oasdiff binary (default oasdiff from PATH)")
	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	if err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n%s\n", fs.Arg(0), usage)
		return exitUsage
	}
	rootDir, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintf(stderr, "resolve root: %v\n", err)
		return exitUsage
	}
	rev := *baseRev
	if rev == "" {
		rev = defaultBaseRev
	}
	sha, err := resolveBase(rootDir, rev)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	violations, err := contractgate.Check(context.Background(), contractgate.Options{Root: rootDir, BaseRev: sha, OasdiffBin: *oasdiff})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	return finish(violations, stdout, stderr, getenv)
}

func runVersions(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("gates versions", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root")
	baseRev := fs.String("base-rev", "", "base revision (default origin/main)")
	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	if err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n%s\n", fs.Arg(0), usage)
		return exitUsage
	}
	rootDir, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintf(stderr, "resolve root: %v\n", err)
		return exitUsage
	}
	rev := *baseRev
	if rev == "" {
		rev = defaultBaseRev
	}
	sha, err := resolveBase(rootDir, rev)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	violations, err := versions.CheckBumps(context.Background(), rootDir, sha)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	return finish(violations, stdout, stderr, getenv)
}

func runTags(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gates tags", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root")
	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	if err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n%s\n", fs.Arg(0), usage)
		return exitUsage
	}
	rootDir, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintf(stderr, "resolve root: %v\n", err)
		return exitUsage
	}
	tags, err := versions.MissingTags(context.Background(), rootDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	for _, tag := range tags {
		fmt.Fprintln(stdout, tag)
	}
	return exitOK
}

// finish prints the violations and returns the exit code for them.
func finish(violations []report.Violation, stdout, stderr io.Writer, getenv func(string) string) int {
	printer := report.NewPlainPrinter(stdout)
	if getenv("GITHUB_ACTIONS") == "true" {
		printer = report.NewGitHubPrinter(stdout)
	}
	err := printer.Print(violations)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}

	errorCount := 0
	for _, v := range violations {
		if v.Severity == report.SeverityError {
			errorCount++
		}
	}
	if errorCount > 0 {
		fmt.Fprintf(stderr, "%d violations\n", errorCount)
		return exitViolated
	}
	return exitOK
}

func resolveToday(text string, now func() time.Time) (time.Time, error) {
	if text == "" {
		return flagscheck.SantiagoDate(now())
	}
	parsed, err := time.Parse("2006-01-02", text)
	if err != nil {
		return time.Time{}, fmt.Errorf("--today %q is not a date YYYY-MM-DD", text)
	}
	return parsed, nil
}

// loadBase returns flags.yaml at the merge base of rev and HEAD, or nil when
// the file did not exist there.
func loadBase(root, rev string) ([]byte, error) {
	sha, err := resolveBase(root, rev)
	if err != nil {
		return nil, err
	}
	mergeBase, err := repo.MergeBase(root, sha, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("find merge base with %s: %w", rev, err)
	}
	content, found, err := repo.ShowFile(root, mergeBase, flagsFileName)
	if err != nil {
		return nil, fmt.Errorf("read %s at %s: %w", flagsFileName, rev, err)
	}
	if !found {
		return nil, nil
	}
	// A present but empty base file must stay distinguishable from absent.
	if content == nil {
		content = []byte{}
	}
	return content, nil
}

// resolveBase returns the commit id of rev, with the error text the CLI prints.
func resolveBase(root, rev string) (string, error) {
	sha, err := repo.ResolveRev(root, rev)
	if errors.Is(err, repo.ErrRevNotFound) {
		return "", fmt.Errorf("base revision %s not found; fetch with fetch-depth: 0", rev)
	}
	if err != nil {
		return "", fmt.Errorf("resolve base revision %s: %w", rev, err)
	}
	return sha, nil
}
