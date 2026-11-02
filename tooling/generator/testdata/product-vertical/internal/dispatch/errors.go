package dispatch

import "example.com/fixture/srv/internal/platform/core/api"

// CodeReferenceTaken answers 409 when a job with the reference is already
// queued. api.Registry lists the core and catalog codes; a vertical declares
// its own here, because a generated project never edits core.
const CodeReferenceTaken api.Code = "DISPATCH_REFERENCE_TAKEN"
