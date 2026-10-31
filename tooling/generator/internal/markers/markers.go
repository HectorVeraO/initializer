// Package markers removes module-keyed and stack-keyed blocks from core-owned text files.
package markers

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
)

const (
	// CodeMarkerInvalid is a candidate marker line that is not an exact marker, or a block structure error.
	CodeMarkerInvalid = "MARKER_INVALID"
	// CodeMarkerModuleUnknown is a marker naming a module that has no manifest.
	CodeMarkerModuleUnknown = "MARKER_MODULE_UNKNOWN"
	// CodeMarkerStackUnknown is a marker naming a stack that is not in the stack table.
	CodeMarkerStackUnknown = "MARKER_STACK_UNKNOWN"
	// CodeMarkerInModuleFile is a marker in a file that no core module owns.
	CodeMarkerInModuleFile = "MARKER_IN_MODULE_FILE"
	// CodeMarkerRunInvalid is a module block outside every run whose end line
	// is directly followed by a stack block.
	CodeMarkerRunInvalid = "MARKER_RUN_INVALID"
)

// coreModule is the one id a marker may never name.
const coreModule = "core"

// MultiID is the pseudo module a module marker may name: it is installed in
// every context of a project with two or more backend components, and in no
// other. A marker lists several ids joined by IDSeparator and is kept when any
// of them is installed.
const MultiID = "multi"

// IDSeparator joins the module ids of one marker ("identity|multi").
const IDSeparator = "|"

// maxIDLength is the longest module id a marker may name.
const maxIDLength = 31

// maxIDsLength is the longest id text of one marker.
const maxIDsLength = 63

var (
	idPattern        = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	candidatePattern = regexp.MustCompile(`^(#|//|<!--)[ \t]*initializer:(module|stack|end)\b`)
)

// Kind says what a marker block names.
type Kind string

const (
	// KindModule is an initializer:module block, kept when the module is installed.
	KindModule Kind = "module"
	// KindStack is an initializer:stack block, kept when the stack is selected.
	KindStack Kind = "stack"
)

// Block is one marker-delimited region of a file.
type Block struct {
	// Kind is the kind named by the opening marker.
	Kind Kind
	// ID is the module id or the stack id named by the opening marker.
	ID string
	// Start and End are 0-based line indexes of the opening and closing marker.
	Start, End int
}

// Syntax is the comment form of one file kind.
type Syntax struct {
	// Open returns the exact opening marker line for kind and id.
	Open func(kind Kind, id string) string
	// Close returns the exact closing marker line for id.
	Close func(id string) string

	prefix string
	suffix string
}

func newSyntax(prefix, suffix string) Syntax {
	return Syntax{
		Open: func(kind Kind, id string) string {
			return prefix + "initializer:" + string(kind) + " " + id + suffix
		},
		Close:  func(id string) string { return prefix + "initializer:end " + id + suffix },
		prefix: prefix,
		suffix: suffix,
	}
}

// SyntaxFor returns the comment form for a repo-relative slash path, or false when the
// path's kind allows no markers.
func SyntaxFor(p string) (Syntax, bool) {
	base := path.Base(p)
	if base == "proxy.conf.json" {
		return newSyntax("// ", ""), true
	}
	switch path.Ext(base) {
	case ".yml", ".yaml", ".env", ".template", ".conf", ".sh", ".toml":
		return newSyntax("# ", ""), true
	case ".md":
		return newSyntax("<!-- ", " -->"), true
	case ".mod":
		return newSyntax("// ", ""), true
	}
	return Syntax{}, false
}

// splitLines splits src into lines that keep their terminator.
func splitLines(src []byte) [][]byte {
	if len(src) == 0 {
		return nil
	}
	return bytes.SplitAfter(src, []byte("\n"))
}

// lineText returns the trimmed text of one line, without its terminator.
func lineText(line []byte) string {
	text := strings.TrimSuffix(string(line), "\n")
	return strings.Trim(text, " \t")
}

