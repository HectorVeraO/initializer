package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/source"
)

// runCatalogBuild builds catalog.lock.json from a checkout: the file list is
// --files when given, else the tracked files of the root.
func runCatalogBuild(ctx *Context, opts any, _ []string) int {
	o := opts.(*catalogBuildOptions)
	host := ctx.host()
	background := context.Background()

	root, err := source.Locate(o.root, host)
	if err != nil {
		return reportSourceError(ctx, err, nil, issueView{})
	}
	var files []string
	if o.files != "" {
		fileList, err := readLimited(o.files, maxFileListBytes)
		if err != nil {
			return fail(ctx, "input_unreadable", codeInputUnreadable, err)
		}
		files, err = catalog.ParseFileList(fileList)
		if err != nil {
			return fail(ctx, "input_unreadable", codeInputUnreadable, err)
		}
	} else {
		files, err = source.TrackedFiles(background, root, host)
		if err != nil {
			return reportSourceError(ctx, err, nil, issueView{})
		}
	}

	built, encoded, issues, err := source.BuildCatalog(root, files)
	if err != nil {
		return reportSourceError(ctx, err, nil, issueView{})
	}
	paths := files
	if len(issues) == 0 {
		paths = source.CatalogPaths(built)
	}
	revision, err := source.ReadRevision(background, root, host, paths)
	if err != nil {
		return reportSourceError(ctx, err, nil, issueView{})
	}
	ref := announceRoot(ctx, root, revision, "files")
	if len(issues) > 0 {
		return reportIssues(ctx, issues, issueView{})
	}
	if err := writeOutput(o.out, encoded); err != nil {
		return fail(ctx, "output_unwritable", codeOutputUnwritable, err)
	}
	sum := sha256.Sum256(encoded)
	digest := hex.EncodeToString(sum[:])
	if code := writeCatalogBuilt(ctx, o.out, digest, ref); code != exitOK {
		return code
	}
	ctx.Logger.Info("catalog_built",
		slog.Int("spec_count", len(built.Specs)),
		slog.Int("stack_count", len(built.Stacks)),
		slog.String("sha256", digest))
	return exitOK
}
