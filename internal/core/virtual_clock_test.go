package core

import (
	"sync"
	"time"
)

// virtualClock is a Clock for tests: time moves only when the test says so.
// Advance moves the wall and the monotonic clock together and fires due
// timers; StepWall sets the wall clock only, like setting the system clock.
type virtualClock struct {
	mu     sync.Mutex
	wall   time.Time
	mono   time.Duration
	timers []*virtualTimer
}

type virtualTimer struct {
	c        chan time.Time
	deadline time.Duration
	fired    bool
	clock    *virtualClock
}

func newVirtualClock(wall time.Time) *virtualClock { return &virtualClock{wall: wall.Round(0)} }

func (c *virtualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wall
}

func (c *virtualClock) Mono() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mono
}

func (c *virtualClock) Read() (time.Time, time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wall, c.mono
}

func (c *virtualClock) NewTimer(d time.Duration) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &virtualTimer{c: make(chan time.Time, 1), deadline: c.mono + d, clock: c}
	c.timers = append(c.timers, t)
	c.fireLocked()
	return t
}

func (c *virtualClock) Advance(d time.Duration) {
	if d < 0 {
		panic("virtualClock: negative Advance")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wall = c.wall.Add(d)
	c.mono += d
	c.fireLocked()
}

func (c *virtualClock) StepWall(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wall = c.wall.Add(d)
}

func (c *virtualClock) fireLocked() {
	kept := c.timers[:0]
	for _, t := range c.timers {
		if !t.fired && t.deadline <= c.mono {
			t.fired = true
			t.c <- c.wall
			continue
		}
		if !t.fired {
			kept = append(kept, t)
		}
	}
	c.timers = kept
}

func (t *virtualTimer) C() <-chan time.Time { return t.c }
func (t *virtualTimer) Stop() {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	t.fired = true
}

// nextDeadline returns the monotonic deadline of the earliest pending timer.
func (c *virtualClock) nextDeadline() (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var best time.Duration
	ok := false
	for _, t := range c.timers {
		if !t.fired && (!ok || t.deadline < best) {
			best, ok = t.deadline, true
		}
	}
	return best, ok
}
