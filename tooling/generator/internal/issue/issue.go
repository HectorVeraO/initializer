// Package issue holds the validation problem every command reports.
package issue

import (
	"encoding/json"
	"io"
	"sort"
	"strings"
)

// Issue is one validation problem; commands collect all of them before failing.
type Issue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
	// Hint is what to do next; omitted from the output when empty.
	Hint string `json:"hint,omitempty"`
}

// Sort orders issues by path, then code, then message, in place.
func Sort(issues []Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		a, b := issues[i], issues[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
}

// Write sorts a copy of issues and writes them as one compact JSON array
// followed by a newline.
func Write(w io.Writer, issues []Issue) error {
	sorted := make([]Issue, len(issues))
	copy(sorted, issues)
	Sort(sorted)
	encoded, err := json.Marshal(sorted)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_, err = w.Write(encoded)
	return err
}

// Pointer builds an RFC 6901 JSON pointer from unescaped reference tokens.
func Pointer(tokens ...string) string {
	var b strings.Builder
	for _, token := range tokens {
		token = strings.ReplaceAll(token, "~", "~0")
		token = strings.ReplaceAll(token, "/", "~1")
		b.WriteByte('/')
		b.WriteString(token)
	}
	return b.String()
}

// Path joins a source file path and a JSON pointer as "<source>#<pointer>".
func Path(source, pointer string) string {
	return source + "#" + pointer
}
