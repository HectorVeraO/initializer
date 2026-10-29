package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/emit"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
)

// Result formats of --format.
const (
	formatText = "text"
	formatJSON = "json"
)

// documentSchema is the version of every JSON document; a removed or renamed
// member bumps it, an added member does not.
const documentSchema = 1

// codeFlagDeprecated is the warning code of a hidden alias that was used.
const codeFlagDeprecated = "FLAG_DEPRECATED"

// Exit-3 codes.
const (
	codeInputUnreadable   = "INPUT_UNREADABLE"
	codeCatalogUnreadable = "CATALOG_UNREADABLE"
	codeOutputUnwritable  = "OUTPUT_UNWRITABLE"
	codeVerifyUnreadable  = "VERIFY_UNREADABLE"
	codeInternalError     = "INTERNAL_ERROR"
)

// Warning is a non-fatal finding that travels in the result document.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// failure is the body of an exit-3 document.
type failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// outputOptions are the flags every command but help shares.
type outputOptions struct{ format, log string }

// registerOutputFlags adds --format and --log to set and returns their values.
func registerOutputFlags(set *flagSet) *outputOptions {
	o := &outputOptions{format: formatText}
	set.choice(&o.format, []string{formatText, formatJSON}, flagSpec{Name: "format", Placeholder: "text|json",
		Usage: "Result format", Default: formatText})
	set.choice(&o.log, []string{formatJSON}, flagSpec{Name: "log", Placeholder: "json",
		Usage: "Write structured log events to stderr", Default: "off"})
	return o
}

// discardLogger is the logger of a run without --log json.
func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// isJSON reports whether the run prints JSON documents.
func (c *Context) isJSON() bool {
	return c.Format == formatJSON
}

// warnings returns the warnings the run collected as a non-nil list.
func (c *Context) warnings() []Warning {
	return list(c.Warnings)
}

// lookAhead reads --format and --log from a line that could not be scanned
// against a command (an unknown command, a group without a verb); it takes
// only valid values and ignores everything else.
func lookAhead(args []string) (format string, logJSON bool) {
	format = formatText
	for i, arg := range args {
		if !looksLikeFlag(arg) {
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if name != "format" && name != "log" {
			continue
		}
		if !hasValue && i+1 < len(args) {
			value = args[i+1]
		}
		if name == "format" && value == formatJSON {
			format = formatJSON
		}
		if name == "log" && value == formatJSON {
			logJSON = true
		}
	}
	return format, logJSON
}

// writeDocument writes v as one JSON line.
func writeDocument(w io.Writer, v any) error {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return err
	}
	_, err := w.Write(buffer.Bytes())
	return err
}

// emitDocument writes the one JSON document of a run to w and returns code,
// or exitInternal when the document cannot be written.
func emitDocument(ctx *Context, w io.Writer, doc any, code int) int {
	if err := writeDocument(w, doc); err != nil {
		ctx.Logger.Error("result_unwritable", slog.String("error", err.Error()))
		return exitInternal
	}
	return code
}

