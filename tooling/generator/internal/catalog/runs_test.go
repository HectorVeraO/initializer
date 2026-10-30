package catalog

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestMarkerRunInvalid(t *testing.T) {
	const moduleBlock = "# initializer:module identity\nsvc: a\n# initializer:end identity\n"
	const stackBlock = "# initializer:stack ng-web\nsvc: b\n# initializer:end ng-web\n"
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"module block directly followed by a stack block outside a run", "name: fixture-app\n" + moduleBlock + stackBlock, []string{"MARKER_RUN_INVALID"}},
		{"a blank line between them is fine", "name: fixture-app\n" + moduleBlock + "\n" + stackBlock, nil},
		{"stack block first starts a run, module then stack inside it is the interleave",
			"name: fixture-app\n# initializer:stack go-srv\na: 1\n# initializer:end go-srv\n" + moduleBlock + stackBlock, nil},
		{"two module blocks in a row are not a run", "name: fixture-app\n" + moduleBlock + "# initializer:module parameters\nb: 2\n# initializer:end parameters\n", nil},
		{"a stack block followed by a module block is a run", "name: fixture-app\n" + stackBlock + moduleBlock, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := cleanRepo(t)
			put(repo, composeFile, tc.text)
			_, issues := buildRepo(t, repo)
			assertCodes(t, issues, tc.want...)
			for _, found := range issues {
				if !strings.Contains(found.Message, "put the stack block first") || found.Path != composeFile {
					t.Errorf("issue = %+v", found)
				}
			}
		})
	}
}

func portRangeRepo(t *testing.T, path, text string) fstest.MapFS {
	t.Helper()
	repo := portsRepo(t)
	put(repo, composeFile, text)
	if path != composeFile {
		put(repo, path, text)
		edit(t, repo, coreManifest, `"portFiles": ["`+composeFile+`"]`, `"portFiles": ["`+composeFile+`", "`+path+`"]`)
	}
	return repo
}

func TestPortRangeInRun(t *testing.T) {
	const rangeText = "10100-10199"
	cases := []struct {
		name string
		path string
		text string
		want []string
	}{
		{"range outside every run", composeFile, "name: fixture-app\nHost ports " + rangeText + ".\n# initializer:stack go-srv\napi: 10100\n# initializer:end go-srv\n", nil},
		{"range inside a run", composeFile, "name: fixture-app\n# initializer:stack go-srv\nHost ports " + rangeText + ".\n# initializer:end go-srv\n", []string{"PORT_RANGE_IN_RUN"}},
		{"range inside the second block of a run", composeFile,
			"name: fixture-app\n# initializer:stack go-srv\na: 1\n# initializer:end go-srv\n# initializer:module identity\n" + rangeText + "\n# initializer:end identity\n", []string{"PORT_RANGE_IN_RUN"}},
		{"range in a module block outside a run", composeFile, "name: fixture-app\n# initializer:module identity\n" + rangeText + "\n# initializer:end identity\n", nil},
		{"another band's range is plain text", composeFile, "name: fixture-app\n# initializer:stack go-srv\n11000-11099\n# initializer:end go-srv\n", []string{"PORT_BAND_MIXED"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, issues := buildRepo(t, portRangeRepo(t, tc.path, tc.text))
			assertCodes(t, issues, tc.want...)
		})
	}
}

func TestPortRangeInAComponentScopeFile(t *testing.T) {
	const proxy = "ng-web/proxy.conf.json"
	for _, tc := range []struct {
		name string
		text string
		want []string
	}{
		{"range", "{\"band\": \"10100-10199\"}\n", []string{"PORT_RANGE_IN_RUN"}},
		{"single port", "{\"target\": \"http://localhost:10100\"}\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := portsRepo(t)
			addNgCore(repo)
			edit(t, repo, ngRoot+"core/module.manifest.json", `"requiresModules": [],`, `"requiresModules": [], "portFiles": ["`+proxy+`"],`)
			put(repo, proxy, tc.text)
			_, issues := buildRepo(t, repo)
			assertCodes(t, issues, tc.want...)
		})
	}
}
