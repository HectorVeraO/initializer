package featureflag_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/featureflag"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
)

type staticEvaluator struct {
	on   bool
	keys []featureflag.Key
}

func (s *staticEvaluator) Enabled(_ context.Context, key featureflag.Key) bool {
	s.keys = append(s.keys, key)
	return s.on
}

func (s *staticEvaluator) Snapshot(context.Context, *reqctx.TenantID) map[featureflag.Key]bool {
	return map[featureflag.Key]bool{}
}

func marker(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
}

func TestDisabled(t *testing.T) {
	var e featureflag.Evaluator = featureflag.Disabled{}
	if e.Enabled(context.Background(), "any-flag") {
		t.Fatal("enabled")
	}
	tenant := reqctx.TenantID("0b6f2a4e-7d1c-4c52-9a38-1f5e6d7c8b90")
	for _, id := range []*reqctx.TenantID{nil, &tenant} {
		snapshot := e.Snapshot(context.Background(), id)
		if snapshot == nil || len(snapshot) != 0 {
			t.Fatalf("snapshot = %v", snapshot)
		}
	}
}

func TestRouteServesOnOrOff(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		want := http.StatusNoContent
		if !enabled {
			want = http.StatusNotFound
		}
		e := &staticEvaluator{on: enabled}
		rec := httptest.NewRecorder()
		featureflag.Route(e, "new-thing", marker(http.StatusNoContent), marker(http.StatusNotFound)).
			ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != want {
			t.Fatalf("enabled=%v status = %d", enabled, rec.Code)
		}
		if len(e.keys) != 1 || e.keys[0] != "new-thing" {
			t.Fatalf("evaluated keys = %v, want [new-thing]", e.keys)
		}
	}
}

func TestRoutePanicsOnBadInputs(t *testing.T) {
	ok := marker(http.StatusOK)
	e := &staticEvaluator{}
	cases := map[string]func(){
		"nil evaluator": func() { featureflag.Route(nil, "k", ok, ok) },
		"empty key":     func() { featureflag.Route(e, "", ok, ok) },
		"nil on":        func() { featureflag.Route(e, "k", nil, ok) },
		"nil off":       func() { featureflag.Route(e, "k", ok, nil) },
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("no panic")
				}
			}()
			call()
		})
	}
}
