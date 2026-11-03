package parameter_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/parameter"
)

func TestNoReaderAnswersNotFoundForEveryKey(t *testing.T) {
	reader := parameter.NoReader{}
	for _, key := range []string{"export.max_rows", "", "Bad Key"} {
		value, found, err := reader.Get(context.Background(), key)
		if err != nil {
			t.Fatalf("%q: err = %v, want none", key, err)
		}
		if found || value.Type() != "" || value.Raw() != nil {
			t.Fatalf("%q: got %+v found, want the zero value", key, value)
		}
	}
}

func TestNewValueAcceptsEachStoredType(t *testing.T) {
	cases := []struct {
		name string
		kind parameter.Type
		raw  string
	}{
		{"string", parameter.TypeString, `"hello"`},
		{"empty string", parameter.TypeString, `""`},
		{"integer", parameter.TypeNumber, `50`},
		{"decimal", parameter.TypeNumber, `-0.25`},
		{"exponent", parameter.TypeNumber, `1e3`},
		{"true", parameter.TypeBoolean, `true`},
		{"false", parameter.TypeBoolean, `false`},
		{"object", parameter.TypeJSON, `{"a":[1,2]}`},
		{"array", parameter.TypeJSON, `[1,"two"]`},
		{"nested string", parameter.TypeJSON, `"text"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, err := parameter.NewValue(tc.kind, json.RawMessage(tc.raw))
			if err != nil {
				t.Fatalf("NewValue(%s, %s): %v", tc.kind, tc.raw, err)
			}
			if value.Type() != tc.kind {
				t.Fatalf("type = %q, want %q", value.Type(), tc.kind)
			}
			if string(value.Raw()) != tc.raw {
				t.Fatalf("raw = %s, want %s", value.Raw(), tc.raw)
			}
		})
	}
}

func TestNewValueRejectsAValueOfAnotherType(t *testing.T) {
	cases := []struct {
		name string
		kind parameter.Type
		raw  string
	}{
		{"string for a number", parameter.TypeNumber, `"50"`},
		{"number for a string", parameter.TypeString, `50`},
		{"string for a boolean", parameter.TypeBoolean, `"true"`},
		{"null", parameter.TypeJSON, `null`},
		{"null anywhere", parameter.TypeString, `null`},
		{"not JSON", parameter.TypeString, `hello`},
		{"empty", parameter.TypeJSON, ``},
		{"truncated", parameter.TypeJSON, `{"a":`},
		{"unknown type", parameter.Type("date"), `"2026-01-01"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, err := parameter.NewValue(tc.kind, json.RawMessage(tc.raw))
			if !errors.Is(err, parameter.ErrInvalidValue) {
				t.Fatalf("err = %v, want ErrInvalidValue", err)
			}
			if value.Type() != "" {
				t.Fatalf("value = %+v, want the zero value", value)
			}
		})
	}
}

