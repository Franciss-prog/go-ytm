// Package clock abstracts time so time-dependent code is testable without
// sleeping. Production code takes a Clock; tests pass a *Fake.
package clock

import (
	"sort"
	"sync"
	"time"
)

// Clock is the subset of the time package the application uses.
type Clock interface {
	Now() time.Time
	// NewTimer returns a timer that fires once after d.
	NewTimer(d time.Duration) Timer
}

// Timer is a one-shot timer.
type Timer interface {
	// C delivers the fire time once.
	C() <-chan time.Time
	// Stop prevents the timer from firing. It reports whether it was active.
	Stop() bool
}

// Real is the wall clock.
type Real struct{}

// Now returns time.Now().
func (Real) Now() time.Time { return time.Now() }

// NewTimer wraps time.NewTimer.
func (Real) NewTimer(d time.Duration) Timer { return realTimer{time.NewTimer(d)} }

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time { return r.t.C }
func (r realTimer) Stop() bool          { return r.t.Stop() }

// Fake is a manually advanced clock. The zero value is not usable; call NewFake.
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	timers  []*fakeTimer
	changed chan struct{} // closed and replaced whenever the timer set changes
}

// NewFake returns a Fake clock set to start.
func NewFake(start time.Time) *Fake {
	return &Fake{now: start, changed: make(chan struct{})}
}

// Now returns the fake time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// NewTimer registers a timer that fires when the clock is advanced past d.
// A non-positive d fires immediately.
func (f *Fake) NewTimer(d time.Duration) Timer {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeTimer{f: f, at: f.now.Add(d), c: make(chan time.Time, 1)}
	if d <= 0 {
		t.c <- f.now
		return t
	}
	f.timers = append(f.timers, t)
	f.notifyLocked()
	return t
}

// Advance moves the clock forward by d and fires every timer due, in order.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
	sort.SliceStable(f.timers, func(i, j int) bool { return f.timers[i].at.Before(f.timers[j].at) })
	kept := f.timers[:0]
	for _, t := range f.timers {
		if t.at.After(f.now) {
			kept = append(kept, t)
			continue
		}
		t.c <- t.at
	}
	clear(f.timers[len(kept):])
	f.timers = kept
	f.notifyLocked()
}

// Pending returns the number of active timers.
func (f *Fake) Pending() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.timers)
}

// BlockUntil waits until at least n timers are pending, or done is closed.
// It reports whether the condition was met. Tests use it to avoid advancing
// the clock before the code under test has armed its timer.
func (f *Fake) BlockUntil(n int, done <-chan struct{}) bool {
	for {
		f.mu.Lock()
		if len(f.timers) >= n {
			f.mu.Unlock()
			return true
		}
		ch := f.changed
		f.mu.Unlock()
		select {
		case <-ch:
		case <-done:
			return false
		}
	}
}

func (f *Fake) notifyLocked() {
	close(f.changed)
	f.changed = make(chan struct{})
}

type fakeTimer struct {
	f  *Fake
	at time.Time
	c  chan time.Time
}

func (t *fakeTimer) C() <-chan time.Time { return t.c }

func (t *fakeTimer) Stop() bool {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	for i, x := range t.f.timers {
		if x == t {
			t.f.timers = append(t.f.timers[:i], t.f.timers[i+1:]...)
			t.f.notifyLocked()
			return true
		}
	}
	return false
}
