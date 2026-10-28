package tenancy

import (
	"errors"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
)

// Error codes the tenancy services raise; the statuses are in api.Registry.
// The tenant access codes (TENANT_ACCESS_DENIED, TENANT_SUSPENDED,
// MEMBERSHIP_REVOKED) are core's.
const (
	CodeTenantSlugTaken         api.Code = "TENANT_SLUG_TAKEN"
	CodeMembershipAlreadyActive api.Code = "MEMBERSHIP_ALREADY_ACTIVE"
	CodeInvitationNotPending    api.Code = "INVITATION_NOT_PENDING"
	CodeInvitationRequired      api.Code = "INVITATION_REQUIRED"
	CodeInvitationInvalid       api.Code = "INVITATION_INVALID"
	CodeInvitationEmailMismatch api.Code = "INVITATION_EMAIL_MISMATCH"
	CodeLastTenantAdmin         api.Code = "LAST_TENANT_ADMIN"
)

// codeAccountLockedOrDisabled is the 403 identity raises for a locked or
// disabled account; accept-invitation raises it for the same account states.
const codeAccountLockedOrDisabled api.Code = "ACCOUNT_LOCKED_OR_DISABLED"

var (
	// ErrNotFound is returned by a store read that matches no row (or whose id is not a UUID).
	ErrNotFound = errors.New("tenancy: not found")
	// ErrAlreadyExists is returned when an insert violates a unique constraint.
	ErrAlreadyExists = errors.New("tenancy: already exists")
)
