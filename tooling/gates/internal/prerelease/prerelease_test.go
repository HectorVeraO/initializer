package prerelease

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSetting(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	err := os.WriteFile(filepath.Join(root, FileName), []byte(content), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLoadAbsentFileIsOff(t *testing.T) {
	got, err := Load(t.TempDir())
	if err != nil || got {
		t.Fatalf("Load = %v, %v; want false, nil", got, err)
	}
}

func TestLoadValid(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"on", `{ "schema": 1, "preRelease": true }`, true},
		{"off", `{ "schema": 1, "preRelease": false }`, false},
		{"on with final newline", "{ \"schema\": 1, \"preRelease\": true }\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(writeSetting(t, tt.content))
			if err != nil || got != tt.want {
				t.Fatalf("Load = %v, %v; want %v, nil", got, err, tt.want)
			}
		})
	}
}

func TestLoadInvalid(t *testing.T) {
	tests := []struct {
		name    string
		content string
		mention []string
	}{
		{"preRelease missing", `{ "schema": 1 }`, []string{"preRelease"}},
		{"schema 2", `{ "schema": 2, "preRelease": true }`, []string{"schema"}},
		{"schema missing", `{ "preRelease": true }`, []string{"schema"}},
		{"schema string", `{ "schema": "1", "preRelease": true }`, []string{"schema"}},
		{"preRelease string", `{ "schema": 1, "preRelease": "true" }`, []string{"preRelease"}},
		{"preRelease number", `{ "schema": 1, "preRelease": 1 }`, []string{"preRelease"}},
		{"preRelease null", `{ "schema": 1, "preRelease": null }`, []string{"preRelease"}},
		{"unknown key", `{ "schema": 1, "preRelease": true, "x": 1 }`, []string{`unknown field "x"`}},
		{"trailing data", `{ "schema": 1, "preRelease": true } {}`, nil},
		{"not json", `not json`, nil},
		{"empty file", ``, nil},
		{"array", `[]`, nil},
		{"both wrong", `{ "preRelease": "yes" }`, []string{"schema", "preRelease"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(writeSetting(t, tt.content))
			if err == nil {
				t.Fatalf("Load = %v, nil; want an error", got)
			}
			if got {
				t.Fatalf("Load returned true with error %v", err)
			}
			if !strings.HasPrefix(err.Error(), FileName) {
				t.Fatalf("error %q does not start with %s", err, FileName)
			}
			for _, word := range tt.mention {
				if !strings.Contains(err.Error(), word) {
					t.Fatalf("error %q does not name %q", err, word)
				}
			}
		})
	}
}

func TestLoadBothProblemsInOneError(t *testing.T) {
	_, err := Load(writeSetting(t, `{ "preRelease": "yes" }`))
	if err == nil || strings.Count(err.Error(), FileName) != 1 {
		t.Fatalf("want one error naming the file once, got %v", err)
	}
}

func TestLoadTypeMismatchAlsoReportsUnknownKeyAndTrailingData(t *testing.T) {
	_, err := Load(writeSetting(t, `{ "schema": 1, "preRelease": "yes", "extra": 1 } {}`))
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"preRelease must be a JSON boolean", `unknown field "extra"`, "unexpected data after the object"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q misses %q", err, want)
		}
	}
}

func TestLoadUnknownKeyBeforeMistypedFieldReportsBoth(t *testing.T) {
	_, err := Load(writeSetting(t, `{ "extra": 1, "schema": 1, "preRelease": "yes" }`))
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"preRelease must be a JSON boolean", `unknown field "extra"`} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q misses %q", err, want)
		}
	}
}

// FuzzLoad checks that any bytes never panic, that errors name the file and
// that a true result only comes from valid JSON.
func FuzzLoad(f *testing.F) {
	f.Add([]byte(`{ "schema": 1, "preRelease": true }`))
	f.Add([]byte(`{ "schema": 1, "preRelease": false }`))
	f.Add([]byte(`{ "preRelease": "yes" }`))
	f.Add([]byte(`{ "schema": 1, "preRelease": true } {}`))
	f.Add([]byte(``))
	f.Fuzz(func(t *testing.T, input []byte) {
		root := t.TempDir()
		err := os.WriteFile(filepath.Join(root, FileName), input, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		got, loadErr := Load(root)
		if loadErr != nil && !strings.HasPrefix(loadErr.Error(), FileName) {
			t.Fatalf("error %q does not start with %s", loadErr, FileName)
		}
		if loadErr != nil && got {
			t.Fatalf("true with error %v", loadErr)
		}
		if got && !json.Valid(input) {
			t.Fatalf("true for invalid JSON %q", input)
		}
	})
}
