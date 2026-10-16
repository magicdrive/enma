package core

import "time"

// Clock is the scheduler's view of time. Now is the wall clock (it may jump
// when the system clock is set); Mono is monotonic and never jumps. Timers run
// on the monotonic clock. Tests use a virtual clock to make time-dependent
// behaviour deterministic.
type Clock interface {
	Now() time.Time      // wall clock, without a monotonic reading
	Mono() time.Duration // monotonic time since an arbitrary origin
	// Read returns both from one reading, so that comparing them is exact.
	Read() (wall time.Time, mono time.Duration)
	NewTimer(d time.Duration) Timer
}

// Timer fires once on C after its duration.
type Timer interface {
	C() <-chan time.Time
	Stop()
}

type realClock struct{ origin time.Time }

// NewRealClock returns the system clock.
func NewRealClock() Clock { return &realClock{origin: time.Now()} }

func (c *realClock) Now() time.Time      { return time.Now().Round(0) }
func (c *realClock) Mono() time.Duration { return time.Since(c.origin) }
func (c *realClock) Read() (time.Time, time.Duration) {
	t := time.Now()
	return t.Round(0), t.Sub(c.origin)
}
func (c *realClock) NewTimer(d time.Duration) Timer {
	return realTimer{time.NewTimer(d)}
}

type realTimer struct{ t *time.Timer }

func (t realTimer) C() <-chan time.Time { return t.t.C }
func (t realTimer) Stop()               { t.t.Stop() }
