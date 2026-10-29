package host

import (
	"errors"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app/state"
)

func stats(cpu float32, used, total uint64) Stats {
	return Stats{CPUPercent: cpu, MemUsed: used, MemTotal: total}
}

func TestLabelsRoundAndClamp(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{stats(37.5, 0, 1).CPULabel(), "38%"},
		{stats(38.4, 0, 1).CPULabel(), "38%"},
		{stats(120, 0, 1).CPULabel(), "100%"},
		{stats(-3, 0, 1).CPULabel(), "0%"},
		{stats(0, 62, 100).MemLabel(), "62%"},
		{stats(0, 1, 3).MemLabel(), "33%"},
		{stats(0, 12, 10).MemLabel(), "100%"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestMemoryIsZeroWithoutATotal(t *testing.T) {
	if f := stats(0, 5, 0).MemFraction(); f != 0 {
		t.Errorf("fraction = %v", f)
	}
	if l := stats(0, 5, 0).MemLabel(); l != "0%" {
		t.Errorf("label = %q", l)
	}
}

func TestMemBarPixelsRoundTheShareOfTheBar(t *testing.T) {
	cases := []struct {
		s    Stats
		want int
	}{
		{stats(0, 50, 100), 30},
		{stats(0, 1, 3), 20},
		{stats(0, 0, 100), 0},
		{stats(0, 100, 100), 60},
		{stats(0, 12, 10), 60}, // clamped to the bar
		{stats(0, 5, 0), 0},    // no total, no fill
	}
	for _, c := range cases {
		if got := c.s.MemBarPixels(); got != c.want {
			t.Errorf("%+v: %d px, want %d", c.s, got, c.want)
		}
	}
}

func TestSystemSamplerReadsThisHost(t *testing.T) {
	s, err := SystemReader()()
	if err != nil {
		t.Fatal(err)
	}
	if s.MemTotal == 0 {
		t.Error("MemTotal is 0")
	}
	if s.MemUsed == 0 || s.MemUsed > s.MemTotal {
		t.Errorf("MemUsed %d of %d", s.MemUsed, s.MemTotal)
	}
	if s.CPUPercent < 0 || s.CPUPercent > 100 {
		t.Errorf("CPUPercent = %v", s.CPUPercent)
	}
}

// fakeClock runs timers when the test moves its time.
type fakeClock struct {
	now    time.Duration
	timers []*fakeTimer
}

type fakeTimer struct {
	at      time.Duration
	f       func()
	stopped bool
}

func (t *fakeTimer) Stop() bool { was := !t.stopped; t.stopped = true; return was }

func (c *fakeClock) afterFunc(d time.Duration, f func()) stopper {
	t := &fakeTimer{at: c.now + d, f: f}
	c.timers = append(c.timers, t)
	return t
}

// advance moves the time and runs the timers that are due, in order. A timer
// that a step arms inside the window runs too.
func (c *fakeClock) advance(by time.Duration) {
	end := c.now + by
	for {
		var next *fakeTimer
		for _, t := range c.timers {
			if !t.stopped && t.at <= end && (next == nil || t.at < next.at) {
				next = t
			}
		}
		if next == nil {
			c.now = end
			return
		}
		c.now, next.stopped = max(c.now, next.at), true
		next.f()
	}
}

func (c *fakeClock) pending() int {
	n := 0
	for _, t := range c.timers {
		if !t.stopped {
			n++
		}
	}
	return n
}

// fake is a host with a fake clock, a synchronous runner and a reader that
// counts calls and returns the reading the test sets.
type fake struct {
	*Host
	clock    *fakeClock
	calls    int
	reading  Stats
	err      error
	notified int
}

func newFake(cpu float32) *fake {
	f := &fake{clock: &fakeClock{}, reading: stats(cpu, 50, 100)}
	f.Host = New(func() (Stats, error) { f.calls++; return f.reading, f.err }, func() { f.notified++ })
	f.Host.afterFunc = f.clock.afterFunc
	f.Host.run = func(fn func()) { fn() }
	return f
}

func (f *fake) shown() (Stats, bool) { return f.Stats() }

func TestLoopEndsWhenTheHostIsClosedWhileHidden(t *testing.T) {
	f := newFake(40)
	f.Close()
	f.SetVisible(true)
	f.clock.advance(time.Minute)
	if f.calls != 0 || f.clock.pending() != 0 {
		t.Errorf("a closed host sampled: calls %d, timers %d", f.calls, f.clock.pending())
	}
}

func TestLoopEndsWhenTheHostIsGone(t *testing.T) {
	f := newFake(40)
	f.SetVisible(true)
	if f.clock.pending() != 1 {
		t.Fatalf("timers = %d, want the resume gap", f.clock.pending())
	}
	f.Close()
	f.clock.advance(time.Minute)
	if f.calls != 1 || f.clock.pending() != 0 {
		t.Errorf("calls %d, timers %d after Close", f.calls, f.clock.pending())
	}
}

func TestFirstReadingIsDroppedThenOnePerGapAndInterval(t *testing.T) {
	f := newFake(40)
	f.SetVisible(true)
	if f.calls != 1 {
		t.Errorf("calls = %d, the baseline is taken", f.calls)
	}
	if _, ok := f.shown(); ok {
		t.Error("the baseline is shown")
	}
	f.clock.advance(ResumeGap)
	if f.calls != 2 {
		t.Errorf("calls = %d, want 2", f.calls)
	}
	if s, _ := f.shown(); s.CPULabel() != "40%" {
		t.Errorf("shown %q", s.CPULabel())
	}
	f.clock.advance(SampleEvery)
	if f.calls != 3 {
		t.Errorf("calls = %d, want 3", f.calls)
	}
}

func TestHiddenWindowTakesNoReadings(t *testing.T) {
	f := newFake(40)
	f.SetVisible(true)
	f.clock.advance(ResumeGap)
	before := f.calls
	f.SetVisible(false)
	if f.clock.pending() != 0 {
		t.Errorf("%d timers pending while hidden", f.clock.pending())
	}
	f.clock.advance(time.Minute)
	if f.calls != before {
		t.Errorf("calls = %d, want %d", f.calls, before)
	}
}

func TestShowingAgainTakesABaselineBeforePublishing(t *testing.T) {
	f := newFake(40)
	f.SetVisible(true)
	f.clock.advance(ResumeGap)
	f.SetVisible(false)
	f.clock.advance(time.Minute)
	hidden := f.calls

	f.SetVisible(true)
	if f.calls != hidden+1 {
		t.Errorf("calls = %d, want a baseline on show", f.calls)
	}
	f.clock.advance(ResumeGap)
	if f.calls != hidden+2 {
		t.Errorf("calls = %d, want %d", f.calls, hidden+2)
	}
	f.clock.advance(SampleEvery)
	if f.calls != hidden+3 {
		t.Errorf("calls = %d, want %d", f.calls, hidden+3)
	}
}

func TestNotifiesOnlyWhenAShownLabelChanges(t *testing.T) {
	f := newFake(40)
	f.SetVisible(true)
	f.clock.advance(ResumeGap)
	if f.notified != 1 {
		t.Fatalf("notified = %d, the first reading shows a value", f.notified)
	}
	// 40.2 rounds to the same "40%"; the memory label and the bar are the same.
	f.reading = stats(40.2, 50, 100)
	f.clock.advance(SampleEvery)
	if f.calls != 3 || f.notified != 1 {
		t.Errorf("calls %d, notified %d", f.calls, f.notified)
	}
	f.reading = stats(55, 50, 100)
	f.clock.advance(SampleEvery)
	if f.notified != 2 {
		t.Errorf("notified = %d, want 2", f.notified)
	}
	if s, _ := f.shown(); s.CPULabel() != "55%" {
		t.Errorf("shown %q", s.CPULabel())
	}
}

func TestNotifiesWhenTheBarWidthChangesAlone(t *testing.T) {
	f := newFake(40)
	f.SetVisible(true)
	// 6 and 9 of 1000 both show "1%", but the bar moves from 0 px to 1 px.
	f.reading = stats(40, 6, 1000)
	f.clock.advance(ResumeGap)
	if s, _ := f.shown(); s.MemBarPixels() != 0 {
		t.Fatalf("bar = %d px", s.MemBarPixels())
	}
	before := f.notified
	f.reading = stats(40, 9, 1000)
	f.clock.advance(SampleEvery)
	s, _ := f.shown()
	if s.MemLabel() != "1%" || s.MemBarPixels() != 1 {
		t.Errorf("label %q, bar %d px", s.MemLabel(), s.MemBarPixels())
	}
	if f.notified != before+1 {
		t.Errorf("the bar width alone should notify once, got %d", f.notified-before)
	}
}

func TestNotifiesWhenTheMemoryLabelChangesAlone(t *testing.T) {
	f := newFake(40)
	f.SetVisible(true)
	f.clock.advance(ResumeGap)
	f.reading = stats(40, 500, 1000)
	f.clock.advance(SampleEvery)
	before := f.notified
	// 50.5% shows "51%" (the status bar's label) while the bar stays 30 px.
	f.reading = stats(40, 505, 1000)
	f.clock.advance(SampleEvery)
	if f.notified != before+1 {
		t.Errorf("the memory label alone should notify once, got %d", f.notified-before)
	}
	if s, _ := f.shown(); s.MemLabel() != "51%" {
		t.Errorf("label %q", s.MemLabel())
	}
}

// fakeWindow is the window that Visibility.Watch follows.
type fakeWindow struct {
	visible                    bool
	show, hide, minimize, rest func()
}

func (w *fakeWindow) IsVisible() bool { return w.visible }
func (w *fakeWindow) OnShow(fn func()) func() {
	w.show = fn
	return func() {}
}
func (w *fakeWindow) OnHide(fn func()) func() {
	w.hide = fn
	return func() {}
}
func (w *fakeWindow) OnMinimize(fn func()) func() {
	w.minimize = fn
	return func() {}
}
func (w *fakeWindow) OnRestore(fn func()) func() {
	w.rest = fn
	return func() {}
}

func TestHiddenWindowTakesNoReadingsThroughTheWindow(t *testing.T) {
	f := newFake(40)
	w := &fakeWindow{visible: true}
	v := state.NewVisibility(f.SetVisible)
	v.Watch(w)
	f.clock.advance(ResumeGap)
	shownCalls := f.calls

	w.hide()
	f.clock.advance(time.Minute)
	if f.calls != shownCalls {
		t.Errorf("%d readings while hidden", f.calls-shownCalls)
	}
	w.show()
	f.clock.advance(ResumeGap)
	if f.calls <= shownCalls {
		t.Error("readings should resume when shown")
	}
}

func TestFailedReadingIsNotPublished(t *testing.T) {
	f := newFake(40)
	f.err = errors.New("no source")
	f.SetVisible(true)
	f.clock.advance(ResumeGap + SampleEvery)
	if _, ok := f.shown(); ok || f.notified != 0 {
		t.Errorf("a failed reading was published (notified %d)", f.notified)
	}
	if f.calls != 3 {
		t.Errorf("calls = %d, the loop should go on", f.calls)
	}
	f.err = nil
	f.clock.advance(SampleEvery)
	if _, ok := f.shown(); !ok {
		t.Error("the next good reading should show")
	}
}

func TestNewHostHasNoStats(t *testing.T) {
	// A host that was never shown has no stats; the views draw nothing. (Rust detached_host_keeps_its_stats has no Go form: the host has no setter.)
	f := newFake(40)
	if _, ok := f.shown(); ok {
		t.Error("a new host has stats")
	}
}
