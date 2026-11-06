package kit

import (
	"bytes"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// at returns a time d after a moment where every cycle of the glyphs starts
// (100.8s is a whole number of 90ms, 140ms, 1.6s and 2.4s), so the tests do
// not depend on the clock.
func at(d time.Duration) time.Time {
	return time.Unix(1_008_000, 0).Add(d)
}

func TestBrailleFramesOfWorking(t *testing.T) {
	want := "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
	if got := join(BrailleFrames[:]); got != want {
		t.Fatalf("frames %q, want %q", got, want)
	}
	for i := range BrailleFrames {
		d := time.Duration(i) * WorkingStep
		if got := Braille(GlyphWorking, at(d), false); got != BrailleFrames[i] {
			t.Errorf("working at %v shows %q, want %q", d, got, BrailleFrames[i])
		}
	}
	// The cycle is 0.9s: it starts over.
	if got := Braille(GlyphWorking, at(10*WorkingStep), false); got != BrailleFrames[0] {
		t.Errorf("working after one cycle shows %q, want %q", got, BrailleFrames[0])
	}
	if got := Braille(GlyphWorking, at(WorkingStep-time.Millisecond), false); got != BrailleFrames[0] {
		t.Errorf("working just before the step shows %q, want %q", got, BrailleFrames[0])
	}
}

func TestBrailleFramesOfWorkingSlow(t *testing.T) {
	if 10*WorkingSlowStep != 1400*time.Millisecond {
		t.Fatalf("slow cycle is %v, want 1.4s", 10*WorkingSlowStep)
	}
	for i := range BrailleFrames {
		d := time.Duration(i) * WorkingSlowStep
		if got := Braille(GlyphWorkingSlow, at(d), false); got != BrailleFrames[i] {
			t.Errorf("slow at %v shows %q, want %q", d, got, BrailleFrames[i])
		}
	}
	// At 90ms the fast spinner is on its second frame; the slow one is not.
	if got := Braille(GlyphWorkingSlow, at(WorkingStep), false); got != BrailleFrames[0] {
		t.Errorf("slow at 90ms shows %q, want the first frame", got)
	}
}

func TestStillStatesShowTheirGlyph(t *testing.T) {
	for _, d := range []time.Duration{0, 400 * time.Millisecond, 3 * time.Second} {
		if got := Braille(GlyphIdle, at(d), false); got != "⠶" {
			t.Errorf("idle shows %q, want ⠶", got)
		}
		if got := Braille(GlyphNeedsYou, at(d), false); got != "⠋" {
			t.Errorf("needs you shows %q, want ⠋", got)
		}
		if got := Braille(GlyphWaiting, at(d), false); got != "⠿" {
			t.Errorf("waiting shows %q, want ⠿", got)
		}
	}
}

func TestReduceMotionShowsFirstFrameAndWaiting(t *testing.T) {
	for _, d := range []time.Duration{0, 4 * WorkingStep, 700 * time.Millisecond} {
		if got := Braille(GlyphWorking, at(d), true); got != "⠋" {
			t.Errorf("working with reduce motion shows %q at %v, want ⠋", got, d)
		}
		if got := Braille(GlyphWorkingSlow, at(d), true); got != "⠋" {
			t.Errorf("working slow with reduce motion shows %q at %v, want ⠋", got, d)
		}
		if got := Braille(GlyphWaiting, at(d), true); got != "⠿" {
			t.Errorf("waiting with reduce motion shows %q, want ⠿", got)
		}
		if o := BrailleOpacity(GlyphWaiting, at(d), true); o != 1 {
			t.Errorf("waiting with reduce motion has opacity %v, want 1", o)
		}
		if a := ClaudeAngle(GlyphWorking, at(d), true); a != 0 {
			t.Errorf("claude with reduce motion turned to %v, want 0", a)
		}
	}
}

func TestWaitingFadesBetweenQuarterAndOne(t *testing.T) {
	near := func(got, want float32) bool { return got > want-0.001 && got < want+0.001 }
	if o := BrailleOpacity(GlyphWaiting, at(0), false); !near(o, 0.25) {
		t.Errorf("waiting opacity at 0 is %v, want .25", o)
	}
	if o := BrailleOpacity(GlyphWaiting, at(WaitingCycle/2), false); !near(o, 1) {
		t.Errorf("waiting opacity at half a cycle is %v, want 1", o)
	}
	if o := BrailleOpacity(GlyphWaiting, at(WaitingCycle), false); !near(o, 0.25) {
		t.Errorf("waiting opacity after a cycle is %v, want .25", o)
	}
	for _, s := range []GlyphState{GlyphIdle, GlyphWorking, GlyphWorkingSlow, GlyphNeedsYou} {
		if o := BrailleOpacity(s, at(300*time.Millisecond), false); o != 1 {
			t.Errorf("%v has opacity %v, want 1", s, o)
		}
	}
}

func TestClaudeTurnsOnceIn2400msWhileWorking(t *testing.T) {
	near := func(got, want float32) bool { return got > want-0.01 && got < want+0.01 }
	if a := ClaudeAngle(GlyphWorking, at(0), false); a != 0 {
		t.Errorf("angle at 0 is %v", a)
	}
	if a := ClaudeAngle(GlyphWorking, at(1200*time.Millisecond), false); !near(a, 180) {
		t.Errorf("angle at 1.2s is %v, want 180", a)
	}
	if a := ClaudeAngle(GlyphWorking, at(ClaudeTurn), false); a != 0 {
		t.Errorf("angle after one turn is %v, want 0", a)
	}
	for _, s := range []GlyphState{GlyphIdle, GlyphWorkingSlow, GlyphWaiting, GlyphNeedsYou} {
		if a := ClaudeAngle(s, at(900*time.Millisecond), false); a != 0 {
			t.Errorf("%v turned to %v, want still", s, a)
		}
	}
	if o := ClaudeOpacity(GlyphIdle); o != 0.45 {
		t.Errorf("idle claude opacity %v, want .45", o)
	}
	for _, s := range []GlyphState{GlyphWorking, GlyphWorkingSlow, GlyphWaiting, GlyphNeedsYou} {
		if o := ClaudeOpacity(s); o != 1 {
			t.Errorf("%v claude opacity %v, want 1", s, o)
		}
	}
}

func TestGlyphColorsComeFromTheTokens(t *testing.T) {
	for _, p := range []*theme.Palette{theme.Dark, theme.Light} {
		cases := []struct {
			s      GlyphState
			claude bool
			want   ui.Color
			name   string
		}{
			{GlyphWorking, false, p.Ink100, "working"},
			{GlyphWorkingSlow, false, p.Ink100, "working slow"},
			{GlyphWaiting, false, p.Ink100, "waiting"},
			{GlyphIdle, false, p.Ink30, "idle"},
			{GlyphNeedsYou, false, p.Blocked, "needs you"},
			{GlyphWorking, true, p.Claude, "claude working"},
			{GlyphIdle, true, p.Claude, "claude idle"},
			{GlyphNeedsYou, true, p.Blocked, "claude needs you"},
		}
		for _, c := range cases {
			if got := GlyphColor(p, c.s, c.claude); got != c.want {
				t.Errorf("dark=%v %s color %v, want %v", p.Dark, c.name, got, c.want)
			}
		}
	}
}

func TestGlyphLabels(t *testing.T) {
	want := map[GlyphState]string{
		GlyphWorking:     "working",
		GlyphWorkingSlow: "working",
		GlyphWaiting:     "waiting",
		GlyphIdle:        "idle",
		GlyphNeedsYou:    "needs you",
	}
	for s, label := range want {
		if s.Label() != label {
			t.Errorf("state %d label %q, want %q", s, s.Label(), label)
		}
		for _, claude := range []bool{false, true} {
			s, claude := s, claude
			tt := ui.NewTester(func(c *ui.Context) {
				StatusGlyph(c, GlyphOpts{State: s, Claude: claude})
			}, 100, 100)
			r, ok := tt.Find(label)
			if !ok {
				t.Errorf("no element labelled %q (state %d, claude %v)", label, s, claude)
				continue
			}
			if r.W != 16 || r.H != 16 {
				t.Errorf("%q is %vx%v, want 16x16", label, r.W, r.H)
			}
		}
	}
	tt := ui.NewTester(func(c *ui.Context) {
		StatusGlyph(c, GlyphOpts{State: GlyphIdle, Small: true})
	}, 100, 100)
	if r, ok := tt.Find("idle"); !ok || r.W != 14 {
		t.Errorf("small glyph is %v (found %v), want 14 wide", r, ok)
	}
}

// glyphView shows one glyph on black. Off screen puts it in a clip, below its
// edge.
func glyphView(o GlyphOpts, offScreen bool) func(*ui.Context) {
	return func(c *ui.Context) {
		ui.Column(c).Fill().Background(ui.RGB(0, 0, 0)).Children(func() {
			if !offScreen {
				StatusGlyph(c, o)
				return
			}
			ui.Box(c).Size(40, 40).Clip().Children(func() {
				ui.Column(c).Margin(100, 0, 0, 0).Children(func() { StatusGlyph(c, o) })
			})
		})
	}
}

// paintsOver returns how many times the glyph paints in d of the Tester's
// clock, counting the first paint.
func paintsOver(o GlyphOpts, offScreen, reduce bool, d time.Duration) int64 {
	start := paints.Load()
	tt := ui.NewTester(glyphView(o, offScreen), 100, 100)
	if reduce {
		tt.SetPreferences(ui.Preferences{ReduceMotion: true})
		tt.Frame()
	}
	tt.Advance(d)
	return paints.Load() - start
}

func TestTimerRunsOnlyWhileWorkingOrWaitingOnScreen(t *testing.T) {
	cases := []struct {
		name string
		o    GlyphOpts
		want bool
	}{
		{"working", GlyphOpts{State: GlyphWorking}, true},
		{"working slow", GlyphOpts{State: GlyphWorkingSlow}, true},
		{"waiting", GlyphOpts{State: GlyphWaiting}, true},
		{"idle", GlyphOpts{State: GlyphIdle}, false},
		{"needs you", GlyphOpts{State: GlyphNeedsYou}, false},
		{"claude working", GlyphOpts{State: GlyphWorking, Claude: true}, true},
		{"claude working slow", GlyphOpts{State: GlyphWorkingSlow, Claude: true}, false},
		{"claude waiting", GlyphOpts{State: GlyphWaiting, Claude: true}, false},
		{"claude idle", GlyphOpts{State: GlyphIdle, Claude: true}, false},
		{"claude needs you", GlyphOpts{State: GlyphNeedsYou, Claude: true}, false},
	}
	for _, c := range cases {
		n := paintsOver(c.o, false, false, 2*time.Second)
		if n == 0 {
			t.Errorf("%s never paints", c.name)
		}
		if got := n > 2; got != c.want {
			t.Errorf("%s paints %d times in 2s; timer running is %v, want %v", c.name, n, got, c.want)
		}
	}
}

func TestWorkingPaintsOncePerStep(t *testing.T) {
	// 1.8s is 20 steps of 90ms, and 12.9 of 140ms.
	if n := paintsOver(GlyphOpts{State: GlyphWorking}, false, false, 1800*time.Millisecond); n < 18 || n > 23 {
		t.Errorf("working paints %d times in 1.8s, want about 20 (a step every 90ms)", n)
	}
	if n := paintsOver(GlyphOpts{State: GlyphWorkingSlow}, false, false, 1800*time.Millisecond); n < 11 || n > 15 {
		t.Errorf("working slow paints %d times in 1.8s, want about 13 (a step every 140ms)", n)
	}
}

func TestNoTimerOffScreen(t *testing.T) {
	// The same glyph that paints again on screen is not painted off it.
	for _, o := range []GlyphOpts{
		{State: GlyphWorking}, {State: GlyphWaiting}, {State: GlyphWorking, Claude: true},
	} {
		if n := paintsOver(o, false, false, 2*time.Second); n <= 2 {
			t.Fatalf("%+v paints %d times on screen; the test would prove nothing", o, n)
		}
		if n := paintsOver(o, true, false, 2*time.Second); n != 0 {
			t.Errorf("%+v paints %d times off screen", o, n)
		}
	}
}

func TestReduceMotionStopsTheTimer(t *testing.T) {
	for _, o := range []GlyphOpts{
		{State: GlyphWorking}, {State: GlyphWorkingSlow}, {State: GlyphWaiting}, {State: GlyphWorking, Claude: true},
	} {
		if n := paintsOver(o, false, false, 2*time.Second); n <= 2 {
			t.Fatalf("%+v paints %d times without Reduce Motion; the test would prove nothing", o, n)
		}
		before := paints.Load()
		tt := ui.NewTester(glyphView(o, false), 100, 100)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true})
		tt.Frame()
		afterFrame := paints.Load()
		tt.Advance(2 * time.Second)
		if n := paints.Load() - afterFrame; n != 0 {
			t.Errorf("%+v paints %d times in 2s with Reduce Motion on (%d before)", o, n, afterFrame-before)
		}
	}
}

