package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// generateAcme generates the acme project from the real checkout with the
// given stacks and every module, and returns its folder.
func generateAcme(t *testing.T, stacks string) string {
	t.Helper()
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, "--out", out,
		"--name", "acme", "--go-module", "example.com/acme", "--stacks", stacks, "--all-modules")
	if code != exitOK {
		t.Fatalf("generate %s exit %d: %s", stacks, code, stderr)
	}
	return out
}

func readProjectFile(t *testing.T, dir, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The deploy README and mise.toml of a project name its components, not the
// reference folders.
func TestGenerateNamesTheComponentsInMiseAndTheDeployReadme(t *testing.T) {
	out := generateAcme(t, "go-srv,ng-web")
	mise := readProjectFile(t, out, "mise.toml")
	for _, want := range []string{
		"dir = \"{{config_root}}/acme-srv\"\n", "dir = \"{{config_root}}/acme-web\"\n",
		"[tasks.up]\n", "[tasks.infra]\n", "[tasks.down]\n", "[tasks.\"dev:srv\"]\n", "[tasks.\"dev:web\"]\n", "[tasks.tck]\n",
	} {
		if !strings.Contains(mise, want) {
			t.Errorf("mise.toml lacks %q", want)
		}
	}
	deploy := readProjectFile(t, out, ".deploy/local/README.md")
	for _, want := range []string{
		"| 10100 | `acme-srv` HTTP API (`GO_SRV_ADDR`) | Docker or host |\n",
		"| 10120 | `acme-web` console, the public origin: nginx in Docker, or the dev server on the host | Docker or host |\n",
		"| 10132 | `acme-postgres` | Docker |\n",
		"- acme-srv applies its own migrations at startup.\n",
		"- Both modes run acme-srv from `acme-srv/` (the container's working directory is `/project/acme-srv`), so relative path values resolve against `acme-srv/`.\n",
		"yml stop acme-srv\nmise run dev:srv\n",
		"stop `acme-srv acme-web`, then run `mise run dev:srv` and `mise run dev:web`",
		"stop `acme-web`, then `mise run dev:web`", "`npm --prefix acme-web ci`",
		"(`../flags.yaml` from `acme-srv/`, set by `.example.env`)",
	} {
		if !strings.Contains(deploy, want) {
			t.Errorf("the deploy README lacks %q", want)
		}
	}
	// The Kratos proxy sentence keeps the reference name until the brand rules land.
	for number, line := range strings.Split(deploy, "\n") {
		isMarker := strings.HasPrefix(line, "<!-- initializer:")
		keepsName := !isMarker && (strings.Contains(line, "go-srv") || strings.Contains(line, "ng-web"))
		isProxySentence := strings.HasPrefix(line, "Identity adds Kratos; `go-srv` on 10100")
		if keepsName && !isProxySentence {
			t.Errorf("deploy README line %d keeps a reference component name: %q", number+1, line)
		}
	}
	example := readProjectFile(t, out, ".deploy/local/.example.env")
	if !strings.Contains(example, "# Overriding ACME_PG_PASSWORD in .env needs APP_DATABASE_URL overridden there too for host-dev.\n") {
		t.Errorf(".example.env header lacks the project password key:\n%s", example)
	}
}

// A backend-only project has no console service, task, row or paragraph.
func TestBackendOnlyProjectShipsNoConsole(t *testing.T) {
	out := generateAcme(t, "go-srv")
	compose := readProjectFile(t, out, ".deploy/local/docker-compose.yml")
	for _, banned := range []string{"acme-web", "ng-web", "GO_SRV_HOST", "      acme-srv:\n"} {
		if strings.Contains(compose, banned) {
			t.Errorf("compose of a backend-only project holds %q:\n%s", banned, compose)
		}
	}
	for _, want := range []string{"\n  acme-srv:\n", "profiles: [app]"} {
		if !strings.Contains(compose, want) {
			t.Errorf("compose of a backend-only project lacks %q", want)
		}
	}
	mise := readProjectFile(t, out, "mise.toml")
	if strings.Contains(mise, "dev:web") || !strings.Contains(mise, "[tasks.\"dev:srv\"]") {
		t.Errorf("mise.toml of a backend-only project:\n%s", mise)
	}
	deploy := readProjectFile(t, out, ".deploy/local/README.md")
	for _, banned := range []string{"console", "dev:web", "10120", "ng-web", "acme-web"} {
		if strings.Contains(deploy, banned) {
			t.Errorf("deploy README of a backend-only project holds %q", banned)
		}
	}
	project := readProjectFile(t, out, "README.md")
	for _, banned := range []string{"console", "dev:web", "acme-web", "npm --prefix acme-web"} {
		if strings.Contains(project, banned) {
			t.Errorf("README of a backend-only project holds %q", banned)
		}
	}
	if !strings.Contains(project, "The API opens on http://localhost:10100.") {
		t.Errorf("README of a backend-only project lacks the API origin:\n%s", project)
	}
	if _, err := os.Stat(filepath.Join(out, "acme-web")); err == nil {
		t.Error("a backend-only project ships a console folder")
	}
}

// The delivered nginx template keeps the upstream and the locations only with a backend.
func TestWebProjectNginxTemplateIsDeliveredWithItsBackendBlocks(t *testing.T) {
	out := generateAcme(t, "go-srv,ng-web")
	nginx := readProjectFile(t, out, "acme-web/nginx.conf.template")
	for _, want := range []string{"upstream go-srv {", "location /api/ {", "location /health/ {", "location /kratos/ {"} {
		if !strings.Contains(nginx, want) {
			t.Errorf("nginx.conf.template lacks %q", want)
		}
	}
}

// composeEnvironment is the whole environment of a docker compose run: the
// variables a CLI needs to start, never one that a project value could come
// from (a password key, APP_*, GO_SRV_*, COMPOSE_*).
func composeEnvironment() []string {
	keep := []string{"PATH", "HOME", "USERPROFILE", "SystemRoot", "TEMP", "TMP", "APPDATA", "LOCALAPPDATA", "ProgramData", "ProgramFiles"}
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		isKept := strings.HasPrefix(strings.ToUpper(key), "DOCKER_")
		for _, name := range keep {
			isKept = isKept || strings.EqualFold(key, name)
		}
		if isKept {
			env = append(env, entry)
		}
	}
	return env
}

