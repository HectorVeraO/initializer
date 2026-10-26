package parameters_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/parameters"
)

const retryDefinition = `{"key":"retry.max_attempts","valueType":"number","minValue":1,"maxValue":10,"defaultValue":3,"description":"Maximum attempts","isSystemRequired":true}`

func TestCreateAcceptsEveryValueType(t *testing.T) {
	svc, _ := newService(t)
	p := globalPrincipal(t)
	cases := []struct {
		name string
		body string
	}{
		{"string", `{"key":"custom.banner_text","valueType":"string","defaultValue":"hello"}`},
		{"string with allowed", `{"key":"custom.mode","valueType":"string","allowedValues":["a","b"],"defaultValue":"b"}`},
		{"number at both bounds", `{"key":"custom.limit_min","valueType":"number","minValue":1,"maxValue":10,"defaultValue":1}`},
		{"number at max bound", `{"key":"custom.limit_max","valueType":"number","minValue":1,"maxValue":10,"defaultValue":10}`},
		{"number with equal bounds", `{"key":"custom.fixed","valueType":"number","minValue":4,"maxValue":4,"defaultValue":4}`},
		{"number allowed written 2.0", `{"key":"custom.steps","valueType":"number","allowedValues":[1,2,3],"defaultValue":2.0}`},
		{"number beyond 2^53", `{"key":"custom.big","valueType":"number","defaultValue":9007199254740993}`},
		{"json with a valid surrogate pair", `{"key":"custom.emoji","valueType":"json","defaultValue":"\ud83d\ude00"}`},
		{"boolean", `{"key":"custom.enabled","valueType":"boolean","defaultValue":false}`},
		{"json object", `{"key":"custom.theme","valueType":"json","defaultValue":{"a":[1,2,{"b":null}]}}`},
		{"json scalar", `{"key":"custom.scalar","valueType":"json","defaultValue":0}`},
		{"json large integer", `{"key":"custom.big_json","valueType":"json","defaultValue":123456789012345678901234567890}`},
		{"description of 500 characters", `{"key":"custom.described","valueType":"string","defaultValue":"x","description":"` + strings.Repeat("é", 500) + `"}`},
		{"key of 128 characters", `{"key":"` + strings.Repeat("a", 128) + `","valueType":"boolean","defaultValue":true}`},
		{"100 allowed values", `{"key":"custom.hundred","valueType":"number","allowedValues":` + manyEntries(100, true) + `,"defaultValue":99}`},
		{"unknown fields ignored", `{"key":"custom.extra","valueType":"boolean","defaultValue":true,"colour":"red"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view := mustCreate(t, svc, p, tc.body)
			if view.Source != "global" || view.EffectiveValue == nil || view.Key == "" {
				t.Fatalf("unexpected view %+v", view)
			}
			var sent struct {
				DefaultValue json.RawMessage `json:"defaultValue"`
			}
			if err := json.Unmarshal([]byte(tc.body), &sent); err != nil {
				t.Fatal(err)
			}
			if !sameJSON(t, sent.DefaultValue, view.DefaultValue) {
				t.Fatalf("default value = %s, sent %s", view.DefaultValue, sent.DefaultValue)
			}
		})
	}
}

// sameJSON reports whether two JSON texts hold the same value, numbers
// compared by their exact text.
func sameJSON(t testing.TB, a, b []byte) bool {
	t.Helper()
	decode := func(raw []byte) any {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
		return value
	}
	return reflect.DeepEqual(decode(a), decode(b))
}

func TestCreateReturnsTheContractView(t *testing.T) {
	svc, _ := newService(t)
	view := mustCreate(t, svc, globalPrincipal(t), retryDefinition)
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"key":"retry.max_attempts","valueType":"number","effectiveValue":3,"defaultValue":3,"minValue":1,"maxValue":10,"allowedValues":null,"description":"Maximum attempts","isSystemRequired":true,"source":"global"}`
	if string(encoded) != want {
		t.Fatalf("view:\n got  %s\n want %s", encoded, want)
	}
}

func TestCreateKeepsALargeIntegerAsSent(t *testing.T) {
	svc, _ := newService(t)
	p := globalPrincipal(t)
	mustCreate(t, svc, p, `{"key":"custom.big","valueType":"number","defaultValue":9007199254740993}`)
	page, err := svc.List(context.Background(), p, pageOf(20))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(page.Items[0].DefaultValue); got != "9007199254740993" {
		t.Fatalf("default value = %s", got)
	}
}

func TestCreateDuplicateKeyConflicts(t *testing.T) {
	svc, _ := newService(t)
	p := globalPrincipal(t)
	mustCreate(t, svc, p, retryDefinition)
	_, err := svc.Create(context.Background(), p, body(t, retryDefinition))
	apiErr := requireStatus(t, err, http.StatusConflict, parameters.CodeParameterAlreadyExists)
	if len(apiErr.Issues) != 0 {
		t.Fatalf("issues = %v", apiErr.Issues)
	}
}

func TestCreateStoresTheActorAndClockTime(t *testing.T) {
	svc, pool := newService(t)
	p := globalPrincipal(t)
	mustCreate(t, svc, p, retryDefinition)
	var updatedBy string
	var updatedAt, createdAt time.Time
	err := pool.QueryRow(context.Background(),
		"SELECT updated_by::text, updated_at, created_at FROM sys_parameters WHERE key = 'retry.max_attempts'").
		Scan(&updatedBy, &updatedAt, &createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if updatedBy != string(p.AccountID) || !updatedAt.Equal(fixedNow) || !createdAt.Equal(fixedNow) {
		t.Fatalf("updated_by=%s updated_at=%s created_at=%s", updatedBy, updatedAt, createdAt)
	}
}

func TestCreateByAnActorWithoutAnAccountStoresNullUpdatedBy(t *testing.T) {
	svc, pool := newService(t)
	keyActor := reqctx.Principal{Kind: reqctx.ActorAPIKey, ActorID: randomUUID(t), Role: reqctx.RoleAdmin}
	mustCreate(t, svc, keyActor, retryDefinition)
	var isNull bool
	err := pool.QueryRow(context.Background(),
		"SELECT updated_by IS NULL FROM sys_parameters WHERE key = 'retry.max_attempts'").Scan(&isNull)
	if err != nil {
		t.Fatal(err)
	}
	if !isNull {
		t.Fatal("updated_by is set for an actor without an account")
	}
}

func TestOverrideStoresTheActor(t *testing.T) {
	svc, pool := newService(t)
	tenant := randomUUID(t)
	p := principal(t, tenant)
	mustCreate(t, svc, p, retryDefinition)
	_, err := svc.SetOverride(context.Background(), p, "retry.max_attempts", body(t, `{"value":5}`))
	if err != nil {
		t.Fatal(err)
	}
	var updatedBy string
	err = pool.QueryRow(context.Background(),
		"SELECT updated_by::text FROM sys_parameter_overrides WHERE tenant_id = $1::uuid", tenant).Scan(&updatedBy)
	if err != nil {
		t.Fatal(err)
	}
	if updatedBy != string(p.AccountID) {
		t.Fatalf("updated_by = %s, want %s", updatedBy, p.AccountID)
	}
}

func TestOverrideResolutionPerTenant(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	tenantA, tenantB := randomUUID(t), randomUUID(t)
	admin := globalPrincipal(t)
	mustCreate(t, svc, admin, retryDefinition)

	view, err := svc.SetOverride(ctx, principal(t, tenantA), "retry.max_attempts", body(t, `{"value":7}`))
	if err != nil {
		t.Fatal(err)
	}
	if view.Source != "tenant" || string(view.EffectiveValue) != "7" || string(view.DefaultValue) != "3" {
		t.Fatalf("view after set = %+v", view)
	}
	cases := []struct {
		name       string
		who        string
		wantSource string
		wantValue  string
	}{
		{"tenant A", tenantA, "tenant", "7"},
		{"tenant B", tenantB, "global", "3"},
	}
	for _, tc := range cases {
		page, listErr := svc.List(ctx, principal(t, tc.who), pageOf(20))
		if listErr != nil {
			t.Fatal(listErr)
		}
		got := page.Items[0]
		if got.Source != tc.wantSource || string(got.EffectiveValue) != tc.wantValue {
			t.Errorf("%s: source=%s effective=%s", tc.name, got.Source, got.EffectiveValue)
		}
	}
	page, err := svc.List(ctx, admin, pageOf(20))
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].Source != "global" || string(page.Items[0].EffectiveValue) != "3" {
		t.Fatalf("no-tenant list = %+v", page.Items[0])
	}
}

func TestOverrideUpsertReplacesTheValue(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	p := principal(t, randomUUID(t))
	mustCreate(t, svc, p, retryDefinition)
	for _, value := range []string{"4", "9"} {
		view, err := svc.SetOverride(ctx, p, "retry.max_attempts", body(t, `{"value":`+value+`}`))
		if err != nil {
			t.Fatal(err)
		}
		if string(view.EffectiveValue) != value {
			t.Fatalf("effective = %s, want %s", view.EffectiveValue, value)
		}
	}
}

func TestClearOverrideNeverTouchesAnotherTenant(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()
	tenantA, tenantB := randomUUID(t), randomUUID(t)
	mustCreate(t, svc, globalPrincipal(t), retryDefinition)
	if _, err := svc.SetOverride(ctx, principal(t, tenantA), "retry.max_attempts", body(t, `{"value":7}`)); err != nil {
		t.Fatal(err)
	}
	view, err := svc.ClearOverride(ctx, principal(t, tenantB), "retry.max_attempts")
	if err != nil {
		t.Fatal(err)
	}
	if view.Source != "global" {
		t.Fatalf("B view source = %s", view.Source)
	}
	var count int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM sys_parameter_overrides WHERE tenant_id = $1::uuid", tenantA).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("tenant A rows = %d, err = %v", count, err)
	}
	page, err := svc.List(ctx, principal(t, tenantA), pageOf(20))
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].Source != "tenant" {
		t.Fatalf("A list source = %s", page.Items[0].Source)
	}
	// Clearing again as A is idempotent and removes only A's row.
	for i := 0; i < 2; i++ {
		view, err = svc.ClearOverride(ctx, principal(t, tenantA), "retry.max_attempts")
		if err != nil || view.Source != "global" {
			t.Fatalf("clear A: %+v, %v", view, err)
		}
	}
}

