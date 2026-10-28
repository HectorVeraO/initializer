package featureflags_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/coretest"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/featureflag"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/featureflags"
)

// aloneEnv is feature-flags with an operator session and no TenantDirectory
// provider, so the core NoTenants default answers.
func aloneEnv(t testing.TB) *routeEnv {
	t.Helper()
	e := &routeEnv{}
	operator := randomUUID(t)
	sessions := &actorSessions{principals: map[string]reqctx.Principal{
		"operator": {Kind: reqctx.ActorAccount, ActorID: operator, AccountID: reqctx.AccountID(operator), IsPlatformOperator: true},
	}}
	e.probe = &env{}
	capture := core.Module{ID: "capture", Mount: func(_ context.Context, h *core.Host) error {
		e.probe.Evaluator = h.FlagEvaluator()
		e.probe.Pool = h.Pool()
		return nil
	}}
	e.handler = coretest.New(t, []core.Module{featureflags.Module(), capture}, coretest.Options{
		Database:        true,
		Config:          map[string]string{"APP_FLAGS_FILE": writeManifest(t, twoFlagManifest)},
		SessionResolver: sessions,
	})
	return e
}

func (e *routeEnv) listing(t testing.TB) []featureflags.FlagView {
	t.Helper()
	page := wantData[api.Page[featureflags.FlagView]](t, e.json("operator", http.MethodGet, flagsPath, ""), http.StatusOK)
	return page.Items
}

func TestWithoutTenancyAPutOnATenantOverrideIsNotFound(t *testing.T) {
	e := aloneEnv(t)
	ids := map[string]string{
		"random id":     randomUUID(t),
		"nil uuid":      "00000000-0000-0000-0000-000000000000",
		"upper-case id": strings.ToUpper(randomUUID(t)),
		"malformed id":  "not-a-uuid",
	}
	for name, id := range ids {
		t.Run(name, func(t *testing.T) {
			path := flagsPath + "/" + releaseKey + "/tenants/" + id

			wantError(t, e.json("operator", http.MethodPut, path, enabledOn), http.StatusNotFound, api.CodeResourceNotFound)
		})
	}
	for _, item := range e.listing(t) {
		if len(item.TenantOverrides) != 0 {
			t.Fatalf("a refused PUT stored an override: %+v", item)
		}
	}
}

func TestWithoutTenancyAPutOnAnUnknownFlagStillNamesTheFlag(t *testing.T) {
	e := aloneEnv(t)

	wantError(t, e.json("operator", http.MethodPut, flagsPath+"/flag_ghost/tenants/"+randomUUID(t), enabledOn), http.StatusNotFound, notDefined)
}

func TestWithoutTenancyADeleteAnswersTheUnchangedView(t *testing.T) {
	e := aloneEnv(t)
	wantData[featureflags.FlagView](t, e.json("operator", http.MethodPut, flagsPath+"/"+releaseKey, enabledOn), http.StatusOK)
	base := flagsPath + "/" + releaseKey + "/tenants/"

	for _, id := range []string{randomUUID(t), strings.ToUpper(randomUUID(t))} {
		view := wantData[featureflags.FlagView](t, e.json("operator", http.MethodDelete, base+id, ""), http.StatusOK)
		if view.Key != releaseKey || view.GlobalOverride == nil || !view.GlobalOverride.Enabled || len(view.TenantOverrides) != 0 {
			t.Fatalf("view = %+v, want the flag unchanged with its global override", view)
		}
	}
	wantError(t, e.json("operator", http.MethodDelete, base+"not-a-uuid", ""), http.StatusNotFound, api.CodeResourceNotFound)
	wantError(t, e.json("operator", http.MethodDelete, flagsPath+"/flag_ghost/tenants/"+randomUUID(t), ""), http.StatusNotFound, notDefined)
}

func TestWithoutTenancySnapshotHoldsOnlyGlobalAndManifestLevels(t *testing.T) {
	e := aloneEnv(t)
	wantData[featureflags.FlagView](t, e.json("operator", http.MethodPut, flagsPath+"/"+releaseKey, enabledOn), http.StatusOK)
	// A tenant row written behind the routes (an earlier tenancy deployment)
	// must not reach a tenantless snapshot.
	other := randomUUID(t)
	_, err := e.probe.Pool.Exec(context.Background(),
		`INSERT INTO flg_tenant_overrides (tenant_id, flag_key, enabled) VALUES ($1, $2, $3)`, other, opsKey, false)
	if err != nil {
		t.Fatalf("seed tenant override: %v", err)
	}

	snapshot := e.probe.Evaluator.Snapshot(context.Background(), nil)

	want := map[featureflag.Key]bool{featureflag.Key(releaseKey): true, featureflag.Key(opsKey): true}
	if len(snapshot) != len(want) {
		t.Fatalf("snapshot = %v, want %v", snapshot, want)
	}
	for key, enabled := range want {
		if snapshot[key] != enabled {
			t.Errorf("snapshot[%s] = %v, want %v", key, snapshot[key], enabled)
		}
	}
}
