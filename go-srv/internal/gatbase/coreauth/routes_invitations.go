package coreauth

import (
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/auth"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
)

const (
	maxFullNameLength = 100
	maxEmailChars     = 254
)

var tokenPattern43 = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// AcceptResult is the AcceptResult schema.
type AcceptResult struct {
	Membership MembershipView `json:"membership"`
}

// tenancyRoutes holds what the tenant and invitation handlers use.
type tenancyRoutes struct {
	tenancy *tenancy
	writer  *api.Writer
}

// mountTenancy registers the tenant, membership and invitation routes.
func (s *service) mountTenancy(h *core.Host) {
	routes := &tenancyRoutes{tenancy: s.tenancy, writer: h.API()}
	admin := auth.Policy{Kind: auth.KindSession, MinRole: reqctx.RoleAdmin}
	operator := auth.Policy{Kind: auth.KindSession, PlatformOperator: true}
	h.Handle("GET /api/v1/tenant", auth.Policy{Kind: auth.KindSessionOrAPIKey, Scope: "tenant:read", MinRole: reqctx.RoleViewer}, http.HandlerFunc(routes.getTenant))
	h.Handle("GET /api/v1/tenants", operator, http.HandlerFunc(routes.listTenants))
	h.Handle("POST /api/v1/tenants", operator, http.HandlerFunc(routes.createTenant))
	h.Handle("PATCH /api/v1/tenants/{id}/status", operator, http.HandlerFunc(routes.setTenantStatus))
	h.Handle("GET /api/v1/memberships", auth.Policy{Kind: auth.KindSession, MinRole: reqctx.RoleViewer}, http.HandlerFunc(routes.listMemberships))
	h.Handle("PATCH /api/v1/memberships/{id}", admin, http.HandlerFunc(routes.updateMembership))
	h.Handle("GET /api/v1/invitations", admin, http.HandlerFunc(routes.listInvitations))
	h.Handle("POST /api/v1/invitations", admin, http.HandlerFunc(routes.createInvitation))
	h.Handle("DELETE /api/v1/invitations/{id}", admin, http.HandlerFunc(routes.revokeInvitation))
	h.Handle("POST /api/v1/invitations/accept", auth.Policy{Kind: auth.KindSession}, http.HandlerFunc(routes.acceptInvitation))
	h.Handle("POST /api/v1/invitations/accept-new", auth.Public, http.HandlerFunc(routes.acceptNew))
}

// readEmail validates an email field: required, string, at most 254
// characters, well-formed. It returns the lower-cased address.
func readEmail(body *api.Body, p *api.Problems, field string) (string, bool) {
	value, ok := body.String(p, field, api.Required)
	if !ok {
		return "", false
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		p.Add(field, api.CodeFieldRequired, "Field is required")
		return "", false
	}
	if utf8.RuneCountInString(trimmed) > maxEmailChars {
		p.Add(field, api.CodeFieldTooLong, "Must be at most 254 characters")
		return "", false
	}
	return api.CheckEmail(p, field, trimmed)
}