func TestSetOverrideValueChecks(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	p := principal(t, randomUUID(t))
	mustCreate(t, svc, p, retryDefinition)
	mustCreate(t, svc, p, `{"key":"custom.mode","valueType":"string","allowedValues":["a","b"],"defaultValue":"a"}`)
	rejected := []struct {
		name string
		key  string
		body string
		want string
	}{
		{"missing value", "retry.max_attempts", `{}`, "value:FIELD_REQUIRED"},
		{"null value", "retry.max_attempts", `{"value":null}`, "value:FIELD_REQUIRED"},
		{"wrong type", "retry.max_attempts", `{"value":"5"}`, "value:PARAMETER_TYPE_MISMATCH"},
		{"one step below min", "retry.max_attempts", `{"value":0.999}`, "value:PARAMETER_OUT_OF_RANGE"},
		{"one step above max", "retry.max_attempts", `{"value":10.001}`, "value:PARAMETER_OUT_OF_RANGE"},
		{"not allowed", "custom.mode", `{"value":"c"}`, "value:PARAMETER_VALUE_NOT_ALLOWED"},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.SetOverride(ctx, p, tc.key, body(t, tc.body))
			requireValidation(t, err, []string{tc.want})
		})
	}
	for _, value := range []string{"1", "10"} {
		if _, err := svc.SetOverride(ctx, p, "retry.max_attempts", body(t, `{"value":`+value+`}`)); err != nil {
			t.Errorf("value %s at a bound: %v", value, err)
		}
	}
}

