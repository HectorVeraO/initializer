package names_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
)

func TestSnakeAndEnvPrefix(t *testing.T) {
	cases := []struct{ in, snake, prefix string }{
		{"acme-backend", "acme_backend", "ACME_BACKEND"},
		{"initializer", "initializer", "INITIALIZER"},
		{"a-b-c", "a_b_c", "A_B_C"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := names.Snake(c.in); got != c.snake {
			t.Errorf("Snake(%q) = %q, want %q", c.in, got, c.snake)
		}
		if got := names.EnvPrefix(c.in); got != c.prefix {
			t.Errorf("EnvPrefix(%q) = %q, want %q", c.in, got, c.prefix)
		}
	}
}

func TestIsSlot(t *testing.T) {
	cases := []struct {
		name   string
		stacks []string
		want   bool
	}{
		{"project", nil, true},
		{"displayName", nil, true},
		{"database", nil, true},
		{"dbUser", nil, true},
		{"envPrefix", nil, true},
		{"composeProject", nil, true},
		{"component.go-srv", []string{"go-srv"}, true},
		{"component.go-srv", nil, false},
		{"component.ng-web", []string{"go-srv"}, false},
		{"service.postgres-init", nil, true},
		{"service.Bad", nil, false},
		{"service.", nil, false},
		{"bogus", nil, false},
		{"", nil, false},
		{"Project", nil, false},
	}
	for _, c := range cases {
		if got := names.IsSlot(c.name, c.stacks); got != c.want {
			t.Errorf("IsSlot(%q, %v) = %v, want %v", c.name, c.stacks, got, c.want)
		}
	}
}

func TestSlots(t *testing.T) {
	valid := []struct {
		text string
		want []string
	}{
		{"name: %{project}", []string{"project"}},
		{"POSTGRES_USER: %{dbUser} %{database} %{dbUser}", []string{"database", "dbUser"}},
		{"go -C %{component.go-srv} run", []string{"component.go-srv"}},
		{"\n  %{service.postgres-init}:\n", []string{"service.postgres-init"}},
		{"%%{literal} %{project}", []string{"project"}},
		{"100% %{project}", []string{"project"}},
	}
	for _, c := range valid {
		got, err := names.Slots(c.text)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("Slots(%q) = %v, %v; want %v", c.text, got, err, c.want)
		}
	}
	invalid := map[string]string{
		"unknown slot":   "name: %{bogus}",
		"unterminated":   "name: %{project",
		"no placeholder": "name: project",
		"only escaped":   "name: %%{project}",
		"blank literal":  " \t%{project}\t ",
		"bare slot":      "%{project}",
		"empty slot":     "name: %{}",
		"empty text":     "",
	}
	for name, text := range invalid {
		if got, err := names.Slots(text); err == nil {
			t.Errorf("%s: Slots(%q) = %v, want an error", name, text, got)
		}
	}
}

func TestExpand(t *testing.T) {
	values := map[string]string{"project": "acme", "dbUser": "acme_user"}
	got, err := names.Expand("name: %{project} 100% %%{x} %{dbUser}", values)
	if err != nil || got != "name: acme 100% %{x} acme_user" {
		t.Fatalf("Expand = %q, %v", got, err)
	}
	if _, err := names.Expand("name: %{database}", values); err == nil {
		t.Fatal("Expand must fail when values lacks a used slot")
	}
	if _, err := names.Expand("name: %{project", values); err == nil {
		t.Fatal("Expand must fail on an unterminated placeholder")
	}
}

var reference = map[string]string{"project": "initializer", "dbUser": "initializer", "service.postgres": "postgres"}

func TestFindReturnsSortedOccurrences(t *testing.T) {
	src := []byte("name: initializer\nuser: initializer\nname: initializer\n")
	rules := []names.Rule{
		{Path: "f", Text: "user: %{dbUser}"},
		{Path: "f", Text: "name: %{project}"},
	}
	got, err := names.Find(src, rules, reference)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	want := []names.Occurrence{{Start: 0, End: 17, Rule: 1}, {Start: 18, End: 35, Rule: 0}, {Start: 36, End: 53, Rule: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("occurrences = %+v, want %+v", got, want)
	}
}

func TestFindReportsOverlaps(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		rules  []names.Rule
		values map[string]string
	}{
		{"different rules", "name: initializer\n", []names.Rule{{Text: "name: %{project}"}, {Text: ": %{project}\n"}}, reference},
		{"same rule twice", "name: initializer\n", []names.Rule{{Text: "name: %{project}"}, {Text: "name: %{project}"}}, reference},
		{"one rule overlapping itself", "aaa", []names.Rule{{Text: "a%{dbUser}"}}, map[string]string{"dbUser": "a"}},
	}
	for _, c := range cases {
		if _, err := names.Find([]byte(c.src), c.rules, c.values); err == nil {
			t.Errorf("%s: Find must report the overlap", c.name)
		}
	}
}

