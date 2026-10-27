package coreauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/auth"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/featureflag"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/coreauth/kratos"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/coreauth/session"
)

const kratosCookieName = "ory_kratos_session"

// sessionRoutes holds what the session handlers use.
type sessionRoutes struct {
	cfg     Config
	store   *store
	access  *access
	codec   *session.Codec
	cookies session.Cookies
	public  *kratos.Public
	flags   featureflag.Evaluator
	writer  *api.Writer
	logger  *slog.Logger
	clock   clock.Clock
}

// mountSession registers the session routes and the Kratos proxy.
func (s *service) mountSession(h *core.Host) {
	if s.codec == nil || s.public == nil || s.cfg.KratosPublicURL == nil {
		panic("coreauth: session routes need the codec and the Kratos public client")
	}
	routes := &sessionRoutes{
		cfg: s.cfg, store: s.store, access: s.access, codec: s.codec, cookies: s.cookies,
		public: s.public, flags: h.FlagEvaluator(), writer: h.API(), logger: h.Logger(), clock: h.Clock(),
	}
	h.Handle("GET /api/v1/auth/session", auth.Public, http.HandlerFunc(routes.getSession))
	h.Handle("POST /api/v1/auth/logout", auth.Policy{Kind: auth.KindSessionAllowExpired}, http.HandlerFunc(routes.logout))
	h.Handle("POST /api/v1/auth/refresh", auth.Policy{Kind: auth.KindSessionAllowExpired}, http.HandlerFunc(routes.refresh))
	h.Handle("POST /api/v1/auth/switch-tenant", auth.Policy{Kind: auth.KindSession}, http.HandlerFunc(routes.switchTenant))

	var opts []kratos.Option
	telemetry := h.Telemetry()
	if telemetry != nil {
		opts = append(opts, kratos.WithTransportWrapper(telemetry.Transport))
	}
	cookieNames := []string{s.cookies.SessionName(), "XSRF-TOKEN"}
	proxy := kratos.NewProxy(s.cfg.KratosPublicURL, h.PublicBaseURL(), cookieNames, opts...)
	h.Handle("/kratos/", auth.Public, proxy)
}

func sessionInvalid() *api.Error {
	return api.NewError(http.StatusUnauthorized, api.CodeSessionInvalid, "Session is missing or invalid.")
}

// getSession serves GET /api/v1/auth/session: it answers from a verifying
// token (moving a tenantless one onto a tenant) or issues one from Kratos.
func (rt *sessionRoutes) getSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token, hasToken := rt.cookies.Session(r)
	if hasToken {
		claims, err := rt.codec.Verify(token)
		if err == nil {
			rt.answerExisting(ctx, w, r, claims)
			return
		}
	}
	rt.issue(ctx, w, r)
}

// answerExisting is step 1 of the session route.
func (rt *sessionRoutes) answerExisting(ctx context.Context, w http.ResponseWriter, r *http.Request, claims session.Claims) {
	if claims.TenantID == nil {
		moved, err := rt.moveOntoTenant(ctx, w, claims)
		if err != nil {
			rt.writer.Fail(w, r, err)
			return
		}
		claims = moved
	}
	rt.respondView(ctx, w, r, claims)
}

// moveOntoTenant re-mints a tenantless token on the tenant selection finds,
// keeping sid, ksid, csrf and iat (the refresh window stays anchored to the
// chain start), and never outliving the old exp. It returns the
// claims unchanged when no tenant is usable.
func (rt *sessionRoutes) moveOntoTenant(ctx context.Context, w http.ResponseWriter, claims session.Claims) (session.Claims, error) {
	tenantID, grant, err := rt.access.selectTenant(ctx, claims.AccountID)
	if err != nil {
		return claims, err
	}
	if tenantID == nil {
		return claims, nil
	}
	now := rt.clock.Now()
	expiresAt := now.Add(rt.cfg.SessionTTL)
	if claims.ExpiresAt.Before(expiresAt) {
		expiresAt = claims.ExpiresAt
	}
	hasRoom := expiresAt.Unix() > now.Unix()
	if !hasRoom {
		return claims, nil
	}
	slug := grant.TenantSlug
	role := string(grant.Role)
	moved := claims
	moved.TenantID = tenantID
	moved.TenantSlug = &slug
	moved.Role = &role
	moved.PlatformOperator = grant.PlatformOperator
	moved.ExpiresAt = expiresAt
	err = rt.setCookies(w, moved)
	if err != nil {
		return claims, err
	}
	return moved, nil
}

