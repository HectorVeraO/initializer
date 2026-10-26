// Package flagscheck validates flags.yaml (RA.2, L24).
package flagscheck

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // America/Santiago must resolve on runners without a tz database

	"go.yaml.in/yaml/v3"

	"github.com/HectorVeraO/initializer/tooling/gates/internal/report"
)

// Rule codes reported by Check.
const (
	RuleFileMissing      = "FLAGS_FILE_MISSING"
	RuleParseError       = "FLAGS_PARSE_ERROR"
	RuleUnknownField     = "FLAGS_UNKNOWN_FIELD"
	RuleKeyInvalid       = "FLAG_KEY_INVALID"
	RuleKeyDuplicate     = "FLAG_KEY_DUPLICATE"
	RuleTypeInvalid      = "FLAG_TYPE_INVALID"
	RuleDefaultMissing   = "FLAG_DEFAULT_MISSING"
	RuleDescriptionMiss  = "FLAG_DESCRIPTION_MISSING"
	RuleReleaseDefault   = "FLAG_RELEASE_DEFAULT_TRUE"
	RuleExpiresAtMissing = "FLAG_EXPIRES_AT_MISSING"
	RuleExpiresAtInvalid = "FLAG_EXPIRES_AT_INVALID"
	RuleOpsHasExpiry     = "FLAG_OPS_HAS_EXPIRY"
	RuleExpired          = "FLAG_EXPIRED"
	RuleExpiresTooFar    = "FLAG_EXPIRES_TOO_FAR"
	RuleExpiryExtended   = "FLAG_EXPIRY_EXTENDED"
	RuleTypeChanged      = "FLAG_TYPE_CHANGED"
	RuleExpiresSoon      = "FLAG_EXPIRES_SOON"
)

const (
	flagsPath          = "flags.yaml"
	ref                = "RA.2"
	maxKeyLength       = 64
	maxNewFlagLifetime = 30
	dateLayout         = "2006-01-02"
	santiagoLocation   = "America/Santiago"
	typeRelease        = "release"
	typeOps            = "ops"
	topLevelFlagsKey   = "flags"
	yamlNullTag        = "!!null"
	yamlBoolTag        = "!!bool"
)

var (
	keyPattern       = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	yamlErrorLine    = regexp.MustCompile(`line (\d+)`)
	knownEntryFields = map[string]bool{"key": true, "type": true, "default": true, "expires_at": true, "description": true}
)

// Options for one run.
type Options struct {
	// FilePath is the flags.yaml to check (the repo root file).
	FilePath string
	// Base is the same file at the base revision; nil when it did not exist.
	Base []byte
	// Today is the calendar date in America/Santiago; the time of day is ignored.
	Today time.Time
	// WarnDays makes release flags expiring within this many days warn.
	WarnDays int
}

// SantiagoDate returns the calendar date of now in America/Santiago as a
// midnight UTC time, the form Options.Today takes.
func SantiagoDate(now time.Time) (time.Time, error) {
	location, err := time.LoadLocation(santiagoLocation)
	if err != nil {
		return time.Time{}, fmt.Errorf("load %s: %w", santiagoLocation, err)
	}
	year, month, day := now.In(location).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC), nil
}

type entry struct {
	line       int
	key        string
	keyOK      bool
	flagType   string
	expiresAt  time.Time
	hasExpires bool
}

type collector struct {
	violations []report.Violation
}

func (c *collector) add(rule string, severity report.Severity, line int, format string, args ...any) {
	c.violations = append(c.violations, report.Violation{
		Rule:     rule,
		Ref:      ref,
		Severity: severity,
		Path:     flagsPath,
		Line:     line,
		Message:  fmt.Sprintf(format, args...),
	})
}

func (c *collector) fail(rule string, line int, format string, args ...any) {
	c.add(rule, report.SeverityError, line, format, args...)
}

