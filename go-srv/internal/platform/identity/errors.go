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

// Error codes the tenancy services raise; the statuses are in api.Registry.
const (
	CodeTenantSlugTaken         api.Code = "TENANT_SLUG_TAKEN"
	CodeMembershipAlreadyActive api.Code = "MEMBERSHIP_ALREADY_ACTIVE"
	CodeInvitationNotPending    api.Code = "INVITATION_NOT_PENDING"
	CodeInvitationRequired      api.Code = "INVITATION_REQUIRED"
	CodeInvitationInvalid       api.Code = "INVITATION_INVALID"
	CodeInvitationEmailMismatch api.Code = "INVITATION_EMAIL_MISMATCH"
	CodeLastTenantAdmin         api.Code = "LAST_TENANT_ADMIN"
	CodeIdentityAlreadyExists            = accounts.CodeIdentityAlreadyExists
)

// CodePasswordRejected is the issue code under a 422 when the identity
// provider's password policy refuses the password of an accept-new.
const CodePasswordRejected = accounts.CodePasswordRejected
