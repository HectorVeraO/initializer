package featureflags

import "github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"

// CodeFlagNotDefinedInManifest answers 404 when an override targets a key the
// manifest does not declare; the operator routes raise it.
const CodeFlagNotDefinedInManifest api.Code = "FLAG_NOT_DEFINED_IN_MANIFEST"
