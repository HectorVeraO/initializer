package catalog

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/markers"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/ports"
)

// Issue codes of the port checks.
const (
	CodePortInvalid         = "PORT_INVALID"
	CodePortOffsetDuplicate = "PORT_OFFSET_DUPLICATE"
	CodePortFileUnowned     = "PORT_FILE_UNOWNED"
	CodePortBandMixed       = "PORT_BAND_MIXED"
	CodePortUndeclared      = "PORT_UNDECLARED"
	CodePortRangeInRun      = "PORT_RANGE_IN_RUN"
	CodePortStrideInvalid   = "PORT_STRIDE_INVALID"
	CodePortUnscoped        = "PORT_UNSCOPED"
)

// Offsets 00 and 99 are the bounds of a band: a token may always carry them.
const (
	bandLowOffset  = 0
	bandHighOffset = 99
	// maxPortOffset is the highest offset a module may declare.
	maxPortOffset = 98
)

// strideMessage is the text of a missing or out-of-range stride.
const strideMessage = "stride must be an integer of at least 1"

// sharedStrideMessage is the text of a shared port that also has a stride.
const sharedStrideMessage = "a shared port has no stride: every component uses the one offset"

// strideProblem returns why the stride of port is wrong, or "" when it is
// right: at least 1 for a port per component, absent for a shared one.
func strideProblem(port ports.Port) string {
	switch {
	case port.Shared && port.Stride != 0:
		return sharedStrideMessage
	case port.Shared || port.Stride >= 1:
		return ""
	}
	return strideMessage
}

const portsReferenceMissing = "some manifest declares ports and the reference file is not tracked"

// declaresPorts reports whether any module declares ports or port files.
func (b *builder) declaresPorts() bool {
	for _, doc := range b.modules {
		if len(doc.module.Ports) > 0 || len(doc.module.PortFiles) > 0 {
			return true
		}
	}
	return false
}

// checkPorts applies the port checks of every module on source bytes.
func (b *builder) checkPorts() error {
	if !b.declaresPorts() {
		return nil
	}
	b.checkPortDeclarations()
	b.checkPortFileOwnership()
	if !b.set[ReferencePath] {
		declaresNames := false
		for _, doc := range b.modules {
			declaresNames = declaresNames || len(doc.module.Names) > 0
		}
		if !declaresNames {
			b.addPlain(CodeReferenceInvalid, ReferencePath, portsReferenceMissing)
		}
		return nil
	}
	if !b.referenceDecoded {
		return nil
	}
	if b.portBand == 0 {
		b.add(CodeReferenceIncomplete, ReferencePath, "/ports/band", "some manifest declares ports and the reference file has no ports.band")
		return nil
	}
	return b.checkPortTokens()
}

// portPlace is where a port is declared, for the messages of a repeat.
type portPlace struct {
	doc   *moduleDoc
	index int
}

// checkPortDeclarations applies PORT_INVALID and PORT_OFFSET_DUPLICATE.
func (b *builder) checkPortDeclarations() {
	firstName := map[string]portPlace{}
	firstOffset := map[int]portPlace{}
	originOf := map[string]portPlace{}
	for _, doc := range b.modules {
		for i, port := range doc.module.Ports {
			pointer := fmt.Sprintf("/ports/%d", i)
			here := portPlace{doc, i}
			if !names.ValidLabel(port.Name) {
				b.add(CodePortInvalid, doc.path, pointer+"/name", names.LabelMessage(port.Name))
			} else if first, repeated := firstName[port.Name]; repeated {
				b.add(CodePortInvalid, doc.path, pointer+"/name", fmt.Sprintf("name repeats %s/ports/%d/name", first.doc.path, first.index))
			} else {
				firstName[port.Name] = here
			}
			if message := strideProblem(port); message != "" {
				b.add(CodePortStrideInvalid, doc.path, pointer+"/stride", message)
			}
			isInRange := port.Offset >= bandLowOffset && port.Offset <= maxPortOffset
			if !isInRange {
				b.add(CodePortInvalid, doc.path, pointer+"/offset", fmt.Sprintf("offset %d must be from %d to %d", port.Offset, bandLowOffset, maxPortOffset))
			} else if first, repeated := firstOffset[port.Offset]; repeated {
				b.add(CodePortOffsetDuplicate, doc.path, pointer+"/offset",
					fmt.Sprintf("offset %d is also declared at %s/ports/%d/offset", port.Offset, first.doc.path, first.index))
			} else {
				firstOffset[port.Offset] = here
			}
			if !port.Origin {
				continue
			}
			if doc.module.Module != manifest.CoreModule {
				b.add(CodePortInvalid, doc.path, pointer+"/origin", "origin is declared by the core module of the stack")
			}
			if first, has := originOf[doc.stack.name]; has {
				b.add(CodePortInvalid, doc.path, pointer+"/origin",
					fmt.Sprintf("stack %s already has an origin port in %s", doc.stack.name, first.doc.path))
			} else {
				originOf[doc.stack.name] = here
			}
		}
	}
}

// checkPortFileOwnership applies PORT_FILE_UNOWNED.
func (b *builder) checkPortFileOwnership() {
	for _, doc := range b.modules {
		owned := ownedPaths(doc)
		for i, path := range doc.module.PortFiles {
			if !owned[path] {
				b.add(CodePortFileUnowned, doc.path, fmt.Sprintf("/portFiles/%d", i), fmt.Sprintf("module %s does not own %s", doc.module.Module, path))
			}
		}
	}
}

// ownedPaths returns the source paths of the files a module owns.
func ownedPaths(doc *moduleDoc) map[string]bool {
	owned := map[string]bool{}
	for _, file := range doc.files {
		owned[file.Path] = true
	}
	return owned
}

