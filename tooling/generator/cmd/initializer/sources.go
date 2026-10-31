package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/source"
)

// sourceRef is the "source" member of a result document: where the sources
// came from and their revision. Commit and Modified are absent for a root that
// is not a git checkout.
type sourceRef struct {
	How      string `json:"how"`
	Dir      string `json:"dir"`
	Commit   string `json:"commit,omitempty"`
	Modified *bool  `json:"modified,omitempty"`
}

// refOf builds the "source" member of a located root and its revision.
func refOf(root source.Root, revision source.Revision) sourceRef {
	ref := sourceRef{How: string(root.How), Dir: filepath.ToSlash(root.Dir)}
	if revision.Known {
		modified := revision.Modified
		ref.Commit, ref.Modified = revision.Commit, &modified
	}
	return ref
}

// sourcesLine renders the Sources: line of a command.
func sourcesLine(ref sourceRef) string {
	switch {
	case ref.Commit == "":
		return fmt.Sprintf("Sources: %s (not a git checkout)", ref.Dir)
	case ref.Modified != nil && *ref.Modified:
		return fmt.Sprintf("Sources: %s at %s (modified)", ref.Dir, ref.Commit[:7])
	}
	return fmt.Sprintf("Sources: %s at %s", ref.Dir, ref.Commit[:7])
}

// host returns the Host of the run with every missing member taken from the
// process.
func (c *Context) host() source.Host {
	host := c.Host
	process := source.DefaultHost()
	if host.Getwd == nil {
		host.Getwd = process.Getwd
		if c.WorkDir != "" {
			workDir := c.WorkDir
			host.Getwd = func() (string, error) { return workDir, nil }
		}
	}
	if host.Executable == nil {
		host.Executable = process.Executable
	}
	if host.Getenv == nil {
		host.Getenv = process.Getenv
	}
	if host.MainModule == "" {
		host.MainModule = process.MainModule
	}
	if host.Git == nil {
		host.Git = process.Git
	}
	return host
}

// resolveSources runs source.ResolveSource for the run.
func (c *Context) resolveSources(flagRoot, catalogFile string) (source.Resolved, []issue.Issue, error) {
	return source.ResolveSource(context.Background(), source.Options{
		FlagRoot:    flagRoot,
		CatalogFile: catalogFile,
		Host:        c.host(),
		Build:       c.Build,
	})
}

// announceSources prints the Sources: line, then the warnings the resolution
// found, and returns the source reference and the notes still to print. The
// warnings join the result document.
func announceSources(ctx *Context, resolved source.Resolved) (ref sourceRef, notes []string) {
	catalogFrom := "checkout"
	if resolved.CatalogFile != "" {
		catalogFrom = "file"
	}
	ref = announceRoot(ctx, resolved.Root, resolved.Revision, catalogFrom)
	announceWarnings(ctx, resolved.Revision, resolved.Warnings)
	return ref, resolved.Notes
}

// announceRoot prints the Sources: line of a located root and logs the
// sources_resolved event. catalogFrom says where the catalog comes from:
// "checkout", "file", "files" (a file list) or "none".
func announceRoot(ctx *Context, root source.Root, revision source.Revision, catalogFrom string) sourceRef {
	ref := refOf(root, revision)
	fmt.Fprintln(ctx.diagnostics(), sourcesLine(ref))
	ctx.Logger.Info("sources_resolved",
		slog.String("how", ref.How),
		slog.String("dir", ref.Dir),
		slog.String("commit", ref.Commit),
		slog.String("catalog_from", catalogFrom))
	return ref
}

// announceWarnings prints and collects the GENERATOR_STALE warnings.
func announceWarnings(ctx *Context, revision source.Revision, warnings []issue.Issue) {
	for _, found := range warnings {
		fmt.Fprintf(ctx.diagnostics(), "warning: %s [%s]\n", found.Message, found.Code)
		if found.Hint != "" {
			fmt.Fprintf(ctx.diagnostics(), "  hint: %s\n", found.Hint)
		}
		ctx.Warnings = append(ctx.Warnings, Warning{Code: found.Code, Message: found.Message, Hint: found.Hint})
		ctx.Logger.Warn("generator_stale",
			slog.String("built_from", ctx.Build.Commit),
			slog.String("head", revision.Commit))
	}
}

// reportSourceError reports an error of the source package: exit 2 as a
// command-line problem next to the value problems found in the same run, exit 3
// as a failure.
func reportSourceError(ctx *Context, err error, issues []issue.Issue, view issueView) int {
	var found *source.Error
	if !errors.As(err, &found) {
		return fail(ctx, "sources_unreadable", codeInternalError, err)
	}
	if found.Exit == source.ExitUsage {
		problem := Problem{Message: found.Issue.Message, Code: found.Issue.Code, Hint: found.Issue.Hint}
		return reportProblemsAndIssues(ctx, []Problem{problem}, issues, view)
	}
	ctx.Logger.Error("input_unreadable", slog.String("error", found.Issue.Message))
	return reportFailureWithHint(ctx, found.Issue.Code, found.Issue.Message, found.Issue.Hint)
}

// stamped returns build with the version filled in for a run that was given no
// build identity.
func stamped(build generator.Build) generator.Build {
	if build.Version == "" {
		build.Version = generator.Version
	}
	if build.Origin == "" {
		build.Origin = generator.OriginDevel
	}
	return build
}
