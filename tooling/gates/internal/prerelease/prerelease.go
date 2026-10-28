// Package prerelease reads the repository's pre-release setting (L76, L92).
package prerelease

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileName is the repo-relative path of the setting.
const FileName = "versioning.json"

const supportedSchema = 1

type document struct {
	Schema     *int  `json:"schema"`
	PreRelease *bool `json:"preRelease"`
}

// Load returns the preRelease value of <root>/versioning.json; a missing
// file is false. A file that exists but breaks the grammar is an error that
// names the file.
func Load(root string) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(root, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%s: %w", FileName, err)
	}
	var doc document
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	err = dec.Decode(&doc)
	if err != nil {
		// A type mismatch still leaves the other fields decoded, so the
		// message below can name every problem at once.
		var typeErr *json.UnmarshalTypeError
		if !errors.As(err, &typeErr) {
			return false, fmt.Errorf("%s: %v", FileName, err)
		}
		return false, fmt.Errorf("%s: %s", FileName, strings.Join(grammarProblems(raw), "; "))
	}
	var problems []string
	if doc.Schema == nil {
		problems = append(problems, "schema is missing")
	} else if *doc.Schema != supportedSchema {
		problems = append(problems, fmt.Sprintf("schema must be %d", supportedSchema))
	}
	if doc.PreRelease == nil {
		problems = append(problems, "preRelease is missing")
	}
	_, err = dec.Token()
	if !errors.Is(err, io.EOF) {
		problems = append(problems, "unexpected data after the object")
	}
	if len(problems) > 0 {
		return false, fmt.Errorf("%s: %s", FileName, strings.Join(problems, "; "))
	}
	return *doc.PreRelease, nil
}

// grammarProblems lists every field-level problem of a document that has the
// right shape of keys but a wrong value type.
func grammarProblems(raw []byte) []string {
	var fields map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(raw))
	err := dec.Decode(&fields)
	if err != nil {
		return []string{"must be a JSON object"}
	}
	var problems []string
	var unknown []string
	for key := range fields {
		if key != "schema" && key != "preRelease" {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	for _, key := range unknown {
		problems = append(problems, fmt.Sprintf("unknown field %q", key))
	}
	_, err = dec.Token()
	if !errors.Is(err, io.EOF) {
		problems = append(problems, "unexpected data after the object")
	}
	var schema int
	schemaRaw, hasSchema := fields["schema"]
	if !hasSchema {
		problems = append(problems, "schema is missing")
	} else if json.Unmarshal(schemaRaw, &schema) != nil || schema != supportedSchema {
		problems = append(problems, fmt.Sprintf("schema must be the number %d", supportedSchema))
	}
	var preRelease bool
	preRaw, hasPre := fields["preRelease"]
	if !hasPre {
		problems = append(problems, "preRelease is missing")
	} else if json.Unmarshal(preRaw, &preRelease) != nil {
		problems = append(problems, "preRelease must be a JSON boolean")
	}
	if len(problems) == 0 {
		problems = append(problems, "invalid value")
	}
	return problems
}
