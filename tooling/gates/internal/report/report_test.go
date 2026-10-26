package report_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/gates/internal/report"
)

var sample = []report.Violation{
	{Rule: "FLAG_EXPIRED", Ref: "RA.2", Severity: report.SeverityError, Path: "flags.yaml", Line: 7, Message: "release flag \"new_settings_ui\" expired"},
	{Rule: "FLAG_EXPIRES_SOON", Ref: "RA.2", Severity: report.SeverityWarning, Path: "flags.yaml", Line: 12, Message: "expires soon"},
}

func TestPlainPrinterLineFormat(t *testing.T) {
	var out bytes.Buffer
	err := report.NewPlainPrinter(&out).Print(sample)
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	want := "flags.yaml:7: FLAG_EXPIRED [RA.2]: release flag \"new_settings_ui\" expired\n" +
		"flags.yaml:12: FLAG_EXPIRES_SOON [RA.2]: expires soon\n"
	if out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
}

func TestPlainPrinterKeepsOneLinePerViolation(t *testing.T) {
	var out bytes.Buffer
	v := report.Violation{Rule: "X", Ref: "RA.2", Path: "flags.yaml", Line: 1, Message: "a\nb"}
	err := report.NewPlainPrinter(&out).Print([]report.Violation{v})
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	if got := strings.Count(out.String(), "\n"); got != 1 {
		t.Fatalf("got %d lines in %q, want 1", got, out.String())
	}
}

func TestGitHubPrinterAddsWorkflowCommands(t *testing.T) {
	var out bytes.Buffer
	err := report.NewGitHubPrinter(&out).Print(sample)
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	want := []string{
		"flags.yaml:7: FLAG_EXPIRED [RA.2]: release flag \"new_settings_ui\" expired",
		"::error file=flags.yaml,line=7::FLAG_EXPIRED: release flag \"new_settings_ui\" expired",
		"flags.yaml:12: FLAG_EXPIRES_SOON [RA.2]: expires soon",
		"::warning file=flags.yaml,line=12::FLAG_EXPIRES_SOON: expires soon",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestGitHubPrinterEscapesCommandData(t *testing.T) {
	var out bytes.Buffer
	v := report.Violation{Rule: "X", Ref: "RA.2", Path: "flags.yaml", Line: 0, Message: "100%\n::error::injected"}
	err := report.NewGitHubPrinter(&out).Print([]report.Violation{v})
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	want := "::error file=flags.yaml::X: 100%25%0A::error::injected"
	var commands []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.HasPrefix(line, "::") {
			commands = append(commands, line)
		}
	}
	if len(commands) != 1 || commands[0] != want {
		t.Fatalf("workflow commands = %q, want exactly [%q]", commands, want)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("disk full")
}

func TestPrintersReportWriteFailure(t *testing.T) {
	printers := map[string]report.Printer{
		"plain":  report.NewPlainPrinter(failingWriter{}),
		"github": report.NewGitHubPrinter(failingWriter{}),
	}
	for name, p := range printers {
		err := p.Print(sample)
		if err == nil {
			t.Errorf("%s printer swallowed a write error", name)
		}
	}
}
