// Package host reads the host's CPU and memory for the status bar and the
// sidebar's host card. One reading every [SampleEvery] while the frame window
// is visible; no timers and no readings while it is hidden (D9).
//
// Ported from the Rust contr module host. The GPUI entity and its async loop
// become a small state machine: each step is one timer, so nothing runs
// while the window is hidden. Readings run on their own goroutine, so the UI
// thread never waits for them.
package host

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/contursofran/contr/internal/app/theme"
)

// SampleEvery is the steady interval between readings while the window is
// visible (Activity Monitor's "often").
const SampleEvery = 2 * time.Second

// ResumeGap is the gap between the two readings taken on show. CPU % is the
// change since the previous reading, so the second reading covers only the
// shown time.
const ResumeGap = 250 * time.Millisecond

// Stats is one reading of the whole host.
type Stats struct {
	CPUPercent float32
	MemUsed    uint64
	MemTotal   uint64
}

// MemFraction is the used share of total memory, 0 to 1. It is 0 when the
// total is unknown.
func (s Stats) MemFraction() float32 {
	if s.MemTotal == 0 {
		return 0
	}
	f := float64(s.MemUsed) / float64(s.MemTotal)
	return float32(min(max(f, 0), 1))
}

// CPULabel is the CPU rounded to a whole percent and clamped to 0 to 100,
// for example "38%".
func (s Stats) CPULabel() string {
	return fmt.Sprintf("%d%%", roundClamp(float64(s.CPUPercent)))
}

// MemBarPixels is the filled length of the sidebar's memory bar, in whole
// pixels, 0 to [theme.MemBarWidth].
func (s Stats) MemBarPixels() int {
	return roundHalfAway(s.MemFraction() * theme.MemBarWidth)
}

// MemLabel is the memory share rounded to a whole percent, for example "62%".
func (s Stats) MemLabel() string {
	return fmt.Sprintf("%d%%", roundHalfAway(s.MemFraction()*100))
}

// roundClamp rounds half away from zero (as Rust does) and clamps to 0..100.
func roundClamp(v float64) int {
	if v != v { // NaN
		return 0
	}
	return min(max(roundHalfAway(float32(min(max(v, 0), 100))), 0), 100)
}

// roundHalfAway rounds a value that is not negative to the nearest integer,
// halves up. It uses float32 as Rust does, so the labels match.
func roundHalfAway(v float32) int { return int(v + 0.5) }

// shown is what the views draw from a reading: the CPU label, the memory
// label and the bar's pixels.
type shown struct {
	cpu, mem string
	bar      int
}

func shownOf(s Stats) shown { return shown{s.CPULabel(), s.MemLabel(), s.MemBarPixels()} }

// Reader takes one reading. It may block, so the host calls it off the UI
// thread.
type Reader func() (Stats, error)

type stopper interface{ Stop() bool }

// Host holds the latest reading and samples while the window is visible. It
// is safe to use from several goroutines. onChange runs without the lock
// held, so it should only ask the window to redraw (Window.Update or
// Invalidate).
type Host struct {
	read     Reader
	onChange func()

	// afterFunc and run are replaced in tests: a fake clock, and a runner
	// that is synchronous.
	afterFunc func(d time.Duration, f func()) stopper
	run       func(f func())

	mu      sync.Mutex
	visible bool
	closed  bool
	gen     uint64 // counts show/hide changes; a step of an older cycle stops
	timer   stopper
	stats   Stats
	has     bool
}

// New returns a host that is hidden: it takes no reading until
// SetVisible(true). onChange may be nil. It runs when a shown value changes.
func New(read Reader, onChange func()) *Host {
	return &Host{
		read:      read,
		onChange:  onChange,
		afterFunc: func(d time.Duration, f func()) stopper { return time.AfterFunc(d, f) },
		run:       func(f func()) { go f() },
	}
}

// NewSystem returns a host that reads this machine.
func NewSystem(onChange func()) *Host { return New(SystemReader(), onChange) }

// Stats returns the latest published reading, or false until the first one.
func (h *Host) Stats() (Stats, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stats, h.has
}

// SetVisible follows the window. Shown: one reading whose result is dropped
// (it covers the hidden time), then one after [ResumeGap], then one every
// [SampleEvery]. Hidden: the pending timer stops and nothing else is
// scheduled.
func (h *Host) SetVisible(visible bool) {
	h.mu.Lock()
	if h.closed || h.visible == visible {
		h.mu.Unlock()
		return
	}
	h.visible = visible
	h.gen++
	gen := h.gen
	h.stopTimerLocked()
	h.mu.Unlock()
	if visible {
		h.run(func() { h.baseline(gen) })
	}
}

// Close ends the sampling for good. The reading that is running, if any, is
// dropped.
func (h *Host) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	h.gen++
	h.stopTimerLocked()
}

func (h *Host) stopTimerLocked() {
	if h.timer != nil {
		h.timer.Stop()
		h.timer = nil
	}
}

// current reports whether the cycle of gen is still the live one.
func (h *Host) currentLocked(gen uint64) bool {
	return !h.closed && h.visible && h.gen == gen
}

// baseline takes the reading that is not shown, then waits ResumeGap.
func (h *Host) baseline(gen uint64) {
	_, err := h.read()
	if err != nil {
		log.Printf("host: baseline reading failed: %v", err)
	}
	h.schedule(gen, ResumeGap)
}

// tick takes one reading and publishes it.
func (h *Host) tick(gen uint64) {
	s, err := h.read()
	if err != nil {
		log.Printf("host: reading failed: %v", err)
	}
	h.mu.Lock()
	if !h.currentLocked(gen) {
		h.mu.Unlock()
		return
	}
	changed := false
	if err == nil {
		changed = !h.has || shownOf(h.stats) != shownOf(s)
		h.stats, h.has = s, true
	}
	h.timer = h.afterFunc(SampleEvery, func() { h.run(func() { h.tick(gen) }) })
	h.mu.Unlock()
	if changed && h.onChange != nil {
		h.onChange()
	}
}

// schedule arms the next step of the cycle, unless the cycle ended.
func (h *Host) schedule(gen uint64, d time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.currentLocked(gen) {
		return
	}
	h.timer = h.afterFunc(d, func() { h.run(func() { h.tick(gen) }) })
}
