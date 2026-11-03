// Package upgrade applies the files the current catalog delivers to a project
// an earlier catalog generated. The lock is the only record of which delivered
// files the project has not edited; a file the lock does not list is either
// new to the plan or a seed, and a seed is never written.
package upgrade

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/emit"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
)

// Status is what an upgrade did with one delivered file.
type Status string

const (
	// Added is a file the plan delivers that was not on disk.
	Added Status = "added"
	// Upgraded is a file this run wrote.
	Upgraded Status = "upgraded"
	// Unchanged is a file that already held the planned bytes.
	Unchanged Status = "unchanged"
	// SkippedModified is a file the project edited, left untouched.
	SkippedModified Status = "skipped-modified"
	// SkippedSeed is a seed file that differs from what generation delivers,
	// left untouched.
	SkippedSeed Status = "skipped-seed"
	// SkippedNotFile is a delivered path that is not a regular file inside the
	// project (a symlink, a folder, a device, or a folder on the way that
	// resolves outside it), left untouched.
	SkippedNotFile Status = "skipped-not-file"
)

// Outcome is what the upgrade did with one file.
type Outcome struct {
	Path   string `json:"path"`
	Status Status `json:"status"`
}

// Options are the inputs of one upgrade.
type Options struct {
	// Dir is the project root.
	Dir string
	// Project is the tree the current catalog delivers; its last file is the
	// lock.
	Project emit.Project
	// Lock is the lock the project holds now.
	Lock lockfile.Lock
	// Force replaces the files the project edited.
	Force bool
	// DryRun reports what the upgrade would do and writes nothing.
	DryRun bool
}

// Result is the outcome of one upgrade.
type Result struct {
	// Files is one outcome per planned file in path order, the lock last.
	Files []Outcome
	// Written are the paths this run wrote, in the same order; empty in a dry
	// run.
	Written []string
	// Stale are the paths the lock lists that the plan no longer delivers.
	Stale []string
}

// Count returns how many files carry status.
func (r Result) Count(status Status) int {
	found := 0
	for _, file := range r.Files {
		if file.Status == status {
			found++
		}
	}
	return found
}

// SkippedModified reports whether the run left an edited file untouched.
func (r Result) SkippedModified() bool {
	return r.Count(SkippedModified) > 0
}

// Skipped reports whether the run left a file in place: an edited file or a
// path that is not a regular file.
func (r Result) Skipped() bool {
	return r.SkippedModified() || r.Count(SkippedNotFile) > 0
}

