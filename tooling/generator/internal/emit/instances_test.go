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
		deployLocal + "acme-srv-second/kratos/identity.schema.json",
	} {
		if !paths[want] {
			t.Errorf("%s was not delivered", want)
		}
	}
	for path := range paths {
		if path == deployLocal+".example.env" || strings.HasPrefix(path, deployLocal+"kratos/") {
			t.Errorf("%s stayed at the source path of a multi project", path)
		}
		if strings.HasPrefix(path, deployLocal+"postgres-init/") {
			t.Errorf("%s: the init step is inline in the compose file", path)
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
		"GO_SRV_ADDR=127.0.0.1:10101\n", "APP_PUBLIC_BASE_URL=http://localhost:10120\n", "@localhost:10132/acme_srv_second?",
		"OTEL_SERVICE_NAME=acme-srv-second\n", "APP_KRATOS_PUBLIC_URL=http://127.0.0.1:10135\n", "APP_KRATOS_ADMIN_URL=http://127.0.0.1:10136\n",
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
	if !strings.Contains(second, "base_url: http://localhost:10120/kratos/") || !strings.Contains(second, "base_url: http://127.0.0.1:10136/") ||
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

func TestComposeGivesEachInstanceItsAppAndKratosAndSharesOnePostgres(t *testing.T) {
	_, project := instancesProject(t)
	compose := projectFile(t, project, composeYML)
	for _, want := range []string{
		"\n  acme-srv-first-kratos:\n", "\n  acme-srv-second-kratos:\n",
		"\n  acme-srv-first-kratos-migrate:\n", "\n  acme-srv-second-kratos-migrate:\n",
		`"127.0.0.1:10133:4433"`, `"127.0.0.1:10135:4433"`, `"127.0.0.1:10134:4434"`, `"127.0.0.1:10136:4434"`,
		`"127.0.0.1:10100:10100"`, `"127.0.0.1:10101:10101"`, `"127.0.0.1:10120:80"`,
		`GO_SRV_ADDR: ":10101"`, "GO_SRV_HOST: acme-srv-second:10101",
		"- path: ./acme-srv-first/.example.env", "- path: ./acme-srv-second/.env",
		"- ./acme-srv-first/kratos:/etc/config/kratos:ro", "- ./acme-srv-second/kratos:/etc/config/kratos:ro",
		"SERVE_PUBLIC_BASE_URL: ${APP_PUBLIC_BASE_URL:-http://localhost:10100}/kratos/",
		"SERVE_PUBLIC_BASE_URL: ${APP_PUBLIC_BASE_URL:-http://localhost:10120}/kratos/",
	} {
		if !strings.Contains(compose, want) {
			t.Errorf("compose lacks %q", want)
		}
	}
}

// The backends of a project run one Postgres server: one service, one volume,
// one host port, one init step; no service, volume or port belongs to a backend.
func TestComposeHasOneSharedPostgresServerVolumeAndPort(t *testing.T) {
	_, project := instancesProject(t)
	compose := projectFile(t, project, composeYML)
	for _, want := range []string{
		"\n  acme-postgres:\n", "\n  acme-postgres-init:\n", "\n  acme-postgres-data:\n",
		`"127.0.0.1:10132:5432"`, "- acme-postgres-data:/var/lib/postgresql",
	} {
		if got := strings.Count(compose, want); got != 1 {
			t.Errorf("compose holds %q %d times, want 1", want, got)
		}
	}
	for _, banned := range []string{
		"acme-srv-first-postgres", "acme-srv-second-postgres", "10142", "10143", "10144",
		"./postgres-init:/postgres-init", "10-kratos-database.sh",
	} {
		if strings.Contains(compose, banned) {
			t.Errorf("compose still holds the per-backend %q", banned)
		}
	}
	if got := strings.Count(compose, "image: postgres:18"); got != 2 {
		t.Errorf("postgres images = %d, want the server and its init step", got)
	}
}

// Each backend and each Kratos has its own database and user on the shared
// server, and the init step creates every one of them.
func TestTheInitStepCreatesTheDatabaseAndUserOfEveryBackendAndEveryKratos(t *testing.T) {
	_, project := instancesProject(t)
	compose := projectFile(t, project, composeYML)
	for _, want := range []string{
		"PG_ROLE_acme_srv_first: acme_srv_first:${ACME_PG_PASSWORD:-change-me-local-only}",
		"PG_ROLE_acme_srv_second: acme_srv_second:${ACME_PG_PASSWORD:-change-me-local-only}",
		"PG_ROLE_acme_srv_first_kratos: acme_srv_first_kratos:${KRATOS_PG_PASSWORD:-change-me-local-only-kratos}",
		"PG_ROLE_acme_srv_second_kratos: acme_srv_second_kratos:${KRATOS_PG_PASSWORD:-change-me-local-only-kratos}",
		"PGHOST: acme-postgres", "PGUSER: postgres", "PGDATABASE: postgres",
	} {
		if got := strings.Count(compose, want); got != 1 {
			t.Errorf("compose holds %q %d times, want 1", want, got)
		}
	}
	// Both backends and both kratos-migrate wait for the init step.
	if got := strings.Count(compose, "      acme-postgres-init:\n        condition: service_completed_successfully"); got != 4 {
		t.Errorf("services waiting for the init step = %d, want 2 backends and 2 kratos-migrate", got)
	}
}

// The connection strings point at the shared server, each with its own
// database and user; the host-dev env file says the same through the host port.
func TestEveryConnectionStringPointsAtTheSharedServerWithItsOwnDatabase(t *testing.T) {
	_, project := instancesProject(t)
	compose := projectFile(t, project, composeYML)
	for _, want := range []string{
		"APP_DATABASE_URL: postgres://acme_srv_first:${ACME_PG_PASSWORD:-change-me-local-only}@acme-postgres:5432/acme_srv_first?sslmode=disable",
		"APP_DATABASE_URL: postgres://acme_srv_second:${ACME_PG_PASSWORD:-change-me-local-only}@acme-postgres:5432/acme_srv_second?sslmode=disable",
		"DSN: postgres://acme_srv_first_kratos:${KRATOS_PG_PASSWORD:-change-me-local-only-kratos}@acme-postgres:5432/acme_srv_first_kratos?",
		"DSN: postgres://acme_srv_second_kratos:${KRATOS_PG_PASSWORD:-change-me-local-only-kratos}@acme-postgres:5432/acme_srv_second_kratos?",
	} {
		if !strings.Contains(compose, want) {
			t.Errorf("compose lacks %q", want)
		}
	}
	for _, name := range []string{"acme_srv_first", "acme_srv_second"} {
		env := projectFile(t, project, deployLocal+strings.ReplaceAll(name, "_", "-")+"/.example.env")
		want := "postgres://" + name + ":${ACME_PG_PASSWORD}@localhost:10132/" + name + "?sslmode=disable"
		if !strings.Contains(env, want) {
			t.Errorf("%s env lacks %q:\n%s", name, want, env)
		}
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
	if first.Ports["srv"] != 10100 || first.Ports["postgres"] != 10132 || second.Ports["srv"] != 10101 || second.Ports["postgres"] != 10132 ||
		first.Ports["kratos-admin"] != 10134 || second.Ports["kratos-admin"] != 10136 || web.Ports["web"] != 10120 {
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

func TestSeveralBackendsGenerateAndShareOnePostgres(t *testing.T) {
	repo := fixture.LoadCheckout(t)
	for _, tc := range []struct {
		backends   int
		allModules bool
	}{{2, true}, {12, true}, {32, false}} {
		sel := manifest.Selection{
			Schema: 2, Project: manifest.Project{Name: "acme", GoModule: "example.com/acme"},
			Stacks: []string{"go-srv"}, AllModules: tc.allModules,
		}
		for i := 1; i <= tc.backends; i++ {
			sel.Components = append(sel.Components, manifest.Component{Stack: "go-srv", Name: fmt.Sprintf("b%02d", i)})
		}
		project := mustBuild(t, repo, sel)
		compose := projectFile(t, project, composeYML)
		if got := strings.Count(compose, "\n  acme-postgres:\n"); got != 1 {
			t.Errorf("%d backends: %d postgres services", tc.backends, got)
		}
		if got := strings.Count(compose, `:5432"`); got != 1 {
			t.Errorf("%d backends: %d published postgres ports", tc.backends, got)
		}
		wantRoles := tc.backends
		if tc.allModules {
			wantRoles *= 2
		}
		if got := strings.Count(compose, "      PG_ROLE_"); got != wantRoles {
			t.Errorf("%d backends: %d roles in the init step, want %d", tc.backends, got, wantRoles)
		}
		for i := 1; i <= tc.backends; i++ {
			name := fmt.Sprintf("b%02d", i)
			for _, want := range []string{
				"PG_ROLE_" + name + ": " + name + ":",
				fmt.Sprintf(`"127.0.0.1:%d:%d"`, 10100+i-1, 10100+i-1),
				"@acme-postgres:5432/" + name + "?sslmode=disable",
			} {
				if !strings.Contains(compose, want) {
					t.Errorf("%d backends: compose lacks %q for %s", tc.backends, want, name)
				}
			}
			if tc.allModules {
				public := 10133 + 2*(i-1)
				for _, want := range []string{fmt.Sprintf(`"127.0.0.1:%d:4433"`, public), fmt.Sprintf(`"127.0.0.1:%d:4434"`, public+1)} {
					if !strings.Contains(compose, want) {
						t.Errorf("%d backends: compose lacks %q for %s", tc.backends, want, name)
					}
				}
			}
		}
	}
}

// A project without identity still gets the init step once it has several
// backends: the databases are created by it, not by the server.
func TestSeveralBackendsWithoutIdentityStillGetTheInitStep(t *testing.T) {
	repo := fixture.LoadCheckout(t)
	sel := manifest.Selection{
		Schema: 2, Project: manifest.Project{Name: "acme", GoModule: "example.com/acme"}, Stacks: []string{"go-srv"},
		Components: []manifest.Component{{Stack: "go-srv", Name: "acme-one"}, {Stack: "go-srv", Name: "acme-two"}},
	}
	compose := projectFile(t, mustBuild(t, repo, sel), composeYML)
	for _, want := range []string{
		"\n  acme-postgres-init:\n", "PG_ROLE_acme_one: acme_one:", "PG_ROLE_acme_two: acme_two:",
		"      acme-postgres-init:\n        condition: service_completed_successfully",
	} {
		if !strings.Contains(compose, want) {
			t.Errorf("compose lacks %q", want)
		}
	}
	if strings.Contains(compose, "kratos") {
		t.Error("a project without identity has no Kratos")
	}
}

// One backend without identity keeps the single-instance compose: the server is
// the backend's own database and user, and there is no init step.
func TestOneBackendWithoutIdentityHasNoInitStep(t *testing.T) {
	repo := fixture.LoadCheckout(t)
	project := mustBuild(t, repo, manifest.Selection{
		Schema: 1, Project: manifest.Project{Name: "acme", GoModule: "example.com/acme"}, Stacks: []string{"go-srv"},
	})
	compose := projectFile(t, project, composeYML)
	for _, banned := range []string{"postgres-init", "PG_ROLE_", "kratos"} {
		if strings.Contains(compose, banned) {
			t.Errorf("a single backend without identity holds %q", banned)
		}
	}
	if !strings.Contains(compose, "POSTGRES_USER: acme\n") || !strings.Contains(compose, "pg_isready -U acme -d acme") {
		t.Errorf("the single server is not the backend's own database and user:\n%s", compose)
	}
}