func isCandidate(text string) bool {
	return candidatePattern.MatchString(text)
}

// HasCandidate reports whether src holds any candidate marker line.
func HasCandidate(src []byte) bool {
	for _, line := range splitLines(src) {
		if isCandidate(lineText(line)) {
			return true
		}
	}
	return false
}

// CandidateLine returns the 1-based number of the first candidate marker line of src, or 0.
func CandidateLine(src []byte) int {
	for i, line := range splitLines(src) {
		if isCandidate(lineText(line)) {
			return i + 1
		}
	}
	return 0
}

// marker is one parsed candidate line.
type marker struct {
	isOpen bool
	kind   Kind
	id     string
}

// parseMarker returns the marker a candidate line spells, or false when it is not exact.
func (s Syntax) parseMarker(text string) (marker, bool) {
	for _, form := range []struct {
		word   string
		isOpen bool
		kind   Kind
	}{
		{"initializer:module ", true, KindModule},
		{"initializer:stack ", true, KindStack},
		{"initializer:end ", false, ""},
	} {
		formPrefix := s.prefix + form.word
		if !strings.HasPrefix(text, formPrefix) || !strings.HasSuffix(text, s.suffix) {
			continue
		}
		if len(text) < len(formPrefix)+len(s.suffix) {
			continue
		}
		id := text[len(formPrefix) : len(text)-len(s.suffix)]
		if !validIDs(id, form.kind) {
			continue
		}
		return marker{isOpen: form.isOpen, kind: form.kind, id: id}, true
	}
	return marker{}, false
}

// validIDs reports whether id is the id text of a marker: one id, or for a
// module marker (or a closing marker, whose kind is unknown) ids joined by
// IDSeparator.
func validIDs(id string, kind Kind) bool {
	if len(id) > maxIDsLength {
		return false
	}
	parts := strings.Split(id, IDSeparator)
	if kind == KindStack && len(parts) > 1 {
		return false
	}
	for _, part := range parts {
		if len(part) > maxIDLength || !idPattern.MatchString(part) {
			return false
		}
	}
	return true
}

// IDs returns the ids the block names: one, or several for a module block that
// lists alternatives.
func (b Block) IDs() []string {
	return strings.Split(b.ID, IDSeparator)
}

// Ref returns the canonical text of the block in a name rule: "module:<ids>" or
// "stack:<id>".
func (b Block) Ref() string {
	return string(b.Kind) + ":" + b.ID
}

// SplitRef splits a block text of a name rule into its kind and ids.
func SplitRef(ref string) (kind string, ids []string) {
	kind, id, _ := strings.Cut(ref, ":")
	return kind, strings.Split(id, IDSeparator)
}

// Parse returns the blocks of src in file order, or every issue found (MARKER_INVALID
// only; each issue's Path is issue.Path(path, "") and its message names the 1-based line).
func Parse(p string, src []byte) ([]Block, []issue.Issue) {
	syntax, ok := SyntaxFor(p)
	var issues []issue.Issue
	report := func(line int, message string) {
		issues = append(issues, issue.Issue{
			Code:    CodeMarkerInvalid,
			Path:    issue.Path(p, ""),
			Message: fmt.Sprintf("%s (line %d)", message, line),
		})
	}
	var blocks []Block
	var open *Block
	for i, raw := range splitLines(src) {
		text := lineText(raw)
		if !isCandidate(text) {
			continue
		}
		if !ok {
			report(i+1, "markers are not allowed in this file kind")
			continue
		}
		found, exact := syntax.parseMarker(text)
		if !exact {
			form := syntax.Open(KindModule, "<id>")
			word := strings.TrimLeft(text, "#/<!- \t")
			switch {
			case strings.HasPrefix(word, "initializer:end"):
				form = syntax.Close("<id>")
			case strings.HasPrefix(word, "initializer:stack"):
				form = syntax.Open(KindStack, "<id>")
			}
			report(i+1, fmt.Sprintf("marker must be exactly %q", form))
			continue
		}
		namedKind := found.kind
		if !found.isOpen && open != nil {
			namedKind = open.Kind
		}
		if namedKind != KindStack && slices.Contains(strings.Split(found.id, IDSeparator), coreModule) {
			report(i+1, "a marker may not name core")
		}
		switch {
		case found.isOpen && open != nil:
			report(i+1, fmt.Sprintf("markers do not nest: %s %s opens inside %s %s", found.kind, found.id, open.Kind, open.ID))
		case found.isOpen:
			open = &Block{Kind: found.kind, ID: found.id, Start: i}
		case open == nil:
			report(i+1, fmt.Sprintf("closing marker for %s has no opening marker", found.id))
		case open.ID != found.id:
			report(i+1, fmt.Sprintf("closing marker names %s, the open block is %s", found.id, open.ID))
		default:
			open.End = i
			blocks = append(blocks, *open)
			open = nil
		}
	}
	if open != nil {
		report(open.Start+1, fmt.Sprintf("opening marker for %s has no closing marker", open.ID))
	}
	return blocks, issues
}

