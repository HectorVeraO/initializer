package parameters_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/parameters"
)

func FuzzValueCheck(f *testing.F) {
	f.Add("number", `5`, 1.0, 10.0, true)
	f.Add("number", `1e999`, 0.0, 0.0, false)
	f.Add("string", `"a\u0000b"`, 0.0, 0.0, false)
	f.Add("string", `"abc"`, 0.0, 0.0, false)
	f.Add("boolean", `true`, 0.0, 0.0, false)
	f.Add("json", `{"a":[1,2,null]}`, 0.0, 0.0, false)
	f.Add("json", `null`, 0.0, 0.0, false)
	f.Add("number", `9007199254740993`, 0.0, 1e300, true)
	f.Add("date", `1`, 0.0, 0.0, false)
	f.Fuzz(func(t *testing.T, typeName, raw string, low, high float64, hasBounds bool) {
		var minValue, maxValue *float64
		if hasBounds {
			minValue, maxValue = &low, &high
		}
		code, ok := parameters.CheckValue(typeName, minValue, maxValue, nil, json.RawMessage(raw))
		if !ok {
			if code == "" {
				t.Fatalf("rejected without a code: %q", raw)
			}
			return
		}
		trimmed := bytes.TrimSpace([]byte(raw))
		if !json.Valid(trimmed) {
			t.Fatalf("accepted invalid JSON %q", raw)
		}
		switch typeName {
		case "string":
			var text string
			if json.Unmarshal(trimmed, &text) != nil || strings.ContainsRune(text, 0) {
				t.Fatalf("accepted %q as a string", raw)
			}
		case "boolean":
			var flag bool
			if json.Unmarshal(trimmed, &flag) != nil {
				t.Fatalf("accepted %q as a boolean", raw)
			}
		case "number":
			number, err := strconv.ParseFloat(string(trimmed), 64)
			if err != nil {
				t.Fatalf("accepted %q as a number", raw)
			}
			if hasBounds && (number < low || number > high) {
				t.Fatalf("accepted %v outside [%v, %v]", number, low, high)
			}
		case "json":
			if string(trimmed) == "null" {
				t.Fatalf("accepted null as json")
			}
		default:
			t.Fatalf("accepted %q for the unknown type %q", raw, typeName)
		}
	})
}

func FuzzCreateBody(f *testing.F) {
	f.Add(`{"key":"retry.max_attempts","valueType":"number","minValue":1,"maxValue":10,"defaultValue":3}`)
	f.Add(`{"key":"a","valueType":"string","allowedValues":["a","a"],"defaultValue":"a"}`)
	f.Add(`{"key":"a","valueType":"json","defaultValue":{"x":"\u0000"}}`)
	f.Add(`{"key":1,"valueType":[],"defaultValue":{},"minValue":"x","allowedValues":{}}`)
	f.Add(`[]`)
	f.Add(``)
	f.Fuzz(func(t *testing.T, raw string) {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(raw))
		request.Header.Set("Content-Type", "application/json")
		decoded, err := api.DecodeBody(request)
		if err != nil {
			return
		}
		def, err := parameters.ParseCreate(decoded)
		if err != nil {
			var apiErr *api.Error
			if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnprocessableEntity ||
				apiErr.Code != api.CodeValidationFailed || len(apiErr.Issues) == 0 {
				t.Fatalf("want a 422 with issues, got %v", err)
			}
			return
		}
		if def.Key == "" || len(def.Key) > 128 || len(def.DefaultValue) == 0 {
			t.Fatalf("accepted an incomplete definition %+v", def)
		}
		_, ok := parameters.CheckValue(string(def.ValueType), def.Min, def.Max, def.Allowed, def.DefaultValue)
		if !ok {
			t.Fatalf("accepted a default that fails its own constraints: %+v", def)
		}
	})
}
