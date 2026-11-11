package kit

import (
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// Each transition of D256 plays for its token, then asks for no frame (D55).

// motionTester builds view in a tester, with Reduce Motion when reduce.
func motionTester(view func(c *ui.Context), reduce bool) *ui.Tester {
	return ui.NewTesterWith(func(c *ui.Context) {
		theme.Apply(c)
		view(c)
	}, 320, 320, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: reduce}})
}

// plays checks that after a change made by change, tt asks for frames until
// length and none after it.
func plays(t *testing.T, tt *ui.Tester, change func(), length time.Duration, what string) {
	t.Helper()
	tt.Frame()
	tt.Advance(time.Second)
	if tt.FramePending() {
		t.Fatalf("%s: a frame is asked before the change", what)
	}
	change()
	tt.Frame()
	tt.Advance(length * 3 / 4)
	if !tt.FramePending() {
		t.Errorf("%s: no frame asked at 3/4 of %v; want the transition to play", what, length)
	}
	tt.Advance(length/4 + 20*time.Millisecond)
	if tt.FramePending() {
		t.Errorf("%s: a frame is asked after %v; want none at rest", what, length)
	}
}

// A row or tab that is selected fades its ink in MotionSelect; the selected
// state is at once. A color fades with Reduce Motion too.
func TestSelectionFadesTheInk(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		sel := false
		var shown ui.Color
		p := theme.Current()
		tt := motionTester(func(c *ui.Context) {
			b := ui.Box(c).Size(20, 20)
			ink := p.Ink60
			if sel {
				ink = p.Ink100
			}
			shown = FadeInk(b, "ink", ink, theme.MotionSelect)
		}, reduce)
		plays(t, tt, func() { sel = true }, theme.MotionSelect, "ink")
		if shown != p.Ink100 {
			t.Errorf("reduce %v: ink %v at rest, want ink-100", reduce, shown)
		}
		sel = false
		tt.Frame()
		tt.Advance(theme.MotionSelect / 2)
		if shown == p.Ink60 || shown == p.Ink100 {
			t.Errorf("reduce %v: ink %v half way, want a mix", reduce, shown)
		}
	}
}

// The agent rows, the flat rows, the card rows and the switch fade when
// selected, and ask for no frame after MotionSelect.
func TestSelectedRowsFade(t *testing.T) {
	sel := false
	tt := motionTester(func(c *ui.Context) {
		AgentRow(c, AgentRowOpts{Label: "row", Title: "Agent", State: GlyphWorking, Selected: sel})
	}, false)
	plays(t, tt, func() { sel = true }, theme.MotionSelect, "agent row")

	sel = false
	tt = motionTester(func(c *ui.Context) {
		AgentRow(c, AgentRowOpts{Label: "row", State: GlyphWorking, Selected: sel, Compact: true})
	}, false)
	plays(t, tt, func() { sel = true }, theme.MotionSelect, "compact agent row")

	sel = false
	tt = motionTester(func(c *ui.Context) {
		Row(c, RowOpts{Text: "main", Selected: sel})
	}, false)
	plays(t, tt, func() { sel = true }, theme.MotionSelect, "card row")

	sel = false
	tt = motionTester(func(c *ui.Context) {
		Row(c, RowOpts{Text: "project", Flat: true, Selected: sel})
	}, false)
	plays(t, tt, func() { sel = true }, theme.MotionSelect, "flat row")

	choice := 0
	tt = motionTester(func(c *ui.Context) { Switch(c, "mode", &choice, "Chat", "Terminal") }, false)
	plays(t, tt, func() { choice = 1 }, theme.MotionSelect, "switch")
}

// A row that turns to needs you fades in MotionState; a selection of the same
// row afterwards fades in MotionSelect.
func TestNeedsYouFadesInMotionState(t *testing.T) {
	needs, sel := false, false
	tt := motionTester(func(c *ui.Context) {
		b := ui.Box(c).Size(20, 20)
		d := RowMotion(b, needs)
		ink := theme.Current().Ink60
		if needs || sel {
			ink = theme.Current().Ink100
		}
		FadeInk(b, "ink", ink, d)
	}, false)
	plays(t, tt, func() { needs = true }, theme.MotionState, "needs you")
	plays(t, tt, func() { needs = false }, theme.MotionState, "needs you ends")
	plays(t, tt, func() { sel = true }, theme.MotionSelect, "selection")
}

// An agent row that turns to needs you fades its title to ink-100 in
// MotionState. A selected row, whose title is already ink-100, fades in the
// "needs you" words in MotionState. No fill changes (D249).
func TestAgentRowTurnsToNeedsYouInMotionState(t *testing.T) {
	for _, selected := range []bool{false, true} {
		needs := false
		tt := motionTester(func(c *ui.Context) {
			AgentRow(c, AgentRowOpts{Label: "row", Title: "Agent", Name: "b-250", Age: "2m", State: GlyphWorking,
				NeedsYou: needs, Reason: "permission", Selected: selected})
		}, false)
		plays(t, tt, func() { needs = true }, theme.MotionState, "agent row turns to needs you")
		if !tt.HasText(NeedsYouText) {
			t.Errorf("selected %v: no %q at rest: %v", selected, NeedsYouText, tt.Texts())
		}
	}
}

