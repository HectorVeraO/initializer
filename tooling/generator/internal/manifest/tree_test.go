package manifest_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

func TestParseJSONBuildsTheTree(t *testing.T) {
	root, issues := manifest.ParseJSON("acme.json", []byte(`{"a": [1, -2, 1.5, 2e3, true, null, "x"], "b": {"c": "d"}}`))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	list, _ := root.Member("a")
	kinds := []manifest.Kind{manifest.KindInt, manifest.KindInt, manifest.KindFloat, manifest.KindFloat, manifest.KindBool, manifest.KindNull, manifest.KindString}
	for i, want := range kinds {
		if list.Items[i].Kind != want {
			t.Errorf("item %d kind = %v, want %v", i, list.Items[i].Kind, want)
		}
	}
	object, _ := root.Member("b")
	inner, _ := object.Member("c")
	if root.Kind != manifest.KindMap || inner.Kind != manifest.KindString || inner.Value != "d" || inner.Line != 0 {
		t.Errorf("tree = %+v", root)
	}
	if got := root.Members[0].Key + root.Members[1].Key; got != "ab" {
		t.Errorf("members are not in source order: %s", got)
	}
}

func TestParseJSONReportsEveryProblem(t *testing.T) {
	cases := map[string]struct {
		input   string
		path    string
		message string
	}{
		"empty":                {"", "#", "input is empty"},
		"blank":                {"  \n", "#", "input is empty"},
		"truncated":            {`{"a":`, "#", "invalid JSON at offset"},
		"two values":           {`{} {}`, "#", "unexpected data after the JSON value"},
		"trailing garbage":     {`{}x`, "#", "unexpected data after the JSON value"},
		"duplicate key":        {`{"a":1,"a":2}`, "#/a", "duplicate key"},
		"nested duplicate key": {`{"a":{"b":1,"b":2}}`, "#/a/b", "duplicate key"},
		"escaped key pointer":  {`{"a/b":1,"a/b":2}`, "#/a~1b", "duplicate key"},
		"too deep":             {strings.Repeat("[", 100) + strings.Repeat("]", 100), "", "nested deeper"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root, issues := manifest.ParseJSON("acme.json", []byte(tc.input))
			if root != nil || len(issues) == 0 {
				t.Fatalf("want issues and no tree, got %v %v", root, issues)
			}
			found := false
			for _, got := range issues {
				isMatch := got.Code == manifest.CodeSelectionInvalid && strings.HasPrefix(got.Path, "acme.json") &&
					strings.HasSuffix(got.Path, tc.path) && strings.Contains(got.Message, tc.message)
				found = found || isMatch
			}
			if !found {
				t.Fatalf("want %q at %s, got %v", tc.message, tc.path, issues)
			}
		})
	}
}

func TestParseJSONNestingBoundary(t *testing.T) {
	nested := func(levels int) []byte {
		return []byte(strings.Repeat("[", levels) + strings.Repeat("]", levels))
	}
	if root, issues := manifest.ParseJSON("acme.json", nested(64)); root == nil || len(issues) != 0 {
		t.Errorf("64 levels: want a tree, got %v %v", root, issues)
	}
	root, issues := manifest.ParseJSON("acme.json", nested(65))
	if root != nil || len(issues) == 0 {
		t.Errorf("65 levels: want issues and no tree, got %v %v", root, issues)
	}
}

func TestParseJSONAcceptsTheRepoSelectionFiles(t *testing.T) {
	for _, path := range ciSelectionPaths(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, issues := manifest.ParseJSON(filepath.Base(path), data); len(issues) != 0 {
			t.Errorf("%s: %v", path, issues)
		}
	}
}

// ciSelectionPaths lists the CI selection files of the repository.
func ciSelectionPaths(t testing.TB) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "ci", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no CI selections: %v", err)
	}
	return paths
}

func FuzzParseJSON(f *testing.F) {
	for _, path := range ciSelectionPaths(f) {
		if data, err := os.ReadFile(path); err == nil {
			f.Add(data)
		}
	}
	f.Add([]byte(`{"schema":1,"x":[1.5e3,null,{"a":"é"}]}`))
	f.Add([]byte(`{"a":1,"a":2}`))
	f.Add([]byte(`[[[[[[[[`))
	f.Add([]byte("\xff\xfe"))
	f.Fuzz(func(t *testing.T, data []byte) {
		root, issues := manifest.ParseJSON("fuzz.json", data)
		if (root == nil) == (len(issues) == 0) {
			t.Fatalf("a tree and issues are exclusive: tree %v issues %v", root != nil, issues)
		}
		if root == nil {
			return
		}
		encoded, err := manifest.EncodeJSON(root)
		if err != nil {
			t.Fatalf("encode accepted tree: %v", err)
		}
		again, issues := manifest.ParseJSON("fuzz.json", encoded)
		if len(issues) != 0 || !reflect.DeepEqual(root, again) {
			t.Fatalf("accepted input does not re-parse to an equal tree:\n%s\n%v", encoded, issues)
		}
		if !bytes.Equal(encoded, mustEncode(t, again)) {
			t.Fatalf("encoding is not stable")
		}
	})
}

func mustEncode(t *testing.T, n *manifest.Node) []byte {
	t.Helper()
	encoded, err := manifest.EncodeJSON(n)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
