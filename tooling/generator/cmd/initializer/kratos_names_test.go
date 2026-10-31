package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// generateExpectingRefusal runs generate for a project of one go-srv backend
// and returns its stderr; it fails unless the run exits 1 and writes nothing.
func generateExpectingRefusal(t *testing.T, modules string, extra ...string) string {
	t.Helper()
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	args := append([]string{"generate", "--root", root, "--catalog", catalogPath, out,
		"--go-module", "example.com/acme", "--stacks", "go-srv", "--modules", modules}, extra...)
	code, _, stderr := execute(t, args...)
	if code != exitIssues {
		t.Fatalf("generate exit %d, want %d: %s", code, exitIssues, stderr)
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("a refused generate wrote %s", out)
	}
	return stderr
}

func TestAProjectDatabaseOrUserNamedKratosIsRefusedWhileIdentityIsInstalled(t *testing.T) {
	cases := map[string]struct {
		extra   []string
		pointer string
	}{
		"project name":  {[]string{"--name", "kratos"}, "--name"},
		"database user": {[]string{"--name", "acme", "--db-user", "kratos"}, "--db-user"},
		"database name": {[]string{"--name", "acme", "--db-name", "kratos"}, "--db-name"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			stderr := generateExpectingRefusal(t, "identity", tc.extra...)
			for _, want := range []string{tc.pointer + ": ", "NAME_RESERVED", "kratos", "for example"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr lacks %q:\n%s", want, stderr)
				}
			}
		})
	}
}

func TestAModuleThatNeedsIdentityMakesTheKratosNamesReservedToo(t *testing.T) {
	stderr := generateExpectingRefusal(t, "tenancy", "--name", "acme", "--db-name", "kratos")
	if !strings.Contains(stderr, "NAME_RESERVED") {
		t.Errorf("tenancy requires identity, so its Kratos owns the database name: %s", stderr)
	}
}

func TestAProjectNamedKratosIsRefusedWhenAnyBackendInstallsIdentity(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, out,
		"--name", "kratos", "--go-module", "example.com/kratos", "--stacks", "go-srv",
		"--component", "go-srv=orders-srv,modules=identity", "--component", "go-srv=users-srv")
	if code != exitIssues || !strings.Contains(stderr, "--name: ") || !strings.Contains(stderr, "NAME_RESERVED") {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
}

func TestTheKratosNamesStayFreeWhereNoKratosIsDelivered(t *testing.T) {
	root, catalogPath := realCatalog(t)
	for name, extra := range map[string][]string{
		"project name":  {"--name", "kratos"},
		"database name": {"--name", "acme", "--db-name", "kratos"},
		"database user": {"--name", "acme", "--db-user", "kratos"},
	} {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			args := append([]string{"generate", "--root", root, "--catalog", catalogPath, out,
				"--go-module", "example.com/acme", "--stacks", "go-srv"}, extra...)
			if code, _, stderr := execute(t, args...); code != exitOK {
				t.Fatalf("a core-only project has no Kratos; generate exit %d: %s", code, stderr)
			}
		})
	}
}

func TestABackendOfASeveralBackendProjectMayUseTheNameKratosForItsDatabaseAndUser(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	code, _, stderr := execute(t, "generate", "--root", root, "--catalog", catalogPath, out,
		"--name", "acme", "--go-module", "example.com/acme", "--stacks", "go-srv",
		"--component", "go-srv=users-srv,modules=identity,db-name=kratos,db-user=kratos",
		"--component", "go-srv=orders-srv,modules=identity")
	if code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	compose := readProjectFile(t, out, ".deploy/local/docker-compose.yml")
	// The backend's role keeps the name; its own Kratos derives "kratos_kratos", so no two roles or databases collide.
	for _, want := range []string{
		"PG_ROLE_kratos: kratos:${ACME_PG_PASSWORD:-",
		"PG_ROLE_kratos_kratos: kratos_kratos:${KRATOS_PG_PASSWORD:-",
		"PG_ROLE_orders_srv_kratos: orders_srv_kratos:${KRATOS_PG_PASSWORD:-",
	} {
		if !strings.Contains(compose, want) {
			t.Errorf("the compose file lacks %q", want)
		}
	}
	if strings.Contains(compose, "POSTGRES_USER: kratos") {
		t.Error("the shared server's superuser must stay postgres")
	}
}
