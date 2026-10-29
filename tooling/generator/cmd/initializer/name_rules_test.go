package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
)

// repoGoModule returns the module path on line 1 of go-srv/go.mod.
func repoGoModule(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "go-srv", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	line, _, _ := strings.Cut(string(data), "\n")
	module, found := strings.CutPrefix(line, "module ")
	if !found {
		t.Fatalf("go.mod line 1 is %q", line)
	}
	return module
}

func TestGenerateRenamesTheComposeProjectAndInfrastructure(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, "--out", out,
		"--name", "acme", "--go-module", "example.com/acme", "--stacks", "go-srv,ng-web", "--all-modules")
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	delivered, err := os.ReadFile(filepath.Join(out, ".deploy", "local", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(delivered)
	for _, want := range []string{
		"name: acme\n", "\n  acme-postgres:\n", "\n  acme-postgres-init:\n", "\n  acme-kratos-migrate:\n", "\n  acme-kratos:\n",
		"POSTGRES_DB: acme\n", "POSTGRES_USER: acme\n", "${ACME_PG_PASSWORD:-change-me-local-only}", "@acme-postgres:5432/kratos",
		"\n  acme-srv:\n", "\n  acme-web:\n", "build: ../../acme-srv\n", "build: ../../acme-web\n",
		"working_dir: /project/acme-srv\n", "GO_SRV_HOST: acme-srv:10100\n", "\n  acme-postgres-data:\n", "- acme-postgres-data:/var/lib/postgresql\n",
		"APP_DATABASE_URL: postgres://acme:${ACME_PG_PASSWORD:-change-me-local-only}@acme-postgres:5432/acme?sslmode=disable\n",
		"APP_KRATOS_PUBLIC_URL: http://acme-kratos:4433\n", "APP_KRATOS_ADMIN_URL: http://acme-kratos:4434\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the compose file lacks %q", want)
		}
	}
	for _, banned := range []string{"INITIALIZER", "initializer"} {
		for _, line := range strings.Split(text, "\n") {
			isMarker := strings.HasPrefix(strings.TrimSpace(line), "# initializer:")
			if strings.Contains(line, banned) && !isMarker {
				t.Errorf("the compose file keeps the reference name on %q", line)
			}
		}
	}
	source, err := os.ReadFile(filepath.Join(root, ".deploy", "local", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	sourceLines := strings.Split(string(source), "\n")
	for index, line := range sourceLines {
		isMarker := strings.HasPrefix(strings.TrimSpace(line), "# initializer:")
		if !isMarker {
			sourceLines[index] = strings.NewReplacer("INITIALIZER", "ACME", "initializer", "acme").Replace(line)
		}
	}
	// The repo copy names its services bare; the delivered copy prefixes them with the project.
	unprefixed := strings.NewReplacer("acme-postgres", "postgres", "acme-kratos", "kratos", "acme-srv", "go-srv", "acme-web", "ng-web").Replace(text)
	if unprefixed != strings.Join(sourceLines, "\n") {
		t.Error("the compose file differs from the repo copy beyond the renamed reference names")
	}
	// The Kratos role and database and the init folder keep their fixed names.
	for _, kept := range []string{"postgres://kratos:", "./postgres-init:/postgres-init:ro", "./kratos:/etc/config/kratos:ro"} {
		if !strings.Contains(text, kept) {
			t.Errorf("the compose file lost %q", kept)
		}
	}
}

// excludedFromRoundtrip lists the delivered files that differ from the repo by design.
var excludedFromRoundtrip = map[string]bool{
	"README.md": true, "initializer.selection.json": true, "initializer.lock.json": true, "flags.yaml": true,
}

func TestGenerateFromTheReferenceFileReproducesTheRepo(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, "--out", out,
		"-f", filepath.Join(root, ".initializer", "reference.json"), "--all-modules", "--go-module", repoGoModule(t, root))
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	compared := 0
	err := filepath.WalkDir(out, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(out, path)
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(relative)
		if excludedFromRoundtrip[slash] {
			return nil
		}
		delivered, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		source, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Errorf("%s has no repo copy: %v", slash, err)
			return nil
		}
		if !bytes.Equal(delivered, source) {
			t.Errorf("%s differs from the repo copy", slash)
		}
		compared++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if compared == 0 {
		t.Fatal("no delivered file was compared")
	}
}

func TestReferenceFileIsCanonical(t *testing.T) {
	root := realCheckoutRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".initializer", "reference.json"))
	if err != nil {
		t.Fatal(err)
	}
	selection, issues := manifest.DecodeReference(data)
	if len(issues) > 0 {
		t.Fatalf("DecodeReference: %+v", issues)
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(selection); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buffer.Bytes(), data) {
		t.Fatalf("reference.json is not canonical; want:\n%s", buffer.Bytes())
	}
}

// Rule texts use %{ as a delimiter, so no catalogued source may contain it,
// except the manifests that declare the rules.
func TestNoSourceFileHoldsThePlaceholderDelimiter(t *testing.T) {
	root := realCheckoutRoot(t)
	listed, err := exec.Command("git", "-C", root, "ls-files", "-z",
		"go-srv", "ng-web", ".deploy", "specs", "mise.toml", ".gitignore", ".initializer").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	for _, path := range strings.Split(strings.TrimSuffix(string(listed), "\x00"), "\x00") {
		if path == "" || filepath.Base(path) == "module.manifest.json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("%{")) {
			t.Errorf("%s contains the rule delimiter %%{", path)
		}
	}
}
