package parameters

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
)

const (
	maxKeyLength         = 128
	maxDescriptionLength = 500
	maxAllowedValues     = 100
)

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

// valueType is the JSON type a parameter holds.
type valueType string

const (
	typeString  valueType = "string"
	typeNumber  valueType = "number"
	typeBoolean valueType = "boolean"
	typeJSON    valueType = "json"
)

var valueTypeNames = []string{string(typeString), string(typeNumber), string(typeBoolean), string(typeJSON)}

// definition is one parameter definition as stored.
type definition struct {
	Key              string
	ValueType        valueType
	DefaultValue     json.RawMessage
	Min              *float64
	Max              *float64
	Allowed          []json.RawMessage
	Description      string
	IsSystemRequired bool
}

// constraints returns what a value of this parameter must satisfy.
func (d definition) constraints() constraints {
	return constraints{valueType: d.ValueType, min: d.Min, max: d.Max, allowed: d.Allowed}
}

// constraints are the rules one value is checked against.
type constraints struct {
	valueType valueType
	min       *float64
	max       *float64
	allowed   []json.RawMessage
}

// valueFailure is the first rule a value breaks.
type valueFailure struct {
	code    api.Code
	message string
}

// validKey applies the key rule.
func validKey(key string) bool {
	isShortEnough := len(key) <= maxKeyLength
	return isShortEnough && keyPattern.MatchString(key)
}

// isNullOrAbsent reports whether raw holds no value or the JSON null.
func isNullOrAbsent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || string(trimmed) == "null"
}

// checkValue applies the value checks in order and returns the first failure,
// or nil when raw is acceptable: absent or null, wrong type, outside the
// bounds, not in the allowed set.
func checkValue(c constraints, raw json.RawMessage) *valueFailure {
	if isNullOrAbsent(raw) {
		return &valueFailure{api.CodeFieldRequired, "Field is required"}
	}
	if !matchesType(c.valueType, raw) {
		return &valueFailure{CodeTypeMismatch, "Must be a " + string(c.valueType) + " value"}
	}
	if c.valueType == typeNumber {
		number, _ := strconv.ParseFloat(string(bytes.TrimSpace(raw)), 64)
		isBelow := c.min != nil && number < *c.min
		isAbove := c.max != nil && number > *c.max
		if isBelow || isAbove {
			return &valueFailure{CodeOutOfRange, "Outside the allowed range"}
		}
	}
	if len(c.allowed) > 0 && !isAllowed(c, raw) {
		return &valueFailure{CodeValueNotAllowed, "Not one of the allowed values"}
	}
	return nil
}

// matchesType reports whether raw is valid, storable JSON of the given type.
func matchesType(t valueType, raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	isStorableJSON := len(trimmed) > 0 && json.Valid(trimmed) && isStorable(trimmed)
	if !isStorableJSON {
		return false
	}
	first := trimmed[0]
	switch t {
	case typeString:
		return first == '"'
	case typeNumber:
		return first == '-' || (first >= '0' && first <= '9')
	case typeBoolean:
		return first == 't' || first == 'f'
	case typeJSON:
		return first != 'n'
	}
	return false
}

// isStorable reports whether PostgreSQL jsonb can hold the valid JSON in raw:
// UTF-8 text, no NUL character in a string, every number finite as a float64.
func isStorable(raw []byte) bool {
	if !utf8.Valid(raw) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return true
		}
		if err != nil {
			return false
		}
		switch value := token.(type) {
		case string:
			if strings.ContainsRune(value, 0) {
				return false
			}
		case json.Number:
			_, parseErr := strconv.ParseFloat(string(value), 64)
			if parseErr != nil {
				return false
			}
		}
	}
}

// identityOf returns the Go value two scalars of type t are compared by:
// the exact rational text for numbers (so 1 equals 1.0 and integers beyond
// 2^53 stay distinct), the decoded text for strings.
func identityOf(t valueType, raw json.RawMessage) any {
	if t == typeNumber {
		exact, isParsed := new(big.Rat).SetString(string(bytes.TrimSpace(raw)))
		if !isParsed {
			return string(raw)
		}
		return exact.RatString()
	}
	var text string
	_ = json.Unmarshal(raw, &text)
	return text
}

