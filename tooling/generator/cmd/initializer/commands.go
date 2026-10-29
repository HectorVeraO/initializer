package main

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

// Command is one entry of the command table; help, parsing and dispatch read it.
type Command struct {
	// Path is the command words, such as {"catalog", "build"}.
	Path []string
	// Label is the overview row name, such as "generate [DIR]".
	Label string
	// Summary is the one overview line.
	Summary string
	// Usage is the usage line after "initializer ", such as "generate [DIR] [flags]".
	Usage string
	// Description is the help paragraph, wrapped at 72 columns.
	Description string
	// MaxArgs is the number of positionals accepted; -1 for any.
	MaxArgs int
	// Flags registers the flags and returns the bound options.
	Flags func(set *flagSet) any
	// Check returns the command-line problems that need the whole line: required
	// flags, exclusive flags and aliases given with their new spelling.
	Check func(set *flagSet, opts any, positionals []string) []Problem
	// Bind copies what the scan saw (which flags were given) into the options.
	Bind func(set *flagSet, opts any)
	// Issues returns the value problems found in the same run when the line has
	// command-line problems, so one report holds both.
	Issues   func(ctx *Context, opts any, positionals []string) ([]issue.Issue, issueView)
	Examples []Example
	Run      func(ctx *Context, opts any, positionals []string) int
}

// Example is one help example: a comment line and a command line.
type Example struct{ Comment, Line string }

// commandTable returns the command table. A function, not a variable: the
// help command reads the table it belongs to.
func commandTable() []*Command {
	return []*Command{generateCommand(), verifyCommand(), catalogBuildCommand(), helpCommand()}
}

type generateOptions struct {
	root, catalog, out, file, selection, name, goModule, stacks, modules string
	allModules                                                           bool
	components                                                           []componentEntry
	// given counts how many times the scan saw each flag, by long name.
	given map[string]int
}

// filePath returns the selection file, from -f or its hidden alias.
func (o *generateOptions) filePath() string {
	if o.file != "" {
		return o.file
	}
	return o.selection
}

func generateCommand() *Command {
	return &Command{
		Path:    []string{"generate"},
		Label:   "generate [DIR]",
		Summary: "Create a project in DIR (default ./<name>)",
		Usage:   "generate [DIR] [flags]",
		Description: "Create a project in DIR from the module catalog. DIR is relative to the\n" +
			"working directory and must be absent or empty. Values come from flags, then\n" +
			"from the input file (-f), then from defaults.",
		MaxArgs: 1,
		Flags: func(set *flagSet) any {
			o := &generateOptions{}
			set.str(&o.file, flagSpec{Name: "file", Short: "f", Placeholder: "PATH", Usage: "Input file: .yaml, .yml or .json; - reads stdin", Default: "none"})
			set.str(&o.name, flagSpec{Name: "name", Placeholder: "NAME", Usage: "Project name",
				Default: `the folder's name, else "demo"`})
			set.str(&o.goModule, flagSpec{Name: "go-module", Placeholder: "PATH", Usage: "Go module path",
				Default: "example.com/<name> when a stack needs one"})
			set.str(&o.stacks, flagSpec{Name: "stacks", Placeholder: "ID[,ID...]",
				Usage:   "Stacks to generate; replaces the file's list",
				Default: "the catalog's backend stack"})
			set.str(&o.modules, flagSpec{Name: "modules", Placeholder: "ID[,ID...]",
				Usage:   "Modules to install; replaces the file's list; required modules are added",
				Default: "none"})
			set.boolean(&o.allModules, flagSpec{Name: "all-modules", Usage: "Install every module of the backend stack", Default: "false"})
			set.components(&o.components, flagSpec{Name: "component", Placeholder: "STACK=NAME",
				Usage:   "Name the folder of STACK's component (repeatable)",
				Default: "the stack id"})
			set.str(&o.root, flagSpec{Name: "root", Placeholder: "DIR", Usage: "Source checkout", Default: "required"})
			set.str(&o.catalog, flagSpec{Name: "catalog", Placeholder: "FILE",
				Usage: "Catalog file built by 'initializer catalog build'", Default: "required"})
			set.str(&o.out, flagSpec{Name: "out", Placeholder: "DIR", Hidden: true,
				Deprecated: "pass the folder as an argument: initializer generate <dir>"})
			set.str(&o.selection, flagSpec{Name: "selection", Placeholder: "PATH", Hidden: true,
				Deprecated: "use -f <file>"})
			return o
		},
		Check:  checkGenerate,
		Bind:   bindGenerate,
		Issues: generateIssues,
		Examples: []Example{
			{"a backend with identity, in ./acme",
				"initializer generate acme --root . --catalog catalog.lock.json --go-module example.com/acme --stacks go-srv --modules identity"},
			{"backend and web console with every module",
				"initializer generate acme --root . --catalog catalog.lock.json --go-module example.com/acme --stacks go-srv,ng-web --all-modules"},
			{"from a file, overriding one value",
				"initializer generate -f acme.yaml --name acme-shop"},
		},
		Run: runGenerate,
	}
}

