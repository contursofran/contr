package kit

import (
	"bytes"
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// at returns a time d after a moment where every cycle of the glyphs starts
// (1,008,000s is a whole number of 90ms, 140ms, .8s and 1.6s), so the tests do
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
		if y := ClaudeHopOffset(GlyphWorking, at(d+ClaudeHop/2), true); y != 0 {
			t.Errorf("claude with reduce motion hopped to %v, want 0", y)
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

func TestClaudeHopsOnceIn800msWhileWorking(t *testing.T) {
	near := func(got, want float32) bool { return got > want-0.01 && got < want+0.01 }
	if y := ClaudeHopOffset(GlyphWorking, at(0), false); y != 0 {
		t.Errorf("hop at 0 is %v", y)
	}
	if y := ClaudeHopOffset(GlyphWorking, at(ClaudeHop/2), false); !near(y, ClaudeHopHeight) {
		t.Errorf("hop at .4s is %v, want the top, %v", y, ClaudeHopHeight)
	}
	if ClaudeHopHeight < 1 || ClaudeHopHeight > 2 {
		t.Errorf("the hop is %v high, want 1 to 2", ClaudeHopHeight)
	}
	if y := ClaudeHopOffset(GlyphWorking, at(ClaudeHop), false); y != 0 {
		t.Errorf("hop after one hop is %v, want 0", y)
	}
	for _, d := range []time.Duration{100, 300, 500, 700} {
		y := ClaudeHopOffset(GlyphWorking, at(d*time.Millisecond), false)
		if y <= 0 || y > ClaudeHopHeight {
			t.Errorf("hop at %vms is %v, want above 0 and at most %v", d, y, ClaudeHopHeight)
		}
	}
	for _, s := range []GlyphState{GlyphIdle, GlyphWorkingSlow, GlyphWaiting, GlyphNeedsYou} {
		if y := ClaudeHopOffset(s, at(ClaudeHop/2), false); y != 0 {
			t.Errorf("%v hopped to %v, want still", s, y)
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

// inkBox returns the bounds of the pixels that are not black, in device
// pixels; ok is false for a black image.
func inkBox(img *image.RGBA) (r image.Rectangle, ok bool) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if c.R == 0 && c.G == 0 && c.B == 0 {
				continue
			}
			p := image.Rect(x, y, x+1, y+1)
			if !ok {
				r, ok = p, true
			} else {
				r = r.Union(p)
			}
		}
	}
	return r, ok
}

// mascotTester shows one Claude glyph at scale 2, as on a Retina screen.
func mascotTester(o GlyphOpts) *ui.Tester {
	o.Claude = true
	tt := ui.NewTester(glyphView(o, false), 100, 100)
	tt.SetScale(2)
	tt.Frame()
	return tt
}

func TestTheGlyphDrawsTheMascot(t *testing.T) {
	if ClaudeMascot() == nil {
		t.Fatal("assets/icons/claude-mascot.svg does not load")
	}
	grid, err := MascotGrid(MascotBanner[:])
	if err != nil {
		t.Fatal(err)
	}
	for _, small := range []bool{false, true} {
		tt := mascotTester(GlyphOpts{State: GlyphNeedsYou, Small: small})
		box, ok := inkBox(tt.Image())
		if !ok {
			t.Fatalf("small=%v: the Claude mascot draws nothing", small)
		}
		g, _ := tt.Find("needs you")
		// The ink is as wide as the glyph box and 5/16 as high; the old
		// mark was as high as wide.
		if w := float32(box.Dx()); w < 2*g.W-1 || w > 2*g.W+1 {
			t.Errorf("small=%v: the ink is %v px wide, want the box, %v px", small, box.Dx(), 2*g.W)
		}
		if h, want := float32(box.Dy()), 2*g.W*mascotInkRows/mascotInkCols; h < want-1.5 || h > want+1.5 { // the small cell is 1.75 px: edges blend
			t.Errorf("small=%v: the ink is %v px high, want %v", small, box.Dy(), want)
		}
		// Centred in the box.
		midX, midY := 2*(g.X+g.W/2), 2*(g.Y+g.H/2)
		if x := float32(box.Min.X+box.Max.X) / 2; x < midX-1 || x > midX+1 {
			t.Errorf("small=%v: the ink is centred at x %v, want %v", small, x, midX)
		}
		if y := float32(box.Min.Y+box.Max.Y) / 2; y < midY-1 || y > midY+1 {
			t.Errorf("small=%v: the ink is centred at y %v, want %v", small, y, midY)
		}
		if small {
			continue
		}
		// In the 16px box a cell is 2×2 device pixels: the image is the
		// grid, cell for cell, eyes and legs included.
		img := tt.Image()
		for row := range MascotRows {
			for col := range MascotCols {
				x, y := box.Min.X+2*(col-mascotInkLeft), box.Min.Y+2*row
				c := img.RGBAAt(x, y)
				ink := c.R != 0 || c.G != 0 || c.B != 0
				if ink != grid[row][col] {
					t.Errorf("cell %d,%d has ink %v, want %v", col, row, ink, grid[row][col])
				}
			}
		}
	}
}

func TestTheWorkingMascotHops(t *testing.T) {
	// The Tester's clock is the wall clock, so the test samples a whole
	// hop from any phase: the top moves by about ClaudeHopHeight.
	tt := mascotTester(GlyphOpts{State: GlyphWorking})
	lo, hi := 1<<30, -1
	for range 8 {
		tt.Advance(ClaudeHop / 8)
		b, _ := inkBox(tt.Image())
		lo, hi = min(lo, b.Min.Y), max(hi, b.Min.Y)
	}
	if d := hi - lo; d < 2 || d > 4 {
		t.Errorf("the working mascot moves %d px (scale 2), want 1 to 2 DIPs", d)
	}
	for _, o := range []struct {
		name   string
		o      GlyphOpts
		reduce bool
	}{
		{"working, Reduce Motion", GlyphOpts{State: GlyphWorking}, true},
		{"working slow", GlyphOpts{State: GlyphWorkingSlow}, false},
		{"waiting", GlyphOpts{State: GlyphWaiting}, false},
		{"idle", GlyphOpts{State: GlyphIdle}, false},
		{"needs you", GlyphOpts{State: GlyphNeedsYou}, false},
	} {
		tt := mascotTester(o.o)
		if o.reduce {
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			tt.Frame()
		}
		before, _ := inkBox(tt.Image())
		for range 4 {
			tt.Advance(ClaudeHop / 4)
			if after, _ := inkBox(tt.Image()); after != before {
				t.Errorf("%s: the mascot moved from %v to %v", o.name, before, after)
				break
			}
		}
	}
}

func join(s []string) string {
	var b bytes.Buffer
	for _, x := range s {
		b.WriteString(x)
	}
	return b.String()
}

// A Claude session that does not orchestrate has a grey mascot; the claude hue
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
