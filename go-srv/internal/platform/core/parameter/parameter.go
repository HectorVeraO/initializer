// Package parameter is the core seam for reading platform parameter values.
package parameter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
)

// Type is the declared type of a parameter value. The set is closed and holds
// the types the parameters module stores.
type Type string

const (
	TypeString  Type = "string"
	TypeNumber  Type = "number"
	TypeBoolean Type = "boolean"
	TypeJSON    Type = "json"
)

var (
	// ErrInvalidValue is what NewValue returns for unreadable JSON.
	ErrInvalidValue = errors.New("parameter: invalid value")
	// ErrTypeMismatch is what a decoder returns when the value holds another
	// type.
	ErrTypeMismatch = errors.New("parameter: value type mismatch")
	// ErrPrecisionLoss is what Number returns when the stored number is not
	// exactly representable as float64.
	ErrPrecisionLoss = errors.New("parameter: number loses precision")
)

// Value is one resolved parameter value: its declared type and the JSON text
// the parameters module stores. The fields are unexported, so every Value comes
// from NewValue and its decoders answer.
type Value struct {
	typ Type
	raw json.RawMessage
}

// NewValue returns the value of type t holding raw. It fails when raw is not
// JSON of that type, so a caller never holds a Value its decoders reject.
func NewValue(t Type, raw json.RawMessage) (Value, error) {
	trimmed := bytes.TrimSpace(raw)
	if !isOfType(t, trimmed) {
		return Value{}, fmt.Errorf("%w: not a %s value", ErrInvalidValue, t)
	}
	return Value{typ: t, raw: trimmed}, nil
}

// isOfType reports whether raw is JSON of type t. The parameters module applies
// it on write, so a mismatch means the row was written outside the module.
func isOfType(t Type, raw []byte) bool {
	isReadable := len(raw) > 0 && json.Valid(raw)
	if !isReadable {
		return false
	}
	first := raw[0]
	switch t {
	case TypeString:
		return first == '"'
	case TypeNumber:
		return first == '-' || (first >= '0' && first <= '9')
	case TypeBoolean:
		return first == 't' || first == 'f'
	case TypeJSON:
		return first != 'n'
	}
	return false
}

// Type is the declared type of the value.
func (v Value) Type() Type { return v.typ }

// Raw returns a copy of the JSON text of the value.
func (v Value) Raw() json.RawMessage {
	if v.raw == nil {
		return nil
	}
	return append(json.RawMessage(nil), v.raw...)
}

// String returns the text of a string parameter.
func (v Value) String() (string, error) {
	err := v.require(TypeString)
	if err != nil {
		return "", err
	}
	var text string
	err = json.Unmarshal(v.raw, &text)
	if err != nil {
		return "", fmt.Errorf("parameter: decode string value: %w", err)
	}
	return text, nil
}

// Number returns a number parameter as float64. It fails with
// ErrPrecisionLoss when the stored number is not exactly representable as
// float64: an integer above 2^53, or a decimal whose shortest form needs more
// than 17 significant digits. `0.1` and `1e3` decode: the float64 nearest each
// literal carries it as its shortest form, which is the bound.
func (v Value) Number() (float64, error) {
	err := v.require(TypeNumber)
	if err != nil {
		return 0, err
	}
	var number float64
	err = json.Unmarshal(v.raw, &number)
	if err != nil {
		return 0, fmt.Errorf("parameter: decode number value: %w", err)
	}
	if !isExactFloat64(v.raw, number) {
		return 0, fmt.Errorf("%w: %s needs more than a float64", ErrPrecisionLoss, v.raw)
	}
	return number, nil
}

// isExactFloat64 reports whether the decimal literal raw is the number number
// carries: both are compared as exact rationals, the literal against the
// shortest round-trip form of the float64. A literal the float64 only
// approximates (9007199254740993) is refused, while 0.1 is accepted because it
// is the shortest form of its float64.
func isExactFloat64(raw []byte, number float64) bool {
	literal, isParsed := new(big.Rat).SetString(string(raw))
	if !isParsed {
		return false
	}
	decoded, isParsed := new(big.Rat).SetString(strconv.FormatFloat(number, 'f', -1, 64))
	if !isParsed {
		return false
	}
	return literal.Cmp(decoded) == 0
}

// Bool returns a boolean parameter.
func (v Value) Bool() (bool, error) {
	err := v.require(TypeBoolean)
	if err != nil {
		return false, err
	}
	var value bool
	err = json.Unmarshal(v.raw, &value)
	if err != nil {
		return false, fmt.Errorf("parameter: decode boolean value: %w", err)
	}
	return value, nil
}

// Decode unmarshals a json parameter into target, which must be a non-nil
// pointer.
func (v Value) Decode(target any) error {
	err := v.require(TypeJSON)
	if err != nil {
		return err
	}
	if target == nil {
		return errors.New("parameter: Decode needs a non-nil target")
	}
	err = json.Unmarshal(v.raw, target)
	if err != nil {
		return fmt.Errorf("parameter: decode json value: %w", err)
	}
	return nil
}

// require fails unless the value holds type want.
func (v Value) require(want Type) error {
	if v.typ != want {
		return fmt.Errorf("%w: want %s, got %q", ErrTypeMismatch, want, v.typ)
	}
	return nil
}

// Reader resolves the effective value of a parameter: the active tenant's
// override, then the global value the definition holds. The tenant comes from
// the request context, never from an argument.
//
// Get answers found=false with no error for a key with no definition. An error
// means the store failed or the stored value does not decode into a Value of
// its declared type; found is false and the Value is zero, so a caller that
// checks the error first never reads a stale or untyped value.
type Reader interface {
	Get(ctx context.Context, key string) (Value, bool, error)
}

// NoReader is the Reader of a build without a module that provides one: every
// key is not found and no call fails.
type NoReader struct{}

// Get answers not found.
func (NoReader) Get(context.Context, string) (Value, bool, error) {
	return Value{}, false, nil
}
