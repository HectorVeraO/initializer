package catalog

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	generator "github.com/HectorVeraO/initializer/tooling/generator"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/ports"
)

const portsCoreBlock = `"tablePrefixes": ["cor"],
  "ports": [{"name": "srv", "offset": 0, "origin": true}, {"name": "postgres", "offset": 32}],
  "portFiles": ["` + composeFile + `"],`

const portsIdentityBlock = `"tablePrefixes": ["iam"],
  "ports": [{"name": "kratos-public", "offset": 33}],
  "portFiles": [".deploy/local/kratos/kratos.yml"],`

// portsRepo returns the clean repo with ports declared by core and identity,
// a reference band of 101 and tokens of every kind in the compose file.
func portsRepo(t *testing.T) fstest.MapFS {
	t.Helper()
	repo := cleanRepo(t)
	edit(t, repo, coreManifest, `"tablePrefixes": ["cor"],`, portsCoreBlock)
	edit(t, repo, identityManifest, `"tablePrefixes": ["iam"],`, portsIdentityBlock)
	edit(t, repo, referenceFile, `"schema": 2,`, `"schema": 2, "ports": {"band": 101},`)
	put(repo, composeFile, "name: fixture-app\n\nservices: {}\n# 10100-10199 api 10100, db 10132, kratos 10133\n")
	put(repo, ".deploy/local/kratos/kratos.yml", "public: http://localhost:10133\n")
	return repo
}

func TestPortsAreRecordedInTheCatalog(t *testing.T) {
	built, issues := buildRepo(t, portsRepo(t))
	assertCodes(t, issues)
	if built.PortBand != 101 {
		t.Errorf("portBand = %d, want 101", built.PortBand)
	}
	core := findModule(t, built, "go-srv", "core")
	wantPorts := []ports.Port{{Name: "srv", Offset: 0, Origin: true}, {Name: "postgres", Offset: 32}}
	if !reflect.DeepEqual(core.Ports, wantPorts) || !reflect.DeepEqual(core.PortFiles, []string{composeFile}) {
		t.Errorf("core ports %+v files %v", core.Ports, core.PortFiles)
	}
	identity := findModule(t, built, "go-srv", "identity")
	if len(identity.Ports) != 1 || identity.Ports[0].Offset != 33 {
		t.Errorf("identity ports %+v", identity.Ports)
	}
}

func TestPortFilesAreRecordedSorted(t *testing.T) {
	repo := portsRepo(t)
	put(repo, ".deploy/local/README.md", "10100\n")
	edit(t, repo, coreManifest, `"portFiles": ["`+composeFile+`"]`, `"portFiles": ["`+composeFile+`", ".deploy/local/README.md"]`)
	built, issues := buildRepo(t, repo)
	assertCodes(t, issues)
	got := findModule(t, built, "go-srv", "core").PortFiles
	if !reflect.DeepEqual(got, []string{".deploy/local/README.md", composeFile}) {
		t.Fatalf("portFiles = %v", got)
	}
}

