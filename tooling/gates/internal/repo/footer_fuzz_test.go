package repo

import (
	"strings"
	"testing"
)

// isTrailerValue reports whether value is the trimmed text after ": " on a
// line of message whose token is wanted.
func isTrailerValue(message, value string, wanted map[string]bool) bool {
	for _, line := range strings.Split(strings.ReplaceAll(message, "\r\n", "\n"), "\n") {
		token, rest, found := strings.Cut(line, ": ")
		hasMatch := found && wanted[strings.ToLower(token)] && strings.TrimSpace(rest) == value
		if hasMatch {
			return true
		}
	}
	return false
}

// FuzzFooterValues checks that footer parsing never panics and only returns
// single-line, trimmed values that follow a wanted token in the message.
func FuzzFooterValues(f *testing.F) {
	f.Add("subject\n\nbody\n\nBREAKING CHANGE: a\nb\nRefs: #1")
	f.Add("x")
	f.Add("a\n\nRefs: #1\nprose")
	wanted := map[string]bool{"breaking change": true, "refs": true}
	f.Fuzz(func(t *testing.T, message string) {
		for _, value := range footerValues(message, wanted) {
			if !isTrailerValue(message, value, wanted) {
				t.Fatalf("value %q is not the text after the colon of a wanted trailer line", value)
			}
			if strings.ContainsAny(value, "\n") || value != strings.TrimSpace(value) {
				t.Fatalf("value %q is not a single trimmed line", value)
			}
		}
	})
}
