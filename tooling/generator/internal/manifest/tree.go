package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
)

// Kind is the type of a tree node.
type Kind int

// Node kinds. A JSON number is KindInt when its literal has no '.', 'e' or 'E',
// else KindFloat.
const (
	KindNull Kind = iota
	KindBool
	KindInt
	KindFloat
	KindString
	KindSeq
	KindMap
)

// maxTreeDepth bounds the nesting ParseJSON walks.
const maxTreeDepth = 64

// Node is one value of an input file, format-neutral.
type Node struct {
	Kind Kind
	// Value is the scalar text: the unquoted string, the number literal, or
	// "true" and "false".
	Value string
	// Line is the 1-based source line; 0 when unknown (JSON).
	Line int
	// Format is the source format of a root node returned by ParseYAML;
	// FormatNone elsewhere.
	Format Format
	// Items holds the elements of a KindSeq.
	Items []*Node
	// Members holds the keys of a KindMap in source order; duplicates are
	// reported, not kept.
	Members []Member
}

// Member is one key of a map node.
type Member struct {
	Key   string
	Line  int
	Value *Node
}

// Format is the input file format of a selection.
type Format int

// Input file formats.
const (
	FormatNone Format = iota
	FormatJSON
	FormatYAML
)

// Member returns the value of key and whether the map has it.
func (n *Node) Member(key string) (*Node, bool) {
	for _, member := range n.Members {
		if member.Key == key {
			return member.Value, true
		}
	}
	return nil, false
}

// ParseJSON parses data as exactly one JSON value and returns its tree. Issues
// are SELECTION_INVALID at the source: an empty or malformed document, data
// after the value, nesting deeper than 64 levels, and a duplicate key at the
// key. With any issue the returned tree is nil.
func ParseJSON(source string, data []byte) (*Node, []issue.Issue) {
	w := &treeWalker{d: newDecoder(source, CodeSelectionInvalid)}
	raw, ok := w.d.syntax(data)
	if !ok {
		return nil, w.d.result()
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	root, err := w.value(dec, "", 0)
	if err != nil {
		if !errors.Is(err, errTooDeep) {
			w.d.add("", "invalid JSON")
		}
		return nil, w.d.result()
	}
	issues := w.d.result()
	if len(issues) > 0 {
		return nil, issues
	}
	return root, nil
}

type treeWalker struct {
	d *decoder
}

// value reads the next value of dec, which holds well-formed JSON. pointer is
// the JSON pointer of the value.
func (w *treeWalker) value(dec *json.Decoder, pointer string, depth int) (*Node, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, isDelim := token.(json.Delim); isDelim {
		if depth >= maxTreeDepth {
			w.d.add(pointer, fmt.Sprintf("nested deeper than %d levels", maxTreeDepth))
			return nil, errTooDeep
		}
		if delim == '[' {
			return w.sequence(dec, pointer, depth)
		}
		return w.mapping(dec, pointer, depth)
	}
	return scalarNode(token), nil
}

var errTooDeep = errors.New("nested too deep")

func scalarNode(token json.Token) *Node {
	switch v := token.(type) {
	case nil:
		return &Node{Kind: KindNull}
	case bool:
		return &Node{Kind: KindBool, Value: fmt.Sprint(v)}
	case json.Number:
		kind := KindInt
		if strings.ContainsAny(v.String(), ".eE") {
			kind = KindFloat
		}
		return &Node{Kind: kind, Value: v.String()}
	case string:
		return &Node{Kind: KindString, Value: v}
	}
	return &Node{Kind: KindNull}
}

func (w *treeWalker) sequence(dec *json.Decoder, pointer string, depth int) (*Node, error) {
	node := &Node{Kind: KindSeq}
	for dec.More() {
		item, err := w.value(dec, fmt.Sprintf("%s/%d", pointer, len(node.Items)), depth+1)
		if err != nil {
			return nil, err
		}
		node.Items = append(node.Items, item)
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return node, nil
}

func (w *treeWalker) mapping(dec *json.Decoder, pointer string, depth int) (*Node, error) {
	node := &Node{Kind: KindMap}
	seen := map[string]bool{}
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, isString := keyToken.(string)
		if !isString {
			return nil, errors.New("object key is not a string")
		}
		member, err := w.value(dec, pointer+issue.Pointer(key), depth+1)
		if err != nil {
			return nil, err
		}
		if seen[key] {
			w.d.add(pointer+issue.Pointer(key), "duplicate key")
			continue
		}
		seen[key] = true
		node.Members = append(node.Members, Member{Key: key, Value: member})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return node, nil
}

// EncodeJSON writes the tree as compact JSON. It is the inverse of ParseJSON
// for a tree that ParseJSON returned.
func EncodeJSON(n *Node) ([]byte, error) {
	var out bytes.Buffer
	if err := encodeNode(&out, n); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func encodeNode(out *bytes.Buffer, n *Node) error {
	if n == nil {
		return errors.New("nil node")
	}
	switch n.Kind {
	case KindNull:
		out.WriteString("null")
	case KindBool, KindInt, KindFloat:
		out.WriteString(n.Value)
	case KindString:
		encoded, err := json.Marshal(n.Value)
		if err != nil {
			return err
		}
		out.Write(encoded)
	case KindSeq:
		out.WriteByte('[')
		for i, item := range n.Items {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := encodeNode(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case KindMap:
		out.WriteByte('{')
		for i, member := range n.Members {
			if i > 0 {
				out.WriteByte(',')
			}
			key, err := json.Marshal(member.Key)
			if err != nil {
				return err
			}
			out.Write(key)
			out.WriteByte(':')
			if err := encodeNode(out, member.Value); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return io.ErrUnexpectedEOF
	}
	return nil
}
