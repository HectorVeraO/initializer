// Package markers removes module-keyed blocks from core-owned text files.
package markers

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
)

const (
	// CodeMarkerInvalid is a candidate marker line that is not an exact marker, or a block structure error.
	CodeMarkerInvalid = "MARKER_INVALID"
	// CodeMarkerModuleUnknown is a marker naming a module that has no manifest.
	CodeMarkerModuleUnknown = "MARKER_MODULE_UNKNOWN"
	// CodeMarkerInModuleFile is a marker in a file that no core module owns.
	CodeMarkerInModuleFile = "MARKER_IN_MODULE_FILE"
)

// coreModule is the one id a marker may never name.
const coreModule = "core"

// maxIDLength is the longest module id a marker may name.
const maxIDLength = 31

var (
	idPattern        = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	candidatePattern = regexp.MustCompile(`^(#|//|<!--)[ \t]*initializer:(module|end)\b`)
)

// Block is one marker-delimited region of a file.
type Block struct {
	// Module is the module id named by the opening marker.
	Module string
	// Start and End are 0-based line indexes of the opening and closing marker.
	Start, End int
}

// Syntax is the comment form of one file kind.
type Syntax struct {
	// Open and Close return the exact marker lines for a module id.
	Open, Close func(id string) string

	openPrefix  string
	closePrefix string
	suffix      string
}

func newSyntax(prefix, suffix string) Syntax {
	return Syntax{
		Open:        func(id string) string { return prefix + "initializer:module " + id + suffix },
		Close:       func(id string) string { return prefix + "initializer:end " + id + suffix },
		openPrefix:  prefix + "initializer:module ",
		closePrefix: prefix + "initializer:end ",
		suffix:      suffix,
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
	case ".yml", ".yaml", ".env", ".template", ".conf", ".sh":
		return newSyntax("# ", ""), true
	case ".md":
		return newSyntax("<!-- ", " -->"), true
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
	id     string
}

// parseMarker returns the marker a candidate line spells, or false when it is not exact.
func (s Syntax) parseMarker(text string) (marker, bool) {
	for _, form := range []struct {
		prefix string
		isOpen bool
	}{{s.openPrefix, true}, {s.closePrefix, false}} {
		if !strings.HasPrefix(text, form.prefix) || !strings.HasSuffix(text, s.suffix) {
			continue
		}
		if len(text) < len(form.prefix)+len(s.suffix) {
			continue
		}
		id := text[len(form.prefix) : len(text)-len(s.suffix)]
		if len(id) > maxIDLength || !idPattern.MatchString(id) {
			continue
		}
		return marker{isOpen: form.isOpen, id: id}, true
	}
	return marker{}, false
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
			form := syntax.Open("<id>")
			if strings.HasPrefix(strings.TrimLeft(text, "#/<!- \t"), "initializer:end") {
				form = syntax.Close("<id>")
			}
			report(i+1, fmt.Sprintf("marker must be exactly %q", form))
			continue
		}
		if found.id == coreModule {
			report(i+1, "a marker may not name core")
		}
		switch {
		case found.isOpen && open != nil:
			report(i+1, fmt.Sprintf("markers do not nest: %s opens inside %s", found.id, open.Module))
		case found.isOpen:
			open = &Block{Module: found.id, Start: i}
		case open == nil:
			report(i+1, fmt.Sprintf("closing marker for %s has no opening marker", found.id))
		case open.Module != found.id:
			report(i+1, fmt.Sprintf("closing marker names %s, the open block is %s", found.id, open.Module))
		default:
			open.End = i
			blocks = append(blocks, *open)
			open = nil
		}
	}
	if open != nil {
		report(open.Start+1, fmt.Sprintf("opening marker for %s has no closing marker", open.Module))
	}
	return blocks, issues
}

// Drop returns src without the blocks whose module installed reports false, applying the
// blank-line rule. It never changes a byte outside a dropped block or its preceding blank
// line. It returns an error when Parse reports an issue.
func Drop(p string, src []byte, installed func(id string) bool) ([]byte, error) {
	if installed == nil {
		return nil, errors.New("installed callback is nil")
	}
	blocks, issues := Parse(p, src)
	if len(issues) > 0 {
		messages := make([]string, len(issues))
		for i, found := range issues {
			messages[i] = found.Code + ": " + found.Message
		}
		return nil, fmt.Errorf("%s: %s", p, strings.Join(messages, "; "))
	}
	lines := splitLines(src)
	removed := make([]bool, len(lines))
	anyRemoved := false
	for _, block := range blocks {
		if installed(block.Module) {
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
