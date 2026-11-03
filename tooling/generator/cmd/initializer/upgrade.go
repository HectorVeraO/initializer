package main

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/emit"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/source"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/upgrade"
)

// maxLockFileBytes bounds the lock an upgrade reads.
const maxLockFileBytes = 32 << 20

// upgradeOptions are the flags of upgrade.
type upgradeOptions struct {
	root, catalog string
	dryRun, force bool
}

func upgradeCommand() *Command {
	return &Command{
		Path:    []string{"upgrade"},
		Label:   "upgrade [DIR]",
		Summary: "Bring a generated project up to the current catalog",
		Usage:   "upgrade [DIR] [flags]",
		Description: "Re-plan the project in DIR from initializer.selection.json with the\n" +
			"current catalog and replace the delivered files the project has not\n" +
			"edited. A file edited after generation is left as it is and reported as\n" +
			"skipped-modified unless --force. Seed files are never written.\n" +
			"initializer.lock.json is rewritten. DIR is relative to the working\n" +
			"directory and defaults to it.",
		MaxArgs: 1,
		Flags: func(set *flagSet) any {
			o := &upgradeOptions{}
			set.boolean(&o.dryRun, flagSpec{Name: "dry-run", Usage: "Print what would change and write nothing", Default: "false"})
			set.boolean(&o.force, flagSpec{Name: "force", Usage: "Replace the files edited after generation", Default: "false"})
			registerSourceFlags(set, &o.root, &o.catalog)
			return o
		},
		Examples: []Example{
			{"in the project folder", "initializer upgrade"},
			{"in ./acme", "initializer upgrade acme"},
			{"see what would change", "initializer upgrade acme --dry-run"},
			{"replace the files the project edited too", "initializer upgrade acme --force"},
		},
		Run: runUpgrade,
	}
}

func runUpgrade(ctx *Context, opts any, positionals []string) int {
	o := opts.(*upgradeOptions)
	dir := "."
	if len(positionals) > 0 {
		dir = positionals[0]
	}
	return upgradeProject(ctx, upgradeInput{
		root: o.root, catalogPath: o.catalog, dir: dir, force: o.force, dryRun: o.dryRun,
	})
}

// upgradeInput is one upgrade run after the flags are read.
type upgradeInput struct {
	root, catalogPath, dir string
	force, dryRun          bool
}

// projectBase returns the absolute cleaned path of the project DIR relative to
// wd.
func projectBase(dir, wd string) string {
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	return filepath.Clean(filepath.Join(wd, dir))
}

// readProjectLock reads and decodes <dir>/initializer.lock.json.
func readProjectLock(dir string) (lockfile.Lock, error) {
	path := filepath.Join(dir, lockfile.Name)
	data, err := readLimited(path, maxLockFileBytes)
	if err != nil {
		return lockfile.Lock{}, fmt.Errorf("%s: %w", lockfile.Name, err)
	}
	lock, err := lockfile.Decode(data)
	if err != nil {
		return lockfile.Lock{}, fmt.Errorf("%s: %w", lockfile.Name, err)
	}
	return lock, nil
}

// readRecordedSelection reads <dir>/initializer.selection.json as the input of
// the run, with no flag on top of it.
func readRecordedSelection(ctx *Context, dir, display string) (inputRun, *readFailure) {
	o := &generateOptions{file: filepath.Join(dir, lockfile.SelectionName), given: map[string]int{}}
	return o.readSelection(ctx, display)
}

