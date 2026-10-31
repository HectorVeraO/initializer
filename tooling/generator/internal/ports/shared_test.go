package ports_test

import (
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/ports"
)

func TestASharedPortHasOneOffsetForEveryIndex(t *testing.T) {
	shared := ports.Port{Name: "postgres", Offset: 32, Shared: true}
	perComponent := ports.Port{Name: "kratos-public", Offset: 33, Stride: 2}
	for index := 0; index < 5; index++ {
		if got := shared.OffsetAt(index); got != 32 {
			t.Errorf("shared port at index %d = %d, want 32", index, got)
		}
		if got := perComponent.OffsetAt(index); got != 33+2*index {
			t.Errorf("per-component port at index %d = %d, want %d", index, got, 33+2*index)
		}
	}
}
