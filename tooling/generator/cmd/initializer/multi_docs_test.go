package main

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	miseTaskPattern       = regexp.MustCompile(`(?m)^\[tasks\.(?:"([^"]+)"|([A-Za-z0-9_-]+))\]`)
	miseRunPattern        = regexp.MustCompile(`mise run ([A-Za-z0-9:._-]+)`)
	kratosPublicURLEnvVar = regexp.MustCompile(`SERVE_PUBLIC_BASE_URL: \$\{([A-Z][A-Z0-9_]*):-([^}]*)\}`)
)

// generateMultiAcme generates a project of two go-srv backends (the second
// with every module) and one console bound to the second.
func generateMultiAcme(t *testing.T) string {
	t.Helper()
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, out,
		"--name", "acme", "--go-module", "example.com/acme", "--stacks", "go-srv,ng-web",
		"--component", "go-srv=acme-srv-first,modules=identity",
		"--component", "go-srv=acme-srv-second,modules=api-keys+feature-flags+identity+parameters+tenancy",
		"--component", "ng-web=acme-web,backend=acme-srv-second")
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	return out
}

func TestEveryMiseTaskTheDocsOfAMultiProjectNameIsDefinedInItsMiseToml(t *testing.T) {
	out := generateMultiAcme(t)
	tasks := map[string]bool{}
	for _, match := range miseTaskPattern.FindAllStringSubmatch(readProjectFile(t, out, "mise.toml"), -1) {
		tasks[match[1]+match[2]] = true
	}
	var defined []string
	for task := range tasks {
		defined = append(defined, task)
	}
	sort.Strings(defined)
	docs := []string{
		"README.md",
		".deploy/local/README.md",
		".deploy/local/acme-srv-first/.example.env",
		".deploy/local/acme-srv-second/.example.env",
	}
	named := 0
	for _, doc := range docs {
		for _, match := range miseRunPattern.FindAllStringSubmatch(readProjectFile(t, out, doc), -1) {
			named++
			if !tasks[match[1]] {
				t.Errorf("%s tells the user to run `mise run %s`; mise.toml defines only %v", doc, match[1], defined)
			}
		}
	}
	if named == 0 {
		t.Fatal("the docs name no mise task")
	}
}

func TestTheDocsOfAMultiProjectSendOverridesToThePerComponentFile(t *testing.T) {
	out := generateMultiAcme(t)
	deployReadme := readProjectFile(t, out, ".deploy/local/README.md")
	for _, component := range []string{"acme-srv-first", "acme-srv-second"} {
		own := ".deploy/local/" + component + "/.env"
		var overrideLines []string
		for _, line := range strings.Split(deployReadme, "\n") {
			if strings.Contains(line, "Overrides go in `"+own+"`") {
				overrideLines = append(overrideLines, line)
			}
		}
		if len(overrideLines) != 1 {
			t.Fatalf(".deploy/local/README.md sends overrides of %s to its own file %d times, want 1", component, len(overrideLines))
		}
		// Compose interpolates `${...}` from .deploy/local/.env only; the per-component file reaches the container through env_file.
		if strings.Contains(overrideLines[0], "compose reads it for its `${...}` values") {
			t.Errorf(".deploy/local/README.md says compose reads %s for its ${...} values:\n%s", own, overrideLines[0])
		}
		header := strings.SplitN(readProjectFile(t, out, ".deploy/local/"+component+"/.example.env"), "\n", 2)[0]
		if !strings.Contains(header, "Overrides go in "+own) {
			t.Errorf(".deploy/local/%s/.example.env sends overrides elsewhere than %s:\n%s", component, own, header)
		}
	}
}

func TestEachKratosOfAMultiProjectTakesItsPublicOriginFromAVariableOfItsOwn(t *testing.T) {
	out := generateMultiAcme(t)
	compose := readProjectFile(t, out, ".deploy/local/docker-compose.yml")
	matches := kratosPublicURLEnvVar.FindAllStringSubmatch(compose, -1)
	want := map[string]string{
		"APP_PUBLIC_BASE_URL_ACME_SRV_FIRST":  "http://localhost:10100",
		"APP_PUBLIC_BASE_URL_ACME_SRV_SECOND": "http://localhost:10120",
	}
	if len(matches) != len(want) {
		t.Fatalf("the compose file has %d Kratos public origins, want %d", len(matches), len(want))
	}
	for _, match := range matches {
		variable, fallback := match[1], match[2]
		if want[variable] != fallback {
			t.Errorf("SERVE_PUBLIC_BASE_URL reads ${%s:-%s}, want one of %v", variable, fallback, want)
		}
		delete(want, variable)
	}
	if strings.Contains(compose, "${APP_PUBLIC_BASE_URL:-") {
		t.Error("an instance's compose value still reads the one variable every backend shares")
	}
	readme := readProjectFile(t, out, "README.md")
	if !strings.Contains(readme, "`APP_PUBLIC_BASE_URL_<COMPONENT>` in `.deploy/local/.env`") {
		t.Errorf("README.md does not say where an instance's Kratos origin is set:\n%s", readme)
	}
}

func TestAOneComponentProjectKeepsTheSingleInstanceDocsAndVariables(t *testing.T) {
	out := generateAcme(t, "go-srv,ng-web")
	deployReadme := readProjectFile(t, out, ".deploy/local/README.md")
	for _, want := range []string{
		"Overrides go in `.deploy/local/.env`", "compose reads it for its `${...}` values",
		"mise run dev:srv\n", "`mise run dev:srv` and `mise run dev:web`",
	} {
		if !strings.Contains(deployReadme, want) {
			t.Errorf(".deploy/local/README.md lacks %q", want)
		}
	}
	header := strings.SplitN(readProjectFile(t, out, ".deploy/local/.example.env"), "\n", 2)[0]
	if !strings.Contains(header, "Overrides go in .deploy/local/.env ") {
		t.Errorf(".deploy/local/.example.env header: %s", header)
	}
	compose := readProjectFile(t, out, ".deploy/local/docker-compose.yml")
	if !strings.Contains(compose, "SERVE_PUBLIC_BASE_URL: ${APP_PUBLIC_BASE_URL:-http://localhost:10120}/kratos/") {
		t.Error("the single instance's Kratos lost the shared APP_PUBLIC_BASE_URL variable")
	}
}
