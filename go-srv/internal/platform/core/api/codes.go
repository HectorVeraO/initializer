package api

import (
	"fmt"
	"net/http"
	"regexp"
)

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
	CodeTenantAccessDenied     Code = "TENANT_ACCESS_DENIED"
	CodeTenantSuspended        Code = "TENANT_SUSPENDED"
	CodeMembershipRevoked      Code = "MEMBERSHIP_REVOKED"
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
	CodeBodyMalformed      Code = "BODY_MALFORMED"
	CodeFieldRequired      Code = "FIELD_REQUIRED"
	CodeFieldTypeInvalid   Code = "FIELD_TYPE_INVALID"
	CodeFieldTooShort      Code = "FIELD_TOO_SHORT"
	CodeFieldTooLong       Code = "FIELD_TOO_LONG"
	CodeFieldOutOfRange    Code = "FIELD_OUT_OF_RANGE"
	CodeFieldInvalidFmt    Code = "FIELD_INVALID_FORMAT"
	CodeFieldNotAllowed    Code = "FIELD_NOT_ALLOWED"
	CodeFieldDuplicate     Code = "FIELD_DUPLICATE"
	CodeFieldMinItems      Code = "FIELD_MIN_ITEMS"
	CodeScopeLevelMismatch Code = "SCOPE_LEVEL_MISMATCH"
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
	raisedByTenancy    = "tenancy"
	raisedByParameters = "parameters"
	raisedByFlags      = "feature-flags"
	raisedByAPIKeys    = "api-keys"
)

// codeShape is the SCREAMING_SNAKE rule every registered code satisfies.
var codeShape = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)*$`)

// Registry returns a fresh copy of the authoritative table of the core and
// catalog module error and issue codes, each once. A product vertical keeps
// its own codes in its own errors.go and never adds them here; Fail never
// consults the registry. A code is contract: renaming one is a breaking
// change.
func Registry() []CodeInfo {
	return []CodeInfo{
		{CodeSessionInvalid, http.StatusUnauthorized, false, raisedByCore},
		{CodeAPIKeyInvalidOrRevoked, http.StatusUnauthorized, false, "core, api-keys"},
		{CodeCSRFTokenInvalid, http.StatusForbidden, false, raisedByCore},
		{CodeInsufficientScope, http.StatusForbidden, false, raisedByCore},
		{CodeInsufficientRole, http.StatusForbidden, false, raisedByCore},
		{CodePlatformOperatorNeeded, http.StatusForbidden, false, raisedByCore},
		{CodeActiveTenantRequired, http.StatusForbidden, false, raisedByCore},
		{CodeTenantMismatch, http.StatusForbidden, false, raisedByCore},
		{CodeTenantAccessDenied, http.StatusForbidden, false, "core, tenancy"},
		{CodeTenantSuspended, http.StatusForbidden, false, "tenancy, api-keys"},
		{CodeMembershipRevoked, http.StatusForbidden, false, raisedByTenancy},
		{"ACCOUNT_LOCKED_OR_DISABLED", http.StatusForbidden, false, "identity, tenancy"},
		{"INVITATION_EMAIL_MISMATCH", http.StatusForbidden, false, raisedByTenancy},
		{CodeResourceNotFound, http.StatusNotFound, false, "core, every module"},
		{CodeRouteNotFound, http.StatusNotFound, false, raisedByCore},
		{"FLAG_NOT_DEFINED_IN_MANIFEST", http.StatusNotFound, false, raisedByFlags},
		{"INVITATION_INVALID", http.StatusNotFound, false, raisedByTenancy},
		{CodeCredentialConflict, http.StatusConflict, false, raisedByCore},
		{CodeActiveTenantChanged, http.StatusConflict, false, raisedByCore},
		{"LAST_TENANT_ADMIN", http.StatusConflict, false, raisedByTenancy},
		{"MEMBERSHIP_ALREADY_ACTIVE", http.StatusConflict, false, raisedByTenancy},
		{"INVITATION_NOT_PENDING", http.StatusConflict, false, raisedByTenancy},
		{"INVITATION_REQUIRED", http.StatusConflict, false, raisedByTenancy},
		{"TENANT_SLUG_TAKEN", http.StatusConflict, false, raisedByTenancy},
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
		{CodeScopeLevelMismatch, http.StatusUnprocessableEntity, true, raisedByAPIKeys},
		{"PARAMETER_KEY_INVALID", http.StatusUnprocessableEntity, true, raisedByParameters},
		{"PARAMETER_TYPE_MISMATCH", http.StatusUnprocessableEntity, true, raisedByParameters},
		{"PARAMETER_OUT_OF_RANGE", http.StatusUnprocessableEntity, true, raisedByParameters},
		{"PARAMETER_VALUE_NOT_ALLOWED", http.StatusUnprocessableEntity, true, raisedByParameters},
		{"PARAMETER_CONSTRAINT_INVALID", http.StatusUnprocessableEntity, true, raisedByParameters},
		{"PARAMETER_FIELD_IMMUTABLE", http.StatusUnprocessableEntity, true, raisedByParameters},
	}
}

// ValidateCodes reports every problem with codes at once, so a product
// vertical's errors_test.go can prove its own codes never collide with the
// generator-owned registry. For each entry it checks that Code is
// SCREAMING_SNAKE, the shape Registry enforces; that Code does not repeat an
// earlier entry of codes; and that Registry does not already hold it. The
// result is empty, never nil, when every code passes. Nothing in production
// calls it.
func ValidateCodes(codes []CodeInfo) []string {
	registered := make(map[Code]struct{})
	for _, info := range Registry() {
		registered[info.Code] = struct{}{}
	}
	problems := []string{}
	seen := make(map[Code]struct{}, len(codes))
	for _, info := range codes {
		code := info.Code
		if !codeShape.MatchString(string(code)) {
			problems = append(problems, fmt.Sprintf("%s: code is not SCREAMING_SNAKE", code))
		}
		if _, repeated := seen[code]; repeated {
			problems = append(problems, fmt.Sprintf("%s: duplicated in the input", code))
		}
		if _, taken := registered[code]; taken {
			problems = append(problems, fmt.Sprintf("%s: already in the registry", code))
		}
		seen[code] = struct{}{}
	}
	return problems
}
