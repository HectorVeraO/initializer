package catalog

import (
	"bytes"
	"flag"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

var update = flag.Bool("update", false, "rewrite the golden catalog")

const (
	paramsManifest   = "go-srv/internal/platform/parameters/module.manifest.json"
	flagsManifest    = "go-srv/internal/platform/featureflags/module.manifest.json"
	coreManifest     = "go-srv/internal/platform/core/module.manifest.json"
	identityManifest = "go-srv/internal/platform/identity/module.manifest.json"
	actorsFile       = "specs/tck-support/actors/index.ts"
	paramsFile       = "go-srv/internal/platform/parameters/parameters.go"
)

func TestCleanRepoMatchesGolden(t *testing.T) {
	repo := cleanRepo(t)
	built, issues := buildRepo(t, repo)
	assertCodes(t, issues)
	encoded, err := Encode(built)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	golden := filepath.Join("testdata", "clean", "catalog.lock.json")
	if *update {
		if err := os.WriteFile(golden, encoded, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("catalog differs from %s (run with -update to rewrite)\n%s", golden, encoded)
	}
}

func TestFilesListMatchesFixtureTree(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "clean", "files.txt"))
	if err != nil {
		t.Fatalf("read files.txt: %v", err)
	}
	want := strings.Split(strings.TrimSpace(string(data)), "\n")
	got := filesOf(cleanRepo(t))
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("files.txt is stale:\n%v\nwant\n%v", want, got)
	}
}

func TestOwnershipLongestPrefix(t *testing.T) {
	built, issues := buildRepo(t, cleanRepo(t))
	assertCodes(t, issues)
	core := findModule(t, built, "go-srv", "core")
	params := findModule(t, built, "go-srv", "parameters")
	if hasFile(core.Files, paramsFile) || !hasFile(params.Files, paramsFile) {
		t.Fatalf("parameters.go must belong to parameters, not core")
	}
	if !hasFile(core.Files, "go-srv/cmd/server/main.go") {
		t.Fatalf("main.go must belong to core")
	}
	identity := findModule(t, built, "go-srv", "identity")
	if hasFile(core.Files, actorsFile) || !hasFile(identity.Files, actorsFile) {
		t.Fatalf("%s must belong to identity, not to core's specs/tck-support/", actorsFile)
	}
	if !hasFile(core.Files, "specs/tck-support/index.ts") {
		t.Fatalf("specs/tck-support/index.ts must stay with core")
	}
	notCatalogued := []string{
		"go-srv/cmd/server/modules.go", "go-srv/AGENTS.md", "go-srv/cookbooks/notes.md",
		"README.md", "flags.yaml", "tooling/notes.txt",
	}
	for _, p := range notCatalogued {
		for _, stack := range built.Stacks {
			for _, module := range stack.Modules {
				if hasFile(module.Files, p) {
					t.Fatalf("%s must not be catalogued", p)
				}
			}
		}
	}
}

func TestTargetIsListedOnTheSourceEntry(t *testing.T) {
	built, _ := buildRepo(t, cleanRepo(t))
	flags := findModule(t, built, "go-srv", "feature-flags")
	for _, f := range flags.Files {
		if f.Path == ".initializer/project/flags.yaml" && f.Target == "flags.yaml" {
			return
		}
	}
	t.Fatalf("feature-flags must list .initializer/project/flags.yaml with target flags.yaml: %+v", flags.Files)
}

func TestExactFileEntryBeatsDirectoryEntry(t *testing.T) {
	repo := cleanRepo(t)
	edit(t, repo, paramsManifest, `["go-srv/internal/platform/parameters/"]`,
		`["go-srv/cmd/server/main.go", "go-srv/internal/platform/parameters/"]`)
	built, issues := buildRepo(t, repo)
	assertCodes(t, issues)
	if !hasFile(findModule(t, built, "go-srv", "parameters").Files, "go-srv/cmd/server/main.go") {
		t.Fatalf("an exact file entry must beat the go-srv/ directory entry of core")
	}
}

