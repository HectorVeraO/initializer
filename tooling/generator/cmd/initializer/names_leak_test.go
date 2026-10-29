package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/lockfile"
)

const (
	leakAllowlistPath = "tooling/generator/testdata/names-leak-allowlist.json"
	nulProbeBytes     = 8000
)

// leakEntry is one allowlist entry: a token occurrence inside an occurrence
// of Context in a file matching Path is not a leak.
type leakEntry struct {
	Path    string `json:"path"`
	Context string `json:"context"`
	Reason  string `json:"reason"`
}

type leakAllowlist struct {
	Schema  int         `json:"schema"`
	Entries []leakEntry `json:"entries"`
}

// leakToken is a reference name that must not survive in a delivered file.
type leakToken struct {
	text string
	// foldCase matches ASCII case-insensitively.
	foldCase bool
	// deployOnly limits the token to the deploy surface.
	deployOnly bool
}

// leakFile is one delivered file under its source-form path.
type leakFile struct {
	path string
	data []byte
}

var deploySurface = []string{
	".deploy/local/**", "mise.toml", "ng-web/nginx.conf.template", "ng-web/proxy.conf.json", "README.md",
}

// parseLeakAllowlist decodes the allowlist and returns every structural
// problem: unknown keys, schema, empty reason or context, duplicates.
func parseLeakAllowlist(data []byte) (leakAllowlist, []string) {
	var list leakAllowlist
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&list); err != nil {
		return leakAllowlist{}, []string{"allowlist: " + err.Error()}
	}
	var problems []string
	if list.Schema != 1 {
		problems = append(problems, fmt.Sprintf("allowlist: schema must be 1, got %d", list.Schema))
	}
	seen := map[string]bool{}
	for i, entry := range list.Entries {
		if entry.Path == "" || entry.Context == "" {
			problems = append(problems, fmt.Sprintf("entry %d: path and context are required", i))
		}
		if strings.TrimSpace(entry.Reason) == "" {
			problems = append(problems, fmt.Sprintf("entry %d (%s, %q): reason is required", i, entry.Path, entry.Context))
		}
		key := entry.Path + "\x00" + entry.Context
		if seen[key] {
			problems = append(problems, fmt.Sprintf("entry %d: duplicate (%s, %q)", i, entry.Path, entry.Context))
		}
		seen[key] = true
	}
	return list, problems
}

// globMatch matches a slash path against a pattern: "*" stays inside one
// segment, "**" spans any number of segments, including none.
func globMatch(pattern, target string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(target, "/"))
}

func matchSegments(pattern, target []string) bool {
	if len(pattern) == 0 {
		return len(target) == 0
	}
	if pattern[0] == "**" {
		for skip := 0; skip <= len(target); skip++ {
			if matchSegments(pattern[1:], target[skip:]) {
				return true
			}
		}
		return false
	}
	if len(target) == 0 {
		return false
	}
	matched, err := filepath.Match(pattern[0], target[0])
	if err != nil || !matched {
		return false
	}
	return matchSegments(pattern[1:], target[1:])
}

