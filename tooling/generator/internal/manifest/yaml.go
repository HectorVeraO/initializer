package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
)

// yamlErrorLine extracts the line from a YAML syntax error, which carries it
// only in its text.
var yamlErrorLine = regexp.MustCompile(`line (\d+)`)

// yamlErrorPrefix is what a YAML error text starts with before the reason.
var yamlErrorPrefix = regexp.MustCompile(`^(yaml: )?(line \d+: )?`)

// decimalInteger is the integer literal a selection accepts: no sign on
// positive numbers, no leading zeros, no base prefixes, no separators.
var decimalInteger = regexp.MustCompile(`^(0|-?[1-9][0-9]*)$`)

// allowedYAMLTags are the tags a selection may use, explicit or resolved.
var allowedYAMLTags = map[string]bool{
	"!!str": true, "!!int": true, "!!bool": true, "!!seq": true,
	"!!map": true, "!!null": true, "!!float": true,
}

// ParseYAML parses data as exactly one YAML document under the node walk and
// returns its tree. Issues are SELECTION_INVALID: a syntax error or a second
// document at the source with the error's line, and each forbidden construct
// at its pointer and line. With any issue the returned tree is nil. The root
// of a returned tree has Format FormatYAML. A float's Value is a canonical
// exponent literal ("1e999" when it is not finite), so the tree renders as
// valid JSON.
func ParseYAML(source string, data []byte) (*Node, []issue.Issue) {
	d := newDecoder(source, CodeSelectionInvalid)
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	err := decoder.Decode(&doc)
	if errors.Is(err, io.EOF) {
		return &Node{Kind: KindNull, Line: 1, Format: FormatYAML}, nil
	}
	if err != nil {
		d.addAt(CodeSelectionInvalid, "", yamlLine(err), "invalid YAML: "+yamlReason(err), "")
		return nil, d.result()
	}
	var second yaml.Node
	err = decoder.Decode(&second)
	if !errors.Is(err, io.EOF) {
		line := second.Line
		if err != nil {
			line = yamlLine(err)
		}
		d.addAt(CodeSelectionInvalid, "", line, "the file must hold one YAML document", "")
		return nil, d.result()
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		d.addAt(CodeSelectionInvalid, "", doc.Line, "invalid YAML: no document", "")
		return nil, d.result()
	}
	w := &yamlWalker{d: d}
	root := w.node(doc.Content[0], "", 0)
	issues := d.result()
	if len(issues) > 0 || root == nil {
		return nil, issues
	}
	root.Format = FormatYAML
	return root, nil
}

// yamlLine returns the line a YAML error names, or 0.
func yamlLine(err error) int {
	match := yamlErrorLine.FindStringSubmatch(err.Error())
	if match == nil {
		return 0
	}
	line, convErr := strconv.Atoi(match[1])
	if convErr != nil {
		return 0
	}
	return line
}

// yamlReason returns the reason of a YAML error on one line.
func yamlReason(err error) string {
	text := strings.Join(strings.Fields(err.Error()), " ")
	return yamlErrorPrefix.ReplaceAllString(text, "")
}

type yamlWalker struct {
	d *decoder
}

// node maps one YAML node at pointer to a tree node. It reports every
// forbidden construct below it and returns nil only when it cannot descend.
func (w *yamlWalker) node(n *yaml.Node, pointer string, depth int) *Node {
	if n.Kind == yaml.AliasNode {
		w.d.addAt(CodeSelectionInvalid, pointer, n.Line, "anchors and aliases are not allowed", "")
		return &Node{Kind: KindNull, Line: n.Line}
	}
	if n.Anchor != "" {
		w.d.addAt(CodeSelectionInvalid, pointer, n.Line, "anchors and aliases are not allowed", "")
	}
	tag := n.ShortTag()
	if !allowedYAMLTags[tag] {
		w.d.addAt(CodeSelectionInvalid, pointer, n.Line, fmt.Sprintf("tag %s is not allowed", tag),
			"quote the value to keep it a string")
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return w.scalar(n, tag, pointer)
	case yaml.SequenceNode, yaml.MappingNode:
		if depth >= maxTreeDepth {
			w.d.addAt(CodeSelectionInvalid, pointer, n.Line, fmt.Sprintf("nested deeper than %d levels", maxTreeDepth), "")
			return &Node{Kind: KindNull, Line: n.Line}
		}
		if n.Kind == yaml.SequenceNode {
			return w.sequence(n, pointer, depth)
		}
		return w.mapping(n, pointer, depth)
	}
	w.d.addAt(CodeSelectionInvalid, pointer, n.Line, "invalid YAML", "")
	return &Node{Kind: KindNull, Line: n.Line}
}

