package sidebar

// The age clock (decision D137, open point O10): one shared timer that moves
// the ages of the agent rows once a minute. It runs only while the window is
// visible and at least one age is on screen. Ages also move on every state
// change, because a build reads the clock again.

import (
	"sync"
	"time"
)

// AgeEvery is the time between two moves of the ages.
const AgeEvery = time.Minute

// AgeClock is the one timer of all ages. SetShown and SetVisible run on the UI
// thread; Tick runs on a timer goroutine, so it must be safe from any
// goroutine (Window.Invalidate is).
type AgeClock struct {
	// Tick asks for a redraw, which reads the ages again. Nil: nothing moves.
	Tick func()
	// Every is the time between ticks. Zero means AgeEvery.
	Every time.Duration
	// AfterFunc starts a timer; stop cancels it. Nil uses time.AfterFunc. Tests
	// replace it.
	AfterFunc func(d time.Duration, f func()) (stop func())

	mu      sync.Mutex
	hidden  bool // the window is hidden; the zero value is visible
	shown   bool // at least one age is on screen
	stop    func()
	gen     uint64 // counts the timers started; a late tick of an older one is dropped
	started int    // timers started in all, for tests
}

// SetShown tells the clock whether an age is on screen.
func (a *AgeClock) SetShown(shown bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.shown = shown
	a.sync()
}

// SetVisible tells the clock whether the window shows.
func (a *AgeClock) SetVisible(visible bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.hidden = !visible
	a.sync()
}

// Running tells whether the timer is armed.
func (a *AgeClock) Running() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stop != nil
}

// Started is the number of timers started so far.
func (a *AgeClock) Started() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.started
}

// sync arms the timer when it should run and is not armed, and cancels it
// when it should not run. The caller holds mu.
func (a *AgeClock) sync() {
	want := a.shown && !a.hidden
	switch {
	case want && a.stop == nil:
		a.arm()
	case !want && a.stop != nil:
		a.stop()
		a.stop = nil
		a.gen++
	}
}

// arm starts the timer for the next tick. The caller holds mu.
func (a *AgeClock) arm() {
	every := a.Every
	if every <= 0 {
		every = AgeEvery
	}
	after := a.AfterFunc
	if after == nil {
		after = func(d time.Duration, f func()) func() {
			t := time.AfterFunc(d, f)
			return func() { t.Stop() }
		}
	}
	a.gen++
	gen := a.gen
	a.started++
	a.stop = after(every, func() { a.fire(gen) })
}

// fire is one tick: it asks for a redraw and arms the next tick, unless the
// timer was cancelled meanwhile.
func (a *AgeClock) fire(gen uint64) {
	a.mu.Lock()
	if gen != a.gen || a.stop == nil {
		a.mu.Unlock()
		return
	}
	a.stop = nil
	a.sync() // arms the next tick if the age is still shown and the window visible
	tick := a.Tick
	a.mu.Unlock()
	if tick != nil {
		tick()
	}
}
