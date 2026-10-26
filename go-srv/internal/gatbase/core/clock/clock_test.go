package clock_test

import (
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/clock"
)

func TestFixedAlwaysAnswersTheSameInstant(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := clock.Fixed(at)
	if !c.Now().Equal(at) || !c.Now().Equal(at) {
		t.Fatal("fixed clock moved")
	}
}

func TestSystemIsUTCAndCurrent(t *testing.T) {
	before := time.Now()
	got := clock.System{}.Now()
	if got.Location() != time.UTC {
		t.Fatalf("location = %v", got.Location())
	}
	if got.Before(before) || got.After(time.Now()) {
		t.Fatalf("system time %v outside the call window", got)
	}
}
