// Package verify compares a generated project with its initializer.lock.json.
package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
)

// Difference codes.
const (
	CodeModified  = "MODIFIED"
	CodeMissing   = "MISSING"
	CodeUntracked = "UNTRACKED"
)

// maxLockBytes bounds the lock read from disk.
const maxLockBytes = 32 << 20

// Report is the outcome of checking one project.
type Report struct {
	// Project is the project name the lock records.
	Project string
	// Differences are sorted by path then code.
	Differences []issue.Issue
}

// Project recomputes every hash the lock lists and returns the differences
// sorted by path then code. The error is for an unreadable or undecodable
// lock and for I/O failures; a project that differs is not an error.
func Project(dir string) ([]issue.Issue, error) {
	report, err := Check(dir)
	return report.Differences, err
}

// Check is Project plus the project name the lock records.
func Check(dir string) (Report, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return Report{}, errors.New("project is not a readable directory")
	}
	lock, err := readLock(filepath.Join(dir, lockfile.Name))
	if err != nil {
		return Report{}, err
	}

	var entries []lockfile.File
	var folders []string
	for _, spec := range lock.Specs {
		entries = append(entries, spec.Files...)
		folders = append(folders, "specs/"+spec.Spec+"/")
	}
	for _, module := range lock.Modules {
		entries = append(entries, module.Files...)
		folder, hasFolder := lock.FolderOf(module.Stack)
		if !hasFolder {
			return Report{}, fmt.Errorf("internal error: lock has no single component for stack %q", module.Stack)
		}
		moduleDir, hasDir := lockfile.ModuleDir(folder, module.Stack, module.Module)
		if !hasDir {
			return Report{}, fmt.Errorf("internal error: unknown stack %q", module.Stack)
		}
		folders = append(folders, moduleDir)
	}
	entries = append(entries, lock.Generated...)

	listed := make(map[string]bool, len(entries))
	var differences []issue.Issue
	for _, entry := range entries {
		listed[entry.Path] = true
		difference, err := compare(dir, entry)
		if err != nil {
			return Report{}, err
		}
		if difference != nil {
			differences = append(differences, *difference)
		}
	}
	untracked, err := findUntracked(dir, folders, listed)
	if err != nil {
		return Report{}, err
	}
	differences = append(differences, untracked...)
	issue.Sort(differences)
	return Report{Project: lock.Selection.Project.Name, Differences: differences}, nil
}

func readLock(path string) (lockfile.Lock, error) {
	file, err := os.Open(path)
	if err != nil {
		return lockfile.Lock{}, fmt.Errorf("open lock: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxLockBytes+1))
	if err != nil {
		return lockfile.Lock{}, fmt.Errorf("read lock: %w", err)
	}
	if len(data) > maxLockBytes {
		return lockfile.Lock{}, fmt.Errorf("lock exceeds %d bytes", maxLockBytes)
	}
	lock, err := lockfile.Decode(data)
	if err != nil {
		return lockfile.Lock{}, fmt.Errorf("decode lock: %w", err)
	}
	return lock, nil
}

// compare returns MISSING, MODIFIED or nil for one listed file.
func compare(dir string, entry lockfile.File) (*issue.Issue, error) {
	path := filepath.Join(dir, filepath.FromSlash(entry.Path))
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &issue.Issue{Code: CodeMissing, Path: entry.Path, Message: "listed file does not exist"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", entry.Path, err)
	}
	if !info.Mode().IsRegular() {
		return &issue.Issue{Code: CodeModified, Path: entry.Path, Message: "listed file is no longer a regular file"}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", entry.Path, err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return nil, fmt.Errorf("read %s: %w", entry.Path, err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != entry.SHA256 {
		return &issue.Issue{Code: CodeModified, Path: entry.Path, Message: "sha256 is " + actual + ", the lock says " + entry.SHA256}, nil
	}
	return nil, nil
}

// findUntracked lists every entry inside the module and spec folders that the
// lock does not list.
func findUntracked(dir string, folders []string, listed map[string]bool) ([]issue.Issue, error) {
	sort.Strings(folders)
	var found []issue.Issue
	for i, folder := range folders {
		if i > 0 && folders[i-1] == folder {
			continue
		}
		root := filepath.Join(dir, filepath.FromSlash(folder))
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if entry.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			slashed := filepath.ToSlash(rel)
			if !listed[slashed] {
				found = append(found, issue.Issue{Code: CodeUntracked, Path: slashed, Message: "file is inside a delivered folder and the lock does not list it"})
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", folder, err)
		}
	}
	return found, nil
}
