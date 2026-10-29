// Command build-cli builds the initializer CLI into tooling/generator/bin/.
//
// It runs from the mise postinstall hook and imports only the standard
// library, so it compiles even when cmd/initializer does not.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

var buildArgs = []string{"build", "-o", "bin/", "./cmd/initializer"}

const failureText = "build-cli: the initializer CLI did not build; fix the error above, " +
	"or set MISE_NO_HOOKS=1 and run mise install again to skip this step"

// run builds the CLI through goCmd and returns the process exit code.
func run(goCmd func(args ...string) error, stderr io.Writer) int {
	if goCmd == nil || stderr == nil {
		return 2
	}
	err := goCmd(buildArgs...)
	if err != nil {
		fmt.Fprintln(stderr, failureText)
		return 1
	}
	return 0
}

func main() {
	goCmd := func(args ...string) error {
		cmd := exec.Command("go", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	os.Exit(run(goCmd, os.Stderr))
}
