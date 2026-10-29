package usage

import (
	"math"
	"sync"
	"time"
)

// ReadEvery is how often each plan is read while the window is visible.
const ReadEvery = 60 * time.Second

// PlanEntry is one plan with its usage. Usage is nil when a read found none.
type PlanEntry struct {
	Plan  PlanID
	Usage *PlanUsage
}

// Reading is every plan in [Plans] order, with its usage when a read found
// any. The status bar draws from it.
type Reading []PlanEntry

// Unread returns every plan with no read yet.
func Unread() Reading {
	out := make(Reading, len(Plans))
	for i, p := range Plans {
		out[i] = PlanEntry{Plan: p}
	}
	return out
}

// ReadPlans reads each plan from its source, in [Plans] order. An empty
// stateDir or codexHome, or a source with no usable data, gives nil usage for
// that plan. It blocks: run it on a goroutine.
func ReadPlans(stateDir, codexHome string) Reading {
	out := Unread()
	for i, p := range Plans {
		switch {
		case p == PlanClaude && stateDir != "":
			out[i].Usage = ReadClaude(stateDir)
		case p == PlanCodex && codexHome != "":
			out[i].Usage = ReadCodex(codexHome)
		}
	}
	return out
}

// FileReader is the real reader: Claude's status line file and Codex's newest
// rollout file. The folders are found at each read.
func FileReader() func() Reading {
	return func() Reading { return ReadPlans(StateDir(), CodexHome()) }
}

// UnixNow is the Unix second now. Window reset times are Unix seconds, so
// [Sampler.Left] takes this value.
func UnixNow() uint64 { return uint64(max(time.Now().Unix(), 0)) }

// leftIn is the whole percent left for plan, as the bar shows it. ok is false
// when the plan has no data.
func leftIn(r Reading, plan PlanID, now uint64) (float64, bool) {
	for _, e := range r {
		if e.Plan != plan || e.Usage == nil {
			continue
		}
		left, ok := e.Usage.LeftPercent(now)
		return math.Round(left), ok
	}
	return 0, false
}

// shownPercent is what the bar shows for one plan.
type shownPercent struct {
	left float64
	ok   bool
}

// shownOf is the shown percent of every plan, in [Plans] order. The low state
// follows from the percent, so comparing percents also compares it.
func shownOf(r Reading, now uint64) [len(Plans)]shownPercent {
	var out [len(Plans)]shownPercent
	for i, p := range Plans {
		out[i].left, out[i].ok = leftIn(r, p, now)
	}
	return out
}

type stopper interface{ Stop() bool }

// Sampler holds the latest plan usage and reads it while the window is
// visible: at once on show when the last read is [ReadEvery] old or older,
// then one read every [ReadEvery]. Hidden: no reads and no timers (D9). It is
// safe to use from several goroutines. onChange runs without the lock held, so
// it should only ask the window to redraw.
//
// Ported from the Rust contr module usage. The async loop becomes a state
// machine in which each step is one timer.
type Sampler struct {
	read     func() Reading
	onChange func()

	// afterFunc, run, and now are replaced in tests: a fake clock, a
	// synchronous runner, and the clock's time.
	afterFunc func(d time.Duration, f func()) stopper
	run       func(f func())
	now       func() time.Time

	mu       sync.Mutex
	visible  bool
	closed   bool
	reading  bool // a read is running
	gen      uint64
	timer    stopper
	plans    Reading
	shown    [len(Plans)]shownPercent
	lastRead time.Time // zero until the first read ends
}

// NewSampler returns a sampler that is hidden: it reads nothing until
// SetVisible(true). onChange may be nil. It runs when a shown percent changes.
func NewSampler(read func() Reading, onChange func()) *Sampler {
	return &Sampler{
		read:      read,
		onChange:  onChange,
		afterFunc: func(d time.Duration, f func()) stopper { return time.AfterFunc(d, f) },
		run:       func(f func()) { go f() },
		now:       time.Now,
		plans:     Unread(),
	}
}

// NewFileSampler returns a sampler that reads the real sources.
func NewFileSampler(onChange func()) *Sampler { return NewSampler(FileReader(), onChange) }

// Plans returns the last read: every plan in [Plans] order.
func (s *Sampler) Plans() Reading {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append(Reading(nil), s.plans...)
}

// Left is the whole percent left for plan at Unix time now. ok is false when
// the plan has no data.
func (s *Sampler) Left(plan PlanID, now uint64) (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return leftIn(s.plans, plan, now)
}

// SetVisible follows the window. Shown: a read at once when the last one is
// [ReadEvery] old or older (or there is none), else a timer for the rest of
// the minute. Hidden: the pending timer stops and nothing is scheduled.
func (s *Sampler) SetVisible(visible bool) {
	s.mu.Lock()
	if s.closed || s.visible == visible {
		s.mu.Unlock()
		return
	}
	s.visible = visible
	s.gen++
	s.stopTimerLocked()
	if !visible || s.reading {
		// A running read arms the timer when it ends.
		s.mu.Unlock()
		return
	}
	age := s.now().Sub(s.lastRead)
	if s.lastRead.IsZero() || age >= ReadEvery {
		s.reading = true
		s.mu.Unlock()
		s.run(s.readOnce)
		return
	}
	s.armLocked(ReadEvery - age)
	s.mu.Unlock()
}

// Close ends the reading for good. A read that is running is dropped.
func (s *Sampler) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.gen++
	s.stopTimerLocked()
}

func (s *Sampler) stopTimerLocked() {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
}

// armLocked arms the next read for the live cycle.
func (s *Sampler) armLocked(d time.Duration) {
	gen := s.gen
	s.timer = s.afterFunc(d, func() { s.run(func() { s.fire(gen) }) })
}

// fire starts the read that a timer asked for, unless the cycle ended.
func (s *Sampler) fire(gen uint64) {
	s.mu.Lock()
	if s.closed || !s.visible || s.gen != gen || s.reading {
		s.mu.Unlock()
		return
	}
	s.reading = true
	s.mu.Unlock()
	s.readOnce()
}

// readOnce reads every plan, stores the result, and arms the next read when
// the window is still visible.
func (s *Sampler) readOnce() {
	plans := s.read()
	now := UnixNow()
	s.mu.Lock()
	s.reading = false
	if s.closed {
		s.mu.Unlock()
		return
	}
	changed := s.recordLocked(plans, s.now(), now)
	if s.visible {
		s.stopTimerLocked()
		s.armLocked(ReadEvery)
	}
	s.mu.Unlock()
	if changed && s.onChange != nil {
		s.onChange()
	}
}

// recordLocked stores the plans and reports whether a percent differs from
// the one the bar last showed. A window can reset between two reads while the
// source file stays the same, so the new percents are compared with the stored
// ones at now, not with the previous plans.
func (s *Sampler) recordLocked(plans Reading, readAt time.Time, now uint64) bool {
	s.plans = plans
	s.lastRead = readAt
	next := shownOf(plans, now)
	if next == s.shown {
		return false
	}
	s.shown = next
	return true
}