// A popover scales and fades in in MotionPopIn and out in MotionPopOut; with
// Reduce Motion it fades only, and still ends.
func TestPopoverPopsInAndOut(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		open := false
		tt := motionTester(func(c *ui.Context) {
			a := ui.Box(c).Size(20, 20)
			ui.PopoverBase(c, a, &open, func(panel ui.Element) {
				PopIn(panel).Size(100, 60).Background(theme.Current().FloatSolid)
			})
		}, reduce)
		plays(t, tt, func() { open = true }, theme.MotionPopIn, "popover in")
		plays(t, tt, func() { open = false }, theme.MotionPopOut, "popover out")
	}
}

// A dialog fades in and its panel scales in MotionPopIn; it fades out in
// MotionPopOut; opened again, it pops in again.
func TestDialogPopsInAndOut(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		open := false
		tt := motionTester(func(c *ui.Context) {
			if !open {
				return
			}
			o := true
			ui.DialogBase(c, &o, func(backdrop, panel ui.Element) {
				panel.Size(100, 60).Background(theme.Current().FloatSolid)
				PopDialog(c, backdrop, panel)
			})
		}, reduce)
		plays(t, tt, func() { open = true }, theme.MotionPopIn, "dialog in")
		plays(t, tt, func() { open = false }, theme.MotionPopOut, "dialog out")
		plays(t, tt, func() { open = true }, theme.MotionPopIn, "dialog in again")
	}
}

// A number rolls in MotionNumber; with Reduce Motion it cross-fades, also in
// MotionNumber.
func TestNumberRolls(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		v := 45.0
		tt := motionTester(func(c *ui.Context) {
			UsageMeter(c, []Meter{{Label: "Context", Value: formatPct(v), Percent: v, Has: true}})
		}, reduce)
		// The meter moves for longer than the digits: check the digits alone.
		tt2 := motionTester(func(c *ui.Context) {
			RollNumber(ui.Text(c, formatPct(v)), v)
		}, reduce)
		plays(t, tt2, func() { v = 52 }, theme.MotionNumber, "number")
		// With Reduce Motion the fill is at once and only the digits fade.
		length := theme.MotionMeter
		if reduce {
			length = theme.MotionNumber
		}
		plays(t, tt, func() { v = 60 }, length, "usage meter")
	}
}

// A meter's fill moves to its width in MotionMeter; with Reduce Motion it is
// there at once and asks for no frame.
func TestMeterFillMoves(t *testing.T) {
	v := 45.0
	var shown float64
	tt := motionTester(func(c *ui.Context) {
		shown = MeterFill(ui.Box(c).Size(40, 4), v)
	}, false)
	plays(t, tt, func() { v = 52 }, theme.MotionMeter, "meter")
	if shown != 52 {
		t.Errorf("fill %v at rest, want 52", shown)
	}
	v = 60
	tt.Frame()
	tt.Advance(theme.MotionMeter / 3)
	if shown <= 52 || shown >= 60 {
		t.Errorf("fill %v at 1/3, want between 52 and 60", shown)
	}

	v = 45
	tt = motionTester(func(c *ui.Context) {
		shown = MeterFill(ui.Box(c).Size(40, 4), v)
	}, true)
	tt.Frame()
	v = 52
	tt.Frame()
	if shown != 52 || tt.FramePending() {
		t.Errorf("reduce motion: fill %v, frame asked %v; want 52 at once and none", shown, tt.FramePending())
	}
}

// PlanUsage moves its bars and rolls its values too, and comes to rest.
func TestPlanUsageComesToRest(t *testing.T) {
	v := 38.0
	tt := motionTester(func(c *ui.Context) {
		PlanUsage(c, []Meter{{Label: "Claude", Value: formatPct(v), Percent: v, Has: true}})
	}, false)
	plays(t, tt, func() { v = 41 }, theme.MotionMeter, "plan usage")
}

func formatPct(v float64) string {
	return strconv.Itoa(int(v)) + "%"
}

// The card of a dialog scales in from 0.96 as it fades in; with Reduce Motion
// it only fades, at its size.
func TestDialogCardScalesIn(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		open := false
		tt := motionTester(func(c *ui.Context) {
			if !open {
				return
			}
			o := true
			ui.DialogBase(c, &o, func(backdrop, panel ui.Element) {
				panel.Size(100, 60).Background(ui.RGBA(255, 0, 0, 1))
				PopDialog(c, backdrop, panel)
			})
		}, reduce)
		tt.Frame()
		open = true
		tt.Frame()
		tt.Advance(theme.MotionPopIn / 3)
		width := redWidth(tt, tt.Image().Bounds().Dy()/2)
		if reduce && width != 100 || !reduce && (width == 0 || width >= 100) {
			t.Errorf("reduce %v: card %d px wide at a third of the way in; want 100 with Reduce Motion, less without", reduce, width)
		}
	}
}

