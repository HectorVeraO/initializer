package featureflags

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/featureflag"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

const (
	listPageSize    = 100
	scopeGlobal     = "global"
	scopeTenant     = "tenant"
	enabledField    = "enabled"
	logOverrideSet  = "flag_override_set"
	logOverrideDrop = "flag_override_cleared"
)

// GlobalOverrideView is the wire shape of a global override.
type GlobalOverrideView struct {
	Enabled   bool      `json:"enabled"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TenantOverrideView is the wire shape of a tenant override.
type TenantOverrideView struct {
	TenantID  string    `json:"tenantId"`
	Enabled   bool      `json:"enabled"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// FlagView is the wire shape of one manifest flag with its overrides.
type FlagView struct {
	Key             string               `json:"key"`
	Type            Type                 `json:"type"`
	Default         bool                 `json:"default"`
	ExpiresAt       *string              `json:"expiresAt"`
	Description     string               `json:"description"`
	GlobalOverride  *GlobalOverrideView  `json:"globalOverride"`
	TenantOverrides []TenantOverrideView `json:"tenantOverrides"`
}

// routes holds what the operator handlers use.
type routes struct {
	manifest Manifest
	store    store
	tenants  access.TenantDirectory
	writer   *api.Writer
	logger   *slog.Logger
}

// mount registers the five operator routes. They are cross-tenant by design:
// the operator targets any tenant by id, so nothing here is tenant-isolated.
func (m *module) mount(_ context.Context, h *core.Host) error {
	if !m.ready {
		return errors.New("featureflags: mount before provide")
	}
	rt := &routes{
		manifest: m.manifest,
		store:    m.store,
		tenants:  h.TenantDirectory(),
		writer:   h.API(),
		logger:   h.Logger(),
	}
	operator := access.Policy{Kind: access.KindSession, PlatformOperator: true}
	h.Handle("GET /api/v1/feature-flags", operator, http.HandlerFunc(rt.list))
	h.Handle("PUT /api/v1/feature-flags/{key}", operator, http.HandlerFunc(rt.setGlobal))
	h.Handle("DELETE /api/v1/feature-flags/{key}", operator, http.HandlerFunc(rt.clearGlobal))
	h.Handle("PUT /api/v1/feature-flags/{key}/tenants/{tenantId}", operator, http.HandlerFunc(rt.setTenant))
	h.Handle("DELETE /api/v1/feature-flags/{key}/tenants/{tenantId}", operator, http.HandlerFunc(rt.clearTenant))
	return nil
}

// actorOf returns the account id to record in updated_by, nil when empty.
func actorOf(r *http.Request) *string {
	principal, ok := reqctx.PrincipalFrom(r.Context())
	if !ok || principal.AccountID == "" {
		return nil
	}
	id := string(principal.AccountID)
	return &id
}

// flagOf resolves the {key} path value against the manifest or answers 404.
func (rt *routes) flagOf(w http.ResponseWriter, r *http.Request) (Flag, bool) {
	flag, ok := rt.manifest.Lookup(featureflag.Key(r.PathValue("key")))
	if !ok {
		rt.writer.Fail(w, r, api.NewError(http.StatusNotFound, CodeFlagNotDefinedInManifest, "Flag is not defined in the manifest"))
	}
	return flag, ok
}

// tenantOf canonicalizes the {tenantId} path value; a malformed id is a 404.
func (rt *routes) tenantOf(w http.ResponseWriter, r *http.Request) (reqctx.TenantID, bool) {
	tenant, ok := reqctx.ParseTenantID(r.PathValue("tenantId"))
	if !ok {
		rt.writer.Fail(w, r, api.NotFound())
	}
	return tenant, ok
}

// enabledOf validates the body: enabled is a required boolean.
func (rt *routes) enabledOf(w http.ResponseWriter, r *http.Request) (bool, bool) {
	body, err := api.DecodeBody(r)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return false, false
	}
	var problems api.Problems
	enabled, _ := body.Bool(&problems, enabledField, api.Required)
	err = problems.Err()
	if err != nil {
		rt.writer.Fail(w, r, err)
		return false, false
	}
	return enabled, true
}