// asciiLower lowercases ASCII letters only, so byte offsets stay valid.
func asciiLower(data []byte) []byte {
	out := make([]byte, len(data))
	for i, c := range data {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return out
}

// occurrences returns the start offsets of every non-overlapping occurrence
// of needle in haystack.
func occurrences(haystack, needle []byte) []int {
	var found []int
	for from := 0; from < len(haystack); {
		at := bytes.Index(haystack[from:], needle)
		if at < 0 {
			break
		}
		found = append(found, from+at)
		from += at + len(needle)
	}
	return found
}

func isTextFile(data []byte) bool {
	probe := data
	if len(probe) > nulProbeBytes {
		probe = probe[:nulProbeBytes]
	}
	return !bytes.Contains(probe, []byte{0})
}

func onDeploySurface(path string) bool {
	for _, pattern := range deploySurface {
		if globMatch(pattern, path) {
			return true
		}
	}
	return false
}

// findLeaks scans files for tokens. It returns one message per unallowed
// occurrence and the entries that allowed nothing.
func findLeaks(files []leakFile, tokens []leakToken, entries []leakEntry) (leaks, stale []string) {
	used := make([]bool, len(entries))
	for _, file := range files {
		if !isTextFile(file.data) {
			continue
		}
		type span struct{ start, end int }
		var allowed []struct {
			entry int
			span  span
		}
		for i, entry := range entries {
			if !globMatch(entry.Path, file.path) {
				continue
			}
			for _, at := range occurrences(file.data, []byte(entry.Context)) {
				allowed = append(allowed, struct {
					entry int
					span  span
				}{i, span{at, at + len(entry.Context)}})
			}
		}
		lowered := asciiLower(file.data)
		for _, token := range tokens {
			if token.deployOnly && !onDeploySurface(file.path) {
				continue
			}
			haystack, needle := file.data, []byte(token.text)
			if token.foldCase {
				haystack, needle = lowered, asciiLower(needle)
			}
			for _, start := range occurrences(haystack, needle) {
				end := start + len(needle)
				isAllowed := false
				for _, candidate := range allowed {
					if candidate.span.start <= start && end <= candidate.span.end {
						used[candidate.entry] = true
						isAllowed = true
					}
				}
				if isAllowed {
					continue
				}
				line := bytes.Count(file.data[:start], []byte("\n")) + 1
				leaks = append(leaks, fmt.Sprintf("%s:%d: %q (token %q)", file.path, line, snippet(file.data, start), token.text))
			}
		}
	}
	for i, entry := range entries {
		if !used[i] {
			stale = append(stale, fmt.Sprintf("%s %q", entry.Path, entry.Context))
		}
	}
	sort.Strings(leaks)
	return leaks, stale
}

// snippet returns the line around start, trimmed to 100 bytes.
func snippet(data []byte, start int) string {
	from := bytes.LastIndexByte(data[:start], '\n') + 1
	to := bytes.IndexByte(data[start:], '\n')
	if to < 0 {
		to = len(data)
	} else {
		to += start
	}
	line := string(data[from:to])
	if len(line) > 100 {
		line = line[:100]
	}
	return strings.TrimSpace(line)
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pattern, target string
		want            bool
	}{
		{"**", "a/b/c.txt", true},
		{"**", "c.txt", true},
		{"a/**", "a/b/c", true},
		{"a/**", "a", true},
		{"a/**/c", "a/c", true},
		{"a/**/c", "a/b/b/c", true},
		{"a/*", "a/b/c", false},
		{"a/*", "a/b", true},
		{"*.md", "docs/a.md", false},
		{"**/module.manifest.json", "go-srv/internal/module.manifest.json", true},
		{"**/module.manifest.json", "module.manifest.json", true},
		{"ng-web/**", "go-srv/x", false},
		{".gitignore", ".gitignore", true},
	}
	for _, c := range cases {
		if got := globMatch(c.pattern, c.target); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pattern, c.target, got, c.want)
		}
	}
}

func TestFindLeaksContainmentAndStaleEntries(t *testing.T) {
	files := []leakFile{{path: "a/b.txt", data: []byte("keep go-srv here\nand go-srv there\nINITIALIZER too\n")}}
	tokens := []leakToken{{text: "go-srv"}, {text: "initializer", foldCase: true}}
	entries := []leakEntry{
		{Path: "a/**", Context: "keep go-srv here", Reason: "r"},
		{Path: "a/**", Context: "never present", Reason: "r"},
		{Path: "z/**", Context: "go-srv", Reason: "r"},
	}
	leaks, stale := findLeaks(files, tokens, entries)
	if len(leaks) != 2 {
		t.Fatalf("leaks = %v", leaks)
	}
	if !strings.Contains(leaks[0], "a/b.txt:2") || !strings.Contains(leaks[1], "a/b.txt:3") {
		t.Fatalf("leaks carry no path and line: %v", leaks)
	}
	if len(stale) != 2 {
		t.Fatalf("stale = %v", stale)
	}
}

func TestFindLeaksContextMustCoverTheWholeToken(t *testing.T) {
	files := []leakFile{{path: "f.txt", data: []byte("x initializer:stack go-srv y\n")}}
	tokens := []leakToken{{text: "go-srv"}}
	leaks, _ := findLeaks(files, tokens, []leakEntry{{Path: "**", Context: "initializer:stack ", Reason: "r"}})
	if len(leaks) != 1 {
		t.Fatalf("a context that stops before the token allowed it: %v", leaks)
	}
	leaks, stale := findLeaks(files, tokens, []leakEntry{{Path: "**", Context: "initializer:stack go-srv", Reason: "r"}})
	if len(leaks) != 0 || len(stale) != 0 {
		t.Fatalf("leaks = %v, stale = %v", leaks, stale)
	}
}

func TestFindLeaksSkipsBinaryAndLimitsServiceTokensToTheDeploySurface(t *testing.T) {
	files := []leakFile{
		{path: "bin.dat", data: []byte("go-srv\x00go-srv")},
		{path: "go-srv/code.go", data: []byte("kratos\n")},
		{path: ".deploy/local/x.yml", data: []byte("kratos\n")},
	}
	tokens := []leakToken{{text: "go-srv"}, {text: "kratos", deployOnly: true}}
	leaks, _ := findLeaks(files, tokens, nil)
	if len(leaks) != 1 || !strings.HasPrefix(leaks[0], ".deploy/local/x.yml:1") {
		t.Fatalf("leaks = %v", leaks)
	}
}

