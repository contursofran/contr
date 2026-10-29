package usage

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// codexLine is one Codex token_count line: 4 % used of a window that resets
// far in the future.
const codexLine = `{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":4.0,"window_minutes":300,"resets_at":4102444800}}}}`

// farReset is a reset time far after the wall clock (Unix seconds).
const farReset = uint64(4_102_444_800)

func windowOf(used float64, resetsAt uint64) []Window {
	return []Window{{Kind: "five_hour", UsedPercent: used, ResetsAt: &resetsAt}}
}

// fixture is plans with Claude and Codex at the given used percents. A nil
// pointer leaves a plan without data.
func fixture(claudeUsed, codexUsed *float64) Reading {
	entry := func(plan PlanID, used *float64) PlanEntry {
		if used == nil {
			return PlanEntry{Plan: plan}
		}
		return PlanEntry{Plan: plan, Usage: &PlanUsage{Plan: plan, Windows: windowOf(*used, farReset)}}
	}
	return Reading{entry(PlanClaude, claudeUsed), entry(PlanCodex, codexUsed)}
}

func p(v float64) *float64 { return &v }

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

func (c *fakeClock) time() time.Time { return time.Unix(1_000_000, 0).Add(c.now) }

// advance moves the time and runs the timers that are due, in order.
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

// fake is a sampler with a fake clock, a synchronous runner and a reader that
// counts calls and returns the plans the test sets.
type fake struct {
	*Sampler
	clock    *fakeClock
	calls    int
	plans    Reading
	notified int
}

func newFake(plans Reading) *fake {
	f := &fake{clock: &fakeClock{}, plans: plans}
	f.Sampler = NewSampler(func() Reading { f.calls++; return f.plans }, func() { f.notified++ })
	f.afterFunc = f.clock.afterFunc
	f.run = func(fn func()) { fn() }
	f.now = f.clock.time
	return f
}

func (f *fake) left(plan PlanID) (float64, bool) { return f.Left(plan, UnixNow()) }

func TestLeftIsTheWholePercentLeftAndNoneWithoutData(t *testing.T) {
	now := UnixNow()
	if l, ok := leftIn(fixture(p(38.4), nil), PlanClaude, now); !ok || l != 62 {
		t.Errorf("claude = %v, %v", l, ok)
	}
	if _, ok := leftIn(fixture(p(38.4), nil), PlanCodex, now); ok {
		t.Error("codex has no data")
	}
	noWindows := Reading{{Plan: PlanClaude, Usage: &PlanUsage{Plan: PlanClaude}}}
	if _, ok := leftIn(noWindows, PlanClaude, now); ok {
		t.Error("a plan with no windows has no data")
	}
}

func TestReadPlansReadsEachSourceInPlansOrder(t *testing.T) {
	state := t.TempDir()
	_, err := WriteSnapshot(state, &ClaudeSnapshot{Schema: 1, UpdatedAt: 1_700_000_000, Windows: windowOf(23.5, farReset)})
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	folder := filepath.Join(home, "sessions/2026/10/23")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "rollout-a.jsonl"), []byte(codexLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	read := ReadPlans(state, home)
	var order []PlanID
	for _, e := range read {
		order = append(order, e.Plan)
	}
	if !reflect.DeepEqual(order, Plans[:]) {
		t.Errorf("order = %v", order)
	}
	if l, _ := leftIn(read, PlanClaude, UnixNow()); l != 77 {
		t.Errorf("claude left = %v", l)
	}
	if l, _ := leftIn(read, PlanCodex, UnixNow()); l != 96 {
		t.Errorf("codex left = %v", l)
	}
}

func TestReadPlansGivesNoneForAMissingOrUnusableSource(t *testing.T) {
	empty, corrupt := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(corrupt, "usage"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corrupt, ClaudeFile), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, read := range map[string]Reading{
		"unusable": ReadPlans(corrupt, empty),
		"unset":    ReadPlans("", ""),
	} {
		for _, e := range read {
			if e.Usage != nil {
				t.Errorf("%s: %s has usage", name, e.Plan)
			}
		}
	}
}

func TestReadsAtOnceThenOnceAMinute(t *testing.T) {
	f := newFake(fixture(p(38.4), nil))
	f.SetVisible(true)
	if f.calls != 1 {
		t.Fatalf("calls = %d, the first read is at once", f.calls)
	}
	f.clock.advance(ReadEvery)
	if f.calls != 2 {
		t.Errorf("calls = %d, want 2", f.calls)
	}
	f.clock.advance(ReadEvery)
	if f.calls != 3 {
		t.Errorf("calls = %d, want 3", f.calls)
	}
}

func TestAHiddenWindowTakesNoReads(t *testing.T) {
	f := newFake(fixture(p(38.4), nil))
	f.SetVisible(true)
	f.SetVisible(false)
	if f.clock.pending() != 0 {
		t.Errorf("%d timers pending while hidden", f.clock.pending())
	}
	f.clock.advance(10 * time.Minute)
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1", f.calls)
	}
}

