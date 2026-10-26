package versions

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// fileEntry is one element of a manifest "files" array: a path string, or an
// object {path, target} whose path is the source in the repository.
type fileEntry struct {
	Path string
}

func (e *fileEntry) UnmarshalJSON(raw []byte) error {
	var text string
	err := json.Unmarshal(raw, &text)
	if err == nil {
		e.Path = text
		return nil
	}
	var object struct {
		Path string `json:"path"`
	}
	err = json.Unmarshal(raw, &object)
	if err != nil {
		return errors.New("a files entry must be a string or an object {path, target}")
	}
	e.Path = object.Path
	return nil
}

// manifestDoc holds the only manifest fields this gate reads.
type manifestDoc struct {
	Module  string      `json:"module"`
	Version string      `json:"version"`
	Files   []fileEntry `json:"files"`
	Exclude []string    `json:"exclude"`
}

func parseManifest(raw []byte) (manifestDoc, error) {
	var doc manifestDoc
	err := json.Unmarshal(raw, &doc)
	if err != nil {
		return manifestDoc{}, fmt.Errorf("not valid JSON of the expected shape: %v", err)
	}
	return doc, nil
}

// normalizeEntry returns a repo-relative entry with forward slashes. A
// trailing slash (a folder) is kept. It rejects empty, absolute, drive-letter
// and dot-segment entries.
func normalizeEntry(entry string) (string, error) {
	normalized := strings.ReplaceAll(entry, "\\", "/")
	hasDrive := len(normalized) >= 2 && normalized[1] == ':'
	if normalized == "" || strings.HasPrefix(normalized, "/") || hasDrive {
		return "", fmt.Errorf("path entry %q is not a repo-relative path", entry)
	}
	for _, segment := range strings.Split(strings.TrimSuffix(normalized, "/"), "/") {
		isBad := segment == "" || segment == "." || segment == ".."
		if isBad {
			return "", fmt.Errorf("path entry %q has an empty or dot segment", entry)
		}
	}
	return normalized, nil
}
