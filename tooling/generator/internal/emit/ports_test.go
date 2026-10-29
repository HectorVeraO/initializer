package emit_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/catalog"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

const (
	exampleEnv = ".deploy/local/.example.env"
	proxyConf  = "acme-backend-web/proxy.conf.json"
)

func withBand(sel manifest.Selection, band int) manifest.Selection {
	sel.Ports = &manifest.Ports{Band: band}
	sel.Schema = 2
	return sel
}

func TestAnotherBandMovesTheTokensOfEveryDeclaredPortFile(t *testing.T) {
	repo := fixture.Load(t)
	project := mustBuild(t, repo, withBand(webSelection("tenancy"), 103))
	env := projectFile(t, project, exampleEnv)
	for _, want := range []string{"localhost:10332/app", "APP_PUBLIC_BASE_URL=http://localhost:10320"} {
		if !strings.Contains(env, want) {
			t.Errorf(".example.env lacks %q:\n%s", want, env)
		}
	}
	proxy := projectFile(t, project, proxyConf)
	if strings.Contains(proxy, "10100") || strings.Count(proxy, "10300") != 3 {
		t.Errorf("proxy.conf.json:\n%s", proxy)
	}
	readme := projectFile(t, project, ".deploy/local/README.md")
	if !strings.Contains(readme, "| 10300 | api |") {
		t.Errorf("deploy README:\n%s", readme)
	}
	if lock := decodeLock(t, project); lock.Ports == nil || lock.Ports.Band != 103 {
		t.Errorf("lock ports = %+v", lock.Ports)
	}
}

func TestIdentityPortsOfAnotherBandMoveInsideAnIdentityBlock(t *testing.T) {
	repo := fixture.Load(t)
	project := mustBuild(t, repo, withBand(webSelection("tenancy"), 110))
	readme := projectFile(t, project, ".deploy/local/README.md")
	if !strings.Contains(readme, "Identity adds Kratos on 11033.") {
		t.Errorf("deploy README:\n%s", readme)
	}
}

func TestDeliveredHashesAreOfTheSubstitutedBytes(t *testing.T) {
	repo := fixture.Load(t)
	project := mustBuild(t, repo, withBand(webSelection("tenancy"), 103))
	lock := decodeLock(t, project)
	found := false
	for _, module := range lock.Modules {
		for _, file := range module.Files {
			if file.Path != exampleEnv {
				continue
			}
			found = true
			if file.SHA256 != hashOf([]byte(projectFile(t, project, exampleEnv))) || file.SourceSHA256 == "" || file.SHA256 == file.SourceSHA256 {
				t.Errorf("entry = %+v", file)
			}
		}
	}
	if !found {
		t.Fatal("the lock does not list .example.env")
	}
}

func TestBackendOnlyProjectUsesTheBandBaseAsOrigin(t *testing.T) {
	repo := fixture.Load(t)
	for _, band := range []int{101, 103} {
		sel := withBand(selection(targetModule, "tenancy"), band)
		project := mustBuild(t, repo, sel)
		env := projectFile(t, project, exampleEnv)
		want := "APP_PUBLIC_BASE_URL=http://localhost:" + strconv.Itoa(band*100)
		if !strings.Contains(env, want) || strings.Contains(env, strconv.Itoa(band*100+20)) {
			t.Errorf("band %d: .example.env:\n%s", band, env)
		}
	}
}

func TestTheReferenceBandKeepsEveryToken(t *testing.T) {
	repo := fixture.Load(t)
	project := mustBuild(t, repo, webSelection("tenancy"))
	env := projectFile(t, project, exampleEnv)
	for _, want := range []string{"localhost:10132/app", "APP_PUBLIC_BASE_URL=http://localhost:10120"} {
		if !strings.Contains(env, want) {
			t.Errorf(".example.env lacks %q:\n%s", want, env)
		}
	}
	if proxy := projectFile(t, project, proxyConf); strings.Count(proxy, "10100") != 3 {
		t.Errorf("proxy.conf.json:\n%s", proxy)
	}
	if lock := decodeLock(t, project); lock.Ports == nil || lock.Ports.Band != 101 {
		t.Errorf("lock ports = %+v", lock.Ports)
	}
}

func TestFilesOutsideThePortFilesKeepTheirTokens(t *testing.T) {
	repo := fixture.Load(t)
	root := t.TempDir()
	copyTree(t, repo.Root, root)
	const extra = "go-srv/internal/platform/core/ports.txt"
	data := []byte("dial 10120 and 10132\n")
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(extra)), data, 0o644); err != nil {
		t.Fatal(err)
	}
	crafted := repo.Catalog
	crafted.Stacks = append([]catalog.StackEntry(nil), crafted.Stacks...)
	for i := range crafted.Stacks {
		if crafted.Stacks[i].Stack != "go-srv" {
			continue
		}
		crafted.Stacks[i].Modules = append([]catalog.ModuleEntry(nil), crafted.Stacks[i].Modules...)
		for j := range crafted.Stacks[i].Modules {
			module := &crafted.Stacks[i].Modules[j]
			if module.Module != "core" {
				continue
			}
			files := append([]catalog.File(nil), module.Files...)
			files = append(files, catalog.File{Path: extra, SHA256: hashOf(data)})
			module.Files = files
		}
	}
	encoded, err := catalog.Encode(crafted)
	if err != nil {
		t.Fatal(err)
	}
	project, err := build(t, os.DirFS(root), crafted, encoded, withBand(selection(targetModule, "tenancy"), 103))
	if err != nil {
		t.Fatal(err)
	}
	if got := projectFile(t, project, "acme-backend-srv/internal/platform/core/ports.txt"); got != string(data) {
		t.Errorf("an undeclared file changed: %q", got)
	}
}
