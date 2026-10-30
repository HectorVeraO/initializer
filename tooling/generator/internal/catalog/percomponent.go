package catalog

import (
	"fmt"
	"strings"
)

// deployLocal is the folder per-component files live under.
const deployLocal = ".deploy/local/"

// checkPerComponent applies PER_COMPONENT_INVALID: a perComponent path the
// module does not own, that is not under .deploy/local/, that is .deploy/local/
// itself.
func (b *builder) checkPerComponent() {
	for _, doc := range b.modules {
		for i, path := range doc.module.PerComponent {
			pointer := fmt.Sprintf("/perComponent/%d", i)
			if reason := b.perComponentProblem(doc, path); reason != "" {
				b.add(CodePerComponentInvalid, doc.path, pointer, reason)
			}
		}
	}
}

// perComponentProblem returns why path cannot be a per-component path of doc,
// or "" when it can.
func (b *builder) perComponentProblem(doc *moduleDoc, path string) string {
	switch {
	case path == deployLocal:
		return "must name a file or folder inside " + deployLocal + ", not the folder itself"
	case !strings.HasPrefix(path, deployLocal):
		return "must be under " + deployLocal
	}
	isDirectory := strings.HasSuffix(path, "/")
	for _, file := range doc.files {
		isOwned := file.Path == path || (isDirectory && strings.HasPrefix(file.Path, path))
		if isOwned {
			return ""
		}
	}
	return fmt.Sprintf("module %s does not own %s", doc.module.Module, path)
}

// isPerComponentPath reports whether some module lists the source path as a
// per-component file, directly or through a folder.
func (b *builder) isPerComponentPath(path string) bool {
	for _, doc := range b.modules {
		for _, entry := range doc.module.PerComponent {
			if path == entry || (strings.HasSuffix(entry, "/") && strings.HasPrefix(path, entry)) {
				return true
			}
		}
	}
	return false
}

// isProjectScope reports whether a source path is delivered once per project:
// under no stack root and in no per-component path.
func (b *builder) isProjectScope(path string) bool {
	for _, stack := range stackTable {
		if strings.HasPrefix(path, stack.root) {
			return false
		}
	}
	return !b.isPerComponentPath(path)
}
