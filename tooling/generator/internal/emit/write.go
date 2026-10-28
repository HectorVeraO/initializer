package emit

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
)

// CodeOutputNotEmpty is the issue code of an output path that already holds something.
const CodeOutputNotEmpty = "OUTPUT_NOT_EMPTY"

// CheckOutput reports OUTPUT_NOT_EMPTY unless out is absent or an empty
// directory. The error is for I/O failures only.
func CheckOutput(out string) ([]issue.Issue, error) {
	info, err := os.Stat(out)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat output: %w", err)
	}
	slashed := filepath.ToSlash(out)
	if !info.IsDir() {
		return []issue.Issue{{Code: CodeOutputNotEmpty, Path: slashed, Message: "output path exists and is not a directory"}}, nil
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		return nil, fmt.Errorf("read output: %w", err)
	}
	if len(entries) > 0 {
		return []issue.Issue{{Code: CodeOutputNotEmpty, Path: slashed, Message: "output directory is not empty"}}, nil
	}
	return nil, nil
}

// Write writes the project under out, files 0644 and directories 0755, in
// project order (initializer.lock.json last). Every destination is checked to stay
// under out. A failure leaves a partial directory.
func Write(out string, project Project) error {
	if out == "" {
		return errors.New("output directory is empty")
	}
	if len(project.Files) == 0 || project.Files[len(project.Files)-1].Path != lockfile.Name {
		return errors.New("project must end with the lock file")
	}
	base, err := filepath.Abs(out)
	if err != nil {
		return fmt.Errorf("resolve output: %w", err)
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	for _, file := range project.Files {
		if !lockfile.ValidPath(file.Path) {
			return fmt.Errorf("output path %q is not a project-relative path", file.Path)
		}
		dest := filepath.Join(base, filepath.FromSlash(file.Path))
		if !strings.HasPrefix(dest, base+string(filepath.Separator)) {
			return fmt.Errorf("output path %q leaves the output directory", file.Path)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("create directory for %s: %w", file.Path, err)
		}
		if err := os.WriteFile(dest, file.Data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", file.Path, err)
		}
	}
	return nil
}
