// Package coreauth is the fixture identity module.
package coreauth

import (
	"example.com/acme/backend/internal/gatbase/core"
)

// Name is the module name.
const Name = "core-auth"

var _ = core.Run