func (w *yamlWalker) scalar(n *yaml.Node, tag, pointer string) *Node {
	node := &Node{Line: n.Line}
	switch tag {
	case "!!int":
		node.Kind, node.Value = KindInt, n.Value
		if !decimalInteger.MatchString(n.Value) {
			w.d.addAt(CodeSelectionInvalid, pointer, n.Line, "must be a decimal integer", "")
		}
	case "!!bool":
		var value bool
		if err := n.Decode(&value); err != nil {
			w.d.addAt(CodeSelectionInvalid, pointer, n.Line, "must be true or false", "")
		}
		node.Kind, node.Value = KindBool, strconv.FormatBool(value)
	case "!!float":
		node.Kind, node.Value = KindFloat, canonicalFloat(n.Value)
	case "!!null":
		node.Kind = KindNull
	default:
		node.Kind, node.Value = KindString, n.Value
	}
	return node
}

// canonicalFloat renders a YAML float as a JSON number literal with an
// exponent, so it stays a float; a value that is not finite becomes 1e999.
func canonicalFloat(text string) string {
	value, err := strconv.ParseFloat(strings.ReplaceAll(text, "_", ""), 64)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
		return "1e999"
	}
	return strconv.FormatFloat(value, 'e', -1, 64)
}

func (w *yamlWalker) sequence(n *yaml.Node, pointer string, depth int) *Node {
	node := &Node{Kind: KindSeq, Line: n.Line}
	for i, item := range n.Content {
		node.Items = append(node.Items, w.node(item, fmt.Sprintf("%s/%d", pointer, i), depth+1))
	}
	return node
}

func (w *yamlWalker) mapping(n *yaml.Node, pointer string, depth int) *Node {
	node := &Node{Kind: KindMap, Line: n.Line}
	firstLine := map[string]int{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		keyNode, value := n.Content[i], n.Content[i+1]
		key, ok := w.key(keyNode, pointer)
		memberPointer := pointer + issue.Pointer(key)
		if !ok {
			w.node(value, memberPointer, depth+1)
			continue
		}
		if first, seen := firstLine[key]; seen {
			w.d.addAt(CodeSelectionInvalid, memberPointer, keyNode.Line,
				fmt.Sprintf("%s is given twice (first at line %d)", key, first), "")
			w.node(value, memberPointer, depth+1)
			continue
		}
		firstLine[key] = keyNode.Line
		member := w.node(value, memberPointer, depth+1)
		node.Members = append(node.Members, Member{Key: key, Line: keyNode.Line, Value: member})
	}
	return node
}

// key returns the text of a map key and whether it is a valid string key. A
// key that is not a scalar is reported at the map's own pointer.
func (w *yamlWalker) key(k *yaml.Node, mapPointer string) (string, bool) {
	if k.Kind == yaml.AliasNode || k.Anchor != "" {
		w.d.addAt(CodeSelectionInvalid, mapPointer+issue.Pointer(k.Value), k.Line, "anchors and aliases are not allowed", "")
		return k.Value, false
	}
	if k.Kind != yaml.ScalarNode {
		w.d.addAt(CodeSelectionInvalid, mapPointer, k.Line, "keys must be strings", "")
		return "", false
	}
	pointer := mapPointer + issue.Pointer(k.Value)
	switch k.ShortTag() {
	case "!!merge":
		w.d.addAt(CodeSelectionInvalid, pointer, k.Line, "merge keys are not allowed", "")
		return k.Value, false
	case "!!str":
		return k.Value, true
	}
	w.d.addAt(CodeSelectionInvalid, pointer, k.Line, "keys must be strings", "")
	return k.Value, false
}
