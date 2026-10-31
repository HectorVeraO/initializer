package main

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/emit"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
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
	// notes are the derived defaults of a multi project, printed after the table.
	notes []string
}

// resolvedSelection returns the resolved document of a dry run: schema 2, every
// default filled in, the installed modules and every component with its
// effective name. It is valid input for -f. A project with several backends
// holds the modules, Go module, database and own services in each backend entry
// and the bound backend in each frontend entry. At the top level it keeps only
// the services every backend shares (the Postgres server and its init step).
func (d dryRun) resolvedSelection() manifest.Selection {
	selection := d.plan.Selection
	selection.Schema = manifest.MaxSchema
	selection.Has = manifest.Presence{Name: true, GoModule: selection.Project.GoModule != "", Stacks: true, Modules: true}
	selection.AllModules = false
	selection.Ports = &manifest.Ports{Band: d.plan.Ports.Band}
	selection.Components = make([]manifest.Component, 0, len(d.plan.Components))
	shared := map[string]string{}
	if !d.plan.Multi() {
		for _, component := range d.plan.Components {
			selection.Components = append(selection.Components, manifest.Component{Stack: component.Stack, Name: component.Name})
		}
		selection.Database = d.plan.Database()
		selection.Services = d.plan.Services()
		return selection
	}
	for _, component := range d.plan.Components {
		entry := manifest.Component{Stack: component.Stack, Name: component.Name}
		switch component.Role {
		case manifest.RoleBackend:
			entry.Modules, entry.HasModules = moduleIDsOf(component), true
			entry.GoModule = component.GoModule
			if component.Database != (manifest.Database{}) {
				database := component.Database
				entry.Database = &database
			}
			entry.Services = copyStrings(instanceServices(component.Services))
			for key, value := range component.Services {
				if names.IsSharedService(key) {
					shared[key] = value
				}
			}
		case manifest.RoleFrontend:
			entry.Backend = component.Partner
		}
		selection.Components = append(selection.Components, entry)
	}
	selection.Modules, selection.Has.Modules = nil, false
	selection.Database, selection.Services = nil, copyStrings(shared)
	return selection
}

// instanceServices returns the services of one backend that belong to it: the
// services every backend shares are set for the project.
func instanceServices(services map[string]string) map[string]string {
	own := map[string]string{}
	for key, value := range services {
		if !names.IsSharedService(key) {
			own[key] = value
		}
	}
	return own
}

