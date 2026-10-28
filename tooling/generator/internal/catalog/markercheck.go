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
	known := b.moduleIDs()
	for _, block := range blocks {
		if !known[block.Module] {
			b.addPlain(markers.CodeMarkerModuleUnknown, p,
				fmt.Sprintf("marker names %s, which has no module manifest (line %d)", block.Module, block.Start+1))
		}
	}
}

// moduleIDs returns every decoded manifest module id of every stack.
func (b *builder) moduleIDs() map[string]bool {
	ids := map[string]bool{}
	for _, doc := range b.modules {
		ids[doc.module.Module] = true
	}
	return ids
}
