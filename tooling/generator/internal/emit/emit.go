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
	"strconv"
	"strings"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/gorewrite"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/markers"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/plan"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/ports"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/render"
)

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
	return b.roots[p]
}

// compositionRoot returns the project path of the composition root of one
// component: its folder plus the composition root of its stack.
func compositionRoot(component plan.ComponentPlan) (string, bool) {
	info, known := manifest.LookupStack(component.Stack)
	if !known {
		return "", false
	}
	return component.Name + "/" + info.CompositionRoot, true
}

// Errors of the module path scans; the message starts with the code.
var (
	// ErrGoModulePathLeak reports the source module path in a delivered file.
	ErrGoModulePathLeak = errors.New("GO_MODULE_PATH_LEAK")
	// ErrGoModuleCrossReference reports the module path of another Go
	// component in a file of a Go component folder.
	ErrGoModuleCrossReference = errors.New("GO_MODULE_CROSS_REFERENCE")
)

type builder struct {
	opts    BuildOptions
	source  string
	project string
	// firstTarget is the Go module path project-scope files are rewritten to:
	// the first Go component's, else the selection's.
	firstTarget string
	files       map[string][]byte
	// roots holds the project path of every composition root of the plan.
	roots map[string]bool
	// projectOwner maps a delivered project-scope path to the module that
	// delivered it ("stack/module"); a later component that installs the same
	// module skips the file.
	projectOwner map[string]projectDelivery
	// rules maps a source path to the name rules of the installed modules.
	rules map[string][]names.Rule
	// portFiles holds the source paths of the port files of the installed modules.
	portFiles map[string]bool
}

