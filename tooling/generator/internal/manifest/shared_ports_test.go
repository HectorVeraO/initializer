package manifest_test

import (
	"reflect"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/ports"
)

func TestDecodeModuleReadsASharedPort(t *testing.T) {
	data := mutateFixture(t, "module.valid.json", func(m map[string]any) {
		m["ports"] = []any{
			map[string]any{"name": "srv", "offset": 0, "origin": true, "stride": 1},
			map[string]any{"name": "postgres", "offset": 32, "shared": true},
		}
	})
	module, issues := manifest.DecodeModule(modulePath, data)
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	want := []ports.Port{{Name: "srv", Offset: 0, Origin: true, Stride: 1}, {Name: "postgres", Offset: 32, Shared: true}}
	if !reflect.DeepEqual(module.Ports, want) {
		t.Errorf("ports = %+v, want %+v", module.Ports, want)
	}
}

func TestDecodeModuleRefusesASharedPortThatIsNotABoolean(t *testing.T) {
	data := mutateFixture(t, "module.valid.json", func(m map[string]any) {
		m["ports"] = []any{map[string]any{"name": "postgres", "offset": 32, "shared": "yes"}}
	})
	_, issues := manifest.DecodeModule(modulePath, data)
	if len(issues) != 1 || issues[0].Path != modulePath+"#/ports/0/shared" {
		t.Fatalf("issues: %+v", issues)
	}
}
