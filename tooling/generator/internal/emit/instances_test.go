package emit_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/emit"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/fixture"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/manifest"
	"github.com/HectorVeraO/initializer/tooling/generator/internal/verify"
)

const (
	deployLocal = ".deploy/local/"
	composeYML  = deployLocal + "docker-compose.yml"
)

// instancesSelection selects two backends of the checkout: index 0 installs
// identity only, index 1 installs every module, and one console is bound to
// index 1.
func instancesSelection() manifest.Selection {
	return manifest.Selection{
		Schema:  2,
		Project: manifest.Project{Name: "acme", GoModule: "example.com/acme"},
		Stacks:  []string{"go-srv", "ng-web"},
		Components: []manifest.Component{
			backend("acme-srv-first", "identity"),
			backend("acme-srv-second", "api-keys", "feature-flags", "identity", "parameters", "tenancy"),
			{Stack: "ng-web", Name: "acme-web", Backend: "acme-srv-second"},
		},
	}
}

// instancesProject generates the two-backend project from the checkout.
func instancesProject(t *testing.T) (fixture.Repo, emit.Project) {
	t.Helper()
	repo := fixture.LoadCheckout(t)
	return repo, mustBuild(t, repo, instancesSelection())
}

func TestPerComponentFilesLandInTheFolderOfTheirComponent(t *testing.T) {
	_, project := instancesProject(t)
	paths := projectPaths(project)
	for _, want := range []string{
		deployLocal + "acme-srv-first/.example.env", deployLocal + "acme-srv-second/.example.env",
		deployLocal + "acme-srv-first/kratos/kratos.yml", deployLocal + "acme-srv-second/kratos/kratos.yml",
		deployLocal + "acme-srv-second/kratos/identity.schema.json", deployLocal + "postgres-init/10-kratos-database.sh",
	} {
		if !paths[want] {
			t.Errorf("%s was not delivered", want)
		}
	}
	for path := range paths {
		if path == deployLocal+".example.env" || strings.HasPrefix(path, deployLocal+"kratos/") {
			t.Errorf("%s stayed at the source path of a multi project", path)
		}
	}
}

func TestASingleProjectKeepsThePerComponentFilesAtTheSourcePath(t *testing.T) {
	repo := fixture.LoadCheckout(t)
	project := mustBuild(t, repo, manifest.Selection{
		Schema: 1, Project: manifest.Project{Name: "acme", GoModule: "example.com/acme"},
		Stacks: []string{"go-srv", "ng-web"}, Modules: []string{"identity"},
	})
	paths := projectPaths(project)
	for _, want := range []string{deployLocal + ".example.env", deployLocal + "kratos/kratos.yml"} {
		if !paths[want] {
			t.Errorf("%s was not delivered at the source path", want)
		}
	}
	env := projectFile(t, project, deployLocal+".example.env")
	if !strings.Contains(env, "APP_SESSION_KEYS=local-dev-1:passphrase:change-me-local-only-session-key\n") {
		t.Errorf("the single session key changed:\n%s", env)
	}
}

func TestEachInstanceHasItsOwnPortsOriginAndDatabaseInItsEnvFile(t *testing.T) {
	_, project := instancesProject(t)
	first := projectFile(t, project, deployLocal+"acme-srv-first/.example.env")
	second := projectFile(t, project, deployLocal+"acme-srv-second/.example.env")
	for _, want := range []string{
		"GO_SRV_ADDR=127.0.0.1:10100\n", "APP_PUBLIC_BASE_URL=http://localhost:10100\n", "@localhost:10132/acme_srv_first?",
		"OTEL_SERVICE_NAME=acme-srv-first\n", "APP_KRATOS_PUBLIC_URL=http://127.0.0.1:10133\n", "APP_KRATOS_ADMIN_URL=http://127.0.0.1:10134\n",
	} {
		if !strings.Contains(first, want) {
			t.Errorf("first env lacks %q:\n%s", want, first)
		}
	}
	for _, want := range []string{
		"GO_SRV_ADDR=127.0.0.1:10101\n", "APP_PUBLIC_BASE_URL=http://localhost:10120\n", "@localhost:10142/acme_srv_second?",
		"OTEL_SERVICE_NAME=acme-srv-second\n", "APP_KRATOS_PUBLIC_URL=http://127.0.0.1:10143\n", "APP_KRATOS_ADMIN_URL=http://127.0.0.1:10144\n",
	} {
		if !strings.Contains(second, want) {
			t.Errorf("second env lacks %q:\n%s", want, second)
		}
	}
	// A module block of the per-component file follows the modules of its component.
	if strings.Contains(first, "APP_BOOTSTRAP_TENANT_SLUG") || !strings.Contains(second, "APP_BOOTSTRAP_TENANT_SLUG") {
		t.Error("the tenancy block must be in the second env file only")
	}
}

