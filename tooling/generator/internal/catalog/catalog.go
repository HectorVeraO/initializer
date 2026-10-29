// Package catalog builds and reads catalog.lock.json, the build output that
// records which files each module and spec owns and their hashes.
package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/ports"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/semver"
)

// SchemaVersion is the only catalog schema this package reads and writes.
const SchemaVersion = 2

// CodeCatalogInvalid is the issue code of a catalog file that Decode refuses.
const CodeCatalogInvalid = "CATALOG_INVALID"

// Catalog is the content of catalog.lock.json. Field order is the key order of
// the encoded file.
type Catalog struct {
	Schema    int    `json:"schema"`
	Generator string `json:"generator"`
	GoModule  string `json:"goModule"`
	// Reference is the slot map of .initializer/reference.json: the value every
	// name rule matches in the source files.
	Reference map[string]string `json:"reference"`
	// PortBand is the reference band of the port tokens, from
	// .initializer/reference.json; 0 when the reference has none.
	PortBand int          `json:"portBand"`
	Specs    []SpecEntry  `json:"specs"`
	Stacks   []StackEntry `json:"stacks"`
}

// SpecEntry is one spec folder.
type SpecEntry struct {
	Spec    string            `json:"spec"`
	Version string            `json:"version"`
	TCKTags []string          `json:"tckTags"`
	TCKEnv  map[string]string `json:"tckEnv"`
	// CooperatesWith lists the tags of other specs this spec's TCK calls.
	CooperatesWith []string `json:"cooperatesWith,omitempty"`
	Files          []File   `json:"files"`
}

// StackEntry is one stack with its modules.
type StackEntry struct {
	Stack   string        `json:"stack"`
	Role    string        `json:"role"`
	Modules []ModuleEntry `json:"modules"`
}

// ModuleEntry is one module of one stack.
type ModuleEntry struct {
	Module          string            `json:"module"`
	Version         string            `json:"version"`
	Implied         bool              `json:"implied"`
	ImplementsSpec  map[string]string `json:"implementsSpec"`
	RequiresCore    string            `json:"requiresCore,omitempty"`
	RequiresModules []Requirement     `json:"requiresModules"`
	TablePrefixes   []string          `json:"tablePrefixes,omitempty"`
	Names           []NameRule        `json:"names,omitempty"`
	Ports           []ports.Port      `json:"ports,omitempty"`
	PortFiles       []string          `json:"portFiles,omitempty"`
	Files           []File            `json:"files"`
}

// NameRule is one name rule of a module manifest as the catalog records it.
type NameRule struct {
	// Path is the source path of the file the rule rewrites.
	Path string `json:"path"`
	// Text is the rule text with %{slot} placeholders.
	Text string `json:"text"`
	// Blocks lists the marker blocks that enclose the rule's occurrences in
	// the source file, sorted: "" for outside any block, "module:<id>",
	// "stack:<id>".
	Blocks []string `json:"blocks"`
}

// Requirement is one element of requiresModules.
type Requirement struct {
	Module string `json:"module"`
	Range  string `json:"range"`
}

// File is one copied file. Target is empty when the file keeps its path.
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Target string `json:"target,omitempty"`
}

// Encode returns the canonical bytes of c: two-space indent, LF line ends, one
// trailing newline, keys in struct order, map keys sorted. Lists are written
// as given; Build sorts them.
func Encode(c Catalog) ([]byte, error) {
	normalized := normalize(c)
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(normalized); err != nil {
		return nil, fmt.Errorf("encode catalog: %w", err)
	}
	return buffer.Bytes(), nil
}

