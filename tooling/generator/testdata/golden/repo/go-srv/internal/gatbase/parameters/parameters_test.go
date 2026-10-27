package parameters

import (
	"testing"

	"example.com/fixture/srv/internal/gatbase/core"
)

func TestName(t *testing.T) {
	core.Run()
	if Name == "" {
		t.Fatal("empty name")
	}
}
