// Package forms exercises every import form.
package forms

import (
	"encoding/json"
	"net/http"
)

import c "example.com/acme/backend/internal/gatbase/core"

import (
	"example.com/acme/backend"
	_ "example.com/acme/backend/blank"
	. "example.com/acme/backend/dot"
	named "example.com/acme/backend/internal/named"
)

var _ = json.Marshal
var _ = http.StatusOK
var _ = c.X
