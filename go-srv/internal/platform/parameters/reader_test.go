package parameters_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/config"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/coretest"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/parameter"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/parameters"
)

// probeKey is the parameter the probe module reads.
const probeKey = "export.max_rows"

// probeValue is what the generic probe route answers.
type probeValue struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

// probeModule is a product-vertical-shaped module: it reads a parameter only
// through Host.Parameters, never through a table, and answers the contract's
// not-found when the parameter is absent.
func probeModule() core.Module {
	return core.Module{
		ID: "probe",
		Mount: func(_ context.Context, h *core.Host) error {
			session := access.Policy{Kind: access.KindSession}

			h.Handle("GET /api/v1/probe/export-max-rows", session, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				value, found, err := h.Parameters().Get(r.Context(), probeKey)
				if err != nil {
					h.API().Fail(w, r, err)
					return
				}
				if !found {
					h.API().Fail(w, r, api.NotFound())
					return
				}
				number, err := value.Number()
				if err != nil {
					h.API().Fail(w, r, err)
					return
				}
				h.API().Data(w, r, http.StatusOK, map[string]float64{"maxRows": number})
			}))

			h.Handle("GET /api/v1/probe/parameter/{key}", session, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				value, found, err := h.Parameters().Get(r.Context(), r.PathValue("key"))
				if err != nil {
					h.API().Fail(w, r, err)
					return
				}
				if !found {
					h.API().Fail(w, r, api.NotFound())
					return
				}
				h.API().Data(w, r, http.StatusOK, probeValue{Type: string(value.Type()), Value: value.Raw()})
			}))
			return nil
		},
	}
}

// probeReader is the reader a vertical writes for its own tests: one number,
// everything else not found.
type probeReader struct {
	value float64
}

func (r *probeReader) Get(_ context.Context, key string) (parameter.Value, bool, error) {
	if key != probeKey {
		return parameter.Value{}, false, nil
	}
	raw := json.RawMessage(strconv.FormatFloat(r.value, 'f', -1, 64))
	value, err := parameter.NewValue(parameter.TypeNumber, raw)
	if err != nil {
		return parameter.Value{}, false, err
	}
	return value, true, nil
}

// newProbeEnv is the probe module behind the real chain, with the parameters
// module when withModule is true.
func newProbeEnv(t testing.TB, withModule bool, opts coretest.Options) *env {
	t.Helper()
	modules := []core.Module{probeModule()}
	if withModule {
		modules = append([]core.Module{parameters.Module()}, modules...)
	}
	return newEnvWith(t, nil, opts, modules...)
}

// TestReaderResolvesTenantOverrideThenGlobalValue is the precedence contract
// over a real database: the tenant's override, else the global value of the
// definition, and never another tenant's override.
func TestReaderResolvesTenantOverrideThenGlobalValue(t *testing.T) {
	e := newProbeEnv(t, true, coretest.Options{Database: true})
	wantData[parameters.View](t, e.json("operator", http.MethodPost, "/api/v1/parameters", numberDefinition), http.StatusCreated)

	requireProbe(t, e, "tenantless", "50", "the global value of a tenantless read")

	wantData[parameters.View](t, e.json("adminA", http.MethodPut, "/api/v1/parameters/"+probeKey+"/tenant-override", `{"value":200}`), http.StatusOK)

	requireProbe(t, e, "adminA", "200", "the override of the active tenant")
	requireProbe(t, e, "adminB", "50", "the global value, untouched by another tenant's override")
	requireProbe(t, e, "tenantless", "50", "the global value for a session with no tenant")

	rows := wantData[map[string]float64](t, e.json("adminA", http.MethodGet, "/api/v1/probe/export-max-rows", ""), http.StatusOK)
	if rows["maxRows"] != 200 {
		t.Fatalf("maxRows = %v, want 200", rows["maxRows"])
	}

	wantData[parameters.View](t, e.json("adminA", http.MethodDelete, "/api/v1/parameters/"+probeKey+"/tenant-override", ""), http.StatusOK)
	requireProbe(t, e, "adminA", "50", "the global value after the override was cleared")
}

// TestReaderAnswersEveryStoredType reads one parameter of each type the module
// stores through the port.
func TestReaderAnswersEveryStoredType(t *testing.T) {
	e := newProbeEnv(t, true, coretest.Options{Database: true})
	definitions := []struct {
		key  string
		body string
		want probeValue
	}{
		{"probe.text", `{"key":"probe.text","valueType":"string","defaultValue":"hello"}`, probeValue{Type: "string", Value: json.RawMessage(`"hello"`)}},
		{"probe.rows", `{"key":"probe.rows","valueType":"number","defaultValue":-0.25}`, probeValue{Type: "number", Value: json.RawMessage(`-0.25`)}},
		{"probe.enabled", `{"key":"probe.enabled","valueType":"boolean","defaultValue":true}`, probeValue{Type: "boolean", Value: json.RawMessage(`true`)}},
		{"probe.limits", `{"key":"probe.limits","valueType":"json","defaultValue":{"a":[1,2]}}`, probeValue{Type: "json", Value: json.RawMessage(`{"a":[1,2]}`)}},
	}
	for _, definition := range definitions {
		wantData[parameters.View](t, e.json("operator", http.MethodPost, "/api/v1/parameters", definition.body), http.StatusCreated)
	}
	for _, definition := range definitions {
		got := wantData[probeValue](t, e.json("adminA", http.MethodGet, "/api/v1/probe/parameter/"+definition.key, ""), http.StatusOK)
		if got.Type != definition.want.Type || string(got.Value) != string(definition.want.Value) {
			t.Errorf("%s = %s %s, want %s %s", definition.key, got.Type, got.Value, definition.want.Type, definition.want.Value)
		}
	}
}

