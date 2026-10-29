package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
)

// generatorFolder is the folder whose changes make a built binary stale.
const generatorFolder = "tooling/generator"

var (
	// headPattern is a full commit id of a SHA-1 or SHA-256 repository.
	headPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
	// builtCommitPattern is a commit id safe to pass to git as an argument.
	builtCommitPattern = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// errGitMissing marks a git that cannot be started.
var errGitMissing = errors.New("git is not available")

// runGitIn runs git through host, mapping a missing git to errGitMissing.
func runGitIn(ctx context.Context, host Host, dir string, args ...string) ([]byte, error) {
	if host.Git == nil {
		return nil, errGitMissing
	}
	out, err := host.Git(ctx, dir, args...)
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return nil, errGitMissing
	}
	return out, err
}

// isTopLevel reports whether root.Dir is the top level of a git work tree.
// A git that fails because dir is no work tree answers false with no error;
// a git that cannot start answers errGitMissing.
func isTopLevel(ctx context.Context, root Root, host Host) (bool, error) {
	out, err := runGitIn(ctx, host, root.Dir, "rev-parse", "--show-toplevel")
	var gitErr *GitError
	switch {
	case errors.As(err, &gitErr):
		return false, nil
	case err != nil:
		return false, err
	}
	top := strings.TrimRight(string(out), "\r\n")
	return samePath(top, root.Dir), nil
}

// samePath compares two directories after resolving symlinks, ignoring case
// on Windows.
func samePath(a, b string) bool {
	normalize := func(path string) string {
		path = filepath.Clean(filepath.FromSlash(path))
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
		return path
	}
	left, right := normalize(a), normalize(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

// unreadable wraps a git failure in the exit-3 *Error.
func unreadable(what string, err error) *Error {
	return newError(CodeUnreadable, fmt.Sprintf("cannot %s: %v", what, err), ExitInternal)
}

// TrackedFiles checks that `git -C dir rev-parse --show-toplevel` equals dir
// (SOURCE_NOT_GIT otherwise), then runs `git -C dir ls-files -z` and parses it
// with catalog.ParseFileList. A git that cannot start is GIT_NOT_FOUND.
func TrackedFiles(ctx context.Context, root Root, host Host) ([]string, error) {
	top, err := isTopLevel(ctx, root, host)
	switch {
	case errors.Is(err, errGitMissing):
		return nil, newError(CodeGitMissing, "git was not found on PATH", ExitUsage)
	case err != nil:
		return nil, unreadable("run git in "+slash(root.Dir), err)
	case !top:
		return nil, newError(CodeNotGit, slash(root.Dir)+" is not the top level of a git work tree", ExitUsage)
	}
	out, err := runGitIn(ctx, host, root.Dir, "ls-files", "-z")
	if err != nil {
		return nil, unreadable("list the tracked files of "+slash(root.Dir), err)
	}
	files, err := catalog.ParseFileList(out)
	if err != nil {
		return nil, unreadable("read the tracked file list of "+slash(root.Dir), err)
	}
	return files, nil
}

// ReadRevision returns HEAD and whether any of paths differs from it or is
// untracked (`git -C dir rev-parse HEAD`, then `git -C dir diff --name-only -z
// HEAD` and `git -C dir ls-files --others --exclude-standard -z`, intersected
// with paths in Go). Known is false, with no error, when dir is not
// a git work tree top level, when git is not available, or when HEAD is not a
// commit yet.
func ReadRevision(ctx context.Context, root Root, host Host, paths []string) (Revision, error) {
	top, err := isTopLevel(ctx, root, host)
	switch {
	case errors.Is(err, errGitMissing):
		return Revision{}, nil
	case err != nil:
		return Revision{}, unreadable("run git in "+slash(root.Dir), err)
	case !top:
		return Revision{}, nil
	}
	head, err := runGitIn(ctx, host, root.Dir, "rev-parse", "HEAD")
	var gitErr *GitError
	if errors.As(err, &gitErr) {
		return Revision{}, nil
	}
	if err != nil {
		return Revision{}, unreadable("read HEAD of "+slash(root.Dir), err)
	}
	commit := strings.TrimSpace(string(head))
	if !headPattern.MatchString(commit) {
		return Revision{}, nil
	}
	diff, err := runGitIn(ctx, host, root.Dir, "diff", "--name-only", "--no-renames", "-z", "HEAD")
	if err != nil {
		return Revision{}, unreadable("compare "+slash(root.Dir)+" with HEAD", err)
	}
	untracked, err := runGitIn(ctx, host, root.Dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return Revision{}, unreadable("list the untracked files of "+slash(root.Dir), err)
	}
	changed := parseDiffNames(append(diff[:len(diff):len(diff)], untracked...), paths)
	return Revision{Commit: commit, Modified: len(changed) > 0, Known: true}, nil
}

// parseDiffNames returns the names of the NUL-separated list diff that are
// also in paths, sorted and without duplicates.
func parseDiffNames(diff []byte, paths []string) []string {
	wanted := make(map[string]bool, len(paths))
	for _, path := range paths {
		wanted[path] = true
	}
	var changed []string
	for _, name := range bytes.Split(diff, []byte{0}) {
		if wanted[string(name)] {
			changed = append(changed, string(name))
		}
	}
	slices.Sort(changed)
	return slices.Compact(changed)
}

// CheckStale compares the commit the running binary was built from with HEAD of
// the checkout. It runs only when build has a commit, the root is marked and
// the revision is known; otherwise it returns nothing. A commit that is not in
// the checkout is a note; a change under tooling/generator since that commit
// is a GENERATOR_STALE warning.
func CheckStale(ctx context.Context, root Root, host Host, build generator.Build, revision Revision) (warnings []issue.Issue, notes []string, err error) {
	if build.Commit == "" || !root.Marked || !revision.Known || !builtCommitPattern.MatchString(build.Commit) {
		return nil, nil, nil
	}
	built7 := build.Commit[:7]
	_, err = runGitIn(ctx, host, root.Dir, "cat-file", "-e", build.Commit+"^{commit}")
	var gitErr *GitError
	if errors.As(err, &gitErr) {
		note := fmt.Sprintf("this initializer was built from %s, which is not in %s; cannot tell whether it is current",
			built7, slash(root.Dir))
		return nil, []string{note}, nil
	}
	if err != nil {
		return nil, nil, unreadable("look up commit "+built7+" in "+slash(root.Dir), err)
	}
	_, err = runGitIn(ctx, host, root.Dir, "diff", "--quiet", build.Commit, "HEAD", "--", generatorFolder)
	switch {
	case err == nil:
		return nil, nil, nil
	case errors.As(err, &gitErr) && gitErr.Code == 1:
		warning := issue.Issue{
			Code:    CodeGeneratorStale,
			Message: fmt.Sprintf("this initializer was built from %s; %s changed since (HEAD %s)", built7, generatorFolder, revision.Commit[:7]),
			Hint:    HintFor(CodeGeneratorStale),
		}
		return []issue.Issue{warning}, nil, nil
	}
	return nil, nil, unreadable("compare "+generatorFolder+" since "+built7, err)
}