// Build produces the whole project in memory. Nothing touches the disk. An
// error means the catalog, the checkout or the generator is inconsistent
// (I/O or internal failure); selection problems are plan issues.
func Build(opts BuildOptions) (Project, error) {
	selection := opts.Plan.Selection
	switch {
	case opts.Root == nil:
		return Project{}, errors.New("source file system is nil")
	case len(opts.Plan.Components) == 0:
		return Project{}, errors.New("plan has no stacks")
	case selection.Project.Name == "":
		return Project{}, errors.New("plan has no project name")
	case opts.Plan.GoModule == "":
		return Project{}, errors.New("plan has no Go module path")
	case len(opts.CatalogSHA256) != sha256.Size*2:
		return Project{}, errors.New("catalog hash is malformed")
	}
	roots := map[string]bool{}
	firstTarget := ""
	for _, component := range opts.Plan.Components {
		info, known := manifest.LookupStack(component.Stack)
		if known && info.GoModule && component.GoModule == "" {
			return Project{}, fmt.Errorf("plan has no Go module path for component %q", component.Name)
		}
		if known && info.GoModule && firstTarget == "" {
			firstTarget = component.GoModule
		}
		if root, ok := compositionRoot(component); ok {
			roots[root] = true
		}
	}
	if firstTarget == "" {
		firstTarget = selection.Project.GoModule
	}
	b := &builder{
		opts:         opts,
		source:       opts.Plan.GoModule,
		project:      selection.Project.Name,
		firstTarget:  firstTarget,
		files:        map[string][]byte{},
		roots:        roots,
		projectOwner: map[string]projectDelivery{},
		rules:        nameRules(opts.Plan),
		portFiles:    portFiles(opts.Plan),
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
		Names:         p.LockNames(),
		Ports:         lockPorts(p),
		TCKTags:       p.TCKTags,
		TCKGrep:       p.TCKGrep,
		TCKGrepInvert: p.TCKGrepInvert,
		TCKEnv:        p.TCKEnv,
	}
	multi := p.Multi()
	for _, component := range p.Components {
		lock.Components = append(lock.Components, lockComponent(component, multi))
	}
	if multi {
		lock.Names = projectNames(lock.Names)
	}
	for _, spec := range p.Specs {
		entry := lockfile.Spec{Spec: spec.Spec, Version: spec.Version}
		for _, file := range spec.Files {
			delivered, _, err := b.copyFile(nil, "", file)
			if err != nil {
				return Project{}, err
			}
			entry.Files = append(entry.Files, delivered)
		}
		lock.Specs = append(lock.Specs, entry)
	}
	for i := range p.Components {
		component := &p.Components[i]
		for _, module := range component.Modules {
			entry := lockfile.Module{
				Stack:          component.Stack,
				Module:         module.Module,
				Version:        module.Version,
				ImplementsSpec: module.ImplementsSpec,
			}
			if multi {
				entry.Component = component.Name
			}
			for _, file := range module.Files {
				delivered, isDelivered, err := b.copyFile(component, module.Module, file)
				if err != nil {
					return Project{}, err
				}
				if isDelivered {
					entry.Files = append(entry.Files, delivered)
				}
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

// place maps a delivered source-form path to its project path. A path under
// the source root of a stack goes into the folder of the component that
// copies it (component scope); every other path stays as it is (project
// scope). isProject reports the second case. A nil component is a spec file.
func (b *builder) place(component *plan.ComponentPlan, delivered string) (dest string, isProject bool, err error) {
	p := b.opts.Plan
	for _, info := range manifest.Stacks() {
		if !strings.HasPrefix(delivered, info.Root) {
			continue
		}
		rest := strings.TrimPrefix(delivered, info.Root)
		if component != nil && component.Stack == info.ID {
			return component.Name + "/" + rest, false, nil
		}
		if only, hasOne := p.Component(info.ID); hasOne {
			return only.Name + "/" + rest, false, nil
		}
		if b.hasStack(info.ID) {
			return "", false, fmt.Errorf("catalog path %q is under the root of stack %s, which has several components", delivered, info.ID)
		}
	}
	return delivered, true, nil
}

// copyFile reads one catalog file from the checkout, checks it against the
// catalog, applies its transform and records it at its delivered path. The
// bool is false when a project-scope file was already delivered by an earlier
// component that installs the same module.
func (b *builder) copyFile(component *plan.ComponentPlan, owner string, file catalog.File) (lockfile.File, bool, error) {
	delivered := file.Path
	if file.Target != "" {
		delivered = file.Target
	}
	if !fs.ValidPath(file.Path) || !lockfile.ValidPath(file.Path) {
		return lockfile.File{}, false, fmt.Errorf("catalog path %q is not a repo-relative path", file.Path)
	}
	dest, isProject, err := b.place(component, delivered)
	if err != nil {
		return lockfile.File{}, false, err
	}
	if b.reserved(dest) {
		return lockfile.File{}, false, fmt.Errorf("catalog file %q lands on the rendered path %q", file.Path, dest)
	}
	ownerKey := ""
	componentName := ""
	if component != nil {
		ownerKey = component.Stack + "/" + owner
		componentName = component.Name
	}
	// Only another component delivering the same module may skip the file;
	// the same component producing a path twice still reaches b.add and fails.
	if previous, seen := b.projectOwner[dest]; seen && isProject && previous.key == ownerKey && previous.component != componentName {
		return lockfile.File{}, false, nil
	}
	data, err := fs.ReadFile(b.opts.Root, file.Path)
	if err != nil {
		return lockfile.File{}, false, fmt.Errorf("read %s: %w", file.Path, err)
	}
	sourceSum := hashHex(data)
	if sourceSum != file.SHA256 {
		return lockfile.File{}, false, fmt.Errorf("source %s changed since the catalog was built", file.Path)
	}
	transformed, err := b.transform(file.Path, data, owner, b.targetOf(component, isProject))
	if err != nil {
		return lockfile.File{}, false, err
	}
	entry := lockfile.File{Path: dest, SHA256: hashHex(transformed)}
	if entry.SHA256 != sourceSum {
		entry.SourceSHA256 = sourceSum
	}
	if dest != file.Path {
		entry.SourcePath = file.Path
	}
	if err := b.add(dest, transformed); err != nil {
		return lockfile.File{}, false, err
	}
	if isProject {
		b.projectOwner[dest] = projectDelivery{key: ownerKey, component: componentName}
	}
	return entry, true, nil
}

// targetOf returns the Go module path a file of component is rewritten to:
// the component's own for a component-scope file, the first Go component's
// for a project-scope or spec file.
func (b *builder) targetOf(component *plan.ComponentPlan, isProject bool) string {
	if component == nil || isProject || component.GoModule == "" {
		return b.firstTarget
	}
	return component.GoModule
}

// transform applies the copy rules of the source path.
func (b *builder) transform(sourcePath string, data []byte, owner, target string) ([]byte, error) {
	if rules := b.rules[sourcePath]; len(rules) > 0 {
		renamed, err := names.Apply(data, rules, b.opts.Plan.Reference, b.opts.Plan.Slots)
		if err != nil {
			return nil, fmt.Errorf("%s: name rules: %w", sourcePath, err)
		}
		data = renamed
	}
	// Order: name rules on the source bytes, then the marker drop, then the
	// port band substitution, then the go.mod and import rewrite.
	if _, hasMarkers := markers.SyntaxFor(sourcePath); hasMarkers && owner == manifest.CoreModule {
		dropped, err := markers.Drop(sourcePath, data, markers.Keep{Module: b.hasModule, Stack: b.hasStack})
		if err != nil {
			return nil, err
		}
		data = dropped
	}
	if b.portFiles[sourcePath] {
		p := b.opts.Plan.Ports
		rewritten, err := ports.Rewrite(data, p.RefBand, p.Band, p.Offsets)
		if err != nil {
			return nil, fmt.Errorf("%s: port band: %w", sourcePath, err)
		}
		data = rewritten
	}
	switch {
	case sourcePath == gorewrite.GoModPath:
		out, err := gorewrite.RewriteModuleLine(data, b.source, target)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", sourcePath, err)
		}
		return out, nil
	case gorewrite.ShouldRewrite(sourcePath):
		out, _, err := gorewrite.RewriteImports(sourcePath, data, b.source, target)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", sourcePath, err)
		}
		return out, nil
	}
	return data, nil
}

// projectDelivery records who delivered a project-scope path: the
// "stack/module" key and the component that produced it.
type projectDelivery struct {
	key       string
	component string
}

// lockComponent returns the lock entry of one component. A single lock keeps
// the stack and the name; a multi lock adds the instance fields. Ports and
// origin are not recorded yet.
func lockComponent(component plan.ComponentPlan, multi bool) lockfile.Component {
	entry := lockfile.Component{Stack: component.Stack, Name: component.Name}
	if !multi {
		return entry
	}
	index := component.Index
	entry.Index = &index
	entry.GoModule = component.GoModule
	if component.Database != (manifest.Database{}) {
		database := component.Database
		entry.Database = &database
	}
	for _, module := range component.Modules {
		entry.Modules = append(entry.Modules, module.Module)
	}
	if component.Role == manifest.RoleFrontend {
		entry.Backend = component.Partner
	}
	if component.TCK != nil {
		entry.TCK = &lockfile.TCK{
			Tags:       component.TCK.Tags,
			Grep:       component.TCK.Grep,
			GrepInvert: component.TCK.GrepInvert,
			Env:        component.TCK.Env,
		}
	}
	entry.Names = map[string]string{names.ComponentSlot(component.Stack): component.Name}
	if component.Database.Name != "" {
		entry.Names[names.SlotDatabase] = component.Database.Name
	}
	if component.Database.User != "" {
		entry.Names[names.SlotDBUser] = component.Database.User
	}
	for key, value := range component.Services {
		entry.Names[names.ServiceSlot(key)] = value
	}
	return entry
}

// projectNames returns the project-level slots of a multi lock: everything
// except the component slots and the instance's database and service slots,
// which the component entries hold.
func projectNames(all map[string]string) map[string]string {
	kept := map[string]string{}
	for key, value := range all {
		isInstance := key == names.SlotDatabase || key == names.SlotDBUser ||
			strings.HasPrefix(key, names.ComponentSlot("")) || strings.HasPrefix(key, names.ServiceSlot(""))
		if !isInstance {
			kept[key] = value
		}
	}
	return kept
}

const (
	devSrvTaskName = "dev:srv"
	devWebTaskName = "dev:web"
	tckTaskName    = "tck"
)

// readmeRun returns the run section data: one instance per backend component,
// bound to its frontend component when it has one. Every instance shows the
// plan's one origin until ports are planned per instance.
func (b *builder) readmeRun() render.ReadmeRun {
	p := b.opts.Plan
	band := p.Ports.Band
	run := render.ReadmeRun{PortLow: band * 100, PortHigh: band*100 + 99}
	originURL := "http://localhost:" + strconv.Itoa(p.Ports.Origin)
	for _, component := range p.Components {
		if component.Role != manifest.RoleBackend {
			continue
		}
		hasIdentity := false
		for _, module := range component.Modules {
			hasIdentity = hasIdentity || module.Module == "identity"
		}
		instance := render.ReadmeInstance{
			Backend:     component.Name,
			Web:         component.Partner,
			OriginURL:   originURL,
			HasIdentity: hasIdentity,
			DevSrvTask:  devSrvTaskName,
			TCKTask:     tckTaskName,
		}
		if component.Partner != "" {
			instance.DevWebTask = devWebTaskName
		}
		run.Instances = append(run.Instances, instance)
	}
	return run
}

// lockPorts returns the lock's ports member: the effective band whenever the
// catalog declares ports, nil otherwise.
func lockPorts(p plan.Plan) *lockfile.Ports {
	if p.Ports.RefBand == 0 {
		return nil
	}
	return &lockfile.Ports{Band: p.Ports.Band}
}

// portFiles collects the port files of the installed modules by source path.
func portFiles(p plan.Plan) map[string]bool {
	files := map[string]bool{}
	if p.Ports.RefBand == 0 {
		return files
	}
	for _, stack := range p.Components {
		for _, module := range stack.Modules {
			for _, path := range module.PortFiles {
				files[path] = true
			}
		}
	}
	return files
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
	for _, stack := range b.opts.Plan.Components {
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
	for _, stack := range b.opts.Plan.Components {
		if stack.Stack == id {
			return true
		}
	}
	return false
}

// nameRules collects the name rules of the installed modules by source path,
// in the catalog order of (path, text).
func nameRules(p plan.Plan) map[string][]names.Rule {
	rules := map[string][]names.Rule{}
	counted := map[string]bool{}
	for _, stack := range p.Components {
		for _, module := range stack.Modules {
			// Components of one stack that install the same module share its rules.
			key := stack.Stack + "/" + module.Module
			if counted[key] {
				continue
			}
			counted[key] = true
			for _, rule := range module.Names {
				rules[rule.Path] = append(rules[rule.Path], names.Rule{Path: rule.Path, Text: rule.Text})
			}
		}
	}
	return rules
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
	listed := map[string]bool{}
	for _, stack := range p.Components {
		for _, module := range stack.Modules {
			key := stack.Stack + "/" + module.Module
			if listed[key] {
				continue
			}
			listed[key] = true
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
	readme, err := render.Readme(render.ReadmeInput{
		Name:             b.project,
		GeneratorVersion: generator.Version,
		Modules:          rows,
		TCKTags:          p.TCKTags,
		TCKEnv:           env,
		Run:              b.readmeRun(),
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

	for _, stack := range p.Components {
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

// renderGoRoot renders the go-srv composition root of one component with its
// own module list and Go module path.
func (b *builder) renderGoRoot(stack plan.ComponentPlan, record func(string, []byte) error) error {
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
	rewritten, _, err := gorewrite.RewriteImports(sourceForm, raw, b.source, stack.GoModule)
	if err != nil {
		return fmt.Errorf("%s: %w", sourceForm, err)
	}
	root, _ := compositionRoot(stack)
	return record(root, rewritten)
}

// renderWebRoot renders the ng-web composition root of one component; no path
// is rewritten.
func (b *builder) renderWebRoot(stack plan.ComponentPlan, record func(string, []byte) error) error {
	ids := make([]string, 0, len(stack.Modules))
	for _, module := range stack.Modules {
		ids = append(ids, module.Module)
	}
	raw, err := render.ModulesTS(ids)
	if err != nil {
		return err
	}
	root, _ := compositionRoot(stack)
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
	if err := b.scanSourceLeak(files); err != nil {
		return Project{}, err
	}
	if err := b.scanCrossReferences(files); err != nil {
		return Project{}, err
	}
	return Project{Files: files}, nil
}

// goComponents returns the components of a stack with a Go module.
func (b *builder) goComponents() []plan.ComponentPlan {
	var found []plan.ComponentPlan
	for _, component := range b.opts.Plan.Components {
		info, known := manifest.LookupStack(component.Stack)
		if known && info.GoModule {
			found = append(found, component)
		}
	}
	return found
}

// scanSourceLeak refuses a delivered file that still holds the source module
// path. It runs whenever some target differs from the source, and skips the
// folder of a component whose target is the source path itself.
func (b *builder) scanSourceLeak(files []File) error {
	goComponents := b.goComponents()
	anyDiffers := len(goComponents) == 0 && b.firstTarget != b.source
	var ownFolders []string
	for _, component := range goComponents {
		anyDiffers = anyDiffers || component.GoModule != b.source
		if component.GoModule == b.source {
			ownFolders = append(ownFolders, component.Name+"/")
		}
	}
	if !anyDiffers {
		return nil
	}
	needle := []byte(b.source)
	for _, file := range files {
		isOwn := false
		for _, folder := range ownFolders {
			isOwn = isOwn || strings.HasPrefix(file.Path, folder)
		}
		if !isOwn && bytes.Contains(file.Data, needle) {
			return fmt.Errorf("%w: source module path found in %s after the rewrite", ErrGoModulePathLeak, file.Path)
		}
	}
	return nil
}

// scanCrossReferences refuses a file under a Go component folder that holds
// the module path of another Go component followed by a slash or a quote.
func (b *builder) scanCrossReferences(files []File) error {
	goComponents := b.goComponents()
	if len(goComponents) < 2 {
		return nil
	}
	for _, file := range files {
		for _, own := range goComponents {
			if !strings.HasPrefix(file.Path, own.Name+"/") {
				continue
			}
			for _, other := range goComponents {
				if other.Name == own.Name || other.GoModule == own.GoModule {
					continue
				}
				for _, suffix := range []string{"/", "\""} {
					if bytes.Contains(file.Data, []byte(other.GoModule+suffix)) {
						return fmt.Errorf("%w: %s names the module path of component %s", ErrGoModuleCrossReference, file.Path, other.Name)
					}
				}
			}
		}
	}
	return nil
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
