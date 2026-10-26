package coreauth

import (
	"errors"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
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
	ErrNotFound = errors.New("coreauth: not found")
	// ErrAlreadyExists is returned when an insert violates a unique constraint.
	ErrAlreadyExists = errors.New("coreauth: already exists")
)

// CodeIdentityProviderUnavailable is the 503 answered when Kratos cannot
// answer during a session issue.
const CodeIdentityProviderUnavailable api.Code = "IDENTITY_PROVIDER_UNAVAILABLE"
