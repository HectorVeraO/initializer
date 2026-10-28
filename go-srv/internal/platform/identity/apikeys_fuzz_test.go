package identity_test

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
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
	for _, value := range []string{"", " ", "ak_live_", "ak_live_" + strings.Repeat("a", 16) + "_", "\x00", "a\nb", "\xff\xfe"} {
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

// FuzzSplitAPIKey checks that the strict parser never panics and accepts
// exactly the strings of the key format: 77 characters, a 24 character public
// prefix, a divider and a 52 character secret.
func FuzzSplitAPIKey(f *testing.F) {
	pattern := regexp.MustCompile(`^ak_live_[a-z2-7]{16}_[a-z2-7]{52}$`)
	valid, err := identity.NewAPIKey()
	if err != nil {
		f.Fatalf("new key: %v", err)
	}
	for _, value := range []string{
		"", valid, valid + "\n", strings.ToUpper(valid), valid[:76], valid + "a", "ak_test_" + valid[8:],
		valid[:24] + "a" + valid[25:], "\x00", "\xff\xfe",
	} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		prefix, secret, ok := identity.SplitAPIKey(value)

		isFormat := pattern.MatchString(value) && len(value) == 77
		if ok != isFormat {
			t.Fatalf("SplitAPIKey(%q) ok = %v, format match = %v", value, ok, isFormat)
		}
		if ok && (prefix != value[:24] || secret != value[25:] || len(secret) != 52) {
			t.Fatalf("SplitAPIKey(%q) = %q, %q", value, prefix, secret)
		}
		if !ok && (prefix != "" || secret != "") {
			t.Fatalf("a rejected value leaked parts: %q, %q", prefix, secret)
		}
	})
}
