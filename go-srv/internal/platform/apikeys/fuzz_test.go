package apikeys

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/coretest"
)

// FuzzAPIKeyHeader sends arbitrary X-API-Key values to a key-accepting route:
// no value may authenticate (no key exists) or crash the server.
func FuzzAPIKeyHeader(f *testing.F) {
	handler := coretest.New(f, []core.Module{Module()}, coretest.Options{Database: true})
	for _, value := range []string{"", " ", "ak_live_", "ak_live_" + strings.Repeat("a", 16) + "_", "\x00", "a\nb", "\xff\xfe"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/api-keys", nil)
		req.Header.Set(apiKeyHeader, value)
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
	valid, err := newAPIKey()
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
		prefix, secret, ok := splitAPIKey(value)

		isFormat := pattern.MatchString(value) && len(value) == 77
		if ok != isFormat {
			t.Fatalf("splitAPIKey(%q) ok = %v, format match = %v", value, ok, isFormat)
		}
		if ok && (prefix != value[:24] || secret != value[25:] || len(secret) != 52) {
			t.Fatalf("splitAPIKey(%q) = %q, %q", value, prefix, secret)
		}
		if !ok && (prefix != "" || secret != "") {
			t.Fatalf("a rejected value leaked parts: %q, %q", prefix, secret)
		}
	})
}
