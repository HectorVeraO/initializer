package session

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
)

const (
	algorithm     = "Ed25519"
	tokenType     = "JWT"
	issuer        = "platform-bff"
	audience      = "platform-bff"
	maxTokenSize  = 4096
	maxFutureSkew = 60 * time.Second
	segmentCount  = 3
)

var (
	// ErrInvalid is the class of every rejected token except an expired one.
	ErrInvalid = errors.New("session: invalid token")
	// ErrExpired means the signature and claims are valid but exp has passed.
	ErrExpired = errors.New("session: token expired")
)

var (
	headerKeys = []string{"alg", "typ", "kid"}
	// strictBase64 rejects padding and non-canonical trailing bits, so one
	// byte string has exactly one accepted text form.
	strictBase64 = base64.RawURLEncoding.Strict()
)

// Claims is the signed session context.
type Claims struct {
	AccountID        string    // sub
	SessionID        string    // sid
	KratosSessionID  string    // ksid
	TenantID         *string   // tid; nil when tenantless
	TenantSlug       *string   // tslug; nil iff TenantID is nil
	Role             *string   // nil iff TenantID is nil
	PlatformOperator bool      // plat
	CSRF             string    // csrf
	IssuedAt         time.Time // iat
	ExpiresAt        time.Time // exp
}

// VerifyError carries the reason a token was rejected. It matches ErrInvalid
// or ErrExpired through errors.Is; Kid and Reason are safe to log, and the
// token is never kept.
type VerifyError struct {
	Class  error
	Kid    string
	Reason string
}

// Error describes the failure without the token.
func (e *VerifyError) Error() string {
	return fmt.Sprintf("%s: %s", e.Class.Error(), e.Reason)
}

// Unwrap returns ErrInvalid or ErrExpired.
func (e *VerifyError) Unwrap() error {
	return e.Class
}

type jsonClaims struct {
	Iss   *string `json:"iss"`
	Aud   *string `json:"aud"`
	Sub   string  `json:"sub"`
	Sid   string  `json:"sid"`
	Ksid  string  `json:"ksid"`
	Tid   *string `json:"tid"`
	Tslug *string `json:"tslug"`
	Role  *string `json:"role"`
	Plat  *bool   `json:"plat"`
	CSRF  string  `json:"csrf"`
	Iat   *int64  `json:"iat"`
	Exp   *int64  `json:"exp"`
}

// Codec encodes and verifies tokens with the fixed algorithm.
type Codec struct {
	keyring *Keyring
	clock   clock.Clock
}

// NewCodec returns a Codec over k and c; a nil argument is a programmer error and panics.
func NewCodec(k *Keyring, c clock.Clock) *Codec {
	if k == nil || c == nil {
		panic("session: NewCodec requires a keyring and a clock")
	}
	return &Codec{keyring: k, clock: c}
}

// Encode signs claims with the active key.
func (c *Codec) Encode(claims Claims) (string, error) {
	err := validateClaims(claims)
	if err != nil {
		return "", fmt.Errorf("session: encode: %w", err)
	}
	header, err := json.Marshal(map[string]string{"alg": algorithm, "typ": tokenType, "kid": c.keyring.signingKID})
	if err != nil {
		return "", fmt.Errorf("session: encode header: %w", err)
	}
	iat := claims.IssuedAt.Unix()
	exp := claims.ExpiresAt.Unix()
	plat := claims.PlatformOperator
	issuerValue, audienceValue := issuer, audience
	body, err := json.Marshal(jsonClaims{
		Iss: &issuerValue, Aud: &audienceValue,
		Sub: claims.AccountID, Sid: claims.SessionID, Ksid: claims.KratosSessionID,
		Tid: claims.TenantID, Tslug: claims.TenantSlug, Role: claims.Role,
		Plat: &plat, CSRF: claims.CSRF, Iat: &iat, Exp: &exp,
	})
	if err != nil {
		return "", fmt.Errorf("session: encode claims: %w", err)
	}
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(body)
	signature := ed25519.Sign(c.keyring.signingKey, []byte(signingInput))
	token := signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
	if len(token) > maxTokenSize {
		return "", errors.New("session: encode: token exceeds the size limit")
	}
	return token, nil
}

// Verify checks signature and every claim rule against now.
// It returns ErrExpired (signature valid, exp passed) or ErrInvalid.
func (c *Codec) Verify(token string) (Claims, error) {
	claims, err := c.verify(token, 0, false)
	return claims, err
}

// VerifyAllowExpired is Verify that accepts a passed exp while now - iat <= maxAge.
func (c *Codec) VerifyAllowExpired(token string, maxAge time.Duration) (Claims, error) {
	claims, err := c.verify(token, maxAge, true)
	return claims, err
}

