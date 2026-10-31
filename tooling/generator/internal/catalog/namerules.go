package catalog

import (
	"fmt"
	"sort"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/markers"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
)

// referenceMissing is the REFERENCE_INVALID message of an absent reference file.
const referenceMissing = "some manifest declares name rules and the reference file is not tracked"

// stackIDs returns the ids of the stack table.
func stackIDs() []string {
	ids := make([]string, len(stackTable))
	for i, info := range stackTable {
		ids[i] = info.name
	}
	return ids
}

// readReference decodes the reference file into the slot map of the catalog.
// An absent file leaves the map empty; checkNameRules reports it when a rule
// needs it. referenceReady is true only for a valid, decoded reference.
func (b *builder) readReference() error {
	b.reference = map[string]string{}
	if !b.set[ReferencePath] {
		return nil
	}
	data, symlink, err := b.readFile(ReferencePath, maxManifestBytes)
	if err != nil {
		return err
	}
	if symlink {
		b.addPlain(CodeSymlink, ReferencePath, "tracked path is a symlink")
		return nil
	}
	selection, issues := manifest.DecodeReference(data)
	if len(issues) > 0 {
		b.issues = append(b.issues, issues...)
		return nil
	}
	b.referenceDecoded = true
	if selection.Ports != nil {
		b.portBand = selection.Ports.Band
	}
	resolved := names.Resolved{Project: selection.Project.Name, DisplayName: selection.DisplayName, Components: map[string]string{}}
	if selection.Database != nil {
		resolved.Database, resolved.DBUser = selection.Database.Name, selection.Database.User
	}
	if len(selection.Services) > 0 {
		resolved.Services = selection.Services
	}
	entries := map[string]bool{}
	before := len(b.issues)
	for i, component := range selection.Components {
		if entries[component.Stack] {
			b.add(CodeReferenceInvalid, ReferencePath, fmt.Sprintf("/components/%d/stack", i), "stack is listed twice")
		}
		entries[component.Stack] = true
		if component.Name != "" {
			resolved.Components[component.Stack] = component.Name
		}
	}
	for _, info := range stackTable {
		if !entries[info.name] {
			b.add(CodeReferenceInvalid, ReferencePath, "/components", "stack "+info.name+" of the stack table has no entry")
		}
	}
	slots, issues := names.Reference(resolved)
	b.issues = append(b.issues, issues...)
	if len(b.issues) == before {
		b.reference = slots
		b.referenceReady = true
	}
	return nil
}

// nameRule is a rule that passed the checks that need no file content.
type nameRule struct {
	index int
	rule  manifest.NameRule
	slots []string
}

// checkNameRules applies the name rule checks of every module on source bytes
// and records each rule with its blocks.
func (b *builder) checkNameRules() error {
	declared := false
	for _, doc := range b.modules {
		declared = declared || len(doc.module.Names) > 0
	}
	if declared && !b.set[ReferencePath] {
		b.addPlain(CodeReferenceInvalid, ReferencePath, referenceMissing)
	}
	for _, doc := range b.modules {
		if err := b.checkModuleRules(doc); err != nil {
			return err
		}
	}
	return nil
}

func (b *builder) checkModuleRules(doc *moduleDoc) error {
	owned := map[string]bool{}
	for _, file := range doc.files {
		owned[file.Path] = true
	}
	ids := stackIDs()
	type key struct{ path, text string }
	seen := map[key]bool{}
	byPath := map[string][]nameRule{}
	for i, rule := range doc.module.Names {
		pointer := fmt.Sprintf("/names/%d", i)
		isValid := true
		slots, err := names.Slots(rule.Text)
		if err != nil {
			b.add(CodeNameRuleInvalid, doc.path, pointer+"/text", err.Error())
			isValid = false
		}
		for _, slot := range slots {
			if !names.IsSlot(slot, ids) {
				b.add(CodeNameRuleInvalid, doc.path, pointer+"/text", fmt.Sprintf("slot %q names a stack outside the stack table", slot))
				isValid = false
			}
		}
		if b.goModule != "" && strings.Contains(rule.Text, b.goModule) {
			b.add(CodeNameRuleInvalid, doc.path, pointer+"/text", "text must not contain the Go module path")
			isValid = false
		}
		id := key{rule.Path, rule.Text}
		if seen[id] {
			b.add(CodeNameRuleInvalid, doc.path, pointer, "duplicate rule for this path and text")
			isValid = false
		}
		seen[id] = true
		if !owned[rule.Path] {
			b.add(CodeNameRuleForeign, doc.path, pointer+"/path", fmt.Sprintf("module %s does not own %s", doc.module.Module, rule.Path))
			isValid = false
		}
		if isValid {
			byPath[rule.Path] = append(byPath[rule.Path], nameRule{index: i, rule: rule, slots: slots})
		}
	}
	if !b.referenceReady {
		return nil
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := b.checkFileRules(doc, path, byPath[path]); err != nil {
			return err
		}
	}
	sort.SliceStable(doc.recorded, func(i, j int) bool {
		left, right := doc.recorded[i], doc.recorded[j]
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.Text < right.Text
	})
	return nil
}

