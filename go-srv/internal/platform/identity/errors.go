package identity

import (
	"errors"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/accounts"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
)

// Error codes the access checks raise; each answers 403. The tenant access
// codes (TENANT_ACCESS_DENIED, TENANT_SUSPENDED, MEMBERSHIP_REVOKED) are core's.
const (
	CodeAccountLockedOrDisabled api.Code = "ACCOUNT_LOCKED_OR_DISABLED"
)

var (
	// ErrNotFound is returned by a store read that matches no row (or whose id is not a UUID).
	ErrNotFound = errors.New("identity: not found")
	// ErrAlreadyExists is returned when an insert violates a unique constraint.
	ErrAlreadyExists = errors.New("identity: already exists")
)

// CodeIdentityProviderUnavailable is the 503 answered when Kratos cannot
// answer during a session issue or an invitation accept.
const CodeIdentityProviderUnavailable = accounts.CodeIdentityProviderUnavailable

// CodeIdentityAlreadyExists is the 409 answered when Kratos already has an
// identity for the email of an accept-new.
const CodeIdentityAlreadyExists = accounts.CodeIdentityAlreadyExists

// CodePasswordRejected is the issue code under a 422 when the identity
// provider's password policy refuses the password of an accept-new.
const CodePasswordRejected = accounts.CodePasswordRejected
