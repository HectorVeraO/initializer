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
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/semver"
)

// SchemaVersion is the only catalog schema this package reads and writes.
const SchemaVersion = 1

// CodeCatalogInvalid is the issue code of a catalog file that Decode refuses.
const CodeCatalogInvalid = "CATALOG_INVALID"

// Catalog is the content of catalog.lock.json. Field order is the key order of
// the encoded file.
type Catalog struct {
	Schema      int          `json:"schema"`
	Generator   string       `json:"generator"`
	GoModule    string       `json:"goModule"`
	ComposeName string       `json:"composeName"`
	Specs       []SpecEntry  `json:"specs"`
	Stacks      []StackEntry `json:"stacks"`
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
	Files           []File            `json:"files"`
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

var (
	sha256Pattern      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	composeNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

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
		v.add("/schema", "must be the number 1")
	}
	if c.Generator == "" {
		v.add("/generator", "must not be empty")
	}
	if !composeNamePattern.MatchString(c.ComposeName) {
		v.add("/composeName", "must match ^[a-z][a-z0-9-]*$")
	}
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
		v.files(at+"/files", module.Files)
	}
	v.sortedStrings(pointer+"/modules", names)
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