func TestParseLeakAllowlistRefusesBadLists(t *testing.T) {
	cases := map[string]string{
		"unknown key":  `{"schema":1,"entries":[{"path":"a","context":"b","reason":"c","extra":1}]}`,
		"empty reason": `{"schema":1,"entries":[{"path":"a","context":"b","reason":" "}]}`,
		"duplicate":    `{"schema":1,"entries":[{"path":"a","context":"b","reason":"c"},{"path":"a","context":"b","reason":"d"}]}`,
		"schema":       `{"schema":2,"entries":[]}`,
		"no context":   `{"schema":1,"entries":[{"path":"a","context":"","reason":"c"}]}`,
	}
	for name, text := range cases {
		if _, problems := parseLeakAllowlist([]byte(text)); len(problems) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, problems := parseLeakAllowlist([]byte(`{"schema":1,"entries":[{"path":"a","context":"b","reason":"c"}]}`)); len(problems) != 0 {
		t.Errorf("a good list was refused: %v", problems)
	}
}

// loadLeakFiles reads every delivered file except the lock and the recorded
// selection, under its source-form path.
func loadLeakFiles(t *testing.T, out string, folders map[string]string) []leakFile {
	t.Helper()
	var files []leakFile
	walkErr := filepath.WalkDir(out, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(out, path)
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(relative)
		if slash == lockfile.Name || slash == lockfile.SelectionName {
			return nil
		}
		for stack, folder := range folders {
			if rest, found := strings.CutPrefix(slash, folder+"/"); found {
				slash = stack + "/" + rest
				break
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files = append(files, leakFile{path: slash, data: data})
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	return files
}

// leakTokens builds the scan tokens from the catalog reference.
func leakTokens(t *testing.T, reference map[string]string) []leakToken {
	t.Helper()
	project := reference["project"]
	hasComponent := false
	var tokens []leakToken
	seen := map[string]bool{}
	add := func(token leakToken) {
		key := fmt.Sprintf("%v|%v|%s", token.foldCase, token.deployOnly, token.text)
		if !seen[key] {
			seen[key] = true
			tokens = append(tokens, token)
		}
	}
	slots := make([]string, 0, len(reference))
	for slot := range reference {
		slots = append(slots, slot)
	}
	sort.Strings(slots)
	for _, slot := range slots {
		value := reference[slot]
		switch {
		case strings.HasPrefix(slot, "component."):
			hasComponent = true
			add(leakToken{text: value})
		case strings.HasPrefix(slot, "service."):
			add(leakToken{text: value, deployOnly: true})
		case strings.EqualFold(strings.ReplaceAll(value, "-", "_"), strings.ReplaceAll(project, "-", "_")):
			add(leakToken{text: value, foldCase: true})
		}
	}
	if project == "" || !hasComponent {
		t.Fatalf("the catalog reference has no project or no component slot: the scan would be vacuous (%v)", reference)
	}
	return tokens
}

// A copied file that names the project, a component, the display name, the
// database or a service must carry a rule; this test generates every module
// of both stacks under distinct names and fails on any reference name left.
func TestGeneratedFilesLeakNoReferenceName(t *testing.T) {
	root, catalogPath := realCatalog(t)
	allowData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(leakAllowlistPath)))
	if err != nil {
		t.Fatal(err)
	}
	allowlist, problems := parseLeakAllowlist(allowData)
	if len(problems) > 0 {
		t.Fatalf("%s: %s", leakAllowlistPath, strings.Join(problems, "; "))
	}
	catalogData, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	var catalogDoc struct {
		Reference map[string]string `json:"reference"`
	}
	if err := json.Unmarshal(catalogData, &catalogDoc); err != nil {
		t.Fatal(err)
	}
	tokens := leakTokens(t, catalogDoc.Reference)

	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, "--out", out,
		"--name", "acme", "--stacks", "go-srv,ng-web", "--all-modules", "--go-module", "example.com/acme",
		"--component", "go-srv=acme-api", "--component", "ng-web=acme-console",
		"--service", "postgres=acme-db", "--service", "postgres-init=acme-dbinit",
		"--service", "kratos-migrate=acme-idmigrate", "--service", "kratos=acme-id",
		"--db-name", "acmedb", "--db-user", "acmeowner", "--display-name", "Acme Shop")
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	folders := map[string]string{"go-srv": "acme-api", "ng-web": "acme-console"}
	files := loadLeakFiles(t, out, folders)
	leaks, stale := findLeaks(files, tokens, allowlist.Entries)
	for _, found := range leaks {
		t.Errorf("reference name left in a delivered file: %s\n  fix: add a rule to the module manifest names, neutralize the source text, or add an allowlist entry with a reason in %s", found, leakAllowlistPath)
	}
	for _, found := range stale {
		t.Errorf("stale allowlist entry (it allows nothing): %s", found)
	}
}
