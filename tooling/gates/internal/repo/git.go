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