// Keep holds the two predicates of Drop. Both must be non-nil.
type Keep struct {
	// Module reports whether the module id is installed in the context; for
	// MultiID, whether the project has two or more backend components.
	Module func(id string) bool
	// Stack reports whether the stack id is selected in the context.
	Stack func(id string) bool
}

// Drop returns src without the blocks whose predicate reports false, applying the
// blank-line rule (a dropped block also removes the one blank line directly before
// it). It never changes a byte outside a dropped block or that blank line.
//
// Blocks that directly follow each other (no line between a closing marker and
// the next opening marker) form a sequence. A sequence that starts with a stack
// block is a run of that stack: when the stack is not selected the whole run is
// dropped, so a block of another stack kept inside it (the console service's
// go-srv dependency) never outlives its service. It returns an error when Parse
// reports an issue or a predicate is nil.
func Drop(p string, src []byte, keep Keep) ([]byte, error) {
	if keep.Module == nil || keep.Stack == nil {
		return nil, errors.New("keep predicates must be non-nil")
	}
	blocks, issues := Parse(p, src)
	if len(issues) > 0 {
		return nil, parseError(p, issues)
	}
	lines := splitLines(src)
	removed := make([]bool, len(lines))
	anyRemoved := false
	runStack := ""
	for i, block := range blocks {
		isContinuing := i > 0 && block.Start == blocks[i-1].End+1
		if !isContinuing {
			runStack = ""
			if block.Kind == KindStack {
				runStack = block.ID
			}
		}
		isKept := false
		for _, id := range block.IDs() {
			if block.Kind == KindStack {
				isKept = isKept || keep.Stack(id)
				continue
			}
			isKept = isKept || keep.Module(id)
		}
		isRunDropped := runStack != "" && !keep.Stack(runStack)
		if isKept && !isRunDropped {
			continue
		}
		anyRemoved = true
		for i := block.Start; i <= block.End; i++ {
			removed[i] = true
		}
		hasBlankBefore := block.Start > 0 && lineText(lines[block.Start-1]) == ""
		if hasBlankBefore {
			removed[block.Start-1] = true
		}
	}
	if !anyRemoved {
		return src, nil
	}
	out := make([]byte, 0, len(src))
	for i, line := range lines {
		if !removed[i] {
			out = append(out, line...)
		}
	}
	return out, nil
}

// Run is a maximal sequence of directly adjacent blocks that starts with a
// stack block; Start and End are 0-based line indexes of its first open and
// last end marker; Blank is the count of empty lines directly above Start.
type Run struct {
	// Stack is the stack id the first block names.
	Stack string
	// Start and End are the line indexes of the first open and the last end marker.
	Start, End int
	// Blank is the count of empty lines directly above Start.
	Blank int
	// Blocks are the blocks of the run in file order.
	Blocks []Block
}

