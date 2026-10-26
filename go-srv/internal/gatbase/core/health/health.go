// Package health holds the readiness registry and the /health/live and
// /health/ready probes. Only core mounts them.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const checkTimeout = time.Second

// Check is one readiness dependency.
type Check struct {
	// Name is snake_case, e.g. "postgres".
	Name string
	// Probe must honour its context's deadline.
	Probe func(ctx context.Context) error
}

// CheckResult is one evaluated check.
type CheckResult struct {
	Name string
	OK   bool
}

// Readiness collects checks and the serving flag.
type Readiness struct {
	mu      sync.Mutex
	checks  []Check
	serving bool
	started bool
}

// NewReadiness returns a registry that is not serving and has no checks.
func NewReadiness() *Readiness {
	return &Readiness{}
}

// Add registers a check. It panics on an empty name, a nil probe, or a call
// after serving started.
func (r *Readiness) Add(c Check) {
	if c.Name == "" || c.Probe == nil {
		panic("health: Add requires a name and a probe")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		panic("health: Add called after serving started")
	}
	r.checks = append(r.checks, c)
}

// SetServing sets the serving flag; the first true ends the registration of checks.
func (r *Readiness) SetServing(serving bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.serving = serving
	if serving {
		r.started = true
	}
}

// Serving reports the serving flag.
func (r *Readiness) Serving() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.serving
}

// Evaluate runs every check in parallel with a 1 s timeout each. ok is true
// when every check passed; results keep registration order. A probe that
// panics, errors or outlives its timeout fails.
func (r *Readiness) Evaluate(ctx context.Context) (ok bool, results []CheckResult) {
	r.mu.Lock()
	checks := make([]Check, len(r.checks))
	copy(checks, r.checks)
	r.mu.Unlock()

	results = make([]CheckResult, len(checks))
	var wg sync.WaitGroup
	for i, check := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = CheckResult{Name: check.Name, OK: runProbe(ctx, check)}
		}()
	}
	wg.Wait()

	ok = true
	for _, result := range results {
		if !result.OK {
			ok = false
		}
	}
	return ok, results
}

// runProbe returns after the timeout even when the probe ignores its context.
func runProbe(parent context.Context, check Check) bool {
	ctx, cancel := context.WithTimeout(parent, checkTimeout)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				done <- fmt.Errorf("health: probe %s panicked", check.Name)
			}
		}()
		done <- check.Probe(ctx)
	}()
	select {
	case err := <-done:
		return err == nil
	case <-ctx.Done():
		return false
	}
}

// Mount registers GET /health/live and GET /health/ready (core only). It
// panics on a nil mux or readiness.
func Mount(mux *http.ServeMux, r *Readiness) {
	if mux == nil || r == nil {
		panic("health: Mount requires a mux and a readiness")
	}
	mux.HandleFunc("GET /health/live", live)
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, req *http.Request) {
		ready(w, req, r)
	})
}

// live reports that the process is up and serving HTTP.
func live(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

type readyBody struct {
	Serving bool          `json:"serving"`
	Checks  []checkStatus `json:"checks"`
}

type checkStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func ready(w http.ResponseWriter, req *http.Request, r *Readiness) {
	serving := r.Serving()
	checksOK, results := r.Evaluate(req.Context())
	if serving && checksOK {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	body := readyBody{Serving: serving, Checks: make([]checkStatus, 0, len(results))}
	for _, result := range results {
		status := "pass"
		if !result.OK {
			status = "fail"
		}
		body.Checks = append(body.Checks, checkStatus{Name: result.Name, Status: status})
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write(encoded)
}
