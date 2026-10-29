package state

import (
	"testing"
	"time"
)

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

func (c *fakeClock) advance(by time.Duration) {
	c.now += by
	for _, t := range c.timers {
		if !t.stopped && t.at <= c.now {
			t.stopped = true
			t.f()
		}
	}
}

func newTestMessage() (*StatusMessage, *fakeClock, *int) {
	clock := &fakeClock{}
	n := 0
	m := NewStatusMessage(func() { n++ })
	m.afterFunc = clock.afterFunc
	return m, clock, &n
}

func shown(m *StatusMessage) (string, bool) { return m.Text() }

func TestStatusMessageStartsEmpty(t *testing.T) {
	m, _, _ := newTestMessage()
	if _, ok := shown(m); ok {
		t.Fatal("want empty")
	}
}

func TestStatusMessageShowReplacesTheText(t *testing.T) {
	m, _, _ := newTestMessage()
	m.Show("D7 → By type · sent")
	if s, _ := shown(m); s != "D7 → By type · sent" {
		t.Fatalf("text = %q", s)
	}
	m.Show("T12 done")
	if s, _ := shown(m); s != "T12 done" {
		t.Fatalf("text = %q", s)
	}
}

func TestStatusMessageClearsExactlyAfterTheTTL(t *testing.T) {
	m, clock, _ := newTestMessage()
	m.Show("sent")
	clock.advance(MessageTTL - time.Millisecond)
	if s, ok := shown(m); !ok || s != "sent" {
		t.Fatalf("text = %q, %v", s, ok)
	}
	clock.advance(time.Millisecond)
	if _, ok := shown(m); ok {
		t.Fatal("want cleared")
	}
}

func TestStatusMessageSecondShowRestartsTheCountdown(t *testing.T) {
	m, clock, _ := newTestMessage()
	m.Show("first")
	clock.advance(4 * time.Second)
	m.Show("second")
	clock.advance(4 * time.Second)
	// The first countdown would have ended at 6 s; the second ends at 10 s.
	if s, _ := shown(m); s != "second" {
		t.Fatalf("text = %q", s)
	}
	clock.advance(2 * time.Second)
	if _, ok := shown(m); ok {
		t.Fatal("want cleared")
	}
}

func TestStatusMessageNotifiesOnlyWhenTheTextChanges(t *testing.T) {
	m, clock, n := newTestMessage()
	m.Show("sent")
	if *n != 1 {
		t.Fatalf("notified %d times, want 1", *n)
	}
	// Same text: the countdown restarts, but nothing shown changes.
	m.Show("sent")
	if *n != 1 {
		t.Fatalf("notified %d times, want 1", *n)
	}
	m.Show("saved")
	if *n != 2 {
		t.Fatalf("notified %d times, want 2", *n)
	}
	// The repeated message restarted the countdown.
	clock.advance(MessageTTL)
	if *n != 3 {
		t.Fatalf("notified %d times, want 3", *n)
	}
}

func TestStatusMessageClearingNotifiesOnce(t *testing.T) {
	m, clock, n := newTestMessage()
	m.Show("sent")
	*n = 0
	clock.advance(MessageTTL)
	clock.advance(MessageTTL)
	if *n != 1 {
		t.Fatalf("notified %d times, want 1", *n)
	}
	if _, ok := shown(m); ok {
		t.Fatal("want cleared")
	}
}

func TestStatusMessageStaleTimerDoesNotClear(t *testing.T) {
	m, _, _ := newTestMessage()
	m.Show("first")
	m.mu.Lock()
	oldGen := m.gen
	m.mu.Unlock()
	m.Show("second")
	m.clear(oldGen)
	if s, ok := shown(m); !ok || s != "second" {
		t.Fatalf("text = %q, %v", s, ok)
	}
}