func TestWorkingGlyphPaintsNewFrames(t *testing.T) {
	// The braille frame changes with the clock, so the image does.
	tt := ui.NewTester(glyphView(GlyphOpts{State: GlyphWorking}, false), 100, 100)
	first := append([]byte(nil), tt.Image().Pix...)
	changed := false
	for i := 0; i < 10 && !changed; i++ {
		tt.Advance(WorkingStep)
		changed = !bytes.Equal(first, tt.Image().Pix)
	}
	if !changed {
		t.Error("the working glyph painted the same image for ten steps")
	}

	idle := ui.NewTester(glyphView(GlyphOpts{State: GlyphIdle}, false), 100, 100)
	before := append([]byte(nil), idle.Image().Pix...)
	idle.Advance(time.Second)
	if !bytes.Equal(before, idle.Image().Pix) {
		t.Error("the idle glyph changed")
	}
}

func TestBrailleGlyphHasInk(t *testing.T) {
	// The font stack shapes the braille glyph: the image is not blank.
	tt := ui.NewTester(glyphView(GlyphOpts{State: GlyphIdle}, false), 100, 100)
	blank := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Background(ui.RGB(0, 0, 0)) }, 100, 100)
	if bytes.Equal(tt.Image().Pix, blank.Image().Pix) {
		t.Error("the idle glyph draws nothing")
	}
	if w, h := brailleLayout(17, "⠶").Size(); w <= 0 || h <= 0 {
		t.Errorf("the glyph layout is %vx%v", w, h)
	}
}

