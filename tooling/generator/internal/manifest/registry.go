package manifest

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/names"
)

// FieldSpec is one key of the selection document at one schema.
type FieldSpec struct {
	// Pointer is a JSON pointer pattern; "*" matches one array index or map key.
	Pointer string
	Kind    Kind
	// Since is the first schema that accepts the key.
	Since int
	// Required1 is true when a schema 1 document must have the key.
	Required1 bool
	// Required is true when the key must be present whenever its parent is, at
	// every schema.
	Required bool
}

// Registry lists every selection key. A track that adds a field appends its rows.
var Registry = []FieldSpec{
	{Pointer: "/schema", Kind: KindInt, Since: 1, Required1: true},
	{Pointer: "/project", Kind: KindMap, Since: 1, Required1: true},
	{Pointer: "/project/name", Kind: KindString, Since: 1, Required1: true},
	{Pointer: "/project/goModule", Kind: KindString, Since: 1},
	{Pointer: "/stacks", Kind: KindSeq, Since: 1, Required1: true},
	{Pointer: "/stacks/*", Kind: KindString, Since: 1},
	{Pointer: "/modules", Kind: KindSeq, Since: 1, Required1: true},
	{Pointer: "/modules/*", Kind: KindString, Since: 1},
	{Pointer: "/components", Kind: KindSeq, Since: 2},
	{Pointer: "/components/*", Kind: KindMap, Since: 2},
	{Pointer: "/components/*/stack", Kind: KindString, Since: 2, Required: true},
	{Pointer: "/components/*/name", Kind: KindString, Since: 2},
	{Pointer: "/database", Kind: KindMap, Since: 2},
	{Pointer: "/database/name", Kind: KindString, Since: 2},
	{Pointer: "/database/user", Kind: KindString, Since: 2},
	{Pointer: "/services", Kind: KindMap, Since: 2},
	{Pointer: "/services/*", Kind: KindString, Since: 2},
}

// MaxSchema is the highest schema this generator reads and writes.
const MaxSchema = 2

// Patterns of the selection keys the grammar rules and the extraction name.
const (
	patternName       = "/project/name"
	patternGoModule   = "/project/goModule"
	patternStacksItem = "/stacks/*"
	patternModuleItem = "/modules/*"
	patternCompName   = "/components/*/name"
	patternDBName     = "/database/name"
	patternDBUser     = "/database/user"
	patternService    = "/services/*"
)

const setSchemaHint = `set "schema": 2`

// registryRow returns the row of pattern.
func registryRow(pattern string) (FieldSpec, bool) {
	for _, row := range Registry {
		if row.Pointer == pattern {
			return row, true
		}
	}
	return FieldSpec{}, false
}

// childRow returns the row of the child key of the node at parent: the exact
// key, else the "*" row.
func childRow(parent, key string) (FieldSpec, bool) {
	if row, found := registryRow(parent + "/" + key); found {
		return row, true
	}
	return registryRow(parent + "/*")
}

// childRows returns the rows that are direct children of parent, in registry
// order.
func childRows(parent string) []FieldSpec {
	var rows []FieldSpec
	for _, row := range Registry {
		rest, isChild := strings.CutPrefix(row.Pointer, parent+"/")
		if isChild && rest != "" && !strings.Contains(rest, "/") {
			rows = append(rows, row)
		}
	}
	return rows
}

