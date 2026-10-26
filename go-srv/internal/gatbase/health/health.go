// Package health exposes the liveness probe of the service.
//
// Readiness (GET /health/ready) depends on the database and lands with the
// first module that owns a connection pool.
package health

import "net/http"

// Register mounts the liveness probe on mux.
func Register(mux *http.ServeMux) {
	if mux == nil {
		panic("health: Register called with a nil mux")
	}
	mux.HandleFunc("GET /health/live", live)
}

// live reports that the process is up and serving HTTP.
func live(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
