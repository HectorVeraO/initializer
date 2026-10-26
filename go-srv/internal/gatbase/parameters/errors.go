package parameters

import "github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"

// Top-level error codes this module raises; the statuses are in api.Registry.
const (
	// CodeSystemParameterProtected answers 409 when a system-required parameter is deleted.
	CodeSystemParameterProtected api.Code = "SYSTEM_PARAMETER_PROTECTED"
	// CodeParameterAlreadyExists answers 409 when a created key is taken.
	CodeParameterAlreadyExists api.Code = "PARAMETER_ALREADY_EXISTS"
	// CodeOverridesViolateConstraints answers 409 when a replaced definition
	// would leave a tenant override outside its constraints.
	CodeOverridesViolateConstraints api.Code = "PARAMETER_OVERRIDES_VIOLATE_CONSTRAINTS"
)

// Issue codes this module raises under a 422 VALIDATION_FAILED.
const (
	CodeKeyInvalid        api.Code = "PARAMETER_KEY_INVALID"
	CodeTypeMismatch      api.Code = "PARAMETER_TYPE_MISMATCH"
	CodeOutOfRange        api.Code = "PARAMETER_OUT_OF_RANGE"
	CodeValueNotAllowed   api.Code = "PARAMETER_VALUE_NOT_ALLOWED"
	CodeConstraintInvalid api.Code = "PARAMETER_CONSTRAINT_INVALID"
	CodeFieldImmutable    api.Code = "PARAMETER_FIELD_IMMUTABLE"
)
