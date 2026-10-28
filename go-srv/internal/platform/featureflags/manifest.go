package featureflags

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/featureflag"
)

// Problem codes reported by the loader; they match the codes of `gates flags`.
const (
	CodeParseError         = "FLAGS_PARSE_ERROR"
	CodeUnknownField       = "FLAGS_UNKNOWN_FIELD"
	CodeKeyInvalid         = "FLAG_KEY_INVALID"
	CodeKeyDuplicate       = "FLAG_KEY_DUPLICATE"
	CodeTypeInvalid        = "FLAG_TYPE_INVALID"
	CodeDefaultMissing     = "FLAG_DEFAULT_MISSING"
	CodeDescriptionMissing = "FLAG_DESCRIPTION_MISSING"
	CodeReleaseDefaultTrue = "FLAG_RELEASE_DEFAULT_TRUE"
	CodeExpiresAtMissing   = "FLAG_EXPIRES_AT_MISSING"
	CodeExpiresAtInvalid   = "FLAG_EXPIRES_AT_INVALID"
	CodeOpsHasExpiry       = "FLAG_OPS_HAS_EXPIRY"
)

const (
	maxKeyLength     = 64
	maxManifestBytes = 1 << 20
	dateLayout       = "2006-01-02"
	topLevelFlags    = "flags"
	yamlNullTag      = "!!null"
	yamlBoolTag      = "!!bool"
)

var (
	keyPattern    = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	yamlErrorLine = regexp.MustCompile(`line (\d+)`)
	entryFields   = map[string]bool{"key": true, "type": true, "default": true, "expires_at": true, "description": true}
)

// Type is the flag lifecycle declared in the manifest.
type Type string

const (
	// TypeRelease is a short-lived flag that guards an unreleased feature.
	TypeRelease Type = "release"
	// TypeOps is a permanent operational switch.
	TypeOps Type = "ops"
)

// Flag is one manifest entry.
type Flag struct {
	Key     featureflag.Key
	Type    Type
	Default bool
	// ExpiresAt is the expiry date at 00:00 UTC; nil for ops flags.
	ExpiresAt   *time.Time
	Description string
	// Line is the line of the entry in the source file.
	Line int
}

// Manifest is the validated, immutable catalog loaded at startup.
type Manifest struct {
	flags []Flag
	index map[featureflag.Key]int
}

// Flags returns a copy of the flags in file order.
func (m Manifest) Flags() []Flag {
	out := make([]Flag, len(m.flags))
	for i, f := range m.flags {
		out[i] = copyFlag(f)
	}
	return out
}

// Lookup returns the flag declared under key.
func (m Manifest) Lookup(key featureflag.Key) (Flag, bool) {
	i, ok := m.index[key]
	if !ok {
		return Flag{}, false
	}
	return copyFlag(m.flags[i]), true
}

func copyFlag(f Flag) Flag {
	if f.ExpiresAt != nil {
		expires := *f.ExpiresAt
		f.ExpiresAt = &expires
	}
	return f
}

// Problem is one manifest violation.
type Problem struct {
	Line int
	// Key is the flag key when known, else "".
	Key     string
	Code    string
	Message string
}

// ManifestError carries every problem found in one parse.
type ManifestError struct {
	Problems []Problem
}

// Error lists every problem on one line.
func (e *ManifestError) Error() string {
	parts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		parts[i] = fmt.Sprintf("line %d: %s: %s", p.Line, p.Code, p.Message)
	}
	return fmt.Sprintf("flags manifest invalid (%d problems): %s", len(e.Problems), strings.Join(parts, "; "))
}

