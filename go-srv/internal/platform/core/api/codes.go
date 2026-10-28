package api

import "net/http"

// Top-level error codes raised by core. Module-raised codes keep their
// constants in their own modules and appear only in Registry.
const (
	CodeSessionInvalid         Code = "SESSION_INVALID"
	CodeAPIKeyInvalidOrRevoked Code = "API_KEY_INVALID_OR_REVOKED"
	CodeCSRFTokenInvalid       Code = "CSRF_TOKEN_INVALID"
	CodeInsufficientScope      Code = "INSUFFICIENT_SCOPE"
	CodeInsufficientRole       Code = "INSUFFICIENT_ROLE"
	CodePlatformOperatorNeeded Code = "PLATFORM_OPERATOR_REQUIRED"
	CodeActiveTenantRequired   Code = "ACTIVE_TENANT_REQUIRED"
	CodeTenantMismatch         Code = "TENANT_MISMATCH"
	CodeResourceNotFound       Code = "RESOURCE_NOT_FOUND"
	CodeRouteNotFound          Code = "ROUTE_NOT_FOUND"
	CodeCredentialConflict     Code = "CREDENTIAL_CONFLICT"
	CodeActiveTenantChanged    Code = "ACTIVE_TENANT_CHANGED"
	CodePayloadTooLarge        Code = "PAYLOAD_TOO_LARGE"
	CodeUnsupportedMediaType   Code = "UNSUPPORTED_MEDIA_TYPE"
	CodeValidationFailed       Code = "VALIDATION_FAILED"
	CodeInternalError          Code = "INTERNAL_ERROR"
	CodeServiceUnavailable     Code = "SERVICE_UNAVAILABLE"
)

// Issue codes raised by core; they appear in error.issues[].code under a
// 422 VALIDATION_FAILED.
const (
	CodeBodyMalformed    Code = "BODY_MALFORMED"
	CodeFieldRequired    Code = "FIELD_REQUIRED"
	CodeFieldTypeInvalid Code = "FIELD_TYPE_INVALID"
	CodeFieldTooShort    Code = "FIELD_TOO_SHORT"
	CodeFieldTooLong     Code = "FIELD_TOO_LONG"
	CodeFieldOutOfRange  Code = "FIELD_OUT_OF_RANGE"
	CodeFieldInvalidFmt  Code = "FIELD_INVALID_FORMAT"
	CodeFieldNotAllowed  Code = "FIELD_NOT_ALLOWED"
	CodeFieldDuplicate   Code = "FIELD_DUPLICATE"
	CodeFieldMinItems    Code = "FIELD_MIN_ITEMS"
)

// CodeInfo describes one registered code.
type CodeInfo struct {
	Code Code
	// Status is the HTTP status of a top-level code; 422 for an issue code.
	Status int
	// Issue is true for codes that appear only in error.issues[].
	Issue bool
	// RaisedBy names the part that raises the code.
	RaisedBy string
}

const (
	raisedByCore       = "core"
	raisedByIdentity   = "identity"
	raisedByParameters = "parameters"
	raisedByFlags      = "feature-flags"
)