func TestIssueCodes(t *testing.T) {
	const xManifest = "go-srv/internal/platform/extra/module.manifest.json"
	cases := []struct {
		name   string
		mutate func(t *testing.T, repo fstest.MapFS)
		want   []string
	}{
		{"manifest entry matches nothing", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, paramsManifest, `["go-srv/internal/platform/parameters/"]`, `["go-srv/internal/platform/parameters/", "go-srv/zzz-missing.go"]`)
		}, []string{"MANIFEST_INVALID"}},
		{"manifest has an unknown key", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, paramsManifest, `"schema": 1,`, `"schema": 1, "entrypoint": "x",`)
		}, []string{"MANIFEST_INVALID"}},
		{"unknown stack", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, paramsManifest, `"stack": "go-srv"`, `"stack": "elixir"`)
		}, []string{"STACK_UNKNOWN"}},
		{"stack differs from the modules root", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, flagsManifest, `"stack": "go-srv"`, `"stack": "ng-web"`)
			edit(t, r, flagsManifest, `  "tablePrefixes": ["flg"],`+"\n", "")
		}, []string{"STACK_UNKNOWN"}},
		{"folder does not match the id", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, paramsManifest, `"module": "parameters"`, `"module": "params"`)
		}, []string{"MODULE_DIR_MISMATCH"}},
		{"duplicate module id", func(t *testing.T, r fstest.MapFS) {
			put(r, "go-srv/internal/platform/parameters2/module.manifest.json", string(r[paramsManifest].Data))
			edit(t, r, "go-srv/internal/platform/parameters2/module.manifest.json", `platform/parameters/`, `platform/parameters2/`)
			put(r, "go-srv/internal/platform/parameters2/x.go", "package parameters2\n")
		}, []string{"MODULE_DUPLICATE", "MODULE_DIR_MISMATCH"}},
		{"frontend module without backend", func(t *testing.T, r fstest.MapFS) {
			addNgCore(r)
			addNgModule(r, "widget", false, `{"parameters": "0.1.0"}`)
		}, []string{"MODULE_NOT_IN_BACKEND"}},
		{"implied differs between stacks", func(t *testing.T, r fstest.MapFS) {
			addNgCore(r)
			addNgModule(r, "identity", true, `{"identity": "1.0.0"}`)
		}, []string{"IMPLIED_MISMATCH"}},
		{"identity implied", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, identityManifest, `"implied": false`, `"implied": true`)
		}, []string{"IMPLIED_MISMATCH"}},
		{"core requires another module", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, coreManifest, `"requiresModules": [],`, `"requiresModules": [{"module": "parameters", "range": "^0.1.0"}],`)
		}, []string{"REQUIRES_UNSATISFIED"}},
		{"implied on a module that may not be", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, paramsManifest, `"implied": false`, `"implied": true`)
		}, []string{"IMPLIED_MISMATCH"}},
		{"core not implied", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, coreManifest, `"implied": true`, `"implied": false`)
		}, []string{"IMPLIED_MISMATCH"}},
		{"stack without core", func(t *testing.T, r fstest.MapFS) {
			addNgModule(r, "parameters", false, `{"parameters": "0.1.0"}`)
		}, []string{"CORE_MISSING"}},
		{"unknown spec", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, paramsManifest, `{"parameters": "0.1.0"}`, `{"ghost": "0.1.0", "parameters": "0.1.0"}`)
		}, []string{"SPEC_UNKNOWN"}},
		{"backend version differs from the spec", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, "specs/parameters/spec.manifest.json", `"version": "0.1.0"`, `"version": "0.1.1"`)
		}, []string{"SPEC_VERSION_MISMATCH"}},
		{"frontend ahead of the spec", func(t *testing.T, r fstest.MapFS) {
			addNgCore(r)
			addNgModule(r, "parameters", false, `{"parameters": "0.2.0"}`)
		}, []string{"SPEC_VERSION_AHEAD"}},
		{"frontend on another major", func(t *testing.T, r fstest.MapFS) {
			addNgCore(r)
			addNgModule(r, "parameters", false, `{"parameters": "1.0.0"}`)
		}, []string{"SPEC_MAJOR_MISMATCH"}},
		{"spec implemented twice", func(t *testing.T, r fstest.MapFS) {
			put(r, xManifest, string(r[paramsManifest].Data))
			edit(t, r, xManifest, `"module": "parameters"`, `"module": "extra"`)
			edit(t, r, xManifest, `platform/parameters/`, `platform/extra/`)
			edit(t, r, xManifest, `"sys"`, `"xtr"`)
			put(r, "go-srv/internal/platform/extra/extra.go", "package extra\n")
		}, []string{"SPEC_IMPLEMENTED_TWICE"}},
		{"spec nobody implements", func(t *testing.T, r fstest.MapFS) {
			put(r, "specs/orphan/spec.manifest.json", `{ "schema": 1, "spec": "orphan", "version": "0.1.0", "tckTags": ["@orphan"], "tckEnv": {} }`+"\n")
		}, []string{"SPEC_UNIMPLEMENTED"}},
		{"tag in two specs", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, "specs/parameters/spec.manifest.json", `["@parameters"]`, `["@feature-flags"]`)
		}, []string{"TCK_TAG_DUPLICATE"}},
		{"core out of range", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, coreManifest, `"version": "0.1.0"`, `"version": "0.2.0"`)
		}, []string{"REQUIRES_UNSATISFIED"}},
		{"required module absent", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, paramsManifest, `"requiresModules": [],`, `"requiresModules": [{"module": "ghost", "range": "^0.1.0"}],`)
		}, []string{"REQUIRES_UNSATISFIED"}},
		{"required module out of range", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, paramsManifest, `"requiresModules": [],`, `"requiresModules": [{"module": "feature-flags", "range": "^0.2.0"}],`)
		}, []string{"REQUIRES_UNSATISFIED"}},
		{"dependency cycle", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, paramsManifest, `"requiresModules": [],`, `"requiresModules": [{"module": "feature-flags", "range": "^0.1.0"}],`)
			edit(t, r, flagsManifest, `"requiresModules": [],`, `"requiresModules": [{"module": "parameters", "range": "^0.1.0"}],`)
		}, []string{"DEPENDENCY_CYCLE"}},
		{"edge only the web manifest declares", func(t *testing.T, r fstest.MapFS) {
			addNgCore(r)
			addNgModule(r, "parameters", false, `{"parameters": "0.1.0"}`)
			addNgModule(r, "feature-flags", false, `{"feature-flags": "0.1.0"}`)
			edit(t, r, ngRoot+"parameters/module.manifest.json", `"requiresModules": [],`, `"requiresModules": [{"module": "feature-flags", "range": "^0.1.0"}],`)
		}, []string{"REQUIRES_STACK_MISMATCH"}},
		{"edge only the backend manifest declares", func(t *testing.T, r fstest.MapFS) {
			addNgCore(r)
			addNgModule(r, "parameters", false, `{"parameters": "0.1.0"}`)
			edit(t, r, paramsManifest, `"requiresModules": [],`, `"requiresModules": [{"module": "feature-flags", "range": "^0.1.0"}],`)
		}, []string{"REQUIRES_STACK_MISMATCH"}},
		{"file under a spec folder without manifest", func(t *testing.T, r fstest.MapFS) {
			put(r, "specs/stray/readme.md", "stray\n")
		}, []string{"FILE_UNOWNED"}},
		{"file under a managed root nobody lists", func(t *testing.T, r fstest.MapFS) {
			put(r, ".initializer/project/other.yaml", "x: 1\n")
		}, []string{"FILE_UNOWNED"}},
		{"equal entries in two manifests", func(t *testing.T, r fstest.MapFS) {
			put(r, "go-srv/shared/x.txt", "shared\n")
			edit(t, r, paramsManifest, `["go-srv/internal/platform/parameters/"]`, `["go-srv/internal/platform/parameters/", "go-srv/shared/"]`)
			edit(t, r, flagsManifest, "\"go-srv/internal/platform/featureflags/\"\n  ],", "\"go-srv/internal/platform/featureflags/\",\n    \"go-srv/shared/\"\n  ],")
		}, []string{"FILE_OWNERSHIP_CONFLICT"}},
		{"target inside another owner's folder", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, flagsManifest, `"target": "flags.yaml"`, `"target": "`+paramsFile+`"`)
		}, []string{"TARGET_CONFLICT"}},
		{"target on a composition root", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, flagsManifest, `"target": "flags.yaml"`, `"target": "go-srv/cmd/server/modules.go"`)
		}, []string{"TARGET_CONFLICT"}},
		{"target on a rendered file", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, flagsManifest, `"target": "flags.yaml"`, `"target": "README.md"`)
		}, []string{"TARGET_CONFLICT"}},
		{"target shared by two manifests", func(t *testing.T, r fstest.MapFS) {
			put(r, "go-srv/shared/x.txt", "shared\n")
			edit(t, r, paramsManifest, `["go-srv/internal/platform/parameters/"]`,
				`["go-srv/internal/platform/parameters/", {"path": "go-srv/shared/x.txt", "target": "flags.yaml"}]`)
		}, []string{"TARGET_CONFLICT"}},
		{"symlink", func(t *testing.T, r fstest.MapFS) {
			r["specs/package.json"] = &fstest.MapFile{Data: []byte("../outside"), Mode: symlinkMode}
		}, []string{"SYMLINK"}},
		{"CRLF in a text file", func(t *testing.T, r fstest.MapFS) {
			put(r, "specs/package.json", "{\r\n}\r\n")
		}, []string{"NON_LF_TEXT"}},
		{"CRLF after byte 8000 of a text file", func(t *testing.T, r fstest.MapFS) {
			put(r, "specs/package.json", strings.Repeat("a", 8100)+"\r\n")
		}, []string{"NON_LF_TEXT"}},
		{"CRLF in a binary file", func(t *testing.T, r fstest.MapFS) {
			put(r, "specs/package.json", "\x00\r\n")
		}, nil},
		{"NUL after byte 8000 does not make a file binary", func(t *testing.T, r fstest.MapFS) {
			put(r, "specs/package.json", strings.Repeat("a", 8100)+"\x00\r\n")
		}, []string{"NON_LF_TEXT"}},
		{"go.mod line 1", func(t *testing.T, r fstest.MapFS) {
			put(r, "go-srv/go.mod", "mod "+fixtureModule+"\n")
		}, []string{"GO_MODULE_LINE_INVALID"}},
		{"go.mod not tracked", func(t *testing.T, r fstest.MapFS) {
			delete(r, "go-srv/go.mod")
		}, []string{"GO_MODULE_LINE_INVALID"}},
		{"module path in a spec file", func(t *testing.T, r fstest.MapFS) {
			put(r, "specs/core/openapi.yaml", "servers: "+fixtureModule+"\n")
		}, []string{"GO_MODULE_PATH_LEAK"}},
		{"module path in a Go string", func(t *testing.T, r fstest.MapFS) {
			put(r, paramsFile, "package parameters\n\nconst Path = \""+fixtureModule+"/x\"\n")
		}, []string{"GO_MODULE_PATH_LEAK"}},
		{"module path in testdata", func(t *testing.T, r fstest.MapFS) {
			put(r, "go-srv/internal/platform/core/testdata/note.txt", fixtureModule+"\n")
		}, []string{"GO_MODULE_PATH_LEAK"}},
		{"import group mixes module and other imports", func(t *testing.T, r fstest.MapFS) {
			put(r, paramsFile, "package parameters\n\nimport (\n\t\""+fixtureModule+"/internal/platform/core\"\n\t\"fmt\"\n)\n\nvar _ = fmt.Sprint\nvar _ = core.Run\n")
		}, []string{"GO_IMPORT_GROUP"}},
		{"trailing comment on a module import", func(t *testing.T, r fstest.MapFS) {
			put(r, paramsFile, "package parameters\n\nimport (\n\t\""+fixtureModule+"/internal/platform/core\" // c\n)\n\nvar _ = core.Run\n")
		}, []string{"GO_IMPORT_GROUP"}},
		{"unparsable Go file", func(t *testing.T, r fstest.MapFS) {
			put(r, paramsFile, "package parameters\n\nimport (\n")
		}, []string{"GO_PARSE_FAILED"}},
		{"unparsable Go file in testdata", func(t *testing.T, r fstest.MapFS) {
			put(r, "go-srv/internal/platform/core/testdata/broken.go", "package (\n")
		}, nil},
		{"module imports a sibling module", func(t *testing.T, r fstest.MapFS) {
			put(r, paramsFile, "package parameters\n\nimport (\n\t\""+fixtureModule+"/internal/platform/featureflags\"\n)\n\nvar _ = featureflags.Name\n")
		}, []string{"IMPORT_BOUNDARY"}},
		{"module imports a package outside core and itself", func(t *testing.T, r fstest.MapFS) {
			put(r, paramsFile, "package parameters\n\nimport (\n\t\""+fixtureModule+"/internal/other\"\n)\n\nvar _ = other.X\n")
		}, []string{"IMPORT_BOUNDARY"}},
		{"core imports a module", func(t *testing.T, r fstest.MapFS) {
			put(r, "go-srv/internal/platform/core/core.go", "package core\n\nimport (\n\t\""+fixtureModule+"/internal/platform/parameters\"\n)\n\nvar _ = parameters.Name\n\n// Run does nothing.\nfunc Run() {}\n")
		}, []string{"IMPORT_BOUNDARY"}},
		{"main imports a module", func(t *testing.T, r fstest.MapFS) {
			put(r, "go-srv/cmd/server/main.go", "package main\n\nimport (\n\t\""+fixtureModule+"/internal/platform/parameters\"\n)\n\nfunc main() { _ = parameters.Name }\n")
		}, []string{"IMPORT_BOUNDARY"}},
		{"package clause differs from the folder", func(t *testing.T, r fstest.MapFS) {
			put(r, paramsFile, "package params\n")
		}, []string{"ENTRYPOINT_MISSING"}},
		{"module folder without Go files", func(t *testing.T, r fstest.MapFS) {
			delete(r, paramsFile)
		}, []string{"ENTRYPOINT_MISSING"}},
		{"frontend module without module.ts", func(t *testing.T, r fstest.MapFS) {
			addNgCore(r)
			delete(r, ngRoot+"core/module.ts")
		}, []string{"ENTRYPOINT_MISSING"}},
		{"compose name", func(t *testing.T, r fstest.MapFS) {
			put(r, ".deploy/local/docker-compose.yml", "name: Bad_Name\n")
		}, []string{"COMPOSE_NAME_INVALID"}},
		{"compose file missing", func(t *testing.T, r fstest.MapFS) {
			delete(r, ".deploy/local/docker-compose.yml")
			put(r, ".deploy/local/other.txt", "other\n")
			edit(t, r, coreManifest, `".deploy/local/", `, `".deploy/local/other.txt", `)
		}, []string{"COMPOSE_NAME_INVALID"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := cleanRepo(t)
			tc.mutate(t, repo)
			_, issues := buildRepo(t, repo)
			assertCodes(t, issues, tc.want...)
		})
	}
}