// A popover panel scales in from 0.96 as it fades in; with Reduce Motion it
// only fades, at its size.
func TestPopoverPanelScalesIn(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		open := false
		tt := motionTester(func(c *ui.Context) {
			a := ui.Box(c).Size(20, 20)
			ui.PopoverBase(c, a, &open, func(panel ui.Element) {
				PopIn(panel).Size(100, 60).Background(ui.RGBA(255, 0, 0, 1))
			})
		}, reduce)
		tt.Frame()
		open = true
		tt.Frame()
		tt.Advance(theme.MotionPopIn / 3)
		width := redWidth(tt, 50)
		if reduce && width != 100 || !reduce && (width == 0 || width >= 100) {
			t.Errorf("reduce %v: panel %d px wide at a third of the way in; want 100 with Reduce Motion, less without", reduce, width)
		}
	}
}

// redLeft is the x of the first red pixel of row y of the tester's image, or
// -1.
func redLeft(tt *ui.Tester, y int) int {
	img := tt.Image()
	for x := 0; x < img.Bounds().Dx(); x++ {
		if px := img.RGBAAt(x, y); px.R > px.G+20 {
			return x
		}
	}
	return -1
}

// redWidth counts the red pixels of row y of the tester's image.
func redWidth(tt *ui.Tester, y int) int {
	img := tt.Image()
	n := 0
	for x := 0; x < img.Bounds().Dx(); x++ {
		if px := img.RGBAAt(x, y); px.R > px.G+20 {
			n++
		}
	}
	return n
}

// slideMatch is the identity of the pill in TestPillSlides.
type slideMatch struct{}

// The pill of the selected tab slides to the tab selected now in MotionTab,
// and asks for no frame after it. The selected state is at once. With
// Reduce Motion the pill is at its place at once and asks for no frame.
func TestPillSlides(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		sel := 0
		tt := motionTester(func(c *ui.Context) {
			ui.Row(c).Children(func() {
				for i := range 2 {
					ui.Box(c.Key(i)).Size(100, 20).Children(func() {
						if i == sel {
							ui.Box(c).Absolute().Fill().Background(ui.RGBA(255, 0, 0, 1)).
								Transition(SlideTransition(slideMatch{}))
						}
					})
				}
			})
		}, reduce)
		if reduce {
			tt.Frame()
			sel = 1
			tt.Frame()
			if x := redLeft(tt, 10); x != 100 || tt.FramePending() {
				t.Errorf("reduce motion: pill at x %v, frame asked %v; want 100 at once and none", x, tt.FramePending())
			}
			continue
		}
		plays(t, tt, func() { sel = 1 }, theme.MotionTab, "tab pill")
		if w := redWidth(tt, 10); w != 100 {
			t.Errorf("pill %d px wide at rest, want 100", w)
		}
		sel = 0
		tt.Frame()
		tt.Advance(theme.MotionTab / 3)
		if x := redLeft(tt, 10); x <= 0 || x >= 100 {
			t.Errorf("pill at x %v a third of the way, want between 0 and 100", x)
		}
	}
}

// A panel that shows comes in with a fade in MotionPanel, and asks for no frame after it. A panel
// built with the frame (show false) is at rest at once. With Reduce Motion
// only the fade plays.
func TestPanelComesIn(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		on, shown := false, false
		tt := motionTester(func(c *ui.Context) {
			if !on {
				return
			}
			b := ui.Box(c).Size(100, 60).Background(ui.RGBA(255, 0, 0, 1))
			PanelIn(c, b, !shown, true)
			shown = true
		}, reduce)
		plays(t, tt, func() { on = true }, theme.MotionPanel, "panel in")
		if w := redWidth(tt, 30); w != 100 {
			t.Errorf("reduce %v: panel %d px wide at rest, want 100", reduce, w)
		}
	}
	// Half way the panel is still to the right of its place.
	on := false
	tt := motionTester(func(c *ui.Context) {
		if on {
			PanelIn(c, ui.Box(c).Size(100, 60).Background(ui.RGBA(255, 0, 0, 1)), true, true)
		}
	}, false)
	tt.Frame()
	on = true
	tt.Frame()
	if px := tt.Image().RGBAAt(2, 30); px.R > px.G+100 {
		t.Errorf("panel in its first frame shows at its place: %v", px)
	}
}

// An agent row that arrives fades and moves in in MotionRowIn, and asks for
// no frame after it.
func TestRowComesIn(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		on, built := false, false
		tt := motionTester(func(c *ui.Context) {
			if !on {
				return
			}
			RowIn(c, ui.Column(c).Children(func() {
				AgentRow(c, AgentRowOpts{Label: "row", Title: "Agent", State: GlyphWorking})
			}), !built)
			built = true
		}, reduce)
		plays(t, tt, func() { on = true }, theme.MotionRowIn, "row in")
	}
}