// upgradeProject re-plans the project from its recorded selection and applies
// the difference.
func upgradeProject(ctx *Context, in upgradeInput) int {
	logger := ctx.Logger
	workDir := ctx.WorkDir
	if workDir == "" {
		workDir = workDirOrDot()
	}
	base := projectBase(in.dir, workDir)
	previous, err := readProjectLock(base)
	if err != nil {
		return fail(ctx, "upgrade_unreadable", codeUpgradeUnreadable, err)
	}
	run, failure := readRecordedSelection(ctx, base, in.dir)
	if failure != nil {
		return fail(ctx, "upgrade_unreadable", codeUpgradeUnreadable,
			fmt.Errorf("%s: %w", lockfile.SelectionName, failure.err))
	}
	selection := run.merged.Selection
	logger.Info("selection_decoded",
		slog.Int("stack_count", len(selection.Stacks)),
		slog.Int("module_count", len(selection.Modules)),
		slog.Bool("all_modules", selection.AllModules))

	found, buildIssues, err := ctx.resolveSources(in.root, in.catalogPath)
	if err != nil {
		var sourceFailure *source.Error
		isFailure := errors.As(err, &sourceFailure)
		if isFailure && sourceFailure.Exit == source.ExitInternal && len(run.issues) > 0 {
			// The input problems are reportable without a catalog.
			return reportIssues(ctx, run.issues, run.view)
		}
		return reportSourceError(ctx, err, run.issues, run.view)
	}
	ref, sourceNotes := announceSources(ctx, found)
	if len(buildIssues) > 0 {
		return reportIssues(ctx, append(append([]issue.Issue(nil), run.issues...), buildIssues...), run.view)
	}
	built := found.Catalog

	resolvedInput, defaultNotes, _, defaultIssues := manifest.ResolveDefaults(run.merged, in.dir, workDir, backendStackIDs(built))
	issues := append(append([]issue.Issue(nil), run.issues...), defaultIssues...)
	if hasCode(defaultIssues, manifest.CodeNameRequired) {
		if run.fileBroken {
			issues = run.issues
		}
		return reportIssues(ctx, issues, run.view)
	}
	notes := append(append(append([]string(nil), run.notes...), defaultNotes...), sourceNotes...)
	componentIssues := plan.ResolveComponents(built, resolvedInput)
	componentIssues = relocate(addNameHints(ctx.Args, resolvedInput, componentIssues), resolvedInput)
	issues = append(issues, componentIssues...)
	if !hasSeveralBackends(resolvedInput.Selection) {
		resolvedInput = manifest.ApplyEntryOverrides(resolvedInput)
	}
	selection = resolvedInput.Selection

	var resolved plan.Plan
	if !hasCode(issues, planBlockingCodes...) {
		var planIssues []issue.Issue
		resolved, planIssues = plan.Resolve(built, selection)
		issues = append(issues, withoutUndetermined(planIssues, len(issues) > 0)...)
		if len(issues) == 0 {
			sourceIssues, err := plan.CheckSources(found.Root.FS, resolved)
			if err != nil {
				return fail(ctx, "sources_unreadable", codeInternalError, err)
			}
			issues = append(issues, sourceIssues...)
		}
	}
	if len(issues) == 0 {
		notes = append(notes, resolved.Notes...)
	}
	for _, note := range notes {
		fmt.Fprintf(ctx.diagnostics(), "note: %s\n", note)
	}
	if len(issues) > 0 {
		issues = folderNameIssues(dedupeIssues(relocate(issues, resolvedInput)), selection.Project.Name, in.dir)
		view := run.view
		view.dir = in.dir
		view.moduleIDs = moduleIDs(built)
		return reportIssues(ctx, issues, view)
	}
	ctx.Notes = notes

	recorded := manifest.RecordedSelection(withProjectName(run.merged, selection.Project.Name), backendModuleLists(resolved)...)
	dropDerivedBackendReferences(&recorded, resolved)
	project, err := emit.Build(emit.BuildOptions{
		Root:          found.Root.FS,
		Plan:          resolved,
		CatalogSHA256: found.CatalogSHA256,
		Source:        lockSource(found.Revision),
		Recorded:      recorded,
	})
	if err != nil {
		var leak *emit.LeakError
		if errors.As(err, &leak) {
			found := issue.Issue{
				Code:    emit.CodeGoModulePathLeak,
				Path:    leak.File,
				Message: fmt.Sprintf("still holds the Go module path of the source, %q", resolved.GoModule),
			}
			return reportIssues(ctx, []issue.Issue{found}, run.view)
		}
		return fail(ctx, "project_build_failed", codeInternalError, err)
	}
	result, err := upgrade.Apply(upgrade.Options{
		Dir: base, Project: project, Lock: previous, Force: in.force, DryRun: in.dryRun,
	})
	if err != nil {
		return fail(ctx, "project_write_failed", codeOutputUnwritable, err)
	}
	logger.Info("project_upgraded",
		slog.Int("file_count", len(result.Files)),
		slog.Int("written_count", len(result.Written)),
		slog.Int("skipped_modified_count", result.Count(upgrade.SkippedModified)),
		slog.Bool("dry_run", in.dryRun))
	return writeUpgrade(ctx, upgradeResult{
		project: selection.Project.Name,
		path:    in.dir,
		dryRun:  in.dryRun,
		result:  result,
		source:  ref,
		catalog: catalogOf(found),
	})
}
