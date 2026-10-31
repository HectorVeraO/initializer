package catalog

import (
	"strings"
	"testing"
	"testing/fstest"
)

const (
	referenceFile = ".initializer/reference.json"
	projectRule   = `{"path": ".deploy/local/docker-compose.yml", "text": "name: %{project}"}`
)

// setRules replaces the names of the core manifest by the JSON rule objects.
func setRules(t *testing.T, repo fstest.MapFS, rules ...string) {
	t.Helper()
	edit(t, repo, coreManifest, `"names": [`+projectRule+`]`, `"names": [`+strings.Join(rules, ", ")+`]`)
}

func TestNameRuleIssueCodes(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, repo fstest.MapFS)
		want   []string
	}{
		{"unknown slot", func(t *testing.T, r fstest.MapFS) {
			setRules(t, r, `{"path": "`+composeFile+`", "text": "name: %{bogus}"}`)
		}, []string{"NAME_RULE_INVALID"}},
		{"no placeholder", func(t *testing.T, r fstest.MapFS) {
			setRules(t, r, `{"path": "`+composeFile+`", "text": "name: fixture-app"}`)
		}, []string{"NAME_RULE_INVALID"}},
		{"component slot of a stack outside the table", func(t *testing.T, r fstest.MapFS) {
			setRules(t, r, `{"path": "`+composeFile+`", "text": "name: %{component.rust-srv}"}`)
		}, []string{"NAME_RULE_INVALID"}},
		{"text holds the go module", func(t *testing.T, r fstest.MapFS) {
			setRules(t, r, `{"path": "`+composeFile+`", "text": "`+fixtureModule+` %{project}"}`)
		}, []string{"GO_MODULE_PATH_LEAK", "NAME_RULE_INVALID"}},
		{"duplicate rule", func(t *testing.T, r fstest.MapFS) {
			setRules(t, r, projectRule, projectRule)
		}, []string{"NAME_RULE_INVALID"}},
		{"file owned by another module", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, identityManifest, `"exclude": []`, `"exclude": [], "names": [`+projectRule+`]`)
		}, []string{"NAME_RULE_FOREIGN"}},
		{"reference expansion missing from the file", func(t *testing.T, r fstest.MapFS) {
			put(r, composeFile, "name: other\n\nservices: {}\n")
		}, []string{"NAME_RULE_UNMATCHED"}},
		{"two rules overlap", func(t *testing.T, r fstest.MapFS) {
			setRules(t, r, projectRule, `{"path": "`+composeFile+`", "text": "e: %{project}"}`)
		}, []string{"NAME_RULE_OVERLAP"}},
		{"occurrence touches a marker line", func(t *testing.T, r fstest.MapFS) {
			put(r, composeFile, "name: fixture-app\n# initializer:module identity\nx: 1\n# initializer:end identity\n")
			setRules(t, r, projectRule, `{"path": "`+composeFile+`", "text": "%{project}\n# initializer:module identity"}`)
		}, []string{"NAME_RULE_OVERLAP"}},
		{"rules without a reference file", func(t *testing.T, r fstest.MapFS) {
			delete(r, referenceFile)
		}, []string{"REFERENCE_INVALID"}},
		{"reference is not JSON", func(t *testing.T, r fstest.MapFS) {
			put(r, referenceFile, "{")
		}, []string{"REFERENCE_INVALID"}},
		{"reference has schema 1", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, referenceFile, `"schema": 2`, `"schema": 1`)
		}, []string{"REFERENCE_INVALID"}},
		{"reference has an unknown key", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, referenceFile, `"schema": 2`, `"schema": 2, "bogus": 1`)
		}, []string{"REFERENCE_INVALID"}},
		{"reference component of a stack outside the table", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, referenceFile, `"stack": "ng-web"`, `"stack": "rust-srv"`)
		}, []string{"REFERENCE_INVALID"}},
		{"reference lacks a stack of the table", func(t *testing.T, r fstest.MapFS) {
			put(r, referenceFile, "{\"schema\": 2, \"project\": {\"name\": \"fixture-app\"}, \"components\": [{\"stack\": \"go-srv\", \"name\": \"go-srv\"}]}\n")
		}, []string{"REFERENCE_INVALID"}},
		{"reference value breaks the label grammar", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, referenceFile, `"name": "fixture-app"`, `"name": "Fixture_App"`)
		}, []string{"REFERENCE_INVALID"}},
		{"reference lists a stack twice", func(t *testing.T, r fstest.MapFS) {
			put(r, referenceFile, "{\"schema\": 2, \"project\": {\"name\": \"fixture-app\"}, \"components\": [{\"stack\": \"go-srv\", \"name\": \"go-srv\"}, {\"stack\": \"go-srv\", \"name\": \"go-srv\"}, {\"stack\": \"ng-web\", \"name\": \"ng-web\"}]}\n")
		}, []string{"REFERENCE_INVALID"}},
		{"reference values conflict", func(t *testing.T, r fstest.MapFS) {
			put(r, referenceFile, "{\"schema\": 2, \"project\": {\"name\": \"fixture-app\"}, \"components\": [{\"stack\": \"go-srv\", \"name\": \"same\"}, {\"stack\": \"ng-web\", \"name\": \"same\"}]}\n")
		}, []string{"REFERENCE_INVALID"}},
		{"rule uses a component the reference does not name", func(t *testing.T, r fstest.MapFS) {
			put(r, referenceFile, "{\"schema\": 2, \"project\": {\"name\": \"fixture-app\"}, \"components\": [{\"stack\": \"go-srv\", \"name\": \"go-srv\"}, {\"stack\": \"ng-web\"}]}\n")
			setRules(t, r, projectRule, `{"path": "`+composeFile+`", "text": "web: %{component.ng-web}"}`)
		}, []string{"REFERENCE_INCOMPLETE"}},
		{"rule uses a slot no reference input sets yet", func(t *testing.T, r fstest.MapFS) {
			setRules(t, r, projectRule, `{"path": "`+composeFile+`", "text": "db: %{database}"}`)
		}, []string{"REFERENCE_INCOMPLETE"}},
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

