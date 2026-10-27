// Package manifest decodes and validates module manifests, spec manifests and
// project selections. Decoding is strict: unknown keys, duplicate keys and
// wrong types are reported, and every problem found is collected. Unknown and
// duplicate keys are found by walking the JSON tokens instead of
// json.Decoder.DisallowUnknownFields, because that stops at the first problem.
package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/issue"
)

// Issue codes emitted by this package.
const (
	CodeManifestInvalid  = "MANIFEST_INVALID"
	CodeStackUnknown     = "STACK_UNKNOWN"
	CodeSelectionInvalid = "SELECTION_INVALID"
)

// decoder collects the issues of one input. At most one issue is kept per
// pointer, so a wrong type is not followed by a rule issue on the same value.
type decoder struct {
	source string
	code   string
	issues []issue.Issue
	seen   map[string]bool
}

func newDecoder(source, code string) *decoder {
	return &decoder{source: source, code: code, seen: map[string]bool{}}
}

func (d *decoder) add(pointer, message string) {
	d.addCode(d.code, pointer, message)
}

func (d *decoder) addCode(code, pointer, message string) {
	if d.seen[pointer] {
		return
	}
	d.seen[pointer] = true
	d.issues = append(d.issues, issue.Issue{
		Code:    code,
		Path:    issue.Path(d.source, pointer),
		Message: message,
	})
}

func (d *decoder) result() []issue.Issue {
	issue.Sort(d.issues)
	return d.issues
}

// syntax returns data as exactly one well-formed JSON value.
func (d *decoder) syntax(data []byte) (json.RawMessage, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var root json.RawMessage
	err := dec.Decode(&root)
	if errors.Is(err, io.EOF) {
		d.add("", "input is empty")
		return nil, false
	}
	if err != nil {
		var syntaxErr *json.SyntaxError
		offset := dec.InputOffset()
		if errors.As(err, &syntaxErr) {
			offset = syntaxErr.Offset
		}
		d.add("", fmt.Sprintf("invalid JSON at offset %d", offset))
		return nil, false
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		d.add("", fmt.Sprintf("unexpected data after the JSON value at offset %d", dec.InputOffset()))
		return nil, false
	}
	return root, true
}

// object splits raw into its members. A duplicate key or a key outside allowed
// is reported at that key; the member is still returned so later rules run.
func (d *decoder) object(raw json.RawMessage, pointer string, allowed []string) (map[string]json.RawMessage, bool) {
	if firstByte(raw) != '{' {
		d.add(pointer, "must be an object")
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil {
		d.add(pointer, "must be an object")
		return nil, false
	}
	members := map[string]json.RawMessage{}
	allowedSet := map[string]bool{}
	for _, key := range allowed {
		allowedSet[key] = true
	}
	for dec.More() {
		keyToken, err := dec.Token()
		key, isString := keyToken.(string)
		if err != nil || !isString {
			d.add(pointer, "must be an object")
			return nil, false
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			d.add(pointer, "must be an object")
			return nil, false
		}
		if _, duplicate := members[key]; duplicate {
			d.add(pointer+issue.Pointer(key), "duplicate key")
			continue
		}
		members[key] = value
		if allowed != nil && !allowedSet[key] {
			d.add(pointer+issue.Pointer(key), "unknown key")
		}
	}
	return members, true
}

// required returns members[key], reporting a missing key.
func (d *decoder) required(members map[string]json.RawMessage, pointer, key string) (json.RawMessage, bool) {
	value, found := members[key]
	if !found {
		d.add(pointer+issue.Pointer(key), "required key is missing")
		return nil, false
	}
	return value, true
}

func (d *decoder) schema(members map[string]json.RawMessage) {
	raw, ok := d.required(members, "", "schema")
	if !ok {
		return
	}
	if string(bytes.TrimSpace(raw)) != "1" {
		d.add("/schema", "must be the number 1")
	}
}

func (d *decoder) str(raw json.RawMessage, pointer string) (string, bool) {
	if firstByte(raw) != '"' {
		d.add(pointer, "must be a string")
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		d.add(pointer, "must be a string")
		return "", false
	}
	return value, true
}

func (d *decoder) boolean(raw json.RawMessage, pointer string) (bool, bool) {
	switch string(bytes.TrimSpace(raw)) {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	d.add(pointer, "must be a boolean")
	return false, false
}

func (d *decoder) array(raw json.RawMessage, pointer string) ([]json.RawMessage, bool) {
	if firstByte(raw) != '[' {
		d.add(pointer, "must be an array")
		return nil, false
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		d.add(pointer, "must be an array")
		return nil, false
	}
	return items, true
}

// strings decodes an array of strings; valid[i] is false for an element that
// is not a string (already reported).
func (d *decoder) strings(raw json.RawMessage, pointer string) ([]string, []bool, bool) {
	items, ok := d.array(raw, pointer)
	if !ok {
		return nil, nil, false
	}
	values := make([]string, len(items))
	valid := make([]bool, len(items))
	for i, item := range items {
		values[i], valid[i] = d.str(item, fmt.Sprintf("%s/%d", pointer, i))
	}
	return values, valid, true
}

// checkOrder reports an element that is lower than or equal to the previous
// valid one: strictly ascending bytewise order means sorted and unique.
func (d *decoder) checkOrder(pointer string, values []string, valid []bool) {
	previous := ""
	havePrevious := false
	for i, value := range values {
		if !valid[i] {
			continue
		}
		if havePrevious && value == previous {
			d.add(fmt.Sprintf("%s/%d", pointer, i), "duplicate entry")
		} else if havePrevious && value < previous {
			d.add(fmt.Sprintf("%s/%d", pointer, i), "entries must be sorted bytewise")
		}
		previous = value
		havePrevious = true
	}
}

func firstByte(raw json.RawMessage) byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return 0
	}
	return trimmed[0]
}
