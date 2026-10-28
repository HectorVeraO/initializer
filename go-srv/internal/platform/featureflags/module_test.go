package featureflags_test

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/config"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/featureflag"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/migrate"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/featureflags"
)

// buildWithoutDatabase runs Build up to Provide: the pool is lazy and these
// cases fail before any I/O.
func buildWithoutDatabase(t *testing.T, settings map[string]string) (*syncBuffer, error) {
	t.Helper()
	logger, sink := newLogger()
	base := map[string]string{
		"APP_PUBLIC_BASE_URL": "http://localhost:10120",
		"APP_DATABASE_URL":    "postgres://flags_test@127.0.0.1:1/flags_test?sslmode=disable",
	}
	for key, value := range settings {
		base[key] = value
	}
	app, err := core.Build(context.Background(), core.Options{Logger: logger, Config: config.MapSource(base)},
		[]core.Module{featureflags.Module()})
	if err == nil {
		_ = app.Close(context.Background())
	}
	return sink, err
}

func TestBuildFailsWhenTheFlagsFileKeyIsMissing(t *testing.T) {
	_, err := buildWithoutDatabase(t, nil)
	if err == nil || !strings.Contains(err.Error(), "APP_FLAGS_FILE") {
		t.Fatalf("err = %v, want one naming APP_FLAGS_FILE", err)
	}
}

func TestBuildFailsWithTheManifestProblems(t *testing.T) {
	path := writeManifest(t, "flags:\n  - key: Bad_Key\n    type: temp\n")
	sink, err := buildWithoutDatabase(t, map[string]string{"APP_FLAGS_FILE": path})
	if err == nil {
		t.Fatal("Build accepted an invalid manifest")
	}
	for _, code := range []string{"FLAG_KEY_INVALID", "FLAG_TYPE_INVALID", "FLAG_DEFAULT_MISSING"} {
		if !strings.Contains(err.Error(), code) {
			t.Fatalf("err misses %s: %v", code, err)
		}
	}
	logged := sink.records(t, "flags_manifest_invalid")
	gotCodes := make([]string, 0, len(logged))
	for _, record := range logged {
		gotCodes = append(gotCodes, fmt.Sprintf("%v@%v:%v", record["code"], record["line"], record["flag_key"]))
	}
	wantCodes := []string{
		"FLAG_KEY_INVALID@2:",
		"FLAG_TYPE_INVALID@3:",
		"FLAG_DESCRIPTION_MISSING@2:",
		"FLAG_DEFAULT_MISSING@2:",
	}
	sort.Strings(gotCodes)
	sort.Strings(wantCodes)
	if !slices.Equal(gotCodes, wantCodes) {
		t.Fatalf("flags_manifest_invalid records = %v, want %v", gotCodes, wantCodes)
	}
}

func TestBuildReportsConfigProblemsInsteadOfPanickingWithoutADatabaseURL(t *testing.T) {
	logger, _ := newLogger()
	path := writeManifest(t, twoFlagManifest)
	source := config.MapSource(map[string]string{
		"APP_PUBLIC_BASE_URL": "http://localhost:10120",
		"APP_FLAGS_FILE":      path,
	})
	_, err := core.Build(context.Background(), core.Options{Logger: logger, Config: source},
		[]core.Module{featureflags.Module()})
	if err == nil || !strings.Contains(err.Error(), "APP_DATABASE_URL") {
		t.Fatalf("err = %v, want one naming APP_DATABASE_URL", err)
	}
}

func TestBuildFailsWhenTheFlagsFileIsMissing(t *testing.T) {
	_, err := buildWithoutDatabase(t, map[string]string{"APP_FLAGS_FILE": "/nonexistent/flags.yaml"})
	if err == nil || !strings.Contains(err.Error(), "flags.yaml") {
		t.Fatalf("err = %v", err)
	}
}

func TestModulePublishesTheEvaluatorPort(t *testing.T) {
	e := newEnv(t, twoFlagManifest)
	if _, disabled := e.Evaluator.(featureflag.Disabled); disabled {
		t.Fatal("the host still serves the Disabled evaluator")
	}
	for _, tenant := range []*reqctx.TenantID{nil, &e.Tenant} {
		snapshot := e.Evaluator.Snapshot(context.Background(), tenant)
		if len(snapshot) != 2 {
			t.Fatalf("snapshot = %v, want exactly the manifest keys", snapshot)
		}
		if _, ok := snapshot[releaseKey]; !ok {
			t.Fatalf("snapshot misses %s: %v", releaseKey, snapshot)
		}
		if _, ok := snapshot[opsKey]; !ok {
			t.Fatalf("snapshot misses %s: %v", opsKey, snapshot)
		}
	}
}

func TestMigrationsLandInTheModuleVersionTable(t *testing.T) {
	e := newEnv(t, "flags: []\n")
	table := migrate.VersionTable("feature-flags")
	if table != "cor_migrations_feature_flags" {
		t.Fatalf("version table = %q", table)
	}
	var count int
	err := e.Pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE version_id > 0").Scan(&count)
	if err != nil || count != 2 {
		t.Fatalf("applied migrations = %d, %v; want 2", count, err)
	}
}

func TestOverrideTablesRefuseInvalidKeys(t *testing.T) {
	e := newEnv(t, "flags: []\n")
	valid64 := "a" + strings.Repeat("b", 63)
	invalid := []string{"a" + strings.Repeat("b", 64), "Upper", "1abc", "has-dash", ""}
	e.setGlobal(t, valid64, true)
	e.setTenant(t, randomUUID(t), valid64, true)
	for _, key := range invalid {
		_, err := e.Pool.Exec(context.Background(), "INSERT INTO flg_overrides (flag_key, enabled) VALUES ($1, true)", key)
		if err == nil {
			t.Errorf("flg_overrides accepted key %q", key)
		}
		_, err = e.Pool.Exec(context.Background(),
			"INSERT INTO flg_tenant_overrides (tenant_id, flag_key, enabled) VALUES ($1, $2, true)", randomUUID(t), key)
		if err == nil {
			t.Errorf("flg_tenant_overrides accepted key %q", key)
		}
	}
}
