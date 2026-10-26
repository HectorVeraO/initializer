package health_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/health"
)

func newServer(t *testing.T, r *health.Readiness) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	health.Mount(mux, r)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func get(t *testing.T, url string) (int, string, http.Header) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return response.StatusCode, string(body), response.Header
}

func passing(name string) health.Check {
	return health.Check{Name: name, Probe: func(context.Context) error { return nil }}
}

func failing(name string) health.Check {
	return health.Check{Name: name, Probe: func(context.Context) error { return errors.New("secret detail") }}
}

func TestLiveReturnsNoContent(t *testing.T) {
	server := newServer(t, health.NewReadiness())

	status, _, _ := get(t, server.URL+"/health/live")

	if status != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", status, http.StatusNoContent)
	}
}

func TestLiveRejectsOtherMethods(t *testing.T) {
	server := newServer(t, health.NewReadiness())

	response, err := http.Post(server.URL+"/health/live", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /health/live: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestReadyIsNoContentWhenServingAndChecksPass(t *testing.T) {
	readiness := health.NewReadiness()
	readiness.Add(passing("postgres"))
	readiness.SetServing(true)
	server := newServer(t, readiness)

	status, body, _ := get(t, server.URL+"/health/ready")

	if status != http.StatusNoContent || body != "" {
		t.Fatalf("status = %d body = %q, want 204 and no body", status, body)
	}
}

func TestReadyIsUnavailableWhenNotServing(t *testing.T) {
	readiness := health.NewReadiness()
	readiness.Add(passing("postgres"))
	server := newServer(t, readiness)

	status, body, header := get(t, server.URL+"/health/ready")

	want := `{"serving":false,"checks":[{"name":"postgres","status":"pass"}]}`
	if status != http.StatusServiceUnavailable || body != want {
		t.Fatalf("status = %d body = %s, want 503 %s", status, body, want)
	}
	if header.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q", header.Get("Content-Type"))
	}
	if header.Get("X-Trace-Id") != "" {
		t.Fatal("health answers must not carry X-Trace-Id")
	}
}

func TestReadyReportsFailedCheckWithoutErrorText(t *testing.T) {
	readiness := health.NewReadiness()
	readiness.Add(passing("cache"))
	readiness.Add(failing("postgres"))
	readiness.SetServing(true)
	server := newServer(t, readiness)

	status, body, _ := get(t, server.URL+"/health/ready")

	want := `{"serving":true,"checks":[{"name":"cache","status":"pass"},{"name":"postgres","status":"fail"}]}`
	if status != http.StatusServiceUnavailable || body != want {
		t.Fatalf("status = %d body = %s, want 503 %s", status, body, want)
	}
	if strings.Contains(body, "secret detail") {
		t.Fatal("probe error text leaked into the body")
	}
}

func TestReadyWithNoChecksListsEmptyArray(t *testing.T) {
	server := newServer(t, health.NewReadiness())

	_, body, _ := get(t, server.URL+"/health/ready")

	if body != `{"serving":false,"checks":[]}` {
		t.Fatalf("body = %s", body)
	}
}

func TestEvaluateFailsProbeThatOutlivesItsTimeout(t *testing.T) {
	readiness := health.NewReadiness()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	readiness.Add(health.Check{Name: "slow", Probe: func(context.Context) error {
		<-release
		return nil
	}})
	readiness.Add(passing("fast"))

	start := time.Now()
	ok, results := readiness.Evaluate(context.Background())
	elapsed := time.Since(start)

	if ok {
		t.Fatal("Evaluate reported ok with a blocked probe")
	}
	if elapsed < 900*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("Evaluate took %v, want about 1s", elapsed)
	}
	if len(results) != 2 || results[0].OK || !results[1].OK {
		t.Fatalf("results = %+v", results)
	}
}

func TestEvaluateRunsChecksInParallel(t *testing.T) {
	readiness := health.NewReadiness()
	const total = 3
	var arrived atomic.Int32
	allArrived := make(chan struct{})
	for _, name := range []string{"a", "b", "c"} {
		readiness.Add(health.Check{Name: name, Probe: func(ctx context.Context) error {
			if arrived.Add(1) == total {
				close(allArrived)
			}
			// Sequential execution never lets the others arrive, so the probe
			// times out and fails.
			select {
			case <-allArrived:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}})
	}

	ok, results := readiness.Evaluate(context.Background())

	if !ok {
		t.Fatalf("checks did not run in parallel: %+v", results)
	}
}

func TestEvaluateTreatsPanickingProbeAsFailure(t *testing.T) {
	readiness := health.NewReadiness()
	readiness.Add(health.Check{Name: "boom", Probe: func(context.Context) error { panic("boom") }})

	ok, results := readiness.Evaluate(context.Background())

	if ok || len(results) != 1 || results[0].OK {
		t.Fatalf("ok = %v results = %+v", ok, results)
	}
}

func TestMisuseMustPanic(t *testing.T) {
	served := health.NewReadiness()
	served.SetServing(true)
	served.SetServing(false)
	cases := map[string]func(){
		"empty name":               func() { health.NewReadiness().Add(health.Check{Probe: func(context.Context) error { return nil }}) },
		"nil probe":                func() { health.NewReadiness().Add(health.Check{Name: "x"}) },
		"add after serving":        func() { served.Add(passing("late")) },
		"mount with nil mux":       func() { health.Mount(nil, health.NewReadiness()) },
		"mount with nil readiness": func() { health.Mount(http.NewServeMux(), nil) },
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				recovered := recover()
				message, isString := recovered.(string)
				if !isString || !strings.HasPrefix(message, "health: ") {
					t.Fatalf("panic = %v, want a health: message", recovered)
				}
			}()
			fn()
		})
	}
}

func TestServingFlagRoundTrip(t *testing.T) {
	readiness := health.NewReadiness()
	if readiness.Serving() {
		t.Fatal("a new registry must not be serving")
	}
	readiness.SetServing(true)
	if !readiness.Serving() {
		t.Fatal("SetServing(true) not visible")
	}
	readiness.SetServing(false)
	if readiness.Serving() {
		t.Fatal("SetServing(false) not visible")
	}
}