// bindGenerate keeps which flags the scan saw, for the merge.
func bindGenerate(set *flagSet, opts any) {
	opts.(*generateOptions).given = set.given
}

// codeInputFormatUnknown is the code of an input file whose extension no
// decoder reads.
const codeInputFormatUnknown = "INPUT_FORMAT_UNKNOWN"

// pathFormat returns the input format an -f value names: by extension in any
// case, or FormatNone with known true for "-", whose format is decided by its
// content. known is false for any other value.
func pathFormat(path string) (format manifest.Format, known bool) {
	lower := strings.ToLower(path)
	switch {
	case path == stdinPath:
		return manifest.FormatNone, true
	case strings.HasSuffix(lower, ".yaml"), strings.HasSuffix(lower, ".yml"):
		return manifest.FormatYAML, true
	case strings.HasSuffix(lower, ".json"):
		return manifest.FormatJSON, true
	}
	return manifest.FormatNone, false
}

// sniffFormat decides the format of standard input: JSON when the first byte
// that is not a space, tab, CR or LF is '{', else YAML.
func sniffFormat(data []byte) manifest.Format {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '{' {
		return manifest.FormatJSON
	}
	return manifest.FormatYAML
}

// checkGenerate reports the problems of a generate line that need more than one flag.
func checkGenerate(set *flagSet, opts any, positionals []string) []Problem {
	o := opts.(*generateOptions)
	var problems []Problem
	problems = append(problems, requiredFlags(set, "root", "catalog")...)
	problems = append(problems, duplicateComponentProblems(o.components)...)
	if set.given["out"] > 0 && len(positionals) > 0 {
		problems = append(problems, Problem{Arg: "--out", Message: "pass the folder once"})
	}
	if set.given["selection"] > 0 && set.given["file"] > 0 {
		problems = append(problems, Problem{Arg: "--selection", Message: "pass the file once"})
	}
	for _, input := range []struct{ flag, path string }{{"--file", o.file}, {"--selection", o.selection}} {
		if input.path != "" && !isKnownPath(input.path) {
			problems = append(problems, Problem{Arg: input.flag,
				Message: fmt.Sprintf("%q has an unsupported input format", input.path), Code: codeInputFormatUnknown})
		}
	}
	if o.allModules && set.given["modules"] > 0 {
		problems = append(problems, Problem{Arg: "--all-modules", Message: "cannot be combined with --modules"})
	}
	return problems
}

// requiredFlags returns a problem for each of names that was not given.
func requiredFlags(set *flagSet, names ...string) []Problem {
	var problems []Problem
	for _, name := range names {
		if set.given[name] == 0 {
			problems = append(problems, Problem{Arg: "--" + name, Message: "is required"})
		}
	}
	return problems
}

type verifyOptions struct{ project string }

func verifyCommand() *Command {
	return &Command{
		Path:    []string{"verify"},
		Label:   "verify [DIR]",
		Summary: "Compare a generated project with its initializer.lock.json",
		Usage:   "verify [DIR] [flags]",
		Description: "Compare the files of a generated project with the hashes recorded in its\n" +
			"initializer.lock.json. DIR defaults to the working directory.",
		MaxArgs: 1,
		Flags: func(set *flagSet) any {
			o := &verifyOptions{}
			set.str(&o.project, flagSpec{Name: "project", Placeholder: "DIR", Hidden: true,
				Deprecated: "pass the folder as an argument: initializer verify <dir>"})
			return o
		},
		Check: func(set *flagSet, _ any, positionals []string) []Problem {
			if set.given["project"] > 0 && len(positionals) > 0 {
				return []Problem{{Arg: "--project", Message: "pass the folder once"}}
			}
			return nil
		},
		Examples: []Example{
			{"in a generated project", "initializer verify"},
			{"in ./acme", "initializer verify acme"},
		},
		Run: runVerify,
	}
}