func TestFindAcceptsTouchingOccurrences(t *testing.T) {
	src := []byte("name: initializername: initializer")
	got, err := names.Find(src, []names.Rule{{Text: "name: %{project}"}}, reference)
	if err != nil || len(got) != 2 {
		t.Fatalf("Find = %+v, %v; want two adjacent occurrences", got, err)
	}
}

func TestFindRejectsMissingReferenceSlot(t *testing.T) {
	if _, err := names.Find([]byte("x"), []names.Rule{{Text: "x %{database}"}}, reference); err == nil {
		t.Fatal("Find must fail when the reference lacks a used slot")
	}
}

func TestApplyRewritesEveryOccurrence(t *testing.T) {
	src := []byte("name: initializer\nkeep: initializer\n  postgres:\nname: initializer\n")
	rules := []names.Rule{
		{Text: "name: %{project}"},
		{Text: "\n  %{service.postgres}:\n"},
	}
	to := map[string]string{"project": "acme-shop", "service.postgres": "acme-shop-postgres"}
	got, err := names.Apply(src, rules, reference, to)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := "name: acme-shop\nkeep: initializer\n  acme-shop-postgres:\nname: acme-shop\n"
	if string(got) != want {
		t.Fatalf("Apply = %q, want %q", got, want)
	}
}

func TestApplyWithEqualValuesKeepsTheBytes(t *testing.T) {
	src := []byte("name: initializer\nuser: initializer\n")
	rules := []names.Rule{{Text: "name: %{project}"}, {Text: "user: %{dbUser}"}}
	got, err := names.Apply(src, rules, reference, reference)
	if err != nil || !bytes.Equal(got, src) {
		t.Fatalf("Apply = %q, %v; want the source bytes", got, err)
	}
}

func TestApplyFailsWhenTheTargetLacksASlot(t *testing.T) {
	src := []byte("name: initializer\n")
	if _, err := names.Apply(src, []names.Rule{{Text: "name: %{project}"}}, reference, map[string]string{}); err == nil {
		t.Fatal("Apply must fail when the target values lack a used slot")
	}
}

func TestResolvedSlotsIncludeDerivedAndSkipEmpty(t *testing.T) {
	got := names.Resolved{
		Project:    "acme-backend",
		Components: map[string]string{"go-srv": "acme-backend-srv"},
	}.Slots()
	want := map[string]string{
		"project": "acme-backend", "composeProject": "acme-backend", "envPrefix": "ACME_BACKEND", "component.go-srv": "acme-backend-srv",
		"instanceDir.go-srv": "", "instanceEnvSuffix.go-srv": "", "instanceSuffix.go-srv": "",
		"composeEnvUse.go-srv": "reads it for its `${...}` values and passes it to", "task.go-srv": "srv",
		"tckTask.go-srv": "tck", "tckArgs.go-srv": "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Slots = %v, want %v", got, want)
	}
}

func TestComposeProjectCarriesThePortBand(t *testing.T) {
	cases := []struct {
		project string
		band    int
		want    string
	}{
		{"acme", 101, "acme-101"},
		{"acme", 103, "acme-103"},
		{"acme-backend", 654, "acme-backend-654"},
		{"initializer", 0, "initializer"},
	}
	for _, c := range cases {
		got := names.Resolved{Project: c.project, Band: c.band}.Slots()["composeProject"]
		if got != c.want || names.ComposeProject(c.project, c.band) != c.want {
			t.Errorf("composeProject(%q, %d) = %q, want %q", c.project, c.band, got, c.want)
		}
	}
	if names.IsRecorded("composeProject") || !names.IsProjectLevel("composeProject") {
		t.Error("composeProject must be derived (never recorded) and project-level")
	}
}

func TestInstanceSlotsTakeTheirMultiForm(t *testing.T) {
	got := names.Resolved{
		Project:    "acme",
		Components: map[string]string{"go-srv": "acme-srv-first", "ng-web": "acme-web"},
		Multi:      true,
	}.Slots()
	want := map[string]string{
		"instanceDir.go-srv": "acme-srv-first/", "instanceSuffix.go-srv": "-acme-srv-first",
		"instanceEnvSuffix.go-srv": "_ACME_SRV_FIRST", "composeEnvUse.go-srv": "passes it to",
		"task.go-srv": "acme-srv-first", "task.ng-web": "acme-web",
		"tckTask.go-srv": `"tck:acme-srv-first"`, "tckArgs.go-srv": " -- --component acme-srv-first",
	}
	for slot, value := range want {
		if got[slot] != value {
			t.Errorf("%s = %q, want %q", slot, got[slot], value)
		}
	}
}

func TestDerivedSlotsAreNeverRecorded(t *testing.T) {
	for _, slot := range []string{"instanceDir.go-srv", "instanceEnvSuffix.go-srv", "instanceSuffix.go-srv", "composeEnvUse.go-srv", "task.go-srv", "task.ng-web", "tckTask.go-srv", "tckArgs.go-srv"} {
		if names.IsRecorded(slot) {
			t.Errorf("%s is recorded", slot)
		}
		if !names.IsSlot(slot, []string{"go-srv", "ng-web"}) {
			t.Errorf("%s is not a slot", slot)
		}
	}
	for _, slot := range []string{"project", "envPrefix", "component.go-srv", "service.postgres"} {
		if !names.IsRecorded(slot) {
			t.Errorf("%s is not recorded", slot)
		}
	}
	if names.IsSlot("task.unknown-stack", []string{"go-srv"}) {
		t.Error("task.<unknown stack> is a slot")
	}
}

