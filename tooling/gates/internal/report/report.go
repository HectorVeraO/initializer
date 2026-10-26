// Package report holds the violation model shared by every gates subcommand.
package report

import (
	"fmt"
	"io"
	"strings"
)

// Severity of a finding. Only SeverityError affects the exit code.
type Severity int

const (
	// SeverityError fails the gate.
	SeverityError Severity = iota
	// SeverityWarning is printed but does not fail the gate.
	SeverityWarning
)

// Violation is one broken rule at one place.
type Violation struct {
	// Rule is a stable SCREAMING_SNAKE code, e.g. "FLAG_EXPIRED".
	Rule string
	// Ref is the binding rule id cited in the message: "RA.2", "RA.3", "RA.4", "L23".
	Ref      string
	Severity Severity
	// Path is repo-relative with forward slashes.
	Path string
	// Line is 1-based; 0 when the finding is about the whole file.
	Line    int
	Message string
}

// Printer writes violations.
type Printer interface {
	Print(vs []Violation) error
}

type plainPrinter struct {
	w io.Writer
}

type githubPrinter struct {
	plain plainPrinter
	w     io.Writer
}

// NewPlainPrinter writes "<path>:<line>: <RULE> [<Ref>]: <message>" lines to w.
func NewPlainPrinter(w io.Writer) Printer {
	return plainPrinter{w: w}
}

// NewGitHubPrinter writes the plain line plus a workflow command
// "::error file=<path>,line=<line>::<RULE>: <message>" ("::warning" for warnings).
// The line property is omitted when the violation has no line.
func NewGitHubPrinter(w io.Writer) Printer {
	return githubPrinter{plain: plainPrinter{w: w}, w: w}
}

func singleLine(s string) string {
	replacer := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ")
	return replacer.Replace(s)
}

func (p plainPrinter) Print(vs []Violation) error {
	for _, v := range vs {
		_, err := fmt.Fprintf(p.w, "%s:%d: %s [%s]: %s\n", v.Path, v.Line, v.Rule, v.Ref, singleLine(v.Message))
		if err != nil {
			return fmt.Errorf("write violation: %w", err)
		}
	}
	return nil
}

// escapeData escapes a workflow command message.
func escapeData(s string) string {
	replacer := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	return replacer.Replace(s)
}

// escapeProperty escapes a workflow command property value.
func escapeProperty(s string) string {
	replacer := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
	return replacer.Replace(s)
}

func (p githubPrinter) Print(vs []Violation) error {
	for _, v := range vs {
		err := p.plain.Print([]Violation{v})
		if err != nil {
			return err
		}
		command := "error"
		if v.Severity == SeverityWarning {
			command = "warning"
		}
		location := "file=" + escapeProperty(v.Path)
		if v.Line > 0 {
			location += fmt.Sprintf(",line=%d", v.Line)
		}
		_, err = fmt.Fprintf(p.w, "::%s %s::%s\n", command, location, escapeData(v.Rule+": "+v.Message))
		if err != nil {
			return fmt.Errorf("write workflow command: %w", err)
		}
	}
	return nil
}
