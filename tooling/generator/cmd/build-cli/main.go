// Command build-cli builds the initializer CLI into tooling/generator/bin/.
//
// It runs from the mise postinstall hook and imports only the standard
// library, so it compiles even when cmd/initializer does not.
//
// The go command stamps the commit and the dirty flag of the enclosing git
// repository, but it does not recognize a worktree (its .git is a file), so a
// worktree nested in another repository would be stamped with the outer
// repository. build-cli asks git for the checkout it runs in and points the go
// command at it through GIT_DIR and GIT_WORK_TREE.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

var buildArgs = []string{"build", "-o", "bin/", "./cmd/initializer"}

const failureText = "build-cli: the initializer CLI did not build; fix the error above, " +
	"or set MISE_NO_HOOKS=1 and run mise install again to skip this step"

// run builds the CLI through goCmd and returns the process exit code.
func run(goCmd func(env []string, args ...string) error, gitCmd func(args ...string) (string, error), stderr io.Writer) int {
	if goCmd == nil || gitCmd == nil || stderr == nil {
		return 2
	}
	err := goCmd(checkoutEnv(gitCmd), buildArgs...)
	if err != nil {
		fmt.Fprintln(stderr, failureText)
		return 1
	}
	return 0
}

// checkoutEnv returns the environment that makes git commands run by the go
// command resolve to the checkout of the working directory, or nil when the
// working directory is not in a git checkout.
func checkoutEnv(gitCmd func(args ...string) (string, error)) []string {
	gitDir, err := gitCmd("rev-parse", "--absolute-git-dir")
	if err != nil || gitDir == "" {
		return nil
	}
	workTree, err := gitCmd("rev-parse", "--show-toplevel")
	if err != nil || workTree == "" {
		return nil
	}
	env := []string{"GIT_DIR=" + gitDir, "GIT_WORK_TREE=" + workTree}
	return env
}

func main() {
	goCmd := func(env []string, args ...string) error {
		cmd := exec.Command("go", args...)
		cmd.Env = append(os.Environ(), env...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	gitCmd := func(args ...string) (string, error) {
		out, err := exec.Command("git", args...).Output()
		return strings.TrimSpace(string(out)), err
	}
	os.Exit(run(goCmd, gitCmd, os.Stderr))
}