func TestEachInstanceSignsItsSessionWithItsOwnKey(t *testing.T) {
	_, project := instancesProject(t)
	pattern := regexp.MustCompile(`(?m)^APP_SESSION_KEYS=(.*)$`)
	keys := map[string]string{}
	for _, name := range []string{"acme-srv-first", "acme-srv-second"} {
		match := pattern.FindStringSubmatch(projectFile(t, project, deployLocal+name+"/.example.env"))
		if match == nil {
			t.Fatalf("%s has no session key", name)
		}
		keys[name] = match[1]
		if want := "local-dev-1:passphrase:change-me-local-only-session-key-" + name; match[1] != want {
			t.Errorf("%s session keys = %q, want %q", name, match[1], want)
		}
	}
	if keys["acme-srv-first"] == keys["acme-srv-second"] {
		t.Error("two instances share one session key")
	}
	if passphrase := strings.TrimPrefix(keys["acme-srv-second"], "local-dev-1:passphrase:"); len(passphrase) < 24 || len(passphrase) > 96 {
		t.Errorf("the passphrase %q is outside the 24-96 character range", passphrase)
	}
}

func TestKratosOfEachInstanceKnowsItsOriginAndItsAdminPort(t *testing.T) {
	_, project := instancesProject(t)
	first := projectFile(t, project, deployLocal+"acme-srv-first/kratos/kratos.yml")
	second := projectFile(t, project, deployLocal+"acme-srv-second/kratos/kratos.yml")
	// The unbound backend is its own origin: its srv port, not the console's.
	if !strings.Contains(first, "base_url: http://localhost:10100/kratos/") || !strings.Contains(first, "base_url: http://127.0.0.1:10134/") ||
		strings.Contains(first, "10120") {
		t.Errorf("first kratos.yml:\n%s", first)
	}
	if !strings.Contains(second, "base_url: http://localhost:10120/kratos/") || !strings.Contains(second, "base_url: http://127.0.0.1:10144/") ||
		strings.Contains(second, "10100") {
		t.Errorf("second kratos.yml:\n%s", second)
	}
}

func TestTheConsoleProxiesToItsBackendsPort(t *testing.T) {
	_, project := instancesProject(t)
	proxy := projectFile(t, project, "acme-web/proxy.conf.json")
	if got := strings.Count(proxy, `"target": "http://localhost:10101"`); got != 3 || strings.Contains(proxy, "10100") {
		t.Errorf("proxy.conf.json targets (%d of 3 at 10101):\n%s", got, proxy)
	}
}

func TestComposeGivesEachInstanceItsServicesPortsAndVolume(t *testing.T) {
	_, project := instancesProject(t)
	compose := projectFile(t, project, composeYML)
	for _, want := range []string{
		"\n  acme-srv-first-postgres:\n", "\n  acme-srv-second-postgres:\n",
		"\n  acme-srv-first-kratos:\n", "\n  acme-srv-second-kratos:\n",
		"\n  acme-srv-first-kratos-migrate:\n", "\n  acme-srv-second-postgres-init:\n",
		`"127.0.0.1:10132:5432"`, `"127.0.0.1:10142:5432"`,
		`"127.0.0.1:10133:4433"`, `"127.0.0.1:10143:4433"`, `"127.0.0.1:10134:4434"`, `"127.0.0.1:10144:4434"`,
		`"127.0.0.1:10100:10100"`, `"127.0.0.1:10101:10101"`, `"127.0.0.1:10120:80"`,
		`GO_SRV_ADDR: ":10101"`, "GO_SRV_HOST: acme-srv-second:10101",
		"- path: ./acme-srv-first/.example.env", "- path: ./acme-srv-second/.env",
		"- ./acme-srv-first/kratos:/etc/config/kratos:ro", "- ./acme-srv-second/kratos:/etc/config/kratos:ro",
		"\n  acme-srv-first-postgres-data:\n", "\n  acme-srv-second-postgres-data:\n",
		"SERVE_PUBLIC_BASE_URL: ${APP_PUBLIC_BASE_URL:-http://localhost:10100}/kratos/",
		"SERVE_PUBLIC_BASE_URL: ${APP_PUBLIC_BASE_URL:-http://localhost:10120}/kratos/",
	} {
		if !strings.Contains(compose, want) {
			t.Errorf("compose lacks %q", want)
		}
	}
	// One postgres-init script serves every instance, read-only.
	if got := strings.Count(compose, "- ./postgres-init:/postgres-init:ro"); got != 2 {
		t.Errorf("postgres-init mounts = %d, want 2", got)
	}
}

