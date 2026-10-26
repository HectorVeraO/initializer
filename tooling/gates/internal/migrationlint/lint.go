// Package migrationlint checks migration names, table names and prefixes (RA.3).
package migrationlint

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/HectorVeraO/initializer/tooling/gates/internal/repo"
	"github.com/HectorVeraO/initializer/tooling/gates/internal/report"
)

// Rule codes.
const (
	RuleLocationInvalid     = "MIGRATION_LOCATION_INVALID"
	RuleNameInvalid         = "MIGRATION_NAME_INVALID"
	RuleTimestampInvalid    = "MIGRATION_TIMESTAMP_INVALID"
	RuleTimestampDuplicate  = "MIGRATION_TIMESTAMP_DUPLICATE"
	RuleGooseUpMissing      = "MIGRATION_GOOSE_UP_MISSING"
	RuleOutOfOrder          = "MIGRATION_OUT_OF_ORDER"
	RuleModuleUnregistered  = "MIGRATION_MODULE_UNREGISTERED"
	RulePrefixUnregistered  = "MIGRATION_PREFIX_UNREGISTERED"
	RulePrefixForeign       = "MIGRATION_PREFIX_FOREIGN"
	RulePrefixReserved      = "MIGRATION_PREFIX_RESERVED"
	RuleTableNameInvalid    = "TABLE_NAME_INVALID"
	RuleTableQuoted         = "TABLE_QUOTED_IDENTIFIER"
	RuleTableSchemaInvalid  = "TABLE_SCHEMA_INVALID"
	RuleTablePrefixMismatch = "TABLE_PREFIX_MISMATCH"
	RuleModified            = "MIGRATION_MODIFIED"
	RuleDestructive         = "MIGRATION_DESTRUCTIVE"
)

const (
	ref        = "RA.3"
	sqlRoot    = "go-srv"
	gooseUp    = "-- +goose Up"
	timeLayout = "20060102150405"
)

//go:embed reserved-prefixes.json
var reservedPrefixesJSON []byte

var (
	locationPattern = regexp.MustCompile(`^go-srv/internal/gatbase/([^/]+)/migrations/([^/]+\.sql)$`)
	// anyMigrationPattern also matches nested files, for files that existed at the base.
	anyMigrationPattern = regexp.MustCompile(`^go-srv/internal/gatbase/[^/]+/migrations/.+\.sql$`)
	namePattern         = regexp.MustCompile(`^(\d{14})_([a-z]{2,4})_([a-z0-9]+(?:_[a-z0-9]+)*)\.sql$`)
	prefixPattern       = regexp.MustCompile(`^[a-z]{2,4}$`)
)

// Options configures Lint.
type Options struct {
	// Root is the repository root.
	Root string
	// BaseRev is the resolved base revision; "" skips MIGRATION_MODIFIED and MIGRATION_OUT_OF_ORDER.
	BaseRev string
}