func TestStackRootIsManagedOnlyWithAManifest(t *testing.T) {
	repo := cleanRepo(t)
	put(repo, "ng-web/package.json", "{}\n")
	put(repo, "ng-web/src/main.ts", "export {};\n")
	built, issues := buildRepo(t, repo)
	assertCodes(t, issues)
	for _, stack := range built.Stacks {
		if stack.Stack == "ng-web" {
			t.Fatalf("ng-web has no manifest and must not appear")
		}
		for _, module := range stack.Modules {
			for _, f := range module.Files {
				if strings.HasPrefix(f.Path, "ng-web/") {
					t.Fatalf("ng-web file %s catalogued", f.Path)
				}
			}
		}
	}

	addNgCore(repo)
	edit(t, repo, ngRoot+"core/module.manifest.json", `["ng-web/"]`, `["ng-web/src/app/platform/core/"]`)
	_, issues = buildRepo(t, repo)
	assertCodes(t, issues, "FILE_UNOWNED")
	for _, found := range issues {
		if !strings.HasPrefix(found.Path, "ng-web/") {
			t.Fatalf("unexpected issue path %s", found.Path)
		}
	}
}

func TestIssuesAreCollectedTogether(t *testing.T) {
	repo := cleanRepo(t)
	put(repo, "specs/stray/readme.md", "stray\n")
	put(repo, ".deploy/local/docker-compose.yml", "name: Bad_Name\n")
	put(repo, "specs/package.json", "{\r\n}\r\n")
	_, issues := buildRepo(t, repo)
	assertCodes(t, issues, "FILE_UNOWNED", "COMPOSE_NAME_INVALID", "NON_LF_TEXT")
}

