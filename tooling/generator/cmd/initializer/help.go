package main

import (
	"fmt"
	"io"
	"strings"
)

// writeOverview writes the command list every bare or help invocation prints.
func writeOverview(w io.Writer) {
	fmt.Fprint(w, "initializer creates projects from the module catalog.\n\n")
	fmt.Fprint(w, "Usage:\n  initializer <command> [flags]\n\n")
	fmt.Fprint(w, "Commands:\n")
	writeCommandRows(w, commandTable())
	fmt.Fprint(w, "\nRun 'initializer help <command>' for details.\n")
}

// writeCommandRows writes one "  label   summary" row per command, summaries aligned.
func writeCommandRows(w io.Writer, commands []*Command) {
	width := 0
	for _, cmd := range commands {
		width = max(width, len(cmd.Label))
	}
	for _, cmd := range commands {
		fmt.Fprintf(w, "  %-*s   %s\n", width, cmd.Label, cmd.Summary)
	}
}

// groupCommands returns the commands whose path starts with group, in table order.
func groupCommands(group string) []*Command {
	var found []*Command
	for _, cmd := range commandTable() {
		if len(cmd.Path) > 1 && cmd.Path[0] == group {
			found = append(found, cmd)
		}
	}
	return found
}

// writeGroupHelp writes the command list of a noun group such as "catalog".
func writeGroupHelp(w io.Writer, group string) {
	fmt.Fprintf(w, "Usage:\n  initializer %s <command> [flags]\n\nCommands:\n", group)
	writeCommandRows(w, groupCommands(group))
	fmt.Fprintf(w, "\nRun 'initializer help %s <command>' for details.\n", group)
}

// writeCommandHelp writes the help of one command from its table entry and
// the specs of its flags; hidden aliases are not listed.
func writeCommandHelp(w io.Writer, cmd *Command) {
	fmt.Fprintf(w, "Usage:\n  initializer %s\n\n%s\n", cmd.Usage, cmd.Description)
	set := newFlagSet(strings.Join(cmd.Path, " "))
	if cmd.Flags != nil {
		cmd.Flags(set)
	}
	var visible []flagSpec
	for _, spec := range set.specs {
		if !spec.Hidden {
			visible = append(visible, spec)
		}
	}
	if len(visible) > 0 {
		fmt.Fprint(w, "\nFlags:\n")
	}
	for _, spec := range visible {
		fmt.Fprintf(w, "  %s\n        %s\n", flagHead(spec), flagDescription(spec))
	}
	if len(cmd.Examples) > 0 {
		fmt.Fprint(w, "\nExamples:\n")
	}
	for _, example := range cmd.Examples {
		fmt.Fprintf(w, "  # %s\n  %s\n", example.Comment, example.Line)
	}
}

// flagHead renders "-f, --file PATH" for a flag.
func flagHead(spec flagSpec) string {
	head := "--" + spec.Name
	if spec.Short != "" {
		head = "-" + spec.Short + ", " + head
	}
	if spec.Placeholder != "" {
		head += " " + spec.Placeholder
	}
	return head
}

// flagDescription renders the usage text followed by its default or "(required)".
func flagDescription(spec flagSpec) string {
	switch spec.Default {
	case "":
		return spec.Usage
	case "required":
		return spec.Usage + " (required)"
	}
	return spec.Usage + " (default: " + spec.Default + ")"
}