// list returns items, or an empty non-nil slice, so JSON encodes [] never null.
func list[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

// plural returns noun, or noun+"s" unless count is 1.
func plural(count int, noun string) string {
	if count == 1 {
		return noun
	}
	return noun + "s"
}

// displayPath renders a path for a person: forward slashes, with "./" in front
// of a relative path that does not already start with a dot.
func displayPath(path string) string {
	slashed := filepath.ToSlash(path)
	isAbsolute := filepath.IsAbs(path) || strings.HasPrefix(slashed, "/")
	if isAbsolute || strings.HasPrefix(slashed, ".") {
		return slashed
	}
	return "./" + slashed
}

// reportProblems writes every command-line problem and the usage_error event;
// it returns the usage exit code.
func reportProblems(ctx *Context, problems []Problem) int {
	ctx.Logger.Error("usage_error", slog.Int("problem_count", len(problems)))
	if ctx.isJSON() {
		doc := struct {
			Schema   int           `json:"schema"`
			OK       bool          `json:"ok"`
			Problems []Problem     `json:"problems"`
			Issues   []issue.Issue `json:"issues"`
		}{documentSchema, false, list(problems), []issue.Issue{}}
		return emitDocument(ctx, ctx.Stderr, doc, exitUsage)
	}
	for _, problem := range problems {
		if problem.Arg == "" {
			fmt.Fprintf(ctx.Stderr, "error: %s\n", problem.Message)
		} else {
			fmt.Fprintf(ctx.Stderr, "error: %s: %s\n", problem.Arg, problem.Message)
		}
		if problem.Suggestion != "" {
			fmt.Fprintf(ctx.Stderr, "  hint: did you mean %q?\n", problem.Suggestion)
		}
	}
	fmt.Fprintf(ctx.Stderr, "%d %s.\n", len(problems), plural(len(problems), "problem"))
	return exitUsage
}

// issueView is what rendering the issues of one run needs besides the issues.
type issueView struct {
	// dir is the DIR argument as typed, for issues at args#/dir.
	dir string
	// moduleIDs are the catalog's module ids, for MODULE_UNKNOWN hints.
	moduleIDs []string
	// selectionFile is the input file the selection came from; "" when flags built it.
	selectionFile string
}

// reportIssues writes the value problems of a run, sorted and hinted, and
// returns the issues exit code.
func reportIssues(ctx *Context, issues []issue.Issue, view issueView) int {
	ctx.Logger.Warn("validation_failed", slog.Int("issue_count", len(issues)))
	sorted := addHints(issues, view.moduleIDs)
	issue.Sort(sorted)
	if ctx.isJSON() {
		doc := struct {
			Schema   int           `json:"schema"`
			OK       bool          `json:"ok"`
			Issues   []issue.Issue `json:"issues"`
			Warnings []Warning     `json:"warnings"`
		}{documentSchema, false, sorted, ctx.warnings()}
		return emitDocument(ctx, ctx.Stdout, doc, exitIssues)
	}
	for _, found := range sorted {
		place := whereOf(found, view)
		if place != "" {
			place += ": "
		}
		fmt.Fprintf(ctx.Stderr, "error: %s%s [%s]\n", place, found.Message, found.Code)
		if found.Hint != "" {
			fmt.Fprintf(ctx.Stderr, "  hint: %s\n", found.Hint)
		}
	}
	fmt.Fprintf(ctx.Stderr, "%d %s; nothing was written.\n", len(sorted), plural(len(sorted), "problem"))
	return exitIssues
}

// flagPointers maps the leading pointer tokens of a flag-origin issue to the
// flag that set the value.
var flagPointers = map[string]string{
	"/project/name":     "--name",
	"/project/goModule": "--go-module",
	"/stacks":           "--stacks",
	"/modules":          "--modules",
}

// flagForPointer returns the flag whose value sits at pointer.
func flagForPointer(pointer string) (string, bool) {
	for prefix, name := range flagPointers {
		if pointer == prefix || strings.HasPrefix(pointer, prefix+"/") {
			return name, true
		}
	}
	return "", false
}

// whereOf renders the where-part of an issue for a person: the flag that set
// the value, "<file>:<line> <pointer>", the folder, or the raw path when no
// rule applies.
func whereOf(found issue.Issue, view issueView) string {
	source, pointer, hasPointer := strings.Cut(found.Path, "#")
	switch {
	case !hasPointer && found.Code == emit.CodeOutputNotEmpty:
		return displayPath(found.Path)
	case !hasPointer:
		return found.Path
	case source == "flags" || (source == "selection" && view.selectionFile == ""):
		if name, ok := flagForPointer(pointer); ok {
			return name
		}
		return found.Path
	case source == "selection":
		source = view.selectionFile
	case source == "args":
		if pointer == "/dir" && view.dir != "" {
			return displayPath(view.dir)
		}
		return found.Path
	}
	place := source
	if found.Line > 0 {
		place += ":" + strconv.Itoa(found.Line)
	}
	return place + " " + pointer
}

// reportFailure writes an exit-3 failure and returns the internal exit code.
func reportFailure(ctx *Context, code, message string) int {
	if ctx.isJSON() {
		doc := struct {
			Schema int     `json:"schema"`
			OK     bool    `json:"ok"`
			Error  failure `json:"error"`
		}{documentSchema, false, failure{Code: code, Message: message}}
		return emitDocument(ctx, ctx.Stderr, doc, exitInternal)
	}
	fmt.Fprintf(ctx.Stderr, "error: %s [%s]\n", message, code)
	return exitInternal
}

// fail logs event with err and reports it as an exit-3 failure with code.
func fail(ctx *Context, event, code string, err error) int {
	ctx.Logger.Error(event, slog.String("error", err.Error()))
	return reportFailure(ctx, code, err.Error())
}

// moduleAdded is one module the requirement closure pulled in.
type moduleAdded struct {
	Module     string   `json:"module"`
	RequiredBy []string `json:"requiredBy"`
	Stack      string   `json:"stack"`
}

// moduleInstalled is one module of the generated project.
type moduleInstalled struct {
	Stack   string `json:"stack"`
	Module  string `json:"module"`
	Version string `json:"version"`
}

// catalogRef names the catalog a run used.
type catalogRef struct {
	SHA256 string `json:"sha256"`
	File   string `json:"file,omitempty"`
}

// generateResult is what a successful generate prints.
type generateResult struct {
	project, path string
	files         int
	catalog       catalogRef
	modules       []moduleInstalled
	added         []moduleAdded
}

// writeGenerate prints the result of generate and returns the exit code.
func writeGenerate(ctx *Context, result generateResult) int {
	if ctx.isJSON() {
		doc := struct {
			Schema   int               `json:"schema"`
			OK       bool              `json:"ok"`
			Project  string            `json:"project"`
			Path     string            `json:"path"`
			Files    int               `json:"files"`
			Catalog  catalogRef        `json:"catalog"`
			Modules  []moduleInstalled `json:"modules"`
			Added    []moduleAdded     `json:"added"`
			Notes    []string          `json:"notes"`
			Warnings []Warning         `json:"warnings"`
		}{documentSchema, true, result.project, filepath.ToSlash(result.path), result.files, result.catalog,
			list(result.modules), list(result.added), list(ctx.Notes), ctx.warnings()}
		return emitDocument(ctx, ctx.Stdout, doc, exitOK)
	}
	var text strings.Builder
	for _, pull := range result.added {
		fmt.Fprintf(&text, "Added %s (required by %s).\n", pull.Module, strings.Join(pull.RequiredBy, ", "))
	}
	shown := displayPath(result.path)
	fmt.Fprintf(&text, "Project %s generated in %s (%d files).\n", result.project, shown, result.files)
	fmt.Fprintf(&text, "Next: cd %s, then read README.md\n", strings.TrimPrefix(shown, "./"))
	return emitText(ctx, text.String())
}

// emitText writes the text result to stdout.
func emitText(ctx *Context, text string) int {
	if _, err := io.WriteString(ctx.Stdout, text); err != nil {
		ctx.Logger.Error("result_unwritable", slog.String("error", err.Error()))
		return exitInternal
	}
	return exitOK
}

// difference is one member of the verify document's differences.
type difference struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// writeVerify prints the result of verify and returns its exit code: 0 for no
// differences, 1 otherwise.
func writeVerify(ctx *Context, project string, differences []issue.Issue) int {
	code := exitOK
	if len(differences) > 0 {
		code = exitIssues
	}
	if ctx.isJSON() {
		members := make([]difference, 0, len(differences))
		for _, found := range differences {
			members = append(members, difference{found.Code, found.Path, found.Message})
		}
		doc := struct {
			Schema      int          `json:"schema"`
			OK          bool         `json:"ok"`
			Project     string       `json:"project"`
			Differences []difference `json:"differences"`
			Warnings    []Warning    `json:"warnings"`
		}{documentSchema, code == exitOK, project, members, ctx.warnings()}
		return emitDocument(ctx, ctx.Stdout, doc, code)
	}
	if code == exitOK {
		return emitText(ctx, "No differences.\n")
	}
	var text strings.Builder
	for _, found := range differences {
		fmt.Fprintf(&text, "%s %s: %s\n", found.Code, found.Path, found.Message)
	}
	fmt.Fprintf(&text, "%d %s\n", len(differences), plural(len(differences), "difference"))
	if emitText(ctx, text.String()) != exitOK {
		return exitInternal
	}
	return code
}

// writeCatalogBuilt prints the result of catalog build and returns the exit code.
func writeCatalogBuilt(ctx *Context, out, sha256 string) int {
	slashed := filepath.ToSlash(out)
	if ctx.isJSON() {
		doc := struct {
			Schema   int       `json:"schema"`
			OK       bool      `json:"ok"`
			Catalog  string    `json:"catalog"`
			SHA256   string    `json:"sha256"`
			Warnings []Warning `json:"warnings"`
		}{documentSchema, true, slashed, sha256, ctx.warnings()}
		return emitDocument(ctx, ctx.Stdout, doc, exitOK)
	}
	return emitText(ctx, fmt.Sprintf("Catalog written to %s (sha256 %s).\n", slashed, sha256[:12]))
}
