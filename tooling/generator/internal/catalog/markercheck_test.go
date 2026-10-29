package catalog

import (
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
)

const composeFile = ".deploy/local/docker-compose.yml"

func TestMarkerIssues(t *testing.T) {
	const open = "# initializer:module identity\n"
	const end = "# initializer:end identity\n"
	cases := []struct {
		name    string
		mutate  func(r fstest.MapFS)
		code    string
		path    string
		message string
	}{
		{"unbalanced", func(r fstest.MapFS) {
			put(r, composeFile, "name: fixture-app\n"+open+"services: {}\n")
		}, "MARKER_INVALID", composeFile, "opening marker for identity has no closing marker (line 2)"},
		{"nested", func(r fstest.MapFS) {
			put(r, composeFile, "name: fixture-app\n"+open+"# initializer:module parameters\n"+end)
		}, "MARKER_INVALID", composeFile, "markers do not nest: module parameters opens inside module identity (line 3)"},
		{"mismatched close", func(r fstest.MapFS) {
			put(r, composeFile, "name: fixture-app\n"+open+"# initializer:end parameters\n"+end)
		}, "MARKER_INVALID", composeFile, "closing marker names parameters, the open block is identity (line 3)"},
		{"wrong form", func(r fstest.MapFS) {
			put(r, composeFile, "name: fixture-app\n#  initializer:module identity\n"+end)
		}, "MARKER_INVALID", composeFile, `marker must be exactly "# initializer:module <id>" (line 2)`},
		{"core named", func(r fstest.MapFS) {
			put(r, composeFile, "name: fixture-app\n# initializer:module core\n# initializer:end core\n")
		}, "MARKER_INVALID", composeFile, "a marker may not name core (line 2)"},
		{"unknown module", func(r fstest.MapFS) {
			put(r, composeFile, "name: fixture-app\n# initializer:module ghost\n# initializer:end ghost\n")
		}, "MARKER_MODULE_UNKNOWN", composeFile, "marker names ghost, which has no module manifest (line 2)"},
		{"unknown stack", func(r fstest.MapFS) {
			put(r, composeFile, "name: fixture-app\n# initializer:stack ng-wbe\n# initializer:end ng-wbe\n")
		}, "MARKER_STACK_UNKNOWN", composeFile, `marker names stack "ng-wbe" (line 2), which is not a stack of the stack table`},
		{"stack core", func(r fstest.MapFS) {
			put(r, composeFile, "name: fixture-app\n# initializer:stack core\n# initializer:end core\n")
		}, "MARKER_STACK_UNKNOWN", composeFile, `marker names stack "core" (line 2), which is not a stack of the stack table`},
		{"stack marker in a Dockerfile", func(r fstest.MapFS) {
			put(r, "go-srv/Dockerfile", "FROM scratch\n# initializer:stack go-srv\n# initializer:end go-srv\n")
		}, "MARKER_INVALID", "go-srv/Dockerfile", "markers are not allowed in this file kind (line 2)"},
		{"disallowed kind in a core file", func(r fstest.MapFS) {
			put(r, "go-srv/data.json", "{\n# initializer:module identity\n}\n")
		}, "MARKER_INVALID", "go-srv/data.json", "markers are not allowed in this file kind (line 2)"},
		{"module file", func(r fstest.MapFS) {
			put(r, ".initializer/project/flags.yaml", "a: 1\n"+open+end)
		}, "MARKER_IN_MODULE_FILE", ".initializer/project/flags.yaml", "markers are allowed only in files owned by a core module (line 2)"},
		{"stack marker in a module file", func(r fstest.MapFS) {
			put(r, ".initializer/project/flags.yaml", "a: 1\n# initializer:stack ng-web\n# initializer:end ng-web\n")
		}, "MARKER_IN_MODULE_FILE", ".initializer/project/flags.yaml", "markers are allowed only in files owned by a core module (line 2)"},
		{"spec file", func(r fstest.MapFS) {
			put(r, "specs/core/openapi.yaml", "# initializer:module identity\n")
		}, "MARKER_IN_MODULE_FILE", "specs/core/openapi.yaml", "markers are allowed only in files owned by a core module (line 1)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := cleanRepo(t)
			tc.mutate(repo)
			_, issues := buildRepo(t, repo)
			want := issue.Issue{Code: tc.code, Path: tc.path, Message: tc.message}
			for _, got := range issues {
				if reflect.DeepEqual(got, want) {
					return
				}
			}
			t.Errorf("want %+v in %+v", want, issues)
		})
	}
}

func TestStackCoreMarkerReportsOnlyTheUnknownStack(t *testing.T) {
	repo := cleanRepo(t)
	put(repo, composeFile, "name: fixture-app\n# initializer:stack core\n# initializer:end core\n")
	_, issues := buildRepo(t, repo)
	want := []issue.Issue{{Code: "MARKER_STACK_UNKNOWN", Path: composeFile, Message: `marker names stack "core" (line 2), which is not a stack of the stack table`}}
	if len(issues) != len(want) || !reflect.DeepEqual(issues[0], want[0]) {
		t.Errorf("want exactly %+v, got %+v", want, issues)
	}
}

func TestValidMarkersAndProseAreAccepted(t *testing.T) {
	repo := cleanRepo(t)
	put(repo, composeFile, "name: fixture-app\n\nservices: {}\n# initializer:module identity\n# initializer:end identity\n")
	put(repo, ".initializer/project/flags.yaml", "# see the initializer:module grammar\n")
	_, issues := buildRepo(t, repo)
	assertCodes(t, issues)
}

func TestKnownStackBlockIsAccepted(t *testing.T) {
	repo := cleanRepo(t)
	put(repo, composeFile, "name: fixture-app\n\n# initializer:stack go-srv\nservices: {}\n# initializer:end go-srv\n# initializer:stack ng-web\n# initializer:end ng-web\n")
	_, issues := buildRepo(t, repo)
	assertCodes(t, issues)
}

func TestBinaryFileWithAMarkerLineIsNotChecked(t *testing.T) {
	repo := cleanRepo(t)
	put(repo, composeFile, "name: fixture-app\n\x00\n# initializer:module identity\n")
	_, issues := buildRepo(t, repo)
	assertCodes(t, issues)
}

func TestIdentityOwnsPostgresInitUnderCoreDeployFolder(t *testing.T) {
	repo := cleanRepo(t)
	put(repo, ".deploy/local/postgres-init/10-init.sh", "#!/bin/sh\n")
	edit(t, repo, "go-srv/internal/platform/identity/module.manifest.json", `".deploy/local/kratos/", `, `".deploy/local/kratos/", ".deploy/local/postgres-init/", `)
	built, issues := buildRepo(t, repo)
	assertCodes(t, issues)
	identity := findModule(t, built, "go-srv", "identity")
	if !hasFile(identity.Files, ".deploy/local/postgres-init/10-init.sh") {
		t.Errorf("identity files: %+v", identity.Files)
	}
	core := findModule(t, built, "go-srv", "core")
	if hasFile(core.Files, ".deploy/local/postgres-init/10-init.sh") {
		t.Error("core must not own the identity folder")
	}
}