func isAllowed(c constraints, raw json.RawMessage) bool {
	want := identityOf(c.valueType, raw)
	for _, entry := range c.allowed {
		if identityOf(c.valueType, entry) == want {
			return true
		}
	}
	return false
}

// parseCreate validates a POST body into a definition. Every problem is
// reported in one 422.
func parseCreate(b *api.Body) (definition, error) {
	return parseDefinition(b, nil)
}

// parseReplace validates a PUT body against the stored definition.
func parseReplace(b *api.Body, stored definition) (definition, error) {
	return parseDefinition(b, &stored)
}

// parseDefinition checks every field in table order; stored is nil for a create.
func parseDefinition(b *api.Body, stored *definition) (definition, error) {
	var problems api.Problems
	var d definition
	if stored != nil {
		d.Key = stored.Key
	} else {
		d.Key = parseKey(b, &problems)
	}
	vt, isTypeKnown := parseValueType(b, &problems, stored)
	d.ValueType = vt
	minValue, maxValue, areBoundsUsable := parseBounds(b, &problems, vt, isTypeKnown)
	d.Min, d.Max = minValue, maxValue
	if !areBoundsUsable {
		minValue, maxValue = nil, nil
	}
	allowed, isAllowedUsable := parseAllowed(b, &problems, vt, isTypeKnown, minValue, maxValue)
	d.Allowed = allowed
	if !isAllowedUsable {
		allowed = nil
	}
	rules := constraints{valueType: vt, min: minValue, max: maxValue, allowed: allowed}
	d.DefaultValue = parseDefault(b, &problems, rules, isTypeKnown)
	d.Description = parseDescription(b, &problems)
	d.IsSystemRequired = parseSystemRequired(b, &problems, stored)

	err := problems.Err()
	if err != nil {
		return definition{}, err
	}
	return d, nil
}

func parseKey(b *api.Body, p *api.Problems) string {
	raw, present := b.Raw("key")
	if !present || b.IsNull("key") {
		p.Add("key", api.CodeFieldRequired, "Field is required")
		return ""
	}
	var key string
	err := json.Unmarshal(raw, &key)
	if err != nil {
		p.Add("key", CodeKeyInvalid, "Must be a string")
		return ""
	}
	if !validKey(key) {
		p.Add("key", CodeKeyInvalid, "Must be dot-separated lowercase segments of at most 128 characters")
		return ""
	}
	return key
}

// parseValueType returns the type and whether later checks may depend on it.
// A replace always works with the stored type.
func parseValueType(b *api.Body, p *api.Problems, stored *definition) (valueType, bool) {
	if stored == nil {
		name, ok := b.String(p, "valueType", api.Required)
		if !ok {
			return "", false
		}
		isKnown := api.CheckOneOf(p, "valueType", name, valueTypeNames...)
		if !isKnown {
			return "", false
		}
		return valueType(name), true
	}
	name, ok := b.String(p, "valueType", api.Optional)
	if ok {
		isKnown := api.CheckOneOf(p, "valueType", name, valueTypeNames...)
		if isKnown && valueType(name) != stored.ValueType {
			p.Add("valueType", CodeFieldImmutable, "Cannot be changed")
		}
	}
	return stored.ValueType, true
}

