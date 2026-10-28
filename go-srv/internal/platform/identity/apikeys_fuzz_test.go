package identity_test

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/coretest"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity"
)

// FuzzAPIKeyHeader sends arbitrary X-API-Key values to a key-accepting route:
// no value may authenticate (no key exists) or crash the server.
func FuzzAPIKeyHeader(f *testing.F) {
	seed := make([]byte, 32)
	_, err := rand.Read(seed)
	if err != nil {
		f.Fatalf("random: %v", err)
	}
	handler := coretest.New(f, []core.Module{identity.Module()}, coretest.Options{
		Database: true,
		Config:   map[string]string{"APP_SESSION_KEYS": "fuzz-1:" + base64.RawURLEncoding.EncodeToString(seed)},
	})
	for _, value := range []string{"", " ", "gbk_live_", "gbk_live_aaaaaaaaaaaaaaaa_", "\x00", "a\nb", "\xff\xfe"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenant", nil)
		req.Header.Set("X-API-Key", value)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		isSuccess := rec.Code == http.StatusOK
		isServerError := rec.Code >= http.StatusInternalServerError
		if isSuccess || isServerError {
			t.Fatalf("X-API-Key %q answered %d", value, rec.Code)
		}
	})
}