// readToken validates a token field: required string of 43 base64url characters.
func readToken(body *api.Body, p *api.Problems) (string, bool) {
	value, ok := body.String(p, "token", api.Required)
	if !ok {
		return "", false
	}
	if value == "" {
		p.Add("token", api.CodeFieldRequired, "Field is required")
		return "", false
	}
	if !tokenPattern43.MatchString(value) {
		p.Add("token", api.CodeFieldInvalidFmt, "Must be 43 base64url characters")
		return "", false
	}
	return value, true
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

// readPassword validates a password: counted in runes, never trimmed.
func readPassword(body *api.Body, p *api.Problems) (string, bool) {
	value, ok := body.String(p, "password", api.Required)
	if !ok {
		return "", false
	}
	length := utf8.RuneCountInString(value)
	switch {
	case length == 0:
		p.Add("password", api.CodeFieldRequired, "Field is required")
		return "", false
	case length < minPasswordLength:
		p.Add("password", api.CodeFieldTooShort, "Must be at least 12 characters")
		return "", false
	case length > maxPasswordLength:
		p.Add("password", api.CodeFieldTooLong, "Must be at most 64 characters")
		return "", false
	}
	return value, true
}

// callerOf returns the account principal the guard installed.
func callerOf(r *http.Request) (reqctx.Principal, bool) {
	principal, ok := reqctx.PrincipalFrom(r.Context())
	hasAccount := ok && principal.Kind == reqctx.ActorAccount
	return principal, hasAccount
}

// activeTenantOf returns the principal's active tenant id.
func activeTenantOf(principal reqctx.Principal) (string, bool) {
	if principal.TenantID == nil {
		return "", false
	}
	return string(*principal.TenantID), true
}

func (rt *tenancyRoutes) createInvitation(w http.ResponseWriter, r *http.Request) {
	principal, ok := callerOf(r)
	tenantID, hasTenant := activeTenantOf(principal)
	if !ok || !hasTenant {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	body, err := api.DecodeBody(r)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	var p api.Problems
	email, emailOK := readEmail(body, &p, "email")
	roleText, roleOK := body.String(&p, "role", api.Required)
	if roleOK && roleText == "" {
		p.Add("role", api.CodeFieldRequired, "Field is required")
		roleOK = false
	}
	if roleOK {
		roleOK = api.CheckOneOf(&p, "role", roleText, string(reqctx.RoleAdmin), string(reqctx.RoleMember), string(reqctx.RoleViewer))
	}
	err = p.Err()
	if err != nil || !emailOK || !roleOK {
		rt.writer.Fail(w, r, err)
		return
	}
	created, err := rt.tenancy.CreateInvitation(r.Context(), tenantID, principal.ActorID, email, reqctx.Role(roleText))
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusCreated, created)
}

func (rt *tenancyRoutes) listInvitations(w http.ResponseWriter, r *http.Request) {
	principal, ok := callerOf(r)
	tenantID, hasTenant := activeTenantOf(principal)
	if !ok || !hasTenant {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	var p api.Problems
	status := invitationPending
	requested := r.URL.Query().Get("status")
	if requested != "" {
		api.CheckOneOf(&p, "status", requested, invitationPending, invitationAccepted, invitationRevoked, invitationExpired)
		status = requested
	}
	query := api.ParsePageQuery(r, &p, api.PageSpec{Sorts: []string{"createdAt"}, DefaultSort: "-createdAt"})
	err := p.Err()
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	items, total, err := rt.tenancy.ListInvitations(r.Context(), tenantID, status, query)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, api.NewPage(items, query, total))
}

func (rt *tenancyRoutes) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	principal, ok := callerOf(r)
	tenantID, hasTenant := activeTenantOf(principal)
	if !ok || !hasTenant {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	view, err := rt.tenancy.RevokeInvitation(r.Context(), tenantID, r.PathValue("id"))
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, view)
}

func (rt *tenancyRoutes) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	principal, ok := callerOf(r)
	if !ok {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	body, err := api.DecodeBody(r)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	var p api.Problems
	token, tokenOK := readToken(body, &p)
	err = p.Err()
	if err != nil || !tokenOK {
		rt.writer.Fail(w, r, err)
		return
	}
	membership, err := rt.tenancy.AcceptInvitation(r.Context(), principal.ActorID, token)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, AcceptResult{Membership: membership})
}

func (rt *tenancyRoutes) acceptNew(w http.ResponseWriter, r *http.Request) {
	body, err := api.DecodeBody(r)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	var p api.Problems
	token, tokenOK := readToken(body, &p)
	password, passwordOK := readPassword(body, &p)
	fullName, nameOK := readTrimmedName(body, &p, "fullName", maxFullNameLength)
	err = p.Err()
	if err != nil || !tokenOK || !passwordOK || !nameOK {
		rt.writer.Fail(w, r, err)
		return
	}
	result, err := rt.tenancy.AcceptNew(r.Context(), token, password, fullName)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusCreated, result)
}