// Check collects every violation in the file; it never stops at the first one.
// The error is operational only (unreadable file).
func Check(opts Options) ([]report.Violation, error) {
	data, err := os.ReadFile(opts.FilePath)
	if errors.Is(err, fs.ErrNotExist) {
		missing := report.Violation{
			Rule: RuleFileMissing, Ref: ref, Severity: report.SeverityError, Path: flagsPath,
			Message: "flags.yaml not found at the repository root",
		}
		return []report.Violation{missing}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", opts.FilePath, err)
	}

	today := midnightUTC(opts.Today)
	c := &collector{}
	entries := parseEntries(data, c)

	var baseByKey map[string]entry
	if opts.Base != nil {
		baseByKey = map[string]entry{}
		for _, e := range parseEntries(opts.Base, &collector{}) {
			if e.keyOK {
				baseByKey[e.key] = e
			}
		}
	}

	firstLine := map[string]int{}
	for _, e := range entries {
		if !e.keyOK {
			continue
		}
		first, seen := firstLine[e.key]
		if seen {
			c.fail(RuleKeyDuplicate, e.line, "flag %q is already declared on line %d", e.key, first)
		} else {
			firstLine[e.key] = e.line
		}
		checkLifecycle(c, e, baseByKey, today, opts.WarnDays)
	}

	sort.SliceStable(c.violations, func(i, j int) bool {
		return c.violations[i].Line < c.violations[j].Line
	})
	return c.violations, nil
}