// checkFileRules finds the occurrences of the rules of one file and records
// the blocks that enclose them.
func (b *builder) checkFileRules(doc *moduleDoc, path string, rules []nameRule) error {
	var usable []nameRule
	for _, candidate := range rules {
		isComplete := true
		for _, slot := range candidate.slots {
			if _, has := b.reference[slot]; !has {
				pointer := fmt.Sprintf("/names/%d/text", candidate.index)
				b.add(CodeReferenceIncomplete, doc.path, pointer, fmt.Sprintf("slot %q has no explicit value in %s", slot, ReferencePath))
				isComplete = false
			}
		}
		if isComplete {
			usable = append(usable, candidate)
		}
	}
	if len(usable) == 0 {
		return nil
	}
	data, symlink, err := b.readFile(path, maxFileBytes)
	if err != nil {
		return err
	}
	if symlink {
		return nil
	}
	engine := make([]names.Rule, len(usable))
	for i, candidate := range usable {
		engine[i] = names.Rule{Path: path, Text: candidate.rule.Text}
	}
	occurrences, findErr := names.Find(data, engine, b.reference)
	if findErr != nil {
		b.addPlain(CodeNameRuleOverlap, path, findErr.Error())
	}
	layout := newLineLayout(path, data)
	blocks := make([]map[string]bool, len(usable))
	for i := range blocks {
		blocks[i] = map[string]bool{}
	}
	for _, occurrence := range occurrences {
		if line, touches := layout.touchedMarker(occurrence); touches {
			b.addPlain(CodeNameRuleOverlap, path, fmt.Sprintf("rule %q at byte %d touches the marker on line %d", usable[occurrence.Rule].rule.Text, occurrence.Start, line+1))
		}
		blocks[occurrence.Rule][layout.enclosing(occurrence)] = true
		if b.isProjectScope(path) && !layout.inRun(occurrence.Start) {
			b.checkScoped(path, usable[occurrence.Rule], layout.lineOf(occurrence.Start))
		}
	}
	for i, candidate := range usable {
		if len(blocks[i]) == 0 {
			pointer := fmt.Sprintf("/names/%d/text", candidate.index)
			b.add(CodeNameRuleUnmatched, doc.path, pointer, "the reference expansion does not occur in "+path)
			continue
		}
		recorded := make([]string, 0, len(blocks[i]))
		for block := range blocks[i] {
			recorded = append(recorded, block)
		}
		sort.Strings(recorded)
		doc.recorded = append(doc.recorded, NameRule{Path: path, Text: candidate.rule.Text, Blocks: recorded})
	}
	return nil
}

// checkScoped applies NAME_RULE_UNSCOPED to one occurrence outside every run
// of a project-scope file: only a project-level slot is allowed there.
func (b *builder) checkScoped(path string, rule nameRule, line int) {
	for _, slot := range rule.slots {
		if names.IsProjectLevel(slot) {
			continue
		}
		b.addPlain(CodeNameRuleUnscoped, path,
			fmt.Sprintf("line %d: rule %q uses slot %q, which is per instance, outside every run of a project-scope file", line+1, rule.rule.Text, slot))
		return
	}
}

// lineLayout maps byte offsets of one file to its lines and marker blocks.
type lineLayout struct {
	starts []int
	data   []byte
	blocks []markers.Block
	runs   []markers.Run
}

// inRun reports whether the byte at offset sits on a line of a run.
func (l lineLayout) inRun(offset int) bool {
	position := offset
	if position < len(l.data) && l.data[position] == '\n' && position+1 < len(l.data) {
		position++
	}
	line := l.lineOf(position)
	for _, run := range l.runs {
		if line >= run.Start && line <= run.End {
			return true
		}
	}
	return false
}

func newLineLayout(path string, data []byte) lineLayout {
	layout := lineLayout{starts: []int{0}, data: data}
	for i, c := range data {
		if c == '\n' {
			layout.starts = append(layout.starts, i+1)
		}
	}
	if _, hasMarkers := markers.SyntaxFor(path); hasMarkers {
		var issues []issue.Issue
		layout.blocks, issues = markers.Parse(path, data)
		if len(issues) == 0 {
			layout.runs = markers.Runs(data, layout.blocks)
		}
	}
	return layout
}

// lineOf returns the 0-based line index that holds the byte at offset.
func (l lineLayout) lineOf(offset int) int {
	next := sort.Search(len(l.starts), func(i int) bool { return l.starts[i] > offset })
	return next - 1
}

// lineRange returns the bytes of a line from its first byte up to, but not
// including, its terminating newline.
func (l lineLayout) lineRange(line int) (start, end int) {
	start = l.starts[line]
	end = len(l.data)
	if line+1 < len(l.starts) {
		end = l.starts[line+1] - 1
	}
	return start, end
}

// touchedMarker reports the 0-based line of the marker line an occurrence
// intersects. The newline that ends the last end marker of a run belongs to
// the marker: generate splits the file there, so an occurrence starting on that
// newline would span two segments and no rule could match it.
func (l lineLayout) touchedMarker(o names.Occurrence) (int, bool) {
	for _, block := range l.blocks {
		for _, line := range []int{block.Start, block.End} {
			start, end := l.lineRange(line)
			if o.Start < end && start < o.End {
				return line, true
			}
		}
	}
	for _, run := range l.runs {
		_, end := l.lineRange(run.End)
		if o.Start == end && o.End > o.Start {
			return run.End, true
		}
	}
	return 0, false
}

// enclosing returns the block that holds the occurrence: "" outside every
// block, else "module:<id>" or "stack:<id>". An occurrence that starts with
// the newline that ends a line belongs to the next line.
func (l lineLayout) enclosing(o names.Occurrence) string {
	position := o.Start
	if l.data[position] == '\n' && position+1 < len(l.data) {
		position++
	}
	line := l.lineOf(position)
	for _, block := range l.blocks {
		if block.Start < line && line < block.End {
			return block.Ref()
		}
	}
	return ""
}
