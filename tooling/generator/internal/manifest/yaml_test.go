package manifest_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

const quoteIt = " (quote it)"

// yamlIssues parses data as YAML and decodes it as a selection, returning
// every issue of both steps.
func yamlIssues(data string) []issue.Issue {
	tree, issues := manifest.ParseYAML("acme.yaml", []byte(data))
	if tree == nil {
		return issues
	}
	_, decodeIssues := manifest.DecodeInput("acme.yaml", tree)
	return decodeIssues
}

type issueFact struct {
	Path    string
	Line    int
	Message string
}

func factsOf(issues []issue.Issue) []issueFact {
	var facts []issueFact
	for _, found := range issues {
		if found.Code != manifest.CodeSelectionInvalid {
			facts = append(facts, issueFact{Path: "BAD CODE " + found.Code})
		}
		facts = append(facts, issueFact{Path: found.Path, Line: found.Line, Message: found.Message})
	}
	return facts
}

func TestParseYAMLRefusesEachForbiddenConstructWithItsLine(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want []issueFact
	}{
		{"duplicate key", "schema: 1\nproject:\n  name: a\n  name: b\n",
			[]issueFact{{"acme.yaml#/project/name", 4, "name is given twice (first at line 3)"}}},
		{"anchor", "base: &x\n  a: 1\n",
			[]issueFact{{"acme.yaml#/base", 1, "anchors and aliases are not allowed"}}},
		{"alias", "a: &x 1\nb: *x\n",
			[]issueFact{{"acme.yaml#/a", 1, "anchors and aliases are not allowed"}, {"acme.yaml#/b", 2, "anchors and aliases are not allowed"}}},
		{"merge key", "base:\n  a: 1\nother:\n  <<: {a: 2}\n",
			[]issueFact{{"acme.yaml#/other/<<", 4, "merge keys are not allowed"}}},
		{"custom tag", "name: !secret abc\n",
			[]issueFact{{"acme.yaml#/name", 1, "tag !secret is not allowed"}}},
		{"binary", "name: !!binary aGk=\n",
			[]issueFact{{"acme.yaml#/name", 1, "tag !!binary is not allowed"}}},
		{"timestamp", "name: 2024-01-02\n",
			[]issueFact{{"acme.yaml#/name", 1, "tag !!timestamp is not allowed"}}},
		{"non-string key", "1: a\n",
			[]issueFact{{"acme.yaml#/1", 1, "keys must be strings"}}},
		{"null key", "? null\n: a\n",
			[]issueFact{{"acme.yaml#/null", 1, "keys must be strings"}}},
		{"leading zero integer", "schema: 010\n",
			[]issueFact{{"acme.yaml#/schema", 1, "must be a decimal integer"}}},
		{"hex integer", "schema: 0x1\n",
			[]issueFact{{"acme.yaml#/schema", 1, "must be a decimal integer"}}},
		{"signed integer", "schema: +1\n",
			[]issueFact{{"acme.yaml#/schema", 1, "must be a decimal integer"}}},
		{"two documents", "schema: 1\n---\nschema: 2\n",
			[]issueFact{{"acme.yaml#", 2, "the file must hold one YAML document"}}},
		{"syntax error", "schema: 1\n\tx: 1\n",
			[]issueFact{{"acme.yaml#", 2, "invalid YAML"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree, issues := manifest.ParseYAML("acme.yaml", []byte(tc.yaml))
			if tree != nil {
				t.Fatal("a tree came back with issues")
			}
			got := factsOf(issues)
			if len(got) == 1 && strings.HasPrefix(got[0].Message, "invalid YAML") {
				got[0].Message = "invalid YAML"
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestParseYAMLReportsSeveralProblemsInOneRun(t *testing.T) {
	data := "schema: 010\nname: a\nname: b\nk: c\nlist:\n  - &a 1\n  - 0x1F\n"
	_, issues := manifest.ParseYAML("acme.yaml", []byte(data))
	paths := map[string]int{}
	for _, found := range issues {
		paths[found.Path] = found.Line
	}
	want := map[string]int{
		"acme.yaml#/schema": 1, "acme.yaml#/name": 3, "acme.yaml#/list/0": 6, "acme.yaml#/list/1": 7,
	}
	for path, line := range want {
		if paths[path] != line {
			t.Errorf("%s at line %d, want %d (all: %v)", path, paths[path], line, paths)
		}
	}
}

func TestParseYAMLTypesScalarsByTag(t *testing.T) {
	tree, issues := manifest.ParseYAML("acme.yaml", []byte(
		"s: no\nq: \"123\"\ni: -12\nz: 0\nb: true\nn: null\ne:\nf: 1.5\ninf: .inf\nlist: [a, 1]\n"))
	if len(issues) != 0 {
		t.Fatalf("issues: %+v", issues)
	}
	kinds := map[string]manifest.Kind{}
	for _, member := range tree.Members {
		kinds[member.Key] = member.Value.Kind
	}
	want := map[string]manifest.Kind{
		"s": manifest.KindString, "q": manifest.KindString, "i": manifest.KindInt, "z": manifest.KindInt,
		"b": manifest.KindBool, "n": manifest.KindNull, "e": manifest.KindNull, "f": manifest.KindFloat,
		"inf": manifest.KindFloat, "list": manifest.KindSeq,
	}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds = %v", kinds)
	}
	if value, _ := tree.Member("s"); value.Value != "no" {
		t.Fatalf("no = %q", value.Value)
	}
	if value, _ := tree.Member("inf"); value.Value != "1e999" {
		t.Fatalf("inf = %q, want 1e999", value.Value)
	}
	if tree.Format != manifest.FormatYAML {
		t.Fatal("root must record the YAML format")
	}
}

func TestAnEmptyYAMLFileIsReportedByTheRules(t *testing.T) {
	for _, data := range []string{"", "# only a comment\n", "\n\n"} {
		issues := yamlIssues(data)
		if len(issues) != 1 || issues[0].Message != "must be an object" || issues[0].Path != "acme.yaml#" {
			t.Fatalf("%q: %+v", data, issues)
		}
	}
}

func TestAYAMLNumberWhereAStringBelongsAsksForQuotes(t *testing.T) {
	yamlDoc := "schema: 1\nproject:\n  name: 123\nstacks: [go-srv]\nmodules: []\n"
	jsonDoc := `{"schema":1,"project":{"name":123},"stacks":["go-srv"],"modules":[]}`
	yamlFound := yamlIssues(yamlDoc)
	if len(yamlFound) != 1 || yamlFound[0].Message != "must be a string"+quoteIt || yamlFound[0].Line != 3 ||
		yamlFound[0].Path != "acme.yaml#/project/name" {
		t.Fatalf("yaml: %+v", yamlFound)
	}
	_, jsonFound := decodeInputFile("acme.json", []byte(jsonDoc))
	if len(jsonFound) != 1 || jsonFound[0].Message != "must be a string" || jsonFound[0].Line != 0 {
		t.Fatalf("json: %+v", jsonFound)
	}
	listed := yamlIssues("schema: 1\nstacks: [go-srv]\nmodules: [true]\n")
	if len(listed) < 1 || listed[0].Path != "acme.yaml#/modules/0" || !strings.HasSuffix(listed[0].Message, quoteIt) {
		t.Fatalf("list item: %+v", listed)
	}
	notScalar := yamlIssues("schema: 1\nproject:\n  name: [a]\n")
	if len(notScalar) == 0 {
		t.Fatalf("a list under a string field must be reported: %+v", notScalar)
	}
	for _, found := range notScalar {
		if strings.Contains(found.Message, quoteIt) {
			t.Fatalf("a list is not fixed by quotes: %+v", notScalar)
		}
	}
}

// stripQuoteAdvice removes the YAML-only suffix and the lines.
func stripQuoteAdvice(issues []issue.Issue) []issueFact {
	var facts []issueFact
	for _, found := range issues {
		path := found.Path[strings.Index(found.Path, "#"):]
		facts = append(facts, issueFact{Path: path, Message: strings.TrimSuffix(found.Message, quoteIt)})
	}
	return facts
}

func TestTheSameInvalidContentGivesTheSameIssuesInBothFormats(t *testing.T) {
	cases := map[string][2]string{
		"wrong kinds, unknown key, bad name": {
			"schema: 1\nproject:\n  name: Acme_\nstacks: go-srv\nmodules: [1, \"ok\"]\nextra: 1\n",
			`{"schema":1,"project":{"name":"Acme_"},"stacks":"go-srv","modules":[1,"ok"],"extra":1}`,
		},
		"missing keys": {"schema: 1\n", `{"schema":1}`},
		"bad schema":   {"schema: 3\n", `{"schema":3}`},
		"not a map":    {"- a\n", `["a"]`},
	}
	for name, pair := range cases {
		t.Run(name, func(t *testing.T) {
			yamlFound := stripQuoteAdvice(yamlIssues(pair[0]))
			_, jsonIssues := decodeInputFile("acme.json", []byte(pair[1]))
			jsonFound := stripQuoteAdvice(jsonIssues)
			if len(yamlFound) == 0 || !reflect.DeepEqual(yamlFound, jsonFound) {
				t.Fatalf("yaml %+v\njson %+v", yamlFound, jsonFound)
			}
		})
	}
}

// toYAML renders a JSON document as block YAML.
func toYAML(t testing.TB, jsonText []byte) []byte {
	t.Helper()
	var value any
	if err := yaml.Unmarshal(jsonText, &value); err != nil {
		t.Fatalf("json as yaml: %v", err)
	}
	out, err := yaml.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// sameDecoded fails when the YAML and JSON documents decode to the same
// selection and the same issues (lines and the quoting advice aside).
func sameDecoded(t testing.TB, jsonText, yamlText []byte) {
	t.Helper()
	jsonInput, jsonIssues := decodeInputFile("sel", jsonText)
	yamlTree, parseIssues := manifest.ParseYAML("sel", yamlText)
	if len(parseIssues) > 0 {
		t.Fatalf("yaml refused: %+v\n%s", parseIssues, yamlText)
	}
	yamlInput, yamlIssuesFound := manifest.DecodeInput("sel", yamlTree)
	merged, _ := manifest.Merge(&yamlInput, mustFlags(manifest.Flags{}))
	yamlIssuesFound = append(yamlIssuesFound, manifest.CheckMerged(merged)...)
	issue.Sort(yamlIssuesFound)
	if !reflect.DeepEqual(jsonInput.Selection, yamlInput.Selection) {
		t.Fatalf("selections differ\njson %+v\nyaml %+v", jsonInput.Selection, yamlInput.Selection)
	}
	if !reflect.DeepEqual(stripQuoteAdvice(jsonIssues), stripQuoteAdvice(yamlIssuesFound)) {
		t.Fatalf("issues differ\njson %+v\nyaml %+v", jsonIssues, yamlIssuesFound)
	}
}

func TestEveryCISelectionDecodesTheSameFromItsYAMLTwin(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "ci", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no CI selections: %v", err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(filepath.Base(path), func(t *testing.T) {
			sameDecoded(t, data, toYAML(t, data))
		})
	}
}

func TestRandomSelectionsDecodeTheSameInBothFormats(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	pick := func(from []string) []string {
		var out []string
		for _, item := range from {
			if rng.Intn(2) == 0 {
				out = append(out, item)
			}
		}
		return out
	}
	for i := 0; i < 200; i++ {
		doc := map[string]any{"schema": 1}
		if rng.Intn(4) > 0 {
			doc["project"] = map[string]any{"name": fmt.Sprintf("app-%d", rng.Intn(100)), "goModule": "example.com/a"}
		}
		if rng.Intn(4) > 0 {
			doc["stacks"] = pick([]string{"go-srv", "ng-web", "rust-srv"})
		}
		if rng.Intn(4) > 0 {
			doc["modules"] = pick([]string{"core", "identity", "Bad", "no", "123", "tenancy"})
		}
		if rng.Intn(3) == 0 {
			doc["extra"] = []any{rng.Intn(5), "x"}
		}
		jsonText, err := jsonOf(doc)
		if err != nil {
			t.Fatal(err)
		}
		sameDecoded(t, jsonText, toYAML(t, jsonText))
	}
}

func FuzzParseYAML(f *testing.F) {
	for _, seed := range []string{
		"", "schema: 1\n", "a: &x 1\nb: *x\n", "a: 1\na: 2\n", "<<: {a: 1}\n", "---\na: 1\n---\nb: 2\n",
		"[", "name: !!binary aGk=\n", "schema: 0x1\n", strings.Repeat("[", 100), "? [a]\n: b\n",
		"schema: 1\nproject:\n  name: acme\nstacks: [go-srv]\nmodules: []\n", "a: .inf\nb: 1e3\n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		tree, issues := manifest.ParseYAML("f.yaml", data)
		if tree != nil && len(issues) > 0 {
			t.Fatal("a tree came back with issues")
		}
		for _, found := range issues {
			if found.Code != manifest.CodeSelectionInvalid || !strings.HasPrefix(found.Path, "f.yaml#") {
				t.Fatalf("bad issue %+v", found)
			}
		}
		if tree == nil || !utf8.Valid(data) {
			return
		}
		encoded, err := manifest.EncodeJSON(tree)
		if err != nil {
			t.Fatalf("a YAML tree must render as JSON: %v", err)
		}
		jsonTree, jsonIssues := manifest.ParseJSON("f.yaml", encoded)
		if len(jsonIssues) > 0 {
			t.Fatalf("rendered JSON refused: %+v\n%s", jsonIssues, encoded)
		}
		yamlInput, yamlFound := manifest.DecodeInput("f.yaml", tree)
		jsonInput, jsonFound := manifest.DecodeInput("f.yaml", jsonTree)
		if !reflect.DeepEqual(yamlInput.Selection, jsonInput.Selection) {
			t.Fatalf("selections differ for %q", data)
		}
		if !reflect.DeepEqual(stripQuoteAdvice(yamlFound), stripQuoteAdvice(jsonFound)) {
			t.Fatalf("issues differ for %q\nyaml %+v\njson %+v", data, yamlFound, jsonFound)
		}
	})
}

func FuzzYAMLIssueLines(f *testing.F) {
	for _, seed := range []string{"a: 1\na: 2\n", "[", "a: &x 1\n", "a: 1\n---\nb: 1\n", "a: 010\n"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, issues := manifest.ParseYAML("f.yaml", data)
		for _, found := range issues {
			if found.Line < 0 {
				t.Fatalf("negative line: %+v", found)
			}
			isConstruct := found.Path != "f.yaml#"
			if isConstruct && found.Line == 0 {
				t.Fatalf("construct issue without a line: %+v", found)
			}
		}
	})
}

func jsonOf(value any) ([]byte, error) {
	return json.Marshal(value)
}
