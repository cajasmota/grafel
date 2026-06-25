package watch

import (
	"sort"
	"sync"
	"time"
)

// clock is the minimal time seam the watcher's debounce/bulk path depends on.
// Production uses realClock (the wall clock); tests inject manualClock so that
// debounce/coalesce outcomes are asserted against controlled time rather than
// against the real CI scheduler. This exists ONLY to make the timing-dependent
// behaviour deterministic in tests — production semantics are unchanged.
//
// Only the two operations the debounce/bulk path actually uses are abstracted:
//   - Now()       — the bulk-window arithmetic in recordAndArm.
//   - AfterFunc() — the per-repo debounce timer.
type clock interface {
	Now() time.Time
	// AfterFunc schedules fn to run after d and returns a handle that can be
	// stopped/reset, mirroring *time.Timer's Stop/Reset contract closely enough
	// for the debounce path (Reset is only ever called on a live timer here).
	AfterFunc(d time.Duration, fn func()) timer
}

// timer is the subset of *time.Timer the watcher relies on.
type timer interface {
	Stop() bool
	Reset(d time.Duration) bool
}

// realClock is the production clock backed by the standard library.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) AfterFunc(d time.Duration, fn func()) timer {
	return realTimer{time.AfterFunc(d, fn)}
}

type realTimer struct{ t *time.Timer }

func (r realTimer) Stop() bool                 { return r.t.Stop() }
func (r realTimer) Reset(d time.Duration) bool { return r.t.Reset(d) }

// manualClock is a deterministic, test-only clock. Time only advances when the
// test calls Advance; AfterFunc callbacks fire (synchronously, on the Advance
// goroutine) once their deadline is reached. This lets a test drive the whole
// debounce/coalesce window with zero dependence on the real scheduler.
type manualClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*manualTimer
}

func newManualClock() *manualClock {
	return &manualClock{now: time.Unix(0, 0)}
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) AfterFunc(d time.Duration, fn func()) timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	ft := &manualTimer{clock: c, deadline: c.now.Add(d), fn: fn, active: true}
	c.timers = append(c.timers, ft)
	return ft
}

// Advance moves the clock forward by d and fires every timer whose deadline is
// now at or before the new time, in deadline order. Callbacks run on the
// caller's goroutine, so once Advance returns every due callback has completed.
func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	// Collect due, active timers ordered by deadline.
	var due []*manualTimer
	for _, ft := range c.timers {
		if ft.active && !ft.deadline.After(now) {
			due = append(due, ft)
		}
	}
	sort.Slice(due, func(i, j int) bool { return due[i].deadline.Before(due[j].deadline) })
	for _, ft := range due {
		ft.active = false
	}
	// Drop fired timers from the slice.
	live := c.timers[:0]
	for _, ft := range c.timers {
		if ft.active {
			live = append(live, ft)
		}
	}
	c.timers = live
	c.mu.Unlock()

	for _, ft := range due {
		ft.fn()
	}
}

type manualTimer struct {
	clock    *manualClock
	deadline time.Time
	fn       func()
	active   bool
}

func (t *manualTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	was := t.active
	t.active = false
	return was
}

func (t *manualTimer) Reset(d time.Duration) bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	was := t.active
	t.deadline = t.clock.now.Add(d)
	if !t.active {
		t.active = true
		// Re-add to the live set if it was previously fired/stopped.
		found := false
		for _, ft := range t.clock.timers {
			if ft == t {
				found = true
				break
			}
		}
		if !found {
			t.clock.timers = append(t.clock.timers, t)
		}
	}
	return was
}
