package main

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/emit"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
)

// dryRun is what a generate --dry-run run resolved: everything generate would
// write, held in memory.
type dryRun struct {
	// path is the DIR as shown to a person.
	path          string
	project       emit.Project
	plan          plan.Plan
	resolvedInput manifest.Input
	recorded      manifest.Selection
	source        sourceRef
	catalog       catalogRef
	added         []moduleAdded
}

// resolvedSelection returns the resolved document of a dry run: schema 2, every
// default filled in, the installed modules and every component with its
// effective name. It is valid input for -f.
func (d dryRun) resolvedSelection() manifest.Selection {
	selection := d.plan.Selection
	selection.Schema = manifest.MaxSchema
	selection.Has = manifest.Presence{Name: true, GoModule: selection.Project.GoModule != "", Stacks: true, Modules: true}
	selection.AllModules = false
	selection.Components = make([]manifest.Component, 0, len(d.plan.Components))
	for _, component := range d.plan.Components {
		selection.Components = append(selection.Components, manifest.Component{Stack: component.Stack, Name: component.Name})
	}
	selection.Database = d.plan.Database()
	selection.Services = d.plan.Services()
	selection.Ports = &manifest.Ports{Band: d.plan.Ports.Band}
	return selection
}

// builtLock decodes the lock the project would carry.
func (d dryRun) builtLock() (lockfile.Lock, error) {
	for _, file := range d.project.Files {
		if file.Path == lockfile.Name {
			return lockfile.Decode(file.Data)
		}
	}
	return lockfile.Lock{}, fmt.Errorf("the project has no %s", lockfile.Name)
}

// writeDryRun prints what generate would do and returns the exit code. Nothing
// has been written.
func writeDryRun(ctx *Context, d dryRun) int {
	lock, err := d.builtLock()
	if err != nil {
		return fail(ctx, "project_build_failed", codeInternalError, err)
	}
	ctx.Logger.Info("dry_run_planned", slog.Int("file_count", len(d.project.Files)))
	name := d.plan.Selection.Project.Name
	if ctx.isJSON() {
		doc := struct {
			Schema     int                  `json:"schema"`
			OK         bool                 `json:"ok"`
			DryRun     bool                 `json:"dryRun"`
			Project    string               `json:"project"`
			Path       string               `json:"path"`
			Files      int                  `json:"files"`
			Source     sourceRef            `json:"source"`
			Catalog    catalogRef           `json:"catalog"`
			Selection  manifest.Selection   `json:"selection"`
			Recorded   manifest.Selection   `json:"recorded"`
			Components []lockfile.Component `json:"components"`
			Names      map[string]string    `json:"names"`
			Ports      manifest.Ports       `json:"ports"`
			Added      []moduleAdded        `json:"added"`
			Notes      []string             `json:"notes"`
			Warnings   []Warning            `json:"warnings"`
		}{documentSchema, true, true, name, filepath.ToSlash(d.path), len(d.project.Files), d.source, d.catalog,
			d.resolvedSelection(), d.recorded, list(lock.Components), lock.Names, manifest.Ports{Band: d.plan.Ports.Band}, list(d.added), list(ctx.Notes), ctx.warnings()}
		return emitDocument(ctx, ctx.Stdout, doc, exitOK)
	}

	var text strings.Builder
	fmt.Fprintf(&text, "Project %s in %s (dry run, nothing written)\n", name, displayPath(d.path))
	rows := [][]string{{"COMPONENT", "STACK", "FOLDER", "MODULES"}}
	for _, component := range d.plan.Components {
		rows = append(rows, []string{component.Name, component.Stack, component.Name + "/", d.modulesText(component.Stack)})
	}
	if goModule := d.plan.Selection.Project.GoModule; goModule != "" {
		if d.resolvedInput.Origins["/project/goModule"].Source == manifest.DefaultSource {
			goModule += " (default)"
		}
		rows = append(rows, []string{"Go module", goModule})
	}
	if database := d.plan.Database(); database != nil {
		rows = append(rows, []string{"Database", databaseText(database)})
	}
	if services := d.plan.Services(); len(services) > 0 {
		rows = append(rows, []string{"Services", strings.Join(serviceNames(services), ", ")})
	}
	rows = append(rows, []string{"Ports", fmt.Sprintf("band %d", d.plan.Ports.Band)})
	writeSummaryRows(&text, rows)
	fmt.Fprintf(&text, "%d %s\n", len(d.project.Files), plural(len(d.project.Files), "file"))
	return emitText(ctx, text.String())
}

// writeSummaryRows writes the component table and the label rows that follow it
// with one column grid: the label rows have two cells, the table four.
func writeSummaryRows(text *strings.Builder, rows [][]string) {
	var table, labels [][]string
	for _, row := range rows {
		if len(row) > 2 {
			table = append(table, row)
		} else {
			labels = append(labels, row)
		}
	}
	writeTable(text, table)
	width := 0
	for _, row := range table {
		width = max(width, len(row[0]))
	}
	for _, row := range labels {
		width = max(width, len(row[0]))
	}
	for _, row := range labels {
		fmt.Fprintf(text, "%-*s   %s\n", width, row[0], row[1])
	}
}

// modulesText lists the modules of a stack in catalog order, with the ones the
// requirement closure added marked.
func (d dryRun) modulesText(stack string) string {
	requiredBy := map[string][]string{}
	for _, added := range d.added {
		if added.Stack == stack {
			requiredBy[added.Module] = added.RequiredBy
		}
	}
	var names []string
	for _, planned := range d.plan.Stacks {
		if planned.Stack != stack {
			continue
		}
		for _, module := range planned.Modules {
			label := module.Module
			if by, pulled := requiredBy[module.Module]; pulled {
				label += " (required by " + strings.Join(by, ", ") + ")"
			}
			names = append(names, label)
		}
	}
	return strings.Join(names, ", ")
}

// databaseText renders the database row: the name and, when live, its user.
func databaseText(database *manifest.Database) string {
	switch {
	case database.Name != "" && database.User != "":
		return database.Name + " (user " + database.User + ")"
	case database.Name != "":
		return database.Name
	}
	return "user " + database.User
}

// serviceNames returns the service names in the display order: the preferred
// keys first, then the others sorted by key.
func serviceNames(services map[string]string) []string {
	var ordered []string
	seen := map[string]bool{}
	for _, key := range preferredServiceOrder {
		if name, has := services[key]; has {
			ordered = append(ordered, name)
			seen[key] = true
		}
	}
	var rest []string
	for key := range services {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	for _, key := range rest {
		ordered = append(ordered, services[key])
	}
	return ordered
}