// issue is step 2 of the session route: a new chain from the Kratos session.
func (rt *sessionRoutes) issue(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	kratosCookie, err := r.Cookie(kratosCookieName)
	if err != nil || kratosCookie.Value == "" {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	kratosSession, err := rt.public.WhoAmI(ctx, kratosCookie)
	if errors.Is(err, kratos.ErrNoSession) {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	if err != nil {
		unavailable := api.Wrap(err, http.StatusServiceUnavailable, CodeIdentityProviderUnavailable,
			"Sign-in is unavailable right now.")
		rt.writer.Fail(w, r, unavailable)
		return
	}
	claims, err := rt.startChain(ctx, w, kratosSession)
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.respondView(ctx, w, r, claims)
}

// startChain is steps 2.3 to 2.6 of the session route: link the identity,
// check the account, select a tenant, mint a new chain and set both cookies.
func (rt *sessionRoutes) startChain(ctx context.Context, w http.ResponseWriter, kratosSession kratos.Session) (session.Claims, error) {
	account, err := rt.access.linkIdentity(ctx, kratosSession.IdentityID, kratosSession.Email)
	if err != nil {
		return session.Claims{}, err
	}
	_, err = rt.access.checkAccess(ctx, account.ID, nil)
	if err != nil {
		return session.Claims{}, err
	}
	tenantID, grant, err := rt.access.selectTenant(ctx, account.ID)
	if err != nil {
		return session.Claims{}, err
	}
	claims, err := rt.newChain(account, kratosSession.ID, tenantID, grant)
	if err != nil {
		return session.Claims{}, err
	}
	err = rt.setCookies(w, claims)
	if err != nil {
		return session.Claims{}, err
	}
	return claims, nil
}

// newChain builds the claims of a new session chain.
func (rt *sessionRoutes) newChain(account Account, kratosSessionID string, tenantID *string, grant Grant) (session.Claims, error) {
	sessionID, err := session.NewRandomID()
	if err != nil {
		return session.Claims{}, err
	}
	csrf, err := session.NewRandomID()
	if err != nil {
		return session.Claims{}, err
	}
	now := rt.clock.Now()
	claims := session.Claims{
		AccountID:        account.ID,
		SessionID:        sessionID,
		KratosSessionID:  kratosSessionID,
		PlatformOperator: account.IsPlatformOperator,
		CSRF:             csrf,
		IssuedAt:         now,
		ExpiresAt:        now.Add(rt.cfg.SessionTTL),
	}
	if tenantID != nil {
		slug := grant.TenantSlug
		role := string(grant.Role)
		claims.TenantID = tenantID
		claims.TenantSlug = &slug
		claims.Role = &role
	}
	return claims, nil
}

// setCookies signs claims and writes both cookies.
func (rt *sessionRoutes) setCookies(w http.ResponseWriter, claims session.Claims) error {
	token, err := rt.codec.Encode(claims)
	if err != nil {
		return fmt.Errorf("coreauth: sign session: %w", err)
	}
	rt.cookies.SetSession(w, token)
	rt.cookies.SetCSRF(w, claims.CSRF)
	return nil
}

// logoutResult is the body of a logout.
type logoutResult struct {
	LogoutURL string `json:"logoutUrl"`
}

// logout serves POST /api/v1/auth/logout. The guard already verified the
// session and the CSRF header.
func (rt *sessionRoutes) logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	kratosCookie, err := r.Cookie(kratosCookieName)
	if err == nil && kratosCookie.Value != "" {
		logoutErr := rt.public.Logout(ctx, kratosCookie)
		if logoutErr != nil && !errors.Is(logoutErr, kratos.ErrNoSession) {
			rt.logger.WarnContext(ctx, "kratos_logout_failed", slog.Any("err", logoutErr))
		}
	}
	rt.cookies.ClearSession(w)
	rt.cookies.ClearCSRF(w)
	http.SetCookie(w, &http.Cookie{
		Name:     kratosCookieName,
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   rt.cfg.PublicBaseURL != nil && rt.cfg.PublicBaseURL.Scheme == "https",
		SameSite: http.SameSiteLaxMode,
	})
	base := *rt.cfg.PublicBaseURL
	result := logoutResult{LogoutURL: base.String() + "/login"}
	rt.writer.Data(w, r, http.StatusOK, result)
}

type tenantSummary struct {
	ID     string `json:"id"`
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type sessionAccount struct {
	ID                 string  `json:"id"`
	Email              string  `json:"email"`
	FullName           *string `json:"fullName"`
	Role               *string `json:"role"`
	IsPlatformOperator bool    `json:"isPlatformOperator"`
}

type membershipView struct {
	ID     string        `json:"id"`
	Tenant tenantSummary `json:"tenant"`
	Role   string        `json:"role"`
	Status string        `json:"status"`
}

type sessionView struct {
	Account      sessionAccount   `json:"account"`
	ActiveTenant *tenantSummary   `json:"activeTenant"`
	Memberships  []membershipView `json:"memberships"`
	Flags        map[string]bool  `json:"flags"`
}

// respondView answers 200 with the session view of the token in force.
func (rt *sessionRoutes) respondView(ctx context.Context, w http.ResponseWriter, r *http.Request, claims session.Claims) {
	view, err := rt.buildView(ctx, claims)
	if errors.Is(err, ErrNotFound) {
		rt.writer.Fail(w, r, sessionInvalid())
		return
	}
	if err != nil {
		rt.writer.Fail(w, r, err)
		return
	}
	rt.writer.Data(w, r, http.StatusOK, view)
}

// buildView reads the account, the active tenant and the active memberships
// now; role, plat and ids come from the token.
func (rt *sessionRoutes) buildView(ctx context.Context, claims session.Claims) (sessionView, error) {
	account, err := rt.store.AccountByID(ctx, claims.AccountID)
	if err != nil {
		return sessionView{}, err
	}
	view := sessionView{
		Account: sessionAccount{
			ID: claims.AccountID, Email: account.Email, FullName: account.FullName,
			Role: claims.Role, IsPlatformOperator: claims.PlatformOperator,
		},
		Memberships: []membershipView{},
		Flags:       map[string]bool{},
	}
	var flagTenant *reqctx.TenantID
	if claims.TenantID != nil {
		tenant, err := rt.store.TenantByID(ctx, *claims.TenantID)
		if err != nil {
			return sessionView{}, err
		}
		view.ActiveTenant = &tenantSummary{ID: tenant.ID, Slug: tenant.Slug, Name: tenant.Name, Status: tenant.Status}
		id := reqctx.TenantID(tenant.ID)
		flagTenant = &id
	}
	memberships, err := rt.activeMemberships(ctx, claims.AccountID)
	if err != nil {
		return sessionView{}, err
	}
	view.Memberships = memberships
	for key, value := range rt.flags.Snapshot(ctx, flagTenant) {
		view.Flags[string(key)] = value
	}
	return view, nil
}

// activeMemberships lists the account's active memberships, any tenant
// status, ordered by tenant slug.
func (rt *sessionRoutes) activeMemberships(ctx context.Context, accountID string) ([]membershipView, error) {
	account, ok := canonicalID(accountID)
	if !ok {
		return nil, ErrNotFound
	}
	rows, err := rt.store.pool.Query(ctx,
		`SELECT m.id::text, t.id::text, t.slug, t.name, t.status, m.role, m.status
FROM ten_memberships m JOIN ten_tenants t ON t.id = m.tenant_id
WHERE m.account_id = $1::uuid AND m.status = 'active' ORDER BY t.slug, m.id`, account)
	if err != nil {
		return nil, fmt.Errorf("coreauth: list memberships: %w", err)
	}
	defer rows.Close()
	list := []membershipView{}
	for rows.Next() {
		var item membershipView
		err := rows.Scan(&item.ID, &item.Tenant.ID, &item.Tenant.Slug, &item.Tenant.Name,
			&item.Tenant.Status, &item.Role, &item.Status)
		if err != nil {
			return nil, fmt.Errorf("coreauth: scan membership: %w", err)
		}
		list = append(list, item)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("coreauth: list memberships: %w", err)
	}
	return list, nil
}
