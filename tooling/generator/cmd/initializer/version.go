package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/source"
)

// buildCommitDisplay is how many hex digits of a commit the version lines show.
const buildCommitDisplay = 12

func versionCommand() *Command {
	return &Command{
		Path:        []string{"version"},
		Label:       "version",
		Summary:     "Print the generator version, build commit and source checkout",
		Usage:       "version [flags]",
		Description: "Print the generator version, the commit this binary was built from and the\nsource checkout it finds from here. A missing checkout is not an error.",
		MaxArgs:     0,
		Flags:       func(*flagSet) any { return nil },
		Examples: []Example{
			{"the version and the checkout", "initializer version"},
			{"as JSON", "initializer version --format json"},
			{"one line", "initializer --version"},
		},
		Run: runVersion,
	}
}

// buildParts renders the parenthesized parts of a build: commit, origin and
// "modified".
func buildParts(build generator.Build) string {
	var parts []string
	if build.Commit != "" {
		parts = append(parts, build.Commit[:min(len(build.Commit), buildCommitDisplay)])
	}
	parts = append(parts, build.Origin)
	if build.Modified {
		parts = append(parts, "modified")
	}
	return strings.Join(parts, ", ")
}

// versionLine is the one line of --version.
func versionLine(build generator.Build) string {
	build = stamped(build)
	return fmt.Sprintf("initializer %s (%s)", build.Version, buildParts(build))
}

// foundFrom says in words which rule found a checkout.
func foundFrom(how string) string {
	switch source.How(how) {
	case source.FromFlag:
		return "--root"
	case source.FromEnv:
		return source.EnvRoot
	case source.FromExecutable:
		return "the binary's folder"
	}
	return "the working directory"
}

// buildDocument is the "build" member of the version document.
type buildDocument struct {
	Commit   string `json:"commit"`
	Origin   string `json:"origin"`
	Modified bool   `json:"modified"`
}

func runVersion(ctx *Context, _ any, _ []string) int {
	build := ctx.Build
	host := ctx.host()
	background := context.Background()

	var ref *sourceRef
	missing := ""
	root, err := source.Locate("", host)
	var locateFailure *source.Error
	switch {
	case errors.As(err, &locateFailure):
		missing = locateFailure.Issue.Code
	case err != nil:
		missing = codeInternalError
	default:
		revision, revisionErr := source.ReadRevision(background, root, host, nil)
		if revisionErr != nil {
			var revisionFailure *source.Error
			if errors.As(revisionErr, &revisionFailure) {
				missing = revisionFailure.Issue.Code
			} else {
				missing = codeInternalError
			}
			break
		}
		found := refOf(root, revision)
		// version reads no catalogued paths, so it cannot tell whether the
		// checkout is modified: the member is left out rather than false.
		found.Modified = nil
		ref = &found
		warnings, notes, staleErr := source.CheckStale(background, root, host, build, revision)
		if staleErr == nil {
			announceWarnings(ctx, revision, warnings)
			ctx.Notes = notes
		}
		ctx.Logger.Info("sources_resolved",
			"how", found.How, "dir", found.Dir, "commit", found.Commit, "catalog_from", "none")
	}

	if ctx.isJSON() {
		doc := struct {
			Schema    int           `json:"schema"`
			OK        bool          `json:"ok"`
			Generator string        `json:"generator"`
			Build     buildDocument `json:"build"`
			Source    *sourceRef    `json:"source,omitempty"`
			Notes     []string      `json:"notes"`
			Warnings  []Warning     `json:"warnings"`
		}{documentSchema, true, build.Version, buildDocument{build.Commit, build.Origin, build.Modified},
			ref, list(ctx.Notes), ctx.warnings()}
		return emitDocument(ctx, ctx.Stdout, doc, exitOK)
	}
	for _, note := range ctx.Notes {
		fmt.Fprintf(ctx.Stderr, "note: %s\n", note)
	}
	var text strings.Builder
	fmt.Fprintf(&text, "initializer %s\n", build.Version)
	commit := "none"
	if build.Commit != "" {
		commit = build.Commit[:min(len(build.Commit), buildCommitDisplay)]
	}
	origin := build.Origin
	if build.Modified {
		origin += ", modified"
	}
	fmt.Fprintf(&text, "  build     %s (%s)\n", commit, origin)
	if ref == nil {
		fmt.Fprintf(&text, "  sources   none found (%s)\n", missing)
	} else {
		fmt.Fprintf(&text, "  sources   %s\n", versionSources(*ref))
	}
	return emitText(ctx, text.String())
}

// versionSources renders the sources row of the version text.
func versionSources(ref sourceRef) string {
	if ref.Commit == "" {
		return fmt.Sprintf("%s (not a git checkout; found from %s)", ref.Dir, foundFrom(ref.How))
	}
	return fmt.Sprintf("%s at %s (found from %s)", ref.Dir, ref.Commit[:7], foundFrom(ref.How))
}
