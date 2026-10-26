// Package repo holds the git helpers the gates need.
package repo

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrRevNotFound means the revision does not resolve to a commit.
var ErrRevNotFound = errors.New("revision not found")

func git(dir string, args ...string) ([]byte, int, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		message := strings.TrimSpace(stderr.String())
		return nil, exitErr.ExitCode(), fmt.Errorf("git %s: %s", args[0], message)
	}
	return nil, -1, fmt.Errorf("run git: %w", err)
}

func validateRev(rev string) error {
	if rev == "" || strings.HasPrefix(rev, "-") || strings.ContainsAny(rev, "\x00\r\n") {
		return fmt.Errorf("invalid revision %q", rev)
	}
	return nil
}

// ResolveRev returns the commit id rev points to in the repository at dir.
// It returns ErrRevNotFound when rev does not resolve to a commit.
func ResolveRev(dir, rev string) (string, error) {
	err := validateRev(rev)
	if err != nil {
		return "", err
	}
	out, code, err := git(dir, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if code == 1 {
		return "", ErrRevNotFound
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// MergeBase returns the best common ancestor commit of revisions a and b.
func MergeBase(dir, a, b string) (string, error) {
	for _, rev := range []string{a, b} {
		err := validateRev(rev)
		if err != nil {
			return "", err
		}
	}
	out, _, err := git(dir, "merge-base", a, b)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ShowFile returns the content of path (relative to dir) at rev. The boolean
// is false, with a nil error, when the file does not exist at rev; any other
// failure is an error.
func ShowFile(dir, rev, path string) ([]byte, bool, error) {
	err := validateRev(rev)
	if err != nil {
		return nil, false, err
	}
	if path == "" || strings.HasPrefix(path, "-") {
		return nil, false, fmt.Errorf("invalid path %q", path)
	}
	listing, _, err := git(dir, "ls-tree", rev, "--", path)
	if err != nil {
		return nil, false, err
	}
	if len(bytes.TrimSpace(listing)) == 0 {
		return nil, false, nil
	}
	content, _, err := git(dir, "show", rev+":./"+path)
	if err != nil {
		return nil, false, err
	}
	return content, true, nil
}

// ChangeKind is the git status letter of a path between two revisions.
type ChangeKind string

const (
	// ChangeAdded is a path that exists only in the later revision.
	ChangeAdded ChangeKind = "A"
	// ChangeModified is a path whose content or mode changed.
	ChangeModified ChangeKind = "M"
	// ChangeDeleted is a path that exists only in the earlier revision.
	ChangeDeleted ChangeKind = "D"
	// ChangeRenamed is a path moved from OldPath to Path (similarity-detected).
	ChangeRenamed ChangeKind = "R"
)

// Change is one entry of a name-status diff. Paths use forward slashes.
type Change struct {
	Kind ChangeKind
	// Path is the path in the later revision (the earlier one for deletions).
	Path string
	// OldPath is set only for ChangeRenamed.
	OldPath string
}

// DiffNameStatus lists the changes under pathspec between from and to, with
// rename detection. Type changes are reported as modifications and copies as
// additions.
func DiffNameStatus(dir, from, to, pathspec string) ([]Change, error) {
	for _, rev := range []string{from, to} {
		err := validateRev(rev)
		if err != nil {
			return nil, err
		}
	}
	if pathspec == "" || strings.HasPrefix(pathspec, "-") {
		return nil, fmt.Errorf("invalid path %q", pathspec)
	}
	out, _, err := git(dir, "diff", "--name-status", "-M", "-z", from, to, "--", pathspec)
	if err != nil {
		return nil, err
	}
	fields := strings.Split(string(out), "\x00")
	var changes []Change
	for i := 0; i < len(fields); i++ {
		status := fields[i]
		if status == "" {
			continue
		}
		switch status[0] {
		case 'R', 'C':
			if i+2 >= len(fields) {
				return nil, fmt.Errorf("parse git diff output: truncated %q entry", status)
			}
			kind := ChangeRenamed
			if status[0] == 'C' {
				kind = ChangeAdded
			}
			changes = append(changes, Change{Kind: kind, OldPath: fields[i+1], Path: fields[i+2]})
			i += 2
		case 'A', 'M', 'D', 'T':
			if i+1 >= len(fields) {
				return nil, fmt.Errorf("parse git diff output: truncated %q entry", status)
			}
			kind := ChangeKind(status[:1])
			if status[0] == 'T' {
				kind = ChangeModified
			}
			changes = append(changes, Change{Kind: kind, Path: fields[i+1]})
			i++
		default:
			return nil, fmt.Errorf("parse git diff output: unknown status %q", status)
		}
	}
	return changes, nil
}

// ListFiles returns the files under pathspec at rev, sorted, with forward slashes.
func ListFiles(dir, rev, pathspec string) ([]string, error) {
	err := validateRev(rev)
	if err != nil {
		return nil, err
	}
	if pathspec == "" || strings.HasPrefix(pathspec, "-") {
		return nil, fmt.Errorf("invalid path %q", pathspec)
	}
	out, _, err := git(dir, "ls-tree", "-r", "--name-only", "-z", rev, "--", pathspec)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, name := range strings.Split(string(out), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	return files, nil
}