type catalogBuildOptions struct{ root, files, out string }

func catalogBuildCommand() *Command {
	return &Command{
		Path:    []string{"catalog", "build"},
		Label:   "catalog build",
		Summary: "Build catalog.lock.json from a checkout (CI, module authors)",
		Usage:   "catalog build [flags]",
		Description: "Build the catalog file from the tracked files of a source checkout.\n" +
			"The output is byte-identical for the same checkout.",
		MaxArgs: 0,
		Flags: func(set *flagSet) any {
			o := &catalogBuildOptions{}
			set.str(&o.root, flagSpec{Name: "root", Placeholder: "DIR", Usage: "Source checkout", Default: "required"})
			set.str(&o.files, flagSpec{Name: "files", Placeholder: "PATH",
				Usage: "File with the tracked paths, NUL-separated (git ls-files -z)", Default: "required"})
			set.str(&o.out, flagSpec{Name: "out", Placeholder: "PATH", Usage: "Catalog file to write", Default: "required"})
			return o
		},
		Check: func(set *flagSet, _ any, _ []string) []Problem {
			return requiredFlags(set, "root", "files", "out")
		},
		Examples: []Example{
			{"build the catalog of this checkout", "initializer catalog build --root . --files files.bin --out catalog.lock.json"},
		},
		Run: runCatalogBuild,
	}
}

func helpCommand() *Command {
	return &Command{
		Path:        []string{"help"},
		Label:       "help [COMMAND]",
		Summary:     "Show help for a command",
		Usage:       "help [COMMAND...]",
		Description: "Show the help of a command, or the command list when none is given.",
		MaxArgs:     -1,
		Flags:       func(*flagSet) any { return nil },
		Examples: []Example{
			{"the flags of generate", "initializer help generate"},
			{"the flags of a grouped command", "initializer help catalog build"},
		},
		Run: runHelp,
	}
}

// commandWords returns the distinct first words of the table, in table order.
func commandWords() []string {
	var words []string
	seen := map[string]bool{}
	for _, cmd := range commandTable() {
		if !seen[cmd.Path[0]] {
			seen[cmd.Path[0]] = true
			words = append(words, cmd.Path[0])
		}
	}
	return words
}

// resolution is the table entry a list of words names, or why it names none.
type resolution struct {
	cmd      *Command
	consumed int
	// group is set when the words name a noun group without a verb.
	group   string
	problem *Problem
}

// resolve finds the command the leading words name; words must not be empty.
func resolve(words []string) resolution {
	var matches []*Command
	for _, cmd := range commandTable() {
		if cmd.Path[0] == words[0] {
			matches = append(matches, cmd)
		}
	}
	if len(matches) == 0 {
		return resolution{problem: &Problem{
			Message:    `unknown command "` + words[0] + `"`,
			Suggestion: suggest(words[0], commandWords()),
		}}
	}
	if len(matches[0].Path) == 1 {
		return resolution{cmd: matches[0], consumed: 1}
	}
	if len(words) == 1 || looksLikeFlag(words[1]) {
		return resolution{group: words[0]}
	}
	var verbs []string
	for _, cmd := range matches {
		if cmd.Path[1] == words[1] {
			return resolution{cmd: cmd, consumed: 2}
		}
		verbs = append(verbs, cmd.Path[1])
	}
	problem := &Problem{Message: `unknown command "` + strings.Join(words[:2], " ") + `"`}
	if nearest := suggest(words[1], verbs); nearest != "" {
		problem.Suggestion = words[0] + " " + nearest
	}
	return resolution{problem: problem}
}

// isKnownPath reports whether an -f value names a format a decoder reads.
func isKnownPath(path string) bool {
	_, known := pathFormat(path)
	return known
}
