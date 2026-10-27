/** Top-level `error.code` values: the `Issue: false` rows of `go-srv/internal/gatbase/core/api/codes.go` `Registry()` (also `specs/core/openapi.yaml` `ApiErrorCode`). */
export const ApiErrorCode = {
  SessionInvalid: 'SESSION_INVALID',
  ApiKeyInvalidOrRevoked: 'API_KEY_INVALID_OR_REVOKED',
  CsrfTokenInvalid: 'CSRF_TOKEN_INVALID',
  InsufficientScope: 'INSUFFICIENT_SCOPE',
  InsufficientRole: 'INSUFFICIENT_ROLE',
  PlatformOperatorRequired: 'PLATFORM_OPERATOR_REQUIRED',
  ActiveTenantRequired: 'ACTIVE_TENANT_REQUIRED',
  TenantMismatch: 'TENANT_MISMATCH',
  TenantAccessDenied: 'TENANT_ACCESS_DENIED',
  TenantSuspended: 'TENANT_SUSPENDED',
  MembershipRevoked: 'MEMBERSHIP_REVOKED',
  AccountLockedOrDisabled: 'ACCOUNT_LOCKED_OR_DISABLED',
  InvitationEmailMismatch: 'INVITATION_EMAIL_MISMATCH',
  ResourceNotFound: 'RESOURCE_NOT_FOUND',
  RouteNotFound: 'ROUTE_NOT_FOUND',
  FlagNotDefinedInManifest: 'FLAG_NOT_DEFINED_IN_MANIFEST',
  InvitationInvalid: 'INVITATION_INVALID',
  CredentialConflict: 'CREDENTIAL_CONFLICT',
  ActiveTenantChanged: 'ACTIVE_TENANT_CHANGED',
  LastTenantAdmin: 'LAST_TENANT_ADMIN',
  MembershipAlreadyActive: 'MEMBERSHIP_ALREADY_ACTIVE',
  InvitationNotPending: 'INVITATION_NOT_PENDING',
  InvitationRequired: 'INVITATION_REQUIRED',
  TenantSlugTaken: 'TENANT_SLUG_TAKEN',
  IdentityAlreadyExists: 'IDENTITY_ALREADY_EXISTS',
  SystemParameterProtected: 'SYSTEM_PARAMETER_PROTECTED',
  ParameterAlreadyExists: 'PARAMETER_ALREADY_EXISTS',
  ParameterOverridesViolateConstraints: 'PARAMETER_OVERRIDES_VIOLATE_CONSTRAINTS',
  PayloadTooLarge: 'PAYLOAD_TOO_LARGE',
  UnsupportedMediaType: 'UNSUPPORTED_MEDIA_TYPE',
  ValidationFailed: 'VALIDATION_FAILED',
  InternalError: 'INTERNAL_ERROR',
  ServiceUnavailable: 'SERVICE_UNAVAILABLE',
  IdentityProviderUnavailable: 'IDENTITY_PROVIDER_UNAVAILABLE'
} as const;

export type ApiErrorCode = (typeof ApiErrorCode)[keyof typeof ApiErrorCode];

/** Core issue codes (`error.issues[].code`, the `Issue: true` rows of `Registry()` raised by core); module issue codes (`PASSWORD_REJECTED`, `PARAMETER_*`) live in their module folder. */
export const IssueCode = {
  BodyMalformed: 'BODY_MALFORMED',
  FieldRequired: 'FIELD_REQUIRED',
  FieldTypeInvalid: 'FIELD_TYPE_INVALID',
  FieldTooShort: 'FIELD_TOO_SHORT',
  FieldTooLong: 'FIELD_TOO_LONG',
  FieldOutOfRange: 'FIELD_OUT_OF_RANGE',
  FieldInvalidFormat: 'FIELD_INVALID_FORMAT',
  FieldNotAllowed: 'FIELD_NOT_ALLOWED',
  FieldDuplicate: 'FIELD_DUPLICATE',
  FieldMinItems: 'FIELD_MIN_ITEMS'
} as const;

export type IssueCode = (typeof IssueCode)[keyof typeof IssueCode];