func (c *Codec) verify(token string, maxAge time.Duration, allowExpired bool) (Claims, error) {
	segments := strings.Split(token, ".")
	if len(token) > maxTokenSize || len(segments) != segmentCount {
		return Claims{}, reject("", "malformed token")
	}
	kid, err := parseHeader(segments[0])
	if err != nil {
		return Claims{}, reject("", err.Error())
	}
	publicKey, known := c.keyring.verifyKeys[kid]
	if !known {
		return Claims{}, reject(kid, "unknown kid")
	}
	signature, err := strictBase64.DecodeString(segments[2])
	if err != nil || len(signature) != ed25519.SignatureSize {
		return Claims{}, reject(kid, "malformed signature")
	}
	signingInput := segments[0] + "." + segments[1]
	if !ed25519.Verify(publicKey, []byte(signingInput), signature) {
		return Claims{}, reject(kid, "bad signature")
	}
	claims, err := parseClaims(segments[1])
	if err != nil {
		return Claims{}, reject(kid, err.Error())
	}
	now := c.clock.Now()
	if claims.IssuedAt.Sub(now) > maxFutureSkew {
		return Claims{}, reject(kid, "iat in the future")
	}
	isExpired := !now.Before(claims.ExpiresAt)
	if !isExpired {
		return claims, nil
	}
	isWithinMaxAge := allowExpired && now.Sub(claims.IssuedAt) <= maxAge
	if isWithinMaxAge {
		return claims, nil
	}
	return Claims{}, &VerifyError{Class: ErrExpired, Kid: kid, Reason: "exp passed"}
}

// reject builds an ErrInvalid error; kid is kept only when it is well formed,
// so attacker-chosen text never reaches a log.
func reject(kid, reason string) error {
	safeKid := ""
	if kidPattern.MatchString(kid) {
		safeKid = kid
	}
	return &VerifyError{Class: ErrInvalid, Kid: safeKid, Reason: reason}
}

// parseHeader returns kid after checking the header is exactly {alg, typ, kid}.
func parseHeader(segment string) (string, error) {
	raw, err := strictBase64.DecodeString(segment)
	if err != nil {
		return "", errors.New("malformed header")
	}
	fields := map[string]string{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	err = decoder.Decode(&fields)
	if err != nil || decoder.More() || len(fields) != len(headerKeys) {
		return "", errors.New("malformed header")
	}
	for _, key := range headerKeys {
		_, present := fields[key]
		if !present {
			return "", errors.New("malformed header")
		}
	}
	if fields["alg"] != algorithm || fields["typ"] != tokenType {
		return "", errors.New("unsupported alg or typ")
	}
	return fields["kid"], nil
}

// parseClaims decodes the payload of a token whose signature already verified.
func parseClaims(segment string) (Claims, error) {
	raw, err := strictBase64.DecodeString(segment)
	if err != nil {
		return Claims{}, errors.New("malformed claims")
	}
	var decoded jsonClaims
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&decoded)
	if err != nil || decoder.More() {
		return Claims{}, errors.New("malformed claims")
	}
	hasRequired := decoded.Iss != nil && decoded.Aud != nil && decoded.Plat != nil &&
		decoded.Iat != nil && decoded.Exp != nil
	if !hasRequired {
		return Claims{}, errors.New("missing claim")
	}
	if *decoded.Iss != issuer || *decoded.Aud != audience {
		return Claims{}, errors.New("wrong iss or aud")
	}
	claims := Claims{
		AccountID: decoded.Sub, SessionID: decoded.Sid, KratosSessionID: decoded.Ksid,
		TenantID: decoded.Tid, TenantSlug: decoded.Tslug, Role: decoded.Role,
		PlatformOperator: *decoded.Plat, CSRF: decoded.CSRF,
		IssuedAt:  time.Unix(*decoded.Iat, 0).UTC(),
		ExpiresAt: time.Unix(*decoded.Exp, 0).UTC(),
	}
	err = validateClaims(claims)
	if err != nil {
		return Claims{}, err
	}
	return claims, nil
}

// validateClaims applies the shape rules shared by Encode and Verify.
func validateClaims(claims Claims) error {
	hasIdentity := claims.AccountID != "" && claims.SessionID != "" &&
		claims.KratosSessionID != "" && claims.CSRF != ""
	if !hasIdentity {
		return errors.New("sub, sid, ksid and csrf must be non-empty")
	}
	setCount := 0
	for _, isSet := range []bool{claims.TenantID != nil, claims.TenantSlug != nil, claims.Role != nil} {
		if isSet {
			setCount++
		}
	}
	if setCount != 0 && setCount != 3 {
		return errors.New("tid, tslug and role must be all set or all null")
	}
	if setCount == 3 {
		isTenantValid := *claims.TenantID != "" && *claims.TenantSlug != "" && isAllowedRole(*claims.Role)
		if !isTenantValid {
			return errors.New("invalid tid, tslug or role")
		}
	}
	if claims.ExpiresAt.Unix() <= claims.IssuedAt.Unix() {
		return errors.New("exp must be after iat by at least one second")
	}
	return nil
}

// isAllowedRole reports whether role is one of the three tenant roles.
func isAllowedRole(role string) bool {
	switch role {
	case "admin", "member", "viewer":
		return true
	}
	return false
}
