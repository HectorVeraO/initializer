package manifest_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/ports"
)

func TestDecodeModuleReadsPortsAndPortFiles(t *testing.T) {
	data := mutateFixture(t, "module.valid.json", func(m map[string]any) {
		m["ports"] = []any{
			map[string]any{"name": "srv", "offset": 0, "origin": true, "stride": 1},
			map[string]any{"name": "postgres", "offset": 32, "stride": 10},
		}
		m["portFiles"] = []any{"mise.toml", ".deploy/local/README.md"}
		m["perComponent"] = []any{".deploy/local/.example.env", ".deploy/local/kratos/"}
	})
	module, issues := manifest.DecodeModule(modulePath, data)
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	wantPorts := []ports.Port{{Name: "srv", Offset: 0, Origin: true, Stride: 1}, {Name: "postgres", Offset: 32, Stride: 10}}
	if !reflect.DeepEqual(module.Ports, wantPorts) {
		t.Errorf("ports = %+v, want %+v", module.Ports, wantPorts)
	}
	if !reflect.DeepEqual(module.PortFiles, []string{"mise.toml", ".deploy/local/README.md"}) {
		t.Errorf("portFiles = %v", module.PortFiles)
	}
	if !reflect.DeepEqual(module.PerComponent, []string{".deploy/local/.example.env", ".deploy/local/kratos/"}) {
		t.Errorf("perComponent = %v", module.PerComponent)
	}
}

func TestDecodeModuleRefusesMalformedPorts(t *testing.T) {
	cases := []struct {
		name   string
		change func(map[string]any)
		path   string
	}{
		{"port is not an object", func(m map[string]any) { m["ports"] = []any{"srv"} }, "#/ports/0"},
		{"unknown port key", func(m map[string]any) {
			m["ports"] = []any{map[string]any{"name": "srv", "offset": 0, "step": 1}}
		}, "#/ports/0/step"},
		{"stride is a string", func(m map[string]any) {
			m["ports"] = []any{map[string]any{"name": "srv", "offset": 0, "stride": "1"}}
		}, "#/ports/0/stride"},
		{"stride is a fraction", func(m map[string]any) {
			m["ports"] = []any{map[string]any{"name": "srv", "offset": 0, "stride": 1.5}}
		}, "#/ports/0/stride"},
		{"perComponent is not a list", func(m map[string]any) { m["perComponent"] = ".deploy/local/.example.env" }, "#/perComponent"},
		{"perComponent escapes the repo", func(m map[string]any) { m["perComponent"] = []any{"../x"} }, "#/perComponent/0"},
		{"perComponent repeats", func(m map[string]any) {
			m["perComponent"] = []any{".deploy/local/kratos/", ".deploy/local/kratos/"}
		}, "#/perComponent/1"},
		{"missing name", func(m map[string]any) { m["ports"] = []any{map[string]any{"offset": 0}} }, "#/ports/0/name"},
		{"missing offset", func(m map[string]any) { m["ports"] = []any{map[string]any{"name": "srv"}} }, "#/ports/0/offset"},
		{"offset is a string", func(m map[string]any) {
			m["ports"] = []any{map[string]any{"name": "srv", "offset": "0"}}
		}, "#/ports/0/offset"},
		{"offset is a fraction", func(m map[string]any) {
			m["ports"] = []any{map[string]any{"name": "srv", "offset": 1.5}}
		}, "#/ports/0/offset"},
		{"origin is a string", func(m map[string]any) {
			m["ports"] = []any{map[string]any{"name": "srv", "offset": 0, "origin": "yes"}}
		}, "#/ports/0/origin"},
		{"portFiles is not a list", func(m map[string]any) { m["portFiles"] = "mise.toml" }, "#/portFiles"},
		{"port file escapes the repo", func(m map[string]any) { m["portFiles"] = []any{"../mise.toml"} }, "#/portFiles/0"},
		{"port file is a folder", func(m map[string]any) { m["portFiles"] = []any{".deploy/local/"} }, "#/portFiles/0"},
		{"port file repeats", func(m map[string]any) { m["portFiles"] = []any{"mise.toml", "mise.toml"} }, "#/portFiles/1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, issues := manifest.DecodeModule(modulePath, mutateFixture(t, "module.valid.json", tc.change))
			if !hasIssue(issues, manifest.CodeManifestInvalid, modulePath+tc.path) {
				t.Fatalf("want %s at %s, got %v", manifest.CodeManifestInvalid, tc.path, issues)
			}
		})
	}
}

func portsFile(t *testing.T, schema int, band any) []byte {
	t.Helper()
	return infraFile(t, schema, map[string]any{"ports": map[string]any{"band": band}})
}

func TestDecodeReadsThePortBand(t *testing.T) {
	selection, issues := decodeFile(t, selectionPath, portsFile(t, 2, 103))
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	if selection.Ports == nil || selection.Ports.Band != 103 {
		t.Fatalf("ports = %+v", selection.Ports)
	}
}

func TestDecodeAcceptsTheBandBounds(t *testing.T) {
	for _, band := range []int{100, 654} {
		selection, issues := decodeFile(t, selectionPath, portsFile(t, 2, band))
		if len(issues) != 0 || selection.Ports == nil || selection.Ports.Band != band {
			t.Errorf("band %d: ports %+v, issues %v", band, selection.Ports, issues)
		}
	}
}

func TestDecodeRefusesAPortBandOutsideItsRange(t *testing.T) {
	for _, band := range []any{99, 655, 0, -101, 100000, 103.5, "103", true} {
		_, issues := decodeFile(t, selectionPath, portsFile(t, 2, band))
		if len(issues) != 1 || issues[0].Code != manifest.CodeSelectionInvalid || issues[0].Message != "must be an integer from 100 to 654" {
			t.Errorf("band %v: issues %v", band, issues)
		}
	}
}

