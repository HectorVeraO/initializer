package dispatch

import (
	"context"
	"errors"
	"net/http"

	"example.com/fixture/srv/internal/platform/core"
	"example.com/fixture/srv/internal/platform/core/access"
	"example.com/fixture/srv/internal/platform/core/api"
	"example.com/fixture/srv/internal/platform/core/reqctx"
)

// routes holds what the dispatch handlers use.
type routes struct {
	service *Service
	writer  *api.Writer
}

// mount registers the two dispatch routes. Any session reads the queue and
// queues a job: the queue is project-wide, so no role and no tenant is
// required.
func (m *module) mount(_ context.Context, h *core.Host) error {
	if m.service == nil {
		return errors.New("dispatch: mount before provide")
	}
	rt := &routes{service: m.service, writer: h.API()}
	anySession := access.Policy{Kind: access.KindSession}
	h.Handle("GET /api/v1/dispatch", anySession, http.HandlerFunc(rt.list))
	h.Handle("POST /api/v1/dispatch", anySession, http.HandlerFunc(rt.create))
	return nil
}

// caller returns the principal the guard installed; false answers a 500
// because every route here is guarded.
func (rt *routes) caller(w http.ResponseWriter, r *http.Request) (reqctx.Principal, bool) {
	principal, ok := reqctx.PrincipalFrom(r.Context())
	if !ok {
		rt.writer.Fail(w, r, errors.New("dispatch: no principal on a guarded route"))
	}
	return principal, ok
}

func (rt *routes) list(w http.ResponseWriter, r *http.Request) {
	var problems api.Problems
	query := api.ParsePageQuery(r, &problems, api.PageSpec{Sorts: []string{"reference"}, DefaultSort: "reference"})
	err := problems.Err()
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	page, err := rt.service.List(r.Context(), query)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, page)
}

func (rt *routes) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := rt.caller(w, r)
	if !ok {
		return
	}
	body, err := api.DecodeBody(r)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	view, err := rt.service.Create(r.Context(), principal, body)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusCreated, view)
}