func (rt *routes) list(w http.ResponseWriter, r *http.Request) {
	var problems api.Problems
	query := api.ParsePageQuery(r, &problems, api.PageSpec{DefaultSize: listPageSize, MaxSize: listPageSize})
	err := problems.Err()
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	all := rt.manifest.Flags()
	start := min(query.Offset(), len(all))
	end := min(start+query.Size, len(all))
	pageFlags := all[start:end]
	keys := make([]string, len(pageFlags))
	for i, flag := range pageFlags {
		keys[i] = string(flag.Key)
	}
	global, err := rt.store.listGlobal(r.Context())
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	tenant, err := rt.store.listTenant(r.Context(), keys)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	items := make([]FlagView, len(pageFlags))
	for i, flag := range pageFlags {
		items[i] = viewOf(flag, global, tenant)
	}
	rt.writer.Data(w, r, http.StatusOK, api.NewPage(items, query, int64(len(all))))
}

func (rt *routes) setGlobal(w http.ResponseWriter, r *http.Request) {
	flag, ok := rt.flagOf(w, r)
	if !ok {
		return
	}
	enabled, ok := rt.enabledOf(w, r)
	if !ok {
		return
	}
	err := rt.store.setGlobal(r.Context(), string(flag.Key), enabled, actorOf(r))
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.logger.InfoContext(r.Context(), logOverrideSet,
		slog.String("flag_key", string(flag.Key)), slog.String("flag_scope", scopeGlobal), slog.Bool(enabledField, enabled))
	rt.respond(w, r, flag)
}

func (rt *routes) clearGlobal(w http.ResponseWriter, r *http.Request) {
	flag, ok := rt.flagOf(w, r)
	if !ok {
		return
	}
	err := rt.store.clearGlobal(r.Context(), string(flag.Key))
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.logger.InfoContext(r.Context(), logOverrideDrop,
		slog.String("flag_key", string(flag.Key)), slog.String("flag_scope", scopeGlobal))
	rt.respond(w, r, flag)
}

func (rt *routes) setTenant(w http.ResponseWriter, r *http.Request) {
	flag, ok := rt.flagOf(w, r)
	if !ok {
		return
	}
	tenant, ok := rt.tenantOf(w, r)
	if !ok {
		return
	}
	exists, err := rt.tenants.TenantExists(r.Context(), tenant)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	if !exists {
		rt.writer.Fail(w, r, api.NotFound())
		return
	}
	enabled, ok := rt.enabledOf(w, r)
	if !ok {
		return
	}
	err = rt.store.setTenant(r.Context(), string(tenant), string(flag.Key), enabled, actorOf(r))
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.logger.InfoContext(r.Context(), logOverrideSet,
		slog.String("flag_key", string(flag.Key)), slog.String("flag_scope", scopeTenant),
		slog.String("target_tenant_id", string(tenant)), slog.Bool(enabledField, enabled))
	rt.respond(w, r, flag)
}

func (rt *routes) clearTenant(w http.ResponseWriter, r *http.Request) {
	flag, ok := rt.flagOf(w, r)
	if !ok {
		return
	}
	tenant, ok := rt.tenantOf(w, r)
	if !ok {
		return
	}
	err := rt.store.clearTenant(r.Context(), string(tenant), string(flag.Key))
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.logger.InfoContext(r.Context(), logOverrideDrop,
		slog.String("flag_key", string(flag.Key)), slog.String("flag_scope", scopeTenant),
		slog.String("target_tenant_id", string(tenant)))
	rt.respond(w, r, flag)
}

// respond answers 200 with the current view of one flag.
func (rt *routes) respond(w http.ResponseWriter, r *http.Request, flag Flag) {
	key := string(flag.Key)
	global, err := rt.store.listGlobal(r.Context())
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	tenant, err := rt.store.listTenant(r.Context(), []string{key})
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, viewOf(flag, global, tenant))
}

// viewOf joins a manifest flag with its override rows.
func viewOf(flag Flag, global map[string]globalRow, tenant map[string][]tenantRow) FlagView {
	key := string(flag.Key)
	view := FlagView{
		Key:             key,
		Type:            flag.Type,
		Default:         flag.Default,
		Description:     flag.Description,
		TenantOverrides: []TenantOverrideView{},
	}
	if flag.ExpiresAt != nil {
		date := flag.ExpiresAt.Format(dateLayout)
		view.ExpiresAt = &date
	}
	row, found := global[key]
	if found {
		view.GlobalOverride = &GlobalOverrideView{Enabled: row.enabled, UpdatedAt: row.updatedAt.UTC()}
	}
	for _, t := range tenant[key] {
		view.TenantOverrides = append(view.TenantOverrides, TenantOverrideView{
			TenantID: t.tenantID, Enabled: t.enabled, UpdatedAt: t.updatedAt.UTC(),
		})
	}
	return view
}