func TestDerivedDefaultIsTheSingleForm(t *testing.T) {
	for slot, want := range map[string]string{"task.ng-web": "web", "tckTask.go-srv": "tck", "instanceDir.go-srv": ""} {
		got, ok := names.DerivedDefault(slot)
		if !ok || got != want {
			t.Errorf("DerivedDefault(%s) = %q, %v, want %q", slot, got, ok, want)
		}
	}
	if _, ok := names.DerivedDefault("project"); ok {
		t.Error("project has a derived default")
	}
}

func TestReferenceReturnsTheSlotMap(t *testing.T) {
	got, issues := names.Reference(names.Resolved{
		Project:    "initializer",
		Components: map[string]string{"go-srv": "go-srv", "ng-web": "ng-web"},
	})
	want := map[string]string{
		"project": "initializer", "composeProject": "initializer", "envPrefix": "INITIALIZER",
		"component.go-srv": "go-srv", "component.ng-web": "ng-web",
		"instanceDir.go-srv": "", "instanceEnvSuffix.go-srv": "", "instanceSuffix.go-srv": "",
		"composeEnvUse.go-srv": "reads it for its `${...}` values and passes it to", "task.go-srv": "srv", "task.ng-web": "web",
		"tckTask.go-srv": "tck", "tckArgs.go-srv": "",
	}
	if len(issues) > 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("Reference = %v, %+v", got, issues)
	}
}

func TestReferenceReportsInvalidAndConflictingValues(t *testing.T) {
	_, issues := names.Reference(names.Resolved{
		Project:    "Bad_Name",
		Components: map[string]string{"go-srv": "same", "ng-web": "same", "x-y": "-bad"},
	})
	if len(issues) != 3 {
		t.Fatalf("issues = %+v, want the project, the bad component and the conflict", issues)
	}
	for _, found := range issues {
		if found.Code != "REFERENCE_INVALID" || !strings.HasPrefix(found.Path, ".initializer/reference.json") {
			t.Errorf("unexpected issue %+v", found)
		}
	}
}

func FuzzSlots(f *testing.F) {
	for _, seed := range []string{"name: %{project}", "%%{x} %{dbUser}", "%{", "%{}", "a%{service.postgres}:", "%", "100%%{"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		slots, err := names.Slots(text)
		if err != nil {
			return
		}
		values := map[string]string{}
		for _, slot := range slots {
			values[slot] = "v"
		}
		expanded, err := names.Expand(text, values)
		if err != nil {
			t.Fatalf("accepted text %q does not expand: %v", text, err)
		}
		if expanded == "" {
			t.Fatalf("accepted text %q expands to nothing", text)
		}
	})
}

func FuzzFind(f *testing.F) {
	f.Add([]byte("name: initializer\n"), "name: %{project}", "x%{project}")
	f.Add([]byte("aaaa"), "%{dbUser}%{dbUser}", "%{dbUser}")
	f.Add([]byte(""), "a%{project}", "b%{project}")
	values := map[string]string{"project": "a", "dbUser": "aa"}
	f.Fuzz(func(t *testing.T, src []byte, first, second string) {
		rules := []names.Rule{{Text: first}, {Text: second}}
		found, err := names.Find(src, rules, values)
		if err != nil {
			return
		}
		previousEnd := 0
		for _, occurrence := range found {
			if occurrence.Start < previousEnd || occurrence.End <= occurrence.Start || occurrence.End > len(src) {
				t.Fatalf("bad occurrence %+v after end %d in %d bytes", occurrence, previousEnd, len(src))
			}
			previousEnd = occurrence.End
		}
	})
}

func FuzzApply(f *testing.F) {
	f.Add([]byte("name: initializer\n"), "name: %{project}")
	f.Add([]byte("a a a"), "a %{project}")
	f.Add([]byte(""), "%{project}x")
	from := map[string]string{"project": "a", "dbUser": "b"}
	to := map[string]string{"project": "zz", "dbUser": "yy"}
	f.Fuzz(func(t *testing.T, src []byte, text string) {
		rules := []names.Rule{{Text: text}}
		same, err := names.Apply(src, rules, from, from)
		if err == nil && !bytes.Equal(same, src) {
			t.Fatalf("Apply with equal values changed %q into %q", src, same)
		}
		changed, err := names.Apply(src, rules, from, to)
		if err != nil {
			return
		}
		occurrences, findErr := names.Find(src, rules, from)
		if findErr != nil {
			t.Fatalf("Apply succeeded but Find failed: %v", findErr)
		}
		if len(occurrences) == 0 && !bytes.Equal(changed, src) {
			t.Fatalf("Apply changed bytes without an occurrence")
		}
	})
}
