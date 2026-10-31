package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// backendFlags returns the flags of n core-only backends named b01..bnn.
func backendFlags(n int) []string {
	flags := []string{"--name", "acme", "--stacks", "go-srv", "--go-module", "example.com/acme"}
	for i := 1; i <= n; i++ {
		flags = append(flags, "--component", fmt.Sprintf("go-srv=b%02d", i))
	}
	return flags
}

func TestSeveralBackendsGenerateOneSharedPostgresServer(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	args := append([]string{"generate", "--root", root, "--catalog", catalogPath, out, "--service", "postgres=acme-db", "--service", "postgres-init=acme-db-init"}, backendFlags(3)...)
	if code, _, stderr := execute(t, args...); code != exitOK {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	if code, stdout, stderr := execute(t, "verify", out); code != exitOK || !strings.Contains(stdout, "No differences.") {
		t.Fatalf("verify exit %d: %s%s", code, stdout, stderr)
	}
	compose := readProjectFile(t, out, ".deploy/local/docker-compose.yml")
	for _, want := range []string{"\n  acme-db:\n", "\n  acme-db-init:\n", "\n  acme-db-data:\n", `"127.0.0.1:10132:5432"`} {
		if got := strings.Count(compose, want); got != 1 {
			t.Errorf("compose holds %q %d times, want 1", want, got)
		}
	}
	for i := 1; i <= 3; i++ {
		name := fmt.Sprintf("b%02d", i)
		for _, want := range []string{
			"PG_ROLE_" + name + ": " + name + ":",
			"APP_DATABASE_URL: postgres://" + name + ":${ACME_PG_PASSWORD:-change-me-local-only}@acme-db:5432/" + name + "?sslmode=disable",
		} {
			if !strings.Contains(compose, want) {
				t.Errorf("compose lacks %q", want)
			}
		}
		env := readProjectFile(t, out, ".deploy/local/"+name+"/.example.env")
		if want := "@localhost:10132/" + name + "?sslmode=disable"; !strings.Contains(env, want) {
			t.Errorf("%s env lacks %q", name, want)
		}
	}
	t.Run("compose config", func(t *testing.T) {
		if _, err := exec.LookPath("docker"); err != nil {
			t.Skip("docker is not installed")
		}
		if output, err := runCompose(out, "-f", filepath.Join(out, ".deploy", "local", "docker-compose.yml"), "--profile", "app", "config", "-q"); err != nil {
			t.Errorf("docker compose config: %v\n%s", err, output)
		}
	})
}

func TestThePostgresServiceOfOneBackendOfSeveralIsRefused(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	args := []string{"generate", "--root", root, "--catalog", catalogPath, out, "--name", "acme", "--stacks", "go-srv", "--go-module", "example.com/acme",
		"--component", "go-srv=b01,service.postgres=b01-db", "--component", "go-srv=b02", "--format", "json"}
	code, stdout, _ := execute(t, args...)
	doc := issuesOf(t, stdout)
	if code != exitIssues || len(doc.Issues) != 1 || doc.Issues[0].Code != "NAME_OVERRIDE_UNUSED" ||
		!strings.Contains(doc.Issues[0].Hint, "--service postgres=") {
		t.Fatalf("exit %d issues %+v", code, doc.Issues)
	}
}

func TestTwoBackendsOnOneDatabaseNameAreRefused(t *testing.T) {
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	args := []string{"generate", "--root", root, "--catalog", catalogPath, out, "--name", "acme", "--stacks", "go-srv", "--go-module", "example.com/acme",
		"--component", "go-srv=b01,db-name=shop", "--component", "go-srv=b02,db-name=shop", "--format", "json"}
	code, stdout, _ := execute(t, args...)
	doc := issuesOf(t, stdout)
	if code != exitIssues || strings.Join(doc.codes(), ",") != "NAME_CONFLICT,NAME_CONFLICT" {
		t.Fatalf("exit %d issues %+v", code, doc.Issues)
	}
}

// The band holds 32 core-only backends; the 33rd answers PORT_BAND_EXHAUSTED
// with the real reason and the count that fits.
func TestTheBackendCountIsLimitedOnlyByTheBand(t *testing.T) {
	root, catalogPath := realCatalog(t)
	for _, tc := range []struct {
		backends int
		fits     bool
	}{{32, true}, {33, false}} {
		out := filepath.Join(t.TempDir(), "out")
		args := append([]string{"generate", "--root", root, "--catalog", catalogPath, out, "--dry-run", "--format", "json"}, backendFlags(tc.backends)...)
		code, stdout, stderr := execute(t, args...)
		if tc.fits {
			if code != exitOK {
				t.Fatalf("%d backends: exit %d %s%s", tc.backends, code, stdout, stderr)
			}
			continue
		}
		doc := issuesOf(t, stdout)
		if code != exitIssues || len(doc.Issues) == 0 {
			t.Fatalf("%d backends: exit %d %s", tc.backends, code, stdout)
		}
		for _, found := range doc.Issues {
			if found.Code == "COMPONENT_LIMIT" {
				t.Errorf("the count limit is gone: %+v", found)
			}
		}
		first := doc.Issues[0]
		if first.Code != "PORT_BAND_EXHAUSTED" || !strings.Contains(first.Message, "b33 (index 32) would use offset 32, which port postgres") ||
			!strings.Contains(first.Hint, "at most 32 go-srv components fit") {
			t.Errorf("first issue = %+v", first)
		}
	}
}

func TestTheDryRunSelectionOfSeveralBackendsKeepsTheSharedServicesAtTheTopLevel(t *testing.T) {
	root, catalogPath := realCatalog(t)
	args := append([]string{"generate", "--root", root, "--catalog", catalogPath, filepath.Join(t.TempDir(), "out"), "--dry-run", "--format", "json"}, backendFlags(2)...)
	code, stdout, stderr := execute(t, args...)
	if code != exitOK {
		t.Fatalf("exit %d %s%s", code, stdout, stderr)
	}
	var doc struct {
		Selection struct {
			Services   map[string]string `json:"services"`
			Components []struct {
				Services map[string]string `json:"services"`
			} `json:"components"`
		} `json:"selection"`
	}
	decodeDocument(t, stdout, &doc)
	if doc.Selection.Services["postgres"] != "acme-postgres" || doc.Selection.Services["postgres-init"] != "acme-postgres-init" {
		t.Errorf("top-level services = %v", doc.Selection.Services)
	}
	for _, component := range doc.Selection.Components {
		if _, has := component.Services["postgres"]; has {
			t.Errorf("a component entry names the shared server: %v", component.Services)
		}
	}
}
