package clock

import (
	"sync"
	"time"
)

// Clock returns the current time and timers.
type Clock interface {
	Now() time.Time
	// After returns a channel that receives once d has elapsed (immediately if d <= 0).
	After(d time.Duration) <-chan time.Time
}

// Real is the system clock (UTC).
type Real struct{}

// Now returns the current UTC time.
func (Real) Now() time.Time { return time.Now().UTC() }

// After wraps time.After.
func (Real) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Fake is a manually controlled clock for tests.
type Fake struct {
	mu      sync.Mutex
	t       time.Time
	waiters []waiter
}

type waiter struct {
	at time.Time
	ch chan time.Time
}

// NewFake returns a fake clock set to t.
func NewFake(t time.Time) *Fake { return &Fake{t: t.UTC()} }

// Now returns the fake time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

// After fires when Set/Advance moves the clock to or past now+d.
func (f *Fake) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- f.t
		return ch
	}
	f.waiters = append(f.waiters, waiter{at: f.t.Add(d), ch: ch})
	return ch
}

// Waiters returns the number of pending After timers.
func (f *Fake) Waiters() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.waiters)
}

// Set moves the clock to t, firing due timers.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = t.UTC()
	f.fire()
}

// Advance moves the clock forward by d, firing due timers.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
	f.fire()
}

func (f *Fake) fire() {
	kept := f.waiters[:0]
	for _, w := range f.waiters {
		if !w.at.After(f.t) {
			w.ch <- f.t
			continue
		}
		kept = append(kept, w)
	}
	f.waiters = kept
}
