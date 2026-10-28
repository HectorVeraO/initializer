package api

import (
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const maxEmailLength = 254

// CheckRequired fails FIELD_REQUIRED on an empty string after TrimSpace.
func CheckRequired(p *Problems, field, v string) bool {
	if strings.TrimSpace(v) == "" {
		p.Add(field, CodeFieldRequired, "Field is required")
		return false
	}
	return true
}

// CheckLength counts runes after TrimSpace: FIELD_TOO_SHORT or FIELD_TOO_LONG.
func CheckLength(p *Problems, field, v string, min, max int) bool {
	length := utf8.RuneCountInString(strings.TrimSpace(v))
	if length < min {
		p.Add(field, CodeFieldTooShort, fmt.Sprintf("Must be at least %d characters", min))
		return false
	}
	if length > max {
		p.Add(field, CodeFieldTooLong, fmt.Sprintf("Must be at most %d characters", max))
		return false
	}
	return true
}

// CheckIntRange fails FIELD_OUT_OF_RANGE outside min..max inclusive.
func CheckIntRange(p *Problems, field string, v, min, max int64) bool {
	if v < min || v > max {
		p.Add(field, CodeFieldOutOfRange, fmt.Sprintf("Must be between %d and %d", min, max))
		return false
	}
	return true
}

// CheckOneOf fails FIELD_NOT_ALLOWED when v is not one of allowed.
func CheckOneOf(p *Problems, field, v string, allowed ...string) bool {
	if !slices.Contains(allowed, v) {
		p.Add(field, CodeFieldNotAllowed, "Must be one of: "+strings.Join(allowed, ", "))
		return false
	}
	return true
}

// CheckMinItems fails FIELD_MIN_ITEMS when n < min.
func CheckMinItems(p *Problems, field string, n, min int) bool {
	if n < min {
		p.Add(field, CodeFieldMinItems, fmt.Sprintf("Must have at least %d items", min))
		return false
	}
	return true
}

// CheckUnique reports FIELD_DUPLICATE on "<field>[i]" for every repeated entry.
func CheckUnique(p *Problems, field string, values []string) bool {
	seen := make(map[string]struct{}, len(values))
	ok := true
	for i, value := range values {
		if _, repeated := seen[value]; repeated {
			p.Add(fmt.Sprintf("%s[%d]", field, i), CodeFieldDuplicate, "Duplicate entry")
			ok = false
			continue
		}
		seen[value] = struct{}{}
	}
	return ok
}

// CheckUUID returns the canonical lowercase form; FIELD_INVALID_FORMAT otherwise.
func CheckUUID(p *Problems, field, v string) (string, bool) {
	canonical, ok := ParseUUID(v)
	if !ok {
		p.Add(field, CodeFieldInvalidFmt, "Must be a UUID")
		return "", false
	}
	return canonical, true
}

// CheckEmail accepts one net/mail address without display name and returns it
// lower-cased; FIELD_INVALID_FORMAT otherwise.
func CheckEmail(p *Problems, field, v string) (string, bool) {
	trimmed := strings.TrimSpace(v)
	parsed, err := mail.ParseAddress(trimmed)
	isBareAddress := err == nil && parsed.Name == "" && parsed.Address == trimmed
	if !isBareAddress || len(trimmed) > maxEmailLength {
		p.Add(field, CodeFieldInvalidFmt, "Must be an email address")
		return "", false
	}
	return strings.ToLower(trimmed), true
}

// CheckTimestamp parses RFC 3339 and returns UTC; FIELD_INVALID_FORMAT otherwise.
func CheckTimestamp(p *Problems, field, v string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339, v)
	if err != nil {
		p.Add(field, CodeFieldInvalidFmt, "Must be an RFC 3339 timestamp")
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

// ParseUUID returns the canonical lowercase form of a hyphenated UUID without
// recording a problem: a malformed path :id is a 404, never a 422.
func ParseUUID(s string) (string, bool) {
	const length = 36
	if len(s) != length {
		return "", false
	}
	for i := 0; i < length; i++ {
		c := s[i]
		isHyphenSlot := i == 8 || i == 13 || i == 18 || i == 23
		if isHyphenSlot {
			if c != '-' {
				return "", false
			}
			continue
		}
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !isHex {
			return "", false
		}
	}
	return strings.ToLower(s), true
}
