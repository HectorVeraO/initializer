package apikeys

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

const maxKeyName = 100

// ScopeInfo is the ScopeInfo schema.
type ScopeInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Level is "tenant" (only a key with a tenant may hold the scope) or "any".
	Level string `json:"level"`
}

// keyRoutes holds what the API key handlers use.
type keyRoutes struct {
	keys   *apiKeys
	scopes *coreaccess.ScopeRegistry
	writer *api.Writer
}

// mountRoutes registers the API key and scope routes. Only the listing
// accepts an API key; no write route does. A platform operator without an
// active tenant manages the global keys (those without a tenant).
func (s *service) mountRoutes(h *core.Host) {
	routes := &keyRoutes{keys: s.keys, scopes: h.Scopes(), writer: h.API()}
	admin := coreaccess.Policy{Kind: coreaccess.KindSession, MinRole: reqctx.RoleAdmin, TenantlessOperator: true}
	list := coreaccess.Policy{
		Kind: coreaccess.KindSessionOrAPIKey, Scope: scopeRead, MinRole: reqctx.RoleAdmin, TenantlessOperator: true,
	}
	h.Handle("GET /api/v1/api-keys", list, http.HandlerFunc(routes.listAPIKeys))
	h.Handle("POST /api/v1/api-keys", admin, http.HandlerFunc(routes.createAPIKey))
	h.Handle("DELETE /api/v1/api-keys/{id}", admin, http.HandlerFunc(routes.revokeAPIKey))
	h.Handle("GET /api/v1/api-key-scopes", admin, http.HandlerFunc(routes.listScopes))
}

func sessionInvalid() *api.Error {
	return api.NewError(http.StatusUnauthorized, api.CodeSessionInvalid, "Session is missing or invalid.")
}

// callerScope returns the caller and the scope of its request: the principal's
// tenant, nil for a platform operator without one (global keys). Never a body
// or query field.
func callerScope(r *http.Request) (reqctx.Principal, *reqctx.TenantID, bool) {
	principal, ok := reqctx.PrincipalFrom(r.Context())
	return principal, principal.TenantID, ok
}

// readScopes validates scopes: a non-empty array of registered, distinct names.
func (rt *keyRoutes) readScopes(body *api.Body, p *api.Problems) ([]string, bool) {
	names, ok := body.StringList(p, "scopes", api.Required)
	if !ok {
		return nil, false
	}
	if len(names) == 0 {
		p.Add("scopes", api.CodeFieldRequired, "Field is required")
		return nil, false
	}
	seen := make(map[string]struct{}, len(names))
	allValid := true
	for i, name := range names {
		field := fmt.Sprintf("scopes[%d]", i)
		_, repeated := seen[name]
		seen[name] = struct{}{}
		switch {
		case repeated:
			p.Add(field, api.CodeFieldDuplicate, "Duplicate entry")
			allValid = false
		case !rt.scopes.Known(name):
			p.Add(field, api.CodeFieldNotAllowed, "Not a registered scope")
			allValid = false
		}
	}
	return names, allValid
}

// readExpiresAt validates expiresAt: absent, null or an RFC 3339 timestamp.
func readExpiresAt(body *api.Body, p *api.Problems) (*time.Time, bool) {
	text, present := body.String(p, "expiresAt", api.Optional)
	if !present {
		isTypeError := body.Has("expiresAt") && !body.IsNull("expiresAt")
		return nil, !isTypeError
	}
	parsed, ok := api.CheckTimestamp(p, "expiresAt", text)
	if !ok {
		return nil, false
	}
	return &parsed, true
}

func (rt *keyRoutes) createAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, scope, ok := callerScope(r)
	if !ok || principal.Kind != reqctx.ActorAccount {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	body, err := api.DecodeBody(r)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	var p api.Problems
	name, nameOK := readTrimmedName(body, &p, "name", maxKeyName)
	scopes, scopesOK := rt.readScopes(body, &p)
	rt.scopes.CheckMint(&p, "scopes", scopes, scope)
	expiresAt, expiresOK := readExpiresAt(body, &p)
	err = p.Err()
	if err != nil || !nameOK || !scopesOK || !expiresOK {
		rt.writer.Fail(w, r, err)
		return
	}
	created, err := rt.keys.Create(r.Context(), scope, principal.AccountID, name, scopes, expiresAt)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusCreated, created)
}

func (rt *keyRoutes) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	_, scope, ok := callerScope(r)
	if !ok {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	var p api.Problems
	status := r.URL.Query().Get("status")
	if status != "" {
		api.CheckOneOf(&p, "status", status, statusActive, statusExpired, statusRevoked)
	}
	query := api.ParsePageQuery(r, &p, api.PageSpec{Sorts: []string{"createdAt", "name"}, DefaultSort: "-createdAt"})
	err := p.Err()
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	items, total, err := rt.keys.List(r.Context(), scope, status, query)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, api.NewPage(items, query, total))
}

func (rt *keyRoutes) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	_, scope, ok := callerScope(r)
	if !ok {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	view, err := rt.keys.Revoke(r.Context(), scope, r.PathValue("id"))
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, view)
}

func (rt *keyRoutes) listScopes(w http.ResponseWriter, r *http.Request) {
	registered := rt.scopes.All()
	infos := make([]ScopeInfo, 0, len(registered))
	for _, scope := range registered {
		infos = append(infos, ScopeInfo{Name: scope.Name, Description: scope.Description, Level: scope.Level.String()})
	}
	rt.writer.Data(w, r, http.StatusOK, infos)
}

// readTrimmedName validates a name field: required after trimming, at most
// max characters. It returns the trimmed value.
func readTrimmedName(body *api.Body, p *api.Problems, field string, max int) (string, bool) {
	value, ok := body.String(p, field, api.Required)
	if !ok {
		return "", false
	}
	if !api.CheckRequired(p, field, value) {
		return "", false
	}
	if !api.CheckLength(p, field, value, 1, max) {
		return "", false
	}
	// Postgres text columns reject NUL: it is invalid input, not a 500.
	if strings.ContainsRune(value, 0) {
		p.Add(field, api.CodeFieldInvalidFmt, "Must not contain a NUL character")
		return "", false
	}
	return strings.TrimSpace(value), true
}