func TestBuildIsDeterministicOverFileOrder(t *testing.T) {
	repo := cleanRepo(t)
	built, _ := buildRepo(t, repo)
	want, err := Encode(built)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	files := filesOf(repo)
	random := rand.New(rand.NewSource(7))
	for i := 0; i < 20; i++ {
		random.Shuffle(len(files), func(a, b int) { files[a], files[b] = files[b], files[a] })
		again, issues, err := Build(BuildOptions{Root: repo, Files: files, Generator: "0.1.0"})
		if err != nil || len(issues) > 0 {
			t.Fatalf("Build: %v %v", err, issues)
		}
		got, err := Encode(again)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("shuffle %d changed the catalog", i)
		}
	}
}

func TestBuildRefusesUnsafeFileLists(t *testing.T) {
	repo := cleanRepo(t)
	for _, bad := range []string{"../escape", "/abs", "a\\b", "a//b", "./a", "a/../b", "a\x01b", "dir/", ""} {
		_, _, err := Build(BuildOptions{Root: repo, Files: []string{bad}, Generator: "0.1.0"})
		if err == nil || !strings.Contains(err.Error(), "invalid tracked path") {
			t.Fatalf("path %q must be refused as an invalid tracked path, got %v", bad, err)
		}
	}
}

func TestBuildFailsOnAMissingTrackedFile(t *testing.T) {
	repo := cleanRepo(t)
	files := append(filesOf(repo), "go-srv/ghost.go")
	if _, _, err := Build(BuildOptions{Root: repo, Files: files, Generator: "0.1.0"}); err == nil {
		t.Fatalf("a tracked file missing on disk is an I/O error")
	}
}

