package manifest

import (
	"encoding/json"
	"fmt"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/semver"
)

// CoreModule is the id of the module every stack has and no module requires.
const CoreModule = "core"

// FileEntry is one element of files or exclude. A directory entry ends in "/".
// Target is empty unless the entry maps the source file to another path.
type FileEntry struct {
	Path   string
	Target string
}

// ModuleRequirement is one element of requiresModules.
type ModuleRequirement struct {
	Module string
	Range  semver.Range
}

// Module is a decoded module.manifest.json.
type Module struct {
	Stack           string
	Module          string
	Version         semver.Version
	Implied         bool
	ImplementsSpec  map[string]semver.Version
	RequiresCore    *semver.Range
	RequiresModules []ModuleRequirement
	TablePrefixes   []string
	Files           []FileEntry
	Exclude         []FileEntry
}

var moduleKeys = []string{
	"schema", "stack", "module", "version", "implied", "implementsSpec",
	"requiresCore", "requiresModules", "tablePrefixes", "files", "exclude",
}

// DecodeModule decodes and validates a module manifest. path is the manifest
// file path used in issue paths. The Module is filled with whatever decoded;
// it is only meaningful when no issue is returned.
func DecodeModule(path string, data []byte) (Module, []issue.Issue) {
	d := newDecoder(path, CodeManifestInvalid)
	var module Module
	root, ok := d.syntax(data)
	if !ok {
		return module, d.result()
	}
	members, ok := d.object(root, "", moduleKeys)
	if !ok {
		return module, d.result()
	}
	d.schema(members)
	d.moduleIdentity(members, &module)
	d.moduleRequirements(members, &module)
	d.moduleFiles(members, &module)
	return module, d.result()
}

func (d *decoder) moduleIdentity(members map[string]json.RawMessage, module *Module) {
	if raw, ok := d.required(members, "", "stack"); ok {
		if stack, ok := d.str(raw, "/stack"); ok {
			module.Stack = stack
			if !isKnownStack(stack) {
				d.addCode(CodeStackUnknown, "/stack", "stack is not in the stack table")
			}
		}
	}
	if raw, ok := d.required(members, "", "module"); ok {
		if id, ok := d.str(raw, "/module"); ok {
			module.Module = id
			if !validID(id) {
				d.add("/module", "must be a module id of at most 31 characters")
			}
		}
	}
	if raw, ok := d.required(members, "", "version"); ok {
		module.Version, _ = d.version(raw, "/version")
	}
	if raw, ok := d.required(members, "", "implied"); ok {
		module.Implied, _ = d.boolean(raw, "/implied")
	}
	if raw, ok := d.required(members, "", "implementsSpec"); ok {
		module.ImplementsSpec = d.implementsSpec(raw)
	}
}

func (d *decoder) version(raw json.RawMessage, pointer string) (semver.Version, bool) {
	text, ok := d.str(raw, pointer)
	if !ok {
		return semver.Version{}, false
	}
	version, err := semver.ParseVersion(text)
	if err != nil {
		d.add(pointer, err.Error())
		return semver.Version{}, false
	}
	return version, true
}

func (d *decoder) versionRange(raw json.RawMessage, pointer string) (semver.Range, bool) {
	text, ok := d.str(raw, pointer)
	if !ok {
		return semver.Range{}, false
	}
	parsed, err := semver.ParseRange(text)
	if err != nil {
		d.add(pointer, err.Error())
		return semver.Range{}, false
	}
	return parsed, true
}

func (d *decoder) implementsSpec(raw json.RawMessage) map[string]semver.Version {
	members, ok := d.object(raw, "/implementsSpec", nil)
	if !ok {
		return nil
	}
	if len(members) == 0 {
		d.add("/implementsSpec", "must have at least one spec")
	}
	specs := map[string]semver.Version{}
	for id, value := range members {
		pointer := "/implementsSpec" + issue.Pointer(id)
		if !validID(id) {
			d.add(pointer, "key must be a spec id of at most 31 characters")
			continue
		}
		if version, ok := d.version(value, pointer); ok {
			specs[id] = version
		}
	}
	return specs
}

func (d *decoder) moduleRequirements(members map[string]json.RawMessage, module *Module) {
	isCore := module.Module == CoreModule
	if raw, present := members["requiresCore"]; present {
		if isCore {
			d.add("/requiresCore", "core must not declare requiresCore")
		} else if parsed, ok := d.versionRange(raw, "/requiresCore"); ok {
			module.RequiresCore = &parsed
		}
	} else if !isCore && module.Module != "" {
		d.add("/requiresCore", "required key is missing")
	}
	if raw, ok := d.required(members, "", "requiresModules"); ok {
		module.RequiresModules = d.requiresModules(raw)
	}
	prefixes, present := members["tablePrefixes"]
	switch {
	case module.Stack == StackNgWeb && present:
		d.add("/tablePrefixes", "ng-web modules must not declare tablePrefixes")
	case module.Stack == StackGoSrv && !present:
		d.add("/tablePrefixes", "required key is missing")
	case present:
		module.TablePrefixes = d.tablePrefixes(prefixes)
	}
}