// Registry returns a fresh copy of the authoritative table of every error and
// issue code, each once. A code is contract: renaming one is a breaking change.
func Registry() []CodeInfo {
	return []CodeInfo{
		{CodeSessionInvalid, http.StatusUnauthorized, false, raisedByCore},
		{CodeAPIKeyInvalidOrRevoked, http.StatusUnauthorized, false, "core, identity"},
		{CodeCSRFTokenInvalid, http.StatusForbidden, false, raisedByCore},
		{CodeInsufficientScope, http.StatusForbidden, false, raisedByCore},
		{CodeInsufficientRole, http.StatusForbidden, false, raisedByCore},
		{CodePlatformOperatorNeeded, http.StatusForbidden, false, raisedByCore},
		{CodeActiveTenantRequired, http.StatusForbidden, false, raisedByCore},
		{CodeTenantMismatch, http.StatusForbidden, false, raisedByCore},
		{"TENANT_ACCESS_DENIED", http.StatusForbidden, false, raisedByIdentity},
		{"TENANT_SUSPENDED", http.StatusForbidden, false, raisedByIdentity},
		{"MEMBERSHIP_REVOKED", http.StatusForbidden, false, raisedByIdentity},
		{"ACCOUNT_LOCKED_OR_DISABLED", http.StatusForbidden, false, raisedByIdentity},
		{"INVITATION_EMAIL_MISMATCH", http.StatusForbidden, false, raisedByIdentity},
		{CodeResourceNotFound, http.StatusNotFound, false, "core, every module"},
		{CodeRouteNotFound, http.StatusNotFound, false, raisedByCore},
		{"FLAG_NOT_DEFINED_IN_MANIFEST", http.StatusNotFound, false, raisedByFlags},
		{"INVITATION_INVALID", http.StatusNotFound, false, raisedByIdentity},
		{CodeCredentialConflict, http.StatusConflict, false, raisedByCore},
		{CodeActiveTenantChanged, http.StatusConflict, false, raisedByCore},
		{"LAST_TENANT_ADMIN", http.StatusConflict, false, raisedByIdentity},
		{"MEMBERSHIP_ALREADY_ACTIVE", http.StatusConflict, false, raisedByIdentity},
		{"INVITATION_NOT_PENDING", http.StatusConflict, false, raisedByIdentity},
		{"INVITATION_REQUIRED", http.StatusConflict, false, raisedByIdentity},
		{"TENANT_SLUG_TAKEN", http.StatusConflict, false, raisedByIdentity},
		{"IDENTITY_ALREADY_EXISTS", http.StatusConflict, false, raisedByIdentity},
		{"SYSTEM_PARAMETER_PROTECTED", http.StatusConflict, false, raisedByParameters},
		{"PARAMETER_ALREADY_EXISTS", http.StatusConflict, false, raisedByParameters},
		{"PARAMETER_OVERRIDES_VIOLATE_CONSTRAINTS", http.StatusConflict, false, raisedByParameters},
		{CodePayloadTooLarge, http.StatusRequestEntityTooLarge, false, raisedByCore},
		{CodeUnsupportedMediaType, http.StatusUnsupportedMediaType, false, raisedByCore},
		{CodeValidationFailed, http.StatusUnprocessableEntity, false, raisedByCore},
		{CodeInternalError, http.StatusInternalServerError, false, raisedByCore},
		{CodeServiceUnavailable, http.StatusServiceUnavailable, false, raisedByCore},
		{"IDENTITY_PROVIDER_UNAVAILABLE", http.StatusServiceUnavailable, false, raisedByIdentity},

		{CodeBodyMalformed, http.StatusUnprocessableEntity, true, raisedByCore},
		{CodeFieldRequired, http.StatusUnprocessableEntity, true, raisedByCore},
		{CodeFieldTypeInvalid, http.StatusUnprocessableEntity, true, raisedByCore},
		{CodeFieldTooShort, http.StatusUnprocessableEntity, true, raisedByCore},
		{CodeFieldTooLong, http.StatusUnprocessableEntity, true, raisedByCore},
		{CodeFieldOutOfRange, http.StatusUnprocessableEntity, true, raisedByCore},
		{CodeFieldInvalidFmt, http.StatusUnprocessableEntity, true, raisedByCore},
		{CodeFieldNotAllowed, http.StatusUnprocessableEntity, true, raisedByCore},
		{CodeFieldDuplicate, http.StatusUnprocessableEntity, true, raisedByCore},
		{CodeFieldMinItems, http.StatusUnprocessableEntity, true, raisedByCore},
		{"PASSWORD_REJECTED", http.StatusUnprocessableEntity, true, raisedByIdentity},
		{"PARAMETER_KEY_INVALID", http.StatusUnprocessableEntity, true, raisedByParameters},
		{"PARAMETER_TYPE_MISMATCH", http.StatusUnprocessableEntity, true, raisedByParameters},
		{"PARAMETER_OUT_OF_RANGE", http.StatusUnprocessableEntity, true, raisedByParameters},
		{"PARAMETER_VALUE_NOT_ALLOWED", http.StatusUnprocessableEntity, true, raisedByParameters},
		{"PARAMETER_CONSTRAINT_INVALID", http.StatusUnprocessableEntity, true, raisedByParameters},
		{"PARAMETER_FIELD_IMMUTABLE", http.StatusUnprocessableEntity, true, raisedByParameters},
	}
}