func TestBuildDoesNotFollowASymlinkedManifest(t *testing.T) {
	repo := cleanRepo(t)
	repo[paramsManifest] = &fstest.MapFile{Data: []byte("../../../../outside.json"), Mode: symlinkMode}
	_, issues := buildRepo(t, repo)
	if !strings.Contains(strings.Join(codesOf(issues), ","), "SYMLINK") {
		t.Fatalf("want SYMLINK, got %v", issues)
	}
}

func TestGoFindingsAreAllReportedTogether(t *testing.T) {
	repo := cleanRepo(t)
	put(repo, paramsFile, "package parameters\n\nconst Path = \""+fixtureModule+"\"\n")
	put(repo, "go-srv/internal/platform/identity/identity.go", "package identity\n\nimport (\n\t\""+fixtureModule+"/internal/platform/core\"\n\t\"fmt\"\n)\n\nvar _ = fmt.Sprint\nvar _ = core.Run\n")
	put(repo, "go-srv/internal/platform/featureflags/featureflags.go", "package featureflags\n\nimport (\n")
	_, issues := buildRepo(t, repo)
	assertCodes(t, issues, "GO_MODULE_PATH_LEAK", "GO_IMPORT_GROUP", "GO_PARSE_FAILED")
}

func TestModuleFolderWithoutManifestIsUnowned(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"go-srv folder", "go-srv/internal/platform/billing/billing.go"},
		{"go-srv nested file", "go-srv/internal/platform/billing/internal/store.go"},
		{"ng-web folder", ngRoot + "billing/billing.ts"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := cleanRepo(t)
			addNgCore(repo)
			put(repo, tc.path, "// Package billing is a module folder whose manifest was forgotten.\npackage billing\n")

			built, issues := buildRepo(t, repo)
			assertCodes(t, issues, "FILE_UNOWNED")
			for _, stack := range []string{"go-srv", "ng-web"} {
				for _, entry := range built.Stacks {
					if entry.Stack != stack {
						continue
					}
					for _, module := range entry.Modules {
						if hasFile(module.Files, tc.path) {
							t.Fatalf("%s/%s delivers %s", stack, module.Module, tc.path)
						}
					}
				}
			}
		})
	}
}