func midnightUTC(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// checkLifecycle applies the date and base-revision rules to one entry.
func checkLifecycle(c *collector, e entry, base map[string]entry, today time.Time, warnDays int) {
	baseEntry, inBase := base[e.key]
	hasBothTypes := inBase && baseEntry.flagType != "" && e.flagType != ""
	if hasBothTypes && baseEntry.flagType != e.flagType {
		c.fail(RuleTypeChanged, e.line, "flag %q changed type from %s to %s; remove the flag and add it again", e.key, baseEntry.flagType, e.flagType)
	}
	if e.flagType != typeRelease || !e.hasExpires {
		return
	}
	expires := e.expiresAt.Format(dateLayout)
	if !e.expiresAt.After(today) {
		c.fail(RuleExpired, e.line, "release flag %q expired on %s; remove the flag and its old implementation (TBD rule 5)", e.key, expires)
	}
	wasDatedRelease := inBase && baseEntry.flagType == typeRelease && baseEntry.hasExpires
	if wasDatedRelease && !baseEntry.expiresAt.Equal(e.expiresAt) {
		c.fail(RuleExpiryExtended, e.line, "release flag %q changed expires_at from %s to %s; the remedy is removing the flag or re-adding it", e.key, baseEntry.expiresAt.Format(dateLayout), expires)
	}
	if !inBase && e.expiresAt.After(today.AddDate(0, 0, maxNewFlagLifetime)) {
		c.fail(RuleExpiresTooFar, e.line, "new release flag %q expires %s, more than %d days after %s", e.key, expires, maxNewFlagLifetime, today.Format(dateLayout))
	}
	isSoon := e.expiresAt.After(today) && !e.expiresAt.After(today.AddDate(0, 0, warnDays))
	if isSoon {
		c.add(RuleExpiresSoon, report.SeverityWarning, e.line, "release flag %q expires %s, within %d days", e.key, expires, warnDays)
	}
}

func deref(n *yaml.Node) *yaml.Node {
	if n != nil && n.Kind == yaml.AliasNode && n.Alias != nil {
		return n.Alias
	}
	return n
}

func isValue(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.ScalarNode && n.Tag != yamlNullTag
}

func singleLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// parseEntries decodes the document and validates each entry's own fields.
func parseEntries(data []byte, c *collector) []entry {
	var doc yaml.Node
	err := yaml.Unmarshal(data, &doc)
	if err != nil {
		line := 0
		match := yamlErrorLine.FindStringSubmatch(err.Error())
		if match != nil {
			line, _ = strconv.Atoi(match[1])
		}
		c.fail(RuleParseError, line, "not valid YAML: %s", singleLine(err.Error()))
		return nil
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		c.fail(RuleParseError, 0, "file is empty; expected a top-level mapping with a flags sequence")
		return nil
	}
	top := deref(doc.Content[0])
	if top.Kind != yaml.MappingNode {
		c.fail(RuleParseError, top.Line, "top level must be a mapping with a flags sequence")
		return nil
	}

	var flagsNode *yaml.Node
	for i := 0; i+1 < len(top.Content); i += 2 {
		name := deref(top.Content[i])
		isFlags := name.Kind == yaml.ScalarNode && name.Value == topLevelFlagsKey
		switch {
		case isFlags && flagsNode != nil:
			c.fail(RuleParseError, name.Line, "top-level field %q is declared twice", topLevelFlagsKey)
		case isFlags:
			flagsNode = deref(top.Content[i+1])
		default:
			c.fail(RuleUnknownField, name.Line, "unknown top-level field %q; only %q is allowed", name.Value, topLevelFlagsKey)
		}
	}
	if flagsNode == nil || flagsNode.Kind != yaml.SequenceNode {
		line := top.Line
		if flagsNode != nil {
			line = flagsNode.Line
		}
		c.fail(RuleParseError, line, "top-level field %q must be a sequence (use [] for no flags)", topLevelFlagsKey)
		return nil
	}

	entries := make([]entry, 0, len(flagsNode.Content))
	for _, item := range flagsNode.Content {
		node := deref(item)
		if node.Kind != yaml.MappingNode {
			c.fail(RuleParseError, node.Line, "each flag must be a mapping")
			continue
		}
		entries = append(entries, parseEntry(node, c))
	}
	return entries
}

// parseEntry validates the fields of one flag that need no other flag or date.
func parseEntry(node *yaml.Node, c *collector) entry {
	fields := map[string]*yaml.Node{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := deref(node.Content[i])
		value := deref(node.Content[i+1])
		isKnown := name.Kind == yaml.ScalarNode && knownEntryFields[name.Value]
		if !isKnown {
			c.fail(RuleUnknownField, name.Line, "unknown flag field %q; allowed: key, type, default, expires_at, description", name.Value)
			continue
		}
		_, dup := fields[name.Value]
		if dup {
			c.fail(RuleParseError, name.Line, "flag field %q is declared twice", name.Value)
			continue
		}
		fields[name.Value] = value
	}

	e := entry{line: node.Line}
	label := "flag"

	keyNode := fields["key"]
	switch {
	case !isValue(keyNode):
		c.fail(RuleKeyInvalid, node.Line, "flag has no key")
	case !keyPattern.MatchString(keyNode.Value) || len(keyNode.Value) > maxKeyLength:
		c.fail(RuleKeyInvalid, keyNode.Line, "flag key %q must match ^[a-z][a-z0-9_]*$ and be at most %d characters", keyNode.Value, maxKeyLength)
	default:
		e.key = keyNode.Value
		e.keyOK = true
		e.line = keyNode.Line
		label = fmt.Sprintf("flag %q", e.key)
	}

	typeNode := fields["type"]
	switch {
	case isValue(typeNode) && (typeNode.Value == typeRelease || typeNode.Value == typeOps):
		e.flagType = typeNode.Value
	case typeNode != nil:
		c.fail(RuleTypeInvalid, typeNode.Line, "%s: type must be release or ops", label)
	default:
		c.fail(RuleTypeInvalid, node.Line, "%s: type must be release or ops", label)
	}

	defaultNode := fields["default"]
	hasBoolDefault := defaultNode != nil && defaultNode.Kind == yaml.ScalarNode && defaultNode.Tag == yamlBoolTag
	if !hasBoolDefault {
		line := node.Line
		if defaultNode != nil {
			line = defaultNode.Line
		}
		c.fail(RuleDefaultMissing, line, "%s: default must be a YAML boolean (true or false)", label)
	}
	if hasBoolDefault && e.flagType == typeRelease && strings.EqualFold(defaultNode.Value, "true") {
		c.fail(RuleReleaseDefault, defaultNode.Line, "%s: a release flag must default to false", label)
	}

	descriptionNode := fields["description"]
	if !isValue(descriptionNode) || strings.TrimSpace(descriptionNode.Value) == "" {
		line := node.Line
		if descriptionNode != nil {
			line = descriptionNode.Line
		}
		c.fail(RuleDescriptionMiss, line, "%s: description is required and must not be blank", label)
	}

	checkExpiry(c, &e, fields["expires_at"], node.Line, label)
	return e
}

func checkExpiry(c *collector, e *entry, expiresNode *yaml.Node, entryLine int, label string) {
	switch e.flagType {
	case typeOps:
		if expiresNode != nil {
			c.fail(RuleOpsHasExpiry, expiresNode.Line, "%s: an ops flag is permanent and must not have expires_at", label)
		}
		return
	case typeRelease:
		if expiresNode == nil {
			c.fail(RuleExpiresAtMissing, entryLine, "%s: a release flag needs expires_at", label)
			return
		}
	default:
		return
	}
	if !isValue(expiresNode) {
		c.fail(RuleExpiresAtInvalid, expiresNode.Line, "%s: expires_at must be a calendar date YYYY-MM-DD", label)
		return
	}
	parsed, err := time.Parse(dateLayout, expiresNode.Value)
	if err != nil {
		c.fail(RuleExpiresAtInvalid, expiresNode.Line, "%s: expires_at %q is not a calendar date YYYY-MM-DD", label, expiresNode.Value)
		return
	}
	e.expiresAt = parsed
	e.hasExpires = true
}
