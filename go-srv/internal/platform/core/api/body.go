package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"math/big"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

// MaxBodyBytes is the request body cap of DecodeBody.
const MaxBodyBytes = 1 << 20

// MaxDrainBytes caps the body bytes read and discarded before a 413, so a
// streaming proxy still sending the body sees the 413 and not a reset.
const MaxDrainBytes = 4 << 20

// maxExactFloatInt is 2^53, the largest magnitude a float64 holds exactly.
const maxExactFloatInt = 1 << 53

// Presence says whether a field must be present and non-null.
type Presence bool

const (
	Required Presence = true
	Optional Presence = false
)

// Body is one decoded JSON object, read field by field so that every type
// problem is reported, not only the first.
type Body struct {
	fields map[string]json.RawMessage
	prefix string
}

func malformedBody() *Error {
	failure := NewError(http.StatusUnprocessableEntity, CodeValidationFailed, "Request validation failed")
	failure.Issues = append(failure.Issues, Issue{Field: "", Code: CodeBodyMalformed, Message: "Body must be one JSON object"})
	return failure
}

// drainBody discards the rest of body, never reading more than MaxDrainBytes
// in total, where alreadyRead bytes were consumed before the call.
func drainBody(body io.Reader, alreadyRead int64) {
	if body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, body, MaxDrainBytes-alreadyRead)
}

// DecodeBody checks the media type and size and parses one JSON object:
//   - Content-Type missing or not application/json (parameters allowed): 415
//     UNSUPPORTED_MEDIA_TYPE.
//   - More than MaxBodyBytes: 413 PAYLOAD_TOO_LARGE.
//   - Empty, not JSON, trailing data, or not an object: 422 VALIDATION_FAILED
//     with one issue {field: "", code: BODY_MALFORMED}.
//
// Unknown fields are ignored. Every error is an *Error.
func DecodeBody(r *http.Request) (*Body, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, NewError(http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, "Content-Type must be application/json")
	}
	tooLarge := NewError(http.StatusRequestEntityTooLarge, CodePayloadTooLarge, "Request body is too large")
	if r.ContentLength > MaxBodyBytes {
		drainBody(r.Body, 0)
		return nil, tooLarge
	}
	var data []byte
	if r.Body != nil {
		data, err = io.ReadAll(http.MaxBytesReader(nil, r.Body, MaxBodyBytes))
	}
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		drainBody(r.Body, MaxBodyBytes+1)
		return nil, tooLarge
	}
	if err != nil {
		return nil, malformedBody()
	}
	data = bytes.TrimSpace(data)
	isObject := len(data) > 0 && data[0] == '{' && json.Valid(data)
	if !isObject {
		return nil, malformedBody()
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, malformedBody()
	}
	return &Body{fields: fields}, nil
}

// Has reports whether the field is present, null included.
func (b *Body) Has(name string) bool {
	_, present := b.fields[name]
	return present
}

// IsNull reports whether the field is present and null.
func (b *Body) IsNull(name string) bool {
	raw, present := b.fields[name]
	return present && isNull(raw)
}

// Raw returns the field's bytes for handlers that validate a value
// themselves; false when the field is absent.
func (b *Body) Raw(name string) (json.RawMessage, bool) {
	raw, present := b.fields[name]
	return raw, present
}

func isNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

// lookup returns the field's bytes when it is present and non-null; otherwise
// it records FIELD_REQUIRED for a Required field.
func (b *Body) lookup(p *Problems, name string, presence Presence) (json.RawMessage, bool) {
	raw, present := b.fields[name]
	if !present || isNull(raw) {
		if presence == Required {
			p.Add(b.prefix+name, CodeFieldRequired, "Field is required")
		}
		return nil, false
	}
	return raw, true
}

func (b *Body) typeInvalid(p *Problems, path, want string) {
	p.Add(path, CodeFieldTypeInvalid, "Must be "+want)
}

// String reads a JSON string.
func (b *Body) String(p *Problems, name string, presence Presence) (string, bool) {
	raw, ok := b.lookup(p, name, presence)
	if !ok {
		return "", false
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		b.typeInvalid(p, b.prefix+name, "a string")
		return "", false
	}
	return v, true
}