func TestModulesRootCompositionRootIsNotAModuleFolder(t *testing.T) {
	repo := cleanRepo(t)
	addNgCore(repo)
	put(repo, ngRoot+"modules.ts", "export const modules = [];\n")

	_, issues := buildRepo(t, repo)
	assertCodes(t, issues)
}

func TestExplicitClaimIntoFolderWithoutManifestIsDelivered(t *testing.T) {
	repo := cleanRepo(t)
	addNgCore(repo)
	addNgModule(repo, "parameters", false, `{"parameters": "0.1.0"}`)
	claimed := ngRoot + "billing/billing.ts"
	put(repo, claimed, "export const billing = true;\n")
	edit(t, repo, ngRoot+"parameters/module.manifest.json", `"files": ["`+ngRoot+`parameters/"]`, `"files": ["`+claimed+`", "`+ngRoot+`parameters/"]`)

	built, issues := buildRepo(t, repo)
	assertCodes(t, issues)
	for _, entry := range built.Stacks {
		for _, module := range entry.Modules {
			if entry.Stack == "ng-web" && module.Module == "parameters" && hasFile(module.Files, claimed) {
				return
			}
		}
	}
	t.Fatalf("ng-web/parameters does not deliver %s", claimed)
}

func TestExplicitClaimOfTheWholeFolderWithoutManifestIsDelivered(t *testing.T) {
	repo := cleanRepo(t)
	addNgCore(repo)
	addNgModule(repo, "parameters", false, `{"parameters": "0.1.0"}`)
	claimed := ngRoot + "billing/billing.ts"
	put(repo, claimed, "export const billing = true;\n")
	edit(t, repo, ngRoot+"parameters/module.manifest.json", `"files": ["`+ngRoot+`parameters/"]`, `"files": ["`+ngRoot+`billing/", "`+ngRoot+`parameters/"]`)

	built, issues := buildRepo(t, repo)
	assertCodes(t, issues)
	for _, entry := range built.Stacks {
		for _, module := range entry.Modules {
			if entry.Stack == "ng-web" && module.Module == "parameters" && hasFile(module.Files, claimed) {
				return
			}
		}
	}
	t.Fatalf("ng-web/parameters does not deliver %s", claimed)
}

func TestConflictInsideFolderWithoutManifestIsOnlyAConflict(t *testing.T) {
	repo := cleanRepo(t)
	put(repo, "go-srv/internal/platform/billing/billing.go", "// Package billing is a module folder whose manifest was forgotten.\npackage billing\n")
	edit(t, repo, paramsManifest, `["go-srv/internal/platform/parameters/"]`, `["go-srv/internal/platform/billing/", "go-srv/internal/platform/parameters/"]`)
	edit(t, repo, flagsManifest, "\"go-srv/internal/platform/featureflags/\"\n  ],", "\"go-srv/internal/platform/billing/\",\n    \"go-srv/internal/platform/featureflags/\"\n  ],")

	_, issues := buildRepo(t, repo)
	assertCodes(t, issues, "FILE_OWNERSHIP_CONFLICT")
}

func TestUnlistedFileInFolderWithManifestIsNotUnowned(t *testing.T) {
	repo := cleanRepo(t)
	put(repo, "go-srv/internal/platform/parameters/unlisted.go", "package parameters\n")

	_, issues := buildRepo(t, repo)
	assertCodes(t, issues)
}

