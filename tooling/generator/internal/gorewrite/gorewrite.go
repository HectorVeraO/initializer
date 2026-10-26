// Package gorewrite swaps a Go module path prefix in import specs.
//
// A generated project copies Gatbase files whose imports are fully qualified
// with the source module path S and builds them under the target module path
// T. The swap is a byte-exact replacement of import path literals, so the
// output of a clean input stays gofmt-clean.
//
// Caller contract: the caller (generate) rewrites a file only when
// ShouldRewrite reports true, so Go files under a testdata directory are
// copied verbatim. A parse error from RewriteImports is returned unchanged;
// the caller decides how to report it.
package gorewrite

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// GoModPath is the catalogued path of the Go module file that holds S once.
const GoModPath = "go-srv/go.mod"

const (
	// RuleModulePathLeak is rule R1: S occurs outside the rewritten import specs.
	RuleModulePathLeak = "R1"
	// RuleImportGroup is rule R2: S imports must sit alone in a group, without trailing comments.
	RuleImportGroup = "R2"
)

var (
	// ErrModulePathLeak reports an R1 violation.
	ErrModulePathLeak = errors.New("module path leak")
	// ErrImportGroup reports an R2 violation.
	ErrImportGroup = errors.New("import group violation")

	segmentPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)
)

// Finding is one R1 or R2 violation in one file.
type Finding struct {
	Rule   string // "R1" (module path leak) or "R2" (import group)
	Line   int    // 1-based
	Detail string
}

// ShouldRewrite reports whether a copied file takes part in the rewrite: a
// path with a .go suffix and no testdata segment.
func ShouldRewrite(filePath string) bool {
	normalized := strings.ReplaceAll(filePath, "\\", "/")
	if !strings.HasSuffix(normalized, ".go") {
		return false
	}
	for _, segment := range strings.Split(normalized, "/") {
		if segment == "testdata" {
			return false
		}
	}
	return true
}

// RewriteImports returns src with every import path equal to from, or prefixed by from+"/",
// re-prefixed with to. Bytes outside those string literals are unchanged.
// It returns the number of specs rewritten. Input that breaks R1 or R2 is
// refused with ErrModulePathLeak or ErrImportGroup.
func RewriteImports(filename string, src []byte, from, to string) ([]byte, int, error) {
	if err := checkArguments(from, to); err != nil {
		return nil, 0, err
	}
	scan, err := scanGoSource(filename, src, from)
	if err != nil {
		return nil, 0, err
	}
	if len(scan.findings) > 0 {
		return nil, 0, findingError(filename, scan.findings[0])
	}
	count := len(scan.starts)
	if from == to {
		return src, count, nil
	}
	out := make([]byte, 0, len(src)+count*(len(to)-len(from)+1))
	cursor := 0
	for _, start := range scan.starts {
		out = append(out, src[cursor:start]...)
		out = append(out, to...)
		cursor = start + len(from)
	}
	out = append(out, src[cursor:]...)
	return out, count, nil
}

// RewriteModuleLine replaces line 1 of a go.mod file.
// It fails if that line is not exactly "module " + from, if from occurs
// anywhere else, or if the file has CRLF line ends.
func RewriteModuleLine(gomod []byte, from, to string) ([]byte, error) {
	if err := checkArguments(from, to); err != nil {
		return nil, err
	}
	if bytes.IndexByte(gomod, '\r') >= 0 {
		return nil, errors.New("go.mod must use LF line ends")
	}
	want := "module " + from
	firstLine := gomod
	if end := bytes.IndexByte(gomod, '\n'); end >= 0 {
		firstLine = gomod[:end]
	}
	if string(firstLine) != want {
		return nil, fmt.Errorf("go.mod line 1 must be exactly %q", want)
	}
	rest := gomod[len(firstLine):]
	if at := bytes.Index(rest, []byte(from)); at >= 0 {
		line := lineOf(gomod, len(firstLine)+at)
		return nil, fmt.Errorf("go.mod:%d: %w: %s occurs again", line, ErrModulePathLeak, from)
	}
	out := make([]byte, 0, len(gomod)+len(to))
	out = append(out, "module "...)
	out = append(out, to...)
	out = append(out, rest...)
	return out, nil
}

// CheckFile applies R1 and R2 to one catalogued file: a .go file outside testdata/ gets both
// rules, go-srv/go.mod must hold module exactly once, any other file must not hold it at all.
// It is the only implementation of R1/R2: RewriteImports and the catalog build both call it.
func CheckFile(filePath string, src []byte, module string) []Finding {
	normalized := strings.ReplaceAll(filePath, "\\", "/")
	switch {
	case module == "":
		return []Finding{{Rule: RuleModulePathLeak, Line: 1, Detail: "module path is empty"}}
	case normalized == GoModPath:
		return checkGoMod(src, module)
	case ShouldRewrite(normalized):
		scan, err := scanGoSource(normalized, src, module)
		if err != nil {
			return []Finding{{Rule: RuleModulePathLeak, Line: 1, Detail: "cannot parse Go file: " + err.Error()}}
		}
		return scan.findings
	default:
		return checkNoOccurrence(src, module)
	}
}

