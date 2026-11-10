package questions

// The attention effect of the notch (D240): spring, glow and a 10 s pulse on
// arrival, then still.

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/kit"
)

// attentionRig is a notch with a fixed clock and the given Reduce Motion.
func attentionRig(t *testing.T, reduce bool) (*notchRig, *source) {
	t.Helper()
	src := &source{}
	r := &notchRig{src: src, project: "a"}
	r.n = &Notch{
		Snapshot: src.Snapshot,
		Project:  func() string { return r.project },
		Open:     func(tab agentstate.NeedTab) { r.opened, r.isOpen = append(r.opened, tab), true },
		Close:    func() { r.closes++; r.isOpen = false },
		Opened:   func() bool { return r.isOpen },
	}
	r.tt = ui.NewTesterWith(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { r.n.View(c) })
	}, 900, 400, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: reduce}})
	return r, src
}

// dotWidth is the width of the notch dot on screen.
func dotWidth(t *testing.T, r *notchRig) float32 {
	t.Helper()
	rect, ok := r.tt.Find(LabelNotchDot)
	if !ok {
		t.Fatalf("no notch dot; texts %q", r.tt.Texts())
	}
	return rect.W
}

// notchWidth is the width of the notch on screen.
func notchWidth(t *testing.T, r *notchRig) float32 {
	t.Helper()
	rect, ok := r.tt.Find(NotchAccessible(r.n.Items()))
	if !ok {
		t.Fatalf("no notch; texts %q", r.tt.Texts())
	}
	return rect.W
}

func TestNotchArrivalBumpsPulsesThenStops(t *testing.T) {
	r, src := attentionRig(t, false)
	src.Snap.Questions = apptest.Questions("a", 1)
	r.tt.Frame()
	if !r.tt.FramePending() {
		t.Fatal("an arrival asks for no frame")
	}
	still := notchWidth(t, r)
	r.tt.Advance(120 * time.Millisecond) // the top of the spring
	if w := notchWidth(t, r); w <= still*1.03 {
		t.Errorf("notch width %v at the top of the spring, want about 1.06 × %v", w, still)
	}
	r.tt.Advance(kit.AttentionBump - 120*time.Millisecond + kit.PulseCycle/2) // the dim end of the first pulse
	if w := dotWidth(t, r); w > notchDotSize*0.85 {
		t.Errorf("dot width %v at the dim end of a pulse, want about 0.8 × %d", w, notchDotSize)
	}
	r.tt.Advance(kit.NotchAttentionLength) // well past the end
	if r.tt.FramePending() {
		t.Error("the notch asks for frames after the effect ended")
	}
	if w := dotWidth(t, r); w != notchDotSize {
		t.Errorf("dot width %v after the effect, want %d", w, notchDotSize)
	}
	if w := notchWidth(t, r); w != still {
		t.Errorf("notch width %v after the effect, want %v", w, still)
	}
	r.tt.Advance(time.Minute)
	if r.tt.FramePending() {
		t.Error("the notch asks for frames while items only wait")
	}
}

func TestNotchPulseLastsTenSeconds(t *testing.T) {
	r, src := attentionRig(t, false)
	src.Snap.Questions = apptest.Questions("a", 1)
	r.tt.Frame()
	r.tt.Advance(kit.NotchAttentionLength - 500*time.Millisecond)
	if !r.tt.FramePending() {
		t.Error("the pulse ended before 10 s")
	}
}

func TestNotchLowerCountStartsNothing(t *testing.T) {
	r, src := attentionRig(t, false)
	src.Snap.Questions = apptest.Questions("a", 2)
	r.tt.Frame()
	r.tt.Advance(kit.NotchAttentionLength + time.Second)
	if r.tt.FramePending() {
		t.Fatal("the effect did not end")
	}
	src.Snap.Questions = apptest.Questions("a", 1)
	r.tt.Frame()
	if r.tt.FramePending() {
		t.Error("a lower count started the effect")
	}
}

func TestNotchArrivalDuringPulseStartsAgain(t *testing.T) {
	r, src := attentionRig(t, false)
	src.Snap.Questions = apptest.Questions("a", 1)
	r.tt.Frame()
	r.tt.Advance(8 * time.Second)
	src.Snap.Questions = apptest.Questions("a", 2)
	r.tt.Frame()
	r.tt.Advance(5 * time.Second) // past the end of the first effect
	if !r.tt.FramePending() {
		t.Error("a second arrival during the pulse did not start the effect again")
	}
	r.tt.Advance(kit.NotchAttentionLength)
	if r.tt.FramePending() {
		t.Error("the second effect does not end")
	}
}

func TestNotchCardStartsNothing(t *testing.T) {
	r, src := attentionRig(t, false)
	src.Snap.Questions = apptest.Questions("a", 1)
	r.tt.Frame()
	r.tt.Advance(kit.NotchAttentionLength + time.Second)
	r.n.Toggle() // opens the card
	r.tt.Frame()
	if r.tt.FramePending() {
		t.Error("opening the card started the effect")
	}
	src.Snap.Questions = apptest.Questions("a", 2) // comes while the card shows
	r.tt.Frame()
	if r.tt.FramePending() {
		t.Error("an item that came while the card shows started the effect")
	}
	r.n.Toggle() // closes the card
	r.tt.Frame()
	if r.tt.FramePending() {
		t.Error("closing the card started the effect")
	}
}

func TestNotchReduceMotionShowsTheStillState(t *testing.T) {
	r, src := attentionRig(t, true)
	src.Snap.Questions = apptest.Questions("a", 1)
	r.tt.Frame()
	if r.tt.FramePending() {
		t.Error("with Reduce Motion an arrival asks for frames")
	}
	if w := dotWidth(t, r); w != notchDotSize {
		t.Errorf("dot width %v, want the solid %d", w, notchDotSize)
	}
}

func TestNotchArrivalsPerProject(t *testing.T) {
	r, src := attentionRig(t, false)
	src.Snap.Questions = apptest.Questions("a", 1)
	r.tt.Frame()
	r.tt.Advance(kit.NotchAttentionLength + time.Second)
	// An item of another project is not an arrival here.
	src.Snap.Questions = append(apptest.Questions("a", 1), apptest.Questions("b", 1)...)
	r.tt.Frame()
	if r.tt.FramePending() {
		t.Error("an item of another project started the effect")
	}
}
