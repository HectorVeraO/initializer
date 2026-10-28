// Package forms exercises every import form.
package forms

import (
	"encoding/json"
	"net/http"
)

import c "example.com/fixture/srv/internal/platform/core"

import (
	"example.com/fixture/srv"
	_ "example.com/fixture/srv/blank"
	. "example.com/fixture/srv/dot"
	named "example.com/fixture/srv/internal/named"
)

var _ = json.Marshal
var _ = http.StatusOK
var _ = c.X