func TestNameRuleIssuesAreCollectedTogether(t *testing.T) {
	repo := cleanRepo(t)
	put(repo, composeFile, "name: other\n")
	setRules(t, repo, projectRule, `{"path": "`+composeFile+`", "text": "n: %{bogus}"}`, `{"path": "`+composeFile+`", "text": "db: %{database}"}`)
	delete(repo, referenceFile)
	_, issues := buildRepo(t, repo)
	assertCodes(t, issues, "NAME_RULE_INVALID", "REFERENCE_INVALID")
}

func TestReferenceFileIsNeverCatalogued(t *testing.T) {
	built, issues := buildRepo(t, cleanRepo(t))
	assertCodes(t, issues)
	for _, stack := range built.Stacks {
		for _, module := range stack.Modules {
			if hasFile(module.Files, referenceFile) {
				t.Fatalf("%s of %s lists the reference file", stack.Stack, module.Module)
			}
		}
	}
}

func TestCatalogRecordsTheReferenceSlotMap(t *testing.T) {
	built, _ := buildRepo(t, cleanRepo(t))
	want := map[string]string{
		"project": "fixture-app", "composeProject": "fixture-app", "envPrefix": "FIXTURE_APP", "component.go-srv": "go-srv", "component.ng-web": "ng-web",
		"instanceDir.go-srv": "", "instanceSuffix.go-srv": "", "task.go-srv": "srv", "task.ng-web": "web",
		"tckTask.go-srv": "tck", "tckArgs.go-srv": "",
	}
	for slot, value := range want {
		got, ok := built.Reference[slot]
		if !ok || got != value {
			t.Errorf("reference[%s] = %q (present %v), want %q", slot, got, ok, value)
		}
	}
	if len(built.Reference) != len(want) {
		t.Errorf("reference = %v", built.Reference)
	}
}

func TestRepoWithoutRulesAndReferenceHasAnEmptyReference(t *testing.T) {
	repo := cleanRepo(t)
	setRules(t, repo)
	delete(repo, referenceFile)
	built, issues := buildRepo(t, repo)
	assertCodes(t, issues)
	if len(built.Reference) != 0 {
		t.Fatalf("reference = %v, want empty", built.Reference)
	}
}