// parseBounds reads minValue and maxValue. The bounds are usable for later
// checks when the type is a number (or unknown, where nothing uses them) and
// they are ordered.
func parseBounds(b *api.Body, p *api.Problems, vt valueType, isTypeKnown bool) (*float64, *float64, bool) {
	isApplicable := !isTypeKnown || vt == typeNumber
	var bounds [2]*float64
	for i, field := range []string{"minValue", "maxValue"} {
		value, ok := b.Number(p, field, api.Optional)
		if !ok {
			continue
		}
		if !isApplicable {
			p.Add(field, CodeConstraintInvalid, "Only allowed for number parameters")
			continue
		}
		bounds[i] = &value
	}
	areOrdered := bounds[0] == nil || bounds[1] == nil || *bounds[0] <= *bounds[1]
	if !areOrdered {
		p.Add("maxValue", CodeConstraintInvalid, "Must be greater than or equal to minValue")
	}
	areUsable := isApplicable && areOrdered
	return bounds[0], bounds[1], areUsable
}

// parseAllowed reads allowedValues. The returned flag says the list is valid
// and may be used to check the default value.
func parseAllowed(b *api.Body, p *api.Problems, vt valueType, isTypeKnown bool, minValue, maxValue *float64) ([]json.RawMessage, bool) {
	raw, present := b.Raw("allowedValues")
	if !present || b.IsNull("allowedValues") {
		return nil, true
	}
	var entries []json.RawMessage
	err := json.Unmarshal(raw, &entries)
	if err != nil {
		p.Add("allowedValues", api.CodeFieldTypeInvalid, "Must be an array")
		return nil, false
	}
	if isTypeKnown && (vt == typeBoolean || vt == typeJSON) {
		p.Add("allowedValues", CodeConstraintInvalid, "Not allowed for "+string(vt)+" parameters")
		return nil, false
	}
	if len(entries) == 0 || len(entries) > maxAllowedValues {
		p.Add("allowedValues", CodeConstraintInvalid, fmt.Sprintf("Must hold between 1 and %d entries", maxAllowedValues))
		return nil, false
	}
	if !isTypeKnown {
		return nil, false
	}
	entryRules := constraints{valueType: vt, min: minValue, max: maxValue}
	seen := make(map[any]struct{}, len(entries))
	areEntriesValid := true
	for i, entry := range entries {
		field := "allowedValues[" + strconv.Itoa(i) + "]"
		failure := checkValue(entryRules, entry)
		if failure != nil {
			code := failure.code
			if code == api.CodeFieldRequired {
				code = CodeTypeMismatch
			}
			p.Add(field, code, failure.message)
			areEntriesValid = false
			continue
		}
		identity := identityOf(vt, entry)
		_, isRepeated := seen[identity]
		if isRepeated {
			p.Add(field, CodeConstraintInvalid, "Duplicate entry")
			areEntriesValid = false
			continue
		}
		seen[identity] = struct{}{}
	}
	return entries, areEntriesValid
}

func parseDefault(b *api.Body, p *api.Problems, rules constraints, isTypeKnown bool) json.RawMessage {
	raw, _ := b.Raw("defaultValue")
	if isNullOrAbsent(raw) {
		p.Add("defaultValue", api.CodeFieldRequired, "Field is required")
		return nil
	}
	if !isTypeKnown {
		return nil
	}
	failure := checkValue(rules, raw)
	if failure != nil {
		p.Add("defaultValue", failure.code, failure.message)
		return nil
	}
	return raw
}

func parseDescription(b *api.Body, p *api.Problems) string {
	text, ok := b.String(p, "description", api.Optional)
	if !ok {
		return ""
	}
	if utf8.RuneCountInString(text) > maxDescriptionLength {
		p.Add("description", api.CodeFieldTooLong, fmt.Sprintf("Must be at most %d characters", maxDescriptionLength))
		return ""
	}
	if strings.ContainsRune(text, 0) {
		p.Add("description", api.CodeFieldNotAllowed, "Must not contain NUL characters")
		return ""
	}
	return text
}

func parseSystemRequired(b *api.Body, p *api.Problems, stored *definition) bool {
	value, ok := b.Bool(p, "isSystemRequired", api.Optional)
	if stored == nil {
		return ok && value
	}
	if ok && value != stored.IsSystemRequired {
		p.Add("isSystemRequired", CodeFieldImmutable, "Cannot be changed")
	}
	return stored.IsSystemRequired
}