// Bool reads a JSON boolean.
func (b *Body) Bool(p *Problems, name string, presence Presence) (bool, bool) {
	raw, ok := b.lookup(p, name, presence)
	if !ok {
		return false, false
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		b.typeInvalid(p, b.prefix+name, "a boolean")
		return false, false
	}
	return v, true
}

// Int reads an integral JSON number that fits int64 (1.0 is accepted, 1.5 is not).
func (b *Body) Int(p *Problems, name string, presence Presence) (int64, bool) {
	raw, ok := b.lookup(p, name, presence)
	if !ok {
		return 0, false
	}
	v, valid := parseIntegral(raw)
	if !valid {
		b.typeInvalid(p, b.prefix+name, "an integer")
		return 0, false
	}
	return v, true
}

// Number reads a finite JSON number.
func (b *Body) Number(p *Problems, name string, presence Presence) (float64, bool) {
	raw, ok := b.lookup(p, name, presence)
	if !ok {
		return 0, false
	}
	v, valid := parseNumber(raw)
	if !valid {
		b.typeInvalid(p, b.prefix+name, "a number")
		return 0, false
	}
	return v, true
}

// StringList reads a JSON array of strings; a bad element is reported as "name[i]".
func (b *Body) StringList(p *Problems, name string, presence Presence) ([]string, bool) {
	raw, ok := b.lookup(p, name, presence)
	if !ok {
		return nil, false
	}
	var elements []json.RawMessage
	if err := json.Unmarshal(raw, &elements); err != nil {
		b.typeInvalid(p, b.prefix+name, "an array of strings")
		return nil, false
	}
	values := make([]string, 0, len(elements))
	allValid := true
	for i, element := range elements {
		var s string
		if err := json.Unmarshal(element, &s); err != nil || isNull(element) {
			p.Add(b.prefix+name+"["+strconv.Itoa(i)+"]", CodeFieldTypeInvalid, "Must be a string")
			allValid = false
			continue
		}
		values = append(values, s)
	}
	if !allValid {
		return nil, false
	}
	return values, true
}

// Object reads a nested JSON object; issues read from it carry the "name." prefix.
func (b *Body) Object(p *Problems, name string, presence Presence) (*Body, bool) {
	raw, ok := b.lookup(p, name, presence)
	if !ok {
		return nil, false
	}
	fields := map[string]json.RawMessage{}
	isObject := len(bytes.TrimSpace(raw)) > 0 && bytes.TrimSpace(raw)[0] == '{'
	if !isObject || json.Unmarshal(raw, &fields) != nil {
		b.typeInvalid(p, b.prefix+name, "an object")
		return nil, false
	}
	return &Body{fields: fields, prefix: b.prefix + name + "."}, true
}

func looksNumeric(raw json.RawMessage) (string, bool) {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return "", false
	}
	first := s[0]
	isNumeric := first == '-' || (first >= '0' && first <= '9')
	return s, isNumeric
}

func parseNumber(raw json.RawMessage) (float64, bool) {
	s, ok := looksNumeric(raw)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, false
	}
	return f, true
}

func parseIntegral(raw json.RawMessage) (int64, bool) {
	s, ok := looksNumeric(raw)
	if !ok {
		return 0, false
	}
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v, true
	}
	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	hasExponent := strings.ContainsAny(s, "eE")
	if hasFrac && !hasExponent && strings.Trim(fracPart, "0") == "" {
		v, err := strconv.ParseInt(intPart, 10, 64)
		return v, err == nil
	}
	if !hasExponent {
		return 0, false
	}
	// Exact rational arithmetic: float64 rounding would alter an integer above 2^53.
	rat, parsed := new(big.Rat).SetString(s)
	if !parsed || !rat.IsInt() {
		return 0, false
	}
	integer := rat.Num()
	isExactInteger := integer.IsInt64() && integer.Int64() >= -maxExactFloatInt && integer.Int64() <= maxExactFloatInt
	if !isExactInteger {
		return 0, false
	}
	return integer.Int64(), true
}
