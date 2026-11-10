package kit

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestArrivalsCountOnlyARise(t *testing.T) {
	var a Arrivals
	t0 := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	if got := a.Observe("p", 0, t0, true); got != (Arrival{}) {
		t.Errorf("no items: %+v, want none", got)
	}
	first := a.Observe("p", 2, t0, true)
	if first.Run != 1 || !first.At.Equal(t0) {
		t.Fatalf("first items: %+v, want run 1 at t0", first)
	}
	if got := a.Observe("p", 2, t0.Add(time.Second), true); got != first {
		t.Errorf("same count: %+v, want %+v", got, first)
	}
	if got := a.Observe("p", 1, t0.Add(2*time.Second), true); got != first {
		t.Errorf("lower count: %+v, want %+v", got, first)
	}
	t3 := t0.Add(3 * time.Second)
	if got := a.Observe("p", 2, t3, true); got.Run != 2 || !got.At.Equal(t3) {
		t.Errorf("a rise: %+v, want run 2 at t3", got)
	}
	if got := a.Observe("p", 3, t0.Add(4*time.Second), false); got.Run != 2 {
		t.Errorf("a rise that may not start: %+v, want run 2", got)
	}
	if got := a.Observe("q", 1, t3, true); got.Run != 1 {
		t.Errorf("another key: %+v, want its own run 1", got)
	}
	a.Observe("p", 0, t3, true)
	if got := a.Observe("p", 1, t3, true); got.Run != 1 {
		t.Errorf("after 0: %+v, want a new arrival", got)
	}
}

// lookAt runs the effect of an arrival at 0 in a tester and returns its look
// after d.
func lookAt(t *testing.T, badge, reduce bool, d time.Duration) (AttentionLook, *ui.Tester) {
	t.Helper()
	var look AttentionLook
	var a Arrival
	tt := ui.NewTesterWith(func(c *ui.Context) {
		if a.Run == 0 {
			a = Arrival{Run: 1, At: c.Now()}
		}
		e := ui.Box(c).Size(20, 20)
		if badge {
			look = BadgeAttention(c, e, a)
		} else {
			look = NotchAttention(c, e, a)
		}
	}, 100, 100, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: reduce}})
	tt.Advance(d)
	return look, tt
}

func TestTheNotchSpringsGlowsAndPulses(t *testing.T) {
	if l, _ := lookAt(t, false, false, 120*time.Millisecond); l.Scale < 1.05 || l.Scale > 1.07 {
		t.Errorf("scale %v at 120 ms, want 1.06", l.Scale)
	}
	if l, _ := lookAt(t, false, false, 80*time.Millisecond); l.Glow < 0.99 {
		t.Errorf("glow %v at 80 ms, want 1", l.Glow)
	}
	if l, _ := lookAt(t, false, false, AttentionBump+PulseCycle/2); l.Pulse < 0.99 || l.Scale != 1 {
		t.Errorf("look %+v at the dim end of the first pulse, want pulse 1, scale 1", l)
	}
	if l, _ := lookAt(t, false, false, AttentionGlow+20*time.Millisecond); l.Glow > 0.001 {
		t.Errorf("glow %v after 2 s, want 0", l.Glow)
	}
	l, tt := lookAt(t, false, false, NotchAttentionLength+100*time.Millisecond)
	if l != Still || tt.FramePending() {
		t.Errorf("at the end: look %+v, frame asked %v; want still and none", l, tt.FramePending())
	}
}

func TestTheBadgeSpringsAndGlows(t *testing.T) {
	if l, _ := lookAt(t, true, false, 120*time.Millisecond); l.Scale < 1.24 || l.Scale > 1.26 {
		t.Errorf("scale %v at 120 ms, want 1.25", l.Scale)
	}
	if l, _ := lookAt(t, true, false, time.Second); l.Glow <= 0 || l.Pulse != 0 {
		t.Errorf("look %+v at 1 s, want a glow and no pulse", l)
	}
	l, tt := lookAt(t, true, false, BadgeAttentionLength+100*time.Millisecond)
	if l != Still || tt.FramePending() {
		t.Errorf("at the end: look %+v, frame asked %v; want still and none", l, tt.FramePending())
	}
}

func TestReduceMotionShowsOnlyTheStillState(t *testing.T) {
	for _, badge := range []bool{false, true} {
		l, tt := lookAt(t, badge, true, 0)
		if l != Still || tt.FramePending() {
			t.Errorf("badge %v: look %+v, frame asked %v; want still and none", badge, l, tt.FramePending())
		}
	}
}
