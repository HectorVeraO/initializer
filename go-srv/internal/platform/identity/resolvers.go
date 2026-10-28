package identity

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	coreaccess "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/access"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/identity/session"
)

const csrfHeader = "X-XSRF-TOKEN"

// sessionResolver implements coreaccess.SessionResolver over the signed BFF token.
// It reads no database and calls no identity provider.
type sessionResolver struct {
	codec      *session.Codec
	cookies    session.Cookies
	refreshMax time.Duration
	logger     *slog.Logger
}

func newSessionResolver(codec *session.Codec, cookies session.Cookies, refreshMax time.Duration, logger *slog.Logger) *sessionResolver {
	return &sessionResolver{codec: codec, cookies: cookies, refreshMax: refreshMax, logger: logger}
}

// Present reports whether the session cookie is present (any value).
func (s *sessionResolver) Present(r *http.Request) bool {
	_, err := r.Cookie(s.cookies.SessionName())
	isPresent := err == nil
	return isPresent
}

// Resolve verifies the cookie. An expired token inside the refresh window is
// SessionExpired; every other failure is SessionInvalid.
func (s *sessionResolver) Resolve(r *http.Request) (coreaccess.SessionResult, error) {
	invalid := coreaccess.SessionResult{State: coreaccess.SessionInvalid}
	token, ok := s.cookies.Session(r)
	if !ok {
		return invalid, nil
	}
	state := coreaccess.SessionValid
	claims, err := s.codec.Verify(token)
	if errors.Is(err, session.ErrExpired) {
		state = coreaccess.SessionExpired
		claims, err = s.codec.VerifyAllowExpired(token, s.refreshMax)
	}
	if err != nil {
		s.logVerifyFailure(r.Context(), err)
		return invalid, nil
	}
	result := coreaccess.SessionResult{
		State:     state,
		Principal: principalOf(claims, state == coreaccess.SessionExpired),
		CSRFValid: session.CSRFMatches(r.Header.Get(csrfHeader), claims),
	}
	return result, nil
}

// logVerifyFailure logs the key id and the reason, never the token.
func (s *sessionResolver) logVerifyFailure(ctx context.Context, err error) {
	var verifyErr *session.VerifyError
	if !errors.As(err, &verifyErr) {
		return
	}
	s.logger.DebugContext(ctx, "session_verify_failed",
		slog.String("kid", verifyErr.Kid),
		slog.String("reason", verifyErr.Reason))
}

// principalOf maps verified claims to the caller of the request.
func principalOf(claims session.Claims, expired bool) reqctx.Principal {
	principal := reqctx.Principal{
		Kind:               reqctx.ActorAccount,
		ActorID:            claims.AccountID,
		AccountID:          reqctx.AccountID(claims.AccountID),
		IsPlatformOperator: claims.PlatformOperator,
		SessionExpired:     expired,
	}
	if claims.TenantID != nil {
		tenant := reqctx.TenantID(*claims.TenantID)
		principal.TenantID = &tenant
	}
	if claims.TenantSlug != nil {
		principal.TenantSlug = *claims.TenantSlug
	}
	if claims.Role != nil {
		principal.Role = reqctx.Role(*claims.Role)
	}
	return principal
}
