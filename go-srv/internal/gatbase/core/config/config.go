// Package config reads typed settings from an injectable Source and records
// every problem instead of failing on the first. Problem text names the key
// and the rule, never the value.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const redacted = "[redacted]"

// Source looks up one key.
type Source func(key string) (string, bool)

// EnvSource reads the process environment.
func EnvSource() Source {
	return os.LookupEnv
}

// MapSource reads a fixed map (tests).
func MapSource(m map[string]string) Source {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}

// Reader reads typed values and records every problem. A getter that records
// a problem returns its fallback (or zero).
type Reader struct {
	src      Source
	problems []error
}

// NewReader builds a Reader over src; a nil src is a programmer error and panics.
func NewReader(src Source) *Reader {
	if src == nil {
		panic("config: NewReader requires a Source")
	}
	return &Reader{src: src}
}

// lookup returns the value of key; an unset or blank key is absent.
func (r *Reader) lookup(key string) (string, bool) {
	v, ok := r.src(key)
	trimmed := strings.TrimSpace(v)
	if !ok || trimmed == "" {
		return "", false
	}
	return trimmed, true
}

// Invalid records a module-specific problem (key and rule text, never the
// value) for rules the typed getters cannot express.
func (r *Reader) Invalid(key, rule string) {
	r.problems = append(r.problems, fmt.Errorf("config %s: %s", key, rule))
}

// Err returns every recorded problem joined with errors.Join, or nil.
func (r *Reader) Err() error {
	return errors.Join(r.problems...)
}

// String returns the key's value, or fallback when unset or blank.
func (r *Reader) String(key, fallback string) string {
	v, ok := r.lookup(key)
	if !ok {
		return fallback
	}
	return v
}

// RequiredString returns the key's value; unset or blank records a problem.
func (r *Reader) RequiredString(key string) string {
	v, ok := r.lookup(key)
	if !ok {
		r.Invalid(key, "is required")
		return ""
	}
	return v
}

// Int reads an integer within min..max, or fallback when unset.
func (r *Reader) Int(key string, fallback, min, max int) int {
	raw, ok := r.lookup(key)
	if !ok {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		r.Invalid(key, "must be an integer")
		return fallback
	}
	if v < min || v > max {
		r.Invalid(key, fmt.Sprintf("must be between %d and %d", min, max))
		return fallback
	}
	return v
}

// Duration reads a time.ParseDuration value within min..max, or fallback when unset.
func (r *Reader) Duration(key string, fallback, min, max time.Duration) time.Duration {
	raw, ok := r.lookup(key)
	if !ok {
		return fallback
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		r.Invalid(key, "must be a duration such as 10s")
		return fallback
	}
	if v < min || v > max {
		r.Invalid(key, fmt.Sprintf("must be between %s and %s", min, max))
		return fallback
	}
	return v
}

// Bool reads a strconv.ParseBool value, or fallback when unset.
func (r *Reader) Bool(key string, fallback bool) bool {
	raw, ok := r.lookup(key)
	if !ok {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		r.Invalid(key, "must be a boolean")
		return fallback
	}
	return v
}

// URL reads an absolute http or https URL without credentials, query or
// fragment. When unset the fallback is parsed the same way.
func (r *Reader) URL(key, fallback string) *url.URL {
	raw, ok := r.lookup(key)
	if !ok {
		raw = fallback
	}
	parsed, err := url.Parse(raw)
	isHTTP := err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https")
	isClean := isHTTP && parsed.Host != "" && parsed.User == nil &&
		!strings.ContainsAny(raw, "?#")
	if !isClean {
		r.Invalid(key, "must be an absolute http or https URL without credentials, query or fragment")
		return nil
	}
	return parsed
}

// List splits on commas, trims, drops empty entries; nil when unset.
func (r *Reader) List(key string) []string {
	raw, ok := r.lookup(key)
	if !ok {
		return nil
	}
	var items []string
	for _, part := range strings.Split(raw, ",") {
		item := strings.TrimSpace(part)
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}

// Secret reads a required secret; its value never reaches a log or an error.
func (r *Reader) Secret(key string) Secret {
	v, ok := r.lookup(key)
	if !ok {
		r.Invalid(key, "is required")
		return Secret{}
	}
	return Secret{value: v}
}

// OptionalSecret reads a secret that may be unset: absent gives a zero Secret
// and false, with no problem recorded.
func (r *Reader) OptionalSecret(key string) (Secret, bool) {
	v, ok := r.lookup(key)
	if !ok {
		return Secret{}, false
	}
	return Secret{value: v}, true
}

// Secret hides a value from fmt, slog and JSON: every text form is "[redacted]".
type Secret struct {
	value string
}

// Reveal returns the secret value.
func (s Secret) Reveal() string {
	return s.value
}

// String returns "[redacted]".
func (s Secret) String() string {
	return redacted
}

// GoString returns "[redacted]" so %#v does not print the value.
func (s Secret) GoString() string {
	return redacted
}

// MarshalText returns "[redacted]" so JSON encoding does not print the value.
func (s Secret) MarshalText() ([]byte, error) {
	return []byte(redacted), nil
}

// LogValue returns "[redacted]" for slog.
func (s Secret) LogValue() slog.Value {
	return slog.StringValue(redacted)
}
