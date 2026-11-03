package featureflags_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/featureflags"
)

const (
	firstFlagsFile = `flags:
  - key: shared_flag
    type: ops
    default: true
    description: Declared in the first file
  - key: first_only
    type: ops
    default: false
    description: Declared only in the first file
`
	secondFlagsFile = `flags:
  - key: shared_flag
    type: ops
    default: false
    description: Redeclared in the second file
  - key: second_only
    type: ops
    default: true
    description: Declared only in the second file
`
)

func TestSplitManifestPaths(t *testing.T) {
	t.Run("one path", func(t *testing.T) {
		paths, err := featureflags.SplitManifestPaths("flags.yaml")
		if err != nil || !slices.Equal(paths, []string{"flags.yaml"}) {
			t.Fatalf("paths = %v, %v", paths, err)
		}
	})
	t.Run("a list keeps its order", func(t *testing.T) {
		const value = "flags.yaml,specs/dispatch/tck/fixtures/flags.yaml"
		paths, err := featureflags.SplitManifestPaths(value)
		want := []string{"flags.yaml", "specs/dispatch/tck/fixtures/flags.yaml"}
		if err != nil || !slices.Equal(paths, want) {
			t.Fatalf("paths = %v, %v; want %v", paths, err, want)
		}
	})
	t.Run("trims the blank space around a path", func(t *testing.T) {
		paths, err := featureflags.SplitManifestPaths(" flags.yaml , specs/x.yaml ")
		want := []string{"flags.yaml", "specs/x.yaml"}
		if err != nil || !slices.Equal(paths, want) {
			t.Fatalf("paths = %v, %v; want %v", paths, err, want)
		}
	})
	t.Run("an empty segment is an error", func(t *testing.T) {
		_, err := featureflags.SplitManifestPaths("flags.yaml,,specs/x.yaml")
		if err == nil || !strings.Contains(err.Error(), "APP_FLAGS_FILE") || !strings.Contains(err.Error(), "path 2") {
			t.Fatalf("err = %v, want one naming APP_FLAGS_FILE and the empty path", err)
		}
	})
	t.Run("a trailing comma is an error", func(t *testing.T) {
		_, err := featureflags.SplitManifestPaths("flags.yaml,")
		if err == nil || !strings.Contains(err.Error(), "path 2") {
			t.Fatalf("err = %v, want one naming the empty path", err)
		}
	})
	t.Run("a blank value is an error", func(t *testing.T) {
		_, err := featureflags.SplitManifestPaths("   ")
		if err == nil {
			t.Fatal("a blank value accepted")
		}
	})
}

func TestLoadManifestFiles(t *testing.T) {
	t.Run("one path loads its flags", func(t *testing.T) {
		path := writeManifest(t, twoFlagManifest)
		manifest, err := featureflags.LoadManifestFiles([]string{path})
		if err != nil {
			t.Fatalf("load = %v", err)
		}
		if len(manifest.Flags()) != 2 {
			t.Fatalf("flags = %v, want the two of the file", manifest.Flags())
		}
		if _, found := manifest.Lookup(releaseKey); !found {
			t.Fatalf("Lookup missed %s", releaseKey)
		}
	})
	t.Run("two paths merge in order", func(t *testing.T) {
		first := writeManifest(t, firstFlagsFile)
		second := writeManifest(t, secondFlagsFile)
		manifest, err := featureflags.LoadManifestFiles([]string{first, second})
		if err != nil {
			t.Fatalf("load = %v", err)
		}
		keys := make([]string, 0, len(manifest.Flags()))
		for _, flag := range manifest.Flags() {
			keys = append(keys, string(flag.Key))
		}
		want := []string{"shared_flag", "first_only", "second_only"}
		if !slices.Equal(keys, want) {
			t.Fatalf("flags = %v, want %v", keys, want)
		}
	})
	t.Run("the last file wins on a duplicate key", func(t *testing.T) {
		first := writeManifest(t, firstFlagsFile)
		second := writeManifest(t, secondFlagsFile)
		manifest, err := featureflags.LoadManifestFiles([]string{first, second})
		if err != nil {
			t.Fatalf("load = %v", err)
		}
		flag, found := manifest.Lookup("shared_flag")
		if !found {
			t.Fatal("Lookup missed shared_flag")
		}
		if flag.Default {
			t.Fatalf("default = %v, want the second file's false", flag.Default)
		}
		if flag.Description != "Redeclared in the second file" {
			t.Fatalf("description = %q, want the second file's", flag.Description)
		}
	})
	t.Run("a missing file in the list names its path", func(t *testing.T) {
		first := writeManifest(t, firstFlagsFile)
		missing := filepath.Join(t.TempDir(), "absent.yaml")
		_, err := featureflags.LoadManifestFiles([]string{first, missing})
		if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), missing) {
			t.Fatalf("err = %v, want one naming %s", err, missing)
		}
	})
	t.Run("an invalid file in the list keeps its problems", func(t *testing.T) {
		first := writeManifest(t, firstFlagsFile)
		invalid := writeManifest(t, "flags:\n  - key: Bad\n")
		_, err := featureflags.LoadManifestFiles([]string{first, invalid})
		var manifestErr *featureflags.ManifestError
		if !errors.As(err, &manifestErr) || len(manifestErr.Problems) < 2 {
			t.Fatalf("err = %v, want the invalid file's problems", err)
		}
	})
	t.Run("no path", func(t *testing.T) {
		_, err := featureflags.LoadManifestFiles(nil)
		if err == nil {
			t.Fatal("an empty list accepted")
		}
	})
}