// Apply compares the project at opts.Dir with the plan and writes what it may:
// a file whose bytes equal the lock's hash is replaced, a file the project
// edited (or deleted) is left alone unless Force, a seed is never written, and
// nothing is ever written through a path that is not a regular file of the
// project. Every access goes through an os.Root, which refuses a symbolic link
// or junction that leaves the project.
func Apply(opts Options) (Result, error) {
	if opts.Dir == "" {
		return Result{}, errors.New("project directory is empty")
	}
	base, err := filepath.Abs(opts.Dir)
	if err != nil {
		return Result{}, fmt.Errorf("resolve project: %w", err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return Result{}, fmt.Errorf("open project: %w", err)
	}
	defer root.Close()
	locked := lockedFiles(opts.Lock)
	var result Result
	delivered := map[string]bool{}
	for _, file := range opts.Project.Files {
		if !lockfile.ValidPath(file.Path) {
			return Result{}, fmt.Errorf("output path %q is not a project-relative path", file.Path)
		}
		if delivered[file.Path] {
			return Result{}, fmt.Errorf("output path %q is delivered twice", file.Path)
		}
		delivered[file.Path] = true
		hash, inLock := locked[file.Path]
		outcome, wrote, err := applyFile(root, file, hash, inLock, opts)
		if err != nil {
			return Result{}, err
		}
		result.Files = append(result.Files, outcome)
		if wrote {
			result.Written = append(result.Written, file.Path)
		}
	}
	for path := range locked {
		if !delivered[path] {
			result.Stale = append(result.Stale, path)
		}
	}
	sort.Strings(result.Stale)
	return result, nil
}

// applyFile decides and, unless the run is a dry run, performs one file. It
// reports whether the file was written.
func applyFile(root *os.Root, file emit.File, locked string, inLock bool, opts Options) (Outcome, bool, error) {
	outcome := Outcome{Path: file.Path, Status: Unchanged}
	inside, err := folderInside(root, file.Path)
	if err != nil {
		return Outcome{}, false, err
	}
	if !inside {
		// A folder on the way is a symlink or junction that leaves the project:
		// the root will not even look at what is below it.
		outcome.Status = SkippedNotFile
		return outcome, false, nil
	}
	data, found, isFile, err := probe(root, file.Path)
	if err != nil {
		return Outcome{}, false, err
	}
	switch {
	case found && isFile && bytes.Equal(data, file.Data):
		return outcome, false, nil
	case found && !isFile:
		// A symlink, a folder or another non-regular file sits where the plan
		// delivers a file: writing would reach something the project never
		// named, whatever Force says.
		outcome.Status = SkippedNotFile
		return outcome, false, nil
	}
	switch {
	case file.Path == lockfile.Name:
		// The lock records what was delivered: a run always leaves it matching
		// the plan, whatever the project did to it.
		outcome.Status = Upgraded
	case file.Seed:
		// A seed belongs to the project: generation writes it, an upgrade never
		// does, whatever its state.
		outcome.Status = SkippedSeed
		return outcome, false, nil
	case !found && inLock:
		// The lock vouches for it and the project deleted it: that is a local
		// change, not a missing delivery.
		if !opts.Force {
			outcome.Status = SkippedModified
			return outcome, false, nil
		}
		outcome.Status = Upgraded
	case !found:
		outcome.Status = Added
	case inLock && hashHex(data) == locked:
		outcome.Status = Upgraded
	case opts.Force:
		outcome.Status = Upgraded
	default:
		outcome.Status = SkippedModified
		return outcome, false, nil
	}
	if opts.DryRun {
		return outcome, false, nil
	}
	if err := writeFile(root, file.Path, file.Data); err != nil {
		return Outcome{}, false, err
	}
	return outcome, true, nil
}

// lockedFiles returns the hash the lock records for every file it lists.
func lockedFiles(lock lockfile.Lock) map[string]string {
	locked := map[string]string{}
	for _, spec := range lock.Specs {
		for _, file := range spec.Files {
			locked[file.Path] = file.SHA256
		}
	}
	for _, module := range lock.Modules {
		for _, file := range module.Files {
			locked[file.Path] = file.SHA256
		}
	}
	for _, file := range lock.Generated {
		locked[file.Path] = file.SHA256
	}
	return locked
}

// probe returns the bytes at path: found is false when nothing is there and
// isFile is false when what is there is not a regular file.
func probe(root *os.Root, path string) (data []byte, found, isFile bool, err error) {
	name := filepath.FromSlash(path)
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, false, nil
	}
	if err != nil {
		return nil, false, false, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, true, false, nil
	}
	data, err = root.ReadFile(name)
	if err != nil {
		return nil, false, false, fmt.Errorf("read %s: %w", path, err)
	}
	return data, true, true, nil
}

// writeFile writes data at path under root. The root refuses a path that leaves
// the project, and the write refuses a path where something other than a
// regular file sits: a symlink or a folder on the way could reach a file the
// project never named.
func writeFile(root *os.Root, path string, data []byte) error {
	if !lockfile.ValidPath(path) {
		return fmt.Errorf("output path %q is not a project-relative path", path)
	}
	name := filepath.FromSlash(path)
	folder := filepath.Dir(name)
	if folder != "." {
		if err := root.MkdirAll(folder, 0o755); err != nil {
			return fmt.Errorf("create directory for %s: %w", path, err)
		}
	}
	info, err := root.Lstat(name)
	switch {
	case err == nil && !info.Mode().IsRegular():
		return fmt.Errorf("%s is not a regular file", path)
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if err := root.WriteFile(name, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// folderInside reports whether the folder that would hold path is a folder of
// the project. The root refuses a component that leaves it, so a folder it will
// not follow is an escaping symlink or junction.
func folderInside(root *os.Root, path string) (bool, error) {
	folder := filepath.Dir(filepath.FromSlash(path))
	for {
		info, err := root.Stat(folder)
		switch {
		case err == nil:
			return info.IsDir(), nil
		case errors.Is(err, fs.ErrPermission):
			return false, fmt.Errorf("stat %s: %w", filepath.ToSlash(folder), err)
		case errors.Is(err, fs.ErrNotExist):
			parent := filepath.Dir(folder)
			if parent == folder {
				return false, fmt.Errorf("stat %s: %w", filepath.ToSlash(folder), err)
			}
			folder = parent
		default:
			// The root refused the path itself: something on the way leaves
			// the project.
			return false, nil
		}
	}
}

func hashHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