func TestShowingAfterAMinuteReadsAtOnce(t *testing.T) {
	f := newFake(fixture(p(38.4), nil))
	f.SetVisible(true)
	f.SetVisible(false)
	f.clock.advance(10 * time.Minute)
	f.SetVisible(true)
	if f.calls != 2 {
		t.Fatalf("calls = %d, the last read is ten minutes old", f.calls)
	}
	f.clock.advance(ReadEvery)
	if f.calls != 3 {
		t.Errorf("calls = %d, want 3", f.calls)
	}
}

func TestShowingWithinAMinuteWaitsForTheTimer(t *testing.T) {
	f := newFake(fixture(p(38.4), nil))
	f.SetVisible(true)
	f.SetVisible(false)
	f.clock.advance(10 * time.Second)
	f.SetVisible(true)
	if f.calls != 1 {
		t.Fatalf("calls = %d, the last read is fresh", f.calls)
	}
	f.clock.advance(50 * time.Second)
	if f.calls != 2 {
		t.Errorf("calls = %d, the timer reads at 60 s", f.calls)
	}
}

func TestNotifiesOnlyWhenAShownPercentChanges(t *testing.T) {
	f := newFake(fixture(p(38.4), p(88)))
	f.SetVisible(true)
	if f.notified != 1 {
		t.Fatalf("notified = %d, the first read shows values", f.notified)
	}
	if l, ok := f.left(PlanClaude); !ok || l != 62 {
		t.Errorf("claude = %v, %v", l, ok)
	}

	// 38.3 used is 61.7 left, which shows as 62%: nothing to repaint.
	f.plans = fixture(p(38.3), p(88))
	f.clock.advance(ReadEvery)
	if f.calls != 2 || f.notified != 1 {
		t.Errorf("calls %d, notified %d", f.calls, f.notified)
	}

	f.plans = fixture(p(45), p(88))
	f.clock.advance(ReadEvery)
	if f.notified != 2 {
		t.Errorf("notified = %d, want 2", f.notified)
	}
	if l, _ := f.left(PlanClaude); l != 55 {
		t.Errorf("claude = %v", l)
	}

	// The Claude source has no data any more: the plan shows no value.
	f.plans = fixture(nil, p(88))
	f.clock.advance(ReadEvery)
	if f.notified != 3 {
		t.Errorf("notified = %d, want 3", f.notified)
	}
	if _, ok := f.left(PlanClaude); ok {
		t.Error("claude should have no value")
	}
}

func TestAResetWithAnUnchangedFileNotifiesOnce(t *testing.T) {
	reset := farReset
	plans := Reading{{Plan: PlanClaude}, {Plan: PlanCodex, Usage: &PlanUsage{Plan: PlanCodex, Windows: windowOf(88, reset)}}}
	f := newFake(plans)
	readAt := f.clock.time()
	// Before the reset the bar shows 12 %.
	f.shown = shownOf(plans, reset-1000)

	f.mu.Lock()
	before := f.recordLocked(plans, readAt, reset-1000)
	// The window resets after the last read, and the source file does not change.
	atReset := f.recordLocked(plans, readAt, reset+1)
	// The next read sees the same reset, so there is nothing new to show.
	after := f.recordLocked(plans, readAt, reset+60)
	f.mu.Unlock()

	if before || !atReset || after {
		t.Errorf("notify: before %v, at reset %v, after %v", before, atReset, after)
	}
	if l, _ := f.Left(PlanCodex, reset+1); l != 100 {
		t.Errorf("codex left = %v", l)
	}
}

func TestCloseStopsReading(t *testing.T) {
	f := newFake(fixture(p(38.4), nil))
	f.SetVisible(true)
	f.Close()
	f.clock.advance(time.Hour)
	if f.calls != 1 || f.clock.pending() != 0 {
		t.Errorf("calls %d, timers %d after Close", f.calls, f.clock.pending())
	}
	g := newFake(fixture(p(38.4), nil))
	g.Close()
	g.SetVisible(true)
	if g.calls != 0 {
		t.Errorf("a closed sampler read %d times", g.calls)
	}
}

func TestAReadThatEndsWhileHiddenArmsNoTimer(t *testing.T) {
	f := newFake(fixture(p(38.4), nil))
	f.run = func(fn func()) {} // reads stay pending until the test runs them
	f.SetVisible(true)
	f.SetVisible(false)
	f.readOnce() // the read ends while hidden
	if f.clock.pending() != 0 {
		t.Errorf("%d timers pending while hidden", f.clock.pending())
	}
	f.SetVisible(true) // fresh read: waits for the timer
	if f.calls != 1 || f.clock.pending() != 1 {
		t.Errorf("calls %d, timers %d", f.calls, f.clock.pending())
	}
}

func TestShowingDuringAReadStartsNoSecondRead(t *testing.T) {
	f := newFake(fixture(p(38.4), nil))
	var queued []func()
	f.run = func(fn func()) { queued = append(queued, fn) }
	f.SetVisible(true)
	f.SetVisible(false)
	f.SetVisible(true)
	if len(queued) != 1 {
		t.Fatalf("%d reads started, want 1", len(queued))
	}
	queued[0]()
	if f.calls != 1 || f.clock.pending() != 1 {
		t.Errorf("calls %d, timers %d", f.calls, f.clock.pending())
	}
}