// Lint returns every violation plus the number of migration files checked.
func Lint(opts Options) (violations []report.Violation, filesChecked int, err error) {
	if opts.Root == "" {
		return nil, 0, fmt.Errorf("migrationlint: root is required")
	}
	reserved, err := loadReserved()
	if err != nil {
		return nil, 0, err
	}
	modules, err := repo.DiscoverModules(opts.Root)
	if err != nil {
		return nil, 0, err
	}
	files, err := listSQLFiles(opts.Root)
	if err != nil {
		return nil, 0, err
	}

	l := &linter{opts: opts, reserved: reserved, modules: map[string]*moduleInfo{}, timestamps: map[string][]string{}}
	l.registerModules(modules, files)
	for _, file := range files {
		err = l.checkFile(file)
		if err != nil {
			return nil, 0, err
		}
	}
	l.checkDuplicateTimestamps()
	if opts.BaseRev != "" {
		err = l.checkAgainstBase()
		if err != nil {
			return nil, 0, err
		}
	}

	sort.SliceStable(l.violations, func(i, j int) bool {
		a, b := l.violations[i], l.violations[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Rule < b.Rule
	})
	return l.violations, len(files), nil
}

func loadReserved() (map[string]bool, error) {
	var list []string
	err := json.Unmarshal(reservedPrefixesJSON, &list)
	if err != nil {
		return nil, fmt.Errorf("decode reserved-prefixes.json: %w", err)
	}
	reserved := make(map[string]bool, len(list))
	for _, prefix := range list {
		reserved[prefix] = true
	}
	return reserved, nil
}

// listSQLFiles returns the repo-relative paths (forward slashes) of every .sql
// file under <root>/go-srv, sorted. Only that tree is walked.
func listSQLFiles(root string) ([]string, error) {
	base := filepath.Join(root, sqlRoot)
	_, err := os.Stat(base)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", sqlRoot, err)
	}
	var files []string
	err = filepath.WalkDir(base, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		isSQL := strings.EqualFold(filepath.Ext(entry.Name()), ".sql")
		if !entry.Type().IsRegular() || !isSQL {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", sqlRoot, err)
	}
	sort.Strings(files)
	return files, nil
}

type moduleInfo struct {
	dir string
	// prefixes holds the well-formed tablePrefixes of the manifest.
	prefixes map[string]bool
}

type linter struct {
	opts       Options
	reserved   map[string]bool
	modules    map[string]*moduleInfo
	owners     map[string]string
	timestamps map[string][]string
	violations []report.Violation
}

func (l *linter) add(rule, filePath string, line int, format string, args ...any) {
	l.violations = append(l.violations, report.Violation{
		Rule:     rule,
		Ref:      ref,
		Severity: report.SeverityError,
		Path:     filePath,
		Line:     line,
		Message:  fmt.Sprintf(format, args...),
	})
}

// registerModules decodes each module's prefixes and reports reserved or
// doubly claimed prefixes and modules that have migrations but no registry.
func (l *linter) registerModules(modules []repo.ModuleDir, files []string) {
	hasFiles := map[string]bool{}
	for _, file := range files {
		match := locationPattern.FindStringSubmatch(file)
		if match != nil {
			hasFiles[match[1]] = true
		}
	}
	l.owners = map[string]string{}
	for _, m := range modules {
		info := &moduleInfo{dir: m.Dir, prefixes: map[string]bool{}}
		l.modules[m.Dir] = info
		needsRegistry := hasFiles[m.Dir]
		if m.ManifestError != "" && needsRegistry {
			l.add(RuleModuleUnregistered, m.ManifestPath, 0, "%s is not usable: %s", repo.ManifestName, m.ManifestError)
		}
		for _, prefix := range m.TablePrefixes {
			if !prefixPattern.MatchString(prefix) {
				if needsRegistry {
					l.add(RuleModuleUnregistered, m.ManifestPath, 0, "tablePrefixes entry %q must be 2 to 4 lowercase letters", prefix)
				}
				continue
			}
			info.prefixes[prefix] = true
			if l.reserved[prefix] {
				l.add(RulePrefixReserved, m.ManifestPath, 0, "tablePrefixes claims reserved prefix %q", prefix)
				continue
			}
			owner, taken := l.owners[prefix]
			if taken && owner != m.Dir {
				l.add(RulePrefixReserved, m.ManifestPath, 0, "tablePrefixes claims prefix %q already claimed by module directory %s", prefix, owner)
				continue
			}
			l.owners[prefix] = m.Dir
		}
		if needsRegistry && m.ManifestError == "" {
			if !m.HasManifest {
				l.add(RuleModuleUnregistered, m.ManifestPath, 0, "module directory %s has migrations but no %s", m.Dir, repo.ManifestName)
			} else if len(m.TablePrefixes) == 0 {
				l.add(RuleModuleUnregistered, m.ManifestPath, 0, "%s has an empty tablePrefixes", repo.ManifestName)
			}
		}
	}
}

func (l *linter) checkFile(file string) error {
	match := locationPattern.FindStringSubmatch(file)
	if match == nil || !strings.HasSuffix(file, ".sql") {
		l.add(RuleLocationInvalid, file, 0, "SQL files live in go-srv/internal/gatbase/<module>/migrations/")
		return nil
	}
	module, name := match[1], match[2]
	info := l.modules[module]

	l.checkName(file, name, info)

	content, err := os.ReadFile(filepath.Join(l.opts.Root, filepath.FromSlash(file)))
	if err != nil {
		return fmt.Errorf("read %s: %w", file, err)
	}
	if !hasGooseUp(content) {
		l.add(RuleGooseUpMissing, file, 0, "the file needs a line exactly %q", gooseUp)
	}
	for _, statement := range Scan(content) {
		l.checkStatement(file, info, statement)
	}
	return nil
}

func hasGooseUp(content []byte) bool {
	for _, line := range bytes.Split(content, []byte{'\n'}) {
		if string(bytes.TrimRight(line, " \t\r")) == gooseUp {
			return true
		}
	}
	return false
}

func (l *linter) checkName(file, name string, info *moduleInfo) {
	match := namePattern.FindStringSubmatch(name)
	if match == nil {
		l.add(RuleNameInvalid, file, 0, "name %q must match <14-digit timestamp>_<prefix>_<name>.sql", name)
		return
	}
	stamp, prefix := match[1], match[2]
	_, err := time.Parse(timeLayout, stamp)
	if err != nil {
		l.add(RuleTimestampInvalid, file, 0, "%s is not a valid YYYYMMDDhhmmss timestamp", stamp)
	}
	l.timestamps[stamp] = append(l.timestamps[stamp], file)

	if len(info.prefixes) == 0 || info.prefixes[prefix] {
		return
	}
	owner, claimed := l.owners[prefix]
	if claimed && owner != info.dir {
		l.add(RulePrefixForeign, file, 0, "prefix %q belongs to module directory %s, not %s", prefix, owner, info.dir)
		return
	}
	l.add(RulePrefixUnregistered, file, 0, "prefix %q is not in tablePrefixes of module directory %s", prefix, info.dir)
}

func (l *linter) checkDuplicateTimestamps() {
	stamps := make([]string, 0, len(l.timestamps))
	for stamp := range l.timestamps {
		stamps = append(stamps, stamp)
	}
	sort.Strings(stamps)
	for _, stamp := range stamps {
		group := l.timestamps[stamp]
		for _, file := range group[1:] {
			l.add(RuleTimestampDuplicate, file, 0, "timestamp %s is already used by %s", stamp, group[0])
		}
	}
}

// checkAgainstBase compares the migration files at the merge base with HEAD.
func (l *linter) checkAgainstBase() error {
	mergeBase, err := repo.MergeBase(l.opts.Root, l.opts.BaseRev, "HEAD")
	if err != nil {
		return fmt.Errorf("find merge base with %s: %w", l.opts.BaseRev, err)
	}
	changes, err := repo.DiffNameStatus(l.opts.Root, mergeBase, "HEAD", repo.GatbaseDir)
	if err != nil {
		return err
	}
	newestAtBase := map[string]string{}
	newest := func(module string) (string, error) {
		cached, known := newestAtBase[module]
		if known {
			return cached, nil
		}
		files, listErr := repo.ListFiles(l.opts.Root, mergeBase, path.Join(repo.GatbaseDir, module, "migrations"))
		if listErr != nil {
			return "", listErr
		}
		latest := ""
		for _, file := range files {
			match := locationPattern.FindStringSubmatch(file)
			if match != nil && match[2] > latest {
				latest = match[2]
			}
		}
		newestAtBase[module] = latest
		return latest, nil
	}

	for _, change := range changes {
		removedPath := ""
		addedPath := ""
		switch change.Kind {
		case repo.ChangeModified, repo.ChangeDeleted:
			removedPath = change.Path
		case repo.ChangeRenamed:
			removedPath = change.OldPath
			addedPath = change.Path
		case repo.ChangeAdded:
			addedPath = change.Path
		}
		if removedPath != "" && anyMigrationPattern.MatchString(removedPath) {
			l.add(RuleModified, removedPath, 0, "a migration that exists at the base is immutable (%s); fix a mistake with a new migration", describe(change))
		}
		match := locationPattern.FindStringSubmatch(addedPath)
		if match == nil {
			continue
		}
		latest, newestErr := newest(match[1])
		if newestErr != nil {
			return newestErr
		}
		if latest != "" && match[2] < latest {
			l.add(RuleOutOfOrder, addedPath, 0, "%s sorts before %s, the newest migration of this module at the base", match[2], latest)
		}
	}
	return nil
}

func describe(change repo.Change) string {
	switch change.Kind {
	case repo.ChangeDeleted:
		return "deleted"
	case repo.ChangeRenamed:
		return "renamed to " + change.Path
	default:
		return "modified"
	}
}
