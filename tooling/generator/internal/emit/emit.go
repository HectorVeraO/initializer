// Package emit builds a generated project in memory and writes it: owned
// files with their transforms, the rendered composition roots and README, the
// recorded selection and initializer.lock.json.
package emit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/gorewrite"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/markers"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/render"
)

// composeFile is the compose file whose line 1 names the compose project.
const composeFile = ".deploy/local/docker-compose.yml"

// readmeName is the rendered project README.
const readmeName = "README.md"

// BuildOptions are the inputs of one project build.
type BuildOptions struct {
	// Root is the repository checkout the catalog describes.
	Root fs.FS
	// Plan is the resolved selection.
	Plan plan.Plan
	// CatalogSHA256 is the SHA-256 of the catalog.lock.json bytes used.
	CatalogSHA256 string
	// Source is the commit of the source tree; nil omits it from the lock.
	Source *lockfile.Source
	// Recorded is the document initializer.selection.json and the lock's
	// selection hold. A zero value (Schema 0) records Plan.Selection.
	Recorded manifest.Selection
}

// File is one file of the generated project.
type File struct {
	// Path is the project-relative slash path.
	Path string
	Data []byte
}

// Project is the whole generated tree in write order: paths ascending, with
// initializer.lock.json last.
type Project struct {
	Files []File
}

// reserved reports whether p is a file the generator renders; no copy may land
// on it. The composition roots sit in the component folders of the plan.
func (b *builder) reserved(p string) bool {
	switch p {
	case readmeName, lockfile.SelectionName, lockfile.Name:
		return true
	}
	for _, stack := range b.opts.Plan.Stacks {
		if root, ok := b.compositionRoot(stack.Stack); ok && root == p {
			return true
		}
	}
	return false
}

// compositionRoot returns the project path of the composition root of stack.
func (b *builder) compositionRoot(stack string) (string, bool) {
	info, known := manifest.LookupStack(stack)
	if !known {
		return "", false
	}
	return b.opts.Plan.ProjectPath(info.Root + info.CompositionRoot), true
}

type builder struct {
	opts    BuildOptions
	source  string
	target  string
	project string
	files   map[string][]byte
}

// Build produces the whole project in memory. Nothing touches the disk. An
// error means the catalog, the checkout or the generator is inconsistent
// (I/O or internal failure); selection problems are plan issues.
func Build(opts BuildOptions) (Project, error) {
	selection := opts.Plan.Selection
	switch {
	case opts.Root == nil:
		return Project{}, errors.New("source file system is nil")
	case len(opts.Plan.Stacks) == 0:
		return Project{}, errors.New("plan has no stacks")
	case selection.Project.Name == "":
		return Project{}, errors.New("plan has no project name")
	case opts.Plan.GoModule == "" || selection.Project.GoModule == "":
		return Project{}, errors.New("plan has no Go module path")
	case opts.Plan.ComposeName == "":
		return Project{}, errors.New("plan has no compose name")
	case len(opts.CatalogSHA256) != sha256.Size*2:
		return Project{}, errors.New("catalog hash is malformed")
	}
	b := &builder{
		opts:    opts,
		source:  opts.Plan.GoModule,
		target:  selection.Project.GoModule,
		project: selection.Project.Name,
		files:   map[string][]byte{},
	}
	return b.run()
}

func (b *builder) run() (Project, error) {
	p := b.opts.Plan
	lock := lockfile.Lock{
		Schema:        lockfile.SchemaVersion,
		Generator:     generator.Version,
		CatalogSHA256: b.opts.CatalogSHA256,
		Source:        b.opts.Source,
		Selection:     b.recorded(),
		Names:         map[string]string{"project": b.project},
		TCKTags:       p.TCKTags,
		TCKGrep:       p.TCKGrep,
		TCKGrepInvert: p.TCKGrepInvert,
		TCKEnv:        p.TCKEnv,
	}
	for _, component := range p.Components {
		lock.Components = append(lock.Components, lockfile.Component{Stack: component.Stack, Name: component.Name})
		lock.Names["component."+component.Stack] = component.Name
	}
	for _, spec := range p.Specs {
		entry := lockfile.Spec{Spec: spec.Spec, Version: spec.Version}
		for _, file := range spec.Files {
			delivered, err := b.copyFile(file, "")
			if err != nil {
				return Project{}, err
			}
			entry.Files = append(entry.Files, delivered)
		}
		lock.Specs = append(lock.Specs, entry)
	}
	for _, stack := range p.Stacks {
		for _, module := range stack.Modules {
			entry := lockfile.Module{
				Stack:          stack.Stack,
				Module:         module.Module,
				Version:        module.Version,
				ImplementsSpec: module.ImplementsSpec,
			}
			for _, file := range module.Files {
				delivered, err := b.copyFile(file, module.Module)
				if err != nil {
					return Project{}, err
				}
				entry.Files = append(entry.Files, delivered)
			}
			lock.Modules = append(lock.Modules, entry)
		}
	}
	generated, err := b.render()
	if err != nil {
		return Project{}, err
	}
	lock.Generated = generated
	encoded, err := lockfile.Encode(lock)
	if err != nil {
		return Project{}, err
	}
	return b.assemble(encoded)
}