// LoadManifestFile opens path (a relative path resolves against the process
// working directory) and parses it; an open or read error carries the path.
func LoadManifestFile(path string) (Manifest, error) {
	if path == "" {
		return Manifest{}, errors.New("flags manifest: path is empty")
	}
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("flags manifest %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	manifest, err := ParseManifest(file)
	if err != nil {
		return Manifest{}, fmt.Errorf("flags manifest %q: %w", path, err)
	}
	return manifest, nil
}

// ParseManifest reads and validates a flags.yaml document. Every problem is
// reported together in a *ManifestError; it never compares expires_at with the
// current date (that is a CI rule of `gates flags`).
func ParseManifest(r io.Reader) (Manifest, error) {
	if r == nil {
		return Manifest{}, errors.New("flags manifest: nil reader")
	}
	data, err := io.ReadAll(io.LimitReader(r, maxManifestBytes+1))
	if err != nil {
		return Manifest{}, fmt.Errorf("flags manifest: read: %w", err)
	}
	p := &parser{}
	if len(data) > maxManifestBytes {
		p.add(0, "", CodeParseError, "manifest is larger than %d bytes", maxManifestBytes)
	} else {
		p.parse(data)
	}
	if len(p.problems) > 0 {
		sort.SliceStable(p.problems, func(i, j int) bool { return p.problems[i].Line < p.problems[j].Line })
		return Manifest{}, &ManifestError{Problems: p.problems}
	}
	m := Manifest{flags: p.flags, index: make(map[featureflag.Key]int, len(p.flags))}
	for i, f := range p.flags {
		m.index[f.Key] = i
	}
	return m, nil
}

type parser struct {
	problems []Problem
	flags    []Flag
}

func (p *parser) add(line int, key, code, format string, args ...any) {
	p.problems = append(p.problems, Problem{Line: line, Key: key, Code: code, Message: fmt.Sprintf(format, args...)})
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

func (p *parser) parse(data []byte) {
	var doc yaml.Node
	err := yaml.Unmarshal(data, &doc)
	if err != nil {
		line := 0
		match := yamlErrorLine.FindStringSubmatch(err.Error())
		if match != nil {
			line, _ = strconv.Atoi(match[1])
		}
		p.add(line, "", CodeParseError, "not valid YAML: %s", singleLine(err.Error()))
		return
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		p.add(0, "", CodeParseError, "file is empty; expected a top-level mapping with a flags sequence")
		return
	}
	top := deref(doc.Content[0])
	if top.Kind != yaml.MappingNode {
		p.add(top.Line, "", CodeParseError, "top level must be a mapping with a flags sequence")
		return
	}

	var flagsNode *yaml.Node
	for i := 0; i+1 < len(top.Content); i += 2 {
		name := deref(top.Content[i])
		isFlags := name.Kind == yaml.ScalarNode && name.Value == topLevelFlags
		switch {
		case isFlags && flagsNode != nil:
			p.add(name.Line, "", CodeParseError, "top-level field %q is declared twice", topLevelFlags)
		case isFlags:
			flagsNode = deref(top.Content[i+1])
		default:
			p.add(name.Line, "", CodeUnknownField, "unknown top-level field %q; only %q is allowed", name.Value, topLevelFlags)
		}
	}
	if flagsNode == nil || flagsNode.Kind != yaml.SequenceNode {
		line := top.Line
		if flagsNode != nil {
			line = flagsNode.Line
		}
		p.add(line, "", CodeParseError, "top-level field %q must be a sequence (use [] for no flags)", topLevelFlags)
		return
	}

	firstLine := map[string]int{}
	for _, item := range flagsNode.Content {
		node := deref(item)
		if node.Kind != yaml.MappingNode {
			p.add(node.Line, "", CodeParseError, "each flag must be a mapping")
			continue
		}
		flag, ok := p.parseEntry(node)
		if flag.Key != "" {
			first, seen := firstLine[string(flag.Key)]
			if seen {
				p.add(flag.Line, string(flag.Key), CodeKeyDuplicate, "flag %q is already declared on line %d", flag.Key, first)
				continue
			}
			firstLine[string(flag.Key)] = flag.Line
		}
		if ok {
			p.flags = append(p.flags, flag)
		}
	}
}

// parseEntry validates one entry and reports every problem in it. ok is true
// only when the entry is complete and valid.
func (p *parser) parseEntry(node *yaml.Node) (Flag, bool) {
	before := len(p.problems)
	fields := map[string]*yaml.Node{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := deref(node.Content[i])
		value := deref(node.Content[i+1])
		isKnown := name.Kind == yaml.ScalarNode && entryFields[name.Value]
		if !isKnown {
			p.add(name.Line, "", CodeUnknownField, "unknown flag field %q; allowed: key, type, default, expires_at, description", name.Value)
			continue
		}
		_, dup := fields[name.Value]
		if dup {
			p.add(name.Line, "", CodeParseError, "flag field %q is declared twice", name.Value)
			continue
		}
		fields[name.Value] = value
	}

	flag := Flag{Line: node.Line}
	label := "flag"
	keyText := ""

	keyNode := fields["key"]
	switch {
	case !isValue(keyNode):
		p.add(node.Line, "", CodeKeyInvalid, "flag has no key")
	case !keyPattern.MatchString(keyNode.Value) || len(keyNode.Value) > maxKeyLength:
		p.add(keyNode.Line, "", CodeKeyInvalid, "flag key %q must match ^[a-z][a-z0-9_]*$ and be at most %d characters", keyNode.Value, maxKeyLength)
	default:
		keyText = keyNode.Value
		flag.Key = featureflag.Key(keyText)
		flag.Line = keyNode.Line
		label = fmt.Sprintf("flag %q", keyText)
	}

	typeNode := fields["type"]
	isKnownType := isValue(typeNode) && (typeNode.Value == string(TypeRelease) || typeNode.Value == string(TypeOps))
	switch {
	case isKnownType:
		flag.Type = Type(typeNode.Value)
	case typeNode != nil:
		p.add(typeNode.Line, keyText, CodeTypeInvalid, "%s: type must be release or ops", label)
	default:
		p.add(node.Line, keyText, CodeTypeInvalid, "%s: type must be release or ops", label)
	}

	defaultNode := fields["default"]
	hasBoolDefault := defaultNode != nil && defaultNode.Kind == yaml.ScalarNode && defaultNode.Tag == yamlBoolTag
	if hasBoolDefault {
		flag.Default = strings.EqualFold(defaultNode.Value, "true")
	} else {
		line := node.Line
		if defaultNode != nil {
			line = defaultNode.Line
		}
		p.add(line, keyText, CodeDefaultMissing, "%s: default must be a YAML boolean (true or false)", label)
	}
	if hasBoolDefault && flag.Type == TypeRelease && flag.Default {
		p.add(defaultNode.Line, keyText, CodeReleaseDefaultTrue, "%s: a release flag must default to false", label)
	}

	descriptionNode := fields["description"]
	if isValue(descriptionNode) && strings.TrimSpace(descriptionNode.Value) != "" {
		flag.Description = descriptionNode.Value
	} else {
		line := node.Line
		if descriptionNode != nil {
			line = descriptionNode.Line
		}
		p.add(line, keyText, CodeDescriptionMissing, "%s: description is required and must not be blank", label)
	}

	p.checkExpiry(&flag, keyText, fields["expires_at"], node.Line, label)
	return flag, len(p.problems) == before
}

func (p *parser) checkExpiry(flag *Flag, keyText string, expiresNode *yaml.Node, entryLine int, label string) {
	switch flag.Type {
	case TypeOps:
		if expiresNode != nil {
			p.add(expiresNode.Line, keyText, CodeOpsHasExpiry, "%s: an ops flag is permanent and must not have expires_at", label)
		}
		return
	case TypeRelease:
		if expiresNode == nil {
			p.add(entryLine, keyText, CodeExpiresAtMissing, "%s: a release flag needs expires_at", label)
			return
		}
	default:
		return
	}
	if !isValue(expiresNode) {
		p.add(expiresNode.Line, keyText, CodeExpiresAtInvalid, "%s: expires_at must be a calendar date YYYY-MM-DD", label)
		return
	}
	parsed, err := time.Parse(dateLayout, expiresNode.Value)
	if err != nil {
		p.add(expiresNode.Line, keyText, CodeExpiresAtInvalid, "%s: expires_at %q is not a calendar date YYYY-MM-DD", label, expiresNode.Value)
		return
	}
	flag.ExpiresAt = &parsed
}
