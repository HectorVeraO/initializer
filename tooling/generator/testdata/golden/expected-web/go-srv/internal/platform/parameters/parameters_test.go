package parameters

import (
	"testing"

	"example.com/acme/backend/internal/platform/core"
)

func TestName(t *testing.T) {
	core.Run()
	if Name == "" {
		t.Fatal("empty name")
	}
}
