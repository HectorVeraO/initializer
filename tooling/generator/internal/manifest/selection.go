package manifest

import (
	"bytes"
	"encoding/json"
)

// FlagsSource is the issue path source of a selection built from flags.
const FlagsSource = "flags"

// Project names the generated project.
type Project struct {
	Name     string `json:"name,omitempty"`
	GoModule string `json:"goModule,omitempty"`
}

// Component is one components[] entry as given. Name is empty when the entry
// does not name the component.
type Component struct {
	Stack string `json:"stack"`
	Name  string `json:"name,omitempty"`
}

// Database names the database and its owner role, as given.
type Database struct {
	Name string `json:"name,omitempty"`
	User string `json:"user,omitempty"`
}

// Presence records which optional top-level values an input gave, so "absent"
// and "empty" differ.
type Presence struct {
	Name, GoModule, Stacks, Modules bool
}

// Selection is a selection document: the input as given (DecodeInput,
// FlagsInput, Merge), the recorded document (RecordedSelection) or the
// resolved document (ResolveDefaults), by context.
type Selection struct {
	Schema  int     `json:"schema"`
	Project Project `json:"project"`
	// DisplayName is the brand text as given; schema 2 only.
	DisplayName string   `json:"displayName,omitempty"`
	Stacks      []string `json:"stacks"`
	Modules     []string `json:"modules"`
	// Components holds the component entries as given; schema 2 only.
	Components []Component `json:"components,omitempty"`
	// Database names the database and its user; schema 2 only.
	Database *Database `json:"database,omitempty"`
	// Services maps a service key to its compose service name; schema 2 only.
	Services map[string]string `json:"services,omitempty"`
	// AllModules is the --all-modules flag; the plan expands it and it is
	// never encoded.
	AllModules bool `json:"-"`
	// Has records which optional lists were given.
	Has Presence `json:"-"`
}

// MarshalJSON writes the selection in key order schema, project, displayName, stacks,
// modules, components, database, services. A schema 1 document always writes
// stacks and modules. A schema 2 document writes each when Has says it was given or when it holds
// values (a decoded lock carries no Has); an absent list is omitted.
func (s Selection) MarshalJSON() ([]byte, error) {
	type document struct {
		Schema      int               `json:"schema"`
		Project     Project           `json:"project"`
		DisplayName string            `json:"displayName,omitempty"`
		Stacks      *[]string         `json:"stacks,omitempty"`
		Modules     *[]string         `json:"modules,omitempty"`
		Components  []Component       `json:"components,omitempty"`
		Database    *Database         `json:"database,omitempty"`
		Services    map[string]string `json:"services,omitempty"`
	}
	out := document{Schema: s.Schema, Project: s.Project, DisplayName: s.DisplayName, Components: s.Components, Database: s.Database, Services: s.Services}
	isFirstSchema := s.Schema == 1
	if isFirstSchema || s.Has.Stacks || len(s.Stacks) > 0 {
		stacks := nonNil(s.Stacks)
		out.Stacks = &stacks
	}
	if isFirstSchema || s.Has.Modules || len(s.Modules) > 0 {
		modules := nonNil(s.Modules)
		out.Modules = &modules
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(out); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buffer.Bytes(), "\n"), nil
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// Origin is where one value came from, for issue paths and text rendering.
type Origin struct {
	// Source is the file path, "stdin", "flags", "args" (the DIR argument) or
	// "default".
	Source string
	// Pointer is the pointer inside that source; for a --component flag it is
	// /components/<flag index>.
	Pointer string
	// Line is the source line of a YAML value, else 0.
	Line int
	// Arg is the flag as typed for a flag origin ("--component go-srv=acme-api").
	Arg string
}

// Input is a selection as given plus the origin of every value, keyed by the
// value's pointer in the merged document.
type Input struct {
	Selection Selection
	Origins   map[string]Origin
}