func TestClaudeMarkLoads(t *testing.T) {
	if ClaudeMark() == nil {
		t.Fatal("assets/icons/claude.svg does not load")
	}
	tt := ui.NewTester(glyphView(GlyphOpts{State: GlyphWorking, Claude: true}, false), 100, 100)
	blank := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Background(ui.RGB(0, 0, 0)) }, 100, 100)
	if bytes.Equal(tt.Image().Pix, blank.Image().Pix) {
		t.Error("the Claude mark draws nothing")
	}
}

func join(s []string) string {
	var b bytes.Buffer
	for _, x := range s {
		b.WriteString(x)
	}
	return b.String()
}

// A Claude session that does not orchestrate has a grey mark; the claude hue
// stays for orchestrators, and needs you keeps its hue (D191).
func TestTheMarkOfAPlainSessionIsGrey(t *testing.T) {
	for _, p := range []*theme.Palette{theme.Dark, theme.Light} {
		if got := optsColor(p, GlyphOpts{State: GlyphWorking, Claude: true, Grey: true}); got != p.Ink45 {
			t.Errorf("dark=%v plain session %v, want ink-45", p.Dark, got)
		}
		if got := optsColor(p, GlyphOpts{State: GlyphWorking, Claude: true}); got != p.Claude {
			t.Errorf("dark=%v orchestrator %v, want claude", p.Dark, got)
		}
		if got := optsColor(p, GlyphOpts{State: GlyphNeedsYou, Claude: true, Grey: true}); got != p.Blocked {
			t.Errorf("dark=%v plain session needs you %v, want blocked", p.Dark, got)
		}
	}
}