func (d *decoder) requiresModules(raw json.RawMessage) []ModuleRequirement {
	items, ok := d.array(raw, "/requiresModules")
	if !ok {
		return nil
	}
	var requirements []ModuleRequirement
	names := make([]string, len(items))
	valid := make([]bool, len(items))
	for i, item := range items {
		pointer := fmt.Sprintf("/requiresModules/%d", i)
		fields, ok := d.object(item, pointer, []string{"module", "range"})
		if !ok {
			continue
		}
		var requirement ModuleRequirement
		complete := true
		if rawName, ok := d.required(fields, pointer, "module"); ok {
			name, ok := d.str(rawName, pointer+"/module")
			switch {
			case !ok:
				complete = false
			case !validID(name):
				d.add(pointer+"/module", "must be a module id of at most 31 characters")
				complete = false
			case name == CoreModule:
				d.add(pointer+"/module", "core is declared by requiresCore")
				complete = false
			default:
				requirement.Module = name
				names[i], valid[i] = name, true
			}
		} else {
			complete = false
		}
		if rawRange, ok := d.required(fields, pointer, "range"); ok {
			parsed, ok := d.versionRange(rawRange, pointer+"/range")
			requirement.Range = parsed
			complete = complete && ok
		} else {
			complete = false
		}
		if complete {
			requirements = append(requirements, requirement)
		}
	}
	d.checkOrderAt("/requiresModules", "/module", names, valid)
	return requirements
}

// checkOrderAt is checkOrder for elements whose key sits under a sub-pointer.
func (d *decoder) checkOrderAt(pointer, sub string, values []string, valid []bool) {
	previous := ""
	havePrevious := false
	for i, value := range values {
		if !valid[i] {
			continue
		}
		at := fmt.Sprintf("%s/%d%s", pointer, i, sub)
		if havePrevious && value == previous {
			d.add(at, "duplicate entry")
		} else if havePrevious && value < previous {
			d.add(at, "entries must be sorted bytewise")
		}
		previous = value
		havePrevious = true
	}
}

func (d *decoder) tablePrefixes(raw json.RawMessage) []string {
	values, valid, ok := d.strings(raw, "/tablePrefixes")
	if !ok {
		return nil
	}
	prefixes := []string{}
	for i, value := range values {
		if !valid[i] {
			continue
		}
		if !tablePrefixPattern.MatchString(value) {
			d.add(fmt.Sprintf("/tablePrefixes/%d", i), "must be 2 to 4 lowercase letters")
			valid[i] = false
			continue
		}
		prefixes = append(prefixes, value)
	}
	d.checkOrder("/tablePrefixes", values, valid)
	return prefixes
}

func (d *decoder) moduleFiles(members map[string]json.RawMessage, module *Module) {
	if raw, ok := d.required(members, "", "files"); ok {
		module.Files = d.fileEntries(raw, "/files")
	}
	if raw, ok := d.required(members, "", "exclude"); ok {
		module.Exclude = d.fileEntries(raw, "/exclude")
	}
}

func (d *decoder) fileEntries(raw json.RawMessage, pointer string) []FileEntry {
	items, ok := d.array(raw, pointer)
	if !ok {
		return nil
	}
	entries := []FileEntry{}
	paths := make([]string, len(items))
	valid := make([]bool, len(items))
	for i, item := range items {
		entry, ok := d.fileEntry(item, fmt.Sprintf("%s/%d", pointer, i))
		if !ok {
			continue
		}
		entries = append(entries, entry)
		paths[i], valid[i] = entry.Path, true
	}
	d.checkOrderAt(pointer, "", paths, valid)
	return entries
}

// fileEntry decodes a string entry or a {path, target} object. The object form
// maps one source file to a target file, so its path cannot be a directory.
func (d *decoder) fileEntry(raw json.RawMessage, pointer string) (FileEntry, bool) {
	if firstByte(raw) == '"' {
		path, ok := d.str(raw, pointer)
		if !ok {
			return FileEntry{}, false
		}
		if !validRepoPath(path) {
			d.add(pointer, "must be a repo-relative path with / separators and no empty, . or .. segment")
			return FileEntry{}, false
		}
		return FileEntry{Path: path}, true
	}
	fields, ok := d.object(raw, pointer, []string{"path", "target"})
	if !ok {
		return FileEntry{}, false
	}
	var entry FileEntry
	complete := true
	if rawPath, ok := d.required(fields, pointer, "path"); ok {
		path, ok := d.str(rawPath, pointer+"/path")
		switch {
		case !ok:
			complete = false
		case !validRepoPath(path):
			d.add(pointer+"/path", "must be a repo-relative path with / separators and no empty, . or .. segment")
			complete = false
		case path[len(path)-1] == '/':
			d.add(pointer+"/path", "must be a file when a target is given")
			complete = false
		default:
			entry.Path = path
		}
	} else {
		complete = false
	}
	if rawTarget, ok := d.required(fields, pointer, "target"); ok {
		target, ok := d.str(rawTarget, pointer+"/target")
		switch {
		case !ok:
			complete = false
		case !validRepoPath(target):
			d.add(pointer+"/target", "must be a repo-relative path with / separators and no empty, . or .. segment")
			complete = false
		case target[len(target)-1] == '/':
			d.add(pointer+"/target", "must be a file path, not a directory")
			complete = false
		default:
			entry.Target = target
		}
	} else {
		complete = false
	}
	return entry, complete
}