func TestCooperatesWith(t *testing.T) {
	addTenancy := func(r fstest.MapFS) {
		put(r, "specs/tenancy/spec.manifest.json", `{ "schema": 1, "spec": "tenancy", "version": "0.1.0", "tckTags": ["@tenancy"], "tckEnv": {} }`+"\n")
		put(r, "specs/tenancy/openapi.yaml", "openapi: 3.1.0\n")
	}
	cases := []struct {
		name     string
		mutate   func(r fstest.MapFS)
		wantCode string
		wantPath string
	}{
		{"tag owned by another spec", func(r fstest.MapFS) {
			addTenancy(r)
			edit(t, r, "specs/parameters/spec.manifest.json", `"tckEnv": {}`, `"tckEnv": {}, "cooperatesWith": ["@tenancy"]`)
		}, "SPEC_UNIMPLEMENTED", ""},
		{"tag owned by no spec", func(r fstest.MapFS) {
			edit(t, r, "specs/parameters/spec.manifest.json", `"tckEnv": {}`, `"tckEnv": {}, "cooperatesWith": ["@ghost"]`)
		}, "TCK_TAG_UNKNOWN", "specs/parameters/spec.manifest.json#/cooperatesWith/0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := cleanRepo(t)
			tc.mutate(repo)
			built, issues := buildRepo(t, repo)
			if tc.wantPath == "" {
				// The only issue is the unimplemented tenancy spec; no
				// TCK_TAG_UNKNOWN means the cooperating tag resolved.
				assertCodes(t, issues, tc.wantCode)
				for _, entry := range built.Specs {
					if entry.Spec == "parameters" && strings.Join(entry.CooperatesWith, ",") != "@tenancy" {
						t.Errorf("cooperatesWith = %v", entry.CooperatesWith)
					}
				}
				return
			}
			assertCodes(t, issues, tc.wantCode)
			if issues[0].Path != tc.wantPath {
				t.Errorf("path = %q, want %q", issues[0].Path, tc.wantPath)
			}
		})
	}
}

func TestCatalogWithoutCooperationHasNoCooperatesWithKey(t *testing.T) {
	built, issues := buildRepo(t, cleanRepo(t))
	assertCodes(t, issues)
	encoded, err := Encode(built)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "cooperatesWith") {
		t.Fatalf("catalog mentions cooperatesWith:\n%s", encoded)
	}
}

func TestCatalogRoundTripKeepsCooperatesWith(t *testing.T) {
	built, _ := buildRepo(t, cleanRepo(t))
	built.Specs[0].CooperatesWith = []string{"@parameters"}
	encoded, err := Encode(built)
	if err != nil {
		t.Fatal(err)
	}
	decoded, issues := Decode(encoded)
	if len(issues) > 0 {
		t.Fatalf("decode: %v", issues)
	}
	if strings.Join(decoded.Specs[0].CooperatesWith, ",") != "@parameters" {
		t.Fatalf("decoded = %v", decoded.Specs[0].CooperatesWith)
	}
	built.Specs[0].CooperatesWith = []string{"@b", "@a"}
	unsorted, err := Encode(built)
	if err != nil {
		t.Fatal(err)
	}
	if _, issues := Decode(unsorted); len(issues) == 0 {
		t.Fatal("unsorted cooperatesWith was accepted")
	}
}

func TestImpliedAndCoreRequirementMessages(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(t *testing.T, repo fstest.MapFS)
		path    string
		message string
	}{
		{"module other than core implied", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, identityManifest, `"implied": false`, `"implied": true`)
		}, identityManifest + "#/implied", "only core may be implied; select the module or require it through requiresModules"},
		{"core not implied", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, coreManifest, `"implied": true`, `"implied": false`)
		}, coreManifest + "#/implied", "core must be implied"},
		{"core requires a module", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, coreManifest, `"requiresModules": [],`, `"requiresModules": [{"module": "identity", "range": "^0.1.0"}],`)
		}, coreManifest + "#/requiresModules", "core must not require another module"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := cleanRepo(t)
			tc.mutate(t, repo)
			_, issues := buildRepo(t, repo)
			for _, found := range issues {
				if found.Path == tc.path && found.Message == tc.message {
					return
				}
			}
			t.Fatalf("want an issue at %s saying %q, got %+v", tc.path, tc.message, issues)
		})
	}
}

func TestCooperationFolderNeedsADeclaredTag(t *testing.T) {
	const scenario = "specs/parameters/tck/with-tenancy/precedence.spec.ts"
	cases := []struct {
		name      string
		declare   bool
		wantCodes []string
	}{
		// The tenancy spec exists only in this test and no module implements it,
		// so SPEC_UNIMPLEMENTED is the one expected issue when the tag is declared.
		{"declared in cooperatesWith", true, []string{"SPEC_UNIMPLEMENTED"}},
		{"not declared in cooperatesWith", false, []string{"SPEC_UNIMPLEMENTED", "TCK_COOPERATION_UNDECLARED"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := cleanRepo(t)
			put(repo, "specs/tenancy/spec.manifest.json", `{ "schema": 1, "spec": "tenancy", "version": "0.1.0", "tckTags": ["@tenancy"], "tckEnv": {} }`+"\n")
			put(repo, "specs/tenancy/openapi.yaml", "openapi: 3.1.0\n")
			put(repo, scenario, "export {};\n")
			if tc.declare {
				edit(t, repo, "specs/parameters/spec.manifest.json", `"tckEnv": {}`, `"tckEnv": {}, "cooperatesWith": ["@tenancy"]`)
			}
			built, issues := buildRepo(t, repo)
			assertCodes(t, issues, tc.wantCodes...)
			if !tc.declare {
				for _, found := range issues {
					if found.Code == CodeTCKCooperationUndeclared && found.Path != scenario {
						t.Errorf("path = %q, want %q", found.Path, scenario)
					}
				}
				return
			}
			for _, entry := range built.Specs {
				if entry.Spec != "parameters" {
					continue
				}
				found := false
				for _, file := range entry.Files {
					found = found || file.Path == scenario
				}
				if !found {
					t.Errorf("the catalog lost %s", scenario)
				}
			}
		})
	}
}