// normalize replaces nil lists and maps by empty ones, so they encode as []
// and {} instead of null.
func normalize(c Catalog) Catalog {
	out := c
	out.Reference = nonNilMap(c.Reference)
	out.Specs = make([]SpecEntry, len(c.Specs))
	for i, spec := range c.Specs {
		spec.TCKTags = nonNilStrings(spec.TCKTags)
		spec.TCKEnv = nonNilMap(spec.TCKEnv)
		spec.Files = nonNilFiles(spec.Files)
		out.Specs[i] = spec
	}
	out.Stacks = make([]StackEntry, len(c.Stacks))
	for i, stack := range c.Stacks {
		modules := make([]ModuleEntry, len(stack.Modules))
		for j, module := range stack.Modules {
			module.ImplementsSpec = nonNilMap(module.ImplementsSpec)
			if module.RequiresModules == nil {
				module.RequiresModules = []Requirement{}
			}
			module.Files = nonNilFiles(module.Files)
			module.Ports = append([]ports.Port(nil), module.Ports...)
			module.PortFiles = append([]string(nil), module.PortFiles...)
			module.Names = append([]NameRule(nil), module.Names...)
			for k := range module.Names {
				module.Names[k].Blocks = nonNilStrings(module.Names[k].Blocks)
			}
			modules[j] = module
		}
		stack.Modules = modules
		out.Stacks[i] = stack
	}
	return out
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nonNilMap(values map[string]string) map[string]string {
	if values == nil {
		return map[string]string{}
	}
	return values
}

func nonNilFiles(files []File) []File {
	if files == nil {
		return []File{}
	}
	return files
}

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Decode reads a catalog strictly: unknown keys, trailing data, a wrong schema,
// malformed versions or hashes, and unsorted or duplicate lists are refused.
func Decode(data []byte) (Catalog, []issue.Issue) {
	var catalog Catalog
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		return Catalog{}, []issue.Issue{invalid("", "invalid catalog JSON: "+err.Error())}
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Catalog{}, []issue.Issue{invalid("", "unexpected data after the JSON value")}
	}
	v := &validator{}
	v.catalog(catalog)
	if len(v.issues) > 0 {
		issue.Sort(v.issues)
		return Catalog{}, v.issues
	}
	return normalize(catalog), nil
}

func invalid(pointer, message string) issue.Issue {
	return issue.Issue{Code: CodeCatalogInvalid, Path: issue.Path("catalog.lock.json", pointer), Message: message}
}

type validator struct {
	issues []issue.Issue
}

func (v *validator) add(pointer, message string) {
	v.issues = append(v.issues, invalid(pointer, message))
}

func (v *validator) catalog(c Catalog) {
	if c.Schema != SchemaVersion {
		v.add("/schema", "must be the number 2")
	}
	if c.Generator == "" {
		v.add("/generator", "must not be empty")
	}
	v.reference(c.Reference)
	v.portBand(c)
	specNames := make([]string, len(c.Specs))
	for i, spec := range c.Specs {
		specNames[i] = spec.Spec
		pointer := fmt.Sprintf("/specs/%d", i)
		v.version(pointer+"/version", spec.Version)
		v.sortedStrings(pointer+"/tckTags", spec.TCKTags)
		v.sortedStrings(pointer+"/cooperatesWith", spec.CooperatesWith)
		v.files(pointer+"/files", spec.Files)
	}
	v.sortedStrings("/specs", specNames)
	stackNames := make([]string, len(c.Stacks))
	for i, stack := range c.Stacks {
		stackNames[i] = stack.Stack
		v.stack(fmt.Sprintf("/stacks/%d", i), stack)
	}
	v.sortedStrings("/stacks", stackNames)
}

func (v *validator) stack(pointer string, stack StackEntry) {
	names := make([]string, len(stack.Modules))
	for i, module := range stack.Modules {
		names[i] = module.Module
		at := fmt.Sprintf("%s/modules/%d", pointer, i)
		v.version(at+"/version", module.Version)
		for spec, version := range module.ImplementsSpec {
			v.version(at+"/implementsSpec"+issue.Pointer(spec), version)
		}
		if module.RequiresCore != "" {
			if _, err := semver.ParseRange(module.RequiresCore); err != nil {
				v.add(at+"/requiresCore", err.Error())
			}
		}
		for j, requirement := range module.RequiresModules {
			if _, err := semver.ParseRange(requirement.Range); err != nil {
				v.add(fmt.Sprintf("%s/requiresModules/%d/range", at, j), err.Error())
			}
		}
		v.sortedStrings(at+"/tablePrefixes", module.TablePrefixes)
		v.nameRules(at+"/names", module.Names)
		v.modulePorts(at, module)
		v.files(at+"/files", module.Files)
	}
	v.sortedStrings(pointer+"/modules", names)
}