// Runs returns the runs of blocks, which Parse returned for src, in file
// order. A block continues the previous one when its open line is the line
// directly after the previous end line; a sequence is a run when its first
// block is a stack block. It does not change Parse's checks.
func Runs(src []byte, blocks []Block) []Run {
	lines := splitLines(src)
	var runs []Run
	var current *Run
	flush := func() {
		if current != nil {
			runs = append(runs, *current)
			current = nil
		}
	}
	for i, block := range blocks {
		isContinuing := i > 0 && block.Start == blocks[i-1].End+1
		if current != nil && isContinuing {
			current.End = block.End
			current.Blocks = append(current.Blocks, block)
			continue
		}
		flush()
		if block.Kind != KindStack {
			continue
		}
		blank := 0
		for at := block.Start - 1; at >= 0 && at < len(lines) && lineText(lines[at]) == ""; at-- {
			blank++
		}
		current = &Run{Stack: block.ID, Start: block.Start, End: block.End, Blank: blank, Blocks: []Block{block}}
	}
	flush()
	return runs
}

// Expand returns src with every run emitted count(run.Stack) times through
// copyRun, and the text outside the runs through outside. Each callback gets
// the raw bytes of its segment and returns the transformed bytes; the caller
// applies its transform order inside the callback. A run with no copy is
// dropped together with the one blank line directly before it (the rule of
// Drop). Copies after the first are each preceded by the blank lines that
// directly precede the run in src. A segment with no bytes is not passed to
// outside. It returns an error when Parse reports an issue, a count is
// negative, or a callback fails.
func Expand(p string, src []byte, count func(stack string) int,
	copyRun func(r Run, index int, seg []byte) ([]byte, error),
	outside func(seg []byte) ([]byte, error)) ([]byte, error) {
	if count == nil || copyRun == nil || outside == nil {
		return nil, errors.New("expand callbacks must be non-nil")
	}
	blocks, issues := Parse(p, src)
	if len(issues) > 0 {
		return nil, parseError(p, issues)
	}
	runs := Runs(src, blocks)
	lines := splitLines(src)
	var out []byte
	next := 0
	emitGap := func(from, to int) error {
		if from >= to {
			return nil
		}
		seg, err := outside(joinLines(lines[from:to]))
		if err != nil {
			return err
		}
		out = append(out, seg...)
		return nil
	}
	for _, run := range runs {
		copies := count(run.Stack)
		if copies < 0 {
			return nil, fmt.Errorf("%s: count for stack %s is negative", p, run.Stack)
		}
		gapEnd := run.Start
		if copies == 0 && run.Blank > 0 {
			gapEnd--
		}
		if err := emitGap(next, gapEnd); err != nil {
			return nil, err
		}
		if copies == 0 {
			next = run.End + 1
			continue
		}
		blank := joinLines(lines[run.Start-run.Blank : run.Start])
		body := joinLines(lines[run.Start : run.End+1])
		for index := 0; index < copies; index++ {
			if index > 0 {
				if len(out) > 0 && out[len(out)-1] != '\n' {
					out = append(out, '\n')
				}
				out = append(out, blank...)
			}
			seg, err := copyRun(run, index, body)
			if err != nil {
				return nil, err
			}
			out = append(out, seg...)
		}
		next = run.End + 1
	}
	if err := emitGap(next, len(lines)); err != nil {
		return nil, err
	}
	return out, nil
}

// joinLines concatenates lines that keep their terminators.
func joinLines(lines [][]byte) []byte {
	return bytes.Join(lines, nil)
}

// parseError joins the issues of Parse into one error for path p.
func parseError(p string, issues []issue.Issue) error {
	messages := make([]string, len(issues))
	for i, found := range issues {
		messages[i] = found.Code + ": " + found.Message
	}
	return fmt.Errorf("%s: %s", p, strings.Join(messages, "; "))
}