func checkGoMod(src []byte, module string) []Finding {
	firstLine := src
	if end := bytes.IndexByte(src, '\n'); end >= 0 {
		firstLine = src[:end]
	}
	if string(firstLine) != "module "+module {
		return []Finding{{Rule: RuleModulePathLeak, Line: 1, Detail: "go.mod line 1 must be exactly \"module " + module + "\""}}
	}
	offsets := occurrences(src, module)
	if len(offsets) == 1 {
		return nil
	}
	return []Finding{{
		Rule:   RuleModulePathLeak,
		Line:   lineOf(src, offsets[1]),
		Detail: fmt.Sprintf("module path %s occurs %d times, want exactly 1", module, len(offsets)),
	}}
}

func checkNoOccurrence(src []byte, module string) []Finding {
	var findings []Finding
	for _, offset := range occurrences(src, module) {
		findings = append(findings, Finding{
			Rule:   RuleModulePathLeak,
			Line:   lineOf(src, offset),
			Detail: "module path " + module + " occurs in a file that must not name it",
		})
	}
	return findings
}

type goScan struct {
	findings []Finding
	// starts holds the offset of the module path prefix inside each
	// rewritable import literal, in source order.
	starts []int
}

// scanGoSource parses the imports of a Go file and applies R1 and R2.
func scanGoSource(filename string, src []byte, module string) (goScan, error) {
	var scan goScan
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return scan, err
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}
		scan.checkDecl(fset, gen, module)
	}
	prefixOffsets := make(map[int]bool, len(scan.starts))
	for _, start := range scan.starts {
		prefixOffsets[start] = true
	}
	for _, offset := range occurrences(src, module) {
		if prefixOffsets[offset] {
			continue
		}
		scan.findings = append(scan.findings, Finding{
			Rule:   RuleModulePathLeak,
			Line:   lineOf(src, offset),
			Detail: "module path " + module + " occurs outside a rewritable import path",
		})
	}
	sort.SliceStable(scan.findings, func(i, j int) bool {
		return scan.findings[i].Line < scan.findings[j].Line
	})
	return scan, nil
}

func (s *goScan) checkDecl(fset *token.FileSet, gen *ast.GenDecl, module string) {
	// A group is a run of specs without a blank line between them, the unit
	// gofmt sorts and aligns.
	var matching, others, blockLine, previousEnd int
	flush := func() {
		if matching > 0 && others > 0 {
			s.findings = append(s.findings, Finding{
				Rule:   RuleImportGroup,
				Line:   blockLine,
				Detail: "import group mixes " + module + " imports with other imports",
			})
		}
		matching, others = 0, 0
	}
	for _, spec := range gen.Specs {
		imp, ok := spec.(*ast.ImportSpec)
		if !ok {
			continue
		}
		startLine := fset.Position(imp.Pos()).Line
		if imp.Doc != nil {
			startLine = fset.Position(imp.Doc.Pos()).Line
		}
		if previousEnd == 0 || startLine > previousEnd+1 {
			flush()
			blockLine = fset.Position(imp.Pos()).Line
		}
		previousEnd = fset.Position(imp.End()).Line
		position := fset.Position(imp.Path.Pos())
		value, err := strconv.Unquote(imp.Path.Value)
		isModule := err == nil && (value == module || strings.HasPrefix(value, module+"/"))
		if !isModule {
			others++
			continue
		}
		matching++
		literal := imp.Path.Value
		if !strings.HasPrefix(literal[1:], module) {
			s.findings = append(s.findings, Finding{
				Rule:   RuleModulePathLeak,
				Line:   position.Line,
				Detail: "import path of " + module + " is written with escapes",
			})
			continue
		}
		s.starts = append(s.starts, position.Offset+1)
		if imp.Comment != nil {
			s.findings = append(s.findings, Finding{
				Rule:   RuleImportGroup,
				Line:   position.Line,
				Detail: "import of " + module + " carries a trailing comment",
			})
		}
	}
	flush()
}

func checkArguments(from, to string) error {
	if from == "" {
		return errors.New("gorewrite: from module path is empty")
	}
	if !validModulePath(to) {
		return fmt.Errorf("gorewrite: target module path %q is not valid", to)
	}
	return nil
}

// validModulePath applies the goModule grammar of a selection: segments of
// [A-Za-z0-9._~-], a lowercase first segment containing a dot, no empty, "."
// or ".." segment, no leading or trailing slash.
func validModulePath(module string) bool {
	if module == "" || module != path.Clean(module) {
		return false
	}
	segments := strings.Split(module, "/")
	first := segments[0]
	if !strings.Contains(first, ".") || first != strings.ToLower(first) {
		return false
	}
	for _, segment := range segments {
		if segment == "." || segment == ".." || !segmentPattern.MatchString(segment) {
			return false
		}
	}
	return true
}

// occurrences returns the offsets of non-overlapping occurrences of needle.
func occurrences(src []byte, needle string) []int {
	var offsets []int
	if needle == "" {
		return offsets
	}
	base := 0
	for {
		at := bytes.Index(src[base:], []byte(needle))
		if at < 0 {
			return offsets
		}
		offsets = append(offsets, base+at)
		base += at + len(needle)
	}
}

func lineOf(src []byte, offset int) int {
	return 1 + bytes.Count(src[:offset], []byte("\n"))
}

func findingError(filename string, finding Finding) error {
	sentinel := ErrModulePathLeak
	if finding.Rule == RuleImportGroup {
		sentinel = ErrImportGroup
	}
	return fmt.Errorf("%s:%d: %w: %s", filename, finding.Line, sentinel, finding.Detail)
}
