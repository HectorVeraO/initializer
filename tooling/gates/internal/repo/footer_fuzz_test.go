package repo

import (
	"strings"
	"testing"
)

// FuzzFooterValues checks that footer parsing never panics and only returns
// single-line, trimmed values taken from the message.
func FuzzFooterValues(f *testing.F) {
	f.Add("subject\n\nbody\n\nBREAKING CHANGE: a\nb\nRefs: #1")
	f.Add("x")
	f.Add("a\n\nRefs: #1\nprose")
	wanted := map[string]bool{"breaking change": true, "refs": true}
	f.Fuzz(func(t *testing.T, message string) {
		for _, value := range footerValues(message, wanted) {
			if !strings.Contains(message, value) {
				t.Fatalf("value %q not in message", value)
			}
			if strings.ContainsAny(value, "\n") || value != strings.TrimSpace(value) {
				t.Fatalf("value %q is not a single trimmed line", value)
			}
		}
	})
}
