package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
)

// buildIssueHint is the hint of an issue found while building the catalog in
// memory.
const buildIssueHint = "fix the module sources, or pass --catalog"

// ResolveSource finds the checkout and its catalog. --root given builds the
// catalog in memory from that checkout unless --catalog is also given; no
// --root uses Locate's default rules. The returned issues are catalog build
// issues (exit 1); err is an *Error (exit 2 or 3). When err is nil or the
// issues are not empty, Resolved.Root is set.
func ResolveSource(ctx context.Context, opts Options) (Resolved, []issue.Issue, error) {
	root, err := Locate(opts.FlagRoot, opts.Host)
	if err != nil {
		return Resolved{}, nil, err
	}
	resolved := Resolved{Root: root, CatalogFile: opts.CatalogFile}

	if opts.CatalogFile != "" {
		if err := readCatalogFile(&resolved, opts.CatalogFile); err != nil {
			return resolved, nil, err
		}
	} else {
		files, err := TrackedFiles(ctx, root, opts.Host)
		if err != nil {
			return resolved, nil, err
		}
		built, encoded, issues, err := BuildCatalog(root, files)
		if err != nil {
			return resolved, nil, err
		}
		if len(issues) > 0 {
			revision, err := ReadRevision(ctx, root, opts.Host, files)
			if err != nil {
				return resolved, nil, err
			}
			resolved.Revision = revision
			return resolved, withHint(issues, buildIssueHint), nil
		}
		resolved.Catalog, resolved.CatalogBytes = built, encoded
	}
	sum := sha256.Sum256(resolved.CatalogBytes)
	resolved.CatalogSHA256 = hex.EncodeToString(sum[:])

	revision, err := ReadRevision(ctx, root, opts.Host, CatalogPaths(resolved.Catalog))
	if err != nil {
		return resolved, nil, err
	}
	resolved.Revision = revision
	resolved.Warnings, resolved.Notes, err = CheckStale(ctx, root, opts.Host, opts.Build, revision)
	if err != nil {
		return resolved, nil, err
	}
	return resolved, nil, nil
}

// readCatalogFile reads and decodes an explicit catalog file into resolved.
func readCatalogFile(resolved *Resolved, path string) error {
	data, err := readLimited(path, MaxCatalogBytes)
	if err != nil {
		return newError(CodeCatalogUnreadable, err.Error(), ExitInternal)
	}
	decoded, issues := catalog.Decode(data)
	if len(issues) > 0 {
		return newError(CodeCatalogUnreadable, fmt.Sprintf("%s: %s", path, issues[0].Message), ExitInternal)
	}
	resolved.Catalog, resolved.CatalogBytes = decoded, data
	return nil
}

// BuildCatalog builds and encodes the catalog of root from its tracked files.
// The issues are build issues; the *Error is exit 3, such as a tracked file
// that is missing from the working tree.
func BuildCatalog(root Root, files []string) (catalog.Catalog, []byte, []issue.Issue, error) {
	built, issues, err := catalog.Build(catalog.BuildOptions{Root: root.FS, Files: files, Generator: generator.Version})
	if err != nil {
		var pathErr *fs.PathError
		if errors.Is(err, fs.ErrNotExist) && errors.As(err, &pathErr) {
			message := fmt.Sprintf("tracked file %s is missing from the working tree", pathErr.Path)
			failure := newError(CodeUnreadable, message, ExitInternal)
			failure.Issue.Hint = fmt.Sprintf("run 'git rm %s' to delete it, or 'git restore %s' to bring it back", pathErr.Path, pathErr.Path)
			return catalog.Catalog{}, nil, nil, failure
		}
		return catalog.Catalog{}, nil, nil, newError(CodeUnreadable, err.Error(), ExitInternal)
	}
	if len(issues) > 0 {
		return catalog.Catalog{}, nil, issues, nil
	}
	encoded, err := catalog.Encode(built)
	if err != nil {
		return catalog.Catalog{}, nil, nil, newError(CodeUnreadable, err.Error(), ExitInternal)
	}
	return built, encoded, nil, nil
}

// CatalogPaths returns the source path of every spec and module file of c.
func CatalogPaths(c catalog.Catalog) []string {
	var paths []string
	for _, spec := range c.Specs {
		for _, file := range spec.Files {
			paths = append(paths, file.Path)
		}
	}
	for _, stack := range c.Stacks {
		for _, module := range stack.Modules {
			for _, file := range module.Files {
				paths = append(paths, file.Path)
			}
		}
	}
	return paths
}

// withHint returns issues with hint set on every issue that has none.
func withHint(issues []issue.Issue, hint string) []issue.Issue {
	hinted := make([]issue.Issue, len(issues))
	copy(hinted, issues)
	for i := range hinted {
		if hinted[i].Hint == "" {
			hinted[i].Hint = hint
		}
	}
	return hinted
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
		return nil, fmt.Errorf("%s exceeds %d bytes", path, limit)
	}
	return data, nil
}
