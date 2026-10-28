// Package versions enforces manifest version bumps and lists missing tags (L23).
package versions

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/gates/internal/repo"
	"github.com/HectorVeraO/initializer/tooling/gates/internal/report"
)

// Rule codes.
const (
	RuleVersionInvalid       = "VERSION_INVALID"
	RuleVersionNotBumped     = "VERSION_NOT_BUMPED"
	RuleVersionDecreased     = "VERSION_DECREASED"
	RuleComponentPathOverlap = "COMPONENT_PATH_OVERLAP"
	RuleVersionNotPrerelease = "VERSION_NOT_PRERELEASE"

	refVersions   = "L23"
	refPrerelease = "L76"

	// preReleaseVersion is the only version a component may have while the
	// pre-release setting is on.
	preReleaseVersion = "0.1.0"

	specManifestName   = "spec.manifest.json"
	moduleManifestName = repo.ManifestName
)

// Component is one versioned unit: a spec or a stack module.
type Component struct {
	Name         string   // tag component: "spec-<spec>" or "<stack>-<module>"
	ManifestPath string   // repo-relative path of its manifest
	Version      string   // X.Y.Z from the manifest
	Paths        []string // repo-relative files and folders that belong to it (folders end in "/")
	// Exclude lists the files and folders the manifest carves out of Paths;
	// they belong to no component unless a longer entry says otherwise.
	Exclude []string
}

// compositionRoots wire the modules together and belong to no component.
var compositionRoots = map[string]bool{
	"go-srv/cmd/server/modules.go":       true,
	"ng-web/src/app/platform/modules.ts": true,
}

// componentName is what a spec folder or a module id must look like to be a
// safe part of a git tag.
var componentName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

type location struct {
	dir          string
	manifestName string
	stack        string // empty for specs
}

var locations = []location{
	{dir: "specs", manifestName: specManifestName},
	{dir: "go-srv/internal/platform", manifestName: moduleManifestName, stack: "go-srv"},
	{dir: "ng-web/src/app/platform", manifestName: moduleManifestName, stack: "ng-web"},
}

// source reads the manifests of one repository state.
type source interface {
	// subdirs lists the directory names directly under dir, sorted.
	subdirs(dir string) ([]string, error)
	// read returns the file at the repo-relative path; found is false when it does not exist.
	read(filePath string) (content []byte, found bool, err error)
}

type fsSource struct{ root string }

func (s fsSource) subdirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, filepath.FromSlash(dir)))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

func (s fsSource) read(filePath string) ([]byte, bool, error) {
	content, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(filePath)))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", filePath, err)
	}
	return content, true, nil
}

type gitSource struct{ root, rev string }

