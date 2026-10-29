package main

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRunReportsFailureNamingTheHookEscape(t *testing.T) {
	var stderr bytes.Buffer
	code := run(func(args ...string) error { return errors.New("exit status 1") }, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "MISE_NO_HOOKS=1") {
		t.Fatalf("stderr = %q, want it to name MISE_NO_HOOKS=1", stderr.String())
	}
}

func TestRunSucceedsSilentlyWithTheGoBuildContract(t *testing.T) {
	var stderr bytes.Buffer
	var got []string
	code := run(func(args ...string) error {
		got = args
		return nil
	}, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	want := []string{"build", "-o", "bin/", "./cmd/initializer"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("go args = %v, want %v", got, want)
	}
}

func TestRunRejectsNilArguments(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(nil, &stderr); code != 2 {
		t.Fatalf("nil go command: exit code = %d, want 2", code)
	}
	goCmd := func(args ...string) error { return nil }
	if code := run(goCmd, nil); code != 2 {
		t.Fatalf("nil stderr: exit code = %d, want 2", code)
	}
}
