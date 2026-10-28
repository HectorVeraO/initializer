package main

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// tenancyMarkers are the strings that tie a TCK source to the tenancy module
// or to the api-keys module.
var tenancyMarkers = []string{"/api/v1/tenants", "/api/v1/invitations", "/api/v1/memberships", "APP_BOOTSTRAP_TENANT_SLUG"}

const apiKeyFormat = "ak_live_"

var relativeImport = regexp.MustCompile(`(?m)^\s*(?:import|export)\b[^'"]*?from\s+'(\.[^']*)'|^\s*import\s+'(\.[^']*)'|\bimport\(\s*'(\.[^']*)'\s*\)`)

// generateFromCheckout generates a go-srv project from the real checkout with the
// given modules and returns its output folder.
func generateFromCheckout(t *testing.T, modules string) string {
	t.Helper()
	root, catalogPath := realCatalog(t)
	out := filepath.Join(t.TempDir(), "out")
	args := []string{"generate", "--root", root, "--catalog", catalogPath, "--out", out,
		"--name", "acme-backend", "--go-module", "example.com/acme/backend", "--stacks", "go-srv"}
	if modules != "" {
		args = append(args, "--modules", modules)
	}
	code, _, stderr := execute(t, args...)
	if code != exitOK {
		t.Fatalf("generate %q exit %d: %s", modules, code, stderr)
	}
	return out
}

// deliveredTS lists the generated project's .ts files under specs/, slash separated.
func deliveredTS(t *testing.T, out string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(filepath.Join(out, "specs"), func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(p, ".ts") {
			rel, relErr := filepath.Rel(out, p)
			if relErr != nil {
				return relErr
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}

func fileExists(out, rel string) bool {
	info, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel)))
	return err == nil && !info.IsDir()
}

// TestIdentityOnlyProjectShipsNoTenancyOrAPIKeyActors checks that the shared
// TCK actors of a project with identity alone name no route, env key or key
// format of tenancy or api-keys.
func TestIdentityOnlyProjectShipsNoTenancyOrAPIKeyActors(t *testing.T) {
	out := generateFromCheckout(t, "identity")
	banned := append([]string{apiKeyFormat}, tenancyMarkers...)
	actors := 0
	for _, rel := range deliveredTS(t, out) {
		if !strings.HasPrefix(rel, "specs/tck-support/actors/") {
			continue
		}
		actors++
		data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range banned {
			if strings.Contains(string(data), marker) {
				t.Errorf("%s names %q in an identity-only project", rel, marker)
			}
		}
	}
	if actors == 0 {
		t.Fatal("an identity-only project ships no TCK actor at all")
	}
	for _, absent := range []string{"specs/tck-support/actors/tenants.ts", "specs/tck-support/actors/api-keys.ts"} {
		if fileExists(out, absent) {
			t.Errorf("identity-only project ships %s", absent)
		}
	}
}

// TestGeneratedTCKImportsOnlyDeliveredFiles checks that, for each module that
// cooperates with tenancy, the TCK sources of a project without tenancy and
// of a project with it import only files the project ships.
func TestGeneratedTCKImportsOnlyDeliveredFiles(t *testing.T) {
	for _, module := range []string{"api-keys", "feature-flags", "parameters"} {
		for _, modules := range []string{module, module + ",tenancy"} {
			t.Run(modules, func(t *testing.T) {
				out := generateFromCheckout(t, modules)
				files := deliveredTS(t, out)
				if len(files) == 0 {
					t.Fatal("no TCK source delivered")
				}
				for _, rel := range files {
					data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(rel)))
					if err != nil {
						t.Fatal(err)
					}
					for _, match := range relativeImport.FindAllStringSubmatch(string(data), -1) {
						specifier := match[1] + match[2] + match[3]
						target := path.Join(path.Dir(rel), specifier)
						if !fileExists(out, target+".ts") && !fileExists(out, target+"/index.ts") {
							t.Errorf("%s imports %q, which the project does not ship", rel, specifier)
						}
					}
				}
			})
		}
	}
}

// TestTenancyCooperationScenariosShipOnlyWithTenancy checks that a scenario
// file that needs tenancy is delivered with tenancy and only then.
func TestTenancyCooperationScenariosShipOnlyWithTenancy(t *testing.T) {
	for _, module := range []string{"api-keys", "feature-flags", "parameters"} {
		without := generateFromCheckout(t, module)
		with := generateFromCheckout(t, module+",tenancy")
		var cooperation []string
		for _, rel := range deliveredTS(t, with) {
			if strings.HasPrefix(rel, "specs/"+module+"/tck/with-tenancy/") {
				cooperation = append(cooperation, rel)
			}
		}
		if len(cooperation) == 0 {
			t.Errorf("%s with tenancy ships no specs/%s/tck/with-tenancy/ scenario", module, module)
		}
		for _, rel := range cooperation {
			if fileExists(without, rel) {
				t.Errorf("%s without tenancy ships %s", module, rel)
			}
		}
		if _, err := os.Stat(filepath.Join(without, "specs", module, "tck", "with-tenancy")); err == nil {
			t.Errorf("%s without tenancy ships an empty with-tenancy folder", module)
		}
	}
}