func TestPortCatalogRoundTripsThroughEncodeAndDecode(t *testing.T) {
	built, issues := buildRepo(t, portsRepo(t))
	assertCodes(t, issues)
	encoded, err := Encode(built)
	if err != nil {
		t.Fatal(err)
	}
	decoded, decodeIssues := Decode(encoded)
	if len(decodeIssues) > 0 {
		t.Fatalf("decode: %v", decodeIssues)
	}
	if decoded.PortBand != 101 || len(findModule(t, decoded, "go-srv", "core").Ports) != 2 {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestPortIssueCodes(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, repo fstest.MapFS)
		want   []string
	}{
		{"name breaks the label grammar", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, coreManifest, `"name": "postgres"`, `"name": "Post_gres"`)
		}, []string{"PORT_INVALID"}},
		{"name repeats across modules", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, identityManifest, `"name": "kratos-public"`, `"name": "srv"`)
		}, []string{"PORT_INVALID"}},
		{"offset is 99", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, coreManifest, `"offset": 32`, `"offset": 99`)
		}, []string{"PORT_INVALID", "PORT_UNDECLARED"}},
		{"offset is negative", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, coreManifest, `"offset": 32`, `"offset": -1`)
		}, []string{"PORT_INVALID", "PORT_UNDECLARED"}},
		{"two origins in one stack", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, coreManifest, `{"name": "postgres", "offset": 32}`, `{"name": "postgres", "offset": 32, "origin": true}`)
		}, []string{"PORT_INVALID"}},
		{"origin outside the core module", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, identityManifest, `{"name": "kratos-public", "offset": 33}`, `{"name": "kratos-public", "offset": 33, "origin": true}`)
		}, []string{"PORT_INVALID"}},
		{"two ports share an offset", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, identityManifest, `"offset": 33`, `"offset": 32`)
		}, []string{"PORT_OFFSET_DUPLICATE", "PORT_UNDECLARED"}},
		{"port file is not tracked", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, coreManifest, `"portFiles": ["`+composeFile+`"]`, `"portFiles": ["`+composeFile+`", ".deploy/local/missing.yml"]`)
		}, []string{"PORT_FILE_UNOWNED"}},
		{"port file belongs to another module", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, identityManifest, `"portFiles": [".deploy/local/kratos/kratos.yml"]`, `"portFiles": ["`+composeFile+`"]`)
		}, []string{"PORT_FILE_UNOWNED"}},
		{"port file is excluded from the copy", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, coreManifest, `"exclude": [`, `"exclude": ["`+composeFile+`", `)
		}, []string{"PORT_FILE_UNOWNED", "NAME_RULE_FOREIGN"}},
		{"token of another band", func(t *testing.T, r fstest.MapFS) {
			put(r, composeFile, "name: fixture-app\n10300\n")
		}, []string{"PORT_BAND_MIXED"}},
		{"token with an undeclared offset", func(t *testing.T, r fstest.MapFS) {
			put(r, composeFile, "name: fixture-app\n10177\n")
		}, []string{"PORT_UNDECLARED"}},
		{"token offset declared by no module", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, identityManifest, `"ports": [{"name": "kratos-public", "offset": 33}],`, ``)
		}, []string{"PORT_UNDECLARED"}},
		{"reference has no band", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, referenceFile, `"ports": {"band": 101},`, ``)
		}, []string{"REFERENCE_INCOMPLETE"}},
		{"reference file is missing", func(t *testing.T, r fstest.MapFS) {
			delete(r, referenceFile)
			setRules(t, r)
		}, []string{"REFERENCE_INVALID"}},
		{"reference band is out of range", func(t *testing.T, r fstest.MapFS) {
			edit(t, r, referenceFile, `"band": 101`, `"band": 99`)
		}, []string{"REFERENCE_INVALID"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := portsRepo(t)
			tc.mutate(t, repo)
			_, issues := buildRepo(t, repo)
			assertCodes(t, issues, tc.want...)
		})
	}
}

func TestBandBoundsAlwaysPass(t *testing.T) {
	repo := portsRepo(t)
	put(repo, composeFile, "name: fixture-app\n10100 10199\n10100-10199\n")
	put(repo, ".deploy/local/kratos/kratos.yml", "10100 10199\n")
	edit(t, repo, identityManifest, `"ports": [{"name": "kratos-public", "offset": 33}],`, ``)
	_, issues := buildRepo(t, repo)
	assertCodes(t, issues)
}

func TestNeighborDigitsMakeNoToken(t *testing.T) {
	repo := portsRepo(t)
	put(repo, composeFile, "name: fixture-app\n1017700 9910177 x1010099\n")
	_, issues := buildRepo(t, repo)
	assertCodes(t, issues)
}