func (s gitSource) subdirs(dir string) ([]string, error) {
	files, err := repo.ListFiles(s.root, s.rev, dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var names []string
	for _, file := range files {
		prefix := dir + "/"
		if dir == "." {
			prefix = ""
		}
		rest, ok := strings.CutPrefix(file, prefix)
		name, _, nested := strings.Cut(rest, "/")
		if ok && nested && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func (s gitSource) read(filePath string) ([]byte, bool, error) {
	return repo.ShowFile(s.root, s.rev, filePath)
}

func violation(rule, manifestPath, format string, args ...any) report.Violation {
	return violationRef(refVersions, rule, manifestPath, format, args...)
}

func violationRef(ref, rule, manifestPath, format string, args ...any) report.Violation {
	return report.Violation{
		Rule:     rule,
		Ref:      ref,
		Severity: report.SeverityError,
		Path:     manifestPath,
		Message:  fmt.Sprintf(format, args...),
	}
}

// Discover reads every spec.manifest.json and module.manifest.json under root.
// Components are sorted by name; a manifest that cannot name a component is
// reported and left out.
func Discover(root string) ([]Component, []report.Violation, error) {
	return load(fsSource{root: root})
}

func load(src source) ([]Component, []report.Violation, error) {
	var components []Component
	var violations []report.Violation
	byName := map[string]string{}
	for _, loc := range locations {
		dirs, err := src.subdirs(loc.dir)
		if err != nil {
			return nil, nil, err
		}
		for _, dir := range dirs {
			manifestPath := path.Join(loc.dir, dir, loc.manifestName)
			raw, found, err := src.read(manifestPath)
			if err != nil {
				return nil, nil, err
			}
			if !found {
				continue
			}
			component, usable, problems := buildComponent(loc, dir, manifestPath, raw)
			violations = append(violations, problems...)
			if !usable {
				continue
			}
			folderProblems, err := folderEntriesWithoutSlash(src, component)
			if err != nil {
				return nil, nil, err
			}
			violations = append(violations, folderProblems...)
			first, duplicated := byName[component.Name]
			if duplicated {
				violations = append(violations, violation(RuleVersionInvalid, manifestPath,
					"component %q is already declared by %s", component.Name, first))
				continue
			}
			byName[component.Name] = manifestPath
			components = append(components, component)
		}
	}
	sort.Slice(components, func(i, j int) bool { return components[i].Name < components[j].Name })
	violations = append(violations, checkOverlap(components)...)
	sortViolations(violations)
	return components, violations, nil
}

// buildComponent turns one manifest into a component; usable is false when
// the manifest cannot name a component.
func buildComponent(loc location, dir, manifestPath string, raw []byte) (Component, bool, []report.Violation) {
	doc, err := parseManifest(raw)
	if err != nil {
		return Component{}, false, []report.Violation{violation(RuleVersionInvalid, manifestPath, "%s %v", loc.manifestName, err)}
	}
	component := Component{ManifestPath: manifestPath, Version: doc.Version}
	var violations []report.Violation
	id := doc.Module
	if loc.stack == "" {
		id = dir
		component.Name = "spec-" + id
		component.Paths = []string{path.Join(loc.dir, dir) + "/"}
	} else {
		component.Name = loc.stack + "-" + id
	}
	if !componentName.MatchString(id) {
		problem := violation(RuleVersionInvalid, manifestPath, "component id %q must match %s", id, componentName)
		return Component{}, false, []report.Violation{problem}
	}
	_, err = parseVersion(doc.Version)
	if err != nil {
		violations = append(violations, violation(RuleVersionInvalid, manifestPath, "%v", err))
	}
	if loc.stack != "" {
		for _, entry := range doc.Files {
			normalized, normErr := normalizeEntry(entry.Path)
			if normErr != nil {
				violations = append(violations, violation(RuleVersionInvalid, manifestPath, "files: %v", normErr))
				continue
			}
			component.Paths = append(component.Paths, normalized)
		}
		for _, entry := range doc.Exclude {
			normalized, normErr := normalizeEntry(entry)
			if normErr != nil {
				violations = append(violations, violation(RuleVersionInvalid, manifestPath, "exclude: %v", normErr))
				continue
			}
			component.Exclude = append(component.Exclude, normalized)
		}
		sort.Strings(component.Paths)
		sort.Strings(component.Exclude)
	}
	return component, true, violations
}

// folderEntriesWithoutSlash reports every path or exclude entry of c that
// names an existing folder but lacks the trailing "/", because matches would
// treat it as a single file and its contents would never require a bump.
func folderEntriesWithoutSlash(src source, c Component) ([]report.Violation, error) {
	var violations []report.Violation
	entries := append(append([]string{}, c.Paths...), c.Exclude...)
	for _, entry := range entries {
		parent := path.Dir(entry)
		if strings.HasSuffix(entry, "/") {
			continue
		}
		siblings, err := src.subdirs(parent)
		if err != nil {
			return nil, err
		}
		if slices.Contains(siblings, path.Base(entry)) {
			violations = append(violations, violation(RuleVersionInvalid, c.ManifestPath,
				"path entry %q is a folder and must end with \"/\"", entry))
		}
	}
	return violations, nil
}

func isSpec(c Component) bool {
	return path.Base(c.ManifestPath) == specManifestName
}

// checkOverlap reports an entry claimed by two components (an equal-length
// winner for the same file) and a module entry inside a spec folder.
func checkOverlap(components []Component) []report.Violation {
	var violations []report.Violation
	claimedBy := map[string][]string{}
	manifestOf := map[string]string{}
	var specFolders []string
	for _, c := range components {
		manifestOf[c.Name] = c.ManifestPath
		if isSpec(c) {
			specFolders = append(specFolders, c.Paths...)
			continue
		}
		seen := map[string]bool{}
		entries := append(append([]string{}, c.Paths...), c.Exclude...)
		for _, entry := range entries {
			if !seen[entry] {
				seen[entry] = true
				claimedBy[entry] = append(claimedBy[entry], c.Name)
			}
		}
	}
	entries := make([]string, 0, len(claimedBy))
	for entry := range claimedBy {
		entries = append(entries, entry)
	}
	sort.Strings(entries)
	for _, entry := range entries {
		names := claimedBy[entry]
		if len(names) > 1 {
			violations = append(violations, violation(RuleComponentPathOverlap, manifestOf[names[0]],
				"%q is listed by %s", entry, strings.Join(names, " and ")))
		}
		for _, folder := range specFolders {
			if strings.HasPrefix(entry, folder) {
				violations = append(violations, violation(RuleComponentPathOverlap, manifestOf[names[0]],
					"%q is inside spec folder %q, which its spec owns", entry, folder))
			}
		}
	}
	return violations
}

func sortViolations(violations []report.Violation) {
	sort.SliceStable(violations, func(i, j int) bool {
		if violations[i].Path != violations[j].Path {
			return violations[i].Path < violations[j].Path
		}
		return violations[i].Rule < violations[j].Rule
	})
}

// ownersOf returns the names of the components that own file: none for a
// composition root or a file carved out by a winning exclude entry.
func ownersOf(components []Component, file string) []string {
	if compositionRoots[file] {
		return nil
	}
	for _, c := range components {
		if isSpec(c) && matches(c.Paths[0], file) {
			return []string{c.Name}
		}
	}
	bestLength := -1
	excluded := map[string]bool{}
	winners := map[string]bool{}
	consider := func(name, entry string, isExclude bool) {
		if !matches(entry, file) || len(entry) < bestLength {
			return
		}
		if len(entry) > bestLength {
			bestLength = len(entry)
			excluded = map[string]bool{}
			winners = map[string]bool{}
		}
		winners[name] = true
		if isExclude {
			excluded[name] = true
		}
	}
	for _, c := range components {
		if isSpec(c) {
			continue
		}
		for _, entry := range c.Paths {
			consider(c.Name, entry, false)
		}
		for _, entry := range c.Exclude {
			consider(c.Name, entry, true)
		}
	}
	var owners []string
	for name := range winners {
		if !excluded[name] {
			owners = append(owners, name)
		}
	}
	sort.Strings(owners)
	return owners
}

// matches reports whether entry (a file, or a folder ending in "/") covers file.
func matches(entry, file string) bool {
	if strings.HasSuffix(entry, "/") {
		return strings.HasPrefix(file, entry)
	}
	return entry == file
}

// CheckBumps compares HEAD with merge-base(BaseRev, HEAD): a component whose
// files changed must carry a higher manifest version, and no version may drop.
// With preRelease on, those two rules are off and every component must be at
// 0.1.0 instead (L76).
func CheckBumps(ctx context.Context, root, baseRev string, preRelease bool) ([]report.Violation, error) {
	err := ctx.Err()
	if err != nil {
		return nil, err
	}
	head, err := repo.ResolveRev(root, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("resolve HEAD: %w", err)
	}
	base, err := repo.ResolveRev(root, baseRev)
	if err != nil {
		return nil, fmt.Errorf("resolve base revision %s: %w", baseRev, err)
	}
	mergeBase, err := repo.MergeBase(root, base, head)
	if err != nil {
		return nil, fmt.Errorf("find merge base with %s: %w", baseRev, err)
	}
	headComponents, violations, err := load(gitSource{root: root, rev: head})
	if err != nil {
		return nil, err
	}
	baseComponents, _, err := load(gitSource{root: root, rev: mergeBase})
	if err != nil {
		return nil, err
	}
	changes, err := repo.DiffNameStatus(root, mergeBase, head, ".")
	if err != nil {
		return nil, err
	}
	changed := map[string]bool{}
	for _, change := range changes {
		for _, file := range []string{change.Path, change.OldPath} {
			if file == "" {
				continue
			}
			for _, name := range ownersOf(headComponents, file) {
				changed[name] = true
			}
			for _, name := range ownersOf(baseComponents, file) {
				changed[name] = true
			}
		}
	}
	baseVersions := map[string]string{}
	for _, c := range baseComponents {
		baseVersions[c.Name] = c.Version
	}
	for _, c := range headComponents {
		if preRelease {
			violations = append(violations, checkPreReleaseVersion(c)...)
			continue
		}
		baseVersion, existed := baseVersions[c.Name]
		if !existed {
			continue
		}
		order, cmpErr := CompareVersions(c.Version, baseVersion)
		if cmpErr != nil {
			continue
		}
		if order < 0 {
			violations = append(violations, violation(RuleVersionDecreased, c.ManifestPath,
				"%s went from %s to %s", c.Name, baseVersion, c.Version))
		}
		if order == 0 && changed[c.Name] {
			violations = append(violations, violation(RuleVersionNotBumped, c.ManifestPath,
				"%s changed since %s but stays at version %s; bump its manifest version", c.Name, baseRev, c.Version))
		}
	}
	sortViolations(violations)
	return violations, nil
}

// checkPreReleaseVersion reports a component whose parseable version is not
// the pre-release version; an unparseable one already has VERSION_INVALID.
func checkPreReleaseVersion(c Component) []report.Violation {
	_, err := parseVersion(c.Version)
	if err != nil || c.Version == preReleaseVersion {
		return nil
	}
	return []report.Violation{violationRef(refPrerelease, RuleVersionNotPrerelease, c.ManifestPath,
		"%s is at %s; while versioning.json preRelease is true every version is %s", c.Name, c.Version, preReleaseVersion)}
}

// MissingTags returns "<component>-v<version>" for every component whose current
// version has no tag yet, sorted bytewise.
func MissingTags(ctx context.Context, root string) ([]string, error) {
	err := ctx.Err()
	if err != nil {
		return nil, err
	}
	components, _, err := Discover(root)
	if err != nil {
		return nil, err
	}
	existing, err := repo.ListTags(root)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, tag := range existing {
		have[tag] = true
	}
	var missing []string
	for _, c := range components {
		_, parseErr := parseVersion(c.Version)
		if parseErr != nil {
			continue
		}
		tag := c.Name + "-v" + c.Version
		if !have[tag] {
			missing = append(missing, tag)
		}
	}
	sort.Strings(missing)
	return missing, nil
}
