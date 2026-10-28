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
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

// Name is the file name of the lock at the project root.
const Name = "initializer.lock.json"

// SelectionName is the file name of the recorded selection at the project root.
const SelectionName = "initializer.selection.json"

// SchemaVersion is the only lock schema this package reads and writes.
const SchemaVersion = 1

// Lock is the content of initializer.lock.json. Field order is the key order of
// the encoded file.
type Lock struct {
	Schema        int                `json:"schema"`
	Generator     string             `json:"generator"`
	CatalogSHA256 string             `json:"catalogSha256"`
	Selection     manifest.Selection `json:"selection"`
	TCKTags       []string           `json:"tckTags"`
	TCKGrep       string             `json:"tckGrep"`
	TCKEnv        map[string]string  `json:"tckEnv"`
	Specs         []Spec             `json:"specs"`
	Modules       []Module           `json:"modules"`
	Generated     []File             `json:"generated"`
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
)

// ModulesRoot returns the modules root of a stack ("go-srv/internal/platform/")
// and false for a stack the generator does not know.
func ModulesRoot(stack string) (string, bool) {
	switch stack {
	case manifest.StackGoSrv:
		return "go-srv/internal/platform/", true
	case manifest.StackNgWeb:
		return "ng-web/src/app/platform/", true
	}
	return "", false
}

// ModuleFolder returns the folder name of a module inside its stack's modules
// root: the Go folder drops hyphens, every other stack keeps the id.
func ModuleFolder(stack, module string) string {
	if stack == manifest.StackGoSrv {
		return strings.ReplaceAll(module, "-", "")
	}
	return module
}

// ModuleDir returns the slash path of a module folder with a trailing slash.
func ModuleDir(stack, module string) (string, bool) {
	root, ok := ModulesRoot(stack)
	if !ok {
		return "", false
	}
	return root + ModuleFolder(stack, module) + "/", true
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

// Decode reads a lock strictly and refuses unknown keys, trailing data, a
// wrong schema, an unknown stack, a malformed id, hash or path, a duplicate
// path, and a lock that lists itself. It returns the first problem found.
func Decode(data []byte) (Lock, error) {
	var lock Lock
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&lock); err != nil {
		return Lock{}, fmt.Errorf("invalid lock JSON: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Lock{}, errors.New("unexpected data after the JSON value")
	}
	if err := check(lock); err != nil {
		return Lock{}, err
	}
	return normalize(lock), nil
}

func check(lock Lock) error {
	if lock.Schema != SchemaVersion {
		return errors.New("schema must be the number 1")
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
		if _, ok := ModulesRoot(module.Stack); !ok {
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
