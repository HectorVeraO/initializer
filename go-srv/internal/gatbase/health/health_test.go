package health_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/health"
)

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	health.Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestLiveReturnsNoContent(t *testing.T) {
	server := newServer(t)

	response, err := http.Get(server.URL + "/health/live")
	if err != nil {
		t.Fatalf("GET /health/live: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
}

func TestLiveRejectsOtherMethods(t *testing.T) {
	server := newServer(t)

	response, err := http.Post(server.URL+"/health/live", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /health/live: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusMethodNotAllowed)
	}
}