// declaredOffsets returns the offsets of every port of the catalog, each
// mapped to whether the port is shared by every component of its stack.
func (b *builder) declaredOffsets() map[int]bool {
	offsets := map[int]bool{}
	for _, doc := range b.modules {
		for _, port := range doc.module.Ports {
			offsets[port.Offset] = port.Shared
		}
	}
	return offsets
}

// isDeclared reports whether some port of the catalog has the offset.
func isDeclared(declared map[int]bool, offset int) bool {
	_, found := declared[offset]
	return found
}

// checkPortTokens applies PORT_BAND_MIXED and PORT_UNDECLARED to the tokens
// of every owned port file.
func (b *builder) checkPortTokens() error {
	declared := b.declaredOffsets()
	checked := map[string]bool{}
	for _, doc := range b.modules {
		owned := ownedPaths(doc)
		for _, path := range doc.module.PortFiles {
			if !owned[path] || checked[path] {
				continue
			}
			checked[path] = true
			data, symlink, err := b.readFile(path, maxFileBytes)
			if err != nil {
				return err
			}
			if symlink {
				continue
			}
			b.checkPortRange(path, data)
			isProject := b.isProjectScope(path)
			layout := newLineLayout(path, data)
			for _, token := range ports.Scan(data) {
				text := string(data[token.Start:token.End])
				isBound := token.Offset == bandLowOffset || token.Offset == bandHighOffset
				switch {
				case token.Band != b.portBand:
					b.addPlain(CodePortBandMixed, path, fmt.Sprintf("line %d: token %s has band %d, the reference band is %d", token.Line, text, token.Band, b.portBand))
				case isBound:
				case isDeclared(declared, token.Offset):
					isShared := declared[token.Offset]
					if isProject && !isShared && !layout.inRun(token.Start) {
						b.addPlain(CodePortUnscoped, path, fmt.Sprintf("line %d: token %s has offset %d, a port of one instance, outside every run of a project-scope file", token.Line, text, token.Offset))
					}
				default:
					b.addPlain(CodePortUndeclared, path, fmt.Sprintf("line %d: token %s has offset %d, which no module declares", token.Line, text, token.Offset))
				}
			}
		}
	}
	return nil
}

// checkPortRange applies PORT_RANGE_IN_RUN: the band range text "1NN00-1NN99"
// inside a run or in a component-scope file would be read as the port of the
// component, so it stays in project-scope text outside every run.
func (b *builder) checkPortRange(path string, data []byte) {
	needle := []byte(fmt.Sprintf("%d%02d-%d%02d", b.portBand, bandLowOffset, b.portBand, bandHighOffset))
	if !bytes.Contains(data, needle) {
		return
	}
	isComponentScope := b.isPerComponentPath(path)
	for _, stack := range stackTable {
		isComponentScope = isComponentScope || strings.HasPrefix(path, stack.root)
	}
	var runs []markers.Run
	if _, hasSyntax := markers.SyntaxFor(path); hasSyntax && !isComponentScope {
		blocks, issues := markers.Parse(path, data)
		if len(issues) == 0 {
			runs = markers.Runs(data, blocks)
		}
	}
	lines := bytes.SplitAfter(data, []byte{'\n'})
	for index, line := range lines {
		if !bytes.Contains(line, needle) {
			continue
		}
		isInRun := isComponentScope
		for _, run := range runs {
			isInRun = isInRun || (index >= run.Start && index <= run.End)
		}
		if isInRun {
			b.addPlain(CodePortRangeInRun, path, fmt.Sprintf("line %d: the band range %s sits inside a run or a component-scope file, where its first token is read as a port", index+1, needle))
		}
	}
}

// recordedPortFiles returns the sorted port files of a module.
func recordedPortFiles(files []string) []string {
	if len(files) == 0 {
		return nil
	}
	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	return sorted
}

// portBand checks the reference band of a decoded catalog.
func (v *validator) portBand(c Catalog) {
	if c.PortBand != 0 && !ports.ValidBand(c.PortBand) {
		v.add("/portBand", "must be 0 or an integer from 100 to 654")
		return
	}
	if c.PortBand != 0 {
		return
	}
	for _, stack := range c.Stacks {
		for _, module := range stack.Modules {
			if len(module.Ports) > 0 || len(module.PortFiles) > 0 {
				v.add("/portBand", "must be set when a module declares ports")
				return
			}
		}
	}
}

// modulePorts checks the ports and port files of one decoded module.
func (v *validator) modulePorts(pointer string, module ModuleEntry) {
	for i, port := range module.Ports {
		at := fmt.Sprintf("%s/ports/%d", pointer, i)
		if !names.ValidLabel(port.Name) {
			v.add(at+"/name", names.LabelMessage(port.Name))
		}
		if port.Offset < bandLowOffset || port.Offset > maxPortOffset {
			v.add(at+"/offset", "must be from 0 to 98")
		}
		if message := strideProblem(port); message != "" {
			v.add(at+"/stride", message)
		}
	}
	for i, path := range module.PortFiles {
		if !validTrackedPath(path) {
			v.add(fmt.Sprintf("%s/portFiles/%d", pointer, i), "must be a repo-relative path")
		}
	}
	v.sortedStrings(pointer+"/portFiles", module.PortFiles)
	for i, path := range module.PerComponent {
		if !validTrackedPath(strings.TrimSuffix(path, "/")) {
			v.add(fmt.Sprintf("%s/perComponent/%d", pointer, i), "must be a repo-relative path")
		}
	}
	v.sortedStrings(pointer+"/perComponent", module.PerComponent)
}