func TestSetOverrideRejectedValueChangesNothing(t *testing.T) {
	svc, pool := newService(t)
	p := principal(t, randomUUID(t))
	mustCreate(t, svc, p, retryDefinition)
	_, err := svc.SetOverride(context.Background(), p, "retry.max_attempts", body(t, `{"value":99}`))
	requireValidation(t, err, []string{"value:PARAMETER_OUT_OF_RANGE"})
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM sys_parameter_overrides").Scan(&count); err != nil || count != 0 {
		t.Fatalf("override rows = %d, err = %v", count, err)
	}
}

func TestUnknownOrMalformedKeyIsNotFound(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	p := principal(t, randomUUID(t))
	keys := []string{"missing.key", "Bad Key", "", strings.Repeat("a", 129), "a'; DROP TABLE sys_parameters; --"}
	for _, key := range keys {
		_, err := svc.Replace(ctx, p, key, body(t, `{"defaultValue":1}`))
		requireStatus(t, err, http.StatusNotFound, api.CodeResourceNotFound)
		_, err = svc.Delete(ctx, p, key)
		requireStatus(t, err, http.StatusNotFound, api.CodeResourceNotFound)
		_, err = svc.SetOverride(ctx, p, key, body(t, `{"value":1}`))
		requireStatus(t, err, http.StatusNotFound, api.CodeResourceNotFound)
		_, err = svc.ClearOverride(ctx, p, key)
		requireStatus(t, err, http.StatusNotFound, api.CodeResourceNotFound)
	}
}

