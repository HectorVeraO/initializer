package featureflags_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/featureflag"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/featureflags"
)

type override struct {
	set   bool
	value bool
}

func TestLevelsTenantOverGlobalOverDefault(t *testing.T) {
	cases := []struct {
		name   string
		key    string
		global override
		tenant override
		want   bool
	}{
		{"release default", releaseKey, override{}, override{}, false},
		{"ops default", opsKey, override{}, override{}, true},
		{"global on", releaseKey, override{true, true}, override{}, true},
		{"global off over ops default", opsKey, override{true, false}, override{}, false},
		{"tenant off over global on", releaseKey, override{true, true}, override{true, false}, false},
		{"tenant on over global off", releaseKey, override{true, false}, override{true, true}, true},
		{"tenant on over default", releaseKey, override{}, override{true, true}, true},
		{"tenant off over ops default", opsKey, override{}, override{true, false}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, twoFlagManifest)
			if tc.global.set {
				e.setGlobal(t, tc.key, tc.global.value)
			}
			if tc.tenant.set {
				e.setTenant(t, string(e.Tenant), tc.key, tc.tenant.value)
			}
			// A row of another tenant never applies.
			e.setTenant(t, randomUUID(t), tc.key, !tc.want)

			if got := e.enabledForTenant(t, tc.key); got != tc.want {
				t.Fatalf("Enabled with tenant = %v, want %v", got, tc.want)
			}
			snapshot := e.Evaluator.Snapshot(context.Background(), &e.Tenant)
			if got := snapshot[featureflag.Key(tc.key)]; got != tc.want {
				t.Fatalf("Snapshot with tenant = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWithoutTenantTheTenantLevelIsSkipped(t *testing.T) {
	e := newEnv(t, twoFlagManifest)
	e.setGlobal(t, releaseKey, true)
	e.setTenant(t, string(e.Tenant), releaseKey, false)
	e.setTenant(t, string(e.Tenant), opsKey, false)

	if !e.Evaluator.Enabled(context.Background(), releaseKey) {
		t.Fatal("global override must apply without a tenant")
	}
	if !e.Evaluator.Enabled(context.Background(), opsKey) {
		t.Fatal("manifest default must apply without a tenant")
	}
	snapshot := e.Evaluator.Snapshot(context.Background(), nil)
	if !snapshot[releaseKey] || !snapshot[opsKey] {
		t.Fatalf("snapshot without tenant = %v", snapshot)
	}
}

func TestUndeclaredKeyIsFalseAndOrphanRowsAreIgnored(t *testing.T) {
	e := newEnv(t, twoFlagManifest)
	e.setGlobal(t, "removed_flag", true)
	e.setTenant(t, string(e.Tenant), "removed_flag", true)

	if e.Evaluator.Enabled(context.Background(), "removed_flag") {
		t.Fatal("an undeclared key must be false even with an override row")
	}
	if e.enabledForTenant(t, "never_declared") {
		t.Fatal("an unknown key must be false")
	}
	warnings := e.Logs.records(t, "flag_unknown_key")
	if len(warnings) != 2 {
		t.Fatalf("flag_unknown_key logged %d times, want 2", len(warnings))
	}
	for _, tenant := range []*reqctx.TenantID{nil, &e.Tenant} {
		snapshot := e.Evaluator.Snapshot(context.Background(), tenant)
		if len(snapshot) != 2 {
			t.Fatalf("snapshot keys = %v, want exactly the manifest keys", snapshot)
		}
		if _, leaked := snapshot["removed_flag"]; leaked {
			t.Fatal("orphan row leaked into the snapshot")
		}
	}
}

func TestSnapshotOfEmptyManifestIsEmptyNotNil(t *testing.T) {
	e := newEnv(t, "flags: []\n")
	for _, tenant := range []*reqctx.TenantID{nil, &e.Tenant} {
		snapshot := e.Evaluator.Snapshot(context.Background(), tenant)
		if snapshot == nil || len(snapshot) != 0 {
			t.Fatalf("snapshot = %#v, want empty non-nil", snapshot)
		}
	}
}

func TestStoreFailureFallsBackToTheSafeValue(t *testing.T) {
	e := newEnv(t, twoFlagManifest)
	e.setGlobal(t, releaseKey, true)
	e.setGlobal(t, opsKey, false)
	e.Pool.Close()

	if e.Evaluator.Enabled(context.Background(), releaseKey) {
		t.Fatal("a release flag must read false when the store fails")
	}
	if !e.Evaluator.Enabled(context.Background(), opsKey) {
		t.Fatal("an ops flag must read its manifest default when the store fails")
	}
	for _, tenant := range []*reqctx.TenantID{nil, &e.Tenant} {
		snapshot := e.Evaluator.Snapshot(context.Background(), tenant)
		if snapshot[releaseKey] || !snapshot[opsKey] || len(snapshot) != 2 {
			t.Fatalf("snapshot on store failure = %v", snapshot)
		}
	}
	failures := e.Logs.records(t, "flag_evaluation_failed")
	if len(failures) == 0 {
		t.Fatal("store failures must be logged as flag_evaluation_failed")
	}
}

func TestTenantRowsFailureDoesNotServeGlobalValues(t *testing.T) {
	e := newEnv(t, twoFlagManifest)
	e.setGlobal(t, releaseKey, true)
	e.exec(t, "DROP TABLE flg_tenant_overrides")

	snapshot := e.Evaluator.Snapshot(context.Background(), &e.Tenant)
	if snapshot[releaseKey] || !snapshot[opsKey] || len(snapshot) != 2 {
		t.Fatalf("tenant snapshot with failed tenant query = %v, want safe values", snapshot)
	}
	global := e.Evaluator.Snapshot(context.Background(), nil)
	if !global[releaseKey] {
		t.Fatalf("global snapshot = %v, want the stored global override", global)
	}
}

func TestGlobalRowsFailureDoesNotServeTenantValues(t *testing.T) {
	e := newEnv(t, twoFlagManifest)
	e.setTenant(t, string(e.Tenant), releaseKey, true)
	e.exec(t, "DROP TABLE flg_overrides")

	snapshot := e.Evaluator.Snapshot(context.Background(), &e.Tenant)
	if snapshot[releaseKey] || !snapshot[opsKey] || len(snapshot) != 2 {
		t.Fatalf("tenant snapshot with failed global query = %v, want safe values", snapshot)
	}
	if e.enabledForTenant(t, releaseKey) {
		t.Fatal("a release flag must read false when the global query fails")
	}
}

func TestNewEvaluatorRejectsNilDependencies(t *testing.T) {
	e := newEnv(t, "flags: []\n")
	manifest, err := featureflags.ParseManifest(strings.NewReader("flags: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		pool   *pgxpool.Pool
		logger *slog.Logger
	}{
		{"nil pool", nil, slog.New(slog.DiscardHandler)},
		{"nil logger", e.Pool, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("NewEvaluator did not panic")
				}
			}()
			featureflags.NewEvaluator(manifest, tc.pool, tc.logger)
		})
	}
}

func TestEvaluationLogsTheSourceAtDebug(t *testing.T) {
	e := newEnv(t, twoFlagManifest)
	e.setGlobal(t, releaseKey, true)
	e.setTenant(t, string(e.Tenant), opsKey, false)

	e.Evaluator.Enabled(context.Background(), releaseKey)
	e.enabledForTenant(t, opsKey)
	e.enabledForTenant(t, releaseKey)

	sources := map[string]string{}
	for _, record := range e.Logs.records(t, "flag_evaluated") {
		key, _ := record["flag_key"].(string)
		source, _ := record["flag_source"].(string)
		sources[key+"/"+source] = "seen"
	}
	for _, want := range []string{releaseKey + "/global", opsKey + "/tenant"} {
		if sources[want] == "" {
			t.Fatalf("missing flag_evaluated for %s in %v", want, sources)
		}
	}
}