func TestCooperationTag(t *testing.T) {
	cases := []struct {
		path string
		tag  string
		ok   bool
	}{
		{"specs/parameters/tck/with-tenancy/precedence.spec.ts", "@tenancy", true},
		{"specs/api-keys/tck/with-tenancy/nested/deeper.spec.ts", "@tenancy", true},
		{"specs/parameters/tck/with-feature-flags/a.spec.ts", "@feature-flags", true},
		{"specs/parameters/tck/with-tenancy", "", false},
		{"specs/parameters/tck/with-tenancy/", "", false},
		{"specs/parameters/tck/precedence.spec.ts", "", false},
		{"specs/parameters/tck/with-Tenancy/a.spec.ts", "", false},
		{"specs/parameters/with-tenancy/a.spec.ts", "", false},
		{"specs/tck-support/actors/tenants.ts", "", false},
		{"go-srv/specs/parameters/tck/with-tenancy/a.spec.ts", "", false},
	}
	for _, tc := range cases {
		tag, ok := CooperationTag(tc.path)
		if tag != tc.tag || ok != tc.ok {
			t.Errorf("CooperationTag(%q) = %q, %v; want %q, %v", tc.path, tag, ok, tc.tag, tc.ok)
		}
	}
}

func TestRequirementParityMessageNamesMissingAndExtra(t *testing.T) {
	cases := []struct {
		name        string
		mutate      func(t *testing.T, repo fstest.MapFS)
		wantMessage string
	}{
		{"edge only the web manifest declares", func(t *testing.T, r fstest.MapFS) {
			addNgCore(r)
			addNgModule(r, "parameters", false, `{"parameters": "0.1.0"}`)
			addNgModule(r, "feature-flags", false, `{"feature-flags": "0.1.0"}`)
			edit(t, r, ngRoot+"parameters/module.manifest.json", `"requiresModules": [],`, `"requiresModules": [{"module": "feature-flags", "range": "^0.1.0"}],`)
		}, "missing [], extra [feature-flags]"},
		{"edge only the backend manifest declares", func(t *testing.T, r fstest.MapFS) {
			addNgCore(r)
			addNgModule(r, "parameters", false, `{"parameters": "0.1.0"}`)
			edit(t, r, paramsManifest, `"requiresModules": [],`, `"requiresModules": [{"module": "feature-flags", "range": "^0.1.0"}],`)
		}, "missing [feature-flags], extra []"},
		{"each manifest declares a different module", func(t *testing.T, r fstest.MapFS) {
			addNgCore(r)
			addNgModule(r, "parameters", false, `{"parameters": "0.1.0"}`)
			addNgModule(r, "feature-flags", false, `{"feature-flags": "0.1.0"}`)
			addNgModule(r, "identity", false, `{"identity": "1.0.0"}`)
			edit(t, r, paramsManifest, `"requiresModules": [],`, `"requiresModules": [{"module": "feature-flags", "range": "^0.1.0"}],`)
			edit(t, r, ngRoot+"parameters/module.manifest.json", `"requiresModules": [],`, `"requiresModules": [{"module": "identity", "range": "^0.1.0"}],`)
		}, "missing [feature-flags], extra [identity]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := cleanRepo(t)
			tc.mutate(t, repo)
			_, issues := buildRepo(t, repo)
			assertCodes(t, issues, "REQUIRES_STACK_MISMATCH")
			wantManifest := ngRoot + "parameters/module.manifest.json#/requiresModules"
			if issues[0].Path != wantManifest {
				t.Fatalf("path = %q, want %q", issues[0].Path, wantManifest)
			}
			if !strings.Contains(issues[0].Message, tc.wantMessage) {
				t.Fatalf("message = %q, want it to contain %q", issues[0].Message, tc.wantMessage)
			}
		})
	}
}

func TestRequirementParityAllowsDifferentRanges(t *testing.T) {
	repo := cleanRepo(t)
	addNgCore(repo)
	addNgModule(repo, "parameters", false, `{"parameters": "0.1.0"}`)
	edit(t, repo, paramsManifest, `"requiresModules": [],`, `"requiresModules": [{"module": "feature-flags", "range": "^0.1.0"}],`)
	edit(t, repo, ngRoot+"parameters/module.manifest.json", `"requiresModules": [],`, `"requiresModules": [{"module": "feature-flags", "range": "0.1.0"}],`)
	addNgModule(repo, "feature-flags", false, `{"feature-flags": "0.1.0"}`)
	_, issues := buildRepo(t, repo)
	assertCodes(t, issues)
}
