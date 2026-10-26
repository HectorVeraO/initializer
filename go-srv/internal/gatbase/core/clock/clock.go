// Package clock is the time boundary of Gatbase code: production code reads
// time through a Clock and tests inject Fixed.
package clock

import "time"

// Clock answers the current time.
type Clock interface {
	Now() time.Time
}

// System is the wall clock in UTC.
type System struct{}

// Now returns the current wall-clock time in UTC.
func (System) Now() time.Time {
	now := time.Now().UTC()
	return now
}

type fixedClock struct {
	at time.Time
}

func (c fixedClock) Now() time.Time {
	return c.at
}

// Fixed returns a clock that always answers t.
func Fixed(t time.Time) Clock {
	return fixedClock{at: t}
}
