package parameters

import (
	"testing"

	"example.com/fixture/srv/internal/platform/core"
)

func TestName(t *testing.T) {
	core.Run()
	if Name == "" {
		t.Fatal("empty name")
	}
}