func TestOverrideCallsNeedAnActiveTenant(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	p := globalPrincipal(t)
	mustCreate(t, svc, p, retryDefinition)
	_, err := svc.SetOverride(ctx, p, "retry.max_attempts", body(t, `{"value":5}`))
	if err == nil {
		t.Fatal("SetOverride without a tenant succeeded")
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		t.Fatalf("want a plain error, got the API error %v", err)
	}
	_, err = svc.ClearOverride(ctx, p, "retry.max_attempts")
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("ClearOverride without a tenant: want a plain error, got %v", err)
	}
}

func TestDeleteProtectsSystemRequiredParameters(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()
	tenant := randomUUID(t)
	p := principal(t, tenant)
	mustCreate(t, svc, p, retryDefinition)
	if _, err := svc.SetOverride(ctx, p, "retry.max_attempts", body(t, `{"value":5}`)); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Delete(ctx, p, "retry.max_attempts")
	apiErr := requireStatus(t, err, http.StatusConflict, parameters.CodeSystemParameterProtected)
	if len(apiErr.Issues) != 0 {
		t.Fatalf("issues = %v", apiErr.Issues)
	}
	var definitions, overrides int
	if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM sys_parameters), (SELECT count(*) FROM sys_parameter_overrides)").Scan(&definitions, &overrides); err != nil {
		t.Fatal(err)
	}
	if definitions != 1 || overrides != 1 {
		t.Fatalf("definitions=%d overrides=%d, want 1 and 1", definitions, overrides)
	}
}