// add records one output file once.
func (b *builder) add(p string, data []byte) error {
	if !lockfile.ValidPath(p) {
		return fmt.Errorf("output path %q is not a project-relative path", p)
	}
	if _, found := b.files[p]; found {
		return fmt.Errorf("output path %q is produced twice", p)
	}
	b.files[p] = data
	return nil
}

// copyFile reads one catalog file from the checkout, checks it against the
// catalog, applies its transform and records it at its delivered path.
func (b *builder) copyFile(file catalog.File, owner string) (lockfile.File, error) {
	delivered := file.Path
	if file.Target != "" {
		delivered = file.Target
	}
	if !fs.ValidPath(file.Path) || !lockfile.ValidPath(file.Path) {
		return lockfile.File{}, fmt.Errorf("catalog path %q is not a repo-relative path", file.Path)
	}
	dest := b.opts.Plan.ProjectPath(delivered)
	if b.reserved(dest) {
		return lockfile.File{}, fmt.Errorf("catalog file %q lands on the rendered path %q", file.Path, dest)
	}
	data, err := fs.ReadFile(b.opts.Root, file.Path)
	if err != nil {
		return lockfile.File{}, fmt.Errorf("read %s: %w", file.Path, err)
	}
	sourceSum := hashHex(data)
	if sourceSum != file.SHA256 {
		return lockfile.File{}, fmt.Errorf("source %s changed since the catalog was built", file.Path)
	}
	transformed, err := b.transform(file.Path, data, owner)
	if err != nil {
		return lockfile.File{}, err
	}
	entry := lockfile.File{Path: dest, SHA256: hashHex(transformed)}
	if entry.SHA256 != sourceSum {
		entry.SourceSHA256 = sourceSum
	}
	if dest != file.Path {
		entry.SourcePath = file.Path
	}
	if err := b.add(dest, transformed); err != nil {
		return lockfile.File{}, err
	}
	return entry, nil
}

// transform applies the copy rule of the source path.
func (b *builder) transform(sourcePath string, data []byte, owner string) ([]byte, error) {
	if _, hasMarkers := markers.SyntaxFor(sourcePath); hasMarkers && owner == manifest.CoreModule {
		dropped, err := markers.Drop(sourcePath, data, markers.Keep{Module: b.hasModule, Stack: b.hasStack})
		if err != nil {
			return nil, err
		}
		data = dropped
	}
	switch {
	case sourcePath == gorewrite.GoModPath:
		out, err := gorewrite.RewriteModuleLine(data, b.source, b.target)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", sourcePath, err)
		}
		return out, nil
	case sourcePath == composeFile:
		return rewriteComposeName(data, b.opts.Plan.ComposeName, b.project)
	case gorewrite.ShouldRewrite(sourcePath):
		out, _, err := gorewrite.RewriteImports(sourcePath, data, b.source, b.target)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", sourcePath, err)
		}
		return out, nil
	}
	return data, nil
}

// recorded returns the selection the project records.
func (b *builder) recorded() manifest.Selection {
	if b.opts.Recorded.Schema == 0 {
		return b.opts.Plan.Selection
	}
	return b.opts.Recorded
}

// hasModule reports whether any selected stack installs the module.
func (b *builder) hasModule(id string) bool {
	for _, stack := range b.opts.Plan.Stacks {
		for _, module := range stack.Modules {
			if module.Module == id {
				return true
			}
		}
	}
	return false
}

// hasStack reports whether the plan selects the stack.
func (b *builder) hasStack(id string) bool {
	for _, stack := range b.opts.Plan.Stacks {
		if stack.Stack == id {
			return true
		}
	}
	return false
}

// rewriteComposeName replaces line 1 "name: <from>" by "name: <to>".
func rewriteComposeName(data []byte, from, to string) ([]byte, error) {
	want := "name: " + from
	first := data
	if end := bytes.IndexByte(data, '\n'); end >= 0 {
		first = data[:end]
	}
	if string(first) != want {
		return nil, fmt.Errorf("%s line 1 must be exactly %q", composeFile, want)
	}
	out := make([]byte, 0, len(data)+len(to))
	out = append(out, "name: "...)
	out = append(out, to...)
	out = append(out, data[len(first):]...)
	return out, nil
}

