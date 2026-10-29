package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/ports"
)

// repoPortFiles are the source paths of the declared port files of the
// repository and, for the ng-web ones, the stack they belong to.
var repoPortFiles = map[string]string{
	".deploy/local/docker-compose.yml": "",
	".deploy/local/.example.env":       "",
	".deploy/local/README.md":          "",
	"mise.toml":                        "",
	".deploy/local/kratos/kratos.yml":  "",
	"ng-web/angular.json":              "ng-web",
	"ng-web/package.json":              "ng-web",
	"ng-web/proxy.conf.json":           "ng-web",
}

// generateWithBand generates the real checkout into a new folder with the
// extra arguments and returns the folder and the checkout root.
func generateWithBand(t *testing.T, extra ...string) (out, root string) {
	t.Helper()
	root, catalogPath := realCatalog(t)
	out = filepath.Join(t.TempDir(), "out")
	args := []string{"generate", out, "--root", root, "--catalog", catalogPath, "--name", "acme",
		"--go-module", "example.com/acme", "--all-modules"}
	code, stdout, stderr := execute(t, append(args, extra...)...)
	if code != exitOK {
		t.Fatalf("generate exit %d: %s%s", code, stdout, stderr)
	}
	return out, root
}

// deliveredPath returns where a repo port file lands in a project.
func deliveredPath(t *testing.T, out, source string) string {
	t.Helper()
	stack := repoPortFiles[source]
	if stack == "" {
		return source
	}
	return folderOf(t, out, stack) + strings.TrimPrefix(source, stack)
}

// sourceTokens scans a repo file.
func sourceTokens(t *testing.T, root, source string) []ports.Token {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(source)))
	if err != nil {
		t.Fatal(err)
	}
	return ports.Scan(data)
}

// projectTokens scans a delivered port file.
func projectTokens(t *testing.T, out, delivered string) []ports.Token {
	t.Helper()
	return ports.Scan([]byte(readProjectFile(t, out, delivered)))
}

// With a console, every token of every port file moves to the chosen band and
// keeps its own offset.
func TestBand103WithAConsoleMovesEveryPortFile(t *testing.T) {
	out, root := generateWithBand(t, "--stacks", "go-srv,ng-web", "--port-band", "103")
	for source := range repoPortFiles {
		want := sourceTokens(t, root, source)
		got := projectTokens(t, out, deliveredPath(t, out, source))
		if len(want) == 0 || len(got) != len(want) {
			t.Errorf("%s: %d tokens, source has %d", source, len(got), len(want))
			continue
		}
		for i, token := range got {
			if token.Band != 103 || token.Offset != want[i].Offset {
				t.Errorf("%s: token %d is band %d offset %d, want band 103 offset %d", source, i, token.Band, token.Offset, want[i].Offset)
			}
		}
	}
	readme := readProjectFile(t, out, "README.md")
	for _, want := range []string{"10300-10399", "http://localhost:10320"} {
		if !strings.Contains(readme, want) {
			t.Errorf("README lacks %q", want)
		}
	}
	for _, banned := range []string{"10100", "10199", "10120"} {
		if strings.Contains(readme, banned) {
			t.Errorf("README still holds %q", banned)
		}
	}
}

// The reference band with a console is the reference output: no token moves.
func TestBand101WithAConsoleKeepsEveryToken(t *testing.T) {
	out, root := generateWithBand(t, "--stacks", "go-srv,ng-web")
	for source := range repoPortFiles {
		want := sourceTokens(t, root, source)
		got := projectTokens(t, out, deliveredPath(t, out, source))
		if len(got) != len(want) {
			t.Fatalf("%s: %d tokens, source has %d", source, len(got), len(want))
		}
		for i, token := range got {
			if token.Band != 101 || token.Offset != want[i].Offset {
				t.Errorf("%s: token %d is %d%02d, want 101%02d", source, i, token.Band, token.Offset, want[i].Offset)
			}
		}
	}
	readme := readProjectFile(t, out, "README.md")
	for _, want := range []string{"10100-10199", "http://localhost:10120"} {
		if !strings.Contains(readme, want) {
			t.Errorf("README lacks %q", want)
		}
	}
}

