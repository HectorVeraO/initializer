package coreauth

import (
	"context"
	"errors"
	"net/http"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/coreauth/kratos"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/coreauth/session"
)

// reissue returns claims for the same chain (sid, ksid, csrf, iat) on the
// tenant the grant describes; a nil tenantID mints a tenantless token.
func reissue(old session.Claims, tenantID *string, grant Grant) session.Claims {
	claims := old
	claims.PlatformOperator = grant.PlatformOperator
	claims.TenantID = nil
	claims.TenantSlug = nil
	claims.Role = nil
	if tenantID != nil {
		slug := grant.TenantSlug
		role := string(grant.Role)
		claims.TenantID = tenantID
		claims.TenantSlug = &slug
		claims.Role = &role
	}
	return claims
}

// clearCookies expires the session and CSRF cookies.
func (rt *sessionRoutes) clearCookies(w http.ResponseWriter) {
	rt.cookies.ClearSession(w)
	rt.cookies.ClearCSRF(w)
}

// refresh serves POST /api/v1/auth/refresh. The guard already verified the
// signature (expired accepted up to the refresh maximum age) and the CSRF
// header against the old csrf claim.
func (rt *sessionRoutes) refresh(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token, hasToken := rt.cookies.Session(r)
	if !hasToken {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	old, err := rt.codec.VerifyAllowExpired(token, rt.cfg.SessionRefreshMaxAge)
	if err != nil {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	kratosCookie, _ := r.Cookie(kratosCookieName)
	kratosSession, err := rt.public.WhoAmI(ctx, kratosCookie)
	if errors.Is(err, kratos.ErrNoSession) {
		rt.clearCookies(w)
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	if err != nil {
		unavailable := api.Wrap(err, http.StatusServiceUnavailable, CodeIdentityProviderUnavailable,
			"Sign-in is unavailable right now.")
		rt.writer.Fail(w, r, unavailable)
		return
	}
	linked, err := rt.store.AccountByKratosIdentity(ctx, kratosSession.IdentityID)
	isLinkedToSubject := err == nil && linked.ID == old.AccountID
	if errors.Is(err, ErrNotFound) || (err == nil && !isLinkedToSubject) {
		rt.clearCookies(w)
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	claims, err := rt.refreshClaims(ctx, w, old, kratosSession)
	if err != nil {
		if isAccessDenial(err) {
			rt.clearCookies(w)
		}
		rt.writer.Fail(w, r, err)
		return
	}
	rt.respondView(ctx, w, r, claims)
}

// refreshClaims steps 1 (new Kratos login) to 4 of the refresh route and
// sets the cookies of the claims it returns.
func (rt *sessionRoutes) refreshClaims(ctx context.Context, w http.ResponseWriter, old session.Claims, kratosSession kratos.Session) (session.Claims, error) {
	isNewKratosLogin := kratosSession.ID != old.KratosSessionID
	if isNewKratosLogin {
		return rt.startChain(ctx, w, kratosSession)
	}
	_, err := rt.access.linkIdentity(ctx, kratosSession.IdentityID, kratosSession.Email)
	if err != nil {
		return session.Claims{}, err
	}
	var tenantID *string
	var grant Grant
	if old.TenantID == nil {
		grant, err = rt.access.checkAccess(ctx, old.AccountID, nil)
		if err != nil {
			return session.Claims{}, err
		}
		tenantID, grant, err = rt.access.selectTenant(ctx, old.AccountID)
		if err != nil {
			return session.Claims{}, err
		}
	} else {
		tenantID = old.TenantID
		grant, err = rt.access.checkAccess(ctx, old.AccountID, tenantID)
		if err != nil {
			return session.Claims{}, err
		}
	}
	claims := reissue(old, tenantID, grant)
	claims.ExpiresAt = rt.clock.Now().Add(rt.cfg.SessionTTL)
	token, err := rt.codec.Encode(claims)
	if err != nil {
		return session.Claims{}, err
	}
	// The XSRF-TOKEN value is unchanged; its cookie is rewritten with the
	// session cookie's Max-Age so the two expire together.
	rt.cookies.SetSession(w, token)
	rt.cookies.SetCSRF(w, claims.CSRF)
	return claims, nil
}

// switchTenant serves POST /api/v1/auth/switch-tenant. The guard already
// verified the session, the CSRF header and the stale-tab header.
func (rt *sessionRoutes) switchTenant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token, hasToken := rt.cookies.Session(r)
	if !hasToken {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	old, err := rt.codec.Verify(token)
	if err != nil {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	tenantID, err := parseSwitchTenantID(r)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	grant, err := rt.access.checkAccess(ctx, old.AccountID, &tenantID)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	claims := reissue(old, &tenantID, grant)
	expiresAt := rt.clock.Now().Add(rt.cfg.SessionTTL)
	if old.ExpiresAt.Before(expiresAt) {
		expiresAt = old.ExpiresAt
	}
	claims.ExpiresAt = expiresAt
	newToken, err := rt.codec.Encode(claims)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	err = rt.access.setLastActiveTenant(ctx, old.AccountID, tenantID)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	// The XSRF-TOKEN value is unchanged; its cookie is rewritten with the
	// session cookie's Max-Age so the two expire together.
	rt.cookies.SetSession(w, newToken)
	rt.cookies.SetCSRF(w, claims.CSRF)
	rt.respondView(ctx, w, r, claims)
}

// parseSwitchTenantID reads the body of a switch request and returns the
// canonical tenant id, or the *api.Error to answer (415, 413 or 422).
func parseSwitchTenantID(r *http.Request) (string, error) {
	body, err := api.DecodeBody(r)
	if err != nil {
		return "", err
	}
	var problems api.Problems
	rawTenantID, isString := body.String(&problems, "tenantId", api.Required)
	tenantID := ""
	if isString {
		canonical, isUUID := api.CheckUUID(&problems, "tenantId", rawTenantID)
		if isUUID {
			tenantID = canonical
		}
	}
	err = problems.Err()
	if err != nil {
		return "", err
	}
	return tenantID, nil
}
