package parameters_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// manyEntries renders a JSON array of n distinct strings or numbers.
func manyEntries(n int, isNumber bool) string {
	parts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		if isNumber {
			parts = append(parts, fmt.Sprintf("%d", i))
		} else {
			parts = append(parts, fmt.Sprintf("%q", fmt.Sprintf("v%d", i)))
		}
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// TestCreateRejectsInvalidBodies runs the POST validation matrix: every
// rejected body answers one 422 whose issues are asserted in full, in order.
func TestCreateRejectsInvalidBodies(t *testing.T) {
	svc := offlineService(t)
	account := globalPrincipal(t)
	longKey := strings.Repeat("a", 129)
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"empty object", `{}`, []string{"key:FIELD_REQUIRED", "valueType:FIELD_REQUIRED", "defaultValue:FIELD_REQUIRED"}},
		{"null for every field", `{"key":null,"valueType":null,"defaultValue":null,"minValue":null,"maxValue":null,"allowedValues":null,"description":null,"isSystemRequired":null}`,
			[]string{"key:FIELD_REQUIRED", "valueType:FIELD_REQUIRED", "defaultValue:FIELD_REQUIRED"}},
		{"key not a string", `{"key":5,"valueType":"string","defaultValue":"x"}`, []string{"key:PARAMETER_KEY_INVALID"}},
		{"key uppercase", `{"key":"Retry","valueType":"string","defaultValue":"x"}`, []string{"key:PARAMETER_KEY_INVALID"}},
		{"key empty segment", `{"key":"a..b","valueType":"string","defaultValue":"x"}`, []string{"key:PARAMETER_KEY_INVALID"}},
		{"key starts with digit", `{"key":"1a","valueType":"string","defaultValue":"x"}`, []string{"key:PARAMETER_KEY_INVALID"}},
		{"key trailing dot", `{"key":"a.","valueType":"string","defaultValue":"x"}`, []string{"key:PARAMETER_KEY_INVALID"}},
		{"key empty", `{"key":"","valueType":"string","defaultValue":"x"}`, []string{"key:PARAMETER_KEY_INVALID"}},
		{"key trailing newline", `{"key":"a\n","valueType":"string","defaultValue":"x"}`, []string{"key:PARAMETER_KEY_INVALID"}},
		{"key of 129 characters", `{"key":"` + longKey + `","valueType":"string","defaultValue":"x"}`, []string{"key:PARAMETER_KEY_INVALID"}},
		{"valueType not a string", `{"key":"a","valueType":3,"defaultValue":"x"}`, []string{"valueType:FIELD_TYPE_INVALID"}},
		{"valueType unknown", `{"key":"a","valueType":"date","defaultValue":"x"}`, []string{"valueType:FIELD_NOT_ALLOWED"}},
		{"unknown valueType skips dependent checks", `{"key":"a","valueType":"date","defaultValue":5,"minValue":1,"maxValue":0,"allowedValues":[],"description":7}`,
			[]string{"valueType:FIELD_NOT_ALLOWED", "maxValue:PARAMETER_CONSTRAINT_INVALID", "allowedValues:PARAMETER_CONSTRAINT_INVALID", "description:FIELD_TYPE_INVALID"}},
		{"string default of the wrong type", `{"key":"a","valueType":"string","defaultValue":5}`, []string{"defaultValue:PARAMETER_TYPE_MISMATCH"}},
		{"number default as text", `{"key":"a","valueType":"number","defaultValue":"5"}`, []string{"defaultValue:PARAMETER_TYPE_MISMATCH"}},
		{"boolean default as text", `{"key":"a","valueType":"boolean","defaultValue":"true"}`, []string{"defaultValue:PARAMETER_TYPE_MISMATCH"}},
		{"json default null", `{"key":"a","valueType":"json","defaultValue":null}`, []string{"defaultValue:FIELD_REQUIRED"}},
		{"json default with NUL escape", `{"key":"a","valueType":"json","defaultValue":{"k":"a\u0000b"}}`, []string{"defaultValue:PARAMETER_TYPE_MISMATCH"}},
		{"number default beyond float range", `{"key":"a","valueType":"number","defaultValue":1e999}`, []string{"defaultValue:PARAMETER_TYPE_MISMATCH"}},
		{"number below min", `{"key":"a","valueType":"number","minValue":1,"maxValue":10,"defaultValue":0.9}`, []string{"defaultValue:PARAMETER_OUT_OF_RANGE"}},
		{"number above max", `{"key":"a","valueType":"number","minValue":1,"maxValue":10,"defaultValue":10.1}`, []string{"defaultValue:PARAMETER_OUT_OF_RANGE"}},
		{"only max set and exceeded", `{"key":"a","valueType":"number","maxValue":10,"defaultValue":11}`, []string{"defaultValue:PARAMETER_OUT_OF_RANGE"}},
		{"min above max", `{"key":"a","valueType":"number","minValue":5,"maxValue":1,"defaultValue":3}`, []string{"maxValue:PARAMETER_CONSTRAINT_INVALID"}},
		{"min on a string", `{"key":"a","valueType":"string","minValue":1,"defaultValue":"x"}`, []string{"minValue:PARAMETER_CONSTRAINT_INVALID"}},
		{"max on a boolean", `{"key":"a","valueType":"boolean","maxValue":1,"defaultValue":true}`, []string{"maxValue:PARAMETER_CONSTRAINT_INVALID"}},
		{"both bounds on json", `{"key":"a","valueType":"json","minValue":1,"maxValue":2,"defaultValue":{}}`, []string{"minValue:PARAMETER_CONSTRAINT_INVALID", "maxValue:PARAMETER_CONSTRAINT_INVALID"}},
		{"min not a number", `{"key":"a","valueType":"number","minValue":"1","defaultValue":3}`, []string{"minValue:FIELD_TYPE_INVALID"}},
		{"max not a number", `{"key":"a","valueType":"number","maxValue":true,"defaultValue":3}`, []string{"maxValue:FIELD_TYPE_INVALID"}},
		{"allowed not an array", `{"key":"a","valueType":"string","allowedValues":"x","defaultValue":"x"}`, []string{"allowedValues:FIELD_TYPE_INVALID"}},
		{"allowed empty", `{"key":"a","valueType":"string","allowedValues":[],"defaultValue":"x"}`, []string{"allowedValues:PARAMETER_CONSTRAINT_INVALID"}},
		{"allowed 101 entries", `{"key":"a","valueType":"string","allowedValues":` + manyEntries(101, false) + `,"defaultValue":"v0"}`, []string{"allowedValues:PARAMETER_CONSTRAINT_INVALID"}},
		{"allowed on a boolean", `{"key":"a","valueType":"boolean","allowedValues":[true],"defaultValue":true}`, []string{"allowedValues:PARAMETER_CONSTRAINT_INVALID"}},
		{"allowed on json", `{"key":"a","valueType":"json","allowedValues":[1],"defaultValue":1}`, []string{"allowedValues:PARAMETER_CONSTRAINT_INVALID"}},
		{"allowed entry of the wrong type", `{"key":"a","valueType":"string","allowedValues":["a",1],"defaultValue":"a"}`, []string{"allowedValues[1]:PARAMETER_TYPE_MISMATCH"}},
		{"allowed entry null", `{"key":"a","valueType":"string","allowedValues":[null],"defaultValue":"a"}`, []string{"allowedValues[0]:PARAMETER_TYPE_MISMATCH"}},
		{"allowed duplicate string", `{"key":"a","valueType":"string","allowedValues":["a","b","a"],"defaultValue":"a"}`, []string{"allowedValues[2]:PARAMETER_CONSTRAINT_INVALID"}},
		{"allowed duplicate number as 2 and 2.0", `{"key":"a","valueType":"number","allowedValues":[1,2,2.0],"defaultValue":1}`, []string{"allowedValues[2]:PARAMETER_CONSTRAINT_INVALID"}},
		{"allowed number outside bounds", `{"key":"a","valueType":"number","maxValue":10,"allowedValues":[1,50],"defaultValue":1}`, []string{"allowedValues[1]:PARAMETER_OUT_OF_RANGE"}},
		{"default not in allowed strings", `{"key":"a","valueType":"string","allowedValues":["a","b"],"defaultValue":"c"}`, []string{"defaultValue:PARAMETER_VALUE_NOT_ALLOWED"}},
		{"default not in allowed numbers", `{"key":"a","valueType":"number","minValue":0,"maxValue":10,"allowedValues":[1,2],"defaultValue":3}`, []string{"defaultValue:PARAMETER_VALUE_NOT_ALLOWED"}},
		{"description 501 characters", `{"key":"a","valueType":"string","defaultValue":"x","description":"` + strings.Repeat("x", 501) + `"}`, []string{"description:FIELD_TOO_LONG"}},
		{"description 501 multibyte characters", `{"key":"a","valueType":"string","defaultValue":"x","description":"` + strings.Repeat("é", 501) + `"}`, []string{"description:FIELD_TOO_LONG"}},
		{"description not a string", `{"key":"a","valueType":"string","defaultValue":"x","description":7}`, []string{"description:FIELD_TYPE_INVALID"}},
		{"description with NUL", `{"key":"a","valueType":"string","defaultValue":"x","description":"a\u0000b"}`, []string{"description:FIELD_NOT_ALLOWED"}},
		{"isSystemRequired not a boolean", `{"key":"a","valueType":"string","defaultValue":"x","isSystemRequired":"yes"}`, []string{"isSystemRequired:FIELD_TYPE_INVALID"}},
		{"every field broken, reported in table order",
			`{"key":"X","valueType":"number","minValue":"a","allowedValues":"x","defaultValue":"s","description":1,"isSystemRequired":2}`,
			[]string{"key:PARAMETER_KEY_INVALID", "minValue:FIELD_TYPE_INVALID", "allowedValues:FIELD_TYPE_INVALID", "defaultValue:PARAMETER_TYPE_MISMATCH", "description:FIELD_TYPE_INVALID", "isSystemRequired:FIELD_TYPE_INVALID"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Create(context.Background(), account, body(t, tc.body))
			requireValidation(t, err, tc.want)
		})
	}
}