// TestAVerticalWithoutAReaderAnswersNotFound is the no-reader path: the probe's
// port is parameter.NoReader, so an absent parameter is the contract's
// not-found and never a 500 or a panic.
func TestAVerticalWithoutAReaderAnswersNotFound(t *testing.T) {
	e := newProbeEnv(t, false, coretest.Options{})
	wantError(t, e.json("adminA", http.MethodGet, "/api/v1/probe/export-max-rows", ""), http.StatusNotFound, api.CodeResourceNotFound)
	wantError(t, e.json("adminA", http.MethodGet, "/api/v1/probe/parameter/some.key", ""), http.StatusNotFound, api.CodeResourceNotFound)
}

// TestCoretestForwardsAProvidedReader proves the seam: a vertical test that
// supplies its own reader through coretest.Options reads it through the Host.
func TestCoretestForwardsAProvidedReader(t *testing.T) {
	e := newProbeEnv(t, false, coretest.Options{Parameters: &probeReader{value: 7}})
	rows := wantData[map[string]float64](t, e.json("adminA", http.MethodGet, "/api/v1/probe/export-max-rows", ""), http.StatusOK)
	if rows["maxRows"] != 7 {
		t.Fatalf("maxRows = %v, want 7", rows["maxRows"])
	}
	wantError(t, e.json("adminA", http.MethodGet, "/api/v1/probe/parameter/other.key", ""), http.StatusNotFound, api.CodeResourceNotFound)
}

// TestBuildRejectsAProvidedReaderAndTheModule follows core.Options: a
// pre-provided port plus a module providing the same port fails startup.
func TestBuildRejectsAProvidedReaderAndTheModule(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://nobody@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatalf("lazy pool: %v", err)
	}
	t.Cleanup(pool.Close)
	app, err := core.Build(context.Background(), core.Options{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config:     config.MapSource(map[string]string{"APP_PUBLIC_BASE_URL": "http://localhost:10120"}),
		Pool:       pool,
		Parameters: &probeReader{value: 7},
	}, []core.Module{parameters.Module()})
	if app != nil {
		_ = app.Close(context.Background())
		t.Fatal("Build accepted a reader and the module that provides one")
	}
	if err == nil || !strings.Contains(err.Error(), "parameter reader is already provided") {
		t.Fatalf("err = %v, want the duplicate port problem", err)
	}
}

// TestReaderFailsOnAStoredValueOfAnotherType covers a row written outside the
// module: the key exists but its value does not decode, so the port answers an
// error and not a value of the wrong type.
func TestReaderFailsOnAStoredValueOfAnotherType(t *testing.T) {
	pool := parameters.NewTestPool(t)
	_, err := pool.Exec(context.Background(),
		`INSERT INTO sys_parameters (key, value_type, default_value) VALUES ($1, 'number', '"not a number"')`, probeKey)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	reader := parameters.NewReader(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))

	value, found, err := reader.Get(context.Background(), probeKey)
	if !errors.Is(err, parameter.ErrInvalidValue) {
		t.Fatalf("err = %v, want ErrInvalidValue", err)
	}
	if found || value.Type() != "" {
		t.Fatalf("got %+v found, want the zero value", value)
	}
}

// TestReaderReportsAStoreFailure proves a failed query is an error and never a
// not-found: a reader that answered a broken pool not found would turn a 500
// into a 404 for every vertical reading through the port.
func TestReaderReportsAStoreFailure(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://nobody@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatalf("lazy pool: %v", err)
	}
	pool.Close()
	reader := parameters.NewReader(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))

	value, found, err := reader.Get(context.Background(), probeKey)
	if err == nil {
		t.Fatal("Get answered a failed query with no error")
	}
	if found {
		t.Fatalf("found = true on a failed query: %v", err)
	}
	if value.Type() != "" {
		t.Fatalf("value = %+v, want the zero value", value)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("a failed query must not read as a missing row: %v", err)
	}
	if !strings.Contains(err.Error(), "read parameter") {
		t.Fatalf("err = %v, want the read failure", err)
	}
}

// TestReaderAnswersNotFoundForAnUnknownOrMalformedKey reads without a tenant:
// the definition does not exist, so the port answers not found and no error.
func TestReaderAnswersNotFoundForAnUnknownOrMalformedKey(t *testing.T) {
	pool := parameters.NewTestPool(t)
	reader := parameters.NewReader(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, key := range []string{"no.such.key", "Bad Key", ""} {
		value, found, err := reader.Get(context.Background(), key)
		if err != nil {
			t.Fatalf("%q: err = %v, want none", key, err)
		}
		if found || value.Type() != "" {
			t.Fatalf("%q: got %+v found, want not found", key, value)
		}
	}
}

// requireProbe asserts the generic probe route answers the parameter's raw
// value for actor.
func requireProbe(t testing.TB, e *env, actor, want, what string) {
	t.Helper()
	got := wantData[probeValue](t, e.json(actor, http.MethodGet, "/api/v1/probe/parameter/"+probeKey, ""), http.StatusOK)
	if got.Type != "number" || string(got.Value) != want {
		t.Fatalf("%s: got %s %s, want the number %s", what, got.Type, got.Value, want)
	}
}