// render writes the README, the selection and the composition roots, and
// returns their lock entries sorted by path.
func (b *builder) render() ([]lockfile.File, error) {
	p := b.opts.Plan
	var generated []lockfile.File
	record := func(path string, data []byte) error {
		if err := b.add(path, data); err != nil {
			return err
		}
		generated = append(generated, lockfile.File{Path: path, SHA256: hashHex(data)})
		return nil
	}

	var rows []render.ReadmeModule
	for _, stack := range p.Stacks {
		for _, module := range stack.Modules {
			rows = append(rows, render.ReadmeModule{Stack: stack.Stack, Module: module.Module, Version: module.Version})
		}
	}
	keys := make([]string, 0, len(p.TCKEnv))
	for key := range p.TCKEnv {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var env []render.EnvPair
	for _, key := range keys {
		env = append(env, render.EnvPair{Key: key, Value: p.TCKEnv[key]})
	}
	var components []render.Component
	for _, component := range p.Components {
		components = append(components, render.Component{Stack: component.Stack, Name: component.Name})
	}
	readme, err := render.Readme(render.ReadmeInput{
		Name:             b.project,
		GeneratorVersion: generator.Version,
		Components:       components,
		Modules:          rows,
		TCKTags:          p.TCKTags,
		TCKEnv:           env,
	})
	if err != nil {
		return nil, err
	}
	if err := record(readmeName, readme); err != nil {
		return nil, err
	}

	selection, err := encodeSelection(b.recorded())
	if err != nil {
		return nil, err
	}
	if err := record(lockfile.SelectionName, selection); err != nil {
		return nil, err
	}

	for _, stack := range p.Stacks {
		switch stack.Stack {
		case manifest.StackGoSrv:
			if err := b.renderGoRoot(stack, record); err != nil {
				return nil, err
			}
		case manifest.StackNgWeb:
			if err := b.renderWebRoot(stack, record); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("stack %q has no composition root template", stack.Stack)
		}
	}
	sort.Slice(generated, func(i, j int) bool { return generated[i].Path < generated[j].Path })
	return generated, nil
}

// renderGoRoot renders the go-srv composition root.
func (b *builder) renderGoRoot(stack plan.StackPlan, record func(string, []byte) error) error {
	var folders []string
	for _, module := range stack.Modules {
		if module.Module != manifest.CoreModule {
			folders = append(folders, lockfile.ModuleFolder(stack.Stack, module.Module))
		}
	}
	raw, err := render.ModulesGo(b.source, folders)
	if err != nil {
		return err
	}
	info, _ := manifest.LookupStack(stack.Stack)
	sourceForm := info.Root + info.CompositionRoot
	rewritten, _, err := gorewrite.RewriteImports(sourceForm, raw, b.source, b.target)
	if err != nil {
		return fmt.Errorf("%s: %w", sourceForm, err)
	}
	root, _ := b.compositionRoot(stack.Stack)
	return record(root, rewritten)
}

// renderWebRoot renders the ng-web composition root; no path is rewritten.
func (b *builder) renderWebRoot(stack plan.StackPlan, record func(string, []byte) error) error {
	ids := make([]string, 0, len(stack.Modules))
	for _, module := range stack.Modules {
		ids = append(ids, module.Module)
	}
	raw, err := render.ModulesTS(ids)
	if err != nil {
		return err
	}
	root, _ := b.compositionRoot(stack.Stack)
	return record(root, raw)
}

// assemble orders the files, appends the lock and scans every file for a
// leaked source module path.
func (b *builder) assemble(lock []byte) (Project, error) {
	paths := make([]string, 0, len(b.files))
	for p := range b.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	files := make([]File, 0, len(paths)+1)
	for _, p := range paths {
		files = append(files, File{Path: p, Data: b.files[p]})
	}
	files = append(files, File{Path: lockfile.Name, Data: lock})
	if b.source != b.target {
		needle := []byte(b.source)
		for _, file := range files {
			if bytes.Contains(file.Data, needle) {
				return Project{}, fmt.Errorf("source module path found in %s after the rewrite", file.Path)
			}
		}
	}
	return Project{Files: files}, nil
}

// encodeSelection returns the canonical bytes of the recorded selection.
func encodeSelection(s manifest.Selection) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(s); err != nil {
		return nil, fmt.Errorf("encode selection: %w", err)
	}
	return buffer.Bytes(), nil
}

func hashHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
