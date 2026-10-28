package parameters

import (
	"context"
	"errors"
	"net/http"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

// routes holds what the parameters handlers use.
type routes struct {
	service *Service
	writer  *api.Writer
}

// mount registers the six parameters routes. A platform operator is not an
// implicit tenant admin: the override routes need the admin role of the
// active tenant, the definition routes need the operator flag.
func (m *module) mount(_ context.Context, h *core.Host) error {
	if m.service == nil {
		return errors.New("parameters: mount before provide")
	}
	rt := &routes{service: m.service, writer: h.API()}
	anySession := access.Policy{Kind: access.KindSession}
	tenantAdmin := access.Policy{Kind: access.KindSession, MinRole: reqctx.RoleAdmin}
	operator := access.Policy{Kind: access.KindSession, PlatformOperator: true}
	h.Handle("GET /api/v1/parameters", anySession, http.HandlerFunc(rt.list))
	h.Handle("POST /api/v1/parameters", operator, http.HandlerFunc(rt.create))
	h.Handle("PUT /api/v1/parameters/{key}", operator, http.HandlerFunc(rt.replace))
	h.Handle("DELETE /api/v1/parameters/{key}", operator, http.HandlerFunc(rt.delete))
	h.Handle("PUT /api/v1/parameters/{key}/tenant-override", tenantAdmin, http.HandlerFunc(rt.setOverride))
	h.Handle("DELETE /api/v1/parameters/{key}/tenant-override", tenantAdmin, http.HandlerFunc(rt.clearOverride))
	return nil
}

// caller returns the principal the guard installed; false answers a 500
// because every route here is guarded.
func (rt *routes) caller(w http.ResponseWriter, r *http.Request) (reqctx.Principal, bool) {
	principal, ok := reqctx.PrincipalFrom(r.Context())
	if !ok {
		rt.writer.Fail(w, r, errors.New("parameters: no principal on a guarded route"))
	}
	return principal, ok
}

func (rt *routes) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := rt.caller(w, r)
	if !ok {
		return
	}
	var problems api.Problems
	query := api.ParsePageQuery(r, &problems, api.PageSpec{Sorts: []string{"key"}, DefaultSort: "key"})
	err := problems.Err()
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	page, err := rt.service.List(r.Context(), principal, query)
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

func (rt *routes) replace(w http.ResponseWriter, r *http.Request) {
	principal, ok := rt.caller(w, r)
	if !ok {
		return
	}
	body, err := api.DecodeBody(r)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	view, err := rt.service.Replace(r.Context(), principal, r.PathValue("key"), body)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, view)
}

func (rt *routes) delete(w http.ResponseWriter, r *http.Request) {
	principal, ok := rt.caller(w, r)
	if !ok {
		return
	}
	result, err := rt.service.Delete(r.Context(), principal, r.PathValue("key"))
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, result)
}

func (rt *routes) setOverride(w http.ResponseWriter, r *http.Request) {
	principal, ok := rt.caller(w, r)
	if !ok {
		return
	}
	body, err := api.DecodeBody(r)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	view, err := rt.service.SetOverride(r.Context(), principal, r.PathValue("key"), body)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, view)
}

func (rt *routes) clearOverride(w http.ResponseWriter, r *http.Request) {
	principal, ok := rt.caller(w, r)
	if !ok {
		return
	}
	view, err := rt.service.ClearOverride(r.Context(), principal, r.PathValue("key"))
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, view)
}
