// Package lockfile defines initializer.lock.json, the record a generated project
// keeps of what the generator delivered, and the project layout rules that
// both the writer (emit) and the reader (verify) share.
package lockfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/ports"
)

// Name is the file name of the lock at the project root.
const Name = "initializer.lock.json"

// SelectionName is the file name of the recorded selection at the project root.
const SelectionName = "initializer.selection.json"

// SchemaVersion is the lock schema this package writes; Decode reads 1 and 2.
const SchemaVersion = 2

// Lock is the content of initializer.lock.json (schema 2). Field order is the
// key order of the encoded file.
type Lock struct {
	Schema        int    `json:"schema"`
	Generator     string `json:"generator"`
	CatalogSHA256 string `json:"catalogSha256"`
	// Source is the commit of the source tree; omitted when unknown.
	Source    *Source            `json:"source,omitempty"`
	Selection manifest.Selection `json:"selection"`
	// Components holds one entry per delivered component, sorted by Stack.
	Components []Component `json:"components"`
	// Names holds the effective value of every live slot, component.* included.
	Names map[string]string `json:"names"`
	// Ports is the effective port band; omitted when the catalog declared no
	// ports and in locks written before ports existed.
	Ports   *Ports   `json:"ports,omitempty"`
	TCKTags []string `json:"tckTags"`
	TCKGrep string   `json:"tckGrep"`
	// TCKGrepInvert excludes the scenarios co-tagged with a cooperating spec
	// that is not installed; omitted when empty.
	TCKGrepInvert string            `json:"tckGrepInvert,omitempty"`
	TCKEnv        map[string]string `json:"tckEnv"`
	Specs         []Spec            `json:"specs"`
	Modules       []Module          `json:"modules"`
	Generated     []File            `json:"generated"`
}

// lockV1 is the schema 1 lock, decoded strictly so that a schema 2 member in a
// schema 1 file is refused.
type lockV1 struct {
	Schema        int                `json:"schema"`
	Generator     string             `json:"generator"`
	CatalogSHA256 string             `json:"catalogSha256"`
	Selection     manifest.Selection `json:"selection"`
	TCKTags       []string           `json:"tckTags"`
	TCKGrep       string             `json:"tckGrep"`
	TCKGrepInvert string             `json:"tckGrepInvert,omitempty"`
	TCKEnv        map[string]string  `json:"tckEnv"`
	Specs         []Spec             `json:"specs"`
	Modules       []Module           `json:"modules"`
	Generated     []File             `json:"generated"`
}

// Component is one delivered component: the stack and the project folder name.
// Every other field is set only in a multi lock.
type Component struct {
	Stack string `json:"stack"`
	Name  string `json:"name"`
	// Index is the position among the components of Stack; a pointer so that
	// index 0 encodes.
	Index *int `json:"index,omitempty"`
	// GoModule is the target Go module path of a component of a stack with one.
	GoModule string `json:"goModule,omitempty"`
	// Database is the database and owner role of a backend whose database slots are live.
	Database *Database `json:"database,omitempty"`
	// Modules are the installed module ids in plan order, core included.
	Modules []string `json:"modules,omitempty"`
	// Ports holds the declared ports of the installed modules.
	Ports map[string]int `json:"ports,omitempty"`
	// Origin is the public base URL of a backend.
	Origin string `json:"origin,omitempty"`
	// TCK is the TCK selection of a backend with at least one spec.
	TCK *TCK `json:"tck,omitempty"`
	// Backend names the backend component a frontend is bound to.
	Backend string `json:"backend,omitempty"`
	// Names holds the live slots of this instance.
	Names map[string]string `json:"names,omitempty"`
}

// Database is the database name and owner role of one backend.
type Database = manifest.Database

// TCK is the TCK selection of one backend component.
type TCK struct {
	Tags       []string          `json:"tags"`
	Grep       string            `json:"grep"`
	GrepInvert string            `json:"grepInvert,omitempty"`
	Env        map[string]string `json:"env"`
}

