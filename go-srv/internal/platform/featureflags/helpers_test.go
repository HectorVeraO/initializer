package featureflags_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/coretest"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/featureflag"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/featureflags"
)

const (
	releaseKey = "new_settings_ui"
	opsKey     = "example_outbound_enabled"

	twoFlagManifest = `flags:
  - key: new_settings_ui
    type: release
    default: false
    expires_at: "2099-12-31"
    description: Synthetic release flag
  - key: example_outbound_enabled
    type: ops
    default: true
    description: Synthetic kill switch
`
)

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// records returns every JSON log record whose msg equals message.
func (b *syncBuffer) records(t testing.TB, message string) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		err := json.Unmarshal([]byte(line), &record)
		if err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		if record["msg"] == message {
			out = append(out, record)
		}
	}
	return out
}

func newLogger() (*slog.Logger, *syncBuffer) {
	sink := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return logger, sink
}

func writeManifest(t testing.TB, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "flags.yaml")
	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

func randomUUID(t testing.TB) string {
	t.Helper()
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		t.Fatalf("random: %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// env is the module running inside coretest over a private database.
type env struct {
	Evaluator featureflag.Evaluator
	Pool      *pgxpool.Pool
	Handler   http.Handler
	Logs      *syncBuffer
	Tenant    reqctx.TenantID
}

// newEnv boots the real module over manifest through coretest. A test-only
// module captures the port and the pool and serves GET /api/v1/probe/{key}
// behind a session of a principal with a generated active tenant.
func newEnv(t testing.TB, manifest string) *env {
	t.Helper()
	logger, sink := newLogger()
	tenant := reqctx.TenantID(randomUUID(t))
	account := randomUUID(t)
	principal := reqctx.Principal{
		Kind:      reqctx.ActorAccount,
		ActorID:   account,
		AccountID: reqctx.AccountID(account),
		TenantID:  &tenant,
		Role:      reqctx.RoleAdmin,
	}
	e := &env{Logs: sink, Tenant: tenant}
	probe := core.Module{ID: "probe", Mount: func(_ context.Context, h *core.Host) error {
		e.Evaluator = h.FlagEvaluator()
		e.Pool = h.Pool()
		h.Handle("GET /api/v1/probe/{key}", access.Policy{Kind: access.KindSession}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			enabled := e.Evaluator.Enabled(r.Context(), featureflag.Key(r.PathValue("key")))
			if enabled {
				_, _ = w.Write([]byte("on"))
				return
			}
			_, _ = w.Write([]byte("off"))
		}))
		return nil
	}}
	e.Handler = coretest.New(t, []core.Module{featureflags.Module(), probe}, coretest.Options{
		Database:        true,
		Config:          map[string]string{"APP_FLAGS_FILE": writeManifest(t, manifest)},
		SessionResolver: coretest.Session(principal),
		Logger:          logger,
	})
	return e
}

// enabledForTenant evaluates key through an authenticated request, so the
// tenant reaches the evaluator exactly as it does in production.
func (e *env) enabledForTenant(t testing.TB, key string) bool {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/probe/"+key, nil)
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "x"})
	recorder := httptest.NewRecorder()
	e.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("probe %s = %d %s", key, recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String() == "on"
}

// exec runs SQL on the test database, the boundary the tests arrange data at.
func (e *env) exec(t testing.TB, sql string, args ...any) {
	t.Helper()
	_, err := e.Pool.Exec(context.Background(), sql, args...)
	if err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func (e *env) setGlobal(t testing.TB, key string, enabled bool) {
	t.Helper()
	e.exec(t, `INSERT INTO flg_overrides (flag_key, enabled) VALUES ($1, $2)
		ON CONFLICT (flag_key) DO UPDATE SET enabled = EXCLUDED.enabled`, key, enabled)
}

func (e *env) setTenant(t testing.TB, tenant, key string, enabled bool) {
	t.Helper()
	e.exec(t, `INSERT INTO flg_tenant_overrides (tenant_id, flag_key, enabled) VALUES ($1, $2, $3)
		ON CONFLICT (tenant_id, flag_key) DO UPDATE SET enabled = EXCLUDED.enabled`, tenant, key, enabled)
}