// runCompose runs docker compose with the minimal environment and returns its combined output.
func runCompose(dir string, args ...string) (string, error) {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = dir
	cmd.Env = composeEnvironment()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Every CI selection yields a compose file that docker compose accepts with
// and without the app profile and with no .env file: the project boots with
// no copy step.
func TestGeneratedComposeConfig(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found")
	}
	if out, err := runCompose(t.TempDir(), "version"); err != nil {
		t.Skipf("docker compose is not available: %v: %s", err, out)
	}
	root, catalogPath := realCatalog(t)
	selections, err := filepath.Glob(filepath.Join(root, "tooling", "generator", "testdata", "ci", "*.json"))
	if err != nil || len(selections) == 0 {
		t.Fatalf("no CI selections: %v", err)
	}
	for _, selection := range selections {
		name := strings.TrimSuffix(filepath.Base(selection), ".json")
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, "--out", out, "--selection", selection)
			if code != exitOK {
				t.Fatalf("generate exit %d: %s", code, stderr)
			}
			if _, err := os.Stat(filepath.Join(out, ".deploy", "local", ".env")); err == nil {
				t.Fatal("the generated project ships a .env file")
			}
			compose := filepath.Join(out, ".deploy", "local", "docker-compose.yml")
			for _, args := range [][]string{
				{"-f", compose, "config", "-q"},
				{"-f", compose, "--profile", "app", "config", "-q"},
			} {
				if output, err := runCompose(out, args...); err != nil {
					t.Errorf("docker compose %s: %v\n%s", strings.Join(args, " "), err, output)
				}
			}
		})
	}
}

var (
	composeDefaultPattern = regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]*_PG_PASSWORD):-([^}]*)\}`)
	exampleEnvKeyPattern  = regexp.MustCompile(`(?m)^([A-Z][A-Z0-9_]*_PG_PASSWORD)=(.*)$`)
)

// checkComposeDefaultsMatchExampleEnv fails unless every password default of
// the compose file equals its .example.env line and every password key of
// .example.env has a compose default.
func checkComposeDefaultsMatchExampleEnv(t *testing.T, dir string) {
	t.Helper()
	compose := readProjectFile(t, dir, ".deploy/local/docker-compose.yml")
	example := readProjectFile(t, dir, ".deploy/local/.example.env")
	defaults := map[string]string{}
	for _, match := range composeDefaultPattern.FindAllStringSubmatch(compose, -1) {
		if previous, seen := defaults[match[1]]; seen && previous != match[2] {
			t.Errorf("compose gives %s two defaults: %q and %q", match[1], previous, match[2])
		}
		defaults[match[1]] = match[2]
	}
	keys := map[string]string{}
	for _, match := range exampleEnvKeyPattern.FindAllStringSubmatch(example, -1) {
		keys[match[1]] = match[2]
	}
	if len(defaults) == 0 || len(keys) == 0 {
		t.Fatalf("found %d compose defaults and %d .example.env keys", len(defaults), len(keys))
	}
	for key, value := range defaults {
		got, found := keys[key]
		if !found || got != value {
			t.Errorf("compose default %s=%q, .example.env has %q (found %v)", key, value, got, found)
		}
	}
	for key := range keys {
		if _, found := defaults[key]; !found {
			t.Errorf(".example.env sets %s and compose has no default for it", key)
		}
	}
}

// Docker mode and host-dev read the same local-only passwords.
func TestComposeDefaultsMatchExampleEnv(t *testing.T) {
	root, _ := realCatalog(t)
	t.Run("repo", func(t *testing.T) {
		checkComposeDefaultsMatchExampleEnv(t, root)
	})
	t.Run("generated project", func(t *testing.T) {
		checkComposeDefaultsMatchExampleEnv(t, generateAcme(t, "go-srv,ng-web"))
	})
}