// reference checks that every key is a slot of the stack table and every value
// is not empty.
func (v *validator) reference(slots map[string]string) {
	var stacks []string
	for _, info := range manifest.Stacks() {
		stacks = append(stacks, info.ID)
	}
	keys := make([]string, 0, len(slots))
	for key := range slots {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		pointer := "/reference" + issue.Pointer(key)
		switch {
		case !names.IsSlot(key, stacks):
			v.add(pointer, "key is not a slot name")
		case slots[key] == "":
			v.add(pointer, "must not be empty")
		}
	}
}

// nameRules checks the order, the text and the blocks of the rules of a module.
func (v *validator) nameRules(pointer string, rules []NameRule) {
	for i, rule := range rules {
		at := fmt.Sprintf("%s/%d", pointer, i)
		if !validTrackedPath(rule.Path) {
			v.add(at+"/path", "must be a repo-relative path")
		}
		if _, err := names.Slots(rule.Text); err != nil {
			v.add(at+"/text", err.Error())
		}
		v.sortedStrings(at+"/blocks", rule.Blocks)
		for j, block := range rule.Blocks {
			if !validBlock(block) {
				v.add(fmt.Sprintf("%s/blocks/%d", at, j), `must be "", "module:<id>" or "stack:<id>"`)
			}
		}
		if i > 0 {
			previous := rules[i-1]
			isSorted := previous.Path < rule.Path || (previous.Path == rule.Path && previous.Text < rule.Text)
			if !isSorted {
				v.add(at, "rules must be sorted by path then text and unique")
			}
		}
	}
}

// validBlock reports whether b names no block, a module block or a stack block.
func validBlock(b string) bool {
	if b == "" {
		return true
	}
	kind, id, found := strings.Cut(b, ":")
	return found && (kind == "module" || kind == "stack") && id != ""
}

func (v *validator) version(pointer, value string) {
	if _, err := semver.ParseVersion(value); err != nil {
		v.add(pointer, err.Error())
	}
}

func (v *validator) sortedStrings(pointer string, values []string) {
	for i := 1; i < len(values); i++ {
		if values[i] <= values[i-1] {
			v.add(fmt.Sprintf("%s/%d", pointer, i), "entries must be sorted bytewise and unique")
			return
		}
	}
}

func (v *validator) files(pointer string, files []File) {
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.Path
		at := fmt.Sprintf("%s/%d", pointer, i)
		if !validTrackedPath(file.Path) {
			v.add(at+"/path", "must be a repo-relative path")
		}
		if file.Target != "" && !validTrackedPath(file.Target) {
			v.add(at+"/target", "must be a repo-relative path")
		}
		if file.Target == file.Path {
			v.add(at+"/target", "must differ from path when present")
		}
		if !sha256Pattern.MatchString(file.SHA256) {
			v.add(at+"/sha256", "must be 64 lowercase hex digits")
		}
	}
	v.sortedStrings(pointer, paths)
}

// ParseFileList splits the NUL-separated output of git ls-files -z. A trailing
// NUL is allowed; an empty element is refused.
func ParseFileList(data []byte) ([]string, error) {
	if len(data) == 0 {
		return []string{}, nil
	}
	trimmed := bytes.TrimSuffix(data, []byte{0})
	parts := strings.Split(string(trimmed), "\x00")
	for i, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("file list element %d is empty", i)
		}
	}
	return parts, nil
}

// validTrackedPath reports whether p is a clean repo-relative slash path: no
// empty, "." or ".." segment, no backslash, no control character.
func validTrackedPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") {
		return false
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c < 0x20 || c == 0x7f || c == '\\' {
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
