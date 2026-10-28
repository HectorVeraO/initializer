package migrationlint

import (
	"bytes"
	"strings"
)

const contractStepMarker = "initializer:contract-step"

// Statement is one SQL statement with comments removed, single-quoted strings
// reduced to ” and dollar-quoted bodies reduced to $$.
type Statement struct {
	// Line is the 1-based line of the statement's first token.
	Line int
	// Text is the stripped statement with runs of whitespace collapsed to one
	// space and no trailing semicolon. It is never empty.
	Text string
	// ContractStep is true when the line directly above the statement is
	// "-- initializer:contract-step <reason>" with a non-empty reason.
	ContractStep bool
}

// Scan splits SQL into statements. It is a scanner, not a parser: it strips
// "--" and nested "/* */" comments and single-quoted strings (including E”
// strings), keeps double-quoted identifiers verbatim, and splits on ";" outside
// strings, quoted identifiers and dollar-quoted bodies. Unterminated constructs
// run to the end of the input. It never panics.
func Scan(src []byte) []Statement {
	s := &scanner{src: src, line: 1, markers: map[int]bool{}}
	s.run()
	return s.statements
}

type scanner struct {
	src  []byte
	pos  int
	line int

	// lineHasCode is true once the current line holds anything but whitespace.
	lineHasCode bool
	markers     map[int]bool
	// prevEnd is the line where the previous statement ended.
	prevEnd int

	text         bytes.Buffer
	textLine     int
	textContract bool
	statements   []Statement
}

func isIdentByte(b byte) bool {
	isLetter := (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
	isDigit := b >= '0' && b <= '9'
	return isLetter || isDigit || b == '_' || b >= 0x80
}

func (s *scanner) peek(offset int) byte {
	index := s.pos + offset
	if index >= len(s.src) {
		return 0
	}
	return s.src[index]
}

// begin records where the current statement starts.
func (s *scanner) begin() {
	if s.textLine != 0 {
		return
	}
	s.textLine = s.line
	s.textContract = s.markers[s.line-1] && s.line-1 > s.prevEnd
}

func (s *scanner) flush() {
	text := strings.Join(strings.Fields(s.text.String()), " ")
	if text != "" {
		s.statements = append(s.statements, Statement{Line: s.textLine, Text: text, ContractStep: s.textContract})
	}
	s.prevEnd = s.line
	s.text.Reset()
	s.textLine = 0
	s.textContract = false
}

func (s *scanner) run() {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == '\n':
			s.line++
			s.lineHasCode = false
			s.text.WriteByte(' ')
			s.pos++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			s.text.WriteByte(' ')
			s.pos++
		case c == '-' && s.peek(1) == '-':
			s.lineComment()
		case c == '/' && s.peek(1) == '*':
			s.blockComment()
		case c == ';':
			s.flush()
			s.lineHasCode = true
			s.pos++
		case c == '\'':
			s.quotedString()
		case c == '"':
			s.quotedIdentifier()
		case c == '$' && s.dollarQuote():
		default:
			s.begin()
			s.lineHasCode = true
			s.text.WriteByte(c)
			s.pos++
		}
	}
	s.flush()
}

func (s *scanner) lineComment() {
	end := bytes.IndexByte(s.src[s.pos:], '\n')
	if end < 0 {
		end = len(s.src) - s.pos
	}
	body := string(s.src[s.pos+2 : s.pos+end])
	isMarkerLine := !s.lineHasCode
	if isMarkerLine && isContractStepMarker(body) {
		s.markers[s.line] = true
	}
	s.text.WriteByte(' ')
	s.pos += end
}

func isContractStepMarker(body string) bool {
	body = strings.TrimSpace(body)
	rest, found := strings.CutPrefix(body, contractStepMarker)
	if !found || rest == "" {
		return false
	}
	startsWithSpace := rest[0] == ' ' || rest[0] == '\t'
	return startsWithSpace && strings.TrimSpace(rest) != ""
}

func (s *scanner) blockComment() {
	depth := 0
	for s.pos < len(s.src) {
		switch {
		case s.src[s.pos] == '/' && s.peek(1) == '*':
			depth++
			s.pos += 2
		case s.src[s.pos] == '*' && s.peek(1) == '/':
			depth--
			s.pos += 2
		default:
			if s.src[s.pos] == '\n' {
				s.line++
			}
			s.pos++
		}
		if depth == 0 {
			break
		}
	}
	s.lineHasCode = true
	s.text.WriteByte(' ')
}

func (s *scanner) quotedString() {
	isEscapeString := s.pos > 0 && (s.src[s.pos-1] == 'E' || s.src[s.pos-1] == 'e') &&
		(s.pos < 2 || !isIdentByte(s.src[s.pos-2]))
	s.begin()
	s.lineHasCode = true
	s.text.WriteString("''")
	s.pos++
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		if c == '\n' {
			s.line++
		}
		if isEscapeString && c == '\\' && s.pos+1 < len(s.src) {
			if s.src[s.pos+1] == '\n' {
				s.line++
			}
			s.pos += 2
			continue
		}
		s.pos++
		if c == '\'' {
			if s.peek(0) == '\'' {
				s.pos++
				continue
			}
			return
		}
	}
}

func (s *scanner) quotedIdentifier() {
	s.begin()
	s.lineHasCode = true
	s.text.WriteByte('"')
	s.pos++
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		if c == '\n' {
			s.line++
		}
		s.text.WriteByte(c)
		s.pos++
		if c == '"' {
			if s.peek(0) == '"' {
				s.text.WriteByte('"')
				s.pos++
				continue
			}
			return
		}
	}
}

// dollarQuote consumes a $tag$ ... $tag$ body and reports whether the "$" at
// the current position opened one.
func (s *scanner) dollarQuote() bool {
	if s.pos > 0 && isIdentByte(s.src[s.pos-1]) {
		return false
	}
	end := s.pos + 1
	if end < len(s.src) && s.src[end] != '$' {
		first := s.src[end]
		isTagStart := (first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || first == '_' || first >= 0x80
		if !isTagStart {
			return false
		}
		for end < len(s.src) && isIdentByte(s.src[end]) {
			end++
		}
	}
	if end >= len(s.src) || s.src[end] != '$' {
		return false
	}
	tag := s.src[s.pos : end+1]
	s.begin()
	s.lineHasCode = true
	s.text.WriteString("$$")
	bodyStart := end + 1
	closing := bytes.Index(s.src[bodyStart:], tag)
	stop := len(s.src)
	if closing >= 0 {
		stop = bodyStart + closing + len(tag)
	}
	s.line += bytes.Count(s.src[s.pos:stop], []byte{'\n'})
	s.pos = stop
	return true
}