// hasMultiFields reports whether the entry carries a field only a multi lock has.
func (c Component) hasMultiFields() bool {
	hasAny := c.Index != nil || c.GoModule != "" || c.Database != nil || len(c.Modules) > 0 ||
		len(c.Ports) > 0 || c.Origin != "" || c.TCK != nil || c.Backend != "" || len(c.Names) > 0
	return hasAny
}

// Source identifies the source tree the project was generated from.
type Source struct {
	// Commit is the full commit hash of the source tree.
	Commit string `json:"commit"`
	// Modified reports whether a catalogued file differed from Commit.
	Modified bool `json:"modified"`
}

// Ports records the port band of the project.
type Ports struct {
	Band int `json:"band"`
}

// PortBand returns the port band of the project: the recorded band, else the
// reference band 101 for a lock written before ports existed.
func (l Lock) PortBand() int {
	if l.Ports == nil {
		return ports.DefaultBand
	}
	return l.Ports.Band
}

// Spec is one copied spec with the delivered hash of each file.
type Spec struct {
	Spec    string `json:"spec"`
	Version string `json:"version"`
	Files   []File `json:"files"`
}

// Module is one copied module of one stack.
type Module struct {
	Stack          string            `json:"stack"`
	Module         string            `json:"module"`
	Version        string            `json:"version"`
	ImplementsSpec map[string]string `json:"implementsSpec"`
	Files          []File            `json:"files"`
	// Component names the component that holds the module; set only in a multi lock.
	Component string `json:"component,omitempty"`
}

// File is one delivered file. SHA256 hashes the delivered bytes.
type File struct {
	Path string `json:"path"`
	// SHA256 is the hash of the delivered bytes.
	SHA256 string `json:"sha256"`
	// SourceSHA256 is present only when the delivered bytes differ from the catalog's.
	SourceSHA256 string `json:"sourceSha256,omitempty"`
	// SourcePath is present only when the delivered path differs from the source path.
	SourcePath string `json:"sourcePath,omitempty"`
}

var (
	idPattern     = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	commitPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
)

// ModuleFolder returns the folder name of a module inside its stack's modules
// root: the Go folder drops hyphens, every other stack keeps the id.
func ModuleFolder(stack, module string) string {
	info, known := manifest.LookupStack(stack)
	if known && info.GoModule {
		return strings.ReplaceAll(module, "-", "")
	}
	return module
}

// ModuleDir returns the project folder of a module inside component folder,
// with a trailing slash: ModuleDir("acme-srv", "go-srv", "api-keys") is
// "acme-srv/internal/platform/apikeys/". It returns false for an unknown stack.
func ModuleDir(folder, stack, module string) (string, bool) {
	info, known := manifest.LookupStack(stack)
	if !known {
		return "", false
	}
	dir := folder + "/" + info.ModulesRoot + ModuleFolder(stack, module) + "/"
	return dir, true
}

// FolderOf returns the component folder of stack recorded in l. It returns
// false when the stack has no component, and when it has two or more. For a
// schema 1 lock the folder is the stack id.
func (l Lock) FolderOf(stack string) (string, bool) {
	folder := ""
	count := 0
	for _, component := range l.Components {
		if component.Stack == stack {
			folder = component.Name
			count++
		}
	}
	if count != 1 {
		return "", false
	}
	return folder, true
}

// Multi reports whether some stack has two or more components.
func (l Lock) Multi() bool {
	perStack := map[string]int{}
	for _, component := range l.Components {
		perStack[component.Stack]++
		if perStack[component.Stack] > 1 {
			return true
		}
	}
	return false
}

// ComponentOf returns the component that holds module entry m: the entry named
// m.Component of the same stack in a multi lock, the only component of
// m.Stack otherwise.
func (l Lock) ComponentOf(m Module) (Component, bool) {
	if !l.Multi() {
		for _, component := range l.Components {
			if component.Stack == m.Stack {
				return component, true
			}
		}
		return Component{}, false
	}
	for _, component := range l.Components {
		if component.Stack == m.Stack && component.Name == m.Component {
			return component, true
		}
	}
	return Component{}, false
}

