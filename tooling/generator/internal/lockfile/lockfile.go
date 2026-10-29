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
type Component struct {
	Stack string `json:"stack"`
	Name  string `json:"name"`
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
	byStack := map[string]string{}
	usedNames := map[string]bool{}
	for _, component := range lock.Components {
		if _, known := manifest.LookupStack(component.Stack); !known {
			return fmt.Errorf("components: stack %q is unknown", component.Stack)
		}
		if _, dup := byStack[component.Stack]; dup {
			return fmt.Errorf("components: stack %q is listed twice", component.Stack)
		}
		if !names.ValidLabel(component.Name) {
			return fmt.Errorf("components: name %q is not a label", component.Name)
		}
		if usedNames[component.Name] {
			return fmt.Errorf("components: name %q is used twice", component.Name)
		}
		byStack[component.Stack] = component.Name
		usedNames[component.Name] = true
	}
	for _, module := range lock.Modules {
		if _, has := byStack[module.Stack]; !has {
			return fmt.Errorf("module %s: stack %q has no component", module.Module, module.Stack)
		}
		for _, file := range module.Files {
			for stack, name := range byStack {
				isOtherFolder := stack != module.Stack && strings.HasPrefix(file.Path, name+"/")
				if isOtherFolder {
					return fmt.Errorf("module %s: %q is inside the folder of the %s component", module.Module, file.Path, stack)
				}
			}
		}
	}
	if err := checkNames(lock.Names, byStack); err != nil {
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