func TestDecodersAnswerEachType(t *testing.T) {
	var decoded struct {
		A int `json:"a"`
	}
	cases := []struct {
		name  string
		value string
		kind  parameter.Type
		read  func(parameter.Value) (any, error)
		want  any
	}{
		{
			name: "string", value: `"hello"`, kind: parameter.TypeString, want: "hello",
			read: func(v parameter.Value) (any, error) {
				text, err := v.String()
				return text, err
			},
		},
		{
			name: "number", value: `-0.25`, kind: parameter.TypeNumber, want: -0.25,
			read: func(v parameter.Value) (any, error) {
				number, err := v.Number()
				return number, err
			},
		},
		{
			name: "boolean", value: `true`, kind: parameter.TypeBoolean, want: true,
			read: func(v parameter.Value) (any, error) {
				flag, err := v.Bool()
				return flag, err
			},
		},
		{
			name: "json", value: `{"a":3}`, kind: parameter.TypeJSON, want: 3,
			read: func(v parameter.Value) (any, error) {
				err := v.Decode(&decoded)
				return decoded.A, err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, err := parameter.NewValue(tc.kind, json.RawMessage(tc.value))
			if err != nil {
				t.Fatalf("NewValue: %v", err)
			}
			got, err := tc.read(value)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestNumberAcceptsEveryExactlyRepresentableValue(t *testing.T) {
	cases := []struct {
		raw  string
		want float64
	}{
		{"50", 50},
		{"1.0", 1},
		{"1.50", 1.5},
		{"1E2", 100},
		{"1e3", 1000},
		{"0.1", 0.1},
		{"-0.25", -0.25},
		{"9007199254740992", 9007199254740992},
		{"-9007199254740992", -9007199254740992},
		{"1.7976931348623157e308", 1.7976931348623157e308},
		{"5e-324", 5e-324},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			value, err := parameter.NewValue(parameter.TypeNumber, json.RawMessage(tc.raw))
			if err != nil {
				t.Fatalf("NewValue: %v", err)
			}
			number, err := value.Number()
			if err != nil {
				t.Fatalf("Number(%s): %v", tc.raw, err)
			}
			if number != tc.want {
				t.Fatalf("Number(%s) = %v, want %v", tc.raw, number, tc.want)
			}
		})
	}
}

func TestNumberRefusesAValueFloat64CannotHold(t *testing.T) {
	cases := []string{
		"9007199254740993",
		"-9007199254740993",
		"18446744073709551617",
		"1.0000000000000000001",
		"123456789012345678",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			value, err := parameter.NewValue(parameter.TypeNumber, json.RawMessage(raw))
			if err != nil {
				t.Fatalf("NewValue: %v", err)
			}
			number, err := value.Number()
			if !errors.Is(err, parameter.ErrPrecisionLoss) {
				t.Fatalf("err = %v, want ErrPrecisionLoss", err)
			}
			if number != 0 {
				t.Fatalf("Number(%s) = %v, want the zero value beside the error", raw, number)
			}
		})
	}
}

func TestADecoderOfAnotherTypeFails(t *testing.T) {
	text, err := parameter.NewValue(parameter.TypeString, json.RawMessage(`"50"`))
	if err != nil {
		t.Fatalf("NewValue: %v", err)
	}
	number, err := parameter.NewValue(parameter.TypeNumber, json.RawMessage(`50`))
	if err != nil {
		t.Fatalf("NewValue: %v", err)
	}
	cases := []struct {
		name string
		err  error
	}{
		{"Number on a string", errOnly(text.Number())},
		{"String on a number", errOnly(number.String())},
		{"Bool on a string", errOnly(text.Bool())},
		{"Decode on a number", text.Decode(&struct{}{})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.err, parameter.ErrTypeMismatch) {
				t.Fatalf("err = %v, want ErrTypeMismatch", tc.err)
			}
		})
	}
	if err := number.Decode(nil); err == nil {
		t.Fatal("Decode accepted a nil target")
	}
}

func TestTheZeroValueDecodersFailWithoutPanicking(t *testing.T) {
	var zero parameter.Value
	if _, err := zero.String(); !errors.Is(err, parameter.ErrTypeMismatch) {
		t.Fatalf("String err = %v", err)
	}
	if _, err := zero.Number(); !errors.Is(err, parameter.ErrTypeMismatch) {
		t.Fatalf("Number err = %v", err)
	}
	if _, err := zero.Bool(); !errors.Is(err, parameter.ErrTypeMismatch) {
		t.Fatalf("Bool err = %v", err)
	}
	if err := zero.Decode(&struct{}{}); !errors.Is(err, parameter.ErrTypeMismatch) {
		t.Fatalf("Decode err = %v", err)
	}
	if zero.Raw() != nil {
		t.Fatalf("Raw = %s, want nil", zero.Raw())
	}
}

// errOnly drops the first result of a two-result call.
func errOnly[T any](_ T, err error) error {
	return err
}
