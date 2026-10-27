package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
)

// CheckSources recomputes the SHA-256 of every source file the plan copies
// (module files of every stack and spec files) and reports
// CATALOG_FILE_MISMATCH for each file whose bytes differ from the catalog or
// that is missing. The issue path is the repo-relative source path. The error
// is for I/O failures only.
func CheckSources(root fs.FS, p Plan) ([]issue.Issue, error) {
	if root == nil {
		return nil, errors.New("source file system is nil")
	}
	var files []catalog.File
	for _, stack := range p.Stacks {
		for _, module := range stack.Modules {
			files = append(files, module.Files...)
		}
	}
	for _, spec := range p.Specs {
		files = append(files, spec.Files...)
	}
	var issues []issue.Issue
	for _, file := range files {
		message, err := checkFile(root, file)
		if err != nil {
			return nil, err
		}
		if message != "" {
			issues = append(issues, issue.Issue{Code: CodeFileMismatch, Path: file.Path, Message: message})
		}
	}
	issue.Sort(issues)
	return issues, nil
}

// checkFile returns a mismatch message, or "" when the file matches.
func checkFile(root fs.FS, file catalog.File) (string, error) {
	if !fs.ValidPath(file.Path) {
		return "path is not a valid repo-relative path", nil
	}
	handle, err := root.Open(file.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return "source file is missing", nil
	}
	if err != nil {
		return "", fmt.Errorf("open %s: %w", file.Path, err)
	}
	defer handle.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, handle); err != nil {
		return "", fmt.Errorf("read %s: %w", file.Path, err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != file.SHA256 {
		return fmt.Sprintf("sha256 is %s, the catalog says %s", actual, file.SHA256), nil
	}
	return "", nil
}