// copyStrings returns a copy of values, nil when it is empty.
func copyStrings(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
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
			Schema     int                `json:"schema"`
			OK         bool               `json:"ok"`
			DryRun     bool               `json:"dryRun"`
			Project    string             `json:"project"`
			Path       string             `json:"path"`
			Files      int                `json:"files"`
			Source     sourceRef          `json:"source"`
			Catalog    catalogRef         `json:"catalog"`
			Selection  manifest.Selection `json:"selection"`
			Recorded   manifest.Selection `json:"recorded"`
			Components any                `json:"components"`
			Names      map[string]string  `json:"names"`
			Ports      manifest.Ports     `json:"ports"`
			Added      []moduleAdded      `json:"added"`
			Notes      []string           `json:"notes"`
			Warnings   []Warning          `json:"warnings"`
		}{documentSchema, true, true, name, filepath.ToSlash(d.path), len(d.project.Files), d.source, d.catalog,
			d.resolvedSelection(), d.recorded, d.componentsDocument(lock), lock.Names, manifest.Ports{Band: d.plan.Ports.Band}, list(d.added), list(ctx.Notes), ctx.warnings()}
		return emitDocument(ctx, ctx.Stdout, doc, exitOK)
	}

	var text strings.Builder
	fmt.Fprintf(&text, "Project %s in %s (dry run, nothing written)\n", name, displayPath(d.path))
	if d.plan.Multi() {
		d.writeMultiTable(&text)
		fmt.Fprintf(&text, "%d %s\n", len(d.project.Files), plural(len(d.project.Files), "file"))
		return emitText(ctx, text.String())
	}
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
	for _, planned := range d.plan.Components {
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

// dryRunComponent is one entry of the components member of a multi project's
// dry-run document: the multi fields of the lock that the plan fills.
type dryRunComponent struct {
	Stack    string             `json:"stack"`
	Name     string             `json:"name"`
	Index    int                `json:"index"`
	GoModule string             `json:"goModule,omitempty"`
	Database *manifest.Database `json:"database,omitempty"`
	Modules  []string           `json:"modules"`
	Ports    map[string]int     `json:"ports,omitempty"`
	Origin   string             `json:"origin,omitempty"`
	TCK      *dryRunTCK         `json:"tck,omitempty"`
	Backend  string             `json:"backend,omitempty"`
	Names    map[string]string  `json:"names"`
}

// dryRunTCK is the tck member of a backend entry.
type dryRunTCK struct {
	Tags       []string          `json:"tags"`
	Grep       string            `json:"grep"`
	GrepInvert string            `json:"grepInvert,omitempty"`
	Env        map[string]string `json:"env"`
}

// componentsDocument returns the components member: the lock's entries in a
// single project, the multi entries in a multi project.
func (d dryRun) componentsDocument(lock lockfile.Lock) any {
	if !d.plan.Multi() {
		return list(lock.Components)
	}
	entries := make([]dryRunComponent, 0, len(d.plan.Components))
	for _, component := range d.plan.Components {
		entry := dryRunComponent{
			Stack: component.Stack, Name: component.Name, Index: component.Index, GoModule: component.GoModule,
			Modules: moduleIDsOf(component), Ports: component.Ports, Names: componentNames(component),
		}
		if component.Role == manifest.RoleBackend {
			entry.Origin = component.Origin
		}
		if component.Role == manifest.RoleFrontend {
			entry.Backend = component.Partner
		}
		if component.Database != (manifest.Database{}) {
			database := component.Database
			entry.Database = &database
		}
		if tck := component.TCK; tck != nil {
			entry.TCK = &dryRunTCK{Tags: list(tck.Tags), Grep: tck.Grep, GrepInvert: tck.GrepInvert, Env: tck.Env}
			if entry.TCK.Env == nil {
				entry.TCK.Env = map[string]string{}
			}
		}
		entries = append(entries, entry)
	}
	return entries
}

// moduleIDsOf returns the installed module ids of a component in plan order.
func moduleIDsOf(component plan.ComponentPlan) []string {
	ids := make([]string, 0, len(component.Modules))
	for _, module := range component.Modules {
		ids = append(ids, module.Module)
	}
	return ids
}

// componentNames returns the live slots of one component: its own component
// slot and, for a backend, its database, user and service names.
func componentNames(component plan.ComponentPlan) map[string]string {
	found := map[string]string{names.ComponentSlot(component.Stack): component.Name}
	if component.Database.Name != "" {
		found[names.SlotDatabase] = component.Database.Name
	}
	if component.Database.User != "" {
		found[names.SlotDBUser] = component.Database.User
	}
	for key, value := range component.Services {
		found[names.ServiceSlot(key)] = value
	}
	return found
}

// portsText lists the host ports of a component by ascending port, as
// "srv 10101, postgres 10142".
func portsText(host map[string]int) string {
	labels := make([]string, 0, len(host))
	for name := range host {
		labels = append(labels, name)
	}
	sort.Slice(labels, func(i, j int) bool { return host[labels[i]] < host[labels[j]] })
	for i, name := range labels {
		labels[i] = name + " " + strconv.Itoa(host[name])
	}
	return strings.Join(labels, ", ")
}

// writeMultiTable writes the component table of a multi project, one row per
// component in lock order, then one line per derived default.
func (d dryRun) writeMultiTable(text *strings.Builder) {
	rows := [][]string{{"COMPONENT", "STACK", "INDEX", "BACKEND", "GO MODULE", "DATABASE", "PORTS", "MODULES"}}
	for _, component := range d.plan.Components {
		backend := ""
		if component.Role == manifest.RoleFrontend {
			backend = component.Partner
		}
		database := ""
		if component.Database.Name != "" {
			database = component.Database.Name + "/" + component.Database.User
		}
		rows = append(rows, []string{
			component.Name, component.Stack, strconv.Itoa(component.Index), backend, component.GoModule, database,
			portsText(component.Ports), strings.Join(moduleIDsOf(component), ", "),
		})
	}
	writeTable(text, rows)
	for _, note := range d.notes {
		fmt.Fprintf(text, "note: %s\n", note)
	}
}
