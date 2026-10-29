// Package source finds the checkout the generator copies modules from, reads
// its tracked files and revision, and resolves the catalog. It is the only
// place the generator runs git.
package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
)

// How names the rule that found a root.
type How string

// The rules that find a root, in precedence order.
const (
	// FromFlag is --root.
	FromFlag How = "flag"
	// FromEnv is INITIALIZER_ROOT.
	FromEnv How = "env"
	// FromWorkingDir is the walk up from the working directory.
	FromWorkingDir How = "working-dir"
	// FromExecutable is the walk up from the binary's folder.
	FromExecutable How = "executable"
)

// EnvRoot names a source checkout.
const EnvRoot = "INITIALIZER_ROOT"

// MainModule is the marker's module path when build info is missing.
const MainModule = "github.com/HectorVeraO/initializer/tooling/generator"

// MaxCatalogBytes bounds a catalog file read from disk.
const MaxCatalogBytes = 32 << 20

// Issue codes. SOURCE_ROOT_NOT_FOUND, SOURCE_ROOT_INVALID, SOURCE_NOT_GIT and
// GIT_NOT_FOUND end the command with exit 2; CATALOG_UNREADABLE and
// SOURCE_UNREADABLE with exit 3; GENERATOR_STALE is a warning.
const (
	CodeRootNotFound      = "SOURCE_ROOT_NOT_FOUND"
	CodeRootInvalid       = "SOURCE_ROOT_INVALID"
	CodeNotGit            = "SOURCE_NOT_GIT"
	CodeGitMissing        = "GIT_NOT_FOUND"
	CodeCatalogUnreadable = "CATALOG_UNREADABLE"
	CodeUnreadable        = "SOURCE_UNREADABLE"
	CodeGeneratorStale    = "GENERATOR_STALE"
)

// Exit codes of an *Error.
const (
	ExitUsage    = 2
	ExitInternal = 3
)

// Host is the process state source reads; tests pass fakes.
type Host struct {
	// Getwd returns the working directory.
	Getwd func() (string, error)
	// Executable returns the binary's path (os.Executable); Locate resolves its
	// symlinks.
	Executable func() (string, error)
	// Getenv reads an environment variable.
	Getenv func(string) string
	// MainModule is debug.ReadBuildInfo().Main.Path, else the MainModule constant.
	MainModule string
	// Git runs `git -C dir args...` and returns its standard output. A git that
	// is absent gives an *exec.Error; a git that exits non-zero gives a *GitError.
	Git func(ctx context.Context, dir string, args ...string) ([]byte, error)
}

// GitError is a git process that ran and exited non-zero.
type GitError struct {
	// Code is the exit status.
	Code int
	// Stderr is the trimmed standard error of the process.
	Stderr string
}

// Error implements error.
func (e *GitError) Error() string {
	return fmt.Sprintf("git exited with status %d: %s", e.Code, e.Stderr)
}

// Root is a located source checkout.
type Root struct {
	// Dir is absolute and cleaned.
	Dir string
	// How is the rule that found Dir.
	How How
	// FS is os.DirFS(Dir).
	FS fs.FS
	// Marked reports that Dir holds the marker: tooling/generator/go.mod with
	// the MainModule.
	Marked bool
}

// Revision is the git state of a root's catalogued files.
type Revision struct {
	// Commit is the full commit of HEAD.
	Commit string
	// Modified reports that a catalogued file differs from HEAD.
	Modified bool
	// Known is false when the root is not a git work tree top level.
	Known bool
}

// Error is a source problem that ends the command with exit 2 or 3.
type Error struct {
	// Issue holds the code, the message and the hint.
	Issue issue.Issue
	// Exit is ExitUsage or ExitInternal.
	Exit int
}

// Error implements error.
func (e *Error) Error() string {
	return e.Issue.Message
}

// newError builds an *Error whose hint comes from the hint table.
func newError(code, message string, exit int) *Error {
	return &Error{Issue: issue.Issue{Code: code, Message: message, Hint: HintFor(code)}, Exit: exit}
}

// HintFor returns the next step for a source issue code, or "".
func HintFor(code string) string {
	switch code {
	case CodeRootNotFound:
		return "run inside a clone, pass --root <clone>, or set " + EnvRoot
	case CodeRootInvalid:
		return "pass --root <clone>, or set " + EnvRoot + " to a clone"
	case CodeNotGit:
		return "clone the repo, or pass --catalog with a catalog built elsewhere"
	case CodeGitMissing:
		return "install git, or pass --catalog"
	case CodeCatalogUnreadable:
		return "rebuild it: initializer catalog build --out <file>"
	case CodeUnreadable:
		return "run 'git status' in the checkout to see why git fails"
	case CodeGeneratorStale:
		return "run 'mise run cli'"
	}
	return ""
}

// DefaultHost returns the Host of the running process.
func DefaultHost() Host {
	return Host{
		Getwd:      os.Getwd,
		Executable: os.Executable,
		Getenv:     os.Getenv,
		MainModule: buildMainModule(),
		Git:        runGit,
	}
}

// buildMainModule returns the main module path of the running binary, or the
// MainModule constant when the build info does not name one.
func buildMainModule() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Path == "" || info.Main.Path == "command-line-arguments" {
		return MainModule
	}
	return info.Main.Path
}

// runGit runs git in dir and returns its standard output.
func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil, &GitError{Code: exitErr.ExitCode(), Stderr: strings.TrimSpace(stderr.String())}
	}
	if err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

// Options configure ResolveSource.
type Options struct {
	// FlagRoot is --root, "" when absent.
	FlagRoot string
	// CatalogFile is --catalog, "" when absent.
	CatalogFile string
	// Host is the process state.
	Host Host
	// Build is the running binary.
	Build generator.Build
}

// Resolved is a ready source: the root, the catalog and its bytes, the revision
// and any warnings.
type Resolved struct {
	// Root is the located checkout.
	Root Root
	// Catalog is the decoded catalog.
	Catalog catalog.Catalog
	// CatalogBytes are the bytes CatalogSHA256 covers.
	CatalogBytes []byte
	// CatalogSHA256 is the hex SHA-256 recorded in the lock as catalogSha256.
	CatalogSHA256 string
	// CatalogFile is the --catalog path, "" when built in memory.
	CatalogFile string
	// Revision is the git state of the catalogued files.
	Revision Revision
	// Warnings holds GENERATOR_STALE.
	Warnings []issue.Issue
	// Notes holds findings that are not warnings, such as a binary whose commit
	// is not in the checkout.
	Notes []string
}
