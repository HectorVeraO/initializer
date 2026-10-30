package catalog

import (
	"fmt"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/markers"
)

// markerChecks validates the module markers of one catalogued text file.
// Only files owned by a core manifest may hold markers; a nil owner is a
// spec folder file.
func (b *builder) markerChecks(p string, data []byte, owner *moduleDoc) {
	isCoreOwned := owner != nil && owner.module.Module == manifest.CoreModule
	if !isCoreOwned {
		line := markers.CandidateLine(data)
		if line > 0 {
			b.addPlain(markers.CodeMarkerInModuleFile, p,
				fmt.Sprintf("markers are allowed only in files owned by a core module (line %d)", line))
		}
		return
	}
	if _, allowed := markers.SyntaxFor(p); !allowed {
		line := markers.CandidateLine(data)
		if line > 0 {
			b.addPlain(markers.CodeMarkerInvalid, p,
				fmt.Sprintf("markers are not allowed in this file kind (line %d)", line))
		}
		return
	}
	blocks, issues := markers.Parse(p, data)
	for _, found := range issues {
		b.addPlain(found.Code, p, found.Message)
	}
	b.checkRuns(p, data, blocks, len(issues) == 0)
	knownModules := b.moduleIDs()
	knownStacks := stackNames()
	for _, block := range blocks {
		switch {
		case block.Kind == markers.KindStack && !knownStacks[block.ID]:
			b.addPlain(markers.CodeMarkerStackUnknown, p,
				fmt.Sprintf("marker names stack %q (line %d), which is not a stack of the stack table", block.ID, block.Start+1))
		case block.Kind == markers.KindModule && !knownModules[block.ID]:
			b.addPlain(markers.CodeMarkerModuleUnknown, p,
				fmt.Sprintf("marker names %s, which has no module manifest (line %d)", block.ID, block.Start+1))
		}
	}
}

// stackNames returns the names of the stack table.
func stackNames() map[string]bool {
	names := map[string]bool{}
	for _, info := range stackTable {
		names[info.name] = true
	}
	return names
}

// moduleIDs returns every decoded manifest module id of every stack.
func (b *builder) moduleIDs() map[string]bool {
	ids := map[string]bool{}
	for _, doc := range b.modules {
		ids[doc.module.Module] = true
	}
	return ids
}

// checkRuns applies MARKER_RUN_INVALID: a module block outside every run
// whose end line is directly followed by the open line of a stack block. The
// repeat unit of that sequence is ambiguous, so the author puts the stack
// block first. Inside a run the same sequence is the normal interleave.
func (b *builder) checkRuns(p string, data []byte, blocks []markers.Block, isParsed bool) {
	if !isParsed {
		return
	}
	inRun := map[int]bool{}
	for _, run := range markers.Runs(data, blocks) {
		for _, block := range run.Blocks {
			inRun[block.Start] = true
		}
	}
	for i := 0; i+1 < len(blocks); i++ {
		block, following := blocks[i], blocks[i+1]
		isOutsideModule := block.Kind == markers.KindModule && !inRun[block.Start]
		isFollowedByStack := following.Kind == markers.KindStack && following.Start == block.End+1
		if isOutsideModule && isFollowedByStack {
			b.addPlain(markers.CodeMarkerRunInvalid, p,
				fmt.Sprintf("module block %s (line %d) is directly followed by the stack block %s (line %d): put the stack block first",
					block.ID, block.Start+1, following.ID, following.Start+1))
		}
	}
}