// Without a console the origin is the API port: no offset 20 survives and every
// former console port reads <band>00.
func TestBackendOnlyOriginIsTheBandBase(t *testing.T) {
	for _, band := range []int{101, 103} {
		t.Run(strconv.Itoa(band), func(t *testing.T) {
			out, root := generateWithBand(t, "--stacks", "go-srv", "--port-band", strconv.Itoa(band))
			sawOrigin := false
			for source, stack := range repoPortFiles {
				if stack != "" {
					continue
				}
				sourceOffsets := map[int]bool{}
				for _, token := range sourceTokens(t, root, source) {
					sourceOffsets[token.Offset] = true
				}
				for _, token := range projectTokens(t, out, source) {
					if token.Band != band || token.Offset == 20 || !(sourceOffsets[token.Offset] || token.Offset == 0) {
						t.Errorf("%s: unexpected token %d%02d", source, token.Band, token.Offset)
					}
					sawOrigin = sawOrigin || token.Offset == 0
				}
			}
			if !sawOrigin {
				t.Fatal("no token is the origin")
			}
			origin := "http://localhost:" + strconv.Itoa(band*100)
			wantLines := map[string]string{
				"mise.toml":                  "APP_PUBLIC_BASE_URL = \"" + origin,
				".deploy/local/.example.env": "APP_PUBLIC_BASE_URL=" + origin,
			}
			for path, want := range wantLines {
				if !strings.Contains(readProjectFile(t, out, path), want) {
					t.Errorf("%s: the public base URL is not %s", path, origin)
				}
			}
			kratos := readProjectFile(t, out, ".deploy/local/kratos/kratos.yml")
			if !strings.Contains(kratos, origin+"/kratos/") || strings.Contains(kratos, strconv.Itoa(band*100+20)) {
				t.Errorf("kratos.yml does not use the origin %s", origin)
			}
			readme := readProjectFile(t, out, "README.md")
			if !strings.Contains(readme, "The API opens on "+origin) {
				t.Errorf("README does not open the API on %s", origin)
			}
		})
	}
}

func TestVerifyPassesOnAProjectOfAnotherBand(t *testing.T) {
	for _, stacks := range []string{"go-srv", "go-srv,ng-web"} {
		out, _ := generateWithBand(t, "--stacks", stacks, "--port-band", "103")
		code, stdout, stderr := execute(t, "verify", out)
		if code != exitOK {
			t.Errorf("%s: verify exit %d: %s%s", stacks, code, stdout, stderr)
		}
	}
}

func TestTheLockRecordsTheEffectiveBand(t *testing.T) {
	cases := map[string]int{"": 101, "103": 103, "654": 654, "100": 100}
	for flagValue, want := range cases {
		args := []string{"--stacks", "go-srv"}
		if flagValue != "" {
			args = append(args, "--port-band", flagValue)
		}
		out, _ := generateWithBand(t, args...)
		lockPorts, _ := projectLock(t, out)["ports"].(map[string]any)
		if lockPorts["band"] != float64(want) {
			t.Errorf("--port-band %q: lock ports = %v, want band %d", flagValue, lockPorts, want)
		}
	}
}

func TestTheRecordedSelectionCarriesPortsOnlyWhenGiven(t *testing.T) {
	given, _ := generateWithBand(t, "--stacks", "go-srv", "--port-band", "103")
	var recorded map[string]any
	if err := json.Unmarshal([]byte(readProjectFile(t, given, "initializer.selection.json")), &recorded); err != nil {
		t.Fatal(err)
	}
	if recorded["schema"] != float64(2) || recorded["ports"].(map[string]any)["band"] != float64(103) {
		t.Errorf("recorded = %v", recorded)
	}
	absent, _ := generateWithBand(t, "--stacks", "go-srv")
	text := readProjectFile(t, absent, "initializer.selection.json")
	if strings.Contains(text, "ports") {
		t.Errorf("a default band must not be recorded:\n%s", text)
	}
}

// readTree returns every file under dir keyed by slash path.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		tree[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func TestTheBandFromAFileAndFromTheFlagGiveTheSameProject(t *testing.T) {
	root, catalogPath := realCatalog(t)
	file := filepath.Join(t.TempDir(), "in.json")
	body := `{"schema":2,"project":{"name":"acme","goModule":"example.com/acme"},"stacks":["go-srv","ng-web"],"modules":[],"ports":{"band":103}}`
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	fromFile := filepath.Join(t.TempDir(), "file")
	code, stdout, stderr := execute(t, "generate", fromFile, "--root", root, "--catalog", catalogPath, "-f", file)
	if code != exitOK {
		t.Fatalf("file run exit %d: %s%s", code, stdout, stderr)
	}
	fromFlag := filepath.Join(t.TempDir(), "flag")
	code, stdout, stderr = execute(t, "generate", fromFlag, "--root", root, "--catalog", catalogPath, "--name", "acme",
		"--go-module", "example.com/acme", "--stacks", "go-srv,ng-web", "--port-band", "103")
	if code != exitOK {
		t.Fatalf("flag run exit %d: %s%s", code, stdout, stderr)
	}
	left, right := readTree(t, fromFile), readTree(t, fromFlag)
	if len(left) == 0 || len(left) != len(right) {
		t.Fatalf("%d files from the file, %d from the flag", len(left), len(right))
	}
	var differing []string
	for path, content := range left {
		if right[path] != content {
			differing = append(differing, path)
		}
	}
	sort.Strings(differing)
	if len(differing) > 0 {
		t.Fatalf("files differ: %v", differing)
	}
}

