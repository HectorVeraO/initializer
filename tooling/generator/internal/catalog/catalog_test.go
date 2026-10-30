package catalog

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	built, _ := buildRepo(t, cleanRepo(t))
	encoded, err := Encode(built)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.HasSuffix(encoded, []byte("}\n")) || bytes.HasSuffix(encoded, []byte("\n\n")) {
		t.Fatalf("want exactly one trailing newline")
	}
	if bytes.Contains(encoded, []byte("\r")) {
		t.Fatalf("encoded catalog must use LF")
	}
	decoded, issues := Decode(encoded)
	if len(issues) > 0 {
		t.Fatalf("Decode: %+v", issues)
	}
	again, err := Encode(decoded)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(encoded, again) {
		t.Fatalf("decode then encode changed the bytes")
	}
	if !reflect.DeepEqual(normalize(built), decoded) {
		t.Fatalf("decoded catalog differs from the built one")
	}
}

func TestEncodeWritesEmptyListsNotNull(t *testing.T) {
	encoded, err := Encode(Catalog{Schema: 2, Generator: "0.1.0"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if bytes.Contains(encoded, []byte("null")) {
		t.Fatalf("nil lists must encode as []: %s", encoded)
	}
}

func TestDecodeRefusesBadCatalogs(t *testing.T) {
	built, _ := buildRepo(t, cleanRepo(t))
	good, err := Encode(built)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	text := string(good)
	cases := []struct {
		name    string
		data    string
		path    string
		message string
	}{
		{"unknown key", strings.Replace(text, `"schema": 2,`, `"schema": 2, "extra": 2,`, 1), "catalog.lock.json#", `unknown field "extra"`},
		{"schema 1", strings.Replace(text, `"schema": 2,`, `"schema": 1,`, 1), "catalog.lock.json#/schema", "must be the number 2"},
		{"trailing data", text + "{}", "catalog.lock.json#", "unexpected data after the JSON value"},
		{"empty", "", "catalog.lock.json#", "EOF"},
		{"not an object", "[]", "catalog.lock.json#", "cannot unmarshal array"},
		{"bad version", strings.Replace(text, `"version": "1.0.0"`, `"version": "1.0"`, 1), "catalog.lock.json#/specs/0/version", "MAJOR.MINOR.PATCH"},
		{"short hash", strings.Replace(text, `"sha256": "`, `"sha256": "zz`, 1), "catalog.lock.json#/specs/0/files/0/sha256", "64 lowercase hex digits"},
		{"unknown reference slot", strings.Replace(text, `"project": "fixture-app"`, `"bogus": "x", "project": "fixture-app"`, 1), "catalog.lock.json#/reference/bogus", "not a slot name"},
		{"empty reference value", strings.Replace(text, `"project": "fixture-app"`, `"project": ""`, 1), "catalog.lock.json#/reference/project", "must not be empty"},
		{"rule with a bad slot", strings.Replace(text, `"text": "name: %{project}"`, `"text": "name: %{bogus}"`, 1), "catalog.lock.json#/stacks/0/modules/0/names/0/text", "unknown slot"},
		{"rule with a bad block", strings.Replace(text, "\n                \"\"\n", "\n                \"x\"\n", 1), "catalog.lock.json#/stacks/0/modules/0/names/0/blocks/0", "module:<id>"},
		{"traversal path", strings.Replace(text, `"path": "specs/core/openapi.yaml"`, `"path": "../openapi.yaml"`, 1), "catalog.lock.json#/specs/0/files/0/path", "repo-relative path"},
		{"unsorted specs", strings.Replace(text, `"spec": "core",`, `"spec": "zzz",`, 1), "catalog.lock.json#/specs/1", "sorted bytewise and unique"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, issues := Decode([]byte(tc.data))
			if len(issues) == 0 {
				t.Fatalf("want issues")
			}
			first := issues[0]
			if first.Code != CodeCatalogInvalid || string(first.Path) != tc.path || !strings.Contains(first.Message, tc.message) {
				t.Fatalf("first issue = %+v, want code %s at %s containing %q", first, CodeCatalogInvalid, tc.path, tc.message)
			}
		})
	}
}

func TestParseFileList(t *testing.T) {
	got, err := ParseFileList([]byte("a/b\x00c\x00"))
	if err != nil || !reflect.DeepEqual(got, []string{"a/b", "c"}) {
		t.Fatalf("ParseFileList = %v, %v", got, err)
	}
	if got, err := ParseFileList(nil); err != nil || len(got) != 0 {
		t.Fatalf("empty list = %v, %v", got, err)
	}
	for _, bad := range []string{"a\x00\x00b", "\x00a", "\x00\x00"} {
		if _, err := ParseFileList([]byte(bad)); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
}

func FuzzParseFileList(f *testing.F) {
	f.Add([]byte("a/b\x00c\x00"))
	f.Add([]byte(""))
	f.Add([]byte("\x00"))
	f.Add([]byte("a\nb\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		files, err := ParseFileList(data)
		if err != nil {
			return
		}
		for _, p := range files {
			if p == "" || strings.ContainsRune(p, 0) {
				t.Fatalf("bad element %q", p)
			}
		}
	})
}

func FuzzDecode(f *testing.F) {
	built, _, err := Build(BuildOptions{Root: fstest.MapFS{}, Files: nil, Generator: "0.1.0"})
	if err != nil {
		f.Fatalf("Build: %v", err)
	}
	empty, err := Encode(built)
	if err != nil {
		f.Fatalf("Encode: %v", err)
	}
	f.Add(empty)
	f.Add([]byte(`{"schema":1}`))
	f.Add([]byte(`{`))
	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, issues := Decode(data)
		if len(issues) > 0 {
			return
		}
		first, err := Encode(decoded)
		if err != nil {
			t.Fatalf("Encode of a decoded catalog: %v", err)
		}
		second, issues := Decode(first)
		if len(issues) > 0 {
			t.Fatalf("re-decode: %+v", issues)
		}
		again, err := Encode(second)
		if err != nil || !bytes.Equal(first, again) {
			t.Fatalf("encoding is not stable")
		}
	})
}

// FuzzBuildManifests feeds arbitrary bytes as manifests: Build must neither
// panic nor depend on anything but its input.
func FuzzBuildManifests(f *testing.F) {
	repo, err := loadCleanRepo()
	if err != nil {
		f.Fatalf("load fixture: %v", err)
	}
	f.Add(string(repo[coreManifest].Data), string(repo["specs/core/spec.manifest.json"].Data))
	f.Add(`{`, `[]`)
	f.Add(`{"schema":1,"files":["../x"]}`, ``)
	f.Add(`{"schema":1,"stack":"go-srv","module":"parameters","version":"0.1.0","implied":false,"implementsSpec":{"parameters":"0.1.0"},"requiresCore":"^0.1.0","requiresModules":[],"tablePrefixes":["par"],"ports":[{"name":"p","offset":50,"stride":0}],"perComponent":[".deploy/local/","../x","go-srv/a"],"files":[]}`, ``)
	f.Fuzz(func(t *testing.T, moduleText, specText string) {
		r := cleanRepo(t)
		put(r, paramsManifest, moduleText)
		put(r, "specs/parameters/spec.manifest.json", specText)
		first, issuesA, err := Build(BuildOptions{Root: r, Files: filesOf(r), Generator: "0.1.0"})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		second, issuesB, err := Build(BuildOptions{Root: r, Files: filesOf(r), Generator: "0.1.0"})
		if err != nil || !reflect.DeepEqual(first, second) || !reflect.DeepEqual(issuesA, issuesB) {
			t.Fatalf("Build is not deterministic")
		}
	})
}
