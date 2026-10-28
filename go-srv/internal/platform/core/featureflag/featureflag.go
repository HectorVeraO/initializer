// Package featureflag is the core seam for feature-flag evaluation.
package featureflag

import (
	"context"
	"net/http"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

// Key names a declared flag.
type Key string

// Evaluator: tenant override, then global override, then manifest default; an
// undeclared key is false. Methods never fail: on a store error an ops flag
// resolves to its manifest default and a release flag to false.
type Evaluator interface {
	Enabled(ctx context.Context, key Key) bool
	// Snapshot resolves every declared key for tenant (nil: no tenant level);
	// never nil.
	Snapshot(ctx context.Context, tenant *reqctx.TenantID) map[Key]bool
}

// Disabled is the Evaluator with every key false and every snapshot empty.
type Disabled struct{}

// Enabled is always false.
func (Disabled) Enabled(ctx context.Context, key Key) bool { return false }

// Snapshot is always an empty, non-nil map.
func (Disabled) Snapshot(ctx context.Context, tenant *reqctx.TenantID) map[Key]bool {
	return map[Key]bool{}
}

// Route evaluates key once per request with e.Enabled(r.Context(), key) and
// serves on when true, off otherwise. Wrap it inside auth.Require. It panics on
// a nil evaluator or handler, or an empty key.
func Route(e Evaluator, key Key, on, off http.Handler) http.Handler {
	if e == nil || on == nil || off == nil || key == "" {
		panic("featureflag: Route requires an evaluator, a key and both handlers")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e.Enabled(r.Context(), key) {
			on.ServeHTTP(w, r)
			return
		}
		off.ServeHTTP(w, r)
	})
}