func TestAFlagBandReplacesTheFileBand(t *testing.T) {
	file := filepath.Join(t.TempDir(), "in.json")
	body := `{"schema":2,"project":{"name":"acme"},"stacks":["go-srv"],"modules":[],"ports":{"band":103}}`
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if doc := infraDryRunJSON(t, "-f", file); doc.Ports.Band != 103 {
		t.Errorf("file band: %+v", doc.Ports)
	}
	if doc := infraDryRunJSON(t, "-f", file, "--port-band", "110"); doc.Ports.Band != 110 {
		t.Errorf("flag over file: %+v", doc.Ports)
	}
}

func TestDryRunShowsTheEffectiveBand(t *testing.T) {
	for flagArgs, want := range map[string]string{"": "101", "--port-band 103": "103"} {
		extra := []string{"--name", "acme", "--stacks", "go-srv"}
		if flagArgs != "" {
			extra = append(extra, strings.Fields(flagArgs)...)
		}
		code, stdout, stderr := infraDryRun(t, extra...)
		if code != exitOK {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
		if len(lines) < 2 || lines[len(lines)-2] != "Ports       band "+want {
			t.Errorf("%q: the row before the file count is %q", flagArgs, lines[len(lines)-2])
		}
		doc := infraDryRunJSON(t, extra...)
		if doc.Ports.Band != mustAtoi(t, want) {
			t.Errorf("%q: JSON ports = %+v", flagArgs, doc.Ports)
		}
	}
}

func mustAtoi(t *testing.T, text string) int {
	t.Helper()
	value, err := strconv.Atoi(text)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestTheDryRunJSONPortsMemberFollowsNames(t *testing.T) {
	_, stdout, _ := infraDryRun(t, "--name", "acme", "--stacks", "go-srv", "--port-band", "103", "--format", "json")
	names, ports, added := strings.Index(stdout, `"names":`), strings.LastIndex(stdout, `"ports":{"band":103}`), strings.Index(stdout, `"added":`)
	if names < 0 || !(names < ports && ports < added) {
		t.Fatalf("ports must sit between names and added: %d %d %d", names, ports, added)
	}
}

func TestABandOutsideItsRangeIsAValueProblemAtTheFlag(t *testing.T) {
	for _, value := range []string{"abc", "99", "655", "", "10x"} {
		code, stdout, _ := infraDryRun(t, "--name", "acme", "--stacks", "go-srv", "--port-band", value, "--format", "json")
		found := issueArray(t, stdout)
		if code != exitIssues || len(found) != 1 {
			t.Errorf("%q: exit %d issues %v", value, code, found)
			continue
		}
		if found[0]["path"] != "flags#/ports/band" || found[0]["code"] != "SELECTION_INVALID" ||
			found[0]["message"] != "must be an integer from 100 to 654" {
			t.Errorf("%q: issue %v", value, found[0])
		}
	}
}

func TestABadBandInTheTextReportNamesTheFlag(t *testing.T) {
	code, _, stderr := infraDryRun(t, "--name", "acme", "--stacks", "go-srv", "--port-band", "abc")
	want := "error: --port-band: must be an integer from 100 to 654 [SELECTION_INVALID]"
	if code != exitIssues || !strings.Contains(stderr, want) {
		t.Fatalf("exit %d, stderr %q lacks %q", code, stderr, want)
	}
}

func TestABandProblemIsCollectedWithOtherProblems(t *testing.T) {
	code, stdout, _ := infraDryRun(t, "--name", "Not_A_Label", "--stacks", "go-srv", "--port-band", "abc", "--format", "json")
	found := issueArray(t, stdout)
	if code != exitIssues || len(found) != 2 {
		t.Fatalf("exit %d issues %v", code, found)
	}
}

func TestABandOutOfRangeInAFileNamesTheFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "in.json")
	body := `{"schema":2,"project":{"name":"acme"},"stacks":["go-srv"],"modules":[],"ports":{"band":99}}`
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := infraDryRun(t, "-f", file, "--format", "json")
	found := issueArray(t, stdout)
	if code != exitIssues || len(found) != 1 || !strings.HasSuffix(found[0]["path"], "#/ports/band") ||
		found[0]["message"] != "must be an integer from 100 to 654" {
		t.Fatalf("exit %d issues %v", code, found)
	}
}

func TestTheGeneratedReadmeNamesTheChosenBand(t *testing.T) {
	out, _ := generateWithBand(t, "--stacks", "go-srv,ng-web", "--port-band", "103")
	readme := readProjectFile(t, out, "README.md")
	for _, want := range []string{"10300-10399", "http://localhost:10320"} {
		if !strings.Contains(readme, want) {
			t.Errorf("README lacks %q", want)
		}
	}
}