func TestPortIssuesNameTheLineAndAreCollectedTogether(t *testing.T) {
	repo := portsRepo(t)
	put(repo, composeFile, "name: fixture-app\n\n10300 10177\n")
	edit(t, repo, coreManifest, `"offset": 32`, `"offset": 99`)
	_, issues := buildRepo(t, repo)
	assertCodes(t, issues, "PORT_BAND_MIXED", "PORT_UNDECLARED", "PORT_INVALID")
	for _, found := range issues {
		if found.Code == "PORT_BAND_MIXED" && !strings.Contains(found.Message, "line 3") {
			t.Errorf("message %q does not name the line", found.Message)
		}
	}
}

func TestACatalogWithoutPortsRecordsNoBand(t *testing.T) {
	repo := cleanRepo(t)
	built, issues := buildRepo(t, repo)
	assertCodes(t, issues)
	if built.PortBand != 0 {
		t.Fatalf("portBand = %d, want 0", built.PortBand)
	}
}

func TestDecodeRefusesBadPortRecords(t *testing.T) {
	built, issues := buildRepo(t, portsRepo(t))
	assertCodes(t, issues)
	type refusal struct {
		mutate   func(c *Catalog)
		wantPath string
	}
	cases := map[string]refusal{
		"band out of range":     {func(c *Catalog) { c.PortBand = 7 }, "#/portBand"},
		"ports without a band":  {func(c *Catalog) { c.PortBand = 0 }, "#/portBand"},
		"offset out of range":   {func(c *Catalog) { c.Stacks[0].Modules[0].Ports[0].Offset = 99 }, "/ports/0/offset"},
		"name is not a label":   {func(c *Catalog) { c.Stacks[0].Modules[0].Ports[0].Name = "Srv" }, "/ports/0/name"},
		"port files unsorted":   {func(c *Catalog) { c.Stacks[0].Modules[0].PortFiles = []string{"b", "a"} }, "/portFiles/1"},
		"port file not a path":  {func(c *Catalog) { c.Stacks[0].Modules[0].PortFiles = []string{"../a"} }, "/portFiles/0"},
		"port files duplicated": {func(c *Catalog) { c.Stacks[0].Modules[0].PortFiles = []string{"a", "a"} }, "/portFiles/1"},
	}
	for name, refusal := range cases {
		mutate := refusal.mutate
		wantPath := refusal.wantPath
		t.Run(name, func(t *testing.T) {
			copied := built
			copied.Stacks = append([]StackEntry(nil), built.Stacks...)
			copied.Stacks[0].Modules = append([]ModuleEntry(nil), built.Stacks[0].Modules...)
			module := &copied.Stacks[0].Modules[0]
			module.Ports = append([]ports.Port(nil), module.Ports...)
			module.PortFiles = append([]string(nil), module.PortFiles...)
			mutate(&copied)
			encoded, err := Encode(copied)
			if err != nil {
				t.Fatal(err)
			}
			_, decodeIssues := Decode(encoded)
			if len(decodeIssues) != 1 || decodeIssues[0].Code != "CATALOG_INVALID" || !strings.HasSuffix(decodeIssues[0].Path, wantPath) {
				t.Fatalf("want one CATALOG_INVALID at ...%s, got %v", wantPath, decodeIssues)
			}
		})
	}
}

// The repository passes its own port checks: every token of a declared port
// file has the reference band and a declared offset.
func TestRepoCatalogPorts(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the test file")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
	listed, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	files, err := ParseFileList(listed)
	if err != nil {
		t.Fatal(err)
	}
	built, issues, err := Build(BuildOptions{Root: os.DirFS(root), Files: files, Generator: generator.Version})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, found := range issues {
		if strings.HasPrefix(found.Code, "PORT_") || strings.HasPrefix(found.Code, "REFERENCE_") {
			t.Errorf("%s at %s: %s", found.Code, found.Path, found.Message)
		}
	}
	if built.PortBand != 101 {
		t.Errorf("portBand = %d, want 101", built.PortBand)
	}
}