func TestDeleteCascadesOverridesAndReportsTheCount(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()
	admin := globalPrincipal(t)
	mustCreate(t, svc, admin, `{"key":"custom.banner_text","valueType":"string","defaultValue":"hi"}`)
	mustCreate(t, svc, admin, `{"key":"custom.other","valueType":"string","defaultValue":"hi"}`)
	for i := 0; i < 3; i++ {
		p := principal(t, randomUUID(t))
		if _, err := svc.SetOverride(ctx, p, "custom.banner_text", body(t, `{"value":"yo"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.SetOverride(ctx, principal(t, randomUUID(t)), "custom.other", body(t, `{"value":"yo"}`)); err != nil {
		t.Fatal(err)
	}
	deleted, err := svc.Delete(ctx, admin, "custom.banner_text")
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Key != "custom.banner_text" || deleted.DeletedOverrideCount != 3 {
		t.Fatalf("deleted = %+v", deleted)
	}
	encoded, _ := json.Marshal(deleted)
	if string(encoded) != `{"key":"custom.banner_text","deletedOverrideCount":3}` {
		t.Fatalf("wire = %s", encoded)
	}
	var left int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sys_parameter_overrides").Scan(&left); err != nil || left != 1 {
		t.Fatalf("remaining overrides = %d, err = %v", left, err)
	}
	_, err = svc.Delete(ctx, admin, "custom.banner_text")
	requireStatus(t, err, http.StatusNotFound, api.CodeResourceNotFound)
}

func TestReplaceRechecksExistingOverrides(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()
	admin := globalPrincipal(t)
	tenantA, tenantB := principal(t, randomUUID(t)), principal(t, randomUUID(t))
	mustCreate(t, svc, admin, `{"key":"retry.max_attempts","valueType":"number","minValue":1,"maxValue":10,"defaultValue":3}`)
	for _, set := range []struct {
		who   reqctx.Principal
		value string
	}{{tenantA, "8"}, {tenantB, "5"}} {
		if _, err := svc.SetOverride(ctx, set.who, "retry.max_attempts", body(t, `{"value":`+set.value+`}`)); err != nil {
			t.Fatal(err)
		}
	}

	_, err := svc.Replace(ctx, admin, "retry.max_attempts", body(t, `{"minValue":1,"maxValue":6,"defaultValue":3}`))
	apiErr := requireStatus(t, err, http.StatusConflict, parameters.CodeOverridesViolateConstraints)
	if len(apiErr.Issues) != 0 || apiErr.Message != "1 tenant override(s) would violate the new constraints" {
		t.Fatalf("issues=%v message=%q", apiErr.Issues, apiErr.Message)
	}
	var maxValue float64
	if err := pool.QueryRow(ctx, "SELECT max_value FROM sys_parameters WHERE key = 'retry.max_attempts'").Scan(&maxValue); err != nil || maxValue != 10 {
		t.Fatalf("max_value = %v, err = %v (must be unchanged)", maxValue, err)
	}

	_, err = svc.Replace(ctx, admin, "retry.max_attempts", body(t, `{"minValue":6,"maxValue":6,"defaultValue":6}`))
	apiErr = requireStatus(t, err, http.StatusConflict, parameters.CodeOverridesViolateConstraints)
	if apiErr.Message != "2 tenant override(s) would violate the new constraints" {
		t.Fatalf("message = %q, want the count 2", apiErr.Message)
	}

	view, err := svc.Replace(ctx, admin, "retry.max_attempts", body(t, `{"minValue":1,"maxValue":9,"defaultValue":4}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(view.DefaultValue) != "4" || *view.MaxValue != 9 || view.Source != "global" {
		t.Fatalf("view = %+v", view)
	}
}

func TestReplaceRechecksAllowedValues(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	p := principal(t, randomUUID(t))
	mustCreate(t, svc, p, `{"key":"custom.mode","valueType":"string","defaultValue":"a"}`)
	if _, err := svc.SetOverride(ctx, p, "custom.mode", body(t, `{"value":"zzz"}`)); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Replace(ctx, p, "custom.mode", body(t, `{"allowedValues":["a","b"],"defaultValue":"a"}`))
	requireStatus(t, err, http.StatusConflict, parameters.CodeOverridesViolateConstraints)
}

func TestReplaceShowsTheOperatorsOwnOverride(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	p := principal(t, randomUUID(t))
	mustCreate(t, svc, p, retryDefinition)
	if _, err := svc.SetOverride(ctx, p, "retry.max_attempts", body(t, `{"value":8}`)); err != nil {
		t.Fatal(err)
	}
	view, err := svc.Replace(ctx, p, "retry.max_attempts", body(t, `{"minValue":1,"maxValue":10,"defaultValue":2,"description":"new","isSystemRequired":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if view.Source != "tenant" || string(view.EffectiveValue) != "8" || string(view.DefaultValue) != "2" || view.Description != "new" {
		t.Fatalf("view = %+v", view)
	}
}

func TestReplaceClearsAbsentOptionalFields(t *testing.T) {
	svc, _ := newService(t)
	p := globalPrincipal(t)
	mustCreate(t, svc, p, `{"key":"custom.level","valueType":"number","minValue":1,"maxValue":10,"allowedValues":[1,2,3],"defaultValue":2,"description":"old"}`)
	view, err := svc.Replace(context.Background(), p, "custom.level", body(t, `{"defaultValue":5}`))
	if err != nil {
		t.Fatal(err)
	}
	if view.MinValue != nil || view.MaxValue != nil || view.AllowedValues != nil || view.Description != "" {
		t.Fatalf("view = %+v", view)
	}
}

func TestReplaceValidationMatrix(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	p := globalPrincipal(t)
	mustCreate(t, svc, p, retryDefinition)
	mustCreate(t, svc, p, `{"key":"custom.banner_text","valueType":"string","defaultValue":"hi"}`)
	mustCreate(t, svc, p, `{"key":"custom.enabled","valueType":"boolean","defaultValue":true}`)
	cases := []struct {
		name string
		key  string
		body string
		want []string
	}{
		{"missing default", "retry.max_attempts", `{}`, []string{"defaultValue:FIELD_REQUIRED"}},
		{"null for every field", "retry.max_attempts", `{"valueType":null,"defaultValue":null,"minValue":null,"maxValue":null,"allowedValues":null,"description":null,"isSystemRequired":null}`, []string{"defaultValue:FIELD_REQUIRED"}},
		{"valueType changed", "retry.max_attempts", `{"valueType":"string","defaultValue":3}`, []string{"valueType:PARAMETER_FIELD_IMMUTABLE"}},
		{"valueType unknown", "retry.max_attempts", `{"valueType":"date","defaultValue":3}`, []string{"valueType:FIELD_NOT_ALLOWED"}},
		{"valueType not a string", "retry.max_attempts", `{"valueType":1,"defaultValue":3}`, []string{"valueType:FIELD_TYPE_INVALID"}},
		{"isSystemRequired changed", "retry.max_attempts", `{"isSystemRequired":false,"defaultValue":3}`, []string{"isSystemRequired:PARAMETER_FIELD_IMMUTABLE"}},
		{"isSystemRequired not a boolean", "retry.max_attempts", `{"isSystemRequired":"x","defaultValue":3}`, []string{"isSystemRequired:FIELD_TYPE_INVALID"}},
		{"both immutables changed", "custom.banner_text", `{"valueType":"number","isSystemRequired":true,"defaultValue":"x"}`, []string{"valueType:PARAMETER_FIELD_IMMUTABLE", "isSystemRequired:PARAMETER_FIELD_IMMUTABLE"}},
		{"default of the stored type is checked", "custom.banner_text", `{"defaultValue":4}`, []string{"defaultValue:PARAMETER_TYPE_MISMATCH"}},
		{"default out of the new range", "retry.max_attempts", `{"minValue":1,"maxValue":10,"defaultValue":11}`, []string{"defaultValue:PARAMETER_OUT_OF_RANGE"}},
		{"min above max", "retry.max_attempts", `{"minValue":9,"maxValue":2,"defaultValue":3}`, []string{"maxValue:PARAMETER_CONSTRAINT_INVALID"}},
		{"min on a string", "custom.banner_text", `{"minValue":1,"defaultValue":"x"}`, []string{"minValue:PARAMETER_CONSTRAINT_INVALID"}},
		{"allowed on a boolean", "custom.enabled", `{"allowedValues":[true],"defaultValue":true}`, []string{"allowedValues:PARAMETER_CONSTRAINT_INVALID"}},
		{"allowed entries", "custom.banner_text", `{"allowedValues":["a",2,"a"],"defaultValue":"a"}`, []string{"allowedValues[1]:PARAMETER_TYPE_MISMATCH", "allowedValues[2]:PARAMETER_CONSTRAINT_INVALID"}},
		{"default not allowed", "custom.banner_text", `{"allowedValues":["a"],"defaultValue":"b"}`, []string{"defaultValue:PARAMETER_VALUE_NOT_ALLOWED"}},
		{"description too long", "custom.banner_text", `{"defaultValue":"x","description":"` + strings.Repeat("x", 501) + `"}`, []string{"description:FIELD_TOO_LONG"}},
		{"description not a string", "custom.banner_text", `{"defaultValue":"x","description":false}`, []string{"description:FIELD_TYPE_INVALID"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Replace(ctx, p, tc.key, body(t, tc.body))
			requireValidation(t, err, tc.want)
		})
	}
	t.Run("matching immutables are accepted and the body key is ignored", func(t *testing.T) {
		_, err := svc.Replace(ctx, p, "custom.banner_text", body(t, `{"key":"other.key","valueType":"string","isSystemRequired":false,"defaultValue":"x"}`))
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestReplaceUnknownKeyIsNotFoundBeforeValidation(t *testing.T) {
	svc, _ := newService(t)
	_, err := svc.Replace(context.Background(), globalPrincipal(t), "missing.key", body(t, `{}`))
	requireStatus(t, err, http.StatusNotFound, api.CodeResourceNotFound)
}

func TestReplaceRejectedLeavesTheRowUnchanged(t *testing.T) {
	svc, pool := newService(t)
	p := globalPrincipal(t)
	mustCreate(t, svc, p, retryDefinition)
	_, err := svc.Replace(context.Background(), p, "retry.max_attempts", body(t, `{"defaultValue":99,"maxValue":10}`))
	requireValidation(t, err, []string{"defaultValue:PARAMETER_OUT_OF_RANGE"})
	var def string
	if err := pool.QueryRow(context.Background(), "SELECT default_value::text FROM sys_parameters").Scan(&def); err != nil || def != "3" {
		t.Fatalf("default = %s, err = %v", def, err)
	}
}

func TestListPaging(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	p := globalPrincipal(t)
	for i := 0; i < 25; i++ {
		mustCreate(t, svc, p, fmt.Sprintf(`{"key":"custom.k%02d","valueType":"boolean","defaultValue":true}`, i))
	}
	first, err := svc.List(ctx, p, api.PageQuery{Page: 1, Size: 20, SortField: "key"})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 20 || first.TotalItems != 25 || first.Page != 1 || first.Size != 20 || first.Items[0].Key != "custom.k00" {
		t.Fatalf("first page: len=%d total=%d first=%s", len(first.Items), first.TotalItems, first.Items[0].Key)
	}
	second, err := svc.List(ctx, p, api.PageQuery{Page: 2, Size: 20, SortField: "key"})
	if err != nil || len(second.Items) != 5 || second.Items[4].Key != "custom.k24" {
		t.Fatalf("second page: %+v, %v", second, err)
	}
	desc, err := svc.List(ctx, p, api.PageQuery{Page: 1, Size: 3, SortField: "key", SortDesc: true})
	if err != nil || desc.Items[0].Key != "custom.k24" || desc.Items[2].Key != "custom.k22" {
		t.Fatalf("descending: %+v, %v", desc, err)
	}
	beyond, err := svc.List(ctx, p, api.PageQuery{Page: 9, Size: 20, SortField: "key"})
	if err != nil || beyond.Items == nil || len(beyond.Items) != 0 || beyond.TotalItems != 25 {
		t.Fatalf("beyond: %+v, %v", beyond, err)
	}
	all, err := svc.List(ctx, p, pageOf(100))
	if err != nil || len(all.Items) != 25 {
		t.Fatalf("size 100: %d items, %v", len(all.Items), err)
	}
}

func TestListSortsKeysByByteOrder(t *testing.T) {
	svc, _ := newService(t)
	p := globalPrincipal(t)
	for _, key := range []string{"a_b", "a.b", "ab", "a1"} {
		mustCreate(t, svc, p, `{"key":"`+key+`","valueType":"boolean","defaultValue":true}`)
	}
	page, err := svc.List(context.Background(), p, pageOf(20))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, item := range page.Items {
		got = append(got, item.Key)
	}
	if strings.Join(got, " ") != "a.b a1 a_b ab" {
		t.Fatalf("order = %v", got)
	}
}

func TestListRejectsAnUnsupportedSort(t *testing.T) {
	svc, _ := newService(t)
	_, err := svc.List(context.Background(), globalPrincipal(t), api.PageQuery{Page: 1, Size: 20, SortField: "description"})
	if err == nil {
		t.Fatal("want an error")
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		t.Fatalf("want a plain error for a caller bug, got the API error %v", err)
	}
	if !strings.Contains(err.Error(), "sorted by key") {
		t.Fatalf("error = %q, want it to name the key sort", err)
	}
}

func TestLogsEventsWithoutValues(t *testing.T) {
	svc, _, logs := newLoggedService(t)
	ctx := context.Background()
	p := principal(t, randomUUID(t))
	mustCreate(t, svc, p, `{"key":"custom.banner_text","valueType":"string","defaultValue":"secret-ish-default"}`)
	if _, err := svc.SetOverride(ctx, p, "custom.banner_text", body(t, `{"value":"secret-ish-override"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Replace(ctx, p, "custom.banner_text", body(t, `{"defaultValue":"secret-ish-default"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ClearOverride(ctx, p, "custom.banner_text"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Delete(ctx, p, "custom.banner_text"); err != nil {
		t.Fatal(err)
	}
	output := logs.String()
	for _, event := range []string{"parameter_created", "parameter_override_set", "parameter_replaced", "parameter_override_cleared", "parameter_deleted"} {
		if !strings.Contains(output, "msg="+event) {
			t.Errorf("missing event %s in:\n%s", event, output)
		}
	}
	if !strings.Contains(output, "deleted_override_count=0") || !strings.Contains(output, "parameter_key=custom.banner_text") {
		t.Errorf("missing attributes in:\n%s", output)
	}
	if strings.Contains(output, "secret-ish") {
		t.Errorf("a value was logged:\n%s", output)
	}
}

func TestJSONParametersRoundTripNestedValues(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	p := principal(t, randomUUID(t))
	mustCreate(t, svc, p, `{"key":"custom.theme","valueType":"json","defaultValue":{"a":1}}`)
	view, err := svc.SetOverride(ctx, p, "custom.theme", body(t, `{"value":[1,{"b":"é"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(view.EffectiveValue) != `[1,{"b":"é"}]` {
		t.Fatalf("effective = %s", view.EffectiveValue)
	}
	_, err = svc.SetOverride(ctx, p, "custom.theme", body(t, `{"value":"a\u0000b"}`))
	requireValidation(t, err, []string{"value:PARAMETER_TYPE_MISMATCH"})
}

func TestJSONParameterAcceptsDeeplyNestedValues(t *testing.T) {
	svc, _ := newService(t)
	nested := strings.Repeat("[", 5000) + strings.Repeat("]", 5000)
	view := mustCreate(t, svc, globalPrincipal(t), `{"key":"custom.deep","valueType":"json","defaultValue":`+nested+`}`)
	if string(view.DefaultValue) != nested {
		t.Fatal("nested value changed")
	}
}