func TestAnchorMayBeginWithTheNewlineThatEndsAMarkerLine(t *testing.T) {
	repo := cleanRepo(t)
	put(repo, composeFile, "name: fixture-app\n# initializer:module identity\n  svc: fixture-app\n# initializer:end identity\n")
	setRules(t, repo, projectRule, `{"path": "`+composeFile+`", "text": "\n  svc: %{project}\n"}`)
	built, issues := buildRepo(t, repo)
	assertCodes(t, issues)
	for _, rule := range findModule(t, built, "go-srv", "core").Names {
		if strings.HasPrefix(rule.Text, "\n") && strings.Join(rule.Blocks, ",") != "module:identity" {
			t.Fatalf("an anchor that starts with the marker newline belongs to the block after it: %+v", rule)
		}
	}
}

func TestRecordedBlocksNameTheEnclosingMarkerBlocks(t *testing.T) {
	repo := cleanRepo(t)
	put(repo, composeFile, strings.Join([]string{
		"name: fixture-app",
		"a: fixture-app",
		"# initializer:module identity",
		"b: fixture-app",
		"# initializer:end identity",
		"",
		"# initializer:stack ng-web",
		"c: fixture-app",
		"# initializer:end ng-web",
		"d: fixture-app",
		"",
	}, "\n"))
	setRules(t, repo,
		projectRule,
		`{"path": "`+composeFile+`", "text": "a: %{project}"}`,
		`{"path": "`+composeFile+`", "text": "b: %{project}"}`,
		`{"path": "`+composeFile+`", "text": "c: %{project}"}`,
		`{"path": "`+composeFile+`", "text": "d: %{project}"}`)
	built, issues := buildRepo(t, repo)
	assertCodes(t, issues)
	got := map[string]string{}
	for _, rule := range findModule(t, built, "go-srv", "core").Names {
		got[rule.Text] = strings.Join(rule.Blocks, ",")
	}
	want := map[string]string{
		"name: %{project}": "", "a: %{project}": "", "b: %{project}": "module:identity",
		"c: %{project}": "stack:ng-web", "d: %{project}": "",
	}
	for text, blocks := range want {
		if got[text] != blocks {
			t.Errorf("blocks of %q = %q, want %q", text, got[text], blocks)
		}
	}
}

func TestRuleSpanningBlocksRecordsEveryBlockOfItsOccurrences(t *testing.T) {
	repo := cleanRepo(t)
	put(repo, composeFile, strings.Join([]string{
		"name: fixture-app",
		"x: fixture-app",
		"# initializer:module identity",
		"x: fixture-app",
		"# initializer:end identity",
		"",
	}, "\n"))
	setRules(t, repo, projectRule, `{"path": "`+composeFile+`", "text": "x: %{project}"}`)
	built, issues := buildRepo(t, repo)
	assertCodes(t, issues)
	found := false
	for _, rule := range findModule(t, built, "go-srv", "core").Names {
		if rule.Text == "x: %{project}" {
			found = true
			if strings.Join(rule.Blocks, ",") != ",module:identity" {
				t.Fatalf("blocks = %q", rule.Blocks)
			}
		}
	}
	if !found {
		t.Fatalf("the spanning rule is missing from Names")
	}
}

func TestRulesAreSortedByPathThenText(t *testing.T) {
	repo := cleanRepo(t)
	put(repo, composeFile, "name: fixture-app\nz: fixture-app\na: fixture-app\n")
	setRules(t, repo,
		`{"path": "`+composeFile+`", "text": "z: %{project}"}`,
		projectRule,
		`{"path": "`+composeFile+`", "text": "a: %{project}"}`)
	built, _ := buildRepo(t, repo)
	var texts []string
	for _, rule := range findModule(t, built, "go-srv", "core").Names {
		texts = append(texts, rule.Text)
	}
	if strings.Join(texts, "|") != "a: %{project}|name: %{project}|z: %{project}" {
		t.Fatalf("texts = %v", texts)
	}
}