func TestMiseTasksOfAnInstanceReachItsOrigin(t *testing.T) {
	_, project := instancesProject(t)
	mise := projectFile(t, project, "mise.toml")
	for _, want := range []string{
		`[tasks."dev:acme-srv-first"]`, `[tasks."dev:acme-srv-second"]`, `[tasks."dev:acme-web"]`,
		`[tasks."tck:acme-srv-first"]`, `[tasks."tck:acme-srv-second"]`,
		`env = { TCK_BASE_URL = "http://localhost:10100", APP_PUBLIC_BASE_URL = "http://localhost:10100" }`,
		`env = { TCK_BASE_URL = "http://localhost:10101", APP_PUBLIC_BASE_URL = "http://localhost:10120" }`,
		`.deploy/local/acme-srv-second/.example.env`,
	} {
		if !strings.Contains(mise, want) {
			t.Errorf("mise.toml lacks %q", want)
		}
	}
	if strings.Contains(mise, `"dev:srv"`) || strings.Contains(mise, "[tasks.tck]") {
		t.Error("a reference task key survived")
	}
}

func TestTheLockRecordsPortsOriginAndPerComponentFilesUnderTheirComponent(t *testing.T) {
	_, project := instancesProject(t)
	lock := decodeLock(t, project)
	first, second, web := lock.Components[0], lock.Components[1], lock.Components[2]
	if first.Ports["srv"] != 10100 || first.Ports["postgres"] != 10132 || second.Ports["srv"] != 10101 || second.Ports["postgres"] != 10142 ||
		second.Ports["kratos-admin"] != 10144 || web.Ports["web"] != 10120 {
		t.Errorf("ports = %v %v %v", first.Ports, second.Ports, web.Ports)
	}
	if first.Origin != "http://localhost:10100" || second.Origin != "http://localhost:10120" {
		t.Errorf("origins = %q %q", first.Origin, second.Origin)
	}
	if _, has := first.Ports["tenancy"]; has || len(first.Ports) != 4 {
		t.Errorf("the first instance lists ports of modules it does not install: %v", first.Ports)
	}
	listed := map[string]string{}
	for _, module := range lock.Modules {
		for _, file := range module.Files {
			if strings.HasPrefix(file.Path, deployLocal+"acme-srv-") {
				listed[file.Path] = module.Component
				if file.SourcePath == "" {
					t.Errorf("%s has no source path", file.Path)
				}
			}
		}
	}
	for path, want := range map[string]string{
		deployLocal + "acme-srv-first/.example.env":                "acme-srv-first",
		deployLocal + "acme-srv-second/.example.env":               "acme-srv-second",
		deployLocal + "acme-srv-second/kratos/kratos.yml":          "acme-srv-second",
		deployLocal + "acme-srv-first/kratos/kratos.yml":           "acme-srv-first",
		deployLocal + "acme-srv-first/kratos/identity.schema.json": "acme-srv-first",
	} {
		if listed[path] != want {
			t.Errorf("%s is listed under %q, want %q", path, listed[path], want)
		}
	}
}

func TestATwoBackendProjectVerifies(t *testing.T) {
	_, project := instancesProject(t)
	dir := mustWrite(t, project)
	if differences, err := verify.Project(dir); err != nil || len(differences) != 0 {
		t.Fatalf("verify: %v %v", differences, err)
	}
}

func TestEveryBackendCountUpToTheLimitGenerates(t *testing.T) {
	repo := fixture.LoadCheckout(t)
	for _, n := range []int{2, 7} {
		sel := manifest.Selection{
			Schema: 2, Project: manifest.Project{Name: "acme", GoModule: "example.com/acme"},
			Stacks: []string{"go-srv"}, AllModules: true,
		}
		for i := 1; i <= n; i++ {
			sel.Components = append(sel.Components, manifest.Component{Stack: "go-srv", Name: "b" + string(rune('0'+i))})
		}
		project := mustBuild(t, repo, sel)
		compose := projectFile(t, project, composeYML)
		for i := 1; i <= n; i++ {
			name := "b" + string(rune('0'+i))
			shift := 10 * (i - 1)
			for _, want := range []string{
				"\n  " + name + "-postgres:\n", "\n  " + name + "-kratos:\n", "\n  " + name + "-kratos-migrate:\n",
				fmt.Sprintf(`"127.0.0.1:%d:5432"`, 10132+shift),
				fmt.Sprintf(`"127.0.0.1:%d:4433"`, 10133+shift),
				fmt.Sprintf(`"127.0.0.1:%d:4434"`, 10134+shift),
			} {
				if !strings.Contains(compose, want) {
					t.Errorf("%d backends: compose lacks %q for %s", n, want, name)
				}
			}
		}
	}
}
