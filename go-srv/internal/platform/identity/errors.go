package identity

import (
	"errors"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
)

// Error codes the access checks raise; each answers 403.
const (
	CodeAccountLockedOrDisabled api.Code = "ACCOUNT_LOCKED_OR_DISABLED"
	CodeTenantAccessDenied      api.Code = "TENANT_ACCESS_DENIED"
	CodeTenantSuspended         api.Code = "TENANT_SUSPENDED"
	CodeMembershipRevoked       api.Code = "MEMBERSHIP_REVOKED"
)

var (
	// ErrNotFound is returned by a store read that matches no row (or whose id is not a UUID).
	ErrNotFound = errors.New("identity: not found")
	// ErrAlreadyExists is returned when an insert violates a unique constraint.
	ErrAlreadyExists = errors.New("identity: already exists")
)

// CodeIdentityProviderUnavailable is the 503 answered when Kratos cannot
// answer during a session issue or an invitation accept.
const CodeIdentityProviderUnavailable api.Code = "IDENTITY_PROVIDER_UNAVAILABLE"

// Error codes the tenancy services raise; the statuses are in api.Registry.
const (
	CodeTenantSlugTaken         api.Code = "TENANT_SLUG_TAKEN"
	CodeMembershipAlreadyActive api.Code = "MEMBERSHIP_ALREADY_ACTIVE"
	CodeInvitationNotPending    api.Code = "INVITATION_NOT_PENDING"
	CodeInvitationRequired      api.Code = "INVITATION_REQUIRED"
	CodeInvitationInvalid       api.Code = "INVITATION_INVALID"
	CodeInvitationEmailMismatch api.Code = "INVITATION_EMAIL_MISMATCH"
	CodeLastTenantAdmin         api.Code = "LAST_TENANT_ADMIN"
	CodeIdentityAlreadyExists   api.Code = "IDENTITY_ALREADY_EXISTS"
)

// CodePasswordRejected is the issue code under a 422 when the identity
// provider's password policy refuses the password of an accept-new.
const CodePasswordRejected api.Code = "PASSWORD_REJECTED"