// allowedKeys returns the named keys allowed in the map at parent, sorted.
func allowedKeys(parent string) []string {
	var keys []string
	for _, row := range childRows(parent) {
		key := row.Pointer[strings.LastIndex(row.Pointer, "/")+1:]
		if key != "*" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// kindMessage is the wrong-kind message of a row.
func kindMessage(row FieldSpec) string {
	switch row.Kind {
	case KindString:
		return "must be a string"
	case KindInt:
		return "must be an integer"
	case KindMap:
		return "must be an object"
	case KindSeq:
		item, hasItem := registryRow(row.Pointer + "/*")
		switch {
		case hasItem && item.Kind == KindString:
			return "must be a list of strings"
		case hasItem && item.Kind == KindMap:
			return "must be a list of objects"
		}
		return "must be a list"
	}
	return "has the wrong type"
}

// valueProblem applies the grammar of the value at a registry pattern. It
// returns the issue code and message, and false when the value is valid. The
// same function serves file and flag values.
func valueProblem(pattern, value string) (code, message string, bad bool) {
	switch pattern {
	case patternName, patternCompName:
		if !names.ValidLabel(value) {
			return CodeNameInvalid, names.LabelMessage(value), true
		}
	case patternDBName, patternDBUser:
		if !names.ValidPGIdent(value) {
			return CodeNameInvalid, names.PGIdentMessage(value), true
		}
	case patternService:
		if !names.ValidLabel(value) {
			return CodeNameInvalid, names.LabelMessage(value), true
		}
	case patternGoModule:
		if !validGoModulePath(value) {
			return CodeSelectionInvalid, "must be a Go module path with a lowercase dotted first segment", true
		}
	case patternStacksItem:
		if !isKnownStack(value) {
			return CodeSelectionInvalid, "stack is not in the stack table", true
		}
	case patternModuleItem:
		if !validID(value) {
			return CodeSelectionInvalid, "must be a module id of at most 31 characters", true
		}
	}
	return "", "", false
}

// inputDecoder walks a selection tree against Registry.
type inputDecoder struct {
	d       *decoder
	source  string
	schema  int
	origins map[string]Origin
	// isYAML adds the quoting advice to wrong-kind messages.
	isYAML bool
}

// DecodeInput validates a parsed selection tree against Registry and returns
// the input as given, with the origin of every value. Every problem found is
// returned, at most one per pointer. With a nil tree it returns an empty input.
func DecodeInput(source string, root *Node) (Input, []issue.Issue) {
	w := &inputDecoder{
		d:       newDecoder(source, CodeSelectionInvalid),
		source:  source,
		origins: map[string]Origin{},
	}
	in := Input{Origins: w.origins}
	if root == nil {
		return in, nil
	}
	w.isYAML = root.Format == FormatYAML
	if root.Kind != KindMap {
		w.d.addAt(CodeSelectionInvalid, "", root.Line, "must be an object", "")
		return in, w.d.result()
	}
	w.origins[""] = Origin{Source: source, Line: root.Line}
	declared := w.readSchema(root)
	w.schema = declared
	if declared == 0 {
		w.schema = MaxSchema
	}
	w.walkMap(root, "", "")
	in.Selection = w.extract(root)
	in.Selection.Schema = declared
	return in, w.d.result()
}

// readSchema applies rule 1 and returns the declared schema, or 0 after
// reporting a missing or unsupported one.
func (w *inputDecoder) readSchema(root *Node) int {
	node, found := root.Member("schema")
	if !found {
		w.d.addAt(CodeSelectionInvalid, "/schema", root.Line, "required; must be 1 or 2", "")
		return 0
	}
	if node.Kind == KindInt {
		if value, err := strconv.Atoi(node.Value); err == nil && value >= 1 && value <= MaxSchema {
			return value
		}
	}
	w.d.addAt(CodeSelectionInvalid, "/schema", node.Line, "must be 1 or 2", "")
	return 0
}

// walkMap checks the members of the map node at pattern/pointer.
func (w *inputDecoder) walkMap(node *Node, pattern, pointer string) {
	present := map[string]bool{}
	for _, member := range node.Members {
		present[member.Key] = true
		memberPointer := pointer + issue.Pointer(member.Key)
		row, known := childRow(pattern, member.Key)
		if !known {
			hint := ""
			if closest := names.Closest(member.Key, allowedKeys(pattern)); closest != "" {
				hint = fmt.Sprintf("did you mean %q?", closest)
			}
			w.d.addAt(CodeSelectionInvalid, memberPointer, member.Value.Line, "unknown key", hint)
			continue
		}
		if row.Since > w.schema {
			w.d.addAt(CodeSelectionInvalid, memberPointer, member.Value.Line,
				fmt.Sprintf("%s needs schema %d", member.Key, row.Since), setSchemaHint)
			continue
		}
		w.walk(member.Value, row, memberPointer)
	}
	for _, row := range childRows(pattern) {
		key := row.Pointer[strings.LastIndex(row.Pointer, "/")+1:]
		isRequired := row.Required || (row.Required1 && w.schema == 1)
		if key == "*" || present[key] || row.Since > w.schema || !isRequired {
			continue
		}
		w.d.addAt(CodeSelectionInvalid, pointer+issue.Pointer(key), node.Line, "required", "")
	}
}

// walk checks one value against its row: kind, grammar, then its children.
func (w *inputDecoder) walk(node *Node, row FieldSpec, pointer string) {
	if node.Kind != row.Kind {
		message := kindMessage(row)
		isScalar := node.Kind == KindInt || node.Kind == KindFloat || node.Kind == KindBool
		if w.isYAML && row.Kind == KindString && isScalar {
			message += " (quote it)"
		}
		w.d.addAt(CodeSelectionInvalid, pointer, node.Line, message, "")
		return
	}
	w.origins[pointer] = Origin{Source: w.source, Pointer: pointer, Line: node.Line}
	switch row.Kind {
	case KindString:
		if code, message, bad := valueProblem(row.Pointer, node.Value); bad {
			w.d.addAt(code, pointer, node.Line, message, "")
		}
	case KindSeq:
		item, _ := registryRow(row.Pointer + "/*")
		for i, element := range node.Items {
			w.walk(element, item, fmt.Sprintf("%s/%d", pointer, i))
		}
	case KindMap:
		w.walkMap(node, row.Pointer, pointer)
	}
}

// extract reads the values of a walked tree; a value of the wrong kind is
// skipped (it was reported).
func (w *inputDecoder) extract(root *Node) Selection {
	var s Selection
	if project, found := root.Member("project"); found && project.Kind == KindMap {
		if name, found := project.Member("name"); found && name.Kind == KindString {
			s.Project.Name, s.Has.Name = name.Value, true
		}
		if module, found := project.Member("goModule"); found && module.Kind == KindString {
			s.Project.GoModule, s.Has.GoModule = module.Value, true
		}
	}
	s.Stacks, s.Has.Stacks = stringList(root, "stacks")
	s.Modules, s.Has.Modules = stringList(root, "modules")
	if list, found := root.Member("components"); found && list.Kind == KindSeq && w.schema >= 2 {
		for _, item := range list.Items {
			if item.Kind != KindMap {
				continue
			}
			var component Component
			if stack, found := item.Member("stack"); found && stack.Kind == KindString {
				component.Stack = stack.Value
			}
			if name, found := item.Member("name"); found && name.Kind == KindString {
				component.Name = name.Value
			}
			s.Components = append(s.Components, component)
		}
	}
	if w.schema >= 2 {
		s.Database = extractDatabase(root)
		s.Services = extractServices(root)
	}
	return s
}

// extractDatabase reads the database map of a walked tree; nil when it holds
// no string value.
func extractDatabase(root *Node) *Database {
	node, found := root.Member("database")
	if !found || node.Kind != KindMap {
		return nil
	}
	var database Database
	if name, has := node.Member("name"); has && name.Kind == KindString {
		database.Name = name.Value
	}
	if user, has := node.Member("user"); has && user.Kind == KindString {
		database.User = user.Value
	}
	if database == (Database{}) {
		return nil
	}
	return &database
}

// extractServices reads the services map of a walked tree: its string
// members, nil when there are none.
func extractServices(root *Node) map[string]string {
	node, found := root.Member("services")
	if !found || node.Kind != KindMap {
		return nil
	}
	services := map[string]string{}
	for _, member := range node.Members {
		if member.Value.Kind == KindString {
			services[member.Key] = member.Value.Value
		}
	}
	if len(services) == 0 {
		return nil
	}
	return services
}

// stringList reads the list at key of a map node: its string elements, and
// whether the key holds a list.
func stringList(parent *Node, key string) ([]string, bool) {
	node, found := parent.Member(key)
	if !found || node.Kind != KindSeq {
		return nil, false
	}
	values := make([]string, 0, len(node.Items))
	for _, item := range node.Items {
		if item.Kind == KindString {
			values = append(values, item.Value)
		}
	}
	return values, true
}