// Encode returns the canonical bytes of l: two-space indent, LF line ends and
// one trailing newline. Nil lists and maps encode as [] and {}.
func Encode(l Lock) ([]byte, error) {
	normalized := normalize(l)
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(normalized); err != nil {
		return nil, fmt.Errorf("encode lock: %w", err)
	}
	return buffer.Bytes(), nil
}

func normalize(l Lock) Lock {
	out := l
	out.Selection.Stacks = nonNilStrings(l.Selection.Stacks)
	out.Selection.Modules = nonNilStrings(l.Selection.Modules)
	if out.Components == nil {
		out.Components = []Component{}
	}
	if l.Multi() {
		out.Components = make([]Component, len(l.Components))
		for i, component := range l.Components {
			if component.TCK != nil {
				tck := *component.TCK
				tck.Tags = nonNilStrings(tck.Tags)
				if tck.Env == nil {
					tck.Env = map[string]string{}
				}
				component.TCK = &tck
			}
			out.Components[i] = component
		}
	}
	if out.Names == nil {
		out.Names = map[string]string{}
	}
	out.TCKTags = nonNilStrings(l.TCKTags)
	if out.TCKEnv == nil {
		out.TCKEnv = map[string]string{}
	}
	out.Specs = make([]Spec, len(l.Specs))
	for i, spec := range l.Specs {
		spec.Files = nonNilFiles(spec.Files)
		out.Specs[i] = spec
	}
	out.Modules = make([]Module, len(l.Modules))
	for i, module := range l.Modules {
		if module.ImplementsSpec == nil {
			module.ImplementsSpec = map[string]string{}
		}
		module.Files = nonNilFiles(module.Files)
		out.Modules[i] = module
	}
	out.Generated = nonNilFiles(l.Generated)
	return out
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nonNilFiles(files []File) []File {
	if files == nil {
		return []File{}
	}
	return files
}

// Decode reads a lock of schema 1 or 2 strictly and refuses unknown keys,
// trailing data, a wrong schema, an unknown stack, a malformed id, hash, path or
// name, a duplicate path, and a lock that lists itself. It returns the first
// problem found. A schema 1 lock keeps Schema 1 and gets one component per
// stack of its modules, named after the stack.
func Decode(data []byte) (Lock, error) {
	var probe struct {
		Schema int `json:"schema"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return Lock{}, fmt.Errorf("invalid lock JSON: %w", err)
	}
	var lock Lock
	switch probe.Schema {
	case 1:
		var old lockV1
		if err := decodeStrict(data, &old); err != nil {
			return Lock{}, err
		}
		if old.Selection.Schema != 1 {
			return Lock{}, errors.New("selection.schema must be the number 1")
		}
		if len(old.Selection.Components) > 0 {
			return Lock{}, errors.New("selection.components needs lock schema 2")
		}
		lock = upgradeV1(old)
	case SchemaVersion:
		if err := decodeStrict(data, &lock); err != nil {
			return Lock{}, err
		}
	default:
		return Lock{}, errors.New("schema must be the number 1 or 2")
	}
	if err := check(lock); err != nil {
		return Lock{}, err
	}
	return normalize(lock), nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid lock JSON: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("unexpected data after the JSON value")
	}
	return nil
}

func upgradeV1(old lockV1) Lock {
	stacks := map[string]bool{}
	for _, module := range old.Modules {
		stacks[module.Stack] = true
	}
	components := make([]Component, 0, len(stacks))
	for stack := range stacks {
		components = append(components, Component{Stack: stack, Name: stack})
	}
	sort.Slice(components, func(i, j int) bool { return components[i].Stack < components[j].Stack })
	return Lock{
		Schema:        old.Schema,
		Generator:     old.Generator,
		CatalogSHA256: old.CatalogSHA256,
		Selection:     old.Selection,
		Components:    components,
		Names:         map[string]string{},
		TCKTags:       old.TCKTags,
		TCKGrep:       old.TCKGrep,
		TCKGrepInvert: old.TCKGrepInvert,
		TCKEnv:        old.TCKEnv,
		Specs:         old.Specs,
		Modules:       old.Modules,
		Generated:     old.Generated,
	}
}

// checkSchema2 applies the rules that only schema 2 has.
func checkSchema2(lock Lock) error {
	usedNames := map[string]bool{}
	for _, component := range lock.Components {
		if _, known := manifest.LookupStack(component.Stack); !known {
			return fmt.Errorf("components: stack %q is unknown", component.Stack)
		}
		if !names.ValidLabel(component.Name) {
			return fmt.Errorf("components: name %q is not a label", component.Name)
		}
		if usedNames[component.Name] {
			return fmt.Errorf("components: name %q is used twice", component.Name)
		}
		usedNames[component.Name] = true
	}
	if lock.Multi() {
		if err := checkMultiComponents(lock); err != nil {
			return err
		}
	} else if err := checkSingleComponents(lock); err != nil {
		return err
	}
	if err := checkModuleFolders(lock); err != nil {
		return err
	}
	if lock.Ports != nil && !ports.ValidBand(lock.Ports.Band) {
		return errors.New("ports.band must be an integer from 100 to 654")
	}
	if lock.Source != nil && !commitPattern.MatchString(lock.Source.Commit) {
		return errors.New("source.commit must be 40 or 64 lowercase hex digits")
	}
	return nil
}

// checkSingleComponents refuses every multi field in a lock whose stacks have
// one component each.
func checkSingleComponents(lock Lock) error {
	byStack := map[string]string{}
	for _, component := range lock.Components {
		if component.hasMultiFields() {
			return fmt.Errorf("components: %q has multi-component fields but no stack has two components", component.Name)
		}
		byStack[component.Stack] = component.Name
	}
	for _, module := range lock.Modules {
		if module.Component != "" {
			return fmt.Errorf("module %s: component is set but no stack has two components", module.Module)
		}
		if _, has := byStack[module.Stack]; !has {
			return fmt.Errorf("module %s: stack %q has no component", module.Module, module.Stack)
		}
	}
	return checkNames(lock.Names, byStack)
}

// checkMultiComponents applies the strict rules of a lock with a stack that
// has two or more components.
func checkMultiComponents(lock Lock) error {
	if err := checkMultiNames(lock); err != nil {
		return err
	}
	seen := map[string]int{}
	goModules := map[string]string{}
	backendOf := map[string]string{}
	byName := map[string]Component{}
	for _, component := range lock.Components {
		byName[component.Name] = component
	}
	for _, component := range lock.Components {
		info, _ := manifest.LookupStack(component.Stack)
		where := "components: " + component.Name
		if component.Index == nil || *component.Index != seen[component.Stack] {
			return fmt.Errorf("%s: index must be %d, the position among the %s components", where, seen[component.Stack], component.Stack)
		}
		seen[component.Stack]++
		if len(component.Modules) == 0 {
			return fmt.Errorf("%s: modules is missing", where)
		}
		if err := checkEntryRole(where, component, info, byName, backendOf); err != nil {
			return err
		}
		if err := checkEntryPorts(where, lock, component, info); err != nil {
			return err
		}
		if info.GoModule {
			if component.GoModule == "" {
				return fmt.Errorf("%s: goModule is missing", where)
			}
			if other, dup := goModules[component.GoModule]; dup {
				return fmt.Errorf("%s: goModule %q is also the one of %s", where, component.GoModule, other)
			}
			goModules[component.GoModule] = component.Name
		}
		if component.TCK != nil && (len(component.TCK.Tags) == 0 || component.TCK.Grep == "") {
			return fmt.Errorf("%s: tck needs tags and grep", where)
		}
	}
	return checkModuleEntries(lock)
}

// checkEntryPorts requires the ports of every entry, and the origin of every
// backend, in a lock whose catalog declared ports. A lock without a port band
// records neither.
func checkEntryPorts(where string, lock Lock, component Component, info manifest.StackInfo) error {
	if lock.Ports == nil {
		return nil
	}
	if len(component.Ports) == 0 {
		return fmt.Errorf("%s: ports is missing", where)
	}
	for name, port := range component.Ports {
		if !names.ValidLabel(name) || port < 1 || port > 65535 {
			return fmt.Errorf("%s: ports %q is not a label with a TCP port", where, name)
		}
	}
	if info.Role == manifest.RoleBackend && component.Origin == "" {
		return fmt.Errorf("%s: origin is missing", where)
	}
	return nil
}

// checkEntryRole applies the rules that depend on the role of the stack.
func checkEntryRole(where string, component Component, info manifest.StackInfo, byName map[string]Component, backendOf map[string]string) error {
	if info.Role != manifest.RoleFrontend {
		if component.Backend != "" {
			return fmt.Errorf("%s: backend is only for frontend components", where)
		}
		return nil
	}
	if !info.GoModule && component.GoModule != "" {
		return fmt.Errorf("%s: goModule is only for stacks with a Go module", where)
	}
	target, found := byName[component.Backend]
	targetInfo, _ := manifest.LookupStack(target.Stack)
	if !found || targetInfo.Role != manifest.RoleBackend {
		return fmt.Errorf("%s: backend %q does not name a backend component", where, component.Backend)
	}
	if taken, isTaken := backendOf[component.Backend]; isTaken {
		return fmt.Errorf("%s: backend %q is also the backend of %s", where, component.Backend, taken)
	}
	backendOf[component.Backend] = component.Name
	return nil
}

// checkMultiNames applies the names split: the top-level map has no
// component slot and each entry names itself.
func checkMultiNames(lock Lock) error {
	var stackIDs []string
	for _, info := range manifest.Stacks() {
		stackIDs = append(stackIDs, info.ID)
	}
	for key, value := range lock.Names {
		if value == "" || !names.IsSlot(key, stackIDs) {
			return fmt.Errorf("names: %q is not a live slot with a value", key)
		}
		if strings.HasPrefix(key, "component.") {
			return fmt.Errorf("names: %q belongs to the component entries in a multi lock", key)
		}
	}
	for _, component := range lock.Components {
		where := "components: " + component.Name
		for key, value := range component.Names {
			if value == "" || !names.IsSlot(key, stackIDs) {
				return fmt.Errorf("%s: names %q is not a live slot with a value", where, key)
			}
			isOtherComponent := strings.HasPrefix(key, "component.") && key != "component."+component.Stack
			if isOtherComponent {
				return fmt.Errorf("%s: names %q is not the slot of this component", where, key)
			}
		}
		if component.Names["component."+component.Stack] != component.Name {
			return fmt.Errorf("%s: names component.%s must equal the name", where, component.Stack)
		}
	}
	return nil
}

// checkModuleEntries pairs the modules list of each component with the
// modules[] entries that name it.
func checkModuleEntries(lock Lock) error {
	// listed maps component -> module -> whether a modules[] entry named it.
	listed := map[string]map[string]bool{}
	for _, component := range lock.Components {
		listed[component.Name] = map[string]bool{}
		for _, module := range component.Modules {
			if !idPattern.MatchString(module) {
				return fmt.Errorf("components: %s: module id %q is malformed", component.Name, module)
			}
			if _, dup := listed[component.Name][module]; dup {
				return fmt.Errorf("components: %s: module %q is listed twice", component.Name, module)
			}
			listed[component.Name][module] = false
		}
	}
	for _, entry := range lock.Modules {
		component, found := lock.ComponentOf(entry)
		if !found {
			return fmt.Errorf("module %s: component %q is not a %s component", entry.Module, entry.Component, entry.Stack)
		}
		done, installed := listed[component.Name][entry.Module]
		if !installed {
			return fmt.Errorf("module %s: component %s does not list it in modules", entry.Module, component.Name)
		}
		if done {
			return fmt.Errorf("module %s: component %s has two entries", entry.Module, component.Name)
		}
		listed[component.Name][entry.Module] = true
	}
	for _, component := range lock.Components {
		for _, module := range component.Modules {
			if !listed[component.Name][module] {
				return fmt.Errorf("components: %s: module %q has no modules entry", component.Name, module)
			}
		}
	}
	return nil
}

// checkModuleFolders refuses a module file inside the folder of a component
// other than the one that holds the module.
func checkModuleFolders(lock Lock) error {
	for _, module := range lock.Modules {
		holder, _ := lock.ComponentOf(module)
		for _, file := range module.Files {
			for _, other := range lock.Components {
				isOtherFolder := other.Name != holder.Name && strings.HasPrefix(file.Path, other.Name+"/")
				if isOtherFolder {
					return fmt.Errorf("module %s: %q is inside the folder of the %s component", module.Module, file.Path, other.Name)
				}
			}
		}
	}
	return nil
}

func checkNames(slots map[string]string, byStack map[string]string) error {
	var stackIDs []string
	for _, info := range manifest.Stacks() {
		stackIDs = append(stackIDs, info.ID)
	}
	for key, value := range slots {
		if value == "" {
			return fmt.Errorf("names: %q has an empty value", key)
		}
		if !names.IsSlot(key, stackIDs) {
			return fmt.Errorf("names: %q is not a slot name", key)
		}
		if stack, isComponent := strings.CutPrefix(key, "component."); isComponent && byStack[stack] != value {
			return fmt.Errorf("names: %q must equal the name of the %s component", key, stack)
		}
	}
	for stack := range byStack {
		if _, has := slots["component."+stack]; !has {
			return fmt.Errorf("names: component.%s is missing", stack)
		}
	}
	return nil
}

func check(lock Lock) error {
	if lock.Schema == SchemaVersion {
		if err := checkSchema2(lock); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	checkFile := func(where string, file File) error {
		if !ValidPath(file.Path) {
			return fmt.Errorf("%s: %q is not a project-relative path", where, file.Path)
		}
		if file.Path == Name {
			return fmt.Errorf("%s: the lock must not list itself", where)
		}
		if !sha256Pattern.MatchString(file.SHA256) {
			return fmt.Errorf("%s: sha256 of %q must be 64 lowercase hex digits", where, file.Path)
		}
		if seen[file.Path] {
			return fmt.Errorf("%s: %q is listed twice", where, file.Path)
		}
		seen[file.Path] = true
		return nil
	}
	for _, spec := range lock.Specs {
		if !idPattern.MatchString(spec.Spec) {
			return fmt.Errorf("spec id %q is malformed", spec.Spec)
		}
		for _, file := range spec.Files {
			if err := checkFile("spec "+spec.Spec, file); err != nil {
				return err
			}
		}
	}
	for _, module := range lock.Modules {
		if _, ok := manifest.LookupStack(module.Stack); !ok {
			return fmt.Errorf("stack %q is unknown", module.Stack)
		}
		if !idPattern.MatchString(module.Module) {
			return fmt.Errorf("module id %q is malformed", module.Module)
		}
		for _, file := range module.Files {
			if err := checkFile("module "+module.Module, file); err != nil {
				return err
			}
		}
	}
	for _, file := range lock.Generated {
		if err := checkFile("generated", file); err != nil {
			return err
		}
	}
	return nil
}

// ValidPath reports whether p is a clean project-relative slash path: no
// empty, "." or ".." segment, no leading or trailing slash, no backslash, no
// colon and no control character.
func ValidPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") {
		return false
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c < 0x20 || c == 0x7f || c == 0x5c || c == ':' {
			return false
		}
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