func TestDecodeRefusesPortsAtSchema1(t *testing.T) {
	_, issues := decodeFile(t, selectionPath, portsFile(t, 1, 103))
	if len(issues) != 1 || issues[0].Message != "ports needs schema 2" {
		t.Fatalf("issues: %v", issues)
	}
}

func TestDecodeRefusesAnUnknownPortsKey(t *testing.T) {
	data := infraFile(t, 2, map[string]any{"ports": map[string]any{"band": 103, "stride": 2}})
	_, issues := decodeFile(t, selectionPath, data)
	if len(issues) != 1 || issues[0].Message != "unknown key" {
		t.Fatalf("issues: %v", issues)
	}
}

func TestFlagBandAndFileBandGiveTheSameInput(t *testing.T) {
	fromFlag, flagIssues := manifest.FlagsInput(manifest.Flags{PortBand: "103", Has: manifest.FlagPresence{PortBand: true}})
	if len(flagIssues) != 0 {
		t.Fatalf("issues: %v", flagIssues)
	}
	fromFile, fileIssues := decodeFile(t, selectionPath, portsFile(t, 2, 103))
	if len(fileIssues) != 0 {
		t.Fatalf("issues: %v", fileIssues)
	}
	if !reflect.DeepEqual(fromFlag.Selection.Ports, fromFile.Ports) || fromFlag.Selection.Schema != 2 {
		t.Fatalf("flag %+v schema %d, file %+v", fromFlag.Selection.Ports, fromFlag.Selection.Schema, fromFile.Ports)
	}
}

func TestFlagBandProblemsAreValueProblemsAtTheFlag(t *testing.T) {
	for _, value := range []string{"abc", "", "99", "655", "10x", "-101", "+103", "1e2", "103.0", "99999999999999999999", " 103", "00101"} {
		in, issues := manifest.FlagsInput(manifest.Flags{PortBand: value, Has: manifest.FlagPresence{PortBand: true}})
		if len(issues) != 1 || issues[0].Path != "flags#/ports/band" || issues[0].Code != manifest.CodeSelectionInvalid ||
			issues[0].Message != "must be an integer from 100 to 654" {
			t.Errorf("value %q: issues %v", value, issues)
		}
		if in.Selection.Ports != nil {
			t.Errorf("value %q: ports = %+v", value, in.Selection.Ports)
		}
	}
}

func TestFlagBandReplacesTheFileBand(t *testing.T) {
	file, issues := decodeInputFile(selectionPath, portsFile(t, 2, 103))
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	flags, _ := manifest.FlagsInput(manifest.Flags{PortBand: "110", Has: manifest.FlagPresence{PortBand: true}})
	merged, mergeIssues := manifest.Merge(&file, flags)
	if len(mergeIssues) != 0 {
		t.Fatalf("issues: %v", mergeIssues)
	}
	if merged.Selection.Ports.Band != 110 || file.Selection.Ports.Band != 103 {
		t.Errorf("merged %d, file %d", merged.Selection.Ports.Band, file.Selection.Ports.Band)
	}
	if origin := merged.Origins["/ports/band"]; origin.Arg != "--port-band" {
		t.Errorf("origin = %+v", origin)
	}
	kept, _ := manifest.Merge(&file, mustFlags(manifest.Flags{}))
	if kept.Selection.Ports == nil || kept.Selection.Ports.Band != 103 || kept.Selection.Schema != 2 {
		t.Errorf("without the flag the file band stays: %+v", kept.Selection)
	}
}

func TestRecordedSelectionHoldsThePortBandOnlyWhenGiven(t *testing.T) {
	given := mustFlagsMergedWith(t, manifest.Flags{PortBand: "103", Has: manifest.FlagPresence{PortBand: true}})
	recorded := manifest.RecordedSelection(given, []string{"core"})
	if recorded.Schema != 2 || recorded.Ports == nil || recorded.Ports.Band != 103 {
		t.Fatalf("recorded = %+v", recorded)
	}
	encoded, err := recorded.MarshalJSON()
	var decoded struct {
		Ports struct{ Band int }
	}
	if err != nil || json.Unmarshal(encoded, &decoded) != nil || decoded.Ports.Band != 103 {
		t.Fatalf("recorded JSON must carry ports.band 103: %s (%v)", encoded, err)
	}
	plain := manifest.RecordedSelection(mustFlagsMerged(t), []string{"core"})
	if plain.Ports != nil || plain.Schema != 1 {
		t.Fatalf("a run without --port-band must record no ports: %+v", plain)
	}
}

func mustFlagsMergedWith(t *testing.T, f manifest.Flags) manifest.Input {
	t.Helper()
	f.Name, f.Stacks = "acme", []string{"go-srv"}
	f.Has.Name, f.Has.Stacks = true, true
	flags, issues := manifest.FlagsInput(f)
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	merged, _ := manifest.Merge(nil, flags)
	return merged
}

func TestDecodeReferenceReadsThePortBand(t *testing.T) {
	data := []byte(`{"schema":2,"project":{"name":"acme"},"components":[{"stack":"go-srv","name":"acme-srv"},{"stack":"ng-web","name":"acme-web"}],"ports":{"band":101}}`)
	selection, issues := manifest.DecodeReference(data)
	if len(issues) != 0 || selection.Ports == nil || selection.Ports.Band != 101 {
		t.Fatalf("ports %+v, issues %v", selection.Ports, issues)
	}
	bad := strings.Replace(string(data), `"band":101`, `"band":7`, 1)
	_, issues = manifest.DecodeReference([]byte(bad))
	if len(issues) != 1 || issues[0].Code != "REFERENCE_INVALID" {
		t.Fatalf("issues: %v", issues)
	}
}
