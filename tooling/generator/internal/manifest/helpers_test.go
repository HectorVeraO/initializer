package manifest_test

import (
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

// decodeFile runs the file pipeline of a selection: parse, decode, then the
// catalog-free checks of the merged input.
func decodeFile(t *testing.T, source string, data []byte) (manifest.Selection, []issue.Issue) {
	t.Helper()
	input, issues := decodeInputFile(source, data)
	return input.Selection, issues
}

func decodeInputFile(source string, data []byte) (manifest.Input, []issue.Issue) {
	tree, issues := manifest.ParseJSON(source, data)
	if len(issues) > 0 {
		return manifest.Input{}, issues
	}
	input, issues := manifest.DecodeInput(source, tree)
	merged, _ := manifest.Merge(&input, mustFlags(manifest.Flags{}))
	issues = append(issues, manifest.CheckMerged(merged)...)
	issue.Sort(issues)
	return input, issues
}

func mustFlags(f manifest.Flags) manifest.Input {
	input, _ := manifest.FlagsInput(f)
	return input
}
